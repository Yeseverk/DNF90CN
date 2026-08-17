package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestParseCurrentRentalCatalogPreservesJobTierPairs(t *testing.T) {
	source := initialEquipmentMemSource{currentRentalSystemPVFPath: `
[limit point]
400
[point charge]
` + "`pdungeon` 1 `gold` 100000" + `
[section]
0 1 7 3 1 1 1
1 20 7 3 1 1 1
[group]
` + "`[gun blader]` 0" + `
[package selection]
416000000 3
[/package selection]
[/group]
[group]
` + "`[gun blader]` 1" + `
[package selection]
416010000 5 416020000 5
[/package selection]
[/group]
`}
	catalog, err := parseCurrentRentalCatalog(source)
	if err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	if catalog.Limit != 400 || catalog.GoldPerPoint != 100000 {
		t.Fatalf("wallet config = limit %d gold %d", catalog.Limit, catalog.GoldPerPoint)
	}
	if tier, ok := catalog.tierForLevel(1); !ok || tier != 0 {
		t.Fatalf("level 1 tier = %d found=%t", tier, ok)
	}
	if tier, ok := catalog.tierForLevel(20); !ok || tier != 1 {
		t.Fatalf("level 20 tier = %d found=%t", tier, ok)
	}
	if cost, ok := catalog.itemCost("[gun blader]", 0, 416000000); !ok || cost != 3 {
		t.Fatalf("tier0 item cost = %d found=%t", cost, ok)
	}
	if _, ok := catalog.itemCost("[gun blader]", 0, 416010000); ok {
		t.Fatal("tier1 item leaked into tier0")
	}
}

func TestCurrentRentalEffectiveLevelUsesOverEquipContract(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	now := time.Unix(1_800_000_000, 0).UTC()
	catalog := &currentRentalCatalog{Tiers: []currentRentalTier{
		{Tier: 0, MinimumLevel: 1},
		{Tier: 1, MinimumLevel: 20},
		{Tier: 2, MinimumLevel: 30},
	}}
	account := dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata: map[string]string{
			"premium_expire_22": strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
		},
	}
	if err := repositories.Account.Save(ctx, account); err != nil {
		t.Fatal(err)
	}

	effectiveLevel, active := currentRentalEffectiveLevel(ctx, repositories.Account, account.AccountID, 20, now)
	if !active || effectiveLevel != 30 {
		t.Fatalf("active contract effective level=%d active=%t, want 30/true", effectiveLevel, active)
	}
	if tier, ok := catalog.tierForLevel(effectiveLevel); !ok || tier != 2 {
		t.Fatalf("active contract tier=%d found=%t, want 2/true", tier, ok)
	}

	account.Metadata["premium_expire_22"] = strconv.FormatInt(now.Add(-time.Second).Unix(), 10)
	if err := repositories.Account.Save(ctx, account); err != nil {
		t.Fatal(err)
	}
	effectiveLevel, active = currentRentalEffectiveLevel(ctx, repositories.Account, account.AccountID, 20, now)
	if active || effectiveLevel != 20 {
		t.Fatalf("expired contract effective level=%d active=%t, want 20/false", effectiveLevel, active)
	}
	if tier, ok := catalog.tierForLevel(effectiveLevel); !ok || tier != 1 {
		t.Fatalf("expired contract tier=%d found=%t, want 1/true", tier, ok)
	}

	effectiveLevel, active = currentRentalEffectiveLevel(ctx, nil, account.AccountID, 20, now)
	if active || effectiveLevel != 20 {
		t.Fatalf("missing repository effective level=%d active=%t, want 20/false", effectiveLevel, active)
	}
}

