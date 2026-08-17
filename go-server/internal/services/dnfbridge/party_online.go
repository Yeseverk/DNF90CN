// party_online.go 处理只依赖当前 game 连接的临时多人组队回包。
// 这里不落 DB，也不替代真正 Party owner；它只把已在线且已选角的 session 组装成客户端可见的队伍快照。
package dnfbridge

import (
	"context"
	"fmt"
	"net"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/party"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

// All online-party mutations below are routed through RuntimePartyManager.
// This bridge keeps the current EXE's packet order and per-client projections
// while the manager remains the sole owner of party identity and membership.

const (
	entryIntoPartyFailureGeneric = 3
	entryIntoPartyFailureInvalid = 19
	entryIntoPartyFailureFull    = 20
	walkoutPartyFailureInvalid   = 19
	walkoutPartyFailureAuthority = 8
	defaultPartyPeerPort         = 10000
)

func (s *Service) handleOnlinePartyCommand(session *gameSession, typ uint16, body []byte) (bool, error) {
	// The old connection can still deliver a buffered party command after a
	// reconnect has made a replacement session authoritative for the same
	// character. Never let that stale packet invite, join, leave, kick, or
	// rebuild the replacement's party. The current session directory is the
	// source of truth; a protocol response to the retired connection would
	// only corrupt its already-disposed client state, so consume it silently.
	if isOnlinePartyCommand(typ) && session != nil && session.selectedCharacterID != 0 {
		identity, current := s.boundGameSessionCharacterSnapshot(session)
		if !current || identity.session != session {
			s.logGameEvent(session, "game-party-command-stale-session-ignored",
				"type", typ,
				"char_id", session.selectedCharacterID,
				"character_generation", session.characterGeneration)
			return true, nil
		}
	}
	switch dnfenum.CmdPacket(typ) {
	case dnfenum.CmdPacketLeaveParty:
		// Always consume a leave request.  The client can send it after a peer
		// disconnect while its old singleton cache is already absent from the
		// central manager; returning it to the legacy path leaves the EXE's
		// non-leader gate intact.
		return true, s.handleOnlineLeaveParty(session, typ)
	case dnfenum.CmdPacketWalkoutPartyMember:
		return true, s.handleOnlineWalkoutPartyMember(session, typ, body)
	case dnfenum.CmdPacketChangeHost:
		return true, s.handleOnlineChangePartyHost(session, body)
	case dnfenum.CmdPacketChangePartyMemberPosition:
		return true, s.handleOnlineChangePartyMemberPosition(session, typ, body)
	case dnfenum.CmdPacketReserveLeaveParty:
		if s.hasManagedRuntimeParty(session) || hasMultiMemberRuntimeParty(session) {
			return true, s.handleOnlineReserveLeaveParty(session, typ, body)
		}
		return false, nil
	case dnfenum.CmdPacketRequestPeer:
		return true, s.handleOnlineRequestPeer(session, typ, body)
	case dnfenum.CmdPacketResponsePeer:
		return true, s.handleOnlineResponsePeer(session, typ, body)
	case dnfenum.CmdPacketEntryIntoParty:
		return true, s.handleOnlineEntryIntoParty(session, typ, body)
	case dnfenum.CmdPacketEntryIntoPartyFinish:
		return true, s.handleOnlineEntryIntoPartyFinish(session, typ)
	case dnfenum.CmdPacketEnterWarroom:
		return s.handleRuntimePartyDirectoryJoin(session, body)
	case dnfenum.CmdPacketQuickJoinRoom:
		return s.handleRuntimePartyDirectoryRefresh(session, body)
	default:
		return false, nil
	}
}

func isOnlinePartyCommand(typ uint16) bool {
	switch dnfenum.CmdPacket(typ) {
	case dnfenum.CmdPacketLeaveParty,
		dnfenum.CmdPacketWalkoutPartyMember,
		dnfenum.CmdPacketChangeHost,
		dnfenum.CmdPacketChangePartyMemberPosition,
		dnfenum.CmdPacketReserveLeaveParty,
		dnfenum.CmdPacketRequestPeer,
		dnfenum.CmdPacketResponsePeer,
		dnfenum.CmdPacketEntryIntoParty,
		dnfenum.CmdPacketEnterWarroom,
		dnfenum.CmdPacketQuickJoinRoom:
		return true
	default:
		return false
	}
}

func (s *Service) handleOnlineRequestPeer(session *gameSession, typ uint16, body []byte) error {
	parsed, err := party.DecodeRequestPeerRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-peer-request-body-invalid",
			"type", typ,
			"body_len", len(body),
			"error", err)
		return nil
	}
	sourceID := selectedCharacterID(session)
	if sourceID == 0 || parsed.TargetID == 0 || parsed.TargetID == sourceID ||
		s.onlinePlayers == nil || !s.onlinePlayers.PeerInSameArea(sourceID, parsed.TargetID) {
		s.logGameEvent(session, "game-peer-request-target-outside-area",
			"type", typ,
			"source_char_id", sourceID,
			"target_id", parsed.TargetID,
			"mode", parsed.Mode)
		return nil
	}
	s.logGameEvent(session, "game-peer-request-accepted",
		"type", typ,
		"source_char_id", sourceID,
		"target_id", parsed.TargetID,
		"mode", parsed.Mode,
		"body_source", "current_exe_sub_325F9D0_request_sub_2261E30_ack_and_sub_1D82CF0_mode15_selection_notice")
	if err := s.sendGameUpperRawClassCodec(
		session,
		typ,
		party.BuildRequestPeerAck(),
		dnfproto.DefaultChannelClassification,
		true,
	); err != nil {
		return err
	}
	if parsed.Mode == 15 {
		// The town interaction menu reads the target actor's current party link,
		// not an invite/join flag from the mode-15 selection notice. Refresh that
		// link at click time from the target's authoritative runtime state so a
		// partyless target offers invite while an active target offers join. This
		// also repairs a stale client-side link left by an earlier party lifecycle.
		targetSession, online := s.onlineGameSession(parsed.TargetID)
		if !online {
			s.logGameEvent(session, "game-peer-selection-party-state-deferred",
				"source_char_id", sourceID,
				"target_id", parsed.TargetID,
				"reason", "target_resident_session_unavailable")
			return nil
		}
		targetParty := runtimePartyStateSnapshot(targetSession)
		projected, projectionErr := s.sendCurrentPartyActorFrameProjection(
			session,
			targetSession,
			targetParty,
			"peer_selection_dynamic_target_party_state",
		)
		if projectionErr != nil {
			return projectionErr
		}
		if !projected {
			s.logGameEvent(session, "game-peer-selection-party-state-deferred",
				"source_char_id", sourceID,
				"target_id", parsed.TargetID,
				"target_party_id", targetParty.PartyID,
				"reason", "target_actor_projection_unavailable")
			return nil
		}
		targetPartyActive := targetParty.PartyID > 0 && len(runtimePartyMembers(targetParty)) > 0
		selectionBody := party.BuildRequestPeerSelectionNotice(parsed.TargetID, parsed.Value0, targetPartyActive)
		s.logGameEvent(session, "game-peer-selection-notice-send",
			"type", 0x0007,
			"source_char_id", sourceID,
			"target_id", parsed.TargetID,
			"target_party_id", targetParty.PartyID,
			"target_party_active", targetPartyActive,
			"party_marker", func() string {
				if targetPartyActive {
					return "0000"
				}
				return "ffff"
			}(),
			"mode", parsed.Mode,
			"body_len", len(selectionBody),
			"body_source", "current_exe_sub_1D82CF0_mode15_party_marker_ffff_absent_else_present_then_sub_325FA70_opens_menu")
		return s.sendGameUpperRawClassCodec(session, 0x0007, selectionBody, 0, true)
	}

	if !isForwardedPeerRequestMode(parsed.Mode) {
		return nil
	}
	sourceIdentity, sourceBound := s.boundGameSessionCharacterSnapshot(session)
	targetIdentity, targetBound := s.onlineGameSessionCharacterSnapshot(parsed.TargetID)
	if !sourceBound || !targetBound || targetIdentity.session == nil {
		return nil
	}
	invitePartyID := uint16(0)
	inviteRecorded := false
	if manager := s.runtimePartyManagerForService(); manager != nil {
		if snapshot, found := manager.SnapshotByUser(sourceIdentity.character, sourceIdentity.generation); found {
			invitePartyID = snapshot.ID
		}
		inviteRecorded = manager.RecordInvite(
			targetIdentity.character,
			targetIdentity.generation,
			sourceIdentity.character,
			sourceIdentity.generation,
			invitePartyID,
			parsed.Mode,
		)
	}
	if !inviteRecorded {
		s.logGameEvent(session, "game-peer-request-invite-deferred",
			"type", typ,
			"source_char_id", sourceID,
			"target_id", parsed.TargetID,
			"mode", parsed.Mode,
			"reason", "session_generation_changed_before_invite_registration")
		return nil
	}
	noticeBody := party.BuildRequestPeerNotice(sourceID, parsed)
	s.logGameEvent(session, "game-peer-request-notice-forward",
		"type", 0x0007,
		"source_char_id", sourceID,
		"target_id", parsed.TargetID,
		"mode", parsed.Mode,
		"body_len", len(noticeBody),
		"body_source", "current_exe_sub_1D82CF0_mode_specific_peer_prompt")
	return s.sendGameUpperRawClassCodec(targetIdentity.session, 0x0007, noticeBody, 0, true)
}

