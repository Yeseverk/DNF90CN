package dnfbridge

import (
	"context"
	"strconv"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfdungeon "longheng.io/server/internal/modules/dnf/dungeon"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	currentDungeonTutorialFinalPrefix   = byte(0)
	currentDungeonTutorialPreFinal      = uint32(30)
	currentDungeonTutorialFinalProgress = uint32(31)
	currentDungeonTutorialFinalCommit   = byte(1)
	currentDungeonTutorialCompleteFlag  = int64(1)
	currentDungeonTutorialCompletedKey  = "tutorial_completed"
	// Live current-EXE evidence shows that the persisted-reentry op33/op143/op24
	// fast-exit leaves the client on the dungeon page and suppresses op45. Keep
	// the durable marker, but do not emit this unproven transition chain.
	currentDungeonCompletedReentryFastExitEnabled = false
)

// handleDungeonTutorialFlag acknowledges the current-EXE tutorial checkpoint
// request and closes the final exit handshake. Progress 30 is an ordinary
// request/response checkpoint: it receives the same empty-reward ACK as the
// old server but never persists completion, sends op115, or returns to town.
func (s *Service) handleDungeonTutorialFlag(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	request, err := dungeoncmd.DecodeChangeTutorialFlagRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-tutorial-flag-blocked",
			"body_len", len(body),
			"expected_body_len", dungeoncmd.ChangeTutorialFlagRequestSize,
			"reason", "current_exe_op143_plaintext_boundary_mismatch",
			"error", err)
		return nil
	}
	s.logGameEvent(session, "game-dungeon-tutorial-flag-request-decoded",
		"body_len", len(body),
		"prefix", request.Prefix,
		"progress", request.Progress,
		"commit_flag", request.CommitFlag,
		"body_source", "current_exe_sub_33BDAF0_exact_six_byte_writer")
	if request.Prefix != currentDungeonTutorialFinalPrefix ||
		request.CommitFlag != currentDungeonTutorialFinalCommit {
		s.logGameEvent(session, "game-dungeon-tutorial-flag-deferred",
			"prefix", request.Prefix,
			"progress", request.Progress,
			"commit_flag", request.CommitFlag,
			"reason", "unsupported_tutorial_checkpoint_envelope")
		return nil
	}
	if request.Progress == currentDungeonTutorialPreFinal {
		s.logGameEvent(session, "game-dungeon-tutorial-flag-op143-ack-send",
			"char_id", session.selectedCharacterID,
			"progress", request.Progress,
			"msg_id", uint16(dnfenum.CmdPacketChangeTutorialFlag),
			"classification", 1,
			"success", 1,
			"reward_count", 0,
			"body_len", 2,
			"completion_persisted", false,
			"town_transition", false,
			"body_source", "current_exe_sub_33C4A20_and_old_86jp_request_response_checkpoint")
		if err := s.sendGameUpperSuccess(
			session,
			uint16(dnfenum.CmdPacketChangeTutorialFlag),
			[]byte{0},
		); err != nil {
			return err
		}
		return nil
	}
	if request.Progress == currentDungeonTutorialRewardProgress {
		return s.handleCurrentDungeonTutorialReward(session, request.Progress)
	}
	if request.Progress == currentInitialTownProgress {
		return s.handleCurrentInitialTownProgress(session, request.Progress)
	}
	if request.Progress != currentDungeonTutorialFinalProgress {
		s.logGameEvent(session, "game-dungeon-tutorial-flag-deferred",
			"prefix", request.Prefix,
			"progress", request.Progress,
			"commit_flag", request.CommitFlag,
			"reason", "unsupported_tutorial_checkpoint_progress")
		return nil
	}
	selectedCharacterID := session.selectedCharacterID
	if selectedCharacterID == 0 {
		s.logGameEvent(session, "game-dungeon-tutorial-flag-blocked",
			"progress", request.Progress,
			"reason", "selected_character_missing")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	transition, err := s.prepareCurrentDungeonTownTransitionForSession(ctx, session, selectedCharacterID)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-tutorial-flag-blocked",
			"char_id", session.selectedCharacterID,
			"progress", request.Progress,
			"reason", "persisted_town_transition_unavailable",
			"error", err)
		return nil
	}

	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil || runtime.Room == nil {
		s.logGameEvent(session, "game-dungeon-tutorial-flag-blocked",
			"char_id", session.selectedCharacterID,
			"progress", request.Progress,
			"reason", "active_dungeon_runtime_missing")
		return nil
	}
	if session.selectedCharacterID != selectedCharacterID || !dungeonRuntimeOwnsCharacter(runtime, selectedCharacterID) {
		s.logGameEvent(session, "game-dungeon-tutorial-flag-blocked",
			"char_id", session.selectedCharacterID,
			"request_char_id", selectedCharacterID,
			"runtime_char_id", runtime.Character.CharacterID,
			"progress", request.Progress,
			"reason", "active_dungeon_runtime_character_owner_mismatch")
		return nil
	}
	snapshot := runtime.Session.Snapshot()
	persistedReentryCompletion := snapshot.Run.Status == worldmap.DungeonRunActive &&
		runtime.tutorialCompletedReentry && runtime.tutorialCompletionPersisted &&
		runtime.tutorialCompletedReentryExitSent && isPVFTutorialDungeonScene(runtime, snapshot.Scene)
	if !persistedReentryCompletion {
		s.logGameEvent(session, "game-dungeon-tutorial-flag-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"run_status", snapshot.Run.Status,
			"boss_die_check_accepted", runtime.bossDieCheckAccepted,
			"tutorial_completed_reentry", runtime.tutorialCompletedReentry,
			"tutorial_completed_reentry_exit_sent", runtime.tutorialCompletedReentryExitSent,
			"reason", "progress31_is_only_valid_for_persisted_completed_reentry")
		return nil
	}
	if !runtime.tutorialFinalFlagAckSent {
		s.logGameEvent(session, "game-dungeon-tutorial-flag-op143-ack-send",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"msg_id", uint16(dnfenum.CmdPacketChangeTutorialFlag),
			"classification", 1,
			"success", 1,
			"reward_count", 0,
			"body_len", 2,
			"body_source", "current_exe_sub_33C4A20_success_then_u8_reward_count")
		if err := s.sendGameUpperSuccess(
			session,
			uint16(dnfenum.CmdPacketChangeTutorialFlag),
			[]byte{0},
		); err != nil {
			return err
		}
		runtime.tutorialFinalFlagAckSent = true
	}

	return s.sendCurrentDungeonReturnToTownLocked(
		session,
		runtime,
		transition,
		uint16(dnfenum.CmdPacketChangeTutorialFlag),
		"persisted_completed_tutorial_reentry_op143_after_finish_loading_op33",
	)
}

