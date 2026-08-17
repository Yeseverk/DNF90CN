package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"reflect"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

type questListTestSource map[string]string

func (s questListTestSource) ReadText(relativePath string) (string, error) {
	value, ok := s[relativePath]
	if !ok {
		return "", errors.New("missing quest test path: " + relativePath)
	}
	return value, nil
}

func TestBuildCurrentAcceptableQuestListBodyMatchesCurrentProtobufFields(t *testing.T) {
	body := buildCurrentAcceptableQuestListBody(37, []int32{3054, 3105})
	if len(body) < 4 || int(binary.LittleEndian.Uint32(body[:4])) != len(body)-4 {
		t.Fatalf("op0x15 protobuf length mismatch body=%x", body)
	}
	varints, messages := consumeCurrentSkillInfoFields(t, body[4:])
	if !reflect.DeepEqual(varints[1], []uint64{currentAcceptableQuestListEnum}) {
		t.Fatalf("field1 enum=%v", varints[1])
	}
	if !reflect.DeepEqual(varints[2], []uint64{37}) {
		t.Fatalf("field2 level=%v", varints[2])
	}
	if _, exists := varints[3]; exists {
		t.Fatalf("unproven field3 was emitted: %v", varints[3])
	}
	if len(messages[4]) != 1 {
		t.Fatalf("field4 packed rows=%d", len(messages[4]))
	}
	if got := consumePackedQuestIDs(t, messages[4][0]); !reflect.DeepEqual(got, []int32{3054, 3105}) {
		t.Fatalf("field4 quest ids=%v", got)
	}
}

func TestCurrentAcceptableQuestListUsesPVFAndRepositoryState(t *testing.T) {
	catalog := buildQuestListTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "12",
		AccountID:   defaultAccountPrefix + "1",
		Name:        "gunner",
		Job:         "2",
		Level:       2,
		Stats:       map[string]int64{"grow_type": 0},
	}
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "12",
		States: map[int64]dnfrepo.QuestState{
			100: {Status: "completed"},
			102: {Status: "active"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: &bufferConn{}, connID: "quest-test", selectedCharacterID: 12}
	body, ok := service.buildCurrentAcceptableQuestListBodyForSession(context.Background(), session)
	if !ok {
		t.Fatal("PVF/DB quest body was skipped")
	}
	_, messages := consumeCurrentSkillInfoFields(t, body[4:])
	if got := consumePackedQuestIDs(t, messages[4][0]); !reflect.DeepEqual(got, []int32{101, 102}) {
		t.Fatalf("quest-list ids=%v, want completed prerequisite successor plus real active quest", got)
	}
}

func TestSendCurrentAcceptableQuestListUsesHex15AndNeverDecimal138(t *testing.T) {
	catalog := buildQuestListTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "12", Job: "2", Level: 1, Stats: map[string]int64{"grow_type": 0},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, selectedCharacterID: 12}
	packet := csharpSelectInitPacket{class: 0, msgID: currentAcceptableQuestListMsgID, kind: currentAcceptableQuestListKind}
	if err := service.sendCSharpSelectInitPacket(session, packet, nil); err != nil {
		t.Fatal(err)
	}
	decoded, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if decoded.Header.Classification != 0 || decoded.Header.MsgID != 0x15 {
		t.Fatalf("quest packet class=%d msg=%d rest=%x", decoded.Header.Classification, decoded.Header.MsgID, rest)
	}
	if decoded.Header.MsgID == 138 {
		t.Fatal("quest packet used disproved decimal op138")
	}
	firstPacketLen := len(connection.write.Bytes()) - len(rest)
	if _, err := dnfproto.ParseChannelPacketUnchecked(connection.write.Bytes()[:firstPacketLen]); err != nil {
		t.Fatalf("parse first sent op0x15: %v", err)
	}
	active, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || active.Header.Classification != 0 || active.Header.MsgID != currentActiveQuestSnapshotMsgID || !reflect.DeepEqual(active.Body, []byte{0, 0, 0, 0}) {
		t.Fatalf("active snapshot header=%+v body=%x trailing=%x", active.Header, active.Body, trailing)
	}
}

