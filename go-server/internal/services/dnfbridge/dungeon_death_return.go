package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfdungeon "longheng.io/server/internal/modules/dnf/dungeon"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	currentDungeonDieCharacterBodySize = 4
	currentDungeonUseCoinBodySize      = 2
	currentDungeonDeathStateMsgID      = uint16(32)
	currentDungeonDeathStateBodySize   = 4
	currentDungeonDeathReturnDelay     = 10 * time.Second
	// Current EXE class-1/op41 success body carries one additional u8 after the
	// success discriminator. 0 is the private-server accepted-revive flag.
	currentDungeonUseCoinSuccessFlag = byte(0)
	// Private-server playability: if the character is dead but has no coin
	// stack, still allow revive so friends are not soft-locked by the 10s timer.
	currentDungeonUseCoinAllowFreeRevive = true
)

var errGameplayTimeQueueUnavailable = errors.New("gameplay time queue is unavailable")

// handleDungeonDieCharacter accepts only the normal current-EXE writer shape:
// two u16 actor coordinates. The request does not identify the actor; the
// selected runtime owner supplies the real scene object key used by op32.
func (s *Service) handleDungeonDieCharacter(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	if len(body) != currentDungeonDieCharacterBodySize {
		s.logGameEvent(session, "game-dungeon-character-death-blocked",
			"body_len", len(body),
			"expected_body_len", currentDungeonDieCharacterBodySize,
			"reason", "current_exe_normal_op40_two_u16_coordinates_boundary_mismatch")
		return nil
	}

	positionX := binary.LittleEndian.Uint16(body[0:2])
	positionY := binary.LittleEndian.Uint16(body[2:4])

	// Do not hold the runtime owner while loading the persisted town target.
	// Revalidate the exact run pointer after the repository read.
	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	characterID, readyReason := currentDungeonDeathRuntimeOwner(runtime, session)
	session.dungeon.mu.Unlock()
	if readyReason != "" {
		s.logGameEvent(session, "game-dungeon-character-death-blocked",
			"body_len", len(body),
			"position_x_raw", positionX,
			"position_y_raw", positionY,
			"reason", readyReason)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	transition, err := s.prepareCurrentDungeonTownTransitionForSession(ctx, session, characterID)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-character-death-blocked",
			"char_id", characterID,
			"body_len", len(body),
			"reason", "persisted_town_transition_unavailable",
			"error", err)
		return nil
	}

	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	if session.dungeon.runtime != runtime {
		s.logGameEvent(session, "game-dungeon-character-death-blocked",
			"char_id", characterID,
			"body_len", len(body),
			"reason", "dungeon_runtime_changed_during_town_transition_load")
		return nil
	}
	if _, readyReason = currentDungeonDeathRuntimeOwner(runtime, session); readyReason != "" {
		s.logGameEvent(session, "game-dungeon-character-death-blocked",
			"char_id", characterID,
			"body_len", len(body),
			"reason", readyReason)
		return nil
	}

	if err := s.armCurrentDungeonDeathReturnLocked(session, runtime, transition); err != nil {
		s.logGameEvent(session, "game-dungeon-character-death-blocked",
			"char_id", characterID,
			"body_len", len(body),
			"reason", "death_return_timer_schedule_failed",
			"error", err)
		return nil
	}

	actorObjectKey := currentSceneActorObjectKey(characterID)
	deathBody := buildCurrentDungeonDeathStateBody(actorObjectKey, 0, 0)
	s.logGameEvent(session, "game-dungeon-character-death-op32-send",
		"char_id", characterID,
		"actor_object_key", actorObjectKey,
		"position_x_raw", positionX,
		"position_y_raw", positionY,
		"position_x_signed", int16(positionX),
		"position_y_signed", int16(positionY),
		"request_msg_id", uint16(dnfenum.CmdPacketDieCharacter),
		"response_msg_id", currentDungeonDeathStateMsgID,
		"classification", 0,
		"body_len", len(deathBody),
		"death_type", 0,
		"flag", 0,
		"timer_delay_ms", currentDungeonDeathReturnDelay.Milliseconds(),
		"body_source", "current_exe_sub_1D407C0_u16_u8_u8_reader")
	if err := s.sendGameUpperRawClass(session, currentDungeonDeathStateMsgID, deathBody, 0); err != nil {
		s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "op32_socket_write_failed")
		return err
	}
	return nil
}

