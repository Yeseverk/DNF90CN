// 本文件由 transaction_test.go 按后端拆分而来。
package memory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/repository"
)

func TestMemoryCharacterCreationUnitOfWorkCommitsCompleteAggregate(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	now := time.Now().UTC()

	err := repos.CharacterCreate.WithinCharacterCreation(ctx, "77", repos, func(tx repository.Group) error {
		if err := tx.Account.Save(ctx, repository.AccountRecord{AccountID: "dnf:1", State: "active"}); err != nil {
			return err
		}
		if err := repository.CreateCharacter(ctx, tx.Character, repository.CharacterRecord{CharacterID: "77", AccountID: "dnf:1", Slot: 0, Level: 1}); err != nil {
			return err
		}
		if err := tx.Inventory.Save(ctx, repository.InventoryRecord{CharacterID: "77", Slots: map[string]repository.ItemStack{}, UpdatedAt: now}); err != nil {
			return err
		}
		if err := tx.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "77", Entries: map[string]repository.EquipmentEntry{"11": {SlotIndex: 11, ItemID: 700}}, UpdatedAt: now}); err != nil {
			return err
		}
		if err := tx.Skill.Save(ctx, repository.SkillRecord{CharacterID: "77", Skills: map[int64]repository.SkillState{46: {Level: 1, Enabled: true}}, UpdatedAt: now}); err != nil {
			return err
		}
		return tx.Settings.Save(ctx, repository.SettingsRecord{Scope: "character:77:init", Values: map[string]string{"state": "ready"}, UpdatedAt: now})
	})
	if err != nil {
		t.Fatalf("WithinCharacterCreation error = %v", err)
	}
	for name, loaded := range map[string]bool{
		"account":   recordExists(t, ctx, repos.Account, "dnf:1"),
		"character": recordExists(t, ctx, repos.Character, "77"),
		"inventory": recordExists(t, ctx, repos.Inventory, "77"),
		"equipment": recordExists(t, ctx, repos.Equipment, "77"),
		"skill":     recordExists(t, ctx, repos.Skill, "77"),
		"settings":  recordExists(t, ctx, repos.Settings, "character:77:init"),
	} {
		if !loaded {
			t.Fatalf("%s aggregate was not committed", name)
		}
	}
}

func TestMemoryCharacterCreationUnitOfWorkRollsBackEveryAggregate(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	rollback := errors.New("rollback")

	err := repos.CharacterCreate.WithinCharacterCreation(ctx, "77", repos, func(tx repository.Group) error {
		if err := tx.Account.Save(ctx, repository.AccountRecord{AccountID: "dnf:1"}); err != nil {
			return err
		}
		if err := repository.CreateCharacter(ctx, tx.Character, repository.CharacterRecord{CharacterID: "77", AccountID: "dnf:1", Slot: 0, Level: 1}); err != nil {
			return err
		}
		if err := tx.Inventory.Save(ctx, repository.InventoryRecord{CharacterID: "77"}); err != nil {
			return err
		}
		if err := tx.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "77"}); err != nil {
			return err
		}
		if err := tx.Skill.Save(ctx, repository.SkillRecord{CharacterID: "77"}); err != nil {
			return err
		}
		if err := tx.Settings.Save(ctx, repository.SettingsRecord{Scope: "character:77:init"}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("WithinCharacterCreation error = %v, want rollback", err)
	}
	for name, loaded := range map[string]bool{
		"account":   recordExists(t, ctx, repos.Account, "dnf:1"),
		"character": recordExists(t, ctx, repos.Character, "77"),
		"inventory": recordExists(t, ctx, repos.Inventory, "77"),
		"equipment": recordExists(t, ctx, repos.Equipment, "77"),
		"skill":     recordExists(t, ctx, repos.Skill, "77"),
		"settings":  recordExists(t, ctx, repos.Settings, "character:77:init"),
	} {
		if loaded {
			t.Fatalf("%s aggregate escaped rollback", name)
		}
	}
}

func recordExists[T any](t *testing.T, ctx context.Context, store interface {
	Load(context.Context, string) (T, bool, error)
}, key string) bool {
	t.Helper()
	_, exists, err := store.Load(ctx, key)
	if err != nil {
		t.Fatalf("load %q: %v", key, err)
	}
	return exists
}

