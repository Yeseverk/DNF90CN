package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestSendAlignedUpperResponsesSuppressesUnsafeEquipmentSlotPostActionBeforeMode0Appearance(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "15",
		Level:       1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"13": {
				SlotIndex: 13,
				ItemID:    100312425,
				RawEntry:  make([]byte, currentItemListEntryWireSize),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{}, Warehouse: map[string]dnfrepo.ItemStack{}}); err != nil {
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
		conn:                            connection,
		connID:                          "aligned-equipment-post-action",
		channel:                         channelcatalog.Channel{ID: 19},
		selectedCharacterID:             19,
		initialTownRouteCharacterID:     19,
		initialTownRouteStage:           currentInitialTownRoutePlayerStateSent,
		townSceneReadyCharacterID:       19,
		connectionTownActorOwnerChannel: 19,
		townActorOwnerChannel:           19,
	}
	ackBody := []byte{1, 0, 9, 0, 1, 0, 0, 0, 3, 12, 0}
	result := alignedcmd.Result{
		Operation: "move_itemspace",
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          19,
			Body:           ackBody,
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		PostActions: []alignedcmd.PostAction{
			alignedcmd.PostActionRefreshSelectedItemContainers,
			alignedcmd.PostActionRefreshSelectedEquipmentSlots,
			alignedcmd.PostActionRefreshSelectedActorAppearance,
		},
	}

	if err := service.sendAlignedUpperResponses(session, result); err != nil {
		t.Fatalf("sendAlignedUpperResponses error = %v", err)
	}
	first, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if first.Header.MsgID != 19 || first.Header.Classification != dnfproto.DefaultChannelClassification || len(first.Body) != len(ackBody)+3 || !bytes.Equal(first.Body[:3], []byte{0, 0, 0}) || !bytes.Equal(first.Body[3:], ackBody) {
		t.Fatalf("first packet header=%+v body=%x", first.Header, first.Body)
	}
	for _, wantListType := range currentSelectedItemListTypes {
		refresh, remaining := splitCurrentSceneItemListPacket(t, rest)
		if refresh.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) || refresh.Header.Classification != 0 || len(refresh.Body) == 0 || refresh.Body[0] != wantListType {
			t.Fatalf("list%d refresh header=%+v body=%x", wantListType, refresh.Header, refresh.Body)
		}
		rest = remaining
	}
	second, trailing := splitGameServerUpperPacket(t, rest)
	const codecPrefix = 3
	const currentObjectKeyOffset = 0x4c
	if second.Header.MsgID != 2 || second.Header.Classification != 0 ||
		len(second.Body) < codecPrefix+currentObjectKeyOffset+2 ||
		!bytes.Equal(second.Body[:codecPrefix], []byte{0, 0, 0}) ||
		second.Body[codecPrefix] != 0 ||
		binary.LittleEndian.Uint16(second.Body[codecPrefix+currentObjectKeyOffset:]) != 19 {
		t.Fatalf("second packet header=%+v body=%x", second.Header, second.Body)
	}
	if len(trailing) != 0 {
		t.Fatalf("unexpected trailing bytes: %x", trailing)
	}
}

