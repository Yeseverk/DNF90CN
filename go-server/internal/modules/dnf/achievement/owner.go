package achievement

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	titleBookListType       byte  = 100
	titleBookSlotBase       int16 = 1000
	titleBookMaxPerCategory       = 200

	statusActive    = "title_book_active"
	statusCompleted = "title_book_completed"

	extraInitialized = "title_book_initialized"
	extraRemain1     = "title_book_remain_1"
	extraRemain2     = "title_book_remain_2"
	extraRemain3     = "title_book_remain_3"
)

var (
	ErrOwnerUnavailable   = errors.New("achievement owner unavailable")
	ErrCharacterRequired  = errors.New("selected character id required")
	ErrInventoryNotFound  = errors.New("achievement inventory not found")
	ErrDefinitionNotFound = errors.New("achievement definition not found")
)

type TitleReward struct {
	ItemID    int32
	Category  int32
	BookIndex int32
}

type Definition struct {
	QuestID int32
	Target1 uint16
	Target2 uint16
	Target3 uint16
	Reward  TitleReward
}

type DefinitionResolver interface {
	ResolveAchievementDefinition(context.Context, int32) (Definition, error)
}

type Command struct {
	SelectedCharacterID uint16
	QuestID             int32
	Delta1              uint16
	Delta2              uint16
	Delta3              uint16
	UpdatedAt           time.Time
}

type Result struct {
	CharacterID       string
	QuestID           int32
	Remain1           uint16
	Remain2           uint16
	Remain3           uint16
	Completed         bool
	AlreadyCompleted  bool
	TitleGranted      bool
	TitleItemID       int32
	TitleCategory     int32
	TitleSlot         int16
	LegacyRowsRemoved int
}

type Progress struct {
	QuestID   int32
	Remain1   uint16
	Remain2   uint16
	Remain3   uint16
	Completed bool
}

// Owner atomically owns the durable title-achievement state and its optional
// physical title reward. Progress deliberately lives in Quest.Progress, not
// inventory list 100: list 100 is the client-visible physical title book.
type Owner struct {
	settlements dnfrepo.CharacterSettlementUnitOfWork
	resolver    DefinitionResolver
}

func NewOwner(repos dnfrepo.Group, resolver DefinitionResolver) (*Owner, error) {
	if repos.CharacterSettlement == nil || repos.Quest == nil || repos.Inventory == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{settlements: repos.CharacterSettlement, resolver: resolver}, nil
}

