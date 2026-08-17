package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"reflect"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestDecodeCurrentEpicProductionStartRequest(t *testing.T) {
	body := make([]byte, currentEpicProductionStartRequestSize)
	binary.LittleEndian.PutUint32(body, 116010124)
	target, err := decodeCurrentEpicProductionStartRequest(body)
	if err != nil || target != 116010124 {
		t.Fatalf("decode target=%d err=%v", target, err)
	}
	for _, invalid := range [][]byte{nil, make([]byte, 3), make([]byte, 5), make([]byte, 4)} {
		if _, err := decodeCurrentEpicProductionStartRequest(invalid); err == nil {
			t.Fatalf("invalid body accepted: %x", invalid)
		}
	}
}

func TestParseCurrentEpicProductionCatalogPreservesJobTargets(t *testing.T) {
	source := initialEquipmentMemSource{currentEpicProductionPVFPath: `
[level limit]
55
[weekly limit]
7
[max charge point]
1380000
[must need item]
10163135 1
[need item type change]
10158124 30 10163135 100
[/need item type change]
[process material need count]
1
[big chance rate]
80000
[big chance max rate]
1000000
[big chance multiply]
2
[production item]
[info]
[job]
` + "`[gun blader]`" + `
[indexes]
116000143 116010124
[indexes]
[/info]
[info]
[job]
` + "`[swordman]`" + `
[indexes]
101000747
[indexes]
[/info]
[/production item]
[liquid list]
[item index]
10163134
[group key]
0
[get point]
20000
[need item list]
10163136
[/need item list]
[need item count]
1
[min make item count]
1
[max make item count]
1
[/item index]
[item index]
10163131
[group key]
1
[get point]
2000
[need item list]
3137
[/need item list]
[need item count]
50
[min make item count]
1
[max make item count]
1
[/item index]
[/liquid list]
`}
	catalog, err := parseCurrentEpicProductionCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.allows("[gun blader]", 116010124) || catalog.allows("[swordman]", 116010124) || !catalog.allows("[swordman]", 101000747) {
		t.Fatalf("unexpected target map: %+v", catalog.targetsByJob)
	}
	if catalog.changeMaterials[10158124] != 30 || catalog.changeMaterials[10163135] != 100 {
		t.Fatalf("unexpected change materials: %+v", catalog.changeMaterials)
	}
}

func TestValidateCurrentEpicProductionTargetRequiresPVFListAndAbility(t *testing.T) {
	const targetID = uint32(116010124)
	source := dungeonDropCatalogTestSource{
		dungeonDropStackableList: "",
		dungeonDropEquipmentList: "116010124 `character/gunBlader/weapon/sblade/116010124.equ`",
		"monster/monster.lst":    "",
		"equipment/character/gunBlader/weapon/sblade/116010124.equ": `
[equipment type]
` + "`[weapon]` 17" + `
[custom ability type]
` + "`epic production`" + `
`,
	}
	items, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &currentEpicProductionCatalog{targetsByJob: map[string]map[uint32]struct{}{
		"[gun blader]": {targetID: {}},
	}}
	definition, err := validateCurrentEpicProductionTarget(source, items, catalog, "[gun blader]", targetID)
	if err != nil || definition.ItemID != targetID {
		t.Fatalf("validate definition=%+v err=%v", definition, err)
	}
	if _, err := validateCurrentEpicProductionTarget(source, items, catalog, "[swordman]", targetID); err == nil {
		t.Fatal("cross-job production target accepted")
	}
}

func TestStartCurrentEpicProductionPersistsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{CharacterID: "2", AccountID: "account-1", Stats: map[string]int64{}}
	if err := repositories.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	changed, err := startCurrentEpicProduction(ctx, repositories.Character, "account-1", "2", 116010124)
	if err != nil || !changed {
		t.Fatalf("first start changed=%t err=%v", changed, err)
	}
	saved, found, err := repositories.Character.Load(ctx, "2")
	if err != nil || !found || saved.Stats[currentEpicProductionTargetStat] != 116010124 {
		t.Fatalf("saved=%+v found=%t err=%v", saved.Stats, found, err)
	}
	changed, err = startCurrentEpicProduction(ctx, repositories.Character, "account-1", "2", 116010124)
	if err != nil || changed {
		t.Fatalf("idempotent retry changed=%t err=%v", changed, err)
	}
	if _, err := startCurrentEpicProduction(ctx, repositories.Character, "account-1", "2", 116020124); !errors.Is(err, errCurrentEpicProductionAlreadyActive) {
		t.Fatalf("different active target err=%v", err)
	}
}

