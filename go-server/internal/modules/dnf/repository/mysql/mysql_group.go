// 本文件负责装配 DNF 仓储的 MySQL 实现。
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"longheng.io/server/internal/modules/dnf/repository"
	"time"
)

var (
	ErrMySQLDBRequired    = errors.New("dnf mysql db is required")
	ErrMySQLConfigInvalid = errors.New("dnf mysql config is invalid")
)

// SQLRow 是 MySQL 查询单行结果的最小扫描接口。
type SQLRow interface {
	Scan(...any) error
}

// SQLRows 是 MySQL 多行查询结果的最小接口。
type SQLRows interface {
	Close() error
	Next() bool
	Scan(...any) error
	Err() error
}

// SQLDB 是 DNF MySQL 仓储依赖的最小数据库接口。
type SQLDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (SQLRows, error)
	QueryRowContext(context.Context, string, ...any) SQLRow
	PingContext(context.Context) error
}

// SQLDBAdapter 把标准库 *sql.DB 适配成 DNF 仓储使用的 SQLDB。
type SQLDBAdapter struct {
	DB *sql.DB
}

// ExecContext 执行 MySQL 写语句。
func (a SQLDBAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if a.DB == nil {
		return nil, ErrMySQLDBRequired
	}
	return a.DB.ExecContext(ctx, query, args...)
}

// QueryContext 执行 MySQL 多行查询。
func (a SQLDBAdapter) QueryContext(ctx context.Context, query string, args ...any) (SQLRows, error) {
	if a.DB == nil {
		return nil, ErrMySQLDBRequired
	}
	return a.DB.QueryContext(ctx, query, args...)
}

// QueryRowContext 执行 MySQL 单行查询。
func (a SQLDBAdapter) QueryRowContext(ctx context.Context, query string, args ...any) SQLRow {
	if a.DB == nil {
		return errorRow{err: ErrMySQLDBRequired}
	}
	return a.DB.QueryRowContext(ctx, query, args...)
}

// PingContext 检查 MySQL 连接是否可用。
func (a SQLDBAdapter) PingContext(ctx context.Context) error {
	if a.DB == nil {
		return ErrMySQLDBRequired
	}
	return a.DB.PingContext(ctx)
}

// BeginTx exposes a standard SQL transaction without widening the minimal
// SQLDB read/write interface used by repository test doubles.
func (a SQLDBAdapter) BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	if a.DB == nil {
		return nil, ErrMySQLDBRequired
	}
	return a.DB.BeginTx(ctx, options)
}

// MySQLGroupOptions 描述 DNF MySQL 仓储的区服数据库计划和表名前缀。
type MySQLGroupOptions struct {
	DatabasePlan repository.DatabasePlan
	TablePrefix  string
	Now          func() time.Time
}

// NewMySQLGroup 使用注入的 SQLDB 创建 DNF MySQL 仓储聚合。
// 它只装配 Load/Save/SaveFields，不自动建库建表；schema 初始化由 SchemaComponent 负责。
func NewMySQLGroup(db SQLDB, options MySQLGroupOptions) (repository.Group, error) {
	router, err := newMySQLRouter(db, options)
	if err != nil {
		return repository.Group{}, err
	}
	return newMySQLGroupFromRouter(router, true), nil
}

func newMySQLGroupFromRouter(router mysqlRouter, includeTransactions bool) repository.Group {
	base := mysqlStoreBase{router: router}
	group := repository.Group{
		Account:           &mysqlAccountStore{mysqlStoreBase: base},
		AccountInventory:  &mysqlAccountInventoryStore{mysqlStoreBase: base},
		Character:         &mysqlCharStore{mysqlStoreBase: base},
		Inventory:         &mysqlBagStore{mysqlStoreBase: base},
		Equipment:         &mysqlEquipmentStore{mysqlStoreBase: base},
		Pet:               &mysqlPetStore{mysqlStoreBase: base},
		Quest:             &mysqlQuestStore{mysqlStoreBase: base},
		Skill:             &mysqlSkillStore{mysqlStoreBase: base},
		DungeonPermission: &mysqlDungeonPermissionStore{mysqlStoreBase: base},
		PacketTemplate:    &mysqlPacketStore{mysqlStoreBase: base},
		Settings:          &mysqlSettingsStore{mysqlStoreBase: base},
		Mailbox:           &mysqlMailboxStore{mysqlStoreBase: base},
		LegacyUserInfo:    &mysqlLegacyUserInfoStore{mysqlStoreBase: base},
		LegacyInventory:   &mysqlLegacyInventoryStore{mysqlStoreBase: base},
	}
	if includeTransactions {
		if _, ok := router.db.(sqlTxBeginner); ok {
			group.CharacterCreate = &mysqlCharacterCreationUnitOfWork{router: router}
			group.CharacterItems = &mysqlCharacterItemUnitOfWork{router: router}
			group.CharacterTrade = &mysqlCharacterTradeUnitOfWork{router: router}
			group.CharacterPets = &mysqlCharacterPetUnitOfWork{router: router}
			group.AccountItems = &mysqlAccountCharacterItemUnitOfWork{router: router}
			group.CharacterAssets = &mysqlCharacterAssetUnitOfWork{router: router}
			group.MailboxAssets = &mysqlMailboxAssetUnitOfWork{router: router}
			group.AccountAssets = &mysqlAccountCharacterAssetUnitOfWork{router: router}
			group.RentalAssets = &mysqlRentalAssetUnitOfWork{router: router}
			group.CeraShopAssets = &mysqlCeraShopAssetUnitOfWork{router: router}
			group.CharacterSkills = &mysqlCharacterSkillUnitOfWork{router: router}
			group.CharacterProgression = &mysqlCharacterProgressionUnitOfWork{router: router}
			group.CharacterSettlement = &mysqlCharacterSettlementUnitOfWork{router: router}
		}
	}
	return group
}

// NewMySQLGroupFromDB 使用标准库 *sql.DB 创建 DNF MySQL 仓储聚合。
// 调用方负责管理 DB 生命周期和连接池参数。
func NewMySQLGroupFromDB(db *sql.DB, options MySQLGroupOptions) (repository.Group, error) {
	return NewMySQLGroup(SQLDBAdapter{DB: db}, options)
}

type errorRow struct {
	err error
}

func (r errorRow) Scan(...any) error {
	return r.err
}

func requireRecordKey[T any](keyFn func(T) string, record T, name string) (string, error) {
	key, err := dbRecordKey(keyFn, record)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return key, nil
}
