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
	ErrHuntEnemyTargetRequired         = errors.New("dnf hunt-enemy quest target is required")
	ErrHuntEnemyCompletionKeyRequired  = errors.New("dnf hunt-enemy completion key is required")
	ErrHuntEnemyCompletionTimeRequired = errors.New("dnf hunt-enemy completion time is required")
)

const (
	huntEnemyCompletionKind = "hunt_enemy"
	defaultHuntEnemyType    = int64(1)
	huntEnemyStride         = 5
)

// HuntEnemyKillInput contains only authoritative dungeon-combat facts. It ports
// the 86JP/Quest::CheckKillMonster domain match for PVF [hunt enemy] int-data:
//
//	dungeonId, difficulty, enemyCode, enemyType, count.
//
// dungeonId/difficulty allow -1 wildcards, enemyCode is exact, and missing or
// non-positive enemyType defaults to 1.
//
// Packet shape remains owned by the bridge; this is only PVF/DB state.
type HuntEnemyKillInput struct {
	DungeonID     int64
	Difficulty    int64
	EnemyCode     int64
	EnemyType     int64
	CompletionKey string
	CompletedAt   time.Time
}

// ActiveHuntEnemyTarget is one still-pending PVF hunt-enemy channel. EnemyType
// is the quest-domain type, not the dungeon actor packet type. In 86JP's
// domain table type 3 is a passive object; it must never be inferred from a
// boss actor's wire type.
type ActiveHuntEnemyTarget struct {
	QuestID    int64
	PVFPath    string
	DungeonID  int64
	Difficulty int64
	EnemyCode  int64
	EnemyType  int64
}

