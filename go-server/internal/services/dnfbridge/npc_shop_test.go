package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"sort"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestParseCurrentNPCShopRequestsUseExactCurrentEXEBodies(t *testing.T) {
	shopID, err := parseCurrentNPCShopPurchaseCountRequest([]byte{0x78, 0x56, 0x34, 0x12})
	if err != nil || shopID != 0x12345678 {
		t.Fatalf("purchase-count shop=%08x err=%v", shopID, err)
	}
	if shopID, err = parseCurrentNPCShopPurchaseCountRequest(nil); err != nil || shopID != 0 {
		t.Fatalf("initial purchase-count shop=%08x err=%v", shopID, err)
	}
	if _, err = parseCurrentNPCShopPurchaseCountRequest(make([]byte, 3)); err == nil {
		t.Fatal("malformed purchase-count request accepted")
	}
	buyBody := currentNPCShopBuyTestBody(600, 3, 0x11223344, 0x55667788)
	buy, err := parseCurrentNPCShopBuyRequest(buyBody)
	if err != nil {
		t.Fatal(err)
	}
	if buy.ItemID != 600 || buy.Count != 3 || buy.ShopContext != 0x11223344 || buy.ActorContext != 0x55667788 {
		t.Fatalf("buy request=%+v", buy)
	}
	equipmentBuy, err := parseCurrentNPCShopBuyRequest(currentNPCShopBuyTestBody(700, 0, 28, 45))
	if err != nil || equipmentBuy.ItemID != 700 || equipmentBuy.Count != 0 {
		t.Fatalf("equipment buy request=%+v err=%v", equipmentBuy, err)
	}
	sellBody := currentNPCShopSellTestBody(dnfrepo.MainInventoryListType, 65, 2, 0xAABBCCDD)
	sell, err := parseCurrentNPCShopSellRequest(sellBody)
	if err != nil {
		t.Fatal(err)
	}
	if sell.ListType != dnfrepo.MainInventoryListType || sell.Slot != 65 || sell.Count != 2 || sell.ActorContext != 0xAABBCCDD {
		t.Fatalf("sell request=%+v", sell)
	}
	for _, body := range [][]byte{buyBody[:15], append(append([]byte(nil), buyBody...), 0), make([]byte, 16)} {
		if _, err := parseCurrentNPCShopBuyRequest(body); err == nil {
			t.Fatalf("malformed buy accepted: %x", body)
		}
	}
	for _, body := range [][]byte{sellBody[:10], append(append([]byte(nil), sellBody...), 0), currentNPCShopSellTestBody(1, 65, 2, 0), currentNPCShopSellTestBody(0, 0, 2, 0)} {
		if _, err := parseCurrentNPCShopSellRequest(body); err == nil {
			t.Fatalf("malformed sell accepted: %x", body)
		}
	}
	if normalized := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketBuyItem), append(append([]byte(nil), buyBody...), 1, 2, 3, 4)); !bytes.Equal(normalized, buyBody) {
		t.Fatalf("normalized buy=%x want=%x", normalized, buyBody)
	}
	if normalized := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketSellItem), append(append([]byte(nil), sellBody...), 1, 2, 3, 4)); !bytes.Equal(normalized, sellBody) {
		t.Fatalf("normalized sell=%x want=%x", normalized, sellBody)
	}
	purchaseCountBody := []byte{0x78, 0x56, 0x34, 0x12}
	if normalized := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketShopPurchaseCount), append(append([]byte(nil), purchaseCountBody...), 1, 2, 3, 4)); !bytes.Equal(normalized, purchaseCountBody) {
		t.Fatalf("normalized purchase-count=%x want=%x", normalized, purchaseCountBody)
	}
}

func TestCurrentNPCShopPurchaseCountReturnsFourEmptyCurrentEXELists(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{}
	session := &gameSession{conn: connection, connID: "npc-shop-purchase-count", selectedCharacterID: 19}
	request := []byte{0x78, 0x56, 0x34, 0x12}
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketShopPurchaseCount), request); err != nil {
		t.Fatal(err)
	}
	ack, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	wantBody := append([]byte{1}, make([]byte, currentNPCShopPurchaseCountListCount*4)...)
	if len(trailing) != 0 || ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketShopPurchaseCount) || !bytes.Equal(ack.Body, wantBody) {
		t.Fatalf("purchase-count ACK header=%+v body=%x trailing=%d", ack.Header, ack.Body, len(trailing))
	}
}

