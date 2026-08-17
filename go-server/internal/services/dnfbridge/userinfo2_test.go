// userinfo2_test.go 覆盖选角 USERINFO subtype1 的 C# 字段顺序和旧角色兜底属性。
package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	dnfcharstat "longheng.io/server/internal/modules/dnf/charstat"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestBuildCSharpSelectedUserInfoSubtype1UsesPVFStatsWhenStoredZero(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats: map[string]int64{
			"stat_hp_max":                 0,
			"stat_mp_max":                 0,
			"stat_physical_attack":        0,
			"stat_magical_attack":         0,
			"stat_inventory_limit":        0,
			"stat_mp_regen_speed":         0,
			"stat_move_speed":             0,
			"stat_attack_speed":           0,
			"stat_cast_speed":             0,
			"stat_hit_recovery":           0,
			"stat_jump_power":             0,
			"stat_weight":                 0,
			"active_status_resistance_16": 0,
		},
	}
	service := Service{characterStats: testCharacterStatTable(t)}

	body := service.buildCSharpSelectedUserInfoBody(context.Background(), nil, nil, 1, character, true, 19, character.Name)
	if got := binary.LittleEndian.Uint32(body[9:13]); got != 83 {
		t.Fatalf("subtype1 marker = %d, want 83", got)
	}
	if got := binary.LittleEndian.Uint32(body[13:17]); got != 11000 {
		t.Fatalf("subtype1 hp max = %d, want 11000", got)
	}
	if got := binary.LittleEndian.Uint32(body[17:21]); got != 11800 {
		t.Fatalf("subtype1 mp max = %d, want 11800", got)
	}
	if got := bodyInt16ForUserInfoTest(body[21:23]); got != 7 {
		t.Fatalf("subtype1 physical attack = %d, want 7", got)
	}
	if got := bodyInt16ForUserInfoTest(body[25:27]); got != 6 {
		t.Fatalf("subtype1 magical attack = %d, want 6", got)
	}
	if got := binary.LittleEndian.Uint32(body[71:75]); got != 480000 {
		t.Fatalf("subtype1 inventory limit = %d, want 480000", got)
	}
	if got := binary.LittleEndian.Uint16(body[77:79]); got != 500 {
		t.Fatalf("subtype1 mp regen = %d, want 500", got)
	}
	if got := binary.LittleEndian.Uint32(body[79:83]); got != 8500 {
		t.Fatalf("subtype1 move speed = %d, want 8500", got)
	}
	if got := binary.LittleEndian.Uint32(body[91:95]); got != 510000 {
		t.Fatalf("subtype1 weight = %d, want 510000", got)
	}
	if got := body[95]; got != csharpSubtype1ProtocolStatLevel {
		t.Fatalf("subtype1 protocol stat level = %d, want %d", got, csharpSubtype1ProtocolStatLevel)
	}
}

func TestBuildCSharpSelectedUserInfoSubtype0WritesEquipmentAppearanceEntries(t *testing.T) {
	repos := testRepositoryGroup()
	rawClone := buildInitialEquipmentRawEntry(9, 900099, 20)
	binary.LittleEndian.PutUint32(rawClone[12:16], 123456)
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"13": {SlotIndex: 13, ItemID: 900013, RawEntry: buildInitialEquipmentRawEntry(13, 900013, 20)},
			"11": {
				SlotIndex: 11,
				ItemID:    900001,
				RawEntry:  buildInitialEquipmentRawEntry(11, 900001, 20),
				Extra: map[string]string{
					"attr":         "2",
					"amplify_type": "1",
					"reinforce":    "7",
				},
			},
			"9": {SlotIndex: 9, ItemID: 900099, RawEntry: rawClone},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
	}

	body := service.buildCSharpSelectedUserInfoBody(context.Background(), nil, nil, 0, character, true, 77, character.Name)
	countOffset := 1 + 2 + 2 + 4 + len(rosterNameBytes(character.Name)) + 6
	if got := body[countOffset]; got != 2 {
		t.Fatalf("subtype0 appearance count = %d, want 2 body=%x", got, body)
	}
	first := body[countOffset+1 : countOffset+1+23]
	if first[0] != 9 || binary.LittleEndian.Uint32(first[1:5]) != 123456 {
		t.Fatalf("first appearance = %x, want slot 9 clone target 123456", first)
	}
	if binary.LittleEndian.Uint32(first[5:9]) != 4 || !bytes.Equal(first[9:13], make([]byte, 4)) {
		t.Fatalf("first appearance expansion = %x, want len4 zero data", first[5:13])
	}
	second := body[countOffset+1+23 : countOffset+1+46]
	if second[0] != 11 || binary.LittleEndian.Uint32(second[1:5]) != 900001 {
		t.Fatalf("second appearance = %x, want slot 11 item 900001", second)
	}
	if second[13] != 5 || second[22] != 7 {
		t.Fatalf("second appearance state/flag = %d/%d, want 5/7 entry=%x", second[13], second[22], second)
	}
}

