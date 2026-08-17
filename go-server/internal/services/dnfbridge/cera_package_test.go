package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestParseCurrentCeraPackageOpenRequestUsesExactCurrentEXEBody(t *testing.T) {
	body := make([]byte, 13)
	binary.LittleEndian.PutUint16(body[0:2], 75)
	body[2] = 2
	binary.LittleEndian.PutUint32(body[3:7], 700)
	body[7] = 9
	binary.LittleEndian.PutUint32(body[8:12], 701)
	body[12] = 3
	request, err := parseCurrentCeraPackageOpenRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	want := currentCeraPackageOpenRequest{
		SourceSlot: 75,
		Choices: []currentCeraPackageChoice{
			{ItemID: 700, Option: 9},
			{ItemID: 701, Option: 3},
		},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("request=%+v want=%+v", request, want)
	}

	for _, malformed := range [][]byte{
		nil,
		{75, 0, 0},
		append(append([]byte(nil), body...), 0),
		{75, 0, 1, 0, 0, 0, 0, 9},
		{75, 0, 2, 188, 2, 0, 0, 9, 188, 2, 0, 0, 3},
	} {
		if _, err := parseCurrentCeraPackageOpenRequest(malformed); err == nil {
			t.Fatalf("malformed body accepted: %x", malformed)
		}
	}
}

func TestCurrentCeraPackageLegacyRouteAtomicallyConsumesAndGrantsPVFRewards(t *testing.T) {
	catalog := mustCurrentCeraPackageTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	sourceStack := dnfrepo.ItemStack{ItemID: 500, Count: 2}
	sourceEntry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, 75, sourceStack)
	sourceStack.RawEntry = append([]byte(nil), sourceEntry.data[:]...)
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:75": sourceStack},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		pvfItemCatalog:     catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "cera-package-test", selectedCharacterID: 19}
	requestBody := currentCeraPackageTestRequestBody(75, currentCeraPackageChoice{ItemID: 700, Option: 9})
	before := time.Now().UTC()
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketOpenCerapackage),
		requestBody,
	); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != uint16(dnfenum.CmdPacketOpenCerapackage) ||
		!bytes.Equal(ack.Body, []byte{1, 75, 0}) {
		t.Fatalf("op518 ACK header=%+v body=%x", ack.Header, ack.Body)
	}
	list0, rest := splitCurrentSceneItemListPacket(t, rest)
	if len(list0.Body) == 0 || list0.Body[0] != dnfrepo.MainInventoryListType {
		t.Fatalf("list0 body=%x", list0.Body)
	}
	list1, trailing := splitCurrentSceneItemListPacket(t, rest)
	if len(trailing) != 0 || len(list1.Body) == 0 || list1.Body[0] != 1 {
		t.Fatalf("list1 body=%x trailing=%d", list1.Body, len(trailing))
	}

	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if source := inventory.Slots["0:75"]; source.ItemID != 500 || source.Count != 1 || binary.LittleEndian.Uint32(source.RawEntry[6:10]) != 1 {
		t.Fatalf("source after=%+v raw=%x", source, source.RawEntry)
	}
	stackable, found := inventory.Slots["0:3"]
	minExpire := time.Unix(before.Unix()+7*86400, 0).UTC()
	maxExpire := time.Unix(time.Now().UTC().Unix()+7*86400, 0).UTC()
	if !found || stackable.ItemID != 600 || stackable.Count != 3 || stackable.Extra["source"] != "cera_package" ||
		stackable.Extra["usable_period_days"] != "7" || stackable.Extra["expiration_source"] != "runtime_pvf_usable_period_grant" ||
		stackable.ExpireAt.Before(minExpire) || stackable.ExpireAt.After(maxExpire) ||
		binary.LittleEndian.Uint32(stackable.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != uint32(stackable.ExpireAt.Unix()) {
		t.Fatalf("stackable reward=%+v found=%t", stackable, found)
	}
	avatar, found := inventory.Slots["1:0"]
	if !found || avatar.ItemID != 700 || avatar.Count != 1 || avatar.Extra["amount_or_count"] != "0" || avatar.Extra["ext_data0"] != "9" || avatar.Extra["source"] != "cera_package" {
		t.Fatalf("avatar reward=%+v found=%t", avatar, found)
	}
}

