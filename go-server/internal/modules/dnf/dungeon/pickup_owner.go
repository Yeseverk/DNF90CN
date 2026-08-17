package dungeon

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type PickupItemKind string

const (
	PickupItemStackable PickupItemKind = "stackable"
	PickupItemEquipment PickupItemKind = "equipment"
)

var (
	ErrPickupItemInvalid   = errors.New("dungeon pickup item is invalid")
	ErrPickupInventoryFull = errors.New("dungeon pickup inventory category is full")
	ErrPickupStackLimit    = errors.New("dungeon pickup exceeds stack limit")
	ErrInventoryNotFound   = errors.New("dungeon inventory is not found")
)

type PickupItemDefinition struct {
	ItemID          uint32
	Kind            PickupItemKind
	StackLimit      int64
	SlotStart       int16
	SlotEnd         int16
	PreferQuickSlot bool
}

type PickupItemPlacement struct {
	Definition        PickupItemDefinition
	Amount            uint32
	BuildNew          func(int16) (dnfrepo.ItemStack, error)
	NormalizeExisting func(dnfrepo.ItemStack) (dnfrepo.ItemStack, error)
}

type PickupItemCommand struct {
	CharacterID string
	Placement   PickupItemPlacement
	UpdatedAt   time.Time
	Finalize    func(int16, dnfrepo.ItemStack) (dnfrepo.ItemStack, error)
}

type PickupItemResult struct {
	Slot  uint16
	Stack dnfrepo.ItemStack
}

func (o *Owner) GrantPickupItem(ctx context.Context, cmd PickupItemCommand) (PickupItemResult, error) {
	if o == nil || o.items == nil || strings.TrimSpace(cmd.CharacterID) == "" {
		return PickupItemResult{}, ErrOwnerUnavailable
	}
	ctx = contextOrBackground(ctx)
	now := updatedAtOrNow(cmd.UpdatedAt)

	var result PickupItemResult
	err := o.items.WithinCharacterItems(ctx, cmd.CharacterID, func(
		inventories dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
	) error {
		record, found, err := inventories.Load(ctx, cmd.CharacterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrInventoryNotFound
		}
		record = dnfrepo.CloneInventory(record)
		slot, stack, err := PlacePickupItem(&record, cmd.Placement)
		if err != nil {
			return err
		}
		if cmd.Finalize != nil {
			stack, err = cmd.Finalize(int16(slot), stack)
			if err != nil {
				return err
			}
			record.Slots[mainSlotKey(slot)] = cloneStack(stack)
		}
		record.UpdatedAt = now
		if err := dnfrepo.SaveInventoryFields(ctx, inventories, record, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}
		result = PickupItemResult{Slot: slot, Stack: cloneStack(stack)}
		return nil
	})
	if err != nil {
		return PickupItemResult{}, err
	}
	return result, nil
}

