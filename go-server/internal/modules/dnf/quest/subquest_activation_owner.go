package quest

import (
	"context"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type LinkedSubQuestActivationPersistResult struct {
	CharacterID   string
	Activations   []LinkedSubQuestActivation
	ChangedFields []dnfrepo.QuestField
	Idempotent    bool
}

func (o *Owner) ApplyActiveQuestClearLinkedSubQuestActivation(
	ctx context.Context,
	catalog *Catalog,
	characterID string,
	activatedAt time.Time,
) (LinkedSubQuestActivationPersistResult, error) {
	if o == nil || o.characters == nil || o.quests == nil {
		return LinkedSubQuestActivationPersistResult{}, ErrOwnerUnavailable
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return LinkedSubQuestActivationPersistResult{}, ErrCharacterRequired
	}
	if _, ok, err := o.characters.Load(ctx, characterID); err != nil {
		return LinkedSubQuestActivationPersistResult{}, err
	} else if !ok {
		return LinkedSubQuestActivationPersistResult{}, ErrCharacterNotFound
	}
	record, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return LinkedSubQuestActivationPersistResult{}, err
	}
	if !ok {
		return LinkedSubQuestActivationPersistResult{CharacterID: characterID, Idempotent: true}, nil
	}
	record = dnfrepo.CloneQuest(record)
	if strings.TrimSpace(record.CharacterID) == "" {
		record.CharacterID = characterID
	}
	plan, err := catalog.PlanActiveQuestClearLinkedSubQuestActivation(record, activatedAt)
	if err != nil {
		return LinkedSubQuestActivationPersistResult{}, err
	}
	if strings.TrimSpace(plan.Record.CharacterID) != characterID {
		return LinkedSubQuestActivationPersistResult{}, ErrQuestPersistVerify
	}
	result := LinkedSubQuestActivationPersistResult{
		CharacterID:   characterID,
		Activations:   append([]LinkedSubQuestActivation(nil), plan.Activations...),
		ChangedFields: append([]dnfrepo.QuestField(nil), plan.ChangedFields...),
		Idempotent:    len(plan.ChangedFields) == 0,
	}
	if len(plan.ChangedFields) == 0 {
		return result, nil
	}
	if err := dnfrepo.SaveQuestFields(ctx, o.quests, plan.Record, plan.ChangedFields...); err != nil {
		return LinkedSubQuestActivationPersistResult{}, err
	}
	persisted, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return LinkedSubQuestActivationPersistResult{}, err
	}
	if !ok {
		return LinkedSubQuestActivationPersistResult{}, ErrQuestPersistVerify
	}
	for _, activation := range result.Activations {
		state, known := questStateFor(persisted, activation.QuestID)
		if !known || !isActiveQuestStatus(state.Status) || state.ProgressValue != int64(activation.InitTrigger) ||
			state.Extra["main_quest_id"] != int64Text(activation.ParentID) ||
			state.Extra["auto_activated_by_main_quest"] != "true" {
			return LinkedSubQuestActivationPersistResult{}, ErrQuestPersistVerify
		}
	}
	return result, nil
}