// sendCompletedTutorialReentryExit conditionally starts the current-EXE
// tutorial exit handshake after FINISH_LOADING has bound the selected actor
// and page. Static gate evidence supports this order; completed-marker reentry
// still requires a fresh live acceptance before it is considered proven.
// Sending a non-empty completion index in the select ACK would switch pages
// synchronously before that owner exists and crashes the current EXE.
func (s *Service) sendCompletedTutorialReentryExit(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil || runtime.Room == nil ||
		!runtime.tutorialCompletedReentry || !runtime.tutorialCompletionPersisted {
		return nil
	}
	if !dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		s.logGameEvent(session, "game-dungeon-tutorial-reentry-exit-blocked",
			"char_id", session.selectedCharacterID,
			"runtime_char_id", runtime.Character.CharacterID,
			"reason", "active_dungeon_runtime_character_owner_mismatch")
		return nil
	}
	if runtime.tutorialCompletedReentryExitSent {
		return nil
	}
	if !currentDungeonCompletedReentryFastExitEnabled {
		s.logGameEvent(session, "game-dungeon-tutorial-reentry-exit-deferred",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"source", source,
			"tutorial_completion_persisted", runtime.tutorialCompletionPersisted,
			"reason", "live_current_exe_op24_return_failed_and_suppressed_next_room_op45")
		return nil
	}
	if !session.postStartMapPlayerStateSent || !session.currentFinishLoadingStateSent ||
		!session.currentFinishLoadingCompletionSent ||
		!session.postFinishLoadingPlayerStateSent {
		s.logGameEvent(session, "game-dungeon-tutorial-reentry-exit-deferred",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"post_start_map_state_sent", session.postStartMapPlayerStateSent,
			"finish_loading_state_sent", session.currentFinishLoadingStateSent,
			"finish_loading_completion_sent", session.currentFinishLoadingCompletionSent,
			"post_finish_loading_state_sent", session.postFinishLoadingPlayerStateSent,
			"reason", "selected_actor_and_page_not_fully_bound")
		return nil
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunActive ||
		!isPVFTutorialDungeonScene(runtime, snapshot.Scene) {
		s.logGameEvent(session, "game-dungeon-tutorial-reentry-exit-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"run_status", snapshot.Run.Status,
			"reason", "owned_active_pvf_tutorial_scene_missing")
		return nil
	}
	body := []byte{currentDungeonTutorialExitReason}
	s.logGameEvent(session, "game-dungeon-tutorial-reentry-exit-op33-send",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", snapshot.Run.Current.String(),
		"msg_id", currentDungeonTutorialExitMsgID,
		"classification", 0,
		"reason_code", currentDungeonTutorialExitReason,
		"source", source,
		"body_source", "persisted_completion_after_current_exe_finish_loading_actor_page_binding")
	if err := s.sendGameUpperRawClass(session, currentDungeonTutorialExitMsgID, body, 0); err != nil {
		return err
	}
	runtime.tutorialCompletedReentryExitSent = true
	return nil
}

