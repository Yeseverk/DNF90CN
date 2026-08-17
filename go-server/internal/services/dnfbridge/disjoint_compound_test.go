package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestParseCurrentDisjointAndEmblemRequests(t *testing.T) {
	disjoint := currentDisjointItemTestBody(65, 0, -1, 0x11223344)
	parsed, err := parseCurrentDisjointItemRequest(disjoint)
	if err != nil || parsed.SourceSlot != 65 || parsed.ListType != 0 || parsed.ToolSlot != -1 || parsed.Context != 0x11223344 {
		t.Fatalf("op26 request=%+v err=%v", parsed, err)
	}
	avatar, err := parseCurrentAvatarDisjointRequest([]byte{2, 0, 0x44, 0x33, 0x22, 0x11})
	if err != nil || avatar.SourceSlot != 2 || avatar.ExpectedItemID != 0x11223344 {
		t.Fatalf("op202 request=%+v err=%v", avatar, err)
	}
	emblemBody := currentEmblemCompoundTestBody([]currentEmblemCompoundInput{{ItemID: 8000, Slot: 70}, {ItemID: 8000, Slot: 71}})
	emblem, err := parseCurrentEmblemCompoundRequest(emblemBody)
	if err != nil || len(emblem.Inputs) != 2 || emblem.Inputs[1].Slot != 71 {
		t.Fatalf("op256 request=%+v err=%v", emblem, err)
	}
	repeatedEmblemBody := currentEmblemCompoundTestBody([]currentEmblemCompoundInput{{ItemID: 8000, Slot: 70}, {ItemID: 8000, Slot: 70}})
	repeatedEmblems, err := parseCurrentEmblemCompoundRequest(repeatedEmblemBody)
	if err != nil || len(repeatedEmblems.Inputs) != 2 || repeatedEmblems.Inputs[0].Slot != repeatedEmblems.Inputs[1].Slot {
		t.Fatalf("same-slot op256 request=%+v err=%v", repeatedEmblems, err)
	}
	if got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketDisjointItem), append(append([]byte(nil), disjoint...), 1, 2, 3, 4)); !bytes.Equal(got, disjoint) {
		t.Fatalf("op26 normalized=%x want=%x", got, disjoint)
	}
	if got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketCompoundEmblem), append(append([]byte(nil), emblemBody...), 1, 2, 3, 4)); !bytes.Equal(got, emblemBody) {
		t.Fatalf("op256 normalized=%x want=%x", got, emblemBody)
	}
	avatarBody := []byte{2, 0, 0x44, 0x33, 0x22, 0x11}
	if got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketDisjointAvatar), avatarBody); !bytes.Equal(got, avatarBody) {
		t.Fatalf("six-byte op202 normalized=%x want=%x", got, avatarBody)
	}
	if got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketDisjointAvatar), append(append([]byte(nil), avatarBody...), 1, 2, 3, 4)); !bytes.Equal(got, avatarBody) {
		t.Fatalf("trailed op202 normalized=%x want=%x", got, avatarBody)
	}
	for _, body := range [][]byte{disjoint[:4], append(disjoint, 0), []byte{0, 0, 1, 0, 0}} {
		if _, err := parseCurrentDisjointItemRequest(body); err == nil {
			t.Fatalf("malformed op26 accepted: %x", body)
		}
	}
	if _, err := parseCurrentEmblemCompoundRequest([]byte{1, 0, 0, 0, 0, 0, 0}); err == nil {
		t.Fatal("one-input op256 accepted")
	}
}

