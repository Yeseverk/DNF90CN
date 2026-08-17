// 本文件验证 DNF 仓储 schema 生成、落地产物和 legacy USERINFO 表白名单。
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

type fakeSchemaExec struct {
	statements []string
	failAt     int
	failErr    error
}

func (f *fakeSchemaExec) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	f.statements = append(f.statements, query)
	if f.failAt > 0 && len(f.statements) == f.failAt {
		if f.failErr != nil {
			return nil, f.failErr
		}
		return nil, errFakeSchemaExec
	}
	return nil, nil
}

var errFakeSchemaExec = errors.New("fake schema exec")

func TestMySQLSchemaCreatesDatabaseAndTables(t *testing.T) {
	schema, err := MySQLSchema(SchemaOptions{
		CreateDatabases: true,
		TablePrefix:     "dnf",
		DatabasePlan: repository.DatabasePlan{
			WriteDatabases: []string{"dnf_s1_w1"},
		},
	})
	if err != nil {
		t.Fatalf("MySQLSchema() error = %v", err)
	}
	if len(schema) != 41 {
		t.Fatalf("schema statement count = %d, want 41", len(schema))
	}
	joined := strings.Join(schema, "\n")
	for _, want := range []string{
		"CREATE DATABASE IF NOT EXISTS `dnf_s1_w1`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_accounts`",
		"honor_exp BIGINT UNSIGNED NOT NULL DEFAULT 0",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_characters`",
		"`pc_room_id` BIGINT NOT NULL DEFAULT 65537",
		"`fatigue` SMALLINT UNSIGNED NOT NULL DEFAULT 156",
		"`fatigue_limit` SMALLINT UNSIGNED NOT NULL DEFAULT 156",
		"`tutorial_completed` TINYINT UNSIGNED NOT NULL DEFAULT 0",
		"`tutorial_reward_progress_38` TINYINT UNSIGNED NOT NULL DEFAULT 0",
		"`story_digest_last_level` INT UNSIGNED NOT NULL DEFAULT 0",
		"`story_digest_migration_version` SMALLINT UNSIGNED NOT NULL DEFAULT 0",
		"`town_id` SMALLINT UNSIGNED NOT NULL DEFAULT 38",
		"`area_id` SMALLINT UNSIGNED NOT NULL DEFAULT 1",
		"`pos_x` SMALLINT NOT NULL DEFAULT 450",
		"`pos_y` SMALLINT NOT NULL DEFAULT 234",
		"`stat_strength` INT NOT NULL DEFAULT 0",
		"`stat_independent_attack` INT NOT NULL DEFAULT 0",
		"`roster_card_flag` TINYINT UNSIGNED NOT NULL DEFAULT 0",
		"`create_option_byte_63` TINYINT UNSIGNED NOT NULL DEFAULT 0",
		"UNIQUE KEY uk_dnf_characters_account_slot_active (account_id, slot, delete_flag)",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_character_rosters`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_equipments`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_equipment_entries`",
		"total_sp INT NOT NULL DEFAULT 0",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_skill_layouts`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_dungeon_permissions`",
		"clear_state TINYINT UNSIGNED NOT NULL DEFAULT 0",
		"KEY idx_dnf_dungeon_permissions_character_sort (character_id, sort_order)",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_pet_entries`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_pets`",
		"equipped_key VARCHAR(128) NOT NULL DEFAULT ''",
		"town_display TINYINT UNSIGNED NOT NULL DEFAULT 0",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_quests`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_quest_states`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_skill_states`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_mailboxes`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_mails`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_packet_templates`",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("schema missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "_json JSON") {
		t.Fatalf("schema still contains repository JSON columns:\n%s", joined)
	}
}