func TestSendAlignedUpperResponsesDoesNotSendPartialListsOrStaleMode0WhenRefreshPlanFails(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "0",
		Level:       1,
		Stats:       map[string]int64{},
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
		Entries:     map[string]dnfrepo.EquipmentEntry{},
	}); err != nil {
		t.Fatal(err)
	}
	failing := &failAfterInventoryLoads{
		InventoryRepository: repositories.Inventory,
		failAfter:           2,
	}
	repositories.Inventory = failing

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
		conn:                            connection,
		connID:                          "aligned-refresh-plan-failure",
		channel:                         channelcatalog.Channel{ID: 253},
		selectedCharacterID:             19,
		initialTownRouteCharacterID:     19,
		initialTownRouteStage:           currentInitialTownRoutePlayerStateSent,
		townSceneReadyCharacterID:       19,
		connectionTownActorOwnerChannel: 253,
		townActorOwnerChannel:           253,
	}
	ackBody := []byte{1, 0, 9, 0, 1, 0, 0, 0, 3, 11, 0}
	result := alignedcmd.Result{
		Operation: "move_itemspace",
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          19,
			Body:           ackBody,
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		PostActions: []alignedcmd.PostAction{
			alignedcmd.PostActionRefreshSelectedItemContainers,
			alignedcmd.PostActionRefreshSelectedActorAppearance,
		},
	}

	if err := service.sendAlignedUpperResponses(session, result); err != nil {
		t.Fatalf("post-commit projection failure disconnected session: %v", err)
	}
	ack, trailing := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != 19 ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, ackBody) {
		t.Fatalf("ACK header=%+v body=%x", ack.Header, ack.Body)
	}
	if len(trailing) != 0 {
		t.Fatalf("failed five-list plan leaked partial list or stale mode0=%x", trailing)
	}
	if failing.loads <= failing.failAfter {
		t.Fatalf("inventory failure was not exercised loads=%d", failing.loads)
	}
}

func TestSendAlignedUpperResponsesKeepsCommittedAckWhenMode0WearStatusCannotLoadCharacter(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "20",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Level:       90,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {SlotIndex: 26, ItemID: 9001},
		},
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
		conn:                        connection,
		connID:                      "aligned-equipment-missing-actor-post-action",
		channel:                     channelcatalog.Channel{ID: 19},
		selectedCharacterID:         19,
		initialTownRouteCharacterID: 19,
		initialTownRouteStage:       currentInitialTownRoutePlayerStateSent,
		townSceneReadyCharacterID:   19,
		townActorOwnerChannel:       currentSceneObjectContext,
	}
	ackBody := []byte{1, 0, 9, 0, 1, 0, 0, 0, 3, 12, 0}
	result := alignedcmd.Result{
		Operation: "move_itemspace",
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          19,
			Body:           ackBody,
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		PostActions: []alignedcmd.PostAction{
			alignedcmd.PostActionRefreshSelectedActorAppearance,
		},
	}

	if err := service.sendAlignedUpperResponses(session, result); err != nil {
		t.Fatalf("committed op19 ACK became fatal after deferred refresh: %v", err)
	}
	ack, trailing := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != 19 ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, ackBody) ||
		len(trailing) != 0 {
		t.Fatalf("committed ACK header=%+v body=%x trailing=%x", ack.Header, ack.Body, trailing)
	}
}

func TestCurrentTownSelfActorAppearanceReadyRequiresFinalizedLocalActor(t *testing.T) {
	session := &gameSession{
		selectedCharacterID:       19,
		townSceneReadyCharacterID: 19,
		townActorOwnerChannel:     currentSceneObjectContext,
	}
	if !currentTownSelfActorAppearanceReady(session) {
		t.Fatal("finalized local selected actor was not appearance-ready")
	}
	session.connectionTownActorOwnerChannel = 253
	session.townActorOwnerChannel = 253
	if !currentTownSelfActorAppearanceReady(session) {
		t.Fatal("finalized CHANNELINFO-owned selected actor was not appearance-ready")
	}
	session.townActorOwnerChannel = 7
	if currentTownSelfActorAppearanceReady(session) {
		t.Fatal("remote scene-context actor was appearance-ready")
	}
	session.townActorOwnerChannel = 253
	session.townSceneReadyCharacterID = 20
	if currentTownSelfActorAppearanceReady(session) {
		t.Fatal("stale different-character scene was appearance-ready")
	}
}

