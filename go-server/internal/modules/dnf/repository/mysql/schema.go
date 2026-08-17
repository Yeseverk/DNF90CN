// 本文件负责 DNF 仓储 MySQL 建库建表语句。
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
)

const defaultTablePrefix = "dnf"

var (
	ErrSchemaConfigInvalid    = errors.New("dnf repository schema config is invalid")
	ErrSchemaExecutorRequired = errors.New("dnf repository schema executor is required")
)

// SQLExecutor 是 DNF 仓储建库建表所需的最小 SQL 执行接口。
type SQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// SchemaOptions 控制 DNF 仓储首次启动建库建表行为。
type SchemaOptions struct {
	AutoCreate                bool
	CreateDatabases           bool
	IncludeCSharpLegacySchema bool
	TablePrefix               string
	DatabasePlan              repository.DatabasePlan
}

// EnsureMySQLSchema 在显式开启 AutoCreate 时创建 DNF 仓储库表。
// 它会写 MySQL schema，必须只在开发环境或已确认的启动迁移流程中调用。
func EnsureMySQLSchema(ctx context.Context, exec SQLExecutor, options SchemaOptions) error {
	if !options.AutoCreate {
		return nil
	}
	if exec == nil {
		return ErrSchemaExecutorRequired
	}
	statements, err := MySQLSchema(options)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := exec.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	migrations, err := mysqlSchemaMigrations(options)
	if err != nil {
		return err
	}
	for _, statement := range migrations {
		if _, err := exec.ExecContext(ctx, statement); err != nil && !isMySQLDuplicateSchemaObjectError(err) {
			return err
		}
	}
	return nil
}

// mysqlSchemaMigrations contains additive migrations for tables that may
// already exist. CREATE TABLE IF NOT EXISTS cannot add newly introduced
// account or character columns to an existing shard table.
func mysqlSchemaMigrations(options SchemaOptions) ([]string, error) {
	tablePrefix, err := normTablePrefix(options.TablePrefix)
	if err != nil {
		return nil, err
	}
	databases, err := repository.ValidateDatabases(options.DatabasePlan.SchemaDatabases())
	if err != nil {
		return nil, err
	}
	statements := make([]string, 0, len(databases)*8)
	for _, database := range databases {
		statements = append(statements, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s AFTER %s",
			mysqlTable(database, tablePrefix, mysqlAccountTable),
			quoteSQLIdentifier(mysqlAccountHonorColumn),
			mysqlAccountHonorDDL,
			quoteSQLIdentifier("state"),
		))
		statements = append(statements, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s AFTER %s",
			mysqlTable(database, tablePrefix, mysqlAccountTable),
			quoteSQLIdentifier(mysqlAccountRepresentNameColumn),
			mysqlAccountRepresentNameDDL,
			quoteSQLIdentifier(mysqlAccountHonorColumn),
		))
		statements = append(statements, fmt.Sprintf(
			"ALTER TABLE %s ADD UNIQUE KEY %s (%s)",
			mysqlTable(database, tablePrefix, mysqlAccountTable),
			quoteSQLIdentifier(mysqlAccountRepresentNameUniqueKey),
			quoteSQLIdentifier(mysqlAccountRepresentNameColumn),
		))
		statements = append(statements, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s AFTER %s",
			mysqlTable(database, tablePrefix, mysqlCharTable),
			quoteSQLIdentifier(mysqlCharacterTutorialCompletedStatColumn),
			mysqlCharacterTutorialCompletedStatDDL,
			quoteSQLIdentifier("user_state"),
		))
		statements = append(statements, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s AFTER %s",
			mysqlTable(database, tablePrefix, mysqlCharTable),
			quoteSQLIdentifier(mysqlCharacterTutorialRewardProgress38StatColumn),
			mysqlCharacterTutorialRewardProgress38StatDDL,
			quoteSQLIdentifier(mysqlCharacterTutorialCompletedStatColumn),
		))
		statements = append(statements, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s AFTER %s",
			mysqlTable(database, tablePrefix, mysqlCharTable),
			quoteSQLIdentifier(mysqlCharacterStoryDigestLastLevelStatColumn),
			mysqlCharacterStoryDigestLastLevelStatDDL,
			quoteSQLIdentifier(mysqlCharacterTutorialRewardProgress38StatColumn),
		))
		statements = append(statements, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s AFTER %s",
			mysqlTable(database, tablePrefix, mysqlCharTable),
			quoteSQLIdentifier(mysqlCharacterStoryDigestMigrationVersionColumn),
			mysqlCharacterStoryDigestMigrationVersionDDL,
			quoteSQLIdentifier(mysqlCharacterStoryDigestLastLevelStatColumn),
		))
		statements = append(statements, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s AFTER %s",
			mysqlTable(database, tablePrefix, mysqlCharTable),
			quoteSQLIdentifier(mysqlCharacterCeraStatColumn),
			mysqlCharacterCeraStatDDL,
			quoteSQLIdentifier("gold"),
		))
		statements = append(statements, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s %s AFTER %s",
			mysqlTable(database, tablePrefix, mysqlCharTable),
			quoteSQLIdentifier(mysqlCharacterCrystalSelectionStatColumn),
			mysqlCharacterCrystalSelectionStatDDL,
			quoteSQLIdentifier("aura_flag"),
		))
		skillTable := mysqlTable(database, tablePrefix, mysqlSkillTable)
		statements = append(statements,
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INT NOT NULL DEFAULT 0", skillTable, quoteSQLIdentifier("total_sp")),
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INT NOT NULL DEFAULT 0", skillTable, quoteSQLIdentifier("remaining_sp")),
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INT NOT NULL DEFAULT 0", skillTable, quoteSQLIdentifier("total_tp")),
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INT NOT NULL DEFAULT 0", skillTable, quoteSQLIdentifier("remaining_tp")),
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INT NOT NULL DEFAULT 0", skillTable, quoteSQLIdentifier("synced_level")),
		)
	}
	return statements, nil
}