func currentDungeonDeathRuntimeOwner(runtime *runtimeDungeonState, session *gameSession) (uint16, string) {
	if runtime == nil || runtime.Session == nil || session == nil {
		return 0, "active_dungeon_runtime_missing"
	}
	characterID := session.selectedCharacterID
	if characterID == 0 || !dungeonRuntimeOwnsCharacter(runtime, characterID) {
		return 0, "active_dungeon_runtime_character_owner_mismatch"
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunActive {
		return 0, "dungeon_run_not_active"
	}
	if runtime.townReturnPending {
		return 0, "town_transition_already_pending"
	}
	return characterID, ""
}

func buildCurrentDungeonDeathStateBody(actorObjectKey uint16, deathType, flag byte) []byte {
	body := make([]byte, currentDungeonDeathStateBodySize)
	binary.LittleEndian.PutUint16(body[0:2], actorObjectKey)
	body[2] = deathType
	body[3] = flag
	return body
}

func (s *Service) armCurrentDungeonDeathReturnLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	transition currentDungeonTownTransition,
) error {
	if s == nil || session == nil || runtime == nil || session.dungeon.runtime != runtime || s.gameplayTimers == nil {
		return errGameplayTimeQueueUnavailable
	}
	if runtime.lifecycleToken == 0 {
		return fmt.Errorf("%w: runtime lifecycle token is zero", errGameplayTimeQueueUnavailable)
	}

	s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "replace_or_initialize_death_timer")
	generation := runtime.deathReturnGeneration
	timerName := fmt.Sprintf("dnf-dungeon-death:%s:run:%d", session.connID, runtime.lifecycleToken)
	runtime.deathReturnWaiting = true
	runtime.deathReturnTimerName = timerName
	runtime.deathReturnDueAt = s.gameplayTimers.Now().Add(currentDungeonDeathReturnDelay)
	runtime.deathReturnTransition = cloneCurrentDungeonTownTransition(transition)
	characterID, characterGeneration, err := gameSessionCharacterEventIdentity(session, session.selectedCharacterID)
	if err != nil {
		s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "death_timer_character_identity_changed")
		return err
	}
	dungeonID := runtime.Dungeon.ID
	runToken := runtime.lifecycleToken

	err = s.gameplayTimers.ScheduleAfter(
		timerName,
		currentDungeonDeathReturnDelay,
		func(due time.Time) {
			postErr := s.postGameSessionCharacterEvent(
				session,
				"dungeon-death-return-timequeue",
				characterID,
				characterGeneration,
				func() error {
					s.fireCurrentDungeonDeathReturn(session, runtime, generation, due)
					return nil
				},
			)
			if postErr != nil && !isClosedGameSessionEventError(postErr) {
				s.logPacketEvent("game-session-event-submit-failed",
					"conn_id", session.connID,
					"source", "dungeon-death-return-timequeue",
					"char_id", characterID,
					"character_generation", characterGeneration,
					"dungeon_id", dungeonID,
					"run_token", runToken,
					"error", postErr)
			}
		},
	)
	if err != nil {
		s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "death_timer_schedule_failed")
		return err
	}
	s.logGameEvent(session, "game-dungeon-character-death-timer-scheduled",
		"char_id", runtime.Character.CharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"run_token", runtime.lifecycleToken,
		"timer_generation", generation,
		"timer_name", timerName,
		"due_at_utc", runtime.deathReturnDueAt.UTC().Format(time.RFC3339Nano),
		"delay_ms", currentDungeonDeathReturnDelay.Milliseconds(),
		"source", "timequeue_one_shot")
	return nil
}

