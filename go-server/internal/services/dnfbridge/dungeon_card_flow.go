package dnfbridge

import (
	"context"
	"fmt"
	"math"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const (
	currentDungeonCardResponseClass byte = 1
	currentDungeonCardAutoFlipDelay      = 4 * time.Second
)

func currentDungeonCardLayoutValues(state ...*dungeonCardState) [dungeonCardWireSlotCount]uint16 {
	values := [dungeonCardWireSlotCount]uint16{0x0001, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff}
	if len(state) == 0 || state[0] == nil {
		return values
	}
	state[0].mu.Lock()
	defer state[0].mu.Unlock()
	if bundle := state[0].plan.Sides[dungeonCardSidePaid]; bundle.Gold > 0 || len(bundle.Items) > 0 {
		values[dungeonCardSlotsPerSide] = 0x0001
	}
	return values
}

func (s *Service) sendCurrentDungeonCardScrollStateLocked(session *gameSession, runtime *runtimeDungeonState, source string) error {
	if session == nil || runtime == nil || session.dungeon.runtime != runtime {
		return nil
	}
	if !runtime.settlementClearRewardSent || runtime.settlementPhase < currentDungeonSettlementPhaseResultShown {
		s.logGameEvent(session, "game-dungeon-card-scroll-state-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"source", source,
			"settlement_phase", runtime.settlementPhase.String(),
			"reason", "result_phase_not_committed")
		return nil
	}
	replay := runtime.settlementCardScrollStateSent
	if err := s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketScoreScrollState),
		buildCurrentDungeonOp69SuccessBody(),
		currentDungeonCardResponseClass,
	); err != nil {
		return err
	}
	runtime.settlementCardScrollStateSent = true
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseCardScrollStarted)
	s.logGameEvent(session, "game-dungeon-card-scroll-state-ack-sent",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"source", source,
		"replay", replay,
		"op69_body_len", len(buildCurrentDungeonOp69SuccessBody()),
		"classification", currentDungeonCardResponseClass,
		"next_stage", "await_current_exe_c2s_op70",
		"body_source", "current_exe_op69_proved_reader")
	return nil
}

func (s *Service) sendCurrentDungeonCardLayoutLocked(session *gameSession, runtime *runtimeDungeonState, source string) error {
	if session == nil || runtime == nil || session.dungeon.runtime != runtime {
		return nil
	}
	if !runtime.settlementCardScrollStateSent || runtime.settlementPhase < currentDungeonSettlementPhaseCardScrollStarted {
		s.logGameEvent(session, "game-dungeon-card-layout-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"source", source,
			"settlement_phase", runtime.settlementPhase.String(),
			"reason", "op69_scroll_state_not_acknowledged")
		return nil
	}
	if runtime.settlementCardRewardState == nil {
		if err := s.freezeCurrentDungeonCardRewardStateLocked(session, runtime); err != nil {
			return err
		}
	}
	replay := runtime.settlementCardLayoutSent
	body := buildCurrentDungeonOp70EightValueSuccessBody(currentDungeonCardLayoutValues(runtime.settlementCardRewardState))
	if err := s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketCardSelectRightState),
		body,
		currentDungeonCardResponseClass,
	); err != nil {
		return err
	}
	runtime.settlementCardRightStateSent = true
	runtime.settlementCardLayoutSent = true
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseCardsRevealed)
	if !replay {
		s.armCurrentDungeonCardAutoFlipLocked(session, runtime)
	}
	s.logGameEvent(session, "game-dungeon-card-layout-sent",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"source", source,
		"replay", replay,
		"op70_body_len", len(body),
		"classification", currentDungeonCardResponseClass,
		"next_stage", "await_current_exe_c2s_op71",
		"body_source", "current_exe_op70_proved_reader")
	return nil
}

