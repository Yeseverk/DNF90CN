package memory

import (
	"context"
	"database/sql"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/repository/mysql"
	"strings"
	"sync"
	"testing"
)

func TestMemoryCharacterPetUnitOfWorkCommitsAllThreeAggregates(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterPetTransaction(t, ctx, repos)

	err := repos.CharacterPets.WithinCharacterPets(ctx, "77", func(inventory repository.InventoryRepository, equipment repository.EquipmentRepository, pets repository.PetRepository) error {
		bag, _, err := inventory.Load(ctx, "77")
		if err != nil {
			return err
		}
		worn, _, err := equipment.Load(ctx, "77")
		if err != nil {
			return err
		}
		creatures, _, err := pets.Load(ctx, "77")
		if err != nil {
			return err
		}
		delete(bag.Slots, "7:3")
		if worn.Entries == nil {
			worn.Entries = make(map[string]repository.EquipmentEntry)
		}
		worn.Entries["24"] = repository.EquipmentEntry{SlotIndex: 24, ItemID: 63000}
		creatures.EquippedKey = "9001"
		entry := creatures.Entries["9001"]
		entry.Exp = 12
		creatures.Entries["9001"] = entry
		if err := inventory.Save(ctx, bag); err != nil {
			return err
		}
		if err := equipment.Save(ctx, worn); err != nil {
			return err
		}
		return pets.Save(ctx, creatures)
	})
	if err != nil {
		t.Fatalf("WithinCharacterPets() error = %v", err)
	}

	bag, _, _ := repos.Inventory.Load(ctx, "77")
	worn, _, _ := repos.Equipment.Load(ctx, "77")
	creatures, _, _ := repos.Pet.Load(ctx, "77")
	if _, exists := bag.Slots["7:3"]; exists {
		t.Fatalf("pet inventory mutation was not committed: %+v", bag.Slots)
	}
	if worn.Entries["24"].ItemID != 63000 || creatures.EquippedKey != "9001" || creatures.Entries["9001"].Exp != 12 {
		t.Fatalf("pet aggregate transaction did not commit equipment=%+v pets=%+v", worn, creatures)
	}
}

