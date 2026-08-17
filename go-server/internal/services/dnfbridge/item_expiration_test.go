package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestCurrentPVFStackableUsePeriodSecondsCapsAndRounds(t *testing.T) {
	now := time.Date(2026, time.July, 19, 10, 0, 0, 250_000_000, time.UTC)
	if got := currentPVFStackableUsePeriodSeconds(now.Add(100*time.Hour), now); got != 0xFFFF {
		t.Fatalf("capped use period=%d want=%d", got, uint16(0xFFFF))
	}
	if got := currentPVFStackableUsePeriodSeconds(now.Add(1500*time.Millisecond), now); got != 2 {
		t.Fatalf("rounded use period=%d want=2", got)
	}
	if got := currentPVFStackableUsePeriodSeconds(now, now); got != 0 {
		t.Fatalf("expired use period=%d want=0", got)
	}
}

func TestCurrentPVFItemDefinitionForGrantResolvesUsablePeriodOnce(t *testing.T) {
	now := time.Date(2026, time.July, 22, 23, 15, 30, 750_000_000, time.UTC)
	staticDate := time.Date(2028, time.August, 15, 22, 0, 0, 0, time.UTC)
	definition := dungeonDropItemDefinition{
		ItemID:           10008064,
		Kind:             dungeonDropItemStackable,
		ExpirationDate:   staticDate,
		UsablePeriodDays: 30,
	}
	granted, err := currentPVFItemDefinitionForGrantAt(definition, now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Unix(now.Unix()+30*86400, 0).UTC()
	if !granted.ExpirationDate.Equal(want) || granted.ExpirationDate.Equal(staticDate) {
		t.Fatalf("grant expiration=%s want=%s static=%s", granted.ExpirationDate, want, staticDate)
	}
	// The catalog definition stays static; only the per-grant copy receives
	// an absolute deadline, so login reconciliation cannot renew it.
	if !definition.ExpirationDate.Equal(staticDate) {
		t.Fatalf("source definition mutated: %+v", definition)
	}

	equipment := dungeonDropItemDefinition{
		ItemID:           100300883,
		Kind:             dungeonDropItemEquipment,
		UsablePeriodDays: 2,
	}
	grantedEquipment, err := currentPVFItemDefinitionForGrantAt(equipment, now)
	if err != nil {
		t.Fatal(err)
	}
	wantEquipment := time.Unix(now.Unix()+2*86400, 0).UTC()
	if !grantedEquipment.ExpirationDate.Equal(wantEquipment) {
		t.Fatalf("equipment grant expiration=%s want=%s", grantedEquipment.ExpirationDate, wantEquipment)
	}

	permanent := dungeonDropItemDefinition{ItemID: 1, Kind: dungeonDropItemStackable}
	got, err := currentPVFItemDefinitionForGrantAt(permanent, now)
	if err != nil || !got.ExpirationDate.IsZero() {
		t.Fatalf("permanent definition=%+v err=%v", got, err)
	}

	tooLong := definition
	tooLong.UsablePeriodDays = 1 << 40
	if _, err := currentPVFItemDefinitionForGrantAt(tooLong, now); !errors.Is(err, errCurrentPVFUsablePeriodOutOfRange) {
		t.Fatalf("out-of-range error=%v", err)
	}
}

func TestCurrentItemStackExpirationMatchesRejectsLegacyPermanentStack(t *testing.T) {
	expireAt := time.Unix(1_900_000_000, 0).UTC()
	legacy := dnfrepo.ItemStack{ItemID: 10008064, Count: 1}
	if currentItemStackExpirationMatches(legacy, expireAt) {
		t.Fatal("legacy expire=0 stack matched a newly timed grant")
	}
	if !currentItemStackExpirationMatches(legacy, time.Time{}) {
		t.Fatal("permanent stack did not match a permanent grant")
	}
	timed := dnfrepo.ItemStack{ItemID: 10008064, Count: 1, Extra: map[string]string{"expire_time": "1900000000"}}
	if !currentItemStackExpirationMatches(timed, expireAt) {
		t.Fatalf("same-expiration stack did not match: %+v", timed)
	}
}

func TestReconcileCurrentPVFItemExpirationsMovesStackablesToUsePeriodAndCleansWrongTail(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":                "",
		"stackable/stackable.lst":            "3227 `event/test_bead.stk`\n",
		"equipment/equipment.lst":            "9001 `event/test_equipment.equ`\n",
		"stackable/event/test_bead.stk":      "[stackable type] `[booster]`\n[stack limit] 1000\n[expiration date] `2028-08-16 06:00:00`\n",
		"equipment/event/test_equipment.equ": "[durability] 50\n[expiration date] `2028-08-16 06:00:00`\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	wantDate := time.Date(2028, time.August, 15, 22, 0, 0, 0, time.UTC)
	wantUnix := uint32(wantDate.Unix())
	wrongStack := func(itemID int64, word uint16) dnfrepo.ItemStack {
		raw := make([]byte, currentItemListEntryWireSize)
		binary.LittleEndian.PutUint16(raw[0x0B:0x0D], word)
		binary.LittleEndian.PutUint32(raw[0x6E:0x72], wantUnix)
		return dnfrepo.ItemStack{
			ItemID:   itemID,
			Count:    1,
			ExpireAt: wantDate,
			RawEntry: raw,
			Extra: map[string]string{
				"expire_time":       "1849989600",
				"expire_unix":       "1849989600",
				"expiration_source": currentPVFWrongExpirationSource,
			},
		}
	}

	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		AccountID:   "account-1",
		CharacterID: "19",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:75": wrongStack(3227, 0)},
		Warehouse:   map[string]dnfrepo.ItemStack{"2:4": wrongStack(3227, 0)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "account-1",
		Slots:     map[string]dnfrepo.ItemStack{"0:354": wrongStack(3227, 0)},
	}); err != nil {
		t.Fatal(err)
	}
	equipmentStack := wrongStack(9001, 50)
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"0": {SlotIndex: 0, ItemID: equipmentStack.ItemID, ExpireAt: equipmentStack.ExpireAt, RawEntry: equipmentStack.RawEntry, Extra: equipmentStack.Extra},
		},
	}); err != nil {
		t.Fatal(err)
	}

	service := &Service{pvfItemCatalog: catalog}
	summary, err := service.reconcileCurrentPVFItemExpirations(ctx, repositories, "19", "account-1")
	if err != nil {
		t.Fatal(err)
	}
	if summary != (currentPVFExpirationReconcileSummary{Inventory: 1, Warehouse: 1, Account: 1, Equipment: 1}) {
		t.Fatalf("summary=%+v", summary)
	}
	assertStackable := func(name string, stack dnfrepo.ItemStack) {
		t.Helper()
		if !stack.ExpireAt.Equal(wantDate) || stack.Extra["expire_time"] != "1849989600" || stack.Extra["expire_unix"] != "1849989600" ||
			stack.Extra["expiration_source"] != "" || stack.Extra["item_kind"] != string(dungeonDropItemStackable) ||
			len(stack.RawEntry) != currentItemListEntryWireSize || binary.LittleEndian.Uint16(stack.RawEntry[0x0B:0x0D]) != 0xFFFF ||
			binary.LittleEndian.Uint32(stack.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != wantUnix ||
			binary.LittleEndian.Uint32(stack.RawEntry[0x6E:0x72]) != 0 {
			t.Fatalf("%s stack=%+v row=%x", name, stack, stack.RawEntry)
		}
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	assertStackable("inventory", inventory.Slots["0:75"])
	assertStackable("warehouse", inventory.Warehouse["2:4"])
	account, found, err := repositories.AccountInventory.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%t err=%v", found, err)
	}
	assertStackable("account", account.Slots["0:354"])
	equipment, found, err := repositories.Equipment.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load equipment found=%t err=%v", found, err)
	}
	worn := equipment.Entries["0"]
	if !worn.ExpireAt.Equal(wantDate) || worn.Extra["expire_time"] != "1849989600" || worn.Extra["expire_unix"] != "1849989600" ||
		worn.Extra["expiration_source"] != "" || worn.Extra["item_kind"] != string(dungeonDropItemEquipment) ||
		binary.LittleEndian.Uint16(worn.RawEntry[0x0B:0x0D]) != 50 ||
		binary.LittleEndian.Uint32(worn.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != wantUnix ||
		binary.LittleEndian.Uint32(worn.RawEntry[0x6E:0x72]) != 0 {
		t.Fatalf("equipment=%+v row=%x", worn, worn.RawEntry)
	}
}

