package mysql

import (
	"context"
	"database/sql"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"testing"
	"time"
)

type fakeSQLCall struct {
	Query string
	Args  []any
}

type fakeSQLDB struct {
	execs   []fakeSQLCall
	rows    []fakeSQLRow
	rowsets []fakeSQLRows
	pings   int
	execErr error
	pingErr error
}

func (f *fakeSQLDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, fakeSQLCall{
		Query: query,
		Args:  append([]any(nil), args...),
	})
	if f.execErr != nil {
		return nil, f.execErr
	}
	return nil, nil
}

func (f *fakeSQLDB) QueryContext(_ context.Context, query string, args ...any) (SQLRows, error) {
	f.execs = append(f.execs, fakeSQLCall{
		Query: query,
		Args:  append([]any(nil), args...),
	})
	if len(f.rowsets) == 0 {
		return &fakeSQLRows{}, nil
	}
	rows := f.rowsets[0]
	f.rowsets = f.rowsets[1:]
	return &rows, nil
}

func (f *fakeSQLDB) QueryRowContext(_ context.Context, query string, args ...any) SQLRow {
	f.execs = append(f.execs, fakeSQLCall{
		Query: query,
		Args:  append([]any(nil), args...),
	})
	if len(f.rows) == 0 {
		return fakeSQLRow{err: sql.ErrNoRows}
	}
	row := f.rows[0]
	f.rows = f.rows[1:]
	return row
}

func (f *fakeSQLDB) PingContext(context.Context) error {
	f.pings++
	if f.pingErr != nil {
		return f.pingErr
	}
	return nil
}

type fakeSQLRow struct {
	values []any
	err    error
}

type fakeSQLRows struct {
	rows []fakeSQLRow
	idx  int
	err  error
}

func (r *fakeSQLRows) Close() error {
	return nil
}

func (r *fakeSQLRows) Next() bool {
	if r == nil || r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeSQLRows) Scan(dest ...any) error {
	if r == nil || r.idx == 0 || r.idx > len(r.rows) {
		return errors.New("fake sql rows scan before next")
	}
	return r.rows[r.idx-1].Scan(dest...)
}

func (r *fakeSQLRows) Err() error {
	if r == nil {
		return nil
	}
	return r.err
}

func (r fakeSQLRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("fake sql row destination count mismatch")
	}
	for idx, value := range r.values {
		if err := assignFakeSQLValue(dest[idx], value); err != nil {
			return err
		}
	}
	return nil
}

func assignFakeSQLValue(dest any, value any) error {
	switch out := dest.(type) {
	case *string:
		if value == nil {
			*out = ""
			return nil
		}
		*out = value.(string)
	case *int:
		if value == nil {
			*out = 0
			return nil
		}
		*out = value.(int)
	case *int16:
		if value == nil {
			*out = 0
			return nil
		}
		switch v := value.(type) {
		case int16:
			*out = v
		case int:
			*out = int16(v)
		case int64:
			*out = int16(v)
		default:
			return errors.New("unsupported fake sql int16 value")
		}
	case *int64:
		if value == nil {
			*out = 0
			return nil
		}
		switch v := value.(type) {
		case int:
			*out = int64(v)
		case int64:
			*out = v
		default:
			return errors.New("unsupported fake sql int64 value")
		}
	case *bool:
		if value == nil {
			*out = false
			return nil
		}
		*out = value.(bool)
	case *byte:
		if value == nil {
			*out = 0
			return nil
		}
		switch v := value.(type) {
		case byte:
			*out = v
		case int:
			*out = byte(v)
		default:
			return errors.New("unsupported fake sql byte value")
		}
	case *uint16:
		if value == nil {
			*out = 0
			return nil
		}
		switch v := value.(type) {
		case uint16:
			*out = v
		case int:
			*out = uint16(v)
		default:
			return errors.New("unsupported fake sql uint16 value")
		}
	case *uint32:
		if value == nil {
			*out = 0
			return nil
		}
		switch v := value.(type) {
		case uint32:
			*out = v
		case int:
			*out = uint32(v)
		default:
			return errors.New("unsupported fake sql uint32 value")
		}
	case *uint64:
		if value == nil {
			*out = 0
			return nil
		}
		switch v := value.(type) {
		case uint64:
			*out = v
		case int64:
			*out = uint64(v)
		case int:
			*out = uint64(v)
		default:
			return errors.New("unsupported fake sql uint64 value")
		}
	case *[]byte:
		if value == nil {
			*out = nil
			return nil
		}
		*out = append([]byte(nil), value.([]byte)...)
	case *sql.NullString:
		if value == nil {
			*out = sql.NullString{}
			return nil
		}
		switch v := value.(type) {
		case string:
			*out = sql.NullString{String: v, Valid: true}
		case []byte:
			*out = sql.NullString{String: string(v), Valid: true}
		default:
			return errors.New("unsupported fake sql null string value")
		}
	case *sql.NullTime:
		if value == nil {
			*out = sql.NullTime{}
			return nil
		}
		*out = sql.NullTime{Time: value.(time.Time), Valid: true}
	case *sql.NullInt64:
		if value == nil {
			*out = sql.NullInt64{}
			return nil
		}
		switch v := value.(type) {
		case int:
			*out = sql.NullInt64{Int64: int64(v), Valid: true}
		case int64:
			*out = sql.NullInt64{Int64: v, Valid: true}
		default:
			return errors.New("unsupported fake sql null int64 value")
		}
	case *time.Time:
		if value == nil {
			*out = time.Time{}
			return nil
		}
		*out = value.(time.Time)
	default:
		return errors.New("unsupported fake sql destination")
	}
	return nil
}