func (s *Service) handleOnlineLeaveParty(session *gameSession, typ uint16) error {
	sourceID := selectedCharacterID(session)
	state := runtimePartyStateSnapshot(session)
	identity, bound := s.boundGameSessionCharacterSnapshot(session)
	if sourceID == 0 || !bound {
		return nil
	}
	nextState, result, removed := s.leaveManagedRuntimeParty(identity)
	if !removed {
		// Do not let an unimportable one-member cache survive just because the
		// manager has already removed it.  A successful native leave ACK plus an
		// empty roster is idempotent and restores solo dungeon permission.
		s.clearRuntimePartyProjection(identity.character, identity.generation)
		s.logGameEvent(session, "game-party-leave-stale-projection-cleared",
			"type", typ,
			"source_char_id", sourceID,
			"reason", result.Reason)
		if err := s.sendGameUpperRawClassCodec(session, typ, []byte{1}, dnfproto.DefaultChannelClassification, true); err != nil {
			return err
		}
		return s.sendRuntimePartyMemberRemoved(session, sourceID, alignedcmd.PartyState{})
	}
	if err := s.sendGameUpperRawClassCodec(session, typ, []byte{1}, dnfproto.DefaultChannelClassification, true); err != nil {
		return err
	}
	if err := s.sendRuntimePartyMemberRemoved(session, sourceID, alignedcmd.PartyState{}); err != nil {
		return err
	}
	if result.Retired != nil {
		s.closeRuntimePartyUDPRelay(int(result.Retired.ID))
		for _, member := range result.Retired.Members {
			if member.UserID == sourceID {
				continue
			}
			if targetSession, online := s.onlineGameSession(member.UserID); online {
				if err := s.sendRuntimePartySnapshot(targetSession, alignedcmd.PartyState{}); err != nil {
					return err
				}
			}
		}
		return s.sendManagedRuntimePartySnapshots(nextState)
	}
	if nextState.PartyID > 0 {
		s.syncRuntimePartyUDPRelay(nextState)
	} else {
		s.closeRuntimePartyUDPRelay(state.PartyID)
	}
	if err := s.broadcastRuntimePartyMemberRemoved(state, nextState, sourceID, session); err != nil {
		return err
	}
	return nil
}