func TestCurrentRentalProtocolBodies(t *testing.T) {
	rentBody := make([]byte, currentRentalRequestWireSize)
	binary.LittleEndian.PutUint32(rentBody[13:17], 416000000)
	binary.LittleEndian.PutUint32(rentBody[17:21], uint32(11)|uint32(7)<<16)
	rent, err := decodeCurrentRentEquipmentRequest(rentBody)
	if err != nil || rent.ItemID != 416000000 || rent.PackedJobTier != uint32(11)|uint32(7)<<16 {
		t.Fatalf("decode rent = %+v err=%v", rent, err)
	}
	chargeBody := make([]byte, currentRentalRequestWireSize)
	binary.LittleEndian.PutUint32(chargeBody[13:17], 1)
	binary.LittleEndian.PutUint32(chargeBody[17:21], 10)
	charge, err := decodeCurrentChargeRentalPointRequest(chargeBody)
	if err != nil || charge.ChargeType != 1 || charge.Count != 10 {
		t.Fatalf("decode charge = %+v err=%v", charge, err)
	}
	state := buildCurrentRentalPointStateBody(97, []currentRentalActiveEntry{{ItemID: 416000000, ExpireUnix: 123456}})
	if len(state) != 16 || binary.LittleEndian.Uint32(state[0:4]) != 97 || binary.LittleEndian.Uint32(state[4:8]) != 1 || binary.LittleEndian.Uint32(state[8:12]) != 416000000 || binary.LittleEndian.Uint32(state[12:16]) != 123456 {
		t.Fatalf("rental state body = %x", state)
	}
	gold := buildCurrentGoldStateBody(1000000)
	if len(gold) != 3+currentItemListEntryWireSize || gold[0] != 0 || binary.LittleEndian.Uint16(gold[1:3]) != 1 || binary.LittleEndian.Uint32(gold[9:13]) != 1000000 {
		t.Fatalf("gold state body len=%d body=%x", len(gold), gold)
	}
	rentalExpire := time.Unix(1_800_000_000, 0).UTC()
	rentalStack := dnfrepo.ItemStack{
		ItemID:   401030031,
		Count:    1,
		ExpireAt: rentalExpire,
		Extra: map[string]string{
			"source":      currentRentalItemSource,
			"durability":  "45",
			"expire_time": strconv.FormatInt(rentalExpire.Unix(), 10),
		},
	}
	rentalEntry := currentItemListEntryFromStack(0, 9, rentalStack)
	rentalUpdate := buildCurrentItemUpdateBody(0, []currentItemListEntry{rentalEntry})
	if len(rentalUpdate) != 3+currentItemListEntryWireSize || rentalUpdate[0] != 0 || binary.LittleEndian.Uint16(rentalUpdate[1:3]) != 1 {
		t.Fatalf("rental op14 header len=%d body=%x", len(rentalUpdate), rentalUpdate)
	}
	row := rentalUpdate[3:]
	if binary.LittleEndian.Uint16(row[0:2]) != 9 || binary.LittleEndian.Uint32(row[2:6]) != 401030031 ||
		binary.LittleEndian.Uint32(row[6:10]) != 1 || binary.LittleEndian.Uint16(row[0x0b:0x0d]) != 45 ||
		binary.LittleEndian.Uint32(row[0x6e:0x72]) != 0 {
		t.Fatalf("rental op14 row=%x", row)
	}
}

func TestResetDungeonEntrySceneGatesClearsRentalWalletState(t *testing.T) {
	session := &gameSession{selectedRentalWalletStateSent: true}
	resetDungeonEntrySceneGates(session)
	if session.selectedRentalWalletStateSent {
		t.Fatal("rental wallet state gate survived scene reset")
	}
}

