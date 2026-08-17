package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const (
	currentPeerChatStateOpcode  = uint16(17)
	currentPeerChatNotifyOpcode = uint16(118)
	currentFriendNotifyOpcode   = uint16(274)
	currentFriendAddOpcode      = uint16(290)
	currentDstrMaximumLength    = 4096
)

var errCurrentSocialBodyInvalid = errors.New("current social command body is invalid")

type currentPeerChatRequest struct {
	MessageType byte
	TargetID    uint16
	Unknown     uint32
	Message     []byte
	TargetName  []byte
}

type currentFriendAddRequest struct {
	TargetID   uint16
	TargetName []byte
}

func (s *Service) handleOnlineSocialCommand(session *gameSession, typ uint16, body []byte) (bool, error) {
	switch typ {
	case currentPeerChatStateOpcode:
		return true, s.handleCurrentPeerChat(session, typ, body)
	case currentFriendAddOpcode:
		return true, s.handleCurrentFriendAdd(session, typ, body)
	case uint16(dnfenum.CmdPacketRegisiterToBlacklist), uint16(dnfenum.CmdPacketDeleteToBlacklist):
		return true, s.handleCurrentBlacklistMutation(session, typ, body)
	default:
		return false, nil
	}
}

func (s *Service) handleCurrentPeerChat(session *gameSession, typ uint16, body []byte) error {
	request, err := decodeCurrentPeerChatRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-peer-chat-request-rejected", "body_len", len(body), "error", err)
		return nil
	}
	sourceID := selectedCharacterID(session)
	if !s.currentSocialPeerAvailable(sourceID, request.TargetID) {
		s.logGameEvent(session, "game-peer-chat-request-rejected",
			"source_char_id", sourceID,
			"target_char_id", request.TargetID,
			"message_type", request.MessageType,
			"reason", "target_not_online_in_same_area")
		return nil
	}
	targetSession, online := s.onlineGameSession(request.TargetID)
	if !online {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	loadedSourceID, sourceName, _, sourceFound := s.characterForEnter(ctx, session, sourceID)
	loadedTargetID, targetName, _, targetFound := s.characterForEnter(ctx, targetSession, request.TargetID)
	if !sourceFound || loadedSourceID != sourceID || !targetFound || loadedTargetID != request.TargetID {
		s.logGameEvent(session, "game-peer-chat-request-rejected",
			"source_char_id", sourceID,
			"target_char_id", request.TargetID,
			"reason", "repository_identity_unavailable")
		return nil
	}
	if len(request.Message) == 0 {
		return nil
	}
	if err := s.sendGameUpperSuccess(session, typ, nil); err != nil {
		return err
	}
	peerState := buildCurrentPeerChatStateBody(request.TargetID, request.MessageType)
	if err := s.sendGameUpperRawClassCodec(session, currentPeerChatStateOpcode, peerState, 0, true); err != nil {
		return err
	}
	incomingState := buildCurrentPeerChatStateBody(sourceID, request.MessageType)
	if err := s.sendGameUpperRawClassCodec(targetSession, currentPeerChatStateOpcode, incomingState, 0, true); err != nil {
		return err
	}
	notice := buildCurrentPeerChatNoticeBody(
		request.MessageType,
		byte(targetSession.channel.ID),
		sourceID,
		rosterNameBytes(sourceName),
		request.Message,
	)
	if err := s.sendGameUpperRawClassCodec(targetSession, currentPeerChatNotifyOpcode, notice, 0, true); err != nil {
		return err
	}
	s.logGameEvent(session, "game-peer-chat-forwarded",
		"source_char_id", sourceID,
		"target_char_id", request.TargetID,
		"target_name", targetName,
		"message_type", request.MessageType,
		"message_len", len(request.Message),
		"body_source", "current_exe_class1_op17_ack_class0_op17_state_class0_op118_message")
	return nil
}

func (s *Service) handleCurrentFriendAdd(session *gameSession, typ uint16, body []byte) error {
	request, err := decodeCurrentFriendAddRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-friend-add-request-rejected", "body_len", len(body), "error", err)
		return nil
	}
	sourceID := selectedCharacterID(session)
	if !s.currentSocialPeerAvailable(sourceID, request.TargetID) {
		s.logGameEvent(session, "game-friend-add-request-rejected",
			"source_char_id", sourceID,
			"target_char_id", request.TargetID,
			"reason", "target_not_online_in_same_area")
		return nil
	}
	targetSession, online := s.onlineGameSession(request.TargetID)
	if !online {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	loadedID, targetName, _, found := s.characterForEnter(ctx, targetSession, request.TargetID)
	if !found || loadedID != request.TargetID {
		return nil
	}
	if err := s.sendGameUpperSuccess(session, typ, nil); err != nil {
		return err
	}
	notice := buildCurrentFriendAddNoticeBody(request.TargetID, rosterNameBytes(targetName), true)
	if err := s.sendGameUpperRawClassCodec(session, currentFriendNotifyOpcode, notice, 0, true); err != nil {
		return err
	}
	s.logGameEvent(session, "game-friend-add-response-send",
		"source_char_id", sourceID,
		"target_char_id", request.TargetID,
		"target_name", targetName,
		"body_source", "current_exe_class1_op290_ack_class0_op274_friend_row")
	return nil
}

