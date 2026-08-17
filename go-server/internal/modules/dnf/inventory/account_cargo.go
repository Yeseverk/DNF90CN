package inventory

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// moveAccountCargo moves real list-12 rows between the selected character and
// the account owner.  AccountInventory is deliberately the sole durable owner
// of list 12; a character Warehouse entry with a 12:* key is old residue and
// is never read or written here.
func (o *Owner) moveAccountCargo(ctx context.Context, characterID string, cmd Command) (MoveResult, error) {
	accountID := strings.TrimSpace(cmd.AccountID)
	if accountID == "" {
		return MoveResult{}, ErrAccountRequired
	}
	if o == nil || o.repo == nil || o.accountRepo == nil || o.accountItems == nil || o.accounts == nil {
		return MoveResult{}, ErrAccountSharedOwnerUnavailable
	}
	account, found, err := o.accounts.Load(ctx, accountID)
	if err != nil {
		return MoveResult{}, err
	}
	if !found {
		return MoveResult{}, ErrAccountRequired
	}
	capacity, created := accountCargoCapacity(account)
	if !created {
		return MoveResult{}, ErrAccountCargoNotCreated
	}
	if err := checkAccountCargoSlot(cmd.SourceListType, cmd.SourceSlotIndex, capacity); err != nil {
		return MoveResult{}, err
	}
	if err := checkAccountCargoSlot(cmd.DestinationListType, cmd.DestinationSlotIndex, capacity); err != nil {
		return MoveResult{}, err
	}

	var result MoveResult
	err = o.accountItems.WithinAccountCharacterItems(ctx, accountID, characterID, func(accounts dnfrepo.AccountInventoryRepository, characters dnfrepo.InventoryRepository) error {
		character, exists, err := characters.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrInventoryNotFound
		}
		cargo, exists, err := accounts.Load(ctx, accountID)
		if err != nil {
			return err
		}
		if !exists {
			cargo = dnfrepo.AccountInventoryRecord{AccountID: accountID}
		}
		character = dnfrepo.CloneInventory(character)
		cargo = dnfrepo.CloneAccountInventory(cargo)
		character.CharacterID = characterID
		cargo.AccountID = accountID
		if character.Slots == nil {
			character.Slots = make(map[string]dnfrepo.ItemStack)
		}
		if character.Warehouse == nil {
			character.Warehouse = make(map[string]dnfrepo.ItemStack)
		}
		if cargo.Slots == nil {
			cargo.Slots = make(map[string]dnfrepo.ItemStack)
		}

		srcItems, srcField, sourceCargo, err := accountCargoItemMap(&character, &cargo, cmd.SourceListType)
		if err != nil {
			return err
		}
		dstItems, dstField, destinationCargo, err := accountCargoItemMap(&character, &cargo, cmd.DestinationListType)
		if err != nil {
			return err
		}
		result, err = moveAccountCargoMaps(characterID, cmd, srcItems, dstItems)
		if err != nil || !result.Changed {
			return err
		}
		return saveAccountCargoMove(ctx, accounts, characters, character, cargo, srcField, dstField, sourceCargo, destinationCargo, result)
	})
	if err != nil {
		return MoveResult{}, err
	}
	return result, nil
}

func accountCargoCapacity(account dnfrepo.AccountRecord) (int16, bool) {
	metadata := account.Metadata
	level, err := strconv.ParseInt(strings.TrimSpace(metadata["account_cargo_level"]), 10, 64)
	if err != nil || level <= 0 || level > int64(^uint16(0)>>1) {
		return 0, false
	}
	created := strings.EqualFold(strings.TrimSpace(metadata["account_cargo_created"]), "true") || level > 0
	return int16(level), created
}

func checkAccountCargoSlot(listType byte, slot int16, capacity int16) error {
	if listType != listTypeAccountCargo {
		return nil
	}
	if slot < 0 || slot >= capacity {
		return fmt.Errorf("%w: slot=%d capacity=%d", ErrAccountCargoSlotOutOfRange, slot, capacity)
	}
	return nil
}

func accountCargoItemMap(
	character *dnfrepo.InventoryRecord,
	cargo *dnfrepo.AccountInventoryRecord,
	listType byte,
) (map[string]dnfrepo.ItemStack, dnfrepo.InventoryField, bool, error) {
	switch listType {
	case listTypeAccountCargo:
		return cargo.Slots, "", true, nil
	case listTypeMain, listTypePersonalCargo:
		items, field := itemMapForList(character, listType)
		return items, field, false, nil
	default:
		return nil, "", false, fmt.Errorf("%w: account cargo move list=%d", ErrUnsupportedList, listType)
	}
}