func TestCurrentNPCShopCatalogAndPricingUsePVF(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	if shop.listedShopCount != 2 || shop.loadedShopCount != 1 || shop.missingShopCount != 1 || shop.firstMissingShopPath != "stale_missing_shop.shp" {
		t.Fatalf("shop load evidence=%+v", shop)
	}
	pricing, err := resolveCurrentNPCShopPricing(shop, items, 600)
	if err != nil {
		t.Fatal(err)
	}
	if !pricing.Buyable || pricing.BuyGold != 100 || pricing.SellGold != 10 || pricing.Definition.StackLimit != 999 {
		t.Fatalf("stackable pricing=%+v", pricing)
	}
	equipment, err := resolveCurrentNPCShopPricing(shop, items, 700)
	if err != nil {
		t.Fatal(err)
	}
	if !equipment.Buyable || equipment.BuyGold != 1000 || equipment.SellGold != 100 || equipment.Definition.Durability != 20 {
		t.Fatalf("equipment pricing=%+v", equipment)
	}
	unlisted, err := resolveCurrentNPCShopPricing(shop, items, 601)
	if err != nil {
		t.Fatal(err)
	}
	if unlisted.Buyable || unlisted.BuyGold != 100 || unlisted.SellGold != 10 {
		t.Fatalf("unlisted pricing=%+v", unlisted)
	}
	material, err := resolveCurrentNPCShopPricing(shop, items, 602)
	if err != nil {
		t.Fatal(err)
	}
	if !material.Buyable || !material.MaterialExchange || material.NeedMaterialItem != 900 || material.NeedMaterialCount != 2 {
		t.Fatalf("material pricing=%+v", material)
	}
}

func TestCurrentNPCShopCatalogFallsBackToRevisedShopFile(t *testing.T) {
	source := bridgePVFSource{
		"monster/monster.lst":            "",
		"stackable/stackable.lst":        "600 `shop_potion.stk`\n",
		"equipment/equipment.lst":        "",
		"itemshop/itemshop.lst":          "15 `EquipmentShop7.shp`\n",
		"itemshop/(r)equipmentshop7.shp": "[sell info]\n[tab]\n`其他`\n[item list]\n600\n[/item list]\n[/tab]\n[/sell info]\n",
		"equipment/pricetable.tbl":       "[rate]\n200 150 30\n",
		"stackable/shop_potion.stk":      "[stackable type]\n`[waste]`\n[stack limit]\n999\n[price]\n100\n[value]\n50\n",
	}
	shop, err := newCurrentNPCShopCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	if shop.missingShopCount != 0 || shop.loadedShopCount != 1 || !currentNPCShopContainsItem(shop, 600) {
		t.Fatalf("revised shop fallback load=%+v contains600=%t", shop, currentNPCShopContainsItem(shop, 600))
	}
}

func TestCurrentNPCShopBuyCommitsGoldAndItemThenSendsIncrementalUpdate(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	repositories := mustCurrentNPCShopRepositories(t, 1000, nil)
	connection := &bufferConn{}
	service := currentNPCShopTestService(repositories, shop, items)
	session := &gameSession{conn: connection, connID: "npc-shop-buy", selectedCharacterID: 19}
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketBuyItem), currentNPCShopBuyTestBody(600, 3, 44, 88)); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyItem) {
		t.Fatalf("buy ACK header=%+v", ack.Header)
	}
	if len(ack.Body) != 1+4*4+currentItemListEntryWireSize+1 || ack.Body[0] != 1 ||
		binary.LittleEndian.Uint32(ack.Body[1:5]) != 700 || binary.LittleEndian.Uint32(ack.Body[5:9]) != 11 ||
		binary.LittleEndian.Uint32(ack.Body[9:13]) != 0 || binary.LittleEndian.Uint32(ack.Body[13:17]) != 22 ||
		binary.LittleEndian.Uint32(ack.Body[19:23]) != 600 || binary.LittleEndian.Uint32(ack.Body[23:27]) != 3 ||
		ack.Body[len(ack.Body)-1] != 0 {
		t.Fatalf("buy ACK body=%x", ack.Body)
	}
	itemSlot := int16(binary.LittleEndian.Uint16(ack.Body[17:19]))
	update, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || update.Header.Classification != 0 || update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf("buy update header=%+v trailing=%d", update.Header, len(trailing))
	}
	assertCurrentNPCShopIncrementalRows(t, update.Body, 700, itemSlot, 600, 3)

	character, found, err := repositories.Character.Load(context.Background(), "19")
	if err != nil || !found || character.Stats["gold"] != 700 {
		t.Fatalf("character=%+v found=%t err=%v", character, found, err)
	}
	inventory, found, err := repositories.Inventory.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("inventory found=%t err=%v", found, err)
	}
	stack, found := inventory.Slots[currentCeraShopInventorySlotKey(0, itemSlot)]
	if !found || stack.ItemID != 600 || stack.Count != 3 || len(stack.RawEntry) != currentItemListEntryWireSize ||
		stack.Extra["last_grant_source"] != "npc_shop" || stack.Extra["last_npc_shop_context"] != "44" || stack.Extra["last_npc_shop_actor_context"] != "88" {
		t.Fatalf("bought stack=%+v found=%t", stack, found)
	}
}

