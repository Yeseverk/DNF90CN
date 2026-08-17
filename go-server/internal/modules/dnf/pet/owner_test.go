package pet

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

type fakeHatchResolver struct {
	definition PetHatchDefinition
	err        error
}

func (r fakeHatchResolver) ResolveHatch(eggItemID int64) (PetHatchDefinition, error) {
	if r.err != nil {
		return PetHatchDefinition{}, r.err
	}
	definition := r.definition
	if definition.EggItemID == 0 {
		definition.EggItemID = eggItemID
	}
	return definition, nil
}

func TestOwnerHatchUsesPVFMappingAndAtomicTypedState(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "61",
		Slots: map[string]dnfrepo.ItemStack{
			"7:5": {
				ItemID:   63006,
				Count:    1,
				Bind:     true,
				RawEntry: []byte{0xAA, 0xBB},
				Extra: map[string]string{
					"creature_serial_or_handle": "123",
					"hatched_item_id":           "999999", // must be ignored
					"creature_level":            "40",     // must be ignored
				},
			},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos, fakeHatchResolver{definition: PetHatchDefinition{
		EggItemID:      63006,
		HatchedItemID:  63000,
		EggPVFPath:     "equipment/creature/egg.equ",
		HatchedPVFPath: "equipment/creature/pet.equ",
		MinimumLevel:   30,
	}})
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	got, err := owner.Hatch(ctx, HatchCommand{SelectedCharacterID: 61, ListType: listTypePet, SlotIndex: 5})
	if err != nil {
		t.Fatalf("Hatch: %v", err)
	}
	if !got.Changed || got.PetKey != "123" || got.ItemID != 63000 || got.EntryCount != 1 || len(got.Entries) != 1 {
		t.Fatalf("result = %+v", got)
	}

	inventory, ok, err := repos.Inventory.Load(ctx, "61")
	if err != nil || !ok {
		t.Fatalf("load inventory ok=%v err=%v", ok, err)
	}
	stack := inventory.Slots["7:5"]
	if stack.ItemID != 63000 || stack.Count != 1 || !stack.Bind || len(stack.RawEntry) != 0 || stack.Extra["creature_serial_or_handle"] != "123" || stack.Extra["hatched_from_item_id"] != "63006" {
		t.Fatalf("hatched stack = %+v", stack)
	}
	if stack.Extra["hatched_item_id"] != "" || stack.Extra["creature_level"] != "" {
		t.Fatalf("untrusted egg Extra leaked into output: %+v", stack.Extra)
	}

	record, ok, err := repos.Pet.Load(ctx, "61")
	if err != nil || !ok {
		t.Fatalf("load pet ok=%v err=%v", ok, err)
	}
	entry := record.Entries["123"]
	if entry.CreatureKey != 123 || entry.ItemID != 63000 || entry.SourceListType != listTypePet || entry.SourceSlotIndex != 5 || entry.Level != 1 || entry.Exp != 0 || entry.Satiety != 100 || entry.ModeFlag != 0 || entry.TailFlag != 0 || len(entry.NameRaw) != 0 || len(entry.RawEntry) != 0 {
		t.Fatalf("typed pet entry = %+v", entry)
	}
}

func TestOwnerHatchAllocatesFreshSerialOnCollision(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "62",
		Slots: map[string]dnfrepo.ItemStack{
			"7:1": {ItemID: 63000, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "1"}},
			"7:4": {ItemID: 63006, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "37"}},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "62",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {PetKey: "37", CreatureKey: 37, ItemID: 63000, Level: 4, Exp: 12, Satiety: 80},
		},
	}); err != nil {
		t.Fatalf("save pet: %v", err)
	}
	owner, _ := NewOwner(repos, fakeHatchResolver{definition: PetHatchDefinition{EggItemID: 63006, HatchedItemID: 63000}})
	got, err := owner.Hatch(ctx, HatchCommand{SelectedCharacterID: 62, ListType: listTypePet, SlotIndex: 4})
	if err != nil {
		t.Fatalf("Hatch: %v", err)
	}
	if got.PetKey != "2" {
		t.Fatalf("allocated pet key = %q, want first free serial 2", got.PetKey)
	}
	record, _, _ := repos.Pet.Load(ctx, "62")
	if existing := record.Entries["37"]; existing.Level != 4 || existing.Exp != 12 || existing.Satiety != 80 {
		t.Fatalf("existing growth changed: %+v", existing)
	}
}