func moveAccountCargoMaps(characterID string, cmd Command, srcItems, dstItems map[string]dnfrepo.ItemStack) (MoveResult, error) {
	srcKey := slotKey(cmd.SourceListType, cmd.SourceSlotIndex)
	dstKey := slotKey(cmd.DestinationListType, cmd.DestinationSlotIndex)
	result := MoveResult{
		CharacterID:          characterID,
		SourceListType:       cmd.SourceListType,
		SourceSlotIndex:      cmd.SourceSlotIndex,
		DestinationListType:  cmd.DestinationListType,
		DestinationSlotIndex: cmd.DestinationSlotIndex,
		MoveCount:            int64(cmd.MoveCount),
		Mode:                 "noop",
	}
	if cmd.SourceListType == cmd.DestinationListType && srcKey == dstKey {
		return result, nil
	}
	source, sourceOK := srcItems[srcKey]
	destination, destinationOK := dstItems[dstKey]
	if !sourceOK && !destinationOK {
		return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
	}
	if sourceOK && source.Count <= 0 {
		return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d count=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex, source.Count)
	}
	if !sourceOK {
		if destination.Count <= 0 {
			return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d count=%d", ErrSlotNotFound, cmd.DestinationListType, cmd.DestinationSlotIndex, destination.Count)
		}
		moveCount := normalizeMoveCount(destination, int64(cmd.MoveCount))
		if moveCount <= 0 {
			return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d count=%d", ErrSlotNotFound, cmd.DestinationListType, cmd.DestinationSlotIndex, destination.Count)
		}
		if canSplitStack(destination, moveCount) {
			moved := cloneStack(destination)
			moved.Count = moveCount
			destination.Count -= moveCount
			dstItems[dstKey] = destination
			srcItems[srcKey] = moved
			result.MoveCount, result.Mode, result.Changed = moveCount, "reverse_split", true
			return result, nil
		}
		delete(dstItems, dstKey)
		srcItems[srcKey] = destination
		result.MoveCount, result.Mode, result.Changed = destination.Count, "reverse_move", true
		return result, nil
	}
	moveCount := normalizeMoveCount(source, int64(cmd.MoveCount))
	if moveCount <= 0 {
		return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d count=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex, source.Count)
	}
	if !destinationOK {
		if canSplitStack(source, moveCount) {
			moved := cloneStack(source)
			moved.Count = moveCount
			source.Count -= moveCount
			srcItems[srcKey] = source
			dstItems[dstKey] = moved
			result.MoveCount, result.Mode, result.Changed = moveCount, "split", true
			return result, nil
		}
		delete(srcItems, srcKey)
		dstItems[dstKey] = source
		result.MoveCount, result.Mode, result.Changed = source.Count, "move", true
		return result, nil
	}
	if canStack(source, destination, moveCount) {
		destination.Count += moveCount
		if source.Count <= moveCount {
			delete(srcItems, srcKey)
		} else {
			source.Count -= moveCount
			srcItems[srcKey] = source
		}
		dstItems[dstKey] = destination
		result.MoveCount, result.Mode, result.Changed = moveCount, "stack", true
		return result, nil
	}
	srcItems[srcKey] = destination
	dstItems[dstKey] = source
	result.MoveCount, result.Mode, result.Changed = source.Count, "swap", true
	return result, nil
}

func saveAccountCargoMove(
	ctx context.Context,
	accounts dnfrepo.AccountInventoryRepository,
	characters dnfrepo.InventoryRepository,
	character dnfrepo.InventoryRecord,
	cargo dnfrepo.AccountInventoryRecord,
	sourceField dnfrepo.InventoryField,
	destinationField dnfrepo.InventoryField,
	sourceCargo bool,
	destinationCargo bool,
	result MoveResult,
) error {
	now := time.Now()
	characterFields := make([]dnfrepo.InventoryField, 0, 2)
	if !sourceCargo {
		characterFields = append(characterFields, sourceField)
	}
	if !destinationCargo && (len(characterFields) == 0 || characterFields[0] != destinationField) {
		characterFields = append(characterFields, destinationField)
	}
	if len(characterFields) > 0 {
		character.UpdatedAt = now
		if err := dnfrepo.SaveInventoryFields(ctx, characters, character, characterFields...); err != nil {
			return err
		}
	}
	if sourceCargo || destinationCargo {
		cargo.UpdatedAt = now
		if err := accounts.Save(ctx, cargo); err != nil {
			return err
		}
	}
	return nil
}
