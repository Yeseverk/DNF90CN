package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfonlineevent "longheng.io/server/internal/modules/dnf/onlineevent"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentSpendTimeStageCount   = 4
	currentSpendTimeObserveTick  = 30 * time.Second
	currentSpendTimeResetHour    = 6
	currentSpendTimeWriteTimeout = 10 * time.Second
)

// currentSpendTimeRuntimeCatalog is the current-EXE bridge projection of the
// raw, protocol-independent attendance PVF catalog. Event 2347 is the only
// activated descriptor: its four ordinary rewards use cumulative thresholds
// 1800/3600/7200/10800 seconds. The separate sum-reward table remains unused.
type currentSpendTimeRuntimeCatalog struct {
	definition        dnfonlineevent.Definition
	rewardItemIDs     []uint32
	totalStageSeconds uint32
}

// currentSpendTimeClockState owns one selected-character observation cursor.
// Its timer callback is routed through the per-session event loop and also
// carries characterGeneration, so a stale callback cannot credit a replacement
// character or write to its socket.
type currentSpendTimeClockState struct {
	mu                             sync.Mutex
	eventInfoSent                  bool
	characterID                    uint16
	characterGeneration            uint64
	initializedCharacterGeneration uint64
	accountID                      string
	anchor                         time.Time
	generation                     uint64
	timerName                      string
	onlineSeconds                  uint64
	completedStages                uint32
	catalog                        *currentSpendTimeRuntimeCatalog
}

type currentSpendTimeSettlementResult struct {
	changed     bool
	rewardItems []dnfonlineevent.CommittedItem
	claimErr    error
}

func buildCurrentSpendTimeRuntimeCatalog(catalog *dnfonlineevent.AttendancePVFCatalog) (*currentSpendTimeRuntimeCatalog, error) {
	if catalog == nil {
		return nil, fmt.Errorf("current spend-time attendance catalog is nil")
	}
	snapshot := catalog.Snapshot()
	if len(snapshot.ProcessDurationsSeconds) != currentSpendTimeStageCount ||
		len(snapshot.RewardItems) != currentSpendTimeStageCount {
		return nil, fmt.Errorf(
			"current spend-time PVF shape durations=%d rewards=%d want=%d/%d",
			len(snapshot.ProcessDurationsSeconds),
			len(snapshot.RewardItems),
			currentSpendTimeStageCount,
			currentSpendTimeStageCount,
		)
	}

	definition := dnfonlineevent.Definition{
		ID:     strconv.FormatUint(uint64(currentSpendTimeEventID), 10),
		Stages: make([]dnfonlineevent.Stage, 0, currentSpendTimeStageCount),
	}
	rewardItemIDs := make([]uint32, 0, currentSpendTimeStageCount)
	var cumulative uint64
	for index := 0; index < currentSpendTimeStageCount; index++ {
		duration := snapshot.ProcessDurationsSeconds[index]
		reward := snapshot.RewardItems[index]
		if duration <= 0 || reward.ItemID <= 0 || reward.ItemID > math.MaxUint32 || reward.Count <= 0 {
			return nil, fmt.Errorf(
				"current spend-time PVF stage=%d duration=%d item=%d count=%d",
				index,
				duration,
				reward.ItemID,
				reward.Count,
			)
		}
		cumulative += uint64(duration)
		if cumulative > math.MaxUint32 {
			return nil, fmt.Errorf("current spend-time cumulative seconds=%d overflows u32", cumulative)
		}
		definition.Stages = append(definition.Stages, dnfonlineevent.Stage{
			ID:              fmt.Sprintf("stage-%d", index+1),
			RequiredSeconds: cumulative,
			Items: []dnfonlineevent.ItemReward{{
				ItemID: reward.ItemID,
				Count:  reward.Count,
			}},
		})
		rewardItemIDs = append(rewardItemIDs, uint32(reward.ItemID))
	}
	if err := definition.Validate(); err != nil {
		return nil, fmt.Errorf("validate current spend-time definition: %w", err)
	}
	return &currentSpendTimeRuntimeCatalog{
		definition:        definition,
		rewardItemIDs:     append([]uint32(nil), rewardItemIDs...),
		totalStageSeconds: uint32(cumulative),
	}, nil
}

