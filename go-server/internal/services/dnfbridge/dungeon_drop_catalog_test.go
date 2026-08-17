package dnfbridge

import (
	"errors"
	"reflect"
	"testing"
	"time"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

type dungeonDropCatalogTestSource map[string]string

func (s dungeonDropCatalogTestSource) ReadText(relativePath string) (string, error) {
	text, ok := s[cleanDungeonDropPath(relativePath)]
	if !ok {
		return "", errors.New("test PVF document missing: " + relativePath)
	}
	return text, nil
}

func TestPVFDungeonDropCatalogMapsMonsterItemPoolToRealItemDocuments(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":                        "65005 `NewMonsters/Goblin/Madman_item.mob`\n",
		"stackable/stackable.lst":                    "3227 `material/solvent_magicpower.stk`\n4225 `quest/charm_soul.stk`\n",
		"equipment/equipment.lst":                    "9001 `weapon/test_sword.equ`\n",
		"monster/NewMonsters/Goblin/Madman_item.mob": "[item]\n3227 3\n4225 7\n9001 2\n[/item]\n",
		"stackable/material/solvent_magicpower.stk":  "[name] `Magic Solvent`\n[stackable type] `[material]`\n[stack limit] 999\n[usable period] 30\n[expiration date] `2028-08-16 06:00:00`\n",
		"stackable/quest/charm_soul.stk":             "[name] `Charm Soul`\n",
		"equipment/weapon/test_sword.equ":            "[name] `Test Sword`\n[durability] 57\n[usable period] 2\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := catalog.MonsterPool(65005)
	if err != nil {
		t.Fatal(err)
	}
	wantPool := []dungeonMonsterDropPoolEntry{
		{ItemID: 3227, Weight: 3},
		{ItemID: 4225, Weight: 7},
		{ItemID: 9001, Weight: 2},
	}
	if !reflect.DeepEqual(pool, wantPool) {
		t.Fatalf("pool = %+v, want %+v", pool, wantPool)
	}

	stackable, err := catalog.ResolveItem(3227)
	if err != nil {
		t.Fatal(err)
	}
	if stackable.Kind != dungeonDropItemStackable || stackable.PVFPath != "stackable/material/solvent_magicpower.stk" ||
		stackable.StackableType != "[material]" || stackable.StackLimit != 999 || stackable.UsablePeriodDays != 30 ||
		stackable.SlotStart != 121 || stackable.SlotEnd != 176 || stackable.Durability != 0 ||
		!stackable.ExpirationDate.Equal(time.Date(2028, time.August, 15, 22, 0, 0, 0, time.UTC)) {
		t.Fatalf("stackable = %+v", stackable)
	}
	equipment, err := catalog.ResolveItem(9001)
	if err != nil {
		t.Fatal(err)
	}
	if equipment.Kind != dungeonDropItemEquipment || equipment.PVFPath != "equipment/weapon/test_sword.equ" ||
		equipment.SlotStart != 9 || equipment.SlotEnd != 64 || equipment.Durability != 57 || equipment.UsablePeriodDays != 2 {
		t.Fatalf("equipment = %+v", equipment)
	}
	if _, err := catalog.ResolveItem(7777); !errors.Is(err, errDungeonDropItemUnresolved) {
		t.Fatalf("unresolved item error = %v", err)
	}
}

func TestParseDungeonMonsterDropPoolRejectsIncompleteAndInvalidPairs(t *testing.T) {
	document, err := dnfpvf.Parse("monster/test.mob", "[item]\n100 5\n0 7\n200 0\n300 -1\n400\n[/item]\n")
	if err != nil {
		t.Fatal(err)
	}
	pool := parseDungeonMonsterDropPool(document)
	want := []dungeonMonsterDropPoolEntry{{ItemID: 100, Weight: 5}}
	if !reflect.DeepEqual(pool, want) {
		t.Fatalf("pool = %+v, want %+v", pool, want)
	}
}

func TestSelectDungeonMonsterDropUsesPVFWeightsWithoutInventingDropRate(t *testing.T) {
	pool := []dungeonMonsterDropPoolEntry{
		{ItemID: 100, Weight: 2},
		{ItemID: 200, Weight: 3},
	}
	tests := []struct {
		roll uint64
		want uint32
	}{
		{roll: 0, want: 100},
		{roll: 1, want: 100},
		{roll: 2, want: 200},
		{roll: 4, want: 200},
		{roll: 5, want: 100},
	}
	for _, test := range tests {
		got, ok := selectDungeonMonsterDrop(pool, test.roll)
		if !ok || got.ItemID != test.want {
			t.Fatalf("roll %d = %+v/%t, want item %d", test.roll, got, ok, test.want)
		}
	}
}

func TestDungeonDropStackableQuickSlotPreferenceUsesPVFType(t *testing.T) {
	tests := []struct {
		stackableType string
		want          bool
	}{
		{stackableType: "[waste]", want: true},
		{stackableType: "`[WASTE]`", want: true},
		{stackableType: "[material]", want: true},
		{stackableType: "[material] 4", want: true},
		{stackableType: "[quest]", want: false},
		{stackableType: "[material expert job]", want: false},
		{stackableType: "[avatar emblem]", want: false},
		{stackableType: "", want: false},
	}
	for _, test := range tests {
		if got := dungeonDropStackablePrefersItemQuickSlots(test.stackableType); got != test.want {
			t.Fatalf("stackable type %q quick-slot preference=%t want=%t", test.stackableType, got, test.want)
		}
	}
}
