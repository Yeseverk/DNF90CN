package quest

import (
	"context"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type ClearMapPersistResult struct {
	CharacterID   string
	Completions   []ClearMapCompletion
	ChangedFields []dnfrepo.QuestField
	Idempotent    bool
}

// ApplyClearMapCompletion persists and verifies the quest-only Phase-A marker.
// It grants no reward. The authoritative op117 owner may continue to op115/op31
// only after this method returns success.
func (o *Owner) ApplyClearMapCompletion(
	ctx context.Context,
	catalog *Catalog,
	characterID string,
	input ClearMapCompletionInput,
) (ClearMapPersistResult, error) {
	if o == nil || o.characters == nil || o.quests == nil {
		return ClearMapPersistResult{}, ErrOwnerUnavailable
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return ClearMapPersistResult{}, ErrCharacterRequired
	}
	if _, ok, err := o.characters.Load(ctx, characterID); err != nil {
		return ClearMapPersistResult{}, err
	} else if !ok {
		return ClearMapPersistResult{}, ErrCharacterNotFound
	}
	record, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return ClearMapPersistResult{}, err
	}
	if !ok {
		record = dnfrepo.QuestRecord{CharacterID: characterID}
	} else {
		record = dnfrepo.CloneQuest(record)
	}
	if strings.TrimSpace(record.CharacterID) == "" {
		record.CharacterID = characterID
	}
	plan, err := catalog.PlanClearMapCompletion(record, input)
	if err != nil {
		return ClearMapPersistResult{}, err
	}
	if err := plan.ValidateCharacter(characterID); err != nil {
		return ClearMapPersistResult{}, err
	}
	result := ClearMapPersistResult{
		CharacterID:   characterID,
		Completions:   append([]ClearMapCompletion(nil), plan.Completions...),
		ChangedFields: append([]dnfrepo.QuestField(nil), plan.ChangedFields...),
		Idempotent:    len(plan.Completions) > 0 && len(plan.ChangedFields) == 0,
	}
	if len(plan.ChangedFields) == 0 {
		return result, nil
	}
	if err := dnfrepo.SaveQuestFields(ctx, o.quests, plan.Record, plan.ChangedFields...); err != nil {
		return ClearMapPersistResult{}, err
	}
	persisted, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return ClearMapPersistResult{}, err
	}
	if !ok {
		return ClearMapPersistResult{}, ErrQuestPersistVerify
	}
	for _, completion := range result.Completions {
		state, known := questStateFor(persisted, completion.QuestID)
		if !known || (!isActiveQuestStatus(state.Status) && !isCompletedQuestStatus(state.Status)) || state.ProgressValue != 0 {
			return ClearMapPersistResult{}, ErrQuestPersistVerify
		}
		if !completion.Idempotent && (state.Extra["completion_key"] != input.CompletionKey || state.Extra["reward_state"] != clearMapRewardPending) {
			if !isCompletedQuestStatus(state.Status) || state.Extra["reward_state"] != finishRewardGranted ||
				state.Extra["completion_key"] != input.CompletionKey {
				return ClearMapPersistResult{}, ErrQuestPersistVerify
			}
		}
	}
	return result, nil
}
