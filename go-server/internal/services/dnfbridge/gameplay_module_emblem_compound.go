package dnfbridge

import (
	"context"
	"math"
	"strings"

	dnfdisjoint "longheng.io/server/internal/modules/dnf/disjoint"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func emblemCompoundGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketCompoundEmblem)
	return gameplayModuleDefinition{
		Name: "emblem-compound",
		LegacyHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentEmblemCompound(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(service, session, request, "game-emblem-compound-blocked", "current_exe_op256_command_class_mismatch") {
					return nil
				}
				return service.handleCurrentEmblemCompound(session, request.Body)
			},
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			opcode: stripLegacyEmblemCompoundTransportTrailer,
		},
	}
}

func (s *Service) handleCurrentEmblemCompound(session *gameSession, body []byte) error {
	request, err := parseCurrentEmblemCompoundRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-emblem-compound-rejected", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketCompoundEmblem), 4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	catalog, err := s.currentPVFItemCatalog()
	var result currentDisjointResult
	if err == nil {
		result, err = s.commitCurrentEmblemCompound(ctx, session, catalog, request)
	}
	if err != nil {
		s.logGameEvent(session, "game-emblem-compound-rejected", "inputs", len(request.Inputs), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketCompoundEmblem), 4)
	}
	if len(result.Rewards) != 1 {
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketCompoundEmblem), 4)
	}
	// This ordering is the observed 86JP result path: changed main-list rows,
	// then the op256 result popup. The body itself is not replayed; item and
	// count come from the committed runtime-PVF roll.
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), buildCurrentItemUpdateBody(0, result.Updates), 0); err != nil {
		return err
	}
	reward := result.Rewards[0]
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketCompoundEmblem), buildCurrentEmblemCompoundSuccessBody(reward), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	// 86JP order is op14 update -> op256 ACK -> full ITEM_LIST(Main); the
	// trailing snapshot is what repaints the emblem page on the client.
	listCtx, listCancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	listBody, _, _, listOK := s.buildCurrentItemListBodyForSession(listCtx, session, dnfrepo.MainInventoryListType)
	listCancel()
	if !listOK {
		s.logGameEvent(session, "game-emblem-compound-refresh-skipped", "reason", "item_list_unavailable")
		return nil
	}
	if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), listBody); err != nil {
		return err
	}
	s.logGameEvent(session, "game-emblem-compound-committed", "inputs", len(request.Inputs), "reward_item", reward.ItemID, "reward_granted", reward.Granted, "reward_slot", reward.Slot, "refresh", "class0_op14+full_item_list")
	return nil
}

func (s *Service) commitCurrentEmblemCompound(ctx context.Context, session *gameSession, catalog *pvfDungeonDropCatalog, request currentEmblemCompoundRequest) (currentDisjointResult, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || catalog == nil || catalog.source == nil {
		return currentDisjointResult{}, errCurrentDisjointUnavailable
	}
	config, err := loadCurrentDisjointPVFConfig(catalog.source)
	if err != nil {
		return currentDisjointResult{}, err
	}
	accountID, characterID, owner, err := s.currentDisjointMutationOwner(session)
	if err != nil {
		return currentDisjointResult{}, err
	}
	type consumption struct {
		itemID uint32
		count  int64
	}
	consumedBySlot := make(map[int16]consumption, len(request.Inputs))
	for _, input := range request.Inputs {
		consumed, exists := consumedBySlot[input.Slot]
		if exists && consumed.itemID != input.ItemID {
			return currentDisjointResult{}, errCurrentDisjointRequestInvalid
		}
		consumed.itemID = input.ItemID
		consumed.count++
		consumedBySlot[input.Slot] = consumed
	}

	var result currentDisjointResult
	err = owner.CompoundEmblems(ctx, dnfdisjoint.Command{
		AccountID:   accountID,
		CharacterID: characterID,
		Project: func(assets *dnfdisjoint.Assets) (dnfdisjoint.Changes, error) {
			account := assets.AccountInventory
			inventory := assets.Inventory
			minimumGrade := math.MaxInt
			for slot, consumed := range consumedBySlot {
				stack, exists := inventory.Slots[currentCeraShopInventorySlotKey(0, slot)]
				if !exists || stack.ItemID != int64(consumed.itemID) || stack.Count < consumed.count || currentNPCShopItemLocked(stack) {
					return dnfdisjoint.Changes{}, errCurrentDisjointSourceInvalid
				}
				definition, resolveErr := catalog.ResolveItem(consumed.itemID)
				if resolveErr != nil || definition.Kind != dungeonDropItemStackable {
					return dnfdisjoint.Changes{}, errCurrentDisjointSourceInvalid
				}
				document, documentErr := parseDungeonCardPVFDocument(catalog.source, definition.PVFPath)
				if documentErr != nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(documentText(document, "stackable type"))), "[avatar emblem]") {
					return dnfdisjoint.Changes{}, errCurrentDisjointSourceInvalid
				}
				grade, gradeFound := document.Int("grade")
				if !gradeFound || grade < 1 || grade > math.MaxInt16 {
					return dnfdisjoint.Changes{}, errCurrentDisjointSourceInvalid
				}
				if int(grade) < minimumGrade {
					minimumGrade = int(grade)
				}
			}
			boosterID, found := config.EmblemBoosters[[2]int{minimumGrade, len(request.Inputs)}]
			if !found || boosterID == 0 {
				return dnfdisjoint.Changes{}, errCurrentDisjointRewardInvalid
			}
			reward, rewardErr := currentRollEmblemBoosterReward(catalog, boosterID)
			if rewardErr != nil {
				return dnfdisjoint.Changes{}, rewardErr
			}
			changedSlots := make(map[int16]struct{}, len(consumedBySlot)+1)
			for slot, consumed := range consumedBySlot {
				key := currentCeraShopInventorySlotKey(0, slot)
				stack := inventory.Slots[key]
				remaining := stack.Count - consumed.count
				if remaining == 0 {
					delete(inventory.Slots, key)
				} else {
					stack.Count = remaining
					entry := currentItemListEntryFromStack(0, slot, stack)
					stack.RawEntry = append([]byte(nil), entry.data[:]...)
					inventory.Slots[key] = stack
				}
				changedSlots[slot] = struct{}{}
			}
			rewardSlots, accountDirty, grantErr := currentGrantDisjointRewards(inventory, account, catalog, []currentDisjointReward{reward})
			if grantErr != nil || len(rewardSlots) != 1 {
				if grantErr != nil {
					return dnfdisjoint.Changes{}, grantErr
				}
				return dnfdisjoint.Changes{}, errCurrentDisjointRewardInvalid
			}
			rewardSlot := rewardSlots[0]
			changedSlots[rewardSlot.Slot] = struct{}{}
			for slot := range changedSlots {
				stack, exists := inventory.Slots[currentCeraShopInventorySlotKey(0, slot)]
				if exists {
					result.Updates = append(result.Updates, currentItemListEntryFromStack(0, slot, stack))
					continue
				}
				var removed currentItemListEntry
				removed.patchCore(slot, math.MaxUint32, 0)
				result.Updates = append(result.Updates, removed)
			}
			sortCurrentItemListEntries(result.Updates)
			result.Rewards = rewardSlots
			return dnfdisjoint.Changes{AccountInventory: accountDirty, Inventory: true}, nil
		},
	})
	if err != nil {
		return currentDisjointResult{}, currentDisjointMutationError(err)
	}
	return result, nil
}