func (s *Service) handleDungeonScoreScrollState(session *gameSession, body []byte) error {
	// The semantic request is bodyless, but the legacy game transport can pad the
	// body (like op71/op72). A padded body carries no extra meaning, so proceed
	// and only note the padding instead of rejecting the whole request.
	if len(body) != 0 {
		s.logGameEvent(session, "game-dungeon-card-layout-request-padding",
			"body_len", len(body),
			"reason", "current_exe_op69_semantic_request_bodyless_legacy_padding_ignored")
	}
	if session == nil {
		return nil
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	return s.sendCurrentDungeonCardScrollStateLocked(session, session.dungeon.runtime, "current_exe_op69_request")
}

func (s *Service) handleDungeonCardSelectRightState(session *gameSession, body []byte) error {
	if len(body) != 0 {
		s.logGameEvent(session, "game-dungeon-card-layout-request-padding",
			"body_len", len(body),
			"reason", "current_exe_op70_semantic_request_bodyless_legacy_padding_ignored")
	}
	if session == nil {
		return nil
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	return s.sendCurrentDungeonCardLayoutLocked(session, session.dungeon.runtime, "current_exe_op70_request")
}

func (s *Service) handleDungeonSelectCard(session *gameSession, body []byte) error {
	request, err := decodeCurrentDungeonOp71Request(body)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-card-select-blocked",
			"body_len", len(body),
			"reason", "current_exe_op71_request_shape_mismatch",
			"error", err)
		return nil
	}
	if session == nil {
		return nil
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if runtime == nil || !runtime.settlementCardLayoutSent ||
		runtime.settlementPhase < currentDungeonSettlementPhaseCardsRevealed {
		s.logGameEvent(session, "game-dungeon-card-select-blocked",
			"char_id", session.selectedCharacterID,
			"request_value_a", request.ValueA,
			"request_value_b", request.ValueB,
			"request_value_count", request.ValueCount,
			"reason", "card_layout_not_committed_after_op70")
		return nil
	}
	side, ok := currentDungeonCardRequestSide(request.ValueA)
	if !ok || request.ValueB >= dungeonCardSlotsPerSide {
		s.logGameEvent(session, "game-dungeon-card-select-blocked",
			"char_id", session.selectedCharacterID,
			"request_value_a", request.ValueA,
			"request_value_b", request.ValueB,
			"reason", "current_exe_op71_row_or_member_index_invalid")
		return nil
	}
	return s.selectCurrentDungeonCardLocked(
		session,
		runtime,
		side,
		request.ValueB,
		"current_exe_op71_select_request",
	)
}

func currentDungeonSelectedCardSlots(
	runtime *runtimeDungeonState,
) ([dungeonCardWireSlotCount]currentDungeonOp71Slot, int) {
	var slots [dungeonCardWireSlotCount]currentDungeonOp71Slot
	for index := range slots {
		slots[index] = currentDungeonOp71Slot{StateA: 0xff, StateB: 0xff}
	}
	if runtime == nil || runtime.settlementCardRewardState == nil {
		return slots, 0
	}
	runtime.settlementCardRewardState.mu.Lock()
	plan := cloneDungeonCardRewardPlan(runtime.settlementCardRewardState.plan)
	runtime.settlementCardRewardState.mu.Unlock()

	rewardTupleCount := 0
	for side := dungeonCardSide(0); side < dungeonCardSideCount; side++ {
		if !runtime.settlementCardSideSelectionKnown[side] {
			continue
		}
		memberIndex := runtime.settlementCardSideMember[side]
		if memberIndex >= dungeonCardSlotsPerSide {
			continue
		}
		wireIndex := int(memberIndex)
		slot := slots[wireIndex]
		if side == dungeonCardSideFree {
			slot.StateA = 0
		} else {
			slot.StateB = 0
			slot.Rewards = currentDungeonOp71RewardTuplesFromCardBundle(plan.Sides[side])
		}
		rewardTupleCount += len(slot.Rewards)
		slots[wireIndex] = slot
	}
	return slots, rewardTupleCount
}

func currentDungeonCardRequestSide(value byte) (dungeonCardSide, bool) {
	switch value {
	case 0:
		return dungeonCardSideFree, true
	case 1, 2:
		return dungeonCardSidePaid, true
	default:
		return 0, false
	}
}

func currentDungeonOp71RewardTuplesFromCardBundle(bundle dungeonCardRewardBundle) []currentDungeonOp71RewardTuple {
	displayItems := currentDungeonCardDisplayItems(bundle)
	tuples := make([]currentDungeonOp71RewardTuple, 0, 1+len(displayItems))
	if bundle.Gold > 0 {
		tuples = append(tuples, currentDungeonOp71RewardTuple{ValueA: 0, ValueB: uint32(bundle.Gold)})
	}
	for _, item := range displayItems {
		if item.ItemID <= 0 || item.Count <= 0 {
			continue
		}
		count := item.Count
		if count > math.MaxUint32 {
			count = math.MaxUint32
		}
		tuples = append(tuples, currentDungeonOp71RewardTuple{
			ValueA: uint32(item.ItemID),
			ValueB: uint32(count),
		})
	}
	return tuples
}

func (s *Service) handleDungeonEplpCommand(session *gameSession, body []byte) error {
	request, err := decodeCurrentDungeonOp72Request(body)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-card-exit-blocked",
			"body_len", len(body),
			"reason", "current_exe_op72_request_shape_mismatch",
			"error", err)
		return nil
	}
	if session == nil {
		return nil
	}

	// Current-EXE EPLP writers prove {u8 state, u8 option} semantics, and live
	// plaintext clicks on the patched client pinned the option indices: hover
	// sends {2, index}; clicks send {1, 0} for 再次挑战, {1, 1} for
	// 选择其它地下城, and {1, 2} for 返回城镇 (the sub_32BF720
	// settlement-countdown expiry also sends {1,2}). Only {1,2} owns the
	// completed-dungeon town route. After their exact ACK echo, {1,0} is
	// answered by a server-driven same-dungeon re-entry (the client waits for
	// the entry sequence instead of sending op16) and {1,1} by the current
	// op15/op27 selector push (the client waits instead of sending op15).
	// None of those actions may cross the op69 -> op70 -> op71 card/reward
	// barriers. The throttled {2,x} focus notification is ACK-only and must not consume
	// the one-shot exit gate, or the following real click would lose its ACK.
	isFocusNotification := request.ValueA == 2
	shouldReturnToTown := request.ValueA == 1 && request.ValueB == 2
	shouldRetrySameDungeon := request.ValueA == 1 && request.ValueB == 0
	shouldOpenOtherSelector := request.ValueA == 1 && request.ValueB == 1

	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	if runtime == nil || !runtime.settlementClearRewardSent {
		s.logGameEvent(session, "game-dungeon-card-exit-blocked",
			"char_id", session.selectedCharacterID,
			"request_value_a", request.ValueA,
			"request_value_b", request.ValueB,
			"reason", "settlement_clear_reward_not_sent")
		session.dungeon.mu.Unlock()
		return nil
	}
	if isFocusNotification {
		response := buildCurrentDungeonOp72SuccessBody(request.ValueA, request.ValueB)
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketEplpCommand), response, currentDungeonCardResponseClass); err != nil {
			session.dungeon.mu.Unlock()
			return err
		}
		s.logGameEvent(session, "game-dungeon-card-focus-ack-sent",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"request_value_a", request.ValueA,
			"request_value_b", request.ValueB,
			"classification", currentDungeonCardResponseClass)
		session.dungeon.mu.Unlock()
		return nil
	}
	if runtime.settlementCardLayoutSent &&
		!runtime.settlementCardSideSelectionSent[dungeonCardSideFree] {
		if err := s.selectCurrentDungeonCardLocked(
			session,
			runtime,
			dungeonCardSideFree,
			0,
			"current_exe_op72_auto_flip_free_row_before_action",
		); err != nil {
			s.logGameEvent(session, "game-dungeon-card-exit-auto-flip-blocked",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"request_value_a", request.ValueA,
				"request_value_b", request.ValueB,
				"error", err)
		}
	}
	if runtime.settlementCardSelectionSent && !runtime.settlementCardRewardCommitted {
		if err := s.commitCurrentDungeonCardRewardLocked(session, runtime, "current_exe_op72_reward_retry_before_exit"); err != nil {
			s.logGameEvent(session, "game-dungeon-card-exit-reward-retry-blocked",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"request_value_a", request.ValueA,
				"request_value_b", request.ValueB,
				"error", err)
		}
	}
	if !runtime.settlementCardLayoutSent || !runtime.settlementCardSelectionSent ||
		!runtime.settlementCardRewardCommitted ||
		runtime.settlementPhase < currentDungeonSettlementPhaseRewardCommitted {
		s.logGameEvent(session, "game-dungeon-card-exit-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"request_value_a", request.ValueA,
			"request_value_b", request.ValueB,
			"layout_sent", runtime.settlementCardLayoutSent,
			"selection_sent", runtime.settlementCardSelectionSent,
			"reward_committed", runtime.settlementCardRewardCommitted,
			"settlement_phase", runtime.settlementPhase.String(),
			"reason", "op72_cannot_cross_unfinished_card_or_reward_phase")
		session.dungeon.mu.Unlock()
		return nil
	}

	runtimeForReturn := runtime
	if runtime.settlementCardExitAckSent {
		s.logGameEvent(session, "game-dungeon-card-exit-replay",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"request_value_a", request.ValueA,
			"request_value_b", request.ValueB,
			"reason", "exit_ack_already_sent")
		session.dungeon.mu.Unlock()
		if shouldReturnToTown {
			ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
			transition, transitionErr := s.prepareCurrentDungeonTownTransitionForSession(ctx, session, session.selectedCharacterID)
			cancel()
			if transitionErr != nil {
				s.logGameEvent(session, "game-dungeon-card-exit-return-blocked",
					"char_id", session.selectedCharacterID,
					"request_value_a", request.ValueA,
					"request_value_b", request.ValueB,
					"reason", "persisted_town_transition_unavailable",
					"error", transitionErr)
				return nil
			}
			return s.sendCurrentCompletedDungeonReturnToTown(
				session,
				runtimeForReturn,
				transition,
				uint16(dnfenum.CmdPacketEplpCommand),
				"current_exe_op72_card_exit_replay_return_to_town",
			)
		}
		return nil
	}
	response := buildCurrentDungeonOp72SuccessBody(request.ValueA, request.ValueB)
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketEplpCommand), response, currentDungeonCardResponseClass); err != nil {
		session.dungeon.mu.Unlock()
		return err
	}
	// A {2,x} focus notification is answered but must not consume the one-shot
	// exit gate; the following real click still needs its own ACK.
	if !isFocusNotification {
		runtime.settlementCardExitAckSent = true
		if shouldReturnToTown || shouldRetrySameDungeon || shouldOpenOtherSelector {
			runtime.advanceSettlementPhase(currentDungeonSettlementPhaseEnding)
		}
	}
	s.logGameEvent(session, "game-dungeon-card-exit-ack-sent",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"request_value_a", request.ValueA,
		"request_value_b", request.ValueB,
		"focus_notification", isFocusNotification,
		"response_body_len", len(response),
		"classification", currentDungeonCardResponseClass,
		"body_source", "current_exe_op72_proved_reader")
	session.dungeon.mu.Unlock()

	if shouldReturnToTown {
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		transition, transitionErr := s.prepareCurrentDungeonTownTransitionForSession(ctx, session, session.selectedCharacterID)
		cancel()
		if transitionErr != nil {
			s.logGameEvent(session, "game-dungeon-card-exit-return-blocked",
				"char_id", session.selectedCharacterID,
				"request_value_a", request.ValueA,
				"request_value_b", request.ValueB,
				"reason", "persisted_town_transition_unavailable",
				"error", transitionErr)
			return nil
		}
		return s.sendCurrentCompletedDungeonReturnToTown(
			session,
			runtimeForReturn,
			transition,
			uint16(dnfenum.CmdPacketEplpCommand),
			"current_exe_op72_card_exit_return_to_town",
		)
	}
	if shouldRetrySameDungeon {
		return s.retryCurrentCompletedDungeon(session, runtimeForReturn, "current_exe_op72_completed_retry_same_dungeon")
	}
	if shouldOpenOtherSelector {
		return s.sendEnterSelectDungeonState(session, "current_exe_op72_completed_select_other_dungeon", false, true)
	}
	return nil
}

