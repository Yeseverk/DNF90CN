package quest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/jobmap"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrAutoCompleteMainUnavailable = errors.New("main quest auto-complete is unavailable")
	ErrAutoCompleteLevelInvalid    = errors.New("main quest auto-complete level is invalid")
	ErrAutoCompleteTargetInvalid   = errors.New("main quest auto-complete target is invalid")
	ErrAutoCompleteTargetAmbiguous = errors.New("main quest auto-complete target is ambiguous")
)

type AutoCompleteMainInput struct {
	CharacterID   string
	Eligibility   CharacterEligibility
	CutoffLevel   int
	TargetQuestID int64
	CompletedAt   time.Time
}

type AutoCompleteMainPlan struct {
	Record                dnfrepo.QuestRecord
	EligibleQuestIDs      []int64
	ChangedQuestIDs       []int64
	ChangedLinkedSubIDs   []int64
	ActiveCompletedCount  int
	HighestCompletedQuest int64
	ChangedFields         []dnfrepo.QuestField
}

type AutoCompleteMainResult struct {
	CharacterID           string
	CutoffLevel           int
	EligibleQuestIDs      []int64
	ChangedQuestIDs       []int64
	ChangedLinkedSubIDs   []int64
	ActiveCompletedCount  int
	HighestCompletedQuest int64
	PostCommitQuest       dnfrepo.QuestRecord
	Idempotent            bool
}

// PlanAutoCompleteMainQuests closes an explicitly selected, currently active
// PVF [epic] quest below the requested level. The task-manual's current-EXE
// op1429 request carries a zero selector, which is the documented bulk action:
// it closes every eligible active epic rather than identifying a UI row. Linked
// subquests and every unrelated quest remain untouched. This planner grants no
// reward and mutates no character, inventory, skill, or currency aggregate.
func (c *Catalog) PlanAutoCompleteMainQuests(
	record dnfrepo.QuestRecord,
	character CharacterEligibility,
	cutoffLevel int,
	targetQuestID int64,
	completedAt time.Time,
) (AutoCompleteMainPlan, error) {
	if c == nil {
		return AutoCompleteMainPlan{}, ErrCatalogEmpty
	}
	if cutoffLevel <= 0 || cutoffLevel >= character.Level || !definitionCharacterJobValid(character) {
		return AutoCompleteMainPlan{}, ErrAutoCompleteLevelInvalid
	}
	if completedAt.IsZero() {
		return AutoCompleteMainPlan{}, ErrAutoCompleteMainUnavailable
	}

	plan := AutoCompleteMainPlan{Record: dnfrepo.CloneQuest(record)}
	_, active := questStateSets(plan.Record)

	eligibleEpic := func(definition Definition) bool {
		return normalizeQuestTag(definition.Grade) == "epic" &&
			definition.LevelMin > 0 && definition.LevelMin <= cutoffLevel &&
			definition.ExposedByNPC != 0 && !definition.IsEvent &&
			definitionMatchesCharacter(definition, character)
	}

	candidates := make([]int64, 0, 4)
	for _, definition := range c.ordered {
		if !eligibleEpic(definition) {
			continue
		}
		if _, wasActive := active[definition.ID]; wasActive {
			candidates = append(candidates, definition.ID)
		}
	}
	if targetQuestID == 0 {
		if len(candidates) == 0 {
			return AutoCompleteMainPlan{}, ErrAutoCompleteTargetInvalid
		}
		for _, candidateID := range candidates {
			definition, definitionKnown := c.Find(candidateID)
			state, field, stateKnown := mutableQuestState(&plan.Record, candidateID)
			if !definitionKnown || !eligibleEpic(definition) || !stateKnown || !isActiveQuestStatus(state.Status) {
				return AutoCompleteMainPlan{}, ErrAutoCompleteTargetInvalid
			}
			state = autoCompleteMainQuestState(state, definition, cutoffLevel, completedAt)
			if field == dnfrepo.QuestFieldStates {
				plan.Record.States[candidateID] = state
			} else {
				plan.Record.Progress[candidateID] = state
			}
			plan.EligibleQuestIDs = append(plan.EligibleQuestIDs, candidateID)
			plan.ChangedQuestIDs = append(plan.ChangedQuestIDs, candidateID)
			plan.ChangedFields = mergeQuestChangedField(plan.ChangedFields, field)
			plan.ActiveCompletedCount++
			if candidateID > plan.HighestCompletedQuest {
				plan.HighestCompletedQuest = candidateID
			}
		}
	} else {
		definition, definitionKnown := c.Find(targetQuestID)
		if !definitionKnown || !eligibleEpic(definition) {
			return AutoCompleteMainPlan{}, ErrAutoCompleteTargetInvalid
		}
		state, field, stateKnown := mutableQuestState(&plan.Record, targetQuestID)
		if !stateKnown {
			return AutoCompleteMainPlan{}, ErrAutoCompleteTargetInvalid
		}
		plan.EligibleQuestIDs = []int64{targetQuestID}
		plan.HighestCompletedQuest = targetQuestID
		if isCompletedQuestStatus(state.Status) {
			return plan, nil
		}
		if !isActiveQuestStatus(state.Status) {
			return AutoCompleteMainPlan{}, ErrAutoCompleteTargetInvalid
		}
		plan.ActiveCompletedCount = 1
		state = autoCompleteMainQuestState(state, definition, cutoffLevel, completedAt)
		if field == dnfrepo.QuestFieldStates {
			plan.Record.States[targetQuestID] = state
		} else {
			plan.Record.Progress[targetQuestID] = state
		}
		plan.ChangedFields = mergeQuestChangedField(plan.ChangedFields, field)
		plan.ChangedQuestIDs = []int64{targetQuestID}
	}
	if len(plan.ChangedFields) != 0 {
		plan.Record.UpdatedAt = completedAt
	}
	return plan, nil
}

