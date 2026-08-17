package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestPVFDungeonTutorialRewardCatalogReadsExactProgressTriples(t *testing.T) {
	catalog, err := newPVFDungeonTutorialRewardCatalog(tutorialRewardFixturePVF())
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Rewards(37); len(got) != 0 {
		t.Fatalf("progress 37 rewards=%+v", got)
	}
	got := catalog.Rewards(currentDungeonTutorialRewardProgress)
	want := []pvfDungeonTutorialReward{{
		Progress: currentDungeonTutorialRewardProgress,
		ItemID:   8474,
		Count:    1,
	}}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("progress 38 rewards=%+v want=%+v", got, want)
	}
	got[0].ItemID = 1
	if replay := catalog.Rewards(currentDungeonTutorialRewardProgress); replay[0].ItemID != 8474 {
		t.Fatalf("catalog exposed mutable reward slice: %+v", replay)
	}
}

func TestPVFDungeonTutorialRewardCatalogRejectsMalformedRows(t *testing.T) {
	tests := []struct {
		name string
		text string
		want error
	}{
		{name: "missing section", text: "[other]\n1\n", want: errDungeonTutorialRewardSectionMissing},
		{name: "empty section", text: "[escalade tutorial reward]\n[/escalade tutorial reward]\n", want: errDungeonTutorialRewardRowInvalid},
		{name: "short row", text: "[escalade tutorial reward]\n38 8474\n[/escalade tutorial reward]\n", want: errDungeonTutorialRewardRowInvalid},
		{name: "non integer", text: "[escalade tutorial reward]\n38 `8474` 1\n[/escalade tutorial reward]\n", want: errDungeonTutorialRewardRowInvalid},
		{name: "zero count", text: "[escalade tutorial reward]\n38 8474 0\n[/escalade tutorial reward]\n", want: errDungeonTutorialRewardRowInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := tutorialRewardFixturePVF()
			source[currentDungeonTutorialRewardPVFPath] = test.text
			_, err := newPVFDungeonTutorialRewardCatalog(source)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestBuildCurrentDungeonTutorialRewardSuccessPayloadMatchesCurrentEXEReader(t *testing.T) {
	payload, err := buildCurrentDungeonTutorialRewardSuccessPayload([]currentDungeonTutorialRewardRow{{
		Slot:   3,
		ItemID: 8474,
		Count:  1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{1, 3, 0, 0x1A, 0x21, 0, 0, 1, 0, 0, 0}
	if !bytes.Equal(payload, want) {
		t.Fatalf("op143 reward payload=%x want=%x", payload, want)
	}
	if binary.LittleEndian.Uint16(payload[1:3]) != 3 ||
		binary.LittleEndian.Uint32(payload[3:7]) != 8474 ||
		binary.LittleEndian.Uint32(payload[7:11]) != 1 {
		t.Fatalf("op143 reward row did not round trip: %x", payload)
	}
}

func TestHandleDungeonTutorialProgress38GrantsPVFHPToQuickSlotOne(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	installTutorialRewardFixturePVF(t, service)
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "progress38-op143-hp-reward-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleDungeonTutorialFlag(session, tutorialRewardFlagRequestBody()); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantBody := []byte{1, 1, 3, 0, 0x1A, 0x21, 0, 0, 1, 0, 0, 0}
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketChangeTutorialFlag) ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, wantBody) || len(rest) != 0 {
		t.Fatalf("progress38 ack=%+v body=%x want=%x rest=%x", ack.Header, ack.Body, wantBody, rest)
	}

	repositories := tutorialRewardRepositories(t, service)
	character, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load rewarded character found=%t err=%v", found, err)
	}
	if got := character.Stats[currentDungeonTutorialRewardMarker(currentDungeonTutorialRewardProgress)]; got != currentDungeonTutorialRewardMarkerValue {
		t.Fatalf("progress38 durable marker=%d", got)
	}
	inventory, found, err := repositories.Inventory.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load rewarded inventory found=%t err=%v", found, err)
	}
	stack, ok := inventory.Slots["0:3"]
	if !ok || stack.ItemID != 8474 || stack.Count != 1 || stack.Bind || !stack.ExpireAt.IsZero() {
		t.Fatalf("quick slot one stack=%+v present=%t inventory=%+v", stack, ok, inventory.Slots)
	}
	if stack.Extra["source"] != "tutorial_pvf_reward" ||
		stack.Extra["pvf_path"] != "stackable/tutorial/tutorial_8474.stk" ||
		stack.Extra["stackable_type"] != "[waste]" ||
		stack.Extra["stack_limit"] != "1000" || len(stack.RawEntry) != 0 {
		t.Fatalf("quick slot one metadata=%+v raw_len=%d", stack.Extra, len(stack.RawEntry))
	}
	if _, misplaced := inventory.Slots["0:65"]; misplaced {
		t.Fatalf("new tutorial potion bypassed empty quick slot: %+v", inventory.Slots)
	}
}

func TestHandleDungeonTutorialProgress38ReplayIsPersistentlyIdempotent(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	installTutorialRewardFixturePVF(t, service)
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "progress38-op143-idempotency-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleDungeonTutorialFlag(session, tutorialRewardFlagRequestBody()); err != nil {
		t.Fatal(err)
	}
	conn.write.Reset()
	if err := service.handleDungeonTutorialFlag(session, tutorialRewardFlagRequestBody()); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if !bytes.Equal(ack.Body, []byte{1, 0}) || len(rest) != 0 {
		t.Fatalf("idempotent replay ack=%x rest=%x", ack.Body, rest)
	}

	repositories := tutorialRewardRepositories(t, service)
	inventory, found, err := repositories.Inventory.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load replay inventory found=%t err=%v", found, err)
	}
	if stack := inventory.Slots["0:3"]; stack.ItemID != 8474 || stack.Count != 1 || len(inventory.Slots) != 1 {
		t.Fatalf("replay duplicated tutorial reward: %+v", inventory.Slots)
	}
}

