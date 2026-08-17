package dnfbridge

import (
	"bytes"
	"context"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

type activeQuestRepairTestSource map[string]string

func (s activeQuestRepairTestSource) ReadText(path string) (string, error) {
	return s[path], nil
}

func TestBuildCurrentActiveQuestSnapshotBodyMatchesCurrentEXEReader(t *testing.T) {
	record := dnfrepo.QuestRecord{States: map[int64]dnfrepo.QuestState{
		3145: {Status: "active", ProgressValue: 1},
		3146: {Status: "completed", ProgressValue: 9},
	}}
	body := buildCurrentActiveQuestSnapshotBody(record, true)
	want := []byte{0x01, 0x00, 0x00, 0x00, 0x49, 0x0c, 0x01, 0x00, 0x00, 0x00}
	if !bytes.Equal(body, want) {
		t.Fatalf("active quest snapshot body=%x want=%x", body, want)
	}
	if empty := buildCurrentActiveQuestSnapshotBody(dnfrepo.QuestRecord{}, false); !bytes.Equal(empty, []byte{0, 0, 0, 0}) {
		t.Fatalf("empty active quest snapshot body=%x", empty)
	}
}

func TestBuildCurrentActiveQuestSnapshotMergesLegacyProgressRows(t *testing.T) {
	record := dnfrepo.QuestRecord{
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "active", ProgressValue: 1},
		},
		Progress: map[int64]dnfrepo.QuestState{
			3145: {Status: "active", ProgressValue: 9},
			3157: {Status: "active", ProgressValue: 0},
			3158: {Status: "completed", ProgressValue: 1},
		},
	}
	body := buildCurrentActiveQuestSnapshotBody(record, true)
	want := []byte{
		0x02, 0x00, 0x00, 0x00,
		0x49, 0x0c, 0x01, 0x00, 0x00, 0x00,
		0x55, 0x0c, 0x00, 0x00, 0x00, 0x00,
	}
	if !bytes.Equal(body, want) {
		t.Fatalf("merged active quest snapshot body=%x want=%x", body, want)
	}
}

func TestSendCurrentActiveQuestSnapshotUsesCurrentOp23E(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "17",
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "active", ProgressValue: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true }}
	session := &gameSession{conn: connection, selectedCharacterID: 17}
	if err := service.sendCurrentActiveQuestSnapshotForSession(session, "test"); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 || packet.Header.Classification != 0 || packet.Header.MsgID != currentActiveQuestSnapshotMsgID {
		t.Fatalf("active snapshot packet=%+v rest=%x", packet.Header, rest)
	}
	want := []byte{0x01, 0x00, 0x00, 0x00, 0x49, 0x0c, 0x01, 0x00, 0x00, 0x00}
	if !bytes.Equal(packet.Body, want) {
		t.Fatalf("active snapshot body=%x want=%x", packet.Body, want)
	}
}

func TestSendCurrentActiveQuestSnapshotRepairsLegacyMultiTargetTrigger(t *testing.T) {
	index, err := dnfpvf.Build(context.Background(), activeQuestRepairTestSource{
		dnfquest.DefaultList:  "2635 `earring.qst`\n",
		"n_quest/earring.qst": "[name]\n`test`\n[grade]\n`[side]`\n[type]\n`[hunt monster]`\n[int data]\n311 2 63767 1 314 2 63796 1\n",
	}, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			2635: {
				Status:        "active",
				ProgressValue: 513,
				Extra: map[string]string{
					"legacy_trigger_repair_kind":     "pvf_multitarget_saturated_0x1ff",
					"legacy_trigger_repair_previous": "511",
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, selectedCharacterID: 19}
	if err := service.sendCurrentActiveQuestSnapshotForSession(session, "test_repair"); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 || packet.Header.MsgID != currentActiveQuestSnapshotMsgID {
		t.Fatalf("active snapshot packet=%+v rest=%x", packet.Header, rest)
	}
	want := []byte{0x01, 0x00, 0x00, 0x00, 0x4b, 0x0a, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(packet.Body, want) {
		t.Fatalf("repaired active snapshot body=%x want=%x", packet.Body, want)
	}
	record, found, err := repositories.Quest.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("load repaired quest record found=%t err=%v", found, err)
	}
	if got := record.States[2635].ProgressValue; got != 0 {
		t.Fatalf("persisted repaired trigger=%d, want completed trigger 0", got)
	}
}
