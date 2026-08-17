package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestParseCurrentBoosterOpenRequestUsesExactLiveBodies(t *testing.T) {
	random, err := parseCurrentBoosterOpenRequest([]byte{0x51, 0x00})
	if err != nil || random.Kind != currentBoosterRequestRandom || random.SourceSlot != 81 {
		t.Fatalf("random=%+v err=%v", random, err)
	}
	selectionBody := []byte{0x4e, 0x00, 0, 0, 0x43, 0x24, 0x0e, 0x06, 0, 0}
	selection, err := parseCurrentBoosterOpenRequest(selectionBody)
	if err != nil || selection.Kind != currentBoosterRequestSelection || selection.SourceSlot != 78 ||
		selection.SelectedItemID != 101590083 || selection.SelectionOption != 0 || selection.TrailingUIByte != 0 {
		t.Fatalf("selection=%+v err=%v", selection, err)
	}
	for _, malformed := range [][]byte{nil, {1}, {1, 2, 3}, make([]byte, 9), make([]byte, 11)} {
		if _, err := parseCurrentBoosterOpenRequest(malformed); err == nil {
			t.Fatalf("malformed body accepted: %x", malformed)
		}
	}
}

func TestParseCurrentBoosterOpenRequestUsesExactLiveEightPartBody(t *testing.T) {
	body, err := hex.DecodeString(
		"5d000100" +
			"5861b5065088b50638afb50610ecb4060b9eb4060bc5b4061a13b5064c3ab506" +
			"08" +
			"5861b50604" +
			"5088b50604" +
			"38afb50602" +
			"10ecb40602" +
			"0b9eb40606" +
			"0bc5b40600" +
			"1a13b50604" +
			"4c3ab50600" +
			"00",
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := parseCurrentBoosterOpenRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	wantSelections := []currentBoosterSelectionRequest{
		{ItemID: 112550232, Option: 4},
		{ItemID: 112560208, Option: 4},
		{ItemID: 112570168, Option: 2},
		{ItemID: 112520208, Option: 2},
		{ItemID: 112500235, Option: 6},
		{ItemID: 112510219, Option: 0},
		{ItemID: 112530202, Option: 4},
		{ItemID: 112540236, Option: 0},
	}
	if request.Kind != currentBoosterRequestSelection ||
		request.SourceSlot != 93 ||
		request.SelectionContext != 1 ||
		request.TrailingUIByte != 0 ||
		!reflect.DeepEqual(request.Selections, wantSelections) {
		t.Fatalf("request=%+v want_selections=%+v", request, wantSelections)
	}
	for name, mutate := range map[string]func([]byte){
		"count": func(value []byte) { value[36] = 7 },
		"record_item": func(value []byte) {
			value[37]++
		},
		"duplicate_prefix_and_record": func(value []byte) {
			copy(value[8:12], value[4:8])
			copy(value[42:46], value[37:41])
		},
	} {
		t.Run(name, func(t *testing.T) {
			malformed := append([]byte(nil), body...)
			mutate(malformed)
			if _, err := parseCurrentBoosterOpenRequest(malformed); err == nil {
				t.Fatalf("malformed body accepted: %x", malformed)
			}
		})
	}
	if _, err := parseCurrentBoosterOpenRequest(body[:len(body)-1]); err == nil {
		t.Fatal("truncated multi-selection body accepted")
	}
}

func TestBuildCurrentBoosterSuccessBodyMatchesCurrentEXEReader(t *testing.T) {
	result := currentBoosterCommitResult{
		SourceItemID:    490702325,
		SourceSlot:      78,
		SourceRemaining: 0,
		Rewards: []currentBoosterGrantedReward{
			{ItemID: 101590083, Count: 1},
		},
	}
	body := buildCurrentBoosterSuccessBody(result)
	want := make([]byte, 24)
	binary.LittleEndian.PutUint32(want[0:4], 490702325)
	binary.LittleEndian.PutUint16(want[4:6], 78)
	binary.LittleEndian.PutUint32(want[6:10], 0)
	binary.LittleEndian.PutUint32(want[10:14], 0)
	binary.LittleEndian.PutUint16(want[14:16], 1)
	binary.LittleEndian.PutUint32(want[16:20], 101590083)
	binary.LittleEndian.PutUint32(want[20:24], 1)
	if !bytes.Equal(body, want) {
		t.Fatalf("body=%x want=%x", body, want)
	}
}

func TestValidateCurrentBoosterSourceExpirationPrefersInstanceDeadline(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 30, 0, 0, time.UTC)
	staticExpired := time.Date(2017, 11, 1, 14, 0, 0, 0, time.UTC)
	instanceFuture := now.Add(24 * time.Hour)
	definition := dungeonDropItemDefinition{
		ItemID:         490701424,
		Kind:           dungeonDropItemStackable,
		ExpirationDate: staticExpired,
	}
	stack := dnfrepo.ItemStack{
		ItemID:   int64(definition.ItemID),
		Count:    1,
		ExpireAt: instanceFuture,
		Extra: map[string]string{
			"expire_unix": strconv.FormatInt(instanceFuture.Unix(), 10),
		},
	}
	if err := validateCurrentBoosterSourceExpiration(stack, definition, now); err != nil {
		t.Fatalf("future instance deadline rejected: %v", err)
	}
	if err := validateCurrentBoosterSourceExpiration(dnfrepo.ItemStack{
		ItemID: int64(definition.ItemID),
		Count:  1,
	}, definition, now); !errors.Is(err, errCurrentBoosterExpired) {
		t.Fatalf("missing instance deadline err=%v want=%v", err, errCurrentBoosterExpired)
	}
	if err := validateCurrentBoosterSourceExpiration(dnfrepo.ItemStack{
		ItemID:   int64(definition.ItemID),
		Count:    1,
		ExpireAt: now.Add(-time.Second),
	}, definition, now); !errors.Is(err, errCurrentBoosterExpired) {
		t.Fatalf("expired instance deadline err=%v want=%v", err, errCurrentBoosterExpired)
	}
}

