package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	dnfdungeon "longheng.io/server/internal/modules/dnf/dungeon"
	dnfhonor "longheng.io/server/internal/modules/dnf/honor"
	"longheng.io/server/internal/modules/dnf/premium"
	"longheng.io/server/internal/modules/dnf/progression"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	errCurrentDungeonSettlementOwnerUnavailable = dnfdungeon.ErrOwnerUnavailable
	errCurrentDungeonSettlementStateInvalid     = dnfdungeon.ErrSettlementStateInvalid
	errCurrentDungeonSettlementPVFInvalid       = errors.New("current dungeon settlement PVF reward source is invalid")
)

const (
	// The active runtime premiumlist_new.etc maps Black Diamond activation
	// items (including 193 and 901) to type 29. Types 1/17 are retained only
	// to honor metadata created by the earlier compatibility profile.
	currentDungeonBlackDiamondPremiumType int64 = 29
	legacyDungeonBlackDiamondPremiumTypeA int64 = 1
	legacyDungeonBlackDiamondPremiumTypeB int64 = 17
	currentDungeonBlackDiamondBonusRate         = 0.10
)

type currentDungeonSettlementRewardResources struct {
	Progression       *progression.Tables
	ExperienceSources *progression.MonsterExperienceSources
	ClearRankCatalog  currentDungeonClearRankCatalog
}

type currentDungeonSettlementCommitResult struct {
	Character       dnfrepo.CharacterRecord
	Skill           dnfrepo.SkillRecord
	Inventory       dnfrepo.InventoryRecord
	ExperienceGain  uint32
	HonorExpertGain uint32
	SPGain          int
	TPGain          int
	Replayed        bool
}

// produceCurrentDungeonSettlementPlanLocked is the missing owner between the
// accepted current-EXE op46 and the frozen op34 -> op37 -> op35 send plan.
// The caller holds session.dungeon.mu, so consumed-drop receipts cannot change
// while the database snapshot and packet plan are frozen.
func (s *Service) produceCurrentDungeonSettlementPlanLocked(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || session.dungeon.runtime != runtime ||
		!runtime.settlementPlayResultReceived {
		return errCurrentDungeonSettlementOwnerUnavailable
	}
	if runtime.clearMapCompletionKey == "" {
		if err := ensureCurrentDungeonCompletionReceiptKey(
			session,
			runtime,
			runtime.Session.Snapshot(),
			s.gameplayNow(),
			"settlement_plan_without_matching_clear_map_quest",
		); err != nil {
			return err
		}
	}
	if runtime.settlementResultPlan != nil {
		return nil
	}
	resources, err := s.currentDungeonSettlementRewardResources(ctx)
	if err != nil {
		return err
	}
	return s.produceCurrentDungeonSettlementPlanWithResourcesLocked(ctx, session, runtime, resources)
}