func (s *Service) handleOnlineWalkoutPartyMember(session *gameSession, typ uint16, body []byte) error {
	parsed, err := party.DecodeWalkoutPartyMemberRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-party-walkout-body-short", "type", typ, "body_len", len(body), "error", err)
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureInvalid)
	}
	identity, bound := s.boundGameSessionCharacterSnapshot(session)
	if !bound {
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureInvalid)
	}
	manager := s.runtimePartyManagerForService()
	snapshot, found := manager.SnapshotByUser(identity.character, identity.generation)
	if !found {
		snapshot, found = s.bootstrapManagedRuntimePartyForSession(session)
	}
	if !found {
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureInvalid)
	}
	if snapshot.Leader != identity.character {
		s.logGameEvent(session, "game-party-walkout-not-leader",
			"type", typ,
			"source_char_id", identity.character,
			"leader_id", snapshot.Leader,
			"slot", parsed.Slot)
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureAuthority)
	}
	var target party.RuntimePartyMember
	for _, member := range snapshot.Members {
		if member.Slot == parsed.Slot {
			target = member
			break
		}
	}
	if target.UserID == 0 || target.UserID == identity.character {
		s.logGameEvent(session, "game-party-walkout-invalid-slot",
			"type", typ,
			"source_char_id", identity.character,
			"slot", parsed.Slot,
			"member_count", len(snapshot.Members))
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureInvalid)
	}
	previousState := snapshot.StateFor(identity.character)
	result := manager.Kick(identity.character, identity.generation, target.UserID, target.SessionGeneration)
	if !result.OK {
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureInvalid)
	}
	nextState := s.publishRuntimePartyMembershipSnapshot(result.Party)
	if err := s.sendGameUpperRawClassCodec(session, typ, []byte{1}, dnfproto.DefaultChannelClassification, true); err != nil {
		return err
	}
	if err := s.broadcastRuntimePartyWalkoutNotice(previousState, parsed.Slot, 0); err != nil {
		return err
	}
	if nextState.PartyID <= 0 {
		s.closeRuntimePartyUDPRelay(int(result.Party.ID))
	} else {
		s.syncRuntimePartyUDPRelay(nextState)
	}
	if targetSession, ok := s.onlineGameSession(target.UserID); ok {
		s.clearRuntimePartyProjection(target.UserID, target.SessionGeneration)
		if err := s.sendRuntimePartyMemberRemoved(targetSession, target.UserID, alignedcmd.PartyState{}); err != nil {
			return err
		}
	}
	if err := s.broadcastRuntimePartyMemberRemoved(previousState, nextState, target.UserID, nil); err != nil {
		return err
	}
	return nil
}

// handleOnlineChangePartyHost implements the current client's real delegate-
// leader command. NoPack sends class1/op121 with one selected party slot. The
// party window derives leadership from slot zero, so changing only UserID is
// insufficient: clear the old client-side table, move the new leader to slot
// zero, then rebuild op9 for every member. The already-connected P2P transport
// must remain intact: replaying op153/op11 here makes the current EXE start a
// second endpoint handshake and leaves one side stuck in "connecting".
func (s *Service) handleOnlineChangePartyHost(session *gameSession, body []byte) error {
	if len(body) < 1 {
		return nil
	}
	identity, bound := s.boundGameSessionCharacterSnapshot(session)
	if !bound {
		return nil
	}
	manager := s.runtimePartyManagerForService()
	snapshot, found := manager.SnapshotByUser(identity.character, identity.generation)
	if !found {
		snapshot, found = s.bootstrapManagedRuntimePartyForSession(session)
	}
	if !found || snapshot.Leader != identity.character {
		return nil
	}
	var target party.RuntimePartyMember
	for _, member := range snapshot.Members {
		if member.Slot == body[0] {
			target = member
			break
		}
	}
	if target.UserID == 0 || target.UserID == identity.character {
		return nil
	}
	result := manager.TransferLeader(identity.character, identity.generation, target.UserID, target.SessionGeneration)
	if !result.OK || result.Retired == nil {
		return nil
	}

	// 86JP retires the old generation first. Clearing the whole client party
	// table before the fresh snapshot prevents the current EXE from retaining
	// a stale slot-zero leader or the previous relay endpoints.
	s.closeRuntimePartyUDPRelay(int(result.Retired.ID))
	for _, member := range result.Retired.Members {
		memberSession, online := s.onlineGameSession(member.UserID)
		if !online {
			continue
		}
		if err := s.sendRuntimePartySnapshot(memberSession, alignedcmd.PartyState{}); err != nil {
			return err
		}
	}
	state := s.publishRuntimePartySnapshot(result.Party)
	s.syncRuntimePartyUDPRelay(state)
	if err := s.sendManagedRuntimePartySnapshots(state); err != nil {
		return err
	}
	s.logGameEvent(session, "game-party-change-host-committed",
		"source_char_id", identity.character,
		"new_leader_id", target.UserID,
		"selected_slot", body[0],
		"old_party_id", result.Retired.ID,
		"new_party_id", result.Party.ID,
		"member_count", len(result.Party.Members),
		"wire_contract", "86jp_retire_old_party_generation_then_clear_and_fresh_realtime_endpoint_roster")
	return nil
}

func (s *Service) handleOnlineChangePartyMemberPosition(session *gameSession, typ uint16, body []byte) error {
	parsed, err := party.DecodeChangePartyMemberPositionRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-party-change-position-body-invalid", "type", typ, "body_len", len(body), "error", err)
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureInvalid)
	}
	identity, bound := s.boundGameSessionCharacterSnapshot(session)
	if !bound {
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureInvalid)
	}
	manager := s.runtimePartyManagerForService()
	snapshot, found := manager.SnapshotByUser(identity.character, identity.generation)
	if !found {
		snapshot, found = s.bootstrapManagedRuntimePartyForSession(session)
	}
	if !found || len(snapshot.Members) <= 1 {
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureInvalid)
	}
	if snapshot.Leader != identity.character {
		s.logGameEvent(session, "game-party-change-position-not-leader",
			"type", typ,
			"source_char_id", identity.character,
			"leader_id", snapshot.Leader,
			"slot", parsed.Slot,
			"position", parsed.Position)
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureAuthority)
	}
	visibleState := snapshot.StateFor(identity.character)
	fromMember, sourceOK := runtimePartyMemberBySlot(visibleState, parsed.Slot)
	if !sourceOK || fromMember.UserID == 0 || fromMember.UserID == snapshot.Leader {
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureInvalid)
	}
	fromSlot := byte(0xff)
	for _, member := range snapshot.Members {
		if member.UserID == fromMember.UserID {
			fromSlot = member.Slot
			break
		}
	}
	toSlot := parsed.Position
	if destination, destinationOK := runtimePartyMemberBySlot(visibleState, parsed.Position); destinationOK && destination.UserID != 0 {
		for _, member := range snapshot.Members {
			if member.UserID == destination.UserID {
				toSlot = member.Slot
				break
			}
		}
	} else {
		for candidate := byte(1); candidate < 4; candidate++ {
			occupied := false
			for _, member := range snapshot.Members {
				if member.Slot == candidate {
					occupied = true
					break
				}
			}
			if !occupied {
				toSlot = candidate
				break
			}
		}
	}
	result := manager.Reposition(identity.character, identity.generation, fromSlot, toSlot)
	if !result.OK {
		s.logGameEvent(session, "game-party-change-position-noop",
			"type", typ,
			"source_char_id", identity.character,
			"slot", parsed.Slot,
			"position", parsed.Position,
			"member_count", len(snapshot.Members))
		return s.sendPartyCommandFailure(session, typ, walkoutPartyFailureInvalid)
	}
	nextState := s.publishRuntimePartySnapshot(result.Party)
	if err := s.broadcastRuntimePartyChangePosition(nextState, parsed.Slot, parsed.Position); err != nil {
		return err
	}
	return nil
}