func TestCurrentBoosterRandomRouteConsumesSourceAndGrantsPVFReward(t *testing.T) {
	catalog := mustCurrentBoosterTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:75": {ItemID: 500, Count: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentBoosterTestService(catalog, repositories)
	session := &gameSession{conn: connection, connID: "booster-random-test", selectedCharacterID: 19}
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketUseBoosterItem),
		[]byte{75, 0},
	); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != uint16(dnfenum.CmdPacketUseBoosterItem) {
		t.Fatalf("ack header=%+v", ack.Header)
	}
	wantAck := append([]byte{1}, buildCurrentBoosterSuccessBody(currentBoosterCommitResult{
		SourceItemID:    500,
		SourceSlot:      75,
		SourceRemaining: 1,
		Rewards:         []currentBoosterGrantedReward{{ItemID: 600, Count: 3}},
	})...)
	if !bytes.Equal(ack.Body, wantAck) {
		t.Fatalf("ack body=%x want=%x", ack.Body, wantAck)
	}
	rest = assertCurrentBoosterSourceRefresh(t, rest, 75, 500, 1)
	list0, trailing := splitCurrentSceneItemListPacket(t, rest)
	if len(trailing) != 0 || len(list0.Body) == 0 || list0.Body[0] != dnfrepo.MainInventoryListType {
		t.Fatalf("list0 body=%x trailing=%d", list0.Body, len(trailing))
	}

	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if source := inventory.Slots["0:75"]; source.ItemID != 500 || source.Count != 1 {
		t.Fatalf("source=%+v", source)
	}
	// Material rewards follow the established quick-slot preference before
	// falling back to the material bag segment.
	reward, found := inventory.Slots["0:3"]
	if !found || reward.ItemID != 600 || reward.Count != 3 || reward.Extra["source"] != "booster_item" || len(reward.RawEntry) != currentItemListEntryWireSize {
		t.Fatalf("reward=%+v found=%t", reward, found)
	}
}

func TestCurrentLotteryDoubleRewardUsesOp27AndConsumesDailyQuota(t *testing.T) {
	catalog := mustCurrentBoosterTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Stats:       map[string]int64{},
	}); err != nil {
		t.Fatal(err)
	}
	account := dnfrepo.AccountRecord{AccountID: "account-1"}
	premium.Upsert(&account, premium.DevilSlotType(premium.DevilSlotDoubleJar), 7*24*60*60, 1, time.Now().UTC())
	if err := repositories.Account.Save(ctx, account); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:75": {ItemID: 508, Count: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentBoosterTestService(catalog, repositories)
	session := &gameSession{conn: connection, connID: "lottery-double-test", selectedCharacterID: 19}

	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketUseLotteryItem),
		[]byte{1, 0, 75, 0},
	); err != nil {
		t.Fatal(err)
	}
	phase, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 ||
		phase.Header.MsgID != uint16(dnfenum.CmdPacketUseLotteryItem) ||
		!bytes.Equal(phase.Body, buildCurrentLotteryPhaseStartBody()) {
		t.Fatalf("phase header=%+v body=%x trailing=%d", phase.Header, phase.Body, len(trailing))
	}

	beforeConfirm := connection.write.Len()
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketOverflowInfo),
		[]byte{1, 0x1b, 0},
	); err != nil {
		t.Fatal(err)
	}
	resultPacket, _ := splitGameServerUpperPacket(t, connection.write.Bytes()[beforeConfirm:])
	if resultPacket.Header.MsgID != uint16(dnfenum.CmdPacketUseLotteryItem) ||
		len(resultPacket.Body) < 13 ||
		resultPacket.Body[0] != 1 ||
		binary.LittleEndian.Uint32(resultPacket.Body[5:9]) != 600 ||
		binary.LittleEndian.Uint32(resultPacket.Body[9:13]) != 6 {
		t.Fatalf("lottery result header=%+v body=%x", resultPacket.Header, resultPacket.Body)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if source := inventory.Slots["0:75"]; source.ItemID != 508 || source.Count != 1 {
		t.Fatalf("source=%+v", source)
	}
	reward, found := inventory.Slots["0:3"]
	if !found || reward.ItemID != 600 || reward.Count != 6 {
		t.Fatalf("double reward=%+v found=%t", reward, found)
	}
	character, found, err := repositories.Character.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if got := premium.DailyUsage(character, premium.DevilSlotDoubleJar, time.Now().UTC()); got != 1 {
		t.Fatalf("double-jar daily usage=%d, want 1", got)
	}
}

