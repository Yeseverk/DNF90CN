package memory

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"sync"
	"testing"
)

func TestMemoryCharacterAssetUnitOfWorkCommitsWalletAndInventory(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterAssets(t, ctx, repos)

	err := repos.CharacterAssets.WithinCharacterAssets(ctx, "77", func(characters repository.CharacterRepository, inventory repository.InventoryRepository, _ repository.EquipmentRepository) error {
		character, ok, err := characters.Load(ctx, "77")
		if err != nil || !ok {
			return errors.New("character missing in transaction")
		}
		bag, ok, err := inventory.Load(ctx, "77")
		if err != nil || !ok {
			return errors.New("inventory missing in transaction")
		}
		character.Stats["gold"] += 125
		bag.Slots["0:1"] = repository.ItemStack{ItemID: 9001, Count: 2}
		if err := repository.SaveCharacterFields(ctx, characters, character, repository.CharacterFieldStats); err != nil {
			return err
		}
		return repository.SaveInventoryFields(ctx, inventory, bag, repository.InventoryFieldSlots)
	})
	if err != nil {
		t.Fatalf("WithinCharacterAssets error = %v", err)
	}

	character, _, _ := repos.Character.Load(ctx, "77")
	bag, _, _ := repos.Inventory.Load(ctx, "77")
	if got := character.Stats["gold"]; got != 225 {
		t.Fatalf("gold = %d, want 225", got)
	}
	if got := bag.Slots["0:1"]; got.ItemID != 9001 || got.Count != 2 {
		t.Fatalf("reward stack = %+v", got)
	}
}

func TestMemoryCharacterAssetUnitOfWorkRollsBackCallback(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterAssets(t, ctx, repos)
	wantErr := errors.New("reject grant")

	err := repos.CharacterAssets.WithinCharacterAssets(ctx, "77", func(characters repository.CharacterRepository, inventory repository.InventoryRepository, _ repository.EquipmentRepository) error {
		character, _, _ := characters.Load(ctx, "77")
		bag, _, _ := inventory.Load(ctx, "77")
		character.Stats["gold"] = 999
		bag.Slots["0:1"] = repository.ItemStack{ItemID: 9001, Count: 1}
		if err := characters.Save(ctx, character); err != nil {
			return err
		}
		if err := inventory.Save(ctx, bag); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithinCharacterAssets error = %v, want %v", err, wantErr)
	}
	assertOriginalCharacterAssets(t, ctx, repos)
}

func TestMemoryCharacterAssetUnitOfWorkRestoresEarlierCommits(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterAssets(t, ctx, repos)
	commitErr := errors.New("equipment commit failed")
	failingEquipment := &mutatingEquipmentStore{EquipmentRepository: repos.Equipment, saveErr: commitErr}
	repos.CharacterAssets = &memoryCharacterAssetUnitOfWork{
		character: repos.Character,
		inventory: repos.Inventory,
		equipment: failingEquipment,
	}

	err := repos.CharacterAssets.WithinCharacterAssets(ctx, "77", func(characters repository.CharacterRepository, inventory repository.InventoryRepository, equipment repository.EquipmentRepository) error {
		character, _, _ := characters.Load(ctx, "77")
		bag, _, _ := inventory.Load(ctx, "77")
		worn, _, _ := equipment.Load(ctx, "77")
		character.Stats["gold"] = 999
		bag.Slots["0:1"] = repository.ItemStack{ItemID: 9001, Count: 1}
		if worn.Entries == nil {
			worn.Entries = make(map[string]repository.EquipmentEntry)
		}
		worn.Entries["11"] = repository.EquipmentEntry{SlotIndex: 11, ItemID: 8001}
		if err := characters.Save(ctx, character); err != nil {
			return err
		}
		if err := inventory.Save(ctx, bag); err != nil {
			return err
		}
		return equipment.Save(ctx, worn)
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("WithinCharacterAssets error = %v, want %v", err, commitErr)
	}
	assertOriginalCharacterAssets(t, ctx, repos)
}

type mutatingEquipmentStore struct {
	repository.EquipmentRepository
	saveErr error
	failed  bool
}

func (s *mutatingEquipmentStore) Save(ctx context.Context, record repository.EquipmentRecord) error {
	if !s.failed {
		s.failed = true
		if err := s.EquipmentRepository.Save(ctx, record); err != nil {
			return err
		}
		return s.saveErr
	}
	return s.EquipmentRepository.Save(ctx, record)
}

func TestMemoryCharacterAssetUnitOfWorkSerializesWalletUpdates(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterAssets(t, ctx, repos)

	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repos.CharacterAssets.WithinCharacterAssets(ctx, "77", func(characters repository.CharacterRepository, _ repository.InventoryRepository, _ repository.EquipmentRepository) error {
				character, ok, err := characters.Load(ctx, "77")
				if err != nil || !ok {
					return errors.New("character missing")
				}
				character.Stats["gold"]++
				return characters.Save(ctx, character)
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
	character, _, _ := repos.Character.Load(ctx, "77")
	if got := character.Stats["gold"]; got != 140 {
		t.Fatalf("gold = %d, want 140", got)
	}
}

func seedCharacterAssets(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	if repos.CharacterAssets == nil {
		t.Fatal("memory character asset unit of work is missing")
	}
	if err := repos.Character.Save(ctx, repository.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Stats:       map[string]int64{"gold": 100},
	}); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, repository.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]repository.ItemStack{"0:0": {ItemID: 700, Count: 1}},
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

func assertOriginalCharacterAssets(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	character, _, _ := repos.Character.Load(ctx, "77")
	bag, _, _ := repos.Inventory.Load(ctx, "77")
	worn, _, _ := repos.Equipment.Load(ctx, "77")
	if character.Stats["gold"] != 100 {
		t.Fatalf("gold mutation escaped rollback: %+v", character.Stats)
	}
	if len(bag.Slots) != 1 || bag.Slots["0:0"].ItemID != 700 {
		t.Fatalf("inventory mutation escaped rollback: %+v", bag.Slots)
	}
	if len(worn.Entries) != 0 {
		t.Fatalf("equipment mutation escaped rollback: %+v", worn.Entries)
	}
}
