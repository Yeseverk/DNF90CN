package dnfbridge

import (
	"context"
	"strconv"
	"time"

	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) reconcileActiveQuestClearLinkedSubQuestsForDungeon(
	ctx context.Context,
	session *gameSession,
	source string,
) (bool, error) {
	if session == nil || session.selectedCharacterID == 0 {
		return false, nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || repositories.Quest == nil {
		return false, errDungeonQuestRepositoryUnavailable
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	if _, found, err := repositories.Quest.Load(ctx, characterID); err != nil {
		return false, err
	} else if !found {
		s.logGameEvent(session, "game-dungeon-quest-clear-linked-subquest-reconcile-skipped",
			"char_id", session.selectedCharacterID,
			"source", source,
			"reason", "quest_record_missing",
			"quest_mutation", "none")
		return false, nil
	}
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		return false, err
	}
	owner, err := dnfquest.NewOwner(repositories)
	if err != nil {
		return false, err
	}
	result, err := owner.ApplyActiveQuestClearLinkedSubQuestActivation(ctx, catalog, characterID, time.Now().UTC())
	if err != nil {
		return false, err
	}
	questIDs := make([]int64, 0, len(result.Activations))
	parentIDs := make([]int64, 0, len(result.Activations))
	for _, activation := range result.Activations {
		questIDs = append(questIDs, activation.QuestID)
		parentIDs = append(parentIDs, activation.ParentID)
	}
	s.logGameEvent(session, "game-dungeon-quest-clear-linked-subquest-reconciled",
		"char_id", session.selectedCharacterID,
		"source", source,
		"activation_count", len(result.Activations),
		"quest_ids", questIDs,
		"parent_ids", parentIDs,
		"changed_fields", append([]dnfrepo.QuestField(nil), result.ChangedFields...),
		"idempotent", result.Idempotent,
		"quest_mutation", "real_pvf_no_reward_subquest_active_rows_only")
	return len(result.Activations) > 0, nil
}