func TestBuildCurrentSelectedUserInfoMode1UsesCurrentExeObjectTail(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats: map[string]int64{
			"exp":                  12345,
			"stat_hp_max":          0,
			"stat_mp_max":          0,
			"stat_physical_attack": 0,
			"stat_weight":          0,
		},
	}
	service := Service{characterStats: testCharacterStatTable(t)}

	body := service.buildCurrentSelectedUserInfoMode1Body(context.Background(), nil, nil, character, true, 19)
	if got := body[0]; got != 1 {
		t.Fatalf("mode = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(body[1:3]); got != 1 {
		t.Fatalf("mode1 count = %d, want 1", got)
	}
	if body[3] != currentSceneObjectRoute || body[4] != currentSceneObjectContext {
		t.Fatalf("mode1 route/context = %d/%d, want %d/%d", body[3], body[4], currentSceneObjectRoute, currentSceneObjectContext)
	}
	if got := binary.LittleEndian.Uint16(body[0x15:0x17]); got != 19 {
		t.Fatalf("mode1 object key = %#x, want %#x", got, 19)
	}
	if got := len(body); got != currentMode1BaseWireSize {
		t.Fatalf("mode1 body len = %d, want current actor branch len %d body=%x", got, currentMode1BaseWireSize, body)
	}
	if got := binary.LittleEndian.Uint32(body[currentMode1ExperienceOffset:currentMode1StatLengthOffset]); got != 12345 {
		t.Fatalf("mode1 cumulative EXP = %d, want 12345", got)
	}
	if got := binary.LittleEndian.Uint32(body[currentMode1StatLengthOffset:currentMode1StatDataOffset]); got != currentMode1StatBlobWireSize {
		t.Fatalf("mode1 sub_2002B30 raw length = %#x, want %d", got, currentMode1StatBlobWireSize)
	}
	if got := binary.LittleEndian.Uint32(body[currentMode1StatDataOffset : currentMode1StatDataOffset+4]); got != 11000 {
		t.Fatalf("mode1 stat hp max = %d, want PVF 11000", got)
	}
	if got := body[currentMode1ObjectTailOffset]; got != 0 {
		t.Fatalf("mode1 extra-equipment-slot state = %#x, want 0", got)
	}
	if got := body[currentMode1CreateCountOffset]; got != 0 {
		t.Fatalf("mode1 sub_1D77560 count = %#x, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(body[currentMode1CreateRowsOffset : currentMode1CreateRowsOffset+4]); got != 0 {
		t.Fatalf("mode1 sub_1D77560 final state = %#x, want 0", got)
	}
	if got := binary.LittleEndian.Uint16(body[currentMode1CreateRowsOffset+4 : currentMode1CreateRowsOffset+6]); got != 19 {
		t.Fatalf("mode1 equipment object key = %#x, want %#x", got, 19)
	}
	if got := body[currentMode1CreateRowsOffset+6]; got != 0 {
		t.Fatalf("mode1 sub_2007B00 row count = %d, want 0", got)
	}
	resourceOffset := currentMode1CreateRowsOffset + 4 + 2 + 1 + 8
	if got := body[resourceOffset]; got != 0xff {
		t.Fatalf("mode1 resource group = %#x, want 0xff", got)
	}
	if got := body[resourceOffset+1]; got != 0 {
		t.Fatalf("mode1 resource group0 count = %#x, want 0", got)
	}
	if got := body[resourceOffset+2]; got != 0 {
		t.Fatalf("mode1 resource group1 count = %#x, want 0", got)
	}
	if got := body[resourceOffset+3]; got != 0 {
		t.Fatalf("mode1 post-resource creature byte = %#x, want 0", got)
	}
	if got := body[resourceOffset+4]; got != 0 {
		t.Fatalf("mode1 sub_2002BE0 count = %#x, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(body[resourceOffset+9 : resourceOffset+13]); got != 0 {
		t.Fatalf("mode1 sub_2002DC0 count = %#x, want 0", got)
	}
}

func TestBuildCurrentActorBindingMode1BodyHasNoEquipmentAndCarriesState(t *testing.T) {
	const (
		objectKey      uint16 = 0x1234
		adventureLevel uint32 = 4
	)
	body := buildCurrentActorBindingMode1Body(objectKey, adventureLevel)
	if len(body) != currentMode1BaseWireSize || body[0] != 1 {
		t.Fatalf("actor-binding mode1 len=%d mode=%d body=%x", len(body), body[0], body)
	}
	if got := binary.LittleEndian.Uint16(body[0x15:0x17]); got != objectKey {
		t.Fatalf("actor-binding mode1 object key=%#x want=%#x body=%x", got, objectKey, body)
	}
	if got := binary.LittleEndian.Uint32(body[currentMode1StatLengthOffset:currentMode1StatDataOffset]); got != currentMode1StatBlobWireSize {
		t.Fatalf("actor-binding mode1 stat length=%d want=%d body=%x", got, currentMode1StatBlobWireSize, body)
	}
	if got := body[currentMode1StatDataOffset+84]; got != currentActorBaseStatScalePercent {
		t.Fatalf("actor-binding mode1 stat scale=%d want=%d body=%x", got, currentActorBaseStatScalePercent, body)
	}
	if got := binary.LittleEndian.Uint16(body[currentMode1CreateRowsOffset+4 : currentMode1CreateRowsOffset+6]); got != objectKey {
		t.Fatalf("actor-binding mode1 equipment actor key=%#x want=%#x body=%x", got, objectKey, body)
	}
	if body[currentMode1CreateCountOffset] != 0 || body[currentMode1CreateRowsOffset+6] != 0 {
		t.Fatalf("actor-binding mode1 create/update counts=%d/%d want=0/0 body=%x", body[currentMode1CreateCountOffset], body[currentMode1CreateRowsOffset+6], body)
	}
	if got := binary.LittleEndian.Uint32(body[len(body)-10 : len(body)-6]); got != adventureLevel {
		t.Fatalf("actor-binding adventure level=%d want=%d body=%x", got, adventureLevel, body)
	}
}

func TestBuildCurrentActorBindingMode1BodyForSelectedUsesRealPVFStats(t *testing.T) {
	service := Service{characterStats: testCharacterStatTable(t)}
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		Name:        "hero",
		Job:         "11",
		Level:       90,
		Stats: map[string]int64{
			"stat_hp_max": 0,
			"stat_mp_max": 0,
		},
	}
	body := service.buildCurrentActorBindingMode1BodyForSelected(
		context.Background(),
		nil,
		character,
		true,
		19,
		4,
	)
	pvfStats, ok := service.characterPVFStatsForUserInfo(context.Background(), nil, character, true)
	if !ok {
		t.Fatal("selected actor-binding PVF stats missing")
	}
	if got := binary.LittleEndian.Uint32(body[currentMode1StatLengthOffset:currentMode1StatDataOffset]); got != currentMode1StatBlobWireSize {
		t.Fatalf("selected actor-binding stat length=%d want=%d body=%x", got, currentMode1StatBlobWireSize, body)
	}
	if got := binary.LittleEndian.Uint32(body[currentMode1StatDataOffset : currentMode1StatDataOffset+4]); got != uint32(pvfStats.HPMax) {
		t.Fatalf("selected actor-binding hp max=%d want PVF %d body=%x", got, pvfStats.HPMax, body)
	}
	if got := binary.LittleEndian.Uint32(body[currentMode1StatDataOffset+4 : currentMode1StatDataOffset+8]); got != uint32(pvfStats.MPMax) {
		t.Fatalf("selected actor-binding mp max=%d want PVF %d body=%x", got, pvfStats.MPMax, body)
	}
	if got := body[currentMode1CreateCountOffset]; got != 0 {
		t.Fatalf("selected actor-binding equipment create count=%d want=0 body=%x", got, body)
	}
}

func TestBuildCurrentSelectedUserInfoMode1CreatesCurrentRowsBeforeSlotUpdates(t *testing.T) {
	repos := testRepositoryGroup()
	rawWeapon := buildInitialEquipmentRawEntry(11, 900001, 27)
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    900001,
				RawEntry:  rawWeapon,
				Extra:     map[string]string{"source": "pvf_create_equipment_list"},
			},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
		characterStats: testCharacterStatTable(t),
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
	}

	body := service.buildCurrentSelectedUserInfoMode1Body(context.Background(), nil, nil, character, true, 77)
	createSize := currentMode1EquipmentCreateRowBaseWireSize
	if got := len(body); got != currentMode1BaseWireSize+createSize {
		t.Fatalf("mode1 body len = %d, want create-only row body=%x", got, body)
	}
	if got := body[currentMode1CreateCountOffset]; got != 1 {
		t.Fatalf("mode1 sub_1D77560 create count = %d, want 1", got)
	}
	if got := body[currentMode1CreateRowsOffset]; got != 12 {
		t.Fatalf("mode1 create actor slot = %d, want 12", got)
	}
	if got := binary.LittleEndian.Uint32(body[currentMode1CreateRowsOffset+1 : currentMode1CreateRowsOffset+5]); got != 900001 {
		t.Fatalf("mode1 create item id = %d, want 900001", got)
	}
	if got := binary.LittleEndian.Uint32(body[currentMode1CreateRowsOffset+5 : currentMode1CreateRowsOffset+9]); got != 0 {
		t.Fatalf("mode1 create v74/grade = %d, want Python default 0", got)
	}
	finalStateStart := currentMode1CreateRowsOffset + createSize
	if got := binary.LittleEndian.Uint32(body[finalStateStart : finalStateStart+4]); got != 0 {
		t.Fatalf("mode1 sub_1D77560 final state = %#x, want 0", got)
	}
	objectKeyStart := finalStateStart + 4
	if got := binary.LittleEndian.Uint16(body[objectKeyStart : objectKeyStart+2]); got != 0 {
		t.Fatalf("mode1 create-only deferred-update key = %#x, want 0", got)
	}
	updateCountStart := objectKeyStart + 2
	if got := body[updateCountStart]; got != 0 {
		t.Fatalf("mode1 sub_2007B00 row count = %d, want 0 after create", got)
	}
	if got := body[updateCountStart+1+8]; got != 0xff {
		t.Fatalf("mode1 resource group after create-only section = %#x, want 0xff", got)
	}
}

