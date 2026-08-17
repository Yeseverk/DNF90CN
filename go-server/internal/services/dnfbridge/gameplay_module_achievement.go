package dnfbridge

import (
	"context"
	"encoding/binary"

	dnfachievement "longheng.io/server/internal/modules/dnf/achievement"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func achievementGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketAchievementTrigger)
	handler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentAchievementTrigger(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name:           "achievement",
		LegacyHandlers: map[uint16]gameplayHandler{opcode: handler},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: defaultClassGameplayHandler(
				"game-achievement-trigger-blocked",
				"current_exe_achievement_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAchievementTrigger(session, body)
				},
			),
		},
	}
}

// handleCurrentAchievementTrigger owns only the current EXE request/response
// bytes and response ordering. Progress, completion, title placement, and the
// inventory commit belong to the achievement domain owner.
func (s *Service) handleCurrentAchievementTrigger(session *gameSession, body []byte) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if len(body) < 10 {
		s.logGameEvent(session, "game-achievement-trigger-blocked",
			"body_len", len(body),
			"reason", "body_too_short")
		return nil
	}
	questID := int32(binary.LittleEndian.Uint32(body[0:4]))
	delta1 := binary.LittleEndian.Uint16(body[4:6])
	delta2 := binary.LittleEndian.Uint16(body[6:8])
	delta3 := binary.LittleEndian.Uint16(body[8:10])

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repos, ok := s.repositoryGroup()
	if !ok {
		return nil
	}
	owner, err := dnfachievement.NewOwner(repos, currentAchievementRewardResolver{service: s})
	if err != nil {
		return nil
	}
	result, err := owner.Trigger(ctx, dnfachievement.Command{
		SelectedCharacterID: session.selectedCharacterID,
		QuestID:             questID,
		Delta1:              delta1,
		Delta2:              delta2,
		Delta3:              delta3,
	})
	if err != nil {
		s.logGameEvent(session, "game-achievement-trigger-save-failed",
			"char_id", session.selectedCharacterID,
			"reason", err)
		return nil
	}

	s.logGameEvent(session, "game-achievement-trigger",
		"char_id", result.CharacterID,
		"quest_id", result.QuestID,
		"remain1", result.Remain1,
		"remain2", result.Remain2,
		"remain3", result.Remain3,
		"completed", result.Completed)

	var w packetWriter
	w.writeInt32(int(result.QuestID))
	w.writeUint16(result.Remain1)
	w.writeUint16(result.Remain2)
	w.writeUint16(result.Remain3)
	if err := s.sendGameUpperSuccess(session, currentAchievementTriggerMsgID, w.bytes()); err != nil {
		return err
	}
	if !result.Completed {
		return nil
	}

	if result.TitleGranted {
		s.logGameEvent(session, "game-achievement-title-granted",
			"char_id", result.CharacterID,
			"quest_id", result.QuestID,
			"title_item_id", result.TitleItemID,
			"category", result.TitleCategory,
			"slot", result.TitleSlot)
		if err := s.sendCurrentTitleBookList(
			session,
			ctx,
			repos,
			result.CharacterID,
			result.TitleCategory,
		); err != nil {
			return err
		}
	}
	// Current EXE op417 marks the achievement complete itself when all three
	// returned remaining counters are zero. Do not send historical op360 here:
	// this client registers that opcode for a different feature.
	return nil
}

type currentAchievementRewardResolver struct {
	service *Service
}

func (r currentAchievementRewardResolver) ResolveAchievementDefinition(
	ctx context.Context,
	questID int32,
) (dnfachievement.Definition, error) {
	if r.service == nil {
		return dnfachievement.Definition{}, dnfachievement.ErrDefinitionNotFound
	}
	return r.service.resolveAchievementDefinition(ctx, questID)
}
