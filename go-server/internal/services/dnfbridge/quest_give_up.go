package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
)

const currentGiveUpQuestRequestBodySize = 4

// currentGiveUpQuestReplayKey scopes the destructive operation receipt to one
// selected character and one TCP session. The first ACK removes the row from
// the current client's task UI, so a duplicate success receipt is unsafe.
type currentGiveUpQuestReplayKey struct {
	characterID uint16
	questID     uint16
}

func newCurrentGiveUpQuestReplayKey(characterID uint16, request dnfquest.QuestIDRequest) currentGiveUpQuestReplayKey {
	return currentGiveUpQuestReplayKey{characterID: characterID, questID: request.QuestID}
}

func (session *gameSession) currentGiveUpQuestReplaySuppressed(key currentGiveUpQuestReplayKey) bool {
	if session == nil {
		return false
	}
	session.questReplay.giveUpMu.Lock()
	defer session.questReplay.giveUpMu.Unlock()
	_, ok := session.questReplay.giveUpAnswered[key]
	return ok
}

func (session *gameSession) suppressCurrentGiveUpQuestReplay(key currentGiveUpQuestReplayKey) {
	if session == nil {
		return
	}
	session.questReplay.giveUpMu.Lock()
	defer session.questReplay.giveUpMu.Unlock()
	if session.questReplay.giveUpAnswered == nil {
		session.questReplay.giveUpAnswered = make(map[currentGiveUpQuestReplayKey]struct{}, 1)
	}
	session.questReplay.giveUpAnswered[key] = struct{}{}
}

func (session *gameSession) clearCurrentGiveUpQuestReplay(key currentGiveUpQuestReplayKey) {
	if session == nil {
		return
	}
	session.questReplay.giveUpMu.Lock()
	defer session.questReplay.giveUpMu.Unlock()
	delete(session.questReplay.giveUpAnswered, key)
}

func (s *Service) handleCurrentGiveUpQuest(session *gameSession, body []byte) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	opcode := uint16(dnfenum.CmdPacketGiveupQuest)
	if len(body) != currentGiveUpQuestRequestBodySize || binary.LittleEndian.Uint16(body[:2]) != opcode {
		s.logGameEvent(session, "game-upper-give-up-quest-blocked",
			"body_len", len(body),
			"reason", "current_exe_op32_requires_exact_echo_and_quest_id")
		return nil
	}
	request, err := dnfquest.DecodeQuestIDRequest(body)
	if err != nil || request.QuestID == 0 {
		s.logGameEvent(session, "game-upper-give-up-quest-blocked",
			"body_len", len(body),
			"reason", "request_decode_failed_or_zero_quest_id",
			"error", err)
		return nil
	}
	replayKey := newCurrentGiveUpQuestReplayKey(session.selectedCharacterID, request)
	if session.currentGiveUpQuestReplaySuppressed(replayKey) {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		s.logGameEvent(session, "game-upper-give-up-quest-blocked",
			"quest_id", request.QuestID,
			"reason", "quest_catalog_unavailable",
			"error", err)
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || repositories.Quest == nil {
		s.logGameEvent(session, "game-upper-give-up-quest-blocked",
			"quest_id", request.QuestID,
			"reason", "character_or_quest_repository_unavailable")
		return nil
	}
	owner, err := dnfquest.NewOwner(repositories)
	if err != nil {
		return fmt.Errorf("create quest give-up owner: %w", err)
	}
	result, err := owner.ApplyGiveUp(ctx, catalog, dnfquest.NewQuestIDCommand(alignedcmd.Request{
		AccountID:           s.accountIDForSession(session),
		SelectedCharacterID: session.selectedCharacterID,
	}, "giveup_quest", request))
	if err != nil {
		s.logGameEvent(session, "game-upper-give-up-quest-blocked",
			"quest_id", request.QuestID,
			"reason", "pvf_db_give_up_preflight_or_persist_failed",
			"error", err)
		switch {
		case errors.Is(err, dnfquest.ErrQuestNotActive), errors.Is(err, dnfquest.ErrQuestDefinitionMissing):
			// 86JP's matching current protocol uses error 19 when the active
			// quest cannot be found. No mutation has occurred on this path.
			return s.sendGameUpperFailure(session, opcode, 19)
		case errors.Is(err, dnfquest.ErrQuestCannotGiveUp):
			return s.sendGameUpperFailure(session, opcode, 20)
		case errors.Is(err, dnfquest.ErrGiveUpNeedsAssets):
			// Keep the quest and its items intact until inventory reclamation
			// can join the same transaction. Error 23 is the current generic
			// unavailable-action resource.
			return s.sendGameUpperFailure(session, opcode, 23)
		default:
			// Persistence uncertainty must never produce a success receipt.
			return nil
		}
	}

	var payload packetWriter
	payload.writeUint16(result.QuestID)
	s.logGameEvent(session, "game-upper-give-up-quest-success-send",
		"quest_id", result.QuestID,
		"msg_id", opcode,
		"classification", dnfproto.DefaultChannelClassification,
		"plain_body_len", 3,
		"body_source", "current_exe_sub_1D1A4B0_u16_success_reader_after_persisted_removal")
	if err := s.sendGameUpperSuccess(session, opcode, payload.bytes()); err != nil {
		return err
	}
	session.clearCurrentAcceptQuestReplay(newCurrentAcceptQuestReplayKey(session.selectedCharacterID, request))
	session.suppressCurrentGiveUpQuestReplay(replayKey)
	return nil
}
