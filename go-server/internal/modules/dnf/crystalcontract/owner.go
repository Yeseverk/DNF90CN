// Package crystalcontract owns the durable rules for 晶之契约: the selected
// cube family and the atomic one-cube consumption reported by the current EXE.
package crystalcontract

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/premium"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	SelectionNone  int8 = -1
	SelectionCount      = 6

	selectionStatKey = "premium_crystal_selection"
)

var (
	ErrRepositoriesUnavailable = errors.New("crystal contract repositories are unavailable")
	ErrAccountRequired         = errors.New("crystal contract account is required")
	ErrCharacterRequired       = errors.New("crystal contract character is required")
	ErrAccountNotFound         = errors.New("crystal contract account was not found")
	ErrCharacterNotFound       = errors.New("crystal contract character was not found")
	ErrCharacterOwnerMismatch  = errors.New("crystal contract character owner mismatch")
	ErrContractInactive        = errors.New("crystal contract is inactive")
	ErrSelectionInvalid        = errors.New("crystal contract selection is invalid")
	ErrCubeUnavailable         = errors.New("selected crystal contract cube is unavailable")
	ErrCubeRequestMismatch     = errors.New("crystal contract cube request does not match the selected cube")
)

// Catalog is the six current-client cube item IDs in native selection order:
// black, white, red, blue, clear, gold. The bridge builds it from the active
// runtime PVF rather than accepting item IDs from a request.
type Catalog struct {
	cubeItemIDs [SelectionCount]int64
}

func NewCatalog(cubeItemIDs [SelectionCount]int64) (Catalog, error) {
	seen := make(map[int64]struct{}, SelectionCount)
	for index, itemID := range cubeItemIDs {
		if itemID <= 0 {
			return Catalog{}, fmt.Errorf("%w: index=%d item=%d", ErrSelectionInvalid, index, itemID)
		}
		if _, duplicate := seen[itemID]; duplicate {
			return Catalog{}, fmt.Errorf("%w: duplicate item=%d", ErrSelectionInvalid, itemID)
		}
		seen[itemID] = struct{}{}
	}
	return Catalog{cubeItemIDs: cubeItemIDs}, nil
}

func (c Catalog) ItemID(selection int8) (int64, bool) {
	if selection < 0 || int(selection) >= len(c.cubeItemIDs) {
		return 0, false
	}
	itemID := c.cubeItemIDs[selection]
	return itemID, itemID > 0
}

func (c Catalog) Selection(itemID int64) (int8, bool) {
	for index, candidate := range c.cubeItemIDs {
		if candidate == itemID && candidate > 0 {
			return int8(index), true
		}
	}
	return SelectionNone, false
}

// AccountSlot returns the current-client crystal warehouse slot for one
// selection. The client fixes the six cube families at list-0 slots 354..359
// in the same black, white, red, blue, clear, gold order as the PVF catalog.
func (c Catalog) AccountSlot(selection int8) (int16, bool) {
	if _, valid := c.ItemID(selection); !valid {
		return 0, false
	}
	return dnfrepo.CrystalWarehouseFirstSlot + int16(selection), true
}

type State struct {
	Active     bool
	Selection  int8
	CubeItemID int64
}

type ConsumeResult struct {
	ItemID         int64
	SlotIndex      uint16
	Consumed       int64
	Remaining      int64
	SelectionAfter int8
}

type Owner struct {
	repositories dnfrepo.Group
	catalog      Catalog
}

func NewOwner(repositories dnfrepo.Group, catalog Catalog) (*Owner, error) {
	if repositories.Account == nil ||
		repositories.AccountInventory == nil ||
		repositories.Character == nil ||
		repositories.AccountAssets == nil {
		return nil, ErrRepositoriesUnavailable
	}
	return &Owner{repositories: repositories, catalog: catalog}, nil
}