func (s *Service) armCurrentDungeonCardAutoFlipLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
) {
	if s == nil || session == nil || runtime == nil ||
		session.dungeon.runtime != runtime ||
		!runtime.settlementCardLayoutSent ||
		runtime.settlementCardSideSelectionKnown[dungeonCardSideFree] {
		return
	}
	s.cancelCurrentDungeonCardAutoFlipLocked(session, runtime, "replace_auto_flip_timer")
	queue := s.ensureGameplayTimeQueue()
	if queue == nil {
		return
	}
	runtime.settlementCardAutoFlipGeneration++
	generation := runtime.settlementCardAutoFlipGeneration
	timerName := fmt.Sprintf(
		"dnf-dungeon-card-auto-flip:%s:run:%d",
		session.connID,
		runtime.lifecycleToken,
	)
	characterID, characterGeneration, err := gameSessionCharacterEventIdentity(
		session,
		session.selectedCharacterID,
	)
	if err != nil {
		return
	}
	runtime.settlementCardAutoFlipTimerName = timerName
	if err := queue.ScheduleAfter(timerName, currentDungeonCardAutoFlipDelay, func(time.Time) {
		postErr := s.postGameSessionCharacterEvent(
			session,
			"dungeon-card-auto-flip",
			characterID,
			characterGeneration,
			func() error {
				session.dungeon.mu.Lock()
				defer session.dungeon.mu.Unlock()
				current := session.dungeon.runtime
				if current != runtime ||
					runtime.settlementCardAutoFlipGeneration != generation ||
					!runtime.settlementCardLayoutSent ||
					runtime.settlementCardSideSelectionKnown[dungeonCardSideFree] {
					return nil
				}
				runtime.settlementCardAutoFlipTimerName = ""
				return s.selectCurrentDungeonCardLocked(
					session,
					runtime,
					dungeonCardSideFree,
					0,
					"timequeue_auto_flip_free_row",
				)
			},
		)
		if postErr != nil && !isClosedGameSessionEventError(postErr) {
			s.logPacketEvent("game-dungeon-card-auto-flip-submit-failed",
				"conn_id", session.connID,
				"char_id", characterID,
				"error", postErr)
		}
	}); err != nil {
		runtime.settlementCardAutoFlipTimerName = ""
		s.logGameEvent(session, "game-dungeon-card-auto-flip-schedule-failed",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"error", err)
	}
}

