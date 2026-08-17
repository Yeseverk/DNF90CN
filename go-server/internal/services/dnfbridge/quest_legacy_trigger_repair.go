package dnfbridge

import (
	"context"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) reconcileLegacySaturatedActiveQuestTriggers(
	ctx context.Context,
	session *gameSession,
	repository dnfrepo.QuestRepository,
	record dnfrepo.QuestRecord,
) dnfrepo.QuestRecord {
	if s == nil || repository == nil {
		return record
	}
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		s.logGameEvent(session, "game-current-active-quest-trigger-repair-skipped",
			"character_id", record.CharacterID,
			"reason", "quest_catalog_unavailable",
			"error", err)
		return record
	}
	plan, err := catalog.PlanLegacySaturatedActiveTriggerRepair(record, time.Now().UTC())
	if err != nil || len(plan.ChangedFields) == 0 {
		return record
	}
	if err := dnfrepo.SaveQuestFields(ctx, repository, plan.Record, plan.ChangedFields...); err != nil {
		s.logGameEvent(session, "game-current-active-quest-trigger-repair-skipped",
			"character_id", record.CharacterID,
			"repair_count", len(plan.Repairs),
			"reason", "quest_state_save_failed",
			"error", err)
		return record
	}
	questIDs := make([]int64, 0, len(plan.Repairs))
	for _, repair := range plan.Repairs {
		questIDs = append(questIDs, repair.QuestID)
	}
	s.logGameEvent(session, "game-current-active-quest-trigger-repaired",
		"character_id", record.CharacterID,
		"repair_count", len(plan.Repairs),
		"quest_ids", questIDs,
		"changed_fields", plan.ChangedFields,
		"reason", "runtime_pvf_multitarget_cleared_legacy_completed_sentinel_0x1ff")
	return plan.Record
}
