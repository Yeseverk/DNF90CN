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

func TestCurrentResetItemAttrConsumesMaterialBeforeRefreshingRandomGradeTarget(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:42": {ItemID: 1001, Count: 1},
			"0:75": {ItemID: 2001, Count: 2, Extra: map[string]string{"item_kind": "stackable"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "account-1"},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "grade-adjust-test", selectedCharacterID: 19}
	body := make([]byte, 8)
	binary.LittleEndian.PutUint16(body[0:2], 42)
	binary.LittleEndian.PutUint32(body[2:6], 1001)
	binary.LittleEndian.PutUint16(body[6:8], 75)
	if err := service.handleCurrentResetItemAttr(session, body); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketResetItemAttr) ||
		len(ack.Body) != 9 || ack.Body[0] != 1 {
		t.Fatalf("ack header=%+v body=%x", ack.Header, ack.Body)
	}
	if slot := binary.LittleEndian.Uint16(ack.Body[1:3]); slot != 75 {
		t.Fatalf("ack slot=%d, want consumed material slot 75", slot)
	}
	if amount := binary.LittleEndian.Uint32(ack.Body[3:7]); amount != 1 {
		t.Fatalf("ack amount=%d, want one material remaining", amount)
	}
	if resultType := binary.LittleEndian.Uint16(ack.Body[7:9]); resultType != currentResetItemAttrGradeResult {
		t.Fatalf("ack result type=%d, want native grade-adjust result", resultType)
	}

	refresh, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 {
		t.Fatalf("trailing bytes=%d", len(trailing))
	}
	if refresh.Header.Classification != 0 ||
		refresh.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf("refresh header=%+v", refresh.Header)
	}
	if len(refresh.Body) != 3+currentItemListEntryWireSize ||
		refresh.Body[0] != dnfrepo.MainInventoryListType ||
		binary.LittleEndian.Uint16(refresh.Body[1:3]) != 1 {
		t.Fatalf("refresh body=%x", refresh.Body)
	}
	targetRow := refresh.Body[3:]
	if slot := int16(binary.LittleEndian.Uint16(targetRow[0:2])); slot != 42 {
		t.Fatalf("refreshed slot=%d, want target slot 42", slot)
	}

	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	target := inventory.Slots["0:42"]
	seed := currentEquipmentQualitySeedFromStack(target)
	if seed == 0 || seed > currentEquipmentRandomQualitySeedCount {
		t.Fatalf("persisted quality seed=%d, want full ordinary seed range", seed)
	}
	if got := binary.LittleEndian.Uint32(targetRow[6:10]); got != seed {
		t.Fatalf("refreshed quality seed=%d, persisted=%d", got, seed)
	}
	if target.Extra["grade_adjust_type"] != "standard_kaleido" {
		t.Fatalf("target extra=%v", target.Extra)
	}
	if target.Extra["item_kind"] != "equipment" {
		t.Fatalf("legacy target identity was not normalized: %v", target.Extra)
	}
	if material := inventory.Slots["0:75"]; material.Count != 1 {
		t.Fatalf("material after=%+v, want one remaining", material)
	}
}

func TestCurrentResetItemAttrGoldUsesGradeResultAndRemovedMaterialSlot(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:42": {ItemID: 1001, Count: 1},
			"0:75": {ItemID: 2001, Count: 1, Extra: map[string]string{"item_kind": "stackable"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	connection := &bufferConn{}
	catalog := &pvfDungeonDropCatalog{
		source: finishBridgePVFSource{},
		itemCache: map[uint32]dungeonDropItemDefinition{
			2001: {
				ItemID:        2001,
				Kind:          dungeonDropItemStackable,
				StackableType: "`gold kaleido`",
			},
		},
	}
	service := &Service{
		options:            options{accountID: "account-1"},
		pvfItemCatalog:     catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "gold-grade-adjust-test", selectedCharacterID: 19}
	body := make([]byte, 8)
	binary.LittleEndian.PutUint16(body[0:2], 42)
	binary.LittleEndian.PutUint32(body[2:6], 1001)
	binary.LittleEndian.PutUint16(body[6:8], 75)
	if err := service.handleCurrentResetItemAttr(session, body); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(ack.Body) != 9 || ack.Body[0] != 1 ||
		binary.LittleEndian.Uint16(ack.Body[1:3]) != 75 ||
		binary.LittleEndian.Uint32(ack.Body[3:7]) != 0 ||
		binary.LittleEndian.Uint16(ack.Body[7:9]) != currentResetItemAttrGradeResult {
		t.Fatalf("gold ack=%x", ack.Body)
	}
	refresh, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 ||
		refresh.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(refresh.Body) != 3+currentItemListEntryWireSize {
		t.Fatalf("gold refresh header=%+v body_len=%d trailing=%d", refresh.Header, len(refresh.Body), len(trailing))
	}
	if seed := binary.LittleEndian.Uint32(refresh.Body[3+6 : 3+10]); seed != currentEquipmentTopQualitySeed {
		t.Fatalf("gold refreshed quality seed=%d, want top seed=%d", seed, currentEquipmentTopQualitySeed)
	}
}
