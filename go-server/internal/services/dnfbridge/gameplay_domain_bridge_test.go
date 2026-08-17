package dnfbridge

import (
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestEmotionBridgePersistsThroughOwnerAndKeepsAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	service, session, connection := newGameplayDomainBridgeFixture(repos)
	var body [2]byte
	binary.LittleEndian.PutUint16(body[:], 7)
	if err := service.handleCurrentChangeEmotion(session, body[:]); err != nil {
		t.Fatalf("handle emotion: %v", err)
	}
	character, _, err := repos.Character.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load character: %v", err)
	}
	if character.Stats["emotion_index"] != 7 {
		t.Fatalf("emotion_index = %d", character.Stats["emotion_index"])
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketChangeEmotion) ||
		len(ack.Body) != 3 ||
		ack.Body[0] != 1 ||
		binary.LittleEndian.Uint16(ack.Body[1:3]) != 7 {
		t.Fatalf("ack header=%+v body=%x rest=%d", ack.Header, ack.Body, len(rest))
	}
}

func TestCloneTitleBridgePersistsThroughOwnerAndRefreshesAppearance(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Name:        "Actor19",
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"100:0": {ItemID: 123456, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"13": {SlotIndex: 13, ItemID: 654321},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	service, session, connection := newGameplayDomainBridgeFixture(repos)
	var body [4]byte
	binary.LittleEndian.PutUint32(body[:], 123456)
	if err := service.handleCurrentSetCloneTitle(session, body[:]); err != nil {
		t.Fatalf("handle clone title: %v", err)
	}
	character, _, err := repos.Character.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load character: %v", err)
	}
	if character.Stats["clone_title_item_id"] != 123456 {
		t.Fatalf("clone title = %d", character.Stats["clone_title_item_id"])
	}
	mode0, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if mode0.Header.Classification != 0 ||
		mode0.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(mode0.Body) == 0 ||
		mode0.Body[0] != 0 {
		t.Fatalf("mode0 refresh header=%+v body=%x rest=%d", mode0.Header, mode0.Body, len(rest))
	}
	assertCurrentTypedMode0FullAppearanceForTest(t, mode0.Body, "Actor19", 13, 123456)
	mode1, trailing := splitGameServerUpperPacket(t, rest)
	if mode1.Header.Classification != 0 ||
		mode1.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(mode1.Body) <= currentMode1CreateCountOffset ||
		mode1.Body[0] != 1 ||
		binary.LittleEndian.Uint16(mode1.Body[21:23]) != 19 {
		t.Fatalf("full mode1 rebind header=%+v body=%x", mode1.Header, mode1.Body)
	}
	if len(trailing) != 0 {
		t.Fatalf("unexpected clone-title op13/op357/trailing=%x", trailing)
	}
}

func TestTitleBookBridgeMovesThroughOwnerAndKeepsPacketOrder(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:42": {ItemID: 9001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	service, session, connection := newGameplayDomainBridgeFixture(repos)
	body := make([]byte, 20)
	binary.LittleEndian.PutUint32(body[0:4], 0)
	binary.LittleEndian.PutUint32(body[4:8], 42)
	binary.LittleEndian.PutUint32(body[8:12], 9001)
	binary.LittleEndian.PutUint32(body[12:16], 2)
	binary.LittleEndian.PutUint32(body[16:20], 7)
	if err := service.handleCurrentTitleBookPut(session, body); err != nil {
		t.Fatalf("handle title put: %v", err)
	}
	inventory, _, err := repos.Inventory.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if _, found := inventory.Slots["0:42"]; found ||
		inventory.Slots["100:2007"].ItemID != 9001 {
		t.Fatalf("slots = %+v", inventory.Slots)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != currentTitleBookPutMsgID ||
		len(ack.Body) != 17 ||
		ack.Body[0] != 1 ||
		binary.LittleEndian.Uint32(ack.Body[1:5]) != 0 ||
		binary.LittleEndian.Uint32(ack.Body[5:9]) != 42 ||
		binary.LittleEndian.Uint32(ack.Body[9:13]) != 2 ||
		binary.LittleEndian.Uint32(ack.Body[13:17]) != 7 {
		t.Fatalf("ack header=%+v body=%x", ack.Header, ack.Body)
	}
	update, rest := splitGameServerUpperPacket(t, rest)
	if update.Header.Classification != 0 ||
		update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf("update header=%+v", update.Header)
	}
	list, trailing := splitGameServerUpperPacket(t, rest)
	if list.Header.Classification != 0 ||
		list.Header.MsgID != currentTitleBookMsgID ||
		len(trailing) != 0 ||
		len(list.Body) != 37 ||
		binary.LittleEndian.Uint32(list.Body[33:37]) != 0 {
		t.Fatalf("list header=%+v trailing=%d", list.Header, len(trailing))
	}
}

func newGameplayDomainBridgeFixture(repos dnfrepo.Group) (*Service, *gameSession, *bufferConn) {
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repos, true
		},
	}
	session := &gameSession{
		conn:                connection,
		connID:              "gameplay-domain-bridge-test",
		selectedCharacterID: 19,
	}
	return service, session, connection
}