func TestHandleDungeonTutorialProgress38UsesNextEmptyQuickSlot(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	installTutorialRewardFixturePVF(t, service)
	repositories := tutorialRewardRepositories(t, service)
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots: map[string]dnfrepo.ItemStack{
			"0:3": {ItemID: 900001, Count: 1},
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "progress38-op143-next-quick-slot-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleDungeonTutorialFlag(session, tutorialRewardFlagRequestBody()); err != nil {
		t.Fatal(err)
	}
	ack, _ := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(ack.Body) != 12 || binary.LittleEndian.Uint16(ack.Body[2:4]) != 4 {
		t.Fatalf("occupied first quick slot ack=%x", ack.Body)
	}
	inventory, _, err := repositories.Inventory.Load(context.Background(), "99")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Slots["0:3"].ItemID != 900001 ||
		inventory.Slots["0:4"].ItemID != 8474 || inventory.Slots["0:4"].Count != 1 {
		t.Fatalf("next quick slot inventory=%+v", inventory.Slots)
	}
}

func TestAddCurrentDungeonTutorialRewardUsesEmptyQuickSlotBeforeCategoryStack(t *testing.T) {
	record := dnfrepo.InventoryRecord{Slots: map[string]dnfrepo.ItemStack{
		"0:65": {ItemID: 8474, Count: 5},
	}}
	definition := currentDungeonTutorialRewardDefinition{
		Reward: pvfDungeonTutorialReward{
			Progress: currentDungeonTutorialRewardProgress,
			ItemID:   8474,
			Count:    1,
		},
		Item: currentDungeonTutorialRewardItemDefinition{
			ItemID:        8474,
			PVFPath:       "stackable/tutorial/tutorial_8474.stk",
			StackableType: "[waste]",
			StackLimit:    1000,
			SlotStart:     65,
			SlotEnd:       120,
		},
	}

	slot, err := addCurrentDungeonTutorialRewardToInventory(
		&record,
		currentDungeonTutorialRewardProgress,
		definition,
	)
	if err != nil {
		t.Fatal(err)
	}
	if slot != currentDungeonTutorialQuickSlotStart ||
		record.Slots["0:3"].ItemID != 8474 ||
		record.Slots["0:3"].Count != 1 ||
		record.Slots["0:65"].Count != 5 {
		t.Fatalf("slot=%d inventory=%+v", slot, record.Slots)
	}
}

func TestHandleDungeonTutorialProgress38RequiresOwnedActivePVFTutorial(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		singleMonster:   true,
		disableTutorial: true,
	})
	installTutorialRewardFixturePVF(t, service)
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "progress38-op143-non-tutorial-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleDungeonTutorialFlag(session, tutorialRewardFlagRequestBody()); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("non-tutorial progress38 wrote=%x", conn.write.Bytes())
	}
	repositories := tutorialRewardRepositories(t, service)
	character, _, _ := repositories.Character.Load(context.Background(), "99")
	inventory, _, _ := repositories.Inventory.Load(context.Background(), "99")
	if character.Stats[currentDungeonTutorialRewardMarker(currentDungeonTutorialRewardProgress)] != 0 ||
		len(inventory.Slots) != 0 {
		t.Fatalf("non-tutorial progress38 mutated character=%+v inventory=%+v", character.Stats, inventory.Slots)
	}
}

