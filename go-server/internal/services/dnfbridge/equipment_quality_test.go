package dnfbridge

import (
	"encoding/binary"
	"strconv"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestNewCurrentEquipmentQualitySeedUsesOrdinaryRandomRange(t *testing.T) {
	seen := make(map[uint32]struct{})
	for index := 0; index < 32; index++ {
		seed, err := newCurrentEquipmentQualitySeed()
		if err != nil {
			t.Fatal(err)
		}
		if seed == 0 || seed >= currentEquipmentTopQualitySeed {
			t.Fatalf("ordinary quality seed=%d outside 1..%d", seed, currentEquipmentTopQualitySeed-1)
		}
		seen[seed] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("32 equipment acquisitions produced one seed: %+v", seen)
	}
}

func TestCurrentEquipmentProjectionAcceptsLegacyExplicitQualitySeedWithoutItemKind(t *testing.T) {
	const seed = uint32(572024372)
	stack := dnfrepo.ItemStack{
		ItemID: 1001,
		Count:  1,
		Extra:  map[string]string{"quality_seed": strconv.FormatUint(uint64(seed), 10)},
	}

	projected := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, 42, stack)
	if got := binary.LittleEndian.Uint32(projected.data[6:10]); got != seed {
		t.Fatalf("legacy projected quality seed=%d, want=%d", got, seed)
	}
}

func TestAddCurrentDungeonPickupCreatesAndPersistsEquipmentQuality(t *testing.T) {
	definition := dungeonDropItemDefinition{
		ItemID:        100390003,
		Kind:          dungeonDropItemEquipment,
		PVFPath:       "equipment/character/common/earring/100390003.equ",
		EquipmentType: "[earring]",
		SlotStart:     9,
		SlotEnd:       64,
		Durability:    45,
	}
	record := dnfrepo.InventoryRecord{Slots: map[string]dnfrepo.ItemStack{}}
	slot, err := addCurrentDungeonPickupToInventory(&record, definition, 1)
	if err != nil {
		t.Fatal(err)
	}
	stack := record.Slots[currentDungeonPickupMainSlotKey(int16(slot))]
	seed := binary.LittleEndian.Uint32(stack.RawEntry[6:10])
	if seed == 0 || seed >= currentEquipmentTopQualitySeed {
		t.Fatalf("persisted quality seed=%d row=%x", seed, stack.RawEntry)
	}
	want := strconv.FormatUint(uint64(seed), 10)
	if stack.Count != 1 ||
		stack.Extra["quality_seed"] != want ||
		stack.Extra["equipment_type"] != "[earring]" {
		t.Fatalf("persisted equipment stack=%+v", stack)
	}

	// List 0 (bag) and list 3 (worn): raw[6:10] carries the quality seed so
	// the client reads equipment grade from op13/op14 item list entries.
	reloaded := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, int16(slot), stack)
	if got := binary.LittleEndian.Uint32(reloaded.data[6:10]); got != seed {
		t.Fatalf("reloaded quality seed=%d want=%d", got, seed)
	}
	if got := binary.LittleEndian.Uint32(reloaded.data[0x0E:0x12]); got != 0 {
		t.Fatalf("quality seed leaked into independent value_a=%d", got)
	}
}

func TestCurrentEquipmentProjectionPreservesCurrentRaw77QualityAndAdjacentFields(t *testing.T) {
	const (
		itemID     = uint32(100390003)
		seed       = uint32(345678901)
		reinforce  = byte(12)
		durability = uint16(45)
	)
	var raw [currentItemListEntryWireSize]byte
	binary.LittleEndian.PutUint16(raw[0:2], 10)
	binary.LittleEndian.PutUint32(raw[2:6], itemID)
	binary.LittleEndian.PutUint32(raw[6:10], seed)
	raw[0x0A] = reinforce
	binary.LittleEndian.PutUint16(raw[0x0B:0x0D], durability)
	copy(raw[0x0E:0x16], []byte{1, 2, 3, 4, 5, 6, 7, 8})

	equipped := dnfrepo.EquipmentEntry{
		SlotIndex: 25,
		ItemID:    int64(itemID),
		RawEntry:  append([]byte(nil), raw[:]...),
		Extra: map[string]string{
			"current_exe_runtime_move":   "1",
			"current_exe_equipment_type": "25",
		},
	}
	projected, ok := currentItemListEntryFromEquipment(equipped)
	if !ok {
		t.Fatal("current equipment row was not projected")
	}
	if got := binary.LittleEndian.Uint32(projected.data[6:10]); got != seed {
		t.Fatalf("equipped quality seed=%d want=%d", got, seed)
	}
	if projected.data[0x0A] != reinforce {
		t.Fatalf("equipped reinforce=%d want=%d", projected.data[0x0A], reinforce)
	}
	if got := binary.LittleEndian.Uint16(projected.data[0x0B:0x0D]); got != durability {
		t.Fatalf("equipped durability=%d want=%d", got, durability)
	}
	if got := projected.data[0x0E:0x16]; string(got) != string(raw[0x0E:0x16]) {
		t.Fatalf("equipped prefix=%x want=%x", got, raw[0x0E:0x16])
	}

	update := currentEquipmentUpdateEntryFromEquipment(equipped)
	if got := binary.LittleEndian.Uint32(update.data[6:10]); got != seed {
		t.Fatalf("equipment update quality seed=%d want=%d", got, seed)
	}
	if update.data[0x0A] != reinforce ||
		binary.LittleEndian.Uint16(update.data[0x0B:0x0D]) != durability {
		t.Fatalf("equipment update adjacent fields=%x", update.data[:0x16])
	}
	if got := update.data[0x0E:0x16]; string(got) != string(raw[0x0E:0x16]) {
		t.Fatalf("equipment update prefix=%x want=%x", got, raw[0x0E:0x16])
	}

	equipped.Extra["current_exe_create_value"] = "1"
	equipped.Extra["quality_seed"] = strconv.FormatUint(uint64(seed), 10)
	if got := currentItemListEquipmentInstance(equipped); got != seed {
		t.Fatalf("persisted quality seed=%d lost to stale create value", got)
	}
}
