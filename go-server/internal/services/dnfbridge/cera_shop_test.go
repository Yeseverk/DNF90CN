package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func seedCeraShopAccountCera(t *testing.T, ctx context.Context, repositories dnfrepo.Group, accountID string, balance int64) {
	t.Helper()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: accountID,
		Metadata:  map[string]string{"account_cera": strconv.FormatInt(balance, 10)},
	}); err != nil {
		t.Fatal(err)
	}
}

func loadCeraShopAccountCera(t *testing.T, ctx context.Context, repositories dnfrepo.Group, accountID string) int64 {
	t.Helper()
	account, found, err := repositories.Account.Load(ctx, accountID)
	if err != nil || !found {
		t.Fatalf("load account found=%t err=%v", found, err)
	}
	return currentAccountCera(account)
}

func TestCurrentCeraShopPurchaseLegacyRouteCommitsPVFProductAndSendsCurrentPackets(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 200)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Stats:       map[string]int64{"cera": 200},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "cera-shop-test", selectedCharacterID: 19}

	// The live current client sent this exact 19-byte cart shape through the
	// legacy game decoder (class1/op64): count=1, Cera payment mode, FF FF row
	// fields, and the commodity little-endian at offset seven.  Use a supported
	// fixture commodity here to prove the legacy route reaches the real owner.
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketBuyCerashopItem),
		currentCeraShopTestRequestBody(100050),
	); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) {
		t.Fatalf("purchase ACK header=%+v", ack.Header)
	}
	if want := buildCurrentCeraShopPurchaseSuccessBodyWithCount(100050, 3); !bytes.Equal(ack.Body, want) {
		t.Fatalf("purchase ACK body=%x want=%x", ack.Body, want)
	}
	itemUpdate, rest := splitGameServerUpperPacket(t, rest)
	if itemUpdate.Header.Classification != 0 || itemUpdate.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf("item update header=%+v", itemUpdate.Header)
	}
	if len(itemUpdate.Body) != 3+currentItemListEntryWireSize || itemUpdate.Body[0] != dnfrepo.MainInventoryListType ||
		binary.LittleEndian.Uint16(itemUpdate.Body[1:3]) != 1 || binary.LittleEndian.Uint16(itemUpdate.Body[3:5]) != 3 ||
		binary.LittleEndian.Uint32(itemUpdate.Body[5:9]) != 37 || binary.LittleEndian.Uint32(itemUpdate.Body[9:13]) != 3 ||
		binary.LittleEndian.Uint16(itemUpdate.Body[3+0x0B:3+0x0D]) != 0xFFFF ||
		binary.LittleEndian.Uint32(itemUpdate.Body[3+0x6E:3+0x72]) != 0 {
		t.Fatalf("item update body=%x", itemUpdate.Body)
	}
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || balance.Header.Classification != 0 || balance.Header.MsgID != currentCeraShopUpdateMsgID {
		t.Fatalf("balance header=%+v trailing=%d", balance.Header, len(trailing))
	}
	if want := buildCurrentCeraShopBalanceBody(120); !bytes.Equal(balance.Body, want) {
		t.Fatalf("balance body=%x want=%x", balance.Body, want)
	}

	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != 120 {
		t.Fatalf("persisted account cera=%d, want 120", got)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load persisted inventory found=%t err=%v", found, err)
	}
	stack, found := inventory.Slots["0:3"]
	if !found || stack.ItemID != 37 || stack.Count != 3 || len(stack.RawEntry) != currentItemListEntryWireSize || stack.Extra["last_grant_source"] != "cera_shop" ||
		!stack.ExpireAt.Equal(time.Date(2028, time.August, 15, 22, 0, 0, 0, time.UTC)) ||
		stack.Extra["expire_time"] != "1849989600" || stack.Extra["expire_unix"] != "1849989600" ||
		binary.LittleEndian.Uint16(stack.RawEntry[0x0B:0x0D]) != 0xFFFF || binary.LittleEndian.Uint32(stack.RawEntry[0x6E:0x72]) != 0 {
		t.Fatalf("persisted item=%+v found=%t", stack, found)
	}
}

