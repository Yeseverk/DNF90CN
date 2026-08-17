package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfbooster "longheng.io/server/internal/modules/dnf/booster"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func boosterGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketUseBoosterItem)
	lotteryOpcode := uint16(dnfenum.CmdPacketUseLotteryItem)
	overflowOpcode := uint16(dnfenum.CmdPacketOverflowInfo)
	handler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentBoosterItem(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name: "booster-item",
		LegacyHandlers: map[uint16]gameplayHandler{
			opcode: handler,
			lotteryOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentLotteryItem(session, request.Body)
			},
			overflowOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentLotteryOverflowConfirm(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(service, session, request, "game-current-booster-blocked", "current_exe_op160_command_class_mismatch") {
					return nil
				}
				return handler(service, session, request)
			},
			lotteryOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(service, session, request, "game-lottery-item-blocked", "current_exe_op27_command_class_mismatch") {
					return nil
				}
				return service.handleCurrentLotteryItem(session, request.Body)
			},
		},
	}
}

func (s *Service) handleCurrentBoosterItem(session *gameSession, body []byte) error {
	request, err := parseCurrentBoosterOpenRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-current-booster-rejected", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseBoosterItem), 4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		s.logGameEvent(session, "game-current-booster-rejected", "source_slot", request.SourceSlot, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseBoosterItem), 4)
	}
	definition, err := s.prepareCurrentBooster(ctx, session, catalog, request)
	if err != nil {
		s.logGameEvent(session, "game-current-booster-rejected",
			"source_slot", request.SourceSlot,
			"selected_item", request.SelectedItemID,
			"selected_count", len(request.Selections),
			"request_kind", request.Kind,
			"reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseBoosterItem), 4)
	}
	result, err := s.commitCurrentBooster(ctx, session, catalog, definition, request)
	if err != nil {
		s.logGameEvent(session, "game-current-booster-rejected",
			"source_slot", request.SourceSlot,
			"source_item", definition.Source.ItemID,
			"selected_item", request.SelectedItemID,
			"selected_count", len(request.Selections),
			"request_kind", request.Kind,
			"reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseBoosterItem), 4)
	}
	if err := s.sendGameUpperSuccess(
		session,
		uint16(dnfenum.CmdPacketUseBoosterItem),
		buildCurrentBoosterSuccessBody(result),
	); err != nil {
		return err
	}
	if err := s.sendSelectedIncrementalItemSlotRefreshes(
		session,
		"use_booster_item",
		[]alignedcmd.ItemSlotRefresh{{
			ListType:  dnfrepo.MainInventoryListType,
			SlotIndex: result.SourceSlot,
		}},
	); err != nil {
		return err
	}
	for _, listType := range result.ChangedLists {
		listBody, _, _, ok := s.buildCurrentItemListBodyForSession(ctx, session, listType)
		if !ok {
			return errCurrentBoosterOwnerUnavailable
		}
		if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), listBody); err != nil {
			return err
		}
	}
	s.logGameEvent(session, "game-current-booster-committed",
		"source_slot", result.SourceSlot,
		"source_item", result.SourceItemID,
		"source_remaining", result.SourceRemaining,
		"request_kind", request.Kind,
		"selected_item", request.SelectedItemID,
		"selected_count", len(request.Selections),
		"reward_count", len(result.Rewards),
		"rewards", fmt.Sprint(result.Rewards),
		"changed_lists", fmt.Sprint(result.ChangedLists),
		"ack_body", "current_exe_op160_u32_source_u16_slot_u32_remaining_u32_mode_u16_rewards",
		"inventory_refresh", "current_exe_op14_source_slot_then_op13_real_repository_lists")
	return nil
}

