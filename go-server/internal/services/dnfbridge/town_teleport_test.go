package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func newTeleportPotionTest(t *testing.T) (*Service, *gameSession, dnfrepo.Group) {
	t.Helper()
	service, session, repositories := newTownMoveTest(t)
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "29",
		Slots: map[string]dnfrepo.ItemStack{
			"0:65": {
				ItemID:   2600014,
				Count:    2,
				RawEntry: make([]byte, currentItemListEntryWireSize),
				Extra:    map[string]string{"item_kind": "stackable", "pvf_path": "stackable/professional/potion/ptn_instantmovement.stk"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, _, _ := repositories.Inventory.Load(context.Background(), "29")
	stack := loaded.Slots["0:65"]
	stack.RawEntry[0x06] = 2
	loaded.Slots["0:65"] = stack
	if err := repositories.Inventory.Save(context.Background(), loaded); err != nil {
		t.Fatal(err)
	}
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":                                   "",
		"equipment/equipment.lst":                               "",
		"stackable/stackable.lst":                               "2600014 `professional/potion/ptn_instantmovement.stk`\n37 `cash/test.stk`\n",
		"stackable/professional/potion/ptn_instantmovement.stk": "[name] `Potion`\n[stackable type] `[teleport potion]`\n[stack limit] 100\n",
		"stackable/cash/test.stk":                               "[name] `Cash`\n[stackable type] `[material]`\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	service.pvfItemCatalog = catalog
	return service, session, repositories
}

func buildTeleportPotionRequest(slot int16, itemCode int32, townID byte) []byte {
	body := make([]byte, 8)
	binary.LittleEndian.PutUint16(body[0:2], uint16(slot))
	binary.LittleEndian.PutUint32(body[2:6], uint32(itemCode))
	body[7] = townID
	return body
}

func TestHandleTeleportPotionTeleportsToPVFGateAndConsumesOne(t *testing.T) {
	service, session, repositories := newTeleportPotionTest(t)

	if err := service.handleTeleportPotion(session, buildTeleportPotionRequest(65, 2600014, 40)); err != nil {
		t.Fatal(err)
	}

	conn := session.conn.(*bufferConn)
	transition, rest := splitTownTransitionAndPostState(
		t,
		conn.write.Bytes(),
		session,
		29,
		40,
		1,
		1016,
		189,
		0,
		3,
		townMoveSkillProjectionBody(t, repositories, "29"),
		false,
	)
	wantTransition, err := buildCurrentSceneTransitionBody(40, 1, []currentSceneTransitionRow{{
		ObjectOrResourceKey: 29,
		Value1:              uint16(1016),
		Value2:              uint16(189),
		Value3:              0,
		Value4:              3,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if transition.Header.MsgID != currentSceneTransitionMsgID || transition.Header.Classification != 0 || !bytes.Equal(transition.Body, wantTransition) {
		t.Fatalf("transition header=%+v body=%x want=%x", transition.Header, transition.Body, wantTransition)
	}

	var ack *dnfproto.ChannelPacket
	for len(rest) > 0 {
		packet, next := splitGameServerUpperPacket(t, rest)
		rest = next
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketTeleport) {
			ack = &packet
			break
		}
	}
	if ack == nil {
		t.Fatalf("teleport ack missing in stream")
	}
	wantAck := []byte{1, 65, 0, 0x4E, 0xAC, 0x27, 0x00}
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || !bytes.Equal(ack.Body, wantAck) {
		t.Fatalf("ack header=%+v body=%x want=%x", ack.Header, ack.Body, wantAck)
	}

	stored, found, err := repositories.Character.Load(context.Background(), "29")
	if err != nil || !found {
		t.Fatalf("load stored character found=%t err=%v", found, err)
	}
	if stored.Stats["town_id"] != 40 || stored.Stats["area_id"] != 1 || stored.Stats["pos_x"] != 1016 || stored.Stats["pos_y"] != 189 || stored.Stats["direction"] != 0 {
		t.Fatalf("stored town location=%+v", stored.Stats)
	}
	inventory, _, err := repositories.Inventory.Load(context.Background(), "29")
	if err != nil {
		t.Fatal(err)
	}
	potion := inventory.Slots["0:65"]
	if potion.Count != 1 || potion.RawEntry[0x06] != 1 {
		t.Fatalf("potion after consume=%+v", potion)
	}
}

func TestHandleTeleportPotionRejectsInvalidPotionAndTownWithoutMutation(t *testing.T) {
	service, session, repositories := newTeleportPotionTest(t)

	if err := service.handleTeleportPotion(session, buildTeleportPotionRequest(65, 37, 40)); err != nil {
		t.Fatal(err)
	}
	failure, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if failure.Header.MsgID != uint16(dnfenum.CmdPacketTeleport) || !bytes.Equal(failure.Body, []byte{0, 23}) || len(rest) != 0 {
		t.Fatalf("failure packet=%+v body=%x rest=%x", failure.Header, failure.Body, rest)
	}

	conn := session.conn.(*bufferConn)
	conn.write.Reset()
	if err := service.handleTeleportPotion(session, buildTeleportPotionRequest(65, 2600014, 99)); err != nil {
		t.Fatal(err)
	}
	failure, rest = splitGameServerUpperPacket(t, conn.write.Bytes())
	if failure.Header.MsgID != uint16(dnfenum.CmdPacketTeleport) || !bytes.Equal(failure.Body, []byte{0, 23}) || len(rest) != 0 {
		t.Fatalf("failure packet=%+v body=%x rest=%x", failure.Header, failure.Body, rest)
	}

	stored, _, _ := repositories.Character.Load(context.Background(), "29")
	if stored.Stats["town_id"] != 38 || stored.Stats["area_id"] != 1 {
		t.Fatalf("location mutated on rejection: %+v", stored.Stats)
	}
	inventory, _, _ := repositories.Inventory.Load(context.Background(), "29")
	if inventory.Slots["0:65"].Count != 2 {
		t.Fatalf("potion consumed on rejection: %+v", inventory.Slots["0:65"])
	}
}

func TestHandleTeleportPotionFallsBackToItemScan(t *testing.T) {
	service, session, repositories := newTeleportPotionTest(t)

	if err := service.handleTeleportPotion(session, buildTeleportPotionRequest(66, 2600014, 40)); err != nil {
		t.Fatal(err)
	}
	conn := session.conn.(*bufferConn)
	splitGameServerUpperPacket(t, conn.write.Bytes())
	inventory, _, _ := repositories.Inventory.Load(context.Background(), "29")
	if inventory.Slots["0:65"].Count != 1 {
		t.Fatalf("scan fallback did not consume the real potion stack: %+v", inventory.Slots)
	}
	stored, _, _ := repositories.Character.Load(context.Background(), "29")
	if stored.Stats["town_id"] != 40 {
		t.Fatalf("stored town location=%+v", stored.Stats)
	}
}