func TestLongHengSceneBootstrapContainsDynamicHex15AndNoDecimal138Quest(t *testing.T) {
	found := 0
	for _, packet := range longHengSceneBootstrapBeforeHudPackets {
		if packet.kind == currentAcceptableQuestListKind {
			found++
			if packet.msgID != 0x15 || len(packet.body) != 0 {
				t.Fatalf("dynamic quest packet msg=%d body=%x", packet.msgID, packet.body)
			}
		}
		if packet.msgID == 138 {
			t.Fatalf("bootstrap still contains disproved decimal op138 body=%x kind=%q", packet.body, packet.kind)
		}
	}
	if found != 1 {
		t.Fatalf("dynamic op0x15 count=%d, want 1", found)
	}
}

func TestDeferredSelectSceneTailKeepsDynamicHex15WithoutLargeRosterFixture(t *testing.T) {
	packets := deferredSelectSceneTailPackets()
	found := 0
	legacyHUDPackets := 0
	for index, packet := range packets {
		if packet.msgID == uint16(dnfenum.CmdPacketCancelCargoPad) {
			t.Fatalf("deferred tail[%d] replayed old op391 cargo transport", index)
		}
		if packet.kind == "select_scene_main_hud" {
			legacyHUDPackets++
		}
		if packet.kind != currentAcceptableQuestListKind {
			continue
		}
		found++
		if packet.class != 0 || packet.msgID != currentAcceptableQuestListMsgID || len(packet.body) != 0 {
			t.Fatalf("deferred quest packet class=%d msg=%d body=%x", packet.class, packet.msgID, packet.body)
		}
	}
	if found != 1 {
		t.Fatalf("deferred dynamic op0x15 count=%d, want 1", found)
	}
	if legacyHUDPackets != 0 {
		t.Fatalf("deferred legacy HUD packet count=%d", legacyHUDPackets)
	}
}

func TestRealScriptPVFPreloadsCurrentQuestCatalog(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify dnfbridge quest preload")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{initialEquipmentArchive: archive}
	if err := service.preloadQuestCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	if service.questCatalog == nil || service.questCatalog.Snapshot().Definitions == 0 {
		t.Fatalf("quest catalog was not retained: %+v", service.questCatalog)
	}
}

