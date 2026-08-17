package dnfbridge

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var currentDungeonCardGoldDifficultyBonus = [...]float64{1.02, 1.38, 1.60, 1.90, 2.0}

const currentDungeonGoldCardItemMultiplier int64 = 2

func (s *Service) freezeCurrentDungeonCardRewardStateLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
) error {
	if s == nil || session == nil || runtime == nil || session.dungeon.runtime != runtime {
		return errDungeonCardAssetOwnerUnavailable
	}
	if runtime.settlementCardRewardState != nil {
		return nil
	}
	monsterCatalog, err := s.dungeonMonsterCatalog()
	if err != nil {
		return err
	}
	goldReferences, err := currentDungeonGoldReferences(monsterCatalog)
	if err != nil {
		return err
	}
	if !runtime.Dungeon.Metadata.BasisLevel.Set || runtime.Dungeon.Metadata.BasisLevel.Value <= 0 {
		return errCurrentDungeonSettlementPVFInvalid
	}
	dungeonLevel := int(runtime.Dungeon.Metadata.BasisLevel.Value)
	goldReference, found := goldReferences[int64(dungeonLevel)]
	if !found {
		return fmt.Errorf("%w: dungeon_level=%d", errCurrentDungeonGoldReferenceInvalid, runtime.Dungeon.Metadata.BasisLevel.Value)
	}
	itemCatalog, err := monsterCatalog.DropCatalog()
	if err != nil {
		return err
	}
	itemDropDocument, err := parseDungeonCardPVFDocument(monsterCatalog.source, dungeonCardDropRulePath)
	if err != nil {
		return err
	}
	itemDropReferences, err := parseDungeonCardPVFItemDropReferences(itemDropDocument)
	if err != nil {
		return err
	}
	itemDropReference, found := itemDropReferences[int64(dungeonLevel)]
	if !found {
		return fmt.Errorf("%w: dungeon_level=%d", errDungeonCardPVFSectionMissing, dungeonLevel)
	}

	rng := newCurrentDungeonDropLCG(runtime.Seed)
	var visit *runtimeDungeonRoomVisit
	if runtime.Session != nil {
		if scene, ok := runtime.Session.Scene(); ok {
			if ownedVisit, visitErr := runtime.currentDungeonRoomVisit(scene); visitErr == nil {
				visit = ownedVisit
				rng = newCurrentDungeonDropLCG(ownedVisit.DropRNG)
			}
		}
	}
	now := s.gameplayNow().UTC()
	freeGold := currentDungeonCardGoldAmount(rng, goldReference, int(runtime.Request.Difficulty))
	freeItem, freeItemFound, err := currentDungeonRandomCardItem(
		rng,
		itemCatalog,
		dungeonLevel,
		itemDropReference,
		now,
	)
	if err != nil {
		return err
	}
	paidGold := currentDungeonCardGoldAmount(rng, goldReference, int(runtime.Request.Difficulty))
	paidItem, paidItemFound, err := currentDungeonRandomCardItem(
		rng,
		itemCatalog,
		dungeonLevel,
		itemDropReference,
		now,
	)
	if err != nil {
		return err
	}
	free := dungeonCardRewardBundle{
		Gold: freeGold,
	}
	if freeItemFound {
		free.Items = []dungeonCardItemReward{freeItem}
	} else {
		free.Gold += 100
	}
	paid := dungeonCardRewardBundle{
		Gold: paidGold,
	}
	if paidItemFound {
		paid.Items = []dungeonCardItemReward{paidItem}
	} else {
		paid.Gold += 100
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	_, account, character, premiumErr := s.currentPremiumServiceRecords(ctx, session)
	cancel()
	if premiumErr == nil &&
		premium.Active(account, premium.DevilSlotType(premium.DevilSlotGoldCard), now) &&
		premium.DailyUsage(character, premium.DevilSlotGoldCard, now) < premium.DailyLimit(premium.DevilSlotGoldCard) {
		paid.ConsumePremiumDaily = true
		paid.PremiumDailySlot = premium.DevilSlotGoldCard
		if err := applyCurrentDungeonGoldCardItemMultiplier(&paid); err != nil {
			return err
		}
	}
	if visit != nil {
		visit.DropRNG = rng.Seed()
	}
	plan, err := newDungeonCardRewardPlan(
		dungeonCardPlanIdentity{
			CharacterID: strconv.Itoa(int(session.selectedCharacterID)),
			DungeonID:   runtime.Dungeon.ID,
			MazeIndex:   runtime.MazeIndex,
			RunSeed:     runtime.Seed,
		},
		"current_pvf_clear_reward_generator_free_and_paid_rows",
		free,
		paid,
	)
	if err != nil {
		return err
	}
	state, err := newDungeonCardState(plan)
	if err != nil {
		return err
	}
	runtime.settlementCardRewardState = state
	s.logGameEvent(session, "game-dungeon-card-reward-plan-frozen",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"plan_id", plan.ID,
		"free_gold", free.Gold,
		"free_item_count", len(free.Items),
		"paid_gold", paid.Gold,
		"paid_item_count", len(paid.Items),
		"premium_gold_card_available", paid.ConsumePremiumDaily,
		"source", plan.Source,
		"domain_source", "server_s4a12_clear_reward_generator_with_current_pvf_item_pools")
	return nil
}

// applyCurrentDungeonGoldCardItemMultiplier turns the current EXE's visible
// x2 Gold Card result into two durable item units. Stackables stay one reward
// row with a doubled count. Equipment is cloned into independent count-one
// instances because equipment must never be stored as a stack.
func applyCurrentDungeonGoldCardItemMultiplier(bundle *dungeonCardRewardBundle) error {
	if bundle == nil || len(bundle.Items) == 0 {
		return nil
	}
	items := make([]dungeonCardItemReward, 0, len(bundle.Items)*int(currentDungeonGoldCardItemMultiplier))
	for _, item := range bundle.Items {
		if item.ItemID <= 0 || item.Count <= 0 ||
			item.Count > math.MaxInt64/currentDungeonGoldCardItemMultiplier {
			return fmt.Errorf(
				"%w: gold-card item=%d count=%d",
				errDungeonCardPlanInvalid,
				item.ItemID,
				item.Count,
			)
		}
		if item.Stackable {
			item.Count *= currentDungeonGoldCardItemMultiplier
			items = append(items, cloneDungeonCardItemReward(item))
			continue
		}
		if item.Count != 1 {
			return fmt.Errorf(
				"%w: gold-card equipment=%d count=%d",
				errDungeonCardPlanInvalid,
				item.ItemID,
				item.Count,
			)
		}
		for range currentDungeonGoldCardItemMultiplier {
			items = append(items, cloneDungeonCardItemReward(item))
		}
	}
	bundle.Items = items
	return nil
}

func currentDungeonRandomCardItem(
	rng *currentDungeonDropLCG,
	catalog *pvfDungeonDropCatalog,
	dungeonLevel int,
	reference dungeonCardPVFItemDropReference,
	now time.Time,
) (dungeonCardItemReward, bool, error) {
	if rng == nil || catalog == nil || dungeonLevel <= 0 {
		return dungeonCardItemReward{}, false, errDungeonCardPVFSourceRequired
	}
	rarityRoll := rng.Next(1_000_000) + 1
	rarity := int64(0)
	switch {
	case rarityRoll <= 500_000:
		rarity = 0
	case rarityRoll <= 799_900:
		rarity = 1
	case rarityRoll <= 999_900:
		rarity = 2
	default:
		rarity = 3
	}
	itemID, found, err := catalog.SelectGenericEquipment(
		rng,
		dungeonLevel,
		reference.ValueA,
		reference.ValueB,
		rarity,
	)
	if err != nil {
		return dungeonCardItemReward{}, false, err
	}
	if !found {
		itemID, found, err = catalog.SelectGenericStackable(
			rng,
			dungeonLevel,
			reference.ValueA,
			reference.ValueB,
			rarity,
		)
		if err != nil {
			return dungeonCardItemReward{}, false, err
		}
	}
	if !found || itemID == 0 {
		return dungeonCardItemReward{}, false, nil
	}
	definition, err := catalog.ResolveItem(itemID)
	if err != nil {
		return dungeonCardItemReward{}, false, err
	}
	definition, err = currentPVFItemDefinitionForGrantAt(definition, now)
	if err != nil {
		return dungeonCardItemReward{}, false, err
	}
	extra := map[string]string{
		"source":    "dungeon_card_reward",
		"item_kind": string(definition.Kind),
		"pvf_path":  definition.PVFPath,
	}
	if definition.StackableType != "" {
		extra["stackable_type"] = definition.StackableType
	}
	if definition.StackLimit > 0 {
		extra["stack_limit"] = strconv.FormatInt(definition.StackLimit, 10)
	}
	if definition.EquipmentType != "" {
		extra["equipment_type"] = definition.EquipmentType
	}
	if definition.Durability > 0 {
		extra["durability"] = strconv.FormatUint(uint64(definition.Durability), 10)
		extra["max_durability"] = strconv.FormatUint(uint64(definition.Durability), 10)
	}
	if definition.Kind == dungeonDropItemEquipment {
		qualitySeed := rng.NextUint32()%currentEquipmentRandomQualitySeedCount + 1
		extra["quality_seed"] = strconv.FormatUint(uint64(qualitySeed), 10)
	}
	return dungeonCardItemReward{
		ItemID:    int64(itemID),
		Count:     1,
		Stackable: definition.Kind == dungeonDropItemStackable,
		SlotStart: definition.SlotStart,
		SlotEnd:   definition.SlotEnd,
		ExpireAt:  definition.ExpirationDate,
		Extra:     extra,
	}, true, nil
}

func currentDungeonCardGoldAmount(
	rng *currentDungeonDropLCG,
	reference currentDungeonGoldReference,
	difficulty int,
) int64 {
	if rng == nil || reference.Base <= 0 {
		return 1
	}
	goldBase := reference.Base * 175 / 1000
	if goldBase < 1 {
		goldBase = 1
	}
	diffBonus := 1.0
	if difficulty >= 0 && difficulty < len(currentDungeonCardGoldDifficultyBonus) {
		diffBonus = currentDungeonCardGoldDifficultyBonus[difficulty]
	}
	amount := int64(math.Floor(float64(goldBase) * diffBonus))
	if amount < 1 {
		amount = 1
	}
	if reference.VariancePct > 0 {
		rangeSize := reference.VariancePct*2 + 1
		variance := (int64(rng.Next(int(rangeSize))) - reference.VariancePct) * amount / 100
		amount += variance
	}
	if amount < 1 {
		return 1
	}
	return amount
}

func (s *Service) selectCurrentDungeonCardLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	side dungeonCardSide,
	memberIndex byte,
	source string,
) error {
	if session == nil || runtime == nil || session.dungeon.runtime != runtime ||
		!runtime.settlementCardLayoutSent ||
		runtime.settlementPhase < currentDungeonSettlementPhaseCardsRevealed ||
		side >= dungeonCardSideCount || memberIndex >= dungeonCardSlotsPerSide {
		return nil
	}
	if runtime.settlementCardSideSelectionKnown[side] &&
		runtime.settlementCardSideMember[side] != memberIndex {
		s.logGameEvent(session, "game-dungeon-card-select-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"side", side,
			"member_index", memberIndex,
			"accepted_member_index", runtime.settlementCardSideMember[side],
			"source", source,
			"reason", "row_selection_conflicts_with_first_accepted_member")
		return nil
	}
	if runtime.settlementCardRewardState == nil {
		if err := s.freezeCurrentDungeonCardRewardStateLocked(session, runtime); err != nil {
			s.logGameEvent(session, "game-dungeon-card-reward-plan-blocked",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"side", side,
				"member_index", memberIndex,
				"source", source,
				"error", err)
			return nil
		}
	}
	replay := runtime.settlementCardSideSelectionSent[side]
	runtime.settlementCardSideSelectionKnown[side] = true
	runtime.settlementCardSideMember[side] = memberIndex
	runtime.settlementCardSelected = byte(side)
	runtime.refreshCurrentDungeonFreeCardReadiness()
	slots71, rewardTupleCount := currentDungeonSelectedCardSlots(runtime)
	body71, err := buildCurrentDungeonOp71SuccessBody(slots71)
	if err != nil {
		return err
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSelectCard), body71, currentDungeonCardResponseClass); err != nil {
		return err
	}
	runtime.settlementCardSideSelectionSent[side] = true
	runtime.refreshCurrentDungeonFreeCardReadiness()
	if side == dungeonCardSideFree {
		s.cancelCurrentDungeonCardAutoFlipLocked(session, runtime, "free_row_selected")
	}
	s.logGameEvent(session, "game-dungeon-card-select-ack-sent",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"side", side,
		"member_index", memberIndex,
		"replay", replay,
		"response_body_len", len(body71),
		"classification", currentDungeonCardResponseClass,
		"source", source,
		"reward_tuple_count", rewardTupleCount,
		"body_source", "current_exe_op71_proved_reader_selection_state_with_frozen_card_reward_context")

	if err := s.commitCurrentDungeonCardSideRewardLocked(session, runtime, side, source); err != nil {
		s.logGameEvent(session, "game-dungeon-card-reward-delivery-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"side", side,
			"member_index", memberIndex,
			"source", source,
			"error", err)
	}
	return nil
}