func TestCurrentNPCShopBuyNormalizesCurrentEXEEquipmentZeroCountToOne(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	repositories := mustCurrentNPCShopRepositories(t, 2000, nil)
	connection := &bufferConn{}
	service := currentNPCShopTestService(repositories, shop, items)
	session := &gameSession{conn: connection, connID: "npc-shop-equipment-zero-count", selectedCharacterID: 19}
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketBuyItem), currentNPCShopBuyTestBody(700, 0, 28, 45)); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyItem) || ack.Body[0] != 1 {
		t.Fatalf("equipment buy ACK header=%+v body=%x", ack.Header, ack.Body)
	}
	_, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 {
		t.Fatalf("equipment buy trailing=%d", len(trailing))
	}
	inventory, found, err := repositories.Inventory.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("inventory found=%t err=%v", found, err)
	}
	var bought dnfrepo.ItemStack
	for _, stack := range inventory.Slots {
		if stack.ItemID == 700 {
			bought = stack
			break
		}
	}
	if bought.ItemID != 700 || bought.Count != 1 || bought.Extra["last_npc_shop_context"] != "28" || bought.Extra["last_npc_shop_actor_context"] != "45" {
		t.Fatalf("bought equipment=%+v", bought)
	}
	character, found, err := repositories.Character.Load(context.Background(), "19")
	if err != nil || !found || character.Stats["gold"] != 1000 {
		t.Fatalf("character=%+v found=%t err=%v", character, found, err)
	}
}

func TestCurrentNPCShopSellUsesPVFPriceNotForgedMetadataAndSendsIncrementalUpdate(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	stack := dnfrepo.ItemStack{ItemID: 600, Count: 5, Extra: map[string]string{"sell_gold": "999999999"}}
	entry := currentItemListEntryFromStack(0, 65, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)
	repositories := mustCurrentNPCShopRepositories(t, 100, map[string]dnfrepo.ItemStack{"0:65": stack})
	connection := &bufferConn{}
	service := currentNPCShopTestService(repositories, shop, items)
	if err := service.handleCurrentNPCShopSell(&gameSession{conn: connection, selectedCharacterID: 19}, currentNPCShopSellTestBody(0, 65, 2, 88)); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != uint16(dnfenum.CmdPacketSellItem) ||
		len(ack.Body) != 12 || ack.Body[0] != 1 || binary.LittleEndian.Uint32(ack.Body[1:5]) != 120 ||
		ack.Body[5] != 0 || binary.LittleEndian.Uint16(ack.Body[6:8]) != 65 || binary.LittleEndian.Uint32(ack.Body[8:12]) != 2 {
		t.Fatalf("sell ACK header=%+v body=%x", ack.Header, ack.Body)
	}
	update, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || update.Header.Classification != 0 || update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf("sell update header=%+v trailing=%d", update.Header, len(trailing))
	}
	assertCurrentNPCShopIncrementalRows(t, update.Body, 120, 65, 600, 3)
	character, found, err := repositories.Character.Load(context.Background(), "19")
	if err != nil || !found || character.Stats["gold"] != 120 {
		t.Fatalf("character=%+v found=%t err=%v", character, found, err)
	}
	inventory, found, err := repositories.Inventory.Load(context.Background(), "19")
	remaining := inventory.Slots["0:65"]
	if err != nil || !found || remaining.Count != 3 || binary.LittleEndian.Uint32(remaining.RawEntry[6:10]) != 3 {
		t.Fatalf("inventory=%+v remaining=%+v found=%t err=%v", inventory, remaining, found, err)
	}
}