func TestCurrentCeraPackageRoutesCreatureAndFeedRewardsToPetSegments(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":                       "",
		"stackable/stackable.lst":                   "502 `cash/pet_package.stk`\n24 `cash/creature/creature_food.stk`\n",
		"equipment/equipment.lst":                   "700 `avatar/test_hat.equ`\n800 `creature/test_pet.equ`\n",
		"stackable/cash/pet_package.stk":            "[stackable type]\n`[usable cera package]`\n[stack limit]\n1\n[package data]\n700 1\n800 1\n24 300\n[/package data]\n",
		"stackable/cash/creature/creature_food.stk": "[stackable type]\n`[feed]`\n[stack limit]\n1000\n",
		"equipment/avatar/test_hat.equ":             "[equipment type]\n`[hat avatar]`\n",
		"equipment/creature/test_pet.equ":           "[equipment type]\n`[creature]`\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := resolveCurrentCeraPackageDefinition(catalog, 502)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:75": {ItemID: 502, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-1"},
		pvfItemCatalog:     catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	result, err := service.commitCurrentCeraPackage(
		ctx,
		&gameSession{selectedCharacterID: 19},
		catalog,
		definition,
		currentCeraPackageOpenRequest{SourceSlot: 75, Choices: []currentCeraPackageChoice{{ItemID: 700, Option: 9}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.ChangedListTypes, []byte{0, 1, currentPetInventoryListType}) {
		t.Fatalf("changed lists=%v", result.ChangedListTypes)
	}

	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, found := inventory.Slots["0:75"]; found {
		t.Fatal("source package was not consumed")
	}
	for key, stack := range inventory.Slots {
		if strings.HasPrefix(key, "0:") && (stack.ItemID == 24 || stack.ItemID == 800) {
			t.Fatalf("pet reward leaked into main bag key=%s stack=%+v", key, stack)
		}
	}
	creature, creatureFound := inventory.Slots["7:0"]
	if !creatureFound || creature.ItemID != 800 || creature.Count != 1 ||
		creature.Extra["source"] != "cera_package" ||
		binary.LittleEndian.Uint32(creature.RawEntry[0x0E:0x12]) != 0 {
		t.Fatalf("creature reward=%+v found=%t", creature, creatureFound)
	}
	feed, feedFound := inventory.Slots["7:189"]
	if !feedFound || feed.ItemID != 24 || feed.Count != 300 ||
		feed.Extra["stackable_type"] != "[feed]" || feed.Extra["source"] != "cera_package" {
		t.Fatalf("feed reward=%+v found=%t", feed, feedFound)
	}
}

func TestCurrentCeraPackageAllowsFutureInstanceExpirationWhenPVFStaticDateExpired(t *testing.T) {
	catalog := mustCurrentCeraPackageTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	instanceExpire := time.Date(2037, time.December, 31, 15, 59, 59, 0, time.UTC)
	sourceStack := dnfrepo.ItemStack{
		ItemID:   501,
		Count:    1,
		ExpireAt: instanceExpire,
		Extra: map[string]string{
			"expire_time": strconv.FormatInt(instanceExpire.Unix(), 10),
			"expire_unix": strconv.FormatInt(instanceExpire.Unix(), 10),
		},
	}
	sourceEntry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, 75, sourceStack)
	sourceStack.RawEntry = append([]byte(nil), sourceEntry.data[:]...)
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:75": sourceStack},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "account-1",
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		pvfItemCatalog:     catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "cera-package-expired-static-test", selectedCharacterID: 19}
	requestBody := currentCeraPackageTestRequestBody(75, currentCeraPackageChoice{ItemID: 700, Option: 9})
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketOpenCerapackage),
		requestBody,
	); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketOpenCerapackage) || !bytes.Equal(ack.Body, []byte{1, 75, 0}) {
		t.Fatalf("op518 ACK header=%+v body=%x", ack.Header, ack.Body)
	}
	list0, rest := splitCurrentSceneItemListPacket(t, rest)
	list1, trailing := splitCurrentSceneItemListPacket(t, rest)
	if len(trailing) != 0 || len(list0.Body) == 0 || list0.Body[0] != dnfrepo.MainInventoryListType || len(list1.Body) == 0 || list1.Body[0] != 1 {
		t.Fatalf("list0=%x list1=%x trailing=%d", list0.Body, list1.Body, len(trailing))
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, stillPresent := inventory.Slots["0:75"]; stillPresent {
		t.Fatalf("future instance package source was not consumed: %+v", inventory.Slots["0:75"])
	}
	if avatar, found := inventory.Slots["1:0"]; !found || avatar.ItemID != 700 || avatar.Extra["source"] != "cera_package" {
		t.Fatalf("avatar reward=%+v found=%t", avatar, found)
	}
	var inheritedReward dnfrepo.ItemStack
	inheritedFound := false
	for _, stack := range inventory.Slots {
		if stack.ItemID == 602 {
			inheritedReward = stack
			inheritedFound = true
			break
		}
	}
	if !inheritedFound || !inheritedReward.ExpireAt.Equal(instanceExpire) ||
		inheritedReward.Extra["expire_unix"] != strconv.FormatInt(instanceExpire.Unix(), 10) ||
		binary.LittleEndian.Uint32(inheritedReward.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != uint32(instanceExpire.Unix()) {
		t.Fatalf("inherited reward=%+v found=%t want_expire=%s", inheritedReward, inheritedFound, instanceExpire)
	}
}

func TestCurrentCeraPackageRejectsExpiredPVFStaticDateWithoutInstanceExpiration(t *testing.T) {
	catalog := mustCurrentCeraPackageTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:75": {ItemID: 501, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-1"},
		pvfItemCatalog:     catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{selectedCharacterID: 19}
	request := currentCeraPackageOpenRequest{SourceSlot: 75, Choices: []currentCeraPackageChoice{{ItemID: 700, Option: 9}}}
	if _, err := service.prepareCurrentCeraPackage(ctx, session, catalog, request); !errors.Is(err, errCurrentCeraPackageExpired) {
		t.Fatalf("prepare expired static package error=%v", err)
	}
}