func TestCurrentEquipmentDisjointUsesPVFRewardsAndCurrentPopupBody(t *testing.T) {
	repositories, catalog := mustCurrentDisjointTestAssets(t, map[string]dnfrepo.ItemStack{"0:65": {ItemID: 5000, Count: 1}})
	ctx := context.Background()
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "account-1",
		Slots: map[string]dnfrepo.ItemStack{
			"0:358": {ItemID: 3037, Count: 800},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentDisjointTestService(repositories, catalog)
	request := currentDisjointItemTestBody(65, 0, -1, 0)
	if err := service.handleCurrentDisjointItem(&gameSession{conn: connection, selectedCharacterID: 19}, request); err != nil {
		t.Fatal(err)
	}
	ack, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || ack.Header.MsgID != uint16(dnfenum.CmdPacketDisjointItem) || ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		len(ack.Body) != 1+2+1+2+1+2*10 || ack.Body[0] != 1 || binary.LittleEndian.Uint16(ack.Body[1:3]) != 65 || ack.Body[3] != 0 || binary.LittleEndian.Uint16(ack.Body[4:6]) != math.MaxUint16 || ack.Body[6] != 2 {
		t.Fatalf("op26 ACK=%x header=%+v trailing=%d", ack.Body, ack.Header, len(trailing))
	}
	var clearCubePopupCount uint32
	for offset := 7; offset < len(ack.Body); offset += 10 {
		if binary.LittleEndian.Uint32(ack.Body[offset+2:offset+6]) == 3037 {
			clearCubePopupCount = binary.LittleEndian.Uint32(ack.Body[offset+6 : offset+10])
		}
	}
	if clearCubePopupCount != 2 {
		t.Fatalf("op26 clear-cube popup count=%d want granted amount 2; ACK=%x", clearCubePopupCount, ack.Body)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("inventory found=%t err=%v", found, err)
	}
	if _, exists := inventory.Slots["0:65"]; exists {
		t.Fatalf("equipment source was not consumed: %+v", inventory.Slots["0:65"])
	}
	assertCurrentDisjointInventoryAmount(t, inventory, 3037, 0)
	assertCurrentDisjointInventoryAmount(t, inventory, 3033, 0)
	account, found, err := repositories.AccountInventory.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("account inventory found=%t err=%v", found, err)
	}
	if stack := account.Slots["0:358"]; stack.ItemID != 3037 || stack.Count != 802 {
		t.Fatalf("crystal warehouse slot 358=%+v want cumulative 3037x802", stack)
	}
	if stack := account.Slots["0:354"]; stack.ItemID != 3033 || stack.Count != 2 {
		t.Fatalf("crystal warehouse slot 354=%+v want 3033x2", stack)
	}
}

func TestCurrentAvatarDisjointUsesRuntimeDisjointInfoAndPopupBody(t *testing.T) {
	repositories, catalog := mustCurrentDisjointTestAssets(t, map[string]dnfrepo.ItemStack{"1:2": {ItemID: 5100, Count: 0}})
	connection := &bufferConn{}
	service := currentDisjointTestService(repositories, catalog)
	if err := service.handleCurrentAvatarDisjoint(&gameSession{conn: connection, selectedCharacterID: 19}, []byte{2, 0, 0xec, 0x13, 0, 0}); err != nil {
		t.Fatal(err)
	}
	ack, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || ack.Header.MsgID != uint16(dnfenum.CmdPacketDisjointAvatar) || ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		len(ack.Body) != 1+2+2+10 || ack.Body[0] != 1 || binary.LittleEndian.Uint16(ack.Body[1:3]) != 2 || binary.LittleEndian.Uint16(ack.Body[3:5]) != 1 ||
		binary.LittleEndian.Uint16(ack.Body[5:7]) != 289 || binary.LittleEndian.Uint32(ack.Body[7:11]) != 9001 || binary.LittleEndian.Uint32(ack.Body[11:15]) != 1 {
		t.Fatalf("op202 ACK=%x header=%+v trailing=%d", ack.Body, ack.Header, len(trailing))
	}
	inventory, _, _ := repositories.Inventory.Load(context.Background(), "19")
	if _, exists := inventory.Slots["1:2"]; exists {
		t.Fatal("avatar source was not consumed")
	}
	if stack := inventory.Slots["0:289"]; stack.ItemID != 9001 || stack.Count != 1 {
		t.Fatalf("emblem page slot 289=%+v want 9001x1", stack)
	}
}

