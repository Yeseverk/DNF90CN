package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	dnfonlineevent "longheng.io/server/internal/modules/dnf/onlineevent"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentSpendTimeNextServiceBoundaryUsesShanghaiSixAM(t *testing.T) {
	location := dnfonlineevent.ChinaCalendar
	definition := dnfonlineevent.Definition{Calendar: location}
	before := time.Date(2026, 8, 4, 5, 59, 59, 0, location)
	if got, want := currentSpendTimeNextServiceBoundary(definition, before),
		time.Date(2026, 8, 4, 6, 0, 0, 0, location); !got.Equal(want) {
		t.Fatalf("before-boundary next=%s want=%s", got, want)
	}
	atBoundary := time.Date(2026, 8, 4, 6, 0, 0, 0, location)
	if got, want := currentSpendTimeNextServiceBoundary(definition, atBoundary),
		time.Date(2026, 8, 5, 6, 0, 0, 0, location); !got.Equal(want) {
		t.Fatalf("at-boundary next=%s want=%s", got, want)
	}
}

func TestSettleCurrentSpendTimeClockSplitsSixAMAndClaimsBothServiceDays(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "7",
		AccountID:   "account-1",
		Stats:       map[string]int64{"gold": 0},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "7",
		Slots:       make(map[string]dnfrepo.ItemStack),
	}); err != nil {
		t.Fatal(err)
	}

	const rewardID uint32 = 490006574
	itemCatalog := &pvfDungeonDropCatalog{
		source: bridgePVFSource{},
		itemCache: map[uint32]dungeonDropItemDefinition{rewardID: {
			ItemID: rewardID, Kind: dungeonDropItemStackable, StackLimit: 1000,
			SlotStart: 9, SlotEnd: 56,
		}},
	}
	definition := dnfonlineevent.Definition{
		ID:       "2347-test",
		Calendar: dnfonlineevent.ChinaCalendar,
		Stages: []dnfonlineevent.Stage{{
			ID: "stage-1", RequiredSeconds: 10,
			Items: []dnfonlineevent.ItemReward{{ItemID: int64(rewardID), Count: 1}},
		}},
	}
	service := &Service{
		pvfItemCatalog: itemCatalog,
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	boundary := time.Date(2026, 8, 4, 6, 0, 0, 0, dnfonlineevent.ChinaCalendar)
	session := &gameSession{}
	session.spendTime.characterID = 7
	session.spendTime.accountID = "account-1"
	session.spendTime.anchor = boundary.Add(-20 * time.Second)
	session.spendTime.catalog = &currentSpendTimeRuntimeCatalog{
		definition: definition,
	}

	settlement, err := service.settleCurrentSpendTimeClockLocked(session, boundary.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if settlement.claimErr != nil || !settlement.changed || len(settlement.rewardItems) != 2 {
		t.Fatalf("settlement=%+v", settlement)
	}
	if !session.spendTime.anchor.Equal(boundary.Add(10*time.Second)) ||
		session.spendTime.onlineSeconds != 10 || session.spendTime.completedStages != 1 {
		t.Fatalf("clock anchor=%s online=%d completed=%d",
			session.spendTime.anchor,
			session.spendTime.onlineSeconds,
			session.spendTime.completedStages)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "7")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if stack := inventory.Slots["0:9"]; stack.ItemID != int64(rewardID) || stack.Count != 2 {
		t.Fatalf("reward stack=%+v inventory=%+v", stack, inventory.Slots)
	}
}

func TestBuildCurrentSpendTimeEventInfoBodyMatchesCurrentNoPackFirstProcessDescriptor(t *testing.T) {
	rewards := []uint32{490006574, 490006575, 490006576, 490006577}
	body, err := buildCurrentSpendTimeEventInfoBody(rewards, 10800)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 184 {
		t.Fatalf("op108 body len=%d body=%x", len(body), body)
	}
	const (
		extendedOffset    = 17
		joustBaseOffset   = 68
		activeOffset      = 83
		paramsOffset      = 86
		joustActiveOffset = 134
		joustParamsOffset = 136
	)
	if binary.LittleEndian.Uint16(body[:2]) != 2 ||
		binary.LittleEndian.Uint16(body[2:4]) != uint16(currentSpendTimeEventID) ||
		binary.LittleEndian.Uint32(body[4:8]) != currentSpendTimeBaseCatalogValue ||
		binary.LittleEndian.Uint32(body[8:12]) != 0 ||
		binary.LittleEndian.Uint32(body[12:16]) != 0 || body[16] != 1 {
		t.Fatalf("op108 list prefix=%x", body[:17])
	}
	if body[extendedOffset] != 0 || body[extendedOffset+1] != 0 ||
		binary.LittleEndian.Uint32(body[19:23]) != 0 ||
		binary.LittleEndian.Uint32(body[23:27]) != 0 ||
		binary.LittleEndian.Uint32(body[27:31]) != 0 ||
		binary.LittleEndian.Uint32(body[31:35]) != 0 ||
		binary.LittleEndian.Uint32(body[35:39]) != 0 {
		t.Fatalf("op108 extended prefix=%x", body[extendedOffset:39])
	}
	routeLength := int(binary.LittleEndian.Uint32(body[39:43]))
	if routeLength != len(currentSpendTimeEventRoute) ||
		string(body[43:43+routeLength]) != currentSpendTimeEventRoute ||
		binary.LittleEndian.Uint32(body[63:67]) != 0 || body[67] != 0 {
		t.Fatalf("op108 click route extension=%x", body[39:joustBaseOffset])
	}
	if binary.LittleEndian.Uint16(body[joustBaseOffset:joustBaseOffset+2]) != uint16(currentJoustEventID) ||
		binary.LittleEndian.Uint32(body[joustBaseOffset+2:joustBaseOffset+6]) != currentSpendTimeBaseCatalogValue ||
		binary.LittleEndian.Uint32(body[joustBaseOffset+6:joustBaseOffset+10]) != 0 ||
		binary.LittleEndian.Uint32(body[joustBaseOffset+10:joustBaseOffset+14]) != 0 ||
		body[joustBaseOffset+14] != 0 {
		t.Fatalf("op108 joust base row=%x", body[joustBaseOffset:activeOffset])
	}
	if body[activeOffset] != 2 ||
		binary.LittleEndian.Uint16(body[activeOffset+1:paramsOffset]) != uint16(currentSpendTimeEventID) {
		t.Fatalf("op108 active prefix=%x", body[activeOffset:paramsOffset])
	}
	for index, want := range rewards {
		if got := binary.LittleEndian.Uint32(body[paramsOffset+index*4 : paramsOffset+(index+1)*4]); got != want {
			t.Fatalf("op108 param[%d]=%d want=%d", index, got, want)
		}
	}
	for index := len(rewards); index < 10; index++ {
		if got := binary.LittleEndian.Uint32(body[paramsOffset+index*4 : paramsOffset+(index+1)*4]); got != currentSpendTimeUnusedParam {
			t.Fatalf("op108 param[%d]=%#x want=%#x", index, got, currentSpendTimeUnusedParam)
		}
	}
	if got := binary.LittleEndian.Uint32(body[paramsOffset+40 : paramsOffset+44]); got != uint32(len(rewards)) {
		t.Fatalf("op108 reward count=%d", got)
	}
	if got := binary.LittleEndian.Uint32(body[paramsOffset+44 : paramsOffset+48]); got != 10800 {
		t.Fatalf("op108 total stage seconds=%d", got)
	}
	if binary.LittleEndian.Uint16(body[joustActiveOffset:joustParamsOffset]) != uint16(currentJoustEventID) {
		t.Fatalf("op108 joust active row=%x", body[joustActiveOffset:])
	}
	for index := 0; index < currentSpendTimeDescriptorParamCount; index++ {
		if got := binary.LittleEndian.Uint32(body[joustParamsOffset+index*4 : joustParamsOffset+(index+1)*4]); got != 0 {
			t.Fatalf("op108 joust param[%d]=%d want=0", index, got)
		}
	}
}

func TestBuildCurrentSpendTimeEventInfoBodyRejectsUnprovedDescriptor(t *testing.T) {
	tests := []struct {
		name    string
		rewards []uint32
		seconds uint32
	}{
		{name: "empty rewards", seconds: 10800},
		{name: "too many rewards", rewards: make([]uint32, 11), seconds: 10800},
		{name: "zero item", rewards: []uint32{490006574, 0}, seconds: 10800},
		{name: "zero duration", rewards: []uint32{490006574}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := buildCurrentSpendTimeEventInfoBody(test.rewards, test.seconds); err == nil {
				t.Fatal("descriptor unexpectedly accepted")
			}
		})
	}
}

