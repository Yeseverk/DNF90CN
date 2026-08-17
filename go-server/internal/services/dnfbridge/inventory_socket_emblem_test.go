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
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func assertCurrentEquipmentVectorEquals(t *testing.T, want, got []byte) {
	t.Helper()
	end := currentEquipmentVectorOffset + currentEquipmentEmblemDataBytes
	if len(want) < end || len(got) < end {
		t.Fatalf("raw row too short for current vector: want=%d got=%d", len(want), len(got))
	}
	if !bytes.Equal(want[currentEquipmentVectorOffset:end], got[currentEquipmentVectorOffset:end]) {
		t.Fatalf("current vector changed: got=%x want=%x", got[currentEquipmentVectorOffset:end], want[currentEquipmentVectorOffset:end])
	}
}

func TestCurrentEquipmentSocketOpenAndEmblemAttachPersistsPVFFields(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 9001, Count: 1},
			"0:8": {ItemID: 6001, Count: 2},
			"0:9": {ItemID: 5001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "socket-test", conn: &bufferConn{}}

	result, err := service.commitCurrentEquipmentSocketOpen(session, currentSocketOpenRequest{
		TargetSlot:   5,
		TargetItemID: 9001,
		MaterialSlot: 8,
	})
	if err != nil {
		t.Fatalf("equipment socket open: %v", err)
	}
	if result.Target != (currentSocketChangedSlot{ListType: 0, Slot: 5}) || len(result.Consumed) != 1 || result.Consumed[0].Slot != 8 {
		t.Fatalf("open result=%+v", result)
	}

	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if got := inventory.Slots["0:8"].Count; got != 1 {
		t.Fatalf("material count after open=%d want=1", got)
	}
	equipmentData := currentEquipmentEmblemData(inventory.Slots["0:5"].Extra, inventory.Slots["0:5"].RawEntry)
	if equipmentData[0] != 2 ||
		binary.LittleEndian.Uint32(equipmentData[1:5]) != 0xFFFFFFFF ||
		binary.LittleEndian.Uint32(equipmentData[5:9]) != 0xFFFFFFFF {
		t.Fatalf("opened equipment emblem data=%x", equipmentData)
	}
	// Current EXE vector at raw+0x3C: count=2 and two contiguous empty emblem
	// IDs.
	wantRaw := make([]byte, currentItemListEntryWireSize)
	wantRaw[currentEquipmentVectorOffset] = 2
	binary.LittleEndian.PutUint32(wantRaw[currentEquipmentVectorOffset+1:currentEquipmentVectorOffset+5], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(wantRaw[currentEquipmentVectorOffset+5:currentEquipmentVectorOffset+9], 0xFFFFFFFF)
	assertCurrentEquipmentVectorEquals(t, wantRaw, inventory.Slots["0:5"].RawEntry)
	if inventory.Slots["0:5"].Extra["equipment_emblem_data"] == "" ||
		inventory.Slots["0:5"].Extra["equipment_socket_data"] == "" ||
		inventory.Slots["0:5"].Extra["tail_data_2f"] != "" ||
		inventory.Slots["0:5"].Extra["tailData2F"] != "" {
		t.Fatalf("opened equipment wrote unexpected socket/tail aliases: %+v", inventory.Slots["0:5"].Extra)
	}

	result, err = service.commitCurrentEquipmentEmblemAttach(session, currentEmblemAttachRequest{
		TargetSlot:   5,
		TargetItemID: 9001,
		Emblems: []currentEmblemApplyRequest{{
			EmblemSlot:   9,
			EmblemItemID: 5001,
			SocketIndex:  0,
		}},
	})
	if err != nil {
		t.Fatalf("equipment emblem attach: %v", err)
	}
	if result.Target != (currentSocketChangedSlot{ListType: 0, Slot: 5}) || len(result.Consumed) != 1 || result.Consumed[0].Slot != 9 {
		t.Fatalf("attach result=%+v", result)
	}

	inventory = mustLoadCurrentSocketInventory(t, repositories, "77")
	if _, ok := inventory.Slots["0:9"]; ok {
		t.Fatalf("emblem stack was not consumed: %+v", inventory.Slots["0:9"])
	}
	equipmentData = currentEquipmentEmblemData(inventory.Slots["0:5"].Extra, inventory.Slots["0:5"].RawEntry)
	if equipmentData[0] != 2 ||
		binary.LittleEndian.Uint32(equipmentData[1:5]) != 5001 ||
		binary.LittleEndian.Uint32(equipmentData[5:9]) != 0xFFFFFFFF {
		t.Fatalf("attached equipment emblem data=%x", equipmentData)
	}
	// After emblem attach: count=2, emblem[0]=5001, emblem[1]=FFFFFFFF.
	wantAttached := make([]byte, currentItemListEntryWireSize)
	wantAttached[currentEquipmentVectorOffset] = 2
	binary.LittleEndian.PutUint32(wantAttached[currentEquipmentVectorOffset+1:currentEquipmentVectorOffset+5], 5001)
	binary.LittleEndian.PutUint32(wantAttached[currentEquipmentVectorOffset+5:currentEquipmentVectorOffset+9], 0xFFFFFFFF)
	assertCurrentEquipmentVectorEquals(t, wantAttached, inventory.Slots["0:5"].RawEntry)
}

