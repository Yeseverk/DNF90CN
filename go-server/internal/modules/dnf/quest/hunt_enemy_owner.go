package quest

import (
	"context"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type HuntEnemyPersistResult struct {
	CharacterID   string
	Completions   []HuntEnemyCompletion
	ChangedFields []dnfrepo.QuestField
	Idempotent    bool
}

// ApplyHuntEnemyKill persists only the active quest trigger update for a real
// dungeon kill. Rewards stay behind the normal FinishQuest settlement owner.
func (o *Owner) ApplyHuntEnemyKill(
	ctx context.Context,
	catalog *Catalog,
	characterID string,
	input HuntEnemyKillInput,
) (HuntEnemyPersistResult, error) {
	if o == nil || o.characters == nil || o.quests == nil {
		return HuntEnemyPersistResult{}, ErrOwnerUnavailable
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return HuntEnemyPersistResult{}, ErrCharacterRequired
	}
	if _, ok, err := o.characters.Load(ctx, characterID); err != nil {
		return HuntEnemyPersistResult{}, err
	} else if !ok {
		return HuntEnemyPersistResult{}, ErrCharacterNotFound
	}
	record, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return HuntEnemyPersistResult{}, err
	}
	if !ok {
		record = dnfrepo.QuestRecord{CharacterID: characterID}
	} else {
		record = dnfrepo.CloneQuest(record)
	}
	if strings.TrimSpace(record.CharacterID) == "" {
		record.CharacterID = characterID
	}
	plan, err := catalog.PlanHuntEnemyKill(record, input)
	if err != nil {
		return HuntEnemyPersistResult{}, err
	}
	if err := plan.ValidateCharacter(characterID); err != nil {
		return HuntEnemyPersistResult{}, err
	}
	result := HuntEnemyPersistResult{
		CharacterID:   characterID,
		Completions:   append([]HuntEnemyCompletion(nil), plan.Completions...),
		ChangedFields: append([]dnfrepo.QuestField(nil), plan.ChangedFields...),
		Idempotent:    len(plan.Completions) > 0 && len(plan.ChangedFields) == 0,
	}
	if len(plan.ChangedFields) == 0 {
		return result, nil
	}
	if err := dnfrepo.SaveQuestFields(ctx, o.quests, plan.Record, plan.ChangedFields...); err != nil {
		return HuntEnemyPersistResult{}, err
	}
	persisted, ok, err := o.quests.Load(ctx, characterID)
	if err != nil {
		return HuntEnemyPersistResult{}, err
	}
	if !ok {
		return HuntEnemyPersistResult{}, ErrQuestPersistVerify
	}
	for _, completion := range result.Completions {
		state, known := questStateFor(persisted, completion.QuestID)
		if !known || (!isActiveQuestStatus(state.Status) && !isCompletedQuestStatus(state.Status)) ||
			state.ProgressValue != completion.CurrentTrigger {
			return HuntEnemyPersistResult{}, ErrQuestPersistVerify
		}
		if completion.Completed && !completion.Idempotent &&
			(state.Extra["completion_key"] != input.CompletionKey || state.Extra["reward_state"] != clearMapRewardPending) {
			if !isCompletedQuestStatus(state.Status) || state.Extra["reward_state"] != finishRewardGranted ||
				state.Extra["completion_key"] != input.CompletionKey {
				return HuntEnemyPersistResult{}, ErrQuestPersistVerify
			}
		}
	}
	return result, nil
}
