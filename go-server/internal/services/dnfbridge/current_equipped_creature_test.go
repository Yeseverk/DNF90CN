package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	dnfpet "longheng.io/server/internal/modules/dnf/pet"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestCurrentEquippedCreatureProjectsSameRealStateAcrossOp2Modes(t *testing.T) {
	const (
		characterID    = "77"
		itemID         = uint32(0x17e69f80)
		serialOrHandle = uint32(37)
		creatureLevel  = byte(11)
	)
	repos := testRepositoryGroup()
	raw := testCurrentEquippedCreatureRaw(26, itemID, serialOrHandle)
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: characterID,
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {SlotIndex: 26, ItemID: int64(itemID), RawEntry: raw},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	if err := repos.Pet.Save(context.Background(), dnfrepo.PetRecord{
		CharacterID: characterID,
		Entries: map[string]dnfrepo.PetEntry{
			"pet-37": {
				PetKey: "pet-37",
				ItemID: int64(itemID),
				Name:   "petty",
				Level:  int64(creatureLevel),
				Extra:  map[string]string{"creature_serial_or_handle": "37"},
			},
		},
		EquippedKey: "pet-37",
		TownDisplay: true,
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save pet: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
		characterStats:     testCharacterStatTable(t),
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: characterID,
		Name:        "hero",
		Job:         "11",
		Level:       90,
		Stats:       defaultCreatedCharacterStats(0),
	}

	snapshot := service.currentEquippedCreatureForCharacter(context.Background(), characterID)
	if !snapshot.valid() || snapshot.itemID != itemID || snapshot.serialOrHandle != serialOrHandle ||
		snapshot.name != "petty" || snapshot.level != creatureLevel || snapshot.aliveState != 1 ||
		!snapshot.townDisplay || snapshot.source != "equipment_slot26_raw" {
		t.Fatalf("equipped creature snapshot = %+v", snapshot)
	}

	mode0 := buildCurrentSceneObjectListBodyWithCreature(77, character, true, character.Name, snapshot)
	mode0ItemID, mode0Name, mode0Alive := currentMode0CreatureForTest(t, mode0)
	if mode0ItemID != itemID || !bytes.Equal(mode0Name, rosterNameBytes("petty")) || mode0Alive != 1 {
		t.Fatalf("mode0 creature item=%#x name=%x alive=%d body=%x", mode0ItemID, mode0Name, mode0Alive, mode0)
	}

	mode1 := service.buildCurrentSelectedUserInfoMode1Body(context.Background(), nil, nil, character, true, 77)
	if got := mode1[currentMode1CreateCountOffset]; got != 0 {
		t.Fatalf("slot26 must not enter normal equipment create list: count=%d body=%x", got, mode1)
	}
	mode1RawOffset := currentMode1CreateRowsOffset + 4 + 2 + 1
	assertCurrentCreatureRawForTest(t, "mode1", mode1, mode1RawOffset, itemID, serialOrHandle, creatureLevel)

	mode3 := service.buildCurrentSelectedUserInfoMode3Body(context.Background(), nil, nil, character, true, 77)
	mode3TailOffset := 0x17 + int(binary.LittleEndian.Uint32(mode3[0x13:0x17]))
	if !bytes.Equal(
		mode1[currentMode1StatDataOffset:currentMode1ObjectTailOffset],
		mode3[0x17:mode3TailOffset],
	) {
		t.Fatalf("mode1/mode3 actor state differs: mode1=%x mode3=%x", mode1[currentMode1StatDataOffset:currentMode1ObjectTailOffset], mode3[0x17:mode3TailOffset])
	}
	if got := mode3[mode3TailOffset+1]; got != 0 {
		t.Fatalf("slot26 must not enter mode3 normal equipment create list: count=%d body=%x", got, mode3)
	}
	mode3RawOffset := mode3TailOffset + 2 + 4
	assertCurrentCreatureRawForTest(t, "mode3", mode3, mode3RawOffset, itemID, serialOrHandle, creatureLevel)
}

func TestCurrentMode1EquippedCreatureRestoresInstanceAndEnchant(t *testing.T) {
	const (
		characterID = "77"
		itemID      = uint32(400990168)
		serial      = uint32(41)
		cardID      = uint32(10008705)
		upgrade     = byte(3)
	)
	repos := testRepositoryGroup()
	raw := testCurrentEquippedCreatureRaw(26, itemID, serial)
	binary.LittleEndian.PutUint32(raw[0x10:0x14], cardID)
	raw[0x14] = upgrade
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: characterID,
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {
				SlotIndex: 26,
				ItemID:    int64(itemID),
				RawEntry:  raw,
				Extra: map[string]string{
					"equipment_slot":            "26",
					"equipped_slot":             "26",
					"creature_serial_or_handle": "41",
					"value_a":                   "10008705",
					"byte_12":                   "3",
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true }}
	reader := csharpLegacyUserInfoReader{
		ctx:         context.Background(),
		characterID: characterID,
		service:     service,
	}
	rows := currentMode1EquipmentCreateRows(reader.currentMode1EquipmentObjectRows())
	if len(rows) != 1 {
		t.Fatalf("mode1 creature create rows=%+v", rows)
	}
	row := rows[0]
	if row.slot != 26 || row.itemID != itemID || row.instance != serial || row.auxValue != cardID || row.auxByte != upgrade {
		t.Fatalf("mode1 creature row=%+v", row)
	}
	var writer packetWriter
	writeCurrentMode1EquipmentCreateRow(&writer, row)
	wire := writer.bytes()
	if len(wire) < 21 || wire[0] != 26 ||
		binary.LittleEndian.Uint32(wire[1:5]) != itemID ||
		binary.LittleEndian.Uint32(wire[5:9]) != serial ||
		binary.LittleEndian.Uint32(wire[16:20]) != cardID ||
		wire[20] != upgrade {
		t.Fatalf("mode1 creature wire=%x", wire)
	}
}

func TestCurrentEquippedCreatureProjectsPVFNameIntoMode0WithoutPersistingFallback(t *testing.T) {
	const characterID = "78"
	repos := testRepositoryGroup()
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: characterID,
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {
				SlotIndex: 26,
				ItemID:    63000,
				RawEntry:  testCurrentEquippedCreatureRaw(26, 63000, 37),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Pet.Save(context.Background(), dnfrepo.PetRecord{
		CharacterID: characterID,
		Entries: map[string]dnfrepo.PetEntry{
			"37": {
				PetKey:      "37",
				CreatureKey: 37,
				ItemID:      63000,
				Level:       1,
				Satiety:     100,
			},
		},
		EquippedKey: "37",
		TownDisplay: true,
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfpet.NewPVFCatalog(bridgePetCatalogFixture())
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
		petCatalog:         catalog,
	}

	snapshot := service.currentEquippedCreatureForCharacter(context.Background(), characterID)
	if !snapshot.valid() || snapshot.name != "跳舞的汪" {
		t.Fatalf("PVF-enriched snapshot = %+v", snapshot)
	}
	body := buildCurrentSceneObjectListBodyWithCreature(78, dnfrepo.CharacterRecord{
		CharacterID: characterID,
		Name:        "hero",
		Job:         "11",
		Level:       90,
	}, true, "hero", snapshot)
	itemID, name, alive := currentMode0CreatureForTest(t, body)
	wantName := []byte{0xCC, 0xF8, 0xCE, 0xE8, 0xB5, 0xC4, 0xCD, 0xF4}
	if itemID != 63000 || !bytes.Equal(name, wantName) || alive != 1 {
		t.Fatalf("mode0 fallback item=%d name=% X alive=%d", itemID, name, alive)
	}
	record, found, err := repos.Pet.Load(context.Background(), characterID)
	if err != nil || !found {
		t.Fatalf("load pet found=%t err=%v", found, err)
	}
	if entry := record.Entries["37"]; entry.Name != "" || len(entry.NameRaw) != 0 {
		t.Fatalf("PVF fallback leaked into durable record: %+v", entry)
	}
}

func TestCurrentEquippedCreatureDoesNotResurrectStalePetAfterUnequip(t *testing.T) {
	repos := testRepositoryGroup()
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "88",
		Entries:     map[string]dnfrepo.EquipmentEntry{},
	}); err != nil {
		t.Fatalf("save empty equipment: %v", err)
	}
	if err := repos.Pet.Save(context.Background(), dnfrepo.PetRecord{
		CharacterID: "88",
		Entries: map[string]dnfrepo.PetEntry{
			"pet-37": {
				PetKey: "pet-37",
				ItemID: 0x17e69f80,
				Name:   "stale",
				Level:  9,
				Extra:  map[string]string{"creature_serial_or_handle": "37"},
			},
		},
		EquippedKey: "pet-37",
		TownDisplay: true,
	}); err != nil {
		t.Fatalf("save stale pet record: %v", err)
	}
	service := Service{repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true }}

	snapshot := service.currentEquippedCreatureForCharacter(context.Background(), "88")
	if snapshot.valid() || snapshot.itemID != 0 || snapshot.serialOrHandle != 0 || snapshot.source != "equipment_slot26_absent" {
		t.Fatalf("stale equipped creature was resurrected: %+v", snapshot)
	}
}