func TestSelectedRentalWalletStateDoesNotSendSyntheticGoldItemUpdate(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{currentRentalPointMetadataKey: "100"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Stats:       map[string]int64{"gold": 1_000_000},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := Service{
		options: options{accountID: "account-1"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{conn: connection, selectedCharacterID: 19}
	if err := service.sendSelectedRentalWalletStateWithRefresh(session, "test_login", false); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if packet.Header.Classification != 0 || packet.Header.MsgID != currentRentalStateMsgID {
		t.Fatalf("wallet packet class=%d msg=%d, want class0/op%d", packet.Header.Classification, packet.Header.MsgID, currentRentalStateMsgID)
	}
	if len(rest) != 0 {
		next, _ := splitGameServerUpperPacket(t, rest)
		t.Fatalf("wallet emitted an extra packet msg=%d; synthetic class0/op14 must not be sent", next.Header.MsgID)
	}
	if len(packet.Body) != 8 || binary.LittleEndian.Uint32(packet.Body[:4]) != 100 ||
		binary.LittleEndian.Uint32(packet.Body[4:8]) != 0 {
		t.Fatalf("rental wallet body=%x, want points=100 active_count=0", packet.Body)
	}
}

func TestCurrentRentalAssetsCommitPointsGoldAndPVFEquipmentAtomically(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	now := time.Unix(1_700_000_000, 0).UTC()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "account-1", Metadata: map[string]string{currentRentalPointMetadataKey: "5"}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1", Level: 1, Stats: map[string]int64{"gold": 1_000_000}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "19", Entries: map[string]dnfrepo.EquipmentEntry{}}); err != nil {
		t.Fatal(err)
	}
	owner, err := currentRentalAssetOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	definition := dungeonDropItemDefinition{ItemID: 416000000, Kind: dungeonDropItemEquipment, PVFPath: "equipment/rental.equ", SlotStart: 9, SlotEnd: 64, Durability: 45}
	rentResult, err := rentCurrentEquipment(ctx, owner, "account-1", "19", 416000000, 3, definition, now)
	if err != nil {
		t.Fatalf("rent equipment: %v", err)
	}
	if rentResult.Points != 2 || rentResult.Slot != 9 || !rentResult.ExpireAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("rent result = %+v", rentResult)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "19")
	stack, ok := inventory.Slots["0:9"]
	if !ok || stack.ItemID != 416000000 || len(stack.RawEntry) != currentItemListEntryWireSize || stack.Extra["durability"] != "45" || stack.Extra["source"] != currentRentalItemSource {
		t.Fatalf("rental stack = %+v", stack)
	}
	chargeResult, err := purchaseCurrentRentalPoints(ctx, owner, "account-1", "19", 2, 400, 100000, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("purchase points: %v", err)
	}
	if chargeResult.Points != 4 || chargeResult.Gold != 800000 {
		t.Fatalf("charge result = %+v", chargeResult)
	}
	rerentAt := now.Add(2 * time.Minute)
	rerentResult, err := rentCurrentEquipment(ctx, owner, "account-1", "19", 416000000, 3, definition, rerentAt)
	if err != nil {
		t.Fatalf("refresh existing rental: %v", err)
	}
	if rerentResult.Points != 1 || rerentResult.Slot != 9 || !rerentResult.ExpireAt.Equal(rerentAt.Add(24*time.Hour)) {
		t.Fatalf("rerent result = %+v", rerentResult)
	}
	inventory, _, _ = repositories.Inventory.Load(ctx, "19")
	if len(inventory.Slots) != 1 || !inventory.Slots["0:9"].ExpireAt.Equal(rerentResult.ExpireAt) {
		t.Fatalf("rerent inventory = %+v", inventory.Slots)
	}
	if _, err := rentCurrentEquipment(ctx, owner, "account-1", "19", 416000000, 2, definition, rerentAt.Add(time.Minute)); !errors.Is(err, errCurrentRentalPoints) {
		t.Fatalf("insufficient point error = %v", err)
	}
	if _, err := purchaseCurrentRentalPoints(ctx, owner, "account-1", "19", 9, 400, 100000, rerentAt.Add(2*time.Minute)); !errors.Is(err, errCurrentRentalGold) {
		t.Fatalf("insufficient gold error = %v", err)
	}
	account, _, _ := repositories.Account.Load(ctx, "account-1")
	character, _, _ := repositories.Character.Load(ctx, "19")
	if account.Metadata[currentRentalPointMetadataKey] != "1" || character.Stats["gold"] != 800000 {
		t.Fatalf("persisted account=%+v character=%+v", account, character)
	}
	expired := inventory.Slots["0:9"]
	expired.ExpireAt = rerentAt.Add(-time.Minute)
	expired.Extra["expire_time"] = strconv.FormatInt(expired.ExpireAt.Unix(), 10)
	inventory.Slots["0:9"] = expired
	if err := repositories.Inventory.Save(ctx, inventory); err != nil {
		t.Fatal(err)
	}
	removed, err := cleanupExpiredCurrentRentalEquipment(ctx, owner, "account-1", "19", rerentAt)
	if err != nil || removed != 1 {
		t.Fatalf("expired cleanup removed=%d err=%v", removed, err)
	}
	inventory, _, _ = repositories.Inventory.Load(ctx, "19")
	if len(inventory.Slots) != 0 {
		t.Fatalf("expired rental survived cleanup: %+v", inventory.Slots)
	}
}

func TestCurrentRentalDoesNotConvertPermanentSameItem(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	now := time.Unix(1_700_000_000, 0).UTC()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "account-1", Metadata: map[string]string{currentRentalPointMetadataKey: "5"}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1", Level: 1, Stats: map[string]int64{"gold": 1_000_000}}); err != nil {
		t.Fatal(err)
	}
	permanentStack := dnfrepo.ItemStack{ItemID: 416000000, Count: 1, Bind: true, Extra: map[string]string{"source": "quest_reward", "marker": "keep"}}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{"0:9": permanentStack}}); err != nil {
		t.Fatal(err)
	}
	permanentEquipment := dnfrepo.EquipmentEntry{ItemID: 416000000, SlotIndex: 0, Extra: map[string]string{"source": "starter", "marker": "keep"}}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "19", Entries: map[string]dnfrepo.EquipmentEntry{"weapon": permanentEquipment}}); err != nil {
		t.Fatal(err)
	}
	owner, err := currentRentalAssetOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	definition := dungeonDropItemDefinition{ItemID: 416000000, Kind: dungeonDropItemEquipment, PVFPath: "equipment/rental.equ", SlotStart: 9, SlotEnd: 64, Durability: 45}
	result, err := rentCurrentEquipment(ctx, owner, "account-1", "19", 416000000, 3, definition, now)
	if err != nil {
		t.Fatalf("rent alongside permanent item: %v", err)
	}
	if result.Slot != 10 || result.Points != 2 || result.Equipped {
		t.Fatalf("rent result = %+v", result)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "19")
	if got := inventory.Slots["0:9"]; got.ItemID != permanentStack.ItemID || got.Bind != permanentStack.Bind || got.Extra["source"] != "quest_reward" || got.Extra["marker"] != "keep" || !got.ExpireAt.IsZero() {
		t.Fatalf("permanent inventory item mutated: %+v", got)
	}
	if got := inventory.Slots["0:10"]; got.ItemID != 416000000 || got.Extra["source"] != currentRentalItemSource || !got.ExpireAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("rental copy = %+v", got)
	}
	equipment, _, _ := repositories.Equipment.Load(ctx, "19")
	if got := equipment.Entries["weapon"]; got.ItemID != permanentEquipment.ItemID || got.Extra["source"] != "starter" || got.Extra["marker"] != "keep" || !got.ExpireAt.IsZero() {
		t.Fatalf("permanent equipped item mutated: %+v", got)
	}
}

