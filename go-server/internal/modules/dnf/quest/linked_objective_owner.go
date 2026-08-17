package quest

import (
	"context"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type LinkedObjectiveCommitResult struct {
	CharacterID       string
	RequestedQuestID  int64
	ParentQuestID     int64
	ParentProgress    int64
	CompletedQuestIDs []int64
	ChangedFields     []dnfrepo.QuestField
	PostCommitQuest   dnfrepo.QuestRecord
	Applied           bool
}

func (o *Owner) ApplyTownLinkedProgress(ctx context.Context, catalog *Catalog, characterID string, questID int64, completedAt time.Time) (LinkedObjectiveCommitResult, error) {
	return o.applyLinkedObjectiveProgress(ctx, catalog, characterID, questID, completedAt, true)
}

func (o *Owner) ApplyLinkedObjectiveFinish(ctx context.Context, catalog *Catalog, characterID string, questID int64, completedAt time.Time) (LinkedObjectiveCommitResult, error) {
	return o.applyLinkedObjectiveProgress(ctx, catalog, characterID, questID, completedAt, false)
}

func (o *Owner) applyLinkedObjectiveProgress(
	ctx context.Context,
	catalog *Catalog,
	characterID string,
	questID int64,
	completedAt time.Time,
	townParentCallback bool,
) (LinkedObjectiveCommitResult, error) {
	if o == nil || o.repositories.CharacterSettlement == nil || catalog == nil {
		return LinkedObjectiveCommitResult{}, ErrOwnerUnavailable
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return LinkedObjectiveCommitResult{}, ErrCharacterRequired
	}
	if questID <= 0 {
		return LinkedObjectiveCommitResult{}, ErrQuestIDRequired
	}
	result := LinkedObjectiveCommitResult{CharacterID: characterID, RequestedQuestID: questID}
	err := o.repositories.CharacterSettlement.WithinCharacterSettlement(ctx, characterID, func(tx dnfrepo.Group) error {
		if tx.Quest == nil {
			return ErrOwnerUnavailable
		}
		record, found, err := tx.Quest.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(record.CharacterID) != characterID {
			return nil
		}
		var plan LinkedObjectiveProgressPlan
		if townParentCallback {
			plan, err = catalog.PlanTownLinkedProgress(record, questID, completedAt)
		} else {
			plan, err = catalog.PlanLinkedObjectiveFinish(record, questID, completedAt)
		}
		if err != nil {
			return err
		}
		if len(plan.ChangedFields) == 0 {
			return nil
		}
		if err := dnfrepo.SaveQuestFields(ctx, tx.Quest, plan.Record, plan.ChangedFields...); err != nil {
			return err
		}
		result.ParentQuestID = plan.ParentQuestID
		result.ParentProgress = plan.ParentProgress
		result.CompletedQuestIDs = append([]int64(nil), plan.CompletedQuestIDs...)
		result.ChangedFields = append([]dnfrepo.QuestField(nil), plan.ChangedFields...)
		result.PostCommitQuest = dnfrepo.CloneQuest(plan.Record)
		result.Applied = true
		return nil
	})
	if err != nil {
		return LinkedObjectiveCommitResult{}, err
	}
	return result, nil
}