func TestCurrentBoosterSoulRewardUsesAccountSharedWarehouse(t *testing.T) {
	catalog := mustCurrentBoosterTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:75": {ItemID: 507, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}

	connection := &bufferConn{}
	service := currentBoosterTestService(catalog, repositories)
	session := &gameSession{conn: connection, connID: "booster-soul-test", selectedCharacterID: 19}
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketUseBoosterItem),
		[]byte{75, 0},
	); err != nil {
		t.Fatal(err)
	}

	account, found, err := repositories.AccountInventory.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%t err=%v", found, err)
	}
	soul := account.Slots["0:363"]
	if soul.ItemID != 10099774 || soul.Count != 5 || soul.Extra["source"] != "booster_item" {
		t.Fatalf("shared soul=%+v", soul)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load character inventory found=%t err=%v", found, err)
	}
	for key, stack := range inventory.Slots {
		if stack.ItemID == 10099774 {
			t.Fatalf("soul leaked into character inventory key=%s stack=%+v", key, stack)
		}
	}
}

func TestCurrentBoosterRewardInheritsFutureSourceDeadlineWhenStaticRewardExpired(t *testing.T) {
	catalog := mustCurrentBoosterTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	instanceExpire := time.Date(2037, time.December, 31, 15, 59, 59, 0, time.UTC)
	sourceStack := dnfrepo.ItemStack{
		ItemID:   504,
		Count:    1,
		ExpireAt: instanceExpire,
		Extra: map[string]string{
			"expire_unix": strconv.FormatInt(instanceExpire.Unix(), 10),
		},
	}
	sourceEntry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, 78, sourceStack)
	sourceStack.RawEntry = append([]byte(nil), sourceEntry.data[:]...)
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:78": sourceStack},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentBoosterTestService(catalog, repositories)
	session := &gameSession{conn: connection, connID: "booster-nested-expire-test", selectedCharacterID: 19}
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketUseBoosterItem),
		[]byte{78, 0},
	); err != nil {
		t.Fatal(err)
	}
	ack, _ := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketUseBoosterItem) || ack.Body[0] != 1 {
		t.Fatalf("op160 ack header=%+v body=%x", ack.Header, ack.Body)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	var reward dnfrepo.ItemStack
	rewardFound := false
	for _, stack := range inventory.Slots {
		if stack.ItemID == 605 {
			reward = stack
			rewardFound = true
			break
		}
	}
	if !rewardFound || !reward.ExpireAt.Equal(instanceExpire) ||
		reward.Extra["expire_unix"] != strconv.FormatInt(instanceExpire.Unix(), 10) ||
		binary.LittleEndian.Uint32(reward.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != uint32(instanceExpire.Unix()) {
		t.Fatalf("reward=%+v found=%t want_expire=%s", reward, rewardFound, instanceExpire)
	}
}

func TestCurrentBoosterSelectionRouteGrantsAvatarAndRefreshesBothLists(t *testing.T) {
	catalog := mustCurrentBoosterTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:76": {ItemID: 501, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentBoosterTestService(catalog, repositories)
	session := &gameSession{conn: connection, connID: "booster-selection-test", selectedCharacterID: 19}
	body := make([]byte, currentBoosterSelectionBodySize)
	binary.LittleEndian.PutUint16(body[0:2], 76)
	binary.LittleEndian.PutUint32(body[4:8], 700)
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketUseBoosterItem), body); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	wantAck := append([]byte{1}, buildCurrentBoosterSuccessBody(currentBoosterCommitResult{
		SourceItemID: 501,
		SourceSlot:   76,
		Rewards:      []currentBoosterGrantedReward{{ItemID: 700, Count: 1}},
	})...)
	if !bytes.Equal(ack.Body, wantAck) {
		t.Fatalf("ack body=%x want=%x", ack.Body, wantAck)
	}
	rest = assertCurrentBoosterSourceRefresh(t, rest, 76, math.MaxUint32, 0)
	list0, rest := splitCurrentSceneItemListPacket(t, rest)
	list1, trailing := splitCurrentSceneItemListPacket(t, rest)
	if len(trailing) != 0 || list0.Body[0] != 0 || list1.Body[0] != 1 {
		t.Fatalf("list0=%x list1=%x trailing=%d", list0.Body, list1.Body, len(trailing))
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, stillPresent := inventory.Slots["0:76"]; stillPresent {
		t.Fatal("source was not consumed")
	}
	avatar, found := inventory.Slots["1:0"]
	if !found || avatar.ItemID != 700 || avatar.Count != 1 || avatar.Extra["amount_or_count"] != "0" || avatar.Extra["source"] != "booster_item" {
		t.Fatalf("avatar=%+v found=%t", avatar, found)
	}
}