func TestSelectedActorAppearanceRefreshClearsUnequippedSlotsWithMode0ThenFullMode1(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "15",
		Level:       90,
	}
	if err := repositories.Character.Save(ctx, character); err != nil {
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
		Entries:     map[string]dnfrepo.EquipmentEntry{},
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
		connID:              "aligned-unequip-mode0-refresh",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 19,
	}
	if err := service.sendSelectedActorAppearanceRefresh(session, "move_itemspace", "test_unequip"); err != nil {
		t.Fatal(err)
	}

	mode0Packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	const codecPrefix = 3
	if mode0Packet.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		mode0Packet.Header.Classification != 0 ||
		len(mode0Packet.Body) <= codecPrefix ||
		!bytes.Equal(mode0Packet.Body[:codecPrefix], []byte{0, 0, 0}) {
		t.Fatalf("unequip mode0 header=%+v body=%x", mode0Packet.Header, mode0Packet.Body)
	}
	assertCurrentTypedMode0FullAppearanceForTest(t, mode0Packet.Body[codecPrefix:], "Actor19", 3, currentActorMode0AppearanceEmptyItem)

	mode1Packet, trailing := splitGameServerUpperPacket(t, rest)
	if mode1Packet.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		mode1Packet.Header.Classification != 0 ||
		len(mode1Packet.Body) <= codecPrefix+currentMode1CreateCountOffset ||
		!bytes.Equal(mode1Packet.Body[:codecPrefix], []byte{0, 0, 0}) {
		t.Fatalf("unequip full mode1 header=%+v body=%x", mode1Packet.Header, mode1Packet.Body)
	}
	mode1 := mode1Packet.Body[codecPrefix:]
	if mode1[0] != 1 ||
		binary.LittleEndian.Uint16(mode1[21:23]) != 19 ||
		mode1[currentMode1CreateCountOffset] != 0 {
		t.Fatalf("unequip full mode1 body=%x", mode1)
	}
	if len(trailing) != 0 {
		t.Fatalf("unexpected unequip op13/op357/trailing=%x", trailing)
	}
}

func TestSelectedActorMode1AppearanceRefreshClearsUnequippedSlots(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "15",
		Level:       90,
	}
	if err := repositories.Character.Save(ctx, character); err != nil {
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
		Entries:     map[string]dnfrepo.EquipmentEntry{},
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
		connID:              "aligned-unequip-mode1-refresh",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 19,
	}
	if err := service.sendSelectedActorMode1AppearanceRefresh(session, "move_itemspace", "test_unequip"); err != nil {
		t.Fatal(err)
	}

	packet, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	const codecPrefix = 3
	if packet.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		packet.Header.Classification != 0 ||
		len(packet.Body) <= codecPrefix+currentMode1CreateCountOffset ||
		!bytes.Equal(packet.Body[:codecPrefix], []byte{0, 0, 0}) {
		t.Fatalf("unequip mode1 refresh header=%+v body=%x", packet.Header, packet.Body)
	}
	mode1 := packet.Body[codecPrefix:]
	if mode1[0] != 1 ||
		binary.LittleEndian.Uint16(mode1[21:23]) != 19 ||
		mode1[currentMode1CreateCountOffset] != 0 {
		t.Fatalf("unequip mode1 body=%x", mode1)
	}
	if len(trailing) != 0 {
		t.Fatalf("unexpected unequip mode1 trailing=%x", trailing)
	}
}