func TestCurrentNPCShopSellAllUsesCurrentEXEMinusOneDeletionRow(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	stack := dnfrepo.ItemStack{ItemID: 600, Count: 2}
	repositories := mustCurrentNPCShopRepositories(t, 100, map[string]dnfrepo.ItemStack{"0:65": stack})
	connection := &bufferConn{}
	service := currentNPCShopTestService(repositories, shop, items)
	if err := service.handleCurrentNPCShopSell(&gameSession{conn: connection, selectedCharacterID: 19}, currentNPCShopSellTestBody(0, 65, 2, 0)); err != nil {
		t.Fatal(err)
	}
	_, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	update, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || len(update.Body) != 3+2*currentItemListEntryWireSize {
		t.Fatalf("deletion update len=%d trailing=%d", len(update.Body), len(trailing))
	}
	second := update.Body[3+currentItemListEntryWireSize:]
	if binary.LittleEndian.Uint16(second[0:2]) != 65 || binary.LittleEndian.Uint32(second[2:6]) != math.MaxUint32 || binary.LittleEndian.Uint32(second[6:10]) != 0 {
		t.Fatalf("deletion row=%x", second)
	}
	inventory, found, err := repositories.Inventory.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("inventory found=%t err=%v", found, err)
	}
	if _, exists := inventory.Slots["0:65"]; exists {
		t.Fatalf("sold row remains: %+v", inventory.Slots["0:65"])
	}
}

func TestCurrentNPCShopBuyFailuresDoNotMutateGoldOrInventory(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	for _, test := range []struct {
		name   string
		gold   int64
		itemID uint32
		count  uint32
		slots  map[string]dnfrepo.ItemStack
	}{
		{name: "insufficient gold", gold: 200, itemID: 600, count: 3},
		{name: "zero stackable count", gold: 1000, itemID: 600, count: 0},
		{name: "forged unlisted item", gold: 1000, itemID: 601, count: 1},
		{name: "material exchange unsupported", gold: 1000, itemID: 602, count: 1},
		{name: "inventory full", gold: 1000, itemID: 600, count: 1, slots: fullCurrentNPCShopWasteInventory()},
	} {
		t.Run(test.name, func(t *testing.T) {
			repositories := mustCurrentNPCShopRepositories(t, test.gold, test.slots)
			connection := &bufferConn{}
			service := currentNPCShopTestService(repositories, shop, items)
			if err := service.handleCurrentNPCShopBuy(&gameSession{conn: connection, selectedCharacterID: 19}, currentNPCShopBuyTestBody(test.itemID, test.count, 0, 0)); err != nil {
				t.Fatal(err)
			}
			failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
			if len(trailing) != 0 || failure.Header.MsgID != uint16(dnfenum.CmdPacketBuyItem) ||
				failure.Header.Classification != dnfproto.DefaultChannelClassification || !bytes.Equal(failure.Body, []byte{0, currentNPCShopBuyErrorCode}) {
				t.Fatalf("failure header=%+v body=%x trailing=%d", failure.Header, failure.Body, len(trailing))
			}
			character, found, err := repositories.Character.Load(context.Background(), "19")
			if err != nil || !found || character.Stats["gold"] != test.gold {
				t.Fatalf("character mutated=%+v found=%t err=%v", character, found, err)
			}
			inventory, found, err := repositories.Inventory.Load(context.Background(), "19")
			if err != nil || !found || !equalCurrentNPCShopSlots(inventory.Slots, test.slots) {
				t.Fatalf("inventory mutated=%+v found=%t err=%v", inventory.Slots, found, err)
			}
		})
	}
}

func TestCurrentNPCShopSellLockedOrOverflowDoesNotMutate(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	for _, test := range []struct {
		name  string
		gold  int64
		stack dnfrepo.ItemStack
	}{
		{name: "locked", gold: 100, stack: dnfrepo.ItemStack{ItemID: 600, Count: 2, Extra: map[string]string{"equipment_lock_state": "locked"}}},
		{name: "gold overflow", gold: math.MaxInt32, stack: dnfrepo.ItemStack{ItemID: 600, Count: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repositories := mustCurrentNPCShopRepositories(t, test.gold, map[string]dnfrepo.ItemStack{"0:65": test.stack})
			connection := &bufferConn{}
			service := currentNPCShopTestService(repositories, shop, items)
			if err := service.handleCurrentNPCShopSell(&gameSession{conn: connection, selectedCharacterID: 19}, currentNPCShopSellTestBody(0, 65, 1, 0)); err != nil {
				t.Fatal(err)
			}
			failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
			if len(trailing) != 0 || failure.Header.MsgID != uint16(dnfenum.CmdPacketSellItem) || !bytes.Equal(failure.Body, []byte{0, currentNPCShopSellErrorCode}) {
				t.Fatalf("failure header=%+v body=%x trailing=%d", failure.Header, failure.Body, len(trailing))
			}
			character, _, _ := repositories.Character.Load(context.Background(), "19")
			inventory, _, _ := repositories.Inventory.Load(context.Background(), "19")
			if character.Stats["gold"] != test.gold || inventory.Slots["0:65"].Count != test.stack.Count {
				t.Fatalf("mutation character=%+v inventory=%+v", character, inventory)
			}
		})
	}
}