func TestCurrentCeraShopPurchaseRejectsUnknownCommodityWithoutMutation(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 200)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1", Stats: map[string]int64{"cera": 200}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.handleCurrentCeraShopPurchase(&gameSession{conn: connection, selectedCharacterID: 19}, currentCeraShopTestRequestBody(999999)); err != nil {
		t.Fatal(err)
	}
	failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || failure.Header.Classification != dnfproto.DefaultChannelClassification ||
		failure.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) ||
		!bytes.Equal(failure.Body, buildCurrentCeraShopPurchaseFailureBody()) {
		t.Fatalf("failure packet=%+v body=%x trailing=%d", failure.Header, failure.Body, len(trailing))
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != 200 {
		t.Fatalf("cera mutated: account cera=%d, want 200", got)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found || len(inventory.Slots) != 0 {
		t.Fatalf("inventory mutated inventory=%+v found=%t err=%v", inventory, found, err)
	}
}

func TestCurrentCeraShopPurchaseRoutesPVFFeedToPetConsumables(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-pet-feed", 100)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "23",
		AccountID:   "account-pet-feed",
		Stats:       map[string]int64{"cera": 100},
	}); err != nil {
		t.Fatal(err)
	}
	// Keep an already misplaced same-template row in list 0.  A correct
	// purchase must neither merge into it nor select another main-bag slot.
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "23",
		Slots: map[string]dnfrepo.ItemStack{
			"0:65": {
				ItemID: 42,
				Count:  6,
				Extra: map[string]string{
					"item_kind":      string(dungeonDropItemStackable),
					"stackable_type": "[feed]",
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-pet-feed"},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	request, err := parseCurrentCeraShopPurchaseRequest(currentCeraShopTestRequestBody(100055))
	if err != nil {
		t.Fatal(err)
	}
	for purchaseIndex := 0; purchaseIndex < 2; purchaseIndex++ {
		result, err := service.commitCurrentCeraShopPurchase(ctx, &gameSession{selectedCharacterID: 23}, catalog, request)
		if err != nil {
			t.Fatalf("commit pet-feed purchase %d: %v", purchaseIndex+1, err)
		}
		if !result.PetInventoryChanged {
			t.Fatalf("purchase %d did not request a pet-container refresh: %+v", purchaseIndex+1, result)
		}
	}

	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-pet-feed"); got != 60 {
		t.Fatalf("account cera after pet-feed purchases=%d, want 60", got)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "23")
	if err != nil || !found {
		t.Fatalf("load pet-feed inventory found=%t err=%v", found, err)
	}
	if main := inventory.Slots["0:65"]; main.ItemID != 42 || main.Count != 6 {
		t.Fatalf("misplaced pre-existing main row was merged or mutated: %+v", main)
	}
	feed, found := inventory.Slots["7:189"]
	if !found || feed.ItemID != 42 || feed.Count != 20 || feed.Extra["stackable_type"] != "[feed]" ||
		len(feed.RawEntry) != currentItemListEntryWireSize ||
		binary.LittleEndian.Uint16(feed.RawEntry[0:2]) != 189 ||
		binary.LittleEndian.Uint32(feed.RawEntry[2:6]) != 42 ||
		binary.LittleEndian.Uint32(feed.RawEntry[6:10]) != 20 {
		t.Fatalf("pet-consumable row=%+v found=%t raw=%x", feed, found, feed.RawEntry)
	}
	if len(inventory.Slots) != 2 {
		t.Fatalf("unexpected extra rows after pet-feed purchases: %+v", inventory.Slots)
	}
	if isCurrentCeraShopPetConsumable(dungeonDropItemDefinition{Kind: dungeonDropItemStackable, StackableType: "[waste]"}) {
		t.Fatal("ordinary stackable was classified as a pet consumable")
	}
}

func TestCurrentCeraShopPurchasePersistsUsablePeriodWithoutMergingLegacyRow(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-timed-item", 100)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "24",
		AccountID:   "account-timed-item",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "24",
		Slots: map[string]dnfrepo.ItemStack{
			"0:65": {ItemID: 43, Count: 1, Extra: map[string]string{"item_kind": "stackable"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-timed-item"},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	request, err := parseCurrentCeraShopPurchaseRequest(currentCeraShopTestRequestBody(100056))
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	result, err := service.commitCurrentCeraShopPurchase(ctx, &gameSession{selectedCharacterID: 24}, catalog, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.CeraAfter != 80 || result.PetInventoryChanged || len(result.Updates) != 1 || result.Updates[0].ListType != dnfrepo.MainInventoryListType {
		t.Fatalf("commit result=%+v", result)
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-timed-item"); got != 80 {
		t.Fatalf("account cera=%d want=80", got)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "24")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	// C# 86JP TryMergeStack merges same-ItemID stacks regardless of expiration.
	// The legacy row at 0:65 absorbs the purchase and gets the PVF expiration applied.
	merged := inventory.Slots["0:65"]
	minExpire := time.Unix(before.Unix()+10*86400, 0).UTC()
	maxExpire := time.Unix(time.Now().UTC().Unix()+10*86400, 0).UTC()
	if merged.Count != 2 || merged.ItemID != 43 || merged.ExpireAt.Before(minExpire) || merged.ExpireAt.After(maxExpire) {
		t.Fatalf("merged row=%+v want count=2 with expiration", merged)
	}
	if _, hasOldSlot := inventory.Slots["0:66"]; hasOldSlot {
		t.Fatalf("expected merge into 0:65, but 0:66 also exists: %+v", inventory.Slots["0:66"])
	}
}

func TestCurrentCeraShopPurchaseMaterializesVisualDurationAtCheckout(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-visual", 100)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "25", AccountID: "account-visual"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "25", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-visual"},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	request, err := parseCurrentCeraShopPurchaseRequest(currentCeraShopTestRequestBody(100057))
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	result, err := service.commitCurrentCeraShopPurchase(ctx, &gameSession{selectedCharacterID: 25}, catalog, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.CeraAfter != 40 || len(result.Updates) != 1 || result.Updates[0].ListType != dnfrepo.MainInventoryListType {
		t.Fatalf("result=%+v", result)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "25")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	stack, found := inventory.Slots["0:9"]
	minExpire := time.Unix(before.Unix()+30*86400, 0).UTC()
	maxExpire := time.Unix(time.Now().UTC().Unix()+30*86400, 0).UTC()
	if !found || stack.ItemID != 9002 || stack.Count != 1 || stack.ExpireAt.Before(minExpire) || stack.ExpireAt.After(maxExpire) ||
		stack.Extra["cera_shop_duration_days"] != "30" || stack.Extra["expiration_source"] != "runtime_pvf_cera_shop_duration_grant" ||
		stack.Extra["last_cera_shop_commodity"] != "100057" || len(stack.RawEntry) != currentItemListEntryWireSize ||
		binary.LittleEndian.Uint32(stack.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != uint32(stack.ExpireAt.Unix()) ||
		binary.LittleEndian.Uint32(stack.RawEntry[legacyWrongCurrentItemListExpireTimeOffset:legacyWrongCurrentItemListExpireTimeOffset+4]) != 0 {
		t.Fatalf("visual row=%+v found=%t raw=%x", stack, found, stack.RawEntry)
	}
}

func TestCurrentCeraShopPurchaseConsumesPVFAccountCargoUpgrade(t *testing.T) {
	ctx := context.Background()
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-cargo-1",
		Metadata: map[string]string{
			"account_cargo_created": "true",
			"account_cargo_level":   "1",
			"account_cargo_gold":    "0",
			"account_cera":          "5000",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "29", AccountID: "account-cargo-1", Stats: map[string]int64{"cera": 5000}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "29", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-cargo-1"},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	request, err := parseCurrentCeraShopPurchaseRequest(currentCeraShopTestRequestBody(100083))
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.commitCurrentCeraShopPurchase(ctx, &gameSession{selectedCharacterID: 29}, catalog, request)
	if err != nil {
		t.Fatalf("commit account cargo purchase: %v", err)
	}
	if got.AccountCargoSelectionKey != 8 || got.CeraAfter != 3000 || len(got.Updates) != 0 {
		t.Fatalf("commit result=%+v", got)
	}
	account, found, err := repositories.Account.Load(ctx, "account-cargo-1")
	if err != nil || !found || account.Metadata["account_cargo_level"] != "8" {
		t.Fatalf("account after purchase=%+v found=%t err=%v", account, found, err)
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-cargo-1"); got != 3000 {
		t.Fatalf("account cera after purchase=%d, want 3000", got)
	}
}

func TestCurrentCeraShopPurchaseStoresAvatarInAvatarContainerAndUpdatesThatContainer(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 40)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "20",
		AccountID:   "account-1",
		Stats:       map[string]int64{"cera": 40},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "20", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.handleCurrentCeraShopPurchase(&gameSession{conn: connection, selectedCharacterID: 20}, currentCeraShopTestRequestBody(100070)); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) || !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(100070, 1)) {
		t.Fatalf("avatar purchase ACK=%+v body=%x", ack.Header, ack.Body)
	}
	update, rest := splitGameServerUpperPacket(t, rest)
	if update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) || update.Header.Classification != 0 ||
		len(update.Body) != 3+currentItemListEntryWireSize+2*4 || update.Body[0] != 1 || binary.LittleEndian.Uint16(update.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint16(update.Body[3:5]) != 0 || binary.LittleEndian.Uint32(update.Body[5:9]) != 9001 || update.Body[3+0x0A] != 0xff ||
		!bytes.Equal(update.Body[3+currentItemListEntryWireSize:], make([]byte, 8)) {
		t.Fatalf("avatar list1 update=%+v body=%x", update.Header, update.Body)
	}
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || balance.Header.MsgID != currentCeraShopUpdateMsgID || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(10)) {
		t.Fatalf("avatar balance=%+v body=%x trailing=%d", balance.Header, balance.Body, len(trailing))
	}

	inventory, found, err := repositories.Inventory.Load(ctx, "20")
	if err != nil || !found {
		t.Fatalf("load avatar inventory found=%t err=%v", found, err)
	}
	stack, found := inventory.Slots["1:0"]
	if !found || stack.ItemID != 9001 || stack.Count != 1 || stack.Extra["amount_or_count"] != "0" || stack.Extra["last_cera_shop_section"] != "avatar" || stack.Extra["ext_data0"] != "255" {
		t.Fatalf("persisted avatar=%+v found=%t", stack, found)
	}
}

func TestCurrentCeraShopPurchaseAvatarCouponConsumesVoucherAndDoesNotDeductCera(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 40)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "30",
		AccountID:   "account-1",
		Stats:       map[string]int64{"cera": 40},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "30",
		Slots: map[string]dnfrepo.ItemStack{
			"0:67": {ItemID: 2681594, Count: 8, Bind: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.handleCurrentCeraShopPurchase(
		&gameSession{conn: connection, selectedCharacterID: 30},
		currentCeraShopTestRequestBodyWithPayment(100071, currentCeraShopPaymentModeAvatarCoupon, 0),
	); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) || !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(100071, 1)) {
		t.Fatalf("avatar coupon purchase ACK=%+v body=%x", ack.Header, ack.Body)
	}
	couponUpdate, rest := splitGameServerUpperPacket(t, rest)
	if couponUpdate.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) || couponUpdate.Body[0] != dnfrepo.MainInventoryListType ||
		binary.LittleEndian.Uint16(couponUpdate.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint16(couponUpdate.Body[3:5]) != 67 ||
		binary.LittleEndian.Uint32(couponUpdate.Body[5:9]) != 2681594 ||
		binary.LittleEndian.Uint32(couponUpdate.Body[9:13]) != 7 {
		t.Fatalf("coupon update=%+v body=%x", couponUpdate.Header, couponUpdate.Body)
	}
	avatarUpdate, rest := splitGameServerUpperPacket(t, rest)
	if avatarUpdate.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) || avatarUpdate.Body[0] != 1 ||
		binary.LittleEndian.Uint16(avatarUpdate.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint16(avatarUpdate.Body[3:5]) != 0 ||
		binary.LittleEndian.Uint32(avatarUpdate.Body[5:9]) != 9001 {
		t.Fatalf("avatar update=%+v body=%x", avatarUpdate.Header, avatarUpdate.Body)
	}
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || balance.Header.MsgID != currentCeraShopUpdateMsgID || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(40)) {
		t.Fatalf("avatar coupon balance=%+v body=%x trailing=%d", balance.Header, balance.Body, len(trailing))
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != 40 {
		t.Fatalf("account cera after avatar coupon=%d, want 40", got)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "30")
	if err != nil || !found {
		t.Fatalf("load avatar coupon inventory found=%t err=%v", found, err)
	}
	if coupon := inventory.Slots["0:67"]; coupon.ItemID != 2681594 || coupon.Count != 7 {
		t.Fatalf("coupon after purchase=%+v", coupon)
	}
	if avatar := inventory.Slots["1:0"]; avatar.ItemID != 9001 || avatar.Count != 1 || avatar.Extra["avatar_duration_selector_index"] != "3" {
		t.Fatalf("avatar after coupon purchase=%+v", avatar)
	}
}

func TestGrantCurrentCeraShopAvatarAutoOpensOnlyPVFDefaultAuraSockets(t *testing.T) {
	tests := []struct {
		name          string
		equipmentType string
		socketPVF     string
		wantTypes     []byte
	}{
		{
			name:          "aura default sockets",
			equipmentType: "[aurora avatar]",
			socketPVF:     "[avatar emblem socket num]\n2\n[emblem socket default]\n`[M socket]`\n`[M socket]`\n[/emblem socket default]\n",
			wantTypes:     []byte{0xEF, 0xEF},
		},
		{
			name:          "aura manual socket choices",
			equipmentType: "[aurora avatar]",
			socketPVF:     "[avatar type select]\n`[M socket]`\n`[M socket]`\n[/avatar type select]\n",
		},
		{
			name:          "ordinary avatar default declaration",
			equipmentType: "[hat avatar]",
			socketPVF:     "[avatar emblem socket num]\n2\n[emblem socket default]\n`[M socket]`\n`[M socket]`\n[/emblem socket default]\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const path = "equipment/avatar/test_default_socket.equ"
			source := dungeonDropCatalogTestSource{
				path: "[equipment type]\n`" + test.equipmentType + "`\n" + test.socketPVF,
			}
			inventory := dnfrepo.InventoryRecord{CharacterID: "19", Slots: make(map[string]dnfrepo.ItemStack)}
			slot, err := grantCurrentCeraShopAvatar(
				&inventory,
				source,
				dungeonDropItemDefinition{ItemID: 9001, Kind: dungeonDropItemEquipment, PVFPath: path},
				currentCeraShopProduct{ItemID: 9001, Count: 1, Section: "avatar"},
				0,
			)
			if err != nil {
				t.Fatal(err)
			}
			stack := inventory.Slots[currentCeraShopInventorySlotKey(1, slot)]
			data := currentAvatarSocketData(stack.Extra)
			if got := currentAvatarSocketOpenCount(data); got != len(test.wantTypes) {
				t.Fatalf("open sockets=%d want=%d data=%x extra=%+v", got, len(test.wantTypes), data, stack.Extra)
			}
			for index, want := range test.wantTypes {
				if got := currentAvatarSocketType(data, byte(index)); got != want {
					t.Fatalf("socket[%d]=0x%02x want=0x%02x data=%x", index, got, want, data)
				}
			}
			if len(test.wantTypes) == 0 {
				for _, key := range []string{"avatar_socket_data", "reserved2", "jewel_socket"} {
					if stack.Extra[key] != "" {
						t.Fatalf("unopened avatar wrote %s=%q", key, stack.Extra[key])
					}
				}
				return
			}
			for _, key := range []string{"avatar_socket_data", "reserved2", "jewel_socket"} {
				if stack.Extra[key] == "" {
					t.Fatalf("opened aura missing %s: %+v", key, stack.Extra)
				}
			}
		})
	}
}

func TestCurrentCeraShopPurchaseNormalizesPackageQuantityAndUsesMainContainer(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 60)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "21", AccountID: "account-1", Stats: map[string]int64{"cera": 60}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "21", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.handleCurrentCeraShopPurchase(&gameSession{conn: connection, selectedCharacterID: 21}, currentCeraShopTestRequestBody(100060)); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) || !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(100060, 1)) {
		t.Fatalf("package purchase ACK=%+v body=%x", ack.Header, ack.Body)
	}
	update, rest := splitGameServerUpperPacket(t, rest)
	if update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) || update.Body[0] != dnfrepo.MainInventoryListType ||
		binary.LittleEndian.Uint16(update.Body[1:3]) != 1 || binary.LittleEndian.Uint32(update.Body[5:9]) != 37 || binary.LittleEndian.Uint32(update.Body[9:13]) != 1 {
		t.Fatalf("package main update=%+v body=%x", update.Header, update.Body)
	}
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || balance.Header.MsgID != currentCeraShopUpdateMsgID || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(10)) {
		t.Fatalf("package balance=%+v body=%x trailing=%d", balance.Header, balance.Body, len(trailing))
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "21")
	if err != nil || !found {
		t.Fatalf("load package inventory found=%t err=%v", found, err)
	}
	stack, found := inventory.Slots["0:3"]
	if !found || stack.ItemID != 37 || stack.Count != 1 || stack.Extra["last_cera_shop_section"] != "package" {
		t.Fatalf("persisted package=%+v found=%t", stack, found)
	}
}