func (s *Service) produceCurrentDungeonSettlementPlanWithResourcesLocked(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
	resources currentDungeonSettlementRewardResources,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || session.dungeon.runtime != runtime ||
		!runtime.settlementPlayResultReceived {
		return errCurrentDungeonSettlementOwnerUnavailable
	}
	if runtime.clearMapCompletionKey == "" {
		if err := ensureCurrentDungeonCompletionReceiptKey(
			session,
			runtime,
			runtime.Session.Snapshot(),
			s.gameplayNow(),
			"settlement_plan_without_matching_clear_map_quest",
		); err != nil {
			return err
		}
	}
	if runtime.settlementResultPlan != nil {
		return nil
	}
	gain, err := currentDungeonClearExperience(runtime, resources)
	if err != nil {
		return err
	}
	// 成长契约 (premium type 84): +20% clear experience while active. The
	// total is committed and receipted, so an idempotent replay re-applies
	// exactly the same amount (86JP ComputeClearExp growth part).
	growthBonus := s.currentGrowthContractBonusExp(ctx, s.accountIDForSession(session), gain)
	// 评分奖励: base × ClearRankRates[bonusIndex] (86JP scoreBonusExp).
	clientRankPoint := runtime.settlementPlayResultBody[currentDungeonPlayResultClientRankPointOffset]
	scoreBonus := currentDungeonScoreBonusExp(gain, resources, clientRankPoint)
	// 黑钻加成: base × 10% (premium type 1 or 17).
	blackDiamondBonus := s.currentBlackDiamondBonusExp(ctx, s.accountIDForSession(session), gain)
	totalGain := gain + growthBonus + scoreBonus + blackDiamondBonus
	presentation, err := currentDungeonSettlementPresentationForRuntime(
		runtime,
		resources.ClearRankCatalog,
		clientRankPoint,
	)
	if err != nil {
		return err
	}
	result, err := s.commitCurrentDungeonSettlement(
		ctx,
		session,
		runtime,
		resources.Progression,
		totalGain,
	)
	if err != nil {
		return err
	}
	permissionBody, permissionState, permissionUpdated, permissionErr := s.commitCurrentDungeonPermission(
		ctx,
		session,
		runtime,
	)
	if permissionErr != nil {
		s.logGameEvent(session, "game-dungeon-permission-clear-state-update-failed",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"difficulty", runtime.Request.Difficulty,
			"source", "settlement_plan_after_reward_commit",
			"error", permissionErr)
	}
	if err := s.freezeCurrentDungeonCardRewardStateLocked(session, runtime); err != nil {
		s.logGameEvent(session, "game-dungeon-card-reward-plan-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"completion_key", runtime.clearMapCompletionKey,
			"source", "settlement_plan_pre_op35_freeze",
			"error", err)
	}
	drops, err := currentDungeonCommittedDropReceipts(runtime, result.Inventory)
	if err != nil {
		return err
	}
	cardSlots := currentDungeonClearRewardCardSlotsFromRuntime(runtime)
	var bonusPresentation [currentDungeonClearRewardBonusFieldCount]uint32
	bonusPresentation[currentDungeonClearRewardBonusClearBlackDiamondIndex] = blackDiamondBonus
	bonusPresentation[currentDungeonClearRewardBonusClearGrowthContractIndex] = growthBonus
	bonusPresentation[currentDungeonClearRewardBonusMonsterGrowthContractIndex] = runtime.settlementMonsterGrowthContractBonus
	notice := currentDungeonPlayResultNotice{
		RankGrade:       presentation.RankGrade,
		ClearTimeMS:     presentation.ClearTimeMS,
		TimeBonusPoint:  presentation.TimeBonusPoint,
		RankPoint:       clientRankPoint,
		AllVisitedClear: presentation.AllVisitedClear,
		Participants: []currentDungeonPlayResultParticipant{{
			ObjectKey:   session.selectedCharacterID,
			ClearTimeMS: presentation.ClearTimeMS,
		}},
	}
	reward := currentDungeonClearRewardSnapshot{
		CharacterID:             session.selectedCharacterID,
		CompletionKey:           runtime.clearMapCompletionKey,
		Source:                  "runtime_pvf_dungeon_clear_and_committed_pickups",
		Committed:               true,
		CommittedExperienceGain: result.ExperienceGain,
		CommittedSPGain:         result.SPGain,
		CommittedTPGain:         result.TPGain,
		Base: [4]uint32{
			currentDungeonClearRewardBaseClearExperienceIndex: gain,
			currentDungeonClearRewardBaseScoreBonusIndex:      scoreBonus,
		},
		Bonus:     bonusPresentation,
		Score:     currentDungeonSettlementScore(runtime),
		Drops:     drops,
		CardSlots: cardSlots,
		Tail: currentDungeonClearRewardTail{
			ShowResult:      true,
			MonsterTotalExp: runtime.settlementMonsterExperienceTotal,
		},
	}
	plan, err := buildCurrentDungeonSettlementPacketPlan(
		clientRankPoint,
		notice,
		reward,
		result.Character,
		result.Skill.Points,
	)
	if err != nil {
		return err
	}
	if len(permissionBody) != 0 {
		plan.DungeonPermissionBody = append([]byte(nil), permissionBody...)
	}
	runtime.Character = dnfrepo.CloneCharacter(result.Character)
	runtime.settlementResultPlan = &plan
	s.logGameEvent(session, "game-dungeon-settlement-plan-frozen",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"completion_key", runtime.clearMapCompletionKey,
		"experience_gain", result.ExperienceGain,
		"growth_contract_bonus", growthBonus,
		"sp_gain", result.SPGain,
		"tp_gain", result.TPGain,
		"remaining_sp", result.Skill.Points.RemainingSP,
		"remaining_tp", result.Skill.Points.RemainingTP,
		"op34_rank_grade", presentation.RankGrade,
		"op34_rank_point", clientRankPoint,
		"op34_clear_time_ms", presentation.ClearTimeMS,
		"op34_all_visited_clear", presentation.AllVisitedClear,
		"op34_time_bonus_point", presentation.TimeBonusPoint,
		"picked_drop_receipts", len(drops),
		"replayed", result.Replayed,
		"op35_clear_base_exp", gain,
		"op35_clear_score_bonus_exp", scoreBonus,
		"op35_clear_growth_contract_bonus", growthBonus,
		"op35_clear_black_diamond_bonus", blackDiamondBonus,
		"op35_monster_base_exp", runtime.settlementMonsterExperienceTotal,
		"op35_monster_growth_contract_bonus", runtime.settlementMonsterGrowthContractBonus,
		"op35_show_result", true,
		"op35_free_card_pairs", len(cardSlots[0]),
		"op35_champion_experience", runtime.settlementChampionExperience,
		"op35_super_champion_experience", runtime.settlementSuperChampionExperience,
		"op35_boss_experience", runtime.settlementBossExperience,
		"card_rewards", runtime.settlementCardRewardState != nil,
		"dungeon_permission_updated", permissionUpdated,
		"dungeon_permission_state", permissionState,
		"unknown_op35_business_fields", "zero_except_proved_result_gate_score_experience_and_card_reward_context")
	return nil
}

