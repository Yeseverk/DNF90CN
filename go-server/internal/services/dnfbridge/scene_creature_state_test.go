package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestSelectedCreatureSceneReadyProjectionDoesNotRebuildActorAfterOp24(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "15",
		Level:       90,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{},
		Warehouse:   map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {
				SlotIndex: 26,
				ItemID:    400990167,
				RawEntry:  testCurrentEquippedCreatureRaw(26, 400990167, 1784487991),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.PetEntry{
			"1784487991": {
				PetKey:          "1784487991",
				CreatureKey:     1784487991,
				ItemID:          400990167,
				Name:            "PetA",
				NameRaw:         []byte("PetA"),
				Satiety:         14,
				Level:           3,
				Exp:             5,
				SourceListType:  3,
				SourceSlotIndex: 26,
			},
		},
		EquippedKey: "1784487991",
	}); err != nil {
		t.Fatal(err)
	}

	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "dnf:1",
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                           connection,
		connID:                         "scene-ready-pet-bind",
		channel:                        channelcatalog.Channel{ID: 19},
		residentChannel:                channelcatalog.Channel{ID: 19},
		selectedCharacterID:            19,
		townActorOwnerChannel:          19,
		selectedCreatureStateTableSent: true,
	}

	if err := service.sendSelectedCreatureSceneReadyProjection(session, "test_scene_ready"); err != nil {
		t.Fatal(err)
	}

	if got := connection.write.Bytes(); len(got) != 0 {
		t.Fatalf("scene-ready tail replaced the op24-bound local actor: %x", got)
	}
	if !session.selectedCreatureStateTableSent {
		t.Fatal("op105 state was not marked sent")
	}
}

func TestSelectedCreatureInitialStateAfterMode1PushesBarsBeforeSceneTail(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {
				SlotIndex: 26,
				ItemID:    400990167,
				RawEntry:  testCurrentEquippedCreatureEnchantRaw(26, 400990167, 37, 10008713, 2),
				Extra: map[string]string{
					"value_a":                  "10008713",
					"pet_enchant_card_item_id": "10008713",
					"byte_12":                  "2",
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {
				PetKey:          "37",
				CreatureKey:     37,
				ItemID:          400990167,
				Name:            "PetB",
				NameRaw:         []byte("PetB"),
				Satiety:         72,
				Level:           7,
				Exp:             0x12345678,
				SourceListType:  3,
				SourceSlotIndex: 26,
			},
		},
		EquippedKey: "37",
	}); err != nil {
		t.Fatal(err)
	}

	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "dnf:1",
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                connection,
		connID:              "initial-pet-bars",
		channel:             channelcatalog.Channel{ID: 19},
		residentChannel:     channelcatalog.Channel{ID: 19},
		selectedCharacterID: 19,
	}
	if err := service.sendSelectedCreatureInitialStateAfterMode1(session, "test_after_mode1"); err != nil {
		t.Fatal(err)
	}

	itemRefresh, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	assertEquippedCreatureItemRefreshPacket(t, itemRefresh, 400990167, 37, 10008713, 2)
	state, rest := splitGameServerUpperPacket(t, rest)
	if state.Header.MsgID != currentCreatureStateTableMsgID ||
		state.Header.Classification != 0 ||
		len(state.Body) < 20 ||
		state.Body[3] != 1 ||
		binary.LittleEndian.Uint32(state.Body[4:8]) != 37 ||
		state.Body[8] != 72 ||
		binary.LittleEndian.Uint32(state.Body[10:14]) != 0x12345678 ||
		state.Body[14] != 7 {
		t.Fatalf("initial creature table header=%+v body=%x", state.Header, state.Body)
	}
	growth, trailing := splitGameServerUpperPacket(t, rest)
	wantGrowth := []byte{0, 0, 0, 7, 0, 0x78, 0x56, 0x34, 0x12}
	if growth.Header.MsgID != currentCreatureGrowthMsgID ||
		growth.Header.Classification != 0 ||
		!bytes.Equal(growth.Body, wantGrowth) {
		t.Fatalf("initial creature growth header=%+v body=%x want=%x", growth.Header, growth.Body, wantGrowth)
	}
	if len(trailing) != 0 {
		t.Fatalf("unexpected trailing initial creature packets=%x", trailing)
	}
}