// currentSpendTimeRuntimeCatalogIfAvailable lazily projects the same archive
// already loaded by the startup PVF stages. Hand-assembled unit-test services
// without an archive keep the feature absent unless the test injects a proved
// projection; production Start always establishes the archive first.
func (s *Service) currentSpendTimeRuntimeCatalogIfAvailable(ctx context.Context) (*currentSpendTimeRuntimeCatalog, bool, error) {
	if s == nil {
		return nil, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}

	s.spendTimeCatalogMu.Lock()
	defer s.spendTimeCatalogMu.Unlock()
	if s.spendTimeCatalogState != nil {
		return s.spendTimeCatalogState, true, nil
	}
	if s.spendTimeCatalogLoadErr != nil {
		return nil, true, s.spendTimeCatalogLoadErr
	}

	s.initialEquipmentMu.Lock()
	archive := s.initialEquipmentArchive
	s.initialEquipmentMu.Unlock()
	if archive == nil {
		return nil, false, nil
	}
	rawCatalog, err := dnfonlineevent.LoadAttendancePVFCatalog(ctx, archive)
	if err == nil {
		s.spendTimeCatalogState, err = buildCurrentSpendTimeRuntimeCatalog(rawCatalog)
	}
	s.spendTimeCatalogLoadErr = err
	if err != nil {
		return nil, true, err
	}
	s.logPacketEvent("dnf-spend-time-catalog-loaded",
		"event_id", currentSpendTimeEventID,
		"reward_count", len(s.spendTimeCatalogState.rewardItemIDs),
		"total_stage_seconds", s.spendTimeCatalogState.totalStageSeconds,
		"source", dnfonlineevent.AttendanceEventPVFPath)
	return s.spendTimeCatalogState, true, nil
}

func (s *Service) currentSpendTimeOwner() (*dnfonlineevent.Owner, error) {
	repositories, ok := s.repositoryGroup()
	if !ok {
		return nil, dnfonlineevent.ErrOwnerUnavailable
	}
	return dnfonlineevent.NewOwner(repositories)
}