func (s *Service) broadcastRuntimePartyWalkoutNotice(state alignedcmd.PartyState, slot byte, mode byte) error {
	body := party.BuildWalkoutPartyMemberNotice(slot, mode)
	for _, member := range runtimePartyMembers(state) {
		session, ok := s.onlineGameSession(member.UserID)
		if !ok {
			continue
		}
		if err := s.sendGameUpperRawClassCodec(session, uint16(dnfenum.CmdPacketRequestPeer), body, dnfproto.DefaultChannelClassification, true); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) broadcastRuntimePartyChangePosition(state alignedcmd.PartyState, fromSlot byte, toSlot byte) error {
	body := party.BuildChangePartyMemberPositionAck(toSlot, 1)
	for _, member := range runtimePartyMembers(state) {
		session, ok := s.onlineGameSession(member.UserID)
		if !ok {
			continue
		}
		if err := s.sendGameUpperRawClassCodec(session, uint16(dnfenum.CmdPacketChangePartyMemberPosition), body, dnfproto.DefaultChannelClassification, true); err != nil {
			return err
		}
		if err := s.sendRuntimePartyRosterLocal(session, state, "change_party_member_position_after_ack"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) handleOnlineReserveLeaveParty(session *gameSession, typ uint16, body []byte) error {
	parsed, err := party.DecodeReserveLeavePartyRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-party-reserve-leave-body-short", "type", typ, "body_len", len(body), "error", err)
		return s.sendGameUpperRawClassCodec(session, typ, []byte{0}, dnfproto.DefaultChannelClassification, true)
	}
	identity, bound := s.boundGameSessionCharacterSnapshot(session)
	if !bound {
		return s.sendGameUpperRawClassCodec(session, typ, []byte{0}, dnfproto.DefaultChannelClassification, true)
	}
	manager := s.runtimePartyManagerForService()
	snapshot, found := manager.SnapshotByUser(identity.character, identity.generation)
	if !found {
		snapshot, found = s.bootstrapManagedRuntimePartyForSession(session)
	}
	if !found {
		return s.sendGameUpperRawClassCodec(session, typ, []byte{0}, dnfproto.DefaultChannelClassification, true)
	}
	bodyResp := party.BuildReserveLeavePartyAck(parsed.Flag, identity.character)
	for _, member := range snapshot.Members {
		targetSession, ok := s.onlineGameSession(member.UserID)
		if !ok {
			continue
		}
		if err := s.sendGameUpperRawClassCodec(targetSession, typ, bodyResp, dnfproto.DefaultChannelClassification, true); err != nil {
			return err
		}
	}
	s.logGameEvent(session, "game-party-reserve-leave-broadcast",
		"type", typ,
		"source_char_id", identity.character,
		"flag", parsed.Flag,
		"member_count", len(snapshot.Members))
	return nil
}

func (s *Service) handleOnlineResponsePeer(session *gameSession, typ uint16, body []byte) error {
	parsed, err := party.DecodeResponsePeerRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-party-response-peer-body-short", "type", typ, "body_len", len(body), "error", err)
		return nil
	}
	s.logGameEvent(session, "game-party-response-peer-received",
		"type", typ,
		"source_char_id", selectedCharacterID(session),
		"target_id", parsed.TargetID,
		"mode", parsed.Mode,
		"value", parsed.Value)
	if !isForwardedPeerRequestMode(parsed.Mode) {
		return nil
	}
	acceptorIdentity, acceptorBound := s.boundGameSessionCharacterSnapshot(session)
	targetIdentity, targetBound := s.onlineGameSessionCharacterSnapshot(parsed.TargetID)
	invitePartyID := uint16(0)
	inviteConsumed := false
	if acceptorBound && targetBound {
		if manager := s.runtimePartyManagerForService(); manager != nil {
			invitePartyID, inviteConsumed = manager.ConsumeInvite(
				acceptorIdentity.character,
				acceptorIdentity.generation,
				targetIdentity.character,
				targetIdentity.generation,
				parsed.Mode,
			)
		}
	}
	if !inviteConsumed {
		s.logGameEvent(session, "game-peer-response-without-pending-request",
			"type", typ,
			"source_char_id", selectedCharacterID(session),
			"target_id", parsed.TargetID,
			"mode", parsed.Mode)
		return nil
	}
	_ = invitePartyID // recorded generation is revalidated by create/join below.
	ackPayload := party.BuildResponsePeerAckPayload(parsed.TargetID, parsed.Mode)
	if err := s.sendGameUpperSuccess(session, typ, ackPayload); err != nil {
		return err
	}
	targetSession := targetIdentity.session
	if targetSession == nil || session == nil || session.selectedCharacterID == 0 {
		s.logGameEvent(session, "game-party-response-peer-target-offline",
			"type", typ,
			"source_char_id", selectedCharacterID(session),
			"target_id", parsed.TargetID)
		return nil
	}
	responseNotice := party.BuildResponsePeerNotice(session.selectedCharacterID, parsed.Mode, parsed.Value)
	if err := s.sendGameUpperRawClassCodec(targetSession, 0x0008, responseNotice, 0, true); err != nil {
		return err
	}
	if peerResponseRejected(parsed) {
		s.logGameEvent(session, "game-peer-response-rejected",
			"type", typ,
			"source_char_id", selectedCharacterID(session),
			"target_id", parsed.TargetID,
			"mode", parsed.Mode,
			"value", parsed.Value)
		return nil
	}
	if parsed.Mode == 1 {
		s.beginOnlineItemTrade(session, targetSession)
		s.logGameEvent(session, "game-trade-response-peer-forwarded",
			"type", typ,
			"source_char_id", session.selectedCharacterID,
			"target_id", parsed.TargetID,
			"mode", parsed.Mode,
			"value", parsed.Value,
			"body_source", "current_exe_class1_op11_trade_ack_and_class0_op8_peer_notice")
		return nil
	}
	if !isPartyInviteResponse(parsed) {
		return nil
	}
	// op8 is emitted by the accepting player, so it joins the invite target's
	// already-owned party (or creates that target's lobby).
	state, result, ok := s.createOrJoinManagedRuntimeParty(session, targetSession)
	if !ok {
		s.logGameEvent(session, "game-party-response-peer-party-full",
			"type", typ,
			"source_char_id", session.selectedCharacterID,
			"target_id", parsed.TargetID,
			"reason", result.Reason)
		return nil
	}
	return s.sendManagedRuntimePartySnapshots(state)
}

func (s *Service) handleOnlineEntryIntoParty(session *gameSession, typ uint16, body []byte) error {
	parsed, err := party.DecodeEntryIntoPartyRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-party-entry-body-short", "type", typ, "body_len", len(body), "error", err)
		return s.sendPartyCommandFailure(session, typ, entryIntoPartyFailureGeneric)
	}
	if session == nil || session.selectedCharacterID == 0 || parsed.TargetID == 0 || parsed.TargetID > 0xffff {
		s.logGameEvent(session, "game-party-entry-invalid-context",
			"type", typ,
			"source_char_id", selectedCharacterID(session),
			"target_id", parsed.TargetID)
		return s.sendPartyCommandFailure(session, typ, entryIntoPartyFailureInvalid)
	}
	targetID := uint16(parsed.TargetID)
	if targetID == session.selectedCharacterID {
		s.logGameEvent(session, "game-party-entry-self-rejected", "type", typ, "char_id", session.selectedCharacterID)
		return s.sendPartyCommandFailure(session, typ, entryIntoPartyFailureInvalid)
	}
	targetSession, ok := s.onlineGameSession(targetID)
	if !ok {
		s.logGameEvent(session, "game-party-entry-target-offline",
			"type", typ,
			"source_char_id", session.selectedCharacterID,
			"target_id", targetID)
		return s.sendPartyCommandFailure(session, typ, entryIntoPartyFailureGeneric)
	}
	// op706 is emitted by the existing party owner while it attaches the
	// selected target. Its source is therefore the anchor/leader, unlike the
	// op8 invite response where the source is the accepting joiner.
	state, result, ok := s.createOrJoinManagedRuntimeParty(targetSession, session)
	if !ok {
		s.logGameEvent(session, "game-party-entry-party-full",
			"type", typ,
			"source_char_id", session.selectedCharacterID,
			"target_id", targetID,
			"reason", result.Reason)
		return s.sendPartyCommandFailure(session, typ, entryIntoPartyFailureFull)
	}
	if err := s.sendGameUpperRawClassCodec(session, typ, party.BuildEntryIntoPartyAck(parsed.TargetID, uint32(session.selectedCharacterID)), dnfproto.DefaultChannelClassification, true); err != nil {
		return err
	}
	if err := s.sendManagedRuntimePartySnapshots(state); err != nil {
		return err
	}
	s.logGameEvent(session, "game-party-entry-linked-online-sessions",
		"type", typ,
		"source_char_id", session.selectedCharacterID,
		"target_id", targetID,
		"member_count", len(state.Members))
	return nil
}

