package memory

import (
	"context"
	"database/sql"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/repository/mysql"
	"strings"
	"testing"

	platformdb "longheng.io/server/internal/platform/db"
)

func TestMemoryCharacterSettlementCommitsAllAggregates(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterSettlement(t, ctx, repos)

	err := repos.CharacterSettlement.WithinCharacterSettlement(ctx, "77", func(tx repository.Group) error {
		character, _, _ := tx.Character.Load(ctx, "77")
		quest, _, _ := tx.Quest.Load(ctx, "77")
		skill, _, _ := tx.Skill.Load(ctx, "77")
		inventory, _, _ := tx.Inventory.Load(ctx, "77")
		equipment, _, _ := tx.Equipment.Load(ctx, "77")
		character.Level = 3
		quest.States[3145] = repository.QuestState{Status: "completed"}
		skill.Points.TotalSP = 130
		if inventory.Slots == nil {
			inventory.Slots = make(map[string]repository.ItemStack)
		}
		inventory.Slots["0:1"] = repository.ItemStack{ItemID: 8474, Count: 1}
		if equipment.Entries == nil {
			equipment.Entries = make(map[string]repository.EquipmentEntry)
		}
		equipment.Entries["0"] = repository.EquipmentEntry{SlotIndex: 0, ItemID: 1001}
		if err := repository.SaveCharacterFields(ctx, tx.Character, character, repository.CharacterFieldBase); err != nil {
			return err
		}
		if err := repository.SaveQuestFields(ctx, tx.Quest, quest, repository.QuestFieldStates); err != nil {
			return err
		}
		if err := repository.SaveSkillFields(ctx, tx.Skill, skill, repository.SkillFieldPoints); err != nil {
			return err
		}
		if err := repository.SaveInventoryFields(ctx, tx.Inventory, inventory, repository.InventoryFieldSlots); err != nil {
			return err
		}
		return repository.SaveEquipmentFields(ctx, tx.Equipment, equipment, repository.EquipmentFieldEntries)
	})
	if err != nil {
		t.Fatalf("WithinCharacterSettlement() error = %v", err)
	}

	character, _, _ := repos.Character.Load(ctx, "77")
	quest, _, _ := repos.Quest.Load(ctx, "77")
	skill, _, _ := repos.Skill.Load(ctx, "77")
	inventory, _, _ := repos.Inventory.Load(ctx, "77")
	equipment, _, _ := repos.Equipment.Load(ctx, "77")
	if character.Level != 3 || quest.States[3145].Status != "completed" || skill.Points.TotalSP != 130 ||
		inventory.Slots["0:1"].ItemID != 8474 || equipment.Entries["0"].ItemID != 1001 {
		t.Fatalf("settlement did not commit all aggregates: character=%+v quest=%+v skill=%+v inventory=%+v equipment=%+v", character, quest, skill, inventory, equipment)
	}
}