func TestRealScriptPVFCurrentRentalCatalog(t *testing.T) {
	pvfPath := os.Getenv("DNF_RENTAL_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_RENTAL_REAL_PVF_SMOKE to run the real Script.pvf rental smoke test")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open real Script.pvf: %v", err)
	}
	catalog, err := parseCurrentRentalCatalog(archive)
	if err != nil {
		t.Fatalf("parse real rental catalog: %v", err)
	}
	if catalog.Limit != 400 || catalog.GoldPerPoint != 100000 {
		t.Fatalf("real wallet config = %+v", catalog)
	}
	if cost, ok := catalog.itemCost("[gun blader]", 0, 416000000); !ok || cost != 3 {
		t.Fatalf("real gun blader tier0 cost=%d found=%t", cost, ok)
	}
	monsters, err := newPVFDungeonMonsterCatalog(archive)
	if err != nil {
		t.Fatalf("load real monster/item catalog: %v", err)
	}
	items, err := monsters.DropCatalog()
	if err != nil {
		t.Fatalf("load real item catalog: %v", err)
	}
	definition, err := validateCurrentRentalItem(archive, items, 416000000, "[gun blader]")
	if err != nil {
		t.Fatalf("validate real gun blader rental: %v", err)
	}
	if definition.Durability != 45 {
		t.Fatalf("real gun blader durability=%d want=45", definition.Durability)
	}
}
