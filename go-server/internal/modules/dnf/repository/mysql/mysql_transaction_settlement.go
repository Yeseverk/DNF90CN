package mysql

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
)

// mysqlCharacterSettlementUnitOfWork keeps quest state, progression, wallet,
// inventory and equipment changes for one character in a single SQL
// transaction. Domain code still decides what to grant; this layer only owns
// transactionality and owner scoping.
type mysqlCharacterSettlementUnitOfWork struct {
	router mysqlRouter
}

func (u *mysqlCharacterSettlementUnitOfWork) WithinCharacterSettlement(
	ctx context.Context,
	characterID string,
	apply func(repository.Group) error,
) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil || u == nil {
		return repository.ErrCharacterSettlementTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok || len(u.router.writeDBs) == 0 {
		return repository.ErrCharacterSettlementTransactionUnavailable
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txRouter := u.router
	txRouter.db = sqlTransactionDB{tx: tx}
	// Character settlement can also consume account-owned crystal/soul slots.
	// Keep the full logical database plan inside the same physical SQL
	// transaction so the character and account keys may route independently.
	txRouter.readDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.writeDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.lockReads = true
	base := newMySQLGroupFromRouter(txRouter, false)
	txGroup := repository.Group{
		Account: &characterSettlementAccountRepository{
			characterID: characterID,
			characters:  base.Character,
			accounts:    base.Account,
		},
		AccountInventory: &characterSettlementAccountInventoryRepository{
			characterID: characterID,
			characters:  base.Character,
			accounts:    base.AccountInventory,
		},
		Character: &characterSettlementScopedCharacterRepository{characterID: characterID, base: base.Character},
		Quest:     &characterSettlementScopedQuestRepository{characterID: characterID, base: base.Quest},
		Skill:     &characterSettlementScopedSkillRepository{characterID: characterID, base: base.Skill},
		Inventory: &characterSettlementScopedInventoryRepository{characterID: characterID, base: base.Inventory},
		Equipment: &characterSettlementScopedEquipmentRepository{characterID: characterID, base: base.Equipment},
	}
	if err := apply(txGroup); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// characterSettlementAccountRepository scopes account metadata and currencies
// to the selected character's owner inside the same SQL transaction.
type characterSettlementAccountRepository struct {
	characterID string
	characters  repository.CharacterRepository
	accounts    repository.AccountRepository
}

func (r *characterSettlementAccountRepository) accountID(ctx context.Context) (string, error) {
	if r == nil || r.characters == nil || r.accounts == nil {
		return "", repository.ErrCharacterSettlementTransactionUnavailable
	}
	character, found, err := r.characters.Load(ctx, r.characterID)
	if err != nil {
		return "", err
	}
	accountID := strings.TrimSpace(character.AccountID)
	if !found || accountID == "" {
		return "", repository.ErrCharacterSettlementTransactionUnavailable
	}
	return accountID, nil
}

func (r *characterSettlementAccountRepository) Load(ctx context.Context, accountID string) (repository.AccountRecord, bool, error) {
	want, err := r.accountID(ctx)
	if err != nil {
		return repository.AccountRecord{}, false, err
	}
	if err := repository.TransactionKeyError(ctx, want, accountID); err != nil {
		return repository.AccountRecord{}, false, err
	}
	return r.accounts.Load(ctx, accountID)
}

func (r *characterSettlementAccountRepository) Save(ctx context.Context, record repository.AccountRecord) error {
	want, err := r.accountID(ctx)
	if err != nil {
		return err
	}
	if err := repository.TransactionKeyError(ctx, want, record.AccountID); err != nil {
		return err
	}
	return r.accounts.Save(ctx, record)
}

// characterSettlementAccountInventoryRepository resolves and verifies the
// account owner while keeping the transaction limited to the selected
// character's account inventory record.
type characterSettlementAccountInventoryRepository struct {
	characterID string
	characters  repository.CharacterRepository
	accounts    repository.AccountInventoryRepository
}

func (r *characterSettlementAccountInventoryRepository) accountID(ctx context.Context) (string, error) {
	if r == nil || r.characters == nil || r.accounts == nil {
		return "", repository.ErrCharacterSettlementTransactionUnavailable
	}
	character, found, err := r.characters.Load(ctx, r.characterID)
	if err != nil {
		return "", err
	}
	accountID := strings.TrimSpace(character.AccountID)
	if !found || accountID == "" {
		return "", repository.ErrCharacterSettlementTransactionUnavailable
	}
	return accountID, nil
}

func (r *characterSettlementAccountInventoryRepository) Load(ctx context.Context, accountID string) (repository.AccountInventoryRecord, bool, error) {
	want, err := r.accountID(ctx)
	if err != nil {
		return repository.AccountInventoryRecord{}, false, err
	}
	if err := repository.TransactionKeyError(ctx, want, accountID); err != nil {
		return repository.AccountInventoryRecord{}, false, err
	}
	return r.accounts.Load(ctx, accountID)
}

func (r *characterSettlementAccountInventoryRepository) Save(ctx context.Context, record repository.AccountInventoryRecord) error {
	want, err := r.accountID(ctx)
	if err != nil {
		return err
	}
	if err := repository.TransactionKeyError(ctx, want, record.AccountID); err != nil {
		return err
	}
	return r.accounts.Save(ctx, record)
}

type characterSettlementScopedCharacterRepository struct {
	characterID string
	base        repository.CharacterRepository
}

func (r *characterSettlementScopedCharacterRepository) Load(ctx context.Context, characterID string) (repository.CharacterRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.CharacterRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterSettlementScopedCharacterRepository) Save(ctx context.Context, record repository.CharacterRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *characterSettlementScopedCharacterRepository) SaveFields(ctx context.Context, record repository.CharacterRecord, fields ...repository.CharacterField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveCharacterFields(ctx, r.base, record, fields...)
}

func (r *characterSettlementScopedCharacterRepository) ListByAccount(context.Context, string, int) ([]repository.CharacterRecord, error) {
	return nil, repository.ErrCharacterSettlementTransactionUnavailable
}

func (r *characterSettlementScopedCharacterRepository) FindIDByName(context.Context, string) (string, bool, error) {
	return "", false, repository.ErrCharacterSettlementTransactionUnavailable
}

func (r *characterSettlementScopedCharacterRepository) NextNumericID(context.Context) (int, error) {
	return 0, repository.ErrCharacterSettlementTransactionUnavailable
}

type characterSettlementScopedQuestRepository struct {
	characterID string
	base        repository.QuestRepository
}

func (r *characterSettlementScopedQuestRepository) Load(ctx context.Context, characterID string) (repository.QuestRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.QuestRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterSettlementScopedQuestRepository) Save(ctx context.Context, record repository.QuestRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *characterSettlementScopedQuestRepository) SaveFields(ctx context.Context, record repository.QuestRecord, fields ...repository.QuestField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveQuestFields(ctx, r.base, record, fields...)
}

type characterSettlementScopedSkillRepository struct {
	characterID string
	base        repository.SkillRepository
}

func (r *characterSettlementScopedSkillRepository) Load(ctx context.Context, characterID string) (repository.SkillRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.SkillRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterSettlementScopedSkillRepository) Save(ctx context.Context, record repository.SkillRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *characterSettlementScopedSkillRepository) SaveFields(ctx context.Context, record repository.SkillRecord, fields ...repository.SkillField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveSkillFields(ctx, r.base, record, fields...)
}

type characterSettlementScopedInventoryRepository struct {
	characterID string
	base        repository.InventoryRepository
}

func (r *characterSettlementScopedInventoryRepository) Load(ctx context.Context, characterID string) (repository.InventoryRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.InventoryRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterSettlementScopedInventoryRepository) Save(ctx context.Context, record repository.InventoryRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *characterSettlementScopedInventoryRepository) SaveFields(ctx context.Context, record repository.InventoryRecord, fields ...repository.InventoryField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveInventoryFields(ctx, r.base, record, fields...)
}

type characterSettlementScopedEquipmentRepository struct {
	characterID string
	base        repository.EquipmentRepository
}

func (r *characterSettlementScopedEquipmentRepository) Load(ctx context.Context, characterID string) (repository.EquipmentRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.EquipmentRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterSettlementScopedEquipmentRepository) Save(ctx context.Context, record repository.EquipmentRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *characterSettlementScopedEquipmentRepository) SaveFields(ctx context.Context, record repository.EquipmentRecord, fields ...repository.EquipmentField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveEquipmentFields(ctx, r.base, record, fields...)
}
