package dnfbridge

import (
	"encoding/binary"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const currentTownActorSceneSnapshotMsgID uint16 = 0x0320

// buildCurrentTownActorSceneSnapshotBody matches the current EXE's scene
// snapshot reader (sub_1D6C6F0):
//
//	u8 actor_count, actor_count * { u16 existing_actor_key, u8 child_count }
//
// The reader first resolves every actor key through sub_2699A30, then builds
// its scene-side bindings. A selected character has no server-owned children
// at town entry, so child_count is zero. The key is session data, never a
// captured DOVE key.
func buildCurrentTownActorSceneSnapshotBody(actorKey uint16) []byte {
	body := make([]byte, 4)
	body[0] = 1
	binary.LittleEndian.PutUint16(body[1:3], actorKey)
	return body
}

// sendCurrentInitialTownActorSceneSnapshotLocked completes the mode0-created
// selected actor's scene binding before full player state/location packets.
// The caller holds session.townMu.
func (s *Service) sendCurrentInitialTownActorSceneSnapshotLocked(
	session *gameSession,
	characterID uint16,
) error {
	if session == nil || characterID == 0 ||
		session.initialTownRouteCharacterID != characterID ||
		session.initialTownRouteStage < currentInitialTownRouteActorBound ||
		session.initialTownActorSceneSnapshotSent {
		return nil
	}
	actorKey := currentSceneActorObjectKey(characterID)
	body := buildCurrentTownActorSceneSnapshotBody(actorKey)
	if err := s.sendCurrentSceneFixedClass0Packet(
		session,
		currentTownActorSceneSnapshotMsgID,
		body,
		"initial_town_actor_scene_snapshot_current_exe_sub_1D6C6F0",
	); err != nil {
		return err
	}
	session.initialTownActorSceneSnapshotSent = true
	s.logGameEvent(session, "game-initial-town-actor-scene-snapshot-sent",
		"char_id", characterID,
		"actor_object_key", actorKey,
		"child_count", 0,
		"sequence", "mode0_mode1_actor_created_then_noti0x320_scene_snapshot_then_full_player_state")
	return nil
}

// sendCurrentInitialTownActorRoutePacketsLocked owns completed character-login
// ordering. Unlike a dungeon return, it finishes the full actor state before
// op24 becomes the final transition commit. The caller must hold session.townMu.
func (s *Service) sendCurrentInitialTownActorRoutePacketsLocked(
	session *gameSession,
	characterID uint16,
	townID byte,
	areaID byte,
	row currentSceneTransitionRow,
	objectBody []byte,
	mode1Body []byte,
	transitionBody []byte,
) error {
	return s.sendCurrentInitialTownActorRoutePacketsWithOptionsLocked(
		session,
		characterID,
		townID,
		areaID,
		row,
		objectBody,
		mode1Body,
		transitionBody,
		currentInitialTownRoutePolicy{
			includeReportClientSpec: true,
			includeSecondFullMode1:  true,
			skipPrerequisiteMode1:   false,
		},
	)
}

func (s *Service) sendCurrentChannelReconnectTownActorRoutePacketsLocked(
	session *gameSession,
	characterID uint16,
	townID byte,
	areaID byte,
	row currentSceneTransitionRow,
	objectBody []byte,
	mode1Body []byte,
	transitionBody []byte,
) error {
	return s.sendCurrentInitialTownActorRoutePacketsWithOptionsLocked(
		session,
		characterID,
		townID,
		areaID,
		row,
		objectBody,
		mode1Body,
		transitionBody,
		currentInitialTownRoutePolicy{
			includeReportClientSpec: true,
			includeSecondFullMode1:  true,
			skipPrerequisiteMode1:   false,
		},
	)
}

func (s *Service) sendCurrentInitialTownActorRoutePacketsWithOptionsLocked(
	session *gameSession,
	characterID uint16,
	townID byte,
	areaID byte,
	row currentSceneTransitionRow,
	objectBody []byte,
	mode1Body []byte,
	transitionBody []byte,
	policy currentInitialTownRoutePolicy,
) error {
	prerequisiteMode1Body := mode1Body
	if policy.skipPrerequisiteMode1 {
		prerequisiteMode1Body = nil
	}
	if err := s.sendCurrentTownActorPrerequisitesLocked(session, characterID, objectBody, prerequisiteMode1Body); err != nil {
		return err
	}
	if err := s.sendCurrentInitialTownActorSceneSnapshotLocked(session, characterID); err != nil {
		return fmt.Errorf("initialize selected town actor scene snapshot: %w", err)
	}
	if session.initialTownRouteStage < currentInitialTownRoutePlayerStatePrepared {
		if err := s.sendCurrentTownPlayerStateLocked(
			session,
			"initial_town_before_final_transition",
			policy,
		); err != nil {
			return fmt.Errorf("initialize selected town actor before transition: %w", err)
		}
	}
	if err := s.sendCurrentInitialTownLocationNotificationsLocked(session, characterID, townID, areaID, row); err != nil {
		return fmt.Errorf("initialize selected town location notifications: %w", err)
	}
	if err := s.sendCurrentTownTransitionLocked(session, characterID, townID, areaID, transitionBody); err != nil {
		return err
	}
	// The endpoint handshake is connection-scoped and completes before the
	// selected-town route. Never follow op24 with another class0/op1: it would
	// arm a fresh client watchdog after the one allowed class1/op1 success.
	s.logGameEvent(session, "game-initial-town-route-endpoint-preserved",
		"char_id", characterID,
		"town_id", townID,
		"area_id", areaID,
		"route_stage", session.initialTownRouteStage,
		"resident_notice_sent", session.currentChannelResidentNoticeSent,
		"town_actor_owner_channel", session.townActorOwnerChannel,
		"endpoint_policy", "channelinfo_then_single_requested_class1_op1")
	return nil
}

// waitCurrentInitialTownSceneCommitLocked preserves the former visual-cover
// boundary as a zero-duration test seam. The caller continues directly into
// the route-specific scene finalizers.
func (s *Service) waitCurrentInitialTownSceneCommitLocked(
	session *gameSession,
	characterID uint16,
) {
	if s == nil || session == nil || s.initialTownEntryWait == nil {
		return
	}
	s.logGameEvent(session, "game-initial-town-client-loading-cover-disabled",
		"char_id", characterID,
		"duration", currentInitialTownEntryDelay.String(),
		"owner", "none",
		"server_packet_stream_blocked", false)
	s.initialTownEntryWait(currentInitialTownEntryDelay)
}

// sendCurrentTownActorRoutePacketsLocked preserves the accepted completed
// dungeon-return ordering. The caller must hold session.townMu.
func (s *Service) sendCurrentTownActorRoutePacketsLocked(
	session *gameSession,
	characterID uint16,
	townID byte,
	areaID byte,
	objectBody []byte,
	mode1Body []byte,
	transitionBody []byte,
) error {
	if err := s.sendCurrentTownActorPrerequisitesLocked(session, characterID, objectBody, mode1Body); err != nil {
		return err
	}
	if session.initialTownRouteStage == currentInitialTownRouteActorBound {
		// Completed dungeon return already has a live selected actor from the
		// dungeon state chain. Its accepted town route is action-table, mode0
		// object, equipment-bearing mode1 actor state, then typed op24. Mark the town
		// player-state gate as satisfied before op24 so the later real client
		// scene callback does not replay the character-list full mode3/op19
		// initialization in visible town, which opens the personal-info panel.
		session.initialTownRouteStage = currentInitialTownRoutePlayerStatePrepared
		session.selectedUserInfoRefreshSent = true
		s.logGameEvent(session, "game-dungeon-town-route-player-state-replay-suppressed",
			"char_id", characterID,
			"town_id", townID,
			"area_id", areaID,
			"route_stage", session.initialTownRouteStage,
			"reason", "completed_dungeon_return_uses_equipment_bearing_mode1_and_must_not_replay_initial_town_full_mode3_after_op24")
	}
	// The current client rebuilds NPC task markers from op21/op574 only after
	// the selected actor exists. Completed dungeon return bypasses the full
	// cold-login initialization, so publish the repository/PVF snapshots at
	// the same safe pre-op24 boundary instead of leaving the task manual stale.
	if err := s.sendCurrentInitialTownQuestSnapshotsLocked(
		session,
		"completed_dungeon_return_after_mode1_before_typed_op24",
	); err != nil {
		return err
	}
	return s.sendCurrentTownTransitionLocked(session, characterID, townID, areaID, transitionBody)
}

func (s *Service) sendCurrentTownActorPrerequisitesLocked(
	session *gameSession,
	characterID uint16,
	objectBody []byte,
	mode1Body []byte,
) error {
	if session == nil || characterID == 0 ||
		session.initialTownRouteCharacterID != characterID ||
		session.initialTownRouteStage < currentInitialTownRouteArmed {
		return nil
	}
	if session.initialTownRouteStage < currentInitialTownRouteActionTableSent {
		if err := s.sendGameUpperRawClass(
			session,
			uint16(dnfenum.CmdPacketPVPMissionHpPercent),
			buildCurrentActionTableStateBody(),
			0,
		); err != nil {
			return err
		}
		session.initialTownRouteStage = currentInitialTownRouteActionTableSent
	}
	if session.initialTownRouteStage < currentInitialTownRouteObjectSent {
		objectPacket := csharpSelectInitPacket{
			class: 0,
			msgID: uint16(dnfenum.CmdPacketSetUDPIPPort),
			kind:  csharpCurrentSceneObjectListKind,
		}
		if err := s.sendCSharpSelectInitPacket(session, objectPacket, objectBody); err != nil {
			return err
		}
		session.initialTownRouteStage = currentInitialTownRouteObjectSent
	}
	if session.initialTownRouteStage < currentInitialTownRouteActorBound {
		if len(mode1Body) > 0 {
			if err := s.sendGameUpperRawClass(
				session,
				uint16(dnfenum.CmdPacketSetUDPIPPort),
				mode1Body,
				0,
			); err != nil {
				return err
			}
		} else {
			s.logPacketEvent("game-initial-town-prerequisite-mode1-skipped",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"char_id", characterID,
				"object_key", currentSceneActorObjectKey(characterID),
				"reason", "cold_select_uses_single_complete_mode1_after_mode0")
		}
		session.initialTownRouteStage = currentInitialTownRouteActorBound
	}
	return nil
}

func (s *Service) sendCurrentTownTransitionLocked(
	session *gameSession,
	characterID uint16,
	townID byte,
	areaID byte,
	transitionBody []byte,
) error {
	if session == nil || characterID == 0 ||
		session.initialTownRouteCharacterID != characterID ||
		session.initialTownRouteStage < currentInitialTownRouteActorBound {
		return nil
	}
	if session.initialTownRouteStage < currentInitialTownRouteTransitionSent {
		if err := s.sendGameUpperRawClass(session, currentSceneTransitionMsgID, transitionBody, 0); err != nil {
			return err
		}
		setCurrentTownPositionSceneLocked(session, characterID, townID, areaID)
		session.initialTownRouteStage = currentInitialTownRouteTransitionSent
	}
	if session.initialTownRouteStage == currentInitialTownRouteTransitionSent {
		// The first selected-town op24 is followed by legacy type1345. It does
		// not own the ordinary movement/return post-op24 actor reconstruction:
		// replaying that generation at type1345 reproducibly crashes the current
		// client.
		// The skill projection was already installed at the four preserved
		// 2026-07-27 sources' full-mode1/creature pre-op24 boundary.
		session.initialTownRouteStage = currentInitialTownRoutePlayerStateSent
		session.townSceneReadyCharacterID = characterID
	}
	return nil
}