// sendCurrentSpendTimeInitialStateOnce publishes the proved activity
// descriptor immediately followed by authoritative progress, then starts the
// server-owned clock. The 2347 native widget consumes this pair as one
// initialization transaction: no unrelated class-0 packet may appear between
// op108 and op1206. Inventory rows produced by automatic threshold awards are
// emitted only after the op108 -> op1206 pair and the separate joust snapshot.
func (s *Service) sendCurrentSpendTimeInitialStateOnce(
	session *gameSession,
	source string,
) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	characterID := session.selectedCharacterID
	characterGeneration := session.characterGeneration

	session.spendTime.mu.Lock()
	alreadyInitialized := session.spendTime.characterID == characterID &&
		session.spendTime.initializedCharacterGeneration == characterGeneration &&
		session.spendTime.catalog != nil && !session.spendTime.anchor.IsZero()
	session.spendTime.mu.Unlock()
	if alreadyInitialized {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), currentSpendTimeWriteTimeout)
	defer cancel()
	catalog, available, err := s.currentSpendTimeRuntimeCatalogIfAvailable(ctx)
	if err != nil {
		return fmt.Errorf("load current spend-time catalog: %w", err)
	}
	if !available || catalog == nil {
		s.logGameEvent(session, "game-spend-time-initialization-deferred",
			"source", source,
			"char_id", characterID,
			"reason", "runtime_pvf_archive_not_loaded")
		return nil
	}
	owner, err := s.currentSpendTimeOwner()
	if err != nil {
		return fmt.Errorf("create current spend-time owner: %w", err)
	}
	now := s.gameplayNow()
	accountID := s.accountIDForSession(session)
	characterIDText := strconv.Itoa(int(characterID))

	session.spendTime.mu.Lock()
	defer session.spendTime.mu.Unlock()
	if session.spendTime.characterID == characterID &&
		session.spendTime.initializedCharacterGeneration == characterGeneration &&
		session.spendTime.catalog != nil && !session.spendTime.anchor.IsZero() {
		return nil
	}
	if session.spendTime.characterID != 0 && session.spendTime.characterID != characterID {
		return fmt.Errorf("current spend-time clock still owns character %d", session.spendTime.characterID)
	}
	snapshot, err := owner.Status(ctx, dnfonlineevent.StatusCommand{
		AccountID:   accountID,
		CharacterID: characterIDText,
		Definition:  catalog.definition,
		ObservedAt:  now,
	})
	if err != nil {
		return fmt.Errorf("load current spend-time status: %w", err)
	}
	snapshot, rewardItems, claimErr := s.claimCurrentSpendTimeUnlockedStages(
		ctx,
		owner,
		accountID,
		characterIDText,
		catalog,
		snapshot,
		now,
	)
	if claimErr != nil {
		s.logGameEvent(session, "game-spend-time-initial-auto-award-deferred",
			"source", source,
			"char_id", characterID,
			"online_seconds", snapshot.OnlineSeconds,
			"error", claimErr)
	}
	completedStages := currentSpendTimeClaimedStages(catalog.definition, snapshot)
	sendDescriptor, descriptorReady := s.beginCurrentSpendTimeEventInfo(session)
	if !descriptorReady {
		return fmt.Errorf("current spend-time first descriptor is being sent by another session for client pid=%d", session.clientPID)
	}
	descriptorSent, sendErr := s.sendCurrentSpendTimeProtocolState(
		session,
		catalog,
		snapshot.OnlineSeconds,
		completedStages,
		sendDescriptor,
	)
	if sendDescriptor {
		// Commit immediately after the op108 write, even when the following
		// op1206 progress or a later joust snapshot write failed.
		// Retrying the first-process base-catalog grammar would then be invalid.
		s.finishCurrentSpendTimeEventInfo(session, descriptorSent)
	}
	if sendErr != nil {
		return sendErr
	}
	if !sendDescriptor {
		s.logGameEvent(session, "game-spend-time-descriptor-skipped",
			"source", source,
			"char_id", characterID,
			"client_pid", session.clientPID,
			"already_sent", session.spendTime.eventInfoSent,
			"reason", "client_pid_registry_owns_process_lifetime_descriptor_progress_only_after_first_op108")
	}
	if err := s.sendCurrentSpendTimeRewardItemUpdates(session, rewardItems, source+"_initial_auto_award"); err != nil {
		return err
	}

	session.spendTime.characterID = characterID
	session.spendTime.characterGeneration = characterGeneration
	session.spendTime.initializedCharacterGeneration = characterGeneration
	session.spendTime.accountID = accountID
	// Keep the exact observation start. onlineevent.Owner conservatively
	// ceil(from)/floor(to); truncating here would manufacture up to one second.
	session.spendTime.anchor = now
	session.spendTime.generation++
	session.spendTime.onlineSeconds = snapshot.OnlineSeconds
	session.spendTime.completedStages = completedStages
	session.spendTime.catalog = catalog
	if err := s.armCurrentSpendTimeTickLocked(session); err != nil {
		session.spendTime.characterID = 0
		session.spendTime.characterGeneration = 0
		session.spendTime.initializedCharacterGeneration = 0
		session.spendTime.accountID = ""
		session.spendTime.anchor = time.Time{}
		session.spendTime.timerName = ""
		session.spendTime.onlineSeconds = 0
		session.spendTime.completedStages = 0
		session.spendTime.catalog = nil
		return err
	}
	s.logGameEvent(session, "game-spend-time-initial-state-sent",
		"source", source,
		"char_id", characterID,
		"event_id", currentSpendTimeEventID,
		"online_seconds", snapshot.OnlineSeconds,
		"completed_stages", completedStages,
		"reward_item_updates", len(rewardItems),
		"sequence", "class0_op108_descriptor_then_class0_op1206_progress_then_class0_op1240_joust_opening_state_then_class0_op1241_joust_roster_then_class0_op1242_joust_pool_then_atomic_reward_op14_before_typed_op24")
	return nil
}

