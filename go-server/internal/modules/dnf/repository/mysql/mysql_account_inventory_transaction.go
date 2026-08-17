package mysql

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
)

type mysqlAccountCharacterItemUnitOfWork struct {
	router mysqlRouter
}

type mysqlAccountCharacterAssetUnitOfWork struct {
	router mysqlRouter
}

func (u *mysqlAccountCharacterItemUnitOfWork) WithinAccountCharacterItems(
	ctx context.Context,
	accountID string,
	characterID string,
	apply func(repository.AccountInventoryRepository, repository.InventoryRepository) error,
) error {
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	if accountID == "" || characterID == "" || apply == nil {
		return repository.ErrAccountCharacterItemTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrAccountCharacterItemTransactionUnavailable
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	// Account and character keys may route to different logical databases,
	// but both tables live on the same MySQL server and share this sql.Tx.
	txRouter := u.router
	txRouter.db = sqlTransactionDB{tx: tx}
	txRouter.readDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.writeDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.lockReads = true
	txGroup := newMySQLGroupFromRouter(txRouter, false)
	accounts := &accountInventoryScopedRepository{accountID: accountID, base: txGroup.AccountInventory}
	characters := &accountCharacterInventoryScopedRepository{characterID: characterID, base: txGroup.Inventory}
	if err := apply(accounts, characters); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (u *mysqlAccountCharacterAssetUnitOfWork) WithinAccountCharacterAssets(
	ctx context.Context,
	accountID string,
	characterID string,
	apply func(repository.AccountInventoryRepository, repository.CharacterRepository, repository.InventoryRepository, repository.EquipmentRepository) error,
) error {
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	if accountID == "" || characterID == "" || apply == nil {
		return repository.ErrAccountCharacterAssetTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrAccountCharacterAssetTransactionUnavailable
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	// Account inventory and character rows can hash to different logical
	// databases. They still share one physical MySQL transaction, so keep the
	// full write plan while forcing locked reads through the transaction.
	txRouter := u.router
	txRouter.db = sqlTransactionDB{tx: tx}
	txRouter.readDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.writeDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.lockReads = true
	txGroup := newMySQLGroupFromRouter(txRouter, false)
	accounts := &accountInventoryScopedRepository{accountID: accountID, base: txGroup.AccountInventory}
	characters := &rentalScopedCharacterRepository{characterID: characterID, base: txGroup.Character}
	inventories := &rentalScopedInventoryRepository{characterID: characterID, base: txGroup.Inventory}
	equipment := &rentalScopedEquipmentRepository{characterID: characterID, base: txGroup.Equipment}
	if err := apply(accounts, characters, inventories, equipment); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

type accountInventoryScopedRepository struct {
	accountID string
	base      repository.AccountInventoryRepository
}

func (r *accountInventoryScopedRepository) Load(ctx context.Context, accountID string) (repository.AccountInventoryRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.accountID, accountID); err != nil {
		return repository.AccountInventoryRecord{}, false, err
	}
	return r.base.Load(ctx, accountID)
}

func (r *accountInventoryScopedRepository) Save(ctx context.Context, record repository.AccountInventoryRecord) error {
	if err := repository.TransactionKeyError(ctx, r.accountID, record.AccountID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

type accountCharacterInventoryScopedRepository struct {
	characterID string
	base        repository.InventoryRepository
}

func (r *accountCharacterInventoryScopedRepository) Load(ctx context.Context, characterID string) (repository.InventoryRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.InventoryRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *accountCharacterInventoryScopedRepository) Save(ctx context.Context, record repository.InventoryRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *accountCharacterInventoryScopedRepository) SaveFields(ctx context.Context, record repository.InventoryRecord, fields ...repository.InventoryField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveInventoryFields(ctx, r.base, record, fields...)
}