func TestCleanupCurrentPVFWrongExpirationProjectionPreservesUnmarkedRentalAndRealPrice(t *testing.T) {
	expireAt := time.Date(2028, time.August, 15, 22, 0, 0, 0, time.UTC)
	rental := dnfrepo.ItemStack{ExpireAt: expireAt, Extra: map[string]string{"expire_time": "1849989600", "source": currentRentalItemSource}}
	if got, changed := cleanupCurrentPVFWrongExpirationProjection(rental); changed || !got.ExpireAt.Equal(expireAt) {
		t.Fatalf("unmarked rental changed=%t stack=%+v", changed, got)
	}

	stack := dnfrepo.ItemStack{
		ExpireAt: expireAt,
		RawEntry: make([]byte, currentItemListEntryWireSize),
		Extra: map[string]string{
			"expire_time":       "1849989600",
			"expire_unix":       "1849989600",
			"expiration_source": currentPVFWrongExpirationSource,
		},
	}
	binary.LittleEndian.PutUint32(stack.RawEntry[0x6E:0x72], 777)
	got, changed := cleanupCurrentPVFWrongExpirationProjection(stack)
	if !changed || !got.ExpireAt.IsZero() || binary.LittleEndian.Uint32(got.RawEntry[0x6E:0x72]) != 777 {
		t.Fatalf("cleanup changed=%t stack=%+v row=%x", changed, got, got.RawEntry)
	}
}