func (o *Owner) ApplyAutoCompleteMain(
	ctx context.Context,
	catalog *Catalog,
	input AutoCompleteMainInput,
) (AutoCompleteMainResult, error) {
	if o == nil || o.repositories.CharacterSettlement == nil || catalog == nil {
		return AutoCompleteMainResult{}, ErrAutoCompleteMainUnavailable
	}
	input.CharacterID = strings.TrimSpace(input.CharacterID)
	if input.CharacterID == "" || input.CompletedAt.IsZero() {
		return AutoCompleteMainResult{}, ErrCharacterRequired
	}
	if input.CutoffLevel <= 0 || input.CutoffLevel >= input.Eligibility.Level {
		return AutoCompleteMainResult{}, ErrAutoCompleteLevelInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var plan AutoCompleteMainPlan
	err := o.repositories.CharacterSettlement.WithinCharacterSettlement(ctx, input.CharacterID, func(tx dnfrepo.Group) error {
		character, found, err := tx.Character.Load(ctx, input.CharacterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrCharacterNotFound
		}
		if character.Level != input.Eligibility.Level || input.CutoffLevel >= character.Level {
			return ErrAutoCompleteLevelInvalid
		}
		record, found, err := tx.Quest.Load(ctx, input.CharacterID)
		if err != nil {
			return err
		}
		if !found {
			record = dnfrepo.QuestRecord{CharacterID: input.CharacterID}
		} else if strings.TrimSpace(record.CharacterID) != input.CharacterID {
			return ErrQuestPersistVerify
		}
		plan, err = catalog.PlanAutoCompleteMainQuests(record, input.Eligibility, input.CutoffLevel, input.TargetQuestID, input.CompletedAt)
		if err != nil {
			return err
		}
		plan.Record.CharacterID = input.CharacterID
		if len(plan.ChangedFields) == 0 {
			return nil
		}
		return dnfrepo.SaveQuestFields(ctx, tx.Quest, plan.Record, plan.ChangedFields...)
	})
	if err != nil {
		return AutoCompleteMainResult{}, err
	}

	persisted, found, err := o.quests.Load(ctx, input.CharacterID)
	if err != nil {
		return AutoCompleteMainResult{}, err
	}
	if !found {
		return AutoCompleteMainResult{}, ErrQuestPersistVerify
	}
	for _, questID := range append(append([]int64(nil), plan.EligibleQuestIDs...), plan.ChangedLinkedSubIDs...) {
		state, known := questStateFor(persisted, questID)
		if !known || !isCompletedQuestStatus(state.Status) {
			return AutoCompleteMainResult{}, fmt.Errorf("%w: quest=%d", ErrQuestPersistVerify, questID)
		}
	}
	return AutoCompleteMainResult{
		CharacterID:           input.CharacterID,
		CutoffLevel:           input.CutoffLevel,
		EligibleQuestIDs:      append([]int64(nil), plan.EligibleQuestIDs...),
		ChangedQuestIDs:       append([]int64(nil), plan.ChangedQuestIDs...),
		ChangedLinkedSubIDs:   append([]int64(nil), plan.ChangedLinkedSubIDs...),
		ActiveCompletedCount:  plan.ActiveCompletedCount,
		HighestCompletedQuest: plan.HighestCompletedQuest,
		PostCommitQuest:       dnfrepo.CloneQuest(persisted),
		Idempotent:            len(plan.ChangedFields) == 0,
	}, nil
}

func definitionCharacterJobValid(character CharacterEligibility) bool {
	return character.Level > 0 && jobmap.Valid(character.Job)
}

func sortedQuestIDSet(values map[int64]struct{}) []int64 {
	ids := make([]int64, 0, len(values))
	for questID := range values {
		ids = append(ids, questID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func autoCompleteMainQuestState(state dnfrepo.QuestState, definition Definition, cutoffLevel int, completedAt time.Time) dnfrepo.QuestState {
	state.Status = "completed"
	state.ProgressValue = 0
	state.RewardSelectIndex = 0
	state.UpdatedAt = completedAt
	if state.Extra == nil {
		state.Extra = make(map[string]string, 8)
	}
	if state.Extra["pvf_path"] == "" {
		state.Extra["pvf_path"] = definition.Path
	}
	if state.Extra["quest_type"] == "" {
		state.Extra["quest_type"] = definition.Type
	}
	state.Extra["reward_state"] = finishRewardGranted
	state.Extra["auto_completed"] = "true"
	state.Extra["auto_complete_reason"] = "epic_below_character_level_no_reward"
	state.Extra["auto_complete_level_cutoff"] = strconv.Itoa(cutoffLevel)
	return state
}

func shouldAutoCompleteNoRewardSubQuest(definition Definition) bool {
	rewardType := normalizeQuestTag(definition.RewardType)
	legacyEmptyReward := rewardType == ""
	zeroItemPlaceholder := definition.NoExperience && rewardType == "item" &&
		len(definition.RewardIntData) == 2 &&
		definition.RewardIntData[0] == 0 && definition.RewardIntData[1] == 0
	return normalizeQuestTag(definition.Grade) == "sub" &&
		(legacyEmptyReward || zeroItemPlaceholder) && definition.RewardParseError == "" &&
		!definition.HasGoldReward && emptyRewardDataValid(definition.RewardIntData) &&
		len(definition.RewardItems) == 0 && len(definition.RewardSelectionItems) == 0
}

func autoCompleteNoRewardSubQuestState(state *dnfrepo.QuestState, definition Definition, completedAt time.Time) bool {
	if state == nil || !shouldAutoCompleteNoRewardSubQuest(definition) {
		return false
	}
	state.Status = "completed"
	state.ProgressValue = 0
	state.UpdatedAt = completedAt
	if state.Extra == nil {
		state.Extra = make(map[string]string, 8)
	}
	state.Extra["reward_state"] = finishRewardGranted
	state.Extra["auto_completed"] = "true"
	state.Extra["auto_complete_reason"] = "no_reward_sub_quest"
	return true
}

func mergeQuestChangedField(current []dnfrepo.QuestField, field dnfrepo.QuestField) []dnfrepo.QuestField {
	if field == "" {
		return current
	}
	for _, existing := range current {
		if existing == field {
			return current
		}
	}
	return append(current, field)
}

type LinkedSubQuestActivation struct {
	ParentID       int64
	QuestID        int64
	Field          dnfrepo.QuestField
	InitTrigger    uint32
	PVFPath        string
	QuestType      string
	AlreadyPresent bool
}

type LinkedSubQuestActivationPlan struct {
	Record        dnfrepo.QuestRecord
	Activations   []LinkedSubQuestActivation
	ChangedFields []dnfrepo.QuestField
}

func (c *Catalog) PlanActiveQuestClearLinkedSubQuestActivation(record dnfrepo.QuestRecord, activatedAt time.Time) (LinkedSubQuestActivationPlan, error) {
	if c == nil {
		return LinkedSubQuestActivationPlan{}, ErrCatalogEmpty
	}
	if activatedAt.IsZero() {
		activatedAt = time.Now()
	}
	plan := LinkedSubQuestActivationPlan{Record: dnfrepo.CloneQuest(record)}
	plan.Activations, plan.ChangedFields = c.activateMissingLinkedNoRewardSubQuests(&plan.Record, activatedAt, "")
	if len(plan.ChangedFields) > 0 {
		plan.Record.UpdatedAt = activatedAt
	}
	return plan, nil
}

func (c *Catalog) activateMissingLinkedNoRewardSubQuests(
	record *dnfrepo.QuestRecord,
	activatedAt time.Time,
	childType string,
) ([]LinkedSubQuestActivation, []dnfrepo.QuestField) {
	if c == nil {
		return nil, nil
	}
	childType = normalizeQuestTag(childType)
	completed, _ := questStateSets(*record)
	activations := make([]LinkedSubQuestActivation, 0)
	changedFields := make([]dnfrepo.QuestField, 0, 2)
	seenChildren := make(map[int64]struct{})
	collect := func(field dnfrepo.QuestField, states map[int64]dnfrepo.QuestState) {
		if len(states) == 0 {
			return
		}
		parentIDs := make([]int64, 0, len(states))
		for questID := range states {
			parentIDs = append(parentIDs, questID)
		}
		sort.Slice(parentIDs, func(left, right int) bool { return parentIDs[left] < parentIDs[right] })
		for _, parentID := range parentIDs {
			parentState := states[parentID]
			if !isActiveQuestStatus(parentState.Status) {
				continue
			}
			parent, known := c.Find(parentID)
			if !known {
				continue
			}
			parentType := normalizeQuestTag(parent.Type)
			if parentType != "quest clear" && parentType != "clear quest" {
				continue
			}
			for _, childID := range parent.IntData {
				if childID <= 0 {
					continue
				}
				if _, duplicate := seenChildren[childID]; duplicate {
					continue
				}
				if _, known := questStateFor(*record, childID); known {
					continue
				}
				child, known := c.Find(childID)
				if !known || child.MainQuestID != parent.ID || child.HasDependGiveItem ||
					(childType != "" && normalizeQuestTag(child.Type) != childType) ||
					!shouldAutoCompleteNoRewardSubQuest(child) {
					continue
				}
				seenChildren[childID] = struct{}{}
				initialTrigger := definitionInitialTrigger(child, completed)
				state := dnfrepo.QuestState{
					Status:        "active",
					ProgressValue: int64(initialTrigger),
					UpdatedAt:     activatedAt,
					Extra: map[string]string{
						"pvf_path":                              child.Path,
						"quest_type":                            child.Type,
						"main_quest_id":                         int64Text(parent.ID),
						"auto_activated_by_main_quest":          "true",
						"auto_activation_reason":                "active_quest_clear_parent_reconcile",
						"auto_activation_parent_progress_value": int64Text(parentState.ProgressValue),
					},
				}
				switch field {
				case dnfrepo.QuestFieldStates:
					if record.States == nil {
						record.States = make(map[int64]dnfrepo.QuestState, 1)
					}
					record.States[child.ID] = state
					changedFields = mergeQuestChangedField(changedFields, dnfrepo.QuestFieldStates)
				case dnfrepo.QuestFieldProgress:
					if record.Progress == nil {
						record.Progress = make(map[int64]dnfrepo.QuestState, 1)
					}
					record.Progress[child.ID] = state
					changedFields = mergeQuestChangedField(changedFields, dnfrepo.QuestFieldProgress)
				}
				activations = append(activations, LinkedSubQuestActivation{
					ParentID:    parent.ID,
					QuestID:     child.ID,
					Field:       field,
					InitTrigger: initialTrigger,
					PVFPath:     child.Path,
					QuestType:   child.Type,
				})
			}
		}
	}
	collect(dnfrepo.QuestFieldStates, record.States)
	collect(dnfrepo.QuestFieldProgress, record.Progress)
	return activations, changedFields
}

func int64Text(value int64) string {
	return strconv.FormatInt(value, 10)
}
