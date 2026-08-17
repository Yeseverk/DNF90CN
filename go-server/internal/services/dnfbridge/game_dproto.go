package dnfbridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dproto"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func (s *Service) gameDprotoMode() string {
	if s == nil {
		return gameDprotoModeLegacy
	}
	return normalizeGameDprotoMode(s.options.gameDprotoMode)
}

func (s *Service) gameDprotoMaxPacketBytes() int {
	if s != nil && s.options.maxPacketBytes > 0 {
		return s.options.maxPacketBytes
	}
	return defaultMaxPacketBytes
}

func (s *Service) validateDprotoConfiguration() error {
	if s.gameDprotoMode() == gameDprotoModeNative && s.dprotoProvider == nil {
		return dproto.ErrProviderUnavailable
	}
	return nil
}

func (s *Service) openGameDprotoSession(ctx context.Context, session *gameSession) error {
	if s.gameDprotoMode() != gameDprotoModeNative {
		return nil
	}
	if s.dprotoProvider == nil {
		return dproto.ErrProviderUnavailable
	}
	info := dproto.ConnectionInfo{
		ConnectionID: session.connID,
		ChannelID:    session.channel.ID,
	}
	if session.conn != nil {
		if address := session.conn.RemoteAddr(); address != nil {
			info.RemoteAddr = address.String()
		}
		if address := session.conn.LocalAddr(); address != nil {
			info.LocalAddr = address.String()
		}
	}
	nativeSession, err := s.dprotoProvider.Open(ctx, info)
	if err != nil {
		return err
	}
	if nativeSession == nil {
		return dproto.ErrProviderUnavailable
	}
	session.dproto = nativeSession
	s.logGameEvent(session, "game-dproto-opened", "mode", gameDprotoModeNative)
	return nil
}

func (s *Service) closeGameDprotoSession(session *gameSession) {
	if session == nil || session.dproto == nil {
		return
	}
	session.wireMu.Lock()
	err := session.dproto.Close()
	session.dproto = nil
	session.wireMu.Unlock()
	if err != nil {
		s.logGameEvent(session, "game-dproto-close-failed", "error", err)
	}
}

