package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	dnfenum "longheng.io/server/internal/modules/dnf/dnfenum"
	dnfitemlock "longheng.io/server/internal/modules/dnf/itemlock"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func requireCurrentSelectInventoryBootstrapPackets(t *testing.T, stream []byte) ([]byte, map[byte]dnfproto.ChannelPacket, dnfproto.ChannelPacket) {
	t.Helper()
	lists := make(map[byte]dnfproto.ChannelPacket, len(currentSelectInventoryBootstrapListTypes))
	for _, wantType := range currentSelectInventoryBootstrapListTypes {
		packetWire := stream
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		stream = rest
		if packet.Header.Classification != 0 ||
			packet.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) ||
			len(packet.Body) < 3 || packet.Body[0] != wantType {
			t.Fatalf("select inventory bootstrap list%d = class %d msg %d body=%x", wantType, packet.Header.Classification, packet.Header.MsgID, packet.Body)
		}
		if len(packetWire) < dnfproto.GameServerUpperHeaderSize16 ||
			!bytes.Equal(packetWire[11:16], make([]byte, 5)) {
			t.Fatalf("select inventory bootstrap list%d header=%x", wantType, packetWire[:minInt(len(packetWire), dnfproto.GameServerUpperHeaderSize16)])
		}
		lists[wantType] = packet
	}
	lockSnapshot, stream := splitCurrentGameServerUpperPacketAuto(t, stream)
	if lockSnapshot.Header.Classification != 0 || lockSnapshot.Header.MsgID != dnfitemlock.LockListMessageID {
		t.Fatalf("selected inventory lock snapshot header=%+v body=%x", lockSnapshot.Header, lockSnapshot.Body)
	}
	return stream, lists, lockSnapshot
}

func TestCurrentSelectInventoryBootstrapIsBoundToSelectedCharacter(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, characterID := range []string{"77", "78"} {
		if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
			CharacterID: characterID,
			AccountID:   defaultAccountPrefix + "1",
			Stats:       map[string]int64{"gold": 1000000},
		}); err != nil {
			t.Fatal(err)
		}
		if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
			CharacterID: characterID,
			Slots:       map[string]dnfrepo.ItemStack{},
			Warehouse:   map[string]dnfrepo.ItemStack{},
		}); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{
		options:            options{accountPrefix: defaultAccountPrefix},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "select-inventory-character-gate",
		channel:             channelcatalog.Channel{ID: 38},
		selectedCharacterID: 77,
	}

	if err := service.sendCurrentSelectInventoryBootstrap(session, "first_character"); err != nil {
		t.Fatal(err)
	}
	rest, _, _ := requireCurrentSelectInventoryBootstrapPackets(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("first character emitted trailing packets: %x", rest)
	}
	if session.selectedItemListBootstrapCharacterID != 77 || !session.selectedItemListRefreshSent {
		t.Fatalf("first character bootstrap state: char=%d sent=%t", session.selectedItemListBootstrapCharacterID, session.selectedItemListRefreshSent)
	}

	connection.write.Reset()
	if err := service.sendCurrentSelectInventoryBootstrap(session, "same_character_repeat"); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("same character bootstrap repeated: %x", connection.write.Bytes())
	}

	// A direct bind to another character must not inherit the connection-level
	// sent flag from the previous selection.
	session.selectedCharacterID = 78
	if err := service.sendCurrentSelectInventoryBootstrap(session, "second_character"); err != nil {
		t.Fatal(err)
	}
	rest, _, _ = requireCurrentSelectInventoryBootstrapPackets(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("second character emitted trailing packets: %x", rest)
	}
	if session.selectedItemListBootstrapCharacterID != 78 {
		t.Fatalf("second character bootstrap owner=%d, want 78", session.selectedItemListBootstrapCharacterID)
	}
}