func TestBuildCurrentSpendTimeProgressBodyMatchesCurrentNoPackNarrowRecord(t *testing.T) {
	body, err := buildCurrentSpendTimeProgressBody(7199, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 37 {
		t.Fatalf("op1206 body len=%d body=%x", len(body), body)
	}
	if binary.LittleEndian.Uint32(body[:4]) != currentSpendTimeEventID || body[4] != 0 {
		t.Fatalf("op1206 prefix=%x", body[:5])
	}
	if binary.LittleEndian.Uint32(body[5:9]) != 7199 || binary.LittleEndian.Uint32(body[9:13]) != 2 {
		t.Fatalf("op1206 progress=%x", body[5:13])
	}
	for index, value := range body[13:] {
		if value != 0 {
			t.Fatalf("op1206 raw padding[%d]=%d", index+8, value)
		}
	}
	if _, err := buildCurrentSpendTimeProgressBody(uint64(math.MaxUint32)+1, 0); err == nil {
		t.Fatal("op1206 accepted elapsed overflow")
	}
}

func TestSendCurrentSpendTimeProtocolStateKeepsProtectedDescriptorAndProgressAdjacent(t *testing.T) {
	connection := &bufferConn{}
	queue := newFakeCurrentDungeonDeathTimerQueue()
	queue.now = time.Unix(7200*321, 0).UTC().Add(10 * time.Minute)
	service := &Service{joustCatalog: mustTestJoustEventCatalog(t), gameplayTimers: queue}
	session := &gameSession{conn: connection, connID: "spend-time-protocol"}
	catalog := &currentSpendTimeRuntimeCatalog{
		rewardItemIDs:     []uint32{490006574, 490006575, 490006576, 490006577},
		totalStageSeconds: 10800,
	}
	descriptorSent, err := service.sendCurrentSpendTimeProtocolState(session, catalog, 7199, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if !descriptorSent {
		t.Fatal("first descriptor was not reported as sent")
	}

	wire := connection.write.Bytes()
	descriptor, rest := splitGameServerUpperPacketWithHeader(t, wire, dnfproto.GameServerUpperHeaderSize16)
	if descriptor.Header.Classification != 0 || descriptor.Header.MsgID != currentSpendTimeEventInfoMsgID ||
		descriptor.Header.Checksum != 1 || wire[15] != 1 {
		t.Fatalf("op108 protected header=%+v raw=%x", descriptor.Header, wire[:dnfproto.GameServerUpperHeaderSize16])
	}
	plainDescriptor, err := zlibDecompress(descriptor.Body)
	if err != nil {
		t.Fatalf("decompress op108: %v", err)
	}
	wantDescriptor, err := buildCurrentSpendTimeEventInfoBody(catalog.rewardItemIDs, catalog.totalStageSeconds)
	if err != nil {
		t.Fatal(err)
	}
	if len(plainDescriptor) != 184 || !bytes.Equal(plainDescriptor, wantDescriptor) {
		t.Fatalf("op108 plain=%x want=%x", plainDescriptor, wantDescriptor)
	}
	progress, rest := splitGameServerUpperPacket(t, rest)
	if progress.Header.Classification != 0 || progress.Header.MsgID != currentSpendTimeProgressMsgID {
		t.Fatalf("op1206 header=%+v", progress.Header)
	}
	wantProgress, err := buildCurrentSpendTimeProgressBody(7199, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(progress.Body, wantProgress) {
		t.Fatalf("op1206 body=%x want=%x", progress.Body, wantProgress)
	}
	state, rest := splitGameServerUpperPacket(t, rest)
	if state.Header.Classification != 0 || state.Header.MsgID != currentJoustStatePushMsgID ||
		len(state.Body) != 3 || state.Body[2] != currentJoustStateOpening {
		t.Fatalf("op1240 state header=%+v body=%x", state.Header, state.Body)
	}
	roster, rest := splitGameServerUpperPacket(t, rest)
	if roster.Header.Classification != 0 || roster.Header.MsgID != currentJoustRosterPushMsgID ||
		len(roster.Body) != 90 || binary.LittleEndian.Uint16(roster.Body[:2]) != binary.LittleEndian.Uint16(state.Body[:2]) {
		t.Fatalf("op1241 roster header=%+v body=%x", roster.Header, roster.Body)
	}
	pool, rest := splitGameServerUpperPacket(t, rest)
	if pool.Header.Classification != 0 || pool.Header.MsgID != currentJoustPoolPushMsgID ||
		len(pool.Body) != 46 || binary.LittleEndian.Uint16(pool.Body[:2]) != binary.LittleEndian.Uint16(state.Body[:2]) {
		t.Fatalf("op1242 pool header=%+v body=%x", pool.Header, pool.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("joust packets trailing=%x", rest)
	}
}

func TestSendCurrentSpendTimeProtocolStatePartialWriteNeverRequiresFirstDescriptorReplay(t *testing.T) {
	connection := &failNthDungeonWriteConn{failAt: 3, err: errors.New("joust state write failed")}
	queue := newFakeCurrentDungeonDeathTimerQueue()
	queue.now = time.Unix(7200*321, 0).UTC().Add(10 * time.Minute)
	service := &Service{clientAccounts: newClientAccountRegistry(), joustCatalog: mustTestJoustEventCatalog(t), gameplayTimers: queue}
	session := &gameSession{conn: connection, connID: "spend-time-partial", clientPID: 101}
	if err := service.RegisterClientAccount(session.clientPID, "account-1"); err != nil {
		t.Fatal(err)
	}
	catalog := &currentSpendTimeRuntimeCatalog{
		rewardItemIDs:     []uint32{490006574, 490006575, 490006576, 490006577},
		totalStageSeconds: 10800,
	}
	sendDescriptor, ready := service.beginCurrentSpendTimeEventInfo(session)
	if !sendDescriptor || !ready {
		t.Fatalf("descriptor reservation send=%t ready=%t", sendDescriptor, ready)
	}
	descriptorSent, err := service.sendCurrentSpendTimeProtocolState(session, catalog, 1, 0, sendDescriptor)
	if err == nil || !descriptorSent {
		t.Fatalf("first send descriptor_sent=%t err=%v", descriptorSent, err)
	}
	service.finishCurrentSpendTimeEventInfo(session, descriptorSent)
	if !session.spendTime.eventInfoSent {
		t.Fatal("successful op108 partial write did not advance process-lifetime gate")
	}
	sendDescriptor, ready = service.beginCurrentSpendTimeEventInfo(session)
	if sendDescriptor || !ready {
		t.Fatalf("retry descriptor reservation send=%t ready=%t", sendDescriptor, ready)
	}
	descriptorSent, err = service.sendCurrentSpendTimeProtocolState(
		session,
		catalog,
		1,
		0,
		sendDescriptor,
	)
	if err != nil || descriptorSent {
		t.Fatalf("progress retry descriptor_sent=%t err=%v", descriptorSent, err)
	}

	wire := connection.bufferConn.write.Bytes()
	descriptor, rest := splitGameServerUpperPacketWithHeader(t, wire, dnfproto.GameServerUpperHeaderSize16)
	if descriptor.Header.MsgID != currentSpendTimeEventInfoMsgID {
		t.Fatalf("first packet msg=%d", descriptor.Header.MsgID)
	}
	progress, rest := splitGameServerUpperPacket(t, rest)
	if progress.Header.MsgID != currentSpendTimeProgressMsgID {
		t.Fatalf("first progress packet msg=%d", progress.Header.MsgID)
	}
	progress, rest = splitGameServerUpperPacket(t, rest)
	if progress.Header.MsgID != currentSpendTimeProgressMsgID {
		t.Fatalf("retry progress packet msg=%d", progress.Header.MsgID)
	}
	state, rest := splitGameServerUpperPacket(t, rest)
	if state.Header.MsgID != currentJoustStatePushMsgID || len(state.Body) != 3 || state.Body[2] != currentJoustStateOpening {
		t.Fatalf("retry state packet msg=%d body=%x", state.Header.MsgID, state.Body)
	}
	roster, rest := splitGameServerUpperPacket(t, rest)
	if roster.Header.MsgID != currentJoustRosterPushMsgID {
		t.Fatalf("retry roster packet msg=%d", roster.Header.MsgID)
	}
	pool, rest := splitGameServerUpperPacket(t, rest)
	if pool.Header.MsgID != currentJoustPoolPushMsgID || len(rest) != 0 {
		t.Fatalf("retry pool packet msg=%d trailing=%x", pool.Header.MsgID, rest)
	}
}

func TestCurrentSpendTimeClaimItemsForCharacterRejectsReplayAndOtherCharacter(t *testing.T) {
	item := dnfonlineevent.CommittedItem{SlotKey: "0:9", SlotIndex: 9, ItemID: 490006574, Delta: 1, PostCount: 1, RawEntry: []byte{1, 2, 3}}
	if got := currentSpendTimeClaimItemsForCharacter(dnfonlineevent.ClaimResult{
		CharacterID: "7", Replayed: true, Items: []dnfonlineevent.CommittedItem{item},
	}, "7"); len(got) != 0 {
		t.Fatalf("replayed receipt leaked %d item updates", len(got))
	}
	if got := currentSpendTimeClaimItemsForCharacter(dnfonlineevent.ClaimResult{
		CharacterID: "8", Items: []dnfonlineevent.CommittedItem{item},
	}, "7"); len(got) != 0 {
		t.Fatalf("other-character receipt leaked %d item updates", len(got))
	}
	got := currentSpendTimeClaimItemsForCharacter(dnfonlineevent.ClaimResult{
		CharacterID: "7", Items: []dnfonlineevent.CommittedItem{item},
	}, "7")
	if len(got) != 1 || got[0].ItemID != item.ItemID {
		t.Fatalf("fresh same-character receipt=%+v", got)
	}
	got[0].RawEntry[0] = 9
	if item.RawEntry[0] != 1 {
		t.Fatal("claim item raw entry was not detached")
	}
}