func TestCurrentBoosterMultiSelectionRouteGrantsWholePVFCategory(t *testing.T) {
	catalog := mustCurrentBoosterTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:93": {ItemID: 505, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	body, err := hex.DecodeString(
		"5d000100" +
			"5861b5065088b50638afb50610ecb4060b9eb4060bc5b4061a13b5064c3ab506" +
			"08" +
			"5861b50604" +
			"5088b50604" +
			"38afb50602" +
			"10ecb40602" +
			"0b9eb40606" +
			"0bc5b40600" +
			"1a13b50604" +
			"4c3ab50600" +
			"00",
	)
	if err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentBoosterTestService(catalog, repositories)
	session := &gameSession{conn: connection, connID: "booster-multi-selection-test", selectedCharacterID: 19}
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketUseBoosterItem), body); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Body[0] != 1 {
		t.Fatalf("failure ack=%x", ack.Body)
	}
	rest = assertCurrentBoosterSourceRefresh(t, rest, 93, math.MaxUint32, 0)
	list0, rest := splitCurrentSceneItemListPacket(t, rest)
	list1, trailing := splitCurrentSceneItemListPacket(t, rest)
	if len(trailing) != 0 || list0.Body[0] != 0 || list1.Body[0] != 1 {
		t.Fatalf("list0=%x list1=%x trailing=%d", list0.Body, list1.Body, len(trailing))
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, stillPresent := inventory.Slots["0:93"]; stillPresent {
		t.Fatal("source was not consumed")
	}
	want := map[int64]string{
		112550232: "4",
		112560208: "4",
		112570168: "2",
		112520208: "2",
		112500235: "6",
		112510219: "0",
		112530202: "4",
		112540236: "0",
	}
	got := make(map[int64]string, len(want))
	for index := 0; index < len(want); index++ {
		stack, found := inventory.Slots["1:"+strconv.Itoa(index)]
		if !found || stack.Count != 1 || stack.Extra["source"] != "booster_item" {
			t.Fatalf("slot=%d stack=%+v found=%t", index, stack, found)
		}
		got[stack.ItemID] = stack.Extra["ext_data0"]
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("avatar item/options=%v want=%v", got, want)
	}
}

func TestCurrentBoosterZeroSelectionSingleIgnoresPVFExchangeMaterial(t *testing.T) {
	catalog := mustCurrentBoosterTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:95": {ItemID: 506, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	body, err := hex.DecodeString("5f000100e923b6060000")
	if err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentBoosterTestService(catalog, repositories)
	session := &gameSession{conn: connection, connID: "booster-zero-single-test", selectedCharacterID: 19}
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketUseBoosterItem), body); err != nil {
		t.Fatal(err)
	}
	ack, _ := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Body[0] != 1 {
		t.Fatalf("failure ack=%x", ack.Body)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, found := inventory.Slots["0:95"]; found {
		t.Fatal("source was not consumed")
	}
	avatar, found := inventory.Slots["1:0"]
	if !found || avatar.ItemID != 112600041 || avatar.Count != 1 || avatar.Extra["ext_data0"] != "0" {
		t.Fatalf("avatar=%+v found=%t", avatar, found)
	}
}

func TestCurrentBoosterForgedSelectionReturnsFailureWithoutMutation(t *testing.T) {
	catalog := mustCurrentBoosterTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	before := dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{"0:76": {ItemID: 501, Count: 1}}}
	if err := repositories.Inventory.Save(ctx, before); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentBoosterTestService(catalog, repositories)
	session := &gameSession{conn: connection, connID: "booster-forged-test", selectedCharacterID: 19}
	body := make([]byte, currentBoosterSelectionBodySize)
	binary.LittleEndian.PutUint16(body[0:2], 76)
	binary.LittleEndian.PutUint32(body[4:8], 701)
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketUseBoosterItem), body); err != nil {
		t.Fatal(err)
	}
	ack, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || !bytes.Equal(ack.Body, []byte{0, 4}) {
		t.Fatalf("failure body=%x trailing=%d", ack.Body, len(trailing))
	}
	after, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found || !reflect.DeepEqual(after.Slots, before.Slots) {
		t.Fatalf("inventory changed found=%t err=%v after=%+v", found, err, after.Slots)
	}
}

func TestCurrentBoosterCreatureSelectionUsesPetBodyContainer(t *testing.T) {
	catalog := mustCurrentBoosterTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:76": {ItemID: 503, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentBoosterTestService(catalog, repositories)
	session := &gameSession{conn: connection, connID: "booster-creature-test", selectedCharacterID: 19}
	body := make([]byte, currentBoosterSelectionBodySize)
	binary.LittleEndian.PutUint16(body[0:2], 76)
	binary.LittleEndian.PutUint32(body[4:8], 702)
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketUseBoosterItem), body); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Body[0] != 1 {
		t.Fatalf("failure ack=%x", ack.Body)
	}
	rest = assertCurrentBoosterSourceRefresh(t, rest, 76, math.MaxUint32, 0)
	list0, rest := splitCurrentSceneItemListPacket(t, rest)
	list7, trailing := splitCurrentSceneItemListPacket(t, rest)
	if len(trailing) != 0 || list0.Body[0] != 0 || list7.Body[0] != currentPetInventoryListType {
		t.Fatalf("list0=%x list7=%x trailing=%d", list0.Body, list7.Body, len(trailing))
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	pet, found := inventory.Slots["7:0"]
	if !found || pet.ItemID != 702 || pet.Count != 1 || pet.Extra["source"] != "booster_item" || len(pet.RawEntry) != currentItemListEntryWireSize {
		t.Fatalf("pet=%+v found=%t", pet, found)
	}
}