func (o *Owner) State(
	ctx context.Context,
	accountID string,
	characterID string,
	now time.Time,
) (State, error) {
	accountID, characterID, err := normalizeOwnerIDs(accountID, characterID)
	if err != nil {
		return State{}, err
	}
	account, found, err := o.repositories.Account.Load(ctx, accountID)
	if err != nil {
		return State{}, err
	}
	if !found {
		return State{}, ErrAccountNotFound
	}
	character, found, err := o.repositories.Character.Load(ctx, characterID)
	if err != nil {
		return State{}, err
	}
	if !found {
		return State{}, ErrCharacterNotFound
	}
	if strings.TrimSpace(character.AccountID) != accountID {
		return State{}, ErrCharacterOwnerMismatch
	}
	active := premium.Active(account, premium.TypeCrystal, now)
	if !active {
		return State{Active: false, Selection: SelectionNone}, nil
	}
	selection := storedSelection(character)
	itemID, valid := o.catalog.ItemID(selection)
	if !valid {
		return State{Active: true, Selection: SelectionNone}, nil
	}
	accountInventory, found, err := o.repositories.AccountInventory.Load(ctx, accountID)
	if err != nil {
		return State{}, err
	}
	if !found || !accountInventoryHasSelectedCube(accountInventory, o.catalog, selection, itemID) {
		return State{Active: true, Selection: SelectionNone}, nil
	}
	return State{Active: true, Selection: selection, CubeItemID: itemID}, nil
}

func (o *Owner) Select(
	ctx context.Context,
	accountID string,
	characterID string,
	selection int8,
	now time.Time,
) (State, error) {
	accountID, characterID, err := normalizeOwnerIDs(accountID, characterID)
	if err != nil {
		return State{}, err
	}
	if selection != SelectionNone {
		if _, valid := o.catalog.ItemID(selection); !valid {
			return State{}, ErrSelectionInvalid
		}
	}
	account, found, err := o.repositories.Account.Load(ctx, accountID)
	if err != nil {
		return State{}, err
	}
	if !found {
		return State{}, ErrAccountNotFound
	}
	active := premium.Active(account, premium.TypeCrystal, now)
	if selection != SelectionNone && !active {
		return State{}, ErrContractInactive
	}

	result := State{Active: active, Selection: selection}
	err = o.repositories.AccountAssets.WithinAccountCharacterAssets(
		ctx,
		accountID,
		characterID,
		func(
			accountInventories dnfrepo.AccountInventoryRepository,
			characters dnfrepo.CharacterRepository,
			_ dnfrepo.InventoryRepository,
			_ dnfrepo.EquipmentRepository,
		) error {
			character, characterFound, loadErr := characters.Load(ctx, characterID)
			if loadErr != nil {
				return loadErr
			}
			if !characterFound {
				return ErrCharacterNotFound
			}
			if strings.TrimSpace(character.AccountID) != accountID {
				return ErrCharacterOwnerMismatch
			}
			if selection != SelectionNone {
				itemID, _ := o.catalog.ItemID(selection)
				accountInventory, inventoryFound, inventoryErr := accountInventories.Load(ctx, accountID)
				if inventoryErr != nil {
					return inventoryErr
				}
				if !inventoryFound || !accountInventoryHasSelectedCube(accountInventory, o.catalog, selection, itemID) {
					return ErrCubeUnavailable
				}
				result.CubeItemID = itemID
			}
			storeSelection(&character, selection)
			character.UpdatedAt = now
			return dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats)
		},
	)
	if err != nil {
		return State{}, err
	}
	return result, nil
}