func (s *Service) cancelCurrentDungeonCardAutoFlipLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	source string,
) {
	if runtime == nil {
		return
	}
	timerName := runtime.settlementCardAutoFlipTimerName
	runtime.settlementCardAutoFlipTimerName = ""
	runtime.settlementCardAutoFlipGeneration++
	if timerName == "" || s == nil {
		return
	}
	cancelled := false
	if queue := s.ensureGameplayTimeQueue(); queue != nil {
		cancelled = queue.Cancel(timerName)
	}
	s.logGameEvent(session, "game-dungeon-card-auto-flip-cancelled",
		"char_id", runtime.Character.CharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"source", source,
		"cancelled", cancelled)
}

// retryCurrentCompletedDungeon answers the settlement dialog's 再次挑战
// request ({state=1, option=0}). Live plaintext evidence shows the client
// waits for a server-driven re-entry after the ACK instead of sending op16,
// so the owner retires the completed run and reuses the standard op16 entry
// flow with the client's own proven request (same dungeon, difficulty, and
// entry options) cloned from the retired runtime.
func (s *Service) retryCurrentCompletedDungeon(session *gameSession, runtime *runtimeDungeonState, source string) error {
	if session == nil || session.selectedCharacterID == 0 || runtime == nil || runtime.Session == nil {
		return nil
	}
	characterID := session.selectedCharacterID
	if !completedDungeonSettlementExitReady(runtime, characterID) {
		s.logGameEvent(session, "game-dungeon-card-exit-retry-blocked",
			"char_id", characterID,
			"reason", "completed_settlement_exit_not_ready",
			"source", source)
		return nil
	}
	request := runtime.Request
	dungeonID := runtime.Dungeon.ID
	nextDungeonID, nextQuestID, hasNextStory := s.resolveNextStoryQuestDungeon(runtime)
	retired, err := s.retireCompletedDungeonForTownSelect(session)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-card-exit-retry-blocked",
			"char_id", characterID,
			"dungeon_id", dungeonID,
			"reason", "completed_runtime_retire_failed",
			"source", source,
			"error", err)
		return nil
	}
	if !retired {
		s.logGameEvent(session, "game-dungeon-card-exit-retry-blocked",
			"char_id", characterID,
			"dungeon_id", dungeonID,
			"reason", "completed_runtime_retire_declined",
			"source", source)
		return nil
	}
	// The ordinary-dungeon entry flow freezes the new run's town-return origin
	// from the bound selector origin. Re-bind it from the retirement-restored
	// town snapshot, or ordinary re-entry is rejected with
	// ordinary_dungeon_town_return_origin_unavailable (live proof).
	origin, originBound := bindCurrentTownSelectorOrigin(session)
	if !originBound {
		s.logGameEvent(session, "game-dungeon-card-exit-retry-blocked",
			"char_id", characterID,
			"dungeon_id", dungeonID,
			"reason", "town_selector_origin_unavailable",
			"source", source)
		return nil
	}
	enterDungeonID := dungeonID
	enterReason := "retired_runtime_cloned_op16_request"
	if hasNextStory && nextDungeonID != dungeonID {
		// Story context (the dialog's 开始下个任务): the completed maze is
		// quest-connected, so the client's own chain leads to the next quest's
		// dungeon instead of a same-dungeon retry. Difficulty/entry options
		// still come from the finished run.
		enterDungeonID = nextDungeonID
		enterReason = "pvf_quest_chain_next_dungeon"
	}
	request.DungeonID = uint32(enterDungeonID)
	s.logGameEvent(session, "game-dungeon-card-exit-retry-enter",
		"char_id", characterID,
		"dungeon_id", dungeonID,
		"enter_dungeon_id", enterDungeonID,
		"next_quest_id", nextQuestID,
		"has_next_story", hasNextStory,
		"difficulty", request.Difficulty,
		"entry_option", request.EntryOption,
		"selection_mode", request.SelectionMode,
		"town_id", origin.TownID,
		"area_id", origin.AreaID,
		"source", source,
		"body_source", enterReason)
	return s.handleDungeonSelectUpper(session, encodeCurrentDungeonSelectRequest(request))
}