func TestCurrentAvatarDisjointRejectsMismatchedExpectedItemWithoutMutation(t *testing.T) {
	repositories, catalog := mustCurrentDisjointTestAssets(t, map[string]dnfrepo.ItemStack{"1:2": {ItemID: 5100, Count: 0}})
	connection := &bufferConn{}
	service := currentDisjointTestService(repositories, catalog)
	if err := service.handleCurrentAvatarDisjoint(&gameSession{conn: connection, selectedCharacterID: 19}, []byte{2, 0, 0xed, 0x13, 0, 0}); err != nil {
		t.Fatal(err)
	}
	failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || failure.Header.MsgID != uint16(dnfenum.CmdPacketDisjointAvatar) || !bytes.Equal(failure.Body, []byte{0, 4}) {
		t.Fatalf("failure=%x header=%+v trailing=%d", failure.Body, failure.Header, len(trailing))
	}
	inventory, _, _ := repositories.Inventory.Load(context.Background(), "19")
	if stack, exists := inventory.Slots["1:2"]; !exists || stack.ItemID != 5100 || stack.Count != 0 {
		t.Fatalf("avatar source mutated: exists=%t stack=%+v", exists, stack)
	}
}

func TestCurrentEmblemCompoundConsumesInputsRefreshesAndShowsRealReward(t *testing.T) {
	repositories, catalog := mustCurrentDisjointTestAssets(t, map[string]dnfrepo.ItemStack{
		"0:70": {ItemID: 8000, Count: 1},
		"0:71": {ItemID: 8000, Count: 1},
	})
	connection := &bufferConn{}
	service := currentDisjointTestService(repositories, catalog)
	if err := service.handleCurrentEmblemCompound(&gameSession{conn: connection, selectedCharacterID: 19}, currentEmblemCompoundTestBody([]currentEmblemCompoundInput{{ItemID: 8000, Slot: 70}, {ItemID: 8000, Slot: 71}})); err != nil {
		t.Fatal(err)
	}
	update, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) || update.Header.Classification != 0 || len(update.Body) != 3+3*currentItemListEntryWireSize || update.Body[0] != 0 || binary.LittleEndian.Uint16(update.Body[1:3]) != 3 {
		t.Fatalf("op14=%x header=%+v", update.Body, update.Header)
	}
	ack, trailing := splitGameServerUpperPacket(t, rest)
	if len(ack.Body) != 10 || ack.Header.MsgID != uint16(dnfenum.CmdPacketCompoundEmblem) || ack.Header.Classification != dnfproto.DefaultChannelClassification || !bytes.Equal(ack.Body, []byte{1, 1, 0x29, 0x23, 0, 0, 1, 0, 0, 0}) {
		t.Fatalf("op256 ACK=%x header=%+v", ack.Body, ack.Header)
	}
	list, trailing := splitGameServerUpperPacket(t, trailing)
	if len(trailing) != 0 || list.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) || !bytes.Contains(list.Body, []byte{0x21, 0x01, 0x29, 0x23}) {
		t.Fatalf("full list-0 refresh=%x... header=%+v trailing=%d", list.Body[:min(16, len(list.Body))], list.Header, len(trailing))
	}
	inventory, _, _ := repositories.Inventory.Load(context.Background(), "19")
	if _, exists := inventory.Slots["0:70"]; exists {
		t.Fatal("first emblem input remains")
	}
	if _, exists := inventory.Slots["0:71"]; exists {
		t.Fatal("second emblem input remains")
	}
	assertCurrentDisjointInventoryAmount(t, inventory, 9001, 1)
}