func TestMemoryCharacterPetUnitOfWorkRollsBackCallbackAndLatePetSaveFailure(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterPetTransaction(t, ctx, repos)
	wantErr := errors.New("reject pet transaction")

	err := repos.CharacterPets.WithinCharacterPets(ctx, "77", func(inventory repository.InventoryRepository, equipment repository.EquipmentRepository, pets repository.PetRepository) error {
		if err := mutateCharacterPetTransaction(ctx, inventory, equipment, pets); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("callback rollback error = %v, want %v", err, wantErr)
	}
	assertOriginalCharacterPetTransaction(t, ctx, repos)

	commitErr := errors.New("pet commit failed")
	uow := &memoryCharacterPetUnitOfWork{
		inventory: repos.Inventory,
		equipment: repos.Equipment,
		pets:      &mutatingPetStore{PetRepository: repos.Pet, saveErr: commitErr},
	}
	err = uow.WithinCharacterPets(ctx, "77", func(inventory repository.InventoryRepository, equipment repository.EquipmentRepository, pets repository.PetRepository) error {
		return mutateCharacterPetTransaction(ctx, inventory, equipment, pets)
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("late commit error = %v, want %v", err, commitErr)
	}
	assertOriginalCharacterPetTransaction(t, ctx, repos)
}

func TestMemoryCharacterPetUnitOfWorkSerializesUpdates(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterPetTransaction(t, ctx, repos)

	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repos.CharacterPets.WithinCharacterPets(ctx, "77", func(inventory repository.InventoryRepository, _ repository.EquipmentRepository, pets repository.PetRepository) error {
				bag, _, err := inventory.Load(ctx, "77")
				if err != nil {
					return err
				}
				creatures, _, err := pets.Load(ctx, "77")
				if err != nil {
					return err
				}
				stack := bag.Slots["7:3"]
				stack.Count++
				bag.Slots["7:3"] = stack
				entry := creatures.Entries["9001"]
				entry.Exp++
				creatures.Entries["9001"] = entry
				if err := inventory.Save(ctx, bag); err != nil {
					return err
				}
				return pets.Save(ctx, creatures)
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent pet update: %v", err)
		}
	}
	bag, _, _ := repos.Inventory.Load(ctx, "77")
	creatures, _, _ := repos.Pet.Load(ctx, "77")
	if got := bag.Slots["7:3"].Count; got != workers+1 {
		t.Fatalf("pet inventory count = %d, want %d", got, workers+1)
	}
	if got := creatures.Entries["9001"].Exp; got != workers {
		t.Fatalf("pet exp = %d, want %d", got, workers)
	}
}

func TestMySQLCharacterPetUnitOfWorkCommitsThreeWrites(t *testing.T) {
	state := &progressionSQLState{}
	database := sql.OpenDB(progressionSQLConnector{state: state})
	t.Cleanup(func() { _ = database.Close() })
	repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("NewMySQLGroupFromDB() error = %v", err)
	}

	err = repos.CharacterPets.WithinCharacterPets(context.Background(), "77", func(inventory repository.InventoryRepository, equipment repository.EquipmentRepository, pets repository.PetRepository) error {
		if err := inventory.Save(context.Background(), repository.InventoryRecord{CharacterID: "77", Slots: map[string]repository.ItemStack{}}); err != nil {
			return err
		}
		if err := equipment.Save(context.Background(), repository.EquipmentRecord{CharacterID: "77", Entries: map[string]repository.EquipmentEntry{}}); err != nil {
			return err
		}
		return pets.Save(context.Background(), repository.PetRecord{CharacterID: "77", Entries: map[string]repository.PetEntry{}})
	})
	if err != nil {
		t.Fatalf("WithinCharacterPets() error = %v", err)
	}

	begin, commit, rollback, queries := state.snapshot()
	if begin != 1 || commit != 1 || rollback != 0 {
		t.Fatalf("mysql transaction begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
	}
	for _, table := range []string{"`dnf_s1_w1`.`dnf_inventories`", "`dnf_s1_w1`.`dnf_equipments`", "`dnf_s1_w1`.`dnf_pets`"} {
		found := false
		for _, query := range queries {
			if strings.Contains(query, table) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("mysql pet transaction queries %v do not include %s", queries, table)
		}
	}
}

func TestMySQLCharacterPetUnitOfWorkRollsBackCallbackFailure(t *testing.T) {
	state := &progressionSQLState{}
	database := sql.OpenDB(progressionSQLConnector{state: state})
	t.Cleanup(func() { _ = database.Close() })
	repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("NewMySQLGroupFromDB() error = %v", err)
	}

	err = repos.CharacterPets.WithinCharacterPets(context.Background(), "77", func(inventory repository.InventoryRepository, _ repository.EquipmentRepository, pets repository.PetRepository) error {
		if err := inventory.Save(context.Background(), repository.InventoryRecord{CharacterID: "77", Slots: map[string]repository.ItemStack{}}); err != nil {
			return err
		}
		return pets.Save(context.Background(), repository.PetRecord{CharacterID: "78"})
	})
	if err == nil {
		t.Fatal("WithinCharacterPets() accepted a cross-character pet write")
	}
	begin, commit, rollback, queries := state.snapshot()
	if begin != 1 || commit != 0 || rollback != 1 {
		t.Fatalf("mysql rollback begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
	}
}

func seedCharacterPetTransaction(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	if repos.CharacterPets == nil {
		t.Fatal("character pet unit of work is missing")
	}
	if err := repos.Inventory.Save(ctx, repository.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]repository.ItemStack{"7:3": {ItemID: 63000, Count: 1}},
	}); err != nil {
		t.Fatalf("seed pet inventory: %v", err)
	}
	if err := repos.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "77", Entries: map[string]repository.EquipmentEntry{}}); err != nil {
		t.Fatalf("seed pet equipment: %v", err)
	}
	if err := repos.Pet.Save(ctx, repository.PetRecord{
		CharacterID: "77",
		Entries: map[string]repository.PetEntry{
			"9001": {PetKey: "9001", ItemID: 63000, Level: 1},
		},
	}); err != nil {
		t.Fatalf("seed pet record: %v", err)
	}
}

func mutateCharacterPetTransaction(ctx context.Context, inventory repository.InventoryRepository, equipment repository.EquipmentRepository, pets repository.PetRepository) error {
	bag, _, err := inventory.Load(ctx, "77")
	if err != nil {
		return err
	}
	worn, _, err := equipment.Load(ctx, "77")
	if err != nil {
		return err
	}
	creatures, _, err := pets.Load(ctx, "77")
	if err != nil {
		return err
	}
	delete(bag.Slots, "7:3")
	if worn.Entries == nil {
		worn.Entries = make(map[string]repository.EquipmentEntry)
	}
	worn.Entries["24"] = repository.EquipmentEntry{SlotIndex: 24, ItemID: 63000}
	creatures.EquippedKey = "9001"
	entry := creatures.Entries["9001"]
	entry.Exp = 99
	creatures.Entries["9001"] = entry
	if err := inventory.Save(ctx, bag); err != nil {
		return err
	}
	if err := equipment.Save(ctx, worn); err != nil {
		return err
	}
	return pets.Save(ctx, creatures)
}

func assertOriginalCharacterPetTransaction(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	bag, _, _ := repos.Inventory.Load(ctx, "77")
	worn, _, _ := repos.Equipment.Load(ctx, "77")
	creatures, _, _ := repos.Pet.Load(ctx, "77")
	if bag.Slots["7:3"].Count != 1 || len(worn.Entries) != 0 || creatures.EquippedKey != "" || creatures.Entries["9001"].Exp != 0 {
		t.Fatalf("pet transaction mutation escaped rollback inventory=%+v equipment=%+v pets=%+v", bag, worn, creatures)
	}
}

type mutatingPetStore struct {
	repository.PetRepository
	saveErr error
	failed  bool
}

func (s *mutatingPetStore) Save(ctx context.Context, record repository.PetRecord) error {
	if !s.failed {
		s.failed = true
		if err := s.PetRepository.Save(ctx, record); err != nil {
			return err
		}
		return s.saveErr
	}
	return s.PetRepository.Save(ctx, record)
}