// persistCurrentDungeonTutorialCompletion makes the final tutorial handshake
// survive reconnects. It updates the formal mirrored character-stat field; it
// never fabricates tutorial indexes or stores the transient progress value 31.
func (s *Service) persistCurrentDungeonTutorialCompletion(
	ctx context.Context,
	characterID uint16,
) (int64, error) {
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		return 0, dungeoncmd.ErrOwnerUnavailable
	}
	owner, err := dnfdungeon.NewOwner(repositories)
	if err != nil {
		return 0, dungeoncmd.ErrOwnerUnavailable
	}
	result, err := owner.CompleteTutorial(ctx, dnfdungeon.TutorialCompletionCommand{
		CharacterID:  strconv.FormatUint(uint64(characterID), 10),
		CompletedKey: currentDungeonTutorialCompletedKey,
		Completed:    currentDungeonTutorialCompleteFlag,
		NextLogin: map[string]int64{
			"town_id":    newCharacterInitialTownID,
			"area_id":    newCharacterInitialAreaID,
			"pos_x":      newCharacterInitialPosX,
			"pos_y":      newCharacterInitialPosY,
			"direction":  newCharacterInitialDirection,
			"area_state": newCharacterInitialAreaState,
		},
	})
	if err != nil {
		return result.Previous, err
	}
	return result.Previous, nil
}

// applyCurrentDungeonTutorialCompletionStats mirrors the durable tutorial
// decision into an already-owned character aggregate. Persistence belongs to
// the dungeon owner.
func applyCurrentDungeonTutorialCompletionStats(character *dnfrepo.CharacterRecord) bool {
	if character == nil {
		return false
	}
	if character.Stats == nil {
		character.Stats = make(map[string]int64)
	}
	values := map[string]int64{
		currentDungeonTutorialCompletedKey: currentDungeonTutorialCompleteFlag,
		"town_id":                          newCharacterInitialTownID,
		"area_id":                          newCharacterInitialAreaID,
		"pos_x":                            newCharacterInitialPosX,
		"pos_y":                            newCharacterInitialPosY,
		"direction":                        newCharacterInitialDirection,
		"area_state":                       newCharacterInitialAreaState,
	}
	changed := false
	for key, value := range values {
		if current, ok := character.Stats[key]; !ok || current != value {
			character.Stats[key] = value
			changed = true
		}
	}
	return changed
}

func hasPersistedDungeonTutorialCompletion(character dnfrepo.CharacterRecord) bool {
	return character.Stats != nil &&
		character.Stats[currentDungeonTutorialCompletedKey] == currentDungeonTutorialCompleteFlag
}