func TestMemoryCharacterSettlementRollsBackCallbackAndRejectsCrossCharacter(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterSettlement(t, ctx, repos)
	wantErr := errors.New("reject settlement")

	err := repos.CharacterSettlement.WithinCharacterSettlement(ctx, "77", func(tx repository.Group) error {
		character, _, _ := tx.Character.Load(ctx, "77")
		character.Level = 9
		if err := tx.Character.Save(ctx, character); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("callback error = %v, want %v", err, wantErr)
	}
	assertOriginalCharacterSettlement(t, ctx, repos)

	err = repos.CharacterSettlement.WithinCharacterSettlement(ctx, "77", func(tx repository.Group) error {
		_, _, err := tx.Quest.Load(ctx, "78")
		return err
	})
	if !errors.Is(err, platformdb.ErrRecordKeyRequired) {
		t.Fatalf("cross-character error = %v, want ErrRecordKeyRequired", err)
	}
	assertOriginalCharacterSettlement(t, ctx, repos)
}

func TestMemoryCharacterSettlementRestoresLateMutatingFailure(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterSettlement(t, ctx, repos)
	wantErr := errors.New("equipment commit failed")
	failingEquipment := &mutatingSettlementEquipmentStore{EquipmentRepository: repos.Equipment, saveErr: wantErr}
	uow := &memoryCharacterSettlementUnitOfWork{
		character: repos.Character,
		quests:    repos.Quest,
		skills:    repos.Skill,
		inventory: repos.Inventory,
		equipment: failingEquipment,
	}

	err := uow.WithinCharacterSettlement(ctx, "77", func(tx repository.Group) error {
		character, _, _ := tx.Character.Load(ctx, "77")
		quest, _, _ := tx.Quest.Load(ctx, "77")
		skill, _, _ := tx.Skill.Load(ctx, "77")
		inventory, _, _ := tx.Inventory.Load(ctx, "77")
		equipment, _, _ := tx.Equipment.Load(ctx, "77")
		character.Level = 9
		quest.States[3145] = repository.QuestState{Status: "completed"}
		skill.Points.TotalSP = 999
		if inventory.Slots == nil {
			inventory.Slots = make(map[string]repository.ItemStack)
		}
		inventory.Slots["0:1"] = repository.ItemStack{ItemID: 8474, Count: 1}
		if equipment.Entries == nil {
			equipment.Entries = make(map[string]repository.EquipmentEntry)
		}
		equipment.Entries["0"] = repository.EquipmentEntry{SlotIndex: 0, ItemID: 1001}
		if err := tx.Character.Save(ctx, character); err != nil {
			return err
		}
		if err := tx.Quest.Save(ctx, quest); err != nil {
			return err
		}
		if err := tx.Skill.Save(ctx, skill); err != nil {
			return err
		}
		if err := tx.Inventory.Save(ctx, inventory); err != nil {
			return err
		}
		return tx.Equipment.Save(ctx, equipment)
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("late commit error = %v, want %v", err, wantErr)
	}
	assertOriginalCharacterSettlement(t, ctx, repos)
}

func TestMySQLCharacterSettlementCommitsFiveWritesAndScopesOwner(t *testing.T) {
	state := &progressionSQLState{}
	database := openProgressionSQLDB(t, state)
	repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("NewMySQLGroupFromDB() error = %v", err)
	}
	ctx := context.Background()
	err = repos.CharacterSettlement.WithinCharacterSettlement(ctx, "77", func(tx repository.Group) error {
		if err := tx.Character.Save(ctx, repository.CharacterRecord{CharacterID: "77", AccountID: "dnf:1", Level: 2}); err != nil {
			return err
		}
		if err := tx.Quest.Save(ctx, repository.QuestRecord{CharacterID: "77", States: map[int64]repository.QuestState{3145: {Status: "completed"}}}); err != nil {
			return err
		}
		if err := tx.Skill.Save(ctx, repository.SkillRecord{CharacterID: "77", Points: repository.SkillPointState{TotalSP: 30}}); err != nil {
			return err
		}
		if err := tx.Inventory.Save(ctx, repository.InventoryRecord{CharacterID: "77", Slots: map[string]repository.ItemStack{"0:1": {ItemID: 8474, Count: 1}}}); err != nil {
			return err
		}
		return tx.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "77", Entries: map[string]repository.EquipmentEntry{"0": {ItemID: 1001}}})
	})
	if err != nil {
		t.Fatalf("WithinCharacterSettlement() error = %v", err)
	}
	begin, commit, rollback, queries := state.snapshot()
	if begin != 1 || commit != 1 || rollback != 0 {
		t.Fatalf("mysql settlement begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
	}
	joined := strings.Join(queries, "\n")
	for _, table := range []string{"dnf_characters", "dnf_quests", "dnf_skills", "dnf_inventories", "dnf_equipments"} {
		if !strings.Contains(joined, table) {
			t.Fatalf("mysql settlement queries missing %s: %v", table, queries)
		}
	}

	err = repos.CharacterSettlement.WithinCharacterSettlement(ctx, "77", func(tx repository.Group) error {
		return tx.Inventory.Save(ctx, repository.InventoryRecord{CharacterID: "78"})
	})
	if !errors.Is(err, platformdb.ErrRecordKeyRequired) {
		t.Fatalf("cross-character error = %v, want ErrRecordKeyRequired", err)
	}
	begin, commit, rollback, _ = state.snapshot()
	if begin != 2 || commit != 1 || rollback != 1 {
		t.Fatalf("mysql scoped rollback begin=%d commit=%d rollback=%d", begin, commit, rollback)
	}
}

func openProgressionSQLDB(t *testing.T, state *progressionSQLState) *sql.DB {
	t.Helper()
	database := sql.OpenDB(progressionSQLConnector{state: state})
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func seedCharacterSettlement(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	if repos.CharacterSettlement == nil {
		t.Fatal("character settlement unit of work is missing")
	}
	mustSave := func(err error) {
		if err != nil {
			t.Fatalf("seed settlement: %v", err)
		}
	}
	mustSave(repos.Character.Save(ctx, repository.CharacterRecord{CharacterID: "77", AccountID: "dnf:1", Level: 2, Stats: map[string]int64{"exp": 240}}))
	mustSave(repos.Quest.Save(ctx, repository.QuestRecord{CharacterID: "77", States: map[int64]repository.QuestState{3145: {Status: "active"}}}))
	mustSave(repos.Skill.Save(ctx, repository.SkillRecord{CharacterID: "77", Points: repository.SkillPointState{TotalSP: 100, RemainingSP: 75}}))
	mustSave(repos.Inventory.Save(ctx, repository.InventoryRecord{CharacterID: "77", Slots: map[string]repository.ItemStack{}}))
	mustSave(repos.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "77", Entries: map[string]repository.EquipmentEntry{}}))
}

func assertOriginalCharacterSettlement(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	character, _, _ := repos.Character.Load(ctx, "77")
	quest, _, _ := repos.Quest.Load(ctx, "77")
	skill, _, _ := repos.Skill.Load(ctx, "77")
	inventory, _, _ := repos.Inventory.Load(ctx, "77")
	equipment, _, _ := repos.Equipment.Load(ctx, "77")
	if character.Level != 2 || character.Stats["exp"] != 240 || quest.States[3145].Status != "active" ||
		skill.Points.TotalSP != 100 || len(inventory.Slots) != 0 || len(equipment.Entries) != 0 {
		t.Fatalf("settlement mutation escaped rollback: character=%+v quest=%+v skill=%+v inventory=%+v equipment=%+v", character, quest, skill, inventory, equipment)
	}
}

type mutatingSettlementEquipmentStore struct {
	repository.EquipmentRepository
	saveErr error
	failed  bool
}

func (s *mutatingSettlementEquipmentStore) Save(ctx context.Context, record repository.EquipmentRecord) error {
	if !s.failed {
		s.failed = true
		if err := s.EquipmentRepository.Save(ctx, record); err != nil {
			return err
		}
		return s.saveErr
	}
	return s.EquipmentRepository.Save(ctx, record)
}
