package dnfbridge

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
	"unicode/utf8"

	dnfcharstat "longheng.io/server/internal/modules/dnf/charstat"
	dnfcombatpower "longheng.io/server/internal/modules/dnf/combatpower"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestBuildCurrentCombatPowerAffixBodyKeepsPVFCategoriesSeparate(t *testing.T) {
	body := buildCurrentCombatPowerAffixBody(currentCombatPowerProjection{
		Result: dnfcombatpower.Result{Affixes: dnfcombatpower.Affixes{
			WhiteDamage:        37,
			YellowDamage:       20,
			CriticalDamage:     20,
			YellowAdditional:   16,
			CriticalAdditional: 18,
			AllAttack:          35,
		}, EquippedItems: 12, PVFEquipmentScore: 38835, ActiveSets: []dnfcombatpower.ActiveSet{
			{ID: 12590, Pieces: 5},
			{ID: 12594, Pieces: 3},
			{ID: 12605, Pieces: 3},
		}},
		Job: 11, GrowType: 0x21, Level: 90,
		ProfessionName: "剑皇",
		Stats: dnfcharstat.Vector{
			PhysicalAttack: 12345, MagicalAttack: 6789, IndependentAttack: 2500,
		},
	})
	want := make([]byte, currentCombatPowerAffixBodyLength)
	want[0] = 1
	want[1] = 4
	binary.LittleEndian.PutUint16(want[2:4], 370)
	binary.LittleEndian.PutUint16(want[4:6], 200)
	binary.LittleEndian.PutUint16(want[6:8], 200)
	binary.LittleEndian.PutUint16(want[8:10], 160)
	binary.LittleEndian.PutUint16(want[10:12], 180)
	binary.LittleEndian.PutUint16(want[12:14], 350)
	binary.LittleEndian.PutUint16(want[14:16], 12)
	binary.LittleEndian.PutUint16(want[16:18], 3)
	want[18] = 11
	want[19] = 0x21
	want[20] = 90
	want[21] = byte(len([]byte("剑皇")))
	binary.LittleEndian.PutUint32(want[22:26], 12345)
	binary.LittleEndian.PutUint32(want[26:30], 6789)
	binary.LittleEndian.PutUint32(want[30:34], 2500)
	binary.LittleEndian.PutUint32(want[66:70], 38835)
	copy(want[34:66], []byte("剑皇"))
	if !bytes.Equal(body, want) {
		t.Fatalf("body=%x want=%x", body, want)
	}
}

func TestBoundedCombatPowerProfessionKeepsWholeUTF8Characters(t *testing.T) {
	got := boundedCombatPowerProfession("  超长职业名称超长职业名称  ")
	if len(got) > currentCombatPowerProfessionBytes-1 || !utf8.Valid(got) {
		t.Fatalf("bounded profession=%x len=%d", got, len(got))
	}
}

func TestCombatPowerEquippedItemIDsIncludesPetAndEveryArtifactSlot(t *testing.T) {
	now := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	got := combatPowerEquippedItemIDs(
		dnfrepo.EquipmentRecord{Entries: map[string]dnfrepo.EquipmentEntry{
			"0":  {SlotIndex: 0, ItemID: 100},
			"12": {SlotIndex: 12, ItemID: 112},
			"26": {SlotIndex: 26, ItemID: 126},
		}}, true,
		dnfrepo.PetRecord{
			EquippedKey: "legacy",
			Entries: map[string]dnfrepo.PetEntry{
				"legacy": {ItemID: 999},
			},
			Artifacts: map[string]dnfrepo.ItemStack{
				"red":   {ItemID: 127, Count: 1},
				"blue":  {ItemID: 128, Count: 1},
				"green": {ItemID: 129, Count: 1},
			},
		}, true, now,
	)
	want := []int64{100, 112, 126, 127, 128, 129}
	if !bytes.Equal(int64sAsBytes(got), int64sAsBytes(want)) {
		t.Fatalf("item ids=%v want=%v", got, want)
	}
}

func TestCombatPowerEquippedItemIDsUsesLegacyPetOnlyWithoutEquipmentRecord(t *testing.T) {
	got := combatPowerEquippedItemIDs(
		dnfrepo.EquipmentRecord{}, false,
		dnfrepo.PetRecord{
			EquippedKey: "legacy",
			Entries: map[string]dnfrepo.PetEntry{
				"legacy": {ItemID: 726},
			},
		}, true, time.Now(),
	)
	if len(got) != 1 || got[0] != 726 {
		t.Fatalf("legacy pet item ids=%v", got)
	}
}

func int64sAsBytes(values []int64) []byte {
	out := make([]byte, 0, len(values)*8)
	for _, value := range values {
		var raw [8]byte
		binary.LittleEndian.PutUint64(raw[:], uint64(value))
		out = append(out, raw[:]...)
	}
	return out
}

func TestCombatPowerActorSlotIncludesCompleteCurrentActorEquipmentMap(t *testing.T) {
	for _, slot := range []int16{0, 1, 9, 10, 11, 12, 25, 26, 27, 28, 29, 30, 31, 32} {
		if !combatPowerActorSlot(slot) {
			t.Fatalf("combat slot %d rejected", slot)
		}
	}
	for _, slot := range []int16{-2, -1, 33, 34, 255} {
		if combatPowerActorSlot(slot) {
			t.Fatalf("non-combat slot %d accepted", slot)
		}
	}
}