func TestBuildCurrentSelectedUserInfoMode1PreservesRuntimeEquipmentQuality(t *testing.T) {
	const (
		itemID = uint32(900001)
		seed   = uint32(345678901)
	)
	repos := testRepositoryGroup()
	rawWeapon := make([]byte, currentItemListEntryWireSize)
	binary.LittleEndian.PutUint16(rawWeapon[0:2], 12)
	binary.LittleEndian.PutUint32(rawWeapon[2:6], itemID)
	binary.LittleEndian.PutUint32(rawWeapon[6:10], seed)
	binary.LittleEndian.PutUint16(rawWeapon[0x0b:0x0d], 45)
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"12": {
				SlotIndex: 12,
				ItemID:    int64(itemID),
				RawEntry:  rawWeapon,
				Extra: map[string]string{
					"current_exe_equipment_type": "12",
					"current_exe_runtime_move":   "1",
					"item_kind":                  "equipment",
					"quality_seed":               strconv.FormatUint(uint64(seed), 10),
				},
			},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
		characterStats: testCharacterStatTable(t),
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
	}

	body := service.buildCurrentSelectedUserInfoMode1Body(context.Background(), nil, nil, character, true, 77)
	if got := body[currentMode1CreateCountOffset]; got != 1 {
		t.Fatalf("mode1 create count = %d, want 1", got)
	}
	createStart := currentMode1CreateRowsOffset
	if got := binary.LittleEndian.Uint32(body[createStart+5 : createStart+9]); got != seed {
		t.Fatalf("mode1 equipped quality seed = %d, want %d", got, seed)
	}
}

func TestBuildCurrentSelectedUserInfoMode1WritesExtraBackedCurrentContainers(t *testing.T) {
	repos := testRepositoryGroup()
	rawWeapon := buildInitialEquipmentRawEntry(11, 900001, 27)
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    900001,
				RawEntry:  rawWeapon,
				Extra: map[string]string{
					"source":                        "pvf_create_equipment_list",
					"mode1_create_enabled":          "1",
					"mode1_detail_state_word":       "27",
					"mode1_reads_raw_blocks":        "1",
					"mode1_raw_a_hex":               "010203",
					"mode1_raw_b_hex":               "0405",
					"mode1_sub_1d6e020_records_hex": "1122334401020304aabbccdd05060708",
					"mode1_state_112":               "0x4d2",
					"mode1_sub_225cca0_dwords_hex":  "78563412f0debc9a",
					"mode1_state_120":               "1",
					"mode1_state_160":               "2",
					"mode1_state_128":               "3",
					"mode1_state_140":               "4660",
					"mode1_state_144":               "5",
					"mode1_resource_byte":           "6",
					"mode1_state_168":               "7",
					"mode1_state_169":               "8",
					"mode1_state_170":               "9",
					"mode1_raw_c_hex":               "090a",
					"mode1_raw_d_hex":               "0100000002000000",
					"mode1_state_183":               "10",
					"mode1_raw_e_hex":               "0300000004",
				},
			},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
		characterStats: testCharacterStatTable(t),
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
	}

	body := service.buildCurrentSelectedUserInfoMode1Body(context.Background(), nil, nil, character, true, 77)
	expectedCreateSize := currentMode1EquipmentCreateRowWireSizeFor(currentMode1EquipmentObjectRow{
		readsRawBlocks: true,
		rawA:           []byte{1, 2, 3},
		rawB:           []byte{4, 5},
		vector1D6E020:  make([]byte, 16),
		vector225CCA0:  make([]byte, 8),
		lateRawC:       []byte{9, 10},
		lateRawD:       make([]byte, 8),
		lateRawE:       make([]byte, 5),
	})
	if got := len(body); got != currentMode1BaseWireSize+expectedCreateSize {
		t.Fatalf("mode1 body len = %d, want current actor branch plus extra-backed row body=%x", got, body)
	}
	if got := body[currentMode1CreateCountOffset]; got != 1 {
		t.Fatalf("mode1 verified sub_1D77560 create count = %d, want 1", got)
	}
	createStart := currentMode1CreateRowsOffset
	pos := createStart
	if got := body[pos]; got != 12 {
		t.Fatalf("mode1 extra row actor slot = %d, want 12", got)
	}
	pos++    // Current EXE runtime equipment slot.
	pos += 4 // item id
	pos += 4 // instance
	pos++    // ext data
	pos += 2 // current durability word
	pos += 4 // optional clone/linked id
	pos += 4 // auxiliary value
	pos++    // auxiliary flag
	pos++    // bind flag
	pos += 2 // marker16

	var raw []byte
	raw, pos = readMode1RawBlockForTest(body, pos)
	if !bytes.Equal(raw, []byte{1, 2, 3}) {
		t.Fatalf("mode1 rawA = %x, want 010203", raw)
	}
	raw, pos = readMode1RawBlockForTest(body, pos)
	if !bytes.Equal(raw, []byte{4, 5}) {
		t.Fatalf("mode1 rawB = %x, want 0405", raw)
	}
	if got := body[pos]; got != 2 {
		t.Fatalf("mode1 sub_1D6E020 count = %d, want 2", got)
	}
	pos++
	if got := body[pos : pos+16]; !bytes.Equal(got, []byte{0x11, 0x22, 0x33, 0x44, 1, 2, 3, 4, 0xaa, 0xbb, 0xcc, 0xdd, 5, 6, 7, 8}) {
		t.Fatalf("mode1 sub_1D6E020 records = %x", got)
	}
	pos += 16
	if got := binary.LittleEndian.Uint32(body[pos : pos+4]); got != 1234 {
		t.Fatalf("mode1 state112 = %d, want 1234", got)
	}
	pos += 4
	if got := body[pos]; got != 2 {
		t.Fatalf("mode1 sub_225CCA0 count = %d, want 2", got)
	}
	pos++
	if got := body[pos : pos+8]; !bytes.Equal(got, []byte{0x78, 0x56, 0x34, 0x12, 0xf0, 0xde, 0xbc, 0x9a}) {
		t.Fatalf("mode1 sub_225CCA0 dwords = %x", got)
	}
	pos += 8
	if got := binary.LittleEndian.Uint16(body[pos : pos+2]); got != 0 {
		t.Fatalf("mode1 sub_1E636E0 state = %d, want current-item zero", got)
	}
	pos += 2
	if got := body[pos]; got != 0 {
		t.Fatalf("mode1 sub_1C61F40 count = %d, want current-item zero", got)
	}
	pos++
	if got := body[pos : pos+3]; !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("mode1 state120/160/128 = %x", got)
	}
	pos += 3
	if got := binary.LittleEndian.Uint16(body[pos : pos+2]); got != 4660 {
		t.Fatalf("mode1 state140 = %d, want 4660", got)
	}
	pos += 2
	if got := body[pos : pos+5]; !bytes.Equal(got, []byte{5, 6, 7, 8, 9}) {
		t.Fatalf("mode1 state144/resource/168/169/170 = %x", got)
	}
	pos += 5
	raw, pos = readMode1RawBlockForTest(body, pos)
	if !bytes.Equal(raw, []byte{9, 10}) {
		t.Fatalf("mode1 late rawC = %x, want 090a", raw)
	}
	raw, pos = readMode1RawBlockForTest(body, pos)
	if !bytes.Equal(raw, []byte{1, 0, 0, 0, 2, 0, 0, 0}) {
		t.Fatalf("mode1 late rawD = %x", raw)
	}
	if got := body[pos]; got != 10 {
		t.Fatalf("mode1 state183 = %d, want 10", got)
	}
	pos++
	raw, pos = readMode1RawBlockForTest(body, pos)
	if !bytes.Equal(raw, []byte{3, 0, 0, 0, 4}) {
		t.Fatalf("mode1 late rawE = %x", raw)
	}
	if got := pos; got != createStart+expectedCreateSize {
		t.Fatalf("mode1 create row ended at %#x, want %#x", got, createStart+expectedCreateSize)
	}
	if got := binary.LittleEndian.Uint32(body[pos : pos+4]); got != 0 {
		t.Fatalf("mode1 final state after extra row = %#x, want 0", got)
	}
	pos += 4
	if got := binary.LittleEndian.Uint16(body[pos : pos+2]); got != 0 {
		t.Fatalf("mode1 create-only update key = %#x, want 0", got)
	}
	if got := body[pos+2]; got != 0 {
		t.Fatalf("mode1 create-only update count = %d, want 0", got)
	}
}

