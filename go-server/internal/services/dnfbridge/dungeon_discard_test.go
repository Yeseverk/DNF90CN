package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestParseCurrentDungeonDiscardRequestMatchesLiveOp47(t *testing.T) {
	body := []byte{
		0x29, 0x04,
		0x0e, 0x01,
		0x00,
		0x47, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0x00,
	}
	request, err := parseCurrentDungeonDiscardRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.PositionX != 1065 || request.PositionY != 270 ||
		request.ListType != 0 || request.SourceSlot != 71 ||
		request.Count != 1 || request.SceneMode != 0 ||
		!bytes.Equal(request.AckPayload, []byte{0, 0x47, 0, 1, 0, 0, 0}) {
		t.Fatalf("request=%+v ack=%x", request, request.AckPayload)
	}

	for _, malformed := range [][]byte{
		body[:len(body)-1],
		append(append([]byte(nil), body...), 0),
		buildCurrentDungeonDiscardTestRequest(1065, 270, 0, 71, 0, 0),
		buildCurrentDungeonDiscardTestRequest(1065, 270, 1, 71, 1, 0),
		buildCurrentDungeonDiscardTestRequest(1065, 270, 0, 71, 1, 1),
	} {
		if _, err := parseCurrentDungeonDiscardRequest(malformed); !errors.Is(err, errCurrentDungeonDiscardMalformed) {
			t.Fatalf("malformed body accepted body=%x err=%v", malformed, err)
		}
	}
}