func (s *Service) fireCurrentDungeonDeathReturn(
	session *gameSession,
	runtime *runtimeDungeonState,
	generation uint64,
	due time.Time,
) {
	if s == nil || session == nil || runtime == nil {
		return
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	if session.dungeon.runtime != runtime || !runtime.deathReturnWaiting ||
		runtime.deathReturnGeneration != generation || runtime.lifecycleToken == 0 {
		s.logGameEvent(session, "game-dungeon-character-death-timer-stale",
			"timer_generation", generation,
			"current_generation", runtime.deathReturnGeneration,
			"run_token", runtime.lifecycleToken,
			"runtime_is_current", session.dungeon.runtime == runtime,
			"waiting", runtime.deathReturnWaiting,
			"reason", "run_identity_or_generation_changed")
		return
	}
	if _, reason := currentDungeonDeathRuntimeOwner(runtime, session); reason != "" {
		s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "timer_fired_after_runtime_state_changed")
		s.logGameEvent(session, "game-dungeon-character-death-timer-stale",
			"char_id", runtime.Character.CharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"run_token", runtime.lifecycleToken,
			"timer_generation", generation,
			"reason", reason)
		return
	}

	s.logGameEvent(session, "game-dungeon-character-death-timer-fired",
		"char_id", runtime.Character.CharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"run_token", runtime.lifecycleToken,
		"timer_generation", generation,
		"scheduled_due_utc", due.UTC().Format(time.RFC3339Nano),
		"source", "timequeue_one_shot")
	if err := s.sendCurrentDungeonReturnToTownLocked(
		session,
		runtime,
		cloneCurrentDungeonTownTransition(runtime.deathReturnTransition),
		uint16(dnfenum.CmdPacketDieCharacter),
		"current_exe_op40_death_state_10s_timequeue",
	); err != nil {
		s.logGameEvent(session, "game-dungeon-character-death-return-failed",
			"char_id", runtime.Character.CharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"run_token", runtime.lifecycleToken,
			"timer_generation", generation,
			"error", err)
	}
}

func (s *Service) cancelCurrentDungeonDeathReturn(session *gameSession, source string) {
	if session == nil {
		return
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	s.cancelCurrentDungeonDeathReturnLocked(session, session.dungeon.runtime, source)
}

// cancelCurrentDungeonDeathReturnLocked invalidates callbacks before removing
// the named queue item. This also defeats a callback that was already dequeued
// but is waiting for the dungeon owner lock.
func (s *Service) cancelCurrentDungeonDeathReturnLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	source string,
) {
	if runtime == nil {
		return
	}
	wasWaiting := runtime.deathReturnWaiting
	timerName := runtime.deathReturnTimerName
	previousGeneration := runtime.deathReturnGeneration
	runtime.deathReturnGeneration = nextCurrentDungeonDeathGeneration(previousGeneration)
	runtime.deathReturnWaiting = false
	runtime.deathReturnTimerName = ""
	runtime.deathReturnDueAt = time.Time{}
	runtime.deathReturnTransition = currentDungeonTownTransition{}
	cancelled := false
	if timerName != "" && s != nil && s.gameplayTimers != nil {
		cancelled = s.gameplayTimers.Cancel(timerName)
	}
	if wasWaiting || timerName != "" {
		s.logGameEvent(session, "game-dungeon-character-death-timer-cancelled",
			"char_id", runtime.Character.CharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"run_token", runtime.lifecycleToken,
			"previous_generation", previousGeneration,
			"current_generation", runtime.deathReturnGeneration,
			"timer_name", timerName,
			"queue_item_cancelled", cancelled,
			"source", source)
	}
}

func nextCurrentDungeonDeathGeneration(current uint64) uint64 {
	next := current + 1
	if next == 0 {
		next++
	}
	return next
}

// completeCurrentDungeonReviveLocked is the cancellation boundary for a real
// future revive transaction. Callers must first commit the authoritative coin
// consumption and prove the current op41 success flag; a request alone is not
// a revive and must not cancel the death timer.
func (s *Service) completeCurrentDungeonReviveLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	source string,
) {
	if session == nil || runtime == nil || session.dungeon.runtime != runtime {
		return
	}
	s.cancelCurrentDungeonDeathReturnLocked(session, runtime, source)
}