func TestBuildCurrentSelectedUserInfoMode1WritesPVFStarterCreateRowFromCurrentItemState(t *testing.T) {
	repos := testRepositoryGroup()
	rawWeapon := buildInitialEquipmentRawEntry(11, 101010912, 45)
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    101010912,
				RawEntry:  rawWeapon,
				Extra: map[string]string{
					"source":                     "pvf_create_equipment_list",
					"current_exe_equipment_type": "17",
					"instance_value":             strconv.FormatUint(initialEquipmentCreateValue, 10),
					"durability":                 "45",
				},
			},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
		characterStats: testCharacterStatTable(t),
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
	}

	body := service.buildCurrentSelectedUserInfoMode1Body(context.Background(), nil, nil, character, true, 77)
	createSize := currentMode1EquipmentCreateRowBaseWireSize
	if got := len(body); got != currentMode1BaseWireSize+createSize {
		t.Fatalf("mode1 PVF starter body len = %d, want %d+%d body=%x", got, currentMode1BaseWireSize, createSize, body)
	}
	if got := body[currentMode1CreateCountOffset]; got != 1 {
		t.Fatalf("mode1 PVF starter create count = %d, want 1", got)
	}
	createStart := currentMode1CreateRowsOffset
	if got := body[createStart]; got != 12 {
		t.Fatalf("mode1 PVF starter actor slot = %d, want 12", got)
	}
	if got := binary.LittleEndian.Uint32(body[createStart+1 : createStart+5]); got != 101010912 {
		t.Fatalf("mode1 PVF starter item id = %d, want 101010912", got)
	}
	if got := binary.LittleEndian.Uint32(body[createStart+5 : createStart+9]); got != 0 {
		t.Fatalf("mode1 PVF starter v74/grade = %d, want Python default 0", got)
	}
	if got := binary.LittleEndian.Uint16(body[createStart+10 : createStart+12]); got != 45 {
		t.Fatalf("mode1 PVF starter durability = %d, want 45", got)
	}
	if got := binary.LittleEndian.Uint32(body[createStart+25 : createStart+29]); got != 0 {
		t.Fatalf("mode1 PVF starter state112 = %#x, want current 0x77 value 0", got)
	}
	if got := body[createStart+30 : createStart+33]; !bytes.Equal(got, []byte{0, 0, 0}) {
		t.Fatalf("mode1 PVF starter mandatory nested state = %x, want u16 zero plus empty count", got)
	}
	pos := createStart + createSize
	pos += 4 // final state
	if got := binary.LittleEndian.Uint16(body[pos : pos+2]); got != 0 {
		t.Fatalf("mode1 PVF starter update key = %#x, want 0", got)
	}
	pos += 2
	if got := body[pos]; got != 0 {
		t.Fatalf("mode1 PVF starter update count = %d, want 0", got)
	}
	pos++
	pos += 8 // sub_20077D0 raw8 after sub_2007B00 rows.
	if got := body[pos]; got != 0xff {
		t.Fatalf("mode1 PVF starter resource selector after create-only section = %#x, want 0xff", got)
	}
}

func TestBuildCurrentSelectedUserInfoMode1CreatesAllPVFStarterObjects(t *testing.T) {
	repos := testRepositoryGroup()
	entries := make(map[string]dnfrepo.EquipmentEntry)
	for _, seed := range []struct {
		slot       int16
		itemID     int64
		durability uint16
	}{
		{slot: 11, itemID: 101010912, durability: 45},
		{slot: 13, itemID: 10400, durability: 33},
		{slot: 15, itemID: 12400, durability: 27},
		{slot: 16, itemID: 18400, durability: 20},
		{slot: 17, itemID: 16400, durability: 20},
	} {
		entries[strconv.Itoa(int(seed.slot))] = dnfrepo.EquipmentEntry{
			SlotIndex: seed.slot,
			ItemID:    seed.itemID,
			RawEntry:  buildInitialEquipmentRawEntry(seed.slot, seed.itemID, seed.durability),
			Extra: map[string]string{
				"source":                   "pvf_create_equipment_list",
				"current_exe_create_value": "1",
				"durability":               strconv.Itoa(int(seed.durability)),
			},
		}
	}
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries:     entries,
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
		characterStats:     testCharacterStatTable(t),
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
	}

	body := service.buildCurrentSelectedUserInfoMode1Body(context.Background(), nil, nil, character, true, 77)
	const createCount = 5
	wantLen := currentMode1BaseWireSize + createCount*currentMode1EquipmentCreateRowBaseWireSize
	if got := len(body); got != wantLen {
		t.Fatalf("mode1 body len = %d, want %d body=%x", got, wantLen, body)
	}
	if got := body[currentMode1CreateCountOffset]; got != createCount {
		t.Fatalf("mode1 create count = %d, want %d", got, createCount)
	}
	for index, wantSlot := range []byte{12, 14, 16, 17, 18} {
		rowStart := currentMode1CreateRowsOffset + index*currentMode1EquipmentCreateRowBaseWireSize
		if got := body[rowStart]; got != wantSlot {
			t.Fatalf("mode1 create row %d actor slot = %d, want %d", index, got, wantSlot)
		}
		if got := binary.LittleEndian.Uint32(body[rowStart+5 : rowStart+9]); got != 0 {
			t.Fatalf("mode1 create row %d v74/grade = %d, want 0", index, got)
		}
	}
	updateCountPos := currentMode1CreateRowsOffset + createCount*currentMode1EquipmentCreateRowBaseWireSize + 4 + 2
	if got := body[updateCountPos]; got != 0 {
		t.Fatalf("mode1 update count = %d, want 0 after complete create list", got)
	}
}

func TestCurrentMode1CreateRowsRequireVerifiedDetailState(t *testing.T) {
	emptyDetailRows := currentMode1EquipmentCreateRows([]currentMode1EquipmentObjectRow{{
		createEnabled: true,
		slot:          11,
		itemID:        900001,
		state112:      0xffffffff,
	}})
	if len(emptyDetailRows) != 0 {
		t.Fatalf("empty-detail mode1 create rows = %d, want 0", len(emptyDetailRows))
	}

	verifiedRows := currentMode1EquipmentCreateRows([]currentMode1EquipmentObjectRow{{
		createEnabled:      true,
		slot:               11,
		itemID:             900001,
		equipmentType:      11,
		equipmentTypeKnown: true,
		instance:           1,
		durability:         27,
		durabilityKnown:    true,
		state112:           0xffffffff,
		readsRawBlocks:     true,
		rawB:               []byte{1},
	}})
	if len(verifiedRows) != 1 {
		t.Fatalf("verified-detail mode1 create rows = %d, want 1", len(verifiedRows))
	}

	skippedRawRows := currentMode1EquipmentCreateRows([]currentMode1EquipmentObjectRow{{
		createEnabled: true,
		slot:          11,
		itemID:        900001,
		state112:      0xffffffff,
		rawB:          []byte{1},
	}})
	if len(skippedRawRows) != 0 {
		t.Fatalf("non-read rawB-only mode1 create rows = %d, want 0", len(skippedRawRows))
	}

	plainCurrentItemRows := currentMode1EquipmentCreateRows([]currentMode1EquipmentObjectRow{{
		createEnabled:           true,
		currentItemStateDerived: true,
		slot:                    11,
		itemID:                  900001,
		equipmentType:           12,
		equipmentTypeKnown:      true,
		instance:                initialEquipmentCreateValue,
		durability:              27,
		durabilityKnown:         true,
		state112:                0,
	}})
	if len(plainCurrentItemRows) != 1 {
		t.Fatalf("plain current-item mode1 create rows = %d, want 1", len(plainCurrentItemRows))
	}

	constructorSentinelRows := currentMode1EquipmentCreateRows([]currentMode1EquipmentObjectRow{{
		createEnabled:           true,
		currentItemStateDerived: true,
		slot:                    11,
		itemID:                  900001,
		equipmentType:           12,
		equipmentTypeKnown:      true,
		instance:                initialEquipmentCreateValue,
		durability:              0xffff,
		durabilityKnown:         true,
		state112:                0xffffffff,
	}})
	if len(constructorSentinelRows) != 0 {
		t.Fatalf("constructor-sentinel rows = %d, want 0 because they do not match current 0x77 item state", len(constructorSentinelRows))
	}

	unknownWordRows := currentMode1EquipmentCreateRows([]currentMode1EquipmentObjectRow{{
		createEnabled:      true,
		slot:               11,
		itemID:             900001,
		equipmentType:      11,
		equipmentTypeKnown: true,
		state112:           0xffffffff,
		readsRawBlocks:     true,
		rawB:               []byte{1},
	}})
	if len(unknownWordRows) != 0 {
		t.Fatalf("unknown packet-word create rows = %d, want 0", len(unknownWordRows))
	}
}

