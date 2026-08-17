// 本文件实现 DNF 角色仓储的 MySQL 字段化读写。
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

const mysqlCharTable = "characters"

const (
	mysqlCharacterTutorialCompletedStatColumn        = "tutorial_completed"
	mysqlCharacterTutorialCompletedStatDDL           = "TINYINT UNSIGNED NOT NULL DEFAULT 0"
	mysqlCharacterTutorialRewardProgress38StatColumn = "tutorial_reward_progress_38"
	mysqlCharacterTutorialRewardProgress38StatDDL    = "TINYINT UNSIGNED NOT NULL DEFAULT 0"
	mysqlCharacterStoryDigestLastLevelStatColumn     = repository.CharacterStoryDigestLastLevelStatKey
	mysqlCharacterStoryDigestLastLevelStatDDL        = "INT UNSIGNED NOT NULL DEFAULT 0"
	mysqlCharacterStoryDigestMigrationVersionColumn  = repository.CharacterStoryDigestMigrationVersionStatKey
	mysqlCharacterStoryDigestMigrationVersionDDL     = "SMALLINT UNSIGNED NOT NULL DEFAULT 0"
	mysqlCharacterCeraStatColumn                     = "cera"
	mysqlCharacterCeraStatDDL                        = "BIGINT NOT NULL DEFAULT 0"
	mysqlCharacterCrystalSelectionStatColumn         = "premium_crystal_selection"
	mysqlCharacterCrystalSelectionStatDDL            = "TINYINT NOT NULL DEFAULT -1"
)

type mysqlCharacterStatColumn struct {
	column   string
	key      string
	ddl      string
	fallback int64
}

var mysqlCharacterStatColumns = buildMySQLCharacterStatColumns()

