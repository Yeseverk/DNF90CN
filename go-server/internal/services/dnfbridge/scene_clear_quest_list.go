package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// The current EXE registers S2C 0x164 at sub_1D58470. Although the generated
// command enum retains the historical PASS_GATE_OBJECT name, the reader loads
// a fixed quest-id-indexed clear-state table and refreshes task/NPC visibility.
const (
	currentClearQuestListMsgID          = uint16(dnfenum.CmdPacketPassGateObject)
	currentClearQuestListPayloadSize    = 30000
	currentClearQuestListTransportCodec = "current_op356_clear_quest_list_transport_zlib"
)

func buildCurrentClearQuestListBody(record dnfrepo.QuestRecord, hasRecord bool) []byte {
	body := make([]byte, 4+currentClearQuestListPayloadSize)
	binary.LittleEndian.PutUint32(body[:4], currentClearQuestListPayloadSize)
	if !hasRecord {
		return body
	}

	setState := func(states map[int64]dnfrepo.QuestState) {
		for questID, state := range states {
			if questID <= 0 || questID >= currentClearQuestListPayloadSize {
				continue
			}
			flag := byte(0)
			switch normalizeDungeonQuestStatus(state.Status) {
			case "complete", "completed", "cleared", "finished", "done":
				flag = 1
			}
			body[4+int(questID)] = flag
		}
	}
	// QuestRecord.States is the canonical field. Progress is retained for old
	// rows, but a canonical state must override it even when that state is
	// active (byte 0); otherwise a stale completed Progress row resurrects a
	// cleared quest after login or an op34 refresh.
	setState(record.Progress)
	setState(record.States)
	return body
}

func buildCurrentClearQuestListTransportBody(record dnfrepo.QuestRecord, hasRecord bool) ([]byte, error) {
	return zlibCompress(buildCurrentClearQuestListBody(record, hasRecord))
}

func currentClearQuestCount(body []byte) int {
	if len(body) != 4+currentClearQuestListPayloadSize ||
		binary.LittleEndian.Uint32(body[:4]) != currentClearQuestListPayloadSize {
		return 0
	}
	count := 0
	for _, flag := range body[4:] {
		if flag != 0 {
			count++
		}
	}
	return count
}

func (s *Service) buildCurrentClearQuestListTransportBodyForSession(
	ctx context.Context,
	session *gameSession,
	source string,
) ([]byte, error) {
	if session == nil || session.selectedCharacterID == 0 {
		return nil, fmt.Errorf("build current clear quest list: selected character is unavailable")
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Quest == nil {
		s.logGameEvent(session, "game-upper-current-clear-quest-list-repository-unavailable",
			"char_id", session.selectedCharacterID,
			"msg_id", currentClearQuestListMsgID,
			"source", source)
		return nil, fmt.Errorf("build current clear quest list for character %s: %w", characterID, dnfrepo.ErrRepoMissing)
	}
	record, found, err := repositories.Quest.Load(ctx, characterID)
	if err != nil {
		return nil, fmt.Errorf("load clear quest list for character %s: %w", characterID, err)
	}
	if found && strings.TrimSpace(record.CharacterID) != characterID {
		return nil, fmt.Errorf("clear quest list owner mismatch: selected=%s record=%q", characterID, record.CharacterID)
	}
	return s.buildCurrentClearQuestListTransportBodyForRecord(session, record, found, source)
}

func (s *Service) buildCurrentClearQuestListTransportBodyForCommittedQuest(
	session *gameSession,
	record dnfrepo.QuestRecord,
	source string,
) ([]byte, error) {
	if session == nil || session.selectedCharacterID == 0 {
		return nil, fmt.Errorf("build committed clear quest list: selected character is unavailable")
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	if strings.TrimSpace(record.CharacterID) != characterID {
		return nil, fmt.Errorf("committed clear quest list owner mismatch: selected=%s record=%q", characterID, record.CharacterID)
	}
	return s.buildCurrentClearQuestListTransportBodyForRecord(session, record, true, source)
}

func (s *Service) buildCurrentClearQuestListTransportBodyForRecord(
	session *gameSession,
	record dnfrepo.QuestRecord,
	found bool,
	source string,
) ([]byte, error) {
	plain := buildCurrentClearQuestListBody(record, found)
	transport, err := zlibCompress(plain)
	if err != nil {
		return nil, fmt.Errorf("compress current clear quest list: %w", err)
	}
	if session != nil {
		s.logGameEvent(session, "game-upper-current-clear-quest-list-built",
			"char_id", session.selectedCharacterID,
			"msg_id", currentClearQuestListMsgID,
			"source", source,
			"quest_record_found", found,
			"completed_count", currentClearQuestCount(plain),
			"plain_body_len", len(plain),
			"transport_body_len", len(transport),
			"body_source", "current_exe_sub_1D58470_fixed_30000_byte_completed_quest_id_table")
	}
	return transport, nil
}

func (s *Service) sendCurrentClearQuestListFromCommittedQuest(
	session *gameSession,
	record dnfrepo.QuestRecord,
	source string,
) error {
	transport, err := s.buildCurrentClearQuestListTransportBodyForCommittedQuest(session, record, source)
	if err != nil {
		return err
	}
	return s.sendGameUpperFixed16Transport(
		session,
		currentClearQuestListMsgID,
		transport,
		0,
		1,
		true,
		currentClearQuestListTransportCodec,
	)
}

// sendCurrentPersistedClearQuestListIfCompleted restores the completed-quest
// bitmap before a town op24 commits the destination scene. Empty records need
// no replay because they cannot activate a completed-quest visibility gate.
// This helper is projection-only: it never mutates quest persistence.
func (s *Service) sendCurrentPersistedClearQuestListIfCompleted(
	ctx context.Context,
	session *gameSession,
	repositories dnfrepo.Group,
	source string,
) (bool, error) {
	if session == nil || session.selectedCharacterID == 0 {
		return false, nil
	}
	if repositories.Quest == nil {
		return false, dnfrepo.ErrRepoMissing
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	record, found, err := repositories.Quest.Load(ctx, characterID)
	if err != nil {
		return false, err
	}
	if !found || currentClearQuestCount(buildCurrentClearQuestListBody(record, true)) == 0 {
		return false, nil
	}
	if err := s.sendCurrentClearQuestListFromCommittedQuest(session, record, source); err != nil {
		return false, err
	}
	return true, nil
}
