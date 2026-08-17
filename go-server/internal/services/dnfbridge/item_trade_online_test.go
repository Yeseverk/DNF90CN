package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOnlineItemTradeProjectsOfferAndAtomicallyCommitsOnBothReady(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "1",
		Slots: map[string]dnfrepo.ItemStack{
			"0:97": {ItemID: 10008073, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "5", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers:      newOnlinePlayerManager(),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	firstConn, secondConn := &bufferConn{}, &bufferConn{}
	channel := channelcatalog.Channel{ID: 42, Name: "ch.42"}
	first := &gameSession{conn: firstConn, channel: channel, selectedCharacterID: 1}
	second := &gameSession{conn: secondConn, channel: channel, selectedCharacterID: 5}
	service.bindGameSessionCharacter(first, 1)
	service.bindGameSessionCharacter(second, 5)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1, TownID: 38, AreaID: 0, Session: first})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 5, TownID: 38, AreaID: 0, Session: second})
	service.beginOnlineItemTrade(first, second)

	// op19 carries an opaque live-item instance field, not the PVF item ID or a
	// durable repository quantity. A mismatch must not reject the authoritative
	// source slot.
	request := buildOnlineItemTradeMoveRequest(0, 97, 0x12345678, 1, currentItemTradeListType, 0)
	handled, err := service.handleAlignedGameCommand(first, byte(dnfenum.GameCmdCommand), currentItemTradeMoveMsg, request)
	if err != nil || !handled {
		t.Fatalf("offer handled=%t err=%v", handled, err)
	}
	ack, rest := splitGameServerUpperPacket(t, firstConn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != currentItemTradeMoveMsg ||
		!bytes.Equal(ack.Body, []byte{1, 0, 97, 0, 1, 0, 0, 0, 4, 3, 0}) || len(rest) != 0 {
		t.Fatalf("offer ack class=%d msg=%d body=% X trailing=%d", ack.Header.Classification, ack.Header.MsgID, ack.Body, len(rest))
	}
	change, rest := splitGameServerUpperPacket(t, secondConn.write.Bytes())
	if change.Header.Classification != 0 || change.Header.MsgID != currentItemTradeChangeMsg || len(change.Body) != currentItemListEntryWireSize ||
		binary.LittleEndian.Uint16(change.Body[:2]) != 3 || binary.LittleEndian.Uint32(change.Body[2:6]) != 10008073 || len(rest) != 0 {
		t.Fatalf("peer offer class=%d msg=%d body_len=%d slot=%d item=%d trailing=%d", change.Header.Classification, change.Header.MsgID,
			len(change.Body), binary.LittleEndian.Uint16(change.Body[:2]), binary.LittleEndian.Uint32(change.Body[2:6]), len(rest))
	}

	firstConn.write.Reset()
	secondConn.write.Reset()
	for _, participant := range []*gameSession{first, second} {
		handled, err = service.handleAlignedGameCommand(participant, byte(dnfenum.GameCmdCommand), currentItemTradeReadyMsg, []byte{currentItemTradeAddRequest})
		if err != nil || !handled {
			t.Fatalf("adding done char=%d handled=%t err=%v", participant.selectedCharacterID, handled, err)
		}
	}
	if service.currentOnlineItemTrade(1) == nil || service.currentOnlineItemTrade(5) == nil {
		t.Fatal("both adding-done states committed the trade before confirmation")
	}
	assertOnlineItemTradeStatePacket(t, firstConn.write.Bytes(), 1, currentItemTradeAddingDone)
	assertOnlineItemTradeStatePacket(t, firstConn.write.Bytes(), 5, currentItemTradeAddingDone)
	assertOnlineItemTradeStatePacket(t, secondConn.write.Bytes(), 1, currentItemTradeAddingDone)
	assertOnlineItemTradeStatePacket(t, secondConn.write.Bytes(), 5, currentItemTradeAddingDone)

	firstConn.write.Reset()
	secondConn.write.Reset()
	handled, err = service.handleAlignedGameCommand(first, byte(dnfenum.GameCmdCommand), currentItemTradeReadyMsg, []byte{currentItemTradeOfferAck})
	if err != nil || !handled {
		t.Fatalf("offer-change ack handled=%t err=%v", handled, err)
	}
	offerAck, rest := splitGameServerUpperPacket(t, firstConn.write.Bytes())
	if offerAck.Header.Classification != dnfproto.DefaultChannelClassification || offerAck.Header.MsgID != currentItemTradeReadyMsg ||
		!bytes.Equal(offerAck.Body, []byte{1}) || len(rest) != 0 {
		t.Fatalf("offer-change ack class=%d msg=%d body=% X trailing=%d", offerAck.Header.Classification, offerAck.Header.MsgID, offerAck.Body, len(rest))
	}
	if len(secondConn.write.Bytes()) != 0 || service.currentOnlineItemTrade(1).states[1] != currentItemTradeAddingDone {
		t.Fatalf("offer-change ack leaked to peer or mutated state: peer=%x state=%d", secondConn.write.Bytes(), service.currentOnlineItemTrade(1).states[1])
	}

	firstConn.write.Reset()
	secondConn.write.Reset()
	handled, err = service.handleAlignedGameCommand(first, byte(dnfenum.GameCmdCommand), currentItemTradeReadyMsg, []byte{currentItemTradeReady})
	if err != nil || !handled {
		t.Fatalf("first ready handled=%t err=%v", handled, err)
	}
	if service.currentOnlineItemTrade(1) == nil || service.currentOnlineItemTrade(5) == nil {
		t.Fatal("first final confirmation committed before the peer confirmed")
	}
	assertOnlineItemTradeStateAbsent(t, firstConn.write.Bytes(), currentItemTradeReady)
	assertOnlineItemTradeStateAbsent(t, secondConn.write.Bytes(), currentItemTradeReady)
	handled, err = service.handleAlignedGameCommand(second, byte(dnfenum.GameCmdCommand), currentItemTradeReadyMsg, []byte{currentItemTradeReady})
	if err != nil || !handled {
		t.Fatalf("second ready handled=%t err=%v", handled, err)
	}
	assertOnlineItemTradeStateAbsent(t, firstConn.write.Bytes(), currentItemTradeReady)
	assertOnlineItemTradeStateAbsent(t, secondConn.write.Bytes(), currentItemTradeReady)

	firstInventory, _, err := repositories.Inventory.Load(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	secondInventory, _, err := repositories.Inventory.Load(ctx, "5")
	if err != nil {
		t.Fatal(err)
	}
	if _, present := firstInventory.Slots["0:97"]; present {
		t.Fatalf("source inventory still contains traded item: %+v", firstInventory.Slots)
	}
	if got := secondInventory.Slots["0:65"]; got.ItemID != 10008073 || got.Count != 1 {
		t.Fatalf("recipient item = %+v", got)
	}
	assertOnlineItemTradeFinishPacket(t, firstConn.write.Bytes(), 0)
	assertOnlineItemTradeFinishPacket(t, secondConn.write.Bytes(), 1)
	if service.currentOnlineItemTrade(1) != nil || service.currentOnlineItemTrade(5) != nil {
		t.Fatal("completed trade remained registered")
	}
}

func TestOnlineItemTradeCancelReturnsAssignedItemRowAndClearsState(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "1",
		Slots: map[string]dnfrepo.ItemStack{
			"0:97": {ItemID: 10008073, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers:      newOnlinePlayerManager(),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	firstConn, secondConn := &bufferConn{}, &bufferConn{}
	channel := channelcatalog.Channel{ID: 42, Name: "ch.42"}
	first := &gameSession{conn: firstConn, channel: channel, selectedCharacterID: 1}
	second := &gameSession{conn: secondConn, channel: channel, selectedCharacterID: 5}
	service.bindGameSessionCharacter(first, 1)
	service.bindGameSessionCharacter(second, 5)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1, TownID: 38, AreaID: 0, Session: first})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 5, TownID: 38, AreaID: 0, Session: second})
	service.beginOnlineItemTrade(first, second)

	request := buildOnlineItemTradeMoveRequest(0, 97, 0x12345678, 1, currentItemTradeListType, 0)
	if handled, err := service.handleAlignedGameCommand(first, byte(dnfenum.GameCmdCommand), currentItemTradeMoveMsg, request); err != nil || !handled {
		t.Fatalf("offer handled=%t err=%v", handled, err)
	}
	firstConn.write.Reset()
	secondConn.write.Reset()
	service.cancelOnlineItemTrade(service.currentOnlineItemTrade(1), "test_cancel")

	firstCancel, rest := splitGameServerUpperPacket(t, firstConn.write.Bytes())
	if firstCancel.Header.Classification != 0 || firstCancel.Header.MsgID != currentItemTradeCancelMsg ||
		!bytes.Equal(firstCancel.Body, []byte{1, 0, 3, 0, 0, 97, 0}) || len(rest) != 0 {
		t.Fatalf("offerer cancel class=%d msg=%d body=% X trailing=%d", firstCancel.Header.Classification, firstCancel.Header.MsgID, firstCancel.Body, len(rest))
	}
	secondCancel, rest := splitGameServerUpperPacket(t, secondConn.write.Bytes())
	if secondCancel.Header.Classification != 0 || secondCancel.Header.MsgID != currentItemTradeCancelMsg ||
		!bytes.Equal(secondCancel.Body, []byte{0, 0}) || len(rest) != 0 {
		t.Fatalf("peer cancel class=%d msg=%d body=% X trailing=%d", secondCancel.Header.Classification, secondCancel.Header.MsgID, secondCancel.Body, len(rest))
	}
	if service.currentOnlineItemTrade(1) != nil || service.currentOnlineItemTrade(5) != nil {
		t.Fatal("cancelled trade remained registered")
	}
}

