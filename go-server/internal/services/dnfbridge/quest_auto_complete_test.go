package dnfbridge

import (
	"context"
	"encoding/binary"
	"reflect"
	"strconv"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentAutoCompleteMainQuestLegacyRouteCommitsNoRewardAndSendsExactReaderThenSnapshots(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "17",
		AccountID:   defaultAccountPrefix + "1",
		Job:         "2",
		Level:       90,
		Stats:       map[string]int64{"grow_type": 0, "sp": 999, "exp": 54321},
	}
	if err := repositories.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "17",
		States: map[int64]dnfrepo.QuestState{
			100: {Status: "active", ProgressValue: 1},
			102: {Status: "active", ProgressValue: 1},
			103: {Status: "active", ProgressValue: 1},
			106: {Status: "active", ProgressValue: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAutoCompleteMainBridgeCatalog(t),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                connection,
		connID:              "auto-complete-main-legacy",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 17,
	}
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketClearQuestTicket),
		[]byte{100, 0, 0, 0},
	); err != nil {
		t.Fatal(err)
	}

	stored, found, err := repositories.Quest.Load(ctx, "17")
	if err != nil || !found {
		t.Fatalf("load quest found=%t err=%v", found, err)
	}
	for _, questID := range []int64{100} {
		if state := stored.States[questID]; state.Status != "completed" || state.Extra["reward_state"] != "granted" {
			t.Fatalf("epic quest %d state=%+v", questID, state)
		}
	}
	if _, exists := stored.States[101]; exists {
		t.Fatalf("absent successor quest 101 was manufactured: %+v", stored.States[101])
	}
	for _, questID := range []int64{102, 103, 106} {
		if state := stored.States[questID]; state.Status != "active" || state.ProgressValue != 1 {
			t.Fatalf("non-target quest %d state=%+v", questID, state)
		}
	}
	postCharacter, found, err := repositories.Character.Load(ctx, "17")
	if err != nil || !found || !reflect.DeepEqual(postCharacter, character) {
		t.Fatalf("no-reward operation changed character found=%t err=%v got=%+v want=%+v", found, err, postCharacter, character)
	}

	response, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if response.Header.MsgID != uint16(dnfenum.CmdPacketClearQuestTicket) ||
		response.Header.Classification != dnfproto.DefaultChannelClassification ||
		len(response.Body) != currentAutoCompleteMainQuestResponseSize {
		t.Fatalf("op1429 response header=%+v body=%x", response.Header, response.Body)
	}
	if got := binary.LittleEndian.Uint32(response.Body[0:4]); got != 100 {
		t.Fatalf("request selector=%d", got)
	}
	if got := binary.LittleEndian.Uint32(response.Body[4:8]); got != 100 {
		t.Fatalf("highest completed quest=%d", got)
	}
	if got := binary.LittleEndian.Uint32(response.Body[8:12]); got != 89 || response.Body[12] != 1 {
		t.Fatalf("cutoff/committed body=%x", response.Body)
	}

	if len(rest) < 16 || rest[0] != 0 || binary.LittleEndian.Uint16(rest[1:3]) != currentClearQuestListMsgID {
		t.Fatalf("completed-quest snapshot header=%x", rest[:minInt(len(rest), 16)])
	}
	clearPacketLength := int(binary.LittleEndian.Uint32(rest[3:7]))
	if clearPacketLength < 16 || clearPacketLength > len(rest) || rest[15] != 1 {
		t.Fatalf("completed-quest snapshot length=%d wire=%x", clearPacketLength, rest[:minInt(len(rest), 16)])
	}
	clearBody, err := zlibDecompress(rest[16:clearPacketLength])
	if err != nil {
		t.Fatal(err)
	}
	if clearBody[4+100] != 1 || clearBody[4+101] != 0 || clearBody[4+102] != 0 || clearBody[4+103] != 0 {
		t.Fatalf("completed-quest flags 100=%d 101=%d 102=%d 103=%d", clearBody[4+100], clearBody[4+101], clearBody[4+102], clearBody[4+103])
	}
	active, acceptableWire := splitGameServerUpperPacket(t, rest[clearPacketLength:])
	if active.Header.MsgID != currentActiveQuestSnapshotMsgID || active.Header.Classification != 0 {
		t.Fatalf("active snapshot header=%+v body=%x", active.Header, active.Body)
	}
	acceptable, trailing := splitGameServerUpperPacket(t, acceptableWire)
	if acceptable.Header.MsgID != currentAcceptableQuestListMsgID || acceptable.Header.Classification != 0 || len(trailing) != 0 {
		t.Fatalf("acceptable snapshot header=%+v trailing=%x", acceptable.Header, trailing)
	}
	if len(acceptable.Body) < 4 || int(binary.LittleEndian.Uint32(acceptable.Body[:4])) != len(acceptable.Body)-4 {
		t.Fatalf("acceptable snapshot protobuf length mismatch: %x", acceptable.Body)
	}
	_, messages := consumeCurrentSkillInfoFields(t, acceptable.Body[4:])
	if len(messages[4]) != 1 {
		t.Fatalf("acceptable snapshot field4 rows=%d body=%x", len(messages[4]), acceptable.Body)
	}
	questIDs := consumePackedQuestIDs(t, messages[4][0])
	foundSuccessor := false
	for _, questID := range questIDs {
		if questID == 100 {
			t.Fatalf("completed active quest 100 remained acceptable: %v", questIDs)
		}
		if questID == 101 {
			foundSuccessor = true
		}
	}
	if !foundSuccessor {
		t.Fatalf("successor main quest 101 missing from acceptable snapshot: %v", questIDs)
	}
	activeCount := int(binary.LittleEndian.Uint32(active.Body[:4]))
	activeIDs := make([]uint16, 0, activeCount)
	for offset := 4; offset+6 <= len(active.Body); offset += 6 {
		activeIDs = append(activeIDs, binary.LittleEndian.Uint16(active.Body[offset:offset+2]))
	}
	if !reflect.DeepEqual(activeIDs, []uint16{102, 103, 106}) {
		t.Fatalf("active quest ids=%v body=%x", activeIDs, active.Body)
	}
}