func TestCurrentEmblemCompoundConsumesRepeatedInputsFromOneStack(t *testing.T) {
	repositories, catalog := mustCurrentDisjointTestAssets(t, map[string]dnfrepo.ItemStack{
		"0:70": {ItemID: 8000, Count: 2},
	})
	connection := &bufferConn{}
	service := currentDisjointTestService(repositories, catalog)
	body := currentEmblemCompoundTestBody([]currentEmblemCompoundInput{{ItemID: 8000, Slot: 70}, {ItemID: 8000, Slot: 70}})
	if err := service.handleCurrentEmblemCompound(&gameSession{conn: connection, selectedCharacterID: 19}, body); err != nil {
		t.Fatal(err)
	}
	update, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) || update.Body[0] != 0 || binary.LittleEndian.Uint16(update.Body[1:3]) != 2 {
		t.Fatalf("same-slot op14=%x header=%+v", update.Body, update.Header)
	}
	ack, rest := splitGameServerUpperPacket(t, rest)
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketCompoundEmblem) || !bytes.Equal(ack.Body, []byte{1, 1, 0x29, 0x23, 0, 0, 1, 0, 0, 0}) {
		t.Fatalf("same-slot op256 ACK=%x header=%+v", ack.Body, ack.Header)
	}
	_, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 {
		t.Fatalf("same-slot trailing=%d", len(trailing))
	}
	inventory, _, _ := repositories.Inventory.Load(context.Background(), "19")
	if _, exists := inventory.Slots["0:70"]; exists {
		t.Fatal("same-slot emblem stack remains")
	}
	assertCurrentDisjointInventoryAmount(t, inventory, 9001, 1)
}

func TestCurrentEmblemCompoundRejectsRepeatedInputsBeyondStackCount(t *testing.T) {
	repositories, catalog := mustCurrentDisjointTestAssets(t, map[string]dnfrepo.ItemStack{
		"0:70": {ItemID: 8000, Count: 1},
	})
	connection := &bufferConn{}
	service := currentDisjointTestService(repositories, catalog)
	body := currentEmblemCompoundTestBody([]currentEmblemCompoundInput{{ItemID: 8000, Slot: 70}, {ItemID: 8000, Slot: 70}})
	if err := service.handleCurrentEmblemCompound(&gameSession{conn: connection, selectedCharacterID: 19}, body); err != nil {
		t.Fatal(err)
	}
	failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || failure.Header.MsgID != uint16(dnfenum.CmdPacketCompoundEmblem) || !bytes.Equal(failure.Body, []byte{0, 4}) {
		t.Fatalf("same-slot failure=%x header=%+v trailing=%d", failure.Body, failure.Header, len(trailing))
	}
	inventory, _, _ := repositories.Inventory.Load(context.Background(), "19")
	if stack := inventory.Slots["0:70"]; stack.ItemID != 8000 || stack.Count != 1 {
		t.Fatalf("insufficient same-slot source mutated: %+v", stack)
	}
}

func TestCurrentDisjointFailureDoesNotConsumeSource(t *testing.T) {
	repositories, catalog := mustCurrentDisjointTestAssets(t, map[string]dnfrepo.ItemStack{"0:65": {ItemID: 5000, Count: 1}})
	connection := &bufferConn{}
	service := currentDisjointTestService(repositories, catalog)
	if err := service.handleCurrentAvatarDisjoint(&gameSession{conn: connection, selectedCharacterID: 19}, []byte{2, 0}); err != nil {
		t.Fatal(err)
	}
	failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || failure.Header.MsgID != uint16(dnfenum.CmdPacketDisjointAvatar) || !bytes.Equal(failure.Body, []byte{0, 4}) {
		t.Fatalf("failure=%x header=%+v trailing=%d", failure.Body, failure.Header, len(trailing))
	}
	inventory, _, _ := repositories.Inventory.Load(context.Background(), "19")
	if stack := inventory.Slots["0:65"]; stack.ItemID != 5000 || stack.Count != 1 {
		t.Fatalf("source mutated: %+v", stack)
	}
}