func (s *Service) handleOnlineEntryIntoPartyFinish(session *gameSession, typ uint16) error {
	state := runtimePartyStateSnapshot(session)
	if state.PartyID > 0 && len(state.Members) > 1 {
		userState := state.UserState
		if userState == 0 {
			userState = 1
		}
		return s.sendGameUpperRawClassCodec(session, typ, party.BuildEntryIntoPartyFinishEmptyBody(userState), 0, false)
	}
	s.logGameEvent(session, "game-party-entry-finish-missing-context", "type", typ, "source_char_id", selectedCharacterID(session))
	// Current EXE registers only the class0 state/count reader for op706. It has
	// no class1 failure envelope, so the old {0,error} reply would be parsed as
	// state/count and consume bytes from following packets.
	return nil
}

func isPartyInviteResponse(req party.ResponsePeerRequest) bool {
	return req.Mode == 0 || req.Mode == 4 || req.Mode == 12 || req.Mode == 13
}

func isForwardedPeerRequestMode(mode byte) bool {
	return mode == 0 || mode == 1 || mode == 4 || mode == 12 || mode == 13
}

func peerResponseRejected(req party.ResponsePeerRequest) bool {
	// Current EXE sub_269FBF0 accepts ordinary mode-0/1/12 prompts by sending
	// value zero. Quick-party modes 4/13 use explicit value 1 for accept and
	// value 0 for reject (sub_2114B10/sub_2114BB0).
	return (req.Mode == 4 || req.Mode == 13) && req.Value == 0
}

func (s *Service) sendPartyCommandFailure(session *gameSession, typ uint16, code byte) error {
	return s.sendGameUpperRawClassCodec(session, typ, []byte{0, code}, dnfproto.DefaultChannelClassification, true)
}

func (s *Service) sendRuntimePartySnapshot(session *gameSession, state alignedcmd.PartyState) error {
	if err := s.sendRuntimePartySnapshotLocal(session, state); err != nil {
		return err
	}
	return s.broadcastCurrentPartyActorProjection(session, state, "runtime_party_snapshot")
}

func (s *Service) sendRuntimePartySnapshotLocal(session *gameSession, state alignedcmd.PartyState) error {
	partyActive := state.PartyID > 0 && len(runtimePartyMembers(state)) > 0
	var beforeFrame func() error
	if partyActive {
		beforeFrame = func() error {
			if err := s.sendGameUpperRawClassCodec(session, 0x0099, party.BuildSingleMemberRealtimeInfo(state), 0, true); err != nil {
				return err
			}
			body := s.buildRuntimePartyPeerEndpointInfoForReceiver(session, state)
			s.logPacketEvent("game-runtime-party-peer-endpoints-send",
				"conn_id", session.connID,
				"char_id", session.selectedCharacterID,
				"party_id", state.PartyID,
				"member_count", len(runtimePartyMembers(state)),
				"body_len", len(body),
				"msg_id", 0x000b,
				"classification", 0,
				"ordering", "op153_then_op11_before_op9")
			return s.sendGameUpperRawClassCodec(session, 0x000b, body, 0, true)
		}
	}
	frameSent, err := s.sendCurrentPartyActorFrameProjectionBefore(
		session,
		session,
		state,
		"runtime_party_snapshot",
		beforeFrame,
	)
	if err != nil {
		return err
	}
	if !frameSent {
		return nil
	}
	if !partyActive {
		return s.sendGameUpperRawClassCodec(session, 0x0099, party.BuildEmptyRealtimeInfo(), 0, true)
	}
	return nil
}

