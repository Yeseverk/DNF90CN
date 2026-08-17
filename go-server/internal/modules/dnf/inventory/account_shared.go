package inventory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var ErrAccountSharedMigrationConflict = errors.New("account-shared inventory migration conflict")

// MigrateAccountSharedSlots atomically moves stale character-owned crystal and
// soul slots into their account owner. Callers may invoke it before producing
// an op13 list-0 snapshot; repeated calls are safe once the migration commits.
func (o *Owner) MigrateAccountSharedSlots(ctx context.Context, accountID, characterID string) error {
	if o == nil || o.repo == nil || o.accountRepo == nil || o.accountItems == nil {
		return ErrAccountSharedOwnerUnavailable
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return ErrAccountRequired
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return ErrCharacterRequired
	}

	return o.accountItems.WithinAccountCharacterItems(ctx, accountID, characterID, func(accounts dnfrepo.AccountInventoryRepository, characters dnfrepo.InventoryRepository) error {
		character, exists, err := characters.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrInventoryNotFound
		}
		account, _, err := accounts.Load(ctx, accountID)
		if err != nil {
			return err
		}

		character = dnfrepo.CloneInventory(character)
		account = dnfrepo.CloneAccountInventory(account)
		account.AccountID = accountID
		if character.Slots == nil {
			character.Slots = make(map[string]dnfrepo.ItemStack)
		}
		if account.Slots == nil {
			account.Slots = make(map[string]dnfrepo.ItemStack)
		}

		// Preflight the complete range before staging either owner. A single
		// conflicting slot must leave all twelve slots byte-for-byte unchanged.
		for slot := dnfrepo.CrystalWarehouseFirstSlot; slot <= dnfrepo.SoulWarehouseLastSlot; slot++ {
			key := dnfrepo.AccountSharedInventorySlotKey(slot)
			legacy, legacyExists := character.Slots[key]
			current, currentExists := account.Slots[key]
			if legacyExists && currentExists && !accountSharedStacksEqual(legacy, current) {
				return fmt.Errorf("%w: slot=%d", ErrAccountSharedMigrationConflict, slot)
			}
		}

		accountDirty := false
		characterDirty := false
		for slot := dnfrepo.CrystalWarehouseFirstSlot; slot <= dnfrepo.SoulWarehouseLastSlot; slot++ {
			key := dnfrepo.AccountSharedInventorySlotKey(slot)
			legacy, legacyExists := character.Slots[key]
			if !legacyExists {
				continue
			}
			if _, currentExists := account.Slots[key]; !currentExists {
				account.Slots[key] = cloneStack(legacy)
				accountDirty = true
			}
			delete(character.Slots, key)
			characterDirty = true
		}

		if !accountDirty && !characterDirty {
			return nil
		}
		now := time.Now()
		if characterDirty {
			character.UpdatedAt = now
			if err := dnfrepo.SaveInventoryFields(ctx, characters, character, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
		}
		if accountDirty {
			account.UpdatedAt = now
			if err := accounts.Save(ctx, account); err != nil {
				return err
			}
		}
		return nil
	})
}

func accountSharedStacksEqual(left, right dnfrepo.ItemStack) bool {
	return left.ItemID == right.ItemID &&
		left.Count == right.Count &&
		left.Bind == right.Bind &&
		left.ExpireAt.Equal(right.ExpireAt) &&
		bytes.Equal(left.RawEntry, right.RawEntry) &&
		maps.Equal(left.Extra, right.Extra)
}

// LoadWithAccountSharedSlots returns the character inventory with account-owned
// crystal/soul slots overlaid at their unchanged list-0 wire addresses.
func (o *Owner) LoadWithAccountSharedSlots(ctx context.Context, accountID string, selectedCharacterID uint16) (dnfrepo.InventoryRecord, bool, error) {
	if o == nil || o.repo == nil || o.accountRepo == nil {
		return dnfrepo.InventoryRecord{}, false, ErrAccountSharedOwnerUnavailable
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return dnfrepo.InventoryRecord{}, false, ErrAccountRequired
	}
	if selectedCharacterID == 0 {
		return dnfrepo.InventoryRecord{}, false, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(selectedCharacterID), 10)
	character, exists, err := o.repo.Load(ctx, characterID)
	if err != nil || !exists {
		return dnfrepo.InventoryRecord{}, exists, err
	}
	account, _, err := o.accountRepo.Load(ctx, accountID)
	if err != nil {
		return dnfrepo.InventoryRecord{}, false, err
	}
	return dnfrepo.MergeAccountSharedInventory(character, account), true, nil
}