func TestCurrentEquippedCreatureDoesNotTreatOrdinarySlot26InstanceAsPet(t *testing.T) {
	repos := testRepositoryGroup()
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "99",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {
				SlotIndex: 26,
				ItemID:    900024,
				RawEntry:  buildInitialEquipmentRawEntry(26, 900024, 20),
				Extra:     map[string]string{"source": "pvf_create_equipment_list", "instance_value": "1"},
			},
		},
	}); err != nil {
		t.Fatalf("save ordinary slot26 item: %v", err)
	}
	service := Service{repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true }}

	snapshot := service.currentEquippedCreatureForCharacter(context.Background(), "99")
	if snapshot.valid() || snapshot.itemID != 0 || snapshot.serialOrHandle != 0 || snapshot.source != "equipment_slot26_invalid_item_or_serial" {
		t.Fatalf("ordinary slot26 item was projected as creature: %+v", snapshot)
	}
}

func testCurrentEquippedCreatureRaw(slot byte, itemID uint32, serialOrHandle uint32) []byte {
	raw := make([]byte, 50)
	raw[0] = slot
	binary.LittleEndian.PutUint32(raw[1:5], itemID)
	binary.LittleEndian.PutUint32(raw[5:9], serialOrHandle)
	binary.LittleEndian.PutUint32(raw[24:28], serialOrHandle)
	return raw
}

