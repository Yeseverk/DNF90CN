package socketemblem

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerCommitsInventoryAndEquipmentTogether(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:8": {ItemID: 6001, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"17": {SlotIndex: 17, ItemID: 9001},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	err = owner.AttachEquipmentEmblems(ctx, Command{
		CharacterID: "77",
		Project: func(assets *Assets) (Changes, error) {
			delete(assets.Inventory.Slots, "0:8")
			entry := assets.Equipment.Entries["17"]
			entry.Extra = map[string]string{"equipment_emblem_data": "02"}
			assets.Equipment.Entries["17"] = entry
			return Changes{Inventory: true, Equipment: true}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load inventory found=%v err=%v", found, err)
	}
	if _, found := inventory.Slots["0:8"]; found {
		t.Fatalf("consumed stack persisted: %+v", inventory.Slots)
	}
	equipment, found, err := repositories.Equipment.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load equipment found=%v err=%v", found, err)
	}
	if equipment.Entries["17"].Extra["equipment_emblem_data"] != "02" {
		t.Fatalf("equipment projection not persisted: %+v", equipment.Entries["17"])
	}
}

func TestOwnerRollsBackProjectorFailure(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:8": {ItemID: 6001, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("projection failed")
	err = owner.OpenAvatarSocket(ctx, Command{
		CharacterID: "77",
		Project: func(assets *Assets) (Changes, error) {
			delete(assets.Inventory.Slots, "0:8")
			return Changes{Inventory: true}, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("OpenAvatarSocket() error=%v want=%v", err, wantErr)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load inventory found=%v err=%v", found, err)
	}
	if inventory.Slots["0:8"].Count != 1 {
		t.Fatalf("failed projection leaked mutation: %+v", inventory.Slots)
	}
}

func TestOwnerExposesFourGameplayBoundaries(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	command := Command{
		CharacterID: "77",
		Project: func(*Assets) (Changes, error) {
			return Changes{}, nil
		},
	}
	for name, apply := range map[string]func(context.Context, Command) error{
		"equipment socket": owner.OpenEquipmentSocket,
		"equipment emblem": owner.AttachEquipmentEmblems,
		"avatar socket":    owner.OpenAvatarSocket,
		"avatar emblem":    owner.AttachAvatarEmblems,
	} {
		if err := apply(ctx, command); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
