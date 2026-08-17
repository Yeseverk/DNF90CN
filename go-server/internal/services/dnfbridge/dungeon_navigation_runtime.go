package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfdungeon "longheng.io/server/internal/modules/dnf/dungeon"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const currentSelectDungeonEntryLevelTooLowReason byte = 14

func (s *Service) handleDungeonSelectUpper(session *gameSession, body []byte) error {
	return s.handleDungeonSelectUpperPlanned(session, body, nil, true)
}

type runtimePartyDungeonEntryPlan struct {
	dungeonID int64
	mazeIndex int
	maps      map[worldmap.RoomCoordinate]int64
	room      worldmap.RoomCoordinate
	mapID     int64
	boss      worldmap.RoomCoordinate
	seed      uint32
}

type runtimePartyPreparedDungeonEntry struct {
	session   *gameSession
	runtime   *runtimeDungeonState
	scene     worldmap.DungeonRoomScene
	plan      runtimePartyDungeonEntryPlan
	committed bool
}

func (s *Service) handleDungeonSelectUpperPlanned(
	session *gameSession,
	body []byte,
	prepared *runtimePartyPreparedDungeonEntry,
	fanoutParty bool,
) error {
	if len(body) != dungeoncmd.SelectDungeonRequestSize {
		s.logGameEvent(session, "game-dungeon-select-blocked",
			"body_len", len(body),
			"expected_body_len", dungeoncmd.SelectDungeonRequestSize,
			"reason", "current_exe_op16_request_boundary_mismatch")
		return nil
	}
	request, err := dungeoncmd.DecodeSelectDungeonRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-select-blocked",
			"body_len", len(body),
			"reason", "current_exe_op16_request_malformed",
			"error", err)
		return nil
	}
	partyStateAtRequest := runtimePartyStateSnapshot(session)
	partyMemberIndex, partyMemberIndexed := runtimePartyMemberIndex(session.selectedCharacterID, partyStateAtRequest)
	s.logGameEvent(session, "game-dungeon-select-request-decoded",
		"body_len", len(body),
		"dungeon_id", request.DungeonID,
		"difficulty", request.Difficulty,
		"entry_option", request.EntryOption,
		"selection_mode", request.SelectionMode,
		"runtime_state", request.RuntimeState,
		"runtime_token", request.RuntimeToken,
		"reserved", request.Reserved,
		"party_state", request.PartyState,
		"offset16_unproven", request.LeaderObjectKey,
		"special_mode", request.SpecialMode,
		"source", "current_exe_sub_342EA40_fixed_21_byte_writer")
	if err := s.commitPendingDungeonReturnForSceneRequest(
		session,
		"current_exe_op16_after_pending_town_transition",
	); err != nil {
		s.logGameEvent(session, "game-dungeon-select-blocked",
			"dungeon_id", request.DungeonID,
			"difficulty", request.Difficulty,
			"reason", "pending_town_transition_commit_failed",
			"error", err)
		return nil
	}
	if prepared == nil {
		if handled, err := s.replayDungeonSelectAckForActiveRuntime(session, request, len(body)); handled || err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	var runtime *runtimeDungeonState
	var scene worldmap.DungeonRoomScene
	if prepared != nil {
		if prepared.session != session || prepared.runtime == nil || prepared.runtime.Session == nil ||
			!sameSelectDungeonRequest(prepared.runtime.Request, request) {
			return fmt.Errorf("prepared party dungeon entry does not belong to the target session")
		}
		runtime, scene = prepared.runtime, prepared.scene
	} else {
		var err error
		runtime, scene, err = s.prepareDungeonRuntime(ctx, session, request)
		if err != nil {
			s.logGameEvent(session, "game-dungeon-select-blocked",
				"dungeon_id", request.DungeonID,
				"difficulty", request.Difficulty,
				"reason", "dungeon_owner_validation_failed",
				"error", err)
			if errors.Is(err, dnfdungeon.ErrEntryLevelTooLow) {
				return s.sendSelectDungeonFailure(session, currentSelectDungeonEntryLevelTooLowReason)
			}
			return nil
		}
	}
	runtime.partyMemberIndex, runtime.partyMemberIndexed = partyMemberIndex, partyMemberIndexed
	var preparedFollowers []*runtimePartyPreparedDungeonEntry
	if prepared == nil && fanoutParty {
		var err error
		preparedFollowers, err = s.prepareRuntimePartyDungeonEntry(ctx, session, partyStateAtRequest, request, runtime, scene)
		if err != nil {
			s.logGameEvent(session, "game-party-dungeon-entry-blocked",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"difficulty", request.Difficulty,
				"reason", "party_entry_preflight_failed_before_any_member_commit",
				"error", err)
			return nil
		}
	}
	if err := freezeCurrentDungeonTownReturnOrigin(session, runtime); err != nil {
		s.logGameEvent(session, "game-dungeon-select-blocked",
			"dungeon_id", request.DungeonID,
			"maze_index", runtime.MazeIndex,
			"reason", "ordinary_dungeon_town_return_origin_unavailable",
			"error", err)
		return nil
	}
	if _, err := buildCurrentDungeonEntryPackets(runtime, scene); err != nil {
		s.logGameEvent(session, "game-dungeon-select-blocked",
			"dungeon_id", request.DungeonID,
			"maze_index", runtime.MazeIndex,
			"reason", "dungeon_entry_packet_preflight_failed",
			"error", err)
		return nil
	}
	if prepared == nil && len(preparedFollowers) > 0 {
		if err := s.commitRuntimePartyPreparedFollowers(preparedFollowers); err != nil {
			s.logGameEvent(session, "game-party-dungeon-entry-blocked",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"difficulty", request.Difficulty,
				"reason", "party_follower_runtime_commit_failed_before_leader_commit",
				"error", err)
			return nil
		}
	}
	if prepared == nil || !prepared.committed {
		if err := s.commitDungeonRuntime(session, runtime); err != nil {
			s.rollbackRuntimePartyPreparedFollowers(preparedFollowers)
			s.logGameEvent(session, "game-dungeon-select-blocked",
				"dungeon_id", request.DungeonID,
				"maze_index", runtime.MazeIndex,
				"reason", "dungeon_runtime_commit_failed",
				"error", err)
			return nil
		}
	}
	resetDungeonEntrySceneGates(session)
	if err := s.sendCurrentSceneOp9PreviewActorRemovalOnce(session, "select_dungeon_after_runtime_commit_before_op27"); err != nil {
		return err
	}

	if prepared == nil {
		selectAckBody := buildSelectDungeonBodyForCharacter(session.selectedCharacterID)
		s.logGameEvent(session, "game-dungeon-select-ack-send",
			"msg_id", uint16(dnfenum.CmdPacketSelectDungeon),
			"body_len", len(selectAckBody)+1,
			"compatibility_mode", selectAckBody[0],
			"compatibility_object_key", binary.LittleEndian.Uint16(selectAckBody[1:3]),
			"request_body_len", len(body),
			"source", "current_exe_sub_1CFC410_generic_success_byte_trailing_body_ignored",
			"reason", "stop_rejected_op16_request_echo")
		if err := s.sendGameUpperSuccess(
			session,
			uint16(dnfenum.CmdPacketSelectDungeon),
			selectAckBody,
		); err != nil {
			return err
		}
	} else {
		s.logGameEvent(session, "game-party-dungeon-select-follower-ack-suppressed",
			"msg_id", uint16(dnfenum.CmdPacketSelectDungeon),
			"request_body_len", len(body),
			"reason", "follower_did_not_submit_op16_and_receives_leader_owned_op28_op29_only")
	}
	if err := s.sendCurrentDungeonResourceState(session, runtime, "select_dungeon_after_op16_ack_before_op27"); err != nil {
		return err
	}
	if err := s.sendCurrentPreDungeonContextPlayerState(session, "select_dungeon_after_op5_before_op27"); err != nil {
		return err
	}
	if !session.enterSelectDungeonContextSent {
		if err := s.sendCurrentDungeonContextOp27(session, "select_dungeon_after_pre_actor_binding"); err != nil {
			return err
		}
		session.enterSelectDungeonContextSent = true
	} else {
		s.logGameEvent(session, "game-upper-current-dungeon-context-op27-duplicate-skipped",
			"source", "select_dungeon_after_pre_actor_binding",
			"char_id", session.selectedCharacterID,
			"msg_id", currentDungeonContextMsgID,
			"reason", "town_enter_select_page_already_committed_before_real_op16")
	}
	entryPackets, err := buildCurrentDungeonEntryPackets(runtime, scene)
	if err != nil {
		s.logGameEvent(session, "game-upper-current-dungeon-info-op28-deferred",
			"source", "select_dungeon_after_op27",
			"char_id", session.selectedCharacterID,
			"msg_id", currentDungeonInfoNotification,
			"dungeon_id", runtime.Dungeon.ID,
			"reason", "typed_runtime_packet_plan_failed",
			"error", err)
		return nil
	}
	s.logGameEvent(session, "game-upper-current-dungeon-info-op28-send",
		"source", "select_dungeon_after_op27",
		"char_id", session.selectedCharacterID,
		"msg_id", currentDungeonInfoNotification,
		"classification", 0,
		"body_len", len(entryPackets.DungeonInfo),
		"dungeon_id", runtime.Dungeon.ID,
		"difficulty", runtime.Request.Difficulty,
		"entry_option", runtime.Request.EntryOption,
		"maze_index", runtime.MazeIndex,
		"boss", runtime.BossCoordinate.String(),
		"body_source", "current_exe_sub_1D440C0_real_pvf_runtime",
		"start_map", "deferred_until_op28_runtime_acceptance")
	if err := s.sendGameUpperRawClass(session, currentDungeonInfoNotification, entryPackets.DungeonInfo, 0); err != nil {
		return err
	}
	roomSnapshot := runtime.Room.Snapshot()
	s.logGameEvent(session, "game-upper-current-dungeon-start-op29-send",
		"source", "select_dungeon_after_op28",
		"char_id", session.selectedCharacterID,
		"msg_id", currentDungeonStartNotification,
		"classification", 0,
		"body_len", len(entryPackets.StartMap),
		"room", scene.Coordinate.String(),
		"map_id", scene.Map.Map.ID,
		"seed", runtime.Seed,
		"actor_count", len(roomSnapshot.Monsters)+len(roomSnapshot.ExtendedActors),
		"body_source", "current_exe_sub_1D479A0_real_pvf_runtime")
	if err := s.sendGameUpperRawClass(session, currentDungeonStartNotification, entryPackets.StartMap, 0); err != nil {
		return err
	}
	announcedActors, err := runtime.Room.AnnounceAllActors(runtime.Session)
	if err != nil {
		return fmt.Errorf("commit current start-map runtime actors after op29 send: %w", err)
	}
	if err := runtime.cacheCurrentDungeonRoom(); err != nil {
		return fmt.Errorf("cache current start-map runtime room after actor announce: %w", err)
	}
	s.logGameEvent(session, "game-upper-current-dungeon-start-op29-committed",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"room", scene.Coordinate.String(),
		"map_id", scene.Map.Map.ID,
		"announced_actor_count", announcedActors)
	if err := s.sendCurrentPostStartMapPlayerPlacement(session, runtime, scene, "select_dungeon_after_op29_commit"); err != nil {
		return fmt.Errorf("send current post-start-map player state: %w", err)
	}

	s.logGameEvent(session, "game-dungeon-runtime-prepared",
		"dungeon_id", request.DungeonID,
		"difficulty", request.Difficulty,
		"maze_index", runtime.MazeIndex,
		"room", scene.Coordinate.String(),
		"map_id", scene.Map.Map.ID,
		"map_path", scene.Map.Map.Path,
		"monsters", len(scene.Monsters),
		"hostiles", len(scene.ExpectedHostiles),
		"blocking_hostiles", len(scene.BlockingHostiles),
		"npcs", len(scene.NPCs),
		"passive_objects", len(scene.PassiveObjects)+len(scene.SpecialPassiveObjects),
		"summons", len(scene.Summons),
		"runtime_monsters", runtime.Room.MonsterCount(),
		"opaque_hostiles", runtime.Room.OpaqueHostileCount(),
		"tutorial_completed_reentry", runtime.tutorialCompletedReentry,
		"entry_sequence", "requester_class1_op16_ack_or_party_follower_no_ack_then_class0_op5_then_final_owner_mode0_actor_binding_mode1_then_op27_page_enter_runtime_recompute_gate_then_op28_op29_then_full_equipment_mode1_mode3_optional_pvf_tutorial_op3_user_state_op359_op356_op124_op9_op120",
		"deferred", "real_hud_and_remaining_scene_objects")
	if fanoutParty {
		if err := s.enterRuntimePartyFollowers(session, body, preparedFollowers, partyStateAtRequest); err != nil {
			return err
		}
	}
	s.leaveTownPresenceForDungeon(session)
	return nil
}