func currentDungeonSettlementScore(runtime *runtimeDungeonState) [currentDungeonClearRewardScoreFieldCount]uint32 {
	if runtime == nil {
		return [currentDungeonClearRewardScoreFieldCount]uint32{}
	}
	// Current NoPack consumes Score[1..3] as the champion,
	// super-champion, and boss result breakdown. Score[0] remains neutral;
	// aggregate monster EXP has its dedicated tail field. Clamp every
	// classified subtotal to that aggregate so damaged runtime state cannot
	// overstate it.
	return [currentDungeonClearRewardScoreFieldCount]uint32{
		0,
		min(runtime.settlementChampionExperience, runtime.settlementMonsterExperienceTotal),
		min(runtime.settlementSuperChampionExperience, runtime.settlementMonsterExperienceTotal),
		min(runtime.settlementBossExperience, runtime.settlementMonsterExperienceTotal),
	}
}

func currentDungeonClearRewardCardSlotsFromRuntime(runtime *runtimeDungeonState) [currentDungeonClearRewardGroupCount][]currentDungeonClearRewardPair {
	var slots [currentDungeonClearRewardGroupCount][]currentDungeonClearRewardPair
	if runtime == nil || runtime.settlementCardRewardState == nil {
		return slots
	}
	runtime.settlementCardRewardState.mu.Lock()
	plan := cloneDungeonCardRewardPlan(runtime.settlementCardRewardState.plan)
	runtime.settlementCardRewardState.mu.Unlock()
	currentDungeonProjectCardBundleToClearRewardSlots(&slots, 0, plan.Sides[dungeonCardSideFree])
	currentDungeonProjectCardBundleToClearRewardSlots(
		&slots,
		dungeonCardSlotsPerSide,
		plan.Sides[dungeonCardSidePaid],
	)
	return slots
}

func currentDungeonProjectCardBundleToClearRewardSlots(
	slots *[currentDungeonClearRewardGroupCount][]currentDungeonClearRewardPair,
	base int,
	bundle dungeonCardRewardBundle,
) {
	if slots == nil || base < 0 || base >= len(slots) {
		return
	}
	displayItems := currentDungeonCardDisplayItems(bundle)
	pairs := make([]currentDungeonClearRewardPair, 0, 1+len(displayItems))
	if bundle.Gold > 0 && bundle.Gold <= math.MaxUint32 {
		pairs = append(pairs, currentDungeonClearRewardPair{Key: 0, Value: uint32(bundle.Gold)})
	}
	for _, item := range displayItems {
		if item.ItemID <= 0 || item.ItemID > math.MaxUint32 || item.Count <= 0 {
			continue
		}
		count := item.Count
		if count > math.MaxUint32 {
			count = math.MaxUint32
		}
		pairs = append(pairs, currentDungeonClearRewardPair{
			Key:   uint32(item.ItemID),
			Value: uint32(count),
		})
	}
	// The eight op35 groups are party-member seats (free 0..3, paid 4..7),
	// not separate gold/item card faces. Keep every reward tuple for one side
	// in that member's single group so the free-row reveal can render the item
	// while retaining the accompanying gold amount.
	slots[base] = pairs
}

func (s *Service) currentDungeonSettlementRewardResources(ctx context.Context) (currentDungeonSettlementRewardResources, error) {
	if s == nil {
		return currentDungeonSettlementRewardResources{}, errCurrentDungeonSettlementOwnerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return currentDungeonSettlementRewardResources{}, err
	}
	tables, err := progression.Load(ctx, archive)
	if err != nil {
		return currentDungeonSettlementRewardResources{}, err
	}
	sources, err := progression.LoadMonsterExperienceSources(archive)
	if err != nil {
		return currentDungeonSettlementRewardResources{}, err
	}
	clearRankCatalog, err := loadCurrentDungeonClearRankCatalog(archive)
	if err != nil {
		return currentDungeonSettlementRewardResources{}, err
	}
	return currentDungeonSettlementRewardResources{
		Progression:       tables,
		ExperienceSources: sources,
		ClearRankCatalog:  clearRankCatalog,
	}, nil
}

