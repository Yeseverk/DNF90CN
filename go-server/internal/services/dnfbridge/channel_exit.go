package dnfbridge

import (
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

const currentChannelExitRequestBodySize = 1

// handleCurrentChannelExit acknowledges the old game connection before the
// client tears down its scene and opens the selected channel connection.
// Keep reading after the ACK: current NoPack owns the orderly socket close.
func (s *Service) handleCurrentChannelExit(session *gameSession, body []byte, source string) error {
	if len(body) != currentChannelExitRequestBodySize {
		return fmt.Errorf(
			"current channel exit body length = %d, want %d",
			len(body),
			currentChannelExitRequestBodySize,
		)
	}
	if err := s.sendGameUpperRawClassNoCodec(
		session,
		uint16(dnfenum.CmdPacketExit),
		[]byte{1},
		dnfproto.DefaultChannelClassification,
	); err != nil {
		return err
	}
	s.logGameEvent(session, "game-channel-exit-acknowledged",
		"source", source,
		"request_body_len", len(body),
		"response_classification", dnfproto.DefaultChannelClassification,
		"response_msg_id", uint16(dnfenum.CmdPacketExit),
		"response_body", "01",
		"socket_owner", "client_closes_old_channel_after_ack")
	return nil
}