// resolveNextStoryQuestDungeon resolves the story chain's next dungeon after a
// quest-connected completed run: the completed maze's active quest connection
// identifies the finished story quest, the PVF quest catalog's prerequisite
// chain identifies its successor, and the next dungeon is the one whose maze
// carries the successor's active quest connection. It returns false for
// ordinary (non quest-connected) runs and for chain ends whose successor has
// no dungeon, leaving those to the same-dungeon retry owner.
func (s *Service) resolveNextStoryQuestDungeon(runtime *runtimeDungeonState) (int64, int64, bool) {
	if s == nil || runtime == nil || runtime.MazeIndex < 0 || runtime.MazeIndex >= len(runtime.Dungeon.Mazes) {
		return 0, 0, false
	}
	connection := runtime.Dungeon.Mazes[runtime.MazeIndex].QuestConnection
	if len(connection) < 2 || connection[0] != 0 {
		return 0, 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	catalog, err := s.loadQuestCatalog(ctx)
	cancel()
	if err != nil || catalog == nil {
		return 0, 0, false
	}
	table, _, err := s.dungeonWorldMap()
	if err != nil || table == nil {
		return 0, 0, false
	}
	currentQuestID := connection[1]
	for _, successor := range catalog.Successors(currentQuestID) {
		for _, dungeon := range table.Dungeons() {
			for _, maze := range dungeon.Mazes {
				candidate := maze.QuestConnection
				if len(candidate) >= 2 && candidate[0] == 0 && candidate[1] == successor.ID {
					return dungeon.ID, successor.ID, true
				}
			}
		}
	}
	return 0, 0, false
}