func TestMySQLSchemaIncludesCSharpLegacyTables(t *testing.T) {
	wantLegacyTables := embeddedCSharpTableCount(t) + 2
	schema, err := MySQLSchema(SchemaOptions{
		IncludeCSharpLegacySchema: true,
		TablePrefix:               "dnf",
		DatabasePlan: repository.DatabasePlan{
			WriteDatabases: []string{"dnf_s1_w1"},
		},
	})
	if err != nil {
		t.Fatalf("MySQLSchema() error = %v", err)
	}
	joined := strings.Join(schema, "\n")
	if got := countSQLPrefix(schema, "CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_legacy_"); got != wantLegacyTables {
		t.Fatalf("legacy table count = %d, want %d", got, wantLegacyTables)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_legacy_accounts`",
		"`extra46` BIGINT NOT NULL DEFAULT 0",
		"`extra47` BIGINT NOT NULL DEFAULT 0",
		"`extra51` BIGINT NOT NULL DEFAULT 0",
		"`active_status_resistance_17` BIGINT NOT NULL DEFAULT 0",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_legacy_character_userinfo80_slots`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_legacy_character_userinfo90_control`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_legacy_character_userinfob6_values`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_legacy_character_userinfo22e_state`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_legacy_character_userinfo29f_rows`",
		"CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_legacy_account_settings`",
		"INSERT IGNORE INTO `dnf_s1_w1`.`dnf_legacy_accounts`",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("legacy schema missing %q", want)
		}
	}
}

func TestCSharpLegacySchemaRemovesInvalidLargeDefaults(t *testing.T) {
	schema, err := CSharpLegacyMySQLSchema("dnf_s1_w1", "dnf")
	if err != nil {
		t.Fatalf("CSharpLegacyMySQLSchema() error = %v", err)
	}
	joined := strings.Join(schema, "\n")
	if !strings.Contains(joined, "`payload` LONGBLOB NOT NULL") {
		t.Fatalf("legacy schema should keep payload as required LONGBLOB:\n%s", joined)
	}
	for _, bad := range []string{
		"LONGBLOB NOT NULL DEFAULT",
		"BLOB NOT NULL DEFAULT",
		"LONGTEXT NOT NULL DEFAULT",
		"TEXT NOT NULL DEFAULT",
		"JSON NOT NULL DEFAULT",
	} {
		if strings.Contains(joined, bad) {
			t.Fatalf("legacy schema contains MySQL-invalid large column default %q", bad)
		}
	}
}

func TestRepositorySQLFileContainsLegacySchema(t *testing.T) {
	wantLegacyTables := embeddedCSharpTableCount(t) + 2
	path := filepath.Join("testdata", "repository.mysql.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read repository.mysql.sql: %v", err)
	}
	sql := string(data)
	if got := strings.Count(sql, "CREATE TABLE IF NOT EXISTS `dnf_s9999_w01`.`dnf_legacy_"); got != wantLegacyTables {
		t.Fatalf("repository.mysql.sql legacy table count = %d, want %d", got, wantLegacyTables)
	}
	for _, want := range []string{
		"dnf_character_rosters",
		"dnf_equipments",
		"dnf_equipment_entries",
		"dnf_pets",
		"dnf_pet_entries",
		"dnf_quests",
		"equipped_key VARCHAR(128) NOT NULL DEFAULT ''",
		"town_display TINYINT UNSIGNED NOT NULL DEFAULT 0",
		"dnf_quest_states",
		"dnf_skill_states",
		"dnf_skill_layouts",
		"dnf_mailboxes",
		"dnf_mails",
		"dnf_dungeon_permissions",
		"honor_exp BIGINT UNSIGNED NOT NULL DEFAULT 0",
		"`pc_room_id` BIGINT NOT NULL DEFAULT 65537",
		"`fatigue` SMALLINT UNSIGNED NOT NULL DEFAULT 156",
		"`fatigue_limit` SMALLINT UNSIGNED NOT NULL DEFAULT 156",
		"`tutorial_completed` TINYINT UNSIGNED NOT NULL DEFAULT 0",
		"`story_digest_last_level` INT UNSIGNED NOT NULL DEFAULT 0",
		"`story_digest_migration_version` SMALLINT UNSIGNED NOT NULL DEFAULT 0",
		"`stat_strength` INT NOT NULL DEFAULT 0",
		"`stat_independent_attack` INT NOT NULL DEFAULT 0",
		"`roster_card_flag` TINYINT UNSIGNED NOT NULL DEFAULT 0",
		"`create_option_byte_63` TINYINT UNSIGNED NOT NULL DEFAULT 0",
		"UNIQUE KEY uk_dnf_characters_account_slot_active (account_id, slot, delete_flag)",
		"dnf_legacy_accounts",
		"dnf_legacy_character_items",
		"`extra46` BIGINT NOT NULL DEFAULT 0",
		"`extra47` BIGINT NOT NULL DEFAULT 0",
		"`extra51` BIGINT NOT NULL DEFAULT 0",
		"`active_status_resistance_17` BIGINT NOT NULL DEFAULT 0",
		"dnf_legacy_character_userinfo80_slots",
		"dnf_legacy_character_userinfo90_control",
		"dnf_legacy_character_userinfob6_values",
		"dnf_legacy_character_userinfo22e_state",
		"dnf_legacy_character_userinfo29f_rows",
		"dnf_legacy_account_settings",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("repository.mysql.sql missing %q", want)
		}
	}
	if strings.Contains(sql, "_json JSON") {
		t.Fatal("repository.mysql.sql still contains repository JSON columns")
	}
	if strings.Contains(sql, "LONGBLOB NOT NULL DEFAULT") {
		t.Fatal("repository.mysql.sql contains MySQL-invalid LONGBLOB default")
	}
}