func TestCurrentNPCShopBuyCommitFailureRollsBackWalletAndInventory(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	repositories := mustCurrentNPCShopRepositories(t, 1000, nil)
	repositories.AccountAssets = rejectingCurrentNPCShopCommit{
		inner: repositories.AccountAssets,
		err:   errors.New("injected NPC shop commit failure"),
	}
	connection := &bufferConn{}
	service := currentNPCShopTestService(repositories, shop, items)
	if err := service.handleCurrentNPCShopBuy(&gameSession{conn: connection, selectedCharacterID: 19}, currentNPCShopBuyTestBody(600, 2, 0, 0)); err != nil {
		t.Fatal(err)
	}
	failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || !bytes.Equal(failure.Body, []byte{0, currentNPCShopBuyErrorCode}) {
		t.Fatalf("failure body=%x trailing=%d", failure.Body, len(trailing))
	}
	character, found, err := repositories.Character.Load(context.Background(), "19")
	if err != nil || !found || character.Stats["gold"] != 1000 {
		t.Fatalf("character=%+v found=%t err=%v", character, found, err)
	}
	inventory, found, err := repositories.Inventory.Load(context.Background(), "19")
	if err != nil || !found || len(inventory.Slots) != 0 {
		t.Fatalf("inventory=%+v found=%t err=%v", inventory, found, err)
	}
}

func TestRealPVFCurrentNPCShopCatalogAndPrice(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to the runtime Script.pvf")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	shop, err := newCurrentNPCShopCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("real NPC shop catalog: listed=%d loaded=%d missing=%d first_missing=%q buyable_items=%d", shop.listedShopCount, shop.loadedShopCount, shop.missingShopCount, shop.firstMissingShopPath, len(shop.buyableItems))
	items, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uint32, 0, len(shop.buyableItems))
	for itemID := range shop.buyableItems {
		ids = append(ids, itemID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, itemID := range ids {
		pricing, priceErr := resolveCurrentNPCShopPricing(shop, items, itemID)
		if priceErr == nil && pricing.Buyable && pricing.BuyGold > 0 && pricing.SellGold >= 0 {
			return
		}
	}
	t.Fatalf("no positive-price real NPC shop item resolved from %d listed items", len(ids))
}

func TestRealPVFCurrentNPCShopObservedEquipmentMaterialExchange(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to the runtime Script.pvf")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	shop, err := newCurrentNPCShopCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	items, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	const itemID = uint32(116010117)
	pricing, err := resolveCurrentNPCShopPricing(shop, items, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if !pricing.Buyable || !pricing.MaterialExchange || pricing.Definition.Kind != dungeonDropItemEquipment || pricing.NeedMaterialItem == 0 || pricing.NeedMaterialCount <= 0 {
		t.Fatalf("observed shop equipment pricing=%+v", pricing)
	}
	t.Logf("item=%d path=%s material=%d x%d", itemID, pricing.Definition.PVFPath, pricing.NeedMaterialItem, pricing.NeedMaterialCount)
}

func TestCurrentNPCShopBuyMaterialExchangeConsumesCharacterStack(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	repositories := mustCurrentNPCShopRepositories(t, 1000, map[string]dnfrepo.ItemStack{"0:121": {ItemID: 900, Count: 10}})
	connection := &bufferConn{}
	service := currentNPCShopTestService(repositories, shop, items)
	session := &gameSession{conn: connection, connID: "npc-shop-exchange", selectedCharacterID: 19}
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketBuyItem), currentNPCShopBuyTestBody(602, 3, 15, 2)); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyItem) || ack.Body[0] != 1 {
		t.Fatalf("exchange ACK header=%+v body=%x", ack.Header, ack.Body)
	}
	if len(ack.Body) != 1+4*4+currentItemListEntryWireSize+1+8 ||
		binary.LittleEndian.Uint32(ack.Body[1:5]) != 1000 || // gold untouched
		ack.Body[len(ack.Body)-9] != 1 || binary.LittleEndian.Uint32(ack.Body[len(ack.Body)-8:]) != 900 || binary.LittleEndian.Uint32(ack.Body[len(ack.Body)-4:]) != 4 {
		t.Fatalf("exchange ACK cost rows=%x", ack.Body)
	}
	_, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 {
		t.Fatalf("exchange trailing packets=%d", len(trailing))
	}
	character, _, _ := repositories.Character.Load(context.Background(), "19")
	if character.Stats["gold"] != 1000 {
		t.Fatalf("gold after exchange=%d want unchanged 1000", character.Stats["gold"])
	}
	inventory, _, _ := repositories.Inventory.Load(context.Background(), "19")
	if stack := inventory.Slots["0:121"]; stack.ItemID != 900 || stack.Count != 4 {
		t.Fatalf("material stack=%+v want 900x4", stack)
	}
	assertCurrentNPCShopInventoryAmount(t, inventory, 602, 3)
}