func TestSendAlignedUpperResponsesRefreshesOnlyUsedStackableRow(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {
				ItemID: 500,
				Count:  108,
				Extra: map[string]string{
					"item_kind":       "stackable",
					"amount":          "117",
					"amount_or_count": "117",
				},
			},
		},
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
	session := &gameSession{conn: connection, connID: "stackable-use-incremental", selectedCharacterID: 19}
	ackBody := []byte{1, 5, 0, 0, 0x11, 0x22, 0x33, 0x44, 0xF4, 0x01, 0, 0}
	result := alignedcmd.Result{
		Operation: "use_stackable",
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          uint16(dnfenum.CmdPacketUseStackable),
			Body:           ackBody,
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		ItemSlotRefreshes: []alignedcmd.ItemSlotRefresh{{ListType: 0, SlotIndex: 5}},
	}

	if err := service.sendAlignedUpperResponses(session, result); err != nil {
		t.Fatalf("sendAlignedUpperResponses error = %v", err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketUseStackable) {
		t.Fatalf("ack header=%+v", ack.Header)
	}
	update, trailing := splitGameServerUpperPacket(t, rest)
	const codecPrefix = 3
	updateBody := update.Body[codecPrefix:]
	if update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		update.Header.Classification != 0 ||
		len(updateBody) != 3+currentItemListEntryWireSize ||
		updateBody[0] != 0 ||
		binary.LittleEndian.Uint16(updateBody[1:3]) != 1 {
		t.Fatalf("incremental update header=%+v body=%x", update.Header, update.Body)
	}
	row := updateBody[3:]
	if binary.LittleEndian.Uint16(row[0:2]) != 5 ||
		binary.LittleEndian.Uint32(row[2:6]) != 500 ||
		binary.LittleEndian.Uint32(row[6:10]) != 108 {
		t.Fatalf("incremental row=%x", row)
	}
	if len(trailing) != 0 {
		t.Fatalf("unexpected full-list/trailing packets=%x", trailing)
	}
}

func TestSendAlignedUpperResponsesRefreshesRemovedStackableAndReviveWalletRows(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:1": {ItemID: 1, Count: 4},
		},
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
	session := &gameSession{conn: connection, connID: "revive-use-incremental", selectedCharacterID: 19}
	result := alignedcmd.Result{
		Operation: "use_stackable",
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          uint16(dnfenum.CmdPacketUseStackable),
			Body:           []byte{1, 5, 0, 0, 0x11, 0x22, 0x33, 0x44, 42, 0, 0, 0},
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		ItemSlotRefreshes: []alignedcmd.ItemSlotRefresh{
			{ListType: 0, SlotIndex: 5},
			{ListType: 0, SlotIndex: 1},
		},
	}

	if err := service.sendAlignedUpperResponses(session, result); err != nil {
		t.Fatalf("sendAlignedUpperResponses error = %v", err)
	}
	_, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	update, trailing := splitGameServerUpperPacket(t, rest)
	const codecPrefix = 3
	updateBody := update.Body[codecPrefix:]
	if len(updateBody) != 3+2*currentItemListEntryWireSize ||
		binary.LittleEndian.Uint16(updateBody[1:3]) != 2 {
		t.Fatalf("incremental update body=%x", update.Body)
	}
	removed := updateBody[3 : 3+currentItemListEntryWireSize]
	wallet := updateBody[3+currentItemListEntryWireSize:]
	if binary.LittleEndian.Uint16(removed[0:2]) != 5 ||
		binary.LittleEndian.Uint32(removed[2:6]) != math.MaxUint32 ||
		binary.LittleEndian.Uint32(removed[6:10]) != 0 {
		t.Fatalf("removed source row=%x", removed)
	}
	if binary.LittleEndian.Uint16(wallet[0:2]) != 1 ||
		binary.LittleEndian.Uint32(wallet[2:6]) != 1 ||
		binary.LittleEndian.Uint32(wallet[6:10]) != 4 {
		t.Fatalf("wallet row=%x", wallet)
	}
	if len(trailing) != 0 {
		t.Fatalf("unexpected full-list/trailing packets=%x", trailing)
	}
}