type currentDungeonReviveCoinConsumeResult struct {
	Consumed      bool
	FreeRevive    bool
	SlotKey       string
	Slot          int16
	ItemID        int64
	CountAfter    int64
	HasItemUpdate bool
	ItemUpdate    currentItemListEntry
}

// handleDungeonUseCoin accepts current-EXE op41 (body = target actor object
// key u16) while the death-return timer is armed. It consumes one revive coin
// stack (item id 1) when available, cancels the death timer, and ACKs with
// class1/op41 success plus the extra success flag byte.
func (s *Service) handleDungeonUseCoin(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	if len(body) != currentDungeonUseCoinBodySize {
		s.logGameEvent(session, "game-dungeon-use-coin-blocked",
			"body_len", len(body),
			"expected_body_len", currentDungeonUseCoinBodySize,
			"reason", "current_exe_normal_op41_single_u16_target_boundary_mismatch")
		return nil
	}
	targetObjectKey := binary.LittleEndian.Uint16(body)

	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	characterID := session.selectedCharacterID
	ownerID, readyReason := currentDungeonDeathRuntimeOwner(runtime, session)
	waiting := runtime != nil && runtime.deathReturnWaiting
	runToken := uint64(0)
	generation := uint64(0)
	dungeonID := int64(0)
	if runtime != nil {
		runToken = runtime.lifecycleToken
		generation = runtime.deathReturnGeneration
		dungeonID = int64(runtime.Dungeon.ID)
	}
	expectedObjectKey := currentSceneActorObjectKey(characterID)
	session.dungeon.mu.Unlock()

	if readyReason != "" || !waiting {
		reason := readyReason
		if reason == "" {
			reason = "death_timer_not_waiting"
		}
		s.logGameEvent(session, "game-dungeon-use-coin-blocked",
			"char_id", characterID,
			"target_object_key", targetObjectKey,
			"expected_object_key", expectedObjectKey,
			"run_token", runToken,
			"death_timer_waiting", waiting,
			"reason", reason)
		return nil
	}
	if targetObjectKey != expectedObjectKey || targetObjectKey != currentSceneActorObjectKey(ownerID) {
		s.logGameEvent(session, "game-dungeon-use-coin-blocked",
			"char_id", characterID,
			"target_object_key", targetObjectKey,
			"expected_object_key", expectedObjectKey,
			"run_token", runToken,
			"reason", "target_object_key_mismatch")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()

	session.dungeon.mu.Lock()
	if session.dungeon.runtime != runtime || runtime == nil || !runtime.deathReturnWaiting ||
		runtime.deathReturnGeneration != generation || runtime.lifecycleToken != runToken {
		session.dungeon.mu.Unlock()
		s.logGameEvent(session, "game-dungeon-use-coin-blocked",
			"char_id", characterID,
			"target_object_key", targetObjectKey,
			"run_token", runToken,
			"timer_generation", generation,
			"reason", "death_timer_state_changed_before_commit")
		return nil
	}
	if _, reason := currentDungeonDeathRuntimeOwner(runtime, session); reason != "" {
		session.dungeon.mu.Unlock()
		s.logGameEvent(session, "game-dungeon-use-coin-blocked",
			"char_id", characterID,
			"target_object_key", targetObjectKey,
			"run_token", runToken,
			"reason", reason)
		return nil
	}

	consume, consumeErr := s.consumeCurrentDungeonReviveCoin(ctx, characterID)
	if consumeErr != nil {
		session.dungeon.mu.Unlock()
		s.logGameEvent(session, "game-dungeon-use-coin-blocked",
			"char_id", characterID,
			"target_object_key", targetObjectKey,
			"run_token", runToken,
			"reason", "revive_coin_asset_transaction_failed",
			"error", consumeErr)
		_ = s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseCoin), 1)
		return nil
	}
	if !consume.Consumed && !consume.FreeRevive {
		session.dungeon.mu.Unlock()
		s.logGameEvent(session, "game-dungeon-use-coin-blocked",
			"char_id", characterID,
			"target_object_key", targetObjectKey,
			"run_token", runToken,
			"reason", "revive_coin_insufficient")
		_ = s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseCoin), 1)
		return nil
	}

	source := "current_exe_op41_revive_coin_consumed"
	if consume.FreeRevive {
		source = "current_exe_op41_private_server_free_revive"
	}
	s.completeCurrentDungeonReviveLocked(session, runtime, source)
	session.dungeon.mu.Unlock()

	if err := s.sendGameUpperSuccess(
		session,
		uint16(dnfenum.CmdPacketUseCoin),
		[]byte{currentDungeonUseCoinSuccessFlag},
	); err != nil {
		s.logGameEvent(session, "game-dungeon-use-coin-ack-failed",
			"char_id", characterID,
			"target_object_key", targetObjectKey,
			"run_token", runToken,
			"error", err)
		return err
	}

	if consume.HasItemUpdate {
		updateBody := buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, []currentItemListEntry{consume.ItemUpdate})
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), updateBody, 0); err != nil {
			s.logGameEvent(session, "game-dungeon-use-coin-inventory-update-failed",
				"char_id", characterID,
				"slot_key", consume.SlotKey,
				"count_after", consume.CountAfter,
				"error", err)
			return err
		}
	}

	s.logGameEvent(session, "game-dungeon-use-coin-success",
		"char_id", characterID,
		"target_object_key", targetObjectKey,
		"dungeon_id", dungeonID,
		"run_token", runToken,
		"timer_generation", generation,
		"coin_consumed", consume.Consumed,
		"free_revive", consume.FreeRevive,
		"slot_key", consume.SlotKey,
		"item_id", consume.ItemID,
		"count_after", consume.CountAfter,
		"inventory_update_sent", consume.HasItemUpdate,
		"success_flag", currentDungeonUseCoinSuccessFlag,
		"response_sent", true,
		"timer_cancelled", true,
		"source", source)
	return nil
}