func TestCurrentNPCShopBuyEquipmentMaterialExchangeConsumesMaterialAndGrantsOne(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	repositories := mustCurrentNPCShopRepositories(t, 1000, map[string]dnfrepo.ItemStack{"0:121": {ItemID: 900, Count: 10}})
	connection := &bufferConn{}
	service := currentNPCShopTestService(repositories, shop, items)
	session := &gameSession{conn: connection, connID: "npc-shop-equipment-exchange", selectedCharacterID: 19}
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketBuyItem), currentNPCShopBuyTestBody(701, 0, 28, 45)); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyItem) || ack.Body[0] != 1 ||
		ack.Body[len(ack.Body)-9] != 1 || binary.LittleEndian.Uint32(ack.Body[len(ack.Body)-8:]) != 900 || binary.LittleEndian.Uint32(ack.Body[len(ack.Body)-4:]) != 8 {
		t.Fatalf("equipment exchange ACK=%x", ack.Body)
	}
	_, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 {
		t.Fatalf("equipment exchange trailing=%d", len(trailing))
	}
	inventory, found, err := repositories.Inventory.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("inventory found=%t err=%v", found, err)
	}
	if stack := inventory.Slots["0:121"]; stack.ItemID != 900 || stack.Count != 8 {
		t.Fatalf("material stack=%+v want 900x8", stack)
	}
	assertCurrentNPCShopInventoryAmount(t, inventory, 701, 1)
	for _, stack := range inventory.Slots {
		if stack.ItemID == 701 && (stack.Extra["last_npc_shop_context"] != "28" || stack.Extra["last_npc_shop_actor_context"] != "45") {
			t.Fatalf("equipment exchange metadata=%+v", stack.Extra)
		}
	}
}

func TestCurrentNPCShopBuyEquipmentMaterialExchangeInsufficientMaterialDoesNotMutate(t *testing.T) {
	shop, items := mustCurrentNPCShopTestCatalogs(t)
	wantSlots := map[string]dnfrepo.ItemStack{"0:121": {ItemID: 900, Count: 1}}
	repositories := mustCurrentNPCShopRepositories(t, 1000, wantSlots)
	connection := &bufferConn{}
	service := currentNPCShopTestService(repositories, shop, items)
	session := &gameSession{conn: connection, connID: "npc-shop-equipment-exchange-insufficient", selectedCharacterID: 19}
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketBuyItem), currentNPCShopBuyTestBody(701, 0, 28, 45)); err != nil {
		t.Fatal(err)
	}

	failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || !bytes.Equal(failure.Body, []byte{0, currentNPCShopBuyErrorCode}) {
		t.Fatalf("failure body=%x trailing=%d", failure.Body, len(trailing))
	}
	inventory, found, err := repositories.Inventory.Load(context.Background(), "19")
	if err != nil || !found || !equalCurrentNPCShopSlots(inventory.Slots, wantSlots) {
		t.Fatalf("inventory=%+v found=%t err=%v", inventory, found, err)
	}
	assertCurrentNPCShopInventoryAmount(t, inventory, 701, 0)
}