func TestCurrentMode1CreateRowWireSizeFollowsCurrentExeConditions(t *testing.T) {
	baseRow := currentMode1EquipmentObjectRow{}
	if got := currentMode1EquipmentCreateRowWireSizeFor(baseRow); got != 56 {
		t.Fatalf("mode1 create row base wire size = %d, want 56 including mandatory nested u16/count", got)
	}

	rawRow := currentMode1EquipmentObjectRow{readsRawBlocks: true}
	if got := currentMode1EquipmentCreateRowWireSizeFor(rawRow); got != 64 {
		t.Fatalf("mode1 create row raw-block wire size = %d, want base plus two raw headers", got)
	}

	type26Row := currentMode1EquipmentObjectRow{readsDurabilityOverrideU32: true}
	if got := currentMode1EquipmentCreateRowWireSizeFor(type26Row); got != 60 {
		t.Fatalf("mode1 create row type26 wire size = %d, want base plus one u32", got)
	}

	linkedRow := currentMode1EquipmentObjectRow{
		slot:               9,
		equipmentType:      9,
		equipmentTypeKnown: true,
		linkedItemID:       123,
		linkedRaw:          []byte{1, 2, 3},
	}
	if got := currentMode1EquipmentCreateRowWireSizeFor(linkedRow); got != 63 {
		t.Fatalf("mode1 create row linked slot9 wire size = %d, want base plus raw header/data", got)
	}

	ignoredRawRow := currentMode1EquipmentObjectRow{rawA: []byte{1, 2, 3}, rawB: []byte{4}, linkedRaw: []byte{5, 6}}
	if got := currentMode1EquipmentCreateRowWireSizeFor(ignoredRawRow); got != 56 {
		t.Fatalf("mode1 create row ignored raw wire size = %d, want base only when EXE branch does not read raw", got)
	}
}

func TestCurrentMode1IndexedStateComesFromCurrentItemOffsets(t *testing.T) {
	var entry currentItemListEntry
	entry.data[0x47] = 2
	entry.data[0x48] = 1
	entry.data[0x49] = 2
	entry.data[0x4b] = 4
	entry.data[0x4c] = 5
	entry.data[0x4e] = 7
	entry.data[0x4f] = 8
	entry.data[0x51] = 9
	entry.data[0x52] = 1
	entry.data[0x53] = 10
	entry.data[0x54] = 11
	entry.data[0x55] = 12
	entry.data[0x56] = 13

	state := currentMode1EquipmentIndexedStateFromCurrentItem(entry)
	if state.currentItemMismatch {
		t.Fatal("current 0x77 indexed state unexpectedly rejected")
	}
	var writer packetWriter
	writeCurrentMode1EquipmentIndexedState(&writer, state)
	want := []byte{2, 1, 4, 7, 2, 5, 8, 9, 1, 10, 11, 12, 13}
	if got := writer.bytes(); !bytes.Equal(got, want) {
		t.Fatalf("mode1 sub_1C61F40 wire = %x, want %x", got, want)
	}
	if got := currentMode1EquipmentIndexedStateVariableWireSize(state); got != len(want)-1 {
		t.Fatalf("mode1 sub_1C61F40 variable size = %d, want %d", got, len(want)-1)
	}
}

func TestCurrentMode1IndexedStateRejectsMismatchedCurrentItemCount(t *testing.T) {
	var entry currentItemListEntry
	entry.data[0x47] = 2
	entry.data[0x48] = 1
	state := currentMode1EquipmentIndexedStateFromCurrentItem(entry)
	if !state.currentItemMismatch {
		t.Fatal("mismatched current 0x77 indexed state should be rejected")
	}
	rows := currentMode1EquipmentCreateRows([]currentMode1EquipmentObjectRow{{
		createEnabled:           true,
		currentItemStateDerived: true,
		itemID:                  900001,
		equipmentType:           12,
		equipmentTypeKnown:      true,
		instance:                1,
		durability:              20,
		durabilityKnown:         true,
		indexedState1C61F40:     state,
	}})
	if len(rows) != 0 {
		t.Fatalf("mismatched current indexed state create rows = %d, want 0", len(rows))
	}
}

func TestCurrentMode1EquipmentReadsRawBlocksFollowsCurrentExeEquipmentType(t *testing.T) {
	if !currentMode1EquipmentReadsRawBlocks(nil, 11, true) {
		t.Fatalf("equipment type 11 should read raw blocks")
	}
	if currentMode1EquipmentReadsRawBlocks(nil, 12, true) {
		t.Fatalf("equipment type 12 should not read raw blocks")
	}
	if !currentMode1EquipmentReadsRawBlocks(map[string]string{"mode1_reads_raw_blocks": "1"}, 12, true) {
		t.Fatalf("explicit mode1 raw-block flag should override equipment type")
	}
	if currentMode1EquipmentReadsRawBlocks(nil, 17, true) {
		t.Fatalf("looked-up equipment type 17 should not read raw blocks")
	}
	if currentMode1EquipmentReadsRawBlocks(nil, 0, false) {
		t.Fatalf("unknown equipment type should not guess raw blocks without current EXE/PVF evidence")
	}
}

