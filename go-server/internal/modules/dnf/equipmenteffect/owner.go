package equipmenteffect

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
	ErrOwnerUnavailable   = errors.New("equipment effect owner is unavailable")
	ErrCharacterRequired  = errors.New("equipment effect character id is required")
	ErrRequestInvalid     = errors.New("equipment effect request is invalid")
	ErrInventoryMissing   = errors.New("equipment effect inventory is missing")
	ErrSourceMissing      = errors.New("equipment effect rune is missing")
	ErrSourceAmbiguous    = errors.New("equipment effect rune source is ambiguous")
	ErrTargetMissing      = errors.New("equipment effect target is missing")
	ErrTargetInvalid      = errors.New("equipment effect target is invalid")
	ErrTargetSealed       = errors.New("equipment effect target is sealed")
	ErrCatalogUnavailable = errors.New("equipment effect item catalog is unavailable")
)

// ItemDefinition is the small, PVF-derived subset needed to validate one
// weapon-effect operation. The bridge owns PVF decoding; this owner owns the
// durable business rule and transaction.
type ItemDefinition struct {
	IsEquipment   bool
	EquipmentType string
	Grade         int64
	StackableType string
	EffectID      uint16
}

type ItemCatalog interface {
	ResolveEquipmentEffectItem(uint32) (ItemDefinition, error)
}

type Command struct {
	CharacterID         string
	RequestedSourceSlot int16
	TargetListType      byte
	TargetSlot          int16
	UpdatedAt           time.Time
}

type Result struct {
	CharacterID          string
	RequestedSourceSlot  int16
	SourceSlot           int16
	SourceRecovered      bool
	SourceItemID         int64
	SourceRemainingCount int64
	SourceRemoved        bool
	TargetListType       byte
	TargetSlot           int16
	TargetItemID         int64
	EffectID             uint16
	TargetStack          dnfrepo.ItemStack
}

// Owner atomically consumes a real [equipment effect] rune and records its
// effect id on one un-equipped weapon in the main inventory.
type Owner struct {
	items   dnfrepo.CharacterItemUnitOfWork
	catalog ItemCatalog
}

func NewOwner(repositories dnfrepo.Group, catalog ItemCatalog) (*Owner, error) {
	if repositories.CharacterItems == nil {
		return nil, ErrOwnerUnavailable
	}
	if catalog == nil {
		return nil, ErrCatalogUnavailable
	}
	return &Owner{items: repositories.CharacterItems, catalog: catalog}, nil
}