func TestApplyCurrentPVFUsePeriodsProjectsOnlyStackableWordAndClearsWrongTail(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":                "",
		"stackable/stackable.lst":            "3227 `event/test_bead.stk`\n",
		"equipment/equipment.lst":            "9001 `event/test_equipment.equ`\n",
		"stackable/event/test_bead.stk":      "[expiration date] `2028-08-16 06:00:00`\n",
		"equipment/event/test_equipment.equ": "[expiration date] `2028-08-16 06:00:00`\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{pvfItemCatalog: catalog}
	wantUnix := uint32(time.Date(2028, time.August, 15, 22, 0, 0, 0, time.UTC).Unix())
	rows := []currentItemListEntry{{}, {}}
	rows[0].patchCore(75, 3227, 1)
	rows[1].patchCore(4, 9001, 1)
	binary.LittleEndian.PutUint16(rows[1].data[0x0B:0x0D], 50)
	binary.LittleEndian.PutUint32(rows[0].data[0x6E:0x72], wantUnix)
	binary.LittleEndian.PutUint32(rows[1].data[0x6E:0x72], wantUnix)
	now := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
	patched, err := service.applyCurrentPVFUsePeriodsToEntriesAt(rows, now)
	if err != nil || patched != 2 {
		t.Fatalf("rows patched=%d err=%v", patched, err)
	}
	if binary.LittleEndian.Uint16(rows[0].data[0x0B:0x0D]) != 0xFFFF ||
		binary.LittleEndian.Uint32(rows[0].data[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != wantUnix ||
		binary.LittleEndian.Uint32(rows[0].data[0x6E:0x72]) != 0 {
		t.Fatalf("stackable row=%x", rows[0].data)
	}
	if binary.LittleEndian.Uint16(rows[1].data[0x0B:0x0D]) != 50 ||
		binary.LittleEndian.Uint32(rows[1].data[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != wantUnix ||
		binary.LittleEndian.Uint32(rows[1].data[0x6E:0x72]) != 0 {
		t.Fatalf("equipment row=%x", rows[1].data)
	}
}

func TestApplyCurrentPVFItemExpirationPreservesFutureInstanceDateOverExpiredStaticDate(t *testing.T) {
	now := time.Date(2026, time.July, 23, 11, 40, 0, 0, time.UTC)
	staticExpired := time.Date(2017, time.November, 1, 14, 0, 0, 0, time.UTC)
	instanceExpire := time.Date(2037, time.December, 31, 15, 59, 59, 0, time.UTC)
	instanceUnix := uint32(instanceExpire.Unix())
	stack := dnfrepo.ItemStack{
		ItemID:   490701424,
		Count:    1,
		ExpireAt: instanceExpire,
		Extra: map[string]string{
			"expire_time": "2145887999",
			"expire_unix": "2145887999",
		},
	}
	entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, 75, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)

	got, changed := applyCurrentPVFItemExpirationAt(stack, dungeonDropItemDefinition{
		ItemID:         490701424,
		Kind:           dungeonDropItemStackable,
		PVFPath:        "stackable/cash/chn_490701424.stk",
		StackableType:  "[usable cera package]",
		StackLimit:     1,
		ExpirationDate: staticExpired,
	}, now)
	if !changed {
		t.Fatal("expiration metadata/use-period was not refreshed")
	}
	if !got.ExpireAt.Equal(instanceExpire) || got.Extra["expire_time"] != "2145887999" || got.Extra["expire_unix"] != "2145887999" ||
		got.Extra["item_kind"] != string(dungeonDropItemStackable) ||
		got.Extra["stackable_type"] != "[usable cera package]" ||
		binary.LittleEndian.Uint32(got.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != instanceUnix ||
		binary.LittleEndian.Uint16(got.RawEntry[0x0B:0x0D]) != 0xFFFF {
		t.Fatalf("stack=%+v row=%x", got, got.RawEntry)
	}
}

func TestApplyCurrentPVFItemExpirationRepairsRetiredJoustShopInstance(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	retired := time.Date(2017, time.November, 1, 22, 0, 0, 0, time.UTC)
	repaired := time.Date(2028, time.August, 15, 22, 0, 0, 0, time.UTC)
	stack := dnfrepo.ItemStack{
		ItemID:   int64(currentJoustEventItemFirst),
		Count:    3,
		ExpireAt: retired,
		Extra: map[string]string{
			"expire_time": strconv.FormatInt(retired.Unix(), 10),
			"expire_unix": strconv.FormatInt(retired.Unix(), 10),
		},
	}
	entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, 75, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)

	got, changed := applyCurrentPVFItemExpirationAt(stack, dungeonDropItemDefinition{
		ItemID:         currentJoustEventItemFirst,
		Kind:           dungeonDropItemStackable,
		PVFPath:        "stackable/490005001/chn_490005585.stk",
		StackableType:  "[material]",
		ExpirationDate: repaired,
	}, now)
	if !changed {
		t.Fatal("retired joust shop item was not repaired")
	}
	if !got.ExpireAt.Equal(repaired) ||
		got.Extra["expire_time"] != strconv.FormatInt(repaired.Unix(), 10) ||
		got.Extra["expire_unix"] != strconv.FormatInt(repaired.Unix(), 10) ||
		binary.LittleEndian.Uint32(got.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != uint32(repaired.Unix()) ||
		binary.LittleEndian.Uint16(got.RawEntry[0x0B:0x0D]) != math.MaxUint16 {
		t.Fatalf("repaired stack=%+v row=%x", got, got.RawEntry)
	}

	nonJoust := stack
	nonJoust.ItemID = 490701424
	if got, _ := applyCurrentPVFItemExpirationAt(nonJoust, dungeonDropItemDefinition{
		ItemID: 490701424, Kind: dungeonDropItemStackable, ExpirationDate: repaired,
	}, now); !got.ExpireAt.Equal(retired) {
		t.Fatalf("non-joust per-instance expiration changed: %s", got.ExpireAt)
	}
}

func TestApplyCurrentPVFUsePeriodsPreservesWireInstanceDateOverExpiredStaticDate(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":              "",
		"stackable/stackable.lst":          "490701424 `cash/chn_490701424.stk`\n",
		"equipment/equipment.lst":          "",
		"stackable/cash/chn_490701424.stk": "[stackable type]\n`[usable cera package]`\n[stack limit]\n1\n[expiration date]\n`2017-11-01 22:00:00`\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{pvfItemCatalog: catalog}
	instanceExpire := time.Date(2037, time.December, 31, 15, 59, 59, 0, time.UTC)
	instanceUnix := uint32(instanceExpire.Unix())
	rows := []currentItemListEntry{{}}
	rows[0].patchCore(75, 490701424, 1)
	binary.LittleEndian.PutUint32(rows[0].data[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4], instanceUnix)
	definition, err := catalog.ResolveItem(490701424)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(rows[0].data[0x6E:0x72], uint32(definition.ExpirationDate.Unix()))

	patched, err := service.applyCurrentPVFUsePeriodsToEntriesAt(rows, time.Date(2026, time.July, 23, 11, 40, 0, 0, time.UTC))
	if err != nil || patched != 1 {
		t.Fatalf("rows patched=%d err=%v", patched, err)
	}
	if binary.LittleEndian.Uint32(rows[0].data[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != instanceUnix ||
		binary.LittleEndian.Uint32(rows[0].data[0x6E:0x72]) != 0 ||
		binary.LittleEndian.Uint16(rows[0].data[0x0B:0x0D]) != 0xFFFF {
		t.Fatalf("row=%x", rows[0].data)
	}
}

func TestRealScriptPVFSummerPackageHas2028RuntimeExpiration(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the repacked event-item expiration")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := catalog.ResolveItem(490702475)
	if err != nil {
		t.Fatal(err)
	}
	wantDate := time.Date(2028, time.August, 15, 22, 0, 0, 0, time.UTC)
	if definition.Kind != dungeonDropItemStackable || definition.PVFPath != "stackable/cash/chn_490702475.stk" || !definition.ExpirationDate.Equal(wantDate) {
		t.Fatalf("real summer package definition=%+v want_expiration=%s", definition, wantDate)
	}
}