func TestBuildCurrentEpicProductionStartSuccessBody(t *testing.T) {
	body := buildCurrentEpicProductionStartSuccessBody(116010124)
	if len(body) != 8 || binary.LittleEndian.Uint32(body[0:4]) != 116010124 || binary.LittleEndian.Uint32(body[4:8]) != 0 {
		t.Fatalf("success body=%x", body)
	}
}

func TestDecodeCurrentEpicProductionProcessRequest(t *testing.T) {
	body := []byte{
		3, 0,
		0xbe, 0x13, 0x9b, 0x00, 0x7e, 0, 0, 0,
		0xbb, 0x13, 0x9b, 0x00, 0x7b, 0, 0, 0,
		0xba, 0x13, 0x9b, 0x00, 0x7a, 0, 0, 0,
	}
	request, err := decodeCurrentEpicProductionProcessRequest(body)
	if err != nil || len(request.Materials) != 3 {
		t.Fatalf("decode request=%+v err=%v", request, err)
	}
	want := []currentEpicProductionProcessMaterial{{ItemID: 10163134, Slot: 126}, {ItemID: 10163131, Slot: 123}, {ItemID: 10163130, Slot: 122}}
	for index := range want {
		if request.Materials[index] != want[index] {
			t.Fatalf("material[%d]=%+v want=%+v", index, request.Materials[index], want[index])
		}
	}
	for _, invalid := range [][]byte{nil, {0, 0}, body[:len(body)-1], append(append([]byte(nil), body...), 0)} {
		if _, err := decodeCurrentEpicProductionProcessRequest(invalid); err == nil {
			t.Fatalf("invalid process body accepted: %x", invalid)
		}
	}
	forged := append([]byte(nil), body...)
	binary.LittleEndian.PutUint32(forged[6:10], math.MaxUint32)
	if _, err := decodeCurrentEpicProductionProcessRequest(forged); err == nil {
		t.Fatal("forged u32 slot accepted")
	}
}

func TestDecodeCurrentEpicProductionChangeRequestFromLivePacket(t *testing.T) {
	body := []byte{
		0xca, 0x7a, 0xea, 0x06,
		0x02, 0x00,
		0x2c, 0x00, 0x9b, 0x00, 0x6d, 0x01, 0x00, 0x00,
		0xbf, 0x13, 0x9b, 0x00, 0x7f, 0x00, 0x00, 0x00,
	}
	request, err := decodeCurrentEpicProductionChangeRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.TargetItemID != 116030154 || len(request.Materials) != 2 ||
		request.Materials[0] != (currentEpicProductionProcessMaterial{ItemID: 10158124, Slot: 365}) ||
		request.Materials[1] != (currentEpicProductionProcessMaterial{ItemID: 10163135, Slot: 127}) {
		t.Fatalf("request=%+v", request)
	}
	for _, invalid := range [][]byte{nil, body[:5], body[:len(body)-1], append(append([]byte(nil), body...), 0)} {
		if _, err := decodeCurrentEpicProductionChangeRequest(invalid); err == nil {
			t.Fatalf("invalid change body accepted: %x", invalid)
		}
	}
	forged := append([]byte(nil), body...)
	binary.LittleEndian.PutUint32(forged[10:14], math.MaxUint32)
	if _, err := decodeCurrentEpicProductionChangeRequest(forged); err == nil {
		t.Fatal("forged change slot accepted")
	}
}

