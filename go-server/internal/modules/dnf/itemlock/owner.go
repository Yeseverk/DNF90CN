package itemlock

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
	listTypeMain          byte = 0
	listTypeAvatar        byte = 1
	listTypePersonalCargo byte = 2
	listTypeEquipment     byte = 3
	listTypePet           byte = 7

	lockStateActive = "1"
)

var (
	ErrOwnerUnavailable       = errors.New("itemlock owner unavailable")
	ErrCharacterRequired      = errors.New("selected character id required")
	ErrInventoryNotFound      = errors.New("inventory record not found")
	ErrUnsupportedList        = errors.New("itemlock list type is not supported")
	ErrSlotNotFound           = errors.New("itemlock slot not found")
	ErrAlreadyLocked          = errors.New("item is already locked")
	ErrNotLocked              = errors.New("item is not locked")
	ErrEquipmentOwnerRequired = errors.New("equipment item lock requires equipment owner")
)

type Owner struct {
	repo dnfrepo.InventoryRepository
}

type Result struct {
	CharacterID string
	ListType    byte
	SlotIndex   int16
	State       string
	Changed     bool
}

func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Inventory == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{repo: repos.Inventory}, nil
}

func (o *Owner) Apply(ctx context.Context, cmd Command) (Result, error) {
	if o == nil || o.repo == nil {
		return Result{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return Result{}, ErrCharacterRequired
	}
	if err := checkListType(cmd.ListType); err != nil {
		return Result{}, err
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	record, ok, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, ErrInventoryNotFound
	}

	record = dnfrepo.CloneInventory(record)
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if record.Warehouse == nil {
		record.Warehouse = make(map[string]dnfrepo.ItemStack)
	}

	items, field := itemMapForList(&record, cmd.ListType)
	key := slotKey(cmd.ListType, cmd.SlotIndex)
	stack, ok := items[key]
	if !ok {
		return Result{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.ListType, cmd.SlotIndex)
	}

	result := Result{
		CharacterID: characterID,
		ListType:    cmd.ListType,
		SlotIndex:   cmd.SlotIndex,
		State:       currentLockState(stack),
	}

	switch cmd.Operation {
	case "request_item_lock":
		if isLocked(stack) {
			return Result{}, fmt.Errorf("%w: list=%d slot=%d", ErrAlreadyLocked, cmd.ListType, cmd.SlotIndex)
		}
		stack = setLockState(stack, lockStateActive, cmd.ListType, cmd.SlotIndex)
		result.State = lockStateActive
		result.Changed = true
	case "request_item_unlock":
		if !isLocked(stack) {
			return Result{}, fmt.Errorf("%w: list=%d slot=%d", ErrNotLocked, cmd.ListType, cmd.SlotIndex)
		}
		stack = clearLockState(stack)
		result.State = ""
		result.Changed = true
	case "request_item_unlock_cancel":
		if !isLocked(stack) {
			return Result{}, fmt.Errorf("%w: list=%d slot=%d", ErrNotLocked, cmd.ListType, cmd.SlotIndex)
		}
		if currentLockState(stack) != lockStateActive {
			stack = setLockState(stack, lockStateActive, cmd.ListType, cmd.SlotIndex)
			result.Changed = true
		}
		result.State = lockStateActive
	default:
		return Result{}, fmt.Errorf("unsupported itemlock operation %q", cmd.Operation)
	}

	if !result.Changed {
		return result, nil
	}
	items[key] = stack
	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, field); err != nil {
		return Result{}, err
	}
	return result, nil
}

func checkListType(listType byte) error {
	switch listType {
	case listTypeMain, listTypeAvatar, listTypePersonalCargo, listTypePet:
		return nil
	case listTypeEquipment:
		return ErrEquipmentOwnerRequired
	default:
		return fmt.Errorf("%w: %d", ErrUnsupportedList, listType)
	}
}

func itemMapForList(record *dnfrepo.InventoryRecord, listType byte) (map[string]dnfrepo.ItemStack, dnfrepo.InventoryField) {
	if listType == listTypePersonalCargo {
		return record.Warehouse, dnfrepo.InventoryFieldWarehouse
	}
	return record.Slots, dnfrepo.InventoryFieldSlots
}

func slotKey(listType byte, slotIndex int16) string {
	return fmt.Sprintf("%d:%d", listType, slotIndex)
}

func isLocked(stack dnfrepo.ItemStack) bool {
	switch strings.ToLower(strings.TrimSpace(currentLockState(stack))) {
	case "1", "2", "active", "locked", "unlocking", "pending_unlock":
		return true
	default:
		return false
	}
}

func currentLockState(stack dnfrepo.ItemStack) string {
	if stack.Extra == nil {
		return ""
	}
	return strings.TrimSpace(stack.Extra["equipment_lock_state"])
}

func setLockState(stack dnfrepo.ItemStack, state string, listType byte, slotIndex int16) dnfrepo.ItemStack {
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 5)
	}
	stack.Extra["equipment_lock_state"] = state
	stack.Extra["equipment_lock_kind"] = "equipment"
	stack.Extra["equipment_lock_list_type"] = strconv.Itoa(int(listType))
	stack.Extra["equipment_lock_slot"] = strconv.Itoa(int(slotIndex))
	delete(stack.Extra, "equipment_lock_remaining_seconds")
	return stack
}

func clearLockState(stack dnfrepo.ItemStack) dnfrepo.ItemStack {
	if stack.Extra == nil {
		return stack
	}
	delete(stack.Extra, "equipment_lock_state")
	delete(stack.Extra, "equipment_lock_kind")
	delete(stack.Extra, "equipment_lock_list_type")
	delete(stack.Extra, "equipment_lock_slot")
	delete(stack.Extra, "equipment_lock_remaining_seconds")
	if len(stack.Extra) == 0 {
		stack.Extra = nil
	}
	return stack
}