func TestCurrentCeraShopPurchaseConsumesPersonalCargoUpgradeAndRefreshesListTwo(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 120)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "22", AccountID: "account-1", Stats: map[string]int64{"cera": 120}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "22", Slots: map[string]dnfrepo.ItemStack{}, Warehouse: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Settings.Save(ctx, newCharacterContainerStateSettings("22", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "cera-cargo-upgrade", selectedCharacterID: 22}
	if err := service.handleCurrentCeraShopPurchase(session, currentCeraShopTestRequestBody(100080)); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) || !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(100080, 1)) {
		t.Fatalf("cargo upgrade ACK=%+v body=%x", ack.Header, ack.Body)
	}
	cargo, rest := splitGameServerUpperPacketWithHeader(t, rest, dnfproto.GameServerUpperHeaderSize16)
	if cargo.Header.Classification != 0 || cargo.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) || !bytes.Equal(cargo.Body, []byte{2, 24, 0, 0, 0, 0}) {
		t.Fatalf("cargo refresh header=%+v body=%x", cargo.Header, cargo.Body)
	}
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || balance.Header.MsgID != currentCeraShopUpdateMsgID || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(60)) {
		t.Fatalf("cargo upgrade balance=%+v body=%x trailing=%d", balance.Header, balance.Body, len(trailing))
	}

	state, found, err := dnfrepo.LoadCharacterContainerState(ctx, repositories.Settings, "22")
	if err != nil || !found || state.PersonalCargoSlotCount != 24 {
		t.Fatalf("persisted personal cargo state=%+v found=%t err=%v", state, found, err)
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != 60 {
		t.Fatalf("persisted account cera=%d, want 60", got)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "22")
	if err != nil || !found || len(inventory.Slots) != 0 {
		t.Fatalf("personal cargo upgrade must not create a bag stack: inventory=%+v found=%t err=%v", inventory, found, err)
	}

	retryConnection := &bufferConn{}
	if err := service.handleCurrentCeraShopPurchase(&gameSession{conn: retryConnection, selectedCharacterID: 22}, currentCeraShopTestRequestBody(100080)); err != nil {
		t.Fatal(err)
	}
	failure, trailing := splitGameServerUpperPacket(t, retryConnection.write.Bytes())
	if len(trailing) != 0 || failure.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) || !bytes.Equal(failure.Body, buildCurrentCeraShopPurchaseFailureBody()) {
		t.Fatalf("repeated cargo upgrade response=%+v body=%x trailing=%d", failure.Header, failure.Body, len(trailing))
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != 60 {
		t.Fatalf("repeated cargo upgrade debited account Cera: %d", got)
	}
}

