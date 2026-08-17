// service_test.go 验证 dnfbridge 对最新 DNF 客户端频道服和 game 端口兼容行为。
package dnfbridge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/channelinfo"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/platform/config"
)

func TestConfigFileLoads(t *testing.T) {
	path := filepath.Join("testdata", "dnfbridge.toml")
	if _, err := config.Load(path, "dnfbridge"); err != nil {
		t.Fatalf("load dnfbridge config: %v", err)
	}
}

func TestBuildCurrentRequestOverseerBodyForSessionUsesInventoryRows(t *testing.T) {
	ctx := context.Background()
	repos := testRepositoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "12",
		Slots: map[string]dnfrepo.ItemStack{
			"100:2": {
				ItemID: 1001,
				Count:  5,
				Extra: map[string]string{
					"packed_flag_byte":          "3",
					"durability":                "27",
					"instance_value":            "0x4D2",
					"unused_byte_after_value_a": "9",
					"value_c":                   "7",
					"value_d":                   "321",
				},
			},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	session := &gameSession{connID: "game-test", selectedCharacterID: 12}

	body := service.buildCurrentRequestOverseerBodyForSession(ctx, session, buildCurrentRequestOverseerBody(0))
	if len(body) != 37 {
		t.Fatalf("op358 body len = %d, want 37 body=%x", len(body), body)
	}
	if body[0] != 0 ||
		binary.LittleEndian.Uint16(body[1:3]) != 0 ||
		binary.LittleEndian.Uint32(body[3:7]) != 0 ||
		binary.LittleEndian.Uint32(body[7:11]) != 1 {
		t.Fatalf("op358 header mismatch: %x", body[:11])
	}
	row := body[11:]
	if binary.LittleEndian.Uint16(row[0:2]) != 2 ||
		binary.LittleEndian.Uint32(row[2:6]) != 1001 ||
		binary.LittleEndian.Uint32(row[6:10]) != 5 ||
		row[10] != 3 ||
		binary.LittleEndian.Uint16(row[11:13]) != 27 ||
		row[13] != 1 ||
		binary.LittleEndian.Uint32(row[14:18]) != 0x4D2 ||
		row[18] != 9 ||
		row[19] != 7 ||
		binary.LittleEndian.Uint16(row[20:22]) != 321 ||
		binary.LittleEndian.Uint32(row[22:26]) != 0 {
		t.Fatalf("op358 row mismatch: %x", row)
	}
}

func TestBuildCurrentRequestOverseerBodyForSessionDoesNotProjectOrdinaryLegacyInventory(t *testing.T) {
	ctx := context.Background()
	repos := testRepositoryGroup()
	repos.LegacyInventory = &fakeLegacyInventoryStore{
		items: map[byte][]dnfrepo.LegacyInventoryItem{
			1: {
				{
					ListType:       1,
					SlotIndex:      4,
					ItemTemplateID: 777,
					StackCount:     1,
					InstanceValue:  0x12345678,
					Durability:     42,
					SealFlag:       2,
				},
			},
		},
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	session := &gameSession{connID: "game-test", selectedCharacterID: 12}

	body := service.buildCurrentRequestOverseerBodyForSession(ctx, session, buildCurrentRequestOverseerBody(1))
	if len(body) != 11 {
		t.Fatalf("legacy op358 body len = %d, want empty 11-byte page body=%x", len(body), body)
	}
	if binary.LittleEndian.Uint32(body[3:7]) != 1 || binary.LittleEndian.Uint32(body[7:11]) != 0 {
		t.Fatalf("legacy op358 header mismatch: %x", body[:11])
	}
}

func TestBuildCurrentRequestOverseerBodyForSessionUsesLegacyAchievementChunks(t *testing.T) {
	ctx := context.Background()
	repos := testRepositoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "12",
		Slots: map[string]dnfrepo.ItemStack{
			"0:2": {ItemID: 9999, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	var entry packetWriter
	entry.writeUint16(6)
	entry.writeUint32(1200401)
	entry.writeUint32(3)
	entry.writeByte(4)
	entry.writeUint16(55)
	entry.writeByte(1)
	entry.writeUint32(0x11223344)
	entry.writeByte(8)
	entry.writeByte(9)
	entry.writeUint16(0x5566)
	repos.LegacyUserInfo = fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_achievement_chunks": {
			{
				"chunk_index":  "0",
				"mode_byte":    "1",
				"owner_id16":   "77",
				"entries_blob": string(entry.bytes()),
			},
		},
	}}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	session := &gameSession{connID: "game-test", selectedCharacterID: 12}

	body := service.buildCurrentRequestOverseerBodyForSession(ctx, session, buildCurrentRequestOverseerBody(0))
	if len(body) != 37 {
		t.Fatalf("achievement op358 body len = %d, want 37 body=%x", len(body), body)
	}
	if body[0] != 1 ||
		binary.LittleEndian.Uint16(body[1:3]) != 77 ||
		binary.LittleEndian.Uint32(body[3:7]) != 0 ||
		binary.LittleEndian.Uint32(body[7:11]) != 1 {
		t.Fatalf("achievement op358 header mismatch: %x", body[:11])
	}
	row := body[11:]
	if binary.LittleEndian.Uint16(row[0:2]) != 6 ||
		binary.LittleEndian.Uint32(row[2:6]) != 1200401 ||
		binary.LittleEndian.Uint32(row[6:10]) != 3 ||
		row[10] != 4 ||
		binary.LittleEndian.Uint16(row[11:13]) != 55 ||
		row[13] != 1 ||
		binary.LittleEndian.Uint32(row[14:18]) != 0x11223344 ||
		row[18] != 8 ||
		row[19] != 9 ||
		binary.LittleEndian.Uint16(row[20:22]) != 0x5566 ||
		binary.LittleEndian.Uint32(row[22:26]) != 0 {
		t.Fatalf("achievement op358 row mismatch: %x", row)
	}
}

func TestBuildCurrentInsertOverseerBodyForSessionUsesLegacyAchievementComplete(t *testing.T) {
	ctx := context.Background()
	repos := testRepositoryGroup()
	repos.LegacyUserInfo = fakeLegacyUserInfoRepo{rows: map[string][]dnfrepo.LegacyUserInfoRow{
		"legacy_character_achievement_complete": {
			{
				"sort_order":     "0",
				"achievement_id": "5001",
				"p1":             "11",
				"p2":             "22",
				"p3":             "33",
				"p4":             "44",
			},
		},
	}}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	session := &gameSession{connID: "game-test", selectedCharacterID: 12}

	body := service.buildCurrentInsertOverseerBodyForSession(ctx, session, buildCurrentInsertOverseerBody())
	if len(body) != 30 {
		t.Fatalf("op359 body len = %d, want 30 body=%x", len(body), body)
	}
	if binary.LittleEndian.Uint32(body[:4]) != 1 {
		t.Fatalf("op359 count mismatch: %x", body[:4])
	}
	row := body[4 : 4+currentInsertOverseerRowWireSize]
	if binary.LittleEndian.Uint32(row[0:4]) != 5001 ||
		binary.LittleEndian.Uint16(row[4:6]) != 11 ||
		binary.LittleEndian.Uint16(row[6:8]) != 22 ||
		binary.LittleEndian.Uint16(row[8:10]) != 33 {
		t.Fatalf("op359 row mismatch: %x", row)
	}
	if tail := body[4+currentInsertOverseerRowWireSize:]; len(tail) != currentInsertOverseerTailWireSize ||
		!bytes.Equal(tail, make([]byte, currentInsertOverseerTailWireSize)) {
		t.Fatalf("op359 fixed tail mismatch: %x", tail)
	}
}

func TestBuildCurrentInsertOverseerEmptyBodyCarriesFixedTail(t *testing.T) {
	body := buildCurrentInsertOverseerBody()
	if len(body) != 4+currentInsertOverseerTailWireSize ||
		binary.LittleEndian.Uint32(body[:4]) != 0 ||
		!bytes.Equal(body[4:], make([]byte, currentInsertOverseerTailWireSize)) {
		t.Fatalf("empty op359 body=%x, want count=0 plus raw[16]", body)
	}
}

func TestBuildCurrentItemListBodyForSessionUsesExe119ByteRows(t *testing.T) {
	ctx := context.Background()
	repos := testRepositoryGroup()
	rawEntry := make([]byte, currentItemListEntryWireSize)
	rawEntry[0x60] = 0xA5
	binary.LittleEndian.PutUint32(rawEntry[legacyWrongCurrentItemListExpireTimeOffset:legacyWrongCurrentItemListExpireTimeOffset+4], 123456)
	binary.LittleEndian.PutUint32(rawEntry[0x6E:0x72], 123456)
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "12",
		Slots: map[string]dnfrepo.ItemStack{
			"0:2": {
				ItemID:   1001,
				Count:    5,
				RawEntry: rawEntry,
				Extra: map[string]string{
					"packed_flag_byte": "3",
					"durability":       "27",
					"bind_flag":        "1",
					"instance_value":   "0x4D2",
					"value_c":          "7",
					"value_d":          "321",
					"value_b":          "777",
					"expire_time":      "123456",
				},
			},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	session := &gameSession{connID: "game-test", selectedCharacterID: 12}

	body, source, count, ok := service.buildCurrentItemListBodyForSession(ctx, session, 0)
	if !ok || source != "inventory" || count != 1 {
		t.Fatalf("current item list ok=%v source=%q count=%d", ok, source, count)
	}
	if len(body) != 5+currentItemListEntryWireSize {
		t.Fatalf("op13 body len = %d, want %d body=%x", len(body), 5+currentItemListEntryWireSize, body)
	}
	if body[0] != 0 || binary.LittleEndian.Uint16(body[1:3]) != currentExeInitialMainSlotCount || binary.LittleEndian.Uint16(body[3:5]) != 1 {
		t.Fatalf("op13 header mismatch: %x", body[:5])
	}
	entry := body[5:]
	if binary.LittleEndian.Uint16(entry[0x00:0x02]) != 2 ||
		binary.LittleEndian.Uint32(entry[0x02:0x06]) != 1001 ||
		binary.LittleEndian.Uint32(entry[0x06:0x0A]) != 5 ||
		entry[0x0A] != 3 ||
		binary.LittleEndian.Uint16(entry[0x0B:0x0D]) != 27 ||
		entry[0x0D] != 1 ||
		binary.LittleEndian.Uint32(entry[0x0E:0x12]) != 0x4D2 ||
		entry[0x13] != 7 ||
		binary.LittleEndian.Uint16(entry[0x14:0x16]) != 321 ||
		binary.LittleEndian.Uint32(entry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != 123456 ||
		binary.LittleEndian.Uint32(entry[legacyWrongCurrentItemListExpireTimeOffset:legacyWrongCurrentItemListExpireTimeOffset+4]) != 0 ||
		entry[0x60] != 0xA5 ||
		binary.LittleEndian.Uint32(entry[0x6E:0x72]) != 123456 {
		t.Fatalf("op13 entry mismatch: %x", entry)
	}
}

func TestBuildCurrentItemListBodyForSessionUsesPersistedCurrentExeContainerState(t *testing.T) {
	ctx := context.Background()
	repos := testRepositoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "27",
		Slots:       map[string]dnfrepo.ItemStack{},
		Warehouse:   map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repos.Settings.Save(ctx, dnfrepo.SettingsRecord{
		Scope: dnfrepo.CharacterContainerStateScope("27"),
		Values: map[string]string{
			"source":                      "current_exe_86jp_op13_container_state",
			"main_list_param16":           "24",
			"avatar_list_param16":         "0",
			"personal_cargo_list_param16": "8",
			"account_cargo_selection_key": "3",
			"account_cargo_value32":       "4660",
		},
	}); err != nil {
		t.Fatalf("save container state: %v", err)
	}
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "dnf:27",
		Metadata: map[string]string{
			"account_cargo_created": "true",
			"account_cargo_level":   "3",
			"account_cargo_gold":    "4660",
		},
	}); err != nil {
		t.Fatalf("save account cargo state: %v", err)
	}
	service := &Service{
		options: options{accountID: "dnf:27"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	session := &gameSession{connID: "game-test", selectedCharacterID: 27}

	tests := []struct {
		listType byte
		want     []byte
	}{
		{listType: 0, want: []byte{0, 24, 0, 0, 0}},
		{listType: 1, want: []byte{1, 0, 0, 0, 0}},
		{listType: 2, want: []byte{2, 8, 0, 0, 0, 0}},
		{listType: 12, want: []byte{12, 3, 0, 0x34, 0x12, 0, 0, 0, 0}},
	}
	for _, test := range tests {
		body, source, count, ok := service.buildCurrentItemListBodyForSession(ctx, session, test.listType)
		wantSource := "inventory"
		if test.listType == 12 {
			wantSource = "account_metadata+account_inventory_absent_empty"
		}
		if !ok || source != wantSource || count != 0 {
			t.Fatalf("list=%d ok=%v source=%q count=%d", test.listType, ok, source, count)
		}
		if !bytes.Equal(body, test.want) {
			t.Fatalf("list=%d body=%x want=%x", test.listType, body, test.want)
		}
	}
}

func TestBuildCurrentItemListBodyForSessionDoesNotMirrorEquipmentIntoListType1(t *testing.T) {
	ctx := context.Background()
	repos := testRepositoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "12", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "12",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    900001,
				Bind:      true,
				Extra: map[string]string{
					"durability":                 "45",
					"instance_value":             "999999998",
					"value_c":                    "7",
					"current_exe_equipment_type": "12",
					"current_exe_runtime_move":   "1",
				},
			},
			"13": {
				SlotIndex: 13,
				ItemID:    900002,
				Extra: map[string]string{
					"durability":                 "44",
					"current_exe_equipment_type": "14",
					"current_exe_runtime_move":   "1",
				},
			},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	session := &gameSession{connID: "game-test", selectedCharacterID: 12}

	inventoryBody, inventorySource, inventoryCount, ok := service.buildCurrentItemListBodyForSession(ctx, session, 0)
	if !ok || inventorySource != "inventory" || inventoryCount != 0 {
		t.Fatalf("inventory list ok=%v source=%q count=%d", ok, inventorySource, inventoryCount)
	}
	if len(inventoryBody) != 5 || inventoryBody[0] != 0 || binary.LittleEndian.Uint16(inventoryBody[3:5]) != 0 {
		t.Fatalf("inventory list should remain empty, body=%x", inventoryBody)
	}

	body, source, count, ok := service.buildCurrentItemListBodyForSession(ctx, session, 1)
	if !ok || source != "inventory" || count != 0 {
		t.Fatalf("list type 1 ok=%v source=%q count=%d", ok, source, count)
	}
	if len(body) != 5 || body[0] != 1 || binary.LittleEndian.Uint16(body[1:3]) != 0 || binary.LittleEndian.Uint16(body[3:5]) != 0 {
		t.Fatalf("list type 1 should stay empty instead of receiving equipped rows, body=%x", body)
	}

	equipmentBody, equipmentSource, equipmentCount, ok := service.buildCurrentItemListBodyForSession(ctx, session, 3)
	if !ok || equipmentSource != "equipment" || equipmentCount != 2 {
		t.Fatalf("list type 3 ok=%v source=%q count=%d", ok, equipmentSource, equipmentCount)
	}
	if len(equipmentBody) != 3+2*currentItemListEntryWireSize || equipmentBody[0] != 3 || binary.LittleEndian.Uint16(equipmentBody[1:3]) != 2 {
		t.Fatalf("list type 3 equipped body mismatch len=%d body=%x", len(equipmentBody), equipmentBody)
	}
	firstEquipped := equipmentBody[3 : 3+currentItemListEntryWireSize]
	if binary.LittleEndian.Uint16(firstEquipped[0x00:0x02]) != 12 ||
		binary.LittleEndian.Uint32(firstEquipped[0x02:0x06]) != 900001 ||
		binary.LittleEndian.Uint32(firstEquipped[0x06:0x0A]) != 999999998 ||
		binary.LittleEndian.Uint16(firstEquipped[0x0B:0x0D]) != 45 ||
		firstEquipped[0x0D] != 1 ||
		binary.LittleEndian.Uint32(firstEquipped[0x0E:0x12]) != 999999998 ||
		firstEquipped[0x13] != 7 {
		t.Fatalf("list type 3 first equipped row mismatch: %x", firstEquipped)
	}
}

func TestBuildCurrentEquipmentItemUpdateBodyForSessionUsesEquippedRows(t *testing.T) {
	ctx := context.Background()
	rawWeapon := buildInitialEquipmentRawEntry(11, 900001, 45)
	rawWeapon[16] = 0x34
	rawWeapon[17] = 0x12
	rawWeapon[20] = 2
	rawWeapon[21] = 3
	rawWeapon[22] = 0x78
	rawWeapon[23] = 0x56
	repos := testRepositoryGroup()
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "12",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    900001,
				Bind:      true,
				RawEntry:  rawWeapon,
				Extra: map[string]string{
					"durability":     "45",
					"instance_value": "999999998",
					"expire_time":    "1849989600",
				},
			},
			"13": {
				SlotIndex: 13,
				ItemID:    900002,
				Extra: map[string]string{
					"durability":     "44",
					"instance_value": "999999997",
				},
			},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	session := &gameSession{connID: "game-test", selectedCharacterID: 12}

	body, source, count, ok := service.buildCurrentEquipmentItemUpdateBodyForSession(ctx, session)
	if !ok || source != "equipment" || count != 2 {
		t.Fatalf("equipment update ok=%v source=%q count=%d", ok, source, count)
	}
	if len(body) != 3+2*currentEquipmentUpdateEntryWireSize || body[0] != 3 || binary.LittleEndian.Uint16(body[1:3]) != 2 {
		t.Fatalf("equipment update body mismatch len=%d body=%x", len(body), body)
	}
	firstEquipped := body[3 : 3+currentEquipmentUpdateEntryWireSize]
	if binary.LittleEndian.Uint16(firstEquipped[0x00:0x02]) != 11 ||
		binary.LittleEndian.Uint32(firstEquipped[0x02:0x06]) != 900001 ||
		binary.LittleEndian.Uint32(firstEquipped[0x06:0x0A]) != 999999998 ||
		binary.LittleEndian.Uint16(firstEquipped[0x0B:0x0D]) != 45 ||
		firstEquipped[0x0D] != 0 ||
		binary.LittleEndian.Uint32(firstEquipped[0x16:0x1A]) != 0xFFFFFFFF ||
		binary.LittleEndian.Uint32(firstEquipped[currentEquipmentUpdateExpireTimeOffset:currentEquipmentUpdateExpireTimeOffset+4]) != 1849989600 {
		t.Fatalf("equipment update first row mismatch: %x", firstEquipped)
	}
	if got := firstEquipped[0x0E:0x16]; !bytes.Equal(got, []byte{0x34, 0x12, 0, 0, 2, 3, 0x78, 0x56}) {
		t.Fatalf("equipment update prefix = %x", got)
	}
}

func TestBuildCurrentPetItemListBodyHasNoSelectorPrefix(t *testing.T) {
	ctx := context.Background()
	repos := testRepositoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "12",
		Slots: map[string]dnfrepo.ItemStack{
			"7:5": {ItemID: 9001, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "37"}},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	session := &gameSession{connID: "game-test", selectedCharacterID: 12}

	body, source, count, ok := service.buildCurrentItemListBodyForSession(ctx, session, 7)
	if !ok || source != "inventory" || count != 1 {
		t.Fatalf("pet item list ok=%v source=%q count=%d", ok, source, count)
	}
	if len(body) != 3+currentItemListEntryWireSize {
		t.Fatalf("pet op13 body len = %d, want %d body=%x", len(body), 3+currentItemListEntryWireSize, body)
	}
	if body[0] != 7 || binary.LittleEndian.Uint16(body[1:3]) != 1 {
		t.Fatalf("pet op13 header mismatch: %x", body[:3])
	}
	entry := body[3:]
	if binary.LittleEndian.Uint16(entry[0x00:0x02]) != 5 ||
		binary.LittleEndian.Uint32(entry[0x02:0x06]) != 9001 ||
		binary.LittleEndian.Uint32(entry[0x06:0x0A]) != 37 {
		t.Fatalf("pet op13 entry mismatch: %x", entry[:0x0A])
	}
}

func TestPacketLoggerIncludesConnectionFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packet_log.txt")
	logger, err := openPacketLogger(path)
	if err != nil {
		t.Fatalf("open packet logger: %v", err)
	}
	logger.Log("SEND", "game", []byte{0x01, 0x02, 0x03}, "conn_id", "game-000001", "pkt_seq", 2, "cmd", 1)
	if err := logger.Close(); err != nil {
		t.Fatalf("close packet logger: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read packet log: %v", err)
	}
	line := string(data)
	for _, want := range []string{
		"SEND kind=game",
		"conn_id=game-000001",
		"pkt_seq=2",
		"cmd=1",
		"len=3",
		"hex=010203",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("packet log missing %q in:\n%s", want, line)
		}
	}
}

func TestBundledChannelInfoMatchesCompleteVersionChannelSet(t *testing.T) {
	path := filepath.Join("testdata", "channel_info.etc")
	index, err := channelinfo.LoadFile(path)
	if err != nil {
		t.Fatalf("load bundled channel info: %v", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{ServerID: 1})
	if err != nil {
		t.Fatalf("build server 1 catalog: %v", err)
	}
	if channel, ok := catalog.Channel(dnfenum.DefaultAutoChannelID); ok {
		t.Fatalf("server 1 unexpectedly contains legacy auto channel: %+v", channel)
	}
	if got := len(catalog.Channels()); got != 118 {
		t.Fatalf("complete channel count = %d, want 118", got)
	}
	ordinary, ok := catalog.Channel(201)
	if !ok || ordinary.Type != 11 || ordinary.Group != "metro" {
		t.Fatalf("channel 201 = %+v found=%t, want source type 11 metro", ordinary, ok)
	}
	hiddenRaid, ok := catalog.Channel(241)
	if !ok || hiddenRaid.Type != dnfenum.HiddenRaidType || hiddenRaid.Group != "luke_raid" {
		t.Fatalf("channel 241 = %+v found=%t, want source type %d luke_raid", hiddenRaid, ok, dnfenum.HiddenRaidType)
	}
}

func TestChannelForPortMatchesExact90CNCatalogAndRejectsUnknownPorts(t *testing.T) {
	const fixture = `
	[dungeon]
	` + "`[trade]` `交易频道` 1" + `
	[/dungeon]
	[server]
	1 1 ` + "`洛兰`" + ` 2 ` + "`[trade]`" + ` 0 0 38 ` + "`自动频道`" + ` 3 ` + "`[trade]`" + ` 0 0
	[/server]
	`
	index, err := channelinfo.Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse channel info: %v", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{ServerID: 1})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	service := &Service{options: options{channelServerID: 1}, catalog: catalog}

	cases := []struct {
		name      string
		port      int
		wantFound bool
		wantID    int
		wantType  uint8
		wantName  string
		wantPort  int
		wantGroup string
	}{
		{name: "catalog channel from local game port", port: dnfenum.GamePortBase + 38, wantFound: true, wantID: 38, wantType: 3, wantName: "ch.38", wantPort: dnfenum.GamePortBase + 38, wantGroup: "trade"},
		{name: "unknown game port", port: dnfenum.GamePortBase + 99, wantFound: false},
		{name: "non game port", port: 0, wantFound: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channel, found := service.channelForPort(tc.port)
			if found != tc.wantFound {
				t.Fatalf("channelForPort(%d) found=%t want=%t channel=%+v", tc.port, found, tc.wantFound, channel)
			}
			if !found {
				return
			}
			if channel.ID != tc.wantID || channel.Type != tc.wantType || channel.Name != tc.wantName || channel.NoticeName != tc.wantName || channel.Port != tc.wantPort || channel.Group != tc.wantGroup {
				t.Fatalf("channelForPort(%d) = %+v", tc.port, channel)
			}
		})
	}
}

func TestOptionsPreserveExplicitZeroAdvertiseServerIndex(t *testing.T) {
	t.Setenv("DNFBRIDGE_CHANNEL_SERVER_INDEX", "1")
	t.Setenv("DNF_CHANNEL_SOURCE_SERVER_INDEX", "")
	t.Setenv("DNFBRIDGE_CHANNEL_ADVERTISE_SERVER_INDEX", "0")
	t.Setenv("DFO_CHANNEL_SERVER_INDEX", "")

	opts := optionsFromConfig(config.ServiceConfig{})
	service := &Service{options: opts}
	if got := service.channelServerID(); got != 1 {
		t.Fatalf("channelServerID() = %d, want 1", got)
	}
	if got := service.channelAdvertiseID(); got != 0 {
		t.Fatalf("channelAdvertiseID() = %d, want online server 0", got)
	}
}

func TestOptionsFallBackToSourceAdvertiseIndexWhenUnset(t *testing.T) {
	t.Setenv("DNFBRIDGE_CHANNEL_SERVER_INDEX", "1")
	t.Setenv("DNF_CHANNEL_SOURCE_SERVER_INDEX", "")
	t.Setenv("DNFBRIDGE_CHANNEL_ADVERTISE_SERVER_INDEX", "")
	t.Setenv("DFO_CHANNEL_SERVER_INDEX", "")

	opts := optionsFromConfig(config.ServiceConfig{})
	service := &Service{options: opts}
	if got := service.channelAdvertiseID(); got != 1 {
		t.Fatalf("channelAdvertiseID() = %d, want source server 1", got)
	}
}

func TestOptionsIgnoreLegacyCSharpChannelInfoBodyMode(t *testing.T) {
	t.Setenv("DNFBRIDGE_CHANNEL_INFO_BODY_MODE", "legacy_csharp")

	opts := optionsFromConfig(config.ServiceConfig{})
	service := &Service{options: opts}
	if got := service.channelInfoBodyMode(); got != channelInfoBodyLatest {
		t.Fatalf("channelInfoBodyMode() = %q, want %q", got, channelInfoBodyLatest)
	}
}

func TestInitialModeNotice(t *testing.T) {
	for _, raw := range []string{"", "upper", "msg1_endpoint", "login", "wait_client", "none"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("DNFBRIDGE_GAME_INITIAL_MODE", raw)

			opts := optionsFromConfig(config.ServiceConfig{})
			if opts.gameInitialMode != gameInitialModeNotice {
				t.Fatalf("gameInitialMode = %q, want %q", opts.gameInitialMode, gameInitialModeNotice)
			}
		})
	}
}

func TestPostBootstrapMode(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "client check alias disabled", raw: "upper_client_check", want: gamePostBootstrapNone},
		{name: "roster init alias disabled", raw: "character_roster", want: gamePostBootstrapNone},
		{name: "upper roster alias disabled", raw: "upper_roster", want: gamePostBootstrapNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DNFBRIDGE_GAME_POST_BOOTSTRAP", tc.raw)

			opts := optionsFromConfig(config.ServiceConfig{})
			if opts.gamePostBootstrap != tc.want {
				t.Fatalf("gamePostBootstrap = %q, want %q", opts.gamePostBootstrap, tc.want)
			}
		})
	}
}

func TestOptionsNormalizeGameUpperHeader(t *testing.T) {
	t.Setenv("DNFBRIDGE_GAME_UPPER_HEADER", "16")

	opts := optionsFromConfig(config.ServiceConfig{})
	if opts.gameUpperHeader != gameUpperHeaderServer16 {
		t.Fatalf("gameUpperHeader = %q, want %q", opts.gameUpperHeader, gameUpperHeaderServer16)
	}
}

func TestOptionsNormalizeGameUpperBodyCodec(t *testing.T) {
	t.Setenv("DNFBRIDGE_GAME_UPPER_BODY_CODEC", "plain")

	opts := optionsFromConfig(config.ServiceConfig{})
	if opts.gameUpperBodyCodec != gameUpperBodyCodecPlain {
		t.Fatalf("gameUpperBodyCodec = %q, want %q", opts.gameUpperBodyCodec, gameUpperBodyCodecPlain)
	}
}

func TestOptionsParseGameOuterTokenAliasHex(t *testing.T) {
	t.Setenv("DNFBRIDGE_GAME_OUTER_TOKEN", "de509f65e9ccaae621cb7278fc2b8e6c")
	t.Setenv("DFO_GAME_OUTER_TOKEN", "")

	opts := optionsFromConfig(config.ServiceConfig{})
	if opts.gameOuterToken != 0xde509f65 {
		t.Fatalf("gameOuterToken = %#x, want %#x", opts.gameOuterToken, uint32(0xde509f65))
	}
}

func TestBuildChannelInfoBodyEncryptsLatestCatalog(t *testing.T) {
	service := &Service{options: options{serverIP: "127.0.0.1", scriptVersion: "59"}}
	body, err := service.buildScriptVersionBody()
	if err != nil {
		t.Fatalf("build script version body: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected encrypted script version")
	}
}

func TestBuildChannelInfoBodyUsesLatestHeader(t *testing.T) {
	const fixture = `
[dungeon]
` + "`[elven_guard]` `艾尔文防线` 1" + `
[/dungeon]
[server]
1 11 ` + "`普通频道`" + ` 1 ` + "`[elven_guard]`" + ` 0 0 38 ` + "`推荐频道`" + ` 1 ` + "`[elven_guard]`" + ` 0 0
[/server]
`
	index, err := channelinfo.Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse channel info: %v", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{ServerID: 1})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	service := &Service{
		options: options{
			serverIP:           "42.240.165.245",
			channelServerID:    1,
			channelAdvertiseID: 0,
		},
		catalog: catalog,
	}

	encrypted, err := service.buildChannelInfoBody(nil)
	if err != nil {
		t.Fatalf("build channel info body: %v", err)
	}
	raw, err := decryptCompressedChannelData(encrypted, service.aesKey())
	if err != nil {
		t.Fatalf("decrypt channel info body: %v", err)
	}
	if len(raw) < 12 {
		t.Fatalf("raw body too short: %d", len(raw))
	}
	if got := binary.LittleEndian.Uint32(raw[0:4]); got != 1 {
		t.Fatalf("header[0] = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(raw[4:8]); got != 0 {
		t.Fatalf("server index = %d, want online server 0", got)
	}
	assertChannelInfoHasEntries(t, raw, 2,
		channelEntryWant{name: "#ch.11", port: dnfenum.GamePortBase + 11},
		channelEntryWant{name: "#ch.38", port: dnfenum.GamePortBase + 38},
	)
}

