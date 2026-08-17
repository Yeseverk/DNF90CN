package quest

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable   = errors.New("quest owner unavailable")
	ErrCharacterRequired  = errors.New("selected character id required")
	ErrQuestIDRequired    = errors.New("quest id required")
	ErrCharacterNotFound  = errors.New("character record not found")
	ErrQuestPersistVerify = errors.New("persisted quest state verification failed")
	ErrQuestNotActive     = errors.New("quest is not active")
	ErrQuestCannotGiveUp  = errors.New("quest cannot be given up")
	ErrGiveUpNeedsAssets  = errors.New("quest give-up requires an inventory transaction")
)

type Owner struct {
	repositories dnfrepo.Group
	characters   dnfrepo.CharacterRepository
	quests       dnfrepo.QuestRepository
	inventory    dnfrepo.InventoryRepository
}

type PlanResult struct {
	AccountID   string
	CharacterID string
	Operation   string
	QuestID     int64
	Known       bool
	// StateChanged is true only after ApplySetTrigger has durably updated the
	// active quest state. A valid replay at the already-persisted state is not
	// a new client-success boundary.
	StateChanged      bool
	Status            string
	TriggerType       byte
	ProgressValue     int64
	RewardSelectIndex int64
	HasRewardSelect   bool
	Multiplier        int64
}

type AcceptResult struct {
	AccountID   string
	CharacterID string
	QuestID     uint16
	InitTrigger uint32
	Idempotent  bool
	PVFPath     string
	QuestType   string
}

type GiveUpResult struct {
	AccountID   string
	CharacterID string
	QuestID     uint16
}

func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Character == nil || repos.Quest == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{repositories: repos, characters: repos.Character, quests: repos.Quest, inventory: repos.Inventory}, nil
}

func (o *Owner) Plan(ctx context.Context, cmd Command) (PlanResult, error) {
	if o == nil || o.characters == nil || o.quests == nil {
		return PlanResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return PlanResult{}, ErrCharacterRequired
	}
	if cmd.QuestID == 0 {
		return PlanResult{}, ErrQuestIDRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	character, ok, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return PlanResult{}, err
	}
	if !ok {
		return PlanResult{}, ErrCharacterNotFound
	}
	character = dnfrepo.CloneCharacter(character)
	record, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return PlanResult{}, err
	}
	state, known := questStateFor(record, int64(cmd.QuestID))
	result := PlanResult{
		AccountID:         character.AccountID,
		CharacterID:       characterID,
		Operation:         cmd.Operation,
		QuestID:           int64(cmd.QuestID),
		Known:             ok && known,
		Status:            state.Status,
		TriggerType:       state.TriggerType,
		ProgressValue:     state.ProgressValue,
		RewardSelectIndex: int64(cmd.RewardSelectIndex),
		HasRewardSelect:   cmd.HasRewardSelect,
		Multiplier:        int64(cmd.Multiplier),
	}
	if result.Multiplier == 0 {
		result.Multiplier = 1
	}
	if result.Status == "" {
		result.Status = "unknown"
	}
	if cmd.Operation == "set_quest_trigger" {
		result.TriggerType = cmd.TriggerType
	}
	return result, nil
}

