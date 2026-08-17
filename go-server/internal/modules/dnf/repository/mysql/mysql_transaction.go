package mysql

import (
	"context"
	"database/sql"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
)

type sqlTxBeginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type sqlTransactionDB struct {
	tx *sql.Tx
}

func (db sqlTransactionDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.tx.ExecContext(ctx, query, args...)
}

func (db sqlTransactionDB) QueryContext(ctx context.Context, query string, args ...any) (SQLRows, error) {
	return db.tx.QueryContext(ctx, query, args...)
}

func (db sqlTransactionDB) QueryRowContext(ctx context.Context, query string, args ...any) SQLRow {
	return db.tx.QueryRowContext(ctx, query, args...)
}

func (db sqlTransactionDB) PingContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

type mysqlCharacterItemUnitOfWork struct {
	router mysqlRouter
}

type mysqlCharacterTradeUnitOfWork struct {
	router mysqlRouter
}

type mysqlCharacterAssetUnitOfWork struct {
	router mysqlRouter
}

type mysqlRentalAssetUnitOfWork struct {
	router mysqlRouter
}

type mysqlCeraShopAssetUnitOfWork struct {
	router mysqlRouter
}

type mysqlCharacterSkillUnitOfWork struct {
	router mysqlRouter
}

type mysqlCharacterProgressionUnitOfWork struct {
	router mysqlRouter
}

type mysqlCharacterCreationUnitOfWork struct {
	router mysqlRouter
}