func TestBuildCurrentSelectedUserInfoMode1CarriesEquippedAuraSocketAndColorState(t *testing.T) {
	repos := testRepositoryGroup()
	socketData := make([]byte, currentAvatarSocketBytes)
	binary.LittleEndian.PutUint16(socketData[0:2], 0x00ef)
	binary.LittleEndian.PutUint32(socketData[2:6], 10095199)
	binary.LittleEndian.PutUint16(socketData[6:8], 0x0004)
	binary.LittleEndian.PutUint32(socketData[8:12], 10095200)
	colorData := []byte{0x21, 0x43, 0x65}
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"9": {
				SlotIndex: 9,
				ItemID:    101590068,
				Extra: map[string]string{
					"current_exe_equipment_type": "9",
					"current_exe_runtime_move":   "1",
					"avatar_socket_data":         hex.EncodeToString(socketData),
					"avatar_color_data":          hex.EncodeToString(colorData),
				},
			},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipped aura: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
		characterStats:     testCharacterStatTable(t),
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
	}

	body := service.buildCurrentSelectedUserInfoMode1Body(context.Background(), nil, nil, character, true, 77)
	if got := body[currentMode1CreateCountOffset]; got != 1 {
		t.Fatalf("mode1 equipped aura create count=%d want=1 body=%x", got, body)
	}
	createStart := currentMode1CreateRowsOffset
	if got := body[createStart]; got != 9 {
		t.Fatalf("mode1 equipped aura actor slot=%d want=9", got)
	}
	rawAStart := createStart + 24
	if got := binary.LittleEndian.Uint32(body[rawAStart : rawAStart+4]); got != currentAvatarSocketBytes {
		t.Fatalf("mode1 equipped aura socket length=%d want=%d body=%x", got, currentAvatarSocketBytes, body)
	}
	projectedSocket := body[rawAStart+4 : rawAStart+4+currentAvatarSocketBytes]
	if got := binary.LittleEndian.Uint16(projectedSocket[0:2]); got != 0xffef {
		t.Fatalf("mode1 equipped aura universal socket type=%#04x want=0xffef data=%x", got, projectedSocket)
	}
	if got := binary.LittleEndian.Uint32(projectedSocket[2:6]); got != 10095199 {
		t.Fatalf("mode1 equipped aura first emblem=%d want=10095199 data=%x", got, projectedSocket)
	}
	if got := binary.LittleEndian.Uint32(projectedSocket[8:12]); got != 10095200 {
		t.Fatalf("mode1 equipped aura second emblem=%d want=10095200 data=%x", got, projectedSocket)
	}
	rawBStart := rawAStart + 4 + currentAvatarSocketBytes
	if got := binary.LittleEndian.Uint32(body[rawBStart : rawBStart+4]); got != uint32(len(colorData)) {
		t.Fatalf("mode1 equipped aura color length=%d want=%d body=%x", got, len(colorData), body)
	}
	if got := body[rawBStart+4 : rawBStart+4+len(colorData)]; !bytes.Equal(got, colorData) {
		t.Fatalf("mode1 equipped aura color data=%x want=%x", got, colorData)
	}
}

func TestCurrentMode1EquipmentReadsDurabilityOverrideFollowsCurrentExeType26(t *testing.T) {
	if !currentMode1EquipmentReadsDurabilityOverrideU32(nil, 26, true) {
		t.Fatalf("equipment type 26 should read durability override u32")
	}
	if currentMode1EquipmentReadsDurabilityOverrideU32(nil, 11, true) {
		t.Fatalf("equipment type 11 should not read durability override u32")
	}
	if !currentMode1EquipmentReadsDurabilityOverrideU32(map[string]string{"mode1_reads_durability_override_u32": "1"}, 11, true) {
		t.Fatalf("explicit type26 override flag should force extra u32")
	}
	if !currentMode1EquipmentReadsDurabilityOverrideU32(nil, 26, true) {
		t.Fatalf("looked-up equipment type 26 should read durability override u32")
	}
}

func TestCurrentMode1EquipmentTypeRejectsPVFMetadataAndLegacySeedAlias(t *testing.T) {
	reader := csharpLegacyUserInfoReader{}
	for name, entry := range map[string]dnfrepo.EquipmentEntry{
		"pvf_only": {
			SlotIndex: -1,
			Extra:     map[string]string{"equipment_type": "17"},
		},
		"legacy_seed_alias": {
			Extra: map[string]string{
				"source":                     "pvf_create_equipment_list",
				"equipment_type":             "17",
				"current_exe_equipment_type": "17",
			},
		},
	} {
		if value, ok := reader.currentMode1EquipmentType(entry); ok {
			t.Fatalf("%s runtime type = %d, want unknown", name, value)
		}
	}

	value, ok := reader.currentMode1EquipmentType(dnfrepo.EquipmentEntry{
		Extra: map[string]string{"current_exe_equipment_type": "12"},
	})
	if !ok || value != 12 {
		t.Fatalf("explicit current EXE runtime type = %d ok=%t, want 12/true", value, ok)
	}

	value, ok = reader.currentMode1EquipmentType(dnfrepo.EquipmentEntry{
		SlotIndex: 11,
		Extra: map[string]string{
			"source":                     "pvf_create_equipment_list",
			"current_exe_equipment_type": "17",
			"current_exe_runtime_move":   "1",
		},
	})
	if !ok || value != 17 {
		t.Fatalf("runtime-moved PVF equipment type = %d ok=%t, want 17/true", value, ok)
	}
}

func TestCurrentEXEActorEquipmentSlotFollowsWornSlotMap(t *testing.T) {
	want := map[int16]uint64{
		11: 12,
		13: 14,
		14: 15,
		15: 16,
		16: 17,
		17: 18,
		18: 19,
		19: 20,
		20: 21,
		21: 22,
		22: 23,
		23: 25,
		32: 32,
	}
	for slot, expected := range want {
		value, ok := currentEXEActorEquipmentSlotForWornSlot(slot)
		if !ok || value != expected {
			t.Fatalf("slot %d runtime type = %d ok=%t, want %d/true", slot, value, ok, expected)
		}
	}
	for _, slot := range []int16{0, 9, 10, 12, 24, 31} {
		if value, ok := currentEXEActorEquipmentSlotForWornSlot(slot); ok {
			t.Fatalf("unmapped slot %d runtime type = %d, want unknown", slot, value)
		}
	}
}

func TestCurrentEXEActorEquipmentSlotUsesDirectRuntimeBaseAppearanceSlots(t *testing.T) {
	for slot := int16(0); slot < currentActorMode0AppearanceSlotCount; slot++ {
		value, ok := currentEXEActorEquipmentSlot(dnfrepo.EquipmentEntry{
			SlotIndex: slot,
			ItemID:    int64(400000000) + int64(slot),
		})
		if !ok || value != uint64(slot) {
			t.Fatalf("runtime base slot %d actor slot=%d ok=%t, want %d/true", slot, value, ok, slot)
		}
	}
	value, ok := currentEXEActorEquipmentSlot(dnfrepo.EquipmentEntry{
		SlotIndex: 11,
		ItemID:    700,
		Extra:     map[string]string{"source": "pvf_create_equipment_list"},
	})
	if !ok || value != 12 {
		t.Fatalf("legacy PVF slot 11 actor slot=%d ok=%t, want 12/true", value, ok)
	}
	if value, ok := currentEXEActorEquipmentSlot(dnfrepo.EquipmentEntry{
		SlotIndex: 26,
		ItemID:    9001,
	}); ok {
		t.Fatalf("pet creature slot 26 actor equipment slot=%d, want dedicated lifecycle", value)
	}
	value, ok = currentEXEActorEquipmentSlot(dnfrepo.EquipmentEntry{
		SlotIndex: 26,
		ItemID:    9001,
		Extra: map[string]string{
			"equipment_slot":            "26",
			"creature_serial_or_handle": "27",
		},
	})
	if !ok || value != 26 {
		t.Fatalf("proven legacy pet slot 26 actor slot=%d ok=%t, want 26/true", value, ok)
	}
}

func TestBuildCurrentSelectedUserInfoMode1UsesCurrentItemDurabilityInsteadOfConstructorSentinel(t *testing.T) {
	repos := testRepositoryGroup()
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    900001,
				RawEntry:  buildInitialEquipmentRawEntry(11, 900001, 27),
				Extra: map[string]string{
					"source":                     "pvf_create_equipment_list",
					"equipment_type":             "17",
					"current_exe_equipment_type": "17",
					"current_exe_create_value":   "1",
					"durability":                 "27",
				},
			},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
		characterStats:     testCharacterStatTable(t),
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
	}

	body := service.buildCurrentSelectedUserInfoMode1Body(context.Background(), nil, nil, character, true, 77)
	const createSize = currentMode1EquipmentCreateRowBaseWireSize
	if got := len(body); got != currentMode1BaseWireSize+createSize {
		t.Fatalf("mode1 body len = %d, want create-only %d body=%x", got, currentMode1BaseWireSize+createSize, body)
	}
	if got := body[currentMode1CreateCountOffset]; got != 1 {
		t.Fatalf("mode1 create count = %d, want 1 from current 0x77 detail proof", got)
	}
	createStart := currentMode1CreateRowsOffset
	if got := binary.LittleEndian.Uint16(body[createStart+10 : createStart+12]); got != 27 {
		t.Fatalf("mode1 create durability = %#x, want current item durability 27 instead of constructor sentinel", got)
	}
	pos := createStart + createSize
	if got := binary.LittleEndian.Uint32(body[pos : pos+4]); got != 0 {
		t.Fatalf("mode1 final state = %#x, want 0", got)
	}
	pos += 4 + 2
	if got := body[pos]; got != 0 {
		t.Fatalf("mode1 update count = %d, want 0", got)
	}
}