func TestBuildChannelInfoBodyIgnoresCSharpPrefix(t *testing.T) {
	catalog := testChannelCatalog(t)
	service := &Service{
		options: options{
			serverIP:            "42.240.165.245",
			channelServerID:     1,
			channelAdvertiseID:  0,
			channelInfoBodyMode: "legacy_csharp",
		},
		catalog: catalog,
	}

	encrypted, err := service.buildChannelInfoBody([]byte("cain"))
	if err != nil {
		t.Fatalf("build channel info body: %v", err)
	}
	raw, err := decryptCompressedChannelData(encrypted, service.aesKey())
	if err != nil {
		t.Fatalf("decrypt channel info body: %v", err)
	}
	if len(raw) < 12 {
		t.Fatalf("raw body too short: %d", len(raw))
	}
	if got := binary.LittleEndian.Uint32(raw[0:4]); got != 1 {
		t.Fatalf("header[0] = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(raw[4:8]); got != 0 {
		t.Fatalf("server index = %d, want online server 0", got)
	}
	assertChannelInfoHasEntries(t, raw, 2,
		channelEntryWant{name: "#ch.11", port: dnfenum.GamePortBase + 11},
		channelEntryWant{name: "#ch.38", port: dnfenum.GamePortBase + 38},
	)
}

func TestBuildChannelInfoBodyUsesCatalogOnly(t *testing.T) {
	catalog := testCatalogWithoutAutoChannel(t)
	service := &Service{
		options: options{
			serverIP:           "42.240.165.245",
			channelServerID:    1,
			channelAdvertiseID: 0,
		},
		catalog: catalog,
	}

	encrypted, err := service.buildChannelInfoBody([]byte("cain"))
	if err != nil {
		t.Fatalf("build channel info body: %v", err)
	}
	raw, err := decryptCompressedChannelData(encrypted, service.aesKey())
	if err != nil {
		t.Fatalf("decrypt channel info body: %v", err)
	}
	assertChannelInfoHasEntries(t, raw, 2,
		channelEntryWant{name: "#ch.1", port: dnfenum.GamePortBase + 1},
		channelEntryWant{name: "#ch.11", port: dnfenum.GamePortBase + 11},
	)
}

func TestBootstrapAdsSkipTowerTrade(t *testing.T) {
	catalog := testTowerCatalog(t)
	service := &Service{
		options: options{
			serverIP:        "42.240.165.245",
			channelServerID: 1,
		},
		catalog: catalog,
	}

	encrypted, err := service.buildChannelInfoBodyFor([]byte("cain"), true)
	if err != nil {
		t.Fatalf("build channel info body: %v", err)
	}
	raw, err := decryptCompressedChannelData(encrypted, service.aesKey())
	if err != nil {
		t.Fatalf("decrypt channel info body: %v", err)
	}
	count := int(binary.LittleEndian.Uint32(raw[8:12]))
	if count != 2 {
		t.Fatalf("channel count = %d, want 2", count)
	}
	if hasChannelEntryFrom(raw, 12, count, "#ch.1", dnfenum.GamePortBase+1) {
		t.Fatalf("default ads must not include death tower channel: %x", raw)
	}
	if hasChannelEntryFrom(raw, 12, count, "#ch.6", dnfenum.GamePortBase+6) ||
		hasChannelEntryFrom(raw, 12, count, "#ch.38", dnfenum.GamePortBase+38) {
		t.Fatalf("default ads must not include trade channel: %x", raw)
	}
	name, port := channelEntryNamePort(raw, 12)
	if name != "#ch.10" || port != dnfenum.GamePortBase+10 {
		t.Fatalf("first channel = %s/%d, want #ch.10/%d", name, port, dnfenum.GamePortBase+10)
	}
	assertChannelInfoHasEntries(t, raw, 2,
		channelEntryWant{name: "#ch.10", port: dnfenum.GamePortBase + 10},
		channelEntryWant{name: "#ch.11", port: dnfenum.GamePortBase + 11},
	)
}

func TestRefreshAdsKeepTowerTrade(t *testing.T) {
	catalog := testTowerCatalog(t)
	service := &Service{
		options: options{
			serverIP:        "42.240.165.245",
			channelServerID: 1,
		},
		catalog: catalog,
	}

	encrypted, err := service.buildChannelInfoBody([]byte("cain"))
	if err != nil {
		t.Fatalf("build channel info body: %v", err)
	}
	raw, err := decryptCompressedChannelData(encrypted, service.aesKey())
	if err != nil {
		t.Fatalf("decrypt channel info body: %v", err)
	}
	assertChannelInfoHasEntries(t, raw, 5,
		channelEntryWant{name: "#ch.1", port: dnfenum.GamePortBase + 1},
		channelEntryWant{name: "#ch.6", port: dnfenum.GamePortBase + 6},
		channelEntryWant{name: "#ch.38", port: dnfenum.GamePortBase + 38},
		channelEntryWant{name: "#ch.10", port: dnfenum.GamePortBase + 10},
		channelEntryWant{name: "#ch.11", port: dnfenum.GamePortBase + 11},
	)
}

func TestBundledDefaultAdsKeepCrackBootstrapChannel(t *testing.T) {
	path := filepath.Join("testdata", "channel_info.etc")
	index, err := channelinfo.LoadFile(path)
	if err != nil {
		t.Fatalf("load bundled channel info: %v", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{ServerID: 1})
	if err != nil {
		t.Fatalf("build bundled catalog: %v", err)
	}
	service := &Service{
		options: options{serverIP: "42.240.165.245", channelServerID: 1},
		catalog: catalog,
	}

	channels := service.channelsForRequestMode(dnfenum.GroupCain, true)
	if len(channels) == 0 {
		t.Fatal("default channel advertisement is empty")
	}
	found := false
	for _, channel := range channels {
		if !strings.EqualFold(strings.TrimSpace(channel.Group), dnfenum.GroupCrack) {
			continue
		}
		found = true
		if channel.Port <= 0 || strings.TrimSpace(channel.Name) == "" {
			t.Fatalf("crack bootstrap channel is incomplete: %+v", channel)
		}
	}
	if !found {
		t.Fatal("default channel advertisement is missing the current EXE crack bootstrap entry")
	}
}

func TestBuildChannelInfoBodyPreservesCatalogOrder(t *testing.T) {
	catalog := testChannelCatalog(t)
	service := &Service{
		options: options{
			serverIP:        "42.240.165.245",
			channelServerID: 1,
		},
		catalog: catalog,
	}

	encrypted, err := service.buildChannelInfoBody([]byte("cain"))
	if err != nil {
		t.Fatalf("build channel info body: %v", err)
	}
	raw, err := decryptCompressedChannelData(encrypted, service.aesKey())
	if err != nil {
		t.Fatalf("decrypt channel info body: %v", err)
	}
	name, port := channelEntryNamePort(raw, 12)
	if name != "#ch.11" || port != dnfenum.GamePortBase+11 {
		t.Fatalf("first channel = %s/%d, want #ch.11/%d", name, port, dnfenum.GamePortBase+11)
	}
}

func TestBuildNoticeBody(t *testing.T) {
	service := &Service{options: options{
		channelServerID:    1,
		channelAdvertiseID: 0,
		serverIP:           "42.240.165.245",
		initialUDPPort1:    defaultInitialUDPPort1,
		initialUDPPort2:    defaultInitialUDPPort2,
		commandCount:       defaultCommandCount,
		notificationCount:  defaultNotificationCount,
	}}
	channel := channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", NoticeName: "ch.2", Port: 10038}
	body := service.buildInitialLoginNotice(channel)
	if got, want := len(body), 65; got != want {
		t.Fatalf("initial notice body len = %d, want %d", got, want)
	}
	if body[0] != 1 {
		t.Fatalf("initial notice marker = %d, want 1", body[0])
	}
	offset := 1
	nameLen := int(binary.LittleEndian.Uint32(body[offset : offset+4]))
	offset += 4
	if got := string(body[offset : offset+nameLen]); got != "ch.38" {
		t.Fatalf("channel name = %q, want ch.38", got)
	}
	offset += nameLen
	for _, want := range []uint32{0, 0} {
		got := binary.LittleEndian.Uint32(body[offset : offset+4])
		if got != want {
			t.Fatalf("reserved u32 at %d = %d, want %d", offset, got, want)
		}
		offset += 4
	}
	if got := body[offset]; got != byte(service.channelAdvertiseID()) {
		t.Fatalf("advertise id = %d, want %d", got, service.channelAdvertiseID())
	}
	offset++
	if got := body[offset]; got != 38 {
		t.Fatalf("channel id = %d, want 38", got)
	}
	offset++
	if got := body[offset]; got != 0 {
		t.Fatalf("reserved byte = %d, want 0", got)
	}
	offset++
	offset += 4 // 时间戳是运行期字段，只校验后续固定形态。
	if got := binary.LittleEndian.Uint32(body[offset : offset+4]); got != 1 {
		t.Fatalf("post-time flag = %d, want 1", got)
	}
	offset += 4
	ipLen := int(binary.LittleEndian.Uint32(body[offset : offset+4]))
	offset += 4
	if got := string(body[offset : offset+ipLen]); got != "42.240.165.245" {
		t.Fatalf("server ip = %q, want 42.240.165.245", got)
	}
	offset += ipLen
	for _, want := range []uint32{defaultInitialUDPPort1, defaultInitialUDPPort2} {
		if offset+4 > len(body) {
			t.Fatalf("initial notice tail ended before %d", want)
		}
		got := binary.LittleEndian.Uint32(body[offset : offset+4])
		if got != want {
			t.Fatalf("tail u32 at %d = %d, want %d", offset, got, want)
		}
		offset += 4
	}
	if body[offset] != 0 || body[offset+1] != 0 {
		t.Fatalf("tail flags = %02x %02x, want 00 00", body[offset], body[offset+1])
	}
	offset += 2
	for _, want := range []uint32{uint32(defaultCommandCount), uint32(defaultNotificationCount)} {
		if offset+4 > len(body) {
			t.Fatalf("initial notice tail ended before %d", want)
		}
		got := binary.LittleEndian.Uint32(body[offset : offset+4])
		if got != want {
			t.Fatalf("tail u32 at %d = %d, want %d", offset, got, want)
		}
		offset += 4
	}
	if offset != len(body) {
		t.Fatalf("initial notice parse ended at %d, len %d", offset, len(body))
	}
	frame, err := dnfproto.BuildLatestGameTCP(0, 1, body, dnfproto.TransportOptions{})
	if err != nil {
		t.Fatalf("build latest game tcp: %v", err)
	}
	records, err := dnfproto.ParseLatestGameTCPRecords(frame)
	if err != nil {
		t.Fatalf("parse latest game tcp: %v", err)
	}
	if len(records) != 1 || records[0].GameHeader.Cmd != 0 || records[0].GameHeader.Type != 1 {
		t.Fatalf("unexpected record: %+v", records)
	}
	if !bytes.Equal(records[0].Body, body) {
		t.Fatal("body mismatch")
	}
}

func TestSendGameInitial(t *testing.T) {
	service := &Service{options: options{
		serverIP:        "42.240.165.245",
		gameInitialMode: gameInitialModeUpper,
	}}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channel}

	mode, bodyLen, sent, err := service.sendGameInitial(session)
	if err != nil {
		t.Fatalf("send initial: %v", err)
	}
	if mode != gameInitialModeUpper || !sent || bodyLen != len(service.buildLoginSuccess(channel)) {
		t.Fatalf("mode=%q sent=%v bodyLen=%d", mode, sent, bodyLen)
	}
	assertUpperInitialWire(t, conn.write.Bytes(), service.gameUpperHeaderSize(), upperSuccessBody(service.buildLoginSuccess(channel)), true)
}

func TestBuildLoginSuccessMatchesCurrentExeMsg1Layout(t *testing.T) {
	service := &Service{options: options{
		serverIP:       "42.240.165.245",
		gameOuterToken: 0x11223344,
	}}
	channel := channelcatalog.Channel{ID: 19, Type: 22, Name: "ch.19", Port: 10019}

	body := service.buildLoginSuccess(channel)
	if len(body) < 25 {
		t.Fatalf("body len = %d, want at least 25", len(body))
	}
	if body[0] != 1 || body[1] != 1 {
		t.Fatalf("msg1 leading flags = %x, want 01 01", body[:2])
	}
	if body[2] != channel.Type {
		t.Fatalf("msg1 channel type byte = %d, want %d", body[2], channel.Type)
	}
	if body[3] != 1 {
		t.Fatalf("msg1 entry flag = %d, want 1", body[3])
	}
	if got := binary.LittleEndian.Uint32(body[4:8]); got != service.options.gameOuterToken {
		t.Fatalf("outer token = 0x%08x, want 0x%08x", got, service.options.gameOuterToken)
	}
	ipLen := int(binary.LittleEndian.Uint32(body[8:12]))
	if ipLen != len(service.options.serverIP) {
		t.Fatalf("ip len = %d, want %d", ipLen, len(service.options.serverIP))
	}
	portOffset := 12 + ipLen
	if got := binary.LittleEndian.Uint32(body[portOffset : portOffset+4]); got != uint32(channel.Port) {
		t.Fatalf("port = %d, want %d", got, channel.Port)
	}
	tailOffset := portOffset + 8
	if len(body) != tailOffset+24 {
		t.Fatalf("msg1 body len = %d, want %d-byte account state tail", len(body), 24)
	}
	tail := body[tailOffset:]
	if tail[2] != 1 || tail[3] != 1 {
		t.Fatalf("security-card state = use:%d authenticated:%d, want 1/1", tail[2], tail[3])
	}
	if tail[4] != 0 || tail[5] != 0 || tail[6] != 0 {
		t.Fatalf("account state = passpad:%d resting:%d seria_lock:%d, want 0/0/0", tail[4], tail[5], tail[6])
	}
}

func TestSendGameInitialHeader(t *testing.T) {
	service := &Service{options: options{
		serverIP:        "42.240.165.245",
		gameInitialMode: gameInitialModeUpper,
		gameUpperHeader: gameUpperHeaderServer16,
	}}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channel}

	_, bodyLen, sent, err := service.sendGameInitial(session)
	if err != nil {
		t.Fatalf("send initial: %v", err)
	}
	if !sent || bodyLen == 0 {
		t.Fatalf("sent=%v bodyLen=%d", sent, bodyLen)
	}
	assertUpperInitialWire(t, conn.write.Bytes(), service.gameUpperHeaderSize(), upperSuccessBody(service.buildLoginSuccess(channel)), true)
}

func TestSendGameInitialCodec(t *testing.T) {
	service := &Service{options: options{
		serverIP:           "42.240.165.245",
		gameInitialMode:    gameInitialModeUpper,
		gameUpperHeader:    gameUpperHeaderServer16,
		gameUpperBodyCodec: gameUpperBodyCodecPlain,
	}}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channel}

	_, bodyLen, sent, err := service.sendGameInitial(session)
	if err != nil {
		t.Fatalf("send initial: %v", err)
	}
	if !sent || bodyLen == 0 {
		t.Fatalf("sent=%v bodyLen=%d", sent, bodyLen)
	}
	assertUpperInitialWire(t, conn.write.Bytes(), service.gameUpperHeaderSize(), upperSuccessBody(service.buildLoginSuccess(channel)), false)
}

func assertUpperInitialWire(t *testing.T, raw []byte, headerSize int, wantBody []byte, wantEncoded bool) {
	t.Helper()
	packet, rest := splitGameServerUpperPacketWithHeader(t, raw, headerSize)
	if len(rest) != 0 {
		t.Fatalf("unexpected bytes after initial upper packet: %d", len(rest))
	}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification {
		t.Fatalf("initial class = %d, want %d", packet.Header.Classification, dnfproto.DefaultChannelClassification)
	}
	if packet.Header.MsgID != uint16(dnfenum.UpperMsgGameEndpoint) {
		t.Fatalf("initial msg = %d, want %d", packet.Header.MsgID, dnfenum.UpperMsgGameEndpoint)
	}
	if packet.Header.Seq != 0 {
		t.Fatalf("endpoint success seq = %d, want 0", packet.Header.Seq)
	}
	wantWireBody := wantBody
	if wantEncoded {
		encodedBody, encoded, err := dnfproto.EncodeLatestUpperServerBody(uint16(dnfenum.UpperMsgGameEndpoint), wantBody)
		if err != nil {
			t.Fatalf("encode initial body: %v", err)
		}
		if !encoded {
			t.Fatal("initial upper body should be encoded")
		}
		wantWireBody = encodedBody
	}
	if !bytes.Equal(packet.Body, wantWireBody) {
		t.Fatalf("initial upper body = %x, want %x", packet.Body, wantWireBody)
	}
}

func assertNoticeWire(t *testing.T, raw []byte, wantBody []byte, wantEncoded bool) {
	t.Helper()
	if len(raw) != 15+len(wantBody) {
		t.Fatalf("initial raw len = %d, want %d", len(raw), 15+len(wantBody))
	}
	if raw[0] != 0 {
		t.Fatalf("initial class = %d, want 0", raw[0])
	}
	if got := binary.LittleEndian.Uint16(raw[1:3]); got != 1 {
		t.Fatalf("initial msg = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(raw[3:7]); got != uint32(len(raw)) {
		t.Fatalf("initial packet len = %d, want %d", got, len(raw))
	}
	if got := binary.LittleEndian.Uint32(raw[7:11]); got != 0 {
		t.Fatalf("initial marker = %d, want 0", got)
	}
	if !bytes.Equal(raw[11:15], []byte{0, 0, 0, 0}) {
		t.Fatalf("initial reserved = %x, want 00000000", raw[11:15])
	}
	wantWireBody := wantBody
	if wantEncoded {
		wantWireBody = encNoticeBody(wantBody)
		if bytes.Equal(wantWireBody, wantBody) {
			t.Fatal("initial wire body should be encoded, not handler plaintext")
		}
	}
	if !bytes.Equal(raw[15:], wantWireBody) {
		t.Fatalf("initial wire body = %x, want %x", raw[15:], wantWireBody)
	}
}

func TestEncNoticeBody(t *testing.T) {
	plain := []byte{0x00, 0x01, 0x04, 0x06, 0x34, 0x2e, 0x63, 0x68}
	want := []byte{0xd6, 0xd2, 0xc6, 0xce, 0x06, 0x6e, 0x5b, 0x77}

	if got := encNoticeBody(plain); !bytes.Equal(got, want) {
		t.Fatalf("encoded body = %x, want %x", got, want)
	}
	if !bytes.Equal(plain, []byte{0x00, 0x01, 0x04, 0x06, 0x34, 0x2e, 0x63, 0x68}) {
		t.Fatalf("encode mutated input: %x", plain)
	}
}

func TestNoticeChanName(t *testing.T) {
	channel := channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", NoticeName: "ch.2", Port: 10038}

	if got := noticeChanName(channel); got != "ch.38" {
		t.Fatalf("initial channel name = %q, want ch.38", got)
	}
}

func TestNoticeChanNameIgnoresName(t *testing.T) {
	channel := channelcatalog.Channel{ID: 19, Type: 1, Name: "ignored.by.initial.notice", Port: 10019}

	if got := noticeChanName(channel); got != "ch.19" {
		t.Fatalf("initial channel name = %q, want ch.19", got)
	}
}

func TestNoticeChanNameUsesID(t *testing.T) {
	channel := channelcatalog.Channel{ID: 6, Type: 3, Port: 10006}

	if got := noticeChanName(channel); got != "ch.6" {
		t.Fatalf("initial channel name = %q, want ch.6", got)
	}
}

func TestPostBootstrapWaits(t *testing.T) {
	service := &Service{options: options{gamePostBootstrap: gamePostBootstrapNone}}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}

	if err := service.sendPostInitialBootstrap(session, gameInitialModeUpper, true); err != nil {
		t.Fatalf("post initial bootstrap: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("post initial bootstrap wrote unexpected bytes: %x", conn.write.Bytes())
	}
}

func TestPreBootstrapIsWriteSilentDespiteLegacyOption(t *testing.T) {
	service := &Service{options: options{
		gameInitialMode:  gameInitialModeUpper,
		gamePreBootstrap: gamePreBootstrapGuard,
	}}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}}

	if err := service.sendPreInitialBootstrap(session); err != nil {
		t.Fatalf("pre initial bootstrap: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("pre bootstrap wrote unexpected bytes: %x", conn.write.Bytes())
	}
}

func TestPostCheckWaits(t *testing.T) {
	service := &Service{options: options{gamePostBootstrap: gamePostBootstrapCheck}}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}

	if err := service.sendPostInitialBootstrap(session, gameInitialModeUpper, true); err != nil {
		t.Fatalf("post initial bootstrap: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("post initial client-check wrote unexpected bytes: %x", conn.write.Bytes())
	}
}

func TestPostRosterWaits(t *testing.T) {
	service := &Service{options: options{gamePostBootstrap: gamePostBootstrapRoster}}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}

	if err := service.sendPostInitialBootstrap(session, gameInitialModeUpper, true); err != nil {
		t.Fatalf("post initial bootstrap: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("post initial roster wrote unexpected bytes: %x", conn.write.Bytes())
	}
}

func assertRuntimeAfterBlacklistSafePrefix(t *testing.T, service *Service, session *gameSession, rest []byte) []byte {
	t.Helper()
	_ = service
	_ = session
	for idx, want := range longHengSceneRuntimeAfterBlacklistSafePrefixPackets() {
		var packet dnfproto.ChannelPacket
		packet, rest = splitCSharpSelectInitPacket(t, rest, want)
		wantBody := want.body
		if packet.Header.Classification != want.class ||
			packet.Header.MsgID != want.msgID ||
			!bytes.Equal(packet.Body, wantBody) {
			t.Fatalf("runtime after blacklist safe prefix[%d] = class %d msg %d len=%d", idx, packet.Header.Classification, packet.Header.MsgID, len(packet.Body))
		}
		assertNoUnsafeRuntimeScenePacket(t, "runtime after blacklist safe prefix", idx, packet.Header.Classification, packet.Header.MsgID)
	}
	return rest
}

func assertNoActiveFinishLoadingPacket(t *testing.T, data []byte, label string) []byte {
	t.Helper()
	rest := data
	for len(rest) > 0 {
		packet, next := splitGameServerUpperPacket(t, rest)
		if packet.Header.Classification == 0 && packet.Header.MsgID == uint16(dnfenum.CmdPacketFinishLoading) {
			t.Fatalf("%s actively sent class0/op37 body=%x", label, packet.Body)
		}
		rest = next
	}
	return data
}

func assertSelectedSceneUserInfoRefresh(t *testing.T, service *Service, session *gameSession, data []byte) []byte {
	t.Helper()
	ctx := context.Background()
	charID, _, character, hasCharacter := service.selectedCharacterForEnter(ctx, session)
	var legacyRepo dnfrepo.LegacyUserInfoRepository
	if repos, ok := service.repositoryGroup(); ok {
		legacyRepo = repos.LegacyUserInfo
	}
	packet, rest := splitGameServerUpperPacket(t, data)
	wantBody := service.buildCurrentSelectedUserInfoMode1Body(ctx, session, legacyRepo, character, hasCharacter, charID)
	if packet.Header.Classification != 0 ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		!bytes.Equal(packet.Body, wantBody) {
		t.Fatalf("selected scene userinfo mode1 refresh = class %d msg %d body=%x want=%x", packet.Header.Classification, packet.Header.MsgID, packet.Body, wantBody)
	}
	if len(packet.Body) < 23 ||
		packet.Body[0] != 1 ||
		binary.LittleEndian.Uint16(packet.Body[1:3]) != 1 ||
		packet.Body[3] != currentSceneObjectRoute ||
		packet.Body[4] != currentSceneObjectContext ||
		!bytes.Equal(packet.Body[5:21], make([]byte, 16)) ||
		binary.LittleEndian.Uint16(packet.Body[21:23]) != currentSceneActorObjectKey(charID) {
		t.Fatalf("selected scene userinfo mode1 layout invalid body=%x", packet.Body)
	}
	if !session.selectedUserInfoRefreshSent {
		t.Fatalf("selectedUserInfoRefreshSent = false after userinfo mode1")
	}
	return rest
}

func assertSelectedSceneUserInfoMode3Refresh(t *testing.T, service *Service, session *gameSession, data []byte) []byte {
	t.Helper()
	ctx := context.Background()
	charID, _, character, hasCharacter := service.selectedCharacterForEnter(ctx, session)
	var legacyRepo dnfrepo.LegacyUserInfoRepository
	if repos, ok := service.repositoryGroup(); ok {
		legacyRepo = repos.LegacyUserInfo
	}
	packet, rest := splitGameServerUpperPacket(t, data)
	wantBody := service.buildCurrentSelectedUserInfoMode3Body(ctx, session, legacyRepo, character, hasCharacter, charID)
	if packet.Header.Classification != 0 ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		!bytes.Equal(packet.Body, wantBody) {
		t.Fatalf("selected scene userinfo mode3 refresh = class %d msg %d body=%x want=%x", packet.Header.Classification, packet.Header.MsgID, packet.Body, wantBody)
	}
	if len(packet.Body) < 172 ||
		packet.Body[0] != 3 ||
		binary.LittleEndian.Uint16(packet.Body[1:3]) != 1 ||
		packet.Body[3] != currentSceneObjectRoute ||
		packet.Body[4] != currentSceneObjectContext ||
		binary.LittleEndian.Uint16(packet.Body[0x0d:0x0f]) != currentSceneActorObjectKey(charID) ||
		binary.LittleEndian.Uint32(packet.Body[0x0f:0x13]) != 0 ||
		binary.LittleEndian.Uint32(packet.Body[0x13:0x17]) != 92 {
		t.Fatalf("selected scene userinfo mode3 layout invalid body=%x", packet.Body)
	}
	if !session.selectedUserInfoMode3Sent {
		t.Fatalf("selectedUserInfoMode3Sent = false after mode3")
	}
	return rest
}

func assertSelectedSceneUserInfoRefreshSkipped(t *testing.T, session *gameSession, data []byte) []byte {
	t.Helper()
	if !session.selectedUserInfoRefreshSent {
		t.Fatalf("selectedUserInfoRefreshSent = false, want skipped marker")
	}
	if len(data) != 0 {
		packet, _ := splitGameServerUpperPacket(t, data)
		t.Fatalf("selected scene userinfo refresh wrote unexpected packet class %d msg %d body_len=%d", packet.Header.Classification, packet.Header.MsgID, len(packet.Body))
	}
	return data
}

func assertNoUnsafeProactiveReplayPacket(t *testing.T, stage string, idx int, class byte, msgID uint16) {
	t.Helper()
	if longHengSceneProactiveReplayPacketBlocked(class, msgID) {
		t.Fatalf("%s[%d] sent unsafe proactive replay packet class=%d msg=%d", stage, idx, class, msgID)
	}
}

func assertNoUnsafeRuntimeScenePacket(t *testing.T, stage string, idx int, class byte, msgID uint16) {
	t.Helper()
	assertNoUnsafeProactiveReplayPacket(t, stage, idx, class, msgID)
	if class == 0 && msgID == uint16(dnfenum.CmdPacketSellItem) {
		t.Fatalf("%s[%d] replayed old class0/op22 body instead of a typed current actor-position update", stage, idx)
	}
	if class == dnfproto.DefaultChannelClassification && msgID == uint16(dnfenum.CmdPacketExit) {
		t.Fatalf("%s[%d] sent class1 exit packet", stage, idx)
	}
	if class == 0 && msgID == longHengCurrentRuntimeObjectStateMsgID {
		t.Fatalf("%s[%d] replayed historical object row as current op356 clear-quest list", stage, idx)
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketSetUDPIPPort) {
		t.Fatalf("%s[%d] sent old DOVE op2 runtime object stream before current structure is rebuilt", stage, idx)
	}
	if class == 0 && msgID == uint16(dnfenum.CmdPacketRepairEquipment) {
		t.Fatalf("%s[%d] replayed old class0/op23 body instead of a typed current actor-scene update", stage, idx)
	}
	if class == dnfproto.DefaultChannelClassification && msgID == uint16(dnfenum.CmdPacketWeddingResponse) {
		t.Fatalf("%s[%d] sent request-driven class1/op1033 wedding UI packet in runtime scene prefix", stage, idx)
	}
}

func TestRuntimeAfterBlacklistSafePrefixSkipsSyntheticObjectBitsets(t *testing.T) {
	packets := longHengSceneRuntimeAfterBlacklistSafePrefixPackets()
	if len(packets) != 0 {
		t.Fatalf("runtime after blacklist active prefix = %d packets, want no proactive DOVE-order packets", len(packets))
	}
}

func TestLongHengSceneRuntimeAfterBlacklistLedgerCoversFilteredDebt(t *testing.T) {
	ledger := longHengSceneRuntimeAfterBlacklistPrefixLedger()
	rawCount := longHengRuntimeAfterBlacklistSafeRawCount
	if rawCount > len(longHengSceneRuntimeAfterBlacklistPackets) {
		rawCount = len(longHengSceneRuntimeAfterBlacklistPackets)
	}
	if len(ledger) != rawCount {
		t.Fatalf("runtime ledger count = %d, want raw prefix count %d", len(ledger), rawCount)
	}
	implemented := 0
	var op9Excluded, objectDebt, op22Debt, op23Verdict, op1033Verdict, class1ExitDebt, op380Verdict, op12Verdict bool
	for idx, entry := range ledger {
		if entry.idx != 149+idx {
			t.Fatalf("runtime ledger[%d] idx = %d, want %d", idx, entry.idx, 149+idx)
		}
		if entry.phase != "08_in_scene_runtime_updates" || entry.status == "" || entry.reason == "" {
			t.Fatalf("runtime ledger[%d] incomplete entry: %+v", idx, entry)
		}
		if entry.implemented {
			implemented++
			if entry.status != longHengPacketImplementedCurrentBody {
				t.Fatalf("runtime ledger[%d] implemented with status %q", idx, entry.status)
			}
		}
		if entry.class == 0 && entry.msgID == uint16(dnfenum.CmdPacketRecoverStamina) &&
			entry.status == longHengPacketNotUsedCurrentClient &&
			strings.Contains(entry.reason, "no_runtime_state_owner") {
			op9Excluded = true
		}
		if entry.class == 0 && entry.msgID == uint16(dnfenum.CmdPacketSetUDPIPPort) &&
			entry.status == longHengPacketPendingCurrentStruct &&
			strings.Contains(entry.reason, "old_dove_op2_runtime_object_stream") {
			objectDebt = true
		}
		if entry.class == 0 && entry.msgID == uint16(dnfenum.CmdPacketSellItem) &&
			entry.status == longHengPacketPendingCurrentStruct &&
			strings.Contains(entry.reason, "sub_1D83990_reads") {
			op22Debt = true
		}
		if entry.class == 0 && entry.msgID == uint16(dnfenum.CmdPacketRepairEquipment) &&
			entry.status == longHengPacketPendingCurrentStruct &&
			strings.Contains(entry.reason, "sub_1D89590_reads") {
			op23Verdict = true
		}
		if entry.class == dnfproto.DefaultChannelClassification && entry.msgID == uint16(dnfenum.CmdPacketWeddingResponse) &&
			entry.status == longHengPacketRequestDriven &&
			strings.Contains(entry.reason, "not_runtime_scene_state") {
			op1033Verdict = true
		}
		if entry.class == dnfproto.DefaultChannelClassification &&
			entry.msgID == uint16(dnfenum.CmdPacketExit) &&
			entry.status == longHengPacketNotUsedCurrentClient &&
			strings.Contains(entry.reason, "ui_transition") {
			class1ExitDebt = true
		}
		if entry.class == 0 &&
			entry.msgID == uint16(dnfenum.CmdPacketSetLabyrinthSeatState) &&
			entry.status == longHengPacketRequestDriven &&
			strings.Contains(entry.reason, "current_op380_reads") {
			op380Verdict = true
		}
		if entry.class == 0 &&
			entry.msgID == uint16(dnfenum.CmdPacketSetPartyInfo) &&
			entry.status == longHengPacketRequestDriven &&
			strings.Contains(entry.reason, "current_op12_reads_u8_u16_u8_wstr") {
			op12Verdict = true
		}
	}
	if implemented != len(longHengSceneRuntimeAfterBlacklistSafePrefixPackets()) {
		t.Fatalf("runtime ledger implemented count = %d, want current sent count %d", implemented, len(longHengSceneRuntimeAfterBlacklistSafePrefixPackets()))
	}
	if !op9Excluded || !objectDebt || !op22Debt || !op23Verdict || !op1033Verdict || !class1ExitDebt || !op380Verdict || !op12Verdict {
		t.Fatalf("runtime ledger missing markers op9Excluded=%v object=%v op22=%v op23=%v op1033=%v class1Exit=%v op380=%v op12=%v", op9Excluded, objectDebt, op22Debt, op23Verdict, op1033Verdict, class1ExitDebt, op380Verdict, op12Verdict)
	}
}

func TestLongHengSceneBeforeHudLedgerCoversLongHengOrder(t *testing.T) {
	ledger := longHengSceneBeforeHudLedger()
	if len(ledger) != 43 {
		t.Fatalf("before-HUD ledger count = %d, want DOVE idx25-67 count 43", len(ledger))
	}

	implemented := 0
	implementedOutsideBootstrap := 0
	cargoPadResetMarked := false
	for idx, entry := range ledger {
		wantIdx := longHengSceneBeforeHudStartIndex + idx
		if entry.idx != wantIdx {
			t.Fatalf("before-HUD ledger[%d] idx = %d, want %d", idx, entry.idx, wantIdx)
		}
		if entry.phase != "05_scene_bootstrap_before_hud" || entry.status == "" || entry.reason == "" {
			t.Fatalf("before-HUD ledger[%d] incomplete entry: %+v", idx, entry)
		}
		if entry.implemented {
			implemented++
			if entry.status != longHengPacketImplementedCurrentBody {
				t.Fatalf("before-HUD ledger[%d] implemented with status %q", idx, entry.status)
			}
			if entry.msgID == uint16(dnfenum.CmdPacketCancelCargoPad) {
				implementedOutsideBootstrap++
				cargoPadResetMarked = strings.Contains(entry.reason, "before_real_item_lists")
			}
		}
	}

	activeLongHengOrderPackets := 0
	for _, packet := range longHengSceneBootstrapBeforeHudPackets {
		switch packet.kind {
		case csharpLongHengSceneBootstrapKind, csharpCurrentSceneStageStateKind, csharpCurrentSceneOp9ActorDisplayKind, currentAcceptableQuestListKind:
			activeLongHengOrderPackets++
		}
	}
	if implemented-implementedOutsideBootstrap != activeLongHengOrderPackets {
		t.Fatalf("before-HUD bootstrap implemented count = %d, active DOVE-order send count %d", implemented-implementedOutsideBootstrap, activeLongHengOrderPackets)
	}
	if implementedOutsideBootstrap != 1 || !cargoPadResetMarked {
		t.Fatalf("before-HUD cargo-pad reset ledger marker count=%d marked=%v", implementedOutsideBootstrap, cargoPadResetMarked)
	}

	wantTail := []struct {
		idx    int
		msgID  uint16
		status longHengScenePacketStatus
	}{
		{64, uint16(dnfenum.CmdPacketUseSharedEffectItem), longHengPacketRequestDriven},
		{65, uint16(dnfenum.CmdPacketFrameLagStatistics), longHengPacketPendingCurrentStruct},
		{66, uint16(dnfenum.CmdPacketAuctionRegistItem), longHengPacketRequestDriven},
		{67, uint16(dnfenum.CmdPacketAuctionRegistItem), longHengPacketRequestDriven},
	}
	for _, want := range wantTail {
		entry := ledger[want.idx-longHengSceneBeforeHudStartIndex]
		if entry.idx != want.idx || entry.msgID != want.msgID || entry.status != want.status || entry.implemented {
			t.Fatalf("before-HUD ledger idx%d = %+v", want.idx, entry)
		}
	}
}

func TestLongHengSceneBeforeHudOrderDoesNotInsertNonLongHengTriggers(t *testing.T) {
	reportClientSpecIdx := -1
	op9Idx := -1
	for idx, packet := range longHengSceneBootstrapBeforeHudPackets {
		switch packet.msgID {
		case uint16(dnfenum.CmdPacketPVPMissionHpPercent), uint16(dnfenum.CmdPacketMissionRouletteTrigger):
			if packet.kind != csharpCurrentActionTableStateKind {
				t.Fatalf("before-HUD packet[%d] inserted non-DOVE trigger msg=%d", idx, packet.msgID)
			}
		case uint16(dnfenum.CmdPacketReportClientSpec):
			reportClientSpecIdx = idx
		case uint16(dnfenum.CmdPacketRecoverStamina):
			op9Idx = idx
		case uint16(dnfenum.CmdPacketGuildCargoPushItem):
			t.Fatalf("before-HUD packet[%d] replayed old guild-cargo body", idx)
		case uint16(dnfenum.CmdPacketReqRepresentCharacter):
			t.Fatalf("before-HUD packet[%d] replayed old represent-character body", idx)
		}
	}
	if reportClientSpecIdx < 0 || op9Idx < 0 {
		t.Fatalf("before-HUD required indexes op124=%d op9=%d", reportClientSpecIdx, op9Idx)
	}
	if reportClientSpecIdx+1 != op9Idx {
		t.Fatalf("before-HUD current tail order indexes op124=%d op9=%d", reportClientSpecIdx, op9Idx)
	}
}

func TestSceneBootstrapExcludesAllDirectLongHengFixtureBodies(t *testing.T) {
	excluded := map[uint16]bool{
		uint16(dnfenum.CmdPacketCreateCharacter):       true,
		uint16(dnfenum.CmdPacketLeaveParty):            true,
		uint16(dnfenum.CmdPacketGatheringPartyStatus):  true,
		uint16(dnfenum.CmdPacketWalkoutPartyMember):    true,
		uint16(dnfenum.CmdPacketGetAvatarSpecEvent):    true,
		uint16(dnfenum.CmdPacketReport4Hack):           true,
		uint16(dnfenum.CmdPacketPeerConnectResult):     true,
		uint16(dnfenum.CmdPacketMoveItemspace):         true,
		uint16(dnfenum.CmdPacketChangeDeckInfo):        true,
		uint16(dnfenum.CmdPacketDungeonNPCBuffInfo):    true,
		uint16(dnfenum.CmdPacketApproveJoinGuild):      true,
		uint16(dnfenum.CmdPacketCancelJoinGuild):       true,
		uint16(dnfenum.CmdPacketCancelCargoPad):        true,
		uint16(dnfenum.CmdPacketGuildCargoPushItem):    true,
		uint16(dnfenum.CmdPacketReqRepresentCharacter): true,
	}
	for idx, packet := range longHengSceneBootstrapBeforeHudPackets {
		if excluded[packet.msgID] {
			t.Fatalf("scene bootstrap[%d] still sends excluded direct DOVE fixture msg=%d", idx, packet.msgID)
		}
		if strings.HasPrefix(packet.bodyCodec, "dove_") {
			t.Fatalf("scene bootstrap[%d] still exposes DOVE transport codec %q", idx, packet.bodyCodec)
		}
	}
}

func TestLongHengSceneOp12OldBodiesDoNotMatchCurrentReaderShape(t *testing.T) {
	fixtures := []string{
		"game_54226_s2c_0134_class0_op12_seq0_body32.bin",
		"game_54226_s2c_0135_class0_op12_seq0_body48.bin",
		"game_54226_s2c_0136_class0_op12_seq0_body32.bin",
		"game_54226_s2c_0137_class0_op12_seq0_body32.bin",
		"game_54226_s2c_0138_class0_op12_seq0_body32.bin",
		"game_54226_s2c_0139_class0_op12_seq0_body48.bin",
		"game_54226_s2c_0140_class0_op12_seq0_body64.bin",
		"runtime_after_blacklist_000170_class0_op12_body48.bin",
		"runtime_after_blacklist_000178_class0_op12_body48.bin",
		"runtime_after_blacklist_000248_class0_op12_body48.bin",
	}
	for _, file := range fixtures {
		body := mustLongHengSceneBody(longHengSceneFixtureSpec{file: file})
		if len(body) < 8 {
			t.Fatalf("%s len = %d, want enough bytes for current op12 header", file, len(body))
		}
		wstrBytes := binary.LittleEndian.Uint32(body[4:8])
		if wstrBytes <= uint32(len(body)-8) {
			t.Fatalf("%s unexpectedly fits current op12 u8,u16,u8,wstr shape: body_len=%d wstr_bytes=%d body=%x", file, len(body), wstrBytes, body)
		}
	}
}

func TestLongHengSceneSharedEffectEquipmentOldBodiesAreNotCurrentBootstrapBodies(t *testing.T) {
	oldOp251 := mustLongHengSceneBody(longHengSceneFixtureSpec{file: "000064_05_scene_bootstrap_before_hud_cls0_op0251_ENUM_CMDPACKET_USE_SHARED_EFFECT_ITEM_body8.bin"})
	if len(oldOp251) != 8 {
		t.Fatalf("old op251 body len = %d, want 8", len(oldOp251))
	}
	if count := binary.LittleEndian.Uint16(oldOp251[:2]); count != 0x3455 {
		t.Fatalf("old op251 first u16 = %#x, want DOVE sample count-like value 0x3455", count)
	}

	oldOp194 := mustLongHengSceneBody(longHengSceneFixtureSpec{file: "000065_05_scene_bootstrap_before_hud_cls0_op0194_ENUM_CMDPACKET_FRAME_LAG_STATISTICS_body174.bin"})
	if len(oldOp194) != 174 {
		t.Fatalf("old op194 body len = %d, want 174", len(oldOp194))
	}
	if count := binary.LittleEndian.Uint16(oldOp194[:2]); count != 0x1c45 {
		t.Fatalf("old op194 first u16 = %#x, want DOVE sample count-like value 0x1c45", count)
	}
	if need := 2 + int(binary.LittleEndian.Uint16(oldOp194[:2]))*4; need <= len(oldOp194) {
		t.Fatalf("old op194 unexpectedly fits current u16-count/u16-u16 reader: need %d len %d", need, len(oldOp194))
	}

	oldOp183A := mustLongHengSceneBody(longHengSceneFixtureSpec{file: "000066_05_scene_bootstrap_before_hud_cls0_op0183_ENUM_CMDPACKET_AUCTION_REGIST_ITEM_body8.bin"})
	oldOp183B := mustLongHengSceneBody(longHengSceneFixtureSpec{file: "000067_05_scene_bootstrap_before_hud_cls0_op0183_ENUM_CMDPACKET_AUCTION_REGIST_ITEM_body8.bin"})
	if len(oldOp183A) != 8 || len(oldOp183B) != 8 {
		t.Fatalf("old op183 body lens = %d/%d, want 8/8", len(oldOp183A), len(oldOp183B))
	}
	if oldOp183A[0] == 0 || oldOp183A[0] == 1 || oldOp183B[0] == 0 || oldOp183B[0] == 1 {
		t.Fatalf("old op183 samples unexpectedly start with current small status values: %x / %x", oldOp183A[:2], oldOp183B[:2])
	}

	for idx, packet := range longHengSceneBootstrapBeforeHudPackets {
		switch packet.msgID {
		case uint16(dnfenum.CmdPacketUseSharedEffectItem):
			if bytes.Equal(packet.body, oldOp251) {
				t.Fatalf("before-HUD packet[%d] replays old DOVE op251 shared-effect body", idx)
			}
		case uint16(dnfenum.CmdPacketFrameLagStatistics):
			if bytes.Equal(packet.body, oldOp194) {
				t.Fatalf("before-HUD packet[%d] replays old DOVE op194 frame-lag body", idx)
			}
		case uint16(dnfenum.CmdPacketAuctionRegistItem):
			if bytes.Equal(packet.body, oldOp183A) || bytes.Equal(packet.body, oldOp183B) {
				t.Fatalf("before-HUD packet[%d] replays old DOVE op183 auction/equipment body", idx)
			}
		}
	}
}