// ActiveHuntEnemyTargets returns the exact active PVF targets matching the
// current dungeon scope and quest-domain enemy type. It is intentionally
// read-only and does not fabricate a missing quest row.
func (c *Catalog) ActiveHuntEnemyTargets(
	record dnfrepo.QuestRecord,
	dungeonID int64,
	difficulty int64,
	enemyType int64,
) []ActiveHuntEnemyTarget {
	if c == nil || enemyType <= 0 {
		return nil
	}
	seenQuest := make(map[int64]struct{})
	result := make([]ActiveHuntEnemyTarget, 0, 1)
	visit := func(states map[int64]dnfrepo.QuestState) {
		ids := make([]int64, 0, len(states))
		for questID := range states {
			ids = append(ids, questID)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, questID := range ids {
			if _, duplicate := seenQuest[questID]; duplicate {
				continue
			}
			state := states[questID]
			if !isActiveQuestStatus(state.Status) || state.ProgressValue <= 0 {
				continue
			}
			definition, ok := c.byID[questID]
			if !ok || normalizeQuestTag(definition.Type) != "hunt enemy" {
				continue
			}
			for offset := 0; offset+huntEnemyStride <= len(definition.IntData); offset += huntEnemyStride {
				targetDungeonID := definition.IntData[offset]
				targetDifficulty := definition.IntData[offset+1]
				targetEnemyCode := definition.IntData[offset+2]
				targetEnemyType := definition.IntData[offset+3]
				if targetEnemyType <= 0 {
					targetEnemyType = defaultHuntEnemyType
				}
				if targetEnemyType != enemyType || targetEnemyCode <= 0 ||
					(targetDungeonID != -1 && targetDungeonID != dungeonID) ||
					(targetDifficulty >= 0 && targetDifficulty != difficulty) {
					continue
				}
				result = append(result, ActiveHuntEnemyTarget{
					QuestID: questID, PVFPath: definition.Path,
					DungeonID: targetDungeonID, Difficulty: targetDifficulty,
					EnemyCode: targetEnemyCode, EnemyType: targetEnemyType,
				})
				seenQuest[questID] = struct{}{}
				break
			}
		}
	}
	visit(record.States)
	visit(record.Progress)
	return result
}

type HuntEnemyCompletion struct {
	QuestID         int64
	Field           dnfrepo.QuestField
	PreviousTrigger int64
	CurrentTrigger  int64
	PVFPath         string
	Completed       bool
	Idempotent      bool
}

type HuntEnemyKillPlan struct {
	Record        dnfrepo.QuestRecord
	Completions   []HuntEnemyCompletion
	ChangedFields []dnfrepo.QuestField
}

func (c *Catalog) PlanHuntEnemyKill(record dnfrepo.QuestRecord, input HuntEnemyKillInput) (HuntEnemyKillPlan, error) {
	if c == nil {
		return HuntEnemyKillPlan{}, ErrCatalogEmpty
	}
	if input.EnemyCode <= 0 {
		return HuntEnemyKillPlan{}, ErrHuntEnemyTargetRequired
	}
	input.CompletionKey = strings.TrimSpace(input.CompletionKey)
	if input.CompletionKey == "" {
		return HuntEnemyKillPlan{}, ErrHuntEnemyCompletionKeyRequired
	}
	if input.CompletedAt.IsZero() {
		return HuntEnemyKillPlan{}, ErrHuntEnemyCompletionTimeRequired
	}
	if input.EnemyType <= 0 {
		input.EnemyType = defaultHuntEnemyType
	}

	plan := HuntEnemyKillPlan{Record: dnfrepo.CloneQuest(record)}
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
			if !ok || normalizeQuestTag(definition.Type) != "hunt enemy" {
				continue
			}
			channel, ok := huntEnemyMatchedChannel(definition.IntData, input)
			if !ok {
				continue
			}
			completion := HuntEnemyCompletion{
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
			state.Extra["last_hunt_enemy_code"] = strconv.FormatInt(input.EnemyCode, 10)
			state.Extra["last_hunt_enemy_type"] = strconv.FormatInt(input.EnemyType, 10)
			state.Extra["last_hunt_enemy_dungeon_id"] = strconv.FormatInt(input.DungeonID, 10)
			state.Extra["last_hunt_enemy_difficulty"] = strconv.FormatInt(input.Difficulty, 10)
			if nextTrigger == 0 {
				state.Extra["completion_kind"] = huntEnemyCompletionKind
				state.Extra["completion_key"] = input.CompletionKey
				state.Extra["completion_enemy_code"] = strconv.FormatInt(input.EnemyCode, 10)
				state.Extra["completion_enemy_type"] = strconv.FormatInt(input.EnemyType, 10)
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

func huntEnemyMatchedChannel(values []int64, input HuntEnemyKillInput) (int, bool) {
	for index, offset := 0, 0; offset+huntEnemyStride <= len(values) && index < 3; index, offset = index+1, offset+huntEnemyStride {
		dungeonID := values[offset]
		difficulty := values[offset+1]
		enemyCode := values[offset+2]
		enemyType := values[offset+3]
		if enemyType <= 0 {
			enemyType = defaultHuntEnemyType
		}
		if dungeonID != -1 && dungeonID != input.DungeonID {
			continue
		}
		if difficulty >= 0 && difficulty != input.Difficulty {
			continue
		}
		if enemyCode != input.EnemyCode {
			continue
		}
		if enemyType != input.EnemyType {
			continue
		}
		return index, true
	}
	return 0, false
}

func decrementPackedTriggerChannel(trigger uint32, channel int) uint32 {
	if channel < 0 || channel > 2 {
		return trigger
	}
	shift := channel * 9
	current := (trigger >> shift) & 0x1ff
	if current == 0 {
		return trigger
	}
	next := current - 1
	trigger &^= 0x1ff << shift
	trigger |= next << shift
	return trigger
}

func (p HuntEnemyKillPlan) ValidateCharacter(characterID string) error {
	if strings.TrimSpace(p.Record.CharacterID) != strings.TrimSpace(characterID) {
		return fmt.Errorf("dnf hunt-enemy quest owner mismatch: record=%q selected=%q", p.Record.CharacterID, characterID)
	}
	return nil
}
