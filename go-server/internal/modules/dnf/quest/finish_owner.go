package quest

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/jobmap"
	"longheng.io/server/internal/modules/dnf/profession"
	"longheng.io/server/internal/modules/dnf/progression"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
)

var (
	ErrFinishSettlementUnavailable      = errors.New("dnf quest finish settlement is unavailable")
	ErrFinishQuestNotPending            = errors.New("dnf quest is not pending a finish reward")
	ErrFinishQuestCompletionConflict    = errors.New("dnf quest finish completion key conflicts with persisted state")
	ErrFinishQuestSettlementStale       = errors.New("dnf quest finish replay receipt is stale")
	ErrFinishQuestSettlementCorrupt     = errors.New("dnf quest finish settlement receipt is corrupt")
	ErrFinishQuestMultiplierUnsupported = errors.New("dnf quest finish multiplier is unsupported")
	ErrFinishQuestCommitTimeRequired    = errors.New("dnf quest finish commit time is required")
	ErrFinishItemAllocatorRequired      = errors.New("dnf quest finish item allocator is required")
	ErrFinishProgressionUnproven        = errors.New("dnf quest finish progression is outside proven PVF thresholds")
	ErrFinishExperienceBonusInvalid     = errors.New("dnf quest finish experience bonus is invalid")
	ErrFinishProfessionUnavailable      = errors.New("dnf quest finish profession resources are unavailable")
	ErrFinishRequiredItemsMissing       = errors.New("dnf quest finish required items are missing")
)

const (
	finishRewardGranted          = "granted"
	finishSettlementReceiptKey   = "settlement_receipt_v1"
	finishSettlementReceiptLevel = 1
)

// FinishItemGrantRequest is a PVF-backed item delta which must be placed into
// the transaction-scoped inventory clone. The bridge supplies the allocator
// because current item category ranges and raw entries are current-EXE/PVF
// concerns, while the quest owner retains the atomic transaction boundary.
type FinishItemGrantRequest struct {
	QuestID       int64
	CompletionKey string
	Source        string
	ItemID        int64
	Count         int64
}

// FinishCommittedItem is the exact post-allocation item receipt. RawEntry is a
// deep copy of the current 0x77 inventory row after the grant was applied.
type FinishCommittedItem struct {
	SlotKey     string `json:"slot_key"`
	SlotIndex   uint16 `json:"slot_index"`
	ItemID      int64  `json:"item_id"`
	Delta       int64  `json:"delta"`
	PostCount   int64  `json:"post_count"`
	CountOrSeed uint32 `json:"count_or_seed"`
	RawEntry    []byte `json:"raw_entry"`
}

type FinishCommittedCurrency struct {
	Name      string `json:"name"`
	Delta     int64  `json:"delta"`
	PostValue int64  `json:"post_value"`
}

// FinishConsumedItem records one quest-submit material stack consumed by the
// finish transaction. SlotKey is the current list:slot owner key; the current
// EXE finish ACK subtracts Delta from SlotIndex, while post-finish refreshes
// use RawEntry when the stack remains or a delete row when PostCount is zero.
type FinishConsumedItem struct {
	SlotKey   string `json:"slot_key"`
	SlotIndex uint16 `json:"slot_index"`
	ItemID    int64  `json:"item_id"`
	Delta     int64  `json:"delta"`
	PostCount int64  `json:"post_count"`
	RawEntry  []byte `json:"raw_entry,omitempty"`
}

type FinishItemAllocator func(*dnfrepo.InventoryRecord, FinishItemGrantRequest) (FinishCommittedItem, error)

type FinishCommitInput struct {
	AccountID              string
	CharacterID            string
	QuestID                int64
	RewardSelectIndex      uint16
	HasRewardSelect        bool
	Multiplier             uint16
	ExpectedCompletionKey  string
	CommittedAt            time.Time
	Progression            *progression.Tables
	ExperienceBonusPercent int64
	AllocateItem           FinishItemAllocator
	ProfessionProfiles     *profession.Profiles
	SkillCatalog           *dnfskill.Table
}

// FinishCommitResult is a detached post-commit snapshot. Every map, slice and
// raw row is cloned before it crosses the domain boundary. It contains no
// opcode or packet fields and is shared by town op34 and dungeon settlement.
type FinishCommitResult struct {
	CharacterID                string
	CompletionKey              string
	Source                     string
	QuestID                    int64
	AtomicCommitted            bool
	Replayed                   bool
	BaseExperienceDelta        uint32
	ExperienceBonusDelta       uint32
	ExperienceDelta            uint32
	PreviousLevel              int
	NewLevel                   int
	PreviousExperience         uint32
	NewExperience              uint32
	SPDelta                    int
	TPDelta                    int
	Items                      []FinishCommittedItem
	ConsumedItems              []FinishConsumedItem
	Currency                   []FinishCommittedCurrency
	PostCommitCharacter        dnfrepo.CharacterRecord
	PostCommitSkill            dnfrepo.SkillRecord
	PostCommitInventory        dnfrepo.InventoryRecord
	PostCommitAccountInventory dnfrepo.AccountInventoryRecord
	PostCommitQuest            dnfrepo.QuestRecord
	Profession                 profession.Transition
	ProfessionGrants           []profession.Grant
	HasProfession              bool
	ExpertJobType              byte
	HasExpertJob               bool
	HasSlotExpansion           bool
	SlotExpansionIndex         uint32
	SlotExpansionBit           byte
}

type finishSettlementReceipt struct {
	Version              int                       `json:"version"`
	CharacterID          string                    `json:"character_id"`
	CompletionKey        string                    `json:"completion_key"`
	Source               string                    `json:"source"`
	QuestID              int64                     `json:"quest_id"`
	BaseExperienceDelta  uint32                    `json:"base_experience_delta,omitempty"`
	ExperienceBonusDelta uint32                    `json:"experience_bonus_delta,omitempty"`
	ExperienceDelta      uint32                    `json:"experience_delta"`
	PreviousLevel        int                       `json:"previous_level"`
	NewLevel             int                       `json:"new_level"`
	PreviousExperience   uint32                    `json:"previous_experience"`
	NewExperience        uint32                    `json:"new_experience"`
	SPDelta              int                       `json:"sp_delta"`
	TPDelta              int                       `json:"tp_delta"`
	PostSkillPoints      dnfrepo.SkillPointState   `json:"post_skill_points"`
	Items                []FinishCommittedItem     `json:"items,omitempty"`
	ConsumedItems        []FinishConsumedItem      `json:"consumed_items,omitempty"`
	Currency             []FinishCommittedCurrency `json:"currency,omitempty"`
	Profession           profession.Transition     `json:"profession,omitempty"`
	ProfessionGrants     []profession.Grant        `json:"profession_grants,omitempty"`
	HasProfession        bool                      `json:"has_profession,omitempty"`
	ExpertJobType        byte                      `json:"expert_job_type,omitempty"`
	HasExpertJob         bool                      `json:"has_expert_job,omitempty"`
	HasSlotExpansion     bool                      `json:"has_slot_expansion,omitempty"`
	SlotExpansionIndex   uint32                    `json:"slot_expansion_index,omitempty"`
	SlotExpansionBit     byte                      `json:"slot_expansion_bit,omitempty"`
}