// sendRuntimePartyRosterLocal refreshes the current client's scene-owned
// party tables. The client consumes op153 before op9: op153 carries the
// member HP and stable slot index used by both the party frame and minimap.
// Sending only op9 after rebuilding a town actor leaves the EXE with an old
// (or empty) realtime cache, which presents a zero-HP peer and omits its
// minimap marker until another complete party operation happens.
// The dynamic UDP endpoints in op11 are a party-formation handshake, not a
// general scene refresh: replaying op11 during every town/selector transition
// makes the current client tear down a healthy peer link and enter
// "connecting" again. Therefore scene reconstruction is strictly
// op153-then-op9, without an op11 re-handshake.
func (s *Service) sendRuntimePartyRosterLocal(session *gameSession, state alignedcmd.PartyState, source string) error {
	if err := s.sendRuntimePartyRealtimeInfoLocal(session, state); err != nil {
		return err
	}
	_, err := s.sendCurrentPartyActorFrameProjection(session, session, state, source)
	return err
}

func (s *Service) buildRuntimePartyPeerEndpointInfo(state alignedcmd.PartyState) []byte {
	members := runtimePartyMembers(state)
	endpoints := make([]party.PeerEndpoint, 0, len(members))
	for _, member := range members {
		if member.UserID == 0 {
			continue
		}
		endpoint := party.PeerEndpoint{
			UserID:    member.UserID,
			IPv4:      [4]byte{127, 0, 0, 1},
			Port:      defaultPartyPeerPort,
			AccountID: uint32(member.UserID),
		}
		if peer, ok := s.onlineGameSession(member.UserID); ok && peer.conn != nil {
			registration := currentPartyPeerEndpointSnapshot(peer)
			if registration.Port != 0 {
				endpoint.Port = registration.Port
			}
			if tcpAddress, ok := peer.conn.RemoteAddr().(*net.TCPAddr); ok {
				if ipv4 := tcpAddress.IP.To4(); len(ipv4) == net.IPv4len {
					copy(endpoint.IPv4[:], ipv4)
				}
			}
		}
		endpoints = append(endpoints, endpoint)
	}
	return party.BuildPeerEndpointInfo(endpoints)
}

// buildRuntimePartyPeerEndpointInfoForReceiver preserves the current EXE's
// PARTY_IP_INFO layout while selecting a directed UDP relay port for every
// remote party member. The receiver's own row remains its client-registered
// endpoint, as the client uses it to retain its local socket identity.
func (s *Service) buildRuntimePartyPeerEndpointInfoForReceiver(receiver *gameSession, state alignedcmd.PartyState) []byte {
	members := runtimePartyMembers(state)
	if receiver == nil || receiver.selectedCharacterID == 0 || !s.syncRuntimePartyUDPRelay(state) {
		return s.buildRuntimePartyPeerEndpointInfo(state)
	}
	relay := s.currentPartyUDPRelay()
	if relay == nil {
		return s.buildRuntimePartyPeerEndpointInfo(state)
	}
	endpoints := make([]party.PeerEndpoint, 0, len(members))
	for _, member := range members {
		if member.UserID == 0 {
			continue
		}
		endpoint := party.PeerEndpoint{
			UserID:    member.UserID,
			IPv4:      [4]byte{127, 0, 0, 1},
			Port:      defaultPartyPeerPort,
			AccountID: uint32(member.UserID),
		}
		if member.UserID != receiver.selectedCharacterID {
			if address, port, ok := relay.Endpoint(uint16(state.PartyID), receiver.selectedCharacterID, member.UserID); ok {
				endpoint.IPv4 = address
				endpoint.Port = port
				endpoints = append(endpoints, endpoint)
				continue
			}
			// Sync is transactional, so reaching this fallback means shutdown or a
			// concurrent room teardown. Publish the previous direct form for the
			// entire snapshot rather than mixing a partial relay topology.
			return s.buildRuntimePartyPeerEndpointInfo(state)
		}
		if peer, ok := s.onlineGameSession(member.UserID); ok && peer.conn != nil {
			registration := currentPartyPeerEndpointSnapshot(peer)
			if registration.Port != 0 {
				endpoint.Port = registration.Port
			}
			if tcpAddress, ok := peer.conn.RemoteAddr().(*net.TCPAddr); ok {
				if ipv4 := tcpAddress.IP.To4(); len(ipv4) == net.IPv4len {
					copy(endpoint.IPv4[:], ipv4)
				}
			}
		}
		endpoints = append(endpoints, endpoint)
	}
	return party.BuildPeerEndpointInfo(endpoints)
}

func (s *Service) currentPartyUDPRelay() *currentPartyUDPRelay {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	relay := s.partyUDPRelay
	s.mu.Unlock()
	return relay
}

// syncRuntimePartyUDPRelay ensures that the directed relay set exactly
// matches the current live party. It refuses to use a relay until every party
// member still owns the selected TCP session and that session has an observed
// IPv4 endpoint; the caller then safely falls back to the proven LAN form.
func (s *Service) syncRuntimePartyUDPRelay(state alignedcmd.PartyState) bool {
	relay := s.currentPartyUDPRelay()
	if state.PartyID > 0 && state.PartyID < int(^uint16(0)) {
		manager := s.runtimePartyManagerForService()
		snapshot, found := manager.SnapshotByID(uint16(state.PartyID))
		if !found {
			return false
		}
		state = snapshot.StateFor(snapshot.Leader)
	}
	members := runtimePartyMembers(state)
	if relay == nil || !relay.Enabled() || state.PartyID <= 0 || len(members) < 2 {
		return false
	}
	bindings := make([]currentPartyUDPRelayBinding, 0, len(members))
	for _, member := range members {
		identity, ok := s.onlineGameSessionCharacterSnapshot(member.UserID)
		if !ok || identity.session == nil || identity.generation == 0 || identity.session.conn == nil {
			return false
		}
		tcpAddress, ok := identity.session.conn.RemoteAddr().(*net.TCPAddr)
		if !ok || tcpAddress.IP == nil {
			return false
		}
		ipv4 := tcpAddress.IP.To4()
		if len(ipv4) != net.IPv4len {
			return false
		}
		binding := currentPartyUDPRelayBinding{characterID: identity.character, generation: identity.generation}
		copy(binding.address[:], ipv4)
		bindings = append(bindings, binding)
	}
	return relay.Sync(uint16(state.PartyID), bindings)
}