func TestOnlineItemTradeProjectsAndAtomicallyCommitsGold(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, record := range []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: "a", Stats: map[string]int64{"gold": 250_000}},
		{CharacterID: "5", AccountID: "b", Stats: map[string]int64{"gold": 40_000}},
	} {
		if err := repositories.Character.Save(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers:      newOnlinePlayerManager(),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	firstConn, secondConn := &bufferConn{}, &bufferConn{}
	channel := channelcatalog.Channel{ID: 42, Name: "ch.42"}
	first := &gameSession{conn: firstConn, channel: channel, selectedCharacterID: 1}
	second := &gameSession{conn: secondConn, channel: channel, selectedCharacterID: 5}
	service.bindGameSessionCharacter(first, 1)
	service.bindGameSessionCharacter(second, 5)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1, TownID: 38, AreaID: 0, Session: first})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 5, TownID: 38, AreaID: 0, Session: second})
	service.beginOnlineItemTrade(first, second)

	request := buildOnlineItemTradeMoveRequest(0, 0, 0, 100_000, currentItemTradeListType, 0)
	handled, err := service.handleAlignedGameCommand(first, byte(dnfenum.GameCmdCommand), currentItemTradeMoveMsg, request)
	if err != nil || !handled {
		t.Fatalf("gold offer handled=%t err=%v", handled, err)
	}
	ack, rest := splitGameServerUpperPacket(t, firstConn.write.Bytes())
	if ack.Header.MsgID != currentItemTradeMoveMsg || ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, []byte{1, 0, 0, 0, 0xa0, 0x86, 0x01, 0, 4, 0, 0}) || len(rest) != 0 {
		t.Fatalf("gold ack class=%d msg=%d body=% X trailing=%d", ack.Header.Classification, ack.Header.MsgID, ack.Body, len(rest))
	}
	change, rest := splitGameServerUpperPacket(t, secondConn.write.Bytes())
	if change.Header.MsgID != currentItemTradeChangeMsg || change.Header.Classification != 0 || len(change.Body) != currentItemListEntryWireSize ||
		binary.LittleEndian.Uint16(change.Body[0:2]) != 0 || binary.LittleEndian.Uint32(change.Body[2:6]) != 0 ||
		binary.LittleEndian.Uint32(change.Body[6:10]) != 100_000 || len(rest) != 0 {
		t.Fatalf("peer gold row class=%d msg=%d body=% X trailing=%d", change.Header.Classification, change.Header.MsgID, change.Body[:10], len(rest))
	}

	firstConn.write.Reset()
	secondConn.write.Reset()
	for _, state := range []byte{currentItemTradeAddRequest, currentItemTradeReady} {
		if _, err := service.handleAlignedGameCommand(first, byte(dnfenum.GameCmdCommand), currentItemTradeReadyMsg, []byte{state}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.handleAlignedGameCommand(second, byte(dnfenum.GameCmdCommand), currentItemTradeReadyMsg, []byte{state}); err != nil {
			t.Fatal(err)
		}
	}
	firstCharacter, _, _ := repositories.Character.Load(ctx, "1")
	secondCharacter, _, _ := repositories.Character.Load(ctx, "5")
	if firstCharacter.Stats["gold"] != 150_000 || secondCharacter.Stats["gold"] != 140_000 {
		t.Fatalf("gold after trade first=%d second=%d", firstCharacter.Stats["gold"], secondCharacter.Stats["gold"])
	}
	assertOnlineItemTradeFinishPacket(t, firstConn.write.Bytes(), 0)
	assertOnlineItemTradeFinishPacket(t, secondConn.write.Bytes(), 0)
}