func currentDungeonClearExperience(
	runtime *runtimeDungeonState,
	resources currentDungeonSettlementRewardResources,
) (uint32, error) {
	if runtime == nil || resources.Progression == nil || resources.ExperienceSources == nil ||
		!runtime.Dungeon.Metadata.BasisLevel.Set || runtime.Dungeon.Metadata.BasisLevel.Value <= 0 {
		return 0, errCurrentDungeonSettlementPVFInvalid
	}
	// The dungeon field is optional in the runtime PVF: ordinary dungeon 3
	// (幽暗密林) omits it. The established domain rule is neutral weight 1.0
	// when absent or negative; an explicit non-negative value remains exact.
	weight := 1.0
	if runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Set &&
		runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Value >= 0 {
		weight = runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Value
	}
	if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
		return 0, errCurrentDungeonSettlementPVFInvalid
	}
	base, err := resources.Progression.DungeonClearBaseExperience(int(runtime.Dungeon.Metadata.BasisLevel.Value))
	if err != nil || base == 0 {
		return 0, fmt.Errorf("%w: base=%d: %v", errCurrentDungeonSettlementPVFInvalid, base, err)
	}
	modifiers := resources.ExperienceSources.RawModifiers()
	difficulty := int(runtime.Request.Difficulty)
	if difficulty < 0 || difficulty >= len(modifiers.DifficultyRates) {
		return 0, errCurrentDungeonSettlementPVFInvalid
	}
	difficultyRate := modifiers.DifficultyRates[difficulty]
	if math.IsNaN(difficultyRate) || math.IsInf(difficultyRate, 0) || difficultyRate <= 0 {
		return 0, errCurrentDungeonSettlementPVFInvalid
	}
	value := math.Floor(float64(base) * weight * difficultyRate)
	if value <= 0 || value > math.MaxUint32 {
		return 0, fmt.Errorf("%w: computed=%v", errCurrentDungeonSettlementPVFInvalid, value)
	}
	return uint32(value), nil
}

// currentDungeonScoreBonusExp calculates the score bonus experience from the
// PVF [clear rank exp bonusrate] table. The bonus index is the number of rank
// grade thresholds the client rank point meets or exceeds (86JP
// MonsterRewardTable.GetClearRankBonusIndex).
func currentDungeonScoreBonusExp(base uint32, resources currentDungeonSettlementRewardResources, clientRankPoint byte) uint32 {
	if base == 0 || resources.ExperienceSources == nil {
		return 0
	}
	modifiers := resources.ExperienceSources.RawModifiers()
	bonusIndex := 0
	for _, threshold := range resources.ClearRankCatalog.gradeThresholds {
		if clientRankPoint >= threshold {
			bonusIndex++
		}
	}
	if bonusIndex <= 0 || bonusIndex > len(modifiers.ClearRankRates) {
		return 0
	}
	rate := modifiers.ClearRankRates[bonusIndex-1]
	if rate <= 0 {
		return 0
	}
	bonus := math.Floor(float64(base) * rate)
	if bonus <= 0 || bonus > math.MaxUint32 {
		return 0
	}
	return uint32(bonus)
}