func TestCurrentEquipmentTailDataAliasesProjectSocketVectorIntoEquipmentRows(t *testing.T) {
	raw := make([]byte, currentItemListEntryWireSize)
	binary.LittleEndian.PutUint16(raw[0:2], 17)
	binary.LittleEndian.PutUint32(raw[2:6], 9001)
	zeroTail := make([]byte, currentEquipmentTailDataBytes)
	openTail := make([]byte, currentEquipmentTailDataBytes)
	openTail[0] = 2
	binary.LittleEndian.PutUint32(openTail[1:5], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(openTail[5:9], 0xFFFFFFFF)
	extra := map[string]string{"current_exe_equipment_type": "17"}
	currentSetHexExtra(extra, "tail_data_2f", zeroTail)
	currentSetHexExtra(extra, "tailData2F", openTail)
	equipped := dnfrepo.EquipmentEntry{SlotIndex: 17, ItemID: 9001, RawEntry: raw, Extra: extra}

	row, ok := currentItemListEntryFromEquipment(equipped)
	if !ok {
		t.Fatal("currentItemListEntryFromEquipment rejected equipped row")
	}
	wantRaw := make([]byte, currentItemListEntryWireSize)
	copy(wantRaw, raw)
	wantRaw[currentEquipmentVectorOffset] = 2
	binary.LittleEndian.PutUint32(wantRaw[currentEquipmentVectorOffset+1:currentEquipmentVectorOffset+5], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(wantRaw[currentEquipmentVectorOffset+5:currentEquipmentVectorOffset+9], 0xFFFFFFFF)
	assertCurrentEquipmentVectorEquals(t, wantRaw, row.data[:])
}

func TestCurrentMode1EquipmentCreateProjectsPersistedSocketVectorOnFirstLogin(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	raw := make([]byte, currentItemListEntryWireSize)
	raw[currentEquipmentVectorOffset] = 2
	binary.LittleEndian.PutUint32(raw[currentEquipmentVectorOffset+1:currentEquipmentVectorOffset+5], 5001)
	binary.LittleEndian.PutUint32(raw[currentEquipmentVectorOffset+5:currentEquipmentVectorOffset+9], math.MaxUint32)
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"17": {
				SlotIndex: 17,
				ItemID:    9001,
				RawEntry:  raw,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{repositoryProvider: func() (dnfrepo.Group, bool) {
		return repositories, true
	}}
	reader := csharpLegacyUserInfoReader{
		ctx:         ctx,
		characterID: "19",
		service:     service,
	}
	rows := reader.currentMode1EquipmentObjectRows()
	if len(rows) != 1 {
		t.Fatalf("mode1 equipment rows=%d want=1", len(rows))
	}
	want := []byte{0x89, 0x13, 0, 0, 0xff, 0xff, 0xff, 0xff}
	if !bytes.Equal(rows[0].vector225CCA0, want) {
		t.Fatalf("mode1 socket vector=%x want=%x", rows[0].vector225CCA0, want)
	}
	if len(currentMode1EquipmentCreateRows(rows)) != 1 {
		t.Fatal("socket-bearing equipment row was not accepted for mode1 create")
	}

	var writer packetWriter
	writeCurrentMode1EquipmentCreateRow(&writer, rows[0])
	body := writer.bytes()
	pos := 24
	if body[pos] != 0 {
		t.Fatalf("mode1 sub_1D6E020 count=%d want=0", body[pos])
	}
	pos++    // sub_1D6E020 count
	pos += 4 // state112
	if body[pos] != 2 || !bytes.Equal(body[pos+1:pos+9], want) {
		t.Fatalf("mode1 sub_225CCA0 wire=%x want count2+%x", body[pos:pos+9], want)
	}
}

func TestCurrentEquipmentEmblemDataReadsCurrentRawVectorWithoutLegacyExtra(t *testing.T) {
	raw := make([]byte, currentItemListEntryWireSize)
	raw[currentEquipmentVectorOffset] = 2
	binary.LittleEndian.PutUint32(raw[currentEquipmentVectorOffset+1:currentEquipmentVectorOffset+5], 5001)
	binary.LittleEndian.PutUint32(raw[currentEquipmentVectorOffset+5:currentEquipmentVectorOffset+9], math.MaxUint32)

	data := currentEquipmentEmblemData(nil, raw)
	if data[0] != 2 ||
		binary.LittleEndian.Uint32(data[1:5]) != 5001 ||
		binary.LittleEndian.Uint32(data[5:9]) != math.MaxUint32 {
		t.Fatalf("current raw socket vector decoded as %x", data)
	}
}

func TestCurrentEquipmentVectorIsPreservedFromPersistedRow(t *testing.T) {
	raw := make([]byte, currentItemListEntryWireSize)
	raw[currentEquipmentVectorOffset] = 2
	binary.LittleEndian.PutUint32(raw[currentEquipmentVectorOffset+1:], math.MaxUint32)
	binary.LittleEndian.PutUint32(raw[currentEquipmentVectorOffset+5:], math.MaxUint32)
	stack := dnfrepo.ItemStack{ItemID: 9001, Count: 1, RawEntry: append([]byte(nil), raw...)}
	entry := currentItemListEntryFromStack(currentSocketListMain, 5, stack)
	assertCurrentEquipmentVectorEquals(t, raw, entry.data[:])
}

func TestCurrentEquipmentProjectionMigratesOnlyExactLegacyWrongVector(t *testing.T) {
	data := [currentEquipmentEmblemDataBytes]byte{2}
	binary.LittleEndian.PutUint32(data[1:5], math.MaxUint32)
	binary.LittleEndian.PutUint32(data[5:9], math.MaxUint32)
	extra := map[string]string{"item_kind": "equipment"}
	currentSetEquipmentEmblemDataExtra(extra, data)

	var migrated currentItemListEntry
	oldOffset := currentLegacyWrongEquipmentVectorOffset
	migrated.data[oldOffset] = 2
	binary.LittleEndian.PutUint32(migrated.data[oldOffset+1:oldOffset+5], math.MaxUint32)
	binary.LittleEndian.PutUint32(migrated.data[oldOffset+5:oldOffset+9], math.MaxUint32)
	currentApplyEquipmentSocketVectorToEntry(&migrated, extra)
	if !bytes.Equal(migrated.data[oldOffset:oldOffset+17], make([]byte, 17)) {
		t.Fatalf("legacy wrong vector was not cleared: %x", migrated.data[oldOffset:oldOffset+17])
	}
	if migrated.data[currentEquipmentVectorOffset] != 2 ||
		binary.LittleEndian.Uint32(migrated.data[currentEquipmentVectorOffset+1:currentEquipmentVectorOffset+5]) != math.MaxUint32 ||
		binary.LittleEndian.Uint32(migrated.data[currentEquipmentVectorOffset+5:currentEquipmentVectorOffset+9]) != math.MaxUint32 {
		t.Fatalf("current emblem vector=%x", migrated.data[currentEquipmentVectorOffset:currentEquipmentVectorOffset+currentEquipmentEmblemDataBytes])
	}

	var preserved currentItemListEntry
	preserved.data[oldOffset] = 2
	binary.LittleEndian.PutUint32(preserved.data[oldOffset+1:oldOffset+5], math.MaxUint32)
	binary.LittleEndian.PutUint32(preserved.data[oldOffset+5:oldOffset+9], math.MaxUint32)
	preserved.data[oldOffset+9] = 0x7A
	currentApplyEquipmentSocketVectorToEntry(&preserved, extra)
	if preserved.data[oldOffset+9] != 0x7A {
		t.Fatalf("nonmatching raw+0x27 record was modified: %x", preserved.data[oldOffset:oldOffset+17])
	}
}

func TestCurrentNoBody796SuccessAckHasNoBusinessBody(t *testing.T) {
	if got := buildCurrentNoBody796AckBody(); len(got) != 0 {
		t.Fatalf("op796 success business body=%x want empty", got)
	}
	avatar := buildCurrentAvatarEmblemAttachAckBody(currentEmblemAttachRequest{TargetSlot: 2, TargetItemID: 9101, Emblems: []currentEmblemApplyRequest{{EmblemSlot: 8}}})
	if len(avatar) != 7 {
		t.Fatalf("avatar emblem success business body len=%d want=7", len(avatar))
	}
}

func TestCurrentAvatarSocketOpenAndEmblemAttachPersistsOptionalBlob(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"1:2": {ItemID: 9101, Count: 1},
			"0:8": {ItemID: 6001, Count: 1},
			"0:9": {ItemID: 5001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "avatar-socket-test", conn: &bufferConn{}}

	if _, err := service.commitCurrentAvatarSocketOpen(session, currentSocketOpenRequest{
		TargetSlot:   2,
		TargetItemID: 9101,
		MaterialSlot: 8,
	}); err != nil {
		t.Fatalf("avatar socket open: %v", err)
	}

	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if _, ok := inventory.Slots["0:8"]; ok {
		t.Fatalf("avatar socket material was not consumed: %+v", inventory.Slots["0:8"])
	}
	avatarData := currentAvatarSocketData(inventory.Slots["1:2"].Extra)
	if avatarData[0] != 0x04 || avatarData[1] != 0x00 || avatarData[6] != 0x10 || avatarData[7] != 0x00 {
		t.Fatalf("opened avatar socket data=%x", avatarData)
	}

	if _, err := service.commitCurrentAvatarEmblemAttach(session, currentEmblemAttachRequest{
		TargetSlot:   2,
		TargetItemID: 9101,
		Emblems: []currentEmblemApplyRequest{{
			EmblemSlot:   9,
			EmblemItemID: 5001,
			SocketIndex:  0,
		}},
	}); err != nil {
		t.Fatalf("avatar emblem attach: %v", err)
	}

	inventory = mustLoadCurrentSocketInventory(t, repositories, "77")
	if _, ok := inventory.Slots["0:9"]; ok {
		t.Fatalf("avatar emblem stack was not consumed: %+v", inventory.Slots["0:9"])
	}
	avatarData = currentAvatarSocketData(inventory.Slots["1:2"].Extra)
	if avatarData[0] != 0x04 || binary.LittleEndian.Uint32(avatarData[2:6]) != 5001 {
		t.Fatalf("attached avatar socket data=%x", avatarData)
	}

	entry := currentItemListEntryFromStack(1, 2, inventory.Slots["1:2"])
	body := buildCurrentItemUpdateBody(1, []currentItemListEntry{entry})
	if len(body) != 3+currentItemListEntryWireSize+4+currentAvatarSocketBytes+4 {
		t.Fatalf("list1 update body len=%d body=%x", len(body), body)
	}
	if !bytes.Equal(body[:3], []byte{1, 1, 0}) {
		t.Fatalf("list1 update header=%x", body[:3])
	}
	offset := 3 + currentItemListEntryWireSize
	if got := binary.LittleEndian.Uint32(body[offset : offset+4]); got != currentAvatarSocketBytes {
		t.Fatalf("avatar socket blob length=%d want=%d body=%x", got, currentAvatarSocketBytes, body[offset:])
	}
	blob := body[offset+4 : offset+4+currentAvatarSocketBytes]
	if !bytes.Equal(blob, avatarData[:]) {
		t.Fatalf("avatar socket blob=%x want=%x", blob, avatarData)
	}
	if got := binary.LittleEndian.Uint32(body[offset+4+currentAvatarSocketBytes:]); got != 0 {
		t.Fatalf("second avatar optional blob length=%d want=0", got)
	}
}

func TestCurrentNormalEquipmentOp14DoesNotAppendAvatarOptionalBlobs(t *testing.T) {
	var opened [currentEquipmentEmblemDataBytes]byte
	currentEnsureEquipmentEmblemSocketsOpen(&opened, 2, 2)
	equipped := dnfrepo.EquipmentEntry{
		SlotIndex: 13,
		ItemID:    9001,
		Extra:     map[string]string{"current_exe_equipment_type": "14"},
	}
	currentApplyEquipmentEmblemDataToEquipment(&equipped, opened, 0x04)
	entry, ok := currentItemListEntryFromEquipment(equipped)
	if !ok {
		t.Fatal("normal equipment row was rejected")
	}

	body := buildCurrentItemUpdateBody(currentSocketListEquipment, []currentItemListEntry{entry})
	if len(body) != 3+currentItemListEntryWireSize ||
		body[0] != currentSocketListEquipment ||
		binary.LittleEndian.Uint16(body[1:3]) != 1 {
		t.Fatalf("list-3 op14 body=%x", body)
	}
}

func TestCurrentSocketRequestParsersMatch86JPLayouts(t *testing.T) {
	var socket packetWriter
	socket.writeUint16(5)
	socket.writeUint32(9001)
	socket.writeUint16(8)

	open, err := decodeCurrentSocketOpenRequest(socket.bytes())
	if err != nil {
		t.Fatalf("decode socket open: %v", err)
	}
	if open.TargetSlot != 5 || open.TargetItemID != 9001 || open.MaterialSlot != 8 {
		t.Fatalf("open=%+v", open)
	}
	if got := stripLegacySocketOpenTransportTrailer(append(socket.bytes(), 0, 0, 0, 0)); !bytes.Equal(got, socket.bytes()) {
		t.Fatalf("legacy socket strip=%x want=%x", got, socket.bytes())
	}

	emblemBody := buildCurrentSocketTestEmblemBody(5, 9001,
		currentEmblemApplyRequest{EmblemSlot: 9, EmblemItemID: 5001, SocketIndex: 0},
		currentEmblemApplyRequest{EmblemSlot: 10, EmblemItemID: 5002, SocketIndex: 1},
	)
	attach, err := decodeCurrentEmblemAttachRequest(emblemBody)
	if err != nil {
		t.Fatalf("decode emblem attach: %v", err)
	}
	if attach.TargetSlot != 5 || attach.TargetItemID != 9001 || len(attach.Emblems) != 2 ||
		attach.Emblems[0].EmblemSlot != 9 || attach.Emblems[0].EmblemItemID != 5001 || attach.Emblems[0].SocketIndex != 0 ||
		attach.Emblems[1].EmblemSlot != 10 || attach.Emblems[1].EmblemItemID != 5002 || attach.Emblems[1].SocketIndex != 1 {
		t.Fatalf("attach=%+v", attach)
	}
	if got := stripLegacyEmblemAttachTransportTrailer(append(emblemBody, 0, 0, 0, 0)); !bytes.Equal(got, emblemBody) {
		t.Fatalf("legacy emblem strip=%x want=%x", got, emblemBody)
	}
	if want := []byte{2, 9, 0, 0x89, 0x13, 0, 0, 10, 0, 0x8A, 0x13, 0, 0}; !bytes.Equal(buildCurrentEquipmentEmblemAttachAckBody(attach), want) {
		t.Fatalf("equipment emblem op913 ack=%x want=%x", buildCurrentEquipmentEmblemAttachAckBody(attach), want)
	}

	avatarPrefixed := append([]byte{currentSocketListAvatar}, emblemBody...)
	avatarAttach, err := decodeCurrentAvatarEmblemAttachRequest(avatarPrefixed)
	if err != nil {
		t.Fatalf("decode avatar-prefixed attach: %v", err)
	}
	if avatarAttach.TargetSlot != attach.TargetSlot || avatarAttach.TargetItemID != attach.TargetItemID || len(avatarAttach.Emblems) != len(attach.Emblems) {
		t.Fatalf("avatar attach=%+v want=%+v", avatarAttach, attach)
	}
	if got := stripLegacyEmblemAttachTransportTrailer(append(avatarPrefixed, 0, 0, 0, 0)); !bytes.Equal(got, avatarPrefixed) {
		t.Fatalf("legacy avatar emblem strip=%x want=%x", got, avatarPrefixed)
	}
}

func TestCurrentEquipmentEmblemAttachDecodesCapturedOp913Body(t *testing.T) {
	body := []byte{
		0x0E, 0x00, 0x8D, 0x8A, 0xF9, 0x05, 0x02,
		0x28, 0x01, 0xB7, 0x25, 0x26, 0x00, 0x00,
		0x27, 0x01, 0xC1, 0x25, 0x26, 0x00, 0x01,
	}
	request, err := decodeCurrentEmblemAttachRequest(body)
	if err != nil {
		t.Fatalf("decode captured op913: %v", err)
	}
	if request.TargetSlot != 14 || request.TargetItemID != 100240013 || len(request.Emblems) != 2 ||
		request.Emblems[0] != (currentEmblemApplyRequest{EmblemSlot: 296, EmblemItemID: 2500023, SocketIndex: 0}) ||
		request.Emblems[1] != (currentEmblemApplyRequest{EmblemSlot: 295, EmblemItemID: 2500033, SocketIndex: 1}) {
		t.Fatalf("captured op913 request=%+v", request)
	}
	if want := []byte{
		0x02,
		0x28, 0x01, 0xB7, 0x25, 0x26, 0x00,
		0x27, 0x01, 0xC1, 0x25, 0x26, 0x00,
	}; !bytes.Equal(buildCurrentEquipmentEmblemAttachAckBody(request), want) {
		t.Fatalf("captured op913 ack=%x want=%x", buildCurrentEquipmentEmblemAttachAckBody(request), want)
	}
}

func TestCurrentEquipmentEmblemAttachHandlerMatchesCapturedOp913AndRefreshesRows(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	var opened [currentEquipmentEmblemDataBytes]byte
	currentEnsureEquipmentEmblemSocketsOpen(&opened, 2, 2)
	target := dnfrepo.ItemStack{ItemID: 9001, Count: 1}
	currentApplyEquipmentEmblemDataToStack(&target, currentSocketListMain, 5, opened, 0)
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5":  target,
			"0:9":  {ItemID: 5001, Count: 1},
			"0:10": {ItemID: 5002, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "equipment-emblem-op913", conn: &bufferConn{}}
	requestBody := buildCurrentSocketTestEmblemBody(5, 9001,
		currentEmblemApplyRequest{EmblemSlot: 9, EmblemItemID: 5001, SocketIndex: 0},
		currentEmblemApplyRequest{EmblemSlot: 10, EmblemItemID: 5002, SocketIndex: 1},
	)

	if err := service.handleCurrentEquipmentEmblemAttach(session, requestBody); err != nil {
		t.Fatalf("handle equipment emblem op913: %v", err)
	}
	ack, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != currentEquipmentEmblemAttachOpcode {
		t.Fatalf("op913 ack header=%+v", ack.Header)
	}
	request, err := decodeCurrentEmblemAttachRequest(requestBody)
	if err != nil {
		t.Fatalf("decode request for expected ack: %v", err)
	}
	if want := upperSuccessBody(buildCurrentEquipmentEmblemAttachAckBody(request)); !bytes.Equal(ack.Body, want) {
		t.Fatalf("op913 ack body=%x want=%x", ack.Body, want)
	}
	targetRefresh, rest := splitGameServerUpperPacket(t, rest)
	if targetRefresh.Header.Classification != 0 ||
		targetRefresh.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf("target refresh header=%+v", targetRefresh.Header)
	}
	for index := 0; index < 2; index++ {
		consumed, next := splitGameServerUpperPacket(t, rest)
		rest = next
		if consumed.Header.Classification != 0 ||
			consumed.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
			t.Fatalf("consumed[%d] refresh header=%+v", index, consumed.Header)
		}
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing packets after incremental op913 refreshes=%x", rest)
	}

	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if _, ok := inventory.Slots["0:9"]; ok {
		t.Fatal("first emblem was not consumed")
	}
	if _, ok := inventory.Slots["0:10"]; ok {
		t.Fatal("second emblem was not consumed")
	}
	data := currentEquipmentEmblemData(inventory.Slots["0:5"].Extra, inventory.Slots["0:5"].RawEntry)
	if data[0] != 2 ||
		binary.LittleEndian.Uint32(data[1:5]) != 5001 ||
		binary.LittleEndian.Uint32(data[5:9]) != 5002 {
		t.Fatalf("attached equipment emblem data=%x", data)
	}
}

func TestCurrentAvatarSocketOpenHandlerSendsAckAndVisibleSlotRefreshes(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"1:2": {ItemID: 9101, Count: 1},
			"0:8": {ItemID: 6001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "avatar-handler-test", conn: &bufferConn{}}
	var request packetWriter
	request.writeUint16(2)
	request.writeUint32(9101)
	request.writeUint16(8)

	if err := service.handleCurrentAvatarSocketOpen(session, request.bytes()); err != nil {
		t.Fatalf("handle avatar socket open: %v", err)
	}

	ack, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != currentAvatarSocketOpenOpcode {
		t.Fatalf("ack header=%+v", ack.Header)
	}
	if want := upperSuccessBody(buildCurrentSocketOpenAckBody(currentSocketOpenRequest{TargetSlot: 2, TargetItemID: 9101, MaterialSlot: 8})); !bytes.Equal(ack.Body, want) {
		t.Fatalf("ack body=%x want=%x", ack.Body, want)
	}

	target, rest := splitGameServerUpperPacket(t, rest)
	if target.Header.Classification != 0 || target.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf("target refresh header=%+v", target.Header)
	}
	if len(target.Body) != 3+currentItemListEntryWireSize+4+currentAvatarSocketBytes+4 ||
		target.Body[0] != currentSocketListAvatar ||
		binary.LittleEndian.Uint16(target.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint16(target.Body[3:5]) != 2 ||
		binary.LittleEndian.Uint32(target.Body[5:9]) != 9101 {
		t.Fatalf("target refresh body=%x", target.Body)
	}
	offset := 3 + currentItemListEntryWireSize
	if got := binary.LittleEndian.Uint32(target.Body[offset : offset+4]); got != currentAvatarSocketBytes {
		t.Fatalf("target socket blob length=%d body=%x", got, target.Body[offset:])
	}
	blob := target.Body[offset+4 : offset+4+currentAvatarSocketBytes]
	if blob[0] != 0x04 || blob[1] != 0 || blob[6] != 0x10 || blob[7] != 0 {
		t.Fatalf("target socket blob=%x", blob)
	}
	if got := binary.LittleEndian.Uint32(target.Body[offset+4+currentAvatarSocketBytes:]); got != 0 {
		t.Fatalf("target color blob length=%d want=0", got)
	}

	material, trailing := splitGameServerUpperPacket(t, rest)
	if material.Header.Classification != 0 || material.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf("material refresh header=%+v", material.Header)
	}
	if len(trailing) != 0 {
		t.Fatalf("trailing packets=%x", trailing)
	}
	if len(material.Body) != 3+currentItemListEntryWireSize ||
		material.Body[0] != currentSocketListMain ||
		binary.LittleEndian.Uint16(material.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint16(material.Body[3:5]) != 8 ||
		binary.LittleEndian.Uint32(material.Body[5:9]) != 0 ||
		binary.LittleEndian.Uint32(material.Body[9:13]) != 0 {
		t.Fatalf("material refresh body=%x", material.Body)
	}
}

func TestCurrentEquipmentSocketOpenHandlerRejectsWornEquipmentWithOp914Failure(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:70": {ItemID: 6001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"17": {
				SlotIndex: 17,
				ItemID:    9001,
				Extra:     map[string]string{"current_exe_equipment_type": "17"},
			},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "equipment-socket-handler-test", conn: &bufferConn{}}
	request := currentSocketOpenRequest{TargetSlot: 17, TargetItemID: 9001, MaterialSlot: 70}
	var body packetWriter
	body.writeUint16(uint16(request.TargetSlot))
	body.writeUint32(uint32(request.TargetItemID))
	body.writeUint16(uint16(request.MaterialSlot))

	if err := service.handleCurrentEquipmentSocketOpen(session, body.bytes()); err != nil {
		t.Fatalf("handle equipment socket open: %v", err)
	}

	failure, trailing := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if failure.Header.Classification != dnfproto.DefaultChannelClassification ||
		failure.Header.MsgID != uint16(dnfenum.CmdPacketAddEquipmentSocket) ||
		!bytes.Equal(failure.Body, []byte{0, 4}) ||
		len(trailing) != 0 {
		t.Fatalf("failure packet=%+v body=%x trailing=%x", failure.Header, failure.Body, trailing)
	}
	if inventory := mustLoadCurrentSocketInventory(t, repositories, "77"); inventory.Slots["0:70"].Count != 1 {
		t.Fatalf("socket opener was consumed on worn reject: %+v", inventory.Slots)
	}
	equipment, found, err := repositories.Equipment.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load equipment found=%v err=%v", found, err)
	}
	data := currentEquipmentEmblemData(equipment.Entries["17"].Extra, equipment.Entries["17"].RawEntry)
	if data[0] != 0 || equipment.Entries["17"].Extra["equipment_emblem_data"] != "" || equipment.Entries["17"].Extra["tail_data_2f"] != "" {
		t.Fatalf("worn equipment mutated after rejected open data=%x extra=%+v", data, equipment.Entries["17"].Extra)
	}
}