func TestCurrentBoosterPetArtifactUsesPetEquipmentSegment(t *testing.T) {
	catalog := mustCurrentBoosterTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:77": {ItemID: 502, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := currentBoosterTestService(catalog, repositories)
	session := &gameSession{conn: connection, connID: "booster-artifact-test", selectedCharacterID: 19}
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketUseBoosterItem), []byte{77, 0}); err != nil {
		t.Fatal(err)
	}
	_, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	rest = assertCurrentBoosterSourceRefresh(t, rest, 77, math.MaxUint32, 0)
	list0, rest := splitCurrentSceneItemListPacket(t, rest)
	list7, trailing := splitCurrentSceneItemListPacket(t, rest)
	if len(trailing) != 0 || list0.Body[0] != 0 || list7.Body[0] != currentPetInventoryListType {
		t.Fatalf("list0=%x list7=%x trailing=%d", list0.Body, list7.Body, len(trailing))
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	artifact, found := inventory.Slots["7:140"]
	if !found || artifact.ItemID != 701 || artifact.Count != 1 || artifact.Extra["amount_or_count"] != "0" || binary.LittleEndian.Uint32(artifact.RawEntry[6:10]) != 0 {
		t.Fatalf("artifact=%+v found=%t", artifact, found)
	}
}

func TestRealPVFSummerBoosterDefinitions(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the live summer booster definitions")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	multi, err := resolveCurrentBoosterDefinition(catalog, 490701318, currentBoosterRequestSelection)
	if err != nil {
		t.Fatal(err)
	}
	wantMultiCategory := []uint32{
		112550232,
		112560208,
		112570168,
		112520208,
		112500235,
		112510219,
		112530202,
		112540236,
	}
	if multi.SelectionRequired != 0 || len(multi.SelectionCategory[1]) != len(wantMultiCategory) {
		t.Fatalf("multi selection_required=%d category1=%+v", multi.SelectionRequired, multi.SelectionCategory[1])
	}
	for index, itemID := range wantMultiCategory {
		if multi.SelectionCategory[1][index].ItemID != itemID {
			t.Fatalf("multi category1[%d]=%+v want_item=%d", index, multi.SelectionCategory[1][index], itemID)
		}
	}
	weapon, err := resolveCurrentBoosterDefinition(catalog, 490701320, currentBoosterRequestSelection)
	if err != nil {
		t.Fatal(err)
	}
	if weapon.SelectionRequired != 0 ||
		len(weapon.SelectionCategory[1]) != 1 ||
		weapon.SelectionCategory[1][0].ItemID != 112600041 ||
		weapon.MaterialItemID != 490701321 ||
		weapon.MaterialCount != 2 {
		t.Fatalf("weapon=%+v category1=%+v", weapon, weapon.SelectionCategory[1])
	}
	selection, err := resolveCurrentBoosterDefinition(catalog, 490702325, currentBoosterRequestSelection)
	if err != nil {
		t.Fatal(err)
	}
	wantSelection := []currentBoosterSelectionCandidate{
		{ItemID: 101590082, Count: 1, CategoryKind: 3, Option: 0},
		{ItemID: 101590083, Count: 1, CategoryKind: 3, Option: 0},
		{ItemID: 101590084, Count: 1, CategoryKind: 3, Option: 0},
		{ItemID: 101590085, Count: 1, CategoryKind: 3, Option: 0},
	}
	if !reflect.DeepEqual(selection.Selection, wantSelection) {
		t.Fatalf("selection=%+v want=%+v", selection.Selection, wantSelection)
	}
	for _, sample := range []struct {
		sourceItemID uint32
		want         []currentBoosterSelectionCandidate
	}{
		{
			sourceItemID: 490702327,
			want: []currentBoosterSelectionCandidate{
				{ItemID: 490702340, Count: 5},
				{ItemID: 490702335, Count: 1},
			},
		},
		{
			sourceItemID: 490702320,
			want: []currentBoosterSelectionCandidate{
				{ItemID: 400990198, Count: 1},
				{ItemID: 400990199, Count: 1},
			},
		},
		{
			sourceItemID: 490702341,
			want: []currentBoosterSelectionCandidate{
				{ItemID: 490702355, Count: 1},
				{ItemID: 490702356, Count: 1},
				{ItemID: 490702357, Count: 1},
			},
		},
	} {
		definition, err := resolveCurrentBoosterDefinition(catalog, sample.sourceItemID, currentBoosterRequestSelection)
		if err != nil {
			t.Fatalf("resolve selection item=%d: %v", sample.sourceItemID, err)
		}
		if !reflect.DeepEqual(definition.Selection, sample.want) {
			t.Fatalf("selection item=%d got=%+v want=%+v", sample.sourceItemID, definition.Selection, sample.want)
		}
	}
	for _, itemID := range []uint32{490702317, 490702321} {
		definition, err := resolveCurrentBoosterDefinition(catalog, itemID, currentBoosterRequestRandom)
		if err != nil {
			t.Fatalf("resolve random item=%d: %v", itemID, err)
		}
		if definition.Random.Kind != "random" || len(definition.Random.Groups) != 1 || len(definition.Random.Groups[0].Entries) < 3 {
			t.Fatalf("random item=%d definition=%+v", itemID, definition.Random)
		}
	}
	for _, sample := range []struct {
		sourceItemID uint32
		rewardItemID uint32
		placement    currentBoosterRewardPlacement
		sealed       bool
	}{
		{sourceItemID: 490702317, rewardItemID: 400330118, placement: currentBoosterRewardMain, sealed: true},
		{sourceItemID: 490702321, rewardItemID: 490702361, placement: currentBoosterRewardMain},
		{sourceItemID: 490702361, rewardItemID: 10006786, placement: currentBoosterRewardPetArtifact},
	} {
		definition, err := resolveCurrentBoosterDefinition(catalog, sample.sourceItemID, currentBoosterRequestRandom)
		if err != nil {
			t.Fatalf("resolve random item=%d: %v", sample.sourceItemID, err)
		}
		amounts, options, err := resolveCurrentBoosterRewardAmounts(
			definition,
			currentBoosterOpenRequest{Kind: currentBoosterRequestRandom},
			func(int64) (int64, error) { return 0, nil },
		)
		if err != nil {
			t.Fatalf("roll random item=%d: %v", sample.sourceItemID, err)
		}
		rewards, err := resolveCurrentBoosterRewards(catalog, amounts, options, dnfrepo.ItemStack{}, time.Now().UTC())
		if err != nil {
			t.Fatalf("resolve reward item=%d: %v", sample.sourceItemID, err)
		}
		if len(rewards) != 1 || rewards[0].Definition.ItemID != sample.rewardItemID ||
			rewards[0].Placement != sample.placement || rewards[0].Seal != sample.sealed {
			t.Fatalf("source item=%d rewards=%+v", sample.sourceItemID, rewards)
		}
	}
}