func TestNewMySQLGroupRejectsMissingDB(t *testing.T) {
	_, err := NewMySQLGroup(nil, MySQLGroupOptions{
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if !errors.Is(err, ErrMySQLDBRequired) {
		t.Fatalf("NewMySQLGroup() error = %v, want ErrMySQLDBRequired", err)
	}
}

func TestMySQLGroupCheckPingsStores(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	if err := repos.Check(context.Background()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if sqlDB.pings != 13 {
		t.Fatalf("pings = %d, want 13", sqlDB.pings)
	}
}

func TestMySQLCharacterCreateWritesSQLColumnsOnly(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	record := repository.CharacterRecord{
		CharacterID: "char-1",
		AccountID:   "acc-1",
		Slot:        2,
		Name:        "fighter",
		Job:         "15",
		Level:       1,
	}
	if err := repository.CreateCharacter(context.Background(), repos.Character, record); err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	call := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_characters`")
	assertContains(t, call.Query, "`dnf_s1_w1`.`dnf_characters`")
	assertContains(t, call.Query, "`character_id`")
	assertContains(t, call.Query, "`grow_type`")
	assertContains(t, call.Query, "`user_state`")
	assertContains(t, call.Query, "`tutorial_completed`")
	assertContains(t, call.Query, "`tutorial_reward_progress_38`")
	assertContains(t, call.Query, "`delete_flag`")
	assertContains(t, call.Query, "`pc_room_id`")
	assertContains(t, call.Query, "`stat_block_marker`")
	assertContains(t, call.Query, "`roster_card_flag`")
	assertContains(t, call.Query, "`create_option_byte_63`")
	assertNotContains(t, call.Query, "`stats_json`")
	assertNotContains(t, call.Query, "`location_json`")
	assertNotContains(t, call.Query, "`roster_json`")
	statIndex := requireMySQLCharacterStatIndex(t, "tutorial_completed")
	if got := call.Args[8+statIndex]; got != int64(0) {
		t.Fatalf("created tutorial_completed arg = %#v, want 0", got)
	}
	rewardStatIndex := requireMySQLCharacterStatIndex(t, "tutorial_reward_progress_38")
	if got := call.Args[8+rewardStatIndex]; got != int64(0) {
		t.Fatalf("created tutorial_reward_progress_38 arg = %#v, want 0", got)
	}
}

func TestMySQLCharacterFindIDByNameIgnoresSoftDeletedRows(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)

	_, ok, err := repos.Character.FindIDByName(context.Background(), "gone")
	if err != nil {
		t.Fatalf("FindIDByName() error = %v", err)
	}
	if ok {
		t.Fatalf("FindIDByName() found a row from empty fake DB")
	}
	call := requireExecContaining(t, sqlDB, "`dnf_s1_r1`.`dnf_characters`")
	assertContains(t, call.Query, "WHERE name = ? AND delete_flag = 0")
}

func TestMySQLCharacterSaveFieldsOnlyUpdatesDirtyColumns(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	record := repository.CharacterRecord{
		CharacterID: "char-1",
		AccountID:   "acc-1",
		Name:        "fighter",
		Stats:       map[string]int64{"str": 99},
	}
	if err := repository.SaveCharacterFields(context.Background(), repos.Character, record, repository.CharacterFieldStats); err != nil {
		t.Fatalf("SaveCharacterFields() error = %v", err)
	}
	call := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_characters`")
	assertContains(t, call.Query, "`dnf_s1_w1`.`dnf_characters`")
	assertContains(t, call.Query, "`grow_type` = VALUES(`grow_type`)")
	assertContains(t, call.Query, "`roster_display_flags` = VALUES(`roster_display_flags`)")
	assertNotContains(t, call.Query, "`location_json` = VALUES(`location_json`)")
	assertNotContains(t, call.Query, "`roster_json` = VALUES(`roster_json`)")
	assertNotContains(t, call.Query, "`stats_json`")
	assertNotContains(t, call.Query, "`name` = VALUES(`name`)")
}

func TestMySQLCharacterTutorialFlagsSaveAndLoadThroughMirrorColumns(t *testing.T) {
	ctx := context.Background()
	statIndex := requireMySQLCharacterStatIndex(t, "tutorial_completed")
	rewardStatIndex := requireMySQLCharacterStatIndex(t, "tutorial_reward_progress_38")

	saveDB := &fakeSQLDB{}
	saveRepos := newTestMySQLGroup(t, saveDB)
	record := repository.CharacterRecord{
		CharacterID: "tutorial-char-1",
		AccountID:   "acc-1",
		Name:        "gunner",
		Job:         "2",
		Level:       1,
		Stats: map[string]int64{
			"tutorial_completed":          1,
			"tutorial_reward_progress_38": 1,
		},
	}
	if err := repository.SaveCharacterFields(ctx, saveRepos.Character, record, repository.CharacterFieldStats); err != nil {
		t.Fatalf("SaveCharacterFields() error = %v", err)
	}
	saveCall := requireExecContaining(t, saveDB, "`dnf_s1_w1`.`dnf_characters`")
	assertContains(t, saveCall.Query, "`tutorial_completed` = VALUES(`tutorial_completed`)")
	assertContains(t, saveCall.Query, "`tutorial_reward_progress_38` = VALUES(`tutorial_reward_progress_38`)")
	if got := saveCall.Args[8+statIndex]; got != int64(1) {
		t.Fatalf("saved tutorial_completed arg = %#v, want 1", got)
	}
	if got := saveCall.Args[8+rewardStatIndex]; got != int64(1) {
		t.Fatalf("saved tutorial_reward_progress_38 arg = %#v, want 1", got)
	}

	rowValues := []any{"tutorial-char-1", "acc-1", 0, "gunner", "2", 1}
	for idx := range mysqlCharacterStatColumns {
		value := int64(0)
		if idx == statIndex || idx == rewardStatIndex {
			value = 1
		}
		rowValues = append(rowValues, value)
	}
	rowValues = append(rowValues, nil, nil)
	loadDB := &fakeSQLDB{rows: []fakeSQLRow{{values: rowValues}}}
	loadRepos := newTestMySQLGroup(t, loadDB)
	loaded, ok, err := loadRepos.Character.Load(ctx, "tutorial-char-1")
	if err != nil || !ok {
		t.Fatalf("Load() ok=%v error=%v", ok, err)
	}
	if got := loaded.Stats["tutorial_completed"]; got != 1 {
		t.Fatalf("loaded tutorial_completed = %d, want 1", got)
	}
	if got := loaded.Stats["tutorial_reward_progress_38"]; got != 1 {
		t.Fatalf("loaded tutorial_reward_progress_38 = %d, want 1", got)
	}
	loadCall := requireExecContaining(t, loadDB, "`dnf_s1_r1`.`dnf_characters`")
	assertContains(t, loadCall.Query, "`tutorial_completed`")
	assertContains(t, loadCall.Query, "`tutorial_reward_progress_38`")
}

func TestMySQLCharacterCrystalSelectionSaveAndLoadThroughSignedMirrorColumn(t *testing.T) {
	ctx := context.Background()
	statIndex := requireMySQLCharacterStatIndex(t, "premium_crystal_selection")

	saveDB := &fakeSQLDB{}
	saveRepos := newTestMySQLGroup(t, saveDB)
	record := repository.CharacterRecord{
		CharacterID: "crystal-char-1",
		AccountID:   "acc-1",
		Name:        "slayer",
		Job:         "0",
		Level:       1,
		Stats: map[string]int64{
			"premium_crystal_selection": 4,
		},
	}
	if err := repository.SaveCharacterFields(ctx, saveRepos.Character, record, repository.CharacterFieldStats); err != nil {
		t.Fatalf("SaveCharacterFields() error = %v", err)
	}
	saveCall := requireExecContaining(t, saveDB, "`dnf_s1_w1`.`dnf_characters`")
	assertContains(t, saveCall.Query, "`premium_crystal_selection` = VALUES(`premium_crystal_selection`)")
	if got := saveCall.Args[8+statIndex]; got != int64(4) {
		t.Fatalf("saved premium_crystal_selection arg = %#v, want 4", got)
	}

	rowValues := []any{"crystal-char-1", "acc-1", 0, "slayer", "0", 1}
	for idx, column := range mysqlCharacterStatColumns {
		value := column.fallback
		if idx == statIndex {
			value = 4
		}
		rowValues = append(rowValues, value)
	}
	rowValues = append(rowValues, nil, nil)
	loadDB := &fakeSQLDB{rows: []fakeSQLRow{{values: rowValues}}}
	loadRepos := newTestMySQLGroup(t, loadDB)
	loaded, ok, err := loadRepos.Character.Load(ctx, "crystal-char-1")
	if err != nil || !ok {
		t.Fatalf("Load() ok=%v error=%v", ok, err)
	}
	if got := loaded.Stats["premium_crystal_selection"]; got != 4 {
		t.Fatalf("loaded premium_crystal_selection = %d, want 4", got)
	}
}

func TestMySQLCharacterSaveFieldsUsesRelationalRosterTables(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	record := repository.CharacterRecord{
		CharacterID: "char-1",
		AccountID:   "acc-1",
		Name:        "fighter",
		Roster: repository.CharacterRoster{
			Entry: repository.CharacterRosterEntry{
				PackedJobGrow: 0x24,
				ByteC:         56,
				ObjectID:      17,
				Flags:         []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 4},
			},
		},
	}
	if err := repository.SaveCharacterFields(context.Background(), repos.Character, record, repository.CharacterFieldRoster); err != nil {
		t.Fatalf("SaveCharacterFields() error = %v", err)
	}
	call := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_characters`")
	assertNotContains(t, call.Query, "`roster_json`")
	assertNotContains(t, call.Query, "`stats_json` = VALUES(`stats_json`)")
	assertNotContains(t, call.Query, "`location_json` = VALUES(`location_json`)")
	assertNotContains(t, call.Query, "`name` = VALUES(`name`)")
	requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_character_rosters`")
	requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_character_roster_lists`")
}

func TestMergeCharacterMirrorStatsUsesOnlyMySQLColumns(t *testing.T) {
	record := repository.CharacterRecord{
		Stats: map[string]int64{
			"grow_type":   7,
			"user_state":  9,
			"pvp_grade":   8,
			"custom_only": 123,
		},
	}
	values := make([]sql.NullInt64, len(mysqlCharacterStatColumns))
	for idx, column := range mysqlCharacterStatColumns {
		switch column.key {
		case "grow_type":
			values[idx] = sql.NullInt64{Int64: 0, Valid: true}
		case "user_state":
			values[idx] = sql.NullInt64{Int64: 0, Valid: true}
		case "pvp_grade":
			values[idx] = sql.NullInt64{Int64: 0, Valid: true}
		}
	}

	mergeCharacterMirrorStats(&record, values)

	if record.Stats["grow_type"] != 0 || record.Stats["user_state"] != 0 || record.Stats["pvp_grade"] != 0 {
		t.Fatalf("MySQL mirror columns did not override stale stats_json: %+v", record.Stats)
	}
	if _, ok := record.Stats["custom_only"]; ok {
		t.Fatalf("custom stats_json field leaked into MySQL-only stats: %+v", record.Stats)
	}
}

func TestMySQLInventorySaveFieldsOnlyWritesWarehouse(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	record := repository.InventoryRecord{
		CharacterID: "char-1",
		Warehouse:   map[string]repository.ItemStack{"safe-1": {ItemID: 1001, Count: 2}},
	}
	if err := repository.SaveInventoryFields(context.Background(), repos.Inventory, record, repository.InventoryFieldWarehouse); err != nil {
		t.Fatalf("SaveInventoryFields() error = %v", err)
	}
	requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_inventories`")
	insert := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_inventory_items`")
	assertContains(t, insert.Query, "collection_name")
	assertNotContains(t, strings.Join(execQueries(sqlDB), "\n"), "slots_json")
}

func TestMySQLEquipmentSaveFieldsWritesEntries(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	record := repository.EquipmentRecord{
		CharacterID: "char-1",
		Entries: map[string]repository.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 1001, RawEntry: []byte{1, 2, 3}},
		},
	}
	if err := repository.SaveEquipmentFields(context.Background(), repos.Equipment, record, repository.EquipmentFieldEntries); err != nil {
		t.Fatalf("SaveEquipmentFields() error = %v", err)
	}
	requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_equipments`")
	requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_equipment_entries`")
	assertNotContains(t, strings.Join(execQueries(sqlDB), "\n"), "entries_json")
}

func TestMySQLSkillSaveFieldsWritesLevelsAndPointsTogether(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	record := repository.SkillRecord{
		CharacterID: "char-1",
		Skills:      map[int64]repository.SkillState{46: {Level: 2, Enabled: true}},
		Points:      repository.SkillPointState{TotalSP: 100, RemainingSP: 60},
		Layouts:     map[int]repository.SkillLayout{0: {0: 46}},
	}
	if err := repository.SaveSkillFields(context.Background(), repos.Skill, record, repository.SkillFieldSkills, repository.SkillFieldPoints, repository.SkillFieldLayouts); err != nil {
		t.Fatalf("SaveSkillFields() error = %v", err)
	}
	root := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_skills`")
	assertContains(t, root.Query, "`total_sp` = VALUES(`total_sp`)")
	requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_skill_states`")
	requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_skill_layouts`")
	assertNotContains(t, strings.Join(execQueries(sqlDB), "\n"), "_json")
}

func TestMySQLPetSaveFieldsWritesEntriesAndDisplay(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	record := repository.PetRecord{
		CharacterID: "char-1",
		Entries: map[string]repository.PetEntry{
			"pet-1": {PetKey: "pet-1", ItemID: 1001},
		},
		EquippedKey: "pet-1",
		TownDisplay: true,
	}
	if err := repository.SavePetFields(context.Background(), repos.Pet, record, repository.PetFieldEntries, repository.PetFieldEquipped, repository.PetFieldDisplay); err != nil {
		t.Fatalf("SavePetFields() error = %v", err)
	}
	call := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_pets`")
	assertContains(t, call.Query, "`equipped_key` = VALUES(`equipped_key`)")
	assertContains(t, call.Query, "`town_display` = VALUES(`town_display`)")
	requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_pet_entries`")
	assertNotContains(t, strings.Join(execQueries(sqlDB), "\n"), "entries_json")
}

func TestMySQLMailboxSaveFieldsWritesMails(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	record := repository.MailboxRecord{
		CharacterID: "char-1",
		Mails: map[string]repository.MailRecord{
			"mail-1": {MailID: "mail-1", Title: "hello", Gold: 100},
		},
	}
	if err := repository.SaveMailboxFields(context.Background(), repos.Mailbox, record, repository.MailboxFieldMails); err != nil {
		t.Fatalf("SaveMailboxFields() error = %v", err)
	}
	requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_mailboxes`")
	requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_mails`")
	assertNotContains(t, strings.Join(execQueries(sqlDB), "\n"), "mails_json")
}

func TestMySQLMailboxLoadReadsMails(t *testing.T) {
	sqlDB := &fakeSQLDB{
		rows: []fakeSQLRow{{
			values: []any{
				"char-1",
				time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC),
			},
		}},
		rowsets: []fakeSQLRows{{rows: []fakeSQLRow{{values: []any{
			"mail-1", "", "", "", "", "hello", "", int64(100), false, false, false, nil, nil,
		}}}}},
	}
	repos := newTestMySQLGroup(t, sqlDB)
	record, ok, err := repos.Mailbox.Load(context.Background(), "char-1")
	if err != nil || !ok {
		t.Fatalf("Load() ok=%v error=%v", ok, err)
	}
	if record.Mails["mail-1"].Gold != 100 || record.Mails["mail-1"].Title != "hello" {
		t.Fatalf("mailbox = %+v", record)
	}
	call := requireExecContaining(t, sqlDB, "`dnf_s1_r1`.`dnf_mailboxes`")
	assertContains(t, call.Query, "`dnf_s1_r1`.`dnf_mailboxes`")
}

func TestMySQLAccountLoadUsesReadDatabase(t *testing.T) {
	sqlDB := &fakeSQLDB{
		rows: []fakeSQLRow{{
			values: []any{
				"acc-1",
				"open",
				uint64(123456789),
				"adventure-one",
				time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC),
				time.Date(2026, 6, 29, 2, 0, 0, 0, time.UTC),
			},
		}},
		rowsets: []fakeSQLRows{{rows: []fakeSQLRow{{values: []any{"source", "unit"}}}}},
	}
	repos := newTestMySQLGroup(t, sqlDB)
	record, ok, err := repos.Account.Load(context.Background(), "acc-1")
	if err != nil || !ok {
		t.Fatalf("Load() ok=%v err=%v", ok, err)
	}
	if record.Metadata["source"] != "unit" || record.State != "open" || record.HonorExp != 123456789 || record.RepresentAccountName != "adventure-one" {
		t.Fatalf("unexpected account: %+v", record)
	}
	call := requireExecContaining(t, sqlDB, "`dnf_s1_r1`.`dnf_accounts`")
	assertContains(t, call.Query, "`dnf_s1_r1`.`dnf_accounts`")
	assertNotContains(t, call.Query, "FOR UPDATE")
}