func (s *Service) commitCurrentDungeonCardRewardLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	source string,
) error {
	if runtime == nil || runtime.settlementCardRewardCommitted {
		return nil
	}
	return s.commitCurrentDungeonCardSideRewardLocked(session, runtime, dungeonCardSideFree, source)
}

func (s *Service) commitCurrentDungeonCardSideRewardLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	side dungeonCardSide,
	source string,
) error {
	if runtime == nil || side >= dungeonCardSideCount ||
		runtime.settlementCardSideRewardCommitted[side] {
		return nil
	}
	if !runtime.settlementCardSideSelectionKnown[side] ||
		!runtime.settlementCardSideSelectionSent[side] {
		return errDungeonCardSelectionInvalid
	}
	if err := s.deliverCurrentDungeonSelectedCardRewardLocked(
		session,
		runtime,
		side,
		runtime.settlementCardSideMember[side],
		source,
	); err != nil {
		return err
	}
	runtime.settlementCardSideRewardCommitted[side] = true
	runtime.refreshCurrentDungeonFreeCardReadiness()
	if side == dungeonCardSideFree {
		runtime.advanceSettlementPhase(currentDungeonSettlementPhaseRewardCommitted)
	}
	s.logGameEvent(session, "game-dungeon-card-reward-phase-committed",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"side", side,
		"member_index", runtime.settlementCardSideMember[side],
		"source", source,
		"next_stage", "await_current_exe_c2s_op72_or_completed_op42")
	return nil
}

