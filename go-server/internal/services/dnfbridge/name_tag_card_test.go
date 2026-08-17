package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"strconv"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentSendNameTagRefreshUsesTypedMode0ForSelfAndTownPeer(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	expireAt := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "15",
		Level:       90,
		Stats: map[string]int64{
			"name_tag_item_id":     9003,
			"name_tag_expire_time": expireAt.Unix(),
		},
	}
	if err := repositories.Character.Save(ctx, character); err != nil {
		t.Fatalf("save character: %v", err)
	}
	stack := dnfrepo.ItemStack{
		ItemID:   9003,
		Count:    1,
		Bind:     true,
		ExpireAt: expireAt,
	}
	raw := currentItemListEntryFromStack(0, currentNameTagEquipmentSlot, stack)
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			strconv.Itoa(int(currentNameTagEquipmentSlot)): {
				SlotIndex: currentNameTagEquipmentSlot,
				ItemID:    9003,
				Bind:      true,
				ExpireAt:  expireAt,
				RawEntry:  append([]byte(nil), raw.data[:]...),
				Extra: map[string]string{
					"current_exe_equipment_type": "30",
					"current_exe_runtime_move":   "1",
				},
			},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}

	selfConn := &bufferConn{}
	peerConn := &bufferConn{}
	self := &gameSession{
		conn:                  selfConn,
		connID:                "name-tag-self",
		accountID:             "dnf:1",
		selectedCharacterID:   19,
		townActorOwnerChannel: 42,
	}
	peer := &gameSession{
		conn:                  peerConn,
		connID:                "name-tag-peer",
		selectedCharacterID:   20,
		townActorOwnerChannel: 42,
	}
	manager := newOnlinePlayerManager()
	manager.EnterArea(&onlinePlayerInfo{CharacterID: 19, TownID: 1, AreaID: 2, Session: self})
	manager.EnterArea(&onlinePlayerInfo{CharacterID: 20, TownID: 1, AreaID: 2, Session: peer})
	service := &Service{
		options: options{
			accountID:          "dnf:1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		onlinePlayers: manager,
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}

	if err := service.currentSendNameTagRefresh(ctx, self); err != nil {
		t.Fatal(err)
	}
	update, selfMode0 := splitGameServerUpperPacket(t, selfConn.write.Bytes())
	if update.Header.Classification != 0 || update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(update.Body) != 3+currentItemListEntryWireSize || update.Body[0] != currentSocketListEquipment ||
		binary.LittleEndian.Uint16(update.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint16(update.Body[3:5]) != uint16(currentNameTagEquipmentSlot) ||
		binary.LittleEndian.Uint32(update.Body[5:9]) != 9003 {
		t.Fatalf("name-tag slot update header=%+v body_head=%x", update.Header, update.Body[:min(len(update.Body), 16)])
	}
	assertCurrentNameTagMode0PacketForTest(t, selfMode0, character.Name, 9003, uint32(expireAt.Unix()))
	assertCurrentNameTagMode0PacketForTest(t, peerConn.write.Bytes(), character.Name, 9003, uint32(expireAt.Unix()))
}

func assertCurrentNameTagMode0PacketForTest(t *testing.T, wire []byte, name string, wantItemID, wantExpire uint32) {
	t.Helper()
	packet, trailing := splitGameServerUpperPacket(t, wire)
	if len(trailing) != 0 {
		t.Fatalf("name-tag refresh trailing bytes=%d", len(trailing))
	}
	if packet.Header.Classification != 0 || packet.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) {
		t.Fatalf("name-tag refresh header=%+v", packet.Header)
	}
	body := packet.Body
	if len(body) < 5 || body[0] != 0 || body[4] != 42 {
		t.Fatalf("name-tag mode0 head=%x", body[:min(len(body), 16)])
	}
	tailStart := currentSceneObjectTailStartForTest(name)
	if tailStart >= len(body) {
		t.Fatalf("name-tag mode0 tail start=%d len=%d", tailStart, len(body))
	}
	tail := body[tailStart:]
	equipEnd, ok := currentSceneObjectEquipSummaryEnd(tail, 6)
	if !ok || equipEnd+16 > len(tail) {
		t.Fatalf("name-tag mode0 equipment end=%d ok=%t tail_len=%d", equipEnd, ok, len(tail))
	}
	if got := int(tail[6]); got != currentActorMode0AppearanceSlotCount+1 {
		t.Fatalf("name-tag appearance row count=%d want=%d", got, currentActorMode0AppearanceSlotCount+1)
	}
	appearance := tail[6:equipEnd]
	nameTagRowOffset := 1 + currentActorMode0AppearanceSlotCount*currentActorMode0AppearanceRowBytes
	if appearance[nameTagRowOffset] != byte(currentNameTagAppearanceSlot) ||
		binary.LittleEndian.Uint32(appearance[nameTagRowOffset+1:nameTagRowOffset+5]) != wantItemID {
		t.Fatalf("name-tag appearance row=%x", appearance[nameTagRowOffset:nameTagRowOffset+5])
	}
	if got := binary.LittleEndian.Uint32(tail[equipEnd+8 : equipEnd+12]); got != wantItemID {
		t.Fatalf("name-tag endpoint item=%d want=%d", got, wantItemID)
	}
	if got := binary.LittleEndian.Uint32(tail[equipEnd+12 : equipEnd+16]); got != wantExpire {
		t.Fatalf("name-tag endpoint expire=%d want=%d", got, wantExpire)
	}
	if bytes.HasPrefix(body, []byte{0, 1, 0, 19, 0}) {
		t.Fatal("name-tag refresh used legacy subtype0 instead of typed mode0")
	}
}
