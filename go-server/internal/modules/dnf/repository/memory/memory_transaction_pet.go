package memory

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"sync"
)

// memoryCharacterPetUnitOfWork buffers all three pet-owned aggregates and
// commits them while holding the same mutex used by the other character item
// transactions. This prevents hatch/equip/feed operations from racing a
// concurrent inventory or equipment mutation in memory-backed tests.
type memoryCharacterPetUnitOfWork struct {
	mu        sync.Mutex
	sharedMu  *sync.Mutex
	inventory repository.InventoryRepository
	equipment repository.EquipmentRepository
	pets      repository.PetRepository
}

func (u *memoryCharacterPetUnitOfWork) WithinCharacterPets(
	ctx context.Context,
	characterID string,
	apply func(repository.InventoryRepository, repository.EquipmentRepository, repository.PetRepository) error,
) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil || u == nil || u.inventory == nil || u.equipment == nil || u.pets == nil {
		return repository.ErrCharacterPetTransactionUnavailable
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

	inventory, inventoryExists, err := u.inventory.Load(ctx, characterID)
	if err != nil {
		return err
	}
	equipment, equipmentExists, err := u.equipment.Load(ctx, characterID)
	if err != nil {
		return err
	}
	pet, petExists, err := u.pets.Load(ctx, characterID)
	if err != nil {
		return err
	}

	inventoryTx := &memoryInventoryTransaction{
		characterID: characterID,
		record:      repository.CloneInventory(inventory),
		exists:      inventoryExists,
	}
	equipmentTx := &memoryEquipmentTransaction{
		characterID: characterID,
		record:      repository.CloneEquipment(equipment),
		exists:      equipmentExists,
	}
	petTx := &memoryPetTransaction{
		characterID: characterID,
		record:      repository.ClonePet(pet),
		exists:      petExists,
	}
	if err := apply(inventoryTx, equipmentTx, petTx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if inventoryTx.dirty {
		if err := u.inventory.Save(ctx, inventoryTx.record); err != nil {
			return errors.Join(
				err,
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, true),
			)
		}
	}
	if equipmentTx.dirty {
		if err := u.equipment.Save(ctx, equipmentTx.record); err != nil {
			return errors.Join(
				err,
				restoreEquipmentRecordIfDirty(u.equipment, characterID, equipment, equipmentExists, true),
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, inventoryTx.dirty),
			)
		}
	}
	if petTx.dirty {
		if err := u.pets.Save(ctx, petTx.record); err != nil {
			return errors.Join(
				err,
				restorePetRecordIfDirty(u.pets, characterID, pet, petExists, true),
				restoreEquipmentRecordIfDirty(u.equipment, characterID, equipment, equipmentExists, equipmentTx.dirty),
				restoreInventoryRecordIfDirty(u.inventory, characterID, inventory, inventoryExists, inventoryTx.dirty),
			)
		}
	}
	return nil
}

type memoryPetTransaction struct {
	characterID string
	record      repository.PetRecord
	exists      bool
	dirty       bool
}

func (tx *memoryPetTransaction) Load(ctx context.Context, characterID string) (repository.PetRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, tx.characterID, characterID); err != nil {
		return repository.PetRecord{}, false, err
	}
	return repository.ClonePet(tx.record), tx.exists, nil
}

func (tx *memoryPetTransaction) Save(ctx context.Context, record repository.PetRecord) error {
	if err := repository.TransactionKeyError(ctx, tx.characterID, record.CharacterID); err != nil {
		return err
	}
	tx.record = repository.ClonePet(record)
	tx.exists = true
	tx.dirty = true
	return nil
}

func restorePetRecordIfDirty(repo repository.PetRepository, characterID string, record repository.PetRecord, existed bool, dirty bool) error {
	if !dirty {
		return nil
	}
	if existed {
		return repo.Save(context.Background(), record)
	}
	deleter, ok := repo.(interface {
		Delete(context.Context, string) error
	})
	if !ok {
		return errors.New("pet rollback cannot remove newly created record")
	}
	return deleter.Delete(context.Background(), characterID)
}