func (o *Owner) moveAccountShared(ctx context.Context, characterID string, cmd Command) (MoveResult, error) {
	if strings.TrimSpace(cmd.AccountID) == "" {
		return MoveResult{}, ErrAccountRequired
	}
	if o.accountRepo == nil || o.accountItems == nil {
		return MoveResult{}, ErrAccountSharedOwnerUnavailable
	}
	if !o.inAccountItemTx {
		var result MoveResult
		err := o.accountItems.WithinAccountCharacterItems(ctx, cmd.AccountID, characterID, func(accounts dnfrepo.AccountInventoryRepository, characters dnfrepo.InventoryRepository) error {
			txOwner := *o
			txOwner.accountRepo = accounts
			txOwner.repo = characters
			txOwner.inAccountItemTx = true
			var err error
			result, err = txOwner.moveAccountShared(ctx, characterID, cmd)
			return err
		})
		return result, err
	}

	character, characterExists, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return MoveResult{}, err
	}
	if !characterExists {
		return MoveResult{}, ErrInventoryNotFound
	}
	character = dnfrepo.CloneInventory(character)
	if character.Slots == nil {
		character.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if character.Warehouse == nil {
		character.Warehouse = make(map[string]dnfrepo.ItemStack)
	}

	account, accountExists, err := o.accountRepo.Load(ctx, cmd.AccountID)
	if err != nil {
		return MoveResult{}, err
	}
	if !accountExists {
		account = dnfrepo.AccountInventoryRecord{AccountID: strings.TrimSpace(cmd.AccountID)}
	}
	account = dnfrepo.CloneAccountInventory(account)
	account.AccountID = strings.TrimSpace(cmd.AccountID)
	if account.Slots == nil {
		account.Slots = make(map[string]dnfrepo.ItemStack)
	}

	sourceAccountOwned := dnfrepo.IsAccountSharedInventorySlot(cmd.SourceListType, cmd.SourceSlotIndex)
	destinationAccountOwned := dnfrepo.IsAccountSharedInventorySlot(cmd.DestinationListType, cmd.DestinationSlotIndex)
	srcItems, srcField := accountSharedItemMap(&character, &account, cmd.SourceListType, sourceAccountOwned)
	dstItems, dstField := accountSharedItemMap(&character, &account, cmd.DestinationListType, destinationAccountOwned)
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
	if sourceAccountOwned == destinationAccountOwned && srcField == dstField && srcKey == dstKey {
		return result, nil
	}

	source, sourceOK := srcItems[srcKey]
	destination, destinationOK := dstItems[dstKey]
	if !sourceOK {
		if !destinationOK {
			return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
		}
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
			result.MoveCount = moveCount
			result.Mode = "reverse_split"
			if cmd.SourceListType == cmd.DestinationListType {
				result, err = withAccountSharedCountRefresh(character, account, result, cmd.SourceListType)
				if err != nil {
					return MoveResult{}, err
				}
			}
			return o.saveAccountSharedMove(ctx, character, account, srcField, dstField, sourceAccountOwned, destinationAccountOwned, result)
		}
		delete(dstItems, dstKey)
		srcItems[srcKey] = destination
		result.MoveCount = destination.Count
		result.Mode = "reverse_move"
		return o.saveAccountSharedMove(ctx, character, account, srcField, dstField, sourceAccountOwned, destinationAccountOwned, result)
	}
	if source.Count <= 0 {
		return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d count=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex, source.Count)
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
			result.MoveCount = moveCount
			result.Mode = "split"
			if cmd.SourceListType == cmd.DestinationListType {
				result, err = withAccountSharedCountRefresh(character, account, result, cmd.SourceListType)
				if err != nil {
					return MoveResult{}, err
				}
			}
			return o.saveAccountSharedMove(ctx, character, account, srcField, dstField, sourceAccountOwned, destinationAccountOwned, result)
		}
		delete(srcItems, srcKey)
		dstItems[dstKey] = source
		result.MoveCount = source.Count
		result.Mode = "move"
		return o.saveAccountSharedMove(ctx, character, account, srcField, dstField, sourceAccountOwned, destinationAccountOwned, result)
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
		result.MoveCount = moveCount
		result.Mode = "stack"
		result, err = withAccountSharedCountRefresh(
			character,
			account,
			result,
			cmd.SourceListType,
			cmd.DestinationListType,
		)
		if err != nil {
			return MoveResult{}, err
		}
		return o.saveAccountSharedMove(ctx, character, account, srcField, dstField, sourceAccountOwned, destinationAccountOwned, result)
	}

	srcItems[srcKey] = destination
	dstItems[dstKey] = source
	result.MoveCount = source.Count
	result.Mode = "swap"
	return o.saveAccountSharedMove(ctx, character, account, srcField, dstField, sourceAccountOwned, destinationAccountOwned, result)
}