func TestSelectedCreatureStateAfterMoveAckSendsOnlyAbsolutePetState(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {
				SlotIndex: 26,
				ItemID:    400990167,
				RawEntry:  testCurrentEquippedCreatureEnchantRaw(26, 400990167, 37, 10008713, 2),
				Extra: map[string]string{
					"value_a":                  "10008713",
					"pet_enchant_card_item_id": "10008713",
					"byte_12":                  "2",
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {
				PetKey:          "37",
				CreatureKey:     37,
				ItemID:          400990167,
				Name:            "PetB",
				NameRaw:         []byte("PetB"),
				Satiety:         72,
				Level:           7,
				Exp:             0x12345678,
				SourceListType:  3,
				SourceSlotIndex: 26,
			},
		},
		EquippedKey: "37",
	}); err != nil {
		t.Fatal(err)
	}

	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "dnf:1",
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                connection,
		connID:              "pet-after-op19",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 19,
	}
	if err := service.sendSelectedCreatureStateAfterMoveAck(session, "test"); err != nil {
		t.Fatal(err)
	}

	itemRefresh, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	assertEquippedCreatureItemRefreshPacket(t, itemRefresh, 400990167, 37, 10008713, 2)
	state, rest := splitLongHengGameServerUpperPacket(t, rest)
	if state.Header.MsgID != currentCreatureStateTableMsgID || state.Header.Classification != 0 {
		t.Fatalf("first pet packet header=%+v body=%x", state.Header, state.Body)
	}
	growth, trailing := splitLongHengGameServerUpperPacket(t, rest)
	if growth.Header.MsgID != currentCreatureGrowthMsgID || growth.Header.Classification != 0 {
		t.Fatalf("second pet packet header=%+v body=%x", growth.Header, growth.Body)
	}
	if len(trailing) != 0 {
		t.Fatalf("pet move replayed actor/container packets after targeted item refresh=%x", trailing)
	}
}

func assertEquippedCreatureItemRefreshPacket(
	t *testing.T,
	packet dnfproto.ChannelPacket,
	itemID uint32,
	serial uint32,
	cardID uint32,
	upgrade byte,
) {
	t.Helper()
	if packet.Header.MsgID != 14 || packet.Header.Classification != 0 ||
		len(packet.Body) != 3+currentItemListEntryWireSize ||
		packet.Body[0] != currentSocketListEquipment ||
		binary.LittleEndian.Uint16(packet.Body[1:3]) != 1 {
		t.Fatalf("equipped creature item refresh header=%+v body=%x", packet.Header, packet.Body)
	}
	row := packet.Body[3:]
	if binary.LittleEndian.Uint16(row[0:2]) != 26 ||
		binary.LittleEndian.Uint32(row[2:6]) != itemID ||
		binary.LittleEndian.Uint32(row[6:10]) != serial ||
		binary.LittleEndian.Uint32(row[0x0E:0x12]) != cardID ||
		row[0x12] != upgrade ||
		binary.LittleEndian.Uint32(row[currentPetRemainSecondsOffset:currentPetRemainSecondsOffset+4]) != 0 ||
		binary.LittleEndian.Uint32(row[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != 0 {
		t.Fatalf("equipped creature item refresh row=%x", row)
	}
}

func testCurrentEquippedCreatureEnchantRaw(
	slot byte,
	itemID uint32,
	serial uint32,
	cardID uint32,
	upgrade byte,
) []byte {
	raw := testCurrentEquippedCreatureRaw(slot, itemID, serial)
	binary.LittleEndian.PutUint32(raw[16:20], cardID)
	raw[20] = upgrade
	return raw
}
