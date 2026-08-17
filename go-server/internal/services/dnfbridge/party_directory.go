package dnfbridge

import (
	"encoding/binary"
	"sort"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/party"
)

const (
	currentPartyDirectoryMsgID = uint16(dnfenum.CmdPacketCompleteDisplay)
	currentPartyDirectoryJoin  = uint16(dnfenum.CmdPacketEnterWarroom)
	currentPartyDirectoryQuery = uint16(dnfenum.CmdPacketQuickJoinRoom)

	runtimePartyDirectoryRequestModeFull   = byte(0)
	runtimePartyDirectoryRequestModeRegion = byte(1)
)

// runtimePartyDirectoryRecords takes an immutable, deterministic projection of
// every active server-side party. Resident channel IDs do not belong in op87:
// the current EXE interprets that byte as a bounded directory discriminator.
// party.BuildDirectorySnapshot owns the ordinary-directory discriminator.
func (s *Service) runtimePartyDirectoryRecords() []party.DirectoryRecord {
	// Importing a pre-rewrite client cache is a one-time compatibility path.
	// The returned directory is nevertheless sourced only from the central
	// manager, never by selecting whichever session cache happens to be read
	// last.
	for _, session := range s.runtimePartyDirectorySessions() {
		s.bootstrapManagedRuntimePartyForSession(session)
	}
	manager := s.runtimePartyManagerForService()
	if manager == nil {
		return nil
	}
	snapshots := manager.Snapshots()
	records := make([]party.DirectoryRecord, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.ID == 0 || len(snapshot.Members) == 0 {
			continue
		}
		memberIDs := make([]uint16, 0, len(snapshot.Members))
		for _, member := range snapshot.Members {
			memberIDs = append(memberIDs, member.UserID)
		}
		records = append(records, party.DirectoryRecord{
			PartyID:     snapshot.ID,
			SelectionID: snapshot.Settings.SelectionID,
			MemberIDs:   memberIDs,
		})
	}
	return records
}

func (s *Service) runtimePartyDirectorySessions() []*gameSession {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	sessions := make([]*gameSession, 0, len(s.gameSessions))
	for _, session := range s.gameSessions {
		if session != nil && session.selectedCharacterID != 0 {
			sessions = append(sessions, session)
		}
	}
	s.mu.Unlock()
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].selectedCharacterID < sessions[j].selectedCharacterID
	})
	return sessions
}

func (s *Service) sendRuntimePartyDirectorySnapshot(session *gameSession, requestMode byte, source string) error {
	if session == nil || session.selectedCharacterID == 0 || session.conn == nil {
		return nil
	}
	records := s.runtimePartyDirectoryRecords()
	body := party.BuildDirectorySnapshot(records)
	s.logPacketEvent("game-party-directory-op87-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"request_mode", requestMode,
		"char_id", session.selectedCharacterID,
		"msg_id", currentPartyDirectoryMsgID,
		"classification", 0,
		"party_count", len(records),
		"directory_type", party.DirectoryTypeOrdinary,
		"body_len", len(body),
		"body_source", "current_exe_sub_1D3F040_directory_type_minus_protected_base_2_and_fixed_28_byte_records")
	return s.sendGameUpperRawClass(session, currentPartyDirectoryMsgID, body, 0)
}

func runtimePartyDirectorySessionReady(session *gameSession) bool {
	if session == nil || session.selectedCharacterID == 0 || session.conn == nil {
		return false
	}
	session.townMu.Lock()
	ready := session.townSceneReadyCharacterID == session.selectedCharacterID ||
		session.sceneBootstrapTailSent ||
		session.postStartMapPlayerStateSent
	session.townMu.Unlock()
	return ready && !currentDungeonSceneActive(session)
}

func (s *Service) handleRuntimePartyDirectoryRefresh(session *gameSession, body []byte) (bool, error) {
	if len(body) != 1 || !runtimePartyDirectorySessionReady(session) {
		return false, nil
	}
	requestMode := body[0]
	source := ""
	switch requestMode {
	case runtimePartyDirectoryRequestModeFull:
		source = "current_exe_op98_full_directory"
	case runtimePartyDirectoryRequestModeRegion:
		source = "current_exe_op98_region_summary"
	default:
		return false, nil
	}
	s.logGameEvent(session, "game-party-directory-refresh-request",
		"type", currentPartyDirectoryQuery,
		"body_len", len(body),
		"request_mode", requestMode,
		"source", source)
	return true, s.sendRuntimePartyDirectorySnapshot(session, requestMode, source)
}