func TestCurrentCeraPackageForgedChoiceRollsBackSourceAndRewards(t *testing.T) {
	catalog := mustCurrentCeraPackageTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	before := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:75": {ItemID: 500, Count: 1}},
	}
	if err := repositories.Inventory.Save(ctx, before); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-1"},
		pvfItemCatalog:     catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{selectedCharacterID: 19}
	request := currentCeraPackageOpenRequest{SourceSlot: 75, Choices: []currentCeraPackageChoice{{ItemID: 701, Option: 9}}}
	definition, err := resolveCurrentCeraPackageDefinition(catalog, 500)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.commitCurrentCeraPackage(ctx, session, catalog, definition, request); !errors.Is(err, errCurrentCeraPackageChoiceInvalid) {
		t.Fatalf("commit forged choice error=%v", err)
	}
	after, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found || !reflect.DeepEqual(after.Slots, before.Slots) {
		t.Fatalf("rollback inventory=%+v found=%t err=%v", after, found, err)
	}
}

func TestCurrentCeraPackageInventoryFullRollsBackConsumedSource(t *testing.T) {
	catalog := mustCurrentCeraPackageTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	slots := map[string]dnfrepo.ItemStack{"0:75": {ItemID: 500, Count: 1}}
	for slot := int16(0); slot <= 500; slot++ {
		slots[currentCeraShopInventorySlotKey(1, slot)] = dnfrepo.ItemStack{ItemID: 9000 + int64(slot), Count: 0}
	}
	before := dnfrepo.InventoryRecord{CharacterID: "19", Slots: slots}
	if err := repositories.Inventory.Save(ctx, before); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-1"},
		pvfItemCatalog:     catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	definition, err := resolveCurrentCeraPackageDefinition(catalog, 500)
	if err != nil {
		t.Fatal(err)
	}
	request := currentCeraPackageOpenRequest{SourceSlot: 75, Choices: []currentCeraPackageChoice{{ItemID: 700, Option: 9}}}
	if _, err := service.commitCurrentCeraPackage(ctx, &gameSession{selectedCharacterID: 19}, catalog, definition, request); !errors.Is(err, errDungeonPickupInventoryFull) {
		t.Fatalf("commit full inventory error=%v", err)
	}
	after, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found || !reflect.DeepEqual(after.Slots, before.Slots) {
		t.Fatalf("rollback inventory changed found=%t err=%v", found, err)
	}
}