func (s *Service) prepareRuntimePartyDungeonEntry(
	ctx context.Context,
	leader *gameSession,
	state alignedcmd.PartyState,
	request dungeoncmd.SelectDungeonRequest,
	leaderRuntime *runtimeDungeonState,
	leaderScene worldmap.DungeonRoomScene,
) ([]*runtimePartyPreparedDungeonEntry, error) {
	if leader == nil || state.PartyID <= 0 || state.UserID != leader.selectedCharacterID {
		return nil, nil
	}
	members := runtimePartyMembers(state)
	if len(members) <= 1 {
		return nil, nil
	}
	plan := runtimePartyDungeonEntryPlan{
		dungeonID: leaderRuntime.Dungeon.ID,
		mazeIndex: leaderRuntime.MazeIndex,
		room:      leaderScene.Coordinate,
		mapID:     leaderScene.Map.Map.ID,
		boss:      leaderRuntime.BossCoordinate,
		seed:      leaderRuntime.Seed,
		maps:      make(map[worldmap.RoomCoordinate]int64),
	}
	for _, room := range leaderRuntime.Session.Rooms() {
		if room.Map != nil {
			plan.maps[room.Coordinate] = room.Map.Map.ID
		}
	}
	prepared := make([]*runtimePartyPreparedDungeonEntry, 0, len(members)-1)
	for _, member := range members {
		if member.UserID == 0 || member.UserID == leader.selectedCharacterID {
			continue
		}
		if s.onlinePlayers == nil || !s.onlinePlayers.PeerInSameArea(leader.selectedCharacterID, member.UserID) {
			return nil, fmt.Errorf("party member %d is not present in the leader town area", member.UserID)
		}
		follower, ok := s.onlineGameSession(member.UserID)
		if !ok || follower == leader {
			return nil, fmt.Errorf("party member %d has no resident game session", member.UserID)
		}
		followerState := runtimePartyStateSnapshot(follower)
		if followerState.PartyID != state.PartyID || followerState.UserID != state.UserID {
			return nil, fmt.Errorf("party member %d has stale party state", member.UserID)
		}
		if ready, reason := s.currentTownEnterSelectReady(follower); !ready {
			return nil, fmt.Errorf("party member %d selector is not ready: %s", member.UserID, reason)
		}
		followerRuntime, followerScene, err := s.prepareDungeonRuntimePlanned(ctx, follower, request, &plan)
		if err != nil {
			return nil, fmt.Errorf("prepare party member %d for dungeon %d: %w", member.UserID, request.DungeonID, err)
		}
		memberIndex, memberIndexed := runtimePartyMemberIndex(member.UserID, state)
		followerRuntime.partyMemberIndex, followerRuntime.partyMemberIndexed = memberIndex, memberIndexed
		if err := applyRuntimePartyDungeonEntryPlan(followerRuntime, followerScene, plan); err != nil {
			return nil, fmt.Errorf("apply leader dungeon selection to party member %d: %w", member.UserID, err)
		}
		if err := freezeCurrentDungeonTownReturnOrigin(follower, followerRuntime); err != nil {
			return nil, fmt.Errorf("freeze party member %d town return origin: %w", member.UserID, err)
		}
		if _, err := buildCurrentDungeonEntryPackets(followerRuntime, followerScene); err != nil {
			return nil, fmt.Errorf("build party member %d dungeon entry packets: %w", member.UserID, err)
		}
		prepared = append(prepared, &runtimePartyPreparedDungeonEntry{
			session: follower,
			runtime: followerRuntime,
			scene:   followerScene,
			plan:    plan,
		})
		s.logGameEvent(follower, "game-party-dungeon-entry-member-prepared",
			"party_id", state.PartyID,
			"leader_char_id", leader.selectedCharacterID,
			"char_id", member.UserID,
			"dungeon_id", followerRuntime.Dungeon.ID,
			"maze_index", followerRuntime.MazeIndex,
			"room", followerScene.Coordinate.String(),
			"map_id", followerScene.Map.Map.ID,
			"seed", followerRuntime.Seed)
	}
	for _, member := range prepared {
		ready := false
		err := s.callGameSession(ctx, member.session, "runtime-party-dungeon-selector-preflight", func() error {
			var prepareErr error
			ready, prepareErr = s.prepareRuntimePartyFollowerEnterSelect(
				member.session,
				leader.selectedCharacterID,
				state.PartyID,
				"leader_op16_party_follower_preflight",
			)
			return prepareErr
		})
		if err != nil {
			return nil, fmt.Errorf("prepare party member %d selector: %w", member.session.selectedCharacterID, err)
		}
		if !ready {
			return nil, fmt.Errorf("party member %d selector is not ready", member.session.selectedCharacterID)
		}
	}
	return prepared, nil
}