func (s *Service) deliverCurrentDungeonSelectedCardRewardLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	side dungeonCardSide,
	memberIndex byte,
	source string,
) error {
	if s == nil || session == nil || runtime == nil || runtime.settlementCardRewardState == nil {
		return nil
	}
	reservation, result, err := runtime.settlementCardRewardState.reserveSelection(side, memberIndex)
	if err != nil {
		return err
	}
	if !reservation.grant {
		s.logGameEvent(session, "game-dungeon-card-reward-delivery-skipped",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"selection_result", result,
			"side", side,
			"member_index", memberIndex,
			"source", source,
			"reason", "no_undelivered_reward_for_selection")
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.CharacterAssets == nil {
		runtime.settlementCardRewardState.finishDelivery(reservation, false)
		return errDungeonCardAssetOwnerUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	grant, err := deliverDungeonCardReservation(
		ctx,
		runtime.settlementCardRewardState,
		reservation,
		repositories,
		0,
		s.gameplayNow().UTC(),
	)
	if err != nil {
		return err
	}
	if grant.GoldAfter != grant.GoldBefore {
		body := buildCurrentGoldStateBody(grant.GoldAfter)
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0); err != nil {
			return err
		}
	}
	refreshes := make([]alignedcmd.ItemSlotRefresh, 0, len(grant.ItemSlots))
	for _, slot := range grant.ItemSlots {
		if slot < 0 {
			continue
		}
		refreshes = append(refreshes, alignedcmd.ItemSlotRefresh{
			ListType:  dnfrepo.MainInventoryListType,
			SlotIndex: slot,
		})
	}
	if err := s.sendSelectedIncrementalItemSlotRefreshes(
		session,
		"dungeon-card-reward",
		refreshes,
	); err != nil {
		return err
	}
	if grant.OverflowMailID != "" {
		if err := s.sendMailboxAlarmToOnlineRecipient(session.selectedCharacterID); err != nil {
			// Reward and its mailbox fallback have already committed. A packet
			// delivery failure must not turn that durable result into a retryable
			// card selection; op96 will project it on the next mailbox open.
			s.logWarn("dungeon reward mailbox alarm deferred to next mailbox open",
				"character_id", session.selectedCharacterID,
				"mail_id", grant.OverflowMailID,
				"error", err)
		}
	}
	s.logGameEvent(session, "game-dungeon-card-reward-delivered",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"selection_result", result,
		"side", side,
		"member_index", memberIndex,
		"gold_before", grant.GoldBefore,
		"gold_after", grant.GoldAfter,
		"item_slots", grant.ItemSlots,
		"overflow_mail_id", grant.OverflowMailID,
		"source", source,
		"domain_source", "86jp_auto_flip_reward_delivery")
	return nil
}

func (runtime *runtimeDungeonState) refreshCurrentDungeonFreeCardReadiness() {
	if runtime == nil {
		return
	}
	runtime.settlementCardSelectionKnown = runtime.settlementCardSideSelectionKnown[dungeonCardSideFree]
	runtime.settlementCardSelectionSent = runtime.settlementCardSideSelectionSent[dungeonCardSideFree]
	runtime.settlementCardRewardCommitted = runtime.settlementCardSideRewardCommitted[dungeonCardSideFree]
}