func TestDeliverDungeonTutorialRewardRollsBackAllRowsWhenInventoryIsFull(t *testing.T) {
	service, _ := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	repositories := tutorialRewardRepositories(t, service)
	slots := map[string]dnfrepo.ItemStack{
		"0:3": {ItemID: 8474, Count: 1},
	}
	for slot := int16(4); slot <= currentDungeonTutorialQuickSlotEnd; slot++ {
		slots[currentDungeonTutorialMainSlotKey(slot)] = dnfrepo.ItemStack{ItemID: 900000 + int64(slot), Count: 1}
	}
	for slot := int16(65); slot <= 120; slot++ {
		slots[currentDungeonTutorialMainSlotKey(slot)] = dnfrepo.ItemStack{ItemID: 910000 + int64(slot), Count: 1}
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots:       slots,
	}); err != nil {
		t.Fatal(err)
	}
	definitions := []currentDungeonTutorialRewardDefinition{
		{
			Reward: pvfDungeonTutorialReward{Progress: 38, ItemID: 8474, Count: 1},
			Item: currentDungeonTutorialRewardItemDefinition{
				ItemID: 8474, StackableType: "[waste]", StackLimit: 1000,
				SlotStart: 65, SlotEnd: 120,
			},
		},
		{
			Reward: pvfDungeonTutorialReward{Progress: 38, ItemID: 8475, Count: 1},
			Item: currentDungeonTutorialRewardItemDefinition{
				ItemID: 8475, StackableType: "[waste]", StackLimit: 1000,
				SlotStart: 65, SlotEnd: 120,
			},
		},
	}
	if _, err := deliverCurrentDungeonTutorialReward(
		context.Background(),
		repositories.CharacterAssets,
		"99",
		38,
		definitions,
		time.Now().UTC(),
	); !errors.Is(err, errDungeonTutorialRewardInventoryFull) {
		t.Fatalf("delivery error=%v want=%v", err, errDungeonTutorialRewardInventoryFull)
	}

	inventory, found, err := repositories.Inventory.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load rolled-back inventory found=%t err=%v", found, err)
	}
	if stack := inventory.Slots["0:3"]; stack.ItemID != 8474 || stack.Count != 1 {
		t.Fatalf("first reward row escaped rollback: %+v", stack)
	}
	character, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load rolled-back character found=%t err=%v", found, err)
	}
	if marker := character.Stats[currentDungeonTutorialRewardMarker(38)]; marker != 0 {
		t.Fatalf("failed bundle persisted reward marker=%d", marker)
	}
}

func TestRealScriptPVFTutorialProgress38RewardIsBeginnerHPPotion(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to inspect the real tutorial reward")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	rewardCatalog, err := newPVFDungeonTutorialRewardCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	rewards := rewardCatalog.Rewards(currentDungeonTutorialRewardProgress)
	if len(rewards) != 1 || rewards[0].ItemID != 8474 || rewards[0].Count != 1 {
		t.Fatalf("real progress38 rewards=%+v", rewards)
	}
	item, err := resolveCurrentDungeonTutorialRewardItem(archive, rewards[0].ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if item.PVFPath != "stackable/tutorial/tutorial_8474.stk" ||
		item.StackableType != "[waste]" || item.StackLimit != 1000 ||
		item.SlotStart != 65 || item.SlotEnd != 120 {
		t.Fatalf("real tutorial potion=%+v", item)
	}
}

func tutorialRewardFixturePVF() bridgePVFSource {
	return bridgePVFSource{
		currentDungeonTutorialRewardPVFPath: "[escalade tutorial reward]\n38 8474 1\n[/escalade tutorial reward]\n",
		"monster/monster.lst":               "3001 `tutorial_scope.gob`\n",
		"stackable/stackable.lst":           "8474 `tutorial/tutorial_8474.stk`\n",
		"equipment/equipment.lst":           "9001 `weapon/placeholder.equ`\n",
		"stackable/tutorial/tutorial_8474.stk": "[name]\n`Beginner HP Potion`\n" +
			"[stackable type]\n`[waste]` 4\n" +
			"[stack limit]\n1000\n" +
			"[hp recovery]\n`+` 300 1000 `myself`\n",
	}
}

func installTutorialRewardFixturePVF(t *testing.T, service *Service) {
	t.Helper()
	catalog, err := newPVFDungeonMonsterCatalog(tutorialRewardFixturePVF())
	if err != nil {
		t.Fatal(err)
	}
	service.worldMapMu.Lock()
	service.dungeonMonsterTable = catalog
	service.worldMapMu.Unlock()
}

func tutorialRewardRepositories(t *testing.T, service *Service) dnfrepo.Group {
	t.Helper()
	repositories, ok := service.repositoryGroup()
	if !ok {
		t.Fatal("tutorial reward repositories unavailable")
	}
	return repositories
}

func tutorialRewardFlagRequestBody() []byte {
	return []byte{
		currentDungeonTutorialFinalPrefix,
		byte(currentDungeonTutorialRewardProgress), 0, 0, 0,
		currentDungeonTutorialFinalCommit,
	}
}
