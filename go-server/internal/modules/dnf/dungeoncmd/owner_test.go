package dungeoncmd

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerPlansSelectDungeonContext(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "acc",
		Level:       86,
		Stats:       map[string]int64{"fatigue": 121},
		Location:    dnfrepo.CharacterLocation{TownID: 3, DungeonID: 4001, RoomID: "2:3"},
	}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots: map[string]dnfrepo.ItemStack{
			"0:9": {ItemID: 1001, Count: 2},
		},
	}); err != nil {
		t.Fatalf("Inventory.Save error = %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	got, err := owner.Plan(ctx, Command{
		Operation:           "select_dungeon",
		SelectedCharacterID: 99,
		DungeonID:           4001,
		Difficulty:          3,
	})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if got.AccountID != "acc" || got.CharacterID != "99" || got.Level != 86 || got.Fatigue != 121 {
		t.Fatalf("identity/stat = %+v", got)
	}
	if got.TownID != 3 || got.DungeonID != 4001 || got.RoomID != "2:3" {
		t.Fatalf("location = %+v", got)
	}
	if !got.InventoryKnown || got.InventorySlotCount != 1 {
		t.Fatalf("inventory = known %t slots %d", got.InventoryKnown, got.InventorySlotCount)
	}
	if got.RequestedDungeonID != 4001 || got.Difficulty != 3 {
		t.Fatalf("request = %+v", got)
	}
}

func TestOwnerRejectsGetItemWithoutInventory(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "99", AccountID: "acc"}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Plan(ctx, Command{
		Operation:           "get_item",
		SelectedCharacterID: 99,
		DropObjectKey:       7007,
	})
	if !errors.Is(err, ErrInventoryNotFound) {
		t.Fatalf("Plan error = %v, want ErrInventoryNotFound", err)
	}
}

func TestOwnerBlocksLiveRoomCommandsWithoutRuntimeAuthority(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "99", AccountID: "acc"}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	tests := []struct {
		operation string
		want      error
	}{
		{operation: "move_map", want: ErrRuntimeSessionRequired},
		{operation: "die_monster", want: ErrCombatAuthorityNeeded},
	}
	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			_, planErr := owner.Plan(ctx, Command{
				Operation:           test.operation,
				SelectedCharacterID: 99,
			})
			if !errors.Is(planErr, test.want) {
				t.Fatalf("Plan error = %v, want %v", planErr, test.want)
			}
		})
	}
}
