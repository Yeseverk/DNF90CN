package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
)

type legacyJSONColumn struct {
	table  string
	column string
}

var legacyRepositoryJSONColumns = []legacyJSONColumn{
	{table: mysqlAccountTable, column: "metadata_json"},
	{table: mysqlAccountInventoryTable, column: "slots_json"},
	{table: mysqlCharTable, column: "stats_json"},
	{table: mysqlCharTable, column: "location_json"},
	{table: mysqlCharTable, column: "roster_json"},
	{table: mysqlBagTable, column: "slots_json"},
	{table: mysqlBagTable, column: "warehouse_json"},
	{table: mysqlEquipmentTable, column: "entries_json"},
	{table: mysqlPetTable, column: "entries_json"},
	{table: mysqlQuestTable, column: "states_json"},
	{table: mysqlQuestTable, column: "progress_json"},
	{table: mysqlSkillTable, column: "skills_json"},
	{table: mysqlSkillTable, column: "points_json"},
	{table: mysqlSkillTable, column: "layouts_json"},
	{table: mysqlSkillTable, column: "cooldowns_json"},
	{table: mysqlMailboxTable, column: "mails_json"},
	{table: mysqlPacketTable, column: "metadata_json"},
	{table: mysqlSettingsTable, column: "values_json"},
}