func currentMode0CreatureForTest(t *testing.T, body []byte) (uint32, []byte, byte) {
	t.Helper()
	_, tailStart, ok := currentSceneObjectLevelForLog(body)
	if !ok {
		t.Fatalf("mode0 tail not found: %x", body)
	}
	equipEnd, ok := currentSceneObjectEquipSummaryEnd(body[tailStart:], 6)
	if !ok {
		t.Fatalf("mode0 equipment boundary not found: %x", body)
	}
	offset := tailStart + equipEnd + 22
	if offset+8 > len(body) {
		t.Fatalf("mode0 creature header truncated at %d/%d", offset, len(body))
	}
	itemID := binary.LittleEndian.Uint32(body[offset : offset+4])
	nameLen := int(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
	nameStart := offset + 8
	nameEnd := nameStart + nameLen
	if nameLen < 0 || nameEnd >= len(body) {
		t.Fatalf("mode0 creature name truncated len=%d at %d/%d", nameLen, offset, len(body))
	}
	return itemID, append([]byte(nil), body[nameStart:nameEnd]...), body[nameEnd]
}

func assertCurrentCreatureRawForTest(t *testing.T, mode string, body []byte, rawOffset int, itemID uint32, serialOrHandle uint32, level byte) {
	t.Helper()
	if rawOffset < 0 || rawOffset+12 > len(body) {
		t.Fatalf("%s creature raw truncated at %d/%d body=%x", mode, rawOffset, len(body), body)
	}
	if got := binary.LittleEndian.Uint32(body[rawOffset : rawOffset+4]); got != itemID {
		t.Fatalf("%s creature item id=%#x want=%#x body=%x", mode, got, itemID, body)
	}
	if got := binary.LittleEndian.Uint32(body[rawOffset+4 : rawOffset+8]); got != serialOrHandle {
		t.Fatalf("%s creature serial/handle=%d want=%d body=%x", mode, got, serialOrHandle, body)
	}
	if got := body[rawOffset+8+3]; got != level {
		t.Fatalf("%s creature level=%d want=%d body=%x", mode, got, level, body)
	}
}
