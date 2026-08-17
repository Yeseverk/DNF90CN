package mysql

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
)

type mysqlMailboxAssetUnitOfWork struct {
	router mysqlRouter
}

func (u *mysqlMailboxAssetUnitOfWork) WithinMailboxAssets(
	ctx context.Context,
	assetCharacterID string,
	mailboxCharacterID string,
	apply func(repository.CharacterRepository, repository.InventoryRepository, repository.MailboxRepository) error,
) error {
	assetCharacterID = strings.TrimSpace(assetCharacterID)
	mailboxCharacterID = strings.TrimSpace(mailboxCharacterID)
	if assetCharacterID == "" || mailboxCharacterID == "" || apply == nil || u == nil {
		return repository.ErrMailboxAssetTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok || len(u.router.writeDBs) == 0 {
		return repository.ErrMailboxAssetTransactionUnavailable
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txRouter := u.router
	txRouter.db = sqlTransactionDB{tx: tx}
	txRouter.readDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.writeDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.lockReads = true
	base := newMySQLGroupFromRouter(txRouter, false)
	characters := &characterSettlementScopedCharacterRepository{
		characterID: assetCharacterID,
		base:        base.Character,
	}
	inventories := &characterSettlementScopedInventoryRepository{
		characterID: assetCharacterID,
		base:        base.Inventory,
	}
	mailboxes := &mailboxAssetScopedMailboxRepository{
		characterID: mailboxCharacterID,
		base:        base.Mailbox,
	}
	if err := apply(characters, inventories, mailboxes); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

type mailboxAssetScopedMailboxRepository struct {
	characterID string
	base        repository.MailboxRepository
}

func (r *mailboxAssetScopedMailboxRepository) Load(ctx context.Context, characterID string) (repository.MailboxRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.MailboxRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *mailboxAssetScopedMailboxRepository) Save(ctx context.Context, record repository.MailboxRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *mailboxAssetScopedMailboxRepository) SaveFields(ctx context.Context, record repository.MailboxRecord, fields ...repository.MailboxField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveMailboxFields(ctx, r.base, record, fields...)
}