func applyRuntimePartyDungeonEntryPlan(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	plan runtimePartyDungeonEntryPlan,
) error {
	if runtime == nil || runtime.Session == nil {
		return errors.New("party member runtime is unavailable")
	}
	if runtime.Dungeon.ID != plan.dungeonID || runtime.MazeIndex != plan.mazeIndex ||
		scene.Coordinate.X != plan.room.X || scene.Coordinate.Y != plan.room.Y ||
		scene.Map.Map.ID != plan.mapID ||
		runtime.BossCoordinate.X != plan.boss.X || runtime.BossCoordinate.Y != plan.boss.Y {
		return fmt.Errorf(
			"topology mismatch got dungeon=%d maze=%d room=%s map=%d boss=%s want dungeon=%d maze=%d room=%s map=%d boss=%s",
			runtime.Dungeon.ID, runtime.MazeIndex, scene.Coordinate.String(), scene.Map.Map.ID, runtime.BossCoordinate.String(),
			plan.dungeonID, plan.mazeIndex, plan.room.String(), plan.mapID, plan.boss.String(),
		)
	}
	resolved := runtime.Session.Rooms()
	if len(resolved) != len(plan.maps) {
		return fmt.Errorf("resolved room count mismatch got=%d want=%d", len(resolved), len(plan.maps))
	}
	for _, room := range resolved {
		wantMapID, ok := plan.maps[room.Coordinate]
		if !ok || room.Map == nil || room.Map.Map.ID != wantMapID {
			gotMapID := int64(0)
			if room.Map != nil {
				gotMapID = room.Map.Map.ID
			}
			return fmt.Errorf("room %s map mismatch got=%d want=%d known=%t", room.Coordinate, gotMapID, wantMapID, ok)
		}
	}
	runtime.Seed = plan.seed
	if visit := runtime.Rooms[runtimeDungeonRoomKeyFromScene(scene)]; visit != nil {
		visit.Seed = plan.seed
		visit.DropRNG = plan.seed
	}
	return nil
}