func TestMySQLAccountLoadLocksCurrentRowInsideTransaction(t *testing.T) {
	sqlDB := &fakeSQLDB{rows: []fakeSQLRow{{values: []any{
		"acc-1",
		"open",
		uint64(123),
		nil,
		nil,
		nil,
	}}}}
	store := &mysqlAccountStore{mysqlStoreBase: mysqlStoreBase{router: mysqlRouter{
		db:          sqlDB,
		readDBs:     []string{"dnf_s1_w1"},
		writeDBs:    []string{"dnf_s1_w1"},
		tablePrefix: "dnf",
		lockReads:   true,
	}}}
	if _, ok, err := store.Load(context.Background(), "acc-1"); err != nil || !ok {
		t.Fatalf("Load() ok=%v err=%v", ok, err)
	}
	call := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_accounts`")
	assertContains(t, call.Query, "FOR UPDATE")
}

func TestMySQLAccountSaveWritesHonorExp(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	if err := repos.Account.Save(context.Background(), repository.AccountRecord{
		AccountID:            "acc-1",
		State:                "open",
		HonorExp:             17699999999,
		RepresentAccountName: "adventure-one",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	call := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_accounts`")
	assertContains(t, call.Query, "`honor_exp`")
	assertContains(t, call.Query, "`represent_account_name`")
	if len(call.Args) < 3 || call.Args[2] != uint64(17699999999) {
		t.Fatalf("honor_exp args = %#v", call.Args)
	}
	if len(call.Args) < 4 || call.Args[3] != "adventure-one" {
		t.Fatalf("represent_account_name args = %#v", call.Args)
	}
}