func TestCurrentCeraShopPurchaseConsumesMainInventoryUpgradesAndRefreshesListZero(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 1000)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "26", AccountID: "account-1", Stats: map[string]int64{"cera": 1000}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "26", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Settings.Save(ctx, newCharacterContainerStateSettings("26", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	remainingCera := int64(1000)
	stages := []struct {
		commodityID uint32
		expansion   uint16
		price       int64
	}{
		{commodityID: 101098, expansion: 8, price: 60},
		{commodityID: 102046, expansion: 16, price: 70},
		{commodityID: 102531, expansion: 24, price: 80},
	}
	for _, stage := range stages {
		connection := &bufferConn{}
		session := &gameSession{conn: connection, connID: "cera-main-inventory-upgrade", selectedCharacterID: 26}
		if err := service.handleCurrentCeraShopPurchase(session, currentCeraShopTestRequestBody(stage.commodityID)); err != nil {
			t.Fatal(err)
		}

		ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
		if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) || !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(stage.commodityID, 1)) {
			t.Fatalf("main inventory upgrade ACK=%+v body=%x", ack.Header, ack.Body)
		}
		refresh, rest := splitGameServerUpperPacketWithHeader(t, rest, dnfproto.GameServerUpperHeaderSize16)
		wantRefresh := []byte{0, byte(stage.expansion), 0, 0, 0}
		if refresh.Header.Classification != 0 || refresh.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) || !bytes.Equal(refresh.Body, wantRefresh) {
			t.Fatalf("main inventory refresh header=%+v body=%x want=%x", refresh.Header, refresh.Body, wantRefresh)
		}
		remainingCera -= stage.price
		balance, trailing := splitGameServerUpperPacket(t, rest)
		if len(trailing) != 0 || balance.Header.MsgID != currentCeraShopUpdateMsgID || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(remainingCera)) {
			t.Fatalf("main inventory balance=%+v body=%x trailing=%d", balance.Header, balance.Body, len(trailing))
		}

		state, found, err := dnfrepo.LoadCharacterContainerState(ctx, repositories.Settings, "26")
		if err != nil || !found || state.MainSlotCount != stage.expansion {
			t.Fatalf("persisted main inventory state=%+v found=%t err=%v", state, found, err)
		}
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != remainingCera {
		t.Fatalf("persisted account cera=%d want=%d", got, remainingCera)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "26")
	if err != nil || !found || len(inventory.Slots) != 0 {
		t.Fatalf("main inventory upgrades must not create bag stacks: inventory=%+v found=%t err=%v", inventory, found, err)
	}
}

func TestCurrentCeraShopPurchaseRejectsOutOfOrderMainInventoryUpgradeWithoutDebit(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 500)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "27", AccountID: "account-1", Stats: map[string]int64{"cera": 500}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "27", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Settings.Save(ctx, newCharacterContainerStateSettings("27", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.handleCurrentCeraShopPurchase(&gameSession{conn: connection, selectedCharacterID: 27}, currentCeraShopTestRequestBody(102046)); err != nil {
		t.Fatal(err)
	}
	failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || failure.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) || !bytes.Equal(failure.Body, buildCurrentCeraShopPurchaseFailureBody()) {
		t.Fatalf("out-of-order main inventory response=%+v body=%x trailing=%d", failure.Header, failure.Body, len(trailing))
	}
	state, found, err := dnfrepo.LoadCharacterContainerState(ctx, repositories.Settings, "27")
	if err != nil || !found || state.MainSlotCount != 0 {
		t.Fatalf("out-of-order main inventory changed state=%+v found=%t err=%v", state, found, err)
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != 500 {
		t.Fatalf("out-of-order main inventory debited account Cera: %d", got)
	}
}

func TestCurrentCeraShopPurchaseMainInventoryUpgradeRestoresCapacityWhenSettlementRejects(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 59)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "28", AccountID: "account-1", Stats: map[string]int64{"cera": 59}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "28", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Settings.Save(ctx, newCharacterContainerStateSettings("28", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.handleCurrentCeraShopPurchase(&gameSession{conn: connection, selectedCharacterID: 28}, currentCeraShopTestRequestBody(101098)); err != nil {
		t.Fatal(err)
	}
	failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || !bytes.Equal(failure.Body, buildCurrentCeraShopPurchaseFailureBody()) {
		t.Fatalf("insufficient Cera main inventory response=%+v body=%x trailing=%d", failure.Header, failure.Body, len(trailing))
	}
	state, found, err := dnfrepo.LoadCharacterContainerState(ctx, repositories.Settings, "28")
	if err != nil || !found || state.MainSlotCount != 0 {
		t.Fatalf("insufficient Cera left main inventory capacity changed: state=%+v found=%t err=%v", state, found, err)
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != 59 {
		t.Fatalf("insufficient Cera changed wallet: account cera=%d want=59", got)
	}
}

func TestCurrentCeraShopMainInventoryUpgradeStageUsesExactPVFPaths(t *testing.T) {
	cases := []struct {
		path    string
		stage   uint16
		upgrade bool
	}{
		{path: "cash/inven_upgradekit1.stk", stage: 1, upgrade: true},
		{path: "stackable/cash/inven_upgradekit2.stk", stage: 2, upgrade: true},
		{path: "cash/chn_20140617_new_sales_item/chn_inventory_3rd_expansion_2683675.stk", stage: 3, upgrade: true},
		{path: "cash/safe_upgradekit.stk", stage: 0, upgrade: false},
		{path: "cash/inven_upgradekit3.stk", stage: 0, upgrade: false},
	}
	for _, test := range cases {
		stage, upgrade := currentCeraShopMainInventoryUpgradeStage(dungeonDropItemDefinition{Kind: dungeonDropItemStackable, PVFPath: test.path})
		if stage != test.stage || upgrade != test.upgrade {
			t.Fatalf("path=%q stage=%d upgrade=%t want_stage=%d want_upgrade=%t", test.path, stage, upgrade, test.stage, test.upgrade)
		}
	}
}

func TestCurrentCeraShopPersonalCargoUpgradeTargetUsesOnlyRealPersonalCargoToolPaths(t *testing.T) {
	cases := []struct {
		path   string
		want   uint16
		isTool bool
	}{
		{path: "stackable/cash/safe_upgradekit.stk", want: 24, isTool: true},
		{path: "stackable/cash/safe_upgradekit1.stk", want: 40, isTool: true},
		{path: "stackable/cash/chn_account_cargo/safe_upgradekit.stk", want: 0, isTool: false},
		{path: "stackable/cash/not_safe_upgradekit.stk", want: 0, isTool: false},
	}
	for _, test := range cases {
		got, isTool := currentCeraShopPersonalCargoUpgradeTarget(dungeonDropItemDefinition{Kind: dungeonDropItemStackable, PVFPath: test.path})
		if got != test.want || isTool != test.isTool {
			t.Fatalf("path=%q target=%d tool=%t want_target=%d want_tool=%t", test.path, got, isTool, test.want, test.isTool)
		}
	}
}

func TestCurrentCeraShopPurchaseConsumesDocumentDefinedPersonalCargoUpgrade(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	definition, err := catalog.items.ResolveItem(40)
	if err != nil {
		t.Fatal(err)
	}
	if target, isUpgrade := catalog.personalCargoUpgradeTarget(definition); !isUpgrade || target != 168 {
		t.Fatalf("document-defined cargo target=%d is_upgrade=%t", target, isUpgrade)
	}

	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 2500)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "24", AccountID: "account-1", Stats: map[string]int64{"cera": 2500}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "24", Slots: map[string]dnfrepo.ItemStack{}, Warehouse: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	state := newCharacterContainerStateSettings("24", time.Now().UTC())
	state.Values["personal_cargo_list_param16"] = "152"
	if err := repositories.Settings.Save(ctx, state); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.handleCurrentCeraShopPurchase(&gameSession{conn: connection, selectedCharacterID: 24}, currentCeraShopTestRequestBody(100082)); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) || !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(100082, 1)) {
		t.Fatalf("document-defined cargo ACK=%+v body=%x", ack.Header, ack.Body)
	}
	cargo, rest := splitGameServerUpperPacketWithHeader(t, rest, dnfproto.GameServerUpperHeaderSize16)
	if cargo.Header.Classification != 0 || cargo.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) || !bytes.Equal(cargo.Body, []byte{2, 168, 0, 0, 0, 0}) {
		t.Fatalf("document-defined cargo refresh header=%+v body=%x", cargo.Header, cargo.Body)
	}
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || balance.Header.MsgID != currentCeraShopUpdateMsgID || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(0)) {
		t.Fatalf("document-defined cargo balance=%+v body=%x trailing=%d", balance.Header, balance.Body, len(trailing))
	}
	updatedState, found, loadErr := dnfrepo.LoadCharacterContainerState(ctx, repositories.Settings, "24")
	if loadErr != nil || !found || updatedState.PersonalCargoSlotCount != 168 {
		t.Fatalf("persisted document-defined cargo state=%+v found=%t err=%v", updatedState, found, loadErr)
	}
	inventory, found, loadErr := repositories.Inventory.Load(ctx, "24")
	if loadErr != nil || !found || len(inventory.Slots) != 0 {
		t.Fatalf("document-defined cargo upgrade must not create a bag stack: inventory=%+v found=%t err=%v", inventory, found, loadErr)
	}
}