func (s *Service) commitRuntimePartyPreparedFollowers(prepared []*runtimePartyPreparedDungeonEntry) error {
	committed := make([]*runtimePartyPreparedDungeonEntry, 0, len(prepared))
	for _, member := range prepared {
		if member == nil || member.session == nil || member.runtime == nil {
			s.rollbackRuntimePartyPreparedFollowers(committed)
			return errors.New("prepared party member runtime is incomplete")
		}
		if err := s.commitDungeonRuntime(member.session, member.runtime); err != nil {
			s.rollbackRuntimePartyPreparedFollowers(committed)
			return fmt.Errorf("commit party member %d runtime: %w", member.session.selectedCharacterID, err)
		}
		member.committed = true
		committed = append(committed, member)
	}
	return nil
}

func (s *Service) rollbackRuntimePartyPreparedFollowers(prepared []*runtimePartyPreparedDungeonEntry) {
	for _, member := range prepared {
		if member == nil || member.session == nil || member.runtime == nil || !member.committed {
			continue
		}
		member.session.dungeon.mu.Lock()
		if member.session.dungeon.runtime == member.runtime {
			member.session.dungeon.runtime = nil
		}
		member.session.dungeon.mu.Unlock()
		member.committed = false
	}
}

func runtimePartyMemberIndexForSession(session *gameSession) (byte, bool) {
	if session == nil || session.selectedCharacterID == 0 {
		return 0, false
	}
	return runtimePartyMemberIndex(session.selectedCharacterID, runtimePartyStateSnapshot(session))
}

