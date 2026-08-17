package dnfbridge

import (
	"context"
	"strconv"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) createCharacter(session *gameSession, req createCharacterRequest, upper bool) error {
	repos, ok := s.repositoryGroup()
	if !ok || repos.Character == nil {
		s.logGameEvent(session, "game-create-repository-missing", "upper", upper)
		return s.sendCreateFailure(session, upper, createCodeServerError)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()

	accountID := s.accountIDForSession(session)
	name, err := decodeCharacterName(req.nameRaw)
	if err != nil {
		s.logGameEvent(session, "game-create-name-decode-failed", "account_id", accountID, "name_len", len(req.nameRaw), "error", err)
		return s.sendCreateFailure(session, upper, createCodeServerError)
	}
	existing, err := s.listCharacters(ctx, repos, accountID)
	if err != nil {
		s.logGameEvent(session, "game-create-list-failed", "account_id", accountID, "error", err)
		return s.sendCreateFailure(session, upper, createCodeServerError)
	}
	if len(existing) >= defaultCharacterSlots {
		return s.sendCreateFailure(session, upper, createCodeSlotFull)
	}
	if duplicated, err := s.characterNameExists(ctx, repos, existing, name); err != nil {
		s.logGameEvent(session, "game-create-name-check-failed", "account_id", accountID, "name", name, "error", err)
		return s.sendCreateFailure(session, upper, createCodeServerError)
	} else if duplicated {
		return s.sendCreateFailure(session, upper, createCodeDuplicated)
	}
	characterID, err := s.nextCharacterID(ctx, repos, existing)
	if err != nil {
		s.logGameEvent(session, "game-create-id-failed", "account_id", accountID, "error", err)
		return s.sendCreateFailure(session, upper, createCodeServerError)
	}
	if upper && characterID > upperMaxCharacter {
		s.logGameEvent(session, "game-upper-create-id-too-large", "account_id", accountID, "character_id", characterID)
		return s.sendUpperCreateError(session, createCodeServerError)
	}
	slot := nextCharacterSlot(existing)
	now := time.Now().UTC()
	record := dnfrepo.CharacterRecord{
		CharacterID: strconv.Itoa(characterID),
		AccountID:   accountID,
		Slot:        slot,
		Name:        name,
		Job:         strconv.Itoa(int(req.job)),
		Level:       newCharacterInitialLevel,
		Stats:       defaultCreatedCharacterStatsFromRequest(req),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.saveNewCharacter(ctx, repos, record); err != nil {
		s.logGameEvent(session, "game-create-save-failed", "account_id", accountID, "character_id", characterID, "error", err)
		return s.sendCreateFailure(session, upper, createCodeServerError)
	}
	updated, err := s.listCharacters(ctx, repos, accountID)
	if err != nil {
		updated = append(existing, record)
	}
	if err := s.sendCreateSuccess(session, upper, record, updated); err != nil {
		return err
	}
	s.logGameEvent(session, "game-create-success",
		"upper", upper,
		"account_id", accountID,
		"character_id", characterID,
		"slot", slot,
		"job", req.job,
		"name", name,
		"option_len", len(req.options))
	return nil
}