func (s *Service) handleRuntimePartyDirectoryJoin(session *gameSession, body []byte) (bool, error) {
	if len(body) != 2 || session == nil || session.selectedCharacterID == 0 ||
		currentDungeonSceneActive(session) {
		return false, nil
	}
	partyID := binary.LittleEndian.Uint16(body)
	targetSession, targetState, found := s.runtimePartyDirectoryTarget(partyID)
	if !found {
		return false, nil
	}
	if targetSession == session || containsRuntimePartyMember(runtimePartyMembers(targetState), session.selectedCharacterID) {
		if err := s.sendGameUpperRawClass(session, currentPartyDirectoryJoin, nil, 0); err != nil {
			return true, err
		}
		return true, s.sendRuntimePartySnapshot(session, targetState)
	}
	state, result, ok := s.createOrJoinManagedRuntimeParty(session, targetSession)
	if !ok {
		s.logGameEvent(session, "game-party-directory-join-rejected",
			"type", currentPartyDirectoryJoin,
			"party_id", partyID,
			"source_char_id", session.selectedCharacterID,
			"reason", result.Reason)
		return true, s.sendGameUpperRawClass(session, currentPartyDirectoryJoin, nil, 0)
	}
	if err := s.sendGameUpperRawClass(session, currentPartyDirectoryJoin, nil, 0); err != nil {
		return true, err
	}
	if err := s.sendManagedRuntimePartySnapshots(state); err != nil {
		return true, err
	}
	s.logGameEvent(session, "game-party-directory-join-complete",
		"type", currentPartyDirectoryJoin,
		"party_id", partyID,
		"source_char_id", session.selectedCharacterID,
		"leader_char_id", state.UserID,
		"member_count", len(runtimePartyMembers(state)))
	return true, nil
}

func (s *Service) runtimePartyDirectoryTarget(partyID uint16) (*gameSession, alignedcmd.PartyState, bool) {
	if partyID == 0 || partyID == ^uint16(0) {
		return nil, alignedcmd.PartyState{}, false
	}
	// Make direct op90 joins work even if the requester did not first open the
	// op87 directory panel.
	for _, session := range s.runtimePartyDirectorySessions() {
		s.bootstrapManagedRuntimePartyForSession(session)
	}
	manager := s.runtimePartyManagerForService()
	snapshot, found := manager.SnapshotByID(partyID)
	if !found {
		return nil, alignedcmd.PartyState{}, false
	}
	leader, online := s.onlineGameSession(snapshot.Leader)
	if !online {
		return nil, alignedcmd.PartyState{}, false
	}
	return leader, snapshot.StateFor(snapshot.Leader), true
}

func (s *Service) detachRuntimePartySession(session *gameSession, source string) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return
	}
	identity, bound := s.boundGameSessionCharacterSnapshot(session)
	if !bound {
		return
	}
	state := runtimePartyStateSnapshot(session)
	if !s.hasManagedRuntimeParty(session) && state.PartyID <= 0 {
		return
	}
	nextState, result, removed := s.leaveManagedRuntimeParty(identity)
	if !removed {
		storeRuntimePartyState(session, alignedcmd.PartyState{})
		return
	}
	if result.Retired != nil {
		s.closeRuntimePartyUDPRelay(int(result.Retired.ID))
		for _, member := range result.Retired.Members {
			if member.UserID == identity.character {
				continue
			}
			if target, online := s.onlineGameSession(member.UserID); online {
				if err := s.sendRuntimePartySnapshot(target, alignedcmd.PartyState{}); err != nil {
					s.logWarn("dnfbridge deferred retired party clear during session detach", "source", source, "char_id", identity.character, "error", err)
				}
			}
		}
		s.syncRuntimePartyUDPRelay(nextState)
		if err := s.sendManagedRuntimePartySnapshots(nextState); err != nil {
			s.logWarn("dnfbridge deferred rebuilt party snapshot during session detach", "source", source, "char_id", identity.character, "error", err)
		}
		return
	}
	if nextState.PartyID > 0 {
		s.syncRuntimePartyUDPRelay(nextState)
	} else if state.PartyID > 0 {
		s.closeRuntimePartyUDPRelay(state.PartyID)
	}
	if err := s.broadcastRuntimePartyMemberRemoved(state, nextState, identity.character, session); err != nil {
		s.logWarn("dnfbridge deferred party member removal during session detach",
			"source", source,
			"char_id", identity.character,
			"party_id", nextState.PartyID,
			"error", err)
	}
}