func accountSharedItemMap(
	character *dnfrepo.InventoryRecord,
	account *dnfrepo.AccountInventoryRecord,
	listType byte,
	accountOwned bool,
) (map[string]dnfrepo.ItemStack, dnfrepo.InventoryField) {
	if accountOwned {
		return account.Slots, dnfrepo.InventoryFieldSlots
	}
	return itemMapForList(character, listType)
}

func (o *Owner) saveAccountSharedMove(
	ctx context.Context,
	character dnfrepo.InventoryRecord,
	account dnfrepo.AccountInventoryRecord,
	sourceField dnfrepo.InventoryField,
	destinationField dnfrepo.InventoryField,
	sourceAccountOwned bool,
	destinationAccountOwned bool,
	result MoveResult,
) (MoveResult, error) {
	now := time.Now()
	accountDirty := sourceAccountOwned || destinationAccountOwned
	characterFields := make([]dnfrepo.InventoryField, 0, 2)
	if !sourceAccountOwned {
		characterFields = append(characterFields, sourceField)
	}
	if !destinationAccountOwned && (len(characterFields) == 0 || destinationField != characterFields[0]) {
		characterFields = append(characterFields, destinationField)
	}
	if len(characterFields) != 0 {
		character.UpdatedAt = now
		if err := dnfrepo.SaveInventoryFields(ctx, o.repo, character, characterFields...); err != nil {
			return MoveResult{}, err
		}
	}
	if accountDirty {
		account.UpdatedAt = now
		if err := o.accountRepo.Save(ctx, account); err != nil {
			return MoveResult{}, err
		}
	}
	result.Changed = true
	return result, nil
}

// withAccountSharedCountRefresh freezes the authoritative post-mutation views
// while both account and character owners are still inside the same unit of
// work. Explicit stack merges and same-list partial split/reverse-split use
// these full op13 snapshots; cross-list split and ordinary move/swap operations
// continue to rely on their literal op19 ACK.
func withAccountSharedCountRefresh(
	character dnfrepo.InventoryRecord,
	account dnfrepo.AccountInventoryRecord,
	result MoveResult,
	listTypes ...byte,
) (MoveResult, error) {
	result.Refresh = make(map[byte]map[string]dnfrepo.ItemStack, len(listTypes))
	result.RefreshListTypes = make([]byte, 0, len(listTypes))
	for _, listType := range listTypes {
		if _, exists := result.Refresh[listType]; exists {
			continue
		}

		var items map[string]dnfrepo.ItemStack
		switch listType {
		case listTypeMain:
			items = dnfrepo.MergeAccountSharedInventory(character, account).Slots
		case listTypeAvatar:
			items = character.Slots
		case listTypePersonalCargo:
			items = character.Warehouse
		default:
			return MoveResult{}, fmt.Errorf("%w: account-shared stack refresh list=%d", ErrUnsupportedList, listType)
		}

		result.Refresh[listType] = cloneItemListType(items, listType)
		result.RefreshListTypes = append(result.RefreshListTypes, listType)
	}
	return result, nil
}

func cloneItemListType(items map[string]dnfrepo.ItemStack, listType byte) map[string]dnfrepo.ItemStack {
	out := make(map[string]dnfrepo.ItemStack)
	for key, stack := range items {
		keyListType, _, ok := parseSlotKey(key)
		if !ok || keyListType != listType {
			continue
		}
		out[key] = cloneStack(stack)
	}
	return out
}
