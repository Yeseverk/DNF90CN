// 本文件实现 DNF 技能仓储的 MySQL 字段化读写。
package mysql

import (
	"context"
	"database/sql"
	"longheng.io/server/internal/modules/dnf/repository"
	"sort"
	"time"
)

const mysqlSkillTable = "skills"

type mysqlSkillStore struct {
	mysqlStoreBase
}

// Load 按角色 ID 从 MySQL 读取 DNF 技能记录。
func (s *mysqlSkillStore) Load(ctx context.Context, characterID string) (repository.SkillRecord, bool, error) {
	table, err := s.router.readTable(mysqlSkillTable, characterID)
	if err != nil {
		return repository.SkillRecord{}, false, err
	}
	statesTable, err := s.router.readTable(mysqlSkillStatesTable, characterID)
	if err != nil {
		return repository.SkillRecord{}, false, err
	}
	layoutsTable, err := s.router.readTable(mysqlSkillLayoutsTable, characterID)
	if err != nil {
		return repository.SkillRecord{}, false, err
	}
	cooldownsTable, err := s.router.readTable(mysqlSkillCooldownsTable, characterID)
	if err != nil {
		return repository.SkillRecord{}, false, err
	}
	query := s.router.selectQuery("SELECT character_id, total_sp, remaining_sp, total_tp, remaining_tp, synced_level, updated_at FROM " + table + " WHERE character_id = ?")
	var record repository.SkillRecord
	var updatedAt sql.NullTime
	err = s.router.db.QueryRowContext(ctx, query, characterID).Scan(
		&record.CharacterID,
		&record.Points.TotalSP,
		&record.Points.RemainingSP,
		&record.Points.TotalTP,
		&record.Points.RemainingTP,
		&record.Points.SyncedLevel,
		&updatedAt,
	)
	if err != nil {
		ok, scanErr := scanErr(err)
		return repository.SkillRecord{}, ok, scanErr
	}
	record.Skills, err = loadSkillStates(ctx, s.router, statesTable, characterID)
	if err != nil {
		return repository.SkillRecord{}, false, err
	}
	record.Layouts, err = loadSkillLayouts(ctx, s.router, layoutsTable, characterID)
	if err != nil {
		return repository.SkillRecord{}, false, err
	}
	record.Cooldowns, err = loadSkillCooldowns(ctx, s.router, cooldownsTable, characterID)
	if err != nil {
		return repository.SkillRecord{}, false, err
	}
	record.UpdatedAt = scanTime(updatedAt)
	return repository.CloneSkill(record), true, nil
}

// Save 保存完整 DNF 技能记录到 MySQL。
func (s *mysqlSkillStore) Save(ctx context.Context, record repository.SkillRecord) error {
	return s.SaveFields(ctx, record, repository.AllSkillFields()...)
}