func (s *Service) sendCurrentSpendTimeProtocolState(
	session *gameSession,
	catalog *currentSpendTimeRuntimeCatalog,
	onlineSeconds uint64,
	completedStages uint32,
	includeFirstProcessDescriptor bool,
) (bool, error) {
	if s == nil || session == nil || catalog == nil {
		return false, nil
	}
	var eventInfoTransport []byte
	if includeFirstProcessDescriptor {
		eventInfoBody, err := buildCurrentSpendTimeEventInfoBody(
			catalog.rewardItemIDs,
			catalog.totalStageSeconds,
		)
		if err != nil {
			return false, err
		}
		eventInfoTransport, err = zlibCompress(eventInfoBody)
		if err != nil {
			return false, fmt.Errorf("compress current spend-time op108: %w", err)
		}
	}
	progressBody, err := buildCurrentSpendTimeProgressBody(onlineSeconds, completedStages)
	if err != nil {
		return false, err
	}
	descriptorSent := false
	if includeFirstProcessDescriptor {
		if err := s.sendCurrentProtectedClass0Packet(
			session,
			currentSpendTimeEventInfoMsgID,
			eventInfoTransport,
			currentSpendTimeEventInfoCodec,
			"current_op108_spend_time_catalog",
		); err != nil {
			return false, err
		}
		descriptorSent = true
	}
	// Keep the native cumulative-online-reward initialization atomic. The current
	// client binds the 2347 HUD/hover controls while consuming op1206; injecting
	// the unrelated joust bootstrap between op108 and op1206 leaves the progress
	// bar present but its ItemReward hover page unbound.
	if err := s.sendGameUpperRawClass(session, currentSpendTimeProgressMsgID, progressBody, 0); err != nil {
		return descriptorSent, err
	}
	// Current NoPack creates the joust owners while consuming op108. Its owner
	// 609 open gate then reads the state set by the exact three-byte class-0
	// op1240 handler before it can use the class-0 op1241 roster and op1242
	// support pool. Repeat the opening snapshot for
	// later character selections in the same client process so a failed or
	// reconnected session can recover even though op108 is process-lifetime only.
	if err := s.sendCurrentJoustOpeningState(session); err != nil {
		return descriptorSent, err
	}
	if err := s.sendCurrentJoustOpeningRoster(session); err != nil {
		return descriptorSent, err
	}
	return descriptorSent, nil
}

func currentSpendTimeReachedStages(definition dnfonlineevent.Definition, onlineSeconds uint64) uint32 {
	var completed uint32
	for _, stage := range definition.Stages {
		if onlineSeconds < stage.RequiredSeconds {
			break
		}
		completed++
	}
	return completed
}

func currentSpendTimeClaimedStages(definition dnfonlineevent.Definition, snapshot dnfonlineevent.Snapshot) uint32 {
	claimed := make(map[string]struct{}, len(snapshot.ClaimedStageIDs))
	for _, stageID := range snapshot.ClaimedStageIDs {
		claimed[stageID] = struct{}{}
	}
	var completed uint32
	for _, stage := range definition.Stages {
		if _, ok := claimed[stage.ID]; !ok {
			break
		}
		completed++
	}
	return completed
}

