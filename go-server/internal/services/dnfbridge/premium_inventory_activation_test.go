package dnfbridge

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentPremiumContractGrantAutoActivatesBeforeOp14Projection(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "account-1", Metadata: make(map[string]string)}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1", Stats: make(map[string]int64)}); err != nil {
		t.Fatal(err)
	}
	contract := dnfrepo.ItemStack{ItemID: 44, Count: 2}
	entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, 10, contract)
	contract.RawEntry = append([]byte(nil), entry.data[:]...)
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:10": contract},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "19", Entries: make(map[string]dnfrepo.EquipmentEntry)}); err != nil {
		t.Fatal(err)
	}

	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		premiumCatalog: &currentPremiumCatalog{
			contractsByItem: map[int64]currentPremiumContractInfo{
				44: {ItemID: 44, PremiumType: premium.TypeOverSkill, DurationSeconds: 3 * 86400},
			},
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "contract-auto-activation", selectedCharacterID: 19}
	body := buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, []currentItemListEntry{entry})
	if err := service.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0); err != nil {
		t.Fatal(err)
	}

	notification, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if notification.Header.Classification != 0 || notification.Header.MsgID != currentPremiumActivatedMsgID ||
		len(notification.Body) != 11 || notification.Body[2] != byte(premium.TypeOverSkill) {
		t.Fatalf("activation notification = header=%+v body=%x", notification.Header, notification.Body)
	}
	remaining := int64(binary.LittleEndian.Uint64(notification.Body[3:11]))
	if remaining < 6*86400-30 || remaining > 6*86400 {
		t.Fatalf("remaining = %d, want about six days", remaining)
	}
	update, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || update.Header.Classification != 0 ||
		update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(update.Body) != 3+currentItemListEntryWireSize || update.Body[0] != dnfrepo.MainInventoryListType ||
		binary.LittleEndian.Uint16(update.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint16(update.Body[3:5]) != 10 ||
		binary.LittleEndian.Uint32(update.Body[5:9]) != math.MaxUint32 ||
		binary.LittleEndian.Uint32(update.Body[9:13]) != 0 {
		t.Fatalf("post-activation update = header=%+v body=%x trailing=%x", update.Header, update.Body, trailing)
	}

	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, exists := inventory.Slots["0:10"]; exists {
		t.Fatalf("contract entered inventory after notification: %+v", inventory.Slots)
	}
	account, found, err := repositories.Account.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("load account found=%t err=%v", found, err)
	}
	expire := premium.ExpireAt(account, premium.TypeOverSkill)
	if expire-time.Now().UTC().Unix() < 6*86400-30 || expire-time.Now().UTC().Unix() > 6*86400 {
		t.Fatalf("expire = %d, want about six days", expire)
	}
}

func TestCurrentPremiumContractSelectionMigrationDoesNotEmitPrematureNotification(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "account-1", Metadata: make(map[string]string)}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1", Stats: make(map[string]int64)}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:20": {ItemID: 31, Count: 1},
			"0:21": {ItemID: 44, Count: 1},
			"0:22": {ItemID: 500, Count: 9},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "19", Entries: make(map[string]dnfrepo.EquipmentEntry)}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{accountID: "account-1", gameUpperHeader: gameUpperHeaderChannel13, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		premiumCatalog: &currentPremiumCatalog{contractsByItem: map[int64]currentPremiumContractInfo{
			31: {ItemID: 31, PremiumType: premium.TypeOverEquip, DurationSeconds: 3 * 86400},
			44: {ItemID: 44, PremiumType: premium.TypeOverSkill, DurationSeconds: 3 * 86400},
		}},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "contract-select-migration", selectedCharacterID: 19}
	if err := service.reconcileCurrentPremiumInventoryBeforeList(ctx, session, "test_select", false); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("selection migration emitted packet before select ACK: %x", connection.write.Bytes())
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "19")
	if len(inventory.Slots) != 1 || inventory.Slots["0:22"].Count != 9 {
		t.Fatalf("selection migration inventory = %+v", inventory.Slots)
	}
	account, _, _ := repositories.Account.Load(ctx, "account-1")
	entries := currentSelectAckPremiumEntries(account, time.Now().UTC())
	if len(entries) != 1+2*9 || entries[0] != 2 {
		t.Fatalf("select ACK premium entries = %x", entries)
	}
	for offset := 1; offset < len(entries); offset += 9 {
		if entries[offset] != byte(premium.TypeOverEquip) && entries[offset] != byte(premium.TypeOverSkill) {
			t.Fatalf("unexpected premium type in select ACK entries: %x", entries)
		}
	}
}
