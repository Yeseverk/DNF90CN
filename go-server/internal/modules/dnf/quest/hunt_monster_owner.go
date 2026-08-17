package quest

import (
	"context"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type HuntMonsterPersistResult struct {
	CharacterID   string
	Completions   []HuntMonsterCompletion
	ChangedFields []dnfrepo.QuestField
	Idempotent    bool
}

// ApplyHuntMonsterKill persists only the active trigger change caused by a
// real dungeon monster death. Reward settlement remains behind FinishQuest,
// exactly like the existing hunt-enemy path.
func (o *Owner) ApplyHuntMonsterKill(
	ctx context.Context,
	catalog *Catalog,
	characterID string,
	input HuntMonsterKillInput,
) (HuntMonsterPersistResult, error) {
	if o == nil || o.characters == nil || o.quests == nil {
		return HuntMonsterPersistResult{}, ErrOwnerUnavailable
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return HuntMonsterPersistResult{}, ErrCharacterRequired
	}
	if _, ok, err := o.characters.Load(ctx, characterID); err != nil {
		return HuntMonsterPersistResult{}, err
	} else if !ok {
		return HuntMonsterPersistResult{}, ErrCharacterNotFound
	}
	record, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return HuntMonsterPersistResult{}, err
	}
	if !ok {
		record = dnfrepo.QuestRecord{CharacterID: characterID}
	} else {
		record = dnfrepo.CloneQuest(record)
	}
	if strings.TrimSpace(record.CharacterID) == "" {
		record.CharacterID = characterID
	}
	plan, err := catalog.PlanHuntMonsterKill(record, input)
	if err != nil {
		return HuntMonsterPersistResult{}, err
	}
	if err := plan.ValidateCharacter(characterID); err != nil {
		return HuntMonsterPersistResult{}, err
	}
	result := HuntMonsterPersistResult{
		CharacterID:   characterID,
		Completions:   append([]HuntMonsterCompletion(nil), plan.Completions...),
		ChangedFields: append([]dnfrepo.QuestField(nil), plan.ChangedFields...),
		Idempotent:    len(plan.Completions) > 0 && len(plan.ChangedFields) == 0,
	}
	if len(plan.ChangedFields) == 0 {
		return result, nil
	}
	if err := dnfrepo.SaveQuestFields(ctx, o.quests, plan.Record, plan.ChangedFields...); err != nil {
		return HuntMonsterPersistResult{}, err
	}
	persisted, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return HuntMonsterPersistResult{}, err
	}
	if !ok {
		return HuntMonsterPersistResult{}, ErrQuestPersistVerify
	}
	for _, completion := range result.Completions {
		state, known := questStateFor(persisted, completion.QuestID)
		if !known || (!isActiveQuestStatus(state.Status) && !isCompletedQuestStatus(state.Status)) ||
			state.ProgressValue != completion.CurrentTrigger {
			return HuntMonsterPersistResult{}, ErrQuestPersistVerify
		}
		if completion.Completed && !completion.Idempotent &&
			(state.Extra["completion_key"] != input.CompletionKey || state.Extra["reward_state"] != clearMapRewardPending) {
			if !isCompletedQuestStatus(state.Status) || state.Extra["reward_state"] != finishRewardGranted ||
				state.Extra["completion_key"] != input.CompletionKey {
				return HuntMonsterPersistResult{}, ErrQuestPersistVerify
			}
		}
	}
	return result, nil
}