func TestLongHengSceneOp53OldBodyIsPVPReadyStateNotSceneBootstrap(t *testing.T) {
	body := mustLongHengSceneBody(longHengSceneFixtureSpec{file: "game_54226_s2c_0141_class0_op53_seq0_body16.bin"})
	if len(body) != 16 {
		t.Fatalf("op53 DOVE body len = %d, want 16", len(body))
	}
	if got := body[0]; got != 0x86 {
		t.Fatalf("op53 DOVE first current u8 = %#x, want 0x86", got)
	}
	if got := binary.LittleEndian.Uint32(body[1:5]); got != 0x762ce52d {
		t.Fatalf("op53 DOVE first current u32 = %#x, want 0x762ce52d", got)
	}
	if got := binary.LittleEndian.Uint32(body[5:9]); got != 0x8983f9ec {
		t.Fatalf("op53 DOVE second current u32 = %#x, want 0x8983f9ec", got)
	}
	if got := binary.LittleEndian.Uint32(body[9:13]); got != 0x533ab215 {
		t.Fatalf("op53 DOVE third current u32 = %#x, want 0x533ab215", got)
	}
	if got := body[13:]; !bytes.Equal(got, []byte{0xf0, 0x1a, 0xcf}) {
		t.Fatalf("op53 DOVE extra tail = %x, want f01acf", got)
	}
}

func TestLongHengSceneOp1292OldBodyIsJoustResultStateNotSceneBootstrap(t *testing.T) {
	body := mustLongHengSceneBody(longHengSceneFixtureSpec{file: "game_54226_s2c_0143_class0_op1292_seq0_body16.bin"})
	if len(body) != 16 {
		t.Fatalf("op1292 DOVE body len = %d, want 16", len(body))
	}
	if got := binary.LittleEndian.Uint32(body[:4]); got != 0x477c1f2b {
		t.Fatalf("op1292 DOVE raw4 result field = %#x, want 0x477c1f2b", got)
	}
	if got := binary.LittleEndian.Uint16(body[:2]); got != 0x1f2b {
		t.Fatalf("op1292 DOVE u16 UI field = %#x, want 0x1f2b", got)
	}
	if got := body[4:]; !bytes.Equal(got, []byte{0x70, 0x75, 0x37, 0xd4, 0xa5, 0x80, 0x3d, 0xc3, 0xea, 0x89, 0x49, 0xc1}) {
		t.Fatalf("op1292 DOVE extra tail = %x, want 707537d4a5803dc3ea8949c1", got)
	}
}

func TestLongHengSceneClass1Op140OldBodyIsLargeGuildListNotSceneBootstrap(t *testing.T) {
	body := mustLongHengSceneBody(longHengSceneFixtureSpec{file: "passive_legacy_0140_class1_op140_guild_all_member_list_body15962.bin"})
	if len(body) != 15962 {
		t.Fatalf("op140 DOVE body len = %d, want 15962", len(body))
	}
	if got := binary.LittleEndian.Uint32(body[:4]); got != 0x9d8dd3fb {
		t.Fatalf("op140 DOVE first current u32/guild field = %#x, want 0x9d8dd3fb", got)
	}
	if got := binary.LittleEndian.Uint32(body[4:8]); got != 0x7c5177a4 {
		t.Fatalf("op140 DOVE next raw field = %#x, want 0x7c5177a4", got)
	}
}

func TestCurrentClearQuestListDefaultBodyDoesNotActivateSceneObjectKey(t *testing.T) {
	body := buildCurrentPassGateObjectBody()
	if len(body) != 4+30000 {
		t.Fatalf("clear quest list body len = %d, want %d", len(body), 4+30000)
	}
	rawLen := binary.LittleEndian.Uint32(body[:4])
	if rawLen != 30000 {
		t.Fatalf("clear quest list raw len = %d, want 30000", rawLen)
	}
	raw := body[4:]
	if got := raw[currentSceneBootstrapObjectKey]; got != 0 {
		t.Fatalf("clear quest list wrote scene object key %#x flag=%d", currentSceneBootstrapObjectKey, got)
	}
	if raw[longHengSceneStageFixtureObjectKey] != 0 {
		t.Fatalf("clear quest list must not activate DOVE fixture key %#x", longHengSceneStageFixtureObjectKey)
	}
}

func TestCurrentClearQuestListDefaultTransportDoesNotActivateSceneObjectKey(t *testing.T) {
	body := buildCurrentPassGateObjectTransportBody()
	if len(body) < 2 {
		t.Fatalf("clear quest list transport must stay zlib encoded, got len=%d", len(body))
	}
	if body[0] != 0x78 || body[1] != 0x9c {
		t.Fatalf("clear quest list transport must stay zlib encoded, got prefix=%x", body[:2])
	}
	plain, err := zlibDecompress(body)
	if err != nil {
		t.Fatalf("decompress clear quest list transport: %v", err)
	}
	if len(plain) != 4+30000 {
		t.Fatalf("clear quest list transport expanded len = %d, want %d", len(plain), 4+30000)
	}
	rawLen := binary.LittleEndian.Uint32(plain[:4])
	if rawLen != 30000 {
		t.Fatalf("clear quest list transport raw len = %d, want 30000", rawLen)
	}
	if got := plain[4+currentSceneBootstrapObjectKey]; got != 0 {
		t.Fatalf("clear quest list transport wrote scene object key %#x flag=%d", currentSceneBootstrapObjectKey, got)
	}
	if got := plain[4+longHengSceneStageFixtureObjectKey]; got != 0 {
		t.Fatalf("clear quest list transport must not retain DOVE fixture key %#x, got %d", longHengSceneStageFixtureObjectKey, got)
	}
}

func TestCurrentCancelCargoPadTransportBodyUsesTypedUnusedSlotReset(t *testing.T) {
	body := buildCurrentCancelCargoPadTransportBody()
	if want := 24; len(body) != want {
		t.Fatalf("cargo-pad protected body len=%d, want %d", len(body), want)
	}
	compressedEnd := len(body) - currentCancelCargoPadProtectedTailSize
	if compressedEnd < 2 || body[0] != 0x78 || body[1] != 0x9c {
		t.Fatalf("cargo-pad reset transport must stay zlib encoded, got %x", body)
	}
	if got := body[compressedEnd:]; !bytes.Equal(got, bytes.Repeat([]byte{0xff}, currentCancelCargoPadProtectedTailSize)) {
		t.Fatalf("cargo-pad protected tail=%x, want %d unused sentinels", got, currentCancelCargoPadProtectedTailSize)
	}
	plain, err := zlibDecompress(body[:compressedEnd])
	if err != nil {
		t.Fatalf("decompress cargo-pad reset transport: %v", err)
	}
	if want := currentCancelCargoPadHeaderSize + currentCancelCargoPadSlotCount*4; len(plain) != want {
		t.Fatalf("cargo-pad reset expanded len = %d, want %d", len(plain), want)
	}
	if got := plain[:currentCancelCargoPadHeaderSize]; !bytes.Equal(got, []byte{0, 2, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("cargo-pad reset header = %x", got)
	}
	for offset := currentCancelCargoPadHeaderSize; offset < len(plain); offset += 4 {
		if got := binary.LittleEndian.Uint32(plain[offset : offset+4]); got != ^uint32(0) {
			t.Fatalf("cargo-pad reset slot %d = %#x, want unused", (offset-currentCancelCargoPadHeaderSize)/4, got)
		}
	}
}

func TestRuntimeAfterBlacklistSmallScenePacketsUseCurrentReaders(t *testing.T) {
	if packets := longHengSceneRuntimeAfterBlacklistSafePrefixPackets(); len(packets) != 0 {
		t.Fatalf("runtime after blacklist still sends %d small replay-order packets", len(packets))
	}
}

func TestRuntimeAfterBlacklistOp9RowsUseCurrentNoopBodyAfterValidation(t *testing.T) {
	rawCount := longHengRuntimeAfterBlacklistSafeRawCount
	if rawCount > len(longHengSceneRuntimeAfterBlacklistPackets) {
		rawCount = len(longHengSceneRuntimeAfterBlacklistPackets)
	}
	wantBody := buildCurrentSceneOp9NoopBody()
	rawOp9 := 0
	for idx, packet := range longHengSceneRuntimeAfterBlacklistPackets[:rawCount] {
		if packet.class != 0 || packet.msgID != uint16(dnfenum.CmdPacketRecoverStamina) {
			continue
		}
		rawOp9++
		if !bytes.Equal(packet.body, wantBody) {
			t.Fatalf("runtime raw op9[%d] body = %x, want current noop body %x", idx, packet.body, wantBody)
		}
	}
	if rawOp9 == 0 {
		t.Fatalf("runtime raw prefix missing op9 rows")
	}
	if packets := longHengSceneRuntimeAfterBlacklistSafePrefixPackets(); len(packets) != 0 {
		t.Fatalf("runtime safe prefix still sends %d replay-order packets", len(packets))
	}
}

func TestRuntimeAfterBlacklistRawOldOp2RowsStayOldBodiesWhileGated(t *testing.T) {
	rawCount := longHengRuntimeAfterBlacklistSafeRawCount
	if rawCount > len(longHengSceneRuntimeAfterBlacklistPackets) {
		rawCount = len(longHengSceneRuntimeAfterBlacklistPackets)
	}
	rawObjectRows := 0
	for idx, packet := range longHengSceneRuntimeAfterBlacklistPackets[:rawCount] {
		if packet.class != 0 || packet.msgID != uint16(dnfenum.CmdPacketSetUDPIPPort) {
			continue
		}
		rawObjectRows++
		wantBody := mustLongHengSceneBody(longHengSceneFixtureSpec{file: packet.file})
		if !bytes.Equal(packet.body, wantBody) {
			t.Fatalf("runtime raw old op2[%d] body len=%d want original DOVE body len=%d", idx, len(packet.body), len(wantBody))
		}
		if bytes.Equal(packet.body, buildCurrentPassGateObjectBody()) {
			t.Fatalf("runtime raw old op2[%d] was rewritten to current op356 clear-quest table", idx)
		}
	}
	if rawObjectRows == 0 {
		t.Fatalf("runtime raw prefix missing old op2 rows")
	}
	for idx, packet := range longHengSceneRuntimeAfterBlacklistSafePrefixPackets() {
		if packet.class == 0 && (packet.msgID == uint16(dnfenum.CmdPacketSetUDPIPPort) || packet.msgID == longHengCurrentRuntimeObjectStateMsgID) {
			t.Fatalf("runtime safe prefix[%d] sent object-stream debt before current structure is rebuilt", idx)
		}
	}
}

func TestLongHengRuntimeOldOp2BodiesDoNotFitCurrentObjectDispatchers(t *testing.T) {
	fixtures := []string{
		"runtime_after_blacklist_000163_class0_op2_body415.bin",
		"runtime_after_blacklist_000177_class0_op2_body417.bin",
		"runtime_after_blacklist_000288_class0_op2_body377.bin",
		"runtime_after_blacklist_000303_class0_op2_body396.bin",
	}
	for _, file := range fixtures {
		body := mustLongHengSceneBody(longHengSceneFixtureSpec{file: file})
		if len(body) < 4 {
			t.Fatalf("%s len = %d, want at least current dispatcher header", file, len(body))
		}
		op356RawLen := binary.LittleEndian.Uint32(body[:4])
		if op356RawLen == 30000 && len(body) == 30004 {
			t.Fatalf("%s unexpectedly fits current op356 bitmap shape", file)
		}
		mode := body[0]
		if currentMsg2ObjectModePlausibleForOldLongHengBody(body) {
			t.Fatalf("%s unexpectedly fits current static msg2/sub_200BEA0 object dispatcher shape: mode=%d len=%d head=%x", file, mode, len(body), body[:min(len(body), 16)])
		}
	}
}

func currentMsg2ObjectModePlausibleForOldLongHengBody(body []byte) bool {
	if len(body) < 3 {
		return false
	}
	mode := body[0]
	switch mode {
	case 0:
		count := int(binary.LittleEndian.Uint16(body[1:3]))
		return count <= (len(body)-3)/3
	case 1, 3, 6:
		count := int(binary.LittleEndian.Uint16(body[1:3]))
		return count <= (len(body)-3)/3
	case 8:
		count := int(binary.LittleEndian.Uint16(body[1:3]))
		return count <= (len(body)-3)/6
	case 2, 4, 5, 7, 9, 10:
		return len(body) >= 3
	default:
		return false
	}
}

func TestLongHengSceneProactiveReplayGuardCoversExactGiftAndEventClass(t *testing.T) {
	for _, packet := range []struct {
		class byte
		msgID uint16
	}{
		{class: 0, msgID: uint16(dnfenum.CmdPacketLeaveParty)},
		{class: 0, msgID: uint16(dnfenum.CmdPacketWalkoutPartyMember)},
		{class: dnfproto.DefaultChannelClassification, msgID: uint16(dnfenum.CmdPacketLetsPickPresent)},
		{class: 0, msgID: uint16(dnfenum.CmdPacketReport4Hack)},
		{class: 0, msgID: uint16(dnfenum.CmdPacketGetAvatarSpecEvent)},
		{class: 0, msgID: uint16(dnfenum.CmdPacketAttendanceCheck)},
		{class: 0, msgID: uint16(dnfenum.CmdPacketSetLabyrinthSeatState)},
		{class: 0, msgID: uint16(dnfenum.CmdPacketWelcombackAttendance)},
		{class: 0, msgID: uint16(dnfenum.CmdPacketGiftOfSeria)},
		{class: 0, msgID: uint16(dnfenum.CmdPacketLuckyBalloon)},
	} {
		if !longHengSceneProactiveReplayPacketBlocked(packet.class, packet.msgID) {
			t.Fatalf("gift/event class=%d msg=%d must not be replayed during scene bootstrap", packet.class, packet.msgID)
		}
	}
}

func TestLongHengSceneProactiveReplayGuardDoesNotBanSameNumericOpcodeInOtherClass(t *testing.T) {
	for _, packet := range []struct {
		class byte
		msgID uint16
	}{
		{class: dnfproto.DefaultChannelClassification, msgID: uint16(dnfenum.CmdPacketSetPartyInfo)},
		{class: 0, msgID: uint16(dnfenum.CmdPacketMercenaryCompetition)},
		{class: 0, msgID: uint16(dnfenum.UpperMsgLoadExtendCharacs)},
	} {
		if longHengSceneProactiveReplayPacketBlocked(packet.class, packet.msgID) {
			t.Fatalf("same numeric opcode was globally banned class=%d msg=%d", packet.class, packet.msgID)
		}
	}
}

func TestLongHengSceneBootstrapOp104UsesCurrentEmptyList(t *testing.T) {
	found := false
	for _, packet := range longHengSceneBootstrapBeforeHudPackets {
		if packet.msgID != uint16(dnfenum.CmdPacketRequestAvagachaCoupon) {
			continue
		}
		found = true
		if !bytes.Equal(packet.body, []byte{0}) {
			t.Fatalf("scene bootstrap op104 body = %x, want current empty-list count=0", packet.body)
		}
	}
	if !found {
		t.Fatalf("scene bootstrap missing current op104 empty-list packet")
	}
}

func TestCurrentSceneObjectListUsesFullTemplateBeforeFinalizer(t *testing.T) {
	raw, ok := buildCurrentSceneObjectRawState(dnfrepo.CharacterRecord{}, false, "hero")
	if !ok {
		t.Fatalf("missing current scene object raw state")
	}
	body := buildCurrentSceneObjectListBody(currentSceneBootstrapObjectKey, dnfrepo.CharacterRecord{}, false, "hero")
	tailStart := currentSceneObjectTailStartForTest("hero")
	wantTail := buildCurrentSceneObjectEntryTail(dnfrepo.CharacterRecord{}, false)
	wantLen := tailStart + len(wantTail)
	if len(body) != wantLen ||
		body[0] != 0 ||
		binary.LittleEndian.Uint16(body[1:3]) != 1 ||
		body[3] != currentSceneObjectRoute ||
		body[4] != currentSceneObjectContext ||
		binary.LittleEndian.Uint16(body[0x4c:0x4e]) != currentSceneBootstrapObjectKey {
		t.Fatalf("current scene object template body len=%d head=%x key=%x", len(body), body[:8], body[0x4c:0x4e])
	}
	if got := body[5:0x4c]; !bytes.Equal(got, raw) {
		t.Fatalf("current scene object raw state = %x, want structured raw %x", got, raw)
	}
	wantName := rosterDstrName("hero")
	if got := body[0x4e:tailStart]; !bytes.Equal(got, wantName) {
		t.Fatalf("current scene object dstr name = %x, want %x", got, wantName)
	}
	if got := body[tailStart:]; !bytes.Equal(got, wantTail) {
		t.Fatalf("current scene object tail = %x, want current structured tail %x", got, wantTail)
	}
	equipEnd, ok := currentSceneObjectEquipSummaryEnd(body[tailStart:], 6)
	if !ok {
		t.Fatalf("current scene object creature state missing equipment boundary")
	}
	creatureOffset := tailStart + equipEnd + 22
	if creatureOffset+9 > len(body) {
		t.Fatalf("current scene object creature state truncated at %d of %d", creatureOffset, len(body))
	}
	if got := binary.LittleEndian.Uint32(body[creatureOffset : creatureOffset+4]); got != 0 {
		t.Fatalf("current scene object no-creature template = %#x, want 0; UINT32_MAX resolves to runtime creature type 8", got)
	}
	if nameLen := binary.LittleEndian.Uint32(body[creatureOffset+4 : creatureOffset+8]); nameLen != 0 {
		t.Fatalf("current scene object no-creature name len = %d, want 0", nameLen)
	}
	if visibleWire := body[creatureOffset+8]; visibleWire != 0 {
		t.Fatalf("current scene object no-creature visibility wire = %d, want 0", visibleWire)
	}
	if got := body[tailStart+0x02]; got != 0 {
		t.Fatalf("current scene object missing-character level = %d, want 0", got)
	}
	if noResource, ok := currentSceneObjectTailNoAttachedUIResourceForLog(body); !ok || !noResource {
		t.Fatalf("current scene object no-attached-ui-resource=%v ok=%v, want true true", noResource, ok)
	}
	if stateEventArg, ok := currentSceneObjectTailActorStateEventArgForLog(body); !ok || stateEventArg != currentSceneActorStateEventArg {
		t.Fatalf("current scene object actor state event arg=%d ok=%v, want %d true", stateEventArg, ok, currentSceneActorStateEventArg)
	}
	if visualState, ok := currentSceneObjectTailVisualStateForLog(body); !ok || visualState != currentSceneNormalTownVisualState {
		t.Fatalf("current scene object sub_20042F0 visual state=%#x ok=%v, want %#x true", visualState, ok, currentSceneNormalTownVisualState)
	}
	if level, parsedTailStart, ok := currentSceneObjectLevelForLog(body); !ok || parsedTailStart != tailStart || level != 0 {
		t.Fatalf("current scene object level log parse level=%d tailStart=%d ok=%v, want level=0 tailStart=%d",
			level, parsedTailStart, ok, tailStart)
	}
	highLevel := dnfrepo.CharacterRecord{
		Name:  "hero",
		Job:   "15",
		Level: 86,
		Stats: map[string]int64{"grow_type": 1},
	}
	highLevelBody := buildCurrentSceneObjectListBody(currentSceneBootstrapObjectKey, highLevel, true, "")
	highLevelTailStart := currentSceneObjectTailStartForTest("hero")
	wantHighLevelTail := buildCurrentSceneObjectEntryTail(highLevel, true)
	if len(highLevelBody) != highLevelTailStart+len(wantHighLevelTail) {
		t.Fatalf("current scene object high-level body len = %d, want %d", len(highLevelBody), highLevelTailStart+len(wantHighLevelTail))
	}
	wantCurrentRaw, ok := buildCurrentSceneObjectRawState(highLevel, true, "hero")
	if !ok {
		t.Fatalf("missing current scene object raw state")
	}
	if got := highLevelBody[5:0x4c]; !bytes.Equal(got, wantCurrentRaw) {
		t.Fatalf("current scene object high-level raw = %x, want current raw %x", got, wantCurrentRaw)
	}
	if source, head, rawWord2, rawWord5, raw0F, raw19, raw25, raw26, raw2C, raw43, ok := currentSceneObjectRawStateForLog(highLevelBody); !ok ||
		source != "current_exe_structured_raw" || head == "00000000000000000000000000000000" ||
		bytesAllZero(highLevelBody[5:0x4c]) {
		t.Fatalf("current scene object high-level raw log source=%s head=%s word2=%d word5=%d raw0f=%d raw19=%d raw25=%d raw26=%d raw2c=%d raw43=%d ok=%v",
			source, head, rawWord2, rawWord5, raw0F, raw19, raw25, raw26, raw2C, raw43, ok)
	}
	if got := highLevelBody[highLevelTailStart]; got != 15 {
		t.Fatalf("current scene object high-level job = %d, want 15", got)
	}
	if got := highLevelBody[highLevelTailStart+1]; got != packCurrentSceneObjectGrow(1, 0) {
		t.Fatalf("current scene object high-level grow pack = %d, want %d", got, packCurrentSceneObjectGrow(1, 0))
	}
	if got := highLevelBody[highLevelTailStart+0x02]; got != byte(highLevel.Level) {
		t.Fatalf("current scene object level = %d, want character level %d", got, highLevel.Level)
	}
	if level, parsedTailStart, ok := currentSceneObjectLevelForLog(highLevelBody); !ok || parsedTailStart != highLevelTailStart || level != byte(highLevel.Level) {
		t.Fatalf("current scene object level log parse level=%d tailStart=%d ok=%v, want level=%d tailStart=%d",
			level, parsedTailStart, ok, highLevel.Level, highLevelTailStart)
	}
	if got := highLevelBody[highLevelTailStart:]; !bytes.Equal(got, wantHighLevelTail) {
		t.Fatalf("current scene object high-level tail = %x, want current structured tail %x", got, wantHighLevelTail)
	}
	equipped := dnfrepo.CharacterRecord{
		Name:  "hero",
		Level: 1,
		Roster: dnfrepo.CharacterRoster{
			Entry: dnfrepo.CharacterRosterEntry{
				EquipSummary: []dnfrepo.CharacterRosterEquipSummary{
					{Slot: 11, ItemIDOrIcon: 900001},
				},
			},
		},
	}
	equippedBody := buildCurrentSceneObjectListBody(currentSceneBootstrapObjectKey, equipped, true, "")
	equippedTailStart := currentSceneObjectTailStartForTest("hero")
	wantEquippedTail := buildCurrentSceneObjectEntryTail(equipped, true)
	wantEquippedRaw, ok := buildCurrentSceneObjectRawState(equipped, true, "hero")
	if !ok {
		t.Fatalf("missing equipped scene object raw state")
	}
	if got := equippedBody[5:0x4c]; !bytes.Equal(got, wantEquippedRaw) {
		t.Fatalf("current scene object equipped raw = %x, want current raw %x", got, wantEquippedRaw)
	}
	if got := equippedBody[equippedTailStart:]; !bytes.Equal(got, wantEquippedTail) {
		t.Fatalf("current scene object equipped tail = %x, want current structured mode0 tail %x", got, wantEquippedTail)
	}
	if rows, _, ok := currentSceneObjectTailSummaryForLog(equippedBody); !ok || rows != 1 {
		t.Fatalf("current scene object equipped mode0 summary rows=%d ok=%v, want 1 true", rows, ok)
	}
	if bytes.Contains(body, []byte{0xa1, 0x01}) {
		t.Fatalf("current scene object template still contains DOVE fixture key 0x01a1")
	}

	objectIdx := -1
	actionTableIdx := -1
	insertOverseerIdx := -1
	requestOverseerBeforeInsert := 0
	for idx, packet := range longHengSceneBootstrapBeforeHudPackets {
		if packet.msgID == uint16(dnfenum.CmdPacketRequestSeriaBuff) {
			t.Fatalf("scene bootstrap must not actively send op432; current EXE crashes in sub_217DBB0 before object init")
		}
		if packet.msgID == uint16(dnfenum.CmdPacketPVPMissionHpPercent) {
			if packet.kind != csharpCurrentActionTableStateKind || !bytes.Equal(packet.body, buildCurrentActionTableStateBody()) {
				t.Fatalf("scene bootstrap op376 action-table init mismatch: kind=%q body=%x", packet.kind, packet.body)
			}
			actionTableIdx = idx
		}
		if packet.msgID == uint16(dnfenum.CmdPacketRequestOverseer) && insertOverseerIdx < 0 {
			requestOverseerBeforeInsert++
		}
		if packet.kind == csharpCurrentSceneObjectListKind {
			objectIdx = idx
		}
		if packet.msgID == uint16(dnfenum.CmdPacketInsertOverseer) {
			insertOverseerIdx = idx
			break
		}
	}
	if insertOverseerIdx <= 0 {
		t.Fatalf("op359 insert-overseer packet index = %d", insertOverseerIdx)
	}
	if objectIdx < 0 || objectIdx >= insertOverseerIdx {
		t.Fatalf("scene bootstrap current object index=%d op359=%d, want object before op359", objectIdx, insertOverseerIdx)
	}
	if actionTableIdx < 0 || actionTableIdx+1 != objectIdx {
		t.Fatalf("scene bootstrap action-table init index=%d object=%d, want op376 immediately before object", actionTableIdx, objectIdx)
	}
	if requestOverseerBeforeInsert != 5 {
		t.Fatalf("request-overseer packets before op359 = %d, want 5", requestOverseerBeforeInsert)
	}
}

func TestCurrentSceneModelLayerCandidateWindowIsAfterObjectFinalizer(t *testing.T) {
	objectIdx := -1
	insertOverseerIdx := -1
	sceneReadyIdx := -1
	op9Idx := -1
	for idx, packet := range longHengSceneBootstrapBeforeHudPackets {
		switch {
		case packet.kind == csharpCurrentSceneObjectListKind:
			objectIdx = idx
		case packet.msgID == uint16(dnfenum.CmdPacketInsertOverseer):
			insertOverseerIdx = idx
		case packet.msgID == uint16(dnfenum.CmdPacketReportClientSpec):
			sceneReadyIdx = idx
		case packet.kind == csharpCurrentSceneOp9ActorDisplayKind:
			op9Idx = idx
		}
	}
	if insertOverseerIdx < 0 || sceneReadyIdx < 0 || op9Idx < 0 {
		t.Fatalf("scene indexes object=%d op359=%d op124=%d op9=%d", objectIdx, insertOverseerIdx, sceneReadyIdx, op9Idx)
	}
	if objectIdx < 0 || !(objectIdx < insertOverseerIdx && insertOverseerIdx < sceneReadyIdx && sceneReadyIdx < op9Idx) {
		t.Fatalf("scene order object=%d op359=%d op124=%d op9=%d", objectIdx, insertOverseerIdx, sceneReadyIdx, op9Idx)
	}
}

func TestInsertOverseerFinalizerDefersUntilCurrentObjectStream(t *testing.T) {
	service := &Service{}
	packet := csharpSelectInitPacket{
		msgID: uint16(dnfenum.CmdPacketInsertOverseer),
		body:  buildCurrentInsertOverseerBody(),
	}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, connID: "game-test", channel: channelcatalog.Channel{ID: 19}, selectedCharacterID: 19}

	if err := service.sendCSharpSelectInitPacket(session, packet, packet.body); err != nil {
		t.Fatalf("defer op359 before current object stream: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("op359 before current object stream wrote %d bytes", conn.write.Len())
	}

	session.currentSceneObjectListSent = true
	if err := service.sendCSharpSelectInitPacket(session, packet, packet.body); err != nil {
		t.Fatalf("send op359 after current object stream: %v", err)
	}
	if conn.write.Len() == 0 {
		t.Fatalf("op359 after current object stream wrote no bytes")
	}
}

func TestCurrentSceneStageTransportUsesNeutralCurrentStruct(t *testing.T) {
	for _, packet := range longHengSceneBootstrapBeforeHudPackets {
		if packet.msgID != longHengCurrentSceneStageMsgID || packet.bodyCodec != "current_op1021_scene_state_transport_zlib" {
			continue
		}
		plain, err := zlibDecompress(packet.body)
		if err != nil {
			t.Fatalf("decompress current scene-stage body: %v", err)
		}
		if !bytes.Equal(plain, []byte{0}) {
			t.Fatalf("current scene-stage plain body = %x, want neutral selector 00", plain)
		}
		return
	}
	t.Fatalf("missing current scene-stage transport")
}

func TestCurrentSceneStageTransportDoesNotCarryOldObjectGraph(t *testing.T) {
	stagePackets := 0
	for _, packet := range longHengSceneBootstrapBeforeHudPackets {
		if packet.msgID != longHengCurrentSceneStageMsgID || packet.bodyCodec != "current_op1021_scene_state_transport_zlib" {
			continue
		}
		stagePackets++
		if len(packet.body) < 2 {
			t.Fatalf("scene-stage packet must stay zlib transport, got len=%d", len(packet.body))
		}
		if packet.body[0] != 0x78 || packet.body[1] != 0x9c {
			t.Fatalf("scene-stage packet must stay zlib transport, got prefix=%x", packet.body[:2])
		}
		plain, err := zlibDecompress(packet.body)
		if err != nil {
			t.Fatalf("decompress scene-stage transport: %v", err)
		}
		if !bytes.Equal(plain, []byte{0}) {
			t.Fatalf("scene-stage expanded body = %x, want neutral selector 00", plain)
		}
	}
	if stagePackets != 2 {
		t.Fatalf("scene-stage transport packet count = %d, want 2", stagePackets)
	}
}

func currentSceneObjectTailStartForTest(name string) int {
	return 5 + 0x47 + 2 + len(rosterDstrName(name))
}

func TestBuildCurrentSceneOp9ActorDisplayBodyUsesSafeMinimalRecord(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "60041",
		Name:        "hero",
		Job:         "11",
		Level:       86,
		Stats:       map[string]int64{"grow_type": 1},
	}
	body := buildCurrentSceneOp9ActorDisplayBody(currentSceneBootstrapObjectKey, character, true, "")
	name := rosterNameBytes("hero")
	wantLen := 34 + len(name)
	if len(body) != wantLen {
		t.Fatalf("current op9 body len = %d, want %d body=%x", len(body), wantLen, body)
	}
	if binary.LittleEndian.Uint16(body[0:2]) != 1 ||
		binary.LittleEndian.Uint16(body[2:4]) != currentSceneOp9StableSceneValue ||
		binary.LittleEndian.Uint16(body[4:6]) != currentSceneBootstrapObjectKey ||
		body[9] != currentSceneOp9ActorDisplayKind {
		t.Fatalf("current op9 header/record mismatch: %x", body[:10])
	}
	if body[10] != currentSceneObjectRoute || body[11] != 0 {
		t.Fatalf("current op9 route/name mode = %x", body[10:12])
	}
	if gotLen := binary.LittleEndian.Uint32(body[12:16]); gotLen != uint32(len(name)) {
		t.Fatalf("current op9 name len = %d, want %d", gotLen, len(name))
	}
	if !bytes.Equal(body[16:16+len(name)], name) {
		t.Fatalf("current op9 name bytes = %x, want %x", body[16:16+len(name)], name)
	}
	tail := body[16+len(name):]
	if tail[0] != 11 || tail[1] != 86 || tail[6] != 1 {
		t.Fatalf("current op9 job/level/grow tail = %x", tail[:7])
	}
	if binary.LittleEndian.Uint32(tail[2:6]) != 0 ||
		binary.LittleEndian.Uint16(tail[7:9]) != 0 ||
		binary.LittleEndian.Uint16(tail[11:13]) != 0 {
		t.Fatalf("current op9 unknown fields must stay zero: %x", tail)
	}
	if tail[13] != 0 {
		t.Fatalf("current op9 slot_count = %d, want 0 to avoid raw_len/read_raw branch", tail[13])
	}
	if !bytes.Equal(tail[14:17], []byte{0, 0, 0}) || tail[17] != 0 {
		t.Fatalf("current op9 post-slot/follow fields = %x, want zeros", tail[14:])
	}
}

func TestBuildCurrentSceneOp9NoopBodyUsesZeroRecordShape(t *testing.T) {
	body := buildCurrentSceneOp9NoopBody()
	if len(body) != 4 {
		t.Fatalf("current op9 noop body len = %d, want 4 body=%x", len(body), body)
	}
	if binary.LittleEndian.Uint16(body[0:2]) != 0 ||
		binary.LittleEndian.Uint16(body[2:4]) != currentSceneOp9StableSceneValue {
		t.Fatalf("current op9 noop body = %x, want record_count=0 scene_value=%d", body, currentSceneOp9StableSceneValue)
	}
}

func TestSelectedCharacterForEnterAttachesEquipmentSummary(t *testing.T) {
	repos := testRepositoryGroup()
	if err := repos.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "acc",
		Slot:        4,
		Name:        "hero",
		Job:         "11",
		Level:       1,
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 900001},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := &Service{repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true }}
	session := &gameSession{selectedCharacterID: 19}

	_, _, character, ok := service.selectedCharacterForEnter(context.Background(), session)
	if !ok {
		t.Fatalf("selected character not found")
	}
	rows := characterEquipSummary(character)
	if len(rows) != 1 || rows[0].Slot != 11 || rows[0].ItemIDOrIcon != 900001 {
		t.Fatalf("selected character equip summary = %+v", rows)
	}
}

func TestLongHengSceneSafeTailAfterHudEmitsOnlyVerifiedCurrentPassiveState(t *testing.T) {
	want := []uint16{
		currentSceneLocalStateMsgID,
		uint16(dnfenum.CmdPacketHardcoreCharacList),
	}
	packets := longHengSceneSafeTailAfterHudPackets
	if len(packets) != len(want) {
		t.Fatalf("safe tail after HUD count = %d, want %d", len(packets), len(want))
	}
	for idx, packet := range packets {
		if packet.class != 0 || packet.msgID != want[idx] {
			t.Fatalf("safe tail after HUD[%d] = class %d msg %d, want class 0 msg %d", idx, packet.class, packet.msgID, want[idx])
		}
		assertNoUnsafeProactiveReplayPacket(t, "safe tail after HUD", idx, packet.class, packet.msgID)
	}
	if got := packets[0].body; !bytes.Equal(got, []byte{0, 0}) {
		t.Fatalf("safe tail current op491 local state body = %x, want mode=0 state=0", got)
	}
	if packets[0].bodyEncoded || packets[0].marker != 0 || packets[0].bodyCodec != "" {
		t.Fatalf("safe tail current op491 must be a plain current body, got encoded=%v marker=%d codec=%q",
			packets[0].bodyEncoded, packets[0].marker, packets[0].bodyCodec)
	}
	if got := packets[1].body; !bytes.Equal(got, []byte{0}) {
		t.Fatalf("safe tail op647 body = %x, want current empty hardcore list count", got)
	}
	for idx, packet := range packets {
		switch packet.msgID {
		case uint16(dnfenum.CmdPacketMercenaryInfo),
			uint16(dnfenum.CmdPacketMercenaryCompetition),
			uint16(dnfenum.UpperMsgLoadExtendCharacs),
			uint16(dnfenum.UpperMsgCharacSlotExtendEffect):
			t.Fatalf("safe tail after HUD[%d] restored popup/passive packet %d", idx, packet.msgID)
		}
	}
	for _, excluded := range []uint16{
		uint16(dnfenum.CmdPacketSeriaRidableInHiddenTruthDungeon),
		uint16(dnfenum.CmdPacketLevelupSupport3rdEventGetItem),
		uint16(dnfenum.CmdPacketEventDnftrendGetReward),
		uint16(dnfenum.CmdPacketToBeZombie),
		uint16(dnfenum.CmdPacketModuleExist),
	} {
		for _, packet := range packets {
			if packet.msgID == excluded {
				t.Fatalf("safe tail reintroduced deferred opcode %d", excluded)
			}
		}
	}
}