func TestCurrentCeraShopPurchasePersonalCargoUpgradeRestoresCapacityWhenCeraSettlementRejects(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 59)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "23", AccountID: "account-1", Stats: map[string]int64{"cera": 59}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "23", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Settings.Save(ctx, newCharacterContainerStateSettings("23", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		ceraShopCatalog:    catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.handleCurrentCeraShopPurchase(&gameSession{conn: connection, selectedCharacterID: 23}, currentCeraShopTestRequestBody(100080)); err != nil {
		t.Fatal(err)
	}
	failure, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || !bytes.Equal(failure.Body, buildCurrentCeraShopPurchaseFailureBody()) {
		t.Fatalf("insufficient Cera response=%+v body=%x trailing=%d", failure.Header, failure.Body, len(trailing))
	}
	state, found, err := dnfrepo.LoadCharacterContainerState(ctx, repositories.Settings, "23")
	if err != nil || !found || state.PersonalCargoSlotCount != 8 {
		t.Fatalf("insufficient Cera left capacity changed: state=%+v found=%t err=%v", state, found, err)
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != 59 {
		t.Fatalf("insufficient Cera changed wallet: account cera=%d, want 59", got)
	}
}

func TestParseCurrentCeraShopPurchaseRequestUsesCurrentCartOffset(t *testing.T) {
	request, err := parseCurrentCeraShopPurchaseRequest(currentCeraShopTestRequestBody(100050))
	if err != nil || request.PaymentMode != 0 || len(request.Items) != 1 || request.Items[0].CommodityID != 100050 || request.Items[0].AttributeValue != 0xff {
		t.Fatalf("parsed request=%+v err=%v", request, err)
	}
}

func TestRealPVFCeraShopCatalogResolvesAtLeastOneSupportedProduct(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify real Cera-shop product parsing")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFCeraShopCatalog(archive)
	if err != nil {
		t.Fatalf("load real Cera-shop catalog: %v", err)
	}
	if len(catalog.products) == 0 {
		t.Fatal("real Cera-shop catalog has no supported standard products")
	}
	for _, product := range catalog.products {
		if _, err := catalog.items.ResolveItem(product.ItemID); err == nil {
			return
		}
	}
	t.Fatal("real Cera-shop catalog has no product backed by a current inventory item definition")
}

func TestRealPVFCeraShopCatalogResolvesObservedCurrentCommodity(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the commodity from the current client log")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFCeraShopCatalog(archive)
	if err != nil {
		t.Fatalf("load real Cera-shop catalog: %v", err)
	}
	const observedCommodityID = 102930 // 12 92 01 00 at current C2S op64 body[7:11].
	product, found := catalog.Product(observedCommodityID)
	if !found {
		t.Fatalf("current-client commodity=%d is not in a supported runtime Cera-shop section", observedCommodityID)
	}
	if _, err := catalog.items.ResolveItem(product.ItemID); err != nil {
		t.Fatalf("current-client commodity=%d item=%d cannot be represented in current inventory: %v", observedCommodityID, product.ItemID, err)
	}
	t.Logf("current-client commodity=%d item=%d count=%d cera=%d section=%s", product.CommodityID, product.ItemID, product.Count, product.CeraPrice, product.Section)
}

func TestRealPVFCeraShopVisualDurationMatchesReportedPurchase(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the reported visual duration product")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFCeraShopCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	product, found := catalog.Product(102240)
	if !found || product.Section != "visual" || product.ItemID != 100330500 || product.Count != 1 || product.DurationDays != 30 || product.CeraPrice != 600 {
		t.Fatalf("reported visual product=%+v found=%t", product, found)
	}
	definition, err := catalog.items.ResolveItem(product.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 23, 1, 0, 0, 0, time.UTC)
	grant, err := currentCeraShopProductDefinitionForGrantAt(definition, product, now)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(30 * 24 * time.Hour)
	if !grant.ExpirationDate.Equal(want) {
		t.Fatalf("grant expiration=%s want=%s", grant.ExpirationDate, want)
	}
}

func TestRealPVFCeraShopCatalogResolvesObservedPackageAndAvatarCommodities(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify current package and avatar rows")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFCeraShopCatalog(archive)
	if err != nil {
		t.Fatalf("load real Cera-shop catalog: %v", err)
	}
	packageProduct, found := catalog.Product(104801)
	if !found || packageProduct.Section != "package" || packageProduct.ItemID != 490702473 || packageProduct.Count != 1 || packageProduct.CeraPrice != 27800 {
		t.Fatalf("package product=%+v found=%t", packageProduct, found)
	}
	if _, err := catalog.items.ResolveItem(packageProduct.ItemID); err != nil {
		t.Fatalf("package item=%d cannot be represented: %v", packageProduct.ItemID, err)
	}
	avatarProduct, found := catalog.Product(589071)
	if !found || avatarProduct.Section != "avatar" || avatarProduct.ItemID != 112500217 || avatarProduct.AvatarDurationIndex != 3 {
		t.Fatalf("avatar product=%+v found=%t", avatarProduct, found)
	}
	avatarProduct, err = catalog.resolvePurchase(avatarProduct)
	if err != nil || avatarProduct.CeraPrice == 0 {
		t.Fatalf("resolved avatar=%+v err=%v", avatarProduct, err)
	}
	selectablePremium, found := catalog.Product(100817)
	if !found || selectablePremium.Section != "selectable character premium" || selectablePremium.ItemID != 2682205 || selectablePremium.CeraPrice != 250 {
		t.Fatalf("selectable premium product=%+v found=%t", selectablePremium, found)
	}
	contractPackage, found := catalog.Product(100625)
	if !found || contractPackage.Section != "charac premium package" || contractPackage.ItemID != 2682006 || contractPackage.CeraPrice != 3880 {
		t.Fatalf("contract package product=%+v found=%t", contractPackage, found)
	}
	t.Logf("package commodity=%d item=%d cera=%d; avatar commodity=%d item=%d duration_days=%d cera=%d", packageProduct.CommodityID, packageProduct.ItemID, packageProduct.CeraPrice, avatarProduct.CommodityID, avatarProduct.ItemID, avatarProduct.AvatarDurationDays, avatarProduct.CeraPrice)
}

func TestRealPVFCeraShopCatalogLocatesPersonalCargoUpgradeTool(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the real personal-cargo upgrade product")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFCeraShopCatalog(archive)
	if err != nil {
		t.Fatalf("load real Cera-shop catalog: %v", err)
	}
	matches := make([]string, 0)
	for _, product := range catalog.products {
		definition, definitionErr := catalog.items.ResolveItem(product.ItemID)
		if definitionErr != nil {
			continue
		}
		target, isUpgrade := catalog.personalCargoUpgradeTarget(definition)
		if !isUpgrade || target == 0 {
			continue
		}
		if product.Count != 1 || product.CeraPrice == 0 {
			t.Fatalf("personal cargo product=%+v definition=%+v", product, definition)
		}
		matches = append(matches, fmt.Sprintf("commodity=%d item=%d path=%s target_slots=%d cera=%d", product.CommodityID, product.ItemID, definition.PVFPath, target, product.CeraPrice))
	}
	if len(matches) == 0 {
		t.Fatal("real Cera-shop catalog has no safe_upgradekit personal-cargo product")
	}
	sort.Strings(matches)
	for _, match := range matches {
		t.Logf("personal cargo %s", match)
	}
}

func TestRealPVFCeraShopCatalogLocatesMainInventoryUpgradeStages(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the real main-inventory upgrade products")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFCeraShopCatalog(archive)
	if err != nil {
		t.Fatalf("load real Cera-shop catalog: %v", err)
	}
	expected := []struct {
		commodityID uint32
		itemID      uint32
		stage       uint16
	}{
		{commodityID: 101098, itemID: 2660296, stage: 1},
		{commodityID: 102046, itemID: 2660297, stage: 2},
		{commodityID: 102531, itemID: 2683675, stage: 3},
	}
	for _, want := range expected {
		product, found := catalog.Product(want.commodityID)
		if !found || product.ItemID != want.itemID || product.Count != 1 {
			t.Fatalf("main inventory commodity=%d product=%+v found=%t", want.commodityID, product, found)
		}
		definition, resolveErr := catalog.items.ResolveItem(product.ItemID)
		if resolveErr != nil {
			t.Fatalf("main inventory commodity=%d item=%d: %v", want.commodityID, product.ItemID, resolveErr)
		}
		stage, isUpgrade := currentCeraShopMainInventoryUpgradeStage(definition)
		if !isUpgrade || stage != want.stage {
			t.Fatalf("main inventory commodity=%d item=%d path=%q stage=%d upgrade=%t", want.commodityID, product.ItemID, definition.PVFPath, stage, isUpgrade)
		}
		t.Logf("main inventory commodity=%d item=%d path=%s stage=%d cera=%d", product.CommodityID, product.ItemID, definition.PVFPath, stage, product.CeraPrice)
	}
}

func TestRealPVFListsEveryPersonalCargoUpgradeToolPath(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to inspect every real personal-cargo tool path")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	items, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatalf("load real item catalog: %v", err)
	}
	matches := make([]string, 0)
	for itemID, reference := range items.itemRefs {
		definition := dungeonDropItemDefinition{ItemID: itemID, Kind: reference.kind, PVFPath: cleanDungeonDropPath(reference.path)}
		target, isUpgrade := currentCeraShopPersonalCargoUpgradeTarget(definition)
		if !isUpgrade {
			continue
		}
		matches = append(matches, fmt.Sprintf("item=%d path=%s target_slots=%d", itemID, definition.PVFPath, target))
	}
	if len(matches) == 0 {
		t.Fatal("runtime PVF contains no safe_upgradekit path")
	}
	sort.Strings(matches)
	for _, match := range matches {
		t.Logf("personal cargo tool %s", match)
	}
}

func TestRealPVFCeraShopLocatesAccountCargoUpgradeTools(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to inspect real account-cargo Cera products")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFCeraShopCatalog(archive)
	if err != nil {
		t.Fatalf("load real Cera catalog: %v", err)
	}
	found := 0
	for _, product := range catalog.products {
		definition, err := catalog.items.ResolveItem(product.ItemID)
		if err != nil || !catalog.isAccountCargoUpgradeTool(definition) {
			continue
		}
		found++
		t.Logf("commodity=%d item=%d count=%d cera=%d path=%s", product.CommodityID, product.ItemID, product.Count, product.CeraPrice, definition.PVFPath)
	}
	if found == 0 {
		t.Fatal("real Cera catalog contains no PVF account-cargo upgrade tool")
	}
}

func TestRealPVFCeraShopResolvesObservedPost152Commodity(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to inspect the observed post-152 purchase")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFCeraShopCatalog(archive)
	if err != nil {
		t.Fatalf("load real Cera-shop catalog: %v", err)
	}
	const observedPost152Commodity = 103662 // current live legacy op64 body[7:11] = ee 94 01 00.
	product, found := catalog.Product(observedPost152Commodity)
	if !found {
		t.Fatalf("post-152 commodity=%d is absent from the parsed Cera catalog", observedPost152Commodity)
	}
	definition, err := catalog.items.ResolveItem(product.ItemID)
	if err != nil {
		t.Fatalf("resolve post-152 commodity=%d item=%d: %v", observedPost152Commodity, product.ItemID, err)
	}
	target, isUpgrade := catalog.personalCargoUpgradeTarget(definition)
	itemDocument, err := parseDungeonCardPVFDocument(archive, definition.PVFPath)
	if err != nil {
		t.Fatalf("parse post-152 item document %s: %v", definition.PVFPath, err)
	}
	name, _ := itemDocument.Text("name")
	explain, _ := itemDocument.Text("explain")
	t.Logf("post-152 commodity=%d product=%+v item_path=%s name=%q explain=%q personal_cargo_target=%d is_personal_cargo=%t", observedPost152Commodity, product, definition.PVFPath, name, explain, target, isUpgrade)
}

func TestRealPVFCeraShopDocumentLocatesObservedCommodityRows(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to inspect observed Cera-shop product rows")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	document, err := parseDungeonCardPVFDocument(archive, "etc/cerashop.etc")
	if err != nil {
		t.Fatalf("parse real Cera-shop document: %v", err)
	}
	for _, commodity := range []int64{101062, 101063, 103662} {
		found := false
		for _, section := range document.Sections {
			tokens, ok := document.Section(section.Name)
			if !ok {
				continue
			}
			for index, token := range tokens {
				if token.Int != commodity {
					continue
				}
				end := index + 12
				if end > len(tokens) {
					end = len(tokens)
				}
				window := make([]string, 0, end-index)
				for _, near := range tokens[index:end] {
					window = append(window, near.Raw)
				}
				t.Logf("commodity=%d section=%s tokens=%s", commodity, section.Name, strings.Join(window, " "))
				found = true
			}
		}
		if !found {
			t.Logf("commodity=%d is absent from etc/cerashop.etc", commodity)
		}
	}
}

func TestRealPVFCeraShopDocumentReportsProductSectionShapes(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to inspect Cera-shop category section shapes")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	document, err := parseDungeonCardPVFDocument(archive, "etc/cerashop.etc")
	if err != nil {
		t.Fatalf("parse real Cera-shop document: %v", err)
	}
	for _, section := range document.Sections {
		tokens, found := document.Section(section.Name)
		if !found {
			continue
		}
		values := currentCeraShopPVFValues(tokens)
		if len(values) == 0 {
			continue
		}
		if len(values)%6 == 0 || len(values)%8 == 0 || len(values)%9 == 0 || len(values)%11 == 0 {
			t.Logf("section=%q values=%d", section.Name, len(values))
		}
		if section.Name == "selectable character premium" || section.Name == "charac premium package" {
			limit := 24
			if len(values) < limit {
				limit = len(values)
			}
			first := make([]string, 0, limit)
			for _, value := range values[:limit] {
				first = append(first, value.Raw)
			}
			t.Logf("section=%q first=%s", section.Name, strings.Join(first, " "))
		}
	}
}

func mustCurrentCeraShopPremiumCatalog() *currentPremiumCatalog {
	return &currentPremiumCatalog{
		contractsByItem: map[int64]currentPremiumContractInfo{
			44: {ItemID: 44, PremiumType: premium.TypeOverSkill, DurationSeconds: 3 * 86400},
			45: {ItemID: 45, PremiumType: premium.TypeCrystal, DurationSeconds: 7 * 86400},
		},
		crystalCubeIDs: [6]int64{3033, 3034, 3035, 3036, 3037, 3262},
		devilSlots: map[uint32]currentPremiumDevilSlotInfo{
			100817: {CommodityID: 100817, ItemID: 2682205, Slot: premium.DevilSlotAutoRepair, Days: 7, CeraPrice: 250},
			100818: {CommodityID: 100818, ItemID: 2682205, Slot: premium.DevilSlotAutoRepair, Days: 30, CeraPrice: 500},
		},
	}
}

func newCeraShopPremiumTestService(repositories dnfrepo.Group, catalog *pvfCeraShopCatalog) *Service {
	return &Service{
		options:            options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		ceraShopCatalog:    catalog,
		premiumCatalog:     mustCurrentCeraShopPremiumCatalog(),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
}

func seedCeraShopPremiumCharacter(t *testing.T, ctx context.Context, repositories dnfrepo.Group, characterID string, cera int64) {
	t.Helper()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", cera)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: characterID, AccountID: "account-1", Stats: map[string]int64{"cera": cera}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: characterID, Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
}

func readPremiumActivatedPacket(t *testing.T, rest []byte, wantTypeByte byte, wantRemaining int64) []byte {
	t.Helper()
	noti, trailing := splitGameServerUpperPacket(t, rest)
	if noti.Header.Classification != 0 || noti.Header.MsgID != currentPremiumActivatedMsgID {
		t.Fatalf("premium NOTI header=%+v, want class0/0x42", noti.Header)
	}
	if len(noti.Body) != 11 || noti.Body[0] != 2 || noti.Body[1] != 0 || noti.Body[2] != wantTypeByte {
		t.Fatalf("premium NOTI body=%x, want prefix 02 00 %02X", noti.Body, wantTypeByte)
	}
	remaining := int64(binary.LittleEndian.Uint64(noti.Body[3:11]))
	if remaining < wantRemaining-30 || remaining > wantRemaining {
		t.Fatalf("premium NOTI remaining=%d, want ~%d", remaining, wantRemaining)
	}
	return trailing
}

func readPremiumServiceStatePacket(t *testing.T, rest []byte) []byte {
	t.Helper()
	state, trailing := splitGameServerUpperPacket(t, rest)
	if state.Header.Classification != dnfproto.DefaultChannelClassification ||
		state.Header.MsgID != uint16(dnfenum.CmdPacketPremiumService) {
		t.Fatalf("premium service header=%+v, want class1/op903", state.Header)
	}
	if len(state.Body) != 77 || !bytes.Equal(state.Body[:3], []byte{1, 1, 0}) {
		t.Fatalf("premium service body=%x, want success/action1/74-byte state", state.Body)
	}
	return trailing
}

func TestCurrentCeraShopPurchaseDevilSlotActivatesAccountPremium(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopPremiumCharacter(t, ctx, repositories, "19", 1000)
	connection := &bufferConn{}
	service := newCeraShopPremiumTestService(repositories, catalog)
	session := &gameSession{conn: connection, connID: "cera-shop-devil", selectedCharacterID: 19}

	if err := service.handleCurrentCeraShopPurchase(session, currentCeraShopTestRequestBody(100817)); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketBuyCerashopItem) || !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(100817, 6)) {
		t.Fatalf("purchase ACK=%+v body=%x", ack.Header, ack.Body)
	}
	rest = readPremiumServiceStatePacket(t, rest)
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || balance.Header.MsgID != currentCeraShopUpdateMsgID || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(750)) {
		t.Fatalf("balance=%+v body=%x trailing=%d", balance.Header, balance.Body, len(trailing))
	}

	account, ok, err := repositories.Account.Load(ctx, "account-1")
	if err != nil || !ok {
		t.Fatalf("account load ok=%t err=%v", ok, err)
	}
	expire := premium.ExpireAt(account, premium.DevilSlotType(premium.DevilSlotAutoRepair))
	if want := time.Now().Unix() + 7*86400; expire < want-30 || expire > want {
		t.Fatalf("devil slot expire=%d, want ~%d", expire, want)
	}
	if got := loadCeraShopAccountCera(t, ctx, repositories, "account-1"); got != 750 {
		t.Fatalf("cera=%d, want 750", got)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found || len(inventory.Slots) != 0 {
		t.Fatalf("devil slot purchase must not deliver items: %+v", inventory.Slots)
	}
}