func TestRealPVFShiningRemyUsesUnlimitedStackContract(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the live Shining Remy stack contract")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := catalog.ResolveItem(2660671)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Kind != dungeonDropItemStackable ||
		definition.PVFPath != "stackable/cash/hands_remy_shine.stk" ||
		normalizeDungeonDropStackableType(definition.StackableType) != "[waste]" ||
		definition.StackLimit != 0 {
		t.Fatalf("definition=%+v", definition)
	}
	inventory := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:8": {
				ItemID: int64(definition.ItemID),
				Count:  199,
				Extra: map[string]string{
					"item_kind":      string(definition.Kind),
					"pvf_path":       definition.PVFPath,
					"stackable_type": definition.StackableType,
				},
			},
		},
	}
	slots, err := grantCurrentCeraShopProduct(&inventory, definition, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(slots, []uint16{8}) || inventory.Slots["0:8"].Count != 399 || len(inventory.Slots) != 1 {
		t.Fatalf("slots=%v inventory=%+v", slots, inventory.Slots)
	}
}

func TestRealPVFDreamPlatinumSelectionShape(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the live Dream Platinum selection")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := resolveCurrentBoosterDefinition(catalog, 490701743, currentBoosterRequestSelection)
	if err != nil {
		t.Fatalf("resolve Dream Platinum selection: %v", err)
	}
	const emptyJobGrowCategory = uint16(1 | 4<<8)
	if candidates, found := definition.SelectionCategory[emptyJobGrowCategory]; !found || len(candidates) != 0 {
		t.Fatalf("empty category=%d candidates=%+v found=%t", emptyJobGrowCategory, candidates, found)
	}
	request := currentBoosterOpenRequest{
		Kind:             currentBoosterRequestSelection,
		SelectionContext: 1,
		Selections: []currentBoosterSelectionRequest{{
			ItemID: 10095199,
		}},
	}
	selected, err := currentBoosterSelectedCandidates(definition, request)
	if err != nil {
		t.Fatalf("select live female-slayer platinum emblem: %v", err)
	}
	if len(selected) != 1 || selected[0].ItemID != 10095199 || selected[0].Count != 1 {
		t.Fatalf("selected=%+v", selected)
	}
	request.SelectionContext = emptyJobGrowCategory
	if _, err := currentBoosterSelectedCandidates(definition, request); !errors.Is(err, errCurrentBoosterSelectionInvalid) {
		t.Fatalf("empty category accepted: %v", err)
	}
}

func TestResolveCurrentBoosterRewardAmountsUsesPVFWeights(t *testing.T) {
	definition := currentBoosterDefinition{Random: alignedMagicBoxResolutionForTest()}
	amounts, _, err := resolveCurrentBoosterRewardAmounts(
		definition,
		currentBoosterOpenRequest{Kind: currentBoosterRequestRandom},
		func(limit int64) (int64, error) {
			if limit != 10 {
				t.Fatalf("limit=%d", limit)
			}
			return 9, nil
		},
	)
	if err != nil || !reflect.DeepEqual(amounts, map[uint32]uint32{11: 2}) {
		t.Fatalf("amounts=%v err=%v", amounts, err)
	}
	_, _, err = resolveCurrentBoosterRewardAmounts(
		definition,
		currentBoosterOpenRequest{Kind: currentBoosterRequestRandom},
		func(int64) (int64, error) { return 0, errors.New("entropy unavailable") },
	)
	if err == nil {
		t.Fatal("entropy failure accepted")
	}
}

func TestGrantCurrentCeraShopProductFillsPartialStackBeforeSpill(t *testing.T) {
	inventory := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:9": {ItemID: 600, Count: 60},
		},
	}
	definition := dungeonDropItemDefinition{
		ItemID:        600,
		Kind:          dungeonDropItemStackable,
		StackableType: "[material]",
		StackLimit:    100,
		SlotStart:     9,
		SlotEnd:       104,
	}
	slots, err := grantCurrentCeraShopProduct(&inventory, definition, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(slots, []uint16{9, 3}) {
		t.Fatalf("slots=%v", slots)
	}
	if stack := inventory.Slots["0:9"]; stack.ItemID != 600 || stack.Count != 100 || len(stack.RawEntry) != currentItemListEntryWireSize {
		t.Fatalf("filled stack=%+v", stack)
	}
	if stack := inventory.Slots["0:3"]; stack.ItemID != 600 || stack.Count != 60 || len(stack.RawEntry) != currentItemListEntryWireSize {
		t.Fatalf("spill stack=%+v", stack)
	}
}