func (o *Owner) Apply(ctx context.Context, command Command) (Result, error) {
	if o == nil || o.items == nil || o.catalog == nil {
		return Result{}, ErrOwnerUnavailable
	}
	characterID := strings.TrimSpace(command.CharacterID)
	if characterID == "" {
		return Result{}, ErrCharacterRequired
	}
	if command.RequestedSourceSlot < 0 || command.TargetSlot < 0 ||
		command.TargetListType != dnfrepo.MainInventoryListType {
		return Result{}, ErrRequestInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	now := command.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	result := Result{
		CharacterID:         characterID,
		RequestedSourceSlot: command.RequestedSourceSlot,
		TargetListType:      command.TargetListType,
		TargetSlot:          command.TargetSlot,
	}
	err := o.items.WithinCharacterItems(ctx, characterID, func(inventories dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
		if inventories == nil {
			return ErrOwnerUnavailable
		}
		inventory, found, err := inventories.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(inventory.CharacterID) != characterID {
			return ErrInventoryMissing
		}
		inventory = dnfrepo.CloneInventory(inventory)

		targetKey := inventoryKey(command.TargetListType, command.TargetSlot)
		target, found := inventory.Slots[targetKey]
		if !found || target.ItemID <= 0 {
			return fmt.Errorf("%w: list=%d slot=%d", ErrTargetMissing, command.TargetListType, command.TargetSlot)
		}
		targetDefinition, err := o.definition(target.ItemID)
		if err != nil {
			return err
		}
		if !targetDefinition.IsEquipment || !strings.EqualFold(strings.TrimSpace(targetDefinition.EquipmentType), "[weapon]") || targetDefinition.Grade <= 2 {
			return fmt.Errorf("%w: item=%d", ErrTargetInvalid, target.ItemID)
		}
		if isSealed(target) {
			return fmt.Errorf("%w: item=%d", ErrTargetSealed, target.ItemID)
		}

		sourceSlot, source, recovered, err := o.resolveRuneSource(inventory, command.RequestedSourceSlot)
		if err != nil {
			return err
		}
		sourceDefinition, err := o.definition(source.ItemID)
		if err != nil {
			return err
		}
		if !isEquipmentEffectRune(sourceDefinition) || source.Count <= 0 {
			return fmt.Errorf("%w: slot=%d item=%d", ErrSourceMissing, sourceSlot, source.ItemID)
		}

		if target.Extra == nil {
			target.Extra = make(map[string]string)
		}
		target.Extra["item_kind"] = "equipment"
		target.Extra["equipment_effect_id"] = strconv.FormatUint(uint64(sourceDefinition.EffectID), 10)
		target.Extra["equipment_effect_source_item_id"] = strconv.FormatInt(source.ItemID, 10)
		inventory.Slots[targetKey] = target

		source.Count--
		removed := source.Count == 0
		sourceKey := inventoryKey(dnfrepo.MainInventoryListType, sourceSlot)
		if removed {
			delete(inventory.Slots, sourceKey)
		} else {
			inventory.Slots[sourceKey] = source
		}
		inventory.UpdatedAt = now
		if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}

		result.SourceSlot = sourceSlot
		result.SourceRecovered = recovered
		result.SourceItemID = source.ItemID
		result.SourceRemainingCount = source.Count
		result.SourceRemoved = removed
		result.TargetItemID = target.ItemID
		result.EffectID = sourceDefinition.EffectID
		result.TargetStack = target
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func (o *Owner) resolveRuneSource(inventory dnfrepo.InventoryRecord, requestedSlot int16) (int16, dnfrepo.ItemStack, bool, error) {
	requestedKey := inventoryKey(dnfrepo.MainInventoryListType, requestedSlot)
	if stack, found := inventory.Slots[requestedKey]; found && stack.ItemID > 0 && stack.Count > 0 {
		definition, err := o.definition(stack.ItemID)
		if err != nil {
			return 0, dnfrepo.ItemStack{}, false, err
		}
		if isEquipmentEffectRune(definition) {
			return requestedSlot, stack, false, nil
		}
	}

	// A historical raw-entry slot can remain in this 2018 client after a Cera
	// grant/move. The request then names a non-rune slot although only one real
	// effect rune is present. Recover only that unique rune; never consume a
	// different stack or guess when two candidates exist.
	var (
		candidateSlot  int16
		candidate      dnfrepo.ItemStack
		candidateCount int
	)
	for key, stack := range inventory.Slots {
		listType, slot, ok := parseInventoryKey(key)
		if !ok || listType != dnfrepo.MainInventoryListType || slot < 0 || stack.ItemID <= 0 || stack.Count <= 0 {
			continue
		}
		definition, err := o.definition(stack.ItemID)
		if err != nil {
			return 0, dnfrepo.ItemStack{}, false, err
		}
		if !isEquipmentEffectRune(definition) {
			continue
		}
		candidateSlot = slot
		candidate = stack
		candidateCount++
	}
	if candidateCount == 0 {
		return 0, dnfrepo.ItemStack{}, false, fmt.Errorf("%w: requested_slot=%d", ErrSourceMissing, requestedSlot)
	}
	if candidateCount > 1 {
		return 0, dnfrepo.ItemStack{}, false, fmt.Errorf("%w: requested_slot=%d matches=%d", ErrSourceAmbiguous, requestedSlot, candidateCount)
	}
	return candidateSlot, candidate, true, nil
}

func (o *Owner) definition(itemID int64) (ItemDefinition, error) {
	if itemID <= 0 || itemID > int64(^uint32(0)) {
		return ItemDefinition{}, fmt.Errorf("%w: item=%d", ErrCatalogUnavailable, itemID)
	}
	definition, err := o.catalog.ResolveEquipmentEffectItem(uint32(itemID))
	if err != nil {
		return ItemDefinition{}, fmt.Errorf("%w: item=%d: %v", ErrCatalogUnavailable, itemID, err)
	}
	return definition, nil
}

func isEquipmentEffectRune(definition ItemDefinition) bool {
	return !definition.IsEquipment && definition.EffectID != 0 &&
		strings.Contains(strings.ToLower(definition.StackableType), "equipment effect")
}

func isSealed(stack dnfrepo.ItemStack) bool {
	if stack.Extra == nil {
		return false
	}
	for _, key := range []string{"seal_flag", "seal", "sealed"} {
		value := strings.TrimSpace(stack.Extra[key])
		if value == "" || value == "0" || strings.EqualFold(value, "false") {
			continue
		}
		return true
	}
	return false
}

func inventoryKey(listType byte, slot int16) string {
	return fmt.Sprintf("%d:%d", listType, slot)
}

func parseInventoryKey(key string) (byte, int16, bool) {
	parts := strings.Split(strings.TrimSpace(key), ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	list, err := strconv.ParseUint(parts[0], 10, 8)
	if err != nil {
		return 0, 0, false
	}
	slot, err := strconv.ParseInt(parts[1], 10, 16)
	if err != nil {
		return 0, 0, false
	}
	return byte(list), int16(slot), true
}