func isMySQLDuplicateSchemaObjectError(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && (mysqlErr.Number == 1060 || mysqlErr.Number == 1061)
}

// MySQLSchema 生成 DNF 仓储 MySQL DDL。
// 返回值只包含 schema 语句，不连接数据库，也不执行任何写入。
func MySQLSchema(options SchemaOptions) ([]string, error) {
	tablePrefix, err := normTablePrefix(options.TablePrefix)
	if err != nil {
		return nil, err
	}
	databases, err := repository.ValidateDatabases(options.DatabasePlan.SchemaDatabases())
	if err != nil {
		return nil, err
	}
	if len(databases) == 0 {
		return nil, fmt.Errorf("%w: at least one write database is required", ErrSchemaConfigInvalid)
	}
	statements := make([]string, 0, len(databases)*12)
	for _, database := range databases {
		if options.CreateDatabases {
			statements = append(statements, fmt.Sprintf(
				"CREATE DATABASE IF NOT EXISTS %s DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
				quoteSQLIdentifier(database),
			))
		}
		statements = append(statements, mysqlTableSchema(database, tablePrefix)...)
		if options.IncludeCSharpLegacySchema {
			legacy, err := csharpLegacyMySQLSchema(database, tablePrefix)
			if err != nil {
				return nil, err
			}
			statements = append(statements, legacy...)
		}
	}
	return statements, nil
}

func mysqlTableSchema(database, prefix string) []string {
	accounts := mysqlTable(database, prefix, "accounts")
	accountInventories := mysqlTable(database, prefix, mysqlAccountInventoryTable)
	characters := mysqlTable(database, prefix, "characters")
	inventories := mysqlTable(database, prefix, "inventories")
	equipments := mysqlTable(database, prefix, "equipments")
	pets := mysqlTable(database, prefix, "pets")
	quests := mysqlTable(database, prefix, "quests")
	skills := mysqlTable(database, prefix, "skills")
	dungeonPermissions := mysqlTable(database, prefix, mysqlDungeonPermissionTable)
	mailboxes := mysqlTable(database, prefix, mysqlMailboxTable)
	packetTemplates := mysqlTable(database, prefix, "packet_templates")
	settings := mysqlTable(database, prefix, "settings")
	statements := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  account_id VARCHAR(128) NOT NULL,
  state VARCHAR(32) NOT NULL DEFAULT '',
  honor_exp BIGINT UNSIGNED NOT NULL DEFAULT 0,
  represent_account_name VARCHAR(64) NULL,
  created_at DATETIME(6) NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (account_id),
  UNIQUE KEY uk_dnf_accounts_represent_account_name (represent_account_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, accounts),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  account_id VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, accountInventories),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  account_id VARCHAR(128) NOT NULL,
  slot INT NOT NULL DEFAULT 0,
  name VARCHAR(64) NOT NULL DEFAULT '',
  job VARCHAR(64) NOT NULL DEFAULT '',
  level INT NOT NULL DEFAULT 0,
%s
  created_at DATETIME(6) NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id),
  UNIQUE KEY uk_dnf_characters_account_slot_active (account_id, slot, delete_flag),
  KEY idx_dnf_characters_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, characters, mysqlCharacterStatColumnDDL()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, inventories),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, equipments),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  equipped_key VARCHAR(128) NOT NULL DEFAULT '',
  town_display TINYINT UNSIGNED NOT NULL DEFAULT 0,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, pets),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, quests),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  total_sp INT NOT NULL DEFAULT 0,
  remaining_sp INT NOT NULL DEFAULT 0,
  total_tp INT NOT NULL DEFAULT 0,
  remaining_tp INT NOT NULL DEFAULT 0,
  synced_level INT NOT NULL DEFAULT 0,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, skills),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  dungeon_id BIGINT UNSIGNED NOT NULL,
  clear_state TINYINT UNSIGNED NOT NULL DEFAULT 0,
  sort_order INT NOT NULL DEFAULT 0,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id, dungeon_id),
  KEY idx_dnf_dungeon_permissions_character_sort (character_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, dungeonPermissions),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  character_id VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (character_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, mailboxes),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  template_id VARCHAR(128) NOT NULL,
  name VARCHAR(128) NOT NULL DEFAULT '',
  body LONGBLOB NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (template_id),
  KEY idx_dnf_packet_templates_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, packetTemplates),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  scope VARCHAR(128) NOT NULL,
  updated_at DATETIME(6) NULL,
  PRIMARY KEY (scope)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, settings),
	}
	return append(statements, mysqlRelationalTableSchema(database, prefix)...)
}

func mysqlTable(database, prefix, suffix string) string {
	return quoteSQLIdentifier(database) + "." + quoteSQLIdentifier(prefix+"_"+suffix)
}

func normTablePrefix(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = defaultTablePrefix
	}
	if !repository.IsSQLIdentifier(prefix) {
		return "", fmt.Errorf("%w: table prefix %q is invalid", ErrSchemaConfigInvalid, prefix)
	}
	return prefix, nil
}

func quoteSQLIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}