func buildMySQLCharacterStatColumns() []mysqlCharacterStatColumn {
	columns := []mysqlCharacterStatColumn{
		{column: "grow_type", key: "grow_type", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "exp", key: "exp", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "ex_equip_slot_stat", key: "ex_equip_slot_stat", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "bonus_sp", key: "bonus_sp", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "bonus_tp", key: "bonus_tp", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "pvp_grade", key: "pvp_grade", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "pvp_rating_grade", key: "pvp_rating_grade", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "user_state", key: "user_state", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: mysqlCharacterTutorialCompletedStatColumn, key: mysqlCharacterTutorialCompletedStatColumn, ddl: mysqlCharacterTutorialCompletedStatDDL},
		{column: mysqlCharacterTutorialRewardProgress38StatColumn, key: mysqlCharacterTutorialRewardProgress38StatColumn, ddl: mysqlCharacterTutorialRewardProgress38StatDDL},
		{column: mysqlCharacterStoryDigestLastLevelStatColumn, key: repository.CharacterStoryDigestLastLevelStatKey, ddl: mysqlCharacterStoryDigestLastLevelStatDDL},
		{column: mysqlCharacterStoryDigestMigrationVersionColumn, key: repository.CharacterStoryDigestMigrationVersionStatKey, ddl: mysqlCharacterStoryDigestMigrationVersionDDL},
		{column: "gold", key: "gold", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: mysqlCharacterCeraStatColumn, key: mysqlCharacterCeraStatColumn, ddl: mysqlCharacterCeraStatDDL},
		{column: "coin", key: "coin", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "town_id", key: "town_id", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 38", fallback: 38},
		{column: "area_id", key: "area_id", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 1", fallback: 1},
		{column: "pos_x", key: "pos_x", ddl: "SMALLINT NOT NULL DEFAULT 450", fallback: 450},
		{column: "pos_y", key: "pos_y", ddl: "SMALLINT NOT NULL DEFAULT 234", fallback: 234},
		{column: "direction", key: "direction", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 5", fallback: 5},
		{column: "area_state", key: "area_state", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 3", fallback: 3},
		{column: "delete_flag", key: "delete_flag", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "is_event_character", key: "is_event_character", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "name_tag_item_id", key: "name_tag_item_id", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "stamina", key: "stamina", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "fatigue", key: "fatigue", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 156", fallback: 156},
		{column: "fatigue_limit", key: "fatigue_limit", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 156", fallback: 156},
		{column: "fatigue_penalty", key: "fatigue_penalty", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "pc_room_id", key: "pc_room_id", ddl: "BIGINT NOT NULL DEFAULT 65537", fallback: 0x00010001},
		{column: "is_private_store", key: "is_private_store", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "is_premium_pc_room", key: "is_premium_pc_room", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "server_group_id", key: "server_group_id", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "black_count", key: "black_count", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "guild_level", key: "guild_level", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "chaos_point", key: "chaos_point", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "disguise_kind", key: "disguise_kind", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "is_disguised", key: "is_disguised", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "expert_job_type", key: "expert_job_type", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "expert_job_exp", key: "expert_job_exp", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "extra46", key: "extra46", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "extra47", key: "extra47", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "extra51", key: "extra51", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "is_hardcore_mode", key: "is_hardcore_mode", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "is_hardcore_dead", key: "is_hardcore_dead", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "hardcore_death_count", key: "hardcore_death_count", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "user_state_bits", key: "user_state_bits", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 3", fallback: 3},
		{column: "chat_ban_end_time", key: "chat_ban_end_time", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "fatigue_update", key: "fatigue_update", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "return_user_flag", key: "return_user_flag", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 1", fallback: 1},
		{column: "channel_display_mode", key: "channel_display_mode", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "channel_type", key: "channel_type", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "channel_id", key: "channel_id", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 2", fallback: 2},
		{column: "is_return_user", key: "is_return_user", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "link_slot_enabled", key: "link_slot_enabled", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "link_type_a", key: "link_type_a", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "link_type_b", key: "link_type_b", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "emotion_index", key: "emotion_index", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "action_byte", key: "action_byte", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "fatigue_display_update", key: "fatigue_display_update", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "costume_flag", key: "costume_flag", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "aura_flag", key: "aura_flag", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: mysqlCharacterCrystalSelectionStatColumn, key: mysqlCharacterCrystalSelectionStatColumn, ddl: mysqlCharacterCrystalSelectionStatDDL, fallback: -1},
		{column: "pet_display_flag", key: "pet_display_flag", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "title_display_flag", key: "title_display_flag", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "pvp_stat_a", key: "pvp_stat_a", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "pvp_win_streak", key: "pvp_win_streak", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "pvp_lose_streak", key: "pvp_lose_streak", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "pvp_rank_point", key: "pvp_rank_point", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "trailing_byte", key: "trailing_byte", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		{column: "stat_block_marker", key: "stat_block_marker", ddl: "BIGINT NOT NULL DEFAULT 83", fallback: 83},
		{column: "stat_hp_max", key: "stat_hp_max", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "stat_mp_max", key: "stat_mp_max", ddl: "BIGINT NOT NULL DEFAULT 0"},
		{column: "stat_strength", key: "stat_strength", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_intelligence", key: "stat_intelligence", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_vitality", key: "stat_vitality", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_spirit", key: "stat_spirit", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_physical_attack", key: "stat_physical_attack", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_physical_defense", key: "stat_physical_defense", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_magical_attack", key: "stat_magical_attack", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_magical_defense", key: "stat_magical_defense", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_independent_attack", key: "stat_independent_attack", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_fire_resistance", key: "stat_fire_resistance", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_water_resistance", key: "stat_water_resistance", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_dark_resistance", key: "stat_dark_resistance", ddl: "INT NOT NULL DEFAULT 0"},
		{column: "stat_light_resistance", key: "stat_light_resistance", ddl: "INT NOT NULL DEFAULT 0"},
	}
	for idx := 0; idx < 18; idx++ {
		key := "active_status_resistance_" + mysqlTwoDigit(idx)
		columns = append(columns, mysqlCharacterStatColumn{column: key, key: key, ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"})
	}
	columns = append(columns,
		mysqlCharacterStatColumn{column: "stat_inventory_limit", key: "stat_inventory_limit", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "stat_hp_regen_speed", key: "stat_hp_regen_speed", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "stat_mp_regen_speed", key: "stat_mp_regen_speed", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "stat_move_speed", key: "stat_move_speed", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "stat_attack_speed", key: "stat_attack_speed", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "stat_cast_speed", key: "stat_cast_speed", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "stat_hit_recovery", key: "stat_hit_recovery", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "stat_jump_power", key: "stat_jump_power", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "stat_weight", key: "stat_weight", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "stat_level", key: "stat_level", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "name_tag_expire_time", key: "name_tag_expire_time", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "skill_tree_index", key: "skill_tree_index", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "equipped_creature_level", key: "equipped_creature_level", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "equip_list_trailing", key: "equip_list_trailing", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "manage_level", key: "manage_level", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "flag_byte", key: "flag_byte", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "guild_power_war", key: "guild_power_war", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "server_timestamp", key: "server_timestamp", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "quest_shop_count", key: "quest_shop_count", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "progress1", key: "progress1", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "progress2", key: "progress2", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "create_option_len", key: "create_option_len", ddl: "SMALLINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_state0", key: "roster_state0", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_time_a", key: "roster_time_a", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_time_b", key: "roster_time_b", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_value0", key: "roster_value0", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_value1", key: "roster_value1", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_value2", key: "roster_value2", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_reserved_a", key: "roster_reserved_a", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_reserved_b", key: "roster_reserved_b", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
	)
	for idx := 0; idx < 8; idx++ {
		key := "roster_linked_id_" + mysqlTwoDigit(idx)
		columns = append(columns, mysqlCharacterStatColumn{column: key, key: key, ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"})
	}
	columns = append(columns,
		mysqlCharacterStatColumn{column: "roster_value3", key: "roster_value3", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_object_id", key: "roster_object_id", ddl: "BIGINT NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_flag0_eq1", key: "roster_flag0_eq1", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_card_flag", key: "roster_card_flag", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_value5", key: "roster_value5", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
		mysqlCharacterStatColumn{column: "roster_display_flags", key: "roster_display_flags", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"},
	)
	for idx := 0; idx < 12; idx++ {
		key := "roster_tail_" + mysqlTwoDigit(idx)
		columns = append(columns, mysqlCharacterStatColumn{column: key, key: key, ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"})
	}
	columns = append(columns, mysqlCharacterStatColumn{column: "roster_flag6_eq1", key: "roster_flag6_eq1", ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"})
	for idx := 0; idx < 64; idx++ {
		key := "create_option_byte_" + mysqlTwoDigit(idx)
		columns = append(columns, mysqlCharacterStatColumn{column: key, key: key, ddl: "TINYINT UNSIGNED NOT NULL DEFAULT 0"})
	}
	return columns
}

func mysqlCharacterStatColumnDDL() string {
	lines := make([]string, 0, len(mysqlCharacterStatColumns))
	for _, stat := range mysqlCharacterStatColumns {
		lines = append(lines, "  "+quoteSQLIdentifier(stat.column)+" "+stat.ddl+",")
	}
	return strings.Join(lines, "\n")
}

func mysqlCharacterStatSelectList() string {
	columns := make([]string, len(mysqlCharacterStatColumns))
	for idx, stat := range mysqlCharacterStatColumns {
		columns[idx] = quoteSQLIdentifier(stat.column)
	}
	return strings.Join(columns, ", ")
}

func mysqlTwoDigit(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

type mysqlCharStore struct {
	mysqlStoreBase
}

func (s *mysqlCharStore) AdvanceStoryDigest(ctx context.Context, characterID string, level, migrationVersion uint32) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return repository.ErrCharacterStoryDigestCharacterMissing
	}
	table, err := s.router.writeTable(mysqlCharTable, characterID)
	if err != nil {
		return err
	}
	query := "UPDATE " + table + " SET " +
		quoteSQLIdentifier(mysqlCharacterStoryDigestLastLevelStatColumn) + " = GREATEST(" + quoteSQLIdentifier(mysqlCharacterStoryDigestLastLevelStatColumn) + ", ?), " +
		quoteSQLIdentifier(mysqlCharacterStoryDigestMigrationVersionColumn) + " = GREATEST(" + quoteSQLIdentifier(mysqlCharacterStoryDigestMigrationVersionColumn) + ", ?), " +
		quoteSQLIdentifier("updated_at") + " = ? WHERE " + quoteSQLIdentifier("character_id") + " = ?"
	_, err = s.router.db.ExecContext(ctx, query, int64(level), int64(migrationVersion), s.router.now().UTC(), characterID)
	return err
}

// Load 按角色 ID 从 MySQL 读取 DNF 角色记录。
func (s *mysqlCharStore) Load(ctx context.Context, characterID string) (repository.CharacterRecord, bool, error) {
	table, err := s.router.readTable(mysqlCharTable, characterID)
	if err != nil {
		return repository.CharacterRecord{}, false, err
	}
	query := s.router.selectQuery("SELECT character_id, account_id, slot, name, job, level, " + mysqlCharacterStatSelectList() + ", created_at, updated_at FROM " + table + " WHERE character_id = ?")
	var record repository.CharacterRecord
	var createdAt, updatedAt sql.NullTime
	mirrorStats := make([]sql.NullInt64, len(mysqlCharacterStatColumns))
	dest := []any{
		&record.CharacterID,
		&record.AccountID,
		&record.Slot,
		&record.Name,
		&record.Job,
		&record.Level,
	}
	for idx := range mirrorStats {
		dest = append(dest, &mirrorStats[idx])
	}
	dest = append(dest, &createdAt, &updatedAt)
	err = s.router.db.QueryRowContext(ctx, query, characterID).Scan(dest...)
	if err != nil {
		ok, scanErr := scanErr(err)
		return repository.CharacterRecord{}, ok, scanErr
	}
	mergeCharacterMirrorStats(&record, mirrorStats)
	if err := loadCharacterSupplement(ctx, s.router, characterID, &record); err != nil {
		return repository.CharacterRecord{}, false, err
	}
	record.CreatedAt = scanTime(createdAt)
	record.UpdatedAt = scanTime(updatedAt)
	return repository.CloneCharacter(record), true, nil
}

// Save 保存完整 DNF 角色记录到 MySQL。
func (s *mysqlCharStore) Save(ctx context.Context, record repository.CharacterRecord) error {
	return s.SaveFields(ctx, record, repository.AllCharacterFields()...)
}

// CreateCharacter 只执行 INSERT，避免账号槽位唯一键冲突时把旧角色 upsert 覆盖。
func (s *mysqlCharStore) CreateCharacter(ctx context.Context, record repository.CharacterRecord) error {
	characterID, err := requireRecordKey(repository.CharacterKey, record, "character")
	if err != nil {
		return err
	}
	table, err := s.router.writeTable(mysqlCharTable, characterID)
	if err != nil {
		return err
	}
	updatedAt := timeOrNow(record.UpdatedAt, s.router.now)
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = updatedAt
	}

	columns := []string{"character_id", "updated_at", "account_id", "slot", "name", "job", "level", "created_at"}
	args := []any{characterID, updatedAt, record.AccountID, record.Slot, record.Name, record.Job, record.Level, sqlTime(createdAt)}
	for _, stat := range mysqlCharacterStatColumns {
		columns = append(columns, stat.column)
		args = append(args, characterStatMirrorValue(record, stat.key, stat.fallback))
	}
	dirty := make(map[repository.CharacterField]struct{}, 3)
	for key := range record.Stats {
		known := false
		for _, stat := range mysqlCharacterStatColumns {
			if stat.key == key {
				known = true
				break
			}
		}
		if !known {
			dirty[repository.CharacterFieldStats] = struct{}{}
			break
		}
	}
	if record.Location != (repository.CharacterLocation{}) {
		dirty[repository.CharacterFieldLocation] = struct{}{}
	}
	if !isZeroCharacterRoster(record.Roster) {
		dirty[repository.CharacterFieldRoster] = struct{}{}
	}
	return withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		if _, execErr := database.ExecContext(ctx, buildInsert(table, columns), args...); execErr != nil {
			return execErr
		}
		return saveCharacterSupplement(ctx, database, s.router, characterID, record, dirty)
	})
}

// SaveFields 保存 DNF 角色指定字段到 MySQL。
// 插入路径会补角色基础字段；更新路径只写 dirty 字段，避免整包覆盖。
func (s *mysqlCharStore) SaveFields(ctx context.Context, record repository.CharacterRecord, fields ...repository.CharacterField) error {
	characterID, err := requireRecordKey(repository.CharacterKey, record, "character")
	if err != nil {
		return err
	}
	fields = repository.CharacterFields.Normalize(fields)
	if len(fields) == 0 {
		return nil
	}
	table, err := s.router.writeTable(mysqlCharTable, characterID)
	if err != nil {
		return err
	}
	updatedAt := timeOrNow(record.UpdatedAt, s.router.now)
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = updatedAt
	}

	columns := []string{"character_id", "updated_at"}
	args := []any{characterID, updatedAt}
	updates := []string{updateValue("updated_at")}
	dirty := make(map[repository.CharacterField]struct{}, len(fields))
	for _, field := range fields {
		dirty[field] = struct{}{}
	}

	columns = append(columns, "account_id", "slot", "name", "job", "level", "created_at")
	args = append(args, record.AccountID, record.Slot, record.Name, record.Job, record.Level, sqlTime(createdAt))
	for _, stat := range mysqlCharacterStatColumns {
		columns = append(columns, stat.column)
		args = append(args, characterStatMirrorValue(record, stat.key, stat.fallback))
	}
	if _, ok := dirty[repository.CharacterFieldBase]; ok {
		updates = append(updates,
			updateValue("account_id"),
			updateValue("slot"),
			updateValue("name"),
			updateValue("job"),
			updateValue("level"),
			keepCreatedAt("created_at"),
		)
	}
	if _, ok := dirty[repository.CharacterFieldStats]; ok {
		for _, stat := range mysqlCharacterStatColumns {
			if stat.key == repository.CharacterStoryDigestLastLevelStatKey || stat.key == repository.CharacterStoryDigestMigrationVersionStatKey {
				updates = append(updates, updateMonotonicValue(stat.column))
				continue
			}
			updates = append(updates, updateValue(stat.column))
		}
	}
	return withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		if _, execErr := database.ExecContext(ctx, buildUpsert(table, columns, updates), args...); execErr != nil {
			return execErr
		}
		return saveCharacterSupplement(ctx, database, s.router, characterID, record, dirty)
	})
}

func updateMonotonicValue(column string) string {
	quoted := quoteSQLIdentifier(column)
	return quoted + " = GREATEST(" + quoted + ", VALUES(" + quoted + "))"
}

func mergeCharacterMirrorStats(record *repository.CharacterRecord, values []sql.NullInt64) {
	record.Stats = make(map[string]int64, len(mysqlCharacterStatColumns))
	for idx, stat := range mysqlCharacterStatColumns {
		value := stat.fallback
		if idx < len(values) && values[idx].Valid {
			value = values[idx].Int64
		}
		record.Stats[stat.key] = value
	}
}

func characterStatMirrorValue(record repository.CharacterRecord, key string, fallback int64) int64 {
	if record.Stats == nil {
		return fallback
	}
	value, ok := record.Stats[key]
	if !ok {
		return fallback
	}
	if value < 0 {
		return 0
	}
	return value
}

// ListByAccount 按账号读取角色列表，供旧客户端选择角色列表回包使用。
// 这里只查询仓储记录，不做职业、槽位或创建规则判断。
func (s *mysqlCharStore) ListByAccount(ctx context.Context, accountID string, limit int) ([]repository.CharacterRecord, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, db.ErrRecordKeyRequired
	}
	if limit <= 0 {
		limit = repository.DefaultCharacterSlotLimit
	}
	ids := make([]string, 0, limit)
	for _, database := range s.router.readDBs {
		if len(ids) >= limit {
			break
		}
		table := mysqlTable(database, s.router.tablePrefix, mysqlCharTable)
		query := "SELECT COALESCE(GROUP_CONCAT(character_id ORDER BY slot ASC, character_id ASC SEPARATOR '\n'), '') FROM " + table + " WHERE account_id = ? AND delete_flag = 0"
		var packed sql.NullString
		if err := s.router.db.QueryRowContext(ctx, query, accountID).Scan(&packed); err != nil {
			return nil, err
		}
		if !packed.Valid || strings.TrimSpace(packed.String) == "" {
			continue
		}
		for _, id := range strings.Split(packed.String, "\n") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			ids = append(ids, id)
			if len(ids) >= limit {
				break
			}
		}
	}
	records := make([]repository.CharacterRecord, 0, len(ids))
	for _, id := range ids {
		record, ok, err := s.Load(ctx, id)
		if err != nil {
			return nil, err
		}
		if ok {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *mysqlCharStore) SwapCharacterSlots(ctx context.Context, accountID string, slotA, slotB int) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return db.ErrRecordKeyRequired
	}
	if slotA == slotB {
		return nil
	}
	if slotA < 0 || slotA >= repository.DefaultCharacterSlotLimit || slotB < 0 || slotB >= repository.DefaultCharacterSlotLimit {
		return repository.ErrCharacterSlotMissing
	}
	beginner, ok := s.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrCharacterSlotSwapUnavailable
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txRouter := s.router
	txRouter.db = sqlTransactionDB{tx: tx}
	txRouter.readDBs = append([]string(nil), s.router.writeDBs...)
	txRouter.writeDBs = append([]string(nil), s.router.writeDBs...)
	txRouter.lockReads = true

	type slotRow struct {
		id    string
		slot  int
		table string
	}
	found := map[int]slotRow{}
	for _, database := range txRouter.writeDBs {
		table := mysqlTable(database, txRouter.tablePrefix, mysqlCharTable)
		query := "SELECT character_id, slot FROM " + table + " WHERE account_id = ? AND delete_flag = 0 AND slot IN (?, ?) FOR UPDATE"
		rows, err := txRouter.db.QueryContext(ctx, query, accountID, slotA, slotB)
		if err != nil {
			return errors.Join(err, tx.Rollback())
		}
		for rows.Next() {
			var row slotRow
			if err := rows.Scan(&row.id, &row.slot); err != nil {
				_ = rows.Close()
				return errors.Join(err, tx.Rollback())
			}
			row.table = table
			found[row.slot] = row
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return errors.Join(err, tx.Rollback())
		}
		if err := rows.Close(); err != nil {
			return errors.Join(err, tx.Rollback())
		}
	}
	left, okLeft := found[slotA]
	right, okRight := found[slotB]
	now := s.router.now().UTC()
	if !okLeft || !okRight {
		return tx.Commit()
	}
	tempSlot := -1 - slotA - slotB*repository.DefaultCharacterSlotLimit
	if err := mysqlSwapCharacterSlotUpdate(ctx, txRouter.db, left.table, left.id, accountID, tempSlot, now); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := mysqlSwapCharacterSlotUpdate(ctx, txRouter.db, right.table, right.id, accountID, slotA, now); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := mysqlSwapCharacterSlotUpdate(ctx, txRouter.db, left.table, left.id, accountID, slotB, now); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func mysqlSwapCharacterSlotUpdate(ctx context.Context, db SQLDB, table, characterID, accountID string, slot int, updatedAt time.Time) error {
	query := "UPDATE " + table + " SET slot = ?, updated_at = ? WHERE character_id = ? AND account_id = ?"
	result, err := db.ExecContext(ctx, query, slot, sqlTime(updatedAt), characterID, accountID)
	if err != nil {
		return err
	}
	if result != nil {
		if affected, err := result.RowsAffected(); err == nil && affected != 1 {
			return repository.ErrCharacterSlotMissing
		}
	}
	return nil
}

// FindIDByName 按角色名查找现有角色 ID，用于创建前去重。
func (s *mysqlCharStore) FindIDByName(ctx context.Context, name string) (string, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return "", false, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, nil
	}
	for _, database := range s.router.readDBs {
		table := mysqlTable(database, s.router.tablePrefix, mysqlCharTable)
		query := "SELECT character_id FROM " + table + " WHERE name = ? AND delete_flag = 0 LIMIT 1"
		var id string
		err := s.router.db.QueryRowContext(ctx, query, name).Scan(&id)
		if err == nil {
			return id, true, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		return "", false, err
	}
	return "", false, nil
}

// NextNumericID 分配 latest upper 创建角色需要回传的 u16 数字 ID。
// 当前 DNF bridge 只在单区服重建链路使用；后续 owner 化时应改为数据库序列或雪花号映射。
func (s *mysqlCharStore) NextNumericID(ctx context.Context) (int, error) {
	if err := ctxErr(ctx); err != nil {
		return 0, err
	}
	maxID := 0
	for _, database := range s.router.writeDBs {
		table := mysqlTable(database, s.router.tablePrefix, mysqlCharTable)
		query := "SELECT COALESCE(MAX(CAST(character_id AS UNSIGNED)), 0) FROM " + table
		var value int
		if err := s.router.db.QueryRowContext(ctx, query).Scan(&value); err != nil {
			return 0, err
		}
		if value > maxID {
			maxID = value
		}
	}
	return maxID + 1, nil
}