func TestLongHengSceneTailLedgerCoversFullLongHengTail(t *testing.T) {
	ledger := longHengSceneTailAfterHudLedger()
	if len(ledger) != 56 {
		t.Fatalf("tail ledger count = %d, want DOVE idx92-147 count 56", len(ledger))
	}
	implemented := 0
	statusCounts := make(map[longHengScenePacketStatus]int)
	seen := make(map[int]longHengScenePacketLedgerEntry, len(ledger))
	for offset, entry := range ledger {
		wantIdx := 92 + offset
		if entry.idx != wantIdx {
			t.Fatalf("tail ledger[%d] idx = %d, want %d", offset, entry.idx, wantIdx)
		}
		if entry.phase != "07_scene_bootstrap_tail_social_guild" || entry.status == "" || entry.reason == "" {
			t.Fatalf("tail ledger[%d] incomplete entry: %+v", offset, entry)
		}
		if _, ok := seen[entry.idx]; ok {
			t.Fatalf("tail ledger duplicated idx %d", entry.idx)
		}
		seen[entry.idx] = entry
		statusCounts[entry.status]++
		if entry.implemented {
			implemented++
			if entry.status != longHengPacketImplementedCurrentBody {
				t.Fatalf("tail ledger[%d] implemented with status %q", offset, entry.status)
			}
		}
	}
	if implemented != len(longHengSceneSafeTailAfterHudPackets) {
		t.Fatalf("tail ledger implemented count = %d, want current safe tail count %d", implemented, len(longHengSceneSafeTailAfterHudPackets))
	}
	for _, idx := range []int{131, 133, 145, 147} {
		if _, ok := seen[idx]; !ok {
			t.Fatalf("tail ledger missing DOVE idx %d that is not represented in current Go tail spec", idx)
		}
	}
	for _, idx := range []int{103, 104, 105, 106} {
		entry, ok := seen[idx]
		if !ok {
			t.Fatalf("tail ledger missing op1023 idx %d", idx)
		}
		if entry.status != longHengPacketRequestDriven ||
			entry.reason != "current_op1023_collectbox_result_no_body_reader_old_body8_incompatible" {
			t.Fatalf("tail ledger idx %d op1023 verdict = %s/%q", idx, entry.status, entry.reason)
		}
	}
	for idx := 107; idx <= 120; idx++ {
		entry, ok := seen[idx]
		if !ok {
			t.Fatalf("tail ledger missing op1199 idx %d", idx)
		}
		if entry.status != longHengPacketNotUsedCurrentClient ||
			entry.reason != "current_op1199_has_no_registered_handler_old_body16_incompatible" {
			t.Fatalf("tail ledger idx %d op1199 verdict = %s/%q", idx, entry.status, entry.reason)
		}
	}
	if entry, ok := seen[121]; !ok {
		t.Fatalf("tail ledger missing op1089 idx 121")
	} else if entry.status != longHengPacketNotUsedCurrentClient ||
		entry.reason != "current_op1089_registered_to_DoNothing_old_body24_incompatible" {
		t.Fatalf("tail ledger idx 121 op1089 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[122]; !ok {
		t.Fatalf("tail ledger missing op1145 idx 122")
	} else if entry.status != longHengPacketNotUsedCurrentClient ||
		entry.reason != "current_op1145_has_no_registered_handler_old_body8_incompatible" {
		t.Fatalf("tail ledger idx 122 op1145 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[123]; !ok {
		t.Fatalf("tail ledger missing op825 idx 123")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op825_territory_alliance_request_driven_old_body32_incompatible" {
		t.Fatalf("tail ledger idx 123 op825 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[124]; !ok {
		t.Fatalf("tail ledger missing op647 idx 124")
	} else if entry.status != longHengPacketImplementedCurrentBody ||
		entry.reason != "current_op647_empty_hardcore_list_body_sent" {
		t.Fatalf("tail ledger idx 124 op647 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[125]; !ok {
		t.Fatalf("tail ledger missing op562 idx 125")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op562_fatigue_acceleration_state_request_driven_old_body32_incompatible" {
		t.Fatalf("tail ledger idx 125 op562 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[126]; !ok {
		t.Fatalf("tail ledger missing op761 idx 126")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op761_buy_guild_contents_result_no_body_reader_request_driven_old_body8_incompatible" {
		t.Fatalf("tail ledger idx 126 op761 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[127]; !ok {
		t.Fatalf("tail ledger missing op808 idx 127")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op808_warroom_reward_result_request_driven_old_body8_incompatible" {
		t.Fatalf("tail ledger idx 127 op808 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[128]; !ok {
		t.Fatalf("tail ledger missing op633 idx 128")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op633_avatar_spec_event_request_driven_old_body16_incompatible" {
		t.Fatalf("tail ledger idx 128 op633 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[129]; !ok {
		t.Fatalf("tail ledger missing op428 idx 129")
	} else if entry.status != longHengPacketNotUsedCurrentClient ||
		entry.reason != "current_op428_has_no_registered_handler_old_body16_incompatible" {
		t.Fatalf("tail ledger idx 129 op428 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[130]; !ok {
		t.Fatalf("tail ledger missing op707 idx 130")
	} else if entry.status != longHengPacketNotUsedCurrentClient ||
		entry.reason != "current_op707_has_no_registered_handler_old_body8_incompatible" {
		t.Fatalf("tail ledger idx 130 op707 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[131]; !ok {
		t.Fatalf("tail ledger missing op760 idx 131")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op760_guild_alliance_state_request_driven_old_body16_incompatible" {
		t.Fatalf("tail ledger idx 131 op760 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[132]; !ok {
		t.Fatalf("tail ledger missing op1264 idx 132")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op1264_lucky_balloon_ui_result_request_driven_old_body16_incompatible" {
		t.Fatalf("tail ledger idx 132 op1264 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[133]; !ok {
		t.Fatalf("tail ledger missing op1301 idx 133")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op1301_guild_hongbao_point_list_request_driven_old_body8_incompatible" {
		t.Fatalf("tail ledger idx 133 op1301 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[134]; !ok {
		t.Fatalf("tail ledger missing op1316 idx 134")
	} else if entry.status != longHengPacketNotUsedCurrentClient ||
		entry.reason != "current_op1316_has_no_registered_handler_old_body32_incompatible" {
		t.Fatalf("tail ledger idx 134 op1316 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[135]; !ok {
		t.Fatalf("tail ledger missing op1237 idx 135")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op1237_card_game_result_request_driven_old_body24_incompatible" {
		t.Fatalf("tail ledger idx 135 op1237 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[136]; !ok {
		t.Fatalf("tail ledger missing op413 idx 136")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op413_title_book_get_request_driven_old_body8_incompatible" {
		t.Fatalf("tail ledger idx 136 op413 verdict = %s/%q", entry.status, entry.reason)
	}
	for idx := 137; idx <= 143; idx++ {
		entry, ok := seen[idx]
		if !ok {
			t.Fatalf("tail ledger missing op12 idx %d", idx)
		}
		if entry.status != longHengPacketRequestDriven ||
			entry.reason != "current_op12_reads_u8_u16_u8_wstr_party_ui_state_old_dove_body_incompatible" {
			t.Fatalf("tail ledger idx %d op12 verdict = %s/%q", idx, entry.status, entry.reason)
		}
	}
	if entry, ok := seen[144]; !ok {
		t.Fatalf("tail ledger missing op53 idx 144")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op53_pvp_ready_state_request_driven_old_body16_incompatible" {
		t.Fatalf("tail ledger idx 144 op53 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[145]; !ok {
		t.Fatalf("tail ledger missing class1 op442 idx 145")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_class1_op442_mercenary_competition_no_body_reader_request_driven_old_body48_incompatible" {
		t.Fatalf("tail ledger idx 145 class1 op442 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[146]; !ok {
		t.Fatalf("tail ledger missing op1292 idx 146")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_op1292_joust_betting_request_driven_old_body16_incompatible" {
		t.Fatalf("tail ledger idx 146 op1292 verdict = %s/%q", entry.status, entry.reason)
	}
	if entry, ok := seen[147]; !ok {
		t.Fatalf("tail ledger missing class1 op140 idx 147")
	} else if entry.status != longHengPacketRequestDriven ||
		entry.reason != "current_class1_op140_large_guild_member_list_request_driven_old_body15962_incompatible" {
		t.Fatalf("tail ledger idx 147 class1 op140 verdict = %s/%q", entry.status, entry.reason)
	}
	if statusCounts[longHengPacketPendingCurrentStruct] != 0 || statusCounts[longHengPacketRequestDriven] == 0 {
		t.Fatalf("tail ledger should have no pending markers and keep request-driven debt visible, counts=%v", statusCounts)
	}
}

func TestHandleUpperSelectCharacterSendsCSharpInitStream(t *testing.T) {
	repos := testRepositoryGroup()
	repos.Character.(*fakeCharacterStore).records = map[string]dnfrepo.CharacterRecord{
		"60041": {CharacterID: "60041", AccountID: defaultAccountPrefix + "1", Slot: 12, Name: "hero", Job: "0", Level: 1, Stats: map[string]int64{"town_id": 1, "area_id": 0, currentDungeonTutorialCompletedKey: 0}},
	}
	if err := repos.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "60041",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    101010912,
				Extra: map[string]string{
					"model_layer_count":  "1",
					"model_layer_0_key":  "2150",
					"model_layer_0_name": "at_katanaa",
				},
			},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service := &Service{
		options:            options{accountPrefix: defaultAccountPrefix},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:            conn,
		channel:         channelcatalog.Channel{ID: 16, Type: 1, Name: "ch.16", Port: 10016},
		residentChannel: channelcatalog.Channel{ID: 16, Type: 1, Name: "ch.16", Port: 10016},
		rosterRequested: true,
	}
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, 60041)
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgSelectCharacter), request, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build upper select request: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle upper select: %v", err)
	}
	template := expandedCSharpUpperSelectInitTemplate()
	expected := make([]csharpSelectInitPacket, 0, len(template))
	for _, packet := range template {
		if packet.kind == "userinfo" || packet.kind == "select_deferred" {
			continue
		}
		if !selectInitPacketAllowedBeforeScene(packet) {
			continue
		}
		expected = append(expected, packet)
	}
	rest := conn.write.Bytes()
	longHengBootstrapPackets := 0
	longHengSceneStagePackets := 0
	longHengLargeSceneStagePacket := 0
	longHengSceneManagerInitPackets := 0
	longHengSceneReadyTriggers := 0
	longHengRequestOverseerPackets := 0
	longHengInsertOverseerPackets := 0
	currentSceneObjectListPackets := 0
	currentActionTableStatePackets := 0
	currentSceneMode1AfterObjectPackets := 0
	currentSceneOp9ActorDisplayPackets := 0
	selectSceneUserInfoPackets := 0
	selectSceneMainHudPackets := 0
	longHengTailAfterHudPackets := 0
	var firstMainHudBody []byte
	sceneObjectKey := currentSceneActorObjectKey(60041)
	firstWant := expected[0]
	firstPacket, rest := splitGameServerUpperPacket(t, rest)
	if firstPacket.Header.Classification != firstWant.class || firstPacket.Header.MsgID != firstWant.msgID {
		t.Fatalf("select init ack packet = class %d msg %d", firstPacket.Header.Classification, firstPacket.Header.MsgID)
	}
	assertSelectCharacterAckBody(t, firstPacket.Body, 60041)
	if got, ok := currentSelectAckSelectedSlotFromBody(firstPacket.Body); !ok || got != 12 {
		t.Fatalf("select init ack selected slot = %d ok=%v, want 12 true", got, ok)
	}
	rest = requireCurrentStoryDigestLastLevelPacket(t, rest, 0)
	enterPacket, rest := splitGameServerUpperPacket(t, rest)
	if enterPacket.Header.Classification != dnfproto.DefaultChannelClassification ||
		enterPacket.Header.MsgID != uint16(dnfenum.CmdPacketEnterSelectDungeon) {
		t.Fatalf("select init tutorial-enter packet = class %d msg %d", enterPacket.Header.Classification, enterPacket.Header.MsgID)
	}
	if wantBody := upperSuccessBody(buildEnterSelectDungeonAckBody()); !bytes.Equal(enterPacket.Body, wantBody) {
		t.Fatalf("select init tutorial-enter body = %x, want %x", enterPacket.Body, wantBody)
	}
	areaPacket, rest := splitGameServerUpperPacket(t, rest)
	if areaPacket.Header.Classification != 0 ||
		areaPacket.Header.MsgID != currentFatigueMsgID ||
		!bytes.Equal(areaPacket.Body, buildCurrentFatigueBody(dnfrepo.CharacterRecord{}, false)) {
		t.Fatalf("select init fatigue packet = class %d msg %d body=%x", areaPacket.Header.Classification, areaPacket.Header.MsgID, areaPacket.Body)
	}
	for page := 0; page < currentSceneOverseerPageCount; page++ {
		previewPage, next := splitGameServerUpperPacket(t, rest)
		if previewPage.Header.Classification != 0 || previewPage.Header.MsgID != uint16(dnfenum.CmdPacketRequestOverseer) {
			t.Fatalf("select preview page[%d] = class %d msg %d body=%x", page, previewPage.Header.Classification, previewPage.Header.MsgID, previewPage.Body)
		}
		rest = next
	}
	previewAction, rest := splitGameServerUpperPacket(t, rest)
	if previewAction.Header.Classification != 0 || previewAction.Header.MsgID != uint16(dnfenum.CmdPacketPVPMissionHpPercent) ||
		!bytes.Equal(previewAction.Body, buildCurrentActionTableStateBody()) {
		t.Fatalf("select preview action table = class %d msg %d body=%x", previewAction.Header.Classification, previewAction.Header.MsgID, previewAction.Body)
	}
	previewObject, rest := splitGameServerUpperPacket(t, rest)
	previewObjectKey := currentSceneActorObjectKey(60041)
	previewOwner := byte(currentSceneObjectContext)
	if previewObject.Header.Classification != 0 || previewObject.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(previewObject.Body) < 0x4e || previewObject.Body[0] != 0 ||
		previewObject.Body[3] != currentSceneObjectRoute ||
		previewObject.Body[4] != previewOwner ||
		binary.LittleEndian.Uint16(previewObject.Body[0x4c:0x4e]) != previewObjectKey {
		t.Fatalf("select preview mode0 = class %d msg %d key=%#x body=%x", previewObject.Header.Classification, previewObject.Header.MsgID, previewObjectKey, previewObject.Body)
	}
	previewBinding, rest := splitGameServerUpperPacket(t, rest)
	if previewBinding.Header.Classification != 0 || previewBinding.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(previewBinding.Body) != currentMode1BaseWireSize || previewBinding.Body[0] != 1 ||
		previewBinding.Body[3] != currentSceneObjectRoute || previewBinding.Body[4] != previewOwner ||
		binary.LittleEndian.Uint16(previewBinding.Body[0x15:0x17]) != previewObjectKey ||
		previewBinding.Body[currentMode1CreateCountOffset] != 0 ||
		previewBinding.Body[currentMode1CreateRowsOffset+6] != 0 {
		t.Fatalf("select preview binding mode1 = class %d msg %d key=%#x body=%x", previewBinding.Header.Classification, previewBinding.Header.MsgID, previewObjectKey, previewBinding.Body)
	}
	previewFinalizer, rest := splitGameServerUpperPacket(t, rest)
	if previewFinalizer.Header.Classification != 0 || previewFinalizer.Header.MsgID != uint16(dnfenum.CmdPacketInsertOverseer) {
		t.Fatalf("select preview finalizer = class %d msg %d body=%x", previewFinalizer.Header.Classification, previewFinalizer.Header.MsgID, previewFinalizer.Body)
	}
	previewObjectState, rest := splitLongHengGameServerUpperPacket(t, rest)
	if previewObjectState.Header.Classification != 0 || previewObjectState.Header.MsgID != currentClearQuestListMsgID {
		t.Fatalf("select preview object state = class %d msg %d body=%x", previewObjectState.Header.Classification, previewObjectState.Header.MsgID, previewObjectState.Body)
	}
	previewObjectStatePlain, err := zlibDecompress(previewObjectState.Body)
	if err != nil || len(previewObjectStatePlain) != 30004 ||
		binary.LittleEndian.Uint32(previewObjectStatePlain[:4]) != 30000 {
		t.Fatalf("select preview op356 body_len=%d plain_len=%d err=%v", len(previewObjectState.Body), len(previewObjectStatePlain), err)
	}
	if int(previewObjectKey) < 30000 && previewObjectStatePlain[4+int(previewObjectKey)] != 0 {
		t.Fatalf("select preview op356 wrote actor key %#x into clear-quest list", previewObjectKey)
	}
	previewCommit, rest := splitGameServerUpperPacket(t, rest)
	if previewCommit.Header.Classification != 0 || previewCommit.Header.MsgID != uint16(dnfenum.CmdPacketReportClientSpec) || len(previewCommit.Body) != 0 {
		t.Fatalf("select preview op124 = class %d msg %d body=%x", previewCommit.Header.Classification, previewCommit.Header.MsgID, previewCommit.Body)
	}
	if !session.selectPreviewObjectStateSent || session.selectPreviewActorRemoved || session.selectedUserInfoRefreshSent {
		t.Fatalf("select preview flags sent=%v removed=%v mode1=%v", session.selectPreviewObjectStateSent, session.selectPreviewActorRemoved, session.selectedUserInfoRefreshSent)
	}
	for idx, want := range expected[1:] {
		logicalIdx := idx + 1
		var packet dnfproto.ChannelPacket
		packet, rest = splitCSharpSelectInitPacket(t, rest, want)
		if packet.Header.Classification != want.class || packet.Header.MsgID != want.msgID {
			t.Fatalf("select init[%d] packet = class %d msg %d", logicalIdx, packet.Header.Classification, packet.Header.MsgID)
		}
		if want.kind == "select_scene_userinfo" {
			if currentSceneObjectListPackets != 0 {
				t.Fatalf("select init[%d] sent selected userinfo occurrence %d after current scene object list", logicalIdx, want.occurrence)
			}
			wantBody := service.buildCSharpSelectedUserInfoBody(context.Background(), session, repos.LegacyUserInfo, want.occurrence, dnfrepo.CharacterRecord{
				CharacterID: "60041",
				AccountID:   defaultAccountPrefix + "1",
				Slot:        12,
				Name:        "hero",
				Job:         "0",
				Level:       1,
			}, true, 60041, "hero")
			if packet.Header.Classification != 0 ||
				packet.Header.MsgID != uint16(dnfenum.UpperMsgCharacterRoster) ||
				!bytes.Equal(packet.Body, wantBody) {
				t.Fatalf("select init[%d] selected userinfo occurrence %d mismatch: class=%d msg=%d len=%d body=%x want=%x",
					logicalIdx, want.occurrence, packet.Header.Classification, packet.Header.MsgID, len(packet.Body), packet.Body, wantBody)
			}
			selectSceneUserInfoPackets++
		}
		if want.kind == csharpLongHengSceneBootstrapKind {
			longHengBootstrapPackets++
			wantBody := want.body
			dynamicClearQuestList := want.msgID == currentClearQuestListMsgID && want.bodyCodec == currentClearQuestListTransportCodec
			if dynamicClearQuestList {
				wantBody = buildCurrentPassGateObjectTransportBody()
			}
			if !bytes.Equal(packet.Body, wantBody) {
				t.Fatalf("select init[%d] DOVE scene body mismatch: msg=%d len=%d want_len=%d", logicalIdx, packet.Header.MsgID, len(packet.Body), len(wantBody))
			}
			if packet.Header.MsgID == longHengCurrentSceneStageMsgID {
				longHengSceneStagePackets++
				if !want.bodyEncoded ||
					want.marker != 1 ||
					want.bodyCodec != "current_op1021_scene_state_transport_zlib" ||
					len(packet.Body) < 2 ||
					packet.Body[0] != 0x78 ||
					packet.Body[1] != 0x9c {
					t.Fatalf("select init[%d] scene-stage transport = encoded=%v marker=%d codec=%q len=%d body=%x", logicalIdx, want.bodyEncoded, want.marker, want.bodyCodec, len(packet.Body), packet.Body)
				}
				plain, err := zlibDecompress(packet.Body)
				if err != nil || !bytes.Equal(plain, []byte{0}) {
					t.Fatalf("select init[%d] current scene-stage transport plain=%x err=%v", logicalIdx, plain, err)
				}
			}
		}
		if want.kind == csharpCurrentActionTableStateKind {
			if packet.Header.Classification != 0 ||
				packet.Header.MsgID != uint16(dnfenum.CmdPacketPVPMissionHpPercent) ||
				!bytes.Equal(packet.Body, buildCurrentActionTableStateBody()) {
				t.Fatalf("select init[%d] current action-table state packet mismatch: class=%d msg=%d body=%x",
					logicalIdx, packet.Header.Classification, packet.Header.MsgID, packet.Body)
			}
			currentActionTableStatePackets++
		}
		if want.kind == csharpCurrentSceneObjectListKind {
			if packet.Header.Classification != 0 ||
				packet.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
				len(packet.Body) < 0x50 ||
				packet.Body[0] != 0 ||
				binary.LittleEndian.Uint16(packet.Body[1:3]) != 1 ||
				packet.Body[3] != currentSceneObjectRoute ||
				packet.Body[4] != currentSceneObjectContext ||
				binary.LittleEndian.Uint16(packet.Body[0x4c:0x4e]) != sceneObjectKey {
				t.Fatalf("select init[%d] current scene object list body mismatch: class=%d msg=%d len=%d body=%x", logicalIdx, packet.Header.Classification, packet.Header.MsgID, len(packet.Body), packet.Body)
			}
			currentSceneObjectListPackets++
			rest = assertSelectedSceneUserInfoRefresh(t, service, session, rest)
			currentSceneMode1AfterObjectPackets++
		}
		if want.kind == csharpCurrentSceneOp9ActorDisplayKind {
			wantBody := buildCurrentSceneOp9ActorDisplayBody(sceneObjectKey, dnfrepo.CharacterRecord{
				CharacterID: "60041",
				AccountID:   defaultAccountPrefix + "1",
				Slot:        12,
				Name:        "hero",
				Job:         "0",
				Level:       1,
			}, true, "hero")
			if packet.Header.Classification != 0 ||
				packet.Header.MsgID != uint16(dnfenum.CmdPacketRecoverStamina) ||
				!bytes.Equal(packet.Body, wantBody) {
				t.Fatalf("select init[%d] current op9 actor/display body mismatch: class=%d msg=%d len=%d body=%x want=%x", logicalIdx, packet.Header.Classification, packet.Header.MsgID, len(packet.Body), packet.Body, wantBody)
			}
			if got := packet.Body[33+len(rosterNameBytes("hero"))]; got != 0 {
				t.Fatalf("select init[%d] current op9 follow_mode = %d, want 0", logicalIdx, got)
			}
			currentSceneOp9ActorDisplayPackets++
		}
		if want.kind == "select_scene_main_hud" {
			selectSceneMainHudPackets++
			if firstMainHudBody == nil {
				firstMainHudBody = append([]byte(nil), packet.Body...)
			}
			if packet.Header.MsgID != longHengCurrentSceneMainHudInfoMsgID || len(packet.Body) != 16 || !bytes.Equal(packet.Body, want.body) {
				t.Fatalf("select init[%d] main hud marker = msg %d body %x", logicalIdx, packet.Header.MsgID, packet.Body)
			}
		}
		if want.kind == csharpLongHengSceneTailAfterHudKind {
			longHengTailAfterHudPackets++
			if !bytes.Equal(packet.Body, want.body) {
				t.Fatalf("select init[%d] DOVE tail body mismatch: msg=%d len=%d want_len=%d", logicalIdx, packet.Header.MsgID, len(packet.Body), len(want.body))
			}
		}
		if want.kind != csharpLongHengSceneBootstrapKind {
			assertNoUnsafeProactiveReplayPacket(t, "select init", logicalIdx, packet.Header.Classification, packet.Header.MsgID)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketLeaveParty) && want.kind == csharpLongHengSceneBootstrapKind && len(packet.Body) == 40 {
			t.Fatalf("select init[%d] replayed an old op13 item-list transport", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketWalkoutPartyMember) && want.kind == csharpLongHengSceneBootstrapKind {
			t.Fatalf("select init[%d] replayed an old op14 item-update transport", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketReport4Hack) && want.kind == csharpLongHengSceneBootstrapKind {
			t.Fatalf("select init[%d] replayed an old op108 report transport", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketSetPartyInfo) {
			t.Fatalf("select init[%d] sent party/chat panel packet", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketLevelupSupport3rdEventGetItem) && want.kind == csharpLongHengSceneTailAfterHudKind {
			if !want.bodyEncoded || want.marker != 1 || want.bodyCodec != "current_op1004_levelup_support_transport_zlib" ||
				len(packet.Body) != 92 || packet.Body[0] != 0x78 || packet.Body[1] != 0x9c {
				t.Fatalf("select init[%d] op1004 transport = encoded=%v marker=%d codec=%q len=%d", logicalIdx, want.bodyEncoded, want.marker, want.bodyCodec, len(packet.Body))
			}
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketEventDnftrendGetReward) && want.kind == csharpLongHengSceneTailAfterHudKind {
			if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
				len(packet.Body) != 88 || packet.Body[0] != 1 || packet.Body[1] != 1 || packet.Body[86] != 0xcc || packet.Body[87] != 0xcc {
				t.Fatalf("select init[%d] class1 op901 body len=%d class=%d", logicalIdx, len(packet.Body), packet.Header.Classification)
			}
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketToBeZombie) {
			if want.kind != csharpLongHengSceneTailAfterHudKind || len(packet.Body) != 4 || binary.LittleEndian.Uint32(packet.Body) != 0 {
				t.Fatalf("select init[%d] op542 body len=%d kind=%q body=%x", logicalIdx, len(packet.Body), want.kind, packet.Body)
			}
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketModuleExist) {
			if want.kind != csharpLongHengSceneTailAfterHudKind || len(packet.Body) != 24 || !bytes.Equal(packet.Body[20:], []byte{0xcc, 0xcc, 0xcc, 0xcc}) {
				t.Fatalf("select init[%d] op581 body len=%d kind=%q", logicalIdx, len(packet.Body), want.kind)
			}
		}
		if packet.Header.MsgID == 0x0069 && want.kind != csharpLongHengSceneBootstrapKind {
			t.Fatalf("select init[%d] sent deferred pre-scene state packet", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketDecreaseDurability) {
			t.Fatalf("select init[%d] sent pre-scene durability packet", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketBuyItem) {
			t.Fatalf("select init[%d] sent pre-scene buy-item packet", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketCallGuildCreateRight) {
			t.Fatalf("select init[%d] sent pre-scene guild-create-right packet", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketRequestAvagachaCoupon) && want.kind == csharpLongHengSceneBootstrapKind {
			if !bytes.Equal(packet.Body, []byte{0}) {
				t.Fatalf("select init[%d] op104 body = %x, want current empty-list count=0", logicalIdx, packet.Body)
			}
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketCancelIntegratedMatching) && want.kind == csharpLongHengSceneBootstrapKind {
			if !bytes.Equal(packet.Body, buildCurrentInfiniteDifficultyCharacInfoBody()) {
				t.Fatalf("select init[%d] msg521 body = %x, want current state=0", logicalIdx, packet.Body)
			}
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketPurifyItem) && want.kind == csharpLongHengSceneBootstrapKind {
			if !bytes.Equal(packet.Body, buildCurrentJoinPowerBody()) {
				t.Fatalf("select init[%d] msg204 body = %x, want current join_state=0", logicalIdx, packet.Body)
			}
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketAlldieMonster) {
			t.Fatalf("select init[%d] sent disproved decimal op138 quest packet", logicalIdx)
		}
		if packet.Header.MsgID == currentAcceptableQuestListMsgID && want.kind == currentAcceptableQuestListKind {
			if len(packet.Body) < 4 || int(binary.LittleEndian.Uint32(packet.Body[:4])) != len(packet.Body)-4 {
				t.Fatalf("select init[%d] op0x15 protobuf length mismatch body=%x", logicalIdx, packet.Body)
			}
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketHtIs) && want.kind == csharpLongHengSceneBootstrapKind {
			if !bytes.Equal(packet.Body, buildCurrentDecreaseDurabilityBody()) {
				t.Fatalf("select init[%d] msg281 body = %x, want current neutral durability structure", logicalIdx, packet.Body)
			}
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketJoinPower) {
			t.Fatalf("select init[%d] sent pre-scene join-power packet", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketRequestInfiniteDifficultyCharacInfo) {
			t.Fatalf("select init[%d] sent pre-scene infinite-difficulty packet", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketCancelCargoPad) {
			t.Fatalf("select init[%d] replayed old op391 cargo transport", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketApproveJoinGuild) {
			t.Fatalf("select init[%d] replayed old op350 guild config transport", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketCancelJoinGuild) {
			t.Fatalf("select init[%d] replayed old op349 guild state", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketHatchCreatureEgg) && want.kind == csharpLongHengSceneBootstrapKind {
			t.Fatalf("select init[%d] sent scene bootstrap old391 as current op173; op173 triggers current NoPack config refresh before scene state is ready", logicalIdx)
		}
		if packet.Header.MsgID == currentClearQuestListMsgID {
			if want.kind != csharpLongHengSceneBootstrapKind ||
				!want.bodyEncoded ||
				want.marker != 1 ||
				want.bodyCodec != currentClearQuestListTransportCodec ||
				len(packet.Body) < 2 ||
				packet.Body[0] != 0x78 ||
				packet.Body[1] != 0x9c {
				t.Fatalf("select init[%d] op356 clear-quest list must use current zlib transport, got kind=%q encoded=%v marker=%d codec=%q len=%d", logicalIdx, want.kind, want.bodyEncoded, want.marker, want.bodyCodec, len(packet.Body))
			}
			plain, err := zlibDecompress(packet.Body)
			if err != nil {
				t.Fatalf("select init[%d] op356 clear-quest list zlib: %v", logicalIdx, err)
			}
			if len(plain) != 4+30000 {
				t.Fatalf("select init[%d] op356 clear-quest list expanded len=%d", logicalIdx, len(plain))
			}
			rawLen := binary.LittleEndian.Uint32(plain[:4])
			if rawLen != 30000 {
				t.Fatalf("select init[%d] op356 clear-quest list rawLen=%d, want 30000", logicalIdx, rawLen)
			}
			if int(sceneObjectKey) < int(rawLen) && plain[4+int(sceneObjectKey)] != 0 {
				t.Fatalf("select init[%d] op356 wrote actor key %#x into clear-quest list", logicalIdx, sceneObjectKey)
			}
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketAdvanceAltarStageClearInfo) {
			t.Fatalf("select init[%d] sent old op9 op947 placeholder after current op9 builder was added", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketDiePVPCharacter) {
			t.Fatalf("select init[%d] sent active shared-effect packet op55", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketAuctionRegistItem) {
			t.Fatalf("select init[%d] sent active DOVE shared-effect sample op183", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketReportClientSpec) {
			if want.kind != csharpLongHengSceneBootstrapKind || len(packet.Body) != 0 {
				t.Fatalf("select init[%d] report-client-spec outside DOVE scene-ready: kind=%q len=%d", logicalIdx, want.kind, len(packet.Body))
			}
			longHengSceneReadyTriggers++
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketRequestOverseer) {
			if want.kind != csharpLongHengSceneBootstrapKind ||
				len(packet.Body) != 11 ||
				packet.Body[0] != 0 ||
				binary.LittleEndian.Uint16(packet.Body[1:3]) != 0 ||
				binary.LittleEndian.Uint32(packet.Body[7:11]) != 0 {
				t.Fatalf("select init[%d] op358 current empty body mismatch: kind=%q len=%d body=%x", logicalIdx, want.kind, len(packet.Body), packet.Body)
			}
			listIndex := binary.LittleEndian.Uint32(packet.Body[3:7])
			if listIndex != uint32(longHengRequestOverseerPackets) {
				t.Fatalf("select init[%d] op358 list index = %d, want %d", logicalIdx, listIndex, longHengRequestOverseerPackets)
			}
			longHengRequestOverseerPackets++
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketInsertOverseer) {
			expectedInsertOverseerBody := buildCurrentInsertOverseerBody()
			if want.kind != csharpLongHengSceneBootstrapKind || !bytes.Equal(packet.Body, expectedInsertOverseerBody) {
				t.Fatalf("select init[%d] op359 current body mismatch: kind=%q len=%d body=%x", logicalIdx, want.kind, len(packet.Body), packet.Body)
			}
			longHengInsertOverseerPackets++
		}
		if packet.Header.MsgID == uint16(dnfenum.UpperMsgCharacSlotExtendEffect) {
			t.Fatalf("select init[%d] sent slot extend effect packet", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.UpperMsgLoadExtendCharacs) {
			t.Fatalf("select init[%d] sent character slot unlock query packet", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketMercenaryInfo) || packet.Header.MsgID == uint16(dnfenum.CmdPacketMercenaryCompetition) {
			t.Fatalf("select init[%d] sent mercenary modal packet %d", logicalIdx, packet.Header.MsgID)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketGuildAllMemberList) {
			t.Fatalf("select init[%d] sent passive guild member list response without client request", logicalIdx)
		}
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketGetGuildHongbaoPointList) {
			t.Fatalf("select init[%d] sent guild hongbao panel packet", logicalIdx)
		}
		if logicalIdx == 1 {
			if want.kind != csharpLongHengSceneBootstrapKind ||
				packet.Header.MsgID != uint16(dnfenum.CmdPacketCreateCharacter) ||
				len(packet.Body) != 8 ||
				!bytes.Equal(packet.Body, longHengSceneBootstrapBeforeHudPackets[0].body) {
				t.Fatalf("select init op5 body = msg %d %x", packet.Header.MsgID, packet.Body)
			}
		}
	}
	rest = assertNoActiveFinishLoadingPacket(t, rest, "finish loading gate")
	for len(rest) != 0 {
		var packet dnfproto.ChannelPacket
		packet, rest = splitGameServerUpperPacket(t, rest)
		if packet.Header.Classification == 0 && packet.Header.MsgID == uint16(dnfenum.CmdPacketSetUDPIPPort) {
			t.Fatalf("unexpected current msg2 after scene bootstrap body_len=%d body=%x", len(packet.Body), packet.Body)
		}
		switch packet.Header.MsgID {
		case uint16(dnfenum.CmdPacketLeaveParty):
		case uint16(dnfenum.CmdPacketWalkoutPartyMember):
		default:
			t.Fatalf("unexpected trailing packet class=%d msg=%d body_len=%d", packet.Header.Classification, packet.Header.MsgID, len(packet.Body))
		}
	}
	allowedLongHengBootstrapPackets := 0
	for _, packet := range longHengSceneBootstrapBeforeHudPackets {
		if packet.kind == csharpLongHengSceneBootstrapKind && selectInitPacketAllowedBeforeScene(packet) {
			allowedLongHengBootstrapPackets++
		}
	}
	if longHengBootstrapPackets != allowedLongHengBootstrapPackets {
		t.Fatalf("initial DOVE scene bootstrap packet count = %d, want %d", longHengBootstrapPackets, allowedLongHengBootstrapPackets)
	}
	if longHengSceneStagePackets != 0 || longHengLargeSceneStagePacket != 0 || longHengSceneManagerInitPackets != 0 ||
		longHengSceneReadyTriggers != 0 || longHengRequestOverseerPackets != 0 || currentActionTableStatePackets != 0 ||
		longHengInsertOverseerPackets != 0 || currentSceneMode1AfterObjectPackets != 0 || currentSceneObjectListPackets != 0 ||
		currentSceneOp9ActorDisplayPackets != 0 || selectSceneUserInfoPackets != 0 ||
		selectSceneMainHudPackets != 0 || longHengTailAfterHudPackets != 0 || firstMainHudBody != nil {
		t.Fatalf("select ACK leaked scene state before op29: stage=%d large=%d managers=%d ready=%d pages=%d action=%d insert=%d mode1=%d mode0=%d op9=%d userinfo=%d hud=%d tail=%d",
			longHengSceneStagePackets, longHengLargeSceneStagePacket, longHengSceneManagerInitPackets, longHengSceneReadyTriggers,
			longHengRequestOverseerPackets, currentActionTableStatePackets, longHengInsertOverseerPackets,
			currentSceneMode1AfterObjectPackets, currentSceneObjectListPackets, currentSceneOp9ActorDisplayPackets,
			selectSceneUserInfoPackets, selectSceneMainHudPackets, longHengTailAfterHudPackets)
	}
	if !session.sceneBootstrapTailDeferred || session.sceneBootstrapTailSent {
		t.Fatalf("scene tail flags after head: deferred=%v sent=%v", session.sceneBootstrapTailDeferred, session.sceneBootstrapTailSent)
	}

	conn.write.Reset()
	ceraFrame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgFollowUpStatus), nil, 1, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build cera probe: %v", err)
	}
	if err := service.handleGameUpper(session, ceraFrame); err != nil {
		t.Fatalf("handle cera probe: %v", err)
	}
	rest = conn.write.Bytes()
	if len(rest) != 0 {
		ceraAck, next := splitGameServerUpperPacket(t, rest)
		if ceraAck.Header.Classification != dnfproto.DefaultChannelClassification ||
			ceraAck.Header.MsgID != uint16(dnfenum.UpperMsgFollowUpStatus) ||
			!bytes.Equal(ceraAck.Body, []byte{1}) {
			t.Fatalf("cera ack = class %d msg %d body %x", ceraAck.Header.Classification, ceraAck.Header.MsgID, ceraAck.Body)
		}
		if len(next) != 0 {
			t.Fatalf("cera probe sent unexpected scene tail bytes: %d", len(next))
		}
	}
	if !session.sceneBootstrapTailDeferred || session.sceneBootstrapTailSent {
		t.Fatalf("scene tail flags after cera probe: deferred=%v sent=%v", session.sceneBootstrapTailDeferred, session.sceneBootstrapTailSent)
	}

	conn.write.Reset()
	blacklistFrame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgFollowUpReady), nil, 1, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build blacklist probe: %v", err)
	}
	if err := service.handleGameUpper(session, blacklistFrame); err != nil {
		t.Fatalf("handle blacklist probe: %v", err)
	}
	rest = conn.write.Bytes()
	if len(rest) != 0 {
		blacklistAck, next := splitGameServerUpperPacket(t, rest)
		if blacklistAck.Header.Classification != dnfproto.DefaultChannelClassification ||
			blacklistAck.Header.MsgID != uint16(dnfenum.UpperMsgFollowUpReady) ||
			!bytes.Equal(blacklistAck.Body, currentRequestBlacklistResponseBody) {
			t.Fatalf("blacklist ack = class %d msg %d body %x", blacklistAck.Header.Classification, blacklistAck.Header.MsgID, blacklistAck.Body)
		}
		if len(next) != 0 {
			t.Fatalf("blacklist probe sent runtime seed before op29 = %d", len(next))
		}
		if !session.sceneBootstrapTailDeferred || session.sceneBootstrapTailSent || session.runtimeAfterBlacklistSent {
			t.Fatalf("scene tail flags after blacklist probe: deferred=%v sent=%v", session.sceneBootstrapTailDeferred, session.sceneBootstrapTailSent)
		}
	}

	conn.write.Reset()
	gateFrame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCheckUserConnection), nil, 1, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build check-user-connection gate: %v", err)
	}
	if err := service.handleGameUpper(session, gateFrame); err != nil {
		t.Fatalf("handle check-user-connection gate: %v", err)
	}
	rest = conn.write.Bytes()
	if len(rest) != 0 {
		gateAck, next := splitGameServerUpperPacket(t, rest)
		if gateAck.Header.Classification != dnfproto.DefaultChannelClassification ||
			gateAck.Header.MsgID != uint16(dnfenum.UpperMsgCheckUserConnection) ||
			!bytes.Equal(gateAck.Body, upperSuccessBody(buildCurrentCheckUserConnectionSuccessPayload())) {
			t.Fatalf("scene gate ack = class %d msg %d body %x", gateAck.Header.Classification, gateAck.Header.MsgID, gateAck.Body)
		}
		if len(next) != 0 {
			t.Fatalf("scene gate sent unexpected deferred bytes: %d", len(next))
		}
	}
	if !session.sceneBootstrapTailDeferred || session.sceneBootstrapTailSent {
		t.Fatalf("scene tail flags after gate: deferred=%v sent=%v", session.sceneBootstrapTailDeferred, session.sceneBootstrapTailSent)
	}
}

func TestUpperGetUserinfoBoot(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
	request := make([]byte, 8)
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgGetUserInfo), request, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build get-userinfo upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle get-userinfo upper: %v", err)
	}
	roster, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if roster.Header.Classification != 0 || roster.Header.MsgID != uint16(dnfenum.UpperMsgCharacterRoster) ||
		!bytes.Equal(roster.Body, buildCSharpEmptyRosterBody()) {
		t.Fatalf("get-userinfo roster header=%+v body=%x", roster.Header, roster.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("get-userinfo must only send roster; unexpected trailing bytes: %d", len(rest))
	}
	if session.pendingCharacterRosterBootstrap {
		t.Fatal("get-userinfo must not mark the roster pending")
	}
}

func TestHandleUpperMercenaryInfoRejectsMalformedBeforeSelect(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgMercenaryInfo), make([]byte, 8), 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build mercenary info upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle mercenary info upper: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if packet.Header.MsgID != uint16(dnfenum.UpperMsgMercenaryInfo) ||
		!bytes.Equal(packet.Body, []byte{0, currentAdventureGenericFailureCode}) ||
		len(rest) != 0 {
		t.Fatalf("pre-select mercenary rejection header=%+v body=%x rest=%x", packet.Header, packet.Body, rest)
	}
}

func TestHandleUpperMercenaryInfoRejectsMalformedAfterSelect(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		channel:             channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038},
		selectedCharacterID: 12,
	}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgMercenaryInfo), make([]byte, 8), 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build mercenary info upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle mercenary info upper: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if packet.Header.MsgID != uint16(dnfenum.UpperMsgMercenaryInfo) ||
		!bytes.Equal(packet.Body, []byte{0, currentAdventureGenericFailureCode}) ||
		len(rest) != 0 {
		t.Fatalf("post-select mercenary rejection header=%+v body=%x rest=%x", packet.Header, packet.Body, rest)
	}
}

func TestHandleUpperMercenaryCompetitionRejectsMalformedState(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgMercenaryCompetition), nil, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build mercenary competition upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle mercenary competition upper: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if packet.Header.MsgID != uint16(dnfenum.UpperMsgMercenaryCompetition) ||
		!bytes.Equal(packet.Body, []byte{0, currentAdventureGenericFailureCode}) ||
		len(rest) != 0 {
		t.Fatalf("mercenary competition rejection header=%+v body=%x rest=%x", packet.Header, packet.Body, rest)
	}
}

func TestHandleUpperDprotoCallbackRepliesEmptySuccess(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgDprotoCallback), make([]byte, 112), 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build dproto callback upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle dproto callback upper: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("dproto callback emitted trailing bytes: %x", rest)
	}
	wantBody := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.UpperMsgDprotoCallback) ||
		!bytes.Equal(packet.Body, wantBody) {
		t.Fatalf("dproto callback response = class=%d msg=%d body=%x, want body=%x", packet.Header.Classification, packet.Header.MsgID, packet.Body, wantBody)
	}
}

func TestHandleUpperDprotoCallbackNativeAllowsOpaqueIdx6Control(t *testing.T) {
	service := &Service{options: options{gameUpperClientBodyCodec: gameUpperClientBodyCodecNative}}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}
	body := bytes.Repeat([]byte{0xA5, 0x6C, 0x33, 0x90, 0x12, 0xFE, 0x44, 0x08}, 14)
	msgID := uint16(dnfenum.UpperMsgDprotoCallback)
	if msgID%14 != 6 {
		t.Fatalf("test fixture msg id %d moved away from idx6", msgID)
	}
	frame, err := dnfproto.BuildChannelPacket(msgID, body, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build native opaque dproto callback upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle native opaque dproto callback upper: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("native opaque dproto callback emitted trailing bytes: %x", rest)
	}
	wantBody := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != msgID ||
		!bytes.Equal(packet.Body, wantBody) {
		t.Fatalf("native opaque dproto callback response = class=%d msg=%d body=%x, want body=%x", packet.Header.Classification, packet.Header.MsgID, packet.Body, wantBody)
	}
}

func TestShouldAllowOpaqueUpperClientBodyControlIsNarrow(t *testing.T) {
	if !shouldAllowOpaqueUpperClientBodyControl(uint16(dnfenum.UpperMsgDprotoCallback), dnfproto.DefaultChannelClassification, false, nil) {
		t.Fatalf("op1518 idx6 unsupported control should be allowed as opaque")
	}
	if shouldAllowOpaqueUpperClientBodyControl(uint16(dnfenum.UpperMsgDprotoCallback), dnfproto.DefaultChannelClassification, true, nil) {
		t.Fatalf("supported codec should not use opaque fallback")
	}
	if shouldAllowOpaqueUpperClientBodyControl(uint16(dnfenum.UpperMsgDprotoCallback), 0, false, nil) {
		t.Fatalf("non-default classification should not use opaque fallback")
	}
	if shouldAllowOpaqueUpperClientBodyControl(6, dnfproto.DefaultChannelClassification, false, nil) {
		t.Fatalf("arbitrary idx6 opcode should not use opaque fallback")
	}
	if shouldAllowOpaqueUpperClientBodyControl(uint16(dnfenum.UpperMsgDprotoCallback), dnfproto.DefaultChannelClassification, false, errUpperNativeCodecBody) {
		t.Fatalf("decode errors should not use opaque fallback")
	}
}

func TestHandleUpperAntibotDefersClientReport(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgAntibot), make([]byte, 32), 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build antibot upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle antibot upper: %v", err)
	}
	if got := conn.write.Len(); got != 0 {
		t.Fatalf("antibot client report must not send unverified 1516 response, wrote %d bytes", got)
	}
}

func TestHandleUpperCharacViewHiddenInfoRepliesRebirthHardcoreSuccess(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCharacViewHiddenInfo), nil, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build hidden charac info upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle hidden charac info upper: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("hidden charac info emitted trailing bytes: %x", rest)
	}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.UpperMsgRebirthHardcoreCharac) {
		t.Fatalf("hidden charac info response = class=%d msg=%d body=%x", packet.Header.Classification, packet.Header.MsgID, packet.Body)
	}
	if !bytes.Equal(packet.Body, []byte{1}) {
		t.Fatalf("hidden charac info success body = %x, want 01", packet.Body)
	}
}

func TestHandleUpperCheckUserConnectionDefersUnverifiedAck(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCheckUserConnection), nil, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build check-user-connection upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle check-user-connection upper: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("check-user-connection emitted unverified ack bytes: %x", conn.write.Bytes())
	}
}

func TestHandleUpperCheckUserConnectionAcksAfterSelect(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		channel:             channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019},
		selectedCharacterID: 60041,
	}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCheckUserConnection), nil, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build check-user-connection upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle check-user-connection upper: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("check-user-connection emitted trailing bytes: %x", rest)
	}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.UpperMsgCheckUserConnection) ||
		!bytes.Equal(packet.Body, upperSuccessBody(buildCurrentCheckUserConnectionSuccessPayload())) {
		t.Fatalf("check-user-connection response = class=%d msg=%d body=%x", packet.Header.Classification, packet.Header.MsgID, packet.Body)
	}
}

func TestInitialNoneNotice(t *testing.T) {
	service := &Service{options: options{gameInitialMode: gameInitialModeNone}}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}}

	mode, bodyLen, sent, err := service.sendGameInitial(session)
	if err != nil {
		t.Fatalf("send initial: %v", err)
	}
	if mode != gameInitialModeUpper || !sent || bodyLen == 0 {
		t.Fatalf("mode=%q sent=%v bodyLen=%d", mode, sent, bodyLen)
	}
	assertUpperInitialWire(t, conn.write.Bytes(), service.gameUpperHeaderSize(), upperSuccessBody(service.buildLoginSuccess(session.channel)), true)
}