func (s *Service) claimCurrentSpendTimeUnlockedStages(
	ctx context.Context,
	owner *dnfonlineevent.Owner,
	accountID string,
	characterID string,
	catalog *currentSpendTimeRuntimeCatalog,
	snapshot dnfonlineevent.Snapshot,
	claimedAt time.Time,
) (dnfonlineevent.Snapshot, []dnfonlineevent.CommittedItem, error) {
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	if s == nil || owner == nil || accountID == "" || characterID == "" || catalog == nil {
		return snapshot, nil, dnfonlineevent.ErrOwnerUnavailable
	}
	claimed := make(map[string]struct{}, len(snapshot.ClaimedStageIDs))
	for _, stageID := range snapshot.ClaimedStageIDs {
		claimed[stageID] = struct{}{}
	}
	itemCatalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return snapshot, nil, err
	}
	items := make([]dnfonlineevent.CommittedItem, 0, currentSpendTimeStageCount)
	for _, stage := range catalog.definition.Stages {
		if snapshot.OnlineSeconds < stage.RequiredSeconds {
			break
		}
		if _, alreadyClaimed := claimed[stage.ID]; alreadyClaimed {
			continue
		}
		claim, claimErr := owner.Claim(ctx, dnfonlineevent.ClaimCommand{
			AccountID:   accountID,
			CharacterID: characterID,
			Definition:  catalog.definition,
			StageID:     stage.ID,
			ClaimedAt:   claimedAt,
			Allocate:    currentSpendTimeItemAllocator(itemCatalog, claimedAt, stage.ID),
		})
		if claimErr != nil {
			return snapshot, items, claimErr
		}
		snapshot = claim.PostSnapshot
		claimed[stage.ID] = struct{}{}
		items = append(items, currentSpendTimeClaimItemsForCharacter(claim, characterID)...)
	}
	return snapshot, items, nil
}

func currentSpendTimeClaimItemsForCharacter(
	claim dnfonlineevent.ClaimResult,
	characterID string,
) []dnfonlineevent.CommittedItem {
	if claim.Replayed || claim.CharacterID != characterID {
		return nil
	}
	items := make([]dnfonlineevent.CommittedItem, len(claim.Items))
	for index, item := range claim.Items {
		items[index] = item
		items[index].RawEntry = append([]byte(nil), item.RawEntry...)
	}
	return items
}

func currentSpendTimeItemAllocator(
	catalog *pvfDungeonDropCatalog,
	grantedAt time.Time,
	stageID string,
) dnfonlineevent.ItemAllocator {
	return func(record *dnfrepo.InventoryRecord, reward dnfonlineevent.ItemReward) (dnfonlineevent.CommittedItem, error) {
		if catalog == nil || record == nil || reward.ItemID <= 0 || reward.ItemID > math.MaxUint32 ||
			reward.Count <= 0 || reward.Count > math.MaxUint32 {
			return dnfonlineevent.CommittedItem{}, errDungeonPickupItemInvalid
		}
		definition, err := catalog.ResolveItem(uint32(reward.ItemID))
		if err != nil {
			return dnfonlineevent.CommittedItem{}, err
		}
		definition, err = currentPVFItemDefinitionForGrantAt(definition, grantedAt.UTC())
		if err != nil {
			return dnfonlineevent.CommittedItem{}, err
		}
		slot, err := addCurrentDungeonPickupToInventory(record, definition, uint32(reward.Count))
		if err != nil {
			return dnfonlineevent.CommittedItem{}, err
		}
		key := currentDungeonPickupMainSlotKey(int16(slot))
		stack, found := record.Slots[key]
		if !found || stack.ItemID != reward.ItemID || stack.Count < reward.Count {
			return dnfonlineevent.CommittedItem{}, errDungeonPickupItemInvalid
		}
		entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, int16(slot), stack)
		stack.RawEntry = append([]byte(nil), entry.data[:]...)
		if stack.Extra == nil {
			stack.Extra = make(map[string]string, 5)
		}
		if strings.TrimSpace(stack.Extra["source"]) == "" {
			stack.Extra["source"] = "online_event_spend_time"
		}
		stack.Extra["last_grant_source"] = "online_event_spend_time"
		stack.Extra["last_grant_event_id"] = strconv.FormatUint(uint64(currentSpendTimeEventID), 10)
		stack.Extra["last_grant_stage_id"] = stageID
		record.Slots[key] = stack
		return dnfonlineevent.CommittedItem{
			SlotKey: key, SlotIndex: slot, ItemID: reward.ItemID,
			Delta: reward.Count, PostCount: stack.Count,
			RawEntry: append([]byte(nil), entry.data[:]...),
		}, nil
	}
}

