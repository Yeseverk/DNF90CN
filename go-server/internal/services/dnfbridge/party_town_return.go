package dnfbridge

import (
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// rebuildRuntimePartyTownCoPresenceAfterReturn restores the scene-owned town
// actors only after every same-area party member has consumed its own op132
// scene finalizer.  The typed op24 return packet intentionally contains just
// the receiving character, so its old peer actors have been detached even
// though OnlinePlayerManager still correctly records the party in that town.
// Replaying a peer before it consumes the finalizer corrupts its selector;
// waiting for all members gives both clients the native mode0/mode1/op9/op23
// reconstruction at the same point.
func (s *Service) rebuildRuntimePartyTownCoPresenceAfterReturn(session *gameSession, source string) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 || s.onlinePlayers == nil {
		return nil
	}
	state := runtimePartyStateSnapshot(session)
	members := runtimePartyMembers(state)
	if state.PartyID <= 0 || len(members) < 2 {
		return nil
	}

	sessions := make([]*gameSession, 0, len(members))
	for _, member := range members {
		memberSession, online := s.onlineGameSession(member.UserID)
		if !online || memberSession == nil || !currentPartyTownReturnSceneReady(memberSession) {
			return nil
		}
		memberState := runtimePartyStateSnapshot(memberSession)
		if memberState.PartyID != state.PartyID || memberState.UserID != state.UserID ||
			len(runtimePartyMembers(memberState)) != len(members) {
			return nil
		}
		sessions = append(sessions, memberSession)
	}

	for _, receiver := range sessions {
		for _, member := range members {
			if member.UserID == receiver.selectedCharacterID ||
				!s.onlinePlayers.PeerInSameArea(receiver.selectedCharacterID, member.UserID) {
				continue
			}
			actor, found := s.onlinePlayers.PlayerForCharacter(member.UserID)
			if !found || actor.Session == nil {
				return fmt.Errorf("party return peer %d has no town actor", member.UserID)
			}
			if err := s.sendTownRemoteActorState(receiver, actor, source+"_party_peer_actor"); err != nil {
				return err
			}
			areaBody := buildCurrentTownUserAreaNotificationBody(
				currentSceneActorObjectKey(actor.CharacterID),
				actor.TownID,
				actor.AreaID,
				actor.PositionX,
				actor.PositionY,
				actor.Direction,
				actor.AreaState,
			)
			if err := s.sendCurrentSceneFixedClass0Packet(
				receiver,
				currentTownUserAreaNotificationMsgID,
				areaBody,
				source+"_party_peer_area",
			); err != nil {
				return err
			}
		}
		if err := s.sendRuntimePartyRosterLocal(receiver, stateForRuntimePartyReceiver(state, receiver.selectedCharacterID), source+"_party_roster"); err != nil {
			return err
		}
	}
	s.logGameEvent(session, "game-party-town-return-copresence-rebuilt",
		"source", source,
		"party_id", state.PartyID,
		"member_count", len(members),
		"reason", "all_same_party_members_consumed_op132_scene_finalizer_then_mode0_mode1_op9_op23_and_realtime_roster")
	return nil
}

func currentPartyTownReturnSceneReady(session *gameSession) bool {
	if session == nil || session.selectedCharacterID == 0 || session.backToVillageEnterSelectPending {
		return false
	}
	session.dungeon.mu.Lock()
	pendingReturn := session.confirmedDungeonReturnStatePending
	hasDungeonRuntime := session.dungeon.runtime != nil
	session.dungeon.mu.Unlock()
	if pendingReturn || hasDungeonRuntime {
		return false
	}
	session.townMu.Lock()
	ready := session.townSceneReadyCharacterID == session.selectedCharacterID &&
		session.townPositionSnapshot.CharacterID == session.selectedCharacterID &&
		session.townPositionSnapshot.PositionValid
	session.townMu.Unlock()
	return ready
}

func stateForRuntimePartyReceiver(state alignedcmd.PartyState, receiverID uint16) alignedcmd.PartyState {
	state = cloneRuntimePartyState(state)
	state.IsLeader = state.UserID == receiverID
	return state
}