func TestLegacyGameEndpointACKIsWriteSilent(t *testing.T) {
	service := &Service{options: options{serverIP: "42.240.165.245"}}
	channel := channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channel}

	if _, _, _, err := service.sendGameInitial(session); err != nil {
		t.Fatalf("send endpoint bootstrap: %v", err)
	}
	bootstrapLen := conn.write.Len()
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.GameTypeLogin),
		nil,
	); err != nil {
		t.Fatalf("handle legacy endpoint ACK: %v", err)
	}
	if conn.write.Len() != bootstrapLen {
		t.Fatalf("legacy endpoint ACK added %d bytes", conn.write.Len()-bootstrapLen)
	}
}

func TestHandleGameGetUserInfoSendsCurrentCharacterSelectRoster(t *testing.T) {
	repos := testRepositoryGroup()
	if err := repos.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID:            "dnf:1",
		State:                "active",
		RepresentAccountName: "group",
	}); err != nil {
		t.Fatal(err)
	}
	repos.Character.(*fakeCharacterStore).records["1"] = dnfrepo.CharacterRecord{
		CharacterID: "1",
		AccountID:   "dnf:1",
		Slot:        0,
		Name:        "hero",
		Job:         "1",
		Level:       1,
	}
	service := &Service{
		options:             options{accountPrefix: "dnf:"},
		characterStats:      testCharacterStatTable(t),
		adventureGroupTable: loadAdventureGroupTestTables(t),
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 6, Type: 3, Name: "ch.6", Port: 10006}}
	frame, err := dnfproto.BuildLatestGameTCP(
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.GameTypeGetUserInfo),
		nil,
		dnfproto.TransportOptions{},
	)
	if err != nil {
		t.Fatalf("build get userinfo frame: %v", err)
	}

	if err := service.handleGameFrame(session, frame); err != nil {
		t.Fatalf("handle get userinfo: %v", err)
	}
	roster, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if roster.Header.Classification != 0 || roster.Header.MsgID != uint16(dnfenum.UpperMsgCharacterRoster) {
		t.Fatalf("legacy GET_USERINFO first packet header=%+v, want roster", roster.Header)
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes after legacy GET_USERINFO roster: %x", rest)
	}
	if session.pendingCharacterRosterBootstrap {
		t.Fatal("legacy GET_USERINFO should not leave pending character roster bootstrap")
	}
	assertCSharpRosterBody(t, roster.Body, 1)
	conn.write.Reset()

	hiddenFrame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCharacViewHiddenInfo), nil, 7, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build hidden charac info frame: %v", err)
	}
	if err := service.handleGameUpper(session, hiddenFrame); err != nil {
		t.Fatalf("handle hidden charac info after get userinfo: %v", err)
	}
	rebirth, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if rebirth.Header.Classification != dnfproto.DefaultChannelClassification ||
		rebirth.Header.MsgID != uint16(dnfenum.UpperMsgRebirthHardcoreCharac) ||
		!bytes.Equal(rebirth.Body, []byte{1}) {
		t.Fatalf("hidden charac info ack = header=%+v body=%x", rebirth.Header, rebirth.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes after independent hidden-charac probe ack: %x", rest)
	}
	if session.pendingCharacterRosterBootstrap {
		t.Fatal("hidden-charac probe should not leave pending character roster bootstrap")
	}
}

func TestHandleUpperCreateCharacterWritesRepositoryAndRefreshesList(t *testing.T) {
	repos := testRepositoryGroup()
	service := &Service{
		options:        options{accountPrefix: "dnf:"},
		characterStats: testCharacterStatTable(t),
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	prepareTestCharacterInitialization(service, 15)
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 6, Type: 3, Name: "ch.6", Port: 10006}}

	if err := service.handleUpperCreateCharacter(session, buildCreateRequest(15, "hero")); err != nil {
		t.Fatalf("handle upper create: %v", err)
	}
	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantAckBody := []byte{0x01, 0x01, 0x00, 0x04, 0x00, 0x00, 0x00, 'h', 'e', 'r', 'o'}
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.UpperMsgCreateCharacter) ||
		!bytes.Equal(ack.Body, wantAckBody) {
		t.Fatalf("upper create ack = %+v body=%x want %x", ack.Header, ack.Body, wantAckBody)
	}
	roster, rest := splitGameServerUpperPacket(t, rest)
	if roster.Header.Classification != 0 ||
		roster.Header.MsgID != uint16(dnfenum.UpperMsgCharacterRoster) {
		t.Fatalf("upper create roster = %+v body=%x", roster.Header, roster.Body)
	}
	assertCSharpRosterBody(t, roster.Body, 1)
	if len(rest) != 0 {
		t.Fatalf("unexpected upper create trailing bytes: %x", rest)
	}
	record, ok := repos.Character.(*fakeCharacterStore).records["1"]
	if !ok {
		t.Fatalf("repository missing created character")
	}
	if record.Name != "hero" || record.Job != "15" || record.Level != 1 || record.Slot != 0 {
		t.Fatalf("created character base fields = %+v", record)
	}
	if record.Stats == nil {
		t.Fatalf("created character should write SQL mirror stats")
	}
	for key, want := range map[string]int64{
		"grow_type":             0,
		"delete_flag":           0,
		"pc_room_id":            0x00010001,
		"user_state_bits":       3,
		"return_user_flag":      1,
		"channel_id":            2,
		"stat_hp_max":           11000,
		"stat_mp_max":           11800,
		"stat_block_marker":     83,
		"roster_card_flag":      0,
		"roster_display_flags":  0,
		"create_option_len":     0,
		"create_option_byte_00": 0,
	} {
		if got := record.Stats[key]; got != want {
			t.Fatalf("created stat %s = %d, want %d; stats=%+v", key, got, want, record.Stats)
		}
	}
	if record.Location != (dnfrepo.CharacterLocation{}) {
		t.Fatalf("created character should not write location_json: %+v", record.Location)
	}
	if !reflect.DeepEqual(record.Roster, dnfrepo.CharacterRoster{}) {
		t.Fatalf("created character should not write roster_json: %+v", record.Roster)
	}
	if !session.rosterRequested || session.emptyRosterSlotProbePending ||
		session.pendingCharacterRosterBootstrap {
		t.Fatalf(
			"created roster lifecycle roster=%t empty_probe=%t pending=%t",
			session.rosterRequested,
			session.emptyRosterSlotProbePending,
			session.pendingCharacterRosterBootstrap,
		)
	}
}

func TestFirstCreatedCharacterClearsSpeculativeReconnectBeforeSelection(t *testing.T) {
	repos := testRepositoryGroup()
	service := &Service{
		options:        options{accountPrefix: "dnf:"},
		characterStats: testCharacterStatTable(t),
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	prepareTestCharacterInitialization(service, 15)
	conn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 6, Type: 3, Name: "ch.6", Port: 10006}
	session := &gameSession{
		conn:                            conn,
		channel:                         channel,
		residentChannel:                 channel,
		pendingCharacterRosterBootstrap: true,
		emptyRosterSlotProbePending:     true,
	}
	armCurrentChannelReconnect(session)

	if err := service.handleUpperCreateCharacter(
		session,
		buildCreateRequest(15, "first-hero"),
	); err != nil {
		t.Fatalf("create first character: %v", err)
	}
	if session.channelReconnect || !session.rosterRequested ||
		session.pendingCharacterRosterBootstrap ||
		session.emptyRosterSlotProbePending {
		t.Fatalf(
			"post-create lifecycle reconnect=%t roster=%t pending=%t empty_probe=%t",
			session.channelReconnect,
			session.rosterRequested,
			session.pendingCharacterRosterBootstrap,
			session.emptyRosterSlotProbePending,
		)
	}

	conn.write.Reset()
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, 1)
	frame, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgSelectCharacter),
		request,
		0,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("select first created character: %v", err)
	}
	if session.channelReconnect || session.selectedCharacterID != 1 {
		t.Fatalf(
			"first selection lifecycle reconnect=%t selected=%d",
			session.channelReconnect,
			session.selectedCharacterID,
		)
	}
}

func TestHandleUpperCreateCharacterAllowsSeventeenthCharacterWithThirtyTwoSlots(t *testing.T) {
	if defaultCharacterSlots != 32 {
		t.Fatalf("default character slots = %d, want 32", defaultCharacterSlots)
	}
	repos := testRepositoryGroup()
	store := repos.Character.(*fakeCharacterStore)
	for slot := 0; slot < 16; slot++ {
		id := strconv.Itoa(slot + 1)
		store.records[id] = dnfrepo.CharacterRecord{
			CharacterID: id,
			AccountID:   "dnf:1",
			Slot:        slot,
			Name:        "existing-" + id,
			Job:         "0",
			Level:       1,
		}
	}
	store.nextID = 17
	service := &Service{
		options: options{accountPrefix: "dnf:"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	prepareTestCharacterInitialization(service, 15)
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}

	if err := service.handleUpperCreateCharacter(session, buildCreateRequest(15, "slot-seventeen")); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.UpperMsgCreateCharacter) || len(ack.Body) < 7 ||
		ack.Body[0] != 1 || binary.LittleEndian.Uint16(ack.Body[1:3]) != 17 ||
		binary.LittleEndian.Uint32(ack.Body[3:7]) != 14 {
		t.Fatalf("seventeenth create ack = %+v body=%x", ack.Header, ack.Body)
	}
	roster, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 {
		t.Fatalf("seventeenth create trailing bytes = %x", trailing)
	}
	assertCSharpRosterBody(t, roster.Body, 17)
	if got := binary.LittleEndian.Uint16(roster.Body[3:5]); got != noPackRosterWireSlotLimit {
		t.Fatalf("roster wire slot capacity = %d, want %d", got, noPackRosterWireSlotLimit)
	}
	created, ok := store.records["17"]
	if !ok || created.Slot != 16 || created.Name != "slot-seventeen" {
		t.Fatalf("seventeenth character = %+v found=%t", created, ok)
	}
}

func TestHandleUpperCreateCharacterUsesSlot31ThenRejectsThirtyThirdCharacter(t *testing.T) {
	repos := testRepositoryGroup()
	store := repos.Character.(*fakeCharacterStore)
	characters := make([]dnfrepo.CharacterRecord, 0, 31)
	for slot := 0; slot < 31; slot++ {
		id := strconv.Itoa(slot + 1)
		record := dnfrepo.CharacterRecord{
			CharacterID: id,
			AccountID:   "dnf:1",
			Slot:        slot,
			Name:        "existing-" + id,
			Job:         "0",
			Level:       1,
		}
		store.records[id] = record
		characters = append(characters, record)
	}
	store.nextID = 32
	service := &Service{
		options: options{accountPrefix: "dnf:"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	prepareTestCharacterInitialization(service, 15)
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}

	if err := service.handleUpperCreateCharacter(session, buildCreateRequest(15, "slot-thirty-two")); err != nil {
		t.Fatal(err)
	}
	_, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	roster, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 {
		t.Fatalf("thirty-second create trailing bytes = %x", trailing)
	}
	assertCSharpRosterBody(t, roster.Body, noPackRosterWireSlotLimit)
	created, ok := store.records["32"]
	if !ok || created.Slot != 31 {
		t.Fatalf("thirty-second character = %+v found=%t, want slot 31", created, ok)
	}

	conn.write.Reset()
	if err := service.handleUpperCreateCharacter(session, buildCreateRequest(15, "slot-thirty-three")); err != nil {
		t.Fatal(err)
	}
	full, fullTrailing := splitGameServerUpperPacket(t, conn.write.Bytes())
	if full.Header.MsgID != uint16(dnfenum.UpperMsgCreateCharacter) ||
		!bytes.Equal(full.Body, []byte{0, createCodeSlotFull}) || len(fullTrailing) != 0 {
		t.Fatalf("thirty-third create response=%+v body=%x trailing=%x", full.Header, full.Body, fullTrailing)
	}
	if _, exists := store.records["33"]; exists {
		t.Fatal("slot-full create wrote character 33")
	}
}

func TestHandleUpperCreateCharacterErrorUsesCSharpCreateAck(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 6, Type: 3, Name: "ch.6", Port: 10006}}

	if err := service.handleUpperCreateCharacter(session, []byte{0x01}); err != nil {
		t.Fatalf("handle invalid upper create: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("unexpected invalid create trailing bytes: %x", rest)
	}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.UpperMsgCreateCharacter) ||
		!bytes.Equal(packet.Body, []byte{0x00, 0x04}) {
		t.Fatalf("invalid upper create ack = %+v body=%x", packet.Header, packet.Body)
	}
}

func TestHandleUpperCreateCharacterIgnoresSoftDeletedNameAndSlot(t *testing.T) {
	repos := testRepositoryGroup()
	store := repos.Character.(*fakeCharacterStore)
	store.records["1"] = dnfrepo.CharacterRecord{
		CharacterID: "1",
		AccountID:   "dnf:1",
		Slot:        0,
		Name:        "hero",
		Job:         "0",
		Level:       86,
	}
	store.records["2"] = dnfrepo.CharacterRecord{
		CharacterID: "2",
		AccountID:   "dnf:1",
		Slot:        1,
		Name:        "gone",
		Job:         "14",
		Level:       1,
		Stats:       map[string]int64{"delete_flag": 1},
	}
	store.nextID = 3
	service := &Service{
		options: options{accountPrefix: "dnf:"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	prepareTestCharacterInitialization(service, 14)
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 6, Type: 3, Name: "ch.6", Port: 10006}}

	if err := service.handleUpperCreateCharacter(session, buildCreateRequest(14, "gone")); err != nil {
		t.Fatalf("handle upper create with soft-deleted name: %v", err)
	}
	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantAckBody := []byte{0x01, 0x03, 0x00, 0x04, 0x00, 0x00, 0x00, 'g', 'o', 'n', 'e'}
	if ack.Header.MsgID != uint16(dnfenum.UpperMsgCreateCharacter) || !bytes.Equal(ack.Body, wantAckBody) {
		t.Fatalf("upper create ack = %+v body=%x, want success", ack.Header, ack.Body)
	}
	roster, rest := splitGameServerUpperPacket(t, rest)
	if len(rest) != 0 {
		t.Fatalf("unexpected upper create trailing bytes: %x", rest)
	}
	assertCSharpRosterBody(t, roster.Body, 2)
	record, ok := store.records["3"]
	if !ok {
		t.Fatalf("repository missing created character")
	}
	if record.Name != "gone" || record.Slot != 1 {
		t.Fatalf("created character = %+v, want name gone slot 1", record)
	}
}

func TestHandleUpperCreateCharacterDecodesGB18030Name(t *testing.T) {
	repos := testRepositoryGroup()
	service := &Service{
		options: options{accountPrefix: "dnf:"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	prepareTestCharacterInitialization(service, 15)
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}
	body := append([]byte{15, 7, 0, 0, 0}, []byte{0xc7, 0xbf, 0xd6, 0xc6, '9', '4', '8'}...)

	if err := service.handleUpperCreateCharacter(session, body); err != nil {
		t.Fatalf("handle upper create gb18030 name: %v", err)
	}
	record, ok := repos.Character.(*fakeCharacterStore).records["1"]
	if !ok {
		t.Fatalf("repository missing created character")
	}
	if record.Name != "强制948" {
		t.Fatalf("character name = %q, want 强制948", record.Name)
	}
}

func TestDecodeCharacterNamePrioritizesCurrentClientGB18030WhenBytesAreValidUTF8(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{
			name: "three_hanzi_cyrillic_utf8_shape",
			raw:  []byte{0xd2, 0xa1, 0xd2, 0xbb, 0xd2, 0xa1},
			want: "\u6447\u4e00\u6447",
		},
		{
			name: "two_hanzi_latin_utf8_shape",
			raw:  []byte{0xc2, 0xa5, 0xc5, 0xb6},
			want: "\u697c\u54e6",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := decodeCharacterName(test.raw)
			if err != nil {
				t.Fatalf("decode current-client name: %v", err)
			}
			if got != test.want {
				t.Fatalf("decoded name = %q, want %q", got, test.want)
			}
			if encoded := rosterNameBytes(got); !bytes.Equal(encoded, test.raw) {
				t.Fatalf("roster name bytes = %x, want original %x", encoded, test.raw)
			}
		})
	}
}