func (s *Service) closeRuntimePartyUDPRelay(partyID int) {
	if partyID <= 0 || partyID > int(^uint16(0)) {
		return
	}
	if relay := s.currentPartyUDPRelay(); relay != nil {
		relay.CloseParty(uint16(partyID))
	}
}

func (s *Service) sendRuntimePartyRealtimeInfoLocal(session *gameSession, state alignedcmd.PartyState) error {
	if session == nil || state.PartyID <= 0 || len(runtimePartyMembers(state)) <= 1 {
		return nil
	}
	return s.sendGameUpperRawClassCodec(session, 0x0099, party.BuildSingleMemberRealtimeInfo(state), 0, true)
}

func (s *Service) broadcastRuntimePartyMemberRemoved(previousState, nextState alignedcmd.PartyState, removedID uint16, skip *gameSession) error {
	members := runtimePartyMembers(nextState)
	if nextState.PartyID <= 0 {
		members = runtimePartyMembers(previousState)
	}
	for _, member := range members {
		if member.UserID == removedID {
			continue
		}
		session, ok := s.onlineGameSession(member.UserID)
		if !ok || session == skip {
			continue
		}
		storeRuntimePartyState(session, nextState)
		if err := s.sendRuntimePartyMemberRemoved(session, removedID, nextState); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) sendRuntimePartyMemberRemoved(session *gameSession, _ uint16, state alignedcmd.PartyState) error {
	frameSent, err := s.sendCurrentPartyFrameProjection(session, state, "runtime_party_member_removed")
	if err != nil {
		return err
	}
	if !frameSent {
		return nil
	}
	realtime := party.BuildEmptyRealtimeInfo()
	if state.PartyID > 0 {
		realtime = party.BuildSingleMemberRealtimeInfo(state)
	}
	if err := s.sendGameUpperRawClassCodec(session, 0x0099, realtime, 0, true); err != nil {
		return err
	}
	if state.PartyID <= 0 {
		return s.broadcastCurrentPartyActorProjection(session, state, "runtime_party_member_removed")
	}
	return s.broadcastCurrentPartyActorProjection(session, state, "runtime_party_member_removed")
}

func (s *Service) sendCurrentPartyFrameProjection(session *gameSession, state alignedcmd.PartyState, source string) (bool, error) {
	return s.sendCurrentPartyActorFrameProjection(session, session, state, source)
}

func (s *Service) sendCurrentPartyActorFrameProjection(receiver *gameSession, actor *gameSession, state alignedcmd.PartyState, source string) (bool, error) {
	return s.sendCurrentPartyActorFrameProjectionBefore(receiver, actor, state, source, nil)
}

func (s *Service) sendCurrentPartyActorFrameProjectionBefore(
	receiver *gameSession,
	actor *gameSession,
	state alignedcmd.PartyState,
	source string,
	beforeFrame func() error,
) (bool, error) {
	receiverIdentity, ok := s.boundGameSessionCharacterSnapshot(receiver)
	if !ok {
		return false, nil
	}
	actorIdentity, ok := s.boundGameSessionCharacterSnapshot(actor)
	if !ok {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := actorIdentity.character
	_, characterName, character, hasCharacter := s.characterForEnter(ctx, actorIdentity.session, characterID)
	if characterID == 0 {
		return false, nil
	}
	members := runtimePartyMembers(state)
	partyActive := state.PartyID > 0
	projections := make([]currentSceneOp9PartyMemberProjection, 0, len(members))
	projectionIdentities := make([]boundGameSessionCharacter, 0, len(members))
	for _, member := range members {
		memberIdentity := actorIdentity
		if member.UserID != characterID {
			var found bool
			memberIdentity, found = s.onlineGameSessionCharacterSnapshot(member.UserID)
			if !found {
				continue
			}
		}
		memberID, memberName, memberCharacter, memberHasCharacter := s.characterForEnter(
			ctx,
			memberIdentity.session,
			memberIdentity.character,
		)
		if memberID != member.UserID {
			continue
		}
		memberLevel := byte(1)
		memberJob := byte(0)
		memberGrow := byte(0)
		if memberHasCharacter {
			if memberCharacter.Level > 0 && memberCharacter.Level < 256 {
				memberLevel = byte(memberCharacter.Level)
			}
			memberJob = byte(numericCharacterStat(memberCharacter.Job))
			memberGrow = byte(numericCharacterStatValue(memberCharacter, "grow_type"))
		}
		projections = append(projections, currentSceneOp9PartyMemberProjection{
			State: member,
			Name:  memberName,
			Job:   memberJob,
			Level: memberLevel,
			Grow:  memberGrow,
		})
		projectionIdentities = append(projectionIdentities, memberIdentity)
	}
	projections = currentSceneOp9PartyMembers(projections)
	selectedMemberSlot := byte(0)
	if partyActive {
		var selectedMemberFound bool
		selectedMemberSlot, selectedMemberFound = currentSceneOp9SelectedPartySlot(projections, characterID)
		if !selectedMemberFound {
			return false, fmt.Errorf(
				"current party projection missing selected member %d in party %d",
				characterID,
				state.PartyID,
			)
		}
	}
	if !s.boundGameSessionCharacterCurrent(receiverIdentity) {
		s.logPacketEvent("game-current-party-frame-op9-deferred",
			"source", source,
			"char_id", characterID,
			"party_id", state.PartyID,
			"reason", "receiving_session_character_changed")
		return false, nil
	}
	if !s.boundGameSessionCharacterCurrent(actorIdentity) {
		s.logPacketEvent("game-current-party-frame-op9-deferred",
			"source", source,
			"char_id", characterID,
			"party_id", state.PartyID,
			"reason", "projected_actor_session_character_changed")
		return false, nil
	}
	for _, identity := range projectionIdentities {
		if s.boundGameSessionCharacterCurrent(identity) {
			continue
		}
		s.logPacketEvent("game-current-party-frame-op9-deferred",
			"source", source,
			"char_id", characterID,
			"party_id", state.PartyID,
			"member_id", identity.character,
			"reason", "projected_member_session_character_changed")
		return false, nil
	}
	objectKey := currentSceneActorObjectKey(characterID)
	if !partyActive {
		body := buildCurrentSceneOp9ActorRemovalBodyInContext(
			objectKey,
			receiver.townActorOwnerChannel,
		)
		s.logPacketEvent("game-current-party-frame-op9-remove-send",
			"conn_id", receiver.connID,
			"source", source,
			"receiver_char_id", receiverIdentity.character,
			"char_id", characterID,
			"object_key", objectKey,
			"owner_channel", receiver.townActorOwnerChannel,
			"party_id", state.PartyID,
			"body_len", len(body),
			"msg_id", uint16(dnfenum.CmdPacketRecoverStamina),
			"classification", 0,
			"body_source", "current_exe_sub_1D64CA0_kind3_party_object_destroy")
		if err := s.sendGameUpperRawClass(receiver, uint16(dnfenum.CmdPacketRecoverStamina), body, 0); err != nil {
			return false, err
		}
		return true, nil
	}
	body := buildCurrentSceneOp9ActorPartyDisplayBodyInContext(
		objectKey,
		character,
		hasCharacter,
		characterName,
		receiver.townActorOwnerChannel,
		currentTownActorOwnerContext(receiver),
		projections,
		selectedMemberSlot,
	)
	// The current party transport establishes the realtime roster and P2P
	// endpoints before op9 binds the scene-side member table. Sending op9 first
	// leaves the client in its "connecting" state and prevents teammate map
	// projection from becoming authoritative.
	if beforeFrame != nil {
		if err := beforeFrame(); err != nil {
			return false, err
		}
	}
	s.logPacketEvent("game-current-party-frame-op9-send",
		"conn_id", receiver.connID,
		"source", source,
		"receiver_char_id", receiverIdentity.character,
		"char_id", characterID,
		"object_key", objectKey,
		"owner_channel", receiver.townActorOwnerChannel,
		"local_channel", currentTownActorOwnerContext(receiver),
		"party_id", state.PartyID,
		"party_active", partyActive,
		"slot_count", len(projections),
		"selected_member_slot", selectedMemberSlot,
		"body_len", len(body),
		"msg_id", uint16(dnfenum.CmdPacketRecoverStamina),
		"classification", 0,
		"body_source", "current_exe_sub_1D64CA0_kind0_party_slot_table_with_cross_channel_identity_branch")
	if err := s.sendGameUpperRawClass(receiver, uint16(dnfenum.CmdPacketRecoverStamina), body, 0); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) broadcastCurrentPartyActorProjection(actor *gameSession, state alignedcmd.PartyState, source string) error {
	if s == nil || s.onlinePlayers == nil || actor == nil || actor.selectedCharacterID == 0 {
		return nil
	}
	for _, peer := range s.onlinePlayers.PeersInSameArea(actor.selectedCharacterID) {
		if peer.Session == nil {
			continue
		}
		if _, err := s.sendCurrentPartyActorFrameProjection(peer.Session, actor, state, source+"_to_area_peer"); err != nil {
			return err
		}
	}
	return nil
}

func runtimePartyMember(session *gameSession, state alignedcmd.PartyState) alignedcmd.PartyMemberState {
	member := alignedcmd.PartyMemberState{
		UserID:    session.selectedCharacterID,
		UserState: 1,
		HPPercent: 100,
		MPPercent: 100,
	}
	for _, existing := range state.Members {
		if existing.UserID == session.selectedCharacterID {
			if existing.UserState != 0 {
				member.UserState = existing.UserState
			}
			if existing.HPPercent != 0 {
				member.HPPercent = existing.HPPercent
			}
			if existing.MPPercent != 0 {
				member.MPPercent = existing.MPPercent
			}
			break
		}
	}
	return member
}

func hasMultiMemberRuntimeParty(session *gameSession) bool {
	state := runtimePartyStateSnapshot(session)
	return state.PartyID > 0 && len(runtimePartyMembers(state)) > 1
}

func runtimePartyStateSnapshot(session *gameSession) alignedcmd.PartyState {
	if session == nil {
		return alignedcmd.PartyState{}
	}
	session.party.mu.Lock()
	defer session.party.mu.Unlock()
	return cloneRuntimePartyState(session.party.state)
}

func storeRuntimePartyState(session *gameSession, state alignedcmd.PartyState) {
	if session == nil {
		return
	}
	session.party.mu.Lock()
	defer session.party.mu.Unlock()
	state.Members = runtimePartyMembers(state)
	session.party.state = cloneRuntimePartyState(state)
}

func cloneRuntimePartyState(state alignedcmd.PartyState) alignedcmd.PartyState {
	state.NameBytes = append([]byte(nil), state.NameBytes...)
	state.Members = append([]alignedcmd.PartyMemberState(nil), state.Members...)
	return state
}

func runtimePartyMemberBySlot(state alignedcmd.PartyState, slot byte) (alignedcmd.PartyMemberState, bool) {
	members := runtimePartyMembers(state)
	if int(slot) >= len(members) {
		return alignedcmd.PartyMemberState{}, false
	}
	return members[int(slot)], true
}

func runtimePartyMembers(state alignedcmd.PartyState) []alignedcmd.PartyMemberState {
	maxMembers := state.MaxMembers
	if maxMembers == 0 || maxMembers > 4 {
		maxMembers = 4
	}
	members := make([]alignedcmd.PartyMemberState, 0, int(maxMembers))
	for _, member := range state.Members {
		members = appendRuntimePartyMember(members, member, maxMembers)
	}
	// The current client has no independent leader field in its party member
	// table: slot zero is the leader. Keep the authoritative leader first for
	// every roster, realtime, endpoint and dungeon projection.
	if state.UserID != 0 {
		for index := 1; index < len(members); index++ {
			if members[index].UserID != state.UserID {
				continue
			}
			leader := members[index]
			copy(members[1:index+1], members[0:index])
			members[0] = leader
			break
		}
	}
	return members
}

func appendRuntimePartyMember(members []alignedcmd.PartyMemberState, member alignedcmd.PartyMemberState, maxMembers byte) []alignedcmd.PartyMemberState {
	if member.UserID == 0 || containsRuntimePartyMember(members, member.UserID) || len(members) >= int(maxMembers) {
		return members
	}
	if member.UserState == 0 {
		member.UserState = 1
	}
	if member.HPPercent == 0 {
		member.HPPercent = 100
	}
	if member.MPPercent == 0 {
		member.MPPercent = 100
	}
	return append(members, member)
}

func containsRuntimePartyMember(members []alignedcmd.PartyMemberState, userID uint16) bool {
	for _, member := range members {
		if member.UserID == userID {
			return true
		}
	}
	return false
}

func selectedCharacterID(session *gameSession) uint16 {
	if session == nil {
		return 0
	}
	return session.selectedCharacterID
}
