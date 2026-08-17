package dnfbridge

import (
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDungeonSelectDoesNotReplayOp27AfterTownEnterSelectPage(t *testing.T) {
	table, resolver, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(false))
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       20,
		Stats: map[string]int64{
			"fatigue": 100, "town_id": 38, "area_id": 1,
			"pos_x": 450, "pos_y": 234, "direction": 5, "area_state": 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{CharacterID: "99", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:             options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonChoice:       func(int) (int, error) { return 0, nil },
		dungeonSeed:         func() (uint32, error) { return 1, nil },
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	connection := &bufferConn{}
	session := &gameSession{
		conn:                          connection,
		selectedCharacterID:           99,
		enterSelectDungeonContextSent: true,
	}
	bindDungeonSelectorOriginForTestAt(t, service, session, 38, 1, 450, 234)
	body := make([]byte, 21)
	binary.LittleEndian.PutUint32(body[0:4], 700)
	binary.LittleEndian.PutUint16(body[9:11], 0xffff)
	if err := service.handleDungeonSelectUpper(session, body); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketSelectDungeon) {
		t.Fatalf("op16 ACK header=%+v", ack.Header)
	}
	resource, rest := splitGameServerUpperPacket(t, rest)
	if resource.Header.MsgID != currentDungeonResourceStateMsgID {
		t.Fatalf("op5 header=%+v", resource.Header)
	}
	rest = assertCurrentPreDungeonContextPlayerState(t, session, rest)
	next, _ := splitGameServerUpperPacket(t, rest)
	if next.Header.MsgID != currentDungeonInfoNotification {
		t.Fatalf("packet after pre-op27 actor state=%+v, want op28 without duplicate op27", next.Header)
	}
}