func TestCurrentSelectInventoryBootstrapCarriesRealItemsForAllSixLists(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   defaultAccountPrefix + "1",
		Stats:       map[string]int64{"gold": 456789},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:0":  {ItemID: 0, Count: 1},
			"0:3":  {ItemID: 1001, Count: 2},
			"1:11": {ItemID: 2001, Count: 1},
			"7:5":  {ItemID: 3001, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "444"}},
		},
		Warehouse: map[string]dnfrepo.ItemStack{"2:4": {ItemID: 4001, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID: "dnf:77",
		Metadata: map[string]string{
			"account_cargo_created": "true",
			"account_cargo_level":   "8",
			"account_cargo_gold":    "99",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(context.Background(), dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:77",
		Slots:     map[string]dnfrepo.ItemStack{"12:6": {ItemID: 5001, Count: 3}},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "dnf:77", accountPrefix: defaultAccountPrefix},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "select-inventory-real-six-lists",
		channel:             channelcatalog.Channel{ID: 38},
		selectedCharacterID: 77,
	}

	if err := service.sendCurrentSelectInventoryBootstrap(session, "real_items"); err != nil {
		t.Fatal(err)
	}
	rest, lists, _ := requireCurrentSelectInventoryBootstrapPackets(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("real item bootstrap emitted trailing packets: %x", rest)
	}
	assertBootstrapItem := func(listType byte, countOffset int, rowOffset int, wantCount uint16, wantItemID uint32) {
		t.Helper()
		body := lists[listType].Body
		if len(body) < rowOffset+6 {
			t.Fatalf("list%d body too short: %x", listType, body)
		}
		if got := binary.LittleEndian.Uint16(body[countOffset : countOffset+2]); got != wantCount {
			t.Fatalf("list%d count=%d want=%d body=%x", listType, got, wantCount, body)
		}
		if got := binary.LittleEndian.Uint32(body[rowOffset+2 : rowOffset+6]); got != wantItemID {
			t.Fatalf("list%d first item=%d want=%d", listType, got, wantItemID)
		}
	}
	// list0 is sorted by slot, so its wallet row precedes the real quickbar row.
	assertBootstrapItem(0, 3, 5, 2, 0)
	list0 := lists[0].Body
	wallet := list0[5:]
	if binary.LittleEndian.Uint32(wallet[6:10]) != 456789 {
		t.Fatalf("list0 wallet projection=%d want=456789", binary.LittleEndian.Uint32(wallet[6:10]))
	}
	quickbar := list0[5+currentItemListEntryWireSize:]
	if binary.LittleEndian.Uint16(quickbar[0:2]) != 3 || binary.LittleEndian.Uint32(quickbar[2:6]) != 1001 || binary.LittleEndian.Uint32(quickbar[6:10]) != 2 {
		t.Fatalf("list0 quickbar row=%x", quickbar[:14])
	}
	assertBootstrapItem(1, 3, 5, 1, 2001)
	assertBootstrapItem(2, 3, 5, 1, 4001)
	assertBootstrapItem(7, 1, 3, 1, 3001)
	assertBootstrapItem(12, 7, 9, 1, 5001)
}

func TestCurrentSelectInventoryBootstrapSendsDurableItemLockSnapshotAfterContainers(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:108": {ItemID: 490701734, Count: 36},
			"0:112": {ItemID: 490007240, Count: 1},
			"0:120": {ItemID: 1001, Count: 1, Extra: map[string]string{"equipment_lock_state": "1"}},
		},
		Warehouse: map[string]dnfrepo.ItemStack{
			"2:4": {ItemID: 2001, Count: 1, Extra: map[string]string{"equipment_lock_state": "locked"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true }}
	connection := &bufferConn{}
	session := &gameSession{conn: connection, connID: "select-item-lock-snapshot", selectedCharacterID: 77}

	if err := service.sendCurrentSelectInventoryBootstrap(session, "item_lock_snapshot"); err != nil {
		t.Fatal(err)
	}
	rest, _, snapshot := requireCurrentSelectInventoryBootstrapPackets(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("lock snapshot emitted trailing packets: %x", rest)
	}
	if snapshot.Header.Classification != 0 || snapshot.Header.MsgID != dnfitemlock.LockListMessageID {
		t.Fatalf("lock snapshot header=%+v body=%x", snapshot.Header, snapshot.Body)
	}
	want := []byte{2, 0, 0, 120, 0, 1, 2, 4, 0, 1}
	if !bytes.Equal(snapshot.Body, want) {
		t.Fatalf("lock snapshot=%x want=%x", snapshot.Body, want)
	}
}

func TestCurrentSelectInventoryBootstrapPreservesAccountSharedNativeRowsInList0(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]dnfrepo.ItemStack{"0:3": {ItemID: 1001, Count: 2}},
		Warehouse:   map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(context.Background(), dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:354": {ItemID: 3033, Count: 13, RawEntry: bytes.Repeat([]byte{0x5a}, currentItemListEntryWireSize)},
			"0:365": {ItemID: 10158124, Count: 14, RawEntry: bytes.Repeat([]byte{0x6b}, currentItemListEntryWireSize)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "dnf:77"},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	connection := &bufferConn{}
	session := &gameSession{conn: connection, connID: "select-virtual-counters", selectedCharacterID: 77}

	if err := service.sendCurrentSelectInventoryBootstrap(session, "virtual_counters"); err != nil {
		t.Fatal(err)
	}
	rest, lists, _ := requireCurrentSelectInventoryBootstrapPackets(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("account-shared list0 bootstrap emitted trailing packets: %x", rest)
	}
	list0 := lists[dnfrepo.MainInventoryListType].Body
	if len(list0) != 5+3*currentItemListEntryWireSize || binary.LittleEndian.Uint16(list0[3:5]) != 3 {
		t.Fatalf("list0 body=%x", list0)
	}
	rows := decodeCurrentItemListTestRows(t, list0[5:])
	assertCurrentItemListTestRow(t, rows[0], 3, 1001, 2)
	assertCurrentItemListTestRow(t, rows[1], 354, 3033, 13)
	assertCurrentItemListTestRow(t, rows[2], 365, 10158124, 14)
	if rows[1][0x59] != 0x5a || rows[2][0x59] != 0x6b {
		t.Fatalf("account-shared native row tails were not preserved: crystal=%02x soul=%02x", rows[1][0x59], rows[2][0x59])
	}
}

type failAfterInventoryLoads struct {
	dnfrepo.InventoryRepository
	loads     int
	failAfter int
}

func (repository *failAfterInventoryLoads) Load(ctx context.Context, characterID string) (dnfrepo.InventoryRecord, bool, error) {
	repository.loads++
	if repository.loads > repository.failAfter {
		return dnfrepo.InventoryRecord{}, false, errors.New("injected inventory snapshot failure")
	}
	return repository.InventoryRepository.Load(ctx, characterID)
}

func TestCurrentSelectInventoryBootstrapDoesNotSendPartialContainerPlan(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]dnfrepo.ItemStack{},
		Warehouse:   map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	failing := &failAfterInventoryLoads{InventoryRepository: repositories.Inventory, failAfter: 2}
	repositories.Inventory = failing
	service := &Service{
		options:            options{accountPrefix: defaultAccountPrefix},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "select-inventory-atomic-plan",
		channel:             channelcatalog.Channel{ID: 38},
		selectedCharacterID: 77,
	}

	if err := service.sendCurrentSelectInventoryBootstrap(session, "atomic_plan"); err == nil {
		t.Fatal("repository failure did not stop selected-character initialization")
	}
	if failing.loads <= failing.failAfter {
		t.Fatalf("failure was not exercised: loads=%d", failing.loads)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("partial inventory bootstrap escaped before plan completed: %x", connection.write.Bytes())
	}
	if session.selectedItemListRefreshSent || session.selectedItemListBootstrapCharacterID != 0 {
		t.Fatalf("failed plan committed state: sent=%t char=%d", session.selectedItemListRefreshSent, session.selectedItemListBootstrapCharacterID)
	}
}

func TestCurrentSelectInventoryBootstrapWriteFailureDoesNotCommitCharacterGate(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]dnfrepo.ItemStack{},
		Warehouse:   map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("injected selected-character bootstrap write failure")
	connection := &failNthDungeonWriteConn{failAt: 3, err: wantErr}
	service := &Service{
		options:            options{accountPrefix: defaultAccountPrefix},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                connection,
		connID:              "select-inventory-write-failure",
		channel:             channelcatalog.Channel{ID: 38},
		selectedCharacterID: 77,
	}

	if err := service.sendCurrentSelectInventoryBootstrap(session, "write_failure"); !errors.Is(err, wantErr) {
		t.Fatalf("bootstrap write error=%v, want %v", err, wantErr)
	}
	if connection.writes != 3 {
		t.Fatalf("write attempts=%d, want 3", connection.writes)
	}
	if session.selectedItemListRefreshSent || session.selectedItemListBootstrapCharacterID != 0 {
		t.Fatalf("write failure committed state: sent=%t char=%d", session.selectedItemListRefreshSent, session.selectedItemListBootstrapCharacterID)
	}
}