func TestCurrentAutoCompleteMainQuestNonzeroSelectorCompletesOnlyExactQuest(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "17", AccountID: defaultAccountPrefix + "1", Job: "2", Level: 90,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "17",
		States: map[int64]dnfrepo.QuestState{
			100: {Status: "active"},
			106: {Status: "active"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAutoCompleteMainBridgeCatalog(t),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, selectedCharacterID: 17}
	if err := service.handleCurrentAutoCompleteMainQuest(session, []byte{100, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	stored, found, err := repositories.Quest.Load(ctx, "17")
	if err != nil || !found || stored.States[100].Status != "completed" || stored.States[106].Status != "active" {
		t.Fatalf("exact selector result found=%t err=%v record=%+v", found, err, stored)
	}
	response, _ := splitGameServerUpperPacket(t, connection.write.Bytes())
	if got := binary.LittleEndian.Uint32(response.Body[0:4]); got != 100 || response.Body[12] != 1 {
		t.Fatalf("exact selector response=%x", response.Body)
	}
}

func TestCurrentAutoCompleteMainQuestZeroSelectorCompletesAllEligibleMainQuests(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "17", AccountID: defaultAccountPrefix + "1", Job: "2", Level: 90,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "17",
		States: map[int64]dnfrepo.QuestState{
			100: {Status: "active"},
			106: {Status: "active"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAutoCompleteMainBridgeCatalog(t),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, selectedCharacterID: 17}
	if err := service.handleCurrentAutoCompleteMainQuest(session, []byte{0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	stored, found, err := repositories.Quest.Load(ctx, "17")
	if err != nil || !found || stored.States[100].Status != "completed" || stored.States[106].Status != "completed" {
		t.Fatalf("zero selector result found=%t err=%v record=%+v", found, err, stored)
	}
	if stored.States[100].Extra["reward_state"] != "granted" || stored.States[106].Extra["reward_state"] != "granted" {
		t.Fatalf("zero selector must remain no-reward completion record=%+v", stored)
	}
	response, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(response.Body) != currentAutoCompleteMainQuestResponseSize || response.Body[12] != 1 || binary.LittleEndian.Uint32(response.Body[4:8]) != 106 {
		t.Fatalf("zero selector response=%x trailing=%x", response.Body, trailing)
	}
	if len(trailing) == 0 {
		t.Fatal("zero selector success missing post-commit quest snapshots")
	}
}

func buildAutoCompleteMainBridgeCatalog(t *testing.T) *dnfquest.Catalog {
	t.Helper()
	definition := func(grade string, level int, job, extra string) string {
		return "[grade]\n`" + grade + "`\n[level]\n" + strconv.Itoa(level) + " 99\n[job]\n`" + job + "`\n[exposed by npc]\n1\n" + extra
	}
	source := questListTestSource{
		dnfquest.DefaultList:            "100 `epic_start.qst`\n101 `epic_next.qst`\n102 `epic_level_90.qst`\n103 `side.qst`\n106 `other_active_epic.qst`\n",
		"n_quest/epic_start.qst":        definition("[epic]", 20, "[gunner]", "[type]\n`[clear map]`\n"),
		"n_quest/epic_next.qst":         definition("[epic]", 30, "[gunner]", "[type]\n`[meet npc]`\n[pre required quest]\n100\n"),
		"n_quest/epic_level_90.qst":     definition("[epic]", 90, "[gunner]", "[type]\n`[meet npc]`\n[pre required quest]\n101\n"),
		"n_quest/side.qst":              definition("[side]", 10, "[gunner]", "[type]\n`[meet npc]`\n"),
		"n_quest/other_active_epic.qst": definition("[epic]", 35, "[gunner]", "[type]\n`[meet npc]`\n"),
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