func (s *Service) commitCurrentBooster(
	ctx context.Context,
	session *gameSession,
	catalog *pvfDungeonDropCatalog,
	definition currentBoosterDefinition,
	request currentBoosterOpenRequest,
) (currentBoosterCommitResult, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || catalog == nil || catalog.source == nil || definition.Source.ItemID == 0 {
		return currentBoosterCommitResult{}, errCurrentBoosterOwnerUnavailable
	}
	rewardAmounts, rewardOptions, err := resolveCurrentBoosterRewardAmounts(definition, request, secureCurrentBoosterRoll)
	if err != nil {
		return currentBoosterCommitResult{}, err
	}
	if request.RewardMultiplier > 1 {
		for itemID, count := range rewardAmounts {
			multiplied := uint64(count) * uint64(request.RewardMultiplier)
			if multiplied > currentBoosterMaxRewardUnits {
				return currentBoosterCommitResult{}, errCurrentBoosterPVFInvalid
			}
			rewardAmounts[itemID] = uint32(multiplied)
		}
	}
	accountID, characterID, owner, err := s.currentBoosterMutationOwner(session)
	if err != nil {
		return currentBoosterCommitResult{}, err
	}
	now := time.Now().UTC()
	result := currentBoosterCommitResult{
		SourceItemID: definition.Source.ItemID,
		SourceSlot:   request.SourceSlot,
	}
	err = owner.Open(ctx, dnfbooster.Command{
		AccountID:   accountID,
		CharacterID: characterID,
		UpdatedAt:   now,
		Project: func(assets *dnfbooster.Assets) (dnfbooster.Changes, error) {
			inventory := assets.Inventory
			account := assets.AccountInventory
			sourceKey := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, request.SourceSlot)
			sourceStack, found := inventory.Slots[sourceKey]
			if !found || sourceStack.ItemID != int64(definition.Source.ItemID) || sourceStack.Count <= 0 || sourceStack.Count > math.MaxUint32 {
				return dnfbooster.Changes{}, errCurrentBoosterSourceMissing
			}
			if !definition.Source.ExpirationDate.IsZero() {
				sourceStack, _ = applyCurrentPVFItemExpirationAt(sourceStack, definition.Source, now)
			}
			if err := validateCurrentBoosterSourceExpiration(sourceStack, definition.Source, now); err != nil {
				return dnfbooster.Changes{}, err
			}
			characterDirty := false
			if request.ConsumePremiumDaily {
				if assets.Character == nil || !premium.TryConsumeDaily(assets.Character, request.PremiumDailySlot, now) {
					return dnfbooster.Changes{}, fmt.Errorf("premium slot %d daily limit reached", request.PremiumDailySlot)
				}
				characterDirty = true
			}
			rewards, err := resolveCurrentBoosterRewards(catalog, rewardAmounts, rewardOptions, sourceStack, now)
			if err != nil {
				return dnfbooster.Changes{}, err
			}

			materialKey := ""
			var materialItemID int64
			var materialCount int64
			if request.Kind == currentBoosterRequestRandom {
				materialItemID = definition.Random.MaterialItemID
				materialCount = definition.Random.MaterialCountPerUse
			}
			if materialItemID > 0 && materialCount > 0 {
				materialKey = findCurrentBoosterMaterial(inventory.Slots, materialItemID, materialCount, sourceKey)
				if materialKey == "" {
					return dnfbooster.Changes{}, fmt.Errorf("%w: material item=%d count=%d", errCurrentBoosterSourceMissing, materialItemID, materialCount)
				}
			}

			result.SourceRemaining = uint32(sourceStack.Count - 1)
			changedLists := map[byte]struct{}{dnfrepo.MainInventoryListType: {}}
			if sourceStack.Count == 1 {
				delete(inventory.Slots, sourceKey)
			} else {
				sourceStack.Count--
				entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, request.SourceSlot, sourceStack)
				sourceStack.RawEntry = append([]byte(nil), entry.data[:]...)
				inventory.Slots[sourceKey] = sourceStack
			}
			if materialKey != "" {
				materialStack := inventory.Slots[materialKey]
				materialStack.Count -= materialCount
				if materialStack.Count <= 0 {
					delete(inventory.Slots, materialKey)
				} else {
					listType, slot, parsed := parseSceneInventorySlotKey(materialKey)
					if !parsed || listType != dnfrepo.MainInventoryListType {
						return dnfbooster.Changes{}, errCurrentBoosterPVFInvalid
					}
					entry := currentItemListEntryFromStack(listType, slot, materialStack)
					materialStack.RawEntry = append([]byte(nil), entry.data[:]...)
					inventory.Slots[materialKey] = materialStack
				}
			}

			changedRewardKeys := make(map[string]struct{})
			accountDirty := false
			for _, reward := range rewards {
				keys, sharedChanged, grantErr := grantCurrentBoosterRewardByOwner(inventory, account, catalog.source, reward)
				if grantErr != nil {
					return dnfbooster.Changes{}, grantErr
				}
				accountDirty = accountDirty || sharedChanged
				for _, key := range keys {
					changedRewardKeys[key] = struct{}{}
				}
				result.Rewards = append(result.Rewards, currentBoosterGrantedReward{ItemID: reward.Definition.ItemID, Count: reward.Count})
			}
			for key := range changedRewardKeys {
				stack, found := inventory.Slots[key]
				if !found || stack.ItemID <= 0 {
					return dnfbooster.Changes{}, errCurrentBoosterPVFInvalid
				}
				if stack.Extra == nil {
					stack.Extra = make(map[string]string, 6)
				}
				stack.Extra["source"] = "booster_item"
				stack.Extra["last_grant_source"] = "booster_item"
				stack.Extra["booster_source_item"] = strconv.FormatUint(uint64(definition.Source.ItemID), 10)
				listType, slot, parsed := parseSceneInventorySlotKey(key)
				if !parsed {
					return dnfbooster.Changes{}, errCurrentBoosterPVFInvalid
				}
				entry := currentItemListEntryFromStack(listType, slot, stack)
				stack.RawEntry = append([]byte(nil), entry.data[:]...)
				inventory.Slots[key] = stack
				changedLists[listType] = struct{}{}
			}
			result.ChangedLists = make([]byte, 0, len(changedLists))
			for listType := range changedLists {
				result.ChangedLists = append(result.ChangedLists, listType)
			}
			sort.Slice(result.ChangedLists, func(i, j int) bool { return result.ChangedLists[i] < result.ChangedLists[j] })
			return dnfbooster.Changes{AccountInventory: accountDirty, Character: characterDirty, Inventory: true}, nil
		},
	})
	if err != nil {
		return currentBoosterCommitResult{}, currentBoosterMutationError(err)
	}
	return result, nil
}

