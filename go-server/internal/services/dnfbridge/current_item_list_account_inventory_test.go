package dnfbridge

import (
	"context"
	"encoding/binary"
	"reflect"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestBuildCurrentMainItemListPreservesAccountSharedNativeRowsInList0(t *testing.T) {
	ctx := context.Background()
	repositories := testRepositoryGroup()
	crystalRaw := make([]byte, currentItemListEntryWireSize)
	binary.LittleEndian.PutUint32(crystalRaw[0x0E:0x12], 3033)
	crystalRaw[0x59] = 0x5A
	soulRaw := make([]byte, currentItemListEntryWireSize)
	binary.LittleEndian.PutUint32(soulRaw[0x0E:0x12], 10158124)
	soulRaw[0x59] = 0x6B
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "12",
		Slots: map[string]dnfrepo.ItemStack{
			"0:10": {ItemID: 1000, Count: 2},
		},
	}); err != nil {
		t.Fatalf("save character inventory: %v", err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:shared",
		Slots: map[string]dnfrepo.ItemStack{
			"0:354": {ItemID: 3033, Count: 13, RawEntry: crystalRaw},
			"0:365": {ItemID: 10158124, Count: 14, RawEntry: soulRaw},
		},
	}); err != nil {
		t.Fatalf("save account inventory: %v", err)
	}
	service := &Service{
		options: options{accountID: "dnf:shared"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{connID: "game-account-inventory", selectedCharacterID: 12}

	body, source, count, ok := service.buildCurrentItemListBodyForSession(ctx, session, dnfrepo.MainInventoryListType)
	if !ok || source != "inventory" || count != 3 {
		t.Fatalf("list0 ok=%v source=%q count=%d", ok, source, count)
	}
	if len(body) != 5+3*currentItemListEntryWireSize {
		t.Fatalf("list0 body len=%d want=%d", len(body), 5+3*currentItemListEntryWireSize)
	}

	rows := decodeCurrentItemListTestRows(t, body[5:])
	assertCurrentItemListTestRow(t, rows[0], 10, 1000, 2)
	assertCurrentItemListTestRow(t, rows[1], 354, 3033, 13)
	assertCurrentItemListTestRow(t, rows[2], 365, 10158124, 14)
	if got := binary.LittleEndian.Uint32(rows[1][0x0E:0x12]); got != 3033 || rows[1][0x59] != 0x5A {
		t.Fatalf("crystal native row identity was not preserved: valueA=%d marker=%02x", got, rows[1][0x59])
	}
	if got := binary.LittleEndian.Uint32(rows[2][0x0E:0x12]); got != 10158124 || rows[2][0x59] != 0x6B {
		t.Fatalf("soul native row identity was not preserved: valueA=%d marker=%02x", got, rows[2][0x59])
	}
}

func TestBuildCurrentMainItemListConvertsLegacyCoinGeneralStackBeforeSnapshot(t *testing.T) {
	ctx := context.Background()
	repositories := testRepositoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "12",
		Slots: map[string]dnfrepo.ItemStack{
			"0:1": {
				ItemID: 1,
				Count:  30,
			},
			"0:113": {
				ItemID: 42,
				Count:  856,
				Extra: map[string]string{
					"item_kind": "stackable",
					"pvf_path":  "stackable/cash/coin_general.stk",
				},
			},
		},
	}); err != nil {
		t.Fatalf("save character inventory: %v", err)
	}
	service := &Service{
		options: options{accountID: "dnf:revive-coin-migration"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{connID: "game-revive-coin-migration", selectedCharacterID: 12}

	body, source, count, ok := service.buildCurrentItemListBodyForSession(ctx, session, dnfrepo.MainInventoryListType)
	if !ok || source != "inventory" || count != 1 {
		t.Fatalf("list0 ok=%v source=%q count=%d", ok, source, count)
	}
	rows := decodeCurrentItemListTestRows(t, body[5:])
	assertCurrentItemListTestRow(t, rows[0], 1, 1, 886)

	inventory, found, err := repositories.Inventory.Load(ctx, "12")
	if err != nil || !found {
		t.Fatalf("load migrated character inventory found=%v err=%v", found, err)
	}
	if _, exists := inventory.Slots["0:113"]; exists {
		t.Fatalf("legacy coin_general row was still exposed: %+v", inventory.Slots["0:113"])
	}
	if wallet := inventory.Slots["0:1"]; wallet.ItemID != 1 ||
		wallet.Count != 886 ||
		wallet.Extra["amount_or_count"] != "886" ||
		wallet.Extra["instance_value"] != "886" {
		t.Fatalf("wallet=%+v", wallet)
	}
}

func TestBuildCurrentAccountCargoListUsesAccountStateAndAccountOwnedRows(t *testing.T) {
	ctx := context.Background()
	repositories := testRepositoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "dnf:account-cargo",
		Metadata: map[string]string{
			"account_cargo_created": "true",
			"account_cargo_level":   "24",
			"account_cargo_gold":    "123456",
		},
	}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:account-cargo",
		Slots: map[string]dnfrepo.ItemStack{
			"12:3": {ItemID: 10103, Count: 2},
			"12:9": {ItemID: 10109, Count: 5},
			// A stale character/account-shared row is not an account-cargo row.
			"0:354": {ItemID: 1354, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save account inventory: %v", err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "12",
		Warehouse: map[string]dnfrepo.ItemStack{
			"12:3": {ItemID: 99999, Count: 99},
		},
	}); err != nil {
		t.Fatalf("save stale character cargo residue: %v", err)
	}
	service := &Service{
		options: options{accountID: "dnf:account-cargo"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	body, source, count, ok := service.buildCurrentItemListBodyForSession(ctx, &gameSession{connID: "account-cargo", selectedCharacterID: 12}, 12)
	if !ok || source != "account_metadata+account_inventory" || count != 2 {
		t.Fatalf("list12 ok=%t source=%q count=%d", ok, source, count)
	}
	if len(body) != 9+2*currentItemListEntryWireSize {
		t.Fatalf("list12 body len=%d", len(body))
	}
	if got := binary.LittleEndian.Uint16(body[1:3]); got != 24 {
		t.Fatalf("list12 capacity=%d want=24", got)
	}
	if got := binary.LittleEndian.Uint32(body[3:7]); got != 123456 {
		t.Fatalf("list12 gold=%d want=123456", got)
	}
	rows := decodeCurrentItemListTestRows(t, body[9:])
	assertCurrentItemListTestRow(t, rows[0], 3, 10103, 2)
	assertCurrentItemListTestRow(t, rows[1], 9, 10109, 5)
}

func TestBuildCurrentMainItemListMigratesCharacterSharedResidueBeforeSnapshot(t *testing.T) {
	ctx := context.Background()
	repositories := testRepositoryGroup()
	expireAt := time.Date(2026, time.July, 17, 8, 30, 0, 0, time.Local)
	crystal := dnfrepo.ItemStack{
		ItemID:   1359,
		Count:    2,
		Bind:     true,
		ExpireAt: expireAt,
		RawEntry: []byte{0x35, 0x90, 0x11, 0x7F},
		Extra:    map[string]string{"durability": "27", "source": "legacy_character_shared"},
	}
	soul := dnfrepo.ItemStack{
		ItemID:   1364,
		Count:    3,
		RawEntry: []byte{0x36, 0x40, 0x22, 0x6E},
		Extra:    map[string]string{"value_a": "9981"},
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "12",
		Slots: map[string]dnfrepo.ItemStack{
			"0:11":  {ItemID: 1011, Count: 1},
			"0:359": crystal,
			"0:364": soul,
		},
	}); err != nil {
		t.Fatalf("save character inventory: %v", err)
	}
	service := &Service{
		options: options{accountID: "dnf:empty"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{connID: "game-account-inventory-empty", selectedCharacterID: 12}

	body, source, count, ok := service.buildCurrentItemListBodyForSession(ctx, session, dnfrepo.MainInventoryListType)
	if !ok || source != "inventory" || count != 3 {
		t.Fatalf("list0 ok=%v source=%q count=%d", ok, source, count)
	}
	rows := decodeCurrentItemListTestRows(t, body[5:])
	assertCurrentItemListTestRow(t, rows[0], 11, 1011, 1)
	assertCurrentItemListTestRow(t, rows[1], 359, 1359, 2)
	assertCurrentItemListTestRow(t, rows[2], 364, 1364, 3)
	if got := binary.LittleEndian.Uint16(rows[1][0x0B:0x0D]); got != 27 || rows[1][0x0D] != 1 {
		t.Fatalf("migrated crystal native fields were not projected: durability=%d bind=%d", got, rows[1][0x0D])
	}
	if got := binary.LittleEndian.Uint32(rows[2][0x0E:0x12]); got != 9981 {
		t.Fatalf("migrated soul native value was not projected: valueA=%d", got)
	}

	character, found, err := repositories.Inventory.Load(ctx, "12")
	if err != nil || !found {
		t.Fatalf("load migrated character inventory found=%v err=%v", found, err)
	}
	if _, exists := character.Slots["0:359"]; exists {
		t.Fatalf("character crystal residue was not removed: %+v", character.Slots["0:359"])
	}
	if _, exists := character.Slots["0:364"]; exists {
		t.Fatalf("character soul residue was not removed: %+v", character.Slots["0:364"])
	}
	if got := character.Slots["0:11"]; !reflect.DeepEqual(got, (dnfrepo.ItemStack{ItemID: 1011, Count: 1})) {
		t.Fatalf("ordinary character slot changed during migration: got=%+v", got)
	}
	account, found, err := repositories.AccountInventory.Load(ctx, "dnf:empty")
	if err != nil || !found {
		t.Fatalf("load migrated account inventory found=%v err=%v", found, err)
	}
	if got := account.Slots["0:359"]; !reflect.DeepEqual(got, crystal) {
		t.Fatalf("migrated crystal stack lost fields: got=%+v want=%+v", got, crystal)
	}
	if got := account.Slots["0:364"]; !reflect.DeepEqual(got, soul) {
		t.Fatalf("migrated soul stack lost fields: got=%+v want=%+v", got, soul)
	}
}

func TestBuildCurrentMainItemListRejectsAccountSharedMigrationConflictWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repositories := testRepositoryGroup()
	characterBefore := dnfrepo.InventoryRecord{
		CharacterID: "12",
		Slots: map[string]dnfrepo.ItemStack{
			"0:11":  {ItemID: 1011, Count: 1},
			"0:354": {ItemID: 1354, Count: 3, Bind: true, Extra: map[string]string{"owner": "character"}},
			"0:360": {ItemID: 1360, Count: 4},
		},
	}
	accountBefore := dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:conflict",
		Slots: map[string]dnfrepo.ItemStack{
			"0:354": {ItemID: 9354, Count: 13, Extra: map[string]string{"owner": "account"}},
			"0:365": {ItemID: 9365, Count: 14},
		},
	}
	if err := repositories.Inventory.Save(ctx, characterBefore); err != nil {
		t.Fatalf("save character inventory: %v", err)
	}
	if err := repositories.AccountInventory.Save(ctx, accountBefore); err != nil {
		t.Fatalf("save account inventory: %v", err)
	}
	service := &Service{
		options: options{accountID: "dnf:conflict"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{connID: "game-account-inventory-conflict", selectedCharacterID: 12}

	body, source, count, ok := service.buildCurrentItemListBodyForSession(ctx, session, dnfrepo.MainInventoryListType)
	if ok || body != nil || source != "" || count != 0 {
		t.Fatalf("conflicting list0 must fail closed: ok=%v source=%q count=%d body=%x", ok, source, count, body)
	}

	characterAfter, found, err := repositories.Inventory.Load(ctx, "12")
	if err != nil || !found {
		t.Fatalf("load character after conflict found=%v err=%v", found, err)
	}
	accountAfter, found, err := repositories.AccountInventory.Load(ctx, "dnf:conflict")
	if err != nil || !found {
		t.Fatalf("load account after conflict found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(characterAfter.Slots, characterBefore.Slots) {
		t.Fatalf("character inventory mutated on conflict: got=%+v want=%+v", characterAfter.Slots, characterBefore.Slots)
	}
	if !reflect.DeepEqual(accountAfter.Slots, accountBefore.Slots) {
		t.Fatalf("account inventory mutated on conflict: got=%+v want=%+v", accountAfter.Slots, accountBefore.Slots)
	}
}

func decodeCurrentItemListTestRows(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	if len(raw)%currentItemListEntryWireSize != 0 {
		t.Fatalf("item row bytes=%d are not aligned to %d", len(raw), currentItemListEntryWireSize)
	}
	rows := make([][]byte, 0, len(raw)/currentItemListEntryWireSize)
	for len(raw) > 0 {
		rows = append(rows, raw[:currentItemListEntryWireSize])
		raw = raw[currentItemListEntryWireSize:]
	}
	return rows
}

func assertCurrentItemListTestRow(t *testing.T, row []byte, slot uint16, itemID uint32, count uint32) {
	t.Helper()
	if got := binary.LittleEndian.Uint16(row[0x00:0x02]); got != slot {
		t.Fatalf("slot=%d want=%d row=%x", got, slot, row)
	}
	if got := binary.LittleEndian.Uint32(row[0x02:0x06]); got != itemID {
		t.Fatalf("item_id=%d want=%d row=%x", got, itemID, row)
	}
	if got := binary.LittleEndian.Uint32(row[0x06:0x0A]); got != count {
		t.Fatalf("count=%d want=%d row=%x", got, count, row)
	}
}