func TestSendAlignedUpperResponsesRefreshesCrossContainerMergeRows(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{},
		Warehouse: map[string]dnfrepo.ItemStack{
			"2:8": {ItemID: 3227, Count: 5},
		},
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
	session := &gameSession{conn: connection, connID: "cargo-merge-incremental", selectedCharacterID: 19}
	result := alignedcmd.Result{
		Operation: "move_itemspace",
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          uint16(dnfenum.CmdPacketMoveItemspace),
			Body:           []byte{1, 0, 65, 0, 2, 0, 0, 0, 2, 8, 0},
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		ItemSlotRefreshes: []alignedcmd.ItemSlotRefresh{
			{ListType: 0, SlotIndex: 65},
			{ListType: 2, SlotIndex: 8},
		},
	}

	if err := service.sendAlignedUpperResponses(session, result); err != nil {
		t.Fatalf("sendAlignedUpperResponses error = %v", err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, append([]byte{0, 0, 0}, result.UpperResponses[0].Body...)) {
		t.Fatalf("move ACK header=%+v body=%x", ack.Header, ack.Body)
	}
	sourceUpdate, rest := splitGameServerUpperPacket(t, rest)
	destinationUpdate, trailing := splitGameServerUpperPacket(t, rest)
	const codecPrefix = 3
	sourceBody := sourceUpdate.Body[codecPrefix:]
	destinationBody := destinationUpdate.Body[codecPrefix:]
	if sourceUpdate.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(sourceBody) != 3+currentItemListEntryWireSize ||
		sourceBody[0] != 0 ||
		binary.LittleEndian.Uint16(sourceBody[1:3]) != 1 {
		t.Fatalf("source update header=%+v body=%x", sourceUpdate.Header, sourceUpdate.Body)
	}
	source := sourceBody[3:]
	if binary.LittleEndian.Uint16(source[0:2]) != 65 ||
		binary.LittleEndian.Uint32(source[2:6]) != math.MaxUint32 ||
		binary.LittleEndian.Uint32(source[6:10]) != 0 {
		t.Fatalf("source deletion row=%x", source)
	}
	if destinationUpdate.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(destinationBody) != 3+currentItemListEntryWireSize ||
		destinationBody[0] != 2 ||
		binary.LittleEndian.Uint16(destinationBody[1:3]) != 1 {
		t.Fatalf("destination update header=%+v body=%x", destinationUpdate.Header, destinationUpdate.Body)
	}
	destination := destinationBody[3:]
	if binary.LittleEndian.Uint16(destination[0:2]) != 8 ||
		binary.LittleEndian.Uint32(destination[2:6]) != 3227 ||
		binary.LittleEndian.Uint32(destination[6:10]) != 5 {
		t.Fatalf("destination merged row=%x", destination)
	}
	if len(trailing) != 0 {
		t.Fatalf("unexpected full-list/trailing packets=%x", trailing)
	}
}

func TestSendAlignedPetWearAppendsOnlyCreatureState(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "15",
		Level:       1,
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
				ItemID:    63000,
				RawEntry:  testCurrentEquippedCreatureEnchantRaw(26, 63000, 37, 10008705, 0),
				Extra: map[string]string{
					"value_a":                  "10008705",
					"pet_enchant_card_item_id": "10008705",
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
				ItemID:          63000,
				Name:            "Pet37",
				NameRaw:         []byte("Pet37"),
				Satiety:         73,
				Level:           3,
				Exp:             12,
				SourceListType:  3,
				SourceSlotIndex: 26,
			},
		},
		EquippedKey: "37",
		TownDisplay: true,
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
		conn:                            connection,
		connID:                          "aligned-pet-post-actions",
		channel:                         channelcatalog.Channel{ID: 19},
		selectedCharacterID:             19,
		initialTownRouteCharacterID:     19,
		initialTownRouteStage:           currentInitialTownRoutePlayerStateSent,
		townSceneReadyCharacterID:       19,
		connectionTownActorOwnerChannel: 19,
		townActorOwnerChannel:           19,
		selectedCreatureStateTableSent:  true,
	}
	ackBody := []byte{1, 7, 0x30, 0, 1, 0, 0, 0, 17, 26, 0}
	result := alignedcmd.Result{
		Operation: "move_itemspace",
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          19,
			Body:           ackBody,
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		PostActions: []alignedcmd.PostAction{
			alignedcmd.PostActionRefreshSelectedCreatureState,
		},
	}

	if err := service.sendAlignedUpperResponses(session, result); err != nil {
		t.Fatalf("sendAlignedUpperResponses error = %v", err)
	}
	ack, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != 19 || ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, ackBody) {
		t.Fatalf("ack header=%+v body=%x", ack.Header, ack.Body)
	}
	itemRefresh, rest := splitLongHengGameServerUpperPacket(t, rest)
	assertEquippedCreatureItemRefreshPacket(t, itemRefresh, 63000, 37, 10008705, 0)
	state, rest := splitLongHengGameServerUpperPacket(t, rest)
	wantStateBody := []byte{
		1,
		37, 0, 0, 0,
		73,
		0,
		12, 0, 0, 0,
		3,
		5, 0, 0, 0, 'P', 'e', 't', '3', '7',
		0,
	}
	if state.Header.MsgID != currentCreatureStateTableMsgID || state.Header.Classification != 0 ||
		!bytes.Equal(state.Body, wantStateBody) {
		t.Fatalf("op105 header=%+v body=%x want=%x", state.Header, state.Body, wantStateBody)
	}
	growth, rest := splitLongHengGameServerUpperPacket(t, rest)
	wantGrowth := []byte{3, 0, 12, 0, 0, 0}
	if growth.Header.MsgID != currentCreatureGrowthMsgID || growth.Header.Classification != 0 ||
		!bytes.Equal(growth.Body, wantGrowth) {
		t.Fatalf("growth header=%+v body=%x want=%x", growth.Header, growth.Body, wantGrowth)
	}
	if !session.selectedCreatureStateTableSent {
		t.Fatal("op105 state was not marked sent after successful write")
	}
	if len(rest) != 0 {
		t.Fatalf("pet wear appended packets after targeted slot26 op14/op105/op102=%x", rest)
	}
}