func TestMemoryCharacterItemUnitOfWorkCommitsBothStores(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterItems(t, ctx, repos)

	err := repos.CharacterItems.WithinCharacterItems(ctx, "77", func(inventory repository.InventoryRepository, equipment repository.EquipmentRepository) error {
		bag, ok, err := inventory.Load(ctx, "77")
		if err != nil || !ok {
			return errors.New("inventory missing in transaction")
		}
		worn, ok, err := equipment.Load(ctx, "77")
		if err != nil || !ok {
			return errors.New("equipment missing in transaction")
		}
		delete(bag.Slots, "0:5")
		if worn.Entries == nil {
			worn.Entries = make(map[string]repository.EquipmentEntry)
		}
		worn.Entries["11"] = repository.EquipmentEntry{SlotIndex: 11, ItemID: 700}
		if err := inventory.Save(ctx, bag); err != nil {
			return err
		}
		return equipment.Save(ctx, worn)
	})
	if err != nil {
		t.Fatalf("WithinCharacterItems error = %v", err)
	}

	bag, _, _ := repos.Inventory.Load(ctx, "77")
	worn, _, _ := repos.Equipment.Load(ctx, "77")
	if _, ok := bag.Slots["0:5"]; ok {
		t.Fatalf("inventory mutation was not committed: %+v", bag.Slots)
	}
	if got := worn.Entries["11"].ItemID; got != 700 {
		t.Fatalf("equipment item = %d, want 700", got)
	}
}

func TestMemoryCharacterItemUnitOfWorkRollsBackBothStores(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterItems(t, ctx, repos)
	rollback := errors.New("rollback")

	err := repos.CharacterItems.WithinCharacterItems(ctx, "77", func(inventory repository.InventoryRepository, equipment repository.EquipmentRepository) error {
		bag, _, _ := inventory.Load(ctx, "77")
		worn, _, _ := equipment.Load(ctx, "77")
		delete(bag.Slots, "0:5")
		if worn.Entries == nil {
			worn.Entries = make(map[string]repository.EquipmentEntry)
		}
		worn.Entries["11"] = repository.EquipmentEntry{SlotIndex: 11, ItemID: 700}
		if err := inventory.Save(ctx, bag); err != nil {
			return err
		}
		if err := equipment.Save(ctx, worn); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("WithinCharacterItems error = %v, want rollback", err)
	}

	bag, _, _ := repos.Inventory.Load(ctx, "77")
	worn, _, _ := repos.Equipment.Load(ctx, "77")
	if got := bag.Slots["0:5"].ItemID; got != 700 {
		t.Fatalf("inventory item = %d, want original 700", got)
	}
	if len(worn.Entries) != 0 {
		t.Fatalf("equipment mutation escaped rollback: %+v", worn.Entries)
	}
}

func TestMemoryCharacterItemUnitOfWorkRestoresInventoryWhenEquipmentCommitFails(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterItems(t, ctx, repos)
	commitErr := errors.New("equipment commit failed")
	repos.CharacterItems = &memoryCharacterItemUnitOfWork{
		inventory: repos.Inventory,
		equipment: &failingEquipmentStore{EquipmentRepository: repos.Equipment, saveErr: commitErr},
	}

	err := repos.CharacterItems.WithinCharacterItems(ctx, "77", func(inventory repository.InventoryRepository, equipment repository.EquipmentRepository) error {
		bag, _, _ := inventory.Load(ctx, "77")
		worn, _, _ := equipment.Load(ctx, "77")
		delete(bag.Slots, "0:5")
		if worn.Entries == nil {
			worn.Entries = make(map[string]repository.EquipmentEntry)
		}
		worn.Entries["11"] = repository.EquipmentEntry{SlotIndex: 11, ItemID: 700}
		if err := inventory.Save(ctx, bag); err != nil {
			return err
		}
		return equipment.Save(ctx, worn)
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("WithinCharacterItems error = %v, want equipment commit failure", err)
	}
	bag, _, _ := repos.Inventory.Load(ctx, "77")
	if got := bag.Slots["0:5"].ItemID; got != 700 {
		t.Fatalf("inventory item = %d, want rollback to 700", got)
	}
	worn, _, _ := repos.Equipment.Load(ctx, "77")
	if len(worn.Entries) != 0 {
		t.Fatalf("equipment mutation escaped failed commit: %+v", worn.Entries)
	}
}

func TestMemoryCharacterItemUnitOfWorkSerializesConcurrentUpdates(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, repository.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]repository.ItemStack{"0:5": {ItemID: 700, Count: 40}},
	}); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	if err := repos.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "77", Entries: map[string]repository.EquipmentEntry{}}); err != nil {
		t.Fatalf("seed equipment: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repos.CharacterItems.WithinCharacterItems(ctx, "77", func(inventory repository.InventoryRepository, _ repository.EquipmentRepository) error {
				record, exists, err := inventory.Load(ctx, "77")
				if err != nil || !exists {
					return errors.New("inventory missing")
				}
				stack := record.Slots["0:5"]
				stack.Count--
				record.Slots["0:5"] = stack
				return inventory.Save(ctx, record)
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent update: %v", err)
		}
	}
	record, _, _ := repos.Inventory.Load(ctx, "77")
	if got := record.Slots["0:5"].Count; got != 0 {
		t.Fatalf("final count = %d, want 0", got)
	}
}

