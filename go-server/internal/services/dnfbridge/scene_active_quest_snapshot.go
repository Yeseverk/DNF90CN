package dnfbridge

import (
	"context"
	"fmt"
	"strconv"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// currentActiveQuestSnapshotMsgID is registered by the current EXE at
// sub_1D632D0.  The older 86JP client used 0x023F for the analogous
// notification; that numeric ID is not registered by this client version.
const currentActiveQuestSnapshotMsgID = uint16(0x023E)

func buildCurrentActiveQuestSnapshotBody(record dnfrepo.QuestRecord, hasRecord bool) []byte {
	rows := currentSelectAckQuestRows(record, hasRecord)
	var writer packetWriter
	writer.writeUint32(uint32(len(rows)))
	for _, row := range rows {
		writer.writeUint16(row.questID)
		writer.writeUint32(row.triggerValue)
	}
	return writer.bytes()
}

func (s *Service) sendCurrentActiveQuestSnapshotFromCommittedQuest(
	session *gameSession,
	record dnfrepo.QuestRecord,
	source string,
) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	if record.CharacterID != characterID {
		return fmt.Errorf("committed active quest snapshot owner mismatch: selected=%s record=%q", characterID, record.CharacterID)
	}
	body := buildCurrentActiveQuestSnapshotBody(record, true)
	rows := currentSelectAckQuestRows(record, true)
	s.logGameEvent(session, "game-upper-current-active-quest-snapshot-send",
		"char_id", session.selectedCharacterID,
		"msg_id", currentActiveQuestSnapshotMsgID,
		"classification", 0,
		"source", source,
		"quest_record_found", true,
		"active_count", len(rows),
		"body_len", len(body),
		"body_source", "current_exe_sub_1D632D0_u32_count_u16_quest_u32_trigger")
	return s.sendGameUpperRawClass(session, currentActiveQuestSnapshotMsgID, body, 0)
}

func (s *Service) sendCurrentActiveQuestSnapshotForSession(session *gameSession, source string) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Quest == nil {
		s.logGameEvent(session, "game-upper-current-active-quest-snapshot-skipped",
			"char_id", session.selectedCharacterID,
			"msg_id", currentActiveQuestSnapshotMsgID,
			"source", source,
			"reason", "quest_repository_unavailable")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	record, found, err := repositories.Quest.Load(ctx, strconv.Itoa(int(session.selectedCharacterID)))
	if err != nil {
		cancel()
		s.logGameEvent(session, "game-upper-current-active-quest-snapshot-skipped",
			"char_id", session.selectedCharacterID,
			"msg_id", currentActiveQuestSnapshotMsgID,
			"source", source,
			"reason", "quest_record_load_failed",
			"error", err)
		return nil
	}
	if !found {
		record = dnfrepo.QuestRecord{CharacterID: strconv.Itoa(int(session.selectedCharacterID))}
	} else {
		record = s.reconcileLegacySaturatedActiveQuestTriggers(ctx, session, repositories.Quest, record)
	}
	cancel()
	body := buildCurrentActiveQuestSnapshotBody(record, found)
	rows := currentSelectAckQuestRows(record, found)
	s.logGameEvent(session, "game-upper-current-active-quest-snapshot-send",
		"char_id", session.selectedCharacterID,
		"msg_id", currentActiveQuestSnapshotMsgID,
		"classification", 0,
		"source", source,
		"quest_record_found", found,
		"active_count", len(rows),
		"body_len", len(body),
		"body_source", "current_exe_sub_1D632D0_u32_count_u16_quest_u32_trigger")
	return s.sendGameUpperRawClass(session, currentActiveQuestSnapshotMsgID, body, 0)
}