func (o *Owner) Trigger(ctx context.Context, cmd Command) (Result, error) {
	if o == nil || o.settlements == nil || o.resolver == nil {
		return Result{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return Result{}, ErrCharacterRequired
	}
	definition, err := o.resolver.ResolveAchievementDefinition(ctx, cmd.QuestID)
	if err != nil {
		return Result{}, err
	}
	if definition.QuestID != cmd.QuestID ||
		(definition.Target1 == 0 && definition.Target2 == 0 && definition.Target3 == 0) {
		return Result{}, fmt.Errorf("%w: quest=%d", ErrDefinitionNotFound, cmd.QuestID)
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	result := Result{CharacterID: characterID, QuestID: cmd.QuestID}
	err = o.settlements.WithinCharacterSettlement(ctx, characterID, func(tx dnfrepo.Group) error {
		quests, _, loadErr := tx.Quest.Load(ctx, characterID)
		if loadErr != nil {
			return loadErr
		}
		quests = dnfrepo.CloneQuest(quests)
		quests.CharacterID = characterID
		if quests.Progress == nil {
			quests.Progress = make(map[int64]dnfrepo.QuestState)
		}

		inventory, found, loadErr := tx.Inventory.Load(ctx, characterID)
		if loadErr != nil {
			return loadErr
		}
		if !found || strings.TrimSpace(inventory.CharacterID) != characterID {
			return fmt.Errorf("%w: character=%s", ErrInventoryNotFound, characterID)
		}
		inventory = dnfrepo.CloneInventory(inventory)
		if inventory.Slots == nil {
			inventory.Slots = make(map[string]dnfrepo.ItemStack)
		}
		for key, stack := range inventory.Slots {
			if IsLegacyInventoryProgress(key, stack) {
				delete(inventory.Slots, key)
				result.LegacyRowsRemoved++
			}
		}

		state, known := quests.Progress[int64(cmd.QuestID)]
		if known && strings.EqualFold(strings.TrimSpace(state.Status), statusCompleted) {
			result.AlreadyCompleted = true
			if result.LegacyRowsRemoved > 0 {
				inventory.UpdatedAt = commandTime(cmd.UpdatedAt)
				return dnfrepo.SaveInventoryFields(ctx, tx.Inventory, inventory, dnfrepo.InventoryFieldSlots)
			}
			return nil
		}

		remain1, remain2, remain3 := definition.Target1, definition.Target2, definition.Target3
		if known && strings.EqualFold(strings.TrimSpace(state.Status), statusActive) &&
			state.Extra[extraInitialized] == "1" {
			remain1 = extraUint16(state.Extra, extraRemain1, definition.Target1)
			remain2 = extraUint16(state.Extra, extraRemain2, definition.Target2)
			remain3 = extraUint16(state.Extra, extraRemain3, definition.Target3)
		}
		remain1 = subtractProgress(remain1, cmd.Delta1)
		remain2 = subtractProgress(remain2, cmd.Delta2)
		remain3 = subtractProgress(remain3, cmd.Delta3)

		now := commandTime(cmd.UpdatedAt)
		state.Status = statusActive
		state.ProgressValue = int64(remain1)
		state.UpdatedAt = now
		if state.Extra == nil {
			state.Extra = make(map[string]string, 4)
		}
		state.Extra[extraInitialized] = "1"
		state.Extra[extraRemain1] = strconv.FormatUint(uint64(remain1), 10)
		state.Extra[extraRemain2] = strconv.FormatUint(uint64(remain2), 10)
		state.Extra[extraRemain3] = strconv.FormatUint(uint64(remain3), 10)

		result.Remain1 = remain1
		result.Remain2 = remain2
		result.Remain3 = remain3
		result.Completed = remain1 == 0 && remain2 == 0 && remain3 == 0
		if result.Completed {
			state.Status = statusCompleted
			state.ProgressValue = 0
			grantTitle(&inventory, definition.Reward, cmd.QuestID, &result)
		}
		quests.Progress[int64(cmd.QuestID)] = state
		quests.UpdatedAt = now
		inventory.UpdatedAt = now

		if err := dnfrepo.SaveQuestFields(ctx, tx.Quest, quests, dnfrepo.QuestFieldProgress); err != nil {
			return err
		}
		if result.TitleGranted || result.LegacyRowsRemoved > 0 {
			if err := dnfrepo.SaveInventoryFields(ctx, tx.Inventory, inventory, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

// RepairLegacyRows removes only rows carrying the exact signature emitted by
// the former broken implementation. Their stored counters were initialized
// from the first delta and are not trustworthy enough to migrate.
func (o *Owner) RepairLegacyRows(ctx context.Context, selectedCharacterID uint16) (int, error) {
	if o == nil || o.settlements == nil {
		return 0, ErrOwnerUnavailable
	}
	if selectedCharacterID == 0 {
		return 0, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(selectedCharacterID), 10)
	removed := 0
	err := o.settlements.WithinCharacterSettlement(ctx, characterID, func(tx dnfrepo.Group) error {
		inventory, found, err := tx.Inventory.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		inventory = dnfrepo.CloneInventory(inventory)
		for key, stack := range inventory.Slots {
			if IsLegacyInventoryProgress(key, stack) {
				delete(inventory.Slots, key)
				removed++
			}
		}
		if removed == 0 {
			return nil
		}
		inventory.UpdatedAt = time.Now().UTC()
		return dnfrepo.SaveInventoryFields(ctx, tx.Inventory, inventory, dnfrepo.InventoryFieldSlots)
	})
	return removed, err
}

func Snapshot(record dnfrepo.QuestRecord) []Progress {
	rows := make([]Progress, 0)
	for questID, state := range record.Progress {
		status := strings.ToLower(strings.TrimSpace(state.Status))
		if status != statusActive && status != statusCompleted {
			continue
		}
		if questID <= 0 || questID > int64(^uint32(0)) {
			continue
		}
		row := Progress{QuestID: int32(questID), Completed: status == statusCompleted}
		if !row.Completed {
			row.Remain1 = extraUint16(state.Extra, extraRemain1, 0)
			row.Remain2 = extraUint16(state.Extra, extraRemain2, 0)
			row.Remain3 = extraUint16(state.Extra, extraRemain3, 0)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].QuestID < rows[j].QuestID })
	return rows
}

func IsLegacyInventoryProgress(key string, stack dnfrepo.ItemStack) bool {
	if stack.Extra == nil || stack.Extra["initialized"] != "1" {
		return false
	}
	questID, err := strconv.ParseInt(strings.TrimSpace(stack.Extra["quest_id"]), 10, 32)
	if err != nil || questID <= 0 || stack.ItemID != questID {
		return false
	}
	parts := strings.Split(key, ":")
	if len(parts) != 2 || parts[0] != strconv.Itoa(int(titleBookListType)) {
		return false
	}
	slot, err := strconv.Atoi(parts[1])
	return err == nil && slot >= 900 && slot <= 999
}

func grantTitle(inventory *dnfrepo.InventoryRecord, reward TitleReward, questID int32, result *Result) {
	if inventory == nil || result == nil || reward.ItemID <= 0 ||
		reward.Category < 0 || reward.Category >= 5 {
		return
	}
	for index := int32(0); index < titleBookMaxPerCategory; index++ {
		slot := int16(reward.Category)*titleBookSlotBase + int16(index)
		if existing, found := inventory.Slots[titleSlotKey(slot)]; found &&
			existing.ItemID == int64(reward.ItemID) {
			result.TitleItemID = reward.ItemID
			result.TitleCategory = reward.Category
			result.TitleSlot = slot
			return
		}
	}
	targetSlot, ok := firstTitleSlot(inventory.Slots, reward.Category, reward.BookIndex)
	if !ok {
		return
	}
	key := titleSlotKey(targetSlot)
	inventory.Slots[key] = dnfrepo.ItemStack{
		ItemID: int64(reward.ItemID),
		Count:  1,
		Extra: map[string]string{
			"source":              "achievement_reward",
			"title_book_category": strconv.FormatInt(int64(reward.Category), 10),
			"title_book_index":    strconv.FormatInt(int64(targetSlot%titleBookSlotBase), 10),
			"quest_id":            strconv.FormatInt(int64(questID), 10),
		},
	}
	result.TitleGranted = true
	result.TitleItemID = reward.ItemID
	result.TitleCategory = reward.Category
	result.TitleSlot = targetSlot
}

func titleSlotKey(slot int16) string {
	return fmt.Sprintf("%d:%d", titleBookListType, slot)
}

func categorySlotKey(category, index int32) string {
	return titleSlotKey(int16(category)*titleBookSlotBase + int16(index))
}

func firstTitleSlot(items map[string]dnfrepo.ItemStack, category, preferredIndex int32) (int16, bool) {
	if preferredIndex >= 0 && preferredIndex < titleBookMaxPerCategory {
		slot := int16(category)*titleBookSlotBase + int16(preferredIndex)
		existing, occupied := items[titleSlotKey(slot)]
		if !occupied || existing.ItemID <= 0 {
			return slot, true
		}
	}
	for index := int32(0); index < titleBookMaxPerCategory; index++ {
		existing, occupied := items[categorySlotKey(category, index)]
		if !occupied || existing.ItemID <= 0 {
			return int16(category)*titleBookSlotBase + int16(index), true
		}
	}
	return -1, false
}

func extraUint16(extra map[string]string, key string, fallback uint16) uint16 {
	value, err := strconv.ParseUint(strings.TrimSpace(extra[key]), 10, 16)
	if err != nil {
		return fallback
	}
	return uint16(value)
}

func subtractProgress(remaining, delta uint16) uint16 {
	if delta >= remaining {
		return 0
	}
	return remaining - delta
}

func commandTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}