// ApplyFinishSettlement validates the Phase-A pending marker and atomically
// commits quest state, PVF experience/SP, and PVF item rewards. A same-key
// replay returns the persisted receipt without granting twice; a different or
// no-longer-current receipt fails closed.
func (o *Owner) ApplyFinishSettlement(
	ctx context.Context,
	catalog *Catalog,
	input FinishCommitInput,
) (FinishCommitResult, error) {
	if o == nil || o.repositories.CharacterSettlement == nil || catalog == nil || input.Progression == nil {
		return FinishCommitResult{}, ErrFinishSettlementUnavailable
	}
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.CharacterID = strings.TrimSpace(input.CharacterID)
	input.ExpectedCompletionKey = strings.TrimSpace(input.ExpectedCompletionKey)
	if input.AccountID == "" || input.CharacterID == "" {
		return FinishCommitResult{}, ErrCharacterRequired
	}
	if input.QuestID <= 0 || input.QuestID > math.MaxUint16 {
		return FinishCommitResult{}, ErrQuestIDRequired
	}
	if input.Multiplier != 1 {
		return FinishCommitResult{}, fmt.Errorf("%w: multiplier=%d", ErrFinishQuestMultiplierUnsupported, input.Multiplier)
	}
	if input.CommittedAt.IsZero() {
		return FinishCommitResult{}, ErrFinishQuestCommitTimeRequired
	}
	if input.ExperienceBonusPercent < 0 {
		return FinishCommitResult{}, fmt.Errorf("%w: percent=%d", ErrFinishExperienceBonusInvalid, input.ExperienceBonusPercent)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var candidate FinishCommitResult
	err := o.repositories.CharacterSettlement.WithinCharacterSettlement(ctx, input.CharacterID, func(tx dnfrepo.Group) error {
		if tx.AccountInventory == nil || tx.Character == nil || tx.Quest == nil || tx.Skill == nil || tx.Inventory == nil || tx.Equipment == nil {
			return ErrFinishSettlementUnavailable
		}
		character, found, err := tx.Character.Load(ctx, input.CharacterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrCharacterNotFound
		}
		character = dnfrepo.CloneCharacter(character)
		if strings.TrimSpace(character.CharacterID) != input.CharacterID || strings.TrimSpace(character.AccountID) != input.AccountID {
			return fmt.Errorf("%w: character=%q account=%q", ErrCharacterNotFound, character.CharacterID, character.AccountID)
		}
		accountInventory, accountInventoryFound, err := tx.AccountInventory.Load(ctx, input.AccountID)
		if err != nil {
			return err
		}
		if accountInventoryFound && strings.TrimSpace(accountInventory.AccountID) != input.AccountID {
			return fmt.Errorf("%w: account inventory owner=%q", ErrFinishSettlementUnavailable, accountInventory.AccountID)
		}
		accountInventory = dnfrepo.CloneAccountInventory(accountInventory)
		accountInventory.AccountID = input.AccountID

		quests, found, err := tx.Quest.Load(ctx, input.CharacterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(quests.CharacterID) != input.CharacterID {
			return ErrFinishQuestNotPending
		}
		quests = dnfrepo.CloneQuest(quests)
		state, field, known := mutableQuestState(&quests, input.QuestID)
		if !known {
			return ErrFinishQuestNotPending
		}

		skill, found, err := tx.Skill.Load(ctx, input.CharacterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(skill.CharacterID) != input.CharacterID {
			return fmt.Errorf("%w: skill record", ErrFinishSettlementUnavailable)
		}
		skill = dnfrepo.CloneSkill(skill)
		previousSkill := dnfrepo.CloneSkill(skill)
		previousSkillPoints := skill.Points
		inventory, found, err := tx.Inventory.Load(ctx, input.CharacterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(inventory.CharacterID) != input.CharacterID {
			return fmt.Errorf("%w: inventory record", ErrFinishSettlementUnavailable)
		}
		inventory = dnfrepo.CloneInventory(inventory)
		if equipment, equipmentFound, err := tx.Equipment.Load(ctx, input.CharacterID); err != nil {
			return err
		} else if equipmentFound && strings.TrimSpace(equipment.CharacterID) != input.CharacterID {
			return fmt.Errorf("%w: equipment owner=%q", ErrFinishSettlementUnavailable, equipment.CharacterID)
		}

		now := input.CommittedAt.UTC()
		questFields := []dnfrepo.QuestField{field}
		if isActiveQuestStatus(state.Status) {
			if questClearTrigger, linkedFields, questClear := finishQuestClearTrigger(catalog, &quests, input.QuestID, now); questClear {
				if questClearTrigger != 0 {
					return ErrFinishQuestNotPending
				}
				for _, linkedField := range linkedFields {
					questFields = mergeQuestChangedField(questFields, linkedField)
				}
				if state.ProgressValue != 0 {
					state.ProgressValue = 0
					state.UpdatedAt = now
					switch field {
					case dnfrepo.QuestFieldStates:
						quests.States[input.QuestID] = state
					case dnfrepo.QuestFieldProgress:
						quests.Progress[input.QuestID] = state
					default:
						return ErrFinishQuestNotPending
					}
				}
			}
		}

		completionKey := strings.TrimSpace(state.Extra["completion_key"])
		if completionKey == "" && isActiveQuestStatus(state.Status) && state.ProgressValue == 0 && strings.TrimSpace(state.Extra["reward_state"]) == "" {
			// NPC/dialogue profession quests do not pass through dungeon clear-map
			// Phase A. Their durable completion proof is the accepted quest row
			// itself with a zero trigger. Derive the key from persisted acceptance
			// state so a rolled-back/retried op34 produces the same identity.
			proofTime := state.UpdatedAt.UTC().UnixNano()
			if proofTime == 0 {
				proofTime = quests.UpdatedAt.UTC().UnixNano()
			}
			completionKey = fmt.Sprintf("quest-finish/%s/%d/%d", input.CharacterID, input.QuestID, proofTime)
			if state.Extra == nil {
				state.Extra = make(map[string]string, 8)
			}
			state.Extra["completion_key"] = completionKey
			state.Extra["completion_kind"] = "active_trigger_zero_op34"
			state.Extra["reward_state"] = clearMapRewardPending
		}
		if completionKey == "" {
			return ErrFinishQuestNotPending
		}
		if input.ExpectedCompletionKey != "" && input.ExpectedCompletionKey != completionKey {
			return fmt.Errorf("%w: expected=%q persisted=%q", ErrFinishQuestCompletionConflict, input.ExpectedCompletionKey, completionKey)
		}
		if strings.EqualFold(strings.TrimSpace(state.Extra["reward_state"]), finishRewardGranted) {
			replayed, err := finishResultFromReceipt(input, state, character, skill, inventory, accountInventory, quests)
			if err != nil {
				return err
			}
			candidate = replayed
			return nil
		}
		if !isActiveQuestStatus(state.Status) || state.ProgressValue != 0 || strings.TrimSpace(state.Extra["reward_state"]) != clearMapRewardPending {
			return ErrFinishQuestNotPending
		}

		jobValue, err := strconv.ParseUint(strings.TrimSpace(character.Job), 10, 8)
		if err != nil || !jobmap.Valid(int(jobValue)) || character.Level <= 0 {
			return fmt.Errorf("%w: level=%d job=%q", ErrQuestNotAcceptable, character.Level, character.Job)
		}
		growTypeValue := character.Stats["grow_type"]
		if growTypeValue < 0 || growTypeValue > math.MaxUint8 {
			return fmt.Errorf("%w: grow_type=%d", ErrQuestNotAcceptable, growTypeValue)
		}
		growType := int(growTypeValue)
		eligibility := CharacterEligibility{Level: character.Level, Job: int(jobValue), GrowType: growType}
		definition, definitionKnown := catalog.Find(input.QuestID)
		if !definitionKnown {
			return ErrQuestDefinitionMissing
		}
		reward, err := catalog.PlanFinishReward(eligibility, input.QuestID, input.RewardSelectIndex, input.HasRewardSelect)
		if err != nil {
			return err
		}
		if reward.HasProfession && (input.ProfessionProfiles == nil || input.SkillCatalog == nil) {
			return ErrFinishProfessionUnavailable
		}
		if reward.HasProfession && reward.HasExpertJob {
			return ErrQuestRewardMalformed
		}
		if reward.HasProfession {
			reward.Profession, err = input.ProfessionProfiles.PlanTransition(byte(jobValue), byte(growType), reward.ProfessionRequest)
			if err != nil {
				return err
			}
		}
		baseExperienceDelta, err := input.Progression.QuestExperience(character.Level, reward.QuestLevel, reward.Difficulty, reward.IgnoreLevel4Exp)
		if err != nil {
			return err
		}
		experienceDelta, experienceBonusDelta := finishExperienceWithBonus(baseExperienceDelta, input.ExperienceBonusPercent)
		progressPlan, err := planFinishProgression(input.Progression, character, skill, experienceDelta)
		if err != nil {
			return err
		}

		if character.Stats == nil {
			character.Stats = make(map[string]int64, 2)
		}
		requiredItems := finishRequiredItemRules(catalog, definition, eligibility)
		consumedItems, err := consumeFinishRequiredItems(&inventory, &accountInventory, requiredItems)
		if err != nil {
			return err
		}
		currency, err := applyFinishRewardCurrency(&character, input.Progression, reward)
		if err != nil {
			return err
		}

		source := "quest_pvf:" + reward.PVFPath
		items, err := allocateFinishRewardItems(&inventory, reward.Items, input.AllocateItem, FinishItemGrantRequest{
			QuestID: input.QuestID, CompletionKey: completionKey, Source: source,
		})
		if err != nil {
			return err
		}
		character.Level = progressPlan.Experience.NewLevel
		character.Stats["exp"] = int64(progressPlan.Experience.NewExperience)
		character.UpdatedAt = now
		skill.Points = progressPlan.SkillPoints.New
		if reward.HasProfession {
			character.Stats["grow_type"] = int64(reward.Profession.NewGrowType)
			skill, err = input.ProfessionProfiles.ApplySkillTransition(
				input.SkillCatalog,
				byte(jobValue),
				progressPlan.Experience.NewLevel,
				reward.Profession,
				skill,
				progressPlan.SkillPoints.New,
			)
			if err != nil {
				return err
			}
		}
		professionGrants, err := finishProfessionGrants(previousSkill, skill, reward.HasProfession)
		if err != nil {
			return err
		}
		if reward.HasExpertJob {
			// characters.expert_job_type is the repository-backed source used by
			// the current USERINFO subtype0 builder. The op34 chain-20 ACK mirrors
			// this same committed value for the online client.
			character.Stats["expert_job_type"] = int64(reward.ExpertJobType)
		}
		if reward.HasSlotExpansion {
			character.Stats["ex_equip_slot_stat"] = character.Stats["ex_equip_slot_stat"] | int64(reward.SlotExpansionBit)
		}
		skill.UpdatedAt = now
		inventory.UpdatedAt = now
		if finishConsumedAccountSharedItems(consumedItems) {
			accountInventory.UpdatedAt = now
		}

		receipt := finishSettlementReceipt{
			Version: finishSettlementReceiptLevel, CharacterID: input.CharacterID,
			CompletionKey: completionKey, Source: source, QuestID: input.QuestID,
			BaseExperienceDelta: baseExperienceDelta, ExperienceBonusDelta: experienceBonusDelta,
			ExperienceDelta: experienceDelta,
			PreviousLevel:   progressPlan.Experience.PreviousLevel, NewLevel: progressPlan.Experience.NewLevel,
			PreviousExperience: progressPlan.Experience.PreviousExperience, NewExperience: progressPlan.Experience.NewExperience,
			SPDelta:            skill.Points.RemainingSP - previousSkillPoints.RemainingSP,
			TPDelta:            skill.Points.RemainingTP - previousSkillPoints.RemainingTP,
			PostSkillPoints:    skill.Points,
			Items:              cloneFinishItems(items),
			ConsumedItems:      cloneFinishConsumedItems(consumedItems),
			Currency:           append([]FinishCommittedCurrency(nil), currency...),
			Profession:         reward.Profession,
			ProfessionGrants:   cloneProfessionGrants(professionGrants),
			HasProfession:      reward.HasProfession,
			ExpertJobType:      reward.ExpertJobType,
			HasExpertJob:       reward.HasExpertJob,
			HasSlotExpansion:   reward.HasSlotExpansion,
			SlotExpansionIndex: reward.SlotExpansionIndex,
			SlotExpansionBit:   reward.SlotExpansionBit,
		}
		receiptJSON, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		state.Status = "completed"
		state.ProgressValue = 0
		state.RewardSelectIndex = int64(input.RewardSelectIndex)
		state.Multiplier = 1
		state.UpdatedAt = now
		if state.Extra == nil {
			state.Extra = make(map[string]string, 8)
		}
		state.Extra["reward_state"] = finishRewardGranted
		state.Extra["settlement_source"] = source
		state.Extra[finishSettlementReceiptKey] = string(receiptJSON)
		switch field {
		case dnfrepo.QuestFieldStates:
			quests.States[input.QuestID] = state
		case dnfrepo.QuestFieldProgress:
			quests.Progress[input.QuestID] = state
		default:
			return ErrFinishQuestNotPending
		}
		quests.UpdatedAt = now
		for _, parentField := range syncActiveQuestClearParentProgress(catalog, &quests, now) {
			questFields = mergeQuestChangedField(questFields, parentField)
		}

		if err := dnfrepo.SaveCharacterFields(ctx, tx.Character, character, dnfrepo.CharacterFieldBase, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		skillFields := []dnfrepo.SkillField{dnfrepo.SkillFieldPoints}
		if reward.HasProfession {
			skillFields = append(skillFields, dnfrepo.SkillFieldSkills, dnfrepo.SkillFieldLayouts, dnfrepo.SkillFieldCooldowns)
		}
		if err := dnfrepo.SaveSkillFields(ctx, tx.Skill, skill, skillFields...); err != nil {
			return err
		}
		if err := dnfrepo.SaveInventoryFields(ctx, tx.Inventory, inventory, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}
		if finishConsumedAccountSharedItems(consumedItems) {
			if err := tx.AccountInventory.Save(ctx, accountInventory); err != nil {
				return err
			}
		}
		if err := dnfrepo.SaveQuestFields(ctx, tx.Quest, quests, questFields...); err != nil {
			return err
		}

		candidate = finishResultFromCommitted(receipt, false, character, skill, inventory, accountInventory, quests)
		return nil
	})
	if err != nil {
		return FinishCommitResult{}, err
	}
	candidate.AtomicCommitted = true
	return cloneFinishCommitResult(candidate), nil
}

func finishQuestClearTrigger(catalog *Catalog, record *dnfrepo.QuestRecord, questID int64, completedAt time.Time) (int64, []dnfrepo.QuestField, bool) {
	if catalog == nil || record == nil {
		return 0, nil, false
	}
	definition, known := catalog.Find(questID)
	if !known {
		return 0, nil, false
	}
	tag := normalizeQuestTag(definition.Type)
	if tag != "quest clear" && tag != "clear quest" {
		return 0, nil, false
	}
	completed, _ := questStateSets(*record)
	trigger := questClearParentTrigger(definition.IntData, completed)
	if trigger == 0 {
		return 0, nil, true
	}

	// The current client finishes the final linked subquest differently from
	// intermediate linked steps: after its trigger channels reach zero it sends
	// op33 for the parent and then op34 for the parent, without a child op34.
	// Accept that terminal state only when every unresolved PVF child belongs to
	// this parent and is durably active at exactly zero progress.
	type terminalChild struct {
		questID int64
		field   dnfrepo.QuestField
		state   dnfrepo.QuestState
	}
	children := make([]terminalChild, 0, len(definition.IntData))
	for _, childID := range definition.IntData {
		if childID <= 0 {
			continue
		}
		if _, done := completed[childID]; done {
			continue
		}
		childDefinition, childKnown := catalog.Find(childID)
		childState, childField, stateKnown := mutableQuestState(record, childID)
		if !childKnown || childDefinition.MainQuestID != questID || !stateKnown ||
			!isActiveQuestStatus(childState.Status) || childState.ProgressValue != 0 {
			return trigger, nil, true
		}
		children = append(children, terminalChild{questID: childID, field: childField, state: childState})
	}
	if len(children) == 0 {
		return trigger, nil, true
	}

	changedFields := make([]dnfrepo.QuestField, 0, 2)
	for _, child := range children {
		child.state.Status = "completed"
		child.state.ProgressValue = 0
		child.state.UpdatedAt = completedAt
		if child.state.Extra == nil {
			child.state.Extra = make(map[string]string, 6)
		}
		child.state.Extra["reward_state"] = finishRewardGranted
		child.state.Extra["auto_completed"] = "true"
		child.state.Extra["auto_complete_reason"] = "quest_clear_parent_terminal_zero_trigger"
		child.state.Extra["auto_completed_by_parent"] = strconv.FormatInt(questID, 10)
		switch child.field {
		case dnfrepo.QuestFieldStates:
			record.States[child.questID] = child.state
		case dnfrepo.QuestFieldProgress:
			record.Progress[child.questID] = child.state
		default:
			return trigger, nil, true
		}
		changedFields = mergeQuestChangedField(changedFields, child.field)
	}
	return 0, changedFields, true
}

// syncActiveQuestClearParentProgress ports only the C# completion-transaction
// rule: after a quest becomes cleared, every currently active quest-clear
// parent recomputes its remaining prerequisite count in that same transaction.
// It intentionally changes no protocol field; the existing post-commit quest
// snapshots serialize the persisted trigger values for the current EXE.
func syncActiveQuestClearParentProgress(
	catalog *Catalog,
	record *dnfrepo.QuestRecord,
	now time.Time,
) []dnfrepo.QuestField {
	if catalog == nil || record == nil {
		return nil
	}
	completed, _ := questStateSets(*record)
	changed := make(map[dnfrepo.QuestField]bool, 2)
	syncField := func(field dnfrepo.QuestField, states map[int64]dnfrepo.QuestState) {
		if len(states) == 0 {
			return
		}
		ids := make([]int64, 0, len(states))
		for questID := range states {
			ids = append(ids, questID)
		}
		sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
		for _, questID := range ids {
			state := states[questID]
			if !isActiveQuestStatus(state.Status) {
				continue
			}
			definition, known := catalog.Find(questID)
			if !known {
				continue
			}
			tag := normalizeQuestTag(definition.Type)
			if tag != "quest clear" && tag != "clear quest" {
				continue
			}
			next := questClearParentTrigger(definition.IntData, completed)
			if next < 0 {
				next = 0
			}
			if state.ProgressValue == next {
				continue
			}
			state.ProgressValue = next
			state.UpdatedAt = now
			states[questID] = state
			changed[field] = true
		}
	}
	syncField(dnfrepo.QuestFieldStates, record.States)
	syncField(dnfrepo.QuestFieldProgress, record.Progress)

	fields := make([]dnfrepo.QuestField, 0, len(changed))
	for _, field := range []dnfrepo.QuestField{dnfrepo.QuestFieldStates, dnfrepo.QuestFieldProgress} {
		if changed[field] {
			fields = append(fields, field)
		}
	}
	return fields
}

// questClearParentTrigger deliberately differs from the accept-time helper:
// an active quest-clear parent with real prerequisites is finishable when all
// of them are completed, so its trigger becomes zero. A malformed/empty
// prerequisite list keeps the existing C# fallback of one.
func questClearParentTrigger(values []int64, completed map[int64]struct{}) int64 {
	hasRequired := false
	missing := int64(0)
	for _, questID := range values {
		if questID <= 0 {
			continue
		}
		hasRequired = true
		if _, exists := completed[questID]; !exists {
			missing++
		}
	}
	if !hasRequired {
		return 1
	}
	return int64(boundedTriggerChannel(missing))
}

func planFinishProgression(
	tables *progression.Tables,
	character dnfrepo.CharacterRecord,
	skill dnfrepo.SkillRecord,
	gain uint32,
) (progression.ExperienceSkillPointPlan, error) {
	if tables == nil || character.Level <= 0 || skill.Points.SyncedLevel != character.Level {
		return progression.ExperienceSkillPointPlan{}, fmt.Errorf("%w: level=%d synced=%d", ErrFinishProgressionUnproven, character.Level, skill.Points.SyncedLevel)
	}
	experienceValue, exists := character.Stats["exp"]
	if !exists || experienceValue < 0 || experienceValue > math.MaxUint32 {
		return progression.ExperienceSkillPointPlan{}, fmt.Errorf("%w: exp=%d present=%t", ErrFinishProgressionUnproven, experienceValue, exists)
	}
	newTotal := uint64(experienceValue) + uint64(gain)
	if newTotal > math.MaxUint32 {
		return progression.ExperienceSkillPointPlan{}, progression.ErrExperienceOutOfRange
	}
	result := progression.ExperienceResult{
		PreviousLevel: character.Level, PreviousExperience: uint32(experienceValue), Gain: gain,
		NewLevel: character.Level, NewExperience: uint32(newTotal),
	}
	for {
		threshold, err := tables.ThresholdToNext(result.NewLevel)
		if err != nil {
			return progression.ExperienceSkillPointPlan{}, fmt.Errorf("%w: level=%d: %v", ErrFinishProgressionUnproven, result.NewLevel, err)
		}
		if result.NewExperience < threshold {
			break
		}
		if _, ok := tables.SkillPointsAtLevel(result.NewLevel + 1); !ok {
			return progression.ExperienceSkillPointPlan{}, fmt.Errorf("%w: missing SP row level=%d", ErrFinishProgressionUnproven, result.NewLevel+1)
		}
		result.NewLevel++
	}
	result.LevelsGained = result.NewLevel - result.PreviousLevel
	points, err := tables.AdvanceSkillPoints(skill.Points, result.NewLevel)
	if err != nil {
		return progression.ExperienceSkillPointPlan{}, err
	}
	return progression.ExperienceSkillPointPlan{Experience: result, SkillPoints: points}, nil
}

func finishExperienceWithBonus(base uint32, percent int64) (uint32, uint32) {
	if base == 0 || percent <= 0 {
		return base, 0
	}
	limit := uint64(math.MaxUint32 - base)
	if uint64(percent) > math.MaxUint64/uint64(base) {
		return math.MaxUint32, uint32(limit)
	}
	bonus := uint64(base) * uint64(percent) / 100
	if bonus > limit {
		bonus = limit
	}
	return base + uint32(bonus), uint32(bonus)
}

func allocateFinishRewardItems(
	record *dnfrepo.InventoryRecord,
	rules []RewardItemRule,
	allocator FinishItemAllocator,
	base FinishItemGrantRequest,
) ([]FinishCommittedItem, error) {
	coalesced := make([]RewardItemRule, 0, len(rules))
	positions := make(map[int64]int, len(rules))
	for _, rule := range rules {
		if rule.ItemID <= 0 || rule.Count <= 0 || rule.Count > math.MaxUint32 {
			return nil, ErrQuestRewardMalformed
		}
		if index, exists := positions[rule.ItemID]; exists {
			if coalesced[index].Count > math.MaxUint32-rule.Count {
				return nil, ErrQuestRewardMalformed
			}
			coalesced[index].Count += rule.Count
			continue
		}
		positions[rule.ItemID] = len(coalesced)
		coalesced = append(coalesced, RewardItemRule{ItemID: rule.ItemID, Count: rule.Count})
	}
	if len(coalesced) != 0 && allocator == nil {
		return nil, ErrFinishItemAllocatorRequired
	}
	items := make([]FinishCommittedItem, 0, len(coalesced))
	for _, rule := range coalesced {
		request := base
		request.ItemID = rule.ItemID
		request.Count = rule.Count
		item, err := allocator(record, request)
		if err != nil {
			return nil, err
		}
		if item.ItemID != rule.ItemID || item.Delta != rule.Count || item.SlotKey == "" || item.PostCount < item.Delta || item.CountOrSeed == 0 || len(item.RawEntry) == 0 {
			return nil, fmt.Errorf("%w: invalid allocator receipt item=%d", ErrQuestRewardMalformed, rule.ItemID)
		}
		items = append(items, item)
	}
	return items, nil
}

func finishRequiredItemRules(catalog *Catalog, definition Definition, character CharacterEligibility) []RewardItemRule {
	_ = character
	coalesced := make(map[int64]int64, 4)
	add := func(itemID, count int64) {
		if itemID <= 0 || count <= 0 {
			return
		}
		current := coalesced[itemID]
		if count > math.MaxUint32 || current > math.MaxUint32-count {
			coalesced[itemID] = math.MaxUint32 + 1
			return
		}
		coalesced[itemID] = current + count
	}
	for _, rule := range finishDefinitionSeekingRules(definition) {
		add(rule.ItemID, rule.Count)
	}
	carryForward := finishCarryForwardEventItemIDs(catalog, definition)
	for _, rule := range itemPairRulesFromInts(definition.DependGiveItemData) {
		if !carryForward[rule.ItemID] {
			add(rule.ItemID, rule.Count)
		}
	}
	if len(coalesced) == 0 {
		return nil
	}
	itemIDs := make([]int64, 0, len(coalesced))
	for itemID := range coalesced {
		itemIDs = append(itemIDs, itemID)
	}
	sort.Slice(itemIDs, func(left, right int) bool { return itemIDs[left] < itemIDs[right] })
	out := make([]RewardItemRule, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		out = append(out, RewardItemRule{ItemID: itemID, Count: coalesced[itemID]})
	}
	return out
}

func finishDefinitionSeekingRules(definition Definition) []RewardItemRule {
	switch normalizeQuestTag(definition.Type) {
	case "seek n meet npc":
		if len(definition.IntData) < 2 {
			return nil
		}
		return []RewardItemRule{{ItemID: definition.IntData[0], Count: definition.IntData[1]}}
	case "seeking":
		return itemPairRulesFromInts(definition.IntData)
	default:
		return nil
	}
}

func itemPairRulesFromInts(values []int64) []RewardItemRule {
	if len(values) < 2 {
		return nil
	}
	rules := make([]RewardItemRule, 0, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		itemID, count := values[index], values[index+1]
		if itemID <= 0 || count <= 0 {
			continue
		}
		rules = append(rules, RewardItemRule{ItemID: itemID, Count: count})
	}
	return rules
}

func finishCarryForwardEventItemIDs(catalog *Catalog, definition Definition) map[int64]bool {
	carryForward := make(map[int64]bool)
	if catalog == nil || !definition.HasDependGiveItem {
		return carryForward
	}
	eventRules := itemPairRulesFromInts(definition.DependGiveItemData)
	if len(eventRules) == 0 {
		return carryForward
	}
	for _, successor := range catalog.Successors(definition.ID) {
		nextNeeds := make(map[int64]bool)
		for _, rule := range finishDefinitionSeekingRules(successor) {
			nextNeeds[rule.ItemID] = true
		}
		if len(nextNeeds) == 0 {
			continue
		}
		nextGives := make(map[int64]bool)
		for _, rule := range itemPairRulesFromInts(successor.DependGiveItemData) {
			nextGives[rule.ItemID] = true
		}
		for _, rule := range eventRules {
			if nextNeeds[rule.ItemID] && !nextGives[rule.ItemID] {
				carryForward[rule.ItemID] = true
			}
		}
	}
	return carryForward
}

func consumeFinishRequiredItems(
	record *dnfrepo.InventoryRecord,
	account *dnfrepo.AccountInventoryRecord,
	rules []RewardItemRule,
) ([]FinishConsumedItem, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	if record == nil || account == nil {
		return nil, ErrFinishSettlementUnavailable
	}
	required := make(map[int64]int64, len(rules))
	for _, rule := range rules {
		if rule.ItemID <= 0 || rule.Count <= 0 || rule.Count > math.MaxUint32 {
			return nil, ErrQuestRewardMalformed
		}
		if required[rule.ItemID] > math.MaxUint32-rule.Count {
			return nil, ErrQuestRewardMalformed
		}
		required[rule.ItemID] += rule.Count
	}
	if len(required) == 0 {
		return nil, nil
	}
	type candidate struct {
		key           string
		slot          uint16
		accountShared bool
		stack         dnfrepo.ItemStack
	}
	candidatesByItem := make(map[int64][]candidate, len(required))
	for key, stack := range record.Slots {
		slot, ok := parseFinishMainInventorySlotKey(key)
		if !ok || finishSlotIsAccountShared(slot) || stack.ItemID <= 0 {
			continue
		}
		if _, needed := required[stack.ItemID]; !needed {
			continue
		}
		candidatesByItem[stack.ItemID] = append(candidatesByItem[stack.ItemID], candidate{
			key: key, slot: slot, stack: stack,
		})
	}
	for key, stack := range account.Slots {
		slot, ok := parseFinishMainInventorySlotKey(key)
		if !ok || !finishSlotIsAccountShared(slot) || stack.ItemID <= 0 {
			continue
		}
		if _, needed := required[stack.ItemID]; !needed {
			continue
		}
		candidatesByItem[stack.ItemID] = append(candidatesByItem[stack.ItemID], candidate{
			key: key, slot: slot, accountShared: true, stack: stack,
		})
	}
	for itemID, candidates := range candidatesByItem {
		sort.Slice(candidates, func(left, right int) bool {
			return candidates[left].slot < candidates[right].slot
		})
		candidatesByItem[itemID] = candidates
	}
	for itemID, count := range required {
		held := int64(0)
		for _, candidate := range candidatesByItem[itemID] {
			held += finishRequiredItemAvailableCount(candidate.stack, candidate.accountShared)
			if held >= count {
				break
			}
		}
		if held < count {
			return nil, fmt.Errorf("%w: item=%d required=%d held=%d", ErrFinishRequiredItemsMissing, itemID, count, held)
		}
	}
	itemIDs := make([]int64, 0, len(required))
	for itemID := range required {
		itemIDs = append(itemIDs, itemID)
	}
	sort.Slice(itemIDs, func(left, right int) bool { return itemIDs[left] < itemIDs[right] })

	consumed := make([]FinishConsumedItem, 0, len(required))
	for _, itemID := range itemIDs {
		remaining := required[itemID]
		for _, candidate := range candidatesByItem[itemID] {
			if remaining <= 0 {
				break
			}
			slots := record.Slots
			if candidate.accountShared {
				slots = account.Slots
			}
			stack, exists := slots[candidate.key]
			if !exists || stack.ItemID != itemID {
				return nil, ErrFinishRequiredItemsMissing
			}
			available := finishRequiredItemAvailableCount(stack, candidate.accountShared)
			if available <= 0 {
				continue
			}
			take := available
			if take > remaining {
				take = remaining
			}
			post := available - take
			entry := FinishConsumedItem{
				SlotKey:   candidate.key,
				SlotIndex: candidate.slot,
				ItemID:    itemID,
				Delta:     take,
				PostCount: post,
			}
			if post <= 0 {
				delete(slots, candidate.key)
			} else {
				stack.Count = post
				stack = finishStackWithUpdatedCount(stack, candidate.slot, itemID, post)
				slots[candidate.key] = stack
				entry.RawEntry = append([]byte(nil), stack.RawEntry...)
			}
			consumed = append(consumed, entry)
			remaining -= take
		}
		if remaining != 0 {
			return nil, ErrFinishRequiredItemsMissing
		}
	}
	return consumed, nil
}

func finishSlotIsAccountShared(slot uint16) bool {
	return slot <= math.MaxInt16 && dnfrepo.IsAccountSharedInventorySlot(dnfrepo.MainInventoryListType, int16(slot))
}

func finishConsumedAccountSharedItems(items []FinishConsumedItem) bool {
	for _, item := range items {
		if finishSlotIsAccountShared(item.SlotIndex) {
			return true
		}
	}
	return false
}

func parseFinishMainInventorySlotKey(key string) (uint16, bool) {
	listRaw, slotRaw, ok := strings.Cut(key, ":")
	if !ok || strings.TrimSpace(listRaw) != "0" {
		return 0, false
	}
	slot, err := strconv.ParseUint(strings.TrimSpace(slotRaw), 10, 16)
	if err != nil || slot > math.MaxInt16 {
		return 0, false
	}
	return uint16(slot), true
}

func finishStackAvailableCount(stack dnfrepo.ItemStack) int64 {
	if stack.Count > 0 {
		return stack.Count
	}
	return 1
}

func finishRequiredItemAvailableCount(stack dnfrepo.ItemStack, accountShared bool) int64 {
	if accountShared && stack.Count <= 0 {
		return 0
	}
	return finishStackAvailableCount(stack)
}

func finishStackWithUpdatedCount(stack dnfrepo.ItemStack, slot uint16, itemID, count int64) dnfrepo.ItemStack {
	if stack.Extra != nil {
		for _, key := range []string{"amount_or_count", "amount", "count", "stack", "quantity"} {
			if _, exists := stack.Extra[key]; exists {
				stack.Extra[key] = strconv.FormatInt(count, 10)
			}
		}
	}
	if len(stack.RawEntry) == currentQuestFinishRawEntrySize && itemID > 0 && itemID <= math.MaxUint32 && count > 0 && count <= math.MaxUint32 {
		raw := append([]byte(nil), stack.RawEntry...)
		binary.LittleEndian.PutUint16(raw[0:2], slot)
		binary.LittleEndian.PutUint32(raw[2:6], uint32(itemID))
		binary.LittleEndian.PutUint32(raw[6:10], uint32(count))
		stack.RawEntry = raw
	}
	return stack
}

const currentQuestFinishRawEntrySize = 0x77

func finishResultFromReceipt(
	input FinishCommitInput,
	state dnfrepo.QuestState,
	character dnfrepo.CharacterRecord,
	skill dnfrepo.SkillRecord,
	inventory dnfrepo.InventoryRecord,
	accountInventory dnfrepo.AccountInventoryRecord,
	quests dnfrepo.QuestRecord,
) (FinishCommitResult, error) {
	raw := strings.TrimSpace(state.Extra[finishSettlementReceiptKey])
	if raw == "" {
		return FinishCommitResult{}, ErrFinishQuestSettlementCorrupt
	}
	var receipt finishSettlementReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		return FinishCommitResult{}, fmt.Errorf("%w: %v", ErrFinishQuestSettlementCorrupt, err)
	}
	if receipt.Version != finishSettlementReceiptLevel || receipt.CharacterID != input.CharacterID || receipt.QuestID != input.QuestID ||
		receipt.CompletionKey == "" || receipt.CompletionKey != strings.TrimSpace(state.Extra["completion_key"]) || receipt.Source == "" {
		return FinishCommitResult{}, ErrFinishQuestSettlementCorrupt
	}
	if input.ExpectedCompletionKey != "" && input.ExpectedCompletionKey != receipt.CompletionKey {
		return FinishCommitResult{}, ErrFinishQuestCompletionConflict
	}
	exp, ok := character.Stats["exp"]
	if !ok || character.Level != receipt.NewLevel || exp != int64(receipt.NewExperience) || skill.Points != receipt.PostSkillPoints {
		return FinishCommitResult{}, ErrFinishQuestSettlementStale
	}
	if receipt.HasProfession && character.Stats["grow_type"] != int64(receipt.Profession.NewGrowType) {
		return FinishCommitResult{}, ErrFinishQuestSettlementStale
	}
	if !validFinishProfessionGrants(receipt.ProfessionGrants, receipt.HasProfession) {
		return FinishCommitResult{}, ErrFinishQuestSettlementCorrupt
	}
	if receipt.HasExpertJob && character.Stats["expert_job_type"] != int64(receipt.ExpertJobType) {
		return FinishCommitResult{}, ErrFinishQuestSettlementStale
	}
	if receipt.HasSlotExpansion {
		expectedBit, validIndex := ExEquipSlotBitForPVFIndex(receipt.SlotExpansionIndex)
		if !validIndex ||
			receipt.SlotExpansionBit != expectedBit ||
			character.Stats["ex_equip_slot_stat"]&int64(receipt.SlotExpansionBit) == 0 {
			return FinishCommitResult{}, ErrFinishQuestSettlementStale
		}
	}
	for _, item := range receipt.Items {
		stack, exists := inventory.Slots[item.SlotKey]
		if !exists || stack.ItemID != item.ItemID || stack.Count != item.PostCount || !bytes.Equal(stack.RawEntry, item.RawEntry) {
			return FinishCommitResult{}, ErrFinishQuestSettlementStale
		}
	}
	for _, item := range receipt.ConsumedItems {
		if item.SlotKey == "" || item.SlotIndex > math.MaxInt16 || item.ItemID <= 0 || item.Delta <= 0 || item.Delta > math.MaxUint32 || item.PostCount < 0 {
			return FinishCommitResult{}, ErrFinishQuestSettlementCorrupt
		}
		slots := inventory.Slots
		if finishSlotIsAccountShared(item.SlotIndex) {
			slots = accountInventory.Slots
		}
		stack, exists := slots[item.SlotKey]
		if item.PostCount == 0 {
			if exists && stack.ItemID == item.ItemID && stack.Count > 0 {
				return FinishCommitResult{}, ErrFinishQuestSettlementStale
			}
			continue
		}
		if !exists || stack.ItemID != item.ItemID || stack.Count != item.PostCount || !bytes.Equal(stack.RawEntry, item.RawEntry) {
			return FinishCommitResult{}, ErrFinishQuestSettlementStale
		}
	}
	for _, currency := range receipt.Currency {
		if currency.Delta < 0 || currency.PostValue < 0 {
			return FinishCommitResult{}, ErrFinishQuestSettlementCorrupt
		}
		switch strings.ToLower(strings.TrimSpace(currency.Name)) {
		case "gold":
			if character.Stats["gold"] != currency.PostValue {
				return FinishCommitResult{}, ErrFinishQuestSettlementStale
			}
		default:
			return FinishCommitResult{}, ErrFinishQuestSettlementCorrupt
		}
	}
	return finishResultFromCommitted(receipt, true, character, skill, inventory, accountInventory, quests), nil
}

func applyFinishRewardCurrency(
	character *dnfrepo.CharacterRecord,
	tables *progression.Tables,
	reward FinishRewardPlan,
) ([]FinishCommittedCurrency, error) {
	if character == nil || !reward.HasGoldReward {
		return nil, nil
	}
	goldDelta, err := tables.QuestGold(character.Level, reward.QuestLevel, reward.GoldMultiple, reward.IgnoreLevel4Exp)
	if err != nil {
		return nil, err
	}
	if goldDelta == 0 {
		return nil, nil
	}
	if character.Stats == nil {
		character.Stats = make(map[string]int64, 1)
	}
	before := character.Stats["gold"]
	delta := int64(goldDelta)
	if before < 0 || before > (1<<63-1)-delta {
		return nil, ErrQuestRewardMalformed
	}
	after := before + delta
	character.Stats["gold"] = after
	return []FinishCommittedCurrency{{
		Name:      "gold",
		Delta:     delta,
		PostValue: after,
	}}, nil
}

func finishResultFromCommitted(
	receipt finishSettlementReceipt,
	replayed bool,
	character dnfrepo.CharacterRecord,
	skill dnfrepo.SkillRecord,
	inventory dnfrepo.InventoryRecord,
	accountInventory dnfrepo.AccountInventoryRecord,
	quests dnfrepo.QuestRecord,
) FinishCommitResult {
	return FinishCommitResult{
		CharacterID: receipt.CharacterID, CompletionKey: receipt.CompletionKey, Source: receipt.Source,
		QuestID: receipt.QuestID, Replayed: replayed,
		BaseExperienceDelta: receipt.BaseExperienceDelta, ExperienceBonusDelta: receipt.ExperienceBonusDelta,
		ExperienceDelta: receipt.ExperienceDelta,
		PreviousLevel:   receipt.PreviousLevel, NewLevel: receipt.NewLevel,
		PreviousExperience: receipt.PreviousExperience, NewExperience: receipt.NewExperience,
		SPDelta: receipt.SPDelta, TPDelta: receipt.TPDelta,
		Items: cloneFinishItems(receipt.Items), ConsumedItems: cloneFinishConsumedItems(receipt.ConsumedItems),
		Currency:   append([]FinishCommittedCurrency(nil), receipt.Currency...),
		Profession: receipt.Profession, ProfessionGrants: cloneProfessionGrants(receipt.ProfessionGrants),
		HasProfession: receipt.HasProfession,
		ExpertJobType: receipt.ExpertJobType, HasExpertJob: receipt.HasExpertJob,
		HasSlotExpansion: receipt.HasSlotExpansion, SlotExpansionIndex: receipt.SlotExpansionIndex,
		SlotExpansionBit:    receipt.SlotExpansionBit,
		PostCommitCharacter: dnfrepo.CloneCharacter(character), PostCommitSkill: dnfrepo.CloneSkill(skill),
		PostCommitInventory:        dnfrepo.CloneInventory(inventory),
		PostCommitAccountInventory: dnfrepo.CloneAccountInventory(accountInventory),
		PostCommitQuest:            dnfrepo.CloneQuest(quests),
	}
}

func finishProfessionGrants(before dnfrepo.SkillRecord, after dnfrepo.SkillRecord, enabled bool) ([]profession.Grant, error) {
	if !enabled {
		return nil, nil
	}
	grants := make([]profession.Grant, 0)
	for rawSkillID, state := range after.Skills {
		previous := before.Skills[rawSkillID]
		if !state.Enabled || (previous.Enabled && previous.Level >= state.Level) {
			continue
		}
		if rawSkillID <= 0 || rawSkillID > math.MaxUint16 || state.Level <= 0 || state.Level > math.MaxUint8 {
			return nil, fmt.Errorf("%w: profession skill=%d level=%d", ErrQuestRewardMalformed, rawSkillID, state.Level)
		}
		grants = append(grants, profession.Grant{SkillID: uint16(rawSkillID), Level: state.Level})
	}
	sort.Slice(grants, func(i, j int) bool {
		return grants[i].SkillID < grants[j].SkillID
	})
	return grants, nil
}

func validFinishProfessionGrants(grants []profession.Grant, hasProfession bool) bool {
	if !hasProfession {
		return len(grants) == 0
	}
	var previous uint16
	for index, grant := range grants {
		if grant.SkillID == 0 || grant.Level <= 0 || grant.Level > math.MaxUint8 ||
			(index != 0 && grant.SkillID <= previous) {
			return false
		}
		previous = grant.SkillID
	}
	return len(grants) <= math.MaxUint8
}

func cloneProfessionGrants(grants []profession.Grant) []profession.Grant {
	if len(grants) == 0 {
		return nil
	}
	return append([]profession.Grant(nil), grants...)
}

func cloneFinishItems(items []FinishCommittedItem) []FinishCommittedItem {
	if len(items) == 0 {
		return nil
	}
	out := append([]FinishCommittedItem(nil), items...)
	for index := range out {
		out[index].RawEntry = append([]byte(nil), out[index].RawEntry...)
	}
	return out
}

func cloneFinishConsumedItems(items []FinishConsumedItem) []FinishConsumedItem {
	if len(items) == 0 {
		return nil
	}
	out := append([]FinishConsumedItem(nil), items...)
	for index := range out {
		out[index].RawEntry = append([]byte(nil), out[index].RawEntry...)
	}
	return out
}

func cloneFinishCommitResult(result FinishCommitResult) FinishCommitResult {
	result.Items = cloneFinishItems(result.Items)
	result.ConsumedItems = cloneFinishConsumedItems(result.ConsumedItems)
	result.Currency = append([]FinishCommittedCurrency(nil), result.Currency...)
	result.PostCommitCharacter = dnfrepo.CloneCharacter(result.PostCommitCharacter)
	result.PostCommitSkill = dnfrepo.CloneSkill(result.PostCommitSkill)
	result.PostCommitInventory = dnfrepo.CloneInventory(result.PostCommitInventory)
	result.PostCommitAccountInventory = dnfrepo.CloneAccountInventory(result.PostCommitAccountInventory)
	result.PostCommitQuest = dnfrepo.CloneQuest(result.PostCommitQuest)
	return result
}