func (s *Service) sendCurrentSpendTimeRewardItemUpdates(
	session *gameSession,
	items []dnfonlineevent.CommittedItem,
	source string,
) error {
	if len(items) == 0 {
		return nil
	}
	entries := make([]currentItemListEntry, 0, len(items))
	for _, item := range items {
		if item.ItemID <= 0 || item.ItemID > math.MaxUint32 || item.PostCount < item.Delta ||
			len(item.RawEntry) != currentItemListEntryWireSize ||
			binary.LittleEndian.Uint16(item.RawEntry[:2]) != item.SlotIndex ||
			binary.LittleEndian.Uint32(item.RawEntry[2:6]) != uint32(item.ItemID) {
			return fmt.Errorf("current spend-time committed item has invalid wire receipt: %+v", item)
		}
		var entry currentItemListEntry
		copy(entry.data[:], item.RawEntry)
		entries = append(entries, entry)
	}
	body := buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, entries)
	s.logGameEvent(session, "game-spend-time-reward-item-update-send",
		"source", source,
		"event_id", currentSpendTimeEventID,
		"entry_count", len(entries),
		"body_len", len(body),
		"body_source", "online_event_atomic_claim_receipts_op14_raw77")
	return s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketWalkoutPartyMember),
		body,
		0,
	)
}

func (s *Service) armCurrentSpendTimeTickLocked(session *gameSession) error {
	if s == nil || session == nil || session.spendTime.characterID == 0 || session.spendTime.catalog == nil {
		return nil
	}
	queue := s.ensureGameplayTimeQueue()
	if queue == nil {
		return errGameplayTimeQueueUnavailable
	}
	characterID, characterGeneration, err := gameSessionCharacterEventIdentity(session, session.spendTime.characterID)
	if err != nil {
		return err
	}
	timerName := fmt.Sprintf("dnf-spend-time:%s:%d", session.connID, characterID)
	generation := session.spendTime.generation
	session.spendTime.timerName = timerName
	return queue.ScheduleAfter(timerName, currentSpendTimeObserveTick, func(time.Time) {
		err := s.postGameSessionCharacterEvent(
			session,
			"spend-time-observation-tick",
			characterID,
			characterGeneration,
			func() error {
				s.runCurrentSpendTimeTick(session, generation)
				return nil
			},
		)
		if err != nil && !isClosedGameSessionEventError(err) {
			s.logPacketEvent("game-session-event-submit-failed",
				"conn_id", session.connID,
				"source", "spend-time-observation-tick",
				"char_id", characterID,
				"character_generation", characterGeneration,
				"error", err)
		}
	})
}