// MigrateLegacyJSONStorage imports every legacy repository JSON column into
// typed relational tables. Only after every import succeeds are the obsolete
// columns dropped. The operation is idempotent: interrupted imports are
// replaced from the still-authoritative legacy column on the next start.
func MigrateLegacyJSONStorage(ctx context.Context, database SQLDB, options SchemaOptions) error {
	if !options.AutoCreate {
		return nil
	}
	if database == nil {
		return ErrMySQLDBRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tablePrefix, err := normTablePrefix(options.TablePrefix)
	if err != nil {
		return err
	}
	databases, err := repository.ValidateDatabases(options.DatabasePlan.SchemaDatabases())
	if err != nil {
		return err
	}
	for _, databaseName := range databases {
		if err := migrateLegacyJSONDatabase(ctx, database, databaseName, tablePrefix, options); err != nil {
			return fmt.Errorf("migrate legacy JSON storage in %s: %w", databaseName, err)
		}
	}
	return nil
}

func migrateLegacyJSONDatabase(
	ctx context.Context,
	database SQLDB,
	databaseName, tablePrefix string,
	options SchemaOptions,
) error {
	plan := repository.DatabasePlan{
		ShardID:       options.DatabasePlan.ShardID,
		GroupID:       options.DatabasePlan.GroupID,
		ReadDatabases: []string{databaseName},
		WriteDatabases: []string{
			databaseName,
		},
	}
	router, err := newMySQLRouter(database, MySQLGroupOptions{
		DatabasePlan: plan,
		TablePrefix:  tablePrefix,
	})
	if err != nil {
		return err
	}
	group := newMySQLGroupFromRouter(router, true)

	migrations := []func(context.Context, SQLDB, mysqlRouter, repository.Group, string, string) error{
		migrateLegacyAccountMetadata,
		migrateLegacyAccountInventory,
		migrateLegacyCharacterStats,
		migrateLegacyCharacterLocation,
		migrateLegacyCharacterRoster,
		migrateLegacyInventorySlots,
		migrateLegacyInventoryWarehouse,
		migrateLegacyEquipment,
		migrateLegacyPets,
		migrateLegacyQuestStates,
		migrateLegacyQuestProgress,
		migrateLegacySkillStates,
		migrateLegacySkillPoints,
		migrateLegacySkillLayouts,
		migrateLegacySkillCooldowns,
		migrateLegacyMailboxes,
		migrateLegacyPacketMetadata,
		migrateLegacySettings,
	}
	for index, migration := range migrations {
		column := legacyRepositoryJSONColumns[index]
		exists, err := legacyJSONColumnExists(ctx, database, databaseName, tablePrefix+"_"+column.table, column.column)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := migration(ctx, database, router, group, databaseName, tablePrefix); err != nil {
			return fmt.Errorf("%s.%s: %w", column.table, column.column, err)
		}
	}
	if err := migrateLegacyItemExtraColumn(ctx, database, databaseName, tablePrefix); err != nil {
		return fmt.Errorf("legacy_character_items.extra_json: %w", err)
	}
	if err := migrateLegacyItemAuditPayloadColumn(ctx, database, databaseName, tablePrefix); err != nil {
		return fmt.Errorf("legacy_item_audit_log.payload_json: %w", err)
	}
	for _, column := range legacyRepositoryJSONColumns {
		exists, err := legacyJSONColumnExists(ctx, database, databaseName, tablePrefix+"_"+column.table, column.column)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		table := mysqlTable(databaseName, tablePrefix, column.table)
		if _, err := database.ExecContext(ctx, "ALTER TABLE "+table+" DROP COLUMN "+quoteSQLIdentifier(column.column)); err != nil {
			return fmt.Errorf("drop %s.%s: %w", column.table, column.column, err)
		}
	}
	return nil
}

type legacyAuditPayloadValue struct {
	path        string
	valueType   string
	stringValue any
	numberValue any
	boolValue   any
}

func migrateLegacyItemAuditPayloadColumn(
	ctx context.Context,
	database SQLDB,
	databaseName, tablePrefix string,
) error {
	tableName := tablePrefix + "_legacy_item_audit_log"
	exists, err := legacyColumnExists(ctx, database, databaseName, tableName, "payload_json")
	if err != nil || !exists {
		return err
	}
	table := mysqlTable(databaseName, tablePrefix, "legacy_item_audit_log")
	payloadTable := mysqlTable(databaseName, tablePrefix, mysqlLegacyItemAuditPayloadTable)
	rows, err := database.QueryContext(ctx, "SELECT audit_id, payload_json FROM "+table+" ORDER BY audit_id")
	if err != nil {
		return err
	}
	type auditPayload struct {
		auditID int64
		values  []legacyAuditPayloadValue
	}
	payloads := make([]auditPayload, 0)
	for rows.Next() {
		var auditID int64
		var raw sql.NullString
		if err := rows.Scan(&auditID, &raw); err != nil {
			rows.Close()
			return err
		}
		values, err := decodeLegacyAuditPayload(raw.String)
		if err != nil {
			rows.Close()
			return fmt.Errorf("audit_id %d: %w", auditID, err)
		}
		payloads = append(payloads, auditPayload{auditID: auditID, values: values})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := withMySQLWriteExecutor(ctx, database, func(executor SQLDB) error {
		insert := "INSERT INTO " + payloadTable + " (audit_id, value_path, value_type, string_value, number_value, bool_value) VALUES (?, ?, ?, ?, ?, ?)"
		for _, payload := range payloads {
			if _, err := executor.ExecContext(ctx, "DELETE FROM "+payloadTable+" WHERE audit_id = ?", payload.auditID); err != nil {
				return err
			}
			for _, value := range payload.values {
				if _, err := executor.ExecContext(
					ctx,
					insert,
					payload.auditID,
					value.path,
					value.valueType,
					value.stringValue,
					value.numberValue,
					value.boolValue,
				); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, "ALTER TABLE "+table+" DROP COLUMN "+quoteSQLIdentifier("payload_json"))
	return err
}

func decodeLegacyAuditPayload(raw string) ([]legacyAuditPayloadValue, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	out := make([]legacyAuditPayloadValue, 0)
	if err := flattenLegacyAuditPayload(&out, "", value); err != nil {
		return nil, err
	}
	return out, nil
}

func flattenLegacyAuditPayload(out *[]legacyAuditPayloadValue, path string, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range sortedStringKeys(typed) {
			next := path + "/" + escapeJSONPointerToken(key)
			if err := flattenLegacyAuditPayload(out, next, typed[key]); err != nil {
				return err
			}
		}
	case []any:
		for index, entry := range typed {
			if err := flattenLegacyAuditPayload(out, fmt.Sprintf("%s/%d", path, index), entry); err != nil {
				return err
			}
		}
	case string:
		*out = append(*out, legacyAuditPayloadValue{path: path, valueType: "string", stringValue: typed})
	case json.Number:
		*out = append(*out, legacyAuditPayloadValue{path: path, valueType: "number", numberValue: typed.String()})
	case bool:
		*out = append(*out, legacyAuditPayloadValue{path: path, valueType: "boolean", boolValue: typed})
	case nil:
		*out = append(*out, legacyAuditPayloadValue{path: path, valueType: "null"})
	default:
		return fmt.Errorf("path %s has unsupported value type", path)
	}
	return nil
}

func escapeJSONPointerToken(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

func migrateLegacyItemExtraColumn(
	ctx context.Context,
	database SQLDB,
	databaseName, tablePrefix string,
) error {
	tableName := tablePrefix + "_legacy_character_items"
	exists, err := legacyColumnExists(ctx, database, databaseName, tableName, "extra_json")
	if err != nil || !exists {
		return err
	}
	table := mysqlTable(databaseName, tablePrefix, "legacy_character_items")
	extraTable := mysqlTable(databaseName, tablePrefix, mysqlLegacyItemExtraTable)
	rows, err := database.QueryContext(ctx, "SELECT item_uid, extra_json FROM "+table+" ORDER BY item_uid")
	if err != nil {
		return err
	}
	type legacyItemExtra struct {
		itemUID int64
		values  map[string]string
	}
	records := make([]legacyItemExtra, 0)
	for rows.Next() {
		var itemUID int64
		var raw sql.NullString
		if err := rows.Scan(&itemUID, &raw); err != nil {
			rows.Close()
			return err
		}
		values, err := decodeLegacyItemExtra(raw.String)
		if err != nil {
			rows.Close()
			return fmt.Errorf("item_uid %d: %w", itemUID, err)
		}
		records = append(records, legacyItemExtra{itemUID: itemUID, values: values})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := withMySQLWriteExecutor(ctx, database, func(executor SQLDB) error {
		insert := "INSERT INTO " + extraTable + " (item_uid, extra_key, extra_value) VALUES (?, ?, ?)"
		for _, record := range records {
			if _, err := executor.ExecContext(ctx, "DELETE FROM "+extraTable+" WHERE item_uid = ?", record.itemUID); err != nil {
				return err
			}
			for _, key := range sortedStringKeys(record.values) {
				if _, err := executor.ExecContext(ctx, insert, record.itemUID, key, record.values[key]); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, "ALTER TABLE "+table+" DROP COLUMN "+quoteSQLIdentifier("extra_json"))
	return err
}

func decodeLegacyItemExtra(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || raw == "null" {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	values := make(map[string]any)
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			out[key] = typed
		case json.Number:
			out[key] = typed.String()
		case bool:
			if typed {
				out[key] = "1"
			} else {
				out[key] = "0"
			}
		case nil:
			out[key] = ""
		default:
			return nil, fmt.Errorf("extra key %s has unsupported nested value", key)
		}
	}
	return out, nil
}

func legacyJSONColumnExists(
	ctx context.Context,
	database SQLDB,
	schemaName, tableName, columnName string,
) (bool, error) {
	var count int
	err := database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = ? AND DATA_TYPE = 'json'",
		schemaName,
		tableName,
		columnName,
	).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

func legacyColumnExists(
	ctx context.Context,
	database SQLDB,
	schemaName, tableName, columnName string,
) (bool, error) {
	var count int
	err := database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = ?",
		schemaName,
		tableName,
		columnName,
	).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

func migrateLegacyAccountMetadata(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyStringMap(ctx, database, databaseName, tablePrefix, mysqlAccountTable, "account_id", "metadata_json", func(key string, values map[string]string) error {
		record, ok, err := group.Account.Load(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("account %s not found", key)
		}
		record.Metadata = values
		return group.Account.Save(ctx, record)
	})
}

func migrateLegacyAccountInventory(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyRecordJSON(ctx, database, databaseName, tablePrefix, mysqlAccountInventoryTable, "account_id", "slots_json", func(key string, value sql.NullString, updatedAt sql.NullTime) error {
		if !value.Valid {
			return nil
		}
		var slots map[string]repository.ItemStack
		if err := scanJSON(value, &slots); err != nil {
			return err
		}
		return group.AccountInventory.Save(ctx, repository.AccountInventoryRecord{
			AccountID: key,
			Slots:     slots,
			UpdatedAt: scanTime(updatedAt),
		})
	})
}

func migrateLegacyCharacterStats(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyCharacterField(ctx, database, group, databaseName, tablePrefix, "stats_json", repository.CharacterFieldStats, func(record *repository.CharacterRecord, value sql.NullString) error {
		if !value.Valid {
			return nil
		}
		var stats map[string]int64
		if err := scanJSON(value, &stats); err != nil {
			return err
		}
		for key, statValue := range stats {
			record.Stats[key] = statValue
		}
		return nil
	})
}

func migrateLegacyCharacterLocation(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyCharacterField(ctx, database, group, databaseName, tablePrefix, "location_json", repository.CharacterFieldLocation, func(record *repository.CharacterRecord, value sql.NullString) error {
		if !value.Valid {
			return nil
		}
		return scanJSON(value, &record.Location)
	})
}

func migrateLegacyCharacterRoster(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyCharacterField(ctx, database, group, databaseName, tablePrefix, "roster_json", repository.CharacterFieldRoster, func(record *repository.CharacterRecord, value sql.NullString) error {
		if !value.Valid {
			return nil
		}
		return scanJSON(value, &record.Roster)
	})
}

func migrateLegacyCharacterField(
	ctx context.Context,
	database SQLDB,
	group repository.Group,
	databaseName, tablePrefix, column string,
	field repository.CharacterField,
	decode func(*repository.CharacterRecord, sql.NullString) error,
) error {
	table := mysqlTable(databaseName, tablePrefix, mysqlCharTable)
	rows, err := database.QueryContext(ctx, "SELECT character_id, "+quoteSQLIdentifier(column)+" FROM "+table+" WHERE "+quoteSQLIdentifier(column)+" IS NOT NULL ORDER BY character_id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var characterID string
		var value sql.NullString
		if err := rows.Scan(&characterID, &value); err != nil {
			return err
		}
		record, ok, err := group.Character.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("character %s not found", characterID)
		}
		if err := decode(&record, value); err != nil {
			return fmt.Errorf("character %s: %w", characterID, err)
		}
		if err := repository.SaveCharacterFields(ctx, group.Character, record, field); err != nil {
			return fmt.Errorf("character %s: %w", characterID, err)
		}
	}
	return rows.Err()
}

func migrateLegacyInventorySlots(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyInventoryField(ctx, database, group, databaseName, tablePrefix, "slots_json", repository.InventoryFieldSlots)
}

func migrateLegacyInventoryWarehouse(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyInventoryField(ctx, database, group, databaseName, tablePrefix, "warehouse_json", repository.InventoryFieldWarehouse)
}

func migrateLegacyInventoryField(
	ctx context.Context,
	database SQLDB,
	group repository.Group,
	databaseName, tablePrefix, column string,
	field repository.InventoryField,
) error {
	return migrateLegacyRecordJSON(ctx, database, databaseName, tablePrefix, mysqlBagTable, "character_id", column, func(key string, value sql.NullString, updatedAt sql.NullTime) error {
		if !value.Valid {
			return nil
		}
		record, ok, err := group.Inventory.Load(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			record.CharacterID = key
			record.UpdatedAt = scanTime(updatedAt)
		}
		switch field {
		case repository.InventoryFieldSlots:
			if err := scanJSON(value, &record.Slots); err != nil {
				return err
			}
		case repository.InventoryFieldWarehouse:
			if err := scanJSON(value, &record.Warehouse); err != nil {
				return err
			}
		}
		return repository.SaveInventoryFields(ctx, group.Inventory, record, field)
	})
}

func migrateLegacyEquipment(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyRecordJSON(ctx, database, databaseName, tablePrefix, mysqlEquipmentTable, "character_id", "entries_json", func(key string, value sql.NullString, updatedAt sql.NullTime) error {
		if !value.Valid {
			return nil
		}
		var entries map[string]repository.EquipmentEntry
		if err := scanJSON(value, &entries); err != nil {
			return err
		}
		return group.Equipment.Save(ctx, repository.EquipmentRecord{
			CharacterID: key,
			Entries:     entries,
			UpdatedAt:   scanTime(updatedAt),
		})
	})
}

func migrateLegacyPets(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyRecordJSON(ctx, database, databaseName, tablePrefix, mysqlPetTable, "character_id", "entries_json", func(key string, value sql.NullString, updatedAt sql.NullTime) error {
		if !value.Valid {
			return nil
		}
		record, ok, err := group.Pet.Load(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			record.CharacterID = key
			record.UpdatedAt = scanTime(updatedAt)
		}
		if err := scanPetEntries(value, &record); err != nil {
			return err
		}
		return repository.SavePetFields(ctx, group.Pet, record, repository.PetFieldEntries)
	})
}

func migrateLegacyQuestStates(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyQuestField(ctx, database, group, databaseName, tablePrefix, "states_json", repository.QuestFieldStates)
}

func migrateLegacyQuestProgress(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyQuestField(ctx, database, group, databaseName, tablePrefix, "progress_json", repository.QuestFieldProgress)
}

func migrateLegacyQuestField(
	ctx context.Context,
	database SQLDB,
	group repository.Group,
	databaseName, tablePrefix, column string,
	field repository.QuestField,
) error {
	return migrateLegacyRecordJSON(ctx, database, databaseName, tablePrefix, mysqlQuestTable, "character_id", column, func(key string, value sql.NullString, updatedAt sql.NullTime) error {
		if !value.Valid {
			return nil
		}
		record, ok, err := group.Quest.Load(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			record.CharacterID = key
			record.UpdatedAt = scanTime(updatedAt)
		}
		switch field {
		case repository.QuestFieldStates:
			if err := scanJSON(value, &record.States); err != nil {
				return err
			}
		case repository.QuestFieldProgress:
			if err := scanJSON(value, &record.Progress); err != nil {
				return err
			}
		}
		return repository.SaveQuestFields(ctx, group.Quest, record, field)
	})
}

func migrateLegacySkillStates(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacySkillField(ctx, database, group, databaseName, tablePrefix, "skills_json", repository.SkillFieldSkills)
}

func migrateLegacySkillPoints(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacySkillField(ctx, database, group, databaseName, tablePrefix, "points_json", repository.SkillFieldPoints)
}

func migrateLegacySkillLayouts(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacySkillField(ctx, database, group, databaseName, tablePrefix, "layouts_json", repository.SkillFieldLayouts)
}

func migrateLegacySkillCooldowns(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacySkillField(ctx, database, group, databaseName, tablePrefix, "cooldowns_json", repository.SkillFieldCooldowns)
}

func migrateLegacySkillField(
	ctx context.Context,
	database SQLDB,
	group repository.Group,
	databaseName, tablePrefix, column string,
	field repository.SkillField,
) error {
	return migrateLegacyRecordJSON(ctx, database, databaseName, tablePrefix, mysqlSkillTable, "character_id", column, func(key string, value sql.NullString, updatedAt sql.NullTime) error {
		if !value.Valid {
			return nil
		}
		record, ok, err := group.Skill.Load(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			record.CharacterID = key
			record.UpdatedAt = scanTime(updatedAt)
		}
		switch field {
		case repository.SkillFieldSkills:
			if err := scanJSON(value, &record.Skills); err != nil {
				return err
			}
		case repository.SkillFieldPoints:
			if err := scanJSON(value, &record.Points); err != nil {
				return err
			}
		case repository.SkillFieldLayouts:
			if err := scanJSON(value, &record.Layouts); err != nil {
				return err
			}
		case repository.SkillFieldCooldowns:
			if err := scanJSON(value, &record.Cooldowns); err != nil {
				return err
			}
		}
		return repository.SaveSkillFields(ctx, group.Skill, record, field)
	})
}

func migrateLegacyMailboxes(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyRecordJSON(ctx, database, databaseName, tablePrefix, mysqlMailboxTable, "character_id", "mails_json", func(key string, value sql.NullString, updatedAt sql.NullTime) error {
		if !value.Valid {
			return nil
		}
		var mails map[string]repository.MailRecord
		if err := scanJSON(value, &mails); err != nil {
			return err
		}
		return group.Mailbox.Save(ctx, repository.MailboxRecord{
			CharacterID: key,
			Mails:       mails,
			UpdatedAt:   scanTime(updatedAt),
		})
	})
}

func migrateLegacyPacketMetadata(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyStringMap(ctx, database, databaseName, tablePrefix, mysqlPacketTable, "template_id", "metadata_json", func(key string, values map[string]string) error {
		record, ok, err := group.PacketTemplate.Load(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("packet template %s not found", key)
		}
		record.Metadata = values
		return group.PacketTemplate.Save(ctx, record)
	})
}

func migrateLegacySettings(
	ctx context.Context,
	database SQLDB,
	router mysqlRouter,
	group repository.Group,
	databaseName, tablePrefix string,
) error {
	return migrateLegacyStringMap(ctx, database, databaseName, tablePrefix, mysqlSettingsTable, "scope", "values_json", func(key string, values map[string]string) error {
		record, ok, err := group.Settings.Load(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("settings %s not found", key)
		}
		record.Values = values
		return group.Settings.Save(ctx, record)
	})
}

func migrateLegacyStringMap(
	ctx context.Context,
	database SQLDB,
	databaseName, tablePrefix, tableSuffix, keyColumn, valueColumn string,
	save func(string, map[string]string) error,
) error {
	table := mysqlTable(databaseName, tablePrefix, tableSuffix)
	rows, err := database.QueryContext(
		ctx,
		"SELECT "+quoteSQLIdentifier(keyColumn)+", "+quoteSQLIdentifier(valueColumn)+" FROM "+table+" WHERE "+quoteSQLIdentifier(valueColumn)+" IS NOT NULL ORDER BY "+quoteSQLIdentifier(keyColumn),
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value sql.NullString
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		var values map[string]string
		if err := scanJSON(value, &values); err != nil {
			return fmt.Errorf("%s %s: %w", keyColumn, key, err)
		}
		if err := save(key, values); err != nil {
			return fmt.Errorf("%s %s: %w", keyColumn, key, err)
		}
	}
	return rows.Err()
}

func migrateLegacyRecordJSON(
	ctx context.Context,
	database SQLDB,
	databaseName, tablePrefix, tableSuffix, keyColumn, valueColumn string,
	save func(string, sql.NullString, sql.NullTime) error,
) error {
	table := mysqlTable(databaseName, tablePrefix, tableSuffix)
	query := "SELECT " + quoteSQLIdentifier(keyColumn) + ", " + quoteSQLIdentifier(valueColumn) + ", updated_at FROM " + table +
		" WHERE " + quoteSQLIdentifier(valueColumn) + " IS NOT NULL ORDER BY " + quoteSQLIdentifier(keyColumn)
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value sql.NullString
		var updatedAt sql.NullTime
		if err := rows.Scan(&key, &value, &updatedAt); err != nil {
			return err
		}
		if err := save(strings.TrimSpace(key), value, updatedAt); err != nil {
			return fmt.Errorf("%s %s: %w", keyColumn, key, err)
		}
	}
	return rows.Err()
}