func TestRealPVFDisjointAndEmblemMappings(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to the runtime Script.pvf")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	config, err := loadCurrentDisjointPVFConfig(archive)
	if err != nil {
		t.Fatal(err)
	}
	if config.CubeItemID != 3037 || len(config.AvatarByJob["fighter"]) != 15 || len(config.EmblemBoosters) != 13 || config.EmblemBoosters[[2]int{1, 2}] != 10095581 || config.EmblemBoosters[[2]int{4, 2}] != 10095593 {
		t.Fatalf("runtime mapping cube=%d fighter=%v boosters=%v", config.CubeItemID, config.AvatarByJob["fighter"], config.EmblemBoosters)
	}
	if len(config.AvatarEmblemTables) < 3 || len(config.AvatarEmblemTables[2].Pools) == 0 || config.AvatarEmblemTables[2].Pools[0].PickCount != 4 {
		t.Fatalf("runtime avatar emblem tables=%d grade2 pools=%+v", len(config.AvatarEmblemTables), config.AvatarEmblemTables[2].Pools)
	}
	grade2Pool := config.AvatarEmblemTables[2].Pools[0]
	grade2Has2550046 := false
	for _, entry := range grade2Pool.Rewards {
		if entry.ItemID == 2550046 && entry.Weight == 1500 && entry.Count == 1 {
			grade2Has2550046 = true
		}
	}
	if !grade2Has2550046 {
		t.Fatalf("runtime grade2 pool missing 2550046/1500/1: %+v", grade2Pool.Rewards)
	}
	avatar, err := catalog.ResolveItem(412550012)
	if err != nil {
		t.Fatal(err)
	}
	avatarDoc, err := parseDungeonCardPVFDocument(archive, avatar.PVFPath)
	if err != nil {
		t.Fatal(err)
	}
	rewards, err := currentAvatarDisjointRewards(config, avatarDoc)
	if err != nil || len(rewards) == 0 {
		t.Fatalf("runtime avatar result=%+v err=%v", rewards, err)
	}
	avatarGrade, _ := avatarDoc.Int("grade")
	poolItems := make(map[uint32]bool)
	for _, pool := range config.AvatarEmblemTables[int(avatarGrade)].Pools {
		for _, entry := range pool.Rewards {
			poolItems[entry.ItemID] = true
		}
	}
	totalPicks := 0
	for _, pool := range config.AvatarEmblemTables[int(avatarGrade)].Pools {
		totalPicks += pool.PickCount
	}
	totalCount := uint32(0)
	for _, reward := range rewards {
		if !poolItems[reward.ItemID] || reward.Count == 0 {
			t.Fatalf("runtime avatar reward outside grade %d pool: %+v", avatarGrade, rewards)
		}
		if _, err := catalog.ResolveItem(reward.ItemID); err != nil {
			t.Fatalf("runtime avatar reward item=%d: %v", reward.ItemID, err)
		}
		totalCount += reward.Count
	}
	if totalCount < uint32(totalPicks) {
		t.Fatalf("runtime avatar rewards=%+v grade=%d want at least %d picks", rewards, avatarGrade, totalPicks)
	}
	reward, err := currentRollEmblemBoosterReward(catalog, 10095581)
	if err != nil || reward.ItemID == 0 || reward.Count == 0 {
		t.Fatalf("runtime booster result=%+v err=%v", reward, err)
	}
	if _, err := catalog.ResolveItem(reward.ItemID); err != nil {
		t.Fatalf("runtime booster reward item=%d: %v", reward.ItemID, err)
	}
}

