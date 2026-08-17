package character

import (
	"context"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerPlansSelectCharacterByAccountSlot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "acc",
		Slot:        2,
		Name:        "hero",
		Job:         "15",
		Level:       86,
	}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	got, err := owner.Plan(ctx, Command{
		Operation:         "select_character",
		AccountID:         "acc",
		SlotOrCharacterID: 2,
	})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if !got.CharacterKnown || got.CharacterID != "77" || got.Name != "hero" || got.Slot != 2 || got.Level != 86 {
		t.Fatalf("result = %+v", got)
	}
	if got.RosterCount != 1 {
		t.Fatalf("roster count = %d", got.RosterCount)
	}
}

func TestOwnerSelectCharacterDoesNotCrossAccountOnSlotOrIDCollision(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "2",
		AccountID:   "other",
		Slot:        9,
		Name:        "wrong",
	}); err != nil {
		t.Fatalf("Character.Save(other) error = %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "acc",
		Slot:        2,
		Name:        "hero",
	}); err != nil {
		t.Fatalf("Character.Save(acc) error = %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	got, err := owner.Plan(ctx, Command{
		Operation:         "select_character",
		AccountID:         "acc",
		SlotOrCharacterID: 2,
	})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if got.CharacterID != "77" || got.Name != "hero" {
		t.Fatalf("result = %+v, want account slot match", got)
	}
}

func TestOwnerPlansNameTaken(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "acc",
		Slot:        2,
		Name:        "hero",
	}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	got, err := owner.Plan(ctx, Command{
		Operation: "check_double_character_name",
		AccountID: "acc",
		Name:      "hero",
	})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if !got.NameTaken || got.NameOwnerID != "77" || got.RosterCount != 1 {
		t.Fatalf("result = %+v", got)
	}
}
