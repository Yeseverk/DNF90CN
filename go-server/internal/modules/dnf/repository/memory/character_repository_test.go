// 本文件验证 DNF 角色创建仓储必须使用 insert-only 语义，避免新建角色覆盖旧槽位。
package memory

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strconv"
	"testing"
)

type saveOnlyCharacterRepo struct {
	saved bool
}

func (s *saveOnlyCharacterRepo) Load(context.Context, string) (repository.CharacterRecord, bool, error) {
	return repository.CharacterRecord{}, false, nil
}

func (s *saveOnlyCharacterRepo) Save(context.Context, repository.CharacterRecord) error {
	s.saved = true
	return nil
}

func (s *saveOnlyCharacterRepo) ListByAccount(context.Context, string, int) ([]repository.CharacterRecord, error) {
	return nil, nil
}

func (s *saveOnlyCharacterRepo) FindIDByName(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (s *saveOnlyCharacterRepo) NextNumericID(context.Context) (int, error) {
	return 1, nil
}

func TestCreateCharacterRequiresInsertOnlyRepository(t *testing.T) {
	repo := &saveOnlyCharacterRepo{}

	err := repository.CreateCharacter(context.Background(), repo, repository.CharacterRecord{
		CharacterID: "1",
		AccountID:   "dnf:1",
		Slot:        0,
		Name:        "hero",
	})
	if !errors.Is(err, repository.ErrCharacterCreateMissing) {
		t.Fatalf("CreateCharacter() error = %v, want ErrCharacterCreateMissing", err)
	}
	if repo.saved {
		t.Fatalf("CreateCharacter() fell back to Save and could overwrite an existing slot")
	}
}

func TestMemoryCharacterDefaultListAndSlotConflictCoverThirtyTwoSlots(t *testing.T) {
	ctx := context.Background()
	group := NewMemoryGroup()
	for slot := 0; slot < repository.DefaultCharacterSlotLimit; slot++ {
		id := strconv.Itoa(slot + 1)
		if err := group.Character.Save(ctx, repository.CharacterRecord{
			CharacterID: id,
			AccountID:   "dnf:1",
			Slot:        slot,
			Name:        "character-" + id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	characters, err := group.Character.ListByAccount(ctx, "dnf:1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(characters) != repository.DefaultCharacterSlotLimit || characters[len(characters)-1].Slot != 31 {
		t.Fatalf("default character list count/last slot = %d/%d, want 32/31", len(characters), characters[len(characters)-1].Slot)
	}
	if err := repository.CreateCharacter(ctx, group.Character, repository.CharacterRecord{
		CharacterID: "33",
		AccountID:   "dnf:1",
		Slot:        31,
		Name:        "duplicate-slot-31",
	}); !errors.Is(err, repository.ErrCharacterSlotOccupied) {
		t.Fatalf("slot 31 duplicate error = %v, want ErrCharacterSlotOccupied", err)
	}
}

func TestMemoryCharacterSwapCharacterSlotsOnlySwapsOccupiedSlots(t *testing.T) {
	ctx := context.Background()
	group := NewMemoryGroup()
	for _, record := range []repository.CharacterRecord{
		{CharacterID: "1", AccountID: "dnf:1", Slot: 2, Name: "left"},
		{CharacterID: "2", AccountID: "dnf:1", Slot: 7, Name: "right"},
	} {
		if err := group.Character.Save(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.SwapCharacterSlots(ctx, group.Character, "dnf:1", 2, 7); err != nil {
		t.Fatalf("swap occupied slots: %v", err)
	}
	left, _, _ := group.Character.Load(ctx, "1")
	right, _, _ := group.Character.Load(ctx, "2")
	if left.Slot != 7 || right.Slot != 2 {
		t.Fatalf("after swap left/right slots = %d/%d, want 7/2", left.Slot, right.Slot)
	}
	if err := repository.SwapCharacterSlots(ctx, group.Character, "dnf:1", 7, 31); err != nil {
		t.Fatalf("swap with empty slot: %v", err)
	}
	left, _, _ = group.Character.Load(ctx, "1")
	right, _, _ = group.Character.Load(ctx, "2")
	if left.Slot != 7 || right.Slot != 2 {
		t.Fatalf("empty-slot request moved a character: left/right slots = %d/%d", left.Slot, right.Slot)
	}
}