func (o *Owner) Consume(
	ctx context.Context,
	accountID string,
	characterID string,
	slotIndex uint16,
	itemID int64,
	now time.Time,
) (ConsumeResult, error) {
	accountID, characterID, err := normalizeOwnerIDs(accountID, characterID)
	if err != nil {
		return ConsumeResult{}, err
	}
	account, found, err := o.repositories.Account.Load(ctx, accountID)
	if err != nil {
		return ConsumeResult{}, err
	}
	if !found {
		return ConsumeResult{}, ErrAccountNotFound
	}
	if !premium.Active(account, premium.TypeCrystal, now) {
		return ConsumeResult{}, ErrContractInactive
	}

	result := ConsumeResult{
		ItemID:         itemID,
		SlotIndex:      slotIndex,
		Consumed:       1,
		SelectionAfter: SelectionNone,
	}
	err = o.repositories.AccountAssets.WithinAccountCharacterAssets(
		ctx,
		accountID,
		characterID,
		func(
			accountInventories dnfrepo.AccountInventoryRepository,
			characters dnfrepo.CharacterRepository,
			_ dnfrepo.InventoryRepository,
			_ dnfrepo.EquipmentRepository,
		) error {
			character, characterFound, loadErr := characters.Load(ctx, characterID)
			if loadErr != nil {
				return loadErr
			}
			if !characterFound {
				return ErrCharacterNotFound
			}
			if strings.TrimSpace(character.AccountID) != accountID {
				return ErrCharacterOwnerMismatch
			}
			selection := storedSelection(character)
			selectedItemID, valid := o.catalog.ItemID(selection)
			if !valid || selectedItemID != itemID {
				return ErrCubeRequestMismatch
			}
			selectedSlot, valid := o.catalog.AccountSlot(selection)
			if !valid || slotIndex != uint16(selectedSlot) {
				return ErrCubeRequestMismatch
			}
			accountInventory, inventoryFound, inventoryErr := accountInventories.Load(ctx, accountID)
			if inventoryErr != nil {
				return inventoryErr
			}
			if !inventoryFound {
				return ErrCubeUnavailable
			}
			key := dnfrepo.AccountSharedInventorySlotKey(selectedSlot)
			stack, stackFound := accountInventory.Slots[key]
			if !stackFound || stack.ItemID != itemID || stack.Count <= 0 {
				return ErrCubeUnavailable
			}
			stack.Count--
			result.Remaining = stack.Count
			if stack.Count == 0 {
				delete(accountInventory.Slots, key)
			} else {
				refreshStackCount(&stack)
				accountInventory.Slots[key] = stack
			}
			accountInventory.UpdatedAt = now
			if err := accountInventories.Save(ctx, accountInventory); err != nil {
				return err
			}
			if stack.Count > 0 {
				result.SelectionAfter = selection
				return nil
			}
			storeSelection(&character, SelectionNone)
			character.UpdatedAt = now
			return dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats)
		},
	)
	if err != nil {
		return ConsumeResult{}, err
	}
	return result, nil
}

func normalizeOwnerIDs(accountID string, characterID string) (string, string, error) {
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	if accountID == "" {
		return "", "", ErrAccountRequired
	}
	if characterID == "" {
		return "", "", ErrCharacterRequired
	}
	return accountID, characterID, nil
}

func storedSelection(character dnfrepo.CharacterRecord) int8 {
	if character.Stats == nil {
		return SelectionNone
	}
	value, found := character.Stats[selectionStatKey]
	if !found || value < 0 || value >= SelectionCount {
		return SelectionNone
	}
	return int8(value)
}

func storeSelection(character *dnfrepo.CharacterRecord, selection int8) {
	if character.Stats == nil {
		character.Stats = make(map[string]int64, 8)
	}
	character.Stats[selectionStatKey] = int64(selection)
}

func accountInventoryHasSelectedCube(
	inventory dnfrepo.AccountInventoryRecord,
	catalog Catalog,
	selection int8,
	itemID int64,
) bool {
	slot, valid := catalog.AccountSlot(selection)
	if !valid {
		return false
	}
	stack, found := inventory.Slots[dnfrepo.AccountSharedInventorySlotKey(slot)]
	return found && stack.ItemID == itemID && stack.Count > 0
}

func refreshStackCount(stack *dnfrepo.ItemStack) {
	if stack == nil {
		return
	}
	if len(stack.RawEntry) >= 10 {
		binary.LittleEndian.PutUint32(stack.RawEntry[6:10], uint32(stack.Count))
	}
	if stack.Extra != nil {
		stack.Extra["count"] = strconv.FormatInt(stack.Count, 10)
	}
}