func (s *Service) currentBoosterMutationOwner(session *gameSession) (string, string, *dnfbooster.Owner, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return "", "", nil, errCurrentBoosterOwnerUnavailable
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.AccountAssets == nil {
		return "", "", nil, errCurrentBoosterOwnerUnavailable
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	if accountID == "" {
		return "", "", nil, errCurrentBoosterOwnerUnavailable
	}
	owner, err := dnfbooster.NewOwner(repositories)
	if err != nil {
		return "", "", nil, errCurrentBoosterOwnerUnavailable
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	return accountID, characterID, owner, nil
}

func currentBoosterMutationError(err error) error {
	switch {
	case errors.Is(err, dnfbooster.ErrOwnerUnavailable),
		errors.Is(err, dnfbooster.ErrAccountRequired),
		errors.Is(err, dnfbooster.ErrCharacterRequired),
		errors.Is(err, dnfbooster.ErrCharacterNotFound),
		errors.Is(err, dnfbooster.ErrAccountMismatch),
		errors.Is(err, dnfbooster.ErrInventoryNotFound):
		return errors.Join(errCurrentBoosterOwnerUnavailable, err)
	default:
		return err
	}
}

// prepareCurrentBooster is a read-only fast-fail pass. The domain Owner
// repeats authorization and inventory validation inside the commit
// transaction before consuming anything.
func (s *Service) prepareCurrentBooster(
	ctx context.Context,
	session *gameSession,
	catalog *pvfDungeonDropCatalog,
	request currentBoosterOpenRequest,
) (currentBoosterDefinition, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || request.SourceSlot < 0 || catalog == nil || catalog.source == nil {
		return currentBoosterDefinition{}, errCurrentBoosterOwnerUnavailable
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || repositories.Inventory == nil {
		return currentBoosterDefinition{}, errCurrentBoosterOwnerUnavailable
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return currentBoosterDefinition{}, err
	}
	if !found || character.CharacterID != characterID || strings.TrimSpace(character.AccountID) != strings.TrimSpace(s.accountIDForSession(session)) {
		return currentBoosterDefinition{}, errCurrentBoosterOwnerUnavailable
	}
	inventory, found, err := repositories.Inventory.Load(ctx, characterID)
	if err != nil {
		return currentBoosterDefinition{}, err
	}
	if !found || inventory.CharacterID != characterID {
		return currentBoosterDefinition{}, errCurrentBoosterOwnerUnavailable
	}
	stack, found := inventory.Slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, request.SourceSlot)]
	if !found || stack.ItemID <= 0 || stack.ItemID > math.MaxUint32 || stack.Count <= 0 || stack.Count > math.MaxUint32 {
		return currentBoosterDefinition{}, errCurrentBoosterSourceMissing
	}
	definition, err := resolveCurrentBoosterDefinition(catalog, uint32(stack.ItemID), request.Kind)
	if err != nil {
		return currentBoosterDefinition{}, err
	}
	if err := validateCurrentBoosterSourceExpiration(stack, definition.Source, time.Now().UTC()); err != nil {
		return currentBoosterDefinition{}, err
	}
	if request.Kind == currentBoosterRequestSelection {
		if _, err := currentBoosterSelectedCandidates(definition, request); err != nil {
			return currentBoosterDefinition{}, err
		}
	}
	return definition, nil
}