func currentEpicProductionTestCatalog() *currentEpicProductionCatalog {
	return &currentEpicProductionCatalog{
		catalysts: map[uint32]currentEpicProductionCatalyst{
			10163134: {ItemID: 10163134, GroupKey: 0, GetPoint: 20000},
			10163133: {ItemID: 10163133, GroupKey: 0, GetPoint: 10000},
			10163131: {ItemID: 10163131, GroupKey: 1, GetPoint: 2000},
			10163130: {
				ItemID: 10163130, GroupKey: 2, LevelLimit: 55, GetPoint: 1000,
				NeedItemIDs: map[uint32]struct{}{10158124: {}}, NeedItemType: "material",
				NeedCount: 75000, MinMakeCount: 1, MaxMakeCount: 1, IsMaterialPoint: true,
			},
		},
		targetsByJob: map[string]map[uint32]struct{}{
			"[gun blader]": {116010124: {}, 116020124: {}, 116030154: {}},
		},
		changeMaterials:           map[uint32]uint32{10158124: 30, 10163135: 100},
		materialPointsByRarity:    map[int64]uint32{4: 500},
		levelLimit:                55,
		weeklyLimit:               7,
		maxChargePoint:            1380000,
		processMaterialNeedCount:  1,
		requiredMaterialItemID:    10163135,
		requiredMaterialItemCount: 1,
		bigChanceRate:             0,
		bigChanceMaxRate:          1000000,
		bigChanceMultiply:         2,
	}
}

func currentEpicProductionAbilityTestCatalogText() string {
	return "[custom ability type] `epic production`\n" +
		"[group]\n[key] `1`\n[indexes] 1 2 3 4\n[/indexes]\n[/group]\n" +
		"[group]\n[key] `2`\n[indexes] 11 12 13 14 15 16 17 18 19 20 21 22\n[/indexes]\n[/group]\n" +
		"[info]\n[index] 4\n[material]\n[grade] `D`\n[list] 3173 50 10163135 50 10099774 20\n[/list]\n[/material]\n[/info]\n" +
		"[info]\n[index] 20\n[material]\n[grade] `D`\n[list] 3173 100 10163135 100 10099774 50\n[/list]\n[/material]\n[/info]\n"
}

func TestDecodeAndParseCurrentEpicProductionAbility(t *testing.T) {
	request, err := decodeCurrentEpicProductionAbilityRequest([]byte{0x1a, 0x00, 0x00, 0x04})
	if err != nil {
		t.Fatal(err)
	}
	if request != (currentEpicProductionAbilityRequest{Slot: 26, Category: 0, Option: 4}) {
		t.Fatalf("request=%+v", request)
	}
	for _, invalid := range [][]byte{nil, {0x1a, 0x00, 0x00}, {0x1a, 0x00, 0x00, 0x04, 0x00}, {0xff, 0xff, 0x00, 0x04}} {
		if _, err := decodeCurrentEpicProductionAbilityRequest(invalid); err == nil {
			t.Fatalf("invalid ability body accepted: %x", invalid)
		}
	}

	source := initialEquipmentMemSource{currentEpicProductionAbilityPVFPath: currentEpicProductionAbilityTestCatalogText()}
	catalog, err := parseCurrentEpicProductionAbilityCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	recipe, err := currentEpicProductionAbilityRecipeFor(catalog, request)
	if err != nil {
		t.Fatal(err)
	}
	want := []currentEpicProductionAbilityMaterial{{ItemID: 3173, Count: 50}, {ItemID: 10163135, Count: 50}, {ItemID: 10099774, Count: 20}}
	if !reflect.DeepEqual(recipe.Materials, want) {
		t.Fatalf("recipe materials=%+v want=%+v", recipe.Materials, want)
	}
	request.Category, request.Option = 1, 20
	if _, err := currentEpicProductionAbilityRecipeFor(catalog, request); err != nil {
		t.Fatal(err)
	}
	request.Option = 4
	if _, err := currentEpicProductionAbilityRecipeFor(catalog, request); err == nil {
		t.Fatal("cross-category ability option accepted")
	}
}