func TestGrantCurrentCeraShopProductRoutesReviveCoinRewardToWallet(t *testing.T) {
	inventory := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:99":  {ItemID: 1, Count: 30},
			"0:108": {ItemID: 1, Count: 20},
		},
	}
	definition := dungeonDropItemDefinition{
		ItemID:        42,
		Kind:          dungeonDropItemStackable,
		PVFPath:       "stackable/cash/coin_general.stk",
		StackableType: "[waste]",
		StackLimit:    100,
		SlotStart:     3,
		SlotEnd:       8,
	}
	slots, err := grantCurrentCeraShopProduct(&inventory, definition, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(slots, []uint16{1}) {
		t.Fatalf("slots=%v", slots)
	}
	if _, found := inventory.Slots["0:99"]; found {
		t.Fatal("legacy wallet row 0:99 remains")
	}
	if _, found := inventory.Slots["0:108"]; found {
		t.Fatal("legacy wallet row 0:108 remains")
	}
	if wallet := inventory.Slots["0:1"]; wallet.ItemID != 1 ||
		wallet.Count != 53 ||
		wallet.Extra["amount_or_count"] != "53" {
		t.Fatalf("wallet=%+v", wallet)
	}
}

func TestGrantCurrentCeraShopProductMergesPVFUnlimitedStack(t *testing.T) {
	inventory := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:8": {
				ItemID: 2660671,
				Count:  199,
				Extra: map[string]string{
					"item_kind":      "stackable",
					"pvf_path":       "stackable/cash/hands_remy_shine.stk",
					"stackable_type": "[waste]",
				},
			},
		},
	}
	definition := dungeonDropItemDefinition{
		ItemID:        2660671,
		Kind:          dungeonDropItemStackable,
		PVFPath:       "stackable/cash/hands_remy_shine.stk",
		StackableType: "[waste]",
		StackLimit:    0,
		SlotStart:     9,
		SlotEnd:       104,
	}
	slots, err := grantCurrentCeraShopProduct(&inventory, definition, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(slots, []uint16{8}) {
		t.Fatalf("slots=%v", slots)
	}
	if stack := inventory.Slots["0:8"]; stack.ItemID != 2660671 || stack.Count != 399 || len(stack.RawEntry) != currentItemListEntryWireSize {
		t.Fatalf("merged unlimited stack=%+v", stack)
	}
	if len(inventory.Slots) != 1 {
		t.Fatalf("unexpected spill rows=%+v", inventory.Slots)
	}
}

func alignedMagicBoxResolutionForTest() alignedcmd.MagicBoxResolution {
	return alignedcmd.MagicBoxResolution{
		Kind: "random",
		Groups: []alignedcmd.MagicBoxRewardGroup{{
			DrawCount: 1,
			Entries: []alignedcmd.MagicBoxRewardEntry{
				{ItemID: 10, Weight: 9, Count: 1},
				{ItemID: 11, Weight: 1, Count: 2},
			},
		}},
	}
}

func currentBoosterTestService(catalog *pvfDungeonDropCatalog, repositories dnfrepo.Group) *Service {
	return &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		pvfItemCatalog: catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
}

func assertCurrentBoosterSourceRefresh(
	t *testing.T,
	data []byte,
	wantSlot int16,
	wantItemID uint32,
	wantCount uint32,
) []byte {
	t.Helper()
	update, rest := splitGameServerUpperPacket(t, data)
	if update.Header.Classification != 0 ||
		update.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(update.Body) != 3+currentItemListEntryWireSize ||
		update.Body[0] != dnfrepo.MainInventoryListType ||
		binary.LittleEndian.Uint16(update.Body[1:3]) != 1 {
		t.Fatalf("source update header=%+v body=%x", update.Header, update.Body)
	}
	row := update.Body[3:]
	if binary.LittleEndian.Uint16(row[0:2]) != uint16(wantSlot) ||
		binary.LittleEndian.Uint32(row[2:6]) != wantItemID ||
		binary.LittleEndian.Uint32(row[6:10]) != wantCount {
		t.Fatalf("source update row=%x want_slot=%d want_item=%d want_count=%d", row, wantSlot, wantItemID, wantCount)
	}
	return rest
}