func TestCurrentNPCShopBuyCubeFragmentExchangeUsesAccountWarehouse(t *testing.T) {
	source := bridgePVFSource{
		"monster/monster.lst":            "",
		"stackable/stackable.lst":        "3033 `cube_black.stk`\n3037 `cube_clear.stk`\n",
		"equipment/equipment.lst":        "",
		"itemshop/itemshop.lst":          "15 `EquipmentShop7.shp`\n",
		"itemshop/(r)equipmentshop7.shp": "[sell info]\n[tab]\n`其他`\n[item list]\n3033\n[/item list]\n[/tab]\n[/sell info]\n",
		"equipment/pricetable.tbl":       "[rate]\n200 150 30\n",
		"stackable/cube_black.stk":       "[stackable type]\n`[material]`\n[stack limit]\n999\n[value]\n40\n[need material]\n3037 5\n",
		"stackable/cube_clear.stk":       "[stackable type]\n`[material]`\n[stack limit]\n999\n[value]\n40\n",
	}
	shop, err := newCurrentNPCShopCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	items, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	repositories := mustCurrentNPCShopRepositories(t, 1000, nil)
	if err := repositories.AccountInventory.Save(context.Background(), dnfrepo.AccountInventoryRecord{
		AccountID: "account-1",
		Slots:     map[string]dnfrepo.ItemStack{"0:358": {ItemID: 3037, Count: 10}},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentNPCShopTestService(repositories, shop, items)
	session := &gameSession{conn: connection, connID: "npc-shop-cube-exchange", selectedCharacterID: 19}
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketBuyItem), currentNPCShopBuyTestBody(3033, 2, 15, 2)); err != nil {
		t.Fatal(err)
	}

	ack, _ := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyItem) || ack.Body[0] != 1 ||
		ack.Body[len(ack.Body)-9] != 1 || binary.LittleEndian.Uint32(ack.Body[len(ack.Body)-8:]) != 3037 || binary.LittleEndian.Uint32(ack.Body[len(ack.Body)-4:]) != 0 {
		t.Fatalf("cube exchange ACK=%x", ack.Body)
	}
	account, found, err := repositories.AccountInventory.Load(context.Background(), "account-1")
	if err != nil || !found {
		t.Fatalf("account inventory found=%t err=%v", found, err)
	}
	if stack := account.Slots["0:358"]; stack.ItemID != 3037 || stack.Count != 0 {
		t.Fatalf("colorless cube cell=%+v want 3037x0", stack)
	}
	if stack := account.Slots["0:354"]; stack.ItemID != 3033 || stack.Count != 2 {
		t.Fatalf("black cube cell=%+v want 3033x2", stack)
	}
	character, _, _ := repositories.Character.Load(context.Background(), "19")
	if character.Stats["gold"] != 1000 {
		t.Fatalf("gold after cube exchange=%d want unchanged 1000", character.Stats["gold"])
	}
}

func assertCurrentNPCShopInventoryAmount(t *testing.T, inventory dnfrepo.InventoryRecord, itemID int64, want int64) {
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

func mustCurrentNPCShopTestCatalogs(t *testing.T) (*currentNPCShopCatalog, *pvfDungeonDropCatalog) {
	t.Helper()
	source := bridgePVFSource{
		"monster/monster.lst":             "",
		"stackable/stackable.lst":         "600 `shop_potion.stk`\n601 `unlisted_potion.stk`\n602 `material_exchange.stk`\n",
		"equipment/equipment.lst":         "700 `shop_sword.equ`\n701 `material_sword.equ`\n",
		"itemshop/itemshop.lst":           "1 `test_shop.shp`\n2 `stale_missing_shop.shp`\n",
		"itemshop/test_shop.shp":          "[item list]\n600 700 701 602\n[/item list]\n",
		"equipment/pricetable.tbl":        "[rate]\n200 150 30\n[repair cost]\n0.08\n",
		"stackable/shop_potion.stk":       "[stackable type]\n`[waste]`\n[stack limit]\n999\n[price]\n100\n[value]\n50\n",
		"stackable/unlisted_potion.stk":   "[stackable type]\n`[waste]`\n[stack limit]\n999\n[price]\n100\n[value]\n50\n",
		"stackable/material_exchange.stk": "[stackable type]\n`[material]`\n[stack limit]\n999\n[price]\n100\n[value]\n50\n[need material]\n900 2\n",
		"equipment/shop_sword.equ":        "[price]\n1000\n[value]\n500\n[durability]\n20\n",
		"equipment/material_sword.equ":    "[price]\n1000\n[value]\n500\n[durability]\n20\n[need material]\n900 2\n",
	}
	shop, err := newCurrentNPCShopCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	items, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	return shop, items
}

func mustCurrentNPCShopRepositories(t *testing.T, gold int64, slots map[string]dnfrepo.ItemStack) dnfrepo.Group {
	t.Helper()
	repositories := dnfrepomemory.NewMemoryGroup()
	if slots == nil {
		slots = map[string]dnfrepo.ItemStack{}
	}
	ctx := context.Background()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{"account_cera": "22"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Stats:       map[string]int64{"gold": gold, "sp": 11, "cera": 22},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: dnfrepo.CloneInventory(dnfrepo.InventoryRecord{Slots: slots}).Slots}); err != nil {
		t.Fatal(err)
	}
	return repositories
}

