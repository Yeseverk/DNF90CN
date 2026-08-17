package dnfbridge

import (
	"errors"
	"fmt"
	"math"
	"time"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
)

const (
	currentChannelInfoEnum                = 1
	currentChannelInfoSuccess             = 1
	currentChannelResidentControllerIndex = 0
	currentChannelCommandPacketCount      = 1519
	currentChannelNotificationPacketCount = 1468
)

var (
	errResidentChannelUnavailable = errors.New("dnf resident channel is unavailable")
	errResidentChannelRange       = errors.New("dnf resident channel field exceeds current EXE range")
)

func currentCommittedChannelOwner(session *gameSession) (byte, error) {
	if session == nil {
		return 0, errResidentChannelUnavailable
	}
	resident := session.residentChannel
	if resident.ID <= 0 || resident.ID > math.MaxUint8 {
		return 0, fmt.Errorf("%w: committed channel=%d", errResidentChannelRange, resident.ID)
	}
	if session.channel.ID != resident.ID {
		return 0, fmt.Errorf(
			"%w: connected channel=%d committed channel=%d",
			errResidentChannelUnavailable,
			session.channel.ID,
			resident.ID,
		)
	}
	return byte(resident.ID), nil
}

// buildCurrentChannelResidentNotice builds the body consumed by current
// NoPack.exe sub_1D70130: u32 protobuf length followed by
// PB_ENUM_NOTIPACKET_CHANNELINFO. Field 7 is the server id and field 8 is the
// channel id. Field 9 is the current client's three-entry local-controller
// index, not the unrestricted raw channel type from channel_info.etc.
func (s *Service) buildCurrentChannelResidentNotice(channel channelcatalog.Channel, now time.Time) ([]byte, error) {
	serverID := s.channelAdvertiseID()
	if serverID < 0 || serverID > math.MaxInt32 ||
		channel.ID <= 0 || channel.ID > math.MaxInt32 {
		return nil, fmt.Errorf("%w: server=%d channel=%d type=%d", errResidentChannelRange, serverID, channel.ID, channel.Type)
	}
	if channel.Port <= 0 || channel.Port > math.MaxInt32 ||
		s.options.initialUDPPort1 < 0 || s.options.initialUDPPort1 > math.MaxInt32 ||
		s.options.initialUDPPort2 < 0 || s.options.initialUDPPort2 > math.MaxInt32 ||
		now.Unix() <= 0 {
		return nil, fmt.Errorf("%w: port=%d udp=%d/%d time=%d",
			errResidentChannelRange,
			channel.Port,
			s.options.initialUDPPort1,
			s.options.initialUDPPort2,
			now.Unix())
	}

	protobuf := make([]byte, 0, 96)
	protobuf = appendProtoVarint(protobuf, 1, currentChannelInfoEnum)
	protobuf = appendProtoVarint(protobuf, 2, currentChannelInfoSuccess)
	protobuf = appendProtoBytes(protobuf, 4, []byte(noticeChanName(channel)))
	protobuf = appendProtoVarint(protobuf, 7, uint64(serverID))
	protobuf = appendProtoVarint(protobuf, 8, uint64(channel.ID))
	protobuf = appendProtoVarint(protobuf, 9, currentChannelResidentControllerIndex)
	protobuf = appendProtoVarint(protobuf, 11, uint64(now.Unix()))
	if s.options.serverIP != "" {
		protobuf = appendProtoBytes(protobuf, 12, []byte(s.options.serverIP))
	}
	protobuf = appendProtoVarint(protobuf, 13, uint64(s.options.initialUDPPort1))
	protobuf = appendProtoVarint(protobuf, 14, uint64(s.options.initialUDPPort2))
	protobuf = appendProtoVarint(protobuf, 30004, currentChannelCommandPacketCount)
	protobuf = appendProtoVarint(protobuf, 30005, currentChannelNotificationPacketCount)

	var writer packetWriter
	writer.writeUint32(uint32(len(protobuf)))
	writer.writeBytes(protobuf)
	return writer.bytes(), nil
}

func (s *Service) sendCurrentChannelResidentNotice(session *gameSession, now time.Time) error {
	if session == nil {
		return errResidentChannelUnavailable
	}
	session.endpointHandshakeMu.Lock()
	defer session.endpointHandshakeMu.Unlock()
	if session.currentChannelResidentNoticeSent {
		s.logGameEvent(session, "game-current-channel-notice-duplicate-suppressed",
			"current_channel_id", session.channel.ID,
			"reason", "one_notice_per_tcp_session")
		return nil
	}
	current := session.channel
	body, err := s.buildCurrentChannelResidentNotice(current, now)
	if err != nil {
		return err
	}
	s.logGameEvent(session, "game-current-channel-notice-send",
		"source_server_id", current.ServerID,
		"advertise_server_id", s.channelAdvertiseID(),
		"current_channel_id", current.ID,
		"current_channel_type", current.Type,
		"controller_index", currentChannelResidentControllerIndex,
		"current_channel_group", current.Group,
		"msg_id", 1,
		"classification", 0,
		"transport", "game_upper",
		"header_size", s.gameUpperHeaderSize(),
		"body_len", len(body),
		"protobuf_len", len(body)-4,
		"body_source", "current_exe_sub_1D70130_pb_enum_notipacket_channelinfo")
	if err := s.sendGameUpperRawClass(session, 1, body, 0); err != nil {
		return err
	}
	session.currentChannelResidentNoticeSent = true
	return nil
}