func TestCurrentGrantDisjointRewardsFixedWarehouseSlots(t *testing.T) {
	source := bridgePVFSource{
		"monster/monster.lst":     "",
		"equipment/equipment.lst": "",
		"stackable/stackable.lst": "3037 `cube.stk`\n3166 `element.stk`\n10099773 `soul.stk`\n",
		"stackable/cube.stk":      "[stackable type] `[material]`\n[stack limit] 999\n",
		"stackable/element.stk":   "[stackable type] `[material]`\n[stack limit] 999\n",
		"stackable/soul.stk":      "[stackable type] `[material]`\n[attach type] `[account]`\n[stack limit] 999\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	newOwners := func() (*dnfrepo.InventoryRecord, *dnfrepo.AccountInventoryRecord) {
		return &dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{}},
			&dnfrepo.AccountInventoryRecord{AccountID: "account-1", Slots: map[string]dnfrepo.ItemStack{}}
	}

	// Cube fragments and souls land on their live-tested fixed slots; element
	// crystals stay in the backpack.
	inventory, account := newOwners()
	rows, dirty, err := currentGrantDisjointRewards(inventory, account, catalog, []currentDisjointReward{{ItemID: 3037, Count: 10}, {ItemID: 3166, Count: 2}, {ItemID: 10099773, Count: 2}})
	if err != nil || !dirty {
		t.Fatalf("grant rows=%+v dirty=%t err=%v", rows, dirty, err)
	}
	if stack := account.Slots["0:358"]; stack.ItemID != 3037 || stack.Count != 10 {
		t.Fatalf("cube stack=%+v want 3037x10 at slot 358", stack)
	}
	if stack := account.Slots["0:362"]; stack.ItemID != 10099773 || stack.Count != 2 {
		t.Fatalf("soul stack=%+v want 10099773x2 at slot 362", stack)
	}
	assertCurrentDisjointInventoryAmount(t, *inventory, 3166, 2)
	if len(rows) != 3 || rows[0].Slot > rows[1].Slot || rows[1].Slot > rows[2].Slot {
		t.Fatalf("ack rows not slot-sorted: %+v", rows)
	}

	// Repeat grants merge into the same fixed slot stack.
	if _, dirty, err = currentGrantDisjointRewards(inventory, account, catalog, []currentDisjointReward{{ItemID: 3037, Count: 5}}); err != nil || !dirty {
		t.Fatalf("regrant dirty=%t err=%v", dirty, err)
	}
	if stack := account.Slots["0:358"]; stack.ItemID != 3037 || stack.Count != 15 {
		t.Fatalf("merged cube stack=%+v want 3037x15", stack)
	}

	// A foreign item squatting on the fixed slot is never merged into or
	// evicted; the reward overflows to the backpack.
	inventory, account = newOwners()
	account.Slots["0:358"] = dnfrepo.ItemStack{ItemID: 3166, Count: 3}
	if _, dirty, err = currentGrantDisjointRewards(inventory, account, catalog, []currentDisjointReward{{ItemID: 3037, Count: 4}}); err != nil {
		t.Fatal(err)
	} else if dirty {
		t.Fatal("squatter grant must not dirty the account warehouse")
	}
	if stack := account.Slots["0:358"]; stack.ItemID != 3166 || stack.Count != 3 {
		t.Fatalf("squatter stack evicted: %+v", stack)
	}
	assertCurrentDisjointInventoryAmount(t, *inventory, 3037, 4)
}

func TestCurrentDisjointLevelDivisorCountClampsToOne(t *testing.T) {
	// 86JP CalculateLevelDivisorCount returns Math.Max(1, count): a level-1
	// source with a large divisor must still yield one item, not fail.
	for i := 0; i < 32; i++ {
		count, err := currentDisjointLevelDivisorCount(1, 8)
		if err != nil || count != 1 {
			t.Fatalf("level=1 divisor=8 count=%d err=%v", count, err)
		}
	}
	if count, err := currentDisjointLevelDivisorCount(0, 0); err != nil || count != 1 {
		t.Fatalf("zero divisor count=%d err=%v", count, err)
	}
}