func TestCurrentCeraShopPurchaseContractItemActivatesWithoutDelivery(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopPremiumCharacter(t, ctx, repositories, "19", 2000)
	connection := &bufferConn{}
	service := newCeraShopPremiumTestService(repositories, catalog)
	session := &gameSession{conn: connection, connID: "cera-shop-contract", selectedCharacterID: 19}

	if err := service.handleCurrentCeraShopPurchase(session, currentCeraShopTestRequestBody(100090)); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(100090, 1)) {
		t.Fatalf("purchase ACK body=%x", ack.Body)
	}
	rest = readPremiumActivatedPacket(t, rest, byte(premium.TypeOverSkill), 3*86400)
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(920)) {
		t.Fatalf("balance body=%x trailing=%d", balance.Body, len(trailing))
	}

	account, _, _ := repositories.Account.Load(ctx, "account-1")
	if !premium.Active(account, premium.TypeOverSkill, time.Now()) {
		t.Fatalf("over-skill premium not active: %+v", account.Metadata)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "19")
	if len(inventory.Slots) != 0 {
		t.Fatalf("contract item must be consumed on purchase: %+v", inventory.Slots)
	}
}

func TestCurrentCeraShopPurchaseCrystalContractRefreshesSelectionState(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	catalog.products[100091] = currentCeraShopProduct{
		CommodityID: 100091,
		ItemID:      45,
		Count:       1,
		CeraPrice:   600,
		Section:     "premium",
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopPremiumCharacter(t, ctx, repositories, "19", 2000)
	connection := &bufferConn{}
	service := newCeraShopPremiumTestService(repositories, catalog)
	session := &gameSession{conn: connection, connID: "cera-shop-crystal-contract", selectedCharacterID: 19}

	if err := service.handleCurrentCeraShopPurchase(session, currentCeraShopTestRequestBody(100091)); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(100091, 1)) {
		t.Fatalf("purchase ACK body=%x", ack.Body)
	}
	rest = readPremiumActivatedPacket(t, rest, byte(premium.TypeCrystal), 7*86400)
	state, rest := splitGameServerUpperPacket(t, rest)
	if state.Header.Classification != 0 ||
		state.Header.MsgID != currentCrystalContractStateMsgID ||
		!bytes.Equal(state.Body, []byte{0, 0xff}) {
		t.Fatalf("crystal state header=%+v body=%x", state.Header, state.Body)
	}
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(1400)) {
		t.Fatalf("balance body=%x trailing=%d", balance.Body, len(trailing))
	}

	account, _, _ := repositories.Account.Load(ctx, "account-1")
	if !premium.Active(account, premium.TypeCrystal, time.Now()) {
		t.Fatalf("crystal premium not active: %+v", account.Metadata)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "19")
	if len(inventory.Slots) != 0 {
		t.Fatalf("crystal contract must be consumed on purchase: %+v", inventory.Slots)
	}
}