func currentNPCShopTestService(repositories dnfrepo.Group, shop *currentNPCShopCatalog, items *pvfDungeonDropCatalog) *Service {
	return &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		npcShopCatalog:     shop,
		pvfItemCatalog:     items,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
}

func currentNPCShopBuyTestBody(itemID uint32, count uint32, shopContext uint32, actorContext uint32) []byte {
	body := make([]byte, currentNPCShopBuyRequestWireSize)
	binary.LittleEndian.PutUint32(body[0:4], itemID)
	binary.LittleEndian.PutUint32(body[4:8], count)
	binary.LittleEndian.PutUint32(body[8:12], shopContext)
	binary.LittleEndian.PutUint32(body[12:16], actorContext)
	return body
}

func currentNPCShopSellTestBody(listType byte, slot int16, count uint32, actorContext uint32) []byte {
	body := make([]byte, currentNPCShopSellRequestWireSize)
	body[0] = listType
	binary.LittleEndian.PutUint16(body[1:3], uint16(slot))
	binary.LittleEndian.PutUint32(body[3:7], count)
	binary.LittleEndian.PutUint32(body[7:11], actorContext)
	return body
}

func assertCurrentNPCShopIncrementalRows(t *testing.T, body []byte, gold uint32, itemSlot int16, itemID uint32, count uint32) {
	t.Helper()
	if len(body) != 3+2*currentItemListEntryWireSize || body[0] != 0 || binary.LittleEndian.Uint16(body[1:3]) != 2 {
		t.Fatalf("incremental body header=%x len=%d", body, len(body))
	}
	wallet := body[3 : 3+currentItemListEntryWireSize]
	item := body[3+currentItemListEntryWireSize:]
	if binary.LittleEndian.Uint16(wallet[0:2]) != 0 || binary.LittleEndian.Uint32(wallet[2:6]) != 0 || binary.LittleEndian.Uint32(wallet[6:10]) != gold {
		t.Fatalf("wallet row=%x", wallet)
	}
	if int16(binary.LittleEndian.Uint16(item[0:2])) != itemSlot || binary.LittleEndian.Uint32(item[2:6]) != itemID || binary.LittleEndian.Uint32(item[6:10]) != count {
		t.Fatalf("item row=%x", item)
	}
}

func fullCurrentNPCShopWasteInventory() map[string]dnfrepo.ItemStack {
	slots := make(map[string]dnfrepo.ItemStack)
	for slot := int16(currentDungeonPickupQuickSlotStart); slot <= currentDungeonPickupQuickSlotEnd; slot++ {
		slots[currentCeraShopInventorySlotKey(0, slot)] = dnfrepo.ItemStack{ItemID: 999, Count: 1}
	}
	for slot := int16(65); slot <= 120; slot++ {
		slots[currentCeraShopInventorySlotKey(0, slot)] = dnfrepo.ItemStack{ItemID: 999, Count: 1}
	}
	return slots
}

func equalCurrentNPCShopSlots(left map[string]dnfrepo.ItemStack, right map[string]dnfrepo.ItemStack) bool {
	if len(left) != len(right) {
		return false
	}
	for key, want := range right {
		got, found := left[key]
		if !found || got.ItemID != want.ItemID || got.Count != want.Count || got.Bind != want.Bind {
			return false
		}
	}
	return true
}

type rejectingCurrentNPCShopCommit struct {
	inner dnfrepo.AccountCharacterAssetUnitOfWork
	err   error
}

func (u rejectingCurrentNPCShopCommit) WithinAccountCharacterAssets(
	ctx context.Context,
	accountID string,
	characterID string,
	apply func(dnfrepo.AccountInventoryRepository, dnfrepo.CharacterRepository, dnfrepo.InventoryRepository, dnfrepo.EquipmentRepository) error,
) error {
	return u.inner.WithinAccountCharacterAssets(ctx, accountID, characterID, func(accounts dnfrepo.AccountInventoryRepository, characters dnfrepo.CharacterRepository, inventory dnfrepo.InventoryRepository, equipment dnfrepo.EquipmentRepository) error {
		if err := apply(accounts, characters, inventory, equipment); err != nil {
			return err
		}
		return u.err
	})
}
