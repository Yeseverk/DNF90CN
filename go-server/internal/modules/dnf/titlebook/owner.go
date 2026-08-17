package titlebook

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	ListType       byte  = 100
	SlotBase       int16 = 1000
	MaxPerCategory       = 200
)

var (
	ErrOwnerUnavailable  = errors.New("title book owner unavailable")
	ErrCharacterRequired = errors.New("selected character id required")
	ErrInventoryNotFound = errors.New("title book inventory not found")
	ErrSourceNotFound    = errors.New("title book source item not found")
	ErrCategoryInvalid   = errors.New("title book category or index is invalid")
	ErrCategoryFull      = errors.New("title book category is full")
	ErrTargetFull        = errors.New("title book target inventory is full")
)

type PutCommand struct {
	SelectedCharacterID uint16
	ItemSpace           int32
	SourceSlot          int16
	ItemID              int32
	Category            int32
	BookIndex           int32
	UpdatedAt           time.Time
}

type PutResult struct {
	CharacterID string
	ItemSpace   int32
	SourceList  byte
	SourceSlot  int16
	ItemID      int32
	Category    int32
	BookIndex   int32
	TargetSlot  int16
}

type GetCommand struct {
	SelectedCharacterID uint16
	ItemSpace           int32
	SourceSlot          int16
	ItemID              int32
	Category            int32
	BookIndex           int32
	UpdatedAt           time.Time
}

type GetResult struct {
	CharacterID string
	ItemSpace   int32
	SourceSlot  int16
	ItemID      int32
	Category    int32
	BookIndex   int32
	TargetSlot  int16
	TargetStack dnfrepo.ItemStack
}

type Owner struct {
	settlements dnfrepo.CharacterSettlementUnitOfWork
}

func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Inventory == nil || repos.CharacterSettlement == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{settlements: repos.CharacterSettlement}, nil
}

func (o *Owner) Put(ctx context.Context, cmd PutCommand) (PutResult, error) {
	if o == nil || o.settlements == nil {
		return PutResult{}, ErrOwnerUnavailable
	}
	if err := validateBookLocation(cmd.Category, cmd.BookIndex); err != nil {
		return PutResult{}, err
	}
	characterID, err := selectedCharacterKey(cmd.SelectedCharacterID)
	if err != nil {
		return PutResult{}, err
	}
	var result PutResult
	err = o.settlements.WithinCharacterSettlement(ctx, characterID, func(tx dnfrepo.Group) error {
		inventory, loadErr := loadInventory(ctx, tx.Inventory, characterID)
		if loadErr != nil {
			return loadErr
		}
		sourceList := byte(cmd.ItemSpace)
		sourceKey := inventoryKey(sourceList, cmd.SourceSlot)
		sourceStack, found := inventory.Slots[sourceKey]
		if !found || sourceStack.ItemID != int64(cmd.ItemID) || sourceStack.ItemID <= 0 {
			return fmt.Errorf(
				"%w: list=%d slot=%d item=%d",
				ErrSourceNotFound,
				sourceList,
				cmd.SourceSlot,
				cmd.ItemID,
			)
		}
		targetSlot, bookIndex, found := firstBookSlot(inventory.Slots, cmd.Category, cmd.BookIndex)
		if !found {
			return fmt.Errorf("%w: category=%d", ErrCategoryFull, cmd.Category)
		}

		delete(inventory.Slots, sourceKey)
		titleStack := sourceStack
		titleStack.Count = 1
		if titleStack.Extra == nil {
			titleStack.Extra = make(map[string]string, 4)
		}
		titleStack.Extra["title_book_category"] = strconv.FormatInt(int64(cmd.Category), 10)
		titleStack.Extra["title_book_index"] = strconv.FormatInt(int64(bookIndex), 10)
		titleStack.Extra["source"] = "title_book_put"
		inventory.Slots[inventoryKey(ListType, targetSlot)] = titleStack
		inventory.UpdatedAt = mutationTime(cmd.UpdatedAt)
		if saveErr := dnfrepo.SaveInventoryFields(ctx, tx.Inventory, inventory, dnfrepo.InventoryFieldSlots); saveErr != nil {
			return saveErr
		}
		result = PutResult{
			CharacterID: characterID,
			ItemSpace:   cmd.ItemSpace,
			SourceList:  sourceList,
			SourceSlot:  cmd.SourceSlot,
			ItemID:      cmd.ItemID,
			Category:    cmd.Category,
			BookIndex:   bookIndex,
			TargetSlot:  targetSlot,
		}
		return nil
	})
	return result, err
}

