package quest

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrClearMapTargetRequired         = errors.New("dnf clear-map dungeon or map target is required")
	ErrClearMapCompletionKeyRequired  = errors.New("dnf clear-map completion key is required")
	ErrClearMapCompletionTimeRequired = errors.New("dnf clear-map completion time is required")
)

const (
	clearMapRewardPending  = "pending"
	clearMapCompletionKind = "clear_map"
)

// ClearMapCompletionInput contains only facts owned by an authoritative final
// dungeon completion. CompletionKey must be stable for replays of that run.
type ClearMapCompletionInput struct {
	DungeonID     int64
	MapID         int64
	CompletionKey string
	CompletedAt   time.Time
}

type ClearMapCompletion struct {
	QuestID         int64
	Field           dnfrepo.QuestField
	PreviousTrigger int64
	PVFPath         string
	Idempotent      bool
}

// ClearMapCompletionPlan is a repository-independent mutation plan. Record is
// a deep clone; no caller-owned map is modified.
type ClearMapCompletionPlan struct {
	Record        dnfrepo.QuestRecord
	Completions   []ClearMapCompletion
	ChangedFields []dnfrepo.QuestField
}

// PlanClearMapCompletion changes matching active clear-map triggers to zero,
// which is the quest-complete/pending-reward state used by the existing quest
// snapshot. It does not mark the quest rewarded and does not grant assets or
// experience. Those mutations require a wider Character+Quest+Skill+Asset unit
// of work.
func (c *Catalog) PlanClearMapCompletion(record dnfrepo.QuestRecord, input ClearMapCompletionInput) (ClearMapCompletionPlan, error) {
	if c == nil {
		return ClearMapCompletionPlan{}, ErrCatalogEmpty
	}
	if input.DungeonID <= 0 && input.MapID <= 0 {
		return ClearMapCompletionPlan{}, ErrClearMapTargetRequired
	}
	input.CompletionKey = strings.TrimSpace(input.CompletionKey)
	if input.CompletionKey == "" {
		return ClearMapCompletionPlan{}, ErrClearMapCompletionKeyRequired
	}
	if input.CompletedAt.IsZero() {
		return ClearMapCompletionPlan{}, ErrClearMapCompletionTimeRequired
	}

	plan := ClearMapCompletionPlan{Record: dnfrepo.CloneQuest(record)}
	statesChanged := false
	progressChanged := false
	_, activationFields := c.activateMissingLinkedNoRewardSubQuests(&plan.Record, input.CompletedAt, "")
	for _, field := range activationFields {
		switch field {
		case dnfrepo.QuestFieldStates:
			statesChanged = true
		case dnfrepo.QuestFieldProgress:
			progressChanged = true
		}
	}
	seen := make(map[int64]struct{})
	visit := func(states map[int64]dnfrepo.QuestState, field dnfrepo.QuestField) {
		ids := make([]int64, 0, len(states))
		for questID := range states {
			ids = append(ids, questID)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, questID := range ids {
			if _, duplicate := seen[questID]; duplicate {
				continue
			}
			seen[questID] = struct{}{}
			state := states[questID]
			if !isActiveQuestStatus(state.Status) {
				continue
			}
			definition, ok := c.byID[questID]
			if !ok || normalizeQuestTag(definition.Type) != "clear map" || !clearMapTargetMatches(definition.IntData, input.DungeonID, input.MapID) {
				continue
			}
			completion := ClearMapCompletion{
				QuestID:         questID,
				Field:           field,
				PreviousTrigger: state.ProgressValue,
				PVFPath:         definition.Path,
			}
			if state.ProgressValue < 0 {
				// Repository corruption is not a completed clear-map trigger and
				// must never be normalized into a rewardable state here.
				continue
			}
			if state.ProgressValue == 0 {
				completion.Idempotent = true
				if autoCompleteNoRewardSubQuestState(&state, definition, input.CompletedAt) {
					states[questID] = state
					switch field {
					case dnfrepo.QuestFieldStates:
						statesChanged = true
					case dnfrepo.QuestFieldProgress:
						progressChanged = true
					}
				}
				plan.Completions = append(plan.Completions, completion)
				continue
			}
			state.ProgressValue = 0
			state.UpdatedAt = input.CompletedAt
			if state.Extra == nil {
				state.Extra = make(map[string]string, 8)
			}
			state.Extra["completion_kind"] = clearMapCompletionKind
			state.Extra["completion_key"] = input.CompletionKey
			state.Extra["completion_dungeon_id"] = strconv.FormatInt(input.DungeonID, 10)
			state.Extra["completion_map_id"] = strconv.FormatInt(input.MapID, 10)
			state.Extra["reward_state"] = clearMapRewardPending
			state.Extra["pvf_path"] = definition.Path
			state.Extra["quest_type"] = definition.Type
			autoCompleteNoRewardSubQuestState(&state, definition, input.CompletedAt)
			states[questID] = state
			plan.Completions = append(plan.Completions, completion)
			switch field {
			case dnfrepo.QuestFieldStates:
				statesChanged = true
			case dnfrepo.QuestFieldProgress:
				progressChanged = true
			}
		}
	}
	visit(plan.Record.States, dnfrepo.QuestFieldStates)
	visit(plan.Record.Progress, dnfrepo.QuestFieldProgress)
	for _, field := range syncActiveQuestClearParentProgress(c, &plan.Record, input.CompletedAt) {
		switch field {
		case dnfrepo.QuestFieldStates:
			statesChanged = true
		case dnfrepo.QuestFieldProgress:
			progressChanged = true
		}
	}
	if statesChanged || progressChanged {
		plan.Record.UpdatedAt = input.CompletedAt
	}
	if statesChanged {
		plan.ChangedFields = append(plan.ChangedFields, dnfrepo.QuestFieldStates)
	}
	if progressChanged {
		plan.ChangedFields = append(plan.ChangedFields, dnfrepo.QuestFieldProgress)
	}
	return plan, nil
}

func clearMapTargetMatches(values []int64, dungeonID, mapID int64) bool {
	for _, target := range values {
		if target <= 0 {
			continue
		}
		if dungeonID > 0 && target == dungeonID {
			return true
		}
		if mapID > 0 && target == mapID {
			return true
		}
	}
	return false
}

func (p ClearMapCompletionPlan) ValidateCharacter(characterID string) error {
	if strings.TrimSpace(p.Record.CharacterID) != strings.TrimSpace(characterID) {
		return fmt.Errorf("dnf clear-map quest owner mismatch: record=%q selected=%q", p.Record.CharacterID, characterID)
	}
	return nil
}
