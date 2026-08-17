package quest

import (
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const legacySaturatedTrigger = int64(0x1ff)

type LegacyActiveTriggerRepair struct {
	QuestID         int64
	Field           dnfrepo.QuestField
	PreviousTrigger int64
	CurrentTrigger  int64
	PVFPath         string
}

type LegacyActiveTriggerRepairPlan struct {
	Record        dnfrepo.QuestRecord
	Repairs       []LegacyActiveTriggerRepair
	ChangedFields []dnfrepo.QuestField
}

// PlanLegacySaturatedActiveTriggerRepair repairs one historical persistence
// shape: an already-cleared hunt target was stored as the signed -1 sentinel
// masked into its nine-bit channel (0x1ff). The current client interprets the
// channel as an unsigned remaining count, so it displays required-511. Clear
// that sentinel to zero; do not reset the quest to its PVF initial counts.
//
// It also repairs the same corruption after a few additional decrements. A
// polluted channel may no longer be exactly 0x1ff, for example 505 while PVF
// requires only one kill. The current client displays that as 1-505 = -504.
// Clamp only the oversaturated channel(s) to zero and preserve ordinary
// remaining channels such as an unfinished third target.
func (c *Catalog) PlanLegacySaturatedActiveTriggerRepair(record dnfrepo.QuestRecord, repairedAt time.Time) (LegacyActiveTriggerRepairPlan, error) {
	if c == nil {
		return LegacyActiveTriggerRepairPlan{}, ErrCatalogEmpty
	}

	plan := LegacyActiveTriggerRepairPlan{Record: dnfrepo.CloneQuest(record)}
	changed := make(map[dnfrepo.QuestField]bool, 2)
	repair := func(states map[int64]dnfrepo.QuestState, field dnfrepo.QuestField) {
		for questID, state := range states {
			if !strings.EqualFold(strings.TrimSpace(state.Status), "active") {
				continue
			}
			definition, ok := c.byID[questID]
			if !ok {
				continue
			}
			tag := normalizeQuestTag(definition.Type)
			if tag != "hunt monster" && tag != "hunt enemy" {
				continue
			}
			expected := definitionInitialTrigger(definition, nil)
			if !isLegacySaturatedMultiTargetTrigger(expected) {
				continue
			}
			previous := state.ProgressValue
			current := previous
			repairKind := ""
			expectedText := "0"
			if state.ProgressValue == legacySaturatedTrigger || isPriorIncorrectLegacyTriggerRepair(state, expected) {
				current = 0
				repairKind = "pvf_multitarget_completed_sentinel_0x1ff_to_zero"
			} else if state.ProgressValue >= 0 && state.ProgressValue <= int64(^uint32(0)) {
				if repaired, changed := repairOversaturatedPackedTriggerChannels(uint32(state.ProgressValue), expected); changed {
					current = int64(repaired)
					repairKind = "pvf_multitarget_oversaturated_channel_to_zero"
					expectedText = strconv.FormatInt(current, 10)
				}
			}
			if repairKind == "" {
				continue
			}

			state.ProgressValue = current
			if !repairedAt.IsZero() {
				state.UpdatedAt = repairedAt
			}
			if state.Extra == nil {
				state.Extra = make(map[string]string, 5)
			}
			state.Extra["legacy_trigger_repair_kind"] = repairKind
			state.Extra["legacy_trigger_repair_previous"] = strconv.FormatInt(previous, 10)
			state.Extra["legacy_trigger_repair_expected"] = expectedText
			state.Extra["legacy_trigger_pvf_initial"] = strconv.FormatUint(uint64(expected), 10)
			state.Extra["pvf_path"] = definition.Path
			state.Extra["quest_type"] = definition.Type
			states[questID] = state
			changed[field] = true
			plan.Repairs = append(plan.Repairs, LegacyActiveTriggerRepair{
				QuestID:         questID,
				Field:           field,
				PreviousTrigger: previous,
				CurrentTrigger:  current,
				PVFPath:         definition.Path,
			})
		}
	}

	repair(plan.Record.States, dnfrepo.QuestFieldStates)
	repair(plan.Record.Progress, dnfrepo.QuestFieldProgress)
	for _, field := range []dnfrepo.QuestField{dnfrepo.QuestFieldStates, dnfrepo.QuestFieldProgress} {
		if changed[field] {
			plan.ChangedFields = append(plan.ChangedFields, field)
		}
	}
	if len(plan.ChangedFields) > 0 && !repairedAt.IsZero() {
		plan.Record.UpdatedAt = repairedAt
	}
	return plan, nil
}

func isPriorIncorrectLegacyTriggerRepair(state dnfrepo.QuestState, expected uint32) bool {
	return state.ProgressValue == int64(expected) &&
		state.Extra["legacy_trigger_repair_kind"] == "pvf_multitarget_saturated_0x1ff" &&
		state.Extra["legacy_trigger_repair_previous"] == strconv.FormatInt(legacySaturatedTrigger, 10)
}

func isLegacySaturatedMultiTargetTrigger(expected uint32) bool {
	channel0 := expected & 0x1ff
	channel1 := (expected >> 9) & 0x1ff
	channel2 := (expected >> 18) & 0x1ff
	return channel0 > 0 && channel0 < uint32(legacySaturatedTrigger) && (channel1 > 0 || channel2 > 0)
}

func repairOversaturatedPackedTriggerChannels(current uint32, expected uint32) (uint32, bool) {
	repaired := current
	changed := false
	for channel := 0; channel < 3; channel++ {
		shift := channel * 9
		want := (expected >> shift) & 0x1ff
		if want == 0 || want >= uint32(legacySaturatedTrigger) {
			continue
		}
		got := (repaired >> shift) & 0x1ff
		if got <= want {
			continue
		}
		repaired &^= 0x1ff << shift
		changed = true
	}
	return repaired, changed
}