// currentBlackDiamondBonusExp returns +10% clear experience when the account
// has an active current-PVF black diamond premium (type 29). Legacy type-1
// and type-17 metadata remains readable so existing local accounts do not
// lose an already-active benefit.
func (s *Service) currentBlackDiamondBonusExp(ctx context.Context, accountID string, base uint32) uint32 {
	if s == nil || base == 0 || strings.TrimSpace(accountID) == "" {
		return 0
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Account == nil {
		return 0
	}
	account, found, err := repositories.Account.Load(ctx, strings.TrimSpace(accountID))
	if err != nil || !found {
		return 0
	}
	now := time.Now().UTC()
	if !premium.Active(account, currentDungeonBlackDiamondPremiumType, now) &&
		!premium.Active(account, legacyDungeonBlackDiamondPremiumTypeA, now) &&
		!premium.Active(account, legacyDungeonBlackDiamondPremiumTypeB, now) {
		return 0
	}
	bonus := math.Floor(float64(base) * currentDungeonBlackDiamondBonusRate)
	if bonus <= 0 || bonus > math.MaxUint32-float64(base) {
		return 0
	}
	return uint32(bonus)
}

func (s *Service) commitCurrentDungeonSettlement(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
	tables *progression.Tables,
	gain uint32,
) (currentDungeonSettlementCommitResult, error) {
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.CharacterSettlement == nil || tables == nil || gain == 0 {
		return currentDungeonSettlementCommitResult{}, errCurrentDungeonSettlementOwnerUnavailable
	}
	owner, err := dnfdungeon.NewOwner(repositories)
	if err != nil {
		return currentDungeonSettlementCommitResult{}, errCurrentDungeonSettlementOwnerUnavailable
	}
	if runtime == nil {
		return currentDungeonSettlementCommitResult{}, errCurrentDungeonSettlementStateInvalid
	}
	runtimeExperience, present := runtime.Character.Stats["exp"]
	if !present || runtimeExperience < 0 || uint64(runtimeExperience) > math.MaxUint32 {
		return currentDungeonSettlementCommitResult{}, errCurrentDungeonSettlementStateInvalid
	}
	honorExpertGain, err := currentHonorExpertExperienceGain(
		tables,
		runtime.Character.Level,
		uint32(runtimeExperience),
		gain,
	)
	if err != nil {
		return currentDungeonSettlementCommitResult{}, err
	}
	// Only load the independent PVF honor table for an award that actually
	// reaches the character-level-cap split.
	var honorTables *dnfhonor.Tables
	if honorExpertGain > 0 {
		loaded, loadErr := s.loadHonorTable(ctx)
		if loadErr != nil {
			return currentDungeonSettlementCommitResult{}, loadErr
		}
		honorTables = loaded
	}
	adventureRuntime := s.adventureGroupTable.Runtime()
	result, err := owner.CommitSettlement(ctx, dnfdungeon.SettlementCommand{
		AccountID:               s.accountIDForSession(session),
		CharacterID:             fmt.Sprintf("%d", session.selectedCharacterID),
		CompletionKey:           runtime.clearMapCompletionKey,
		Tables:                  tables,
		Experience:              gain,
		RecommendedDungeonClear: currentDungeonIsRecommendedClear(runtime),
		AdventureRuntime:        &adventureRuntime,
		HonorExpertTables:       honorTables,
		MaximumCharacterLevel:   currentAdventureCharacterLevelCap,
		UpdatedAt:               time.Now().UTC(),
	})
	if err != nil {
		return currentDungeonSettlementCommitResult{}, err
	}
	return currentDungeonSettlementCommitResult{
		Character:       result.Character,
		Skill:           result.Skill,
		Inventory:       result.Inventory,
		ExperienceGain:  result.ExperienceGain,
		HonorExpertGain: result.HonorExpertGain,
		SPGain:          result.SPGain,
		TPGain:          result.TPGain,
		Replayed:        result.Replayed,
	}, nil
}

func planCurrentDungeonSettlementProgression(
	tables *progression.Tables,
	level int,
	total uint32,
	gain uint32,
	points dnfrepo.SkillPointState,
) (progression.ExperienceSkillPointPlan, error) {
	return dnfdungeon.PlanSettlementProgressionAtCap(
		tables,
		level,
		total,
		gain,
		points,
		currentAdventureCharacterLevelCap,
	)
}

func currentDungeonCommittedDropReceipts(
	runtime *runtimeDungeonState,
	inventory dnfrepo.InventoryRecord,
) ([]currentDungeonClearRewardDrop, error) {
	if runtime == nil || runtime.DropOwner == nil {
		return nil, nil
	}
	bySlot := make(map[uint16]currentDungeonClearRewardDrop)
	for _, drop := range runtime.DropOwner.byObjectKey {
		if drop == nil || drop.Status != runtimeDungeonDropConsumed {
			continue
		}
		if drop.Item.ItemID == 0 {
			continue
		}
		if drop.DestinationSlot == 0 {
			return nil, errCurrentDungeonSettlementStateInvalid
		}
		stack, found := inventory.Slots[currentDungeonPickupMainSlotKey(int16(drop.DestinationSlot))]
		if !found || stack.ItemID != int64(drop.Item.ItemID) || stack.Count <= 0 || stack.Count > math.MaxUint32 {
			return nil, errCurrentDungeonSettlementStateInvalid
		}
		bySlot[drop.DestinationSlot] = currentDungeonClearRewardDrop{
			ObjectKey: drop.DestinationSlot, TemplateID: drop.Item.ItemID, StackCount: uint32(stack.Count),
		}
	}
	result := make([]currentDungeonClearRewardDrop, 0, len(bySlot))
	for _, drop := range bySlot {
		result = append(result, drop)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ObjectKey < result[j].ObjectKey })
	return result, nil
}