func TestCurrentEquipmentSocketOpenHandlerSucceedsAndConsumesMaterial(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5":  {ItemID: 9001, Count: 1},
			"0:70": {ItemID: 6001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "equipment-socket-success", conn: &bufferConn{}}
	var body packetWriter
	body.writeUint16(5)
	body.writeUint32(9001)
	body.writeUint16(70)

	if err := service.handleCurrentEquipmentSocketOpen(session, body.bytes()); err != nil {
		t.Fatalf("handle equipment socket open: %v", err)
	}
	// Expect success ACK (class1/op914), the target list-0 op14 row, and the
	// authoritative material deletion row. Current sub_1D73120 decodes the
	// target row through sub_225CD00 and applies the socket vector via vtable
	// method +280, so no full op13 inventory rebuild is needed.
	written := session.conn.(*bufferConn).write.Bytes()
	if len(written) == 0 {
		t.Fatal("no packets written after successful socket open")
	}
	ack, rest := splitGameServerUpperPacket(t, written)
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != currentEquipmentSocketOpenOpcode {
		t.Fatalf("ack packet=%+v", ack.Header)
	}
	if want := upperSuccessBody(buildCurrentEquipmentSocketOpenAckBody(currentSocketOpenRequest{
		TargetSlot:   5,
		TargetItemID: 9001,
		MaterialSlot: 70,
	})); !bytes.Equal(ack.Body, want) {
		t.Fatalf("ack body=%x want=%x", ack.Body, want)
	}
	target, rest := splitGameServerUpperPacket(t, rest)
	if target.Header.Classification != 0 ||
		target.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(target.Body) != 3+currentItemListEntryWireSize ||
		target.Body[0] != currentSocketListMain ||
		binary.LittleEndian.Uint16(target.Body[1:3]) != 1 {
		t.Fatalf("target refresh header=%+v body=%x", target.Header, target.Body)
	}
	targetRow := target.Body[3:]
	if binary.LittleEndian.Uint16(targetRow[0:2]) != 5 ||
		binary.LittleEndian.Uint32(targetRow[2:6]) != 9001 ||
		targetRow[currentEquipmentVectorOffset] != 2 ||
		binary.LittleEndian.Uint32(targetRow[currentEquipmentVectorOffset+1:currentEquipmentVectorOffset+5]) != math.MaxUint32 ||
		binary.LittleEndian.Uint32(targetRow[currentEquipmentVectorOffset+5:currentEquipmentVectorOffset+9]) != math.MaxUint32 {
		t.Fatalf("target socket row=%x", targetRow)
	}
	material, rest := splitGameServerUpperPacket(t, rest)
	if material.Header.Classification != 0 ||
		material.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(material.Body) != 3+currentItemListEntryWireSize ||
		material.Body[0] != currentSocketListMain ||
		binary.LittleEndian.Uint16(material.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint16(material.Body[3:5]) != 70 ||
		binary.LittleEndian.Uint32(material.Body[5:9]) != 0 {
		t.Fatalf("material refresh header=%+v body=%x", material.Header, material.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing packets after incremental op914 refreshes=%x", rest)
	}
	// Material must be consumed.
	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if _, ok := inventory.Slots["0:70"]; ok {
		t.Fatalf("socket opener not consumed: %+v", inventory.Slots)
	}
	// Target must have socket data.
	data := currentEquipmentEmblemData(inventory.Slots["0:5"].Extra, inventory.Slots["0:5"].RawEntry)
	if data[0] != 2 {
		t.Fatalf("target socket count=%d want=2", data[0])
	}
}

func TestCurrentNoBody796AcknowledgesWithoutInventoryMutation(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 9001, Count: 1},
			"0:9": {ItemID: 5001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "no-body-796", conn: &bufferConn{}}

	if err := service.handleCurrentNoBody796(session, nil); err != nil {
		t.Fatalf("handle no-body op796: %v", err)
	}
	success, trailing := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if success.Header.Classification != dnfproto.DefaultChannelClassification ||
		success.Header.MsgID != currentNoBody796Opcode ||
		!bytes.Equal(success.Body, []byte{1}) ||
		len(trailing) != 0 {
		t.Fatalf("success packet=%+v body=%x trailing=%x", success.Header, success.Body, trailing)
	}
	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if inventory.Slots["0:9"].Count != 1 || inventory.Slots["0:5"].Extra["equipment_emblem_data"] != "" {
		t.Fatalf("no-body op796 mutated inventory=%+v", inventory.Slots)
	}
}

func TestCurrentEquipmentSocketOpenRejectsNonOpenerMaterial(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5":  {ItemID: 9001, Count: 1},
			"0:70": {ItemID: 5001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "equipment-socket-invalid-material", conn: &bufferConn{}}
	if _, err := service.commitCurrentEquipmentSocketOpen(session, currentSocketOpenRequest{TargetSlot: 5, TargetItemID: 9001, MaterialSlot: 70}); !errors.Is(err, errCurrentSocketMaterialInvalid) {
		t.Fatalf("open with non-opener material err=%v", err)
	}
	if inventory := mustLoadCurrentSocketInventory(t, repositories, "77"); inventory.Slots["0:70"].Count != 1 {
		t.Fatalf("non-opener material was consumed: %+v", inventory.Slots)
	}
}

func TestCurrentEquipmentSocketOpenResolvesClientKeyToUniqueInventoryItem(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:12": {ItemID: 9001, Count: 1},
			"0:70": {ItemID: 6001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "equipment-socket-client-key", conn: &bufferConn{}}

	result, err := service.commitCurrentEquipmentSocketOpen(session, currentSocketOpenRequest{
		TargetSlot:   2, // current EXE client collection key, not the durable slot
		TargetItemID: 9001,
		MaterialSlot: 70,
	})
	if err != nil {
		t.Fatalf("equipment socket open by client key: %v", err)
	}
	if result.Target != (currentSocketChangedSlot{ListType: currentSocketListMain, Slot: 12}) || len(result.Consumed) != 1 || result.Consumed[0].Slot != 70 {
		t.Fatalf("open result=%+v", result)
	}
	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if _, ok := inventory.Slots["0:70"]; ok {
		t.Fatalf("socket opener should be consumed: %+v", inventory.Slots)
	}
	data := currentEquipmentEmblemData(inventory.Slots["0:12"].Extra, inventory.Slots["0:12"].RawEntry)
	if data[0] != 2 || binary.LittleEndian.Uint32(data[1:5]) != 0xFFFFFFFF || binary.LittleEndian.Uint32(data[5:9]) != 0xFFFFFFFF {
		t.Fatalf("opened equipment data=%x", data)
	}
}

func TestCurrentEquipmentSocketOpenUsesExactSlotWithDuplicateTemplates(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5":  {ItemID: 9001, Count: 1},
			"0:6":  {ItemID: 9001, Count: 1},
			"0:70": {ItemID: 6001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "equipment-socket-duplicate-template", conn: &bufferConn{}}

	result, err := service.commitCurrentEquipmentSocketOpen(session, currentSocketOpenRequest{
		TargetSlot:   6,
		TargetItemID: 9001,
		MaterialSlot: 70,
	})
	if err != nil {
		t.Fatalf("equipment socket open at exact duplicate slot: %v", err)
	}
	if result.Target != (currentSocketChangedSlot{ListType: currentSocketListMain, Slot: 6}) {
		t.Fatalf("open result=%+v", result)
	}
	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if data := currentEquipmentEmblemData(inventory.Slots["0:5"].Extra, inventory.Slots["0:5"].RawEntry); data[0] != 0 {
		t.Fatalf("unselected duplicate was mutated: %x", data)
	}
	if data := currentEquipmentEmblemData(inventory.Slots["0:6"].Extra, inventory.Slots["0:6"].RawEntry); data[0] != 2 {
		t.Fatalf("selected duplicate socket data=%x", data)
	}
}

func TestCurrentEquipmentSocketOpenRepairsAlreadyOpenedInventoryItem(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	var opened [currentEquipmentEmblemDataBytes]byte
	opened[0] = 2
	target := dnfrepo.ItemStack{ItemID: 9001, Count: 1}
	currentApplyEquipmentEmblemDataToStack(&target, currentSocketListMain, 5, opened, currentEquipmentJewelSocketType(currentEquipmentPlacementRule{targetSlot: 17}))
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5":  target,
			"0:70": {ItemID: 6001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "equipment-socket-already-open", conn: &bufferConn{}}

	result, err := service.commitCurrentEquipmentSocketOpen(session, currentSocketOpenRequest{
		TargetSlot:   5,
		TargetItemID: 9001,
		MaterialSlot: 70,
	})
	if err != nil {
		t.Fatalf("repair already-open socket: %v", err)
	}
	if result.Target != (currentSocketChangedSlot{ListType: currentSocketListMain, Slot: 5}) || len(result.Consumed) != 0 {
		t.Fatalf("repair result=%+v", result)
	}
	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if inventory.Slots["0:70"].Count != 1 {
		t.Fatalf("socket opener consumed on already-open repair: %+v", inventory.Slots)
	}
	data := currentEquipmentEmblemData(inventory.Slots["0:5"].Extra, inventory.Slots["0:5"].RawEntry)
	if data[0] != 2 || binary.LittleEndian.Uint32(data[1:5]) != 0xFFFFFFFF || binary.LittleEndian.Uint32(data[5:9]) != 0xFFFFFFFF {
		t.Fatalf("already-open repair data=%x", data)
	}
}

func TestCurrentEquipmentEmblemAttachCanTargetLegacyStoredWornSlotViaActorSlot(t *testing.T) {
	service, repositories := newCurrentSocketTestService(t)
	ctx := context.Background()
	var opened [currentEquipmentEmblemDataBytes]byte
	currentEnsureEquipmentEmblemSocketsOpen(&opened, 2, 2)
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:9": {ItemID: 5101, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	equipped := dnfrepo.EquipmentEntry{
		SlotIndex: 11,
		ItemID:    9201,
		Extra:     map[string]string{"source": "pvf_create_equipment_list"},
	}
	currentApplyEquipmentEmblemDataToEquipment(&equipped, opened, currentEquipmentJewelSocketType(currentEquipmentPlacementRule{targetSlot: 11}))
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": equipped,
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	session := &gameSession{selectedCharacterID: 77, connID: "equipped-socket-test", conn: &bufferConn{}}

	result, err := service.commitCurrentEquipmentEmblemAttach(session, currentEmblemAttachRequest{
		TargetSlot:   12,
		TargetItemID: 9201,
		Emblems: []currentEmblemApplyRequest{{
			EmblemSlot:   9,
			EmblemItemID: 5101,
			SocketIndex:  0,
		}},
	})
	if err != nil {
		t.Fatalf("equipped equipment emblem attach: %v", err)
	}
	if !result.TargetEquipped || result.Target != (currentSocketChangedSlot{ListType: currentSocketListEquipment, Slot: 11}) {
		t.Fatalf("equipped attach result=%+v", result)
	}
	if inventory := mustLoadCurrentSocketInventory(t, repositories, "77"); len(inventory.Slots) != 0 {
		t.Fatalf("equipped attach did not consume emblem inventory=%+v", inventory.Slots)
	}
	equipment, found, err := repositories.Equipment.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load equipment found=%v err=%v", found, err)
	}
	data := currentEquipmentEmblemData(equipment.Entries["11"].Extra, equipment.Entries["11"].RawEntry)
	if data[0] != 2 || binary.LittleEndian.Uint32(data[1:5]) != 5101 {
		t.Fatalf("equipped emblem data=%x", data)
	}
}