func (s *Service) handleCurrentBlacklistMutation(session *gameSession, typ uint16, body []byte) error {
	name, next, ok := readCurrentRawDstr(body, 0)
	if !ok || next != len(body) || len(name) == 0 {
		s.logGameEvent(session, "game-blacklist-mutation-rejected",
			"type", typ,
			"body_len", len(body),
			"reason", "exact_name_dstr_required")
		return nil
	}
	var response packetWriter
	response.writeRawDstr(name)
	s.logGameEvent(session, "game-blacklist-mutation-ack",
		"type", typ,
		"selected_character_id", selectedCharacterID(session),
		"name_len", len(name),
		"body_source", "current_exe_class1_callback_reads_echoed_name_dstr")
	return s.sendGameUpperSuccess(session, typ, response.bytes())
}

func (s *Service) currentSocialPeerAvailable(sourceID, targetID uint16) bool {
	return sourceID != 0 && targetID != 0 && sourceID != targetID &&
		s != nil && s.onlinePlayers != nil && s.onlinePlayers.PeerInSameArea(sourceID, targetID)
}

func decodeCurrentPeerChatRequest(body []byte) (currentPeerChatRequest, error) {
	if len(body) < 15 {
		return currentPeerChatRequest{}, errCurrentSocialBodyInvalid
	}
	request := currentPeerChatRequest{
		MessageType: body[0],
		TargetID:    binary.LittleEndian.Uint16(body[1:3]),
		Unknown:     binary.LittleEndian.Uint32(body[3:7]),
	}
	message, next, ok := readCurrentRawDstr(body, 7)
	if !ok {
		return currentPeerChatRequest{}, errCurrentSocialBodyInvalid
	}
	targetName, next, ok := readCurrentRawDstr(body, next)
	if !ok || next != len(body) || request.TargetID == 0 || len(targetName) == 0 {
		return currentPeerChatRequest{}, errCurrentSocialBodyInvalid
	}
	request.Message = message
	request.TargetName = targetName
	return request, nil
}

func decodeCurrentFriendAddRequest(body []byte) (currentFriendAddRequest, error) {
	if len(body) < 6 {
		return currentFriendAddRequest{}, errCurrentSocialBodyInvalid
	}
	request := currentFriendAddRequest{TargetID: binary.LittleEndian.Uint16(body[:2])}
	name, next, ok := readCurrentRawDstr(body, 2)
	if !ok || next != len(body) || request.TargetID == 0 || len(name) == 0 {
		return currentFriendAddRequest{}, errCurrentSocialBodyInvalid
	}
	request.TargetName = name
	return request, nil
}

func readCurrentRawDstr(body []byte, offset int) ([]byte, int, bool) {
	if offset < 0 || offset > len(body)-4 {
		return nil, offset, false
	}
	length := int32(binary.LittleEndian.Uint32(body[offset : offset+4]))
	if length < 0 || length > currentDstrMaximumLength {
		return nil, offset, false
	}
	start := offset + 4
	end := start + int(length)
	if end < start || end > len(body) {
		return nil, offset, false
	}
	return append([]byte(nil), body[start:end]...), end, true
}

func buildCurrentPeerChatStateBody(peerID uint16, messageType byte) []byte {
	var writer packetWriter
	writer.writeUint16(peerID)
	writer.writeByte(messageType)
	return writer.bytes()
}

func buildCurrentPeerChatNoticeBody(messageType, channelID byte, senderID uint16, senderName, message []byte) []byte {
	var writer packetWriter
	writer.writeByte(messageType)
	writer.writeByte(channelID)
	writer.writeByte(0)
	writer.writeByte(channelID)
	writer.writeUint16(senderID)
	writer.writeRawDstr(senderName)
	writer.writeRawDstr(message)
	return writer.bytes()
}

func buildCurrentFriendAddNoticeBody(characterID uint16, name []byte, online bool) []byte {
	var writer packetWriter
	writer.writeUint16(characterID)
	writer.writeByte(0)
	writer.writeRawDstr(name)
	if online {
		writer.writeByte(1)
	} else {
		writer.writeByte(0)
	}
	return writer.bytes()
}