func TestOwnerHatchRejectsStackAndResolverFailureWithoutMutation(t *testing.T) {
	tests := []struct {
		name     string
		count    int64
		resolver fakeHatchResolver
		wantErr  error
	}{
		{name: "stacked egg", count: 2, resolver: fakeHatchResolver{definition: PetHatchDefinition{EggItemID: 63006, HatchedItemID: 63000}}, wantErr: ErrPetEggStackInvalid},
		{name: "PVF failure", count: 1, resolver: fakeHatchResolver{err: ErrPetPVFNotCreature}, wantErr: ErrPetPVFNotCreature},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "63", Slots: map[string]dnfrepo.ItemStack{"7:3": {ItemID: 63006, Count: tc.count}}}); err != nil {
				t.Fatalf("save inventory: %v", err)
			}
			owner, _ := NewOwner(repos, tc.resolver)
			_, err := owner.Hatch(ctx, HatchCommand{SelectedCharacterID: 63, ListType: listTypePet, SlotIndex: 3})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Hatch error = %v, want %v", err, tc.wantErr)
			}
			inventory, _, _ := repos.Inventory.Load(ctx, "63")
			if stack := inventory.Slots["7:3"]; stack.ItemID != 63006 || stack.Count != tc.count {
				t.Fatalf("source mutated: %+v", stack)
			}
			if record, ok, err := repos.Pet.Load(ctx, "63"); err != nil || ok || len(record.Entries) != 0 {
				t.Fatalf("pet record changed ok=%v err=%v record=%+v", ok, err, record)
			}
		})
	}
}

func TestOwnerHatchRequiresCatalogAndCharacterPetTransaction(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner, _ := NewOwner(repos)
	if _, err := owner.Hatch(ctx, HatchCommand{SelectedCharacterID: 1, ListType: listTypePet, SlotIndex: 0}); !errors.Is(err, ErrPetCatalogUnavailable) {
		t.Fatalf("missing catalog error = %v", err)
	}
	owner, _ = NewOwner(dnfrepo.Group{Pet: repos.Pet}, fakeHatchResolver{})
	if _, err := owner.Hatch(ctx, HatchCommand{SelectedCharacterID: 1, ListType: listTypePet, SlotIndex: 0}); !errors.Is(err, ErrPetTransactionUnavailable) {
		t.Fatalf("missing transaction error = %v", err)
	}
}

func TestOwnerListReturnsTypedSortedState(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "64",
		Entries: map[string]dnfrepo.PetEntry{
			"20": {PetKey: "20", CreatureKey: 20, ItemID: 200, Name: "legacy", Satiety: 50, Level: 2},
			"3":  {PetKey: "3", CreatureKey: 3, ItemID: 100, NameRaw: []byte("typed"), Satiety: 100, Level: 1},
		},
		EquippedKey: "20",
		TownDisplay: true,
	}); err != nil {
		t.Fatalf("save pet: %v", err)
	}
	owner, _ := NewOwner(dnfrepo.Group{Pet: repos.Pet})
	got, err := owner.List(ctx, ListCommand{SelectedCharacterID: 64})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got.EntryCount != 2 || got.Entries[0].CreatureKey != 3 || got.Entries[1].CreatureKey != 20 || string(got.Entries[1].NameRaw) != "legacy" || got.EquippedKey != "20" || !got.TownDisplay {
		t.Fatalf("result = %+v", got)
	}
}

func TestOwnerListAcceptsCurrentWireFullUint32CreatureKey(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	const currentLiveKey uint32 = 1784487991
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.PetEntry{
			"1784487991": {
				PetKey:      "1784487991",
				CreatureKey: currentLiveKey,
				ItemID:      400990167,
				Satiety:     100,
				Level:       1,
			},
		},
	}); err != nil {
		t.Fatalf("save pet: %v", err)
	}
	owner, err := NewOwner(dnfrepo.Group{Pet: repos.Pet})
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	got, err := owner.List(ctx, ListCommand{SelectedCharacterID: 19})
	if err != nil {
		t.Fatalf("List current u32 key: %v", err)
	}
	if got.EntryCount != 1 || got.Entries[0].CreatureKey != currentLiveKey || got.Entries[0].ItemID != 400990167 {
		t.Fatalf("result = %+v", got)
	}
}
