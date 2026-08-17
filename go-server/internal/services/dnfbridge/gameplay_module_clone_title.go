package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"

	dnfclonetitle "longheng.io/server/internal/modules/dnf/clonetitle"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const currentSetCloneTitleMsgID uint16 = uint16(dnfenum.CmdPacketSetCloneTitle)

func cloneTitleGameplayModule() gameplayModuleDefinition {
	opcode := uint16(currentSetCloneTitleMsgID)
	handler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentSetCloneTitle(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name:           "clone-title",
		LegacyHandlers: map[uint16]gameplayHandler{opcode: handler},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: defaultClassGameplayHandler(
				"game-clone-title-blocked",
				"current_exe_clone_title_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentSetCloneTitle(session, body)
				},
			),
		},
	}
}

// handleCurrentSetCloneTitle processes CMD 0x0238 / 568: set clone title animation.
// C->S body: i32 clone_title_item_id.
// Current NoPack registers S->C op568 as DoNothing. The visible state is
// therefore refreshed through the repository-backed class0/op2 mode0 actor
// projection after the selection has been committed.
func (s *Service) handleCurrentSetCloneTitle(session *gameSession, body []byte) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if len(body) < 4 {
		s.logGameEvent(session, "game-set-clone-title-blocked", "body_len", len(body), "reason", "body_too_short")
		return nil
	}
	cloneTitleItemID := int32(binary.LittleEndian.Uint32(body[0:4]))

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repos, ok := s.repositoryGroup()
	if !ok {
		return nil
	}
	owner, err := dnfclonetitle.NewOwner(repos)
	if err != nil {
		return nil
	}
	result, err := owner.Apply(ctx, dnfclonetitle.Command{
		AccountID:           s.accountIDForSession(session),
		SelectedCharacterID: session.selectedCharacterID,
		ItemID:              cloneTitleItemID,
	})
	if errors.Is(err, dnfclonetitle.ErrTitleNotOwned) {
		s.logGameEvent(session, "game-set-clone-title-blocked",
			"char_id", session.selectedCharacterID,
			"clone_title_item_id", cloneTitleItemID,
			"reason", "title_not_owned_in_title_book")
		return nil
	}
	if err != nil {
		s.logGameEvent(session, "game-set-clone-title-save-failed",
			"char_id", session.selectedCharacterID,
			"reason", err)
		return nil
	}

	s.logGameEvent(session, "game-set-clone-title-success",
		"char_id", result.CharacterID, "clone_title_item_id", result.ItemID)

	if err := s.sendSelectedActorAppearanceRefresh(
		session,
		"set_clone_title",
		"durable_clone_title_then_full_class0_op2_mode0_title_slot13",
	); err != nil {
		s.logGameEvent(session, "game-set-clone-title-refresh-failed",
			"char_id", result.CharacterID,
			"clone_title_item_id", result.ItemID,
			"reason", err)
	}
	return nil
}