func TestCurrentCeraShopPurchaseDevilPackageActivatesAllSlots(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopPremiumCharacter(t, ctx, repositories, "19", 5000)
	connection := &bufferConn{}
	service := newCeraShopPremiumTestService(repositories, catalog)
	session := &gameSession{conn: connection, connID: "cera-shop-package", selectedCharacterID: 19}

	if err := service.handleCurrentCeraShopPurchase(session, currentCeraShopTestRequestBody(100625)); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(100625, 1)) {
		t.Fatalf("purchase ACK body=%x", ack.Body)
	}
	rest = readPremiumServiceStatePacket(t, rest)
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(1120)) {
		t.Fatalf("balance body=%x trailing=%d", balance.Body, len(trailing))
	}

	account, _, _ := repositories.Account.Load(ctx, "account-1")
	now := time.Now()
	for slot := int64(0); slot < premium.DevilSlotCount; slot++ {
		if !premium.Active(account, premium.DevilSlotType(slot), now) {
			t.Fatalf("devil slot %d not active after package purchase: %+v", slot, account.Metadata)
		}
	}
}

func TestCurrentCeraShopPurchaseThirtyDayDevilSlotActivates(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopPremiumCharacter(t, ctx, repositories, "19", 1000)
	connection := &bufferConn{}
	service := newCeraShopPremiumTestService(repositories, catalog)
	session := &gameSession{conn: connection, connID: "cera-shop-devil-unknown", selectedCharacterID: 19}

	if err := service.handleCurrentCeraShopPurchase(session, currentCeraShopTestRequestBody(100818)); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if !bytes.Equal(ack.Body, buildCurrentCeraShopPurchaseSuccessBodyWithCount(100818, 6)) {
		t.Fatalf("purchase ACK body=%x", ack.Body)
	}
	rest = readPremiumServiceStatePacket(t, rest)
	balance, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || !bytes.Equal(balance.Body, buildCurrentCeraShopBalanceBody(500)) {
		t.Fatalf("balance body=%x trailing=%d", balance.Body, len(trailing))
	}
	account, _, _ := repositories.Account.Load(ctx, "account-1")
	expire := premium.ExpireAt(account, premium.DevilSlotType(premium.DevilSlotAutoRepair))
	if want := time.Now().Unix() + 30*86400; expire < want-30 || expire > want {
		t.Fatalf("thirty-day devil slot expire=%d, want ~%d", expire, want)
	}
}