func mustCurrentBoosterTestCatalog(t *testing.T) *pvfDungeonDropCatalog {
	t.Helper()
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":                          "",
		"stackable/stackable.lst":                      "500 `cash/random.stk`\n501 `cash/selection.stk`\n502 `cash/artifact_box.stk`\n503 `cash/pet_box.stk`\n504 `cash/nested_expired_reward_box.stk`\n505 `cash/multi_selection.stk`\n506 `cash/zero_single_selection.stk`\n507 `cash/soul_box.stk`\n508 `cash/legacy_jar.stk`\n600 `material/reward.stk`\n601 `material/selection_cost.stk`\n605 `material/expired_reward.stk`\n10099774 `material/unique_soul.stk`\n",
		"equipment/equipment.lst":                      "700 `avatar/reward.equ`\n701 `creature/artifact_red/reward.equ`\n702 `creature/pet.equ`\n112550232 `avatar/multi_0.equ`\n112560208 `avatar/multi_1.equ`\n112570168 `avatar/multi_2.equ`\n112520208 `avatar/multi_3.equ`\n112500235 `avatar/multi_4.equ`\n112510219 `avatar/multi_5.equ`\n112530202 `avatar/multi_6.equ`\n112540236 `avatar/multi_7.equ`\n112600041 `avatar/weapon.equ`\n",
		"stackable/cash/random.stk":                    "[stackable type]\n`[booster]` 0\n[stack limit]\n100\n[booster info]\n[etc]\n1 600 10000 3\n[/etc]\n[/booster info]\n",
		"stackable/cash/selection.stk":                 "[stackable type]\n`[booster selection]` 0\n[stack limit]\n100\n[booster selection num]\n1\n[booster select category]\n0 0\n[avatar]\n700 1 3 0\n[/avatar]\n[/booster select category]\n",
		"stackable/cash/artifact_box.stk":              "[stackable type]\n`[booster]` 0\n[stack limit]\n100\n[booster info]\n[etc]\n1 701 10000 1\n[/etc]\n[/booster info]\n",
		"stackable/cash/pet_box.stk":                   "[stackable type]\n`[booster selection]` 0\n[stack limit]\n100\n[booster selection num]\n1\n[booster select category]\n0 0\n[creature]\n702 1\n[/creature]\n[/booster select category]\n",
		"stackable/cash/nested_expired_reward_box.stk": "[stackable type]\n`[booster]` 0\n[stack limit]\n1\n[booster info]\n[etc]\n1 605 10000 1\n[/etc]\n[/booster info]\n",
		"stackable/cash/multi_selection.stk":           "[stackable type]\n`[booster selection]` 0\n[stack limit]\n1\n[booster selection num]\n0\n[booster select category]\n1 0\n[avatar]\n112550232 1 3 0 112560208 1 3 0 112570168 1 3 0 112520208 1 3 0 112500235 1 3 0 112510219 1 3 0 112530202 1 3 0 112540236 1 3 0\n[/avatar]\n[/booster select category]\n",
		"stackable/cash/zero_single_selection.stk":     "[stackable type]\n`[booster selection]` 0\n[stack limit]\n1\n[booster selection num]\n0\n[booster select category]\n1 0\n[avatar]\n112600041 1 4 0\n[/avatar]\n[/booster select category]\n[need material]\n601 2\n",
		"stackable/cash/soul_box.stk":                  "[stackable type]\n`[booster]` 0\n[stack limit]\n1\n[booster info]\n[etc]\n1 10099774 10000 5\n[/etc]\n[/booster info]\n",
		"stackable/cash/legacy_jar.stk":                "[stackable type]\n`[random upgradable legacy]` 0\n[stack limit]\n100\n[RANDOMBOX]\n[int data]\n0 0 0 600 10000 3 0\n[/int data]\n[/RANDOMBOX]\n",
		"stackable/material/reward.stk":                "[stackable type]\n`[material]` 0\n[stack limit]\n999\n[attach type]\n`[free]`\n",
		"stackable/material/selection_cost.stk":        "[stackable type]\n`[material]` 0\n[stack limit]\n999\n[attach type]\n`[free]`\n",
		"stackable/material/expired_reward.stk":        "[stackable type]\n`[material]` 0\n[stack limit]\n1\n[expiration date]\n`2017-11-01 22:00:00`\n[attach type]\n`[free]`\n",
		"stackable/material/unique_soul.stk":           "[stackable type]\n`[material]` 0\n[stack limit]\n999999\n[attach type]\n`[account]`\n",
		"equipment/avatar/reward.equ":                  "[equipment type]\n`[hat avatar]` 0\n[attach type]\n`[trade]`\n",
		"equipment/creature/artifact_red/reward.equ":   "[equipment type]\n`[artifact red]` 15\n[attach type]\n`[trade]`\n",
		"equipment/creature/pet.equ":                   "[equipment type]\n`[creature]` 0\n[attach type]\n`[trade]`\n",
		"equipment/avatar/multi_0.equ":                 "[equipment type]\n`[hat avatar]` 0\n[attach type]\n`[trade]`\n",
		"equipment/avatar/multi_1.equ":                 "[equipment type]\n`[hair avatar]` 0\n[attach type]\n`[trade]`\n",
		"equipment/avatar/multi_2.equ":                 "[equipment type]\n`[face avatar]` 0\n[attach type]\n`[trade]`\n",
		"equipment/avatar/multi_3.equ":                 "[equipment type]\n`[breast avatar]` 0\n[attach type]\n`[trade]`\n",
		"equipment/avatar/multi_4.equ":                 "[equipment type]\n`[coat avatar]` 0\n[attach type]\n`[trade]`\n",
		"equipment/avatar/multi_5.equ":                 "[equipment type]\n`[pants avatar]` 0\n[attach type]\n`[trade]`\n",
		"equipment/avatar/multi_6.equ":                 "[equipment type]\n`[waist avatar]` 0\n[attach type]\n`[trade]`\n",
		"equipment/avatar/multi_7.equ":                 "[equipment type]\n`[shoes avatar]` 0\n[attach type]\n`[trade]`\n",
		"equipment/avatar/weapon.equ":                  "[equipment type]\n`[weapon avatar]` 0\n[attach type]\n`[trade]`\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