// ApplyAccept closes the no-event-item accept path with one quest-row upsert.
// The ACK owner must call this first and may report success only after the
// persisted active state has been reloaded and verified.
func (o *Owner) ApplyAccept(ctx context.Context, catalog *Catalog, eligibility CharacterEligibility, cmd Command) (AcceptResult, error) {
	if o == nil || o.characters == nil || o.quests == nil {
		return AcceptResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return AcceptResult{}, ErrCharacterRequired
	}
	if cmd.QuestID == 0 {
		return AcceptResult{}, ErrQuestIDRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	character, ok, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return AcceptResult{}, err
	}
	if !ok {
		return AcceptResult{}, ErrCharacterNotFound
	}
	record, hasRecord, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return AcceptResult{}, err
	}
	if !hasRecord {
		record = dnfrepo.QuestRecord{CharacterID: characterID}
	} else {
		record = dnfrepo.CloneQuest(record)
	}
	definition, definitionKnown := catalog.Find(int64(cmd.QuestID))
	if !definitionKnown {
		return AcceptResult{}, ErrQuestDefinitionMissing
	}
	if definition.HasDependGiveItem {
		return AcceptResult{}, ErrQuestAcceptEventItemsRequired
	}
	if existing, known := questStateFor(record, int64(cmd.QuestID)); known && isActiveQuestStatus(existing.Status) {
		if existing.ProgressValue < 0 || existing.ProgressValue > int64(^uint32(0)) {
			return AcceptResult{}, ErrQuestPersistVerify
		}
		return AcceptResult{
			AccountID:   character.AccountID,
			CharacterID: characterID,
			QuestID:     cmd.QuestID,
			InitTrigger: uint32(existing.ProgressValue),
			Idempotent:  true,
			PVFPath:     definition.Path,
			QuestType:   definition.Type,
		}, nil
	}
	plan, err := catalog.PlanAccept(eligibility, record, int64(cmd.QuestID))
	if err != nil {
		return AcceptResult{}, err
	}
	now := time.Now()
	if record.States == nil {
		record.States = make(map[int64]dnfrepo.QuestState, 1)
	}
	record.States[int64(cmd.QuestID)] = dnfrepo.QuestState{
		Status:        "active",
		ProgressValue: int64(plan.InitTrigger),
		UpdatedAt:     now,
		Extra: map[string]string{
			"pvf_path":   plan.Path,
			"quest_type": plan.Type,
		},
	}
	for _, linked := range plan.LinkedSubQuests {
		record.States[linked.QuestID] = dnfrepo.QuestState{
			Status:        "active",
			ProgressValue: int64(linked.InitTrigger),
			UpdatedAt:     now,
			Extra: map[string]string{
				"pvf_path":                     linked.Path,
				"quest_type":                   linked.Type,
				"main_quest_id":                strconv.FormatInt(plan.QuestID, 10),
				"auto_activated_by_main_quest": "true",
			},
		}
	}
	record.CharacterID = characterID
	record.UpdatedAt = now
	// SaveFields is one atomic SQL upsert for the complete states JSON column.
	// Event-item quests are rejected above because they require a wider asset
	// transaction that this owner deliberately does not claim to provide.
	if err := dnfrepo.SaveQuestFields(ctx, o.quests, record, dnfrepo.QuestFieldStates); err != nil {
		return AcceptResult{}, err
	}
	persisted, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return AcceptResult{}, err
	}
	state, known := questStateFor(persisted, int64(cmd.QuestID))
	if !ok || !known || !isActiveQuestStatus(state.Status) || state.ProgressValue != int64(plan.InitTrigger) {
		return AcceptResult{}, ErrQuestPersistVerify
	}
	for _, linked := range plan.LinkedSubQuests {
		state, known := questStateFor(persisted, linked.QuestID)
		if !known || !isActiveQuestStatus(state.Status) || state.ProgressValue != int64(linked.InitTrigger) {
			return AcceptResult{}, ErrQuestPersistVerify
		}
	}
	return AcceptResult{
		AccountID:   character.AccountID,
		CharacterID: characterID,
		QuestID:     cmd.QuestID,
		InitTrigger: plan.InitTrigger,
		PVFPath:     plan.Path,
		QuestType:   plan.Type,
	}, nil
}

func isActiveQuestStatus(status string) bool {
	switch normalizeStatus(status) {
	case "active", "accepted", "inprogress", "progress":
		return true
	default:
		return false
	}
}