func runtimePartyMemberIndex(characterID uint16, state alignedcmd.PartyState) (byte, bool) {
	members := runtimePartyMembers(state)
	if len(members) <= 1 {
		return 0, false
	}
	for index, member := range members {
		if member.UserID == characterID && index <= int(^uint8(0)) {
			return byte(index), true
		}
	}
	return 0, false
}

func currentDungeonRuntimePartyMemberIndex(runtime *runtimeDungeonState) byte {
	if runtime == nil || !runtime.partyMemberIndexed {
		return 0xff
	}
	return runtime.partyMemberIndex
}

func (s *Service) enterRuntimePartyFollowers(
	leader *gameSession,
	body []byte,
	prepared []*runtimePartyPreparedDungeonEntry,
	state alignedcmd.PartyState,
) error {
	if leader == nil || leader.selectedCharacterID == 0 {
		return nil
	}
	if state.PartyID <= 0 || state.UserID != leader.selectedCharacterID {
		return nil
	}
	if len(prepared) == 0 {
		return nil
	}
	for _, member := range prepared {
		if member == nil || member.session == nil || member.runtime == nil || !member.committed {
			return errors.New("party follower entry was not prepared and committed")
		}
		follower := member.session
		followerParty := runtimePartyStateSnapshot(follower)
		if followerParty.PartyID != state.PartyID || followerParty.UserID != state.UserID {
			return fmt.Errorf("party member %d changed party state during dungeon entry", follower.selectedCharacterID)
		}
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		err := s.callGameSession(ctx, follower, "runtime-party-dungeon-entry", func() error {
			return s.handleDungeonSelectUpperPlanned(follower, append([]byte(nil), body...), member, false)
		})
		cancel()
		if err != nil {
			return fmt.Errorf("enter runtime party member %d into dungeon: %w", follower.selectedCharacterID, err)
		}
	}
	return nil
}

func (s *Service) leaveTownPresenceForDungeon(session *gameSession) {
	if s == nil || s.onlinePlayers == nil || session == nil || session.selectedCharacterID == 0 {
		return
	}
	_, others, removed := s.onlinePlayers.LeaveAreaSession(session.selectedCharacterID, session)
	if removed && len(others) > 0 {
		s.broadcastTownPlayerLeave(session.selectedCharacterID, others)
	}
}

func (s *Service) replayDungeonSelectAckForActiveRuntime(
	session *gameSession,
	request dungeoncmd.SelectDungeonRequest,
	requestBodyLen int,
) (bool, error) {
	if session == nil {
		return false, nil
	}
	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil {
		session.dungeon.mu.Unlock()
		return false, nil
	}
	activeRequest := runtime.Request
	snapshot := runtime.Session.Snapshot().Run
	session.dungeon.mu.Unlock()

	if snapshot.Status != worldmap.DungeonRunActive ||
		!dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) ||
		!sameSelectDungeonRequest(activeRequest, request) {
		return false, nil
	}

	selectAckBody := buildSelectDungeonBodyForCharacter(session.selectedCharacterID)
	s.logGameEvent(session, "game-dungeon-select-duplicate-ack-send",
		"msg_id", uint16(dnfenum.CmdPacketSelectDungeon),
		"body_len", len(selectAckBody)+1,
		"mode", selectAckBody[0],
		"reader_key", binary.LittleEndian.Uint16(selectAckBody[1:3]),
		"request_body_len", requestBodyLen,
		"dungeon_id", snapshot.DungeonID,
		"maze_index", snapshot.MazeIndex,
		"room", snapshot.Current.String(),
		"replayed_packets", 0,
		"reason", "idempotent_retry_for_same_active_current_dungeon")
	if err := s.sendGameUpperSuccess(
		session,
		uint16(dnfenum.CmdPacketSelectDungeon),
		selectAckBody,
	); err != nil {
		return true, err
	}
	return true, nil
}