func TestCurrentCeraShopPurchaseCommitsNameTagWithWalletInOneCheckout(t *testing.T) {
	catalog := mustCurrentCeraShopTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	seedCeraShopAccountCera(t, ctx, repositories, "account-1", 200)
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "31",
		AccountID:   "account-1",
		Stats:       make(map[string]int64),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "31",
		Slots:       make(map[string]dnfrepo.ItemStack),
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options: options{accountID: "account-1"},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{selectedCharacterID: 31}
	result, err := service.commitCurrentCeraShopPurchase(
		ctx,
		session,
		catalog,
		currentCeraShopPurchaseRequest{
			PaymentMode: currentCeraShopPaymentModeCera,
			Items:       []currentCeraShopPurchaseRequestItem{{CommodityID: 100058}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.CeraAfter != 140 || result.NameTagActivation == nil ||
		result.NameTagActivation.ItemID != 9003 || result.NameTagActivation.Action != "new" {
		t.Fatalf("result=%+v activation=%+v", result, result.NameTagActivation)
	}
	account, _, _ := repositories.Account.Load(ctx, "account-1")
	character, _, _ := repositories.Character.Load(ctx, "31")
	equipment, found, err := repositories.Equipment.Load(ctx, "31")
	if err != nil || !found {
		t.Fatalf("load equipment found=%t err=%v", found, err)
	}
	nameTag := equipment.Entries[strconv.Itoa(int(currentNameTagEquipmentSlot))]
	if currentAccountCera(account) != 140 ||
		character.Stats["name_tag_item_id"] != 9003 ||
		character.Stats["name_tag_expire_time"] <= time.Now().UTC().Unix() ||
		nameTag.ItemID != 9003 || len(nameTag.RawEntry) != currentItemListEntryWireSize {
		t.Fatalf("account=%+v character=%+v equipment=%+v", account, character, equipment)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "31")
	if len(inventory.Slots) != 0 {
		t.Fatalf("consumed name tag entered inventory: %+v", inventory.Slots)
	}
}

func mustCurrentCeraShopTestCatalog(t *testing.T) *pvfCeraShopCatalog {
	t.Helper()
	source := bridgePVFSource{
		"etc/cerashop.etc": "[item]\n100050 37 3 0 0 80 `test potion` 0 0\n100056 43 1 0 0 20 `test timed item` 0 0\n100080 38 1 0 0 60 `test personal cargo upgrade` 0 0\n100081 39 1 0 0 60 `test personal cargo upgrade two` 0 0\n100082 40 1 0 0 2500 `test personal cargo upgrade 168` 0 0\n100083 41 1 0 0 2000 `test account cargo upgrade` 0 0\n101098 2660296 1 0 0 60 `test main inventory upgrade one` 0 0\n102046 2660297 1 0 0 70 `test main inventory upgrade two` 0 0\n102531 2683675 1 0 0 80 `test main inventory upgrade three` 0 0\n[/item]\n" +
			"[premium]\n100090 44 1 0 0 1080 `test expert contract` 0 0\n[/premium]\n[creature]\n100055 42 10 0 0 20 `test pet feed` 0 0\n[/creature]\n[coin]\n[/coin]\n[material]\n[/material]\n[recoveryitem]\n[/recoveryitem]\n" +
			"[package]\n100060 37 0 0 50 `test package` 0 0 0 0 0\n[/package]\n" +
			"[visual]\n100057 9002 -1 30 0 60 `test visual 30 days` 0\n100058 9003 -1 30 0 60 `test name tag 30 days` 0\n[/visual]\n" +
			"[avatar]\n100070 9001 1 0 0 30\n100071 9001 3 0 0 30\n[/avatar]\n" +
			"[selectable character premium]\n-1 2681927 -1 7 0 2000 `魔王之契约` 0 -1 100817 2682205 6 7 0 250 `自动修理` 0 -1 100818 2682205 6 30 0 500 `自动修理` 0 -1\n[/selectable character premium]\n" +
			"[charac premium package]\n100625 2682006 3880 `60天` 0 0 1 60 100614\n[/charac premium package]\n",
		"monster/monster.lst":                                        "",
		"stackable/stackable.lst":                                    "37 `test_potion.stk`\n38 `cash/safe_upgradekit.stk`\n39 `cash/safe_upgradekit1.stk`\n40 `10098001/10098631.stk`\n41 `cash/chn_account_cargo/account_cargo_upgrade.stk`\n42 `cash/creature/test_feed.stk`\n43 `cash/test_timed.stk`\n2660296 `cash/inven_upgradekit1.stk`\n2660297 `cash/inven_upgradekit2.stk`\n2683675 `cash/chn_20140617_new_sales_item/chn_inventory_3rd_expansion_2683675.stk`\n",
		"equipment/equipment.lst":                                    "9001 `avatar/test_avatar.equ`\n9002 `visual/test_nameplate.equ`\n9003 `visual/test_name_tag.equ`\n",
		"stackable/test_potion.stk":                                  "[stackable type]\n`[waste]`\n[stack limit]\n1000\n[expiration date]\n`2028-08-16 06:00:00`\n",
		"stackable/cash/safe_upgradekit.stk":                         "[stackable type]\n`[etc]`\n[stack limit]\n1\n",
		"stackable/cash/safe_upgradekit1.stk":                        "[stackable type]\n`[etc]`\n[stack limit]\n1\n",
		"stackable/cash/chn_account_cargo/account_cargo_upgrade.stk": "[stackable type]\n`[etc]`\n[stack limit]\n1\n",
		"stackable/cash/creature/test_feed.stk":                      "[stackable type]\n`[feed]`\n[stack limit]\n1000\n",
		"stackable/cash/test_timed.stk":                              "[stackable type]\n`[etc]`\n[stack limit]\n100\n[usable period]\n10\n",
		"stackable/cash/inven_upgradekit1.stk":                       "[stackable type]\n`[etc]`\n[stack limit]\n1\n",
		"stackable/cash/inven_upgradekit2.stk":                       "[stackable type]\n`[etc]`\n[stack limit]\n1\n",
		"stackable/cash/chn_20140617_new_sales_item/chn_inventory_3rd_expansion_2683675.stk": "[stackable type]\n`[etc]`\n[stack limit]\n1\n",
		"stackable/10098001/10098631.stk":     "[name]\n`金库升级工具`\n[explain]\n`可以使金库的空间增加到168格， 最大上限为200格。`\n[stackable type]\n`[etc]`\n[stack limit]\n1\n",
		"equipment/avatar/test_avatar.equ":    "[equipment type]\n`[hat avatar]`\n[grade]\n3\n[avatar type select]\n7 0 0 30 0 0 0 30 0 0 30 0 0 0 0 0 0 30 0 0 0\n",
		"equipment/visual/test_nameplate.equ": "[equipment type]\n`[title name]`\n[attach type]\n`[trade]`\n",
		"equipment/visual/test_name_tag.equ":  "[equipment type]\n`[name tag]`\n[visual duration]\n30\n[attach type]\n`[trade]`\n",
	}
	catalog, err := newPVFCeraShopCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	if product, found := catalog.Product(100050); !found || product.ItemID != 37 || product.Count != 3 || product.CeraPrice != 80 {
		t.Fatalf("catalog product=%+v found=%t", product, found)
	}
	if product, found := catalog.Product(100060); !found || product.Section != "package" || product.ItemID != 37 || product.Count != 1 || product.CeraPrice != 50 {
		t.Fatalf("package catalog product=%+v found=%t", product, found)
	}
	if product, found := catalog.Product(100070); !found || product.Section != "avatar" || product.ItemID != 9001 || product.CeraPrice != 30 {
		t.Fatalf("avatar catalog product=%+v found=%t", product, found)
	}
	if product, found := catalog.Product(100057); !found || product.Section != "visual" || product.ItemID != 9002 || product.Count != 1 || product.DurationDays != 30 || product.CeraPrice != 60 {
		t.Fatalf("visual catalog product=%+v found=%t", product, found)
	}
	if product, found := catalog.Product(100058); !found || product.ItemID != 9003 || product.CeraPrice != 60 {
		t.Fatalf("name-tag catalog product=%+v found=%t", product, found)
	}
	return catalog
}

func currentCeraShopTestRequestBody(commodityID uint32) []byte {
	return currentCeraShopTestRequestBodyWithPayment(commodityID, currentCeraShopPaymentModeCera, 0xff)
}

func currentCeraShopTestRequestBodyWithPayment(commodityID uint32, paymentMode byte, attributeValue byte) []byte {
	body := make([]byte, currentCeraShopRequestHeaderSize+currentCeraShopRequestItemStride)
	body[2] = 1
	body[4] = paymentMode
	body[5] = attributeValue
	binary.LittleEndian.PutUint32(body[7:11], commodityID)
	return body
}
