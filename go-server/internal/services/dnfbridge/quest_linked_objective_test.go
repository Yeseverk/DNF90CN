package dnfbridge

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func linkedObjectiveBridgeCatalog(t *testing.T) *dnfquest.Catalog {
	t.Helper()
	source := finishBridgePVFSource{
		dnfquest.DefaultList:   "3249 `parent.qst`\n3609 `meet.qst`\n3610 `seekmeet.qst`\n3347 `parent2.qst`\n3425 `hunt.qst`\n3426 `seeking.qst`\n",
		"n_quest/parent.qst":   "[grade]\n`[epic]`\n[type]\n`[quest clear]`\n[int data]\n3609 3610\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
		"n_quest/meet.qst":     "[grade]\n`[sub]`\n[attribute]\n`not give exp quest`\n[/attribute]\n[type]\n`[meet npc]`\n[main quest]\n3249\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
		"n_quest/seekmeet.qst": "[grade]\n`[sub]`\n[attribute]\n`not give exp quest`\n[/attribute]\n[type]\n`[seek n meet npc]`\n[main quest]\n3249\n[int data]\n3037 1 309\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
		"n_quest/parent2.qst":  "[grade]\n`[epic]`\n[type]\n`[quest clear]`\n[int data]\n3425 3426\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
		"n_quest/hunt.qst":     "[grade]\n`[sub]`\n[attribute]\n`not give exp quest`\n[/attribute]\n[type]\n`[hunt enemy]`\n[main quest]\n3347\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
		"n_quest/seeking.qst":  "[grade]\n`[sub]`\n[attribute]\n`not give exp quest`\n[/attribute]\n[type]\n`[seeking]`\n[main quest]\n3347\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestCurrentLinkedObjectiveFinishArchivesWithoutExperienceAndSendsMinimalSnapshots(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: defaultAccountPrefix + "1", Level: 60, Stats: map[string]int64{"exp": 1234}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		3347: {Status: "active", ProgressValue: 1},
		3425: {Status: "active", ProgressValue: 0},
		3426: {Status: "completed", ProgressValue: 0},
	}}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		questCatalog:       linkedObjectiveBridgeCatalog(t),
	}
	session := &gameSession{conn: connection, selectedCharacterID: 19, channel: channelcatalog.Channel{ID: 19}}
	body := []byte{0x22, 0x00, 0x61, 0x0d, 0xff, 0xff, 0x01, 0x00, 0xff, 0xff}
	if err := service.handleCurrentFinishQuest(session, body); err != nil {
		t.Fatal(err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	if got := finishBridgePacketIDs(packets); !reflect.DeepEqual(got, []string{"1/34", "0/574", "0/356"}) {
		t.Fatalf("packet order=%v", got)
	}
	if !bytes.Equal(packets[0].Body, []byte{0, 22}) {
		t.Fatalf("op34 body=%x", packets[0].Body)
	}
	character, found, err := repositories.Character.Load(ctx, "19")
	if err != nil || !found || character.Stats["exp"] != 1234 {
		t.Fatalf("character=%+v found=%t err=%v", character, found, err)
	}
	quests, _, _ := repositories.Quest.Load(ctx, "19")
	if quests.States[3425].Status != "completed" || quests.States[3347].ProgressValue != 0 {
		t.Fatalf("quests=%+v", quests.States)
	}
}

func TestCurrentParentScalarArchivesTownLinkedChildAndSendsMinimalSnapshots(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: defaultAccountPrefix + "1", Level: 60}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		3249: {Status: "active", ProgressValue: 1},
		3609: {Status: "completed", ProgressValue: 0},
		3610: {Status: "active", ProgressValue: 0},
	}}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		questCatalog:       linkedObjectiveBridgeCatalog(t),
	}
	session := &gameSession{conn: connection, selectedCharacterID: 19, channel: channelcatalog.Channel{ID: 19}}
	body := []byte{0x21, 0x00, 0xb1, 0x0c, 0x00, 0x00}
	if err := service.handleCurrentSetQuestTrigger(session, body); err != nil {
		t.Fatal(err)
	}
	packets := splitAllFinishBridgePackets(t, connection.write.Bytes())
	if got := finishBridgePacketIDs(packets); !reflect.DeepEqual(got, []string{"1/33", "0/574", "0/356"}) {
		t.Fatalf("packet order=%v", got)
	}
	if len(packets[0].Body) != 7 || packets[0].Header.MsgID != uint16(dnfenum.CmdPacketSetQuestTrigger) {
		t.Fatalf("op33=%+v body=%x", packets[0].Header, packets[0].Body)
	}
	quests, _, _ := repositories.Quest.Load(ctx, "19")
	if quests.States[3610].Status != "completed" || quests.States[3249].ProgressValue != 0 {
		t.Fatalf("quests=%+v", quests.States)
	}
}
