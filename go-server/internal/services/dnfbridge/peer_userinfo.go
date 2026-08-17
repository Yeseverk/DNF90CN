package dnfbridge

import (
	"context"
	"encoding/binary"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentPeerUserInfoMode = byte(3)

// handleCurrentPeerUserInfoRequest owns the current-client three-byte
// GET_USERINFO request emitted by the town interaction menu. The same legacy
// opcode also owns the role-selector bootstrap, so only the exact mode-3 shape
// is consumed here.
func (s *Service) handleCurrentPeerUserInfoRequest(session *gameSession, body []byte) (bool, error) {
	if len(body) != 3 || body[2] != currentPeerUserInfoMode {
		return false, nil
	}
	targetID := binary.LittleEndian.Uint16(body[:2])
	sourceID := selectedCharacterID(session)
	if sourceID == 0 || targetID == 0 || targetID == sourceID ||
		s.onlinePlayers == nil || !s.onlinePlayers.PeerInSameArea(sourceID, targetID) {
		s.logGameEvent(session, "game-peer-userinfo-request-rejected",
			"source_char_id", sourceID,
			"target_char_id", targetID,
			"mode", body[2],
			"reason", "invalid_or_not_in_same_area")
		return true, nil
	}
	targetSession, online := s.onlineGameSession(targetID)
	if !online {
		s.logGameEvent(session, "game-peer-userinfo-request-rejected",
			"source_char_id", sourceID,
			"target_char_id", targetID,
			"mode", body[2],
			"reason", "target_offline")
		return true, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	loadedID, _, character, found := s.characterForEnter(ctx, targetSession, targetID)
	if !found || loadedID != targetID {
		s.logGameEvent(session, "game-peer-userinfo-request-rejected",
			"source_char_id", sourceID,
			"target_char_id", targetID,
			"mode", body[2],
			"reason", "target_repository_record_unavailable")
		return true, nil
	}
	var legacyRepo dnfrepo.LegacyUserInfoRepository
	if repositories, ok := s.repositoryGroup(); ok {
		legacyRepo = repositories.LegacyUserInfo
	}
	response := s.buildCurrentSelectedUserInfoMode3BodyInContext(
		ctx,
		targetSession,
		legacyRepo,
		character,
		true,
		targetID,
		currentTownRemoteActorOwnerContext(session, targetID),
	)
	s.logGameEvent(session, "game-peer-userinfo-response-send",
		"source_char_id", sourceID,
		"target_char_id", targetID,
		"mode", body[2],
		"msg_id", uint16(dnfenum.CmdPacketSetUDPIPPort),
		"classification", 0,
		"body_len", len(response),
		"body_source", "current_exe_mode3_peer_userinfo")
	return true, s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketSetUDPIPPort),
		response,
		0,
	)
}