func TestCurrentMode1EquipmentCreateEnabledAcceptsRuntimeRows(t *testing.T) {
	for name, entry := range map[string]dnfrepo.EquipmentEntry{
		"migrated starter": {
			SlotIndex: 12,
			ItemID:    900001,
			Extra: map[string]string{
				"source":                     "pvf_create_equipment_list",
				"current_exe_equipment_type": "12",
				"current_exe_runtime_move":   "1",
			},
		},
		"rental weapon": {
			SlotIndex: 12,
			ItemID:    900002,
			Extra: map[string]string{
				"source":                     "pvf_rental",
				"current_exe_equipment_type": "12",
			},
		},
	} {
		if !currentMode1EquipmentCreateEnabled(entry) {
			t.Fatalf("%s create disabled for runtime row: %+v", name, entry)
		}
	}
	if currentMode1EquipmentCreateEnabled(dnfrepo.EquipmentEntry{
		SlotIndex: 12,
		ItemID:    900003,
		Extra: map[string]string{
			"current_exe_equipment_type": "12",
			"mode1_create_enabled":       "false",
		},
	}) {
		t.Fatal("explicit mode1 create disable was ignored")
	}
}

func TestBuildCurrentSelectedUserInfoMode3CarriesPVFStatBlob(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats: map[string]int64{
			"exp":                         12345,
			"stat_hp_max":                 0,
			"stat_mp_max":                 0,
			"stat_strength":               0,
			"stat_physical_attack":        0,
			"stat_fire_resistance":        2,
			"stat_water_resistance":       3,
			"stat_dark_resistance":        4,
			"stat_light_resistance":       5,
			"active_status_resistance_00": 6,
			"active_status_resistance_17": 23,
			"stat_weight":                 0,
		},
	}
	service := Service{characterStats: testCharacterStatTable(t)}

	body := service.buildCurrentSelectedUserInfoMode3Body(context.Background(), nil, nil, character, true, 19)
	if got := body[0]; got != 3 {
		t.Fatalf("mode = %d, want 3", got)
	}
	if got := binary.LittleEndian.Uint16(body[1:3]); got != 1 {
		t.Fatalf("mode3 count = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(body[0x0d:0x0f]); got != 19 {
		t.Fatalf("mode3 object key = %#x, want %#x", got, 19)
	}
	if got := binary.LittleEndian.Uint32(body[0x0f:0x13]); got != 12345 {
		t.Fatalf("mode3 cumulative EXP = %d, want 12345", got)
	}
	if got := binary.LittleEndian.Uint32(body[0x13:0x17]); got != 92 {
		t.Fatalf("mode3 stat blob len = %d, want 92", got)
	}
	stat := body[0x17 : 0x17+92]
	if got := binary.LittleEndian.Uint32(stat[0:4]); got != 11000 {
		t.Fatalf("mode3 hp max = %d, want 11000", got)
	}
	if got := binary.LittleEndian.Uint32(stat[4:8]); got != 11800 {
		t.Fatalf("mode3 mp max = %d, want 11800", got)
	}
	if got := bodyInt16ForUserInfoTest(stat[8:10]); got != 110 {
		t.Fatalf("mode3 strength wire = %d, want 110 for client /10 display", got)
	}
	if got := bodyInt16ForUserInfoTest(stat[10:12]); got != 130 {
		t.Fatalf("mode3 vitality wire = %d, want 130 for client /10 display", got)
	}
	if got := bodyInt16ForUserInfoTest(stat[12:14]); got != 120 {
		t.Fatalf("mode3 intelligence wire = %d, want 120 for client /10 display", got)
	}
	if got := bodyInt16ForUserInfoTest(stat[14:16]); got != 140 {
		t.Fatalf("mode3 spirit wire = %d, want 140 for client /10 display", got)
	}
	if got := bodyInt16ForUserInfoTest(stat[16:18]); got != 20 {
		t.Fatalf("mode3 fire resistance wire = %d, want 20 for client /10 display", got)
	}
	if got := bodyInt16ForUserInfoTest(stat[18:20]); got != 30 {
		t.Fatalf("mode3 water resistance wire = %d, want 30 for client /10 display", got)
	}
	if got := bodyInt16ForUserInfoTest(stat[20:22]); got != 40 {
		t.Fatalf("mode3 dark resistance wire = %d, want 40 for client /10 display", got)
	}
	if got := bodyInt16ForUserInfoTest(stat[22:24]); got != 50 {
		t.Fatalf("mode3 light resistance wire = %d, want 50 for client /10 display", got)
	}
	if got := bodyInt16ForUserInfoTest(stat[24:26]); got != 60 {
		t.Fatalf("mode3 active status resistance 00 wire = %d, want 60", got)
	}
	if got := bodyInt16ForUserInfoTest(stat[58:60]); got != 230 {
		t.Fatalf("mode3 active status resistance 17 wire = %d, want 230", got)
	}
	if got := binary.LittleEndian.Uint32(stat[80:84]); got != 510000 {
		t.Fatalf("mode3 weight = %d, want 510000", got)
	}
	if got := stat[84]; got != currentActorBaseStatScalePercent {
		t.Fatalf("mode3 base-stat scale = %d, want neutral percentage %d", got, currentActorBaseStatScalePercent)
	}
	if got := binary.LittleEndian.Uint32(stat[85:89]); got != 0 {
		t.Fatalf("mode3 packed runtime stat at +0x55 = %d, want 0", got)
	}
	if got := len(body); got != 172 {
		t.Fatalf("mode3 body len = %d, want 172", got)
	}
	if got := body[0x81]; got != 0xff {
		t.Fatalf("mode3 resource group marker = %#x, want 0xff", got)
	}
	if got := binary.LittleEndian.Uint32(body[0xa6:0xaa]); got != 0 {
		t.Fatalf("mode3 sub_2002DC0 count = %d, want 0", got)
	}
}

func TestCurrentMode1StatBlobDoesNotLeakExtraEquipmentSlotBitsIntoRuntimeFloat(t *testing.T) {
	reader := csharpLegacyUserInfoReader{}
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		Stats: map[string]int64{
			"ex_equip_slot_stat": int64(dnfquest.ExEquipSlotAll),
		},
	}
	stat := reader.buildCurrentUserInfoMode1StatBlob(dnfrepo.LegacyUserInfoRow{}, character)
	if got := binary.LittleEndian.Uint32(stat[85:89]); got != 0 {
		t.Fatalf("current runtime float at +0x55=%d want neutral 0", got)
	}
}

func TestCurrentMode1AndMode3CarryDatabaseExtraEquipmentSlotStateAfterStatBlob(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       90,
		Stats: map[string]int64{
			"ex_equip_slot_stat": int64(dnfquest.ExEquipSlotEarring),
		},
	}
	service := Service{characterStats: testCharacterStatTable(t)}

	mode1 := service.buildCurrentSelectedUserInfoMode1Body(context.Background(), nil, nil, character, true, 19)
	if got := mode1[currentMode1ObjectTailOffset]; got != dnfquest.ExEquipSlotEarring {
		t.Fatalf("mode1 extra-equipment-slot state=%#x want=%#x", got, dnfquest.ExEquipSlotEarring)
	}

	mode3 := service.buildCurrentSelectedUserInfoMode3Body(context.Background(), nil, nil, character, true, 19)
	const mode3ExtraEquipmentSlotOffset = 0x17 + currentMode1StatBlobWireSize
	if got := mode3[mode3ExtraEquipmentSlotOffset]; got != dnfquest.ExEquipSlotEarring {
		t.Fatalf("mode3 extra-equipment-slot state=%#x want=%#x", got, dnfquest.ExEquipSlotEarring)
	}
}