func TestCommitCurrentEpicProductionAbilityConsumesRecipeAndPersistsRawTail(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "2", AccountID: "account-1", Level: 55,
		Stats: map[string]int64{currentEpicProductionTargetStat: 116010124},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "account-1",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(363): {ItemID: 10099774, Count: 100},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "2",
		Slots: map[string]dnfrepo.ItemStack{
			"0:26": {ItemID: 116010124, Count: 1},
			"0:30": {ItemID: 3173, Count: 20},
			"0:31": {ItemID: 3173, Count: 40},
			"0:32": {ItemID: 10163135, Count: 70},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := Service{options: options{accountID: "account-1"}, repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true }}
	session := &gameSession{selectedCharacterID: 2}
	recipe := currentEpicProductionAbilityRecipe{
		Category: 0,
		Option:   4,
		Materials: []currentEpicProductionAbilityMaterial{
			{ItemID: 3173, Count: 50},
			{ItemID: 10163135, Count: 50},
			{ItemID: 10099774, Count: 20},
		},
	}
	request := currentEpicProductionAbilityRequest{Slot: 26, Category: 0, Option: 4}
	result, err := service.commitCurrentEpicProductionAbility(ctx, session, recipe, request, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || len(result.Updates) != 5 {
		t.Fatalf("result=%+v", result)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "2")
	if _, found := inventory.Slots["0:30"]; found || inventory.Slots["0:31"].Count != 10 || inventory.Slots["0:32"].Count != 20 {
		t.Fatalf("ordinary materials=%+v", inventory.Slots)
	}
	target := inventory.Slots["0:26"]
	if len(target.RawEntry) != currentItemListEntryWireSize || target.RawEntry[currentEpicProductionAbilityTailOffset] != 4 || target.Extra["tail_data_72"] != "0400000000" {
		t.Fatalf("target raw tail=%x extra=%+v", target.RawEntry[currentEpicProductionAbilityTailOffset:], target.Extra)
	}
	account, _, _ := repositories.AccountInventory.Load(ctx, "account-1")
	if account.Slots[dnfrepo.AccountSharedInventorySlotKey(363)].Count != 80 {
		t.Fatalf("unique soul=%+v", account.Slots)
	}

	replay, err := service.commitCurrentEpicProductionAbility(ctx, session, recipe, request, time.Now().UTC())
	if err != nil || replay.Applied || len(replay.Updates) != 0 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	inventory, _, _ = repositories.Inventory.Load(ctx, "2")
	account, _, _ = repositories.AccountInventory.Load(ctx, "account-1")
	if inventory.Slots["0:31"].Count != 10 || inventory.Slots["0:32"].Count != 20 || account.Slots[dnfrepo.AccountSharedInventorySlotKey(363)].Count != 80 {
		t.Fatal("idempotent replay consumed materials")
	}

	inventory.Slots["0:31"] = dnfrepo.ItemStack{ItemID: 3173, Count: 100}
	inventory.Slots["0:32"] = dnfrepo.ItemStack{ItemID: 10163135, Count: 100}
	if err := repositories.Inventory.Save(ctx, inventory); err != nil {
		t.Fatal(err)
	}
	secondRecipe := currentEpicProductionAbilityRecipe{
		Category: 1,
		Option:   20,
		Materials: []currentEpicProductionAbilityMaterial{
			{ItemID: 3173, Count: 100},
			{ItemID: 10163135, Count: 100},
			{ItemID: 10099774, Count: 50},
		},
	}
	secondRequest := currentEpicProductionAbilityRequest{Slot: 26, Category: 1, Option: 20}
	secondResult, err := service.commitCurrentEpicProductionAbility(ctx, session, secondRecipe, secondRequest, time.Now().UTC())
	if err != nil || !secondResult.Applied {
		t.Fatalf("independent category assignment result=%+v err=%v", secondResult, err)
	}
	inventory, _, _ = repositories.Inventory.Load(ctx, "2")
	target = inventory.Slots["0:26"]
	if target.RawEntry[currentEpicProductionAbilityTailOffset] != 4 || target.RawEntry[currentEpicProductionAbilityTailOffset+1] != 20 ||
		target.Extra["tail_data_72"] != "0414000000" {
		t.Fatalf("independent categories did not persist together: tail=%x extra=%+v", target.RawEntry[currentEpicProductionAbilityTailOffset:], target.Extra)
	}

	recipe.Option = 3
	recipe.Materials[0].Count = 999
	request.Option = 3
	if _, err := service.commitCurrentEpicProductionAbility(ctx, session, recipe, request, time.Now().UTC()); !errors.Is(err, errCurrentEpicProductionMaterial) {
		t.Fatalf("insufficient ability material err=%v", err)
	}
	inventory, _, _ = repositories.Inventory.Load(ctx, "2")
	if inventory.Slots["0:26"].RawEntry[currentEpicProductionAbilityTailOffset] != 4 ||
		inventory.Slots["0:26"].RawEntry[currentEpicProductionAbilityTailOffset+1] != 20 {
		t.Fatal("failed ability assignment changed target")
	}
}

func TestCommitCurrentEpicProductionChangeConsumesAccountAndCharacterMaterialsAtomically(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "2", AccountID: "account-1", Level: 25,
		Stats: map[string]int64{
			currentEpicProductionTargetStat: 116010124,
			currentEpicProductionChargeStat: 23000,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "account-1",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(365): {ItemID: 10158124, Count: 9999},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "2",
		Slots: map[string]dnfrepo.ItemStack{
			"0:127": {ItemID: 10163135, Count: 300},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := Service{options: options{accountID: "account-1"}, repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true }}
	session := &gameSession{selectedCharacterID: 2}
	request := currentEpicProductionChangeRequest{
		TargetItemID: 116030154,
		Materials: []currentEpicProductionProcessMaterial{
			{ItemID: 10158124, Slot: 365},
			{ItemID: 10163135, Slot: 127},
		},
	}
	result, err := service.commitCurrentEpicProductionChange(ctx, session, currentEpicProductionTestCatalog(), "[gun blader]", request, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetItemID != 116030154 || len(result.Updates) != 2 {
		t.Fatalf("result=%+v", result)
	}
	character, _, _ := repositories.Character.Load(ctx, "2")
	if character.Stats[currentEpicProductionTargetStat] != 116030154 || character.Stats[currentEpicProductionChargeStat] != 23000 {
		t.Fatalf("character stats=%+v", character.Stats)
	}
	account, _, _ := repositories.AccountInventory.Load(ctx, "account-1")
	if got := account.Slots[dnfrepo.AccountSharedInventorySlotKey(365)].Count; got != 9969 {
		t.Fatalf("epic soul count=%d want=9969", got)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "2")
	if got := inventory.Slots["0:127"].Count; got != 200 {
		t.Fatalf("carbon crystal count=%d want=200", got)
	}

	stack := inventory.Slots["0:127"]
	stack.Count = 99
	inventory.Slots["0:127"] = stack
	if err := repositories.Inventory.Save(ctx, inventory); err != nil {
		t.Fatal(err)
	}
	request.TargetItemID = 116020124
	if _, err := service.commitCurrentEpicProductionChange(ctx, session, currentEpicProductionTestCatalog(), "[gun blader]", request, time.Now().UTC()); !errors.Is(err, errCurrentEpicProductionMaterial) {
		t.Fatalf("insufficient change err=%v", err)
	}
	character, _, _ = repositories.Character.Load(ctx, "2")
	account, _, _ = repositories.AccountInventory.Load(ctx, "account-1")
	inventory, _, _ = repositories.Inventory.Load(ctx, "2")
	if character.Stats[currentEpicProductionTargetStat] != 116030154 ||
		account.Slots[dnfrepo.AccountSharedInventorySlotKey(365)].Count != 9969 ||
		inventory.Slots["0:127"].Count != 99 {
		t.Fatalf("failed change mutated state: stats=%+v account=%+v inventory=%+v", character.Stats, account.Slots, inventory.Slots)
	}
}

func TestBuildCurrentEpicProductionChangeSuccessBody(t *testing.T) {
	body := buildCurrentEpicProductionChangeSuccessBody(116030154)
	if len(body) != 4 || binary.LittleEndian.Uint32(body) != 116030154 {
		t.Fatalf("change success body=%x", body)
	}
}

func TestDecodeCurrentEpicProductionCompoundRequestFromLivePacket(t *testing.T) {
	body := []byte{
		0xba, 0x13, 0x9b, 0x00,
		0x01, 0x00,
		0x2c, 0x00, 0x9b, 0x00,
		0x6d, 0x01, 0x00, 0x00,
		0xc8, 0x00, 0x00, 0x00,
	}
	request, err := decodeCurrentEpicProductionCompoundRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.CatalystItemID != 10163130 || len(request.Materials) != 1 ||
		request.Materials[0] != (currentEpicProductionCompoundMaterial{ItemID: 10158124, Slot: 365, Count: 200}) {
		t.Fatalf("request=%+v", request)
	}
	for _, invalid := range [][]byte{nil, body[:5], body[:len(body)-1], append(append([]byte(nil), body...), 0)} {
		if _, err := decodeCurrentEpicProductionCompoundRequest(invalid); err == nil {
			t.Fatalf("invalid compound body accepted: %x", invalid)
		}
	}
	forged := append([]byte(nil), body...)
	binary.LittleEndian.PutUint32(forged[10:14], math.MaxUint32)
	if _, err := decodeCurrentEpicProductionCompoundRequest(forged); err == nil {
		t.Fatal("forged compound slot accepted")
	}
}

func TestCommitCurrentEpicProductionCompoundConsumesSharedSoulGrantsCatalystAndPersistsCarry(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "2", AccountID: "account-1", Level: 55, Stats: map[string]int64{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "account-1",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(365): {ItemID: 10158124, Count: 9999},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "2", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	service := Service{options: options{accountID: "account-1"}, repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true }}
	session := &gameSession{selectedCharacterID: 2}
	request := currentEpicProductionCompoundRequest{
		CatalystItemID: 10163130,
		Materials:      []currentEpicProductionCompoundMaterial{{ItemID: 10158124, Slot: 365, Count: 200}},
	}
	plan := currentEpicProductionCompoundPlan{
		Recipe: currentEpicProductionTestCatalog().catalysts[10163130],
		OutputDefinition: dungeonDropItemDefinition{
			ItemID: 10163130, Kind: dungeonDropItemStackable, StackableType: "[material]", SlotStart: 97, SlotEnd: 120,
		},
		Contribution: 100000,
	}
	result, err := service.commitCurrentEpicProductionCompound(ctx, session, plan, request, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if result.CatalystItemID != 10163130 || result.OutputCount != 1 || result.GroupSelector != 2 || result.CarryValue != 25000 {
		t.Fatalf("result=%+v", result)
	}
	account, _, _ := repositories.AccountInventory.Load(ctx, "account-1")
	if got := account.Slots[dnfrepo.AccountSharedInventorySlotKey(365)].Count; got != 9799 {
		t.Fatalf("epic soul count=%d want=9799", got)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "2")
	if got := inventory.Slots["0:3"]; got.ItemID != 10163130 || got.Count != 1 {
		t.Fatalf("catalyst stack=%+v", got)
	}
	character, _, _ := repositories.Character.Load(ctx, "2")
	if got := character.Stats[currentEpicProductionCarryGroup2Stat]; got != 25000 {
		t.Fatalf("carry=%d want=25000", got)
	}
	body := buildCurrentEpicProductionCompoundSuccessBody(result)
	if len(body) != 9 || binary.LittleEndian.Uint32(body[:4]) != 1 || body[4] != 2 || binary.LittleEndian.Uint32(body[5:9]) != 25000 {
		t.Fatalf("compound body=%x", body)
	}

	request.Materials[0].Count = 10000
	plan.Contribution = 5000000
	if _, err := service.commitCurrentEpicProductionCompound(ctx, session, plan, request, time.Now().UTC()); !errors.Is(err, errCurrentEpicProductionMaterial) {
		t.Fatalf("insufficient compound err=%v", err)
	}
	account, _, _ = repositories.AccountInventory.Load(ctx, "account-1")
	character, _, _ = repositories.Character.Load(ctx, "2")
	if account.Slots[dnfrepo.AccountSharedInventorySlotKey(365)].Count != 9799 || character.Stats[currentEpicProductionCarryGroup2Stat] != 25000 {
		t.Fatalf("failed compound mutated state: account=%+v stats=%+v", account.Slots, character.Stats)
	}
}

func TestCommitCurrentEpicProductionCompoundKeepsMultiOutputBatch(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "2", AccountID: "account-1", Level: 55,
		Stats: map[string]int64{currentEpicProductionCarryGroup2Stat: 60000},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "account-1",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(365): {ItemID: 10158124, Count: 9999},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "2", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	service := Service{options: options{accountID: "account-1"}, repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true }}
	request := currentEpicProductionCompoundRequest{
		CatalystItemID: 10163130,
		Materials:      []currentEpicProductionCompoundMaterial{{ItemID: 10158124, Slot: 365, Count: 200}},
	}
	plan := currentEpicProductionCompoundPlan{
		Recipe: currentEpicProductionTestCatalog().catalysts[10163130],
		OutputDefinition: dungeonDropItemDefinition{
			ItemID: 10163130, Kind: dungeonDropItemStackable, StackableType: "[material]", SlotStart: 97, SlotEnd: 120,
		},
		Contribution: 100000,
	}
	result, err := service.commitCurrentEpicProductionCompound(ctx, &gameSession{selectedCharacterID: 2}, plan, request, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if result.OutputCount != 2 || result.CarryValue != 10000 {
		t.Fatalf("result=%+v, want output=2 carry=10000", result)
	}
}

func TestCommitCurrentEpicProductionProcessValidatesAndConsumesAtomically(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "2", AccountID: "account-1", Level: 55,
		Stats: map[string]int64{currentEpicProductionTargetStat: 116010124},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "2", Slots: map[string]dnfrepo.ItemStack{
		"0:126": {ItemID: 10163134, Count: 100},
		"0:123": {ItemID: 10163131, Count: 100},
		"0:122": {ItemID: 10163130, Count: 100},
		"0:127": {ItemID: 10163135, Count: 300},
	}}); err != nil {
		t.Fatal(err)
	}
	service := Service{options: options{accountID: "account-1"}, repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true }}
	session := &gameSession{selectedCharacterID: 2}
	request := currentEpicProductionProcessRequest{Materials: []currentEpicProductionProcessMaterial{
		{ItemID: 10163134, Slot: 126}, {ItemID: 10163131, Slot: 123}, {ItemID: 10163130, Slot: 122},
	}}
	now := time.Date(2026, 7, 27, 23, 4, 29, 0, time.UTC)
	result, err := service.commitCurrentEpicProductionProcess(ctx, session, currentEpicProductionTestCatalog(), request, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChargePoint != 23000 || result.WeeklyCount != 1 || result.BigSuccess || len(result.Updates) != 4 {
		t.Fatalf("result=%+v", result)
	}
	character, found, err := repositories.Character.Load(ctx, "2")
	if err != nil || !found || character.Stats[currentEpicProductionChargeStat] != 23000 || character.Stats[currentEpicProductionWeeklyCountStat] != 1 {
		t.Fatalf("character stats=%+v found=%t err=%v", character.Stats, found, err)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "2")
	if err != nil || !found {
		t.Fatalf("inventory found=%t err=%v", found, err)
	}
	for slot, wantCount := range map[string]int64{"0:126": 99, "0:123": 99, "0:122": 99, "0:127": 299} {
		if got := inventory.Slots[slot].Count; got != wantCount {
			t.Fatalf("slot %s count=%d want=%d", slot, got, wantCount)
		}
	}

	forged := request
	forged.Materials = append([]currentEpicProductionProcessMaterial(nil), request.Materials...)
	forged.Materials[0].Slot = 125
	if _, err := service.commitCurrentEpicProductionProcess(ctx, session, currentEpicProductionTestCatalog(), forged, now); !errors.Is(err, errCurrentEpicProductionMaterial) {
		t.Fatalf("forged process err=%v", err)
	}
	after, _, _ := repositories.Character.Load(ctx, "2")
	if after.Stats[currentEpicProductionChargeStat] != 23000 || after.Stats[currentEpicProductionWeeklyCountStat] != 1 {
		t.Fatalf("forged request mutated stats=%+v", after.Stats)
	}
}

func TestBuildCurrentEpicProductionProcessAndInfoBodies(t *testing.T) {
	process := buildCurrentEpicProductionProcessSuccessBody(currentEpicProductionProcessResult{ChargePoint: 23000, BigSuccess: true})
	if len(process) != 6 || binary.LittleEndian.Uint32(process[:4]) != 23000 || process[4] != 1 || process[5] != 0 {
		t.Fatalf("process body=%x", process)
	}
	info := buildCurrentEpicProductionInfoBody(2, 116010124, 23000, 1, 1234, 25000)
	if len(info) != 32 || binary.LittleEndian.Uint32(info[0:4]) != 2 || binary.LittleEndian.Uint32(info[4:8]) != 1 ||
		binary.LittleEndian.Uint32(info[8:12]) != 116010124 || binary.LittleEndian.Uint32(info[12:16]) != 23000 ||
		binary.LittleEndian.Uint32(info[20:24]) != 1234 || binary.LittleEndian.Uint32(info[24:28]) != 25000 {
		t.Fatalf("info body=%x", info)
	}
}

func TestCurrentEpicProductionRemainingWeeklyCount(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	weekStart := currentEpicProductionWeekStart(now)
	for _, test := range []struct {
		name        string
		used        int64
		storedStart int64
		limit       uint32
		want        uint32
	}{
		{name: "unused", used: 0, storedStart: weekStart, limit: 7, want: 7},
		{name: "one used", used: 1, storedStart: weekStart, limit: 7, want: 6},
		{name: "limit reached", used: 7, storedStart: weekStart, limit: 7, want: 0},
		{name: "over limit", used: 8, storedStart: weekStart, limit: 7, want: 0},
		{name: "new week", used: 7, storedStart: weekStart - 7*24*60*60, limit: 7, want: 7},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := currentEpicProductionRemainingWeeklyCount(test.used, test.storedStart, test.limit, now); got != test.want {
				t.Fatalf("remaining=%d want=%d", got, test.want)
			}
		})
	}
}