func TestCurrentCeraPackageMailsMainInventoryOverflowAtomically(t *testing.T) {
	catalog := mustCurrentCeraPackageTestCatalog(t)
	definition, err := resolveCurrentCeraPackageDefinition(catalog, 500)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	slots := make(map[string]dnfrepo.ItemStack)
	var material dungeonDropItemDefinition
	for _, reward := range definition.Rewards {
		if reward.Definition.ItemID == 600 {
			material = reward.Definition
			break
		}
	}
	if material.ItemID == 0 {
		t.Fatal("test package material definition is missing")
	}
	for slot := material.SlotStart; slot <= material.SlotEnd; slot++ {
		slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, slot)] = dnfrepo.ItemStack{ItemID: 90000 + int64(slot), Count: 1}
	}
	for slot := int16(3); slot <= 8; slot++ {
		slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, slot)] = dnfrepo.ItemStack{ItemID: 91000 + int64(slot), Count: 1}
	}
	// Keep the source outside the reward's category range: removing the
	// package must not manufacture a free material slot for this test.
	slots["0:500"] = dnfrepo.ItemStack{ItemID: 500, Count: 1}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: slots}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-1"},
		pvfItemCatalog:     catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}

	result, err := service.commitCurrentCeraPackage(
		ctx,
		&gameSession{selectedCharacterID: 19},
		catalog,
		definition,
		currentCeraPackageOpenRequest{SourceSlot: 500, Choices: []currentCeraPackageChoice{{ItemID: 700, Option: 9}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.OverflowMailID == "" || result.OverflowItems != 1 || !reflect.DeepEqual(result.ChangedListTypes, []byte{0, 1}) {
		t.Fatalf("commit result=%+v", result)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, found := inventory.Slots["0:500"]; found {
		t.Fatal("source package was not consumed")
	}
	if avatar, found := inventory.Slots["1:0"]; !found || avatar.ItemID != 700 || avatar.Extra["source"] != "cera_package" {
		t.Fatalf("avatar after overflow=%+v found=%t", avatar, found)
	}
	mailbox, found, err := repositories.Mailbox.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load mailbox found=%t err=%v", found, err)
	}
	mail, found := mailbox.Mails[result.OverflowMailID]
	if !found || mail.Title != "背包已满：礼包奖励" || len(mail.Attachments) != 1 {
		t.Fatalf("overflow mail=%+v found=%t", mail, found)
	}
	attachment := mail.Attachments[0]
	if attachment.ItemID != 600 || attachment.Count != 3 || attachment.Extra["source"] != "cera_package" ||
		attachment.Extra["cera_package_source_item"] != "500" || attachment.Extra["cera_package_overflow_mail"] != "true" {
		t.Fatalf("overflow attachment=%+v", attachment)
	}
	if attachment.ExpireAt.IsZero() || attachment.RawEntry == nil {
		t.Fatalf("overflow attachment lost its item-instance fields: %+v", attachment)
	}
}

func TestRealPVFSummerCeraPackageResolvesAllRewardsAndAvatarChoices(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the real 2018 summer Cera package")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := resolveCurrentCeraPackageDefinition(catalog, 490702418)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Source.PVFPath != "stackable/cash/chn_490702418.stk" || len(definition.Rewards) != 13 || len(definition.AvatarItemIDs) != 9 {
		t.Fatalf("real package source=%+v reward_rows=%d avatar_choices=%v", definition.Source, len(definition.Rewards), definition.AvatarItemIDs)
	}
	wantAvatarIDs := []uint32{412550012, 112560230, 112570187, 112520228, 412500009, 112510244, 112530222, 112540258, 112580139}
	if !reflect.DeepEqual(definition.AvatarItemIDs, wantAvatarIDs) {
		t.Fatalf("avatar choices=%v want=%v", definition.AvatarItemIDs, wantAvatarIDs)
	}
}

func TestRealPVFBeastGuardianCeraPackageKeepsItsOriginalRewardLayout(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the real 2018 beast-guardian Cera package")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := resolveCurrentCeraPackageDefinition(catalog, 490701964)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Source.PVFPath != "stackable/cash/chn_490701964.stk" || len(definition.Rewards) != 28 || len(definition.AvatarItemIDs) != 8 {
		t.Fatalf("real package source=%+v reward_rows=%d avatar_choices=%v", definition.Source, len(definition.Rewards), definition.AvatarItemIDs)
	}
	var total uint64
	counts := make(map[uint32]uint32, len(definition.Rewards))
	for _, reward := range definition.Rewards {
		total += uint64(reward.Count)
		counts[reward.Definition.ItemID] = reward.Count
	}
	if total != 996 || counts[490701742] != 1 || counts[490701730] != 200 || counts[24] != 300 {
		t.Fatalf("real package reward total=%d counts=%v", total, counts)
	}
}

