package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentClearQuestListBodyUsesOnlyCompletedQuestIDs(t *testing.T) {
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3149:  {Status: "completed"},
			3152:  {Status: "active", ProgressValue: 0},
			30000: {Status: "completed"},
		},
		Progress: map[int64]dnfrepo.QuestState{
			3151: {Status: "cleared"},
			3153: {Status: "in_progress"},
		},
	}
	body := buildCurrentClearQuestListBody(record, true)
	if len(body) != 4+currentClearQuestListPayloadSize {
		t.Fatalf("clear quest list body len=%d", len(body))
	}
	if got := binary.LittleEndian.Uint32(body[:4]); got != currentClearQuestListPayloadSize {
		t.Fatalf("clear quest list payload len=%d", got)
	}
	for _, questID := range []int{3149, 3151} {
		if got := body[4+questID]; got != 1 {
			t.Fatalf("completed quest %d flag=%d, want 1", questID, got)
		}
	}
	for _, questID := range []int{int(currentSceneBootstrapObjectKey), 3152, 3153} {
		if got := body[4+questID]; got != 0 {
			t.Fatalf("non-completed index %d flag=%d, want 0", questID, got)
		}
	}
	if got := currentClearQuestCount(body); got != 2 {
		t.Fatalf("completed quest count=%d, want 2", got)
	}
}

func TestCurrentClearQuestListBodyCanonicalStatesOverrideLegacyProgress(t *testing.T) {
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		Progress: map[int64]dnfrepo.QuestState{
			3149: {Status: "completed"},
			3150: {Status: "active"},
		},
		States: map[int64]dnfrepo.QuestState{
			3149: {Status: "active"},
			3150: {Status: "completed"},
		},
	}
	body := buildCurrentClearQuestListBody(record, true)
	if got := body[4+3149]; got != 0 {
		t.Fatalf("canonical active quest 3149 flag=%d, want 0 over stale Progress completed", got)
	}
	if got := body[4+3150]; got != 1 {
		t.Fatalf("canonical completed quest 3150 flag=%d, want 1 over stale Progress active", got)
	}
}

func TestCurrentClearQuestListTransportLoadsSelectedCharacterRecord(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	session.selectedCharacterID = 29
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "29",
		States: map[int64]dnfrepo.QuestState{
			3149: {Status: "completed"},
			3152: {Status: "active", ProgressValue: 0},
		},
	}); err != nil {
		t.Fatal(err)
	}

	transport, err := service.buildCurrentClearQuestListTransportBodyForSession(
		context.Background(), session, "test_selected_character_clear_quest_list")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := zlibDecompress(transport)
	if err != nil {
		t.Fatal(err)
	}
	if got := plain[4+3149]; got != 1 {
		t.Fatalf("persisted completed quest 3149 flag=%d, want 1", got)
	}
	if got := plain[4+3152]; got != 0 {
		t.Fatalf("active quest 3152 flag=%d, want 0", got)
	}
	if got := plain[4+int(currentSceneActorObjectKey(29))]; got != 0 {
		t.Fatalf("selected actor key was written into clear quest table: %d", got)
	}
}

func TestCurrentClearQuestListTransportDoesNotCompletePVFTownAreaNeedQuests(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	transport, err := service.buildCurrentClearQuestListTransportBodyForSession(
		context.Background(), session, "test_initial_town_reads_completed_quests_only")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := zlibDecompress(transport)
	if err != nil {
		t.Fatal(err)
	}
	for _, questID := range []int64{3155, 3156} {
		if got := plain[4+questID]; got != 0 {
			t.Fatalf("uncompleted PVF town-area need quest %d flag=%d, want 0", questID, got)
		}
	}
	record, found, err := repositories.Quest.Load(context.Background(), "29")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("clear-quest projection mutated persistence: %+v", record)
	}
}

func TestCurrentClearQuestListTransportRejectsUnavailableRepositoryWithoutWriting(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{}
	session := &gameSession{conn: connection, connID: "clear-quest-repo-unavailable", selectedCharacterID: 19}
	transport, err := service.buildCurrentClearQuestListTransportBodyForSession(
		context.Background(), session, "test_repository_unavailable")
	if !errors.Is(err, dnfrepo.ErrRepoMissing) {
		t.Fatalf("repository unavailable error=%v, want %v", err, dnfrepo.ErrRepoMissing)
	}
	if transport != nil {
		t.Fatalf("repository unavailable transport=%x, want nil", transport)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("repository unavailable wrote %d bytes", connection.write.Len())
	}
}

func TestCurrentClearQuestListTransportMissingRecordIsAllZero(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	service := &Service{repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true }}
	session := &gameSession{connID: "clear-quest-record-missing", selectedCharacterID: 19}
	transport, err := service.buildCurrentClearQuestListTransportBodyForSession(
		context.Background(), session, "test_record_missing")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := zlibDecompress(transport)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 4+currentClearQuestListPayloadSize ||
		binary.LittleEndian.Uint32(plain[:4]) != currentClearQuestListPayloadSize {
		t.Fatalf("missing-record clear-list shape len=%d prefix=%x", len(plain), plain[:minInt(len(plain), 4)])
	}
	if !bytes.Equal(plain[4:], make([]byte, currentClearQuestListPayloadSize)) {
		t.Fatal("missing-record clear-list contains a nonzero quest flag")
	}
}

func TestCurrentClearQuestListCommittedRecordUsesProtectedMarkerAndDirectSnapshot(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{}
	session := &gameSession{conn: connection, connID: "clear-quest-committed", selectedCharacterID: 19}
	record := dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		3149: {Status: "completed"},
	}}
	if err := service.sendCurrentClearQuestListFromCommittedQuest(session, record, "test_post_commit_snapshot"); err != nil {
		t.Fatal(err)
	}
	wire := connection.write.Bytes()
	if len(wire) < 16 || wire[0] != 0 || binary.LittleEndian.Uint16(wire[1:3]) != currentClearQuestListMsgID ||
		binary.LittleEndian.Uint32(wire[3:7]) != uint32(len(wire)) ||
		binary.LittleEndian.Uint32(wire[7:11]) != 1 || wire[15] != 1 {
		t.Fatalf("committed clear-list fixed16 header=%x", wire[:minInt(len(wire), 16)])
	}
	plain, err := zlibDecompress(wire[16:])
	if err != nil {
		t.Fatal(err)
	}
	if got := plain[4+3149]; got != 1 {
		t.Fatalf("direct committed quest 3149 flag=%d, want 1", got)
	}

	connection.write.Reset()
	record.CharacterID = "20"
	if err := service.sendCurrentClearQuestListFromCommittedQuest(session, record, "test_owner_mismatch"); err == nil {
		t.Fatal("committed clear-list accepted a mismatched character owner")
	}
	if connection.write.Len() != 0 {
		t.Fatalf("owner mismatch wrote %d bytes", connection.write.Len())
	}
}