func TestCurrentEpicProductionRealPVF(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the runtime epic production catalog")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := parseCurrentEpicProductionCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.changeMaterials[10158124] != 30 || catalog.changeMaterials[10163135] != 100 {
		t.Fatalf("real PVF change materials=%+v", catalog.changeMaterials)
	}
	abilityCatalog, err := parseCurrentEpicProductionAbilityCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	darkElement, err := currentEpicProductionAbilityRecipeFor(abilityCatalog, currentEpicProductionAbilityRequest{Category: 0, Option: 4})
	if err != nil || len(darkElement.Materials) != 3 || darkElement.Materials[0] != (currentEpicProductionAbilityMaterial{ItemID: 3173, Count: 50}) {
		t.Fatalf("real PVF dark-element recipe=%+v err=%v", darkElement, err)
	}
	darkNormal, err := currentEpicProductionAbilityRecipeFor(abilityCatalog, currentEpicProductionAbilityRequest{Category: 1, Option: 20})
	if err != nil || len(darkNormal.Materials) != 3 || darkNormal.Materials[2] != (currentEpicProductionAbilityMaterial{ItemID: 10099774, Count: 50}) {
		t.Fatalf("real PVF dark-normal recipe=%+v err=%v", darkNormal, err)
	}
	items, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	compoundRequest := currentEpicProductionCompoundRequest{
		CatalystItemID: 10163130,
		Materials:      []currentEpicProductionCompoundMaterial{{ItemID: 10158124, Slot: 365, Count: 200}},
	}
	compoundPlan, err := validateCurrentEpicProductionCompoundPlan(archive, items, catalog, compoundRequest)
	if err != nil {
		t.Fatal(err)
	}
	if compoundPlan.Contribution != 100000 || compoundPlan.Recipe.NeedCount != 75000 ||
		compoundPlan.Recipe.GroupKey != 2 || !compoundPlan.Recipe.IsMaterialPoint {
		t.Fatalf("compound plan=%+v", compoundPlan)
	}
	definition, err := validateCurrentEpicProductionTarget(archive, items, catalog, "[gun blader]", 116010124)
	if err != nil {
		t.Fatal(err)
	}
	if definition.PVFPath != "equipment/character/gunBlader/weapon/sblade/116010124.equ" {
		t.Fatalf("target path=%q", definition.PVFPath)
	}

	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "2",
		AccountID:   "account-1",
		Job:         "15",
		Stats:       map[string]int64{},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := Service{
		options:                 options{accountID: "account-1"},
		initialEquipmentArchive: archive,
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{conn: connection, selectedCharacterID: 2}
	request := make([]byte, 4)
	binary.LittleEndian.PutUint32(request, 116010124)
	if err := service.handleCurrentEpicProductionStart(session, request); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 || packet.Header.MsgID != uint16(1417) || packet.Header.Classification != 1 {
		t.Fatalf("response msg=%d class=%d rest=%d", packet.Header.MsgID, packet.Header.Classification, len(rest))
	}
	if want := upperSuccessBody(buildCurrentEpicProductionStartSuccessBody(116010124)); !bytes.Equal(packet.Body, want) {
		t.Fatalf("response body=%x want=%x", packet.Body, want)
	}
	saved, found, err := repositories.Character.Load(context.Background(), "2")
	if err != nil || !found || saved.Stats[currentEpicProductionTargetStat] != 116010124 {
		t.Fatalf("persisted stats=%+v found=%t err=%v", saved.Stats, found, err)
	}
}