func (u *mysqlCharacterCreationUnitOfWork) WithinCharacterCreation(ctx context.Context, characterID string, _ repository.Group, apply func(repository.Group) error) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil {
		return repository.ErrCharacterCreationTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrCharacterCreationTransactionUnavailable
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
	txGroup := newMySQLGroupFromRouter(txRouter, false)
	if err := apply(txGroup); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (u *mysqlCharacterSkillUnitOfWork) WithinCharacterSkill(ctx context.Context, characterID string, apply func(repository.SkillRepository) error) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil {
		return repository.ErrCharacterSkillTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrCharacterSkillTransactionUnavailable
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
	if err := apply(txGroup.Skill); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (u *mysqlCharacterProgressionUnitOfWork) WithinCharacterProgression(
	ctx context.Context,
	characterID string,
	apply func(repository.CharacterRepository, repository.SkillRepository) error,
) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil {
		return repository.ErrCharacterProgressionTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrCharacterProgressionTransactionUnavailable
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
	characters := &characterProgressionScopedCharacterRepository{
		characterID: characterID,
		base:        txGroup.Character,
	}
	skills := &characterProgressionScopedSkillRepository{
		characterID: characterID,
		base:        txGroup.Skill,
	}
	if err := apply(characters, skills); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

type characterProgressionScopedCharacterRepository struct {
	characterID string
	base        repository.CharacterRepository
}

func (r *characterProgressionScopedCharacterRepository) Load(ctx context.Context, characterID string) (repository.CharacterRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.CharacterRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterProgressionScopedCharacterRepository) Save(ctx context.Context, record repository.CharacterRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *characterProgressionScopedCharacterRepository) SaveFields(ctx context.Context, record repository.CharacterRecord, fields ...repository.CharacterField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveCharacterFields(ctx, r.base, record, fields...)
}

func (r *characterProgressionScopedCharacterRepository) ListByAccount(context.Context, string, int) ([]repository.CharacterRecord, error) {
	return nil, repository.ErrCharacterProgressionTransactionUnavailable
}

func (r *characterProgressionScopedCharacterRepository) FindIDByName(context.Context, string) (string, bool, error) {
	return "", false, repository.ErrCharacterProgressionTransactionUnavailable
}

func (r *characterProgressionScopedCharacterRepository) NextNumericID(context.Context) (int, error) {
	return 0, repository.ErrCharacterProgressionTransactionUnavailable
}

type characterProgressionScopedSkillRepository struct {
	characterID string
	base        repository.SkillRepository
}

func (r *characterProgressionScopedSkillRepository) Load(ctx context.Context, characterID string) (repository.SkillRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.SkillRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterProgressionScopedSkillRepository) Save(ctx context.Context, record repository.SkillRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *characterProgressionScopedSkillRepository) SaveFields(ctx context.Context, record repository.SkillRecord, fields ...repository.SkillField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveSkillFields(ctx, r.base, record, fields...)
}

func (u *mysqlCharacterItemUnitOfWork) WithinCharacterItems(ctx context.Context, characterID string, apply func(repository.InventoryRepository, repository.EquipmentRepository) error) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil {
		return repository.ErrCharacterItemTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrCharacterItemTransactionUnavailable
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
	if err := apply(txGroup.Inventory, txGroup.Equipment); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (u *mysqlCharacterTradeUnitOfWork) WithinCharacterTrade(
	ctx context.Context,
	firstCharacterID string,
	secondCharacterID string,
	apply func(repository.CharacterRepository, repository.InventoryRepository) error,
) error {
	firstCharacterID = strings.TrimSpace(firstCharacterID)
	secondCharacterID = strings.TrimSpace(secondCharacterID)
	if firstCharacterID == "" || secondCharacterID == "" || firstCharacterID == secondCharacterID || apply == nil {
		return repository.ErrCharacterTradeTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrCharacterTradeTransactionUnavailable
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txRouter := u.router
	txRouter.db = sqlTransactionDB{tx: tx}
	// Character IDs can route to different logical schemas. They remain on
	// one physical MySQL connection, so keep the full qualified write plan.
	txRouter.readDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.writeDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.lockReads = true
	txGroup := newMySQLGroupFromRouter(txRouter, false)
	allowed := map[string]struct{}{firstCharacterID: {}, secondCharacterID: {}}
	characters := &characterTradeScopedCharacterRepository{allowed: allowed, base: txGroup.Character}
	inventories := &characterTradeScopedInventoryRepository{allowed: allowed, base: txGroup.Inventory}
	if err := apply(characters, inventories); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

type characterTradeScopedCharacterRepository struct {
	allowed map[string]struct{}
	base    repository.CharacterRepository
}

func (r *characterTradeScopedCharacterRepository) Load(ctx context.Context, characterID string) (repository.CharacterRecord, bool, error) {
	characterID = strings.TrimSpace(characterID)
	if _, allowed := r.allowed[characterID]; !allowed {
		return repository.CharacterRecord{}, false, repository.ErrCharacterTradeTransactionUnavailable
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterTradeScopedCharacterRepository) Save(ctx context.Context, record repository.CharacterRecord) error {
	if _, allowed := r.allowed[strings.TrimSpace(record.CharacterID)]; !allowed {
		return repository.ErrCharacterTradeTransactionUnavailable
	}
	return r.base.Save(ctx, record)
}

func (r *characterTradeScopedCharacterRepository) ListByAccount(context.Context, string, int) ([]repository.CharacterRecord, error) {
	return nil, repository.ErrCharacterTradeTransactionUnavailable
}

func (r *characterTradeScopedCharacterRepository) FindIDByName(context.Context, string) (string, bool, error) {
	return "", false, repository.ErrCharacterTradeTransactionUnavailable
}

func (r *characterTradeScopedCharacterRepository) NextNumericID(context.Context) (int, error) {
	return 0, repository.ErrCharacterTradeTransactionUnavailable
}

type characterTradeScopedInventoryRepository struct {
	allowed map[string]struct{}
	base    repository.InventoryRepository
}

func (r *characterTradeScopedInventoryRepository) Load(ctx context.Context, characterID string) (repository.InventoryRecord, bool, error) {
	characterID = strings.TrimSpace(characterID)
	if _, allowed := r.allowed[characterID]; !allowed {
		return repository.InventoryRecord{}, false, repository.ErrCharacterTradeTransactionUnavailable
	}
	return r.base.Load(ctx, characterID)
}

func (r *characterTradeScopedInventoryRepository) Save(ctx context.Context, record repository.InventoryRecord) error {
	if _, allowed := r.allowed[strings.TrimSpace(record.CharacterID)]; !allowed {
		return repository.ErrCharacterTradeTransactionUnavailable
	}
	return r.base.Save(ctx, record)
}

func (u *mysqlCharacterAssetUnitOfWork) WithinCharacterAssets(ctx context.Context, characterID string, apply func(repository.CharacterRepository, repository.InventoryRepository, repository.EquipmentRepository) error) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" || apply == nil {
		return repository.ErrCharacterAssetTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrCharacterAssetTransactionUnavailable
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
	if err := apply(txGroup.Character, txGroup.Inventory, txGroup.Equipment); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (u *mysqlRentalAssetUnitOfWork) WithinRentalAssets(
	ctx context.Context,
	accountID string,
	characterID string,
	apply func(repository.AccountRepository, repository.CharacterRepository, repository.InventoryRepository, repository.EquipmentRepository) error,
) error {
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	if accountID == "" || characterID == "" || apply == nil {
		return repository.ErrRentalAssetTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrRentalAssetTransactionUnavailable
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Account and character keys can hash to different logical databases. Keep
	// the complete write plan while using one physical MySQL transaction.
	txRouter := u.router
	txRouter.db = sqlTransactionDB{tx: tx}
	txRouter.readDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.writeDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.lockReads = true
	txGroup := newMySQLGroupFromRouter(txRouter, false)
	accounts := &rentalScopedAccountRepository{accountID: accountID, base: txGroup.Account}
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

func (u *mysqlCeraShopAssetUnitOfWork) WithinCeraShopAssets(
	ctx context.Context,
	accountID string,
	characterID string,
	settingsScope string,
	apply func(repository.AccountRepository, repository.CharacterRepository, repository.InventoryRepository, repository.EquipmentRepository, repository.SettingsRepository) error,
) error {
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	settingsScope = strings.TrimSpace(settingsScope)
	if accountID == "" || characterID == "" || settingsScope == "" || apply == nil {
		return repository.ErrCeraShopAssetTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	beginner, ok := u.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrCeraShopAssetTransactionUnavailable
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// The active local profile uses one physical MySQL server. Keep the full
	// qualified write plan so account, character, and settings keys remain in
	// one SQL transaction even when their logical database hashes differ.
	txRouter := u.router
	txRouter.db = sqlTransactionDB{tx: tx}
	txRouter.readDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.writeDBs = append([]string(nil), u.router.writeDBs...)
	txRouter.lockReads = true
	txGroup := newMySQLGroupFromRouter(txRouter, false)
	accounts := &rentalScopedAccountRepository{accountID: accountID, base: txGroup.Account}
	characters := &rentalScopedCharacterRepository{characterID: characterID, base: txGroup.Character}
	inventories := &rentalScopedInventoryRepository{characterID: characterID, base: txGroup.Inventory}
	equipment := &rentalScopedEquipmentRepository{characterID: characterID, base: txGroup.Equipment}
	settings := &ceraShopScopedSettingsRepository{scope: settingsScope, base: txGroup.Settings}
	if err := apply(accounts, characters, inventories, equipment, settings); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

type ceraShopScopedSettingsRepository struct {
	scope string
	base  repository.SettingsRepository
}

func (r *ceraShopScopedSettingsRepository) Load(ctx context.Context, scope string) (repository.SettingsRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.scope, scope); err != nil {
		return repository.SettingsRecord{}, false, err
	}
	return r.base.Load(ctx, scope)
}

func (r *ceraShopScopedSettingsRepository) Save(ctx context.Context, record repository.SettingsRecord) error {
	if err := repository.TransactionKeyError(ctx, r.scope, record.Scope); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

type rentalScopedAccountRepository struct {
	accountID string
	base      repository.AccountRepository
}

func (r *rentalScopedAccountRepository) Load(ctx context.Context, accountID string) (repository.AccountRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.accountID, accountID); err != nil {
		return repository.AccountRecord{}, false, err
	}
	return r.base.Load(ctx, accountID)
}

func (r *rentalScopedAccountRepository) Save(ctx context.Context, record repository.AccountRecord) error {
	if err := repository.TransactionKeyError(ctx, r.accountID, record.AccountID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

type rentalScopedCharacterRepository struct {
	characterID string
	base        repository.CharacterRepository
}

func (r *rentalScopedCharacterRepository) Load(ctx context.Context, characterID string) (repository.CharacterRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.CharacterRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *rentalScopedCharacterRepository) Save(ctx context.Context, record repository.CharacterRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *rentalScopedCharacterRepository) SaveFields(ctx context.Context, record repository.CharacterRecord, fields ...repository.CharacterField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveCharacterFields(ctx, r.base, record, fields...)
}

func (r *rentalScopedCharacterRepository) ListByAccount(context.Context, string, int) ([]repository.CharacterRecord, error) {
	return nil, repository.ErrRentalAssetTransactionUnavailable
}

func (r *rentalScopedCharacterRepository) FindIDByName(context.Context, string) (string, bool, error) {
	return "", false, repository.ErrRentalAssetTransactionUnavailable
}

func (r *rentalScopedCharacterRepository) NextNumericID(context.Context) (int, error) {
	return 0, repository.ErrRentalAssetTransactionUnavailable
}

type rentalScopedInventoryRepository struct {
	characterID string
	base        repository.InventoryRepository
}

func (r *rentalScopedInventoryRepository) Load(ctx context.Context, characterID string) (repository.InventoryRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.InventoryRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *rentalScopedInventoryRepository) Save(ctx context.Context, record repository.InventoryRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *rentalScopedInventoryRepository) SaveFields(ctx context.Context, record repository.InventoryRecord, fields ...repository.InventoryField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveInventoryFields(ctx, r.base, record, fields...)
}

type rentalScopedEquipmentRepository struct {
	characterID string
	base        repository.EquipmentRepository
}

func (r *rentalScopedEquipmentRepository) Load(ctx context.Context, characterID string) (repository.EquipmentRecord, bool, error) {
	if err := repository.TransactionKeyError(ctx, r.characterID, characterID); err != nil {
		return repository.EquipmentRecord{}, false, err
	}
	return r.base.Load(ctx, characterID)
}

func (r *rentalScopedEquipmentRepository) Save(ctx context.Context, record repository.EquipmentRecord) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return r.base.Save(ctx, record)
}

func (r *rentalScopedEquipmentRepository) SaveFields(ctx context.Context, record repository.EquipmentRecord, fields ...repository.EquipmentField) error {
	if err := repository.TransactionKeyError(ctx, r.characterID, record.CharacterID); err != nil {
		return err
	}
	return repository.SaveEquipmentFields(ctx, r.base, record, fields...)
}