func TestMySQLAccountFindIDByRepresentNameUsesReadDatabase(t *testing.T) {
	sqlDB := &fakeSQLDB{rows: []fakeSQLRow{{values: []any{"acc-1"}}}}
	repos := newTestMySQLGroup(t, sqlDB)
	finder := repos.Account.(repository.RepresentAccountNameFinder)
	accountID, found, err := finder.FindAccountIDByRepresentName(context.Background(), "adventure-one")
	if err != nil || !found || accountID != "acc-1" {
		t.Fatalf("FindAccountIDByRepresentName() account=%q found=%v err=%v", accountID, found, err)
	}
	call := requireOneExec(t, sqlDB)
	assertContains(t, call.Query, "`dnf_s1_r1`.`dnf_accounts`")
	assertContains(t, call.Query, "represent_account_name = ?")
}

func TestMySQLCharacterLoadNoRows(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	_, ok, err := repos.Character.Load(context.Background(), "missing-char")
	if err != nil || ok {
		t.Fatalf("Load() ok=%v err=%v, want no row", ok, err)
	}
}

func TestMySQLPacketSaveClonesBodyArg(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repos := newTestMySQLGroup(t, sqlDB)
	body := []byte{1, 2, 3}
	if err := repos.PacketTemplate.Save(context.Background(), repository.PacketTemplateRecord{
		TemplateID: "tpl-1",
		Name:       "login",
		Body:       body,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	body[0] = 9
	call := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_packet_templates`")
	got := call.Args[2].([]byte)
	if got[0] != 1 {
		t.Fatalf("body arg should be cloned, got %v", got)
	}
}

func newTestMySQLGroup(t *testing.T, sqlDB *fakeSQLDB) repository.Group {
	t.Helper()
	repos, err := NewMySQLGroup(sqlDB, MySQLGroupOptions{
		DatabasePlan: repository.DatabasePlan{
			WriteDatabases: []string{"dnf_s1_w1"},
			ReadDatabases:  []string{"dnf_s1_r1"},
		},
		Now: func() time.Time {
			return time.Date(2026, 6, 29, 3, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewMySQLGroup() error = %v", err)
	}
	return repos
}

func requireOneExec(t *testing.T, sqlDB *fakeSQLDB) fakeSQLCall {
	t.Helper()
	if len(sqlDB.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(sqlDB.execs))
	}
	return sqlDB.execs[0]
}

func requireExecContaining(t *testing.T, sqlDB *fakeSQLDB, needle string) fakeSQLCall {
	t.Helper()
	for _, call := range sqlDB.execs {
		if strings.Contains(call.Query, needle) {
			return call
		}
	}
	t.Fatalf("no SQL call contains %q; calls=%v", needle, execQueries(sqlDB))
	return fakeSQLCall{}
}

func execQueries(sqlDB *fakeSQLDB) []string {
	queries := make([]string, 0, len(sqlDB.execs))
	for _, call := range sqlDB.execs {
		queries = append(queries, call.Query)
	}
	return queries
}

func assertContains(t *testing.T, value, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Fatalf("value missing %q:\n%s", want, value)
	}
}

func assertNotContains(t *testing.T, value, want string) {
	t.Helper()
	if strings.Contains(value, want) {
		t.Fatalf("value unexpectedly contains %q:\n%s", want, value)
	}
}

func requireMySQLCharacterStatIndex(t *testing.T, key string) int {
	t.Helper()
	for idx, column := range mysqlCharacterStatColumns {
		if column.key == key {
			return idx
		}
	}
	t.Fatalf("mysql character stat column %q not found", key)
	return -1
}