func (o *Owner) Get(ctx context.Context, cmd GetCommand) (GetResult, error) {
	if o == nil || o.settlements == nil {
		return GetResult{}, ErrOwnerUnavailable
	}
	if err := validateBookLocation(cmd.Category, cmd.BookIndex); err != nil {
		return GetResult{}, err
	}
	characterID, err := selectedCharacterKey(cmd.SelectedCharacterID)
	if err != nil {
		return GetResult{}, err
	}
	var result GetResult
	err = o.settlements.WithinCharacterSettlement(ctx, characterID, func(tx dnfrepo.Group) error {
		inventory, loadErr := loadInventory(ctx, tx.Inventory, characterID)
		if loadErr != nil {
			return loadErr
		}
		sourceKey := BookSlotKey(cmd.Category, cmd.BookIndex)
		titleStack, found := inventory.Slots[sourceKey]
		if !found || titleStack.ItemID <= 0 ||
			(cmd.ItemID > 0 && titleStack.ItemID != int64(cmd.ItemID)) {
			return fmt.Errorf(
				"%w: category=%d index=%d item=%d",
				ErrSourceNotFound,
				cmd.Category,
				cmd.BookIndex,
				cmd.ItemID,
			)
		}
		targetSlot, found := firstMainInventorySlot(inventory.Slots)
		if !found {
			return ErrTargetFull
		}

		delete(inventory.Slots, sourceKey)
		targetStack := titleStack
		targetStack.Count = 1
		if targetStack.Extra == nil {
			targetStack.Extra = make(map[string]string, 2)
		}
		targetStack.Extra["source"] = "title_book_get"
		inventory.Slots[inventoryKey(dnfrepo.MainInventoryListType, targetSlot)] = targetStack
		inventory.UpdatedAt = mutationTime(cmd.UpdatedAt)
		if saveErr := dnfrepo.SaveInventoryFields(ctx, tx.Inventory, inventory, dnfrepo.InventoryFieldSlots); saveErr != nil {
			return saveErr
		}
		result = GetResult{
			CharacterID: characterID,
			ItemSpace:   cmd.ItemSpace,
			SourceSlot:  cmd.SourceSlot,
			ItemID:      int32(titleStack.ItemID),
			Category:    cmd.Category,
			BookIndex:   cmd.BookIndex,
			TargetSlot:  targetSlot,
			TargetStack: targetStack,
		}
		return nil
	})
	return result, err
}

func BookSlotKey(category, index int32) string {
	return inventoryKey(ListType, int16(category)*SlotBase+int16(index))
}

func firstBookSlot(items map[string]dnfrepo.ItemStack, category, preferred int32) (int16, int32, bool) {
	if preferred >= 0 && preferred < MaxPerCategory {
		slot := int16(category)*SlotBase + int16(preferred)
		if _, occupied := items[inventoryKey(ListType, slot)]; !occupied {
			return slot, preferred, true
		}
	}
	for index := int32(0); index < MaxPerCategory; index++ {
		slot := int16(category)*SlotBase + int16(index)
		if _, occupied := items[inventoryKey(ListType, slot)]; !occupied {
			return slot, index, true
		}
	}
	return -1, -1, false
}

func firstMainInventorySlot(items map[string]dnfrepo.ItemStack) (int16, bool) {
	for slot := int16(9); slot <= 64; slot++ {
		if _, occupied := items[inventoryKey(dnfrepo.MainInventoryListType, slot)]; !occupied {
			return slot, true
		}
	}
	return -1, false
}

func inventoryKey(listType byte, slot int16) string {
	return fmt.Sprintf("%d:%d", listType, slot)
}

func mutationTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func selectedCharacterKey(selectedCharacterID uint16) (string, error) {
	if selectedCharacterID == 0 {
		return "", ErrCharacterRequired
	}
	return strconv.FormatUint(uint64(selectedCharacterID), 10), nil
}

func loadInventory(ctx context.Context, repository dnfrepo.InventoryRepository, characterID string) (dnfrepo.InventoryRecord, error) {
	if repository == nil {
		return dnfrepo.InventoryRecord{}, ErrOwnerUnavailable
	}
	inventory, found, err := repository.Load(ctx, characterID)
	if err != nil {
		return dnfrepo.InventoryRecord{}, err
	}
	if !found || strings.TrimSpace(inventory.CharacterID) != characterID {
		return dnfrepo.InventoryRecord{}, fmt.Errorf("%w: character=%s", ErrInventoryNotFound, characterID)
	}
	inventory = dnfrepo.CloneInventory(inventory)
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	return inventory, nil
}

func validateBookLocation(category, index int32) error {
	if category < 0 || category >= 5 || index < 0 || index >= MaxPerCategory {
		return fmt.Errorf("%w: category=%d index=%d", ErrCategoryInvalid, category, index)
	}
	return nil
}
