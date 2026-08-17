package memory

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"sync"
)

type memoryAccountCharacterItemUnitOfWork struct {
	mu               sync.Mutex
	sharedMu         *sync.Mutex
	accountInventory repository.AccountInventoryRepository
	inventory        repository.InventoryRepository
}

type memoryAccountCharacterAssetUnitOfWork struct {
	mu               sync.Mutex
	sharedMu         *sync.Mutex
	accountInventory repository.AccountInventoryRepository
	character        repository.CharacterRepository
	inventory        repository.InventoryRepository
	equipment        repository.EquipmentRepository
}

func (u *memoryAccountCharacterItemUnitOfWork) WithinAccountCharacterItems(
	ctx context.Context,
	accountID string,
	characterID string,
	apply func(repository.AccountInventoryRepository, repository.InventoryRepository) error,
) error {
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	if accountID == "" || characterID == "" || apply == nil || u == nil || u.accountInventory == nil || u.inventory == nil {
		return repository.ErrAccountCharacterItemTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	lock := &u.mu
	if u.sharedMu != nil {
		lock = u.sharedMu
	}
	lock.Lock()
	defer lock.Unlock()

	accountRecord, accountExists, err := u.accountInventory.Load(ctx, accountID)
	if err != nil {
		return err
	}
	characterRecord, characterExists, err := u.inventory.Load(ctx, characterID)
	if err != nil {
		return err
	}
	accountTx := &memoryAccountInventoryTransaction{
		accountID: accountID,
		record:    repository.CloneAccountInventory(accountRecord),
		exists:    accountExists,
	}
	characterTx := &memoryInventoryTransaction{
		characterID: characterID,
		record:      repository.CloneInventory(characterRecord),
		exists:      characterExists,
	}
	if err := apply(accountTx, characterTx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Save the character side first. If the account side then fails, restore
	// both preimages so the in-memory implementation has SQL-like atomicity.
	if characterTx.dirty {
		if err := u.inventory.Save(ctx, characterTx.record); err != nil {
			return errors.Join(err, restoreInventoryRecord(u.inventory, characterID, characterRecord, characterExists))
		}
	}
	if accountTx.dirty {
		if err := u.accountInventory.Save(ctx, accountTx.record); err != nil {
			return errors.Join(
				err,
				restoreAccountInventoryRecord(u.accountInventory, accountID, accountRecord, accountExists),
				restoreInventoryRecordIfDirty(u.inventory, characterID, characterRecord, characterExists, characterTx.dirty),
			)
		}
	}
	return nil
}

func (u *memoryAccountCharacterAssetUnitOfWork) WithinAccountCharacterAssets(
	ctx context.Context,
	accountID string,
	characterID string,
	apply func(repository.AccountInventoryRepository, repository.CharacterRepository, repository.InventoryRepository, repository.EquipmentRepository) error,
) error {
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	if accountID == "" || characterID == "" || apply == nil || u == nil ||
		u.accountInventory == nil || u.character == nil || u.inventory == nil || u.equipment == nil {
		return repository.ErrAccountCharacterAssetTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	lock := &u.mu
	if u.sharedMu != nil {
		lock = u.sharedMu
	}
	lock.Lock()
	defer lock.Unlock()

	accountRecord, accountExists, err := u.accountInventory.Load(ctx, accountID)
	if err != nil {
		return err
	}
	characterRecord, characterExists, err := u.character.Load(ctx, characterID)
	if err != nil {
		return err
	}
	inventoryRecord, inventoryExists, err := u.inventory.Load(ctx, characterID)
	if err != nil {
		return err
	}
	equipmentRecord, equipmentExists, err := u.equipment.Load(ctx, characterID)
	if err != nil {
		return err
	}

	accountTx := &memoryAccountInventoryTransaction{
		accountID: accountID,
		record:    repository.CloneAccountInventory(accountRecord),
		exists:    accountExists,
	}
	characterTx := &memoryCharacterTransaction{
		characterID: characterID,
		record:      repository.CloneCharacter(characterRecord),
		exists:      characterExists,
	}
	inventoryTx := &memoryInventoryTransaction{
		characterID: characterID,
		record:      repository.CloneInventory(inventoryRecord),
		exists:      inventoryExists,
	}
	equipmentTx := &memoryEquipmentTransaction{
		characterID: characterID,
		record:      repository.CloneEquipment(equipmentRecord),
		exists:      equipmentExists,
	}
	if err := apply(accountTx, characterTx, inventoryTx, equipmentTx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if accountTx.dirty {
		if err := u.accountInventory.Save(ctx, accountTx.record); err != nil {
			return errors.Join(err, restoreAccountInventoryRecord(u.accountInventory, accountID, accountRecord, accountExists))
		}
	}
	if characterTx.dirty {
		if err := u.character.Save(ctx, characterTx.record); err != nil {
			return errors.Join(
				err,
				restoreCharacterRecord(u.character, characterID, characterRecord, characterExists, true),
				restoreAccountInventoryRecordIfDirty(u.accountInventory, accountID, accountRecord, accountExists, accountTx.dirty),
			)
		}
	}
	if inventoryTx.dirty {
		if err := u.inventory.Save(ctx, inventoryTx.record); err != nil {
			return errors.Join(
				err,
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventoryRecord, inventoryExists, true),
				restoreCharacterRecord(u.character, characterID, characterRecord, characterExists, characterTx.dirty),
				restoreAccountInventoryRecordIfDirty(u.accountInventory, accountID, accountRecord, accountExists, accountTx.dirty),
			)
		}
	}
	if equipmentTx.dirty {
		if err := u.equipment.Save(ctx, equipmentTx.record); err != nil {
			return errors.Join(
				err,
				restoreEquipmentRecordIfDirty(u.equipment, characterID, equipmentRecord, equipmentExists, true),
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventoryRecord, inventoryExists, inventoryTx.dirty),
				restoreCharacterRecord(u.character, characterID, characterRecord, characterExists, characterTx.dirty),
				restoreAccountInventoryRecordIfDirty(u.accountInventory, accountID, accountRecord, accountExists, accountTx.dirty),
			)
		}
	}
	return nil
}

type memoryAccountInventoryTransaction struct {
	accountID string
	record    repository.AccountInventoryRecord
	exists    bool
	dirty     bool
}

func (tx *memoryAccountInventoryTransaction) Load(ctx context.Context, accountID string) (repository.AccountInventoryRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, tx.accountID, accountID); err != nil {
		return repository.AccountInventoryRecord{}, false, err
	}
	return repository.CloneAccountInventory(tx.record), tx.exists, nil
}

func (tx *memoryAccountInventoryTransaction) Save(ctx context.Context, record repository.AccountInventoryRecord) error {
	if err := repository.TransactionKeyError(ctx, tx.accountID, record.AccountID); err != nil {
		return err
	}
	tx.record = repository.CloneAccountInventory(record)
	tx.exists = true
	tx.dirty = true
	return nil
}

func restoreAccountInventoryRecord(repo repository.AccountInventoryRepository, accountID string, record repository.AccountInventoryRecord, existed bool) error {
	if repo == nil {
		return nil
	}
	if existed {
		return repo.Save(context.Background(), record)
	}
	deleter, ok := repo.(interface {
		Delete(context.Context, string) error
	})
	if !ok {
		return errors.New("account inventory rollback cannot remove newly created record")
	}
	return deleter.Delete(context.Background(), accountID)
}

func restoreAccountInventoryRecordIfDirty(repo repository.AccountInventoryRepository, accountID string, record repository.AccountInventoryRecord, existed bool, dirty bool) error {
	if !dirty {
		return nil
	}
	return restoreAccountInventoryRecord(repo, accountID, record, existed)
}