// ApplyGiveUp closes the current no-asset quest-abandon path. It deletes the
// active state from both repository projections, persists the complete changed
// fields, and verifies that the quest is absent before an ACK can be emitted.
// Quests whose PVF definition can own quest items fail closed until their
// inventory reclamation can share the same transaction.
func (o *Owner) ApplyGiveUp(ctx context.Context, catalog *Catalog, cmd Command) (GiveUpResult, error) {
	if o == nil || o.characters == nil || o.quests == nil {
		return GiveUpResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return GiveUpResult{}, ErrCharacterRequired
	}
	if cmd.QuestID == 0 {
		return GiveUpResult{}, ErrQuestIDRequired
	}
	if catalog == nil {
		return GiveUpResult{}, ErrQuestDefinitionMissing
	}
	definition, known := catalog.Find(int64(cmd.QuestID))
	if !known {
		return GiveUpResult{}, ErrQuestDefinitionMissing
	}
	if definition.CantGiveUp {
		return GiveUpResult{}, ErrQuestCannotGiveUp
	}
	if questGiveUpNeedsAssetTransaction(definition) {
		return GiveUpResult{}, ErrGiveUpNeedsAssets
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	character, found, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return GiveUpResult{}, err
	}
	if !found {
		return GiveUpResult{}, ErrCharacterNotFound
	}
	record, found, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return GiveUpResult{}, err
	}
	if !found {
		return GiveUpResult{}, ErrQuestNotActive
	}
	record = dnfrepo.CloneQuest(record)
	questID := int64(cmd.QuestID)
	state, known := questStateFor(record, questID)
	if !known || !isActiveQuestStatus(state.Status) {
		return GiveUpResult{}, ErrQuestNotActive
	}

	changedFields := make([]dnfrepo.QuestField, 0, 2)
	if _, exists := record.States[questID]; exists {
		delete(record.States, questID)
		changedFields = append(changedFields, dnfrepo.QuestFieldStates)
	}
	if _, exists := record.Progress[questID]; exists {
		delete(record.Progress, questID)
		changedFields = append(changedFields, dnfrepo.QuestFieldProgress)
	}
	if len(changedFields) == 0 {
		return GiveUpResult{}, ErrQuestNotActive
	}
	record.CharacterID = characterID
	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveQuestFields(ctx, o.quests, record, changedFields...); err != nil {
		return GiveUpResult{}, err
	}
	persisted, found, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return GiveUpResult{}, err
	}
	if !found {
		return GiveUpResult{}, ErrQuestPersistVerify
	}
	if persistedState, exists := questStateFor(persisted, questID); exists && isActiveQuestStatus(persistedState.Status) {
		return GiveUpResult{}, ErrQuestPersistVerify
	}
	return GiveUpResult{
		AccountID:   character.AccountID,
		CharacterID: characterID,
		QuestID:     cmd.QuestID,
	}, nil
}

func questGiveUpNeedsAssetTransaction(definition Definition) bool {
	if definition.HasDependGiveItem || len(definition.MonsterRewardItems) != 0 || len(definition.EnemyRewardItems) != 0 {
		return true
	}
	switch normalizeQuestTag(definition.Type) {
	case "seeking", "get item", "seek n meet npc":
		return true
	default:
		return false
	}
}

// ApplySetTrigger 按旧客户端 SET_TRIGGER 语义更新任务进度，但不代表 handler 已允许成功 ACK。
func (o *Owner) ApplySetTrigger(ctx context.Context, cmd Command) (PlanResult, error) {
	result, err := o.Plan(ctx, cmd)
	if err != nil {
		return PlanResult{}, err
	}
	if cmd.Operation != "set_quest_trigger" || !result.Known || !isActiveQuestStatus(result.Status) {
		return result, nil
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	record, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return PlanResult{}, err
	}
	if !ok {
		return result, nil
	}
	record = dnfrepo.CloneQuest(record)
	state, field, known := mutableQuestState(&record, int64(cmd.QuestID))
	if !known {
		return result, nil
	}

	oldProgress := state.ProgressValue
	newProgress := clampQuestProgress(nextTriggerProgress(ctx, o.inventory, characterID, state, cmd))
	if newProgress == oldProgress && state.TriggerType == cmd.TriggerType {
		result.ProgressValue = newProgress
		return result, nil
	}

	state.TriggerType = cmd.TriggerType
	state.ProgressValue = newProgress
	state.UpdatedAt = time.Now()
	if state.Extra == nil {
		state.Extra = make(map[string]string, 2)
	}
	state.Extra["last_set_trigger_type"] = strconv.Itoa(int(cmd.TriggerType))
	state.Extra["last_set_trigger_increment"] = strconv.FormatBool(cmd.IsIncrement)
	switch field {
	case dnfrepo.QuestFieldStates:
		record.States[int64(cmd.QuestID)] = state
	case dnfrepo.QuestFieldProgress:
		record.Progress[int64(cmd.QuestID)] = state
	}
	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveQuestFields(ctx, o.quests, record, field); err != nil {
		return PlanResult{}, err
	}
	result.StateChanged = true
	result.TriggerType = state.TriggerType
	result.ProgressValue = state.ProgressValue
	return result, nil
}

func questStateFor(record dnfrepo.QuestRecord, questID int64) (dnfrepo.QuestState, bool) {
	if state, ok := record.States[questID]; ok {
		return state, true
	}
	if state, ok := record.Progress[questID]; ok {
		return state, true
	}
	return dnfrepo.QuestState{}, false
}