func TestHandleUpperCreateCharacterDecodesNoPackProtectedBody(t *testing.T) {
	body := []byte{
		0x3b, 0x3a, 0xd1, 0x48, 0xd1, 0xbf, 0x63, 0x70,
		0xc9, 0xb1, 0x34, 0xfd, 0x51, 0xbc, 0x93, 0x82,
		0x6b, 0xf5, 0x88, 0x9f, 0x80, 0xf0, 0xdb, 0xcc,
	}
	plain, err := decodeUpperKey5(body)
	if err != nil {
		t.Fatalf("decode protected upper create: %v", err)
	}
	wantPlain := []byte{
		0x00, 0x04, 0x00, 0x00, 0x00, 0xce, 0xd2, 0xd2,
		0xb2, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	if !bytes.Equal(plain, wantPlain) {
		t.Fatalf("decoded body = %x, want %x", plain, wantPlain)
	}

	repos := testRepositoryGroup()
	service := &Service{
		options: options{accountPrefix: "dnf:"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	prepareTestCharacterInitialization(service, 0)
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}

	if err := service.handleUpperCreateCharacter(session, body); err != nil {
		t.Fatalf("handle protected upper create: %v", err)
	}
	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantAckBody := []byte{0x01, 0x01, 0x00, 0x04, 0x00, 0x00, 0x00, 0xce, 0xd2, 0xd2, 0xb2}
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.UpperMsgCreateCharacter) ||
		!bytes.Equal(ack.Body, wantAckBody) {
		t.Fatalf("protected create ack = %+v body=%x want %x", ack.Header, ack.Body, wantAckBody)
	}
	roster, rest := splitGameServerUpperPacket(t, rest)
	if roster.Header.Classification != 0 ||
		roster.Header.MsgID != uint16(dnfenum.UpperMsgCharacterRoster) {
		t.Fatalf("protected create roster = %+v body=%x", roster.Header, roster.Body)
	}
	assertCSharpRosterBody(t, roster.Body, 1)
	if len(rest) != 0 {
		t.Fatalf("unexpected protected create trailing bytes: %x", rest)
	}
	if got := len(repos.Character.(*fakeCharacterStore).records); got != 1 {
		t.Fatalf("protected create wrote %d records", got)
	}
}

func TestParseNoPackProtectedSelectCharacterBody(t *testing.T) {
	body := []byte{
		0xa7, 0xc1, 0x2b, 0x5a, 0xc5, 0xf5, 0xe2, 0xf0,
		0xce, 0x15, 0xe9, 0xe5, 0xd7, 0x53, 0xa3, 0xe1,
	}
	plain, err := decodeUpperKey4(body)
	if err != nil {
		t.Fatalf("decode protected upper select: %v", err)
	}
	wantPlain := []byte{
		0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	if !bytes.Equal(plain, wantPlain) {
		t.Fatalf("decoded select body = %x, want %x", plain, wantPlain)
	}
	parsed := parseSelectCharacterRequest(body)
	if parsed.slot != 4 || !parsed.clear || parsed.source != "nopack_key4_slot" {
		t.Fatalf("parsed protected select = %+v, want slot 4 key4", parsed)
	}
}

func TestResolveSelectedCharacterUsesRequestedRosterSlot(t *testing.T) {
	body := []byte{
		0xa7, 0xc1, 0x2b, 0x5a, 0xc5, 0xf5, 0xe2, 0xf0,
		0xce, 0x15, 0xe9, 0xe5, 0xd7, 0x53, 0xa3, 0xe1,
	}
	repos := testRepositoryGroup()
	repos.Character.(*fakeCharacterStore).records = map[string]dnfrepo.CharacterRecord{
		"1":  {CharacterID: "1", AccountID: defaultAccountPrefix + "1", Slot: 0, Name: "hero", Job: "0", Level: 70},
		"8":  {CharacterID: "8", AccountID: defaultAccountPrefix + "1", Slot: 1, Name: "second", Job: "1", Level: 2},
		"10": {CharacterID: "10", AccountID: defaultAccountPrefix + "1", Slot: 2, Name: "third", Job: "4", Level: 3},
		"12": {CharacterID: "12", AccountID: defaultAccountPrefix + "1", Slot: 4, Name: "female_sword", Job: "15", Level: 1},
	}
	service := &Service{
		options:            options{accountPrefix: defaultAccountPrefix},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
	}

	charID, charName, selected, hasCharacter, slot := service.resolveSelectedCharacter(context.Background(), &gameSession{}, body)
	if !hasCharacter {
		t.Fatalf("resolve protected select did not load a character")
	}
	if charID != 12 || charName != "female_sword" || selected.Slot != 4 || selected.Job != "15" || selected.Level != 1 || slot != 4 {
		t.Fatalf("resolved character id=%d name=%q slot=%d job=%q level=%d return_slot=%d", charID, charName, selected.Slot, selected.Job, selected.Level, slot)
	}
}

func TestResolveNoPackPlainSelectDoesNotUseFallbackCharID(t *testing.T) {
	body := []byte{
		0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	repos := testRepositoryGroup()
	repos.Character.(*fakeCharacterStore).records = map[string]dnfrepo.CharacterRecord{
		"1":  {CharacterID: "1", AccountID: defaultAccountPrefix + "1", Slot: 0, Name: "slot0", Job: "0", Level: 70},
		"8":  {CharacterID: "8", AccountID: defaultAccountPrefix + "1", Slot: 1, Name: "slot1", Job: "1", Level: 2},
		"10": {CharacterID: "10", AccountID: defaultAccountPrefix + "1", Slot: 2, Name: "slot2", Job: "4", Level: 3},
		"11": {CharacterID: "11", AccountID: defaultAccountPrefix + "1", Slot: 3, Name: "slot3", Job: "5", Level: 4},
		"12": {CharacterID: "12", AccountID: defaultAccountPrefix + "1", Slot: 4, Name: "female_sword", Job: "15", Level: 1},
	}
	service := &Service{
		options:            options{accountPrefix: defaultAccountPrefix},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
	}

	charID, charName, selected, hasCharacter, slot := service.resolveSelectedCharacter(context.Background(), &gameSession{}, body)
	if !hasCharacter {
		t.Fatalf("resolve plain select did not load a character")
	}
	if charID != 12 || charName != "female_sword" || selected.Slot != 4 || selected.Job != "15" || slot != 4 {
		t.Fatalf("plain select resolved id=%d name=%q slot=%d job=%q return_slot=%d", charID, charName, selected.Slot, selected.Job, slot)
	}
}

func assertSelectCharacterAckBody(t *testing.T, body []byte, wantCharacterID uint16) {
	t.Helper()
	if len(body) < currentSelectAckMinimumBodyLen {
		headLen := len(body)
		if headLen > 32 {
			headLen = 32
		}
		t.Fatalf("select init ack body len=%d, want at least %d head=%x", len(body), currentSelectAckMinimumBodyLen, body[:headLen])
	}
	if body[currentSelectAckResultOffset] != 1 {
		t.Fatalf("select init ack success = %x, want 01", body[0])
	}
	if got, ok := currentSelectAckCharacterID(body); !ok || got != wantCharacterID {
		t.Fatalf("select init ack character id = %d ok=%v, want %d true", got, ok, wantCharacterID)
	}
	if body[currentSelectAckPremiumCountOffset] != 0 {
		t.Fatalf("select init ack premium count = %#x, want typed empty list", body[currentSelectAckPremiumCountOffset])
	}
}

func TestBuildCurrentSelectCharacterAckUsesCurrentTypedBody(t *testing.T) {
	createdAt := time.Unix(1783660048, 0).UTC()
	character := dnfrepo.CharacterRecord{
		CreatedAt: createdAt,
		Stats: map[string]int64{
			"town_id":                1,
			"fatigue":                156,
			"fatigue_limit":          156,
			"fatigue_update":         3,
			"fatigue_display_update": 4,
		},
	}
	body := buildCurrentSelectCharacterAckBody(character, true, dnfrepo.QuestRecord{}, false, 28, 13, 0, []byte{0})
	assertSelectCharacterAckBody(t, body, 28)
	wantLen := currentSelectAckMinimumBodyLen
	if len(body) != wantLen {
		t.Fatalf("select init ack body len = %d, want current no-premium body %d", len(body), wantLen)
	}
	if got := binary.LittleEndian.Uint32(body[currentSelectAckCreatedTimeOffset : currentSelectAckCreatedTimeOffset+4]); got != uint32(createdAt.Unix()) {
		t.Fatalf("select init ack created time = %d, want %d", got, createdAt.Unix())
	}
	if got := binary.LittleEndian.Uint16(body[currentSelectAckFatigueUsedOffset : currentSelectAckFatigueUsedOffset+2]); got != 0 {
		t.Fatalf("select init ack fatigue used = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint16(body[currentSelectAckFatigueLimitOffset : currentSelectAckFatigueLimitOffset+2]); got != 156 {
		t.Fatalf("select init ack fatigue limit = %d, want 156", got)
	}
	if got := binary.LittleEndian.Uint16(body[currentSelectAckFatigueAuxOffset : currentSelectAckFatigueAuxOffset+2]); got != 3 {
		t.Fatalf("select init ack fatigue aux = %d, want 3", got)
	}
	questOffset, ok := currentSelectAckQuestLayout(body)
	if !ok {
		t.Fatalf("select init ack quest layout not readable")
	}
	if got := binary.LittleEndian.Uint16(body[questOffset : questOffset+2]); got != 0xffff {
		t.Fatalf("select init ack first pair id = %#x, want 0xffff", got)
	}
	state, stateOffset, ok := currentSelectAckIntermediateState(body)
	if !ok || stateOffset != 206 {
		t.Fatalf("select init ack sub_1A0C3E0 state offset = %d ok=%v, want 206 true", stateOffset, ok)
	}
	if state != [currentSelectAckStateU32Count]uint32{} {
		t.Fatalf("select init ack sub_1A0C3E0 state = %#v, want four initialized zeros", state)
	}
	if got := body[stateOffset+currentSelectAckStateByteSize]; got != 13 {
		t.Fatalf("select init ack roster key at offset 222 = %#x, want 0x0d", got)
	}
	if got := binary.LittleEndian.Uint16(body[len(body)-6 : len(body)-4]); got != 4 {
		t.Fatalf("select init ack fatigue display = %d, want 4", got)
	}
	if got, ok := currentSelectAckSelectedSlotFromBody(body); !ok || got != 13 {
		t.Fatalf("select init ack selected slot = %d ok=%v, want 13 true", got, ok)
	}
	if flag, count, ok := currentSelectAckTutorialState(body); !ok || flag != 0 || count != 0 {
		t.Fatalf("select init ack tutorial state = flag=%d count=%d ok=%v, want 0 0 true", flag, count, ok)
	}
}

func TestBuildCurrentSelectAckBodyWritesPersistedActiveQuestRows(t *testing.T) {
	quests := dnfrepo.QuestRecord{
		States: map[int64]dnfrepo.QuestState{
			300:    {Status: "active", ProgressValue: 1},
			100:    {Status: " ACTIVE ", ProgressValue: 3},
			200:    {Status: "complete", ProgressValue: 7},
			0:      {Status: "active", ProgressValue: 1},
			0xffff: {Status: "active", ProgressValue: 1},
			70000:  {Status: "active", ProgressValue: 1},
		},
		Progress: map[int64]dnfrepo.QuestState{
			300: {Status: "active", ProgressValue: 9},
			400: {Status: "active", ProgressValue: int64(^uint32(0)) + 9},
			500: {Status: "completed", ProgressValue: 1},
		},
	}
	body := buildCurrentSelectCharacterAckBody(dnfrepo.CharacterRecord{}, true, quests, true, 28, 13, 0, []byte{0})
	assertSelectCharacterAckBody(t, body, 28)
	questOffset, ok := currentSelectAckQuestLayout(body)
	if !ok {
		t.Fatalf("select init ack quest layout not readable")
	}
	for idx, want := range []struct {
		id      uint16
		trigger uint32
	}{{id: 100, trigger: 3}, {id: 300, trigger: 1}, {id: 400, trigger: ^uint32(0)}} {
		off := questOffset + idx*currentSelectAckQuestRowSize
		if got := binary.LittleEndian.Uint16(body[off : off+2]); got != want.id {
			t.Fatalf("select init ack quest[%d] id = %#x, want %#x", idx, got, want.id)
		}
		if got := binary.LittleEndian.Uint32(body[off+2 : off+6]); got != want.trigger {
			t.Fatalf("select init ack quest[%d] trigger = %d, want %d", idx, got, want.trigger)
		}
	}
	fourth := questOffset + 3*currentSelectAckQuestRowSize
	if got := binary.LittleEndian.Uint16(body[fourth : fourth+2]); got != 0xffff {
		t.Fatalf("select init ack first empty quest id = %#x, want 0xffff", got)
	}
	if fixed, overflow, ok := currentSelectAckQuestRowCounts(body); !ok || fixed != 3 || overflow != 0 {
		t.Fatalf("select init ack quest counts fixed=%d overflow=%d ok=%v, want 3 0 true", fixed, overflow, ok)
	}
}

func TestBuildCurrentSelectAckQuestRowsUsesCurrentOverflowList(t *testing.T) {
	quests := make(map[int64]dnfrepo.QuestState, 32)
	for idx := 0; idx < 32; idx++ {
		quests[int64(100+idx)] = dnfrepo.QuestState{Status: "active", ProgressValue: int64(idx + 1)}
	}
	body := buildCurrentSelectCharacterAckBody(dnfrepo.CharacterRecord{}, false, dnfrepo.QuestRecord{States: quests}, true, 28, 13, 0, []byte{0})
	wantLen := currentSelectAckMinimumBodyLen + 2*currentSelectAckQuestRowSize
	if got := len(body); got != wantLen {
		t.Fatalf("select ack overflow body len = %d, want %d", got, wantLen)
	}
	questOffset, ok := currentSelectAckQuestLayout(body)
	if !ok {
		t.Fatalf("select ack overflow quest layout not readable")
	}
	overflowOffset := questOffset + currentSelectAckFixedQuestCount*currentSelectAckQuestRowSize
	if got := binary.LittleEndian.Uint32(body[overflowOffset : overflowOffset+4]); got != 2 {
		t.Fatalf("select ack overflow count = %d, want 2", got)
	}
	firstOverflow := overflowOffset + 4
	if got := binary.LittleEndian.Uint16(body[firstOverflow : firstOverflow+2]); got != 130 {
		t.Fatalf("select ack first overflow quest id = %d, want 130", got)
	}
	if got := binary.LittleEndian.Uint32(body[firstOverflow+2 : firstOverflow+6]); got != 31 {
		t.Fatalf("select ack first overflow quest trigger = %d, want 31", got)
	}
	state, stateOffset, ok := currentSelectAckIntermediateState(body)
	if !ok || stateOffset != firstOverflow+2*currentSelectAckQuestRowSize {
		t.Fatalf("select ack overflow sub_1A0C3E0 state offset=%d ok=%v", stateOffset, ok)
	}
	if state != [currentSelectAckStateU32Count]uint32{} {
		t.Fatalf("select ack overflow sub_1A0C3E0 state = %#v, want zero", state)
	}
	if parsedFixed, parsedOverflow, ok := currentSelectAckQuestRowCounts(body); !ok || parsedFixed != 30 || parsedOverflow != 2 {
		t.Fatalf("select ack parsed counts fixed=%d overflow=%d ok=%v, want 30 2 true", parsedFixed, parsedOverflow, ok)
	}
	if got, ok := currentSelectAckSelectedSlotFromBody(body); !ok || got != 13 {
		t.Fatalf("select ack overflow selected slot = %d ok=%v, want 13 true", got, ok)
	}
}

func TestBuildCurrentFatigueBodyUsesPersistedCharacterState(t *testing.T) {
	character := dnfrepo.CharacterRecord{Stats: map[string]int64{
		"fatigue":                  120,
		"fatigue_limit":            156,
		"fatigue_update":           5,
		"fatigue_display_update":   36,
		"ack_fatigue_grownup_buff": 7,
	}}
	body := buildCurrentFatigueBody(character, true)
	want := []byte{36, 0, 156, 0, 5, 0, 36, 0, 7, 0}
	if !bytes.Equal(body, want) {
		t.Fatalf("fatigue body = %x, want %x", body, want)
	}
}

func TestCurrentSelectAckAndFatigueDoNotUseChannelOrAreaDungeonID(t *testing.T) {
	character := dnfrepo.CharacterRecord{Stats: map[string]int64{"fatigue": 156, "fatigue_limit": 156}}
	first := buildCurrentSelectCharacterAckBody(character, true, dnfrepo.QuestRecord{}, false, 28, 13, 0, []byte{0})
	second := buildCurrentSelectCharacterAckBody(character, true, dnfrepo.QuestRecord{}, false, 28, 13, 0, []byte{0})
	if !bytes.Equal(first, second) {
		t.Fatalf("select ack changed without a character-state change")
	}
	body := buildCurrentFatigueBody(character, true)
	want := []byte{0, 0, 156, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(body, want) {
		t.Fatalf("fatigue body = %x, want %x", body, want)
	}
}

func TestDefaultCreatedCharacterStatsSeedPersistedFatigue(t *testing.T) {
	stats := defaultCreatedCharacterStats(0)
	if got := stats["tutorial_completed"]; got != 0 {
		t.Fatalf("initial tutorial_completed = %d, want 0", got)
	}
	if got := stats["fatigue"]; got != newCharacterInitialFatigue {
		t.Fatalf("initial fatigue = %d, want %d", got, newCharacterInitialFatigue)
	}
	if got := stats["fatigue_limit"]; got != newCharacterFatigueLimit {
		t.Fatalf("initial fatigue limit = %d, want %d", got, newCharacterFatigueLimit)
	}
}

func TestHandleUpperCreateCharacterProtectedDecodeFailureIsNonFatal(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}
	body := []byte{
		0xf1, 0x65, 0x58, 0x40, 0x40, 0x50, 0xf4, 0xfb,
		0xec, 0x23, 0x26, 0x23, 0x1a, 0x1f, 0x17, 0xdc,
		0x36, 0x37, 0x0b, 0xdc, 0xf0, 0xaf, 0x16, 0xbc,
	}

	if err := service.handleUpperCreateCharacter(session, body); err != nil {
		t.Fatalf("handle protected undecodable upper create: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("unexpected protected undecodable trailing bytes: %x", rest)
	}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.UpperMsgCreateCharacter) ||
		!bytes.Equal(packet.Body, []byte{0x00, createCodeDuplicated}) {
		t.Fatalf("protected undecodable create ack = %+v body=%x", packet.Header, packet.Body)
	}
}

func TestHandleGameCreateCharacterSendsAckThenList(t *testing.T) {
	repos := testRepositoryGroup()
	service := &Service{
		options: options{accountPrefix: "dnf:"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	prepareTestCharacterInitialization(service, 15)
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 6, Type: 3, Name: "ch.6", Port: 10006}}

	if err := service.handleGameCreateCharacter(session, buildCreateRequest(15, "hero")); err != nil {
		t.Fatalf("handle game create: %v", err)
	}
	records, err := splitWrittenGameRecords(conn.write.Bytes())
	if err != nil {
		t.Fatalf("split records: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	if records[0].GameHeader.Cmd != byte(dnfenum.GameCmdCommand) ||
		records[0].GameHeader.Type != uint16(dnfenum.GameTypeCreateCharacter) ||
		!bytes.Equal(records[0].Body, []byte{0x01}) {
		t.Fatalf("create ack = %+v body=%x", records[0].GameHeader, records[0].Body)
	}
	if records[1].GameHeader.Cmd != byte(dnfenum.GameCmdNotice) ||
		records[1].GameHeader.Type != uint16(dnfenum.GameTypeCharacterList) {
		t.Fatalf("list refresh = %+v", records[1].GameHeader)
	}
	assertLatestCharacterState(t, records[1], 1)
}

func TestHandleGameCheckNameUsesCurrentExeResultEnvelope(t *testing.T) {
	repos := testRepositoryGroup()
	service := &Service{
		options: options{accountPrefix: "dnf:"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 6, Type: 3, Name: "ch.6", Port: 10006}}

	if err := service.handleGameCheckName(session, buildCheckNameRequest("hero")); err != nil {
		t.Fatalf("check available name: %v", err)
	}
	repos.Character.(*fakeCharacterStore).records["1"] = dnfrepo.CharacterRecord{
		CharacterID: "1",
		AccountID:   "dnf:1",
		Name:        "hero",
	}
	if err := service.handleGameCheckName(session, buildCheckNameRequest("hero")); err != nil {
		t.Fatalf("check duplicated name: %v", err)
	}
	first, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	second, rest := splitGameServerUpperPacket(t, rest)
	if len(rest) != 0 {
		t.Fatalf("unexpected check-name trailing bytes: %x", rest)
	}
	for idx, packet := range []dnfproto.ChannelPacket{first, second} {
		if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
			packet.Header.MsgID != uint16(dnfenum.UpperMsgCheckDoubleCharName) {
			t.Fatalf("check-name ack[%d] header = %+v", idx, packet.Header)
		}
	}
	if !bytes.Equal(first.Body, []byte{1}) {
		t.Fatalf("available ack body = %x, want 01", first.Body)
	}
	if !bytes.Equal(second.Body, []byte{0, createCodeDuplicated}) {
		t.Fatalf("duplicated ack body = %x, want failure plus duplicate code", second.Body)
	}
}

func TestHandleGameCheckNameFailureIncludesCurrentExeErrorByte(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19}}

	if err := service.handleGameCheckName(session, buildCheckNameRequest("hero")); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 || packet.Header.MsgID != uint16(dnfenum.UpperMsgCheckDoubleCharName) ||
		!bytes.Equal(packet.Body, []byte{0, createCodeServerError}) {
		t.Fatalf("repository failure header=%+v body=%x trailing=%d", packet.Header, packet.Body, len(rest))
	}
}

func TestParseCheckNameRejectsTrailingBytes(t *testing.T) {
	body := append(buildCheckNameRequest("hero"), 0)
	if name, code, ok := parseCheckName(body); ok || name != "" || code != createCodeServerError {
		t.Fatalf("trailing-byte request parsed as name=%q code=%d ok=%v", name, code, ok)
	}
}

func TestHandleGameCheckNameAcceptsNoPackProtectedBody(t *testing.T) {
	for _, body := range [][]byte{
		{0xfd, 0x07, 0x49, 0xc0, 0x14, 0x00, 0xb6, 0xa1},
		{0x2a, 0xc3, 0x73, 0x6d, 0xb2, 0x10, 0x34, 0xfb, 0x1d, 0xa4, 0x2a, 0x98, 0xc6, 0x5f, 0x44, 0xf7},
	} {
		service := &Service{}
		conn := &bufferConn{}
		session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 19, Type: 1, Name: "ch.19", Port: 10019}}

		if err := service.handleGameCheckName(session, body); err != nil {
			t.Fatalf("handle protected check-name len=%d: %v", len(body), err)
		}
		packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
		if len(rest) != 0 {
			t.Fatalf("unexpected protected check-name trailing bytes len=%d: %x", len(body), rest)
		}
		if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
			packet.Header.MsgID != uint16(dnfenum.UpperMsgCheckDoubleCharName) {
			t.Fatalf("protected check-name ack header len=%d = %+v", len(body), packet.Header)
		}
		if !bytes.Equal(packet.Body, []byte{1}) {
			t.Fatalf("protected check-name ack body len=%d = %x, want 01", len(body), packet.Body)
		}
	}
}

func TestHandleUpperChangeCharacterSlotSwapsRealSlotsAndRefreshesRoster(t *testing.T) {
	repos := testRepositoryGroup()
	store := repos.Character.(*fakeCharacterStore)
	store.records["10"] = dnfrepo.CharacterRecord{CharacterID: "10", AccountID: "dnf:1", Slot: 2, Name: "left", Job: "0", Level: 1}
	store.records["20"] = dnfrepo.CharacterRecord{CharacterID: "20", AccountID: "dnf:1", Slot: 7, Name: "right", Job: "1", Level: 2}
	service := &Service{
		options: options{accountPrefix: "dnf:"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}}
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[:4], 2)
	binary.LittleEndian.PutUint32(body[4:8], 7)
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.CmdPacketChangeCharacSlot), body, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build change-slot packet: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle change-slot: %v", err)
	}
	if store.records["10"].Slot != 7 || store.records["20"].Slot != 2 {
		t.Fatalf("stored slots = %d/%d, want 7/2", store.records["10"].Slot, store.records["20"].Slot)
	}
	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketChangeCharacSlot) ||
		!bytes.Equal(ack.Body, []byte{0x01}) {
		t.Fatalf("change-slot ack = class %d msg %d body %x", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	roster, _ := splitGameServerUpperPacket(t, rest)
	if roster.Header.Classification != 0 || roster.Header.MsgID != uint16(dnfenum.UpperMsgCharacterRoster) {
		t.Fatalf("change-slot roster header = %+v", roster.Header)
	}
	assertCSharpRosterBody(t, roster.Body, 2)
	entry0 := 15
	if got := binary.LittleEndian.Uint16(roster.Body[entry0 : entry0+2]); got != 2 {
		t.Fatalf("first roster entry key=%d, want slot 2", got)
	}
	name0 := rosterDstrName(store.records["20"].Name)
	if got := roster.Body[entry0+2 : entry0+2+len(name0)]; !bytes.Equal(got, name0) {
		t.Fatalf("first roster entry name bytes=%x, want right %x", got, name0)
	}
	entry1 := 15 + noPackRosterEntryLen(store.records["20"])
	if got := binary.LittleEndian.Uint16(roster.Body[entry1 : entry1+2]); got != 7 {
		t.Fatalf("second roster entry key=%d, want slot 7", got)
	}
	name1 := rosterDstrName(store.records["10"].Name)
	if got := roster.Body[entry1+2 : entry1+2+len(name1)]; !bytes.Equal(got, name1) {
		t.Fatalf("second roster entry name bytes=%x, want left %x", got, name1)
	}
}

func TestHandleUpperChangeCharacterSlotDoesNotMoveIntoEmptySlot(t *testing.T) {
	repos := testRepositoryGroup()
	store := repos.Character.(*fakeCharacterStore)
	store.records["10"] = dnfrepo.CharacterRecord{CharacterID: "10", AccountID: "dnf:1", Slot: 2, Name: "left", Job: "0", Level: 1}
	service := &Service{
		options: options{accountPrefix: "dnf:"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}}
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[:4], 2)
	binary.LittleEndian.PutUint32(body[4:8], 7)
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.CmdPacketChangeCharacSlot), body, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build change-slot packet: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle change-slot with empty slot: %v", err)
	}
	if store.records["10"].Slot != 2 {
		t.Fatalf("empty-slot request moved character to slot %d, want 2", store.records["10"].Slot)
	}
	ack, _ := splitGameServerUpperPacket(t, conn.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketChangeCharacSlot) || !bytes.Equal(ack.Body, []byte{0x01}) {
		t.Fatalf("empty-slot change ack = %+v body=%x", ack.Header, ack.Body)
	}
}

func TestHandleUpperChangeCharacterSlotRejectsTrailingBytes(t *testing.T) {
	repos := testRepositoryGroup()
	store := repos.Character.(*fakeCharacterStore)
	store.records["10"] = dnfrepo.CharacterRecord{CharacterID: "10", AccountID: "dnf:1", Slot: 2, Name: "left"}
	store.records["20"] = dnfrepo.CharacterRecord{CharacterID: "20", AccountID: "dnf:1", Slot: 7, Name: "right"}
	service := &Service{
		options:            options{accountPrefix: "dnf:"},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
	}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38}}
	body := make([]byte, 9)
	binary.LittleEndian.PutUint32(body[:4], 2)
	binary.LittleEndian.PutUint32(body[4:8], 7)

	if err := service.handleChangeCharacterSlot(session, body, true); err != nil {
		t.Fatal(err)
	}
	if store.records["10"].Slot != 2 || store.records["20"].Slot != 7 {
		t.Fatalf("malformed request changed slots to %d/%d", store.records["10"].Slot, store.records["20"].Slot)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 || packet.Header.MsgID != uint16(dnfenum.CmdPacketChangeCharacSlot) ||
		!bytes.Equal(packet.Body, []byte{0, createCodeServerError}) {
		t.Fatalf("malformed response header=%+v body=%x trailing=%d", packet.Header, packet.Body, len(rest))
	}
}

func TestHandleUpperLoadExtendCharacsDefersWithoutPendingUnlock(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgLoadExtendCharacs), make([]byte, 8), 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build load-extend-characs upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle load-extend-characs upper: %v", err)
	}
	if got := conn.write.Len(); got != 0 {
		t.Fatalf("load-extend-characs must not push empty slot state, wrote %d bytes: %x", got, conn.write.Bytes())
	}
}

func TestHandleUpperLoadExtendCharacsCompletesEmptyRosterProbe(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{
		conn:    conn,
		channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038},
	}
	if err := service.sendUpperRoster(session, nil); err != nil {
		t.Fatalf("send empty roster: %v", err)
	}
	if !session.emptyRosterSlotProbePending {
		t.Fatal("empty mode-2 roster did not arm the slot probe completion")
	}
	conn.write.Reset()

	frame, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgLoadExtendCharacs),
		[]byte{0xff, 0xff},
		0,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatalf("build empty-roster load-extend-characs upper: %v", err)
	}
	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle empty-roster load-extend-characs upper: %v", err)
	}
	if session.emptyRosterSlotProbePending {
		t.Fatal("empty roster slot probe remained pending after completion")
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("empty-roster slot probe response has %d trailing bytes: %x", len(rest), rest)
	}
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.UpperMsgLoadExtendCharacs) ||
		!bytes.Equal(packet.Body, []byte{0, 0}) {
		t.Fatalf("empty-roster slot probe response header=%+v body=%x, want class1/op679 failure 0000", packet.Header, packet.Body)
	}
}

func TestNonEmptyRosterClearsEmptyRosterSlotProbe(t *testing.T) {
	session := &gameSession{emptyRosterSlotProbePending: true}
	noteEmptyRosterSlotProbe(session, buildCSharpRosterBody([]dnfrepo.CharacterRecord{{
		CharacterID: "1",
		Slot:        0,
		Name:        "Existing",
	}}))
	if session.emptyRosterSlotProbePending {
		t.Fatal("non-empty mode-2 roster retained an empty-roster slot probe")
	}
}

func TestHandleUpperCharacSlotExtendEffectDefersWithoutPendingUnlock(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 11, Type: 1, Name: "ch.11", Port: 10011}}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCharacSlotExtendEffect), nil, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build slot-extend-effect upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle slot-extend-effect upper: %v", err)
	}
	if got := conn.write.Len(); got != 0 {
		t.Fatalf("slot-extend-effect wrote %d bytes, want none: %x", got, conn.write.Bytes())
	}
}

func TestHandleUpperStaticsRuntimeTingDefersDuplicateLoginEndpoint(t *testing.T) {
	service := &Service{options: options{gameUpperHeader: gameUpperHeaderServer16, gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgStaticsRuntimeTing), make([]byte, 16), 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build statics-runtime upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle statics-runtime upper: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("statics-runtime emitted duplicate login endpoint bytes: %x", conn.write.Bytes())
	}
}

func TestHandleGameStaticsRuntimeTingDoesNotSendLegacyClientCheck(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.GameTypeStaticsRuntimeTing), make([]byte, 16)); err != nil {
		t.Fatalf("handle game statics-runtime: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("statics-runtime game command emitted legacy client-check bytes: %x", conn.write.Bytes())
	}
}

func TestHandleUpperMultiplexedStateEchoesCurrentEXEDiscriminator(t *testing.T) {
	tests := []struct {
		discriminator uint32
		bodyLength    int
	}{
		{discriminator: 222, bodyLength: 16},
		{discriminator: 349, bodyLength: 12},
		{discriminator: 2099, bodyLength: 16},
		{discriminator: 2208, bodyLength: 16},
		{discriminator: 2384, bodyLength: 16},
		{discriminator: 2427, bodyLength: 12},
		{discriminator: 2428, bodyLength: 8},
		{discriminator: 2443, bodyLength: 4},
	}
	for _, test := range tests {
		t.Run(strconv.FormatUint(uint64(test.discriminator), 10), func(t *testing.T) {
			service := &Service{}
			conn := &bufferConn{}
			session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
			body := make([]byte, test.bodyLength)
			binary.LittleEndian.PutUint32(body[:4], test.discriminator)
			frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCreatePostState), body, 0, dnfproto.DefaultChannelClassification)
			if err != nil {
				t.Fatalf("build multiplexed-state upper: %v", err)
			}

			if err := service.handleGameUpper(session, frame); err != nil {
				t.Fatalf("handle multiplexed-state upper: %v", err)
			}
			packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
			if len(rest) != 0 {
				t.Fatalf("unexpected trailing bytes: %d", len(rest))
			}
			if packet.Header.MsgID != uint16(dnfenum.UpperMsgCreatePostState) {
				t.Fatalf("msg id = %d", packet.Header.MsgID)
			}
			wantBody := make([]byte, 5)
			wantBody[0] = 1
			binary.LittleEndian.PutUint32(wantBody[1:], test.discriminator)
			if !bytes.Equal(packet.Body, wantBody) {
				t.Fatalf("multiplexed-state body = %x, want %x", packet.Body, wantBody)
			}
		})
	}
}

func TestHandleUpperMultiplexedStateRejectsUnprovedShapes(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "missing discriminator", body: make([]byte, 3)},
		{name: "known discriminator wrong length", body: func() []byte {
			body := make([]byte, 12)
			binary.LittleEndian.PutUint32(body[:4], 222)
			return body
		}()},
		{name: "unknown discriminator", body: func() []byte {
			body := make([]byte, 4)
			binary.LittleEndian.PutUint32(body, 9999)
			return body
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{}
			conn := &bufferConn{}
			session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
			frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCreatePostState), test.body, 0, dnfproto.DefaultChannelClassification)
			if err != nil {
				t.Fatalf("build multiplexed-state upper: %v", err)
			}
			if err := service.handleGameUpper(session, frame); err != nil {
				t.Fatalf("handle multiplexed-state upper: %v", err)
			}
			if conn.write.Len() != 0 {
				t.Fatalf("unproved request emitted bytes: %x", conn.write.Bytes())
			}
		})
	}
}

func TestHandleUpperCheckCharacterGateEchoesCandidate(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[0:4], 9)
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCheckCharacterGate), body, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build gate upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle gate upper: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes: %d", len(rest))
	}
	if packet.Header.MsgID != uint16(dnfenum.UpperMsgCheckCharacterGate) {
		t.Fatalf("msg id = %d", packet.Header.MsgID)
	}
	if !bytes.Equal(packet.Body, []byte{1, 9, 0, 0, 0}) {
		t.Fatalf("gate body = %x", packet.Body)
	}
}

func TestHandleUpperCheckCharacterGateKeepsZeroFirstValue(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[4:8], 9)
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCheckCharacterGate), body, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build gate upper: %v", err)
	}
	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle gate upper: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes: %d", len(rest))
	}
	if !bytes.Equal(packet.Body, []byte{1, 0, 0, 0, 0}) {
		t.Fatalf("gate body = %x", packet.Body)
	}
}

func TestHandleUpperCheckCharacterGateRejectsUnprovedShapes(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "short", body: make([]byte, 4)},
		{name: "long", body: make([]byte, 12)},
		{name: "candidate sentinel", body: []byte{0xff, 0xff, 0xff, 0xff, 1, 0, 0, 0}},
		{name: "pair sentinel", body: []byte{1, 0, 0, 0, 0xff, 0xff, 0xff, 0xff}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{}
			conn := &bufferConn{}
			session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
			frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgCheckCharacterGate), test.body, 0, dnfproto.DefaultChannelClassification)
			if err != nil {
				t.Fatalf("build gate upper: %v", err)
			}
			if err := service.handleGameUpper(session, frame); err != nil {
				t.Fatalf("handle gate upper: %v", err)
			}
			if conn.write.Len() != 0 {
				t.Fatalf("unproved gate emitted bytes: %x", conn.write.Bytes())
			}
		})
	}
}

func TestHandleGameSelectCharacterEchoesLatestStateTuple(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	channel := channelcatalog.Channel{ServerID: 1, ID: 16, Type: 1, Name: "ch.16", Port: 10016}
	session := &gameSession{conn: conn, channel: channel, residentChannel: channel}
	body := []byte{1, 0, 0, 0, 2, 3, 4, 0, 0, 0, 5}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.GameTypeSelectCharacter), body); err != nil {
		t.Fatalf("handle select: %v", err)
	}
	records, err := splitWrittenGameRecords(conn.write.Bytes())
	if err != nil {
		t.Fatalf("split select record: %v", err)
	}
	if len(records) != 1 ||
		records[0].GameHeader.Cmd != byte(dnfenum.GameCmdCommand) ||
		records[0].GameHeader.Type != uint16(dnfenum.GameTypeSelectCharacter) ||
		!bytes.Equal(records[0].Body, body) {
		t.Fatalf("select response = %+v body=%x", records, records[0].Body)
	}
}

func TestHandleLegacyGameSelectCharacterStripsCodecPrefix(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	channel := channelcatalog.Channel{ServerID: 1, ID: 16, Type: 1, Name: "ch.16", Port: 10016}
	session := &gameSession{conn: conn, channel: channel, residentChannel: channel}
	payload := []byte{1, 0, 0, 0, 2, 3, 4, 0, 0, 0, 5}
	raw := buildLegacyGamePacketForBridgeTest(
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.GameTypeSelectCharacter),
		0,
		append([]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee}, payload...),
	)

	if err := service.handleLegacyGamePacket(session, raw); err != nil {
		t.Fatalf("handle legacy select: %v", err)
	}
	records, err := splitWrittenGameRecords(conn.write.Bytes())
	if err != nil {
		t.Fatalf("split select record: %v", err)
	}
	if len(records) != 1 || !bytes.Equal(records[0].Body, payload) {
		t.Fatalf("select response body = %x, want %x", records[0].Body, payload)
	}
}

func TestHandleGameFinishLoadingReturnsUpperStatusSuccess(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}
	body := make([]byte, 8)

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.GameTypeFinishLoading), body); err != nil {
		t.Fatalf("handle loading: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) ||
		!bytes.Equal(packet.Body, []byte{1}) {
		t.Fatalf("finish-loading status = class %d msg %d body=%x", packet.Header.Classification, packet.Header.MsgID, packet.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("finish-loading status trailing bytes=%x", rest)
	}
}

func TestHandleGameFinishLoadingRejectsUnexpectedProtectedBodyLengths(t *testing.T) {
	for _, bodyLen := range []int{0, 15, 17, 32} {
		t.Run(fmt.Sprintf("body_len_%d", bodyLen), func(t *testing.T) {
			service := &Service{}
			conn := &bufferConn{}
			session := &gameSession{
				conn:                               conn,
				channel:                            channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038},
				selectedCharacterID:                77,
				postStartMapPlayerStateSent:        true,
				currentFinishLoadingStateSent:      false,
				currentFinishLoadingCompletionSent: false,
				postFinishLoadingPlayerStateSent:   false,
			}

			if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.GameTypeFinishLoading), make([]byte, bodyLen)); err != nil {
				t.Fatalf("handle malformed finish loading: %v", err)
			}
			if conn.write.Len() != 0 {
				t.Fatalf("malformed finish loading wrote %d bytes", conn.write.Len())
			}
			if session.currentFinishLoadingStateSent || session.currentFinishLoadingCompletionSent || session.postFinishLoadingPlayerStateSent {
				t.Fatalf("malformed finish loading advanced gates state=%t completion=%t post=%t",
					session.currentFinishLoadingStateSent,
					session.currentFinishLoadingCompletionSent,
					session.postFinishLoadingPlayerStateSent)
			}
		})
	}
}

func TestHandleGameContentsPlayInfoDefersSidePanelAck(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.GameTypeContentsPlayInfo), make([]byte, 16)); err != nil {
		t.Fatalf("handle contents play info: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("contents play info wrote %d bytes, want deferred", conn.write.Len())
	}
}

func TestHandleGameRequestBlacklistReturnsCurrentSceneOp120Body(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		channel:             channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038},
		selectedCharacterID: 12,
	}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketRequestBlacklist), make([]byte, 16)); err != nil {
		t.Fatalf("handle request blacklist: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketRequestBlacklist) ||
		!bytes.Equal(packet.Body, currentRequestBlacklistResponseBody) {
		t.Fatalf("request blacklist response = class %d msg %d body=%x want=%x", packet.Header.Classification, packet.Header.MsgID, packet.Body, currentRequestBlacklistResponseBody)
	}
	if len(rest) != 0 {
		t.Fatalf("request blacklist sent runtime seed before op29 = %d", len(rest))
	}
	if session.runtimeAfterBlacklistSent || session.runtimeFinishLoadingGateSent {
		t.Fatalf("runtime flags after request blacklist: after_blacklist=%v finish_gate=%v", session.runtimeAfterBlacklistSent, session.runtimeFinishLoadingGateSent)
	}

	conn.write.Reset()
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketRequestBlacklist), make([]byte, 16)); err != nil {
		t.Fatalf("handle repeated request blacklist: %v", err)
	}
	packet, rest = splitGameServerUpperPacket(t, conn.write.Bytes())
	if packet.Header.MsgID != uint16(dnfenum.CmdPacketRequestBlacklist) ||
		!bytes.Equal(packet.Body, currentRequestBlacklistResponseBody) {
		t.Fatalf("repeated request blacklist response = msg %d body=%x", packet.Header.MsgID, packet.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("repeated request blacklist resent finish gate bytes = %d", len(rest))
	}
}

func TestCurrentRequestBlacklistBodyMatchesUpperBlacklistEmptyResult(t *testing.T) {
	if !bytes.Equal(currentRequestBlacklistResponseBody, []byte{0, 0}) {
		t.Fatalf("current upper blacklist/op120 body = %x, want 0000", currentRequestBlacklistResponseBody)
	}
	if len(currentRequestBlacklistResponseBody) != 2 {
		t.Fatalf("current upper blacklist/op120 body len = %d, want 2", len(currentRequestBlacklistResponseBody))
	}
}

func TestCurrentSceneActorPlacementBodyMatchesMainOp120Reader(t *testing.T) {
	if body := buildCurrentSceneActorPlacementBody(); !bytes.Equal(body, []byte{0, 0}) {
		t.Fatalf("current main scene op120 body = %x, want actor_slot=0 placement_seed=0", body)
	}
}

func TestHandleGameRequestBlacklistAfterSceneTailSendsSafeRuntimeSeed(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                        conn,
		channel:                     channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038},
		selectedCharacterID:         12,
		sceneBootstrapTailSent:      true,
		postStartMapPlayerStateSent: true,
	}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketRequestBlacklist), make([]byte, 16)); err != nil {
		t.Fatalf("handle request blacklist after scene tail: %v", err)
	}
	rest := conn.write.Bytes()
	blacklist, rest := splitGameServerUpperPacket(t, rest)
	if blacklist.Header.Classification != dnfproto.DefaultChannelClassification ||
		blacklist.Header.MsgID != uint16(dnfenum.CmdPacketRequestBlacklist) ||
		!bytes.Equal(blacklist.Body, currentRequestBlacklistResponseBody) {
		t.Fatalf("request blacklist response = class %d msg %d body=%x", blacklist.Header.Classification, blacklist.Header.MsgID, blacklist.Body)
	}
	rest = assertRuntimeAfterBlacklistSafePrefix(t, service, session, rest)
	if len(rest) != 0 {
		t.Fatalf("request blacklist trailing bytes after runtime seed = %d", len(rest))
	}
	if !session.runtimeAfterBlacklistSent || session.runtimeFinishLoadingGateSent {
		t.Fatalf("runtime flags after scene tail: after_blacklist=%v finish_gate=%v", session.runtimeAfterBlacklistSent, session.runtimeFinishLoadingGateSent)
	}

	conn.write.Reset()
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketRequestBlacklist), make([]byte, 16)); err != nil {
		t.Fatalf("handle repeated request blacklist after scene tail: %v", err)
	}
	_, rest = splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("repeated request blacklist resent runtime seed bytes = %d", len(rest))
	}
}

func TestHandleGameFpsDevideCollectDefersFinishLoadingGate(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		channel:             channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038},
		selectedCharacterID: 12,
	}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketFpsDevideCollect), make([]byte, 976)); err != nil {
		t.Fatalf("handle fps devide collect: %v", err)
	}
	if got := conn.write.Len(); got != 0 {
		t.Fatalf("fps gate wrote %d bytes after class0/op37 defer", got)
	}
	if !session.fpsFinishLoadingGateSent {
		t.Fatalf("fpsFinishLoadingGateSent = false")
	}

	conn.write.Reset()
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketFpsDevideCollect), make([]byte, 976)); err != nil {
		t.Fatalf("handle repeated fps devide collect: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("repeated fps collect wrote %d bytes", conn.write.Len())
	}
}

func TestHandleGameFpsDevideCollectSkipsAfterSceneBootstrapTail(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                   conn,
		channel:                channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038},
		selectedCharacterID:    12,
		sceneBootstrapTailSent: true,
	}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketFpsDevideCollect), make([]byte, 976)); err != nil {
		t.Fatalf("handle fps devide collect after scene tail: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("fps collect after scene tail wrote %d bytes", conn.write.Len())
	}
	if session.fpsFinishLoadingGateSent {
		t.Fatalf("fpsFinishLoadingGateSent after scene tail = true")
	}
}

func TestHandleGameGuildAllMemberListDefersPassiveLongHengResponse(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		channel:             channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038},
		selectedCharacterID: 12,
	}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketGuildAllMemberList), make([]byte, 16)); err != nil {
		t.Fatalf("handle guild all member list: %v", err)
	}
	if got := conn.write.Len(); got != 0 {
		t.Fatalf("guild all member list deferred bytes = %d, want 0", got)
	}
}