func TestSendAlignedPetUnequipAppendsOnlyCreatureTableWithoutGrowth(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "15",
		Level:       1,
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
		Entries:     map[string]dnfrepo.EquipmentEntry{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {
				PetKey:          "37",
				CreatureKey:     37,
				ItemID:          63000,
				Name:            "Pet37",
				NameRaw:         []byte("Pet37"),
				Satiety:         73,
				Level:           3,
				Exp:             12,
				SourceListType:  7,
				SourceSlotIndex: 48,
			},
		},
		EquippedKey: "",
		TownDisplay: false,
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
		conn:                            connection,
		connID:                          "aligned-pet-unequip-post-actions",
		channel:                         channelcatalog.Channel{ID: 19},
		selectedCharacterID:             19,
		initialTownRouteCharacterID:     19,
		initialTownRouteStage:           currentInitialTownRoutePlayerStateSent,
		townSceneReadyCharacterID:       19,
		connectionTownActorOwnerChannel: 19,
		townActorOwnerChannel:           19,
		selectedCreatureStateTableSent:  true,
	}
	ackBody := []byte{1, 7, 0x30, 0, 1, 0, 0, 0, 17, 26, 0}
	result := alignedcmd.Result{
		Operation: "move_itemspace",
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          19,
			Body:           ackBody,
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		PostActions: []alignedcmd.PostAction{
			alignedcmd.PostActionRefreshSelectedCreatureState,
		},
	}

	if err := service.sendAlignedUpperResponses(session, result); err != nil {
		t.Fatalf("sendAlignedUpperResponses error = %v", err)
	}
	ack, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != 19 ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, ackBody) {
		t.Fatalf("ack header=%+v body=%x", ack.Header, ack.Body)
	}
	state, rest := splitLongHengGameServerUpperPacket(t, rest)
	if state.Header.MsgID != currentCreatureStateTableMsgID ||
		state.Header.Classification != 0 {
		t.Fatalf("op105 header=%+v body=%x", state.Header, state.Body)
	}
	if len(rest) != 0 {
		next, _ := splitLongHengGameServerUpperPacket(t, rest)
		t.Fatalf("pet unequip appended packet op=%d body=%x; op102 must be absent", next.Header.MsgID, next.Body)
	}
}