func TestBuildCurrentDungeonDiscardSceneBodyMatchesCurrentEXEOp40Reader(t *testing.T) {
	stack := dnfrepo.ItemStack{
		ItemID: 3037,
		Count:  1,
		Extra: map[string]string{
			"item_kind":      string(dungeonDropItemStackable),
			"stackable_type": "[material]",
		},
	}
	body, entry, err := buildCurrentDungeonDiscardSceneBody(
		401,
		733,
		244,
		0x11223344,
		17,
		stack,
		dungeonDropItemDefinition{
			ItemID:        3037,
			Kind:          dungeonDropItemStackable,
			AttachType:    "[free]",
			StackableType: "[material]",
		},
		1,
		currentDungeonDiscardSceneMode,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != currentDungeonDiscardSceneBodySize ||
		binary.LittleEndian.Uint16(body[0:2]) != 401 ||
		binary.LittleEndian.Uint16(body[2:4]) != 733 ||
		binary.LittleEndian.Uint16(body[4:6]) != 244 ||
		binary.LittleEndian.Uint32(body[6:10]) != 0x11223344 ||
		!bytes.Equal(body[10:10+currentItemListEntryWireSize], entry.data[:]) ||
		body[10+currentItemListEntryWireSize] != currentDungeonDiscardSceneMode ||
		binary.LittleEndian.Uint16(body[len(body)-2:]) != 401 {
		t.Fatalf("body=%x entry=%x", body, entry.data)
	}
	if binary.LittleEndian.Uint16(entry.data[0:2]) != 17 ||
		binary.LittleEndian.Uint32(entry.data[2:6]) != 3037 ||
		binary.LittleEndian.Uint32(entry.data[6:10]) != 1 {
		t.Fatalf("scene item row=%x", entry.data)
	}
}

func TestCurrentDungeonDiscardTradeableUsesRuntimePVFAttachType(t *testing.T) {
	for _, test := range []struct {
		attach string
		want   bool
	}{
		{attach: "`[free]`", want: true},
		{attach: "`[trade]`", want: true},
		{attach: "`[account]`", want: false},
		{attach: "`[sealing]`", want: false},
		{attach: "", want: false},
	} {
		if got := currentDungeonDiscardTradeable(dungeonDropItemDefinition{AttachType: test.attach}); got != test.want {
			t.Fatalf("attach=%q tradeable=%t want=%t", test.attach, got, test.want)
		}
	}
}

func TestCurrentDungeonDiscardMainInventoryCreatesGroundDropAndRepicks(t *testing.T) {
	service, runtime, repositories := prepareCurrentDungeonDropTest(t, 0, 3227)
	stack := currentDungeonDiscardTestStack(3227, 5)
	inventory, _, err := repositories.Inventory.Load(context.Background(), "99")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	inventory.Slots["0:121"] = stack
	if err := repositories.Inventory.Save(context.Background(), inventory); err != nil {
		t.Fatal(err)
	}

	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "dungeon-discard-main-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleTownSetUserPosition(session, buildTownPositionRequest(733, 244, 6, 100)); err != nil {
		t.Fatal(err)
	}
	requestBody := buildCurrentDungeonDiscardTestRequest(733, 244, 0, 121, 2, 0)
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketDropItem),
		requestBody,
	); err != nil {
		t.Fatal(err)
	}

	scenePacket, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	ack, rest := splitGameServerUpperPacket(t, rest)
	update, trailing := splitGameServerUpperPacket(t, rest)
	if ack.Header.MsgID != currentDungeonDiscardOpcode ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		len(ack.Body) != currentDungeonDiscardAckSize+1 ||
		ack.Body[0] != 1 ||
		!bytes.Equal(ack.Body[1:], []byte{0, 121, 0, 2, 0, 0, 0}) {
		t.Fatalf("ack header=%+v body=%x", ack.Header, ack.Body)
	}
	if scenePacket.Header.MsgID != currentDungeonDiscardSceneOpcode ||
		scenePacket.Header.Classification != 0 ||
		len(scenePacket.Body) != currentDungeonDiscardSceneBodySize ||
		len(trailing) != 0 {
		t.Fatalf("scene header=%+v body=%x trailing=%x", scenePacket.Header, scenePacket.Body, trailing)
	}
	if update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		update.Header.Classification != 0 ||
		len(update.Body) != 3+currentItemListEntryWireSize ||
		update.Body[0] != dnfrepo.MainInventoryListType ||
		binary.LittleEndian.Uint16(update.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint16(update.Body[3:5]) != 121 ||
		binary.LittleEndian.Uint32(update.Body[5:9]) != 3227 ||
		binary.LittleEndian.Uint32(update.Body[9:13]) != 3 {
		t.Fatalf("discard item update header=%+v body=%x", update.Header, update.Body)
	}
	if binary.LittleEndian.Uint16(scenePacket.Body[2:4]) != 733 ||
		binary.LittleEndian.Uint16(scenePacket.Body[4:6]) != 244 ||
		binary.LittleEndian.Uint32(scenePacket.Body[10+6:10+10]) != 2 {
		t.Fatalf("scene body=%x", scenePacket.Body)
	}
	inventory, _, err = repositories.Inventory.Load(context.Background(), "99")
	if err != nil || inventory.Slots["0:121"].Count != 3 {
		t.Fatalf("discard inventory=%+v err=%v", inventory.Slots, err)
	}
	dropObjectKey := binary.LittleEndian.Uint32(scenePacket.Body[6:10])
	drop := runtime.DropOwner.byObjectKey[dropObjectKey]
	if drop == nil || drop.Amount != 2 || drop.DiscardOrigin == nil ||
		drop.DiscardOrigin.AccountOwned || drop.DiscardOrigin.SourceSlot != 121 ||
		drop.DiscardOrigin.Stack.Count != 2 || drop.Status != runtimeDungeonDropAvailable {
		t.Fatalf("runtime drop=%+v", drop)
	}

	connection.write.Reset()
	if err := service.handleCurrentDungeonPickup(session, currentDungeonPickupTestBody(dropObjectKey)); err != nil {
		t.Fatal(err)
	}
	pickup, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	update, trailing = splitGameServerUpperPacket(t, rest)
	if pickup.Header.MsgID != currentDungeonPickupResultOpcode || len(trailing) != 0 ||
		binary.LittleEndian.Uint16(pickup.Body[16:18]) != 121 {
		t.Fatalf("pickup header=%+v body=%x trailing=%x", pickup.Header, pickup.Body, trailing)
	}
	if update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		binary.LittleEndian.Uint16(update.Body[3:5]) != 121 ||
		binary.LittleEndian.Uint32(update.Body[5:9]) != 3227 ||
		binary.LittleEndian.Uint32(update.Body[9:13]) != 5 {
		t.Fatalf("repick item update header=%+v body=%x", update.Header, update.Body)
	}
	inventory, _, err = repositories.Inventory.Load(context.Background(), "99")
	if err != nil || inventory.Slots["0:121"].Count != 5 {
		t.Fatalf("repick inventory=%+v err=%v", inventory.Slots, err)
	}
}