func TestHandleGameEnterSelectDungeonUsesRuntimeSceneEnums(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038}}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.GameTypeEnterSelectDungeon), nil); err != nil {
		t.Fatalf("handle enter select dungeon: %v", err)
	}
	records, err := splitWrittenGameRecords(conn.write.Bytes())
	if err != nil {
		t.Fatalf("split enter select dungeon records: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("enter select dungeon records = %d, want 2", len(records))
	}
	if records[0].GameHeader.Type != uint16(dnfenum.CmdPacketEnterSelectDungeon) {
		t.Fatalf("record[0] type = %d, want %d", records[0].GameHeader.Type, dnfenum.CmdPacketEnterSelectDungeon)
	}
	if wantBody := []byte{3, byte(currentSceneBootstrapObjectKey & 0xff), byte(currentSceneBootstrapObjectKey >> 8)}; !bytes.Equal(records[0].Body, wantBody) {
		t.Fatalf("record[0] body = %x, want %x", records[0].Body, wantBody)
	}
	if records[1].GameHeader.Type != currentFatigueMsgID {
		t.Fatalf("record[1] type = %d, want %d", records[1].GameHeader.Type, currentFatigueMsgID)
	}
	if wantBody := buildCurrentFatigueBody(dnfrepo.CharacterRecord{}, false); !bytes.Equal(records[1].Body, wantBody) {
		t.Fatalf("record[1] body = %x, want %x", records[1].Body, wantBody)
	}
}

func TestHandleUpperEnterSelectDungeonUsesRuntimeSceneEnums(t *testing.T) {
	service := &Service{}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                      conn,
		channel:                   channelcatalog.Channel{ID: 38, Type: 3, Name: "ch.38", Port: 10038},
		selectedCharacterID:       12,
		enterSelectDungeonSent:    true,
		enterSelectDungeonAckSent: false,
	}
	frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.UpperMsgSelectStart), nil, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build enter select upper: %v", err)
	}

	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle enter select upper: %v", err)
	}
	first, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if first.Header.Classification != dnfproto.DefaultChannelClassification ||
		first.Header.MsgID != uint16(dnfenum.CmdPacketEnterSelectDungeon) {
		t.Fatalf("first upper = class %d msg %d", first.Header.Classification, first.Header.MsgID)
	}
	if wantBody := upperSuccessBody(buildEnterSelectDungeonAckBody()); !bytes.Equal(first.Body, wantBody) {
		t.Fatalf("first upper body = %x, want %x", first.Body, wantBody)
	}
	second, rest := splitGameServerUpperPacket(t, rest)
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes: %d", len(rest))
	}
	if second.Header.Classification != 0 ||
		second.Header.MsgID != currentFatigueMsgID ||
		!bytes.Equal(second.Body, buildCurrentFatigueBody(dnfrepo.CharacterRecord{}, false)) {
		t.Fatalf("second upper = class %d msg %d body=%x", second.Header.Classification, second.Header.MsgID, second.Body)
	}
	if !session.enterSelectDungeonAckSent {
		t.Fatalf("request-driven enter-select ACK was not marked sent")
	}

	conn.write.Reset()
	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle duplicate enter select upper: %v", err)
	}
	first, rest = splitGameServerUpperPacket(t, conn.write.Bytes())
	if first.Header.Classification != dnfproto.DefaultChannelClassification ||
		first.Header.MsgID != uint16(dnfenum.CmdPacketEnterSelectDungeon) ||
		!bytes.Equal(first.Body, upperSuccessBody(buildEnterSelectDungeonAckBody())) {
		t.Fatalf("duplicate first upper = header=%+v body=%x", first.Header, first.Body)
	}
	second, rest = splitGameServerUpperPacket(t, rest)
	if len(rest) != 0 {
		t.Fatalf("duplicate enter-select trailing bytes: %d", len(rest))
	}
	if second.Header.Classification != 0 ||
		second.Header.MsgID != currentFatigueMsgID ||
		!bytes.Equal(second.Body, buildCurrentFatigueBody(dnfrepo.CharacterRecord{}, false)) {
		t.Fatalf("duplicate second upper = class %d msg %d body=%x", second.Header.Classification, second.Header.MsgID, second.Body)
	}
}

func TestGameListenPortsUsesOnlyExactCatalogPorts(t *testing.T) {
	const fixture = `
[dungeon]
` + "`[trade]` `交易频道` 1" + `
[/dungeon]
[server]
1 1 ` + "`频道1`" + ` 1 ` + "`[trade]`" + ` 0 0
[/server]
`
	index, err := channelinfo.Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse channel info: %v", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{ServerID: 1})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	service := &Service{}

	ports := service.gameListenPorts(catalog)
	if intSliceContains(ports, dnfenum.GamePortBase+dnfenum.DefaultGameChannelID) {
		t.Fatalf("ports = %+v, unexpected fabricated default port %d", ports, dnfenum.GamePortBase+dnfenum.DefaultGameChannelID)
	}
	if !intSliceContains(ports, dnfenum.GamePortBase+1) {
		t.Fatalf("ports = %+v, want catalog port %d", ports, dnfenum.GamePortBase+1)
	}
	for i := 1; i < len(ports); i++ {
		if ports[i-1] > ports[i] {
			t.Fatalf("ports not sorted: %+v", ports)
		}
	}
}

func TestChannelForPortRejectsSyntheticPortMissingFrom90CNCatalog(t *testing.T) {
	catalog := testCatalogWithoutAutoChannel(t)
	service := &Service{
		options: options{channelServerID: 1, channelAdvertiseID: 0},
		catalog: catalog,
	}

	channel, found := service.channelForPort(dnfenum.GamePortBase + 99)
	if found || channel.ID != 0 || channel.Port != 0 {
		t.Fatalf("synthetic port unexpectedly resolved found=%t channel=%+v", found, channel)
	}
}

func TestDofLoginPrefaceKeepsNextChannelPacket(t *testing.T) {
	preface := make([]byte, dnfproto.DofLoginPrefaceSize)
	copy(preface, dnfproto.BuildLegacyClientPacket(dnfenum.LegacyMsgDofLoginPreface, nil))
	copy(preface[dnfproto.LegacyChannelHeaderSize:], bytes.Repeat([]byte{1}, dnfproto.DofLoginTokenSize))
	channelPacket, err := dnfproto.BuildChannelPacket(uint16(dnfenum.MsgCSConnect), nil, 1, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build channel packet: %v", err)
	}
	reader := bufio.NewReader(bytes.NewReader(append(preface, channelPacket...)))
	service := &Service{}
	conn := &bufferConn{}
	session := &channelSession{conn: conn, connID: "channel-test"}

	if err := service.acceptDofLoginPreface(reader, session); err != nil {
		t.Fatalf("accept preface: %v", err)
	}
	ack, rest := splitLegacyPacket(t, conn.write.Bytes(), legacyMsgID(dnfenum.LegacyMsgDofLoginAck))
	if len(ack) != 47 || len(rest) != 0 {
		t.Fatalf("unexpected preface ack len=%d rest=%d", len(ack), len(rest))
	}
	packet, err := service.readChannelPacket(reader, session)
	if err != nil {
		t.Fatalf("read channel after preface: %v", err)
	}
	if packet.Header.MsgID != uint16(dnfenum.MsgCSConnect) {
		t.Fatalf("msg id = %d", packet.Header.MsgID)
	}
}

func TestReadChannelPacketAcceptsMixedLegacyConnect(t *testing.T) {
	raw := []byte{0x00, 0x09, 0x0B, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x11}
	service := &Service{}
	session := &channelSession{conn: &bufferConn{}, connID: "channel-test"}

	packet, err := service.readChannelPacket(bytes.NewReader(raw), session)
	if err != nil {
		t.Fatalf("read mixed legacy connect: %v", err)
	}
	if packet.Header.MsgID != uint16(dnfenum.MsgCSConnect) {
		t.Fatalf("msg id = %d, want %d", packet.Header.MsgID, dnfenum.MsgCSConnect)
	}
	if packet.Header.Length != uint32(len(raw)) {
		t.Fatalf("length = %d, want %d", packet.Header.Length, len(raw))
	}
	if !bytes.Equal(packet.Body, raw[4:]) {
		t.Fatalf("body = %x, want %x", packet.Body, raw[4:])
	}
}

func TestHandleLegacyChannelRefreshDoesNotResendScript(t *testing.T) {
	catalog := testChannelCatalog(t)
	service := &Service{
		options: options{
			serverIP:           "42.240.165.245",
			channelServerID:    1,
			channelAdvertiseID: 0,
			channelInfoFile:    tempChannelInfoFile(t),
		},
		catalog: catalog,
	}
	request := make([]byte, dnfproto.DofAskChannelSize)
	copy(request, dnfproto.BuildLegacyClientPacket(dnfenum.LegacyMsgAskChannelInfo, nil))
	copy(request[dnfproto.LegacyChannelHeaderSize:], []byte("cain"))
	reader := bufio.NewReader(bytes.NewReader(request))
	conn := &bufferConn{}
	session := &channelSession{conn: conn}

	handled, err := service.handleLegacyChannelRequest(reader, session)
	if err != nil {
		t.Fatalf("handle legacy ask: %v", err)
	}
	if !handled {
		t.Fatal("expected legacy request handled")
	}
	if session.scriptSent {
		t.Fatal("refresh without prior 0x09 must not resend the online script")
	}
	if session.closeAfterBootstrapDirectory {
		t.Fatal("normal refresh must keep the channel connection open")
	}
	info, rest := splitLegacyPacket(t, conn.write.Bytes(), legacyMsgID(dnfenum.LegacyMsgChannelInfo))
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes: %d", len(rest))
	}
	raw, err := decryptCompressedChannelData(info[dnfproto.LegacyChannelHeaderSize:], service.aesKey())
	if err != nil {
		t.Fatalf("decrypt legacy channel body: %v", err)
	}
	if got := binary.LittleEndian.Uint32(raw[4:8]); got != 0 {
		t.Fatalf("server index = %d, want online server 0", got)
	}
	assertChannelInfoHasEntries(t, raw, 2,
		channelEntryWant{name: "#ch.11", port: dnfenum.GamePortBase + 11},
		channelEntryWant{name: "#ch.38", port: dnfenum.GamePortBase + 38},
	)
}

func TestQuietChannelReadErrorTreatsTimeoutAsQuiet(t *testing.T) {
	if !isQuietChannelReadError(timeoutNetError{}) {
		t.Fatal("channel idle timeout should be treated as quiet close")
	}
}

func TestHandleLatestChannelAskSendsScriptBeforeChannelInfo(t *testing.T) {
	catalog := testChannelCatalog(t)
	service := &Service{
		options: options{
			serverIP:           "42.240.165.245",
			channelServerID:    1,
			channelAdvertiseID: 0,
			channelInfoFile:    tempChannelInfoFile(t),
		},
		catalog: catalog,
	}
	conn := &bufferConn{}
	session := &channelSession{conn: conn}

	err := service.handleChannelPacket(session, dnfproto.ChannelPacket{
		Header: dnfproto.ChannelHeader{
			Classification: dnfproto.DefaultChannelClassification,
			MsgID:          uint16(dnfenum.MsgCSAskChannelInfoNew),
		},
		Body: []byte("cain"),
	})
	if err != nil {
		t.Fatalf("handle latest ask: %v", err)
	}
	if !session.scriptSent {
		t.Fatal("latest ask without prior 0x09 must send fallback script")
	}
	script, rest := splitChannelPacket(t, conn.write.Bytes())
	if script.Header.MsgID != uint16(dnfenum.MsgSCGetScript) {
		t.Fatalf("script msg id = %d, want %d", script.Header.MsgID, dnfenum.MsgSCGetScript)
	}
	info, rest := splitChannelPacket(t, rest)
	if info.Header.MsgID != uint16(dnfenum.MsgSCAskChannelInfoNew) {
		t.Fatalf("msg id = %d, want %d", info.Header.MsgID, dnfenum.MsgSCAskChannelInfoNew)
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes: %d", len(rest))
	}
	raw, err := decryptCompressedChannelData(info.Body, service.aesKey())
	if err != nil {
		t.Fatalf("decrypt latest channel body: %v", err)
	}
	assertChannelInfoHasEntries(t, raw, 2,
		channelEntryWant{name: "#ch.11", port: dnfenum.GamePortBase + 11},
		channelEntryWant{name: "#ch.38", port: dnfenum.GamePortBase + 38},
	)
}

func TestHandleLatestGetScriptReturnsScript(t *testing.T) {
	service := &Service{
		options: options{
			channelInfoFile: tempChannelInfoFile(t),
		},
	}
	conn := &bufferConn{}
	session := &channelSession{conn: conn}

	err := service.handleChannelPacket(session, dnfproto.ChannelPacket{
		Header: dnfproto.ChannelHeader{
			Classification: dnfproto.DefaultChannelClassification,
			MsgID:          uint16(dnfenum.MsgCSGetScript),
		},
	})
	if err != nil {
		t.Fatalf("handle latest get script: %v", err)
	}
	if !session.scriptSent {
		t.Fatal("expected scriptSent after latest get script")
	}
	script, rest := splitChannelPacket(t, conn.write.Bytes())
	if script.Header.MsgID != uint16(dnfenum.MsgSCGetScript) {
		t.Fatalf("msg id = %d, want %d", script.Header.MsgID, dnfenum.MsgSCGetScript)
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes: %d", len(rest))
	}
	if len(script.Body) == 0 {
		t.Fatal("script body is empty")
	}
}

func TestHandleLegacyChannelProbeKeepsFollowingAskPacket(t *testing.T) {
	catalog := testChannelCatalog(t)
	service := &Service{
		options: options{
			serverIP:           "42.240.165.245",
			channelServerID:    1,
			channelAdvertiseID: 0,
			channelInfoFile:    tempChannelInfoFile(t),
		},
		catalog: catalog,
	}
	probe := dnfproto.BuildLegacyClientPacket(dnfenum.LegacyMsgDofChannelProbe, nil)
	ask := make([]byte, dnfproto.DofAskChannelSize)
	copy(ask, dnfproto.BuildLegacyClientPacket(dnfenum.LegacyMsgAskChannelInfo, nil))
	copy(ask[dnfproto.LegacyChannelHeaderSize:], []byte("cain"))
	reader := bufio.NewReader(bytes.NewReader(append(probe, ask...)))
	conn := &bufferConn{}
	session := &channelSession{conn: conn}

	handled, err := service.handleLegacyChannelRequest(reader, session)
	if err != nil {
		t.Fatalf("handle probe: %v", err)
	}
	if !handled {
		t.Fatal("expected probe handled")
	}
	script, rest := splitLegacyPacket(t, conn.write.Bytes(), legacyMsgID(dnfenum.LegacyMsgDofScript))
	if len(rest) != 0 {
		t.Fatalf("unexpected bytes after script: %d", len(rest))
	}
	if len(script) <= dnfproto.LegacyChannelHeaderSize {
		t.Fatalf("script response too short: %d", len(script))
	}
	if !session.scriptSent {
		t.Fatal("expected scriptSent after get script")
	}
	handled, err = service.handleLegacyChannelRequest(reader, session)
	if err != nil {
		t.Fatalf("handle ask after probe: %v", err)
	}
	if !handled {
		t.Fatal("expected ask handled after probe")
	}
	if !session.closeAfterBootstrapDirectory {
		t.Fatal("bootstrap directory must close the legacy connection so the client commits it")
	}
	_, rest = splitLegacyPacket(t, conn.write.Bytes(), legacyMsgID(dnfenum.LegacyMsgDofScript))
	info, rest := splitLegacyPacket(t, rest, legacyMsgID(dnfenum.LegacyMsgChannelInfo))
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing bytes: %d", len(rest))
	}
	if len(info) <= dnfproto.LegacyChannelHeaderSize {
		t.Fatalf("response too short: %d", len(info))
	}
}

func TestHandleChannelUpdateReturnsLoginChannelEndpoint(t *testing.T) {
	catalog := testChannelCatalog(t)
	service := &Service{
		options: options{
			serverIP: "42.240.165.245",
		},
		catalog: catalog,
	}
	conn := &bufferConn{}

	err := service.handleChannelPacket(&channelSession{conn: conn}, dnfproto.ChannelPacket{
		Header: dnfproto.ChannelHeader{
			Classification: dnfproto.DefaultChannelClassification,
			MsgID:          uint16(dnfenum.MsgCSUpdateChannelInfo),
		},
	})
	if err != nil {
		t.Fatalf("handle channel update: %v", err)
	}
	packet, err := dnfproto.ParseChannelPacket(conn.write.Bytes())
	if err != nil {
		t.Fatalf("parse update response: %v", err)
	}
	if packet.Header.MsgID != uint16(dnfenum.MsgSCAskChannelInfo) {
		t.Fatalf("msg id = %d, want %d", packet.Header.MsgID, dnfenum.MsgSCAskChannelInfo)
	}
	if len(packet.Body) != 32 {
		t.Fatalf("body len = %d, want 32", len(packet.Body))
	}
	if got := binary.LittleEndian.Uint32(packet.Body[20:24]); got != dnfenum.GamePortBase+11 {
		t.Fatalf("port = %d, want %d", got, dnfenum.GamePortBase+11)
	}
	if got := binary.LittleEndian.Uint32(packet.Body[24:28]); got != dnfenum.DefaultChannelMaxUsers {
		t.Fatalf("max users = %d, want %d", got, dnfenum.DefaultChannelMaxUsers)
	}
}

func TestHandleChannelUpdateUsesResidentChannel11Endpoint(t *testing.T) {
	catalog := testCatalogWithoutAutoChannel(t)
	service := &Service{
		options: options{
			serverIP:           "42.240.165.245",
			channelServerID:    1,
			channelAdvertiseID: 0,
		},
		catalog: catalog,
	}
	conn := &bufferConn{}

	err := service.handleChannelPacket(&channelSession{conn: conn}, dnfproto.ChannelPacket{
		Header: dnfproto.ChannelHeader{
			Classification: dnfproto.DefaultChannelClassification,
			MsgID:          uint16(dnfenum.MsgCSUpdateChannelInfo),
		},
	})
	if err != nil {
		t.Fatalf("handle channel update: %v", err)
	}
	packet, err := dnfproto.ParseChannelPacket(conn.write.Bytes())
	if err != nil {
		t.Fatalf("parse update response: %v", err)
	}
	if got := binary.LittleEndian.Uint32(packet.Body[20:24]); got != dnfenum.GamePortBase+dnfenum.DefaultGameChannelID {
		t.Fatalf("port = %d, want %d", got, dnfenum.GamePortBase+dnfenum.DefaultGameChannelID)
	}
}

func decryptCompressedChannelData(data []byte, key string) ([]byte, error) {
	encrypted, err := zlibDecompress(data)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, aes.BlockSize)
	copy(keyBytes, []byte(key))
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(encrypted))
	for offset := 0; offset < len(encrypted); offset += aes.BlockSize {
		block.Decrypt(out[offset:offset+aes.BlockSize], encrypted[offset:offset+aes.BlockSize])
	}
	return out, nil
}

type channelEntryWant struct {
	name string
	port int
}

func assertChannelInfoHasEntries(t *testing.T, raw []byte, count int, wants ...channelEntryWant) {
	t.Helper()
	got := binary.LittleEndian.Uint32(raw[8:12])
	if got != uint32(count) {
		t.Fatalf("channel count = %d, want %d", got, count)
	}
	for _, want := range wants {
		if !hasChannelEntryFrom(raw, 12, int(got), want.name, want.port) {
			t.Fatalf("channel body missing %s/%d: %x", want.name, want.port, raw)
		}
	}
}

func hasChannelEntryFrom(raw []byte, offset int, count int, name string, port int) bool {
	for i := 0; i < count; i++ {
		if offset+48 > len(raw) {
			return false
		}
		entryName, entryPort := channelEntryNamePort(raw, offset)
		if entryName == name && entryPort == port {
			return true
		}
		offset += 48
	}
	return false
}

func channelEntryNamePort(raw []byte, offset int) (string, int) {
	if offset+48 > len(raw) {
		return "", 0
	}
	return trimFixedString(raw[offset : offset+20]), int(binary.LittleEndian.Uint32(raw[offset+44 : offset+48]))
}

func trimFixedString(data []byte) string {
	if end := bytes.IndexByte(data, 0); end >= 0 {
		data = data[:end]
	}
	return string(bytes.TrimSpace(data))
}

func splitLegacyPacket(t *testing.T, data []byte, msgID byte) ([]byte, []byte) {
	t.Helper()
	if len(data) < dnfproto.LegacyChannelHeaderSize {
		t.Fatalf("legacy packet too short: %d", len(data))
	}
	if data[0] != dnfenum.ChannelPacketClass || data[1] != msgID {
		t.Fatalf("legacy header = %x, want class=%02x msg=%02x", data[:dnfproto.LegacyChannelHeaderSize], dnfenum.ChannelPacketClass, msgID)
	}
	total := int(binary.LittleEndian.Uint32(data[2:6]))
	if total < dnfproto.LegacyChannelHeaderSize || total > len(data) {
		t.Fatalf("legacy length = %d, data len = %d", total, len(data))
	}
	if data[10] != 1 {
		t.Fatalf("legacy seq = %d, want 1", data[10])
	}
	return data[:total], data[total:]
}

func splitChannelPacket(t *testing.T, data []byte) (dnfproto.ChannelPacket, []byte) {
	t.Helper()
	if len(data) < dnfproto.ChannelHeaderSize {
		t.Fatalf("channel packet too short: %d", len(data))
	}
	total := int(binary.LittleEndian.Uint32(data[3:7]))
	if total < dnfproto.ChannelHeaderSize || total > len(data) {
		t.Fatalf("channel length = %d, data len = %d", total, len(data))
	}
	packet, err := dnfproto.ParseChannelPacket(data[:total])
	if err != nil {
		t.Fatalf("parse channel packet: %v", err)
	}
	return packet, data[total:]
}

func splitGameServerUpperPacket(t *testing.T, data []byte) (dnfproto.ChannelPacket, []byte) {
	t.Helper()
	return splitGameServerUpperPacketWithHeader(t, data, dnfproto.GameServerUpperHeaderSize)
}

func splitLongHengGameServerUpperPacket(t *testing.T, data []byte) (dnfproto.ChannelPacket, []byte) {
	t.Helper()
	return splitGameServerUpperPacketWithHeader(t, data, dnfproto.GameServerUpperHeaderSize16)
}

func splitFixed15CharacterRosterPacket(t *testing.T, data []byte) ([]byte, []byte) {
	t.Helper()
	const headerSize = 15
	if len(data) < headerSize {
		t.Fatalf("fixed15 character roster too short: %d", len(data))
	}
	total := int(binary.LittleEndian.Uint32(data[3:7]))
	if total < headerSize || total > len(data) {
		t.Fatalf("fixed15 character roster length=%d data=%d", total, len(data))
	}
	if data[0] != byte(dnfenum.GameCmdNotice) ||
		binary.LittleEndian.Uint16(data[1:3]) != uint16(dnfenum.GameTypeCharacterList) ||
		data[7] != latestCharacterStateRoute ||
		!bytes.Equal(data[8:15], make([]byte, 7)) {
		t.Fatalf("fixed15 character roster header=%x", data[:headerSize])
	}
	return append([]byte(nil), data[headerSize:total]...), data[total:]
}

func splitCurrentSceneItemListPacket(t *testing.T, data []byte) (dnfproto.ChannelPacket, []byte) {
	t.Helper()
	packet, rest := splitLongHengGameServerUpperPacket(t, data)
	if packet.Header.Classification != 0 || packet.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) {
		t.Fatalf("current scene item list packet = class %d msg %d", packet.Header.Classification, packet.Header.MsgID)
	}
	if !bytes.Equal(data[11:16], make([]byte, 5)) {
		t.Fatalf("current scene item list fixed header tail=%x want zero", data[11:16])
	}
	return packet, rest
}

// splitCurrentGameServerUpperPacketAuto is only for streams that contain the
// current scene item-list/update packets among ordinary upper packets. These
// are recognizable by their class0 message id plus the required five zero
// bytes at header offsets 11..15; all other packets retain their parser.
func splitCurrentGameServerUpperPacketAuto(t *testing.T, data []byte) (dnfproto.ChannelPacket, []byte) {
	t.Helper()
	if len(data) >= dnfproto.GameServerUpperHeaderSize16 && data[0] == 0 && bytes.Equal(data[11:16], make([]byte, 5)) {
		switch binary.LittleEndian.Uint16(data[1:3]) {
		case uint16(dnfenum.CmdPacketLeaveParty), uint16(dnfenum.CmdPacketWalkoutPartyMember), currentTownActorSceneSnapshotMsgID, currentTownUserPositionNotificationMsgID, currentTownUserAreaNotificationMsgID:
			return splitGameServerUpperPacketWithHeader(t, data, dnfproto.GameServerUpperHeaderSize16)
		}
	}
	return splitGameServerUpperPacket(t, data)
}

func splitCSharpSelectInitPacket(t *testing.T, data []byte, want csharpSelectInitPacket) (dnfproto.ChannelPacket, []byte) {
	t.Helper()
	if want.bodyEncoded || want.marker != 0 || want.bodyCodec != "" {
		if len(data) < dnfproto.GameServerUpperHeaderSize16 {
			t.Fatalf("DOVE select packet too short for protect flag: %d", len(data))
		}
		if got := data[15]; got != byte(want.marker) {
			t.Fatalf("DOVE select packet protect flag = %d, want %d for msg %d", got, want.marker, want.msgID)
		}
		packet, rest := splitLongHengGameServerUpperPacket(t, data)
		if packet.Header.Checksum != want.marker {
			t.Fatalf("DOVE select packet marker = %d, want %d for msg %d", packet.Header.Checksum, want.marker, want.msgID)
		}
		return packet, rest
	}
	return splitGameServerUpperPacket(t, data)
}

func assertLongHengSceneStageTransportObjectKey(t *testing.T, body []byte, want uint16) {
	t.Helper()
	plain, err := zlibDecompress(body)
	if err != nil {
		t.Fatalf("scene-stage transport zlib decode: %v", err)
	}
	wantKey := make([]byte, 2)
	fixtureKey := make([]byte, 2)
	binary.LittleEndian.PutUint16(wantKey, want)
	binary.LittleEndian.PutUint16(fixtureKey, longHengSceneStageFixtureObjectKey)
	if got := bytes.Count(plain, wantKey); got == 0 {
		t.Fatalf("scene-stage transport missing object key %#x in expanded body len=%d", want, len(plain))
	}
	if got := bytes.Count(plain, fixtureKey); got != 0 {
		t.Fatalf("scene-stage transport still contains fixture object key %#x count=%d", longHengSceneStageFixtureObjectKey, got)
	}
}

func splitGameServerUpperPacketWithHeader(t *testing.T, data []byte, headerSize int) (dnfproto.ChannelPacket, []byte) {
	t.Helper()
	if len(data) < headerSize {
		t.Fatalf("game upper packet too short: %d", len(data))
	}
	total := int(binary.LittleEndian.Uint32(data[3:7]))
	if total < headerSize || total > len(data) {
		t.Fatalf("game upper length = %d, data len = %d", total, len(data))
	}
	if headerSize == dnfproto.GameServerUpperHeaderSize16 && (data[13] != 0 || data[14] != 0) {
		t.Fatalf("game upper reserved bytes = %x", data[13:15])
	}
	packet := dnfproto.ChannelPacket{
		Header: dnfproto.ChannelHeader{
			Classification: data[0],
			MsgID:          binary.LittleEndian.Uint16(data[1:3]),
			Length:         binary.LittleEndian.Uint32(data[3:7]),
			Checksum:       binary.LittleEndian.Uint32(data[7:11]),
			Seq:            binary.LittleEndian.Uint16(data[11:13]),
		},
		Body: append([]byte(nil), data[headerSize:total]...),
	}
	return packet, data[total:]
}

func splitWrittenGameRecords(data []byte) ([]dnfproto.LatestGameTransportRecord, error) {
	records := make([]dnfproto.LatestGameTransportRecord, 0)
	for len(data) > 0 {
		frames, remaining, skipped := dnfproto.SplitLatestGameTCPFrames(data)
		if skipped != 0 || len(frames) == 0 {
			return nil, dnfproto.ErrPacketLength
		}
		for _, frame := range frames {
			frameRecords, err := dnfproto.ParseLatestGameTCPRecords(frame)
			if err != nil {
				return nil, err
			}
			records = append(records, frameRecords...)
		}
		data = remaining
	}
	return records, nil
}

func assertRosterSlots(t *testing.T, body []byte, count int) {
	t.Helper()
	if len(body) != 25 {
		t.Fatalf("roster slot body len = %d, want 25", len(body))
	}
	if body[0] != 2 ||
		body[1] != byte(latestRosterRouteNormal) ||
		body[2] != byte(latestRosterContextNormal) ||
		binary.LittleEndian.Uint16(body[3:5]) != uint16(defaultCharacterSlots) ||
		binary.LittleEndian.Uint16(body[5:7]) != 0 ||
		binary.LittleEndian.Uint16(body[7:9]) != 0 ||
		binary.LittleEndian.Uint32(body[9:13]) != 0 ||
		binary.LittleEndian.Uint16(body[13:15]) != 0 {
		t.Fatalf("roster slot body mismatch: %x", body)
	}
}

func assertRosterChars(t *testing.T, body []byte, characters []dnfrepo.CharacterRecord) {
	t.Helper()
	count := rosterCount(characters)
	if len(body) != 3+count*112 {
		t.Fatalf("roster char body len = %d, want %d", len(body), 3+count*112)
	}
	if body[0] != 3 || binary.LittleEndian.Uint16(body[1:3]) != uint16(count) {
		t.Fatalf("roster char header mismatch: %x", body[:3])
	}
	for idx := 0; idx < count; idx++ {
		character := characters[idx]
		base := 3 + idx*112
		charID := uint32(numericCharacterID(character))
		if body[base] != byte(latestRosterRouteNormal) || body[base+1] != byte(latestRosterContextNormal) {
			t.Fatalf("roster char[%d] context = %x", idx, body[base:base+2])
		}
		if binary.LittleEndian.Uint16(body[base+10:base+12]) != uint16(charID) {
			t.Fatalf("roster char[%d] object id = %x", idx, body[base+10:base+12])
		}
		if body[base+12] != latestCharacterStateActive || body[base+13] != 0 {
			t.Fatalf("roster char[%d] state = %x", idx, body[base+12:base+14])
		}
		if binary.LittleEndian.Uint32(body[base+14:base+18]) != charID {
			t.Fatalf("roster char[%d] stat id mismatch", idx)
		}
		if binary.LittleEndian.Uint16(body[base+35:base+37]) != uint16(character.Slot) ||
			binary.LittleEndian.Uint32(body[base+37:base+41]) != charID {
			t.Fatalf("roster char[%d] slot/id section mismatch", idx)
		}
	}
}

func TestRosterBodyDefaultsToNormalCharacter(t *testing.T) {
	characters := []dnfrepo.CharacterRecord{{
		CharacterID: "1",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        0,
		Name:        "hero",
		Job:         "0",
		Level:       1,
	}}
	slots := buildRosterSlots(characters)
	chars := buildRosterChars(characters)

	assertRosterSlots(t, slots, 1)
	assertRosterChars(t, chars, characters)
	if got := chars[3+30]; got != 0 {
		t.Fatalf("roster char appearance flag count = %d, want 0", got)
	}
	if got := chars[3+111]; got != 0 {
		t.Fatalf("roster char extra object count = %d, want 0", got)
	}
}

func TestRosterBodyEncodesNameAsUTF16(t *testing.T) {
	characters := []dnfrepo.CharacterRecord{{
		CharacterID: "1",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        0,
		Name:        "对对对",
		Job:         "0",
		Level:       1,
	}}
	body := buildRosterChars(characters)

	assertRosterChars(t, body, characters)
	nameStart := 3 + 41
	want := []byte{0xf9, 0x5b, 0xf9, 0x5b, 0xf9, 0x5b}
	if got := body[nameStart : nameStart+len(want)]; !bytes.Equal(got, want) {
		t.Fatalf("roster utf16 name bytes = %x, want %x", got, want)
	}
	if got := body[nameStart+len(want) : nameStart+64]; !bytes.Equal(got, make([]byte, 64-len(want))) {
		t.Fatalf("roster utf16 name padding is not zero: %x", got)
	}
}

func assertCSharpRosterBody(t *testing.T, body []byte, count int) {
	t.Helper()
	if len(body) < 15 {
		t.Fatalf("csharp roster body too short: %d", len(body))
	}
	if body[0] != 2 ||
		body[1] != byte(count) ||
		body[2] != 5 ||
		binary.LittleEndian.Uint16(body[3:5]) != noPackRosterWireSlotLimit ||
		binary.LittleEndian.Uint16(body[5:7]) != noPackRosterWireSlotLimit ||
		binary.LittleEndian.Uint16(body[7:9]) != uint16(count) ||
		binary.LittleEndian.Uint32(body[9:13]) != 0 ||
		binary.LittleEndian.Uint16(body[13:15]) != uint16(count) {
		t.Fatalf("csharp roster header mismatch: %x", body[:15])
	}
}

func noPackRosterEntryLen(character dnfrepo.CharacterRecord) int {
	return 2 + 4 + len(rosterRawNameBytes(character)) + noPackRosterEntryTailBytes + noPackRosterEquipRowsLen(character)
}

func noPackRosterJobStart(entryStart int, character dnfrepo.CharacterRecord) int {
	return entryStart + 2 + 4 + len(rosterRawNameBytes(character)) + noPackRosterPreJobBytes
}

func noPackRosterGrowStart(entryStart int, character dnfrepo.CharacterRecord) int {
	return noPackRosterJobStart(entryStart, character) + 1
}

func noPackRosterLevelStart(entryStart int, character dnfrepo.CharacterRecord) int {
	return noPackRosterGrowStart(entryStart, character) + 1
}

func noPackRosterAppearanceStart(entryStart int, character dnfrepo.CharacterRecord) int {
	return noPackRosterLevelStart(entryStart, character) + 1 + 10
}

func noPackRosterPostEquipStart(entryStart int, character dnfrepo.CharacterRecord) int {
	return noPackRosterAppearanceStart(entryStart, character) + 1 + noPackRosterEquipRowsLen(character)
}

func noPackRosterEquipRowsLen(character dnfrepo.CharacterRecord) int {
	return len(currentRosterEquipSummaryRows(characterEquipSummary(character))) * noPackRosterEquipRowBytes
}

func rosterDstrName(value string) []byte {
	var writer packetWriter
	writer.writeRawDstr(rosterNameBytes(value))
	return writer.bytes()
}

func normalNoPackRosterPostEquipBytes() []byte {
	var writer packetWriter
	writeNoPackRosterNormalPostEquip(&writer)
	return writer.bytes()
}

func TestCSharpRosterBodyEmptyEchoesSlotLimitNoPushTail(t *testing.T) {
	body := buildCSharpRosterBody(nil)

	assertCSharpRosterBody(t, body, 0)
	if got := body[:15]; !bytes.Equal(got, []byte{2, 0, 5, byte(noPackRosterWireSlotLimit), 0, byte(noPackRosterWireSlotLimit), 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("empty csharp roster header = %x", got)
	}
	if got := body[15:25]; !bytes.Equal(got, []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("empty csharp roster tail = %x, want page/no-push tail", got)
	}
}

func TestCSharpRosterBodyEchoesSlotLimitAndActualEntryCount(t *testing.T) {
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{{
		CharacterID: "1",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        0,
		Name:        "hero",
		Job:         "0",
		Level:       1,
		Roster: dnfrepo.CharacterRoster{
			Header: dnfrepo.CharacterRosterHeader{
				UnkA:             8,
				UnkB:             8,
				TotalOrSlotLimit: 8,
				UsedOrRemain:     7,
				SelectedOrPage:   7,
				RosterState:      6,
				PageCount:        9,
				RosterFlag:       9,
				RosterValueA:     0x11111111,
				RosterValueB:     0x22222222,
			},
		},
	}})

	assertCSharpRosterBody(t, body, 1)
	if got := body[1]; got != 1 {
		t.Fatalf("roster display count = %d, want 1", got)
	}
	if got := body[2]; got != 5 {
		t.Fatalf("roster fixed flag = %d, want 5", got)
	}
	if got := binary.LittleEndian.Uint16(body[3:5]); got != noPackRosterWireSlotLimit {
		t.Fatalf("stale roster TotalOrSlotLimit leaked into header: got %d", got)
	}
	if got := binary.LittleEndian.Uint16(body[5:7]); got != noPackRosterWireSlotLimit {
		t.Fatalf("roster echoed slot limit = %d, want %d", got, noPackRosterWireSlotLimit)
	}
	if got := binary.LittleEndian.Uint16(body[13:15]); got != 1 {
		t.Fatalf("roster entry count = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(body[9:13]); got != 0 {
		t.Fatalf("stale roster state leaked into header: got %d", got)
	}
	tailStart := 15 + noPackRosterEntryLen(dnfrepo.CharacterRecord{Name: "hero", Level: 1})
	if got := body[tailStart : tailStart+10]; !bytes.Equal(got, []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("stale roster tail leaked into header: got %x", got)
	}
}

func TestCSharpRosterBodyIgnoresStaleSelectedPage(t *testing.T) {
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{{
		CharacterID: "1",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        0,
		Name:        "hero",
		Job:         "0",
		Level:       1,
		Roster: dnfrepo.CharacterRoster{
			Header: dnfrepo.CharacterRosterHeader{
				SelectedOrPage: 7,
			},
		},
	}})

	assertCSharpRosterBody(t, body, 1)
	if got := binary.LittleEndian.Uint16(body[7:9]); got != 1 {
		t.Fatalf("roster shown character count = %d, want 1", got)
	}
}