// PlacePickupItem owns the reusable dungeon/PVF placement policy. Current
// client raw-entry construction is supplied by BuildNew/Finalize.
func PlacePickupItem(record *dnfrepo.InventoryRecord, placement PickupItemPlacement) (uint16, dnfrepo.ItemStack, error) {
	definition := placement.Definition
	if record == nil || placement.Amount == 0 || definition.ItemID == 0 ||
		definition.SlotStart <= 0 || definition.SlotEnd < definition.SlotStart {
		return 0, dnfrepo.ItemStack{}, ErrPickupItemInvalid
	}
	if definition.Kind != PickupItemStackable && definition.Kind != PickupItemEquipment {
		return 0, dnfrepo.ItemStack{}, ErrPickupItemInvalid
	}
	if definition.Kind == PickupItemEquipment && placement.Amount != 1 {
		return 0, dnfrepo.ItemStack{}, ErrPickupItemInvalid
	}
	if definition.Kind == PickupItemStackable && definition.StackLimit > 0 &&
		int64(placement.Amount) > definition.StackLimit {
		return 0, dnfrepo.ItemStack{}, fmt.Errorf(
			"%w: item=%d amount=%d limit=%d",
			ErrPickupStackLimit,
			definition.ItemID,
			placement.Amount,
			definition.StackLimit,
		)
	}
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if definition.Kind == PickupItemStackable && definition.PreferQuickSlot {
		if slot, stack, ok, err := stackPickupInRange(record, placement, 3, 8); err != nil {
			return 0, dnfrepo.ItemStack{}, err
		} else if ok {
			return slot, stack, nil
		}
		// Quick slots and the PVF category are ranges of the same main
		// inventory. Merge every existing stack before allocating another
		// quick-slot stack so one pickup cannot split an already bagged item.
		if slot, stack, ok, err := stackPickupInRange(
			record,
			placement,
			definition.SlotStart,
			definition.SlotEnd,
		); err != nil {
			return 0, dnfrepo.ItemStack{}, err
		} else if ok {
			return slot, stack, nil
		}
		if slot, ok := firstEmptyMainSlot(record, 3, 8); ok {
			return insertPickup(record, placement, slot)
		}
	}
	if slot, stack, ok, err := stackPickupInRange(
		record,
		placement,
		definition.SlotStart,
		definition.SlotEnd,
	); err != nil {
		return 0, dnfrepo.ItemStack{}, err
	} else if ok {
		return slot, stack, nil
	}
	if slot, ok := firstEmptyMainSlot(record, definition.SlotStart, definition.SlotEnd); ok {
		return insertPickup(record, placement, slot)
	}
	return 0, dnfrepo.ItemStack{}, fmt.Errorf(
		"%w: item=%d range=%d..%d",
		ErrPickupInventoryFull,
		definition.ItemID,
		definition.SlotStart,
		definition.SlotEnd,
	)
}

func stackPickupInRange(
	record *dnfrepo.InventoryRecord,
	placement PickupItemPlacement,
	start int16,
	end int16,
) (uint16, dnfrepo.ItemStack, bool, error) {
	definition := placement.Definition
	if definition.Kind != PickupItemStackable {
		return 0, dnfrepo.ItemStack{}, false, nil
	}
	for slot := start; slot <= end; slot++ {
		key := mainSlotKey(uint16(slot))
		stack, exists := record.Slots[key]
		if !exists || stack.ItemID != int64(definition.ItemID) || stack.Bind || stack.Count < 0 {
			continue
		}
		if placement.NormalizeExisting != nil {
			var err error
			stack, err = placement.NormalizeExisting(stack)
			if err != nil {
				return 0, dnfrepo.ItemStack{}, false, err
			}
		}
		amount := int64(placement.Amount)
		if stack.Count > math.MaxInt64-amount ||
			(definition.StackLimit > 0 && stack.Count > definition.StackLimit-amount) {
			continue
		}
		stack.Count += amount
		record.Slots[key] = stack
		return uint16(slot), cloneStack(stack), true, nil
	}
	return 0, dnfrepo.ItemStack{}, false, nil
}

func firstEmptyMainSlot(record *dnfrepo.InventoryRecord, start int16, end int16) (int16, bool) {
	for slot := start; slot <= end; slot++ {
		if _, occupied := record.Slots[mainSlotKey(uint16(slot))]; !occupied {
			return slot, true
		}
	}
	return 0, false
}

func insertPickup(
	record *dnfrepo.InventoryRecord,
	placement PickupItemPlacement,
	slot int16,
) (uint16, dnfrepo.ItemStack, error) {
	if placement.BuildNew == nil {
		return 0, dnfrepo.ItemStack{}, ErrPickupItemInvalid
	}
	stack, err := placement.BuildNew(slot)
	if err != nil {
		return 0, dnfrepo.ItemStack{}, err
	}
	if stack.ItemID != int64(placement.Definition.ItemID) ||
		stack.Count != int64(placement.Amount) {
		return 0, dnfrepo.ItemStack{}, ErrPickupItemInvalid
	}
	record.Slots[mainSlotKey(uint16(slot))] = cloneStack(stack)
	return uint16(slot), cloneStack(stack), nil
}