func TestCurrentDungeonDiscardAccountSharedRoundTripsSameSlot(t *testing.T) {
	service, runtime, repositories := prepareCurrentDungeonDropTest(t, 0, 3037)
	if err := repositories.AccountInventory.Save(context.Background(), dnfrepo.AccountInventoryRecord{
		AccountID: "account-1",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): currentDungeonDiscardTestStack(3037, 10),
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "dungeon-discard-account-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleTownSetUserPosition(session, buildTownPositionRequest(500, 250, 6, 100)); err != nil {
		t.Fatal(err)
	}
	body := buildCurrentDungeonDiscardTestRequest(500, 250, 0, 358, 2, 0)
	handled, err := service.handleCurrentDungeonDiscard(session, body)
	if err != nil || !handled {
		t.Fatalf("handled=%t err=%v", handled, err)
	}
	scenePacket, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	_, rest = splitGameServerUpperPacket(t, rest)
	update, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 {
		t.Fatalf("scene trailing=%x", trailing)
	}
	if update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		binary.LittleEndian.Uint16(update.Body[3:5]) != 358 ||
		binary.LittleEndian.Uint32(update.Body[5:9]) != 3037 ||
		binary.LittleEndian.Uint32(update.Body[9:13]) != 8 {
		t.Fatalf("account discard update header=%+v body=%x", update.Header, update.Body)
	}
	account, found, err := repositories.AccountInventory.Load(context.Background(), "account-1")
	if err != nil || !found || account.Slots["0:358"].Count != 8 {
		t.Fatalf("account after discard found=%t slots=%+v err=%v", found, account.Slots, err)
	}
	dropObjectKey := binary.LittleEndian.Uint32(scenePacket.Body[6:10])
	drop := runtime.DropOwner.byObjectKey[dropObjectKey]
	if drop == nil || drop.DiscardOrigin == nil || !drop.DiscardOrigin.AccountOwned ||
		drop.DiscardOrigin.SourceSlot != 358 {
		t.Fatalf("account drop=%+v", drop)
	}

	connection.write.Reset()
	if err := service.handleCurrentDungeonPickup(session, currentDungeonPickupTestBody(dropObjectKey)); err != nil {
		t.Fatal(err)
	}
	pickup, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	update, trailing = splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || binary.LittleEndian.Uint16(pickup.Body[16:18]) != 358 {
		t.Fatalf("pickup body=%x trailing=%x", pickup.Body, trailing)
	}
	if update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		binary.LittleEndian.Uint16(update.Body[3:5]) != 358 ||
		binary.LittleEndian.Uint32(update.Body[5:9]) != 3037 ||
		binary.LittleEndian.Uint32(update.Body[9:13]) != 10 {
		t.Fatalf("account repick update header=%+v body=%x", update.Header, update.Body)
	}
	account, found, err = repositories.AccountInventory.Load(context.Background(), "account-1")
	if err != nil || !found || account.Slots["0:358"].Count != 10 {
		t.Fatalf("account after repick found=%t slots=%+v err=%v", found, account.Slots, err)
	}
}