func mustCurrentDisjointTestAssets(t *testing.T, slots map[string]dnfrepo.ItemStack) (dnfrepo.Group, *pvfDungeonDropCatalog) {
	t.Helper()
	source := bridgePVFSource{
		"monster/monster.lst":                     "",
		"stackable/stackable.lst":                 "3037 `cube.stk`\n3033 `additional.stk`\n2559002 `rare_red_box.stk`\n8000 `emblem.stk`\n9000 `compound_booster.stk`\n9001 `compound_result.stk`\n",
		"equipment/equipment.lst":                 "5000 `normal_coat.equ`\n5100 `avatar_hat.equ`\n",
		"stackable/cube.stk":                      "[stackable type] `[material]`\n[stack limit] 999\n",
		"stackable/additional.stk":                "[stackable type] `[material]`\n[stack limit] 999\n",
		"stackable/rare_red_box.stk":              "[stackable type] `[booster]`\n[stack limit] 999\n",
		"stackable/emblem.stk":                    "[stackable type] `[avatar emblem]`\n[grade] 1\n[stack limit] 999\n",
		"stackable/compound_booster.stk":          "[stackable type] `[booster]`\n[booster info]\n[stackable]\n1 9001 1 1\n[/stackable]\n[/booster info]\n",
		"stackable/compound_result.stk":           "[stackable type] `[avatar emblem]`\n[grade] 2\n[stack limit] 999\n",
		"equipment/normal_coat.equ":               "[equipment type] `[coat]`\n[rarity] 0\n[minimum level] 10\n[value] 300\n",
		"equipment/avatar_hat.equ":                "[equipment type] `[hat avatar]`\n[usable job] `[fighter]`\n[grade] 2\n",
		"etc/disjoint.etc":                        "[cube index]\n`[no element]` 3037\n[/cube index]\n[cube creation const]\n150 1\n[/cube creation const]\n[additional result]\n1 3033\n[/additional result]\n[additional result const]\n10 5 0 100\n[/additional result const]\n[additional result expand]\n0 0\n[/additional result expand]\n[additional result expand const]\n1 0\n[/additional result expand const]\n[avatar disjoint info]\n[fighter]\n2559000 2559003 2559006 2559009 2559006 2559001 2559004 2559007 2559010 2559007 2559002 2559005 2559008 2559011 2559014\n[/fighter]\n[/avatar disjoint info]\n",
		"etc/emblemcompound.etc":                  "[emblem compound info]\n1 2 9000\n[/emblem compound info]\n",
		"etc/avatardisjoint/emblemlistinfo_0.etc": "[result info]\n1 9000 100 1 0\n[/result info]\n",
		"etc/avatardisjoint/emblemlistinfo_1.etc": "[result info]\n1 9000 100 1 0\n[/result info]\n",
		"etc/avatardisjoint/emblemlistinfo_2.etc": "[min max value]\n50 69\n[result info]\n1 9001 100 1 0\n[/result info]\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	repositories := mustCurrentNPCShopRepositories(t, 0, slots)
	return repositories, catalog
}

func currentDisjointTestService(repositories dnfrepo.Group, catalog *pvfDungeonDropCatalog) *Service {
	return &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		pvfItemCatalog:     catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
}

func currentDisjointItemTestBody(sourceSlot int16, listType byte, toolSlot int16, contextValue uint32) []byte {
	body := make([]byte, currentDisjointItemRequestSize+4)
	binary.LittleEndian.PutUint16(body[0:2], uint16(sourceSlot))
	body[2] = listType
	binary.LittleEndian.PutUint16(body[3:5], uint16(toolSlot))
	binary.LittleEndian.PutUint32(body[5:9], contextValue)
	return body
}

func currentEmblemCompoundTestBody(inputs []currentEmblemCompoundInput) []byte {
	body := make([]byte, 1+len(inputs)*currentEmblemCompoundInputSize)
	body[0] = byte(len(inputs))
	for index, input := range inputs {
		offset := 1 + index*currentEmblemCompoundInputSize
		binary.LittleEndian.PutUint32(body[offset:offset+4], input.ItemID)
		binary.LittleEndian.PutUint16(body[offset+4:offset+6], uint16(input.Slot))
	}
	return body
}

func assertCurrentDisjointInventoryAmount(t *testing.T, inventory dnfrepo.InventoryRecord, itemID int64, want int64) {
	t.Helper()
	var got int64
	for _, stack := range inventory.Slots {
		if stack.ItemID == itemID {
			got += stack.Count
		}
	}
	if got != want {
		t.Fatalf("item=%d count=%d want=%d inventory=%+v", itemID, got, want, inventory.Slots)
	}
}