func (s *Service) runCurrentSpendTimeTick(session *gameSession, generation uint64) {
	if s == nil || session == nil {
		return
	}
	now := s.gameplayNow()
	session.spendTime.mu.Lock()
	defer session.spendTime.mu.Unlock()
	if session.spendTime.characterID == 0 || session.spendTime.catalog == nil ||
		session.spendTime.generation != generation {
		return
	}
	settlement, err := s.settleCurrentSpendTimeClockLocked(session, now)
	if err != nil {
		s.logGameEvent(session, "game-spend-time-observation-deferred",
			"char_id", session.spendTime.characterID,
			"generation", generation,
			"error", err)
	} else if settlement.changed {
		body, buildErr := buildCurrentSpendTimeProgressBody(
			session.spendTime.onlineSeconds,
			session.spendTime.completedStages,
		)
		if buildErr != nil {
			s.logGameEvent(session, "game-spend-time-progress-build-failed", "error", buildErr)
		} else if sendErr := s.sendGameUpperRawClass(session, currentSpendTimeProgressMsgID, body, 0); sendErr != nil {
			s.logGameEvent(session, "game-spend-time-progress-send-failed", "error", sendErr)
		}
		if sendErr := s.sendCurrentSpendTimeRewardItemUpdates(
			session,
			settlement.rewardItems,
			"online_observation_tick",
		); sendErr != nil {
			s.logGameEvent(session, "game-spend-time-reward-item-update-failed", "error", sendErr)
		}
	}
	if settlement.claimErr != nil {
		s.logGameEvent(session, "game-spend-time-auto-award-deferred",
			"char_id", session.spendTime.characterID,
			"online_seconds", session.spendTime.onlineSeconds,
			"error", settlement.claimErr)
	}
	if err := s.armCurrentSpendTimeTickLocked(session); err != nil {
		s.logGameEvent(session, "game-spend-time-observation-rearm-deferred",
			"char_id", session.spendTime.characterID,
			"generation", generation,
			"error", err)
	}
}

func (s *Service) settleCurrentSpendTimeClockLocked(
	session *gameSession,
	now time.Time,
) (currentSpendTimeSettlementResult, error) {
	if session == nil || session.spendTime.characterID == 0 || session.spendTime.catalog == nil ||
		session.spendTime.anchor.IsZero() {
		return currentSpendTimeSettlementResult{}, nil
	}
	intervalTo := now.Truncate(time.Second)
	if !session.spendTime.anchor.Before(intervalTo) {
		return currentSpendTimeSettlementResult{}, nil
	}
	owner, err := s.currentSpendTimeOwner()
	if err != nil {
		return currentSpendTimeSettlementResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), currentSpendTimeWriteTimeout)
	defer cancel()
	definition := session.spendTime.catalog.definition
	characterID := strconv.Itoa(int(session.spendTime.characterID))
	cursor := session.spendTime.anchor
	previousCompleted := session.spendTime.completedStages
	changed := false
	rewardItems := make([]dnfonlineevent.CommittedItem, 0, currentSpendTimeStageCount)
	var snapshot dnfonlineevent.Snapshot
	var claimErr error
	for cursor.Before(intervalTo) {
		nextBoundary := currentSpendTimeNextServiceBoundary(definition, cursor)
		segmentEnd := intervalTo
		endsAtBoundary := false
		if !nextBoundary.After(segmentEnd) {
			segmentEnd = nextBoundary
			endsAtBoundary = true
		}
		result, err := owner.Observe(ctx, dnfonlineevent.ObserveCommand{
			AccountID:    session.spendTime.accountID,
			CharacterID:  characterID,
			Definition:   definition,
			IntervalFrom: cursor,
			IntervalTo:   segmentEnd,
		})
		if err != nil {
			return currentSpendTimeSettlementResult{}, err
		}
		changed = changed || result.Changed
		claimAt := segmentEnd
		if endsAtBoundary {
			// Claim the just-finished service day before a following Observe or
			// Status resets its daily ledger at exactly 06:00.
			claimAt = segmentEnd.Add(-time.Nanosecond)
		}
		var segmentItems []dnfonlineevent.CommittedItem
		snapshot, segmentItems, claimErr = s.claimCurrentSpendTimeUnlockedStages(
			ctx,
			owner,
			session.spendTime.accountID,
			characterID,
			session.spendTime.catalog,
			result.Snapshot,
			claimAt,
		)
		rewardItems = append(rewardItems, segmentItems...)
		session.spendTime.onlineSeconds = snapshot.OnlineSeconds
		session.spendTime.completedStages = currentSpendTimeClaimedStages(definition, snapshot)
		if claimErr != nil {
			// Do not cross the reset boundary until the old day's unlocked
			// rewards are durably settled. Re-observing the retained interval is
			// idempotent because the owner stores an interval union.
			if !endsAtBoundary {
				session.spendTime.anchor = segmentEnd
			}
			return currentSpendTimeSettlementResult{
				changed:     changed || previousCompleted != session.spendTime.completedStages || len(rewardItems) != 0,
				rewardItems: rewardItems,
				claimErr:    claimErr,
			}, nil
		}
		session.spendTime.anchor = segmentEnd
		cursor = segmentEnd

		if endsAtBoundary && !cursor.Before(intervalTo) {
			priorDate := snapshot.CalendarDate
			snapshot, err = owner.Status(ctx, dnfonlineevent.StatusCommand{
				AccountID:   session.spendTime.accountID,
				CharacterID: characterID,
				Definition:  definition,
				ObservedAt:  segmentEnd,
			})
			if err != nil {
				return currentSpendTimeSettlementResult{}, err
			}
			changed = changed || snapshot.CalendarDate != priorDate
			session.spendTime.onlineSeconds = snapshot.OnlineSeconds
			session.spendTime.completedStages = currentSpendTimeClaimedStages(definition, snapshot)
		}
	}
	return currentSpendTimeSettlementResult{
		changed:     changed || previousCompleted != session.spendTime.completedStages || len(rewardItems) != 0,
		rewardItems: rewardItems,
		claimErr:    claimErr,
	}, nil
}