func TestCurrentDungeonDiscardWholeStackPublishesDeleteRow(t *testing.T) {
	service, runtime, repositories := prepareCurrentDungeonDropTest(t, 0, 3227)
	inventory, _, err := repositories.Inventory.Load(context.Background(), "99")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	inventory.Slots["0:80"] = currentDungeonDiscardTestStack(3227, 1)
	if err := repositories.Inventory.Save(context.Background(), inventory); err != nil {
		t.Fatal(err)
	}

	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "dungeon-discard-delete-update-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	handled, err := service.handleCurrentDungeonDiscard(
		session,
		buildCurrentDungeonDiscardTestRequest(500, 250, 0, 80, 1, 0),
	)
	if err != nil || !handled {
		t.Fatalf("handled=%t err=%v", handled, err)
	}
	_, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	_, rest = splitGameServerUpperPacket(t, rest)
	update, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 ||
		update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(update.Body) != 3+currentItemListEntryWireSize ||
		binary.LittleEndian.Uint16(update.Body[3:5]) != 80 ||
		binary.LittleEndian.Uint32(update.Body[5:9]) != math.MaxUint32 ||
		binary.LittleEndian.Uint32(update.Body[9:13]) != 0 {
		t.Fatalf("whole-stack delete update header=%+v body=%x trailing=%x", update.Header, update.Body, trailing)
	}
	inventory, _, err = repositories.Inventory.Load(context.Background(), "99")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := inventory.Slots["0:80"]; exists {
		t.Fatalf("whole-stack source slot still exists: %+v", inventory.Slots["0:80"])
	}
}

func TestCurrentDungeonDiscardIsDungeonOnlyAndRejectsBoundItems(t *testing.T) {
	service, runtime, repositories := prepareCurrentDungeonDropTest(t, 0, 3227)
	body := buildCurrentDungeonDiscardTestRequest(500, 250, 0, 121, 1, 0)
	if handled, err := service.handleCurrentDungeonDiscard(&gameSession{selectedCharacterID: 99}, body); handled || err != nil {
		t.Fatalf("town request intercepted handled=%t err=%v", handled, err)
	}

	stack := currentDungeonDiscardTestStack(3227, 2)
	stack.Bind = true
	inventory, _, err := repositories.Inventory.Load(context.Background(), "99")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	inventory.Slots["0:121"] = stack
	if err := repositories.Inventory.Save(context.Background(), inventory); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "dungeon-discard-bound-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleTownSetUserPosition(session, buildTownPositionRequest(500, 250, 6, 100)); err != nil {
		t.Fatal(err)
	}
	handled, err := service.handleCurrentDungeonDiscard(session, body)
	if err != nil || !handled {
		t.Fatalf("bound handled=%t err=%v", handled, err)
	}
	if connection.write.Len() != 0 || runtime.DropOwner != nil {
		t.Fatalf("bound discard wrote=%x drop_owner=%+v", connection.write.Bytes(), runtime.DropOwner)
	}
	inventory, _, err = repositories.Inventory.Load(context.Background(), "99")
	if err != nil || inventory.Slots["0:121"].Count != 2 {
		t.Fatalf("bound inventory=%+v err=%v", inventory.Slots, err)
	}
}

func buildCurrentDungeonDiscardTestRequest(
	positionX uint16,
	positionY uint16,
	listType byte,
	slot uint16,
	count uint32,
	mode byte,
) []byte {
	body := make([]byte, currentDungeonDiscardRequestSize)
	binary.LittleEndian.PutUint16(body[0:2], positionX)
	binary.LittleEndian.PutUint16(body[2:4], positionY)
	body[4] = listType
	binary.LittleEndian.PutUint16(body[5:7], slot)
	binary.LittleEndian.PutUint32(body[7:11], count)
	body[11] = mode
	return body
}

func currentDungeonDiscardTestStack(itemID int64, count int64) dnfrepo.ItemStack {
	stack := dnfrepo.ItemStack{
		ItemID: itemID,
		Count:  count,
		Extra: map[string]string{
			"item_kind":      string(dungeonDropItemStackable),
			"stackable_type": "[material]",
			"stack_limit":    "999",
			"pvf_path":       "stackable/material/test_drop.stk",
		},
	}
	entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, 121, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)
	return stack
}