func TestRealPVFSummerCeraPackageFeedRewardUsesPetConsumableType(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the real 2018 summer Cera package feed")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := resolveCurrentCeraPackageDefinition(catalog, 490702439)
	if err != nil {
		t.Fatal(err)
	}
	for _, reward := range definition.Rewards {
		if reward.Definition.ItemID != 24 {
			continue
		}
		if reward.Count != 300 || !isCurrentCeraShopPetConsumable(reward.Definition) {
			t.Fatalf("real feed reward=%+v", reward)
		}
		return
	}
	t.Fatal("real package 490702439 has no item 24 feed reward")
}

func TestRealPVFSummerCeraPackageThreeCoversAllSixteenJobs(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify all real 2018 summer Cera packages")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	type match struct {
		itemID        uint32
		name          string
		jobs          []string
		rewardRows    int
		avatarChoices int
	}
	matches := make([]match, 0, 16)
	for itemID, reference := range catalog.itemRefs {
		if itemID < 490702000 || itemID > 490703000 || reference.kind != dungeonDropItemStackable {
			continue
		}
		definition, resolveErr := catalog.ResolveItem(itemID)
		if resolveErr != nil {
			t.Fatalf("resolve candidate item=%d: %v", itemID, resolveErr)
		}
		document, readErr := parseDungeonCardPVFDocument(catalog.source, definition.PVFPath)
		if readErr != nil {
			t.Fatalf("read candidate item=%d: %v", itemID, readErr)
		}
		name, _ := document.Text("name")
		if !strings.Contains(name, "2018热舞一夏乐享礼包 3 (") {
			continue
		}
		resolved, packageErr := resolveCurrentCeraPackageDefinition(catalog, itemID)
		if packageErr != nil {
			t.Fatalf("resolve package item=%d name=%q: %v", itemID, name, packageErr)
		}
		jobList := strings.TrimSuffix(strings.TrimPrefix(name[strings.LastIndex(name, " ("):], " ("), ")")
		matches = append(matches, match{
			itemID:        itemID,
			name:          name,
			jobs:          strings.Split(jobList, "/"),
			rewardRows:    len(resolved.Rewards),
			avatarChoices: len(resolved.AvatarItemIDs),
		})
	}
	sort.Slice(matches, func(left, right int) bool { return matches[left].itemID < matches[right].itemID })
	if len(matches) != 14 {
		t.Fatalf("2018 summer package-3 item count=%d want=14 matches=%+v", len(matches), matches)
	}
	coveredJobs := make(map[string]struct{}, 16)
	for _, packageMatch := range matches {
		if packageMatch.rewardRows == 0 || packageMatch.avatarChoices == 0 {
			t.Fatalf("package=%+v has no real rewards/avatar choices", packageMatch)
		}
		for _, job := range packageMatch.jobs {
			coveredJobs[job] = struct{}{}
		}
		t.Logf("item=%d name=%s reward_rows=%d avatar_choices=%d", packageMatch.itemID, packageMatch.name, packageMatch.rewardRows, packageMatch.avatarChoices)
	}
	if len(coveredJobs) != 16 {
		t.Fatalf("2018 summer package-3 job coverage=%d want=16 jobs=%v", len(coveredJobs), coveredJobs)
	}
}

