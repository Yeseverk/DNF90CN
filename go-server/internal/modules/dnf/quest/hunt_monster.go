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
	ErrHuntMonsterTargetRequired         = errors.New("dnf hunt-monster quest target is required")
	ErrHuntMonsterCompletionKeyRequired  = errors.New("dnf hunt-monster completion key is required")
	ErrHuntMonsterCompletionTimeRequired = errors.New("dnf hunt-monster completion time is required")
)

const (
	huntMonsterCompletionKind = "hunt_monster"
	huntMonsterStride         = 4
)

// HuntMonsterKillInput contains the real dungeon-combat facts required by PVF
// [hunt monster] quests. The PVF int-data shape is:
//
//	dungeonId, difficulty, monsterCode, count.
//
// dungeonId/difficulty allow the same wildcards as the 86JP domain logic:
// dungeonId -1 means any dungeon, difficulty < 0 means any difficulty.
type HuntMonsterKillInput struct {
	DungeonID     int64
	Difficulty    int64
	MonsterCode   int64
	CompletionKey string
	CompletedAt   time.Time
}

type HuntMonsterCompletion struct {
	QuestID         int64
	Field           dnfrepo.QuestField
	PreviousTrigger int64
	CurrentTrigger  int64
	PVFPath         string
	Completed       bool
	Idempotent      bool
}

type HuntMonsterKillPlan struct {
	Record        dnfrepo.QuestRecord
	Completions   []HuntMonsterCompletion
	ChangedFields []dnfrepo.QuestField
}

func (c *Catalog) PlanHuntMonsterKill(record dnfrepo.QuestRecord, input HuntMonsterKillInput) (HuntMonsterKillPlan, error) {
	if c == nil {
		return HuntMonsterKillPlan{}, ErrCatalogEmpty
	}
	if input.MonsterCode <= 0 {
		return HuntMonsterKillPlan{}, ErrHuntMonsterTargetRequired
	}
	input.CompletionKey = strings.TrimSpace(input.CompletionKey)
	if input.CompletionKey == "" {
		return HuntMonsterKillPlan{}, ErrHuntMonsterCompletionKeyRequired
	}
	if input.CompletedAt.IsZero() {
		return HuntMonsterKillPlan{}, ErrHuntMonsterCompletionTimeRequired
	}

	plan := HuntMonsterKillPlan{Record: dnfrepo.CloneQuest(record)}
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
			if !isActiveQuestStatus(state.Status) || state.ProgressValue < 0 {
				continue
			}
			definition, ok := c.byID[questID]
			if !ok || normalizeQuestTag(definition.Type) != "hunt monster" {
				continue
			}
			channel, ok := huntMonsterMatchedChannel(definition.IntData, input)
			if !ok {
				continue
			}
			completion := HuntMonsterCompletion{
				QuestID:         questID,
				Field:           field,
				PreviousTrigger: state.ProgressValue,
				PVFPath:         definition.Path,
			}
			if state.ProgressValue == 0 {
				completion.CurrentTrigger = 0
				completion.Completed = true
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
			nextTrigger := decrementPackedTriggerChannel(uint32(state.ProgressValue), channel)
			if int64(nextTrigger) == state.ProgressValue {
				continue
			}
			state.ProgressValue = int64(nextTrigger)
			state.UpdatedAt = input.CompletedAt
			completion.CurrentTrigger = int64(nextTrigger)
			completion.Completed = nextTrigger == 0
			if state.Extra == nil {
				state.Extra = make(map[string]string, 8)
			}
			state.Extra["pvf_path"] = definition.Path
			state.Extra["quest_type"] = definition.Type
			state.Extra["last_hunt_monster_code"] = strconv.FormatInt(input.MonsterCode, 10)
			state.Extra["last_hunt_monster_dungeon_id"] = strconv.FormatInt(input.DungeonID, 10)
			state.Extra["last_hunt_monster_difficulty"] = strconv.FormatInt(input.Difficulty, 10)
			if nextTrigger == 0 {
				state.Extra["completion_kind"] = huntMonsterCompletionKind
				state.Extra["completion_key"] = input.CompletionKey
				state.Extra["completion_monster_code"] = strconv.FormatInt(input.MonsterCode, 10)
				state.Extra["completion_dungeon_id"] = strconv.FormatInt(input.DungeonID, 10)
				state.Extra["completion_difficulty"] = strconv.FormatInt(input.Difficulty, 10)
				state.Extra["reward_state"] = clearMapRewardPending
				autoCompleteNoRewardSubQuestState(&state, definition, input.CompletedAt)
			}
			plan.Completions = append(plan.Completions, completion)
			states[questID] = state
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

func huntMonsterMatchedChannel(values []int64, input HuntMonsterKillInput) (int, bool) {
	for index, offset := 0, 0; offset+huntMonsterStride <= len(values) && index < 3; index, offset = index+1, offset+huntMonsterStride {
		dungeonID := values[offset]
		difficulty := values[offset+1]
		monsterCode := values[offset+2]
		if dungeonID != -1 && dungeonID != input.DungeonID {
			continue
		}
		if difficulty >= 0 && difficulty != input.Difficulty {
			continue
		}
		if monsterCode != input.MonsterCode {
			continue
		}
		return index, true
	}
	return 0, false
}

func (p HuntMonsterKillPlan) ValidateCharacter(characterID string) error {
	if strings.TrimSpace(p.Record.CharacterID) != strings.TrimSpace(characterID) {
		return fmt.Errorf("dnf hunt-monster quest owner mismatch: record=%q selected=%q", p.Record.CharacterID, characterID)
	}
	return nil
}