func buildOnlineItemTradeMoveRequest(sourceList byte, sourceSlot int16, itemID, count int32, destinationList byte, destinationSlot int16) []byte {
	body := make([]byte, 28)
	body[0] = sourceList
	binary.LittleEndian.PutUint16(body[1:3], uint16(sourceSlot))
	binary.LittleEndian.PutUint32(body[3:7], uint32(itemID))
	binary.LittleEndian.PutUint32(body[7:11], uint32(count))
	body[11] = destinationList
	binary.LittleEndian.PutUint16(body[12:14], uint16(destinationSlot))
	return body
}

func assertOnlineItemTradeFinishPacket(t *testing.T, data []byte, wantCount uint16) {
	t.Helper()
	for len(data) > 0 {
		packet, rest := splitGameServerUpperPacket(t, data)
		if packet.Header.Classification == 0 && packet.Header.MsgID == currentItemTradeFinishMsg {
			if len(packet.Body) < 2 || binary.LittleEndian.Uint16(packet.Body[:2]) != wantCount {
				t.Fatalf("finish body = % X, want count %d", packet.Body, wantCount)
			}
			if wantCount == 1 && (len(packet.Body) != 6 || binary.LittleEndian.Uint16(packet.Body[2:4]) != 3 || binary.LittleEndian.Uint16(packet.Body[4:6]) != 65) {
				t.Fatalf("finish transfer body = % X", packet.Body)
			}
			return
		}
		data = rest
	}
	t.Fatal("finish packet not sent")
}

func assertOnlineItemTradeStatePacket(t *testing.T, data []byte, wantCharacterID uint16, wantState byte) {
	t.Helper()
	for len(data) > 0 {
		packet, rest := splitGameServerUpperPacket(t, data)
		if packet.Header.Classification == 0 && packet.Header.MsgID == currentItemTradeStateMsg &&
			len(packet.Body) == 3 && binary.LittleEndian.Uint16(packet.Body[:2]) == wantCharacterID &&
			packet.Body[2] == wantState {
			return
		}
		data = rest
	}
	t.Fatalf("trade state packet char=%d state=%d not sent", wantCharacterID, wantState)
}

func assertOnlineItemTradeStateAbsent(t *testing.T, data []byte, unwantedState byte) {
	t.Helper()
	for len(data) > 0 {
		packet, rest := splitGameServerUpperPacket(t, data)
		if packet.Header.Classification == 0 && packet.Header.MsgID == currentItemTradeStateMsg &&
			len(packet.Body) == 3 && packet.Body[2] == unwantedState {
			t.Fatalf("unexpected item-trade op17 state %d body=% X", unwantedState, packet.Body)
		}
		data = rest
	}
}