func buildQuestListTestCatalog(t *testing.T) *dnfquest.Catalog {
	t.Helper()
	source := questListTestSource{
		dnfquest.DefaultList: "100 `main.qst`\n101 `next.qst`\n102 `active.qst`\n",
		"n_quest/main.qst":   "[grade]\n`[epic]`\n[level]\n1 10\n[job]\n`[gunner]`\n[exposed by npc]\n1\n",
		"n_quest/next.qst":   "[grade]\n`[epic]`\n[level]\n2 10\n[job]\n`[gunner]`\n[exposed by npc]\n1\n[pre required quest]\n100\n",
		"n_quest/active.qst": "[grade]\n`[normal]`\n[level]\n1 10\n[job]\n`[gunner]`\n[exposed by npc]\n1\n",
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

func TestCurrentAcceptableQuestListRepairsLegacyAbandonedExpertJobChain(t *testing.T) {
	catalog := buildExpertJobAbandonRecoveryTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "12", Job: "2", Level: 90,
		Stats: map[string]int64{"grow_type": 0, "expert_job_type": 0, currentExpertJobGiveUpStateStat: 1},
	}
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "12",
		States: map[int64]dnfrepo.QuestState{
			200: {Status: "completed"}, 201: {Status: "completed"}, 900: {Status: "completed"},
		},
		Progress: map[int64]dnfrepo.QuestState{
			201: {Status: "completed", ProgressValue: 1}, 901: {Status: "active", ProgressValue: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{questCatalog: catalog, repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true }}
	body, ok := service.buildCurrentAcceptableQuestListBodyForSession(context.Background(), &gameSession{selectedCharacterID: 12})
	if !ok {
		t.Fatal("acceptable quest list was skipped")
	}
	_, messages := consumeCurrentSkillInfoFields(t, body[4:])
	if got := consumePackedQuestIDs(t, messages[4][0]); !reflect.DeepEqual(got, []int32{200, 901}) {
		t.Fatalf("quest-list ids=%v want reset expert-job entrance plus unrelated active", got)
	}
	repaired, found, err := repositories.Quest.Load(context.Background(), "12")
	if err != nil || !found {
		t.Fatalf("load repaired record found=%t err=%v", found, err)
	}
	for _, questID := range []int64{200, 201} {
		if _, exists := repaired.States[questID]; exists {
			t.Fatalf("legacy transition quest %d remained in states: %+v", questID, repaired.States)
		}
		if _, exists := repaired.Progress[questID]; exists {
			t.Fatalf("legacy transition quest %d remained in progress: %+v", questID, repaired.Progress)
		}
	}
	if _, exists := repaired.States[900]; !exists {
		t.Fatalf("unrelated completed quest was removed: %+v", repaired.States)
	}
	if _, exists := repaired.Progress[901]; !exists {
		t.Fatalf("unrelated active quest was removed: %+v", repaired.Progress)
	}
}

func TestCurrentAcceptableQuestListDoesNotResetActiveExpertJobTransition(t *testing.T) {
	catalog := buildExpertJobAbandonRecoveryTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	_ = repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "12", Job: "2", Level: 90,
		Stats: map[string]int64{"grow_type": 0, "expert_job_type": 0, currentExpertJobGiveUpStateStat: 1},
	})
	want := dnfrepo.QuestRecord{CharacterID: "12", States: map[int64]dnfrepo.QuestState{200: {Status: "completed"}, 201: {Status: "active"}}}
	_ = repositories.Quest.Save(context.Background(), want)
	service := &Service{questCatalog: catalog, repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true }}
	if _, ok := service.buildCurrentAcceptableQuestListBodyForSession(context.Background(), &gameSession{selectedCharacterID: 12}); !ok {
		t.Fatal("acceptable quest list was skipped")
	}
	got, _, _ := repositories.Quest.Load(context.Background(), "12")
	if !reflect.DeepEqual(got.States, want.States) {
		t.Fatalf("active expert-job transition was reset: got=%+v want=%+v", got.States, want.States)
	}
}

func buildExpertJobAbandonRecoveryTestCatalog(t *testing.T) *dnfquest.Catalog {
	t.Helper()
	source := questListTestSource{
		dnfquest.DefaultList:           "200 `expert_entry.qst`\n201 `expert_terminal.qst`\n900 `unrelated_done.qst`\n901 `unrelated_active.qst`\n",
		"n_quest/expert_entry.qst":     "[grade]\n`[normal]`\n[level]\n1 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n[job change quest]\n20\n",
		"n_quest/expert_terminal.qst":  "[grade]\n`[normal]`\n[level]\n1 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n[job change quest]\n20\n[pre required quest]\n200\n",
		"n_quest/unrelated_done.qst":   "[grade]\n`[normal]`\n[level]\n1 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n",
		"n_quest/unrelated_active.qst": "[grade]\n`[normal]`\n[level]\n1 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n",
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

func consumePackedQuestIDs(t *testing.T, raw []byte) []int32 {
	t.Helper()
	var ids []int32
	for len(raw) > 0 {
		value, consumed := protowire.ConsumeVarint(raw)
		if consumed < 0 {
			t.Fatalf("consume packed quest id: %v", protowire.ParseError(consumed))
		}
		ids = append(ids, int32(value))
		raw = raw[consumed:]
	}
	return ids
}