func currentSpendTimeNextServiceBoundary(
	definition dnfonlineevent.Definition,
	at time.Time,
) time.Time {
	location := definition.Calendar
	if location == nil {
		location = dnfonlineevent.ChinaCalendar
	}
	boundary := dnfonlineevent.DailyBoundary{Hour: currentSpendTimeResetHour}
	if definition.Boundary != nil {
		boundary = *definition.Boundary
	}
	local := at.In(location)
	next := time.Date(
		local.Year(),
		local.Month(),
		local.Day(),
		boundary.Hour,
		boundary.Minute,
		boundary.Second,
		0,
		location,
	)
	if !at.Before(next) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (s *Service) stopCurrentSpendTimeClock(session *gameSession, source string) {
	if s == nil || session == nil {
		return
	}
	session.spendTime.mu.Lock()
	if session.spendTime.characterID == 0 || session.spendTime.catalog == nil {
		session.spendTime.mu.Unlock()
		return
	}
	settlement, settleErr := s.settleCurrentSpendTimeClockLocked(session, s.gameplayNow())
	characterID := session.spendTime.characterID
	timerName := session.spendTime.timerName
	session.spendTime.generation++
	session.spendTime.characterID = 0
	session.spendTime.characterGeneration = 0
	session.spendTime.initializedCharacterGeneration = 0
	session.spendTime.accountID = ""
	session.spendTime.anchor = time.Time{}
	session.spendTime.timerName = ""
	session.spendTime.onlineSeconds = 0
	session.spendTime.completedStages = 0
	session.spendTime.catalog = nil
	session.spendTime.mu.Unlock()
	if timerName != "" {
		if queue := s.ensureGameplayTimeQueue(); queue != nil {
			queue.Cancel(timerName)
		}
	}
	if settleErr != nil {
		s.logGameEvent(session, "game-spend-time-stop-settlement-failed",
			"char_id", characterID,
			"source", source,
			"error", settleErr)
	}
	if settlement.claimErr != nil {
		s.logGameEvent(session, "game-spend-time-stop-auto-award-deferred",
			"char_id", characterID,
			"source", source,
			"error", settlement.claimErr)
	}
}