func TestMemoryCharacterSkillUnitOfWorkCommitsLevelsAndPoints(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seed := repository.SkillRecord{
		CharacterID: "77",
		Skills:      map[int64]repository.SkillState{46: {Level: 1, Enabled: true}},
		Points:      repository.SkillPointState{TotalSP: 100, RemainingSP: 80},
		Layouts:     map[int]repository.SkillLayout{0: {0: 46}},
	}
	if err := repos.Skill.Save(ctx, seed); err != nil {
		t.Fatalf("seed skill: %v", err)
	}

	err := repos.CharacterSkills.WithinCharacterSkill(ctx, "77", func(skills repository.SkillRepository) error {
		record, ok, err := skills.Load(ctx, "77")
		if err != nil || !ok {
			return errors.New("skill record missing in transaction")
		}
		state := record.Skills[46]
		state.Level = 2
		record.Skills[46] = state
		record.Points.RemainingSP = 60
		delete(record.Layouts[0], 0)
		record.Layouts[0][1] = 46
		return repository.SaveSkillFields(ctx, skills, record, repository.SkillFieldSkills, repository.SkillFieldPoints, repository.SkillFieldLayouts)
	})
	if err != nil {
		t.Fatalf("WithinCharacterSkill error = %v", err)
	}

	record, _, _ := repos.Skill.Load(ctx, "77")
	if record.Skills[46].Level != 2 || record.Points.RemainingSP != 60 || record.Layouts[0][1] != 46 {
		t.Fatalf("skill transaction was not committed atomically: %+v", record)
	}
}

func TestMemoryCharacterSkillUnitOfWorkRollsBackLevelsAndPoints(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seed := repository.SkillRecord{
		CharacterID: "77",
		Skills:      map[int64]repository.SkillState{46: {Level: 1, Enabled: true}},
		Points:      repository.SkillPointState{TotalSP: 100, RemainingSP: 80},
		Layouts:     map[int]repository.SkillLayout{0: {0: 46}},
	}
	if err := repos.Skill.Save(ctx, seed); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	rollback := errors.New("rollback")

	err := repos.CharacterSkills.WithinCharacterSkill(ctx, "77", func(skills repository.SkillRepository) error {
		record, _, _ := skills.Load(ctx, "77")
		state := record.Skills[46]
		state.Level = 2
		record.Skills[46] = state
		record.Points.RemainingSP = 60
		delete(record.Layouts[0], 0)
		record.Layouts[0][1] = 46
		if err := repository.SaveSkillFields(ctx, skills, record, repository.SkillFieldSkills, repository.SkillFieldPoints, repository.SkillFieldLayouts); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("WithinCharacterSkill error = %v, want rollback", err)
	}

	record, _, _ := repos.Skill.Load(ctx, "77")
	if record.Skills[46].Level != 1 || record.Points.RemainingSP != 80 || record.Layouts[0][0] != 46 {
		t.Fatalf("skill mutation escaped rollback: %+v", record)
	}
}

func seedCharacterItems(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	if repos.CharacterItems == nil {
		t.Fatal("memory character item unit of work is missing")
	}
	if err := repos.Inventory.Save(ctx, repository.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]repository.ItemStack{"0:5": {ItemID: 700, Count: 1}},
	}); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	if err := repos.Equipment.Save(ctx, repository.EquipmentRecord{
		CharacterID: "77",
		Entries:     map[string]repository.EquipmentEntry{},
	}); err != nil {
		t.Fatalf("seed equipment: %v", err)
	}
}

type failingEquipmentStore struct {
	repository.EquipmentRepository
	saveErr error
}

func (s *failingEquipmentStore) Save(context.Context, repository.EquipmentRecord) error {
	return s.saveErr
}