func (s *Service) sendSelectDungeonFailure(session *gameSession, reason byte) error {
	// Current EXE op16 reads failure, reason, and a separate supplemental byte.
	return s.sendGameUpperRaw(session, uint16(dnfenum.CmdPacketSelectDungeon), []byte{0, reason, 0})
}

func sameSelectDungeonRequest(left, right dungeoncmd.SelectDungeonRequest) bool {
	return dnfdungeon.SameSelectRequest(left, right)
}

func (s *Service) handleDungeonMoveMap(session *gameSession, body []byte) error {
	return s.handleDungeonMoveMapPlanned(session, body, nil, true)
}

type runtimePartyDungeonMovePlan struct {
	target runtimeDungeonRoomKey
	seed   uint32
}

func (s *Service) handleDungeonMoveMapPlanned(
	session *gameSession,
	body []byte,
	partyPlan *runtimePartyDungeonMovePlan,
	fanoutParty bool,
) error {
	if len(body) != dungeoncmd.MoveMapRequestSize {
		s.logGameEvent(session, "game-dungeon-move-blocked",
			"body_len", len(body),
			"expected_body_len", dungeoncmd.MoveMapRequestSize,
			"expected_layout", "current_exe_op45_plaintext_after_client_cipher_bypass",
			"reason", "current_exe_op45_plaintext_request_boundary_mismatch")
		return nil
	}
	request, err := dungeoncmd.DecodeMoveMapRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-move-blocked",
			"body_len", len(body),
			"reason", "current_exe_op45_request_malformed",
			"error", err)
		return nil
	}
	if session == nil {
		return nil
	}

	session.dungeon.mu.Lock()
	before := runtimeDungeonRoomKey{}
	beforeKnown := false
	if session.dungeon.runtime != nil && session.dungeon.runtime.Session != nil {
		if scene, ok := session.dungeon.runtime.Session.Scene(); ok {
			before, beforeKnown = runtimeDungeonRoomKeyFromScene(scene), true
		}
	}
	err = s.handleDungeonMoveRequestLocked(session, request, len(body), "client_op45", partyPlan)
	after := runtimeDungeonRoomKey{}
	afterKnown := false
	afterSeed := uint32(0)
	if session.dungeon.runtime != nil && session.dungeon.runtime.Session != nil {
		if scene, ok := session.dungeon.runtime.Session.Scene(); ok {
			after, afterKnown = runtimeDungeonRoomKeyFromScene(scene), true
			afterSeed = session.dungeon.runtime.Seed
		}
	}
	session.dungeon.mu.Unlock()
	if err != nil {
		return err
	}
	if fanoutParty && beforeKnown && afterKnown && before != after {
		if err := s.moveRuntimePartyFollowers(session, body, runtimePartyDungeonMovePlan{target: after, seed: afterSeed}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) moveRuntimePartyFollowers(leader *gameSession, body []byte, plan runtimePartyDungeonMovePlan) error {
	if leader == nil || leader.selectedCharacterID == 0 {
		return nil
	}
	state := runtimePartyStateSnapshot(leader)
	if state.PartyID <= 0 || state.UserID != leader.selectedCharacterID {
		return nil
	}
	for _, member := range runtimePartyMembers(state) {
		if member.UserID == 0 || member.UserID == leader.selectedCharacterID {
			continue
		}
		follower, ok := s.onlineGameSession(member.UserID)
		if !ok || follower == leader {
			return fmt.Errorf("party member %d has no resident game session during room transition", member.UserID)
		}
		followerState := runtimePartyStateSnapshot(follower)
		if followerState.PartyID != state.PartyID || followerState.UserID != state.UserID {
			return fmt.Errorf("party member %d changed party state during room transition", member.UserID)
		}
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		err := s.callGameSession(ctx, follower, "runtime-party-dungeon-room-transition", func() error {
			return s.handleDungeonMoveMapPlanned(follower, append([]byte(nil), body...), &plan, false)
		})
		cancel()
		if err != nil {
			return fmt.Errorf("move runtime party member %d with leader: %w", member.UserID, err)
		}
	}
	return nil
}

// handleDungeonMoveRequestLocked runs with session.dungeon.mu held. Every op45
// is handled exactly once: an uncleared-room request is rejected and never
// retained for a later unsolicited op29.
func (s *Service) handleDungeonMoveRequestLocked(
	session *gameSession,
	request dungeoncmd.MoveMapRequest,
	requestBodyLen int,
	requestSource string,
	partyPlan *runtimePartyDungeonMovePlan,
) error {
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil || runtime.Room == nil {
		s.logGameEvent(session, "game-dungeon-move-blocked",
			"next_x", request.NextX,
			"next_y", request.NextY,
			"reason", "dungeon_runtime_missing")
		return nil
	}
	plan, err := s.planCurrentDungeonMove(runtime, request)
	if err != nil {
		currentScene, hasCurrentScene := runtime.Session.Scene()
		source := plan.Source.Coordinate.String()
		if hasCurrentScene {
			source = currentScene.Coordinate.String()
		}
		s.logGameEvent(session, "game-dungeon-move-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"source", source,
			"next_x", request.NextX,
			"next_y", request.NextY,
			"request_source", requestSource,
			"reason", "target_room_preflight_failed",
			"error", err)
		return nil
	}
	if partyPlan != nil {
		target := runtimeDungeonRoomKeyFromScene(plan.Target)
		if target != partyPlan.target {
			return fmt.Errorf("party dungeon room topology mismatch got=%+v want=%+v", target, partyPlan.target)
		}
		plan.Seed = partyPlan.seed
	}
	s.cancelCurrentDungeonReturnAfterDungeonEvidenceLocked(
		session,
		runtime,
		"accepted_current_exe_op45_after_pending_op24",
	)
	if plan.LayerBaseAck {
		chain := runtime.LayerChains[plan.Source.Coordinate]
		if chain == nil || !chain.Consumed || !chain.FinalAckPending {
			return fmt.Errorf("%w: layered base ACK state changed before commit", errDungeonMoveRuntimeOwnerMismatch)
		}
		chain.FinalAckPending = false
		s.logGameEvent(session, "game-dungeon-layer-base-ack-consumed",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", plan.Source.Coordinate.String(),
			"map_id", plan.Source.Map.Map.ID,
			"request_body_len", requestBodyLen,
			"request_source", requestSource,
			"response", "none",
			"reason", "current_exe_final_same_coordinate_change_map_confirmation_after_operation2_mode0_base_restore")
		return nil
	}
	startMapBody, err := buildCurrentDungeonMoveStartMapBody(runtime, plan)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-move-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"source", plan.Source.Coordinate.String(),
			"target", plan.Target.Coordinate.String(),
			"reason", "target_start_map_build_failed",
			"error", err)
		return nil
	}
	targetSnapshot := plan.TargetRoom.Snapshot()
	packetActorCount := len(targetSnapshot.Monsters) + len(targetSnapshot.ExtendedActors)
	if plan.PayloadMode == currentDungeonStartMapPayloadCached {
		packetActorCount = 0
	}
	s.logGameEvent(session, "game-dungeon-move-start-map-send",
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"source", plan.Source.Coordinate.String(),
		"target", plan.Target.Coordinate.String(),
		"map_id", plan.Target.Map.Map.ID,
		"seed", plan.Seed,
		"actor_count", packetActorCount,
		"revisit", plan.Revisit,
		"layered", plan.Layered,
		"layer_return", plan.LayerReturn,
		"layer_exit", plan.LayerExit,
		"layer_base_ack", plan.LayerBaseAck,
		"layer_index", plan.LayerIndex,
		"story_stage", plan.StoryStage,
		"story_stage_index", plan.StoryStageIndex,
		"operation", plan.Operation,
		"payload_mode", plan.PayloadMode,
		"move_kind", request.MoveKind,
		"request_body_len", requestBodyLen,
		"request_source", requestSource,
		"body_len", len(startMapBody),
		"sequence", "current_exe_op45_request_to_class0_op29_start_map")
	if err := s.sendGameUpperRawClass(session, currentDungeonStartNotification, startMapBody, 0); err != nil {
		return err
	}
	if err := runtime.cacheCurrentDungeonRoom(); err != nil {
		return fmt.Errorf("cache source dungeon room before transition commit: %w", err)
	}
	var revisitScene *worldmap.DungeonRoomScene
	if plan.Revisit && !plan.StoryStage {
		revisitScene = &plan.TargetVisit.Scene
	}
	var committedScene worldmap.DungeonRoomScene
	if plan.StoryStage {
		transition, commitErr := runtime.Session.CommitStoryStage(plan.StoryStageIndex)
		if commitErr != nil {
			return fmt.Errorf("commit current dungeon story stage after op29 send: %w", commitErr)
		}
		if transition.Revisit != plan.Revisit {
			return fmt.Errorf("%w: story-stage revisit changed before commit", errDungeonMoveRuntimeOwnerMismatch)
		}
		committedScene = transition.Scene
	} else if plan.Layered {
		committedScene, err = runtime.Session.CommitLayered(plan.Target.Coordinate, plan.LayerIndex)
		if err != nil {
			return fmt.Errorf("commit current layered dungeon room after op29 send: %w", err)
		}
	} else if plan.LayerReturn {
		if plan.TargetVisit == nil {
			return fmt.Errorf("%w: layered base visit missing at commit", errDungeonMoveRuntimeOwnerMismatch)
		}
		committedScene, err = runtime.Session.CommitLayeredBase(plan.Target.Coordinate, plan.TargetVisit.Scene)
		if err != nil {
			return fmt.Errorf("commit cached layered dungeon base after op29 send: %w", err)
		}
	} else if plan.LayerExit {
		transition, moveErr := runtime.Session.MoveByteTransition(request.NextX, request.NextY, revisitScene)
		if moveErr != nil {
			return fmt.Errorf("commit terminal layered dungeon exit after op29 send: %w", moveErr)
		}
		committedScene = transition.Scene
	} else {
		transition, moveErr := runtime.Session.MoveByteTransition(request.NextX, request.NextY, revisitScene)
		if moveErr != nil {
			return fmt.Errorf("commit current dungeon room after op29 send: %w", moveErr)
		}
		committedScene = transition.Scene
	}
	if committedScene.Coordinate != plan.Target.Coordinate || committedScene.Map.Map.ID != plan.Target.Map.Map.ID {
		return fmt.Errorf("%w: planned=%s/%d committed=%s/%d",
			errDungeonMoveRuntimeOwnerMismatch,
			plan.Target.Coordinate,
			plan.Target.Map.Map.ID,
			committedScene.Coordinate,
			committedScene.Map.Map.ID,
		)
	}
	s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "dungeon_room_transition_committed")
	runtime.Room = plan.TargetRoom
	runtime.NextObjectKey = plan.NextObjectKey
	runtime.Seed = plan.Seed
	if !plan.LayerReturn {
		if _, err := s.applyPVFTutorialBasicActionRoomToSession(
			session,
			runtime,
			committedScene,
			"room_transition_before_actor_announce",
		); err != nil {
			return fmt.Errorf("apply tutorial basic-action target room: %w", err)
		}
		committedScene, _ = runtime.Session.Scene()
	}
	if plan.StoryStage {
		if plan.StoryStageIndex != runtime.StoryStageIndex+1 {
			return fmt.Errorf("%w: story-stage index changed before commit", errDungeonMoveRuntimeOwnerMismatch)
		}
		runtime.StoryStageIndex = plan.StoryStageIndex
		runtime.LayeredMapIndex = -1
		runtime.LayeredMapActive = false
	} else if plan.Layered {
		if runtime.LayerChains == nil {
			runtime.LayerChains = make(map[worldmap.RoomCoordinate]*runtimeDungeonLayerChain)
		}
		chain := runtime.LayerChains[plan.Target.Coordinate]
		if chain == nil {
			chain = &runtimeDungeonLayerChain{BaseKey: plan.LayerBaseKey}
			runtime.LayerChains[plan.Target.Coordinate] = chain
		} else if chain.BaseKey != plan.LayerBaseKey {
			return fmt.Errorf("%w: layered base key changed before commit", errDungeonMoveRuntimeOwnerMismatch)
		}
		chain.Consumed = false
		chain.FinalAckPending = false
		runtime.LayeredMapIndex = plan.LayerIndex
		runtime.LayeredMapActive = true
	} else if plan.LayerReturn {
		chain := runtime.LayerChains[plan.Target.Coordinate]
		if chain == nil || chain.BaseKey != plan.LayerBaseKey {
			return fmt.Errorf("%w: layered return chain changed before commit", errDungeonMoveRuntimeOwnerMismatch)
		}
		chain.Consumed = true
		chain.FinalAckPending = true
		runtime.LayeredMapIndex = -1
		runtime.LayeredMapActive = false
	} else if plan.LayerExit {
		chain := runtime.LayerChains[plan.Source.Coordinate]
		if chain == nil || chain.BaseKey != plan.LayerBaseKey {
			return fmt.Errorf("%w: terminal layered exit chain changed before commit", errDungeonMoveRuntimeOwnerMismatch)
		}
		chain.Consumed = true
		chain.FinalAckPending = false
		runtime.LayeredMapIndex = -1
		runtime.LayeredMapActive = false
	} else {
		runtime.LayeredMapIndex = -1
		runtime.LayeredMapActive = false
	}
	announcedActors := 0
	if plan.PayloadMode != currentDungeonStartMapPayloadCached {
		announcedActors, err = runtime.Room.AnnounceAllActors(runtime.Session)
		if err != nil {
			return fmt.Errorf("commit target-room runtime actors after op29 send: %w", err)
		}
	}
	if err := runtime.cacheCurrentDungeonRoom(); err != nil {
		return fmt.Errorf("cache committed target dungeon room: %w", err)
	}
	// FINISH_LOADING's main class0 state and op30 completion are room-scoped.
	// Keep the player/equipment/skill initialization gates intact: an ordinary
	// room move only needs the next room's loading completion, not a new actor.
	session.currentFinishLoadingStateSent = false
	session.currentFinishLoadingCompletionSent = false
	s.logGameEvent(session, "game-dungeon-move-committed",
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"source", plan.Source.Coordinate.String(),
		"target", committedScene.Coordinate.String(),
		"map_id", committedScene.Map.Map.ID,
		"announced_actor_count", announcedActors,
		"revisit", plan.Revisit,
		"layered", plan.Layered,
		"layer_return", plan.LayerReturn,
		"layer_exit", plan.LayerExit,
		"layer_base_ack", plan.LayerBaseAck,
		"layer_index", plan.LayerIndex,
		"story_stage", plan.StoryStage,
		"story_stage_index", plan.StoryStageIndex,
		"operation", plan.Operation,
		"payload_mode", plan.PayloadMode,
		"cached_defeated_actor_count", len(committedScene.DefeatedObjects),
		"room_cleared", committedScene.Cleared,
		"request_source", requestSource,
		"finish_loading_main_state_rearmed", true)
	return nil
}
