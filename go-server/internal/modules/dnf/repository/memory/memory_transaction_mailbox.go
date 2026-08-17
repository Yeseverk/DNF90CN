package memory

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"sync"
)

type memoryMailboxAssetUnitOfWork struct {
	mu        sync.Mutex
	sharedMu  *sync.Mutex
	character repository.CharacterRepository
	inventory repository.InventoryRepository
	mailbox   repository.MailboxRepository
}

func (u *memoryMailboxAssetUnitOfWork) WithinMailboxAssets(
	ctx context.Context,
	assetCharacterID string,
	mailboxCharacterID string,
	apply func(repository.CharacterRepository, repository.InventoryRepository, repository.MailboxRepository) error,
) error {
	assetCharacterID = strings.TrimSpace(assetCharacterID)
	mailboxCharacterID = strings.TrimSpace(mailboxCharacterID)
	if assetCharacterID == "" || mailboxCharacterID == "" || apply == nil || u == nil ||
		u.character == nil || u.inventory == nil || u.mailbox == nil {
		return repository.ErrMailboxAssetTransactionUnavailable
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

	character, characterExists, err := u.character.Load(ctx, assetCharacterID)
	if err != nil {
		return err
	}
	inventory, inventoryExists, err := u.inventory.Load(ctx, assetCharacterID)
	if err != nil {
		return err
	}
	mailbox, mailboxExists, err := u.mailbox.Load(ctx, mailboxCharacterID)
	if err != nil {
		return err
	}

	characterTx := &memoryCharacterTransaction{
		characterID: assetCharacterID,
		record:      repository.CloneCharacter(character),
		exists:      characterExists,
	}
	inventoryTx := &memoryInventoryTransaction{
		characterID: assetCharacterID,
		record:      repository.CloneInventory(inventory),
		exists:      inventoryExists,
	}
	mailboxTx := &memoryMailboxTransaction{
		characterID: mailboxCharacterID,
		record:      repository.CloneMailbox(mailbox),
		exists:      mailboxExists,
	}
	if err := apply(characterTx, inventoryTx, mailboxTx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if characterTx.dirty {
		if err := u.character.Save(ctx, characterTx.record); err != nil {
			return errors.Join(err, restoreCharacterRecord(
				u.character,
				assetCharacterID,
				character,
				characterExists,
				true,
			))
		}
	}
	if inventoryTx.dirty {
		if err := u.inventory.Save(ctx, inventoryTx.record); err != nil {
			return errors.Join(
				err,
				restoreInventoryRecordIfDirty(u.inventory, assetCharacterID, inventory, inventoryExists, true),
				restoreCharacterRecord(u.character, assetCharacterID, character, characterExists, characterTx.dirty),
			)
		}
	}
	if mailboxTx.dirty {
		if err := u.mailbox.Save(ctx, mailboxTx.record); err != nil {
			return errors.Join(
				err,
				restoreMailboxRecordIfDirty(u.mailbox, mailboxCharacterID, mailbox, mailboxExists, true),
				restoreInventoryRecordIfDirty(u.inventory, assetCharacterID, inventory, inventoryExists, inventoryTx.dirty),
				restoreCharacterRecord(u.character, assetCharacterID, character, characterExists, characterTx.dirty),
			)
		}
	}
	return nil
}

type memoryMailboxTransaction struct {
	characterID string
	record      repository.MailboxRecord
	exists      bool
	dirty       bool
}

func (tx *memoryMailboxTransaction) Load(ctx context.Context, characterID string) (repository.MailboxRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, tx.characterID, characterID); err != nil {
		return repository.MailboxRecord{}, false, err
	}
	return repository.CloneMailbox(tx.record), tx.exists, nil
}

func (tx *memoryMailboxTransaction) Save(ctx context.Context, record repository.MailboxRecord) error {
	if err := repository.TransactionKeyError(ctx, tx.characterID, record.CharacterID); err != nil {
		return err
	}
	tx.record = repository.CloneMailbox(record)
	tx.exists = true
	tx.dirty = true
	return nil
}

func (tx *memoryMailboxTransaction) SaveFields(ctx context.Context, record repository.MailboxRecord, fields ...repository.MailboxField) error {
	if err := repository.TransactionKeyError(ctx, tx.characterID, record.CharacterID); err != nil {
		return err
	}
	if len(fields) == 0 {
		return nil
	}
	tx.record = repository.CloneMailbox(record)
	tx.exists = true
	tx.dirty = true
	return nil
}

func restoreMailboxRecordIfDirty(
	repo repository.MailboxRepository,
	characterID string,
	record repository.MailboxRecord,
	existed bool,
	dirty bool,
) error {
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
		return errors.New("mailbox rollback cannot remove newly created record")
	}
	return deleter.Delete(context.Background(), characterID)
}
