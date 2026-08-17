package mysql

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
)

type mysqlCharacterPetUnitOfWork struct {
	router mysqlRouter
}

func (u *mysqlCharacterPetUnitOfWork) WithinCharacterPets(
	ctx context.Context,
	characterID string,
	apply func(repository.InventoryRepository, repository.EquipmentRepository, repository.PetRepository) error,
) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil || u == nil {
		return repository.ErrCharacterPetTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrCharacterPetTransactionUnavailable
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	writeDatabase := u.router.writeDBs[pickDatabase(characterID, len(u.router.writeDBs))]
	txRouter := u.router
	txRouter.db = sqlTransactionDB{tx: tx}
	txRouter.readDBs = []string{writeDatabase}
	txRouter.writeDBs = []string{writeDatabase}
	txRouter.lockReads = true
	txGroup := newMySQLGroupFromRouter(txRouter, false)
	inventory := &characterPetScopedInventoryRepository{characterID: characterID, base: txGroup.Inventory}
	equipment := &characterPetScopedEquipmentRepository{characterID: characterID, base: txGroup.Equipment}
	pets := &characterPetScopedPetRepository{characterID: characterID, base: txGroup.Pet}
	if err := apply(inventory, equipment, pets); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

type characterPetScopedInventoryRepository struct {
	characterID string
	base        repository.InventoryRepository
}

func (r *characterPetScopedInventoryRepository) Load(ctx context.Context, characterID string) (repository.InventoryRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.InventoryRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterPetScopedInventoryRepository) Save(ctx context.Context, record repository.InventoryRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *characterPetScopedInventoryRepository) SaveFields(ctx context.Context, record repository.InventoryRecord, fields ...repository.InventoryField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveInventoryFields(ctx, r.base, record, fields...)
}

type characterPetScopedEquipmentRepository struct {
	characterID string
	base        repository.EquipmentRepository
}

func (r *characterPetScopedEquipmentRepository) Load(ctx context.Context, characterID string) (repository.EquipmentRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.EquipmentRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterPetScopedEquipmentRepository) Save(ctx context.Context, record repository.EquipmentRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *characterPetScopedEquipmentRepository) SaveFields(ctx context.Context, record repository.EquipmentRecord, fields ...repository.EquipmentField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveEquipmentFields(ctx, r.base, record, fields...)
}

type characterPetScopedPetRepository struct {
	characterID string
	base        repository.PetRepository
}

func (r *characterPetScopedPetRepository) Load(ctx context.Context, characterID string) (repository.PetRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.PetRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterPetScopedPetRepository) Save(ctx context.Context, record repository.PetRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *characterPetScopedPetRepository) SaveFields(ctx context.Context, record repository.PetRecord, fields ...repository.PetField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SavePetFields(ctx, r.base, record, fields...)
}