func (s *Service) handleGameDprotoPacket(session *gameSession, raw []byte) error {
	if session.dproto == nil {
		envelope, err := dnfproto.ParseDprotoClientEnvelope(raw, s.gameDprotoMaxPacketBytes())
		if err != nil {
			s.logGameEvent(session, "game-dproto-opaque-parse-failed",
				"packet_len", len(raw),
				"reason", "native_dproto_provider_unavailable_and_outer_envelope_invalid",
				"error", err)
			return nil
		}
		s.logGameEvent(session, "game-dproto-opaque-deferred",
			"outer_sequence", envelope.Header.Seq,
			"protected_len", len(envelope.Protected),
			"reason", "native_dproto_provider_unavailable")
		return nil
	}
	maxPacketBytes := s.gameDprotoMaxPacketBytes()
	envelope, err := dnfproto.ParseDprotoClientEnvelope(raw, maxPacketBytes)
	if err != nil {
		return err
	}
	ctx := session.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var decoded dproto.DecodeResult
	err = s.withGameWire(session, func() error {
		var decodeErr error
		decoded, decodeErr = session.dproto.DecodeClient(ctx, envelope.Raw)
		if decodeErr != nil {
			return decodeErr
		}
		return s.writeDprotoProviderPacketsLocked(session, decoded.OutboundPackets, "client_decode_control")
	})
	if err != nil {
		return err
	}
	s.logGameEvent(session, "game-dproto-client-decoded",
		"outer_sequence", envelope.Header.Seq,
		"protected_len", len(envelope.Protected),
		"inner_count", len(decoded.InnerPackets),
		"control_count", len(decoded.OutboundPackets))
	for _, inner := range decoded.InnerPackets {
		if len(inner) > maxPacketBytes {
			return dnfproto.ErrPacketTooLarge
		}
		packet, err := dnfproto.ParseChannelPacket(inner)
		if err != nil {
			return fmt.Errorf("validate decoded inner upper: %w", err)
		}
		if packet.Header.MsgID == dnfproto.DprotoClientEnvelopeOpcode ||
			packet.Header.MsgID == dnfproto.DprotoServerEnvelopeOpcode {
			return fmt.Errorf("decoded nested dproto opcode %d: %w", packet.Header.MsgID, dnfproto.ErrDprotoEnvelopeOpcode)
		}
		if err := s.handleGameUpper(session, inner); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) handleGameDprotoControl(session *gameSession, opcode uint16, raw []byte) error {
	if session == nil || session.dproto == nil {
		return dproto.ErrProviderUnavailable
	}
	ctx := session.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return s.withGameWire(session, func() error {
		outbound, err := session.dproto.HandleClientControl(ctx, opcode, append([]byte(nil), raw...))
		if err != nil {
			return err
		}
		return s.writeDprotoProviderPacketsLocked(session, outbound, "client_control")
	})
}

func (s *Service) withGameWire(session *gameSession, operation func() error) error {
	if session == nil || session.conn == nil {
		return errors.New("dnf game connection is unavailable")
	}
	session.wireMu.Lock()
	defer session.wireMu.Unlock()
	return operation()
}

func (s *Service) writeGameRawPacket(session *gameSession, packet []byte) error {
	return s.withGameWire(session, func() error {
		return s.writeGameRawPacketLocked(session, packet)
	})
}

func (s *Service) writeGameRawPacketLocked(session *gameSession, packet []byte) error {
	_, err := session.conn.Write(packet)
	return err
}

func (s *Service) writeGameUpperPacket(session *gameSession, inner []byte) error {
	return s.withGameWire(session, func() error {
		return s.writeGameUpperPacketLocked(session, inner)
	})
}

func (s *Service) writeGameUpperPacketLocked(session *gameSession, inner []byte) error {
	wire := inner
	protected := false
	if session.dproto != nil {
		ctx := session.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		encoded, err := session.dproto.EncodeServer(ctx, append([]byte(nil), inner...))
		if err != nil {
			return err
		}
		if len(encoded.Packet) == 0 {
			return dproto.ErrEmptyProviderOutput
		}
		wire = encoded.Packet
		protected = encoded.Protected
		outer, err := dnfproto.ParseChannelPacketUnchecked(wire)
		if err != nil {
			return fmt.Errorf("validate dproto server output: %w", err)
		}
		if protected {
			if outer.Header.MsgID != dnfproto.DprotoServerEnvelopeOpcode {
				return fmt.Errorf("protected server opcode %d: %w", outer.Header.MsgID, dnfproto.ErrDprotoEnvelopeOpcode)
			}
		} else if !bytes.Equal(wire, inner) {
			return errors.New("dnf dproto provider mutated an unprotected server packet")
		}
		s.logGameEvent(session, "game-dproto-server-encoded",
			"inner_len", len(inner),
			"wire_len", len(wire),
			"protected", protected,
			"outer_msg_id", outer.Header.MsgID)
	}
	_, err := session.conn.Write(wire)
	return err
}

func (s *Service) writeDprotoProviderPacketsLocked(session *gameSession, packets [][]byte, owner string) error {
	for _, packet := range packets {
		if len(packet) == 0 {
			return dproto.ErrEmptyProviderOutput
		}
		if len(packet) > s.gameDprotoMaxPacketBytes() {
			return dnfproto.ErrPacketTooLarge
		}
		outer, err := dnfproto.ParseChannelPacketUnchecked(packet)
		if err != nil {
			return fmt.Errorf("validate dproto control output: %w", err)
		}
		switch outer.Header.MsgID {
		case dnfproto.DprotoServerControlOpcode, dnfproto.DprotoServerEnvelopeOpcode:
		default:
			return fmt.Errorf("dproto control opcode %d: %w", outer.Header.MsgID, dnfproto.ErrDprotoEnvelopeOpcode)
		}
		s.logGameEvent(session, "game-dproto-control-send",
			"owner", owner,
			"msg_id", outer.Header.MsgID,
			"packet_len", len(packet))
		if _, err := session.conn.Write(packet); err != nil {
			return err
		}
	}
	return nil
}