// SaveFields 保存 DNF 技能指定字段到 MySQL。
// 技能等级和冷却分开写，避免技能释放路径覆盖配置同步后的等级状态。
func (s *mysqlSkillStore) SaveFields(ctx context.Context, record repository.SkillRecord, fields ...repository.SkillField) error {
	characterID, err := requireRecordKey(repository.SkillKey, record, "skill")
	if err != nil {
		return err
	}
	fields = repository.SkillFields.Normalize(fields)
	if len(fields) == 0 {
		return nil
	}
	table, err := s.router.writeTable(mysqlSkillTable, characterID)
	if err != nil {
		return err
	}
	statesTable, err := s.router.writeTable(mysqlSkillStatesTable, characterID)
	if err != nil {
		return err
	}
	layoutsTable, err := s.router.writeTable(mysqlSkillLayoutsTable, characterID)
	if err != nil {
		return err
	}
	cooldownsTable, err := s.router.writeTable(mysqlSkillCooldownsTable, characterID)
	if err != nil {
		return err
	}
	columns := []string{"character_id", "updated_at"}
	args := []any{characterID, timeOrNow(record.UpdatedAt, s.router.now)}
	updates := []string{updateValue("updated_at")}
	for _, field := range fields {
		if field == repository.SkillFieldPoints {
			columns = append(columns, "total_sp", "remaining_sp", "total_tp", "remaining_tp", "synced_level")
			args = append(
				args,
				record.Points.TotalSP,
				record.Points.RemainingSP,
				record.Points.TotalTP,
				record.Points.RemainingTP,
				record.Points.SyncedLevel,
			)
			updates = append(
				updates,
				updateValue("total_sp"),
				updateValue("remaining_sp"),
				updateValue("total_tp"),
				updateValue("remaining_tp"),
				updateValue("synced_level"),
			)
		}
	}
	return withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		if _, execErr := database.ExecContext(ctx, buildUpsert(table, columns, updates), args...); execErr != nil {
			return execErr
		}
		for _, field := range fields {
			switch field {
			case repository.SkillFieldSkills:
				if err := replaceSkillStates(ctx, database, statesTable, characterID, record.Skills); err != nil {
					return err
				}
			case repository.SkillFieldLayouts:
				if err := replaceSkillLayouts(ctx, database, layoutsTable, characterID, record.Layouts); err != nil {
					return err
				}
			case repository.SkillFieldCooldowns:
				if err := replaceSkillCooldowns(ctx, database, cooldownsTable, characterID, record.Cooldowns); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func loadSkillStates(ctx context.Context, router mysqlRouter, table, characterID string) (map[int64]repository.SkillState, error) {
	rows, err := router.db.QueryContext(
		ctx,
		router.selectQuery("SELECT skill_id, skill_level, enabled FROM "+table+" WHERE character_id = ? ORDER BY skill_id"),
		characterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values map[int64]repository.SkillState
	for rows.Next() {
		var skillID int64
		var state repository.SkillState
		if err := rows.Scan(&skillID, &state.Level, &state.Enabled); err != nil {
			return nil, err
		}
		if values == nil {
			values = make(map[int64]repository.SkillState)
		}
		values[skillID] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func loadSkillLayouts(ctx context.Context, router mysqlRouter, table, characterID string) (map[int]repository.SkillLayout, error) {
	rows, err := router.db.QueryContext(
		ctx,
		router.selectQuery("SELECT tree_id, slot_index, skill_id FROM "+table+" WHERE character_id = ? ORDER BY tree_id, slot_index"),
		characterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var layouts map[int]repository.SkillLayout
	for rows.Next() {
		var treeID, slot int
		var skillID uint16
		if err := rows.Scan(&treeID, &slot, &skillID); err != nil {
			return nil, err
		}
		if layouts == nil {
			layouts = make(map[int]repository.SkillLayout)
		}
		layout := layouts[treeID]
		if layout == nil {
			layout = make(repository.SkillLayout)
		}
		layout[slot] = skillID
		layouts[treeID] = layout
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return layouts, nil
}

func loadSkillCooldowns(ctx context.Context, router mysqlRouter, table, characterID string) (map[int64]time.Time, error) {
	rows, err := router.db.QueryContext(
		ctx,
		router.selectQuery("SELECT skill_id, expires_at FROM "+table+" WHERE character_id = ? ORDER BY skill_id"),
		characterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cooldowns map[int64]time.Time
	for rows.Next() {
		var skillID int64
		var expiresAt time.Time
		if err := rows.Scan(&skillID, &expiresAt); err != nil {
			return nil, err
		}
		if cooldowns == nil {
			cooldowns = make(map[int64]time.Time)
		}
		cooldowns[skillID] = expiresAt.UTC()
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cooldowns, nil
}

func replaceSkillStates(ctx context.Context, database SQLDB, table, characterID string, values map[int64]repository.SkillState) error {
	if _, err := database.ExecContext(ctx, "DELETE FROM "+table+" WHERE character_id = ?", characterID); err != nil {
		return err
	}
	query := "INSERT INTO " + table + " (character_id, skill_id, skill_level, enabled) VALUES (?, ?, ?, ?)"
	ids := sortedInt64Keys(values)
	for _, skillID := range ids {
		state := values[skillID]
		if _, err := database.ExecContext(ctx, query, characterID, skillID, state.Level, state.Enabled); err != nil {
			return err
		}
	}
	return nil
}

func replaceSkillLayouts(ctx context.Context, database SQLDB, table, characterID string, layouts map[int]repository.SkillLayout) error {
	if _, err := database.ExecContext(ctx, "DELETE FROM "+table+" WHERE character_id = ?", characterID); err != nil {
		return err
	}
	query := "INSERT INTO " + table + " (character_id, tree_id, slot_index, skill_id) VALUES (?, ?, ?, ?)"
	trees := make([]int, 0, len(layouts))
	for treeID := range layouts {
		trees = append(trees, treeID)
	}
	sort.Ints(trees)
	for _, treeID := range trees {
		layout := layouts[treeID]
		slots := make([]int, 0, len(layout))
		for slot := range layout {
			slots = append(slots, slot)
		}
		sort.Ints(slots)
		for _, slot := range slots {
			if _, err := database.ExecContext(ctx, query, characterID, treeID, slot, layout[slot]); err != nil {
				return err
			}
		}
	}
	return nil
}

func replaceSkillCooldowns(ctx context.Context, database SQLDB, table, characterID string, values map[int64]time.Time) error {
	if _, err := database.ExecContext(ctx, "DELETE FROM "+table+" WHERE character_id = ?", characterID); err != nil {
		return err
	}
	query := "INSERT INTO " + table + " (character_id, skill_id, expires_at) VALUES (?, ?, ?)"
	for _, skillID := range sortedInt64Keys(values) {
		if _, err := database.ExecContext(ctx, query, characterID, skillID, values[skillID].UTC()); err != nil {
			return err
		}
	}
	return nil
}

func sortedInt64Keys[V any](values map[int64]V) []int64 {
	keys := make([]int64, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left] < keys[right] })
	return keys
}