func TestBuildCurrentSelectedUserInfoMode3AppliesCSharpHPMPCompatibilityOnce(t *testing.T) {
	for _, test := range []struct {
		name     string
		storedHP int64
		storedMP int64
	}{
		{name: "pre_compatibility_pvf_base", storedHP: 1200, storedMP: 1300},
		{name: "already_compatible", storedHP: 11000, storedMP: 11800},
	} {
		t.Run(test.name, func(t *testing.T) {
			character := dnfrepo.CharacterRecord{
				CharacterID: "19",
				AccountID:   "dnf:1",
				Name:        "hero",
				Job:         "11",
				Level:       1,
				Stats: map[string]int64{
					"stat_hp_max": test.storedHP,
					"stat_mp_max": test.storedMP,
				},
			}
			service := Service{characterStats: testCharacterStatTable(t)}

			body := service.buildCurrentSelectedUserInfoMode3Body(context.Background(), nil, nil, character, true, 19)
			stat := body[0x17 : 0x17+92]
			if got := binary.LittleEndian.Uint32(stat[0:4]); got != 11000 {
				t.Fatalf("mode3 hp max = %d, want one compatibility application 11000", got)
			}
			if got := binary.LittleEndian.Uint32(stat[4:8]); got != 11800 {
				t.Fatalf("mode3 mp max = %d, want one compatibility application 11800", got)
			}
		})
	}
}

func TestBuildCurrentSelectedUserInfoMode3FallsBackToPVFElementalResistance(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats: map[string]int64{
			"stat_fire_resistance":  0,
			"stat_water_resistance": 0,
			"stat_dark_resistance":  0,
			"stat_light_resistance": 0,
		},
	}
	service := Service{characterStats: testCharacterStatTable(t)}

	body := service.buildCurrentSelectedUserInfoMode3Body(context.Background(), nil, nil, character, true, 19)
	stat := body[0x17 : 0x17+92]
	for _, test := range []struct {
		name   string
		offset int
		want   int
	}{
		{name: "fire", offset: 0x10, want: 60},
		{name: "water", offset: 0x12, want: 70},
		{name: "dark", offset: 0x14, want: 80},
		{name: "light", offset: 0x16, want: 90},
	} {
		if got := bodyInt16ForUserInfoTest(stat[test.offset : test.offset+2]); got != test.want {
			t.Fatalf("mode3 %s resistance wire = %d, want PVF fallback %d", test.name, got, test.want)
		}
	}
}

func TestBuildCurrentSelectedUserInfoMode3DoesNotMergeEquipmentIntoBaseStatBlob(t *testing.T) {
	repos := testRepositoryGroup()
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    900001,
				RawEntry:  buildInitialEquipmentRawEntry(11, 900001, 27),
				Extra:     map[string]string{"source": "pvf_create_equipment_list"},
			},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
		characterStats: testCharacterStatTable(t),
		equipmentStats: map[int64]dnfcharstat.Vector{
			900001: {PhysicalAttack: 99, Strength: 88},
		},
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Stats: map[string]int64{
			"stat_strength":        0,
			"stat_physical_attack": 0,
		},
	}

	body := service.buildCurrentSelectedUserInfoMode3Body(context.Background(), nil, nil, character, true, 77)
	stat := body[0x17 : 0x17+92]
	if got := bodyInt16ForUserInfoTest(stat[8:10]); got != 110 {
		t.Fatalf("mode3 strength wire = %d, want base PVF 110 without equipment merge", got)
	}
	if got := body[0x74]; got != 1 {
		t.Fatalf("mode3 equipment create count = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(body[0x76:0x7a]); got != 900001 {
		t.Fatalf("mode3 equipment item id = %d, want 900001", got)
	}
}

func TestBuildCSharpSelectedUserInfoSubtype1WritesEquippedRawEntries(t *testing.T) {
	repos := testRepositoryGroup()
	rawWeapon := buildInitialEquipmentRawEntry(11, 900001, 27)
	rawCoat := buildInitialEquipmentRawEntry(13, 900002, 31)
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"13": {SlotIndex: 13, ItemID: 900002, RawEntry: rawCoat},
			"11": {SlotIndex: 11, ItemID: 900001, RawEntry: rawWeapon},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "15",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
	}

	body := service.buildCSharpSelectedUserInfoBody(context.Background(), nil, nil, 1, character, true, 77, character.Name)
	if got := body[97]; got != 2 {
		t.Fatalf("subtype1 equipped count = %d, want 2", got)
	}
	if got := body[98 : 98+len(rawWeapon)]; !bytesEqualForUserInfoTest(got, rawWeapon) {
		t.Fatalf("first equipped raw = %x, want weapon %x", got, rawWeapon)
	}
	secondStart := 98 + len(rawWeapon)
	if got := body[secondStart : secondStart+len(rawCoat)]; !bytesEqualForUserInfoTest(got, rawCoat) {
		t.Fatalf("second equipped raw = %x, want coat %x", got, rawCoat)
	}
}

func bodyInt16ForUserInfoTest(data []byte) int {
	return int(int16(binary.LittleEndian.Uint16(data)))
}

func bytesEqualForUserInfoTest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx] != b[idx] {
			return false
		}
	}
	return true
}

func readMode1RawBlockForTest(body []byte, pos int) ([]byte, int) {
	length := int(binary.LittleEndian.Uint32(body[pos : pos+4]))
	pos += 4
	return body[pos : pos+length], pos + length
}

type userInfoPVFMemSource map[string]string

func (s userInfoPVFMemSource) ReadText(relativePath string) (string, error) {
	if text, ok := s[cleanInitialPVFPath(relativePath)]; ok {
		return text, nil
	}
	return "", errInitialEquipmentTestMissing(relativePath)
}

func testCharacterStatTable(t *testing.T) *dnfcharstat.Table {
	t.Helper()
	table, err := dnfcharstat.Load(context.Background(), userInfoPVFMemSource{
		"character/character.lst": "11 `female_swordman.chr`\n15 `fighter.chr`\n",
		"character/female_swordman.chr": `
[initial value]
[HP MAX] 120
[MP MAX] 130
[strength] 11
[intelligence] 12
[vitality] 13
[spirit] 14
[physical attack] 7
[physical defense] 8
[magical attack] 6
[magical defense] 9
[independent attack] 5
[fire resistance] 6
[water resistance] 7
[dark resistance] 8
[light resistance] 9
[inventory limit] 48000
[HP regen speed] 0
[MP regen speed] 50
[move speed] 850
[attack speed] 850
[cast speed] 700
[hit recovery] 600
[jump power] 430
[weight] 51000
[growtype 1]
[HP MAX] 1
`,
		"character/fighter.chr": `
[initial value]
[HP MAX] 120
[MP MAX] 130
[strength] 11
[intelligence] 12
[vitality] 13
[spirit] 14
[physical attack] 7
[physical defense] 8
[magical attack] 6
[magical defense] 9
[independent attack] 5
[inventory limit] 48000
[HP regen speed] 0
[MP regen speed] 50
[move speed] 850
[attack speed] 850
[cast speed] 700
[hit recovery] 600
[jump power] 430
[weight] 51000
[growtype 1]
[HP MAX] 1
`,
	}, dnfcharstat.Options{})
	if err != nil {
		t.Fatalf("load test character stats: %v", err)
	}
	return table
}