func TestRuntimeSceneReadySequenceDoesNotReplaySelectedContainers(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "dnf:1", Name: "Actor19", Job: "15", Level: 1}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:10": {ItemID: 700, Count: 1, RawEntry: []byte{0x70}},
		},
		Warehouse: map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "19"}); err != nil {
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
		conn:                          connection,
		connID:                        "post-hud-container-refresh",
		channel:                       channelcatalog.Channel{ID: 19},
		selectedCharacterID:           19,
		selectedUserInfoRefreshSent:   true,
		selectedItemListRefreshSent:   true,
		selectedRentalWalletStateSent: true,
	}
	if err := service.sendRuntimeSceneReadySequence(session, "test"); err != nil {
		t.Fatalf("sendRuntimeSceneReadySequence error = %v", err)
	}

	itemListCount := 0
	for stream := connection.write.Bytes(); len(stream) > 0; {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketLeaveParty) && packet.Header.Classification == 0 && len(packet.Body) != 0 {
			itemListCount++
		}
		stream = rest
	}
	if itemListCount != 0 {
		t.Fatalf("post-scene item-list refreshes=%d want=0; actor-bound pre-mode1 stage already owns the container bootstrap", itemListCount)
	}
}

func TestSendAlignedUpperResponsesSendsSkillInitAckBeforeCurrentSkillInfo(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "0",
		Level:       90,
	}
	if err := repositories.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Skill.Save(ctx, dnfrepo.SkillRecord{
		CharacterID: "19",
		Skills:      map[int64]dnfrepo.SkillState{46: {Level: 1, Enabled: true}},
		Points:      dnfrepo.SkillPointState{TotalSP: 100, RemainingSP: 100, TotalTP: 10, RemainingTP: 10, SyncedLevel: 90},
		Layouts:     map[int]dnfrepo.SkillLayout{0: {0: 46}},
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := buildSkillCatalogFromSource(ctx, initialEquipmentMemSource{
		"skill/skilllist.lst":    "0 `job0.lst`\n",
		"skill/job0.lst":         "46 `job0/initial.skl`\n",
		"skill/job0/initial.skl": "[skill type]\n`active`\n",
	})
	if err != nil {
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
		skillCatalog:       catalog,
		initialSPTable:     map[int]int{1: 100},
		initialTPTable:     map[int]int{50: 10},
	}
	session := &gameSession{
		conn:                connection,
		connID:              "aligned-skill-reset-post-action",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 19,
	}
	result := alignedcmd.Result{
		Operation: "skill_init",
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          uint16(dnfenum.CmdPacketSkillInit),
			Body:           []byte{1, 0, 1},
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		PostActions: []alignedcmd.PostAction{alignedcmd.PostActionRefreshSelectedActorSkills},
	}

	if err := service.sendAlignedUpperResponses(session, result); err != nil {
		t.Fatalf("sendAlignedUpperResponses error = %v", err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketSkillInit) || ack.Header.Classification != dnfproto.DefaultChannelClassification || !bytes.Equal(ack.Body, []byte{0, 0, 0, 1, 0, 1}) {
		t.Fatalf("skill init ack header=%+v body=%x", ack.Header, ack.Body)
	}
	refresh, trailing := splitGameServerUpperPacket(t, rest)
	if refresh.Header.MsgID != currentSkillInfoMsgID || refresh.Header.Classification != 0 {
		t.Fatalf("skill refresh header=%+v body=%x", refresh.Header, refresh.Body)
	}
	if len(refresh.Body) < 3 || !bytes.Equal(refresh.Body[:3], []byte{0, 0, 0}) {
		t.Fatalf("skill refresh codec prefix=%x", refresh.Body)
	}
	if slots := currentSkillInfoFirstTreeSlots(t, refresh.Body[3:]); len(slots) != 1 || slots[0] != 46 {
		t.Fatalf("skill refresh slots=%v, want slot0=46", slots)
	}
	if len(trailing) != 0 {
		t.Fatalf("unexpected trailing bytes: %x", trailing)
	}
}