func (s *Service) consumeCurrentDungeonReviveCoin(
	ctx context.Context,
	characterID uint16,
) (currentDungeonReviveCoinConsumeResult, error) {
	result := currentDungeonReviveCoinConsumeResult{}
	if characterID == 0 {
		if currentDungeonUseCoinAllowFreeRevive {
			result.FreeRevive = true
			return result, nil
		}
		return result, nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.CharacterItems == nil {
		if currentDungeonUseCoinAllowFreeRevive {
			result.FreeRevive = true
			return result, nil
		}
		return result, errors.New("character item transaction unavailable")
	}
	characterKey := strconv.FormatUint(uint64(characterID), 10)
	owner, err := dnfdungeon.NewOwner(repositories)
	if err != nil {
		return result, err
	}
	var update currentItemListEntry
	consume, err := owner.ConsumeReviveCoin(ctx, dnfdungeon.ReviveCoinCommand{
		CharacterID: characterKey,
		ItemID:      int64(csharpReviveCoinItemID),
		WalletSlot:  int16(csharpReviveCoinWalletSlot),
		AllowFree:   currentDungeonUseCoinAllowFreeRevive,
		UpdatedAt:   time.Now().UTC(),
		Project: func(slot int16, stack dnfrepo.ItemStack) (dnfrepo.ItemStack, error) {
			update = currentItemListEntryFromStack(dnfrepo.MainInventoryListType, slot, stack)
			stack.RawEntry = append([]byte(nil), update.data[:]...)
			return stack, nil
		},
	})
	if err != nil {
		return result, err
	}
	result = currentDungeonReviveCoinConsumeResult{
		Consumed:      consume.Consumed,
		FreeRevive:    consume.FreeRevive,
		SlotKey:       consume.SlotKey,
		Slot:          consume.Slot,
		ItemID:        consume.ItemID,
		CountAfter:    consume.CountAfter,
		HasItemUpdate: consume.Consumed,
	}
	if consume.Removed {
		update.patchCore(consume.Slot, math.MaxUint32, 0)
	}
	result.ItemUpdate = update
	return result, nil
}