func newCurrentSocketTestService(t *testing.T) (*Service, dnfrepo.Group) {
	t.Helper()
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":     "",
		"stackable/stackable.lst": "5001 `emblem/red_c.stk`\n5101 `emblem/red_s.stk`\n6001 `emblem/chn_makesocket.stk`\n",
		"equipment/equipment.lst": "9001 `armor/test_coat.equ`\n9101 `avatar/test_hat.equ`\n9201 `weapon/test_sword.equ`\n",
		"stackable/emblem/red_c.stk": "[name] `Red C`\n" +
			"[stackable type]\n `[avatar emblem]`\n" +
			"[stack limit]\n 999\n" +
			"[avatar emblem target type]\n `[C socket]`\n[/avatar emblem target type]\n",
		"stackable/emblem/red_s.stk": "[name] `Red S`\n" +
			"[stackable type]\n `[avatar emblem]`\n" +
			"[stack limit]\n 999\n" +
			"[avatar emblem target type]\n `[S socket]`\n[/avatar emblem target type]\n",
		"stackable/emblem/chn_makesocket.stk": "[name] `Socket Tool`\n[stackable type]\n `[etc]`\n[stack limit]\n 999\n",
		"equipment/armor/test_coat.equ":       "[name] `Test Coat`\n[equipment type]\n `[coat]`\n[durability]\n 45\n",
		"equipment/avatar/test_hat.equ": "[name] `Test Hat Avatar`\n" +
			"[equipment type]\n `[hat avatar]`\n" +
			"[avatar type select]\n `[C socket]`\n `[S socket]`\n[/avatar type select]\n",
		"equipment/weapon/test_sword.equ": "[name] `Test Sword`\n[equipment type]\n `[weapon]`\n[durability]\n 45\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatalf("new test catalog: %v", err)
	}
	repositories := testRepositoryGroup()
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		pvfItemCatalog:     catalog,
	}
	return service, repositories
}

func mustLoadCurrentSocketInventory(t *testing.T, repositories dnfrepo.Group, characterID string) dnfrepo.InventoryRecord {
	t.Helper()
	inventory, found, err := repositories.Inventory.Load(context.Background(), characterID)
	if err != nil || !found {
		t.Fatalf("load inventory character=%s found=%v err=%v", characterID, found, err)
	}
	return inventory
}

func buildCurrentSocketTestEmblemBody(targetSlot int16, targetItemID int64, emblems ...currentEmblemApplyRequest) []byte {
	var body packetWriter
	body.writeUint16(uint16(targetSlot))
	body.writeUint32(uint32(targetItemID))
	body.writeByte(byte(len(emblems)))
	for _, emblem := range emblems {
		body.writeUint16(uint16(emblem.EmblemSlot))
		body.writeUint32(uint32(emblem.EmblemItemID))
		body.writeByte(emblem.SocketIndex)
	}
	return body.bytes()
}