func mutableQuestState(record *dnfrepo.QuestRecord, questID int64) (dnfrepo.QuestState, dnfrepo.QuestField, bool) {
	if record == nil {
		return dnfrepo.QuestState{}, "", false
	}
	if state, ok := record.States[questID]; ok {
		return state, dnfrepo.QuestFieldStates, true
	}
	if state, ok := record.Progress[questID]; ok {
		return state, dnfrepo.QuestFieldProgress, true
	}
	return dnfrepo.QuestState{}, "", false
}

func nextTriggerProgress(ctx context.Context, inventory dnfrepo.InventoryRepository, characterID string, state dnfrepo.QuestState, cmd Command) int64 {
	if cmd.TriggerType == 1 {
		itemIDs := parseSeekingItemIDs(state.Extra)
		if len(itemIDs) > 0 && inventory != nil {
			return seekingMissingMask(ctx, inventory, characterID, itemIDs, parseSeekingItemCounts(state.Extra, len(itemIDs)))
		}
	}
	if channel, ok := setTriggerPackedChannel(cmd.TriggerType); ok {
		if state.ProgressValue < 0 || state.ProgressValue > int64(^uint32(0)) {
			return state.ProgressValue
		}
		progress := uint32(state.ProgressValue)
		if cmd.IsIncrement {
			return int64(incrementPackedTriggerChannel(progress, channel))
		}
		return int64(decrementPackedTriggerChannel(progress, channel))
	}
	if cmd.IsIncrement {
		return state.ProgressValue + 1
	}
	if state.ProgressValue > 0 {
		return state.ProgressValue - 1
	}
	return 0
}

// Current-client op33 uses one-hot trigger types for the three 9-bit channels
// stored in the active quest's packed u32 trigger value. Treating those values
// as one scalar counter pollutes adjacent channels and makes the client feed
// the malformed value back through op33 indefinitely.
func setTriggerPackedChannel(triggerType byte) (int, bool) {
	switch triggerType {
	case 0x10:
		return 0, true
	case 0x20:
		return 1, true
	case 0x40:
		return 2, true
	default:
		return 0, false
	}
}

func incrementPackedTriggerChannel(trigger uint32, channel int) uint32 {
	if channel < 0 || channel > 2 {
		return trigger
	}
	shift := channel * 9
	current := (trigger >> shift) & 0x1ff
	if current == 0x1ff {
		return trigger
	}
	trigger &^= 0x1ff << shift
	trigger |= (current + 1) << shift
	return trigger
}

// clampQuestProgress ensures quest progress never goes negative (defensive
// guard against malformed client triggers or dungeon-ID leakage).
func clampQuestProgress(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func parseSeekingItemIDs(extra map[string]string) []int64 {
	for _, key := range []string{"seeking_item_ids", "seek_item_ids", "required_item_ids", "item_ids"} {
		if values := parseIntList(extra[key]); len(values) > 0 {
			return values
		}
	}
	return nil
}

func parseSeekingItemCounts(extra map[string]string, n int) []int64 {
	for _, key := range []string{"seeking_item_counts", "seek_item_counts", "required_item_counts", "item_counts"} {
		values := parseIntList(extra[key])
		if len(values) > 0 {
			for len(values) < n {
				values = append(values, 1)
			}
			return values[:n]
		}
	}
	values := make([]int64, n)
	for i := range values {
		values[i] = 1
	}
	return values
}

func parseIntList(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';', '|', ' ', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	})
	out := make([]int64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err == nil && value > 0 {
			out = append(out, value)
		}
	}
	return out
}

func seekingMissingMask(ctx context.Context, inventory dnfrepo.InventoryRepository, characterID string, itemIDs []int64, counts []int64) int64 {
	record, ok, err := inventory.Load(ctx, characterID)
	if err != nil || !ok {
		return 0
	}
	var mask int64
	for index, itemID := range itemIDs {
		required := int64(1)
		if index < len(counts) && counts[index] > 0 {
			required = counts[index]
		}
		if countItemInMain(record, itemID) < required && index < 63 {
			mask |= 1 << index
		}
	}
	return mask
}

func countItemInMain(record dnfrepo.InventoryRecord, itemID int64) int64 {
	var total int64
	for key, stack := range record.Slots {
		if !strings.HasPrefix(key, "0:") || stack.ItemID != itemID {
			continue
		}
		if stack.Count <= 0 {
			total++
			continue
		}
		total += stack.Count
	}
	return total
}

func planError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