func TestLegacyUserInfoQueryAllowsOnlyUserInfoAndSubtypeTables(t *testing.T) {
	for _, table := range []string{
		"legacy_character_userinfo23_scalar_values",
		"legacy_character_userinfob6_values",
		"legacy_character_subtype0_fields",
		"legacy_character_subtype1_fields",
		"legacy_character_achievement_chunks",
		"legacy_character_achievement_complete",
	} {
		if err := validateLegacyUserInfoQuery(table, []string{"character_id"}, nil); err != nil {
			t.Fatalf("validateLegacyUserInfoQuery(%q) error = %v", table, err)
		}
	}
	for _, table := range []string{
		"legacy_character_items",
		"legacy_character_subtype2_fields",
		"legacy_character_userinfo23_scalar_values;DROP",
	} {
		if err := validateLegacyUserInfoQuery(table, []string{"character_id"}, nil); err == nil {
			t.Fatalf("validateLegacyUserInfoQuery(%q) succeeded, want rejected", table)
		}
	}
}

func TestEnsureMySQLSchemaRequiresExplicitAutoCreate(t *testing.T) {
	exec := &fakeSchemaExec{}
	err := EnsureMySQLSchema(context.Background(), exec, SchemaOptions{
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("EnsureMySQLSchema() error = %v", err)
	}
	if len(exec.statements) != 0 {
		t.Fatalf("schema should not run when AutoCreate=false: %v", exec.statements)
	}
}

func TestEnsureMySQLSchemaExecutesWhenEnabled(t *testing.T) {
	exec := &fakeSchemaExec{}
	err := EnsureMySQLSchema(context.Background(), exec, SchemaOptions{
		AutoCreate:      true,
		CreateDatabases: true,
		DatabasePlan:    repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("EnsureMySQLSchema() error = %v", err)
	}
	if len(exec.statements) != 55 {
		t.Fatalf("executed statements = %d, want 55", len(exec.statements))
	}
	wantMigrations := []string{
		"ALTER TABLE `dnf_s1_w1`.`dnf_accounts` ADD COLUMN `honor_exp` BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER `state`",
		"ALTER TABLE `dnf_s1_w1`.`dnf_accounts` ADD COLUMN `represent_account_name` VARCHAR(64) NULL AFTER `honor_exp`",
		"ALTER TABLE `dnf_s1_w1`.`dnf_accounts` ADD UNIQUE KEY `uk_dnf_accounts_represent_account_name` (`represent_account_name`)",
		"ALTER TABLE `dnf_s1_w1`.`dnf_characters` ADD COLUMN `tutorial_completed` TINYINT UNSIGNED NOT NULL DEFAULT 0 AFTER `user_state`",
		"ALTER TABLE `dnf_s1_w1`.`dnf_characters` ADD COLUMN `tutorial_reward_progress_38` TINYINT UNSIGNED NOT NULL DEFAULT 0 AFTER `tutorial_completed`",
		"ALTER TABLE `dnf_s1_w1`.`dnf_characters` ADD COLUMN `story_digest_last_level` INT UNSIGNED NOT NULL DEFAULT 0 AFTER `tutorial_reward_progress_38`",
		"ALTER TABLE `dnf_s1_w1`.`dnf_characters` ADD COLUMN `story_digest_migration_version` SMALLINT UNSIGNED NOT NULL DEFAULT 0 AFTER `story_digest_last_level`",
		"ALTER TABLE `dnf_s1_w1`.`dnf_characters` ADD COLUMN `cera` BIGINT NOT NULL DEFAULT 0 AFTER `gold`",
		"ALTER TABLE `dnf_s1_w1`.`dnf_characters` ADD COLUMN `premium_crystal_selection` TINYINT NOT NULL DEFAULT -1 AFTER `aura_flag`",
	}
	for index, want := range wantMigrations {
		if got := exec.statements[len(exec.statements)-14+index]; got != want {
			t.Fatalf("migration %d = %q, want %q", index, got, want)
		}
	}
}

func TestEnsureMySQLSchemaIgnoresExistingHonorExpColumn(t *testing.T) {
	exec := &fakeSchemaExec{
		failAt:  42,
		failErr: &mysql.MySQLError{Number: 1060, Message: "Duplicate column name 'honor_exp'"},
	}
	err := EnsureMySQLSchema(context.Background(), exec, SchemaOptions{
		AutoCreate:      true,
		CreateDatabases: true,
		DatabasePlan:    repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("EnsureMySQLSchema() duplicate honor migration error = %v", err)
	}
	if len(exec.statements) != 55 {
		t.Fatalf("executed statements = %d, want 55", len(exec.statements))
	}
}

func TestEnsureMySQLSchemaIgnoresExistingRepresentNameIndex(t *testing.T) {
	exec := &fakeSchemaExec{
		failAt:  44,
		failErr: &mysql.MySQLError{Number: 1061, Message: "Duplicate key name 'uk_dnf_accounts_represent_account_name'"},
	}
	err := EnsureMySQLSchema(context.Background(), exec, SchemaOptions{
		AutoCreate:      true,
		CreateDatabases: true,
		DatabasePlan:    repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("EnsureMySQLSchema() duplicate represent-name index error = %v", err)
	}
	if len(exec.statements) != 55 {
		t.Fatalf("executed statements = %d, want 55", len(exec.statements))
	}
}

func TestEnsureMySQLSchemaIgnoresExistingTutorialFlagColumn(t *testing.T) {
	exec := &fakeSchemaExec{
		failAt:  45,
		failErr: &mysql.MySQLError{Number: 1060, Message: "Duplicate column name 'tutorial_completed'"},
	}
	err := EnsureMySQLSchema(context.Background(), exec, SchemaOptions{
		AutoCreate:      true,
		CreateDatabases: true,
		DatabasePlan:    repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("EnsureMySQLSchema() duplicate migration error = %v", err)
	}
	if len(exec.statements) != 55 {
		t.Fatalf("executed statements = %d, want 55", len(exec.statements))
	}
}

func TestEnsureMySQLSchemaIgnoresExistingTutorialRewardFlagColumn(t *testing.T) {
	exec := &fakeSchemaExec{
		failAt:  46,
		failErr: &mysql.MySQLError{Number: 1060, Message: "Duplicate column name 'tutorial_reward_progress_38'"},
	}
	err := EnsureMySQLSchema(context.Background(), exec, SchemaOptions{
		AutoCreate:      true,
		CreateDatabases: true,
		DatabasePlan:    repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("EnsureMySQLSchema() duplicate reward migration error = %v", err)
	}
	if len(exec.statements) != 55 {
		t.Fatalf("executed statements = %d, want 55", len(exec.statements))
	}
}

func TestEnsureMySQLSchemaIgnoresExistingStoryDigestColumns(t *testing.T) {
	for _, test := range []struct {
		name   string
		failAt int
		column string
	}{
		{name: "last level", failAt: 47, column: "story_digest_last_level"},
		{name: "migration version", failAt: 48, column: "story_digest_migration_version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			exec := &fakeSchemaExec{
				failAt:  test.failAt,
				failErr: &mysql.MySQLError{Number: 1060, Message: "Duplicate column name '" + test.column + "'"},
			}
			err := EnsureMySQLSchema(context.Background(), exec, SchemaOptions{
				AutoCreate:      true,
				CreateDatabases: true,
				DatabasePlan:    repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
			})
			if err != nil {
				t.Fatalf("EnsureMySQLSchema() duplicate %s error = %v", test.column, err)
			}
			if len(exec.statements) != 55 {
				t.Fatalf("executed statements = %d, want 55", len(exec.statements))
			}
		})
	}
}

func TestEnsureMySQLSchemaIgnoresExistingCeraColumn(t *testing.T) {
	exec := &fakeSchemaExec{
		failAt:  49,
		failErr: &mysql.MySQLError{Number: 1060, Message: "Duplicate column name 'cera'"},
	}
	err := EnsureMySQLSchema(context.Background(), exec, SchemaOptions{
		AutoCreate:      true,
		CreateDatabases: true,
		DatabasePlan:    repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("EnsureMySQLSchema() duplicate cera migration error = %v", err)
	}
	if len(exec.statements) != 55 {
		t.Fatalf("executed statements = %d, want 55", len(exec.statements))
	}
}

func TestEnsureMySQLSchemaStopsOnTutorialFlagMigrationError(t *testing.T) {
	exec := &fakeSchemaExec{failAt: 45}
	err := EnsureMySQLSchema(context.Background(), exec, SchemaOptions{
		AutoCreate:      true,
		CreateDatabases: true,
		DatabasePlan:    repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if !errors.Is(err, errFakeSchemaExec) {
		t.Fatalf("EnsureMySQLSchema() migration error = %v, want fake error", err)
	}
	if len(exec.statements) != 45 {
		t.Fatalf("executed statements = %d, want 45", len(exec.statements))
	}
}

func TestEnsureMySQLSchemaStopsOnExecError(t *testing.T) {
	exec := &fakeSchemaExec{failAt: 2}
	err := EnsureMySQLSchema(context.Background(), exec, SchemaOptions{
		AutoCreate:      true,
		CreateDatabases: true,
		DatabasePlan:    repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if !errors.Is(err, errFakeSchemaExec) {
		t.Fatalf("EnsureMySQLSchema() error = %v, want fake error", err)
	}
	if len(exec.statements) != 2 {
		t.Fatalf("executed statements = %d, want 2", len(exec.statements))
	}
}

func TestMySQLSchemaRejectsBadTablePrefix(t *testing.T) {
	_, err := MySQLSchema(SchemaOptions{
		TablePrefix:  "dnf;drop",
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if !errors.Is(err, ErrSchemaConfigInvalid) {
		t.Fatalf("MySQLSchema() error = %v, want ErrSchemaConfigInvalid", err)
	}
}

func embeddedCSharpTableCount(t *testing.T) int {
	t.Helper()
	raw, err := csharpSchemaFS.ReadFile(csharpSchemaFile)
	if err != nil {
		t.Fatalf("read embedded C# schema: %v", err)
	}
	count := 0
	for _, statement := range splitSQL(stripSQLComments(string(raw))) {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "CREATE TABLE IF NOT EXISTS ") {
			count++
		}
	}
	if count == 0 {
		t.Fatal("embedded C# schema has no CREATE TABLE statements")
	}
	return count
}

func countSQLPrefix(statements []string, prefix string) int {
	count := 0
	for _, statement := range statements {
		if strings.HasPrefix(statement, prefix) {
			count++
		}
	}
	return count
}