func TestCSharpRosterBodyUsesSQLSlotAsEntryKey(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "3",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        5,
		Name:        "hero",
		Job:         "0",
		Level:       1,
		Roster: dnfrepo.CharacterRoster{
			Entry: dnfrepo.CharacterRosterEntry{
				ObjectID: 99,
			},
		},
	}
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{character})

	if got := binary.LittleEndian.Uint16(body[15:17]); got != 5 {
		t.Fatalf("roster entry key = %d, want slot 5", got)
	}
	postEquipStart := noPackRosterPostEquipStart(15, character)
	if got, want := body[postEquipStart:postEquipStart+noPackRosterPostEquipBytes], normalNoPackRosterPostEquipBytes(); !bytes.Equal(got, want) {
		t.Fatalf("roster stale object/link state leaked after equip summary: %x", got)
	}
}

func TestCSharpRosterBodyPreservesEquipmentSummaryOnCharacterSelect(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "3",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        5,
		Name:        "hero",
		Job:         "11",
		Level:       1,
		Roster: dnfrepo.CharacterRoster{
			Entry: dnfrepo.CharacterRosterEntry{
				EquipSummary: []dnfrepo.CharacterRosterEquipSummary{
					{Slot: 0, ItemIDOrIcon: 1001, PackedFlags: 1, OptionalIDOrExpire: 2, AuxValue: 3, AuxFlag: 4},
					{Slot: 24, ItemIDOrIcon: 2002, PackedFlags: 5, OptionalIDOrExpire: 6, AuxValue: 7, AuxFlag: 8},
				},
			},
		},
	}
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{character})

	assertCSharpRosterBody(t, body, 1)
	entryStart := 15
	tailStart := entryStart + noPackRosterEntryLen(character)
	if got, want := len(body), tailStart+10; got != want {
		t.Fatalf("roster body len = %d, want %d with equipment summary", got, want)
	}
	equipSummaryStart := noPackRosterAppearanceStart(entryStart, character)
	if got := body[equipSummaryStart]; got != 2 {
		t.Fatalf("roster equip summary count = %d, want 2", got)
	}
	postEquipStart := noPackRosterPostEquipStart(entryStart, character)
	if got, want := body[postEquipStart:tailStart], normalNoPackRosterPostEquipBytes(); !bytes.Equal(got, want) {
		t.Fatalf("roster post-equipment tail = %x, want %x", got, want)
	}
}

func TestCurrentRosterEquipSummaryRowsRejectInvalidRows(t *testing.T) {
	rows := currentRosterEquipSummaryRows([]dnfrepo.CharacterRosterEquipSummary{
		{Slot: 8, ItemIDOrIcon: 1001},
		{Slot: 9, ItemIDOrIcon: 1002, OptionalIDOrExpire: 77, RawEntry: []byte{1, 2, 3}},
		{Slot: 33, ItemIDOrIcon: 1003},
		{Slot: 10, ItemIDOrIcon: -1},
		{Slot: 12, ItemIDOrIcon: 1004, PackedFlags: 3, AuxValue: 4, AuxFlag: 5},
	})
	if len(rows) != 3 {
		t.Fatalf("safe roster equipment row count = %d, want 3: %+v", len(rows), rows)
	}
	if rows[0].Slot != 8 || rows[0].ItemIDOrIcon != 1001 || rows[1].Slot != 9 || rows[1].ItemIDOrIcon != 1002 || rows[2].Slot != 12 || rows[2].ItemIDOrIcon != 1004 {
		t.Fatalf("safe roster equipment rows = %+v", rows)
	}
}

func TestCSharpRosterBodyKeepsNextEntryAlignedAfterThreeEquipmentRows(t *testing.T) {
	characters := []dnfrepo.CharacterRecord{
		{
			CharacterID: "28",
			AccountID:   defaultAccountPrefix + "1",
			Slot:        13,
			Name:        "first",
			Job:         "15",
			Level:       1,
			Roster: dnfrepo.CharacterRoster{Entry: dnfrepo.CharacterRosterEntry{
				EquipSummary: []dnfrepo.CharacterRosterEquipSummary{
					{Slot: 15, ItemIDOrIcon: 12400},
					{Slot: 11, ItemIDOrIcon: 116000011},
					{Slot: 13, ItemIDOrIcon: 10400},
				},
			}},
		},
		{
			CharacterID: "29",
			AccountID:   defaultAccountPrefix + "1",
			Slot:        14,
			Name:        "second",
			Job:         "0",
			Level:       1,
		},
	}
	body := buildCSharpRosterBody(characters)

	assertCSharpRosterBody(t, body, 2)
	firstEquipStart := noPackRosterAppearanceStart(15, characters[0])
	if got := body[firstEquipStart]; got != 3 {
		t.Fatalf("first roster equipment count = %d, want 3", got)
	}
	secondStart := 15 + noPackRosterEntryLen(characters[0])
	if got := binary.LittleEndian.Uint16(body[secondStart : secondStart+2]); got != 14 {
		t.Fatalf("second roster key = %d, want slot 14", got)
	}
	secondName := rosterDstrName(characters[1].Name)
	if got := body[secondStart+2 : secondStart+2+len(secondName)]; !bytes.Equal(got, secondName) {
		t.Fatalf("second roster name = %x, want %x", got, secondName)
	}
	if got := body[noPackRosterJobStart(secondStart, characters[1])]; got != 0 {
		t.Fatalf("second roster job = %d, want 0", got)
	}
	if got := body[noPackRosterGrowStart(secondStart, characters[1])]; got != 0 {
		t.Fatalf("second roster grow = %d, want 0", got)
	}
	if got := body[noPackRosterLevelStart(secondStart, characters[1])]; got != 1 {
		t.Fatalf("second roster level = %d, want 1", got)
	}
}

func TestCSharpRosterBodyUsesSlotsAndActualEntryCountForSparseSQLSlots(t *testing.T) {
	characters := []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: defaultAccountPrefix + "1", Slot: 0, Name: "a", Job: "0", Level: 1},
		{CharacterID: "2", AccountID: defaultAccountPrefix + "1", Slot: 7, Name: "b", Job: "1", Level: 1},
	}
	body := buildCSharpRosterBody(characters)

	assertCSharpRosterBody(t, body, 2)
	if got := binary.LittleEndian.Uint16(body[3:5]); got != noPackRosterWireSlotLimit {
		t.Fatalf("roster total slots = %d, want %d", got, noPackRosterWireSlotLimit)
	}
	if got := binary.LittleEndian.Uint16(body[5:7]); got != noPackRosterWireSlotLimit {
		t.Fatalf("roster echoed slot limit = %d, want %d", got, noPackRosterWireSlotLimit)
	}
	if got := binary.LittleEndian.Uint16(body[13:15]); got != 2 {
		t.Fatalf("roster entry count = %d, want 2", got)
	}
	secondEntry := 15 + noPackRosterEntryLen(characters[0])
	if got := binary.LittleEndian.Uint16(body[secondEntry : secondEntry+2]); got != 7 {
		t.Fatalf("second roster entry key = %d, want slot 7", got)
	}
}

func TestCSharpRosterBodyFiveCharactersStillEchoesConfiguredSlots(t *testing.T) {
	characters := []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: defaultAccountPrefix + "1", Slot: 0, Name: "a", Job: "0", Level: 86},
		{CharacterID: "2", AccountID: defaultAccountPrefix + "1", Slot: 1, Name: "b", Job: "1", Level: 1},
		{CharacterID: "3", AccountID: defaultAccountPrefix + "1", Slot: 2, Name: "c", Job: "2", Level: 1},
		{CharacterID: "4", AccountID: defaultAccountPrefix + "1", Slot: 3, Name: "d", Job: "3", Level: 1},
		{CharacterID: "5", AccountID: defaultAccountPrefix + "1", Slot: 4, Name: "e", Job: "4", Level: 1},
	}
	body := buildCSharpRosterBody(characters)

	assertCSharpRosterBody(t, body, len(characters))
	if got := binary.LittleEndian.Uint16(body[3:5]); got != noPackRosterWireSlotLimit {
		t.Fatalf("roster total slots = %d, want %d", got, noPackRosterWireSlotLimit)
	}
	if got := binary.LittleEndian.Uint16(body[5:7]); got != noPackRosterWireSlotLimit {
		t.Fatalf("roster echoed slot limit = %d, want %d", got, noPackRosterWireSlotLimit)
	}
	if got := binary.LittleEndian.Uint16(body[7:9]); got != uint16(len(characters)) {
		t.Fatalf("roster shown character count = %d, want %d", got, len(characters))
	}
}

func TestCSharpRosterBodyThirtyTwoCharactersTruncatesToWireLimit(t *testing.T) {
	characters := make([]dnfrepo.CharacterRecord, 0, defaultCharacterSlots)
	for slot := 0; slot < defaultCharacterSlots; slot++ {
		id := strconv.Itoa(slot + 1)
		characters = append(characters, dnfrepo.CharacterRecord{
			CharacterID: id,
			AccountID:   "dnf:1",
			Slot:        slot,
			Name:        "character-" + id,
			Job:         "0",
			Level:       1,
		})
	}
	body := buildCSharpRosterBody(characters)
	assertCSharpRosterBody(t, body, noPackRosterWireSlotLimit)
	lastEntry := 15
	for _, character := range characters[:noPackRosterWireSlotLimit-1] {
		lastEntry += noPackRosterEntryLen(character)
	}
	if got := binary.LittleEndian.Uint16(body[lastEntry : lastEntry+2]); got != noPackRosterWireSlotLimit-1 {
		t.Fatalf("last roster entry key = %d, want slot %d", got, noPackRosterWireSlotLimit-1)
	}
	lastEnd := lastEntry + noPackRosterEntryLen(characters[noPackRosterWireSlotLimit-1])
	if len(body) != lastEnd+10 {
		t.Fatalf("wire-limited roster body len = %d, want %d", len(body), lastEnd+10)
	}

	request31 := make([]byte, 16)
	binary.LittleEndian.PutUint32(request31[:4], 31)
	if parsed, ok := parseNoPackPlainSelectCharacterRequest(request31); !ok || parsed.slot != 31 {
		t.Fatalf("slot31 select parsed=%+v ok=%t", parsed, ok)
	}
	request32 := make([]byte, 16)
	binary.LittleEndian.PutUint32(request32[:4], 32)
	if parsed, ok := parseNoPackPlainSelectCharacterRequest(request32); ok {
		t.Fatalf("slot32 select unexpectedly accepted: %+v", parsed)
	}
}

func TestCSharpRosterBodyScrubsMySQLRosterStateColumns(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "9",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        1,
		Name:        "hero",
		Job:         "4",
		Level:       7,
		Stats: map[string]int64{
			"grow_type":            2,
			"roster_state0":        2,
			"roster_time_a":        0x01020304,
			"roster_time_b":        0x05060708,
			"roster_value0":        0x11121314,
			"roster_value1":        0x21,
			"roster_value2":        0x22,
			"roster_reserved_a":    0x23,
			"roster_reserved_b":    0x24,
			"roster_linked_id_00":  1,
			"roster_linked_id_01":  2,
			"roster_linked_id_02":  3,
			"roster_linked_id_03":  4,
			"roster_linked_id_04":  5,
			"roster_linked_id_05":  6,
			"roster_linked_id_06":  7,
			"roster_linked_id_07":  8,
			"roster_value3":        0x31,
			"roster_object_id":     0x1234,
			"roster_flag0_eq1":     1,
			"roster_card_flag":     2,
			"roster_value5":        3,
			"roster_display_flags": 4,
			"roster_tail_00":       5,
			"roster_tail_11":       16,
			"roster_flag6_eq1":     17,
		},
	}
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{character})

	assertCSharpRosterBody(t, body, 1)
	entryStart := 15
	jobStart := noPackRosterJobStart(entryStart, character)
	growStart := noPackRosterGrowStart(entryStart, character)
	levelStart := noPackRosterLevelStart(entryStart, character)
	appearanceStart := noPackRosterAppearanceStart(entryStart, character)
	if got := body[jobStart]; got != 4 {
		t.Fatalf("roster job byte = %d, want 4", got)
	}
	if got := body[growStart]; got != 2 {
		t.Fatalf("roster grow byte = %d, want 2", got)
	}
	if got := body[levelStart]; got != 7 {
		t.Fatalf("roster level byte = %d, want 7", got)
	}
	if got := body[levelStart+1 : appearanceStart]; !bytes.Equal(got, make([]byte, appearanceStart-levelStart-1)) {
		t.Fatalf("roster reserved bytes after level = %x, want zero", got)
	}
	if got := body[appearanceStart]; got != 0 {
		t.Fatalf("roster equip summary count = %d, want 0", got)
	}
	value0Start := appearanceStart + 1
	if got := binary.LittleEndian.Uint32(body[value0Start : value0Start+4]); got != 0 {
		t.Fatalf("roster value0 = %#x", got)
	}
	if got := body[value0Start+4 : value0Start+8]; !bytes.Equal(got, []byte{0, 0, 0, 0}) {
		t.Fatalf("roster byte values = %x", got)
	}
	if got, want := body[value0Start:value0Start+noPackRosterPostEquipBytes], normalNoPackRosterPostEquipBytes(); !bytes.Equal(got, want) {
		t.Fatalf("roster post-equip bytes = %x, want %x", got, want)
	}
}

func TestCSharpRosterBodyDefaultsToNormalCharacter(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "1",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        0,
		Name:        "hero",
		Job:         "0",
		Level:       1,
	}
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{character})

	assertCSharpRosterBody(t, body, 1)
	entryStart := 15
	tailStart := entryStart + noPackRosterEntryLen(character)
	jobStart := noPackRosterJobStart(entryStart, character)
	growStart := noPackRosterGrowStart(entryStart, character)
	levelStart := noPackRosterLevelStart(entryStart, character)
	if got, want := len(body), tailStart+10; got != want {
		t.Fatalf("roster body len = %d, want fixed entry len %d", got, want)
	}
	nameStart := entryStart + 2
	wantName := rosterDstrName("hero")
	if got := body[nameStart : nameStart+len(wantName)]; !bytes.Equal(got, wantName) {
		t.Fatalf("roster dstr name = %x, want %x", got, wantName)
	}
	preJobStart := nameStart + len(wantName)
	if got := body[preJobStart:jobStart]; !bytes.Equal(got, make([]byte, noPackRosterPreJobBytes)) {
		t.Fatalf("roster pre-job reserved bytes = %x, want zero", got)
	}
	if got := body[jobStart]; got != 0 {
		t.Fatalf("roster job byte = %d, want 0", got)
	}
	if got := body[growStart]; got != 0 {
		t.Fatalf("roster grow byte = %d, want 0", got)
	}
	if got := body[levelStart]; got != 1 {
		t.Fatalf("roster level byte = %d, want 1", got)
	}
	appearanceStart := noPackRosterAppearanceStart(entryStart, character)
	if got := body[levelStart+1 : appearanceStart]; !bytes.Equal(got, make([]byte, appearanceStart-levelStart-1)) {
		t.Fatalf("roster post-level reserved bytes = %x, want zero", got)
	}
	if got := body[appearanceStart]; got != 0 {
		t.Fatalf("roster equip summary count = %d, want 0", got)
	}
	postEquipStart := noPackRosterPostEquipStart(entryStart, character)
	if got, want := body[postEquipStart:tailStart], normalNoPackRosterPostEquipBytes(); !bytes.Equal(got, want) {
		t.Fatalf("roster post-equip normal-state bytes = %x, want %x", got, want)
	}
	if got := body[tailStart : tailStart+10]; !bytes.Equal(got, []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("post-entry roster tail = %x, want page/no-push tail", got)
	}
}

func TestCSharpRosterBodyScrubsStaleSpecialState(t *testing.T) {
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{{
		CharacterID: "21",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        4,
		Name:        "hero",
		Job:         "11",
		Level:       9,
		Roster: dnfrepo.CharacterRoster{
			Entry: dnfrepo.CharacterRosterEntry{
				PackedJobGrow:     0xff,
				ByteC:             7,
				Field2CC:          8,
				State0:            1,
				TimeA:             123,
				TimeB:             456,
				Value0:            9,
				Value1:            10,
				Value2:            11,
				ReservedA:         12,
				ReservedB:         13,
				LinkedIDBlock:     []int64{1, 2, 3, 4, 5, 6, 7, 8},
				Value3:            14,
				ObjectID:          77,
				Flag0Eq1:          1,
				SpecialStatusFlag: 1,
				Value5:            1,
				DisplayFlags:      0xff,
				ReservedC:         1,
				ReservedD:         1,
				Value6:            1,
				Flag1Nonzero:      1,
				BoolAEq1:          1,
				BoolBEq1:          1,
				BoolCEq1:          1,
				Flag2Nonzero:      1,
				Flag3Nonzero:      1,
				Flag4Nonzero:      1,
				Flag5Nonzero:      1,
				Value7:            1,
				Flag6Eq1:          1,
			},
		},
	}})

	assertCSharpRosterBody(t, body, 1)
	character := dnfrepo.CharacterRecord{
		CharacterID: "21",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        4,
		Name:        "hero",
		Job:         "11",
		Level:       9,
	}
	jobStart := noPackRosterJobStart(15, character)
	growStart := noPackRosterGrowStart(15, character)
	levelStart := noPackRosterLevelStart(15, character)
	if got := binary.LittleEndian.Uint16(body[15:17]); got != 4 {
		t.Fatalf("roster entry key = %d, want slot 4", got)
	}
	if got := body[levelStart]; got != 9 {
		t.Fatalf("roster level byte = %d, want 9", got)
	}
	if got := body[jobStart]; got != 11 {
		t.Fatalf("roster job byte = %d, want 11", got)
	}
	if got := body[growStart]; got != 0 {
		t.Fatalf("roster grow byte = %d, want 0", got)
	}
	appearanceStart := noPackRosterAppearanceStart(15, character)
	if got := body[levelStart+1 : appearanceStart]; !bytes.Equal(got, make([]byte, appearanceStart-levelStart-1)) {
		t.Fatalf("roster stale reserved bytes leaked: %x", got)
	}
	postEquipStart := noPackRosterPostEquipStart(15, character)
	if got, want := body[postEquipStart:postEquipStart+noPackRosterPostEquipBytes], normalNoPackRosterPostEquipBytes(); !bytes.Equal(got, want) {
		t.Fatalf("roster stale special/pvp state leaked after equip summary: %x", got)
	}
}

func TestCSharpRosterBodyEncodesJobGrowAndLevel(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "17",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        2,
		Name:        "hero",
		Job:         "4",
		Level:       56,
		Stats:       map[string]int64{"grow_type": 2},
	}
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{character})

	assertCSharpRosterBody(t, body, 1)
	jobStart := noPackRosterJobStart(15, character)
	growStart := noPackRosterGrowStart(15, character)
	levelStart := noPackRosterLevelStart(15, character)
	if got := binary.LittleEndian.Uint16(body[15:17]); got != 2 {
		t.Fatalf("roster entry key = %d, want slot 2", got)
	}
	if got := body[jobStart]; got != 4 {
		t.Fatalf("roster job byte = %d, want 4", got)
	}
	if got := body[growStart]; got != 2 {
		t.Fatalf("roster grow byte = %d, want 2", got)
	}
	if got := body[levelStart]; got != 56 {
		t.Fatalf("roster level byte = %d, want 56", got)
	}
}

func TestCSharpRosterBodyEncodesNameAsDSTR(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "1",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        0,
		Name:        "\u5bf9\u5bf9\u5bf9",
		Job:         "0",
		Level:       1,
	}
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{character})

	assertCSharpRosterBody(t, body, 1)
	entryStart := 15
	nameStart := entryStart + 2
	want := rosterDstrName(character.Name)
	if got := body[nameStart : nameStart+len(want)]; !bytes.Equal(got, want) {
		t.Fatalf("roster dstr name bytes = %x, want %x", got, want)
	}
}

func TestCSharpRosterBodyUsesMySQLNameOverStaleJSONName(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "1",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        0,
		Name:        "mysql-name",
		Job:         "0",
		Level:       1,
		Stats: map[string]int64{
			"create_name_len":     3,
			"create_name_byte_00": int64('b'),
			"create_name_byte_01": int64('a'),
			"create_name_byte_02": int64('d'),
		},
	}
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{character})

	assertCSharpRosterBody(t, body, 1)
	entryStart := 15
	nameStart := entryStart + 2
	want := rosterDstrName("mysql-name")
	if got := body[nameStart : nameStart+len(want)]; !bytes.Equal(got, want) {
		t.Fatalf("roster name bytes = %x, want MySQL name %x", got, want)
	}
}

func TestCSharpRosterBodyDoesNotReadGunbladerJobAsLevel(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		CharacterID: "13",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        1,
		Name:        "hero",
		Job:         "15",
		Level:       1,
	}
	body := buildCSharpRosterBody([]dnfrepo.CharacterRecord{character})

	assertCSharpRosterBody(t, body, 1)
	jobStart := noPackRosterJobStart(15, character)
	growStart := noPackRosterGrowStart(15, character)
	levelStart := noPackRosterLevelStart(15, character)
	if got := body[jobStart]; got != 15 {
		t.Fatalf("roster gunblader job byte = %d, want 15", got)
	}
	if got := body[growStart]; got != 0 {
		t.Fatalf("roster gunblader grow byte = %d, want 0", got)
	}
	if got := body[levelStart]; got != 1 {
		t.Fatalf("roster level byte = %d, want 1", got)
	}
}

func TestCSharpRosterBodyUsesExactEntryStride(t *testing.T) {
	characters := []dnfrepo.CharacterRecord{
		{
			CharacterID: "1",
			AccountID:   defaultAccountPrefix + "1",
			Slot:        0,
			Name:        "刚刚发给",
			Job:         "0",
			Level:       1,
		},
		{
			CharacterID: "2",
			AccountID:   defaultAccountPrefix + "1",
			Slot:        7,
			Name:        "测试",
			Job:         "15",
			Level:       1,
		},
	}
	body := buildCSharpRosterBody(characters)

	assertCSharpRosterBody(t, body, 2)
	secondStart := 15 + noPackRosterEntryLen(characters[0])
	if got := binary.LittleEndian.Uint16(body[secondStart : secondStart+2]); got != 7 {
		t.Fatalf("second roster entry key = %d, want slot 7", got)
	}
	secondNameStart := secondStart + 2
	secondName := rosterDstrName(characters[1].Name)
	if got := body[secondNameStart : secondNameStart+len(secondName)]; !bytes.Equal(got, secondName) {
		t.Fatalf("second roster dstr name bytes = %x, want %x", got, secondName)
	}
	jobStart := noPackRosterJobStart(secondStart, characters[1])
	growStart := noPackRosterGrowStart(secondStart, characters[1])
	levelStart := noPackRosterLevelStart(secondStart, characters[1])
	if got := body[jobStart]; got != 15 {
		t.Fatalf("second roster job byte = %d, want 15", got)
	}
	if got := body[growStart]; got != 0 {
		t.Fatalf("second roster grow byte = %d, want 0", got)
	}
	if got := body[levelStart]; got != 1 {
		t.Fatalf("second roster level byte = %d, want 1", got)
	}
}

func TestCSharpRosterBodyMatchesSub200B250ReadCursor(t *testing.T) {
	characters := []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: defaultAccountPrefix + "1", Slot: 0, Name: "first", Job: "0", Level: 1},
		{CharacterID: "2", AccountID: defaultAccountPrefix + "1", Slot: 7, Name: "鍒氬垰", Job: "15", Level: 56, Stats: map[string]int64{"grow_type": 2}},
	}
	body := buildCSharpRosterBody(characters)

	assertCSharpRosterBody(t, body, len(characters))
	cursor := 15
	for idx, character := range characters {
		entryStart := cursor
		wantKey := uint16(rosterSlotValue(character.Slot, idx))
		if got := binary.LittleEndian.Uint16(body[cursor : cursor+2]); got != wantKey {
			t.Fatalf("entry[%d] key = %d, want %d", idx, got, wantKey)
		}
		cursor += 2
		nameLen := int(binary.LittleEndian.Uint32(body[cursor : cursor+4]))
		cursor += 4
		name := rosterRawNameBytes(character)
		if nameLen != len(name) {
			t.Fatalf("entry[%d] name len = %d, want %d", idx, nameLen, len(name))
		}
		if got := body[cursor : cursor+nameLen]; !bytes.Equal(got, name) {
			t.Fatalf("entry[%d] name = %x, want %x", idx, got, name)
		}
		cursor += nameLen
		if got := body[cursor : cursor+noPackRosterPreJobBytes]; !bytes.Equal(got, make([]byte, noPackRosterPreJobBytes)) {
			t.Fatalf("entry[%d] pre-job bytes = %x", idx, got)
		}
		cursor += noPackRosterPreJobBytes
		wantJob := rosterByteValue(int64(numericCharacterStat(character.Job)), 0)
		if got := body[cursor]; got != wantJob {
			t.Fatalf("entry[%d] job = %d, want %d", idx, got, wantJob)
		}
		cursor++
		wantGrow := rosterByteValue(numericCharacterStatValue(character, "grow_type"), 0)
		if got := body[cursor]; got != wantGrow {
			t.Fatalf("entry[%d] grow = %d, want %d", idx, got, wantGrow)
		}
		cursor++
		if got := body[cursor]; got != rosterLevel(character) {
			t.Fatalf("entry[%d] level = %d, want %d", idx, got, rosterLevel(character))
		}
		cursor++
		if got := body[cursor : cursor+10]; !bytes.Equal(got, make([]byte, 10)) {
			t.Fatalf("entry[%d] pre-appearance zero block = %x, want 10 zero bytes", idx, got)
		}
		cursor += 10
		rows := currentRosterEquipSummaryRows(characterEquipSummary(character))
		if got := body[cursor]; got != byte(len(rows)) {
			t.Fatalf("entry[%d] equip count = %d, want %d", idx, got, len(rows))
		}
		cursor++
		cursor += len(rows) * noPackRosterEquipRowBytes
		if got, want := body[cursor:cursor+noPackRosterPostEquipBytes], normalNoPackRosterPostEquipBytes(); !bytes.Equal(got, want) {
			t.Fatalf("entry[%d] post-equip bytes = %x, want %x", idx, got, want)
		}
		cursor += noPackRosterPostEquipBytes
		if want := entryStart + noPackRosterEntryLen(character); cursor != want {
			t.Fatalf("entry[%d] cursor = %d, want %d", idx, cursor, want)
		}
	}
	if got := body[cursor : cursor+10]; !bytes.Equal(got, []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("roster tail = %x, want page/no-push tail", got)
	}
	if got := cursor + 10; got != len(body) {
		t.Fatalf("body end cursor = %d, len = %d", got, len(body))
	}
}

func buildLegacyGamePacketForBridgeTest(cmd byte, typ uint16, seq uint16, body []byte) []byte {
	packet := make([]byte, dnfproto.LegacyGameHeaderSize+len(body))
	packet[0] = cmd
	binary.LittleEndian.PutUint16(packet[1:3], typ)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
	binary.LittleEndian.PutUint16(packet[11:13], seq)
	copy(packet[dnfproto.LegacyGameHeaderSize:], body)
	return packet
}

func assertLatestCharacterState(t *testing.T, record dnfproto.LatestGameTransportRecord, count byte) {
	t.Helper()
	if record.TransportHeader.Route != latestCharacterStateRoute {
		t.Fatalf("character state route = %d, want %d", record.TransportHeader.Route, latestCharacterStateRoute)
	}
	minLen := 9 + int(count)*6
	if len(record.Body) < minLen {
		t.Fatalf("character state body too short: %d, want at least %d", len(record.Body), minLen)
	}
	if record.Body[0] != count || record.Body[1] != latestCharacterStateActive || record.Body[3] != latestCharacterCreateEnabled {
		t.Fatalf("character state header mismatch: route=%d body=%x", record.TransportHeader.Route, record.Body)
	}
}

func legacyMsgID(msg dnfenum.LegacyChannelMsg) byte {
	return byte(uint16(msg))
}

func tempChannelInfoFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "channel_info.etc")
	if err := os.WriteFile(path, []byte("[server]\n1 1 `tower` 2 `[deathtower]` 0 0 19 `crack` 1 `[crack]` 5 0\n[/server]\n"), 0o600); err != nil {
		t.Fatalf("write temp channel info: %v", err)
	}
	return path
}

func testChannelCatalog(t *testing.T) *channelcatalog.Catalog {
	t.Helper()
	const fixture = `
[dungeon]
` + "`[elven_guard]` `艾尔文防线` 1 2" + `
[/dungeon]
[server]
1 11 ` + "`普通频道`" + ` 1 ` + "`[elven_guard]`" + ` 0 0 38 ` + "`推荐频道`" + ` 1 ` + "`[elven_guard]`" + ` 0 0
[/server]
`
	index, err := channelinfo.Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse channel info: %v", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{ServerID: 1})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	return catalog
}

func testCatalogWithoutAutoChannel(t *testing.T) *channelcatalog.Catalog {
	t.Helper()
	const fixture = `
[dungeon]
` + "`[elven_guard]` `艾尔文防线` 1 2" + `
[/dungeon]
[server]
1 1 ` + "`普通频道`" + ` 1 ` + "`[elven_guard]`" + ` 0 0 11 ` + "`推荐频道`" + ` 1 ` + "`[elven_guard]`" + ` 0 0
[/server]
`
	index, err := channelinfo.Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse channel info: %v", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{ServerID: 1})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	return catalog
}

func testTowerCatalog(t *testing.T) *channelcatalog.Catalog {
	t.Helper()
	const fixture = `
[dungeon]
` + "`[deathtower]` `亡者峡谷` 1" + `
[/dungeon]
[server]
1 1 ` + "`塔`" + ` 2 ` + "`[deathtower]`" + ` 0 0 6 ` + "`贸易1`" + ` 3 ` + "`[trade]`" + ` 0 0 38 ` + "`贸易2`" + ` 3 ` + "`[trade]`" + ` 0 0 10 ` + "`格兰`" + ` 1 ` + "`[granfloris]`" + ` 0 0 11 ` + "`天空`" + ` 1 ` + "`[sky_catle]`" + ` 0 0
[/server]
`
	index, err := channelinfo.Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse channel info: %v", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{ServerID: 1})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	return catalog
}

func intSliceContains(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func buildCreateRequest(job byte, name string) []byte {
	nameRaw := []byte(name)
	body := make([]byte, 5+len(nameRaw))
	body[0] = job
	binary.LittleEndian.PutUint32(body[1:5], uint32(len(nameRaw)))
	copy(body[5:], nameRaw)
	return body
}

func buildCheckNameRequest(name string) []byte {
	nameRaw := []byte(name)
	body := make([]byte, 4+len(nameRaw))
	binary.LittleEndian.PutUint32(body[:4], uint32(len(nameRaw)))
	copy(body[4:], nameRaw)
	return body
}

func testRepositoryGroup() dnfrepo.Group {
	group := dnfrepomemory.NewMemoryGroup()
	group.Character = &fakeCharacterStore{
		records: make(map[string]dnfrepo.CharacterRecord),
		nextID:  1,
	}
	group.CharacterAssets = &fakeCharacterAssetUnitOfWork{
		character: group.Character,
		inventory: group.Inventory,
		equipment: group.Equipment,
	}
	return group
}

func prepareTestCharacterInitialization(service *Service, jobs ...byte) {
	if service.initialEquipmentByJob == nil {
		service.initialEquipmentByJob = make(map[byte][]initialEquipmentEntry)
	}
	if service.initialSkillsByJob == nil {
		service.initialSkillsByJob = make(map[byte][]initialSkillEntry)
	}
	if service.initialSPTable == nil {
		service.initialSPTable = map[int]int{}
	}
	for _, job := range jobs {
		if _, ok := service.initialEquipmentByJob[job]; !ok {
			service.initialEquipmentByJob[job] = []initialEquipmentEntry{}
		}
		if _, ok := service.initialSkillsByJob[job]; !ok {
			service.initialSkillsByJob[job] = []initialSkillEntry{}
		}
	}
}

type fakeCharacterStore struct {
	records map[string]dnfrepo.CharacterRecord
	nextID  int
}

type fakeCharacterAssetUnitOfWork struct {
	character dnfrepo.CharacterRepository
	inventory dnfrepo.InventoryRepository
	equipment dnfrepo.EquipmentRepository
}

func (u *fakeCharacterAssetUnitOfWork) WithinCharacterAssets(
	ctx context.Context,
	_ string,
	apply func(dnfrepo.CharacterRepository, dnfrepo.InventoryRepository, dnfrepo.EquipmentRepository) error,
) error {
	if u == nil || u.character == nil || u.inventory == nil || u.equipment == nil || apply == nil {
		return dnfrepo.ErrCharacterAssetTransactionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return apply(u.character, u.inventory, u.equipment)
}

type fakeLegacyInventoryStore struct {
	items map[byte][]dnfrepo.LegacyInventoryItem
}

func (s *fakeLegacyInventoryStore) SelectItems(_ context.Context, _ string, listType byte) ([]dnfrepo.LegacyInventoryItem, error) {
	items := s.items[listType]
	out := make([]dnfrepo.LegacyInventoryItem, len(items))
	copy(out, items)
	return out, nil
}

func (s *fakeCharacterStore) Load(_ context.Context, characterID string) (dnfrepo.CharacterRecord, bool, error) {
	record, ok := s.records[characterID]
	return dnfrepo.CloneCharacter(record), ok, nil
}

func (s *fakeCharacterStore) Save(_ context.Context, record dnfrepo.CharacterRecord) error {
	if s.records == nil {
		s.records = make(map[string]dnfrepo.CharacterRecord)
	}
	s.records[record.CharacterID] = dnfrepo.CloneCharacter(record)
	return nil
}

func (s *fakeCharacterStore) CreateCharacter(ctx context.Context, record dnfrepo.CharacterRecord) error {
	if _, ok, err := s.Load(ctx, record.CharacterID); err != nil || ok {
		if err != nil {
			return err
		}
		return dnfrepo.ErrCharacterIDExists
	}
	for _, existing := range s.records {
		if existing.AccountID == record.AccountID &&
			existing.Slot == record.Slot &&
			fakeCharacterDeleteFlag(existing) == 0 {
			return dnfrepo.ErrCharacterSlotOccupied
		}
	}
	return s.Save(ctx, record)
}

func (s *fakeCharacterStore) ListByAccount(_ context.Context, accountID string, _ int) ([]dnfrepo.CharacterRecord, error) {
	out := make([]dnfrepo.CharacterRecord, 0)
	for _, record := range s.records {
		if record.AccountID == accountID && fakeCharacterDeleteFlag(record) == 0 {
			out = append(out, dnfrepo.CloneCharacter(record))
		}
	}
	return out, nil
}

func (s *fakeCharacterStore) FindIDByName(_ context.Context, name string) (string, bool, error) {
	for _, record := range s.records {
		if record.Name == name && fakeCharacterDeleteFlag(record) == 0 {
			return record.CharacterID, true, nil
		}
	}
	return "", false, nil
}

func fakeCharacterDeleteFlag(record dnfrepo.CharacterRecord) int64 {
	if record.Stats == nil {
		return 0
	}
	return record.Stats["delete_flag"]
}

func (s *fakeCharacterStore) NextNumericID(context.Context) (int, error) {
	id := s.nextID
	s.nextID++
	return id, nil
}

type bufferConn struct {
	write bytes.Buffer
}

func (c *bufferConn) Read(_ []byte) (int, error)        { return 0, io.EOF }
func (c *bufferConn) Write(data []byte) (int, error)    { return c.write.Write(data) }
func (c *bufferConn) Close() error                      { return nil }
func (c *bufferConn) LocalAddr() net.Addr               { return fakeAddr("local") }
func (c *bufferConn) RemoteAddr() net.Addr              { return fakeAddr("remote") }
func (c *bufferConn) SetDeadline(_ time.Time) error     { return nil }
func (c *bufferConn) SetReadDeadline(_ time.Time) error { return nil }
func (c *bufferConn) SetWriteDeadline(_ time.Time) error {
	return nil
}

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "i/o timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return true }