func mustCurrentCeraPackageTestCatalog(t *testing.T) *pvfDungeonDropCatalog {
	t.Helper()
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":                       "",
		"stackable/stackable.lst":                   "500 `cash/test_package.stk`\n501 `cash/expired_static_package.stk`\n600 `material/test_reward.stk`\n602 `cash/expired_child_booster.stk`\n",
		"equipment/equipment.lst":                   "700 `avatar/test_hat.equ`\n",
		"stackable/cash/test_package.stk":           "[stackable type]\n`[usable cera package]`\n[stack limit]\n100\n[expiration date]\n`2028-08-16 06:00:00`\n[package data]\n700 1\n600 3\n[/package data]\n",
		"stackable/cash/expired_static_package.stk": "[stackable type]\n`[usable cera package]`\n[stack limit]\n1\n[expiration date]\n`2017-11-01 22:00:00`\n[package data]\n700 1\n600 3\n602 1\n[/package data]\n",
		"stackable/cash/expired_child_booster.stk":  "[stackable type]\n`[booster]`\n[stack limit]\n1\n[expiration date]\n`2017-11-01 22:00:00`\n[booster info]\n[etc]\n1 600 10000 1\n[/etc]\n[/booster info]\n",
		"stackable/material/test_reward.stk":        "[stackable type]\n`[material]`\n[stack limit]\n999\n[usable period]\n7\n",
		"equipment/avatar/test_hat.equ":             "[equipment type]\n`[hat avatar]`\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := resolveCurrentCeraPackageDefinition(catalog, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(definition.Rewards) != 2 || !reflect.DeepEqual(definition.AvatarItemIDs, []uint32{700}) {
		t.Fatalf("fixture package=%+v", definition)
	}
	return catalog
}

func currentCeraPackageTestRequestBody(slot uint16, choices ...currentCeraPackageChoice) []byte {
	body := make([]byte, currentCeraPackageRequestHeaderSize+len(choices)*currentCeraPackageChoiceStride)
	binary.LittleEndian.PutUint16(body[0:2], slot)
	body[2] = byte(len(choices))
	for index, choice := range choices {
		offset := currentCeraPackageRequestHeaderSize + index*currentCeraPackageChoiceStride
		binary.LittleEndian.PutUint32(body[offset:offset+4], choice.ItemID)
		body[offset+4] = choice.Option
	}
	return body
}
