package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfexpertjob "longheng.io/server/internal/modules/dnf/expertjob"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentExpertJobCompoundRequestSize   = 8
	currentExpertJobExtractionRequestSize = 6
)

type currentExpertJobCompoundRequest struct {
	RecipeItemID int64
	Count        int
	CardSlot     int16
}

type currentExpertJobExtractionRequest struct {
	ExtractorType byte
	ExtractorSlot int16
	TargetList    byte
	TargetSlot    int16
}

type currentExpertJobCompoundResult struct {
	Plan    dnfexpertjob.CompoundPlan
	JobType byte
}

type currentExpertJobExtractionMaterial struct {
	Slot   int16
	ItemID uint32
	Count  uint32
}

type currentExpertJobExtractionResult struct {
	JobType      byte
	TargetList   byte
	TargetSlot   int16
	Materials    []currentExpertJobExtractionMaterial
	LevelChanged bool
}

func expertJobGameplayModule() gameplayModuleDefinition {
	compound := uint16(dnfenum.CmdPacketCompoundItemByExpertJob)
	extraction := uint16(dnfenum.CmdPacketExpertExtraction)
	giveUp := uint16(dnfenum.CmdPacketGiveupExpertJob)
	handleCompound := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentExpertJobCompound(session, request.Body)
	}
	handleExtraction := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentExpertJobExtraction(session, request.Body)
	}
	module := gameplayModuleDefinition{
		Name: "expert-job",
		LegacyHandlers: map[uint16]gameplayHandler{
			compound:   handleCompound,
			extraction: handleExtraction,
			giveUp: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentExpertJobGiveUp(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			compound: defaultClassGameplayHandler(
				"game-expert-job-compound-blocked",
				"current_exe_op238_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentExpertJobCompound(session, body)
				},
			),
			extraction: defaultClassGameplayHandler(
				"game-expert-job-extraction-blocked",
				"current_exe_op416_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentExpertJobExtraction(session, body)
				},
			),
			giveUp: defaultClassGameplayHandler(
				"game-expert-job-give-up-blocked",
				"current_exe_op239_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentExpertJobGiveUp(session, body)
				},
			),
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			compound: func(body []byte) []byte {
				return stripLegacyTransportTrailer(body, currentExpertJobCompoundRequestSize)
			},
			extraction: func(body []byte) []byte {
				return stripLegacyTransportTrailer(body, currentExpertJobExtractionRequestSize)
			},
			giveUp: func(body []byte) []byte {
				return stripLegacyTransportTrailer(body, 0)
			},
		},
	}
	registerStore := func(opcode uint16, handler func(*Service, *gameSession, uint16, []byte) error) {
		captured := opcode
		module.LegacyHandlers[captured] = func(service *Service, session *gameSession, request gameplayRequest) error {
			return handler(service, session, captured, request.Body)
		}
		module.UpperHandlers[captured] = defaultClassGameplayHandler(
			"game-expert-job-store-blocked",
			"current_exe_expert_job_store_command_class_mismatch",
			func(service *Service, session *gameSession, body []byte) error {
				return handler(service, session, captured, body)
			},
		)
	}
	registerStore(uint16(dnfenum.CmdPacketCreateExpertJobStore), (*Service).handleCurrentExpertJobStoreCreate)
	registerStore(uint16(dnfenum.CmdPacketEnterExpertJobStore), (*Service).handleCurrentExpertJobStoreEnter)
	registerStore(uint16(dnfenum.CmdPacketCloseExpertJobStore), (*Service).handleCurrentExpertJobStoreClose)
	registerStore(uint16(dnfenum.CmdPacketRepairDisjointMachine), (*Service).handleCurrentExpertJobStoreRepair)
	registerStore(uint16(dnfenum.CmdPacketRepairExpertJobStore), (*Service).handleCurrentExpertJobStoreRepair)
	registerStore(uint16(dnfenum.CmdPacketUpgradeDisjointMachine), (*Service).handleCurrentExpertJobStoreUpgrade)
	registerStore(uint16(dnfenum.CmdPacketRequestDisjointItem), (*Service).handleCurrentExpertJobStoreDisjoint)
	registerStore(uint16(dnfenum.CmdPacketUseEnchantStore), (*Service).handleCurrentExpertJobStoreEnchant)
	return module
}

func parseCurrentExpertJobCompoundRequest(body []byte) (currentExpertJobCompoundRequest, error) {
	if len(body) != currentExpertJobCompoundRequestSize {
		return currentExpertJobCompoundRequest{}, fmt.Errorf("expert job compound body=%d", len(body))
	}
	request := currentExpertJobCompoundRequest{
		RecipeItemID: int64(int32(binary.LittleEndian.Uint32(body[0:4]))),
		Count:        int(binary.LittleEndian.Uint16(body[4:6])),
		CardSlot:     int16(binary.LittleEndian.Uint16(body[6:8])),
	}
	if request.RecipeItemID <= 0 || request.Count <= 0 || request.CardSlot < -1 || (request.CardSlot >= 0 && request.Count != 1) {
		return currentExpertJobCompoundRequest{}, errors.New("expert job compound request is invalid")
	}
	return request, nil
}

func parseCurrentExpertJobExtractionRequest(body []byte) (currentExpertJobExtractionRequest, error) {
	if len(body) != currentExpertJobExtractionRequestSize {
		return currentExpertJobExtractionRequest{}, fmt.Errorf("expert job extraction body=%d", len(body))
	}
	request := currentExpertJobExtractionRequest{
		ExtractorType: body[0],
		ExtractorSlot: int16(binary.LittleEndian.Uint16(body[1:3])),
		TargetList:    body[3],
		TargetSlot:    int16(binary.LittleEndian.Uint16(body[4:6])),
	}
	if (request.ExtractorType != dnfexpertjob.EnchanterType && request.ExtractorType != dnfexpertjob.AlchemistType && request.ExtractorType != dnfexpertjob.DollControllerType) ||
		request.ExtractorSlot < 0 || request.TargetList != dnfrepo.MainInventoryListType ||
		request.TargetSlot < 0 || request.ExtractorSlot == request.TargetSlot {
		return currentExpertJobExtractionRequest{}, errors.New("expert job extraction request is invalid")
	}
	return request, nil
}

func (s *Service) handleCurrentExpertJobCompound(session *gameSession, body []byte) error {
	request, err := parseCurrentExpertJobCompoundRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-expert-job-compound-rejected", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketCompoundItemByExpertJob), 19)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := s.commitCurrentExpertJobCompound(ctx, session, request)
	if err != nil {
		code := currentExpertJobCompoundError(err)
		s.logGameEvent(session, "game-expert-job-compound-rejected", "recipe", request.RecipeItemID, "count", request.Count, "error", err, "error_code", code)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketCompoundItemByExpertJob), code)
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketCompoundItemByExpertJob), buildCurrentExpertJobCompoundSuccessBody(result.Plan), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	if err := s.sendSelectedCurrentContainerListsRefresh(session, "expert_job_compound_after_ack"); err != nil {
		return err
	}
	if result.Plan.LevelChanged {
		if err := s.sendCurrentExpertJobInfoFromRepository(session, result.JobType, true); err != nil {
			return err
		}
	}
	s.logGameEvent(session, "game-expert-job-compound-committed", "job_type", result.JobType, "recipe", request.RecipeItemID, "attempts", request.Count, "successes", result.Plan.SuccessCount, "failures", result.Plan.FailureCount, "experience_gain", result.Plan.ExperienceGain)
	return nil
}

func (s *Service) handleCurrentExpertJobExtraction(session *gameSession, body []byte) error {
	request, err := parseCurrentExpertJobExtractionRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-expert-job-extraction-rejected", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketExpertExtraction), 19)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := s.commitCurrentExpertJobExtraction(ctx, session, request)
	if err != nil {
		code := currentExpertJobExtractionError(err)
		s.logGameEvent(session, "game-expert-job-extraction-rejected", "extractor_type", request.ExtractorType, "extractor_slot", request.ExtractorSlot, "target_slot", request.TargetSlot, "error", err, "error_code", code)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketExpertExtraction), code)
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketExpertExtraction), buildCurrentExpertJobExtractionSuccessBody(result), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	if err := s.sendSelectedCurrentContainerListsRefresh(session, "expert_job_extraction_after_ack"); err != nil {
		return err
	}
	if result.LevelChanged {
		if err := s.sendCurrentExpertJobInfoFromRepository(session, result.JobType, true); err != nil {
			return err
		}
	}
	s.logGameEvent(session, "game-expert-job-extraction-committed", "job_type", result.JobType, "extractor_slot", request.ExtractorSlot, "target_slot", request.TargetSlot, "materials", len(result.Materials))
	return nil
}

func (s *Service) commitCurrentExpertJobCompound(ctx context.Context, session *gameSession, request currentExpertJobCompoundRequest) (currentExpertJobCompoundResult, error) {
	jobCatalog, items, owner, accountID, characterID, err := s.currentExpertJobMutationContext(session)
	if err != nil {
		return currentExpertJobCompoundResult{}, err
	}
	var result currentExpertJobCompoundResult
	err = owner.Compound(ctx, dnfexpertjob.Command{AccountID: accountID, CharacterID: characterID, UpdatedAt: s.gameplayNow(), Project: func(assets *dnfexpertjob.Assets) (dnfexpertjob.Changes, error) {
		jobType, experience, err := currentExpertJobCharacterState(assets.Character)
		if err != nil {
			return dnfexpertjob.Changes{}, err
		}
		if request.CardSlot >= 0 {
			if jobType != dnfexpertjob.EnchanterType {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrJobUnsupported
			}
			cardKey := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, request.CardSlot)
			card, found := assets.Inventory.Slots[cardKey]
			if !found || card.ItemID <= 0 || card.Count <= 0 || currentNPCShopItemLocked(card) {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrRecipeUnavailable
			}
			beadPlan, planErr := jobCatalog.PlanEnchanterBead(experience, request.RecipeItemID, card.ItemID, rand.IntN)
			if planErr != nil {
				return dnfexpertjob.Changes{}, planErr
			}
			upgradeCount := currentExpertJobEnchantUpgradeCount(card)
			card.Count--
			if card.Count == 0 {
				delete(assets.Inventory.Slots, cardKey)
			} else {
				entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, request.CardSlot, card)
				card.RawEntry = append([]byte(nil), entry.data[:]...)
				assets.Inventory.Slots[cardKey] = card
			}
			if consumeErr := consumeCurrentExpertJobRecipe(assets.Inventory, beadPlan.Recipe.Materials, 0); consumeErr != nil {
				return dnfexpertjob.Changes{}, consumeErr
			}
			compoundPlan := dnfexpertjob.CompoundPlan{
				FinalExperience: beadPlan.FinalExperience,
				ExperienceGain:  beadPlan.ExperienceGain,
				LevelChanged:    beadPlan.LevelChanged,
			}
			if beadPlan.Success {
				if beadPlan.BeadItemID <= 0 || beadPlan.BeadItemID > math.MaxUint32 {
					return dnfexpertjob.Changes{}, dnfexpertjob.ErrRecipeUnavailable
				}
				definition, resolveErr := items.ResolveItem(uint32(beadPlan.BeadItemID))
				if resolveErr != nil || definition.Kind != dungeonDropItemStackable {
					return dnfexpertjob.Changes{}, dnfexpertjob.ErrRecipeUnavailable
				}
				slots, grantErr := grantCurrentCeraShopProduct(assets.Inventory, definition, 1)
				if grantErr != nil || len(slots) != 1 {
					if grantErr != nil {
						return dnfexpertjob.Changes{}, grantErr
					}
					return dnfexpertjob.Changes{}, dnfexpertjob.ErrRecipeUnavailable
				}
				grantedKey := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, int16(slots[0]))
				granted := assets.Inventory.Slots[grantedKey]
				currentExpertJobSetEnchantUpgradeCount(&granted, upgradeCount)
				assets.Inventory.Slots[grantedKey] = granted
				compoundPlan.AttemptedOutputs = []dnfexpertjob.RecipeEntry{{ItemID: beadPlan.BeadItemID, Count: 1}}
				compoundPlan.Rewards = append([]dnfexpertjob.RecipeEntry(nil), compoundPlan.AttemptedOutputs...)
				compoundPlan.SuccessCount = 1
			} else {
				compoundPlan.FailureCount = 1
			}
			if assets.Character.Stats == nil {
				assets.Character.Stats = make(map[string]int64)
			}
			assets.Character.Stats["expert_job_exp"] = beadPlan.FinalExperience
			result = currentExpertJobCompoundResult{Plan: compoundPlan, JobType: jobType}
			return dnfexpertjob.Changes{Character: beadPlan.ExperienceGain != 0, Inventory: true}, nil
		}
		learned := assets.Character.Stats[currentExpertJobRecipeStatKey(jobType, request.RecipeItemID)] != 0
		plan, err := jobCatalog.PlanCompoundWithLearned(jobType, experience, request.RecipeItemID, request.Count, learned, rand.IntN)
		if err != nil {
			return dnfexpertjob.Changes{}, err
		}
		if err := consumeCurrentExpertJobRecipe(assets.Inventory, plan.Materials, plan.GoldCost); err != nil {
			return dnfexpertjob.Changes{}, err
		}
		for _, reward := range plan.Rewards {
			if reward.ItemID <= 0 || reward.ItemID > math.MaxUint32 || reward.Count <= 0 || reward.Count > math.MaxUint32 {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrRecipeUnavailable
			}
			definition, resolveErr := items.ResolveItem(uint32(reward.ItemID))
			if resolveErr != nil || definition.Kind != dungeonDropItemStackable {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrRecipeUnavailable
			}
			if _, grantErr := grantCurrentCeraShopProduct(assets.Inventory, definition, uint32(reward.Count)); grantErr != nil {
				return dnfexpertjob.Changes{}, grantErr
			}
		}
		if assets.Character.Stats == nil {
			assets.Character.Stats = make(map[string]int64)
		}
		assets.Character.Stats["expert_job_exp"] = plan.FinalExperience
		result = currentExpertJobCompoundResult{Plan: plan, JobType: jobType}
		return dnfexpertjob.Changes{Character: plan.ExperienceGain != 0, Inventory: true}, nil
	}})
	return result, err
}

func (s *Service) commitCurrentExpertJobExtraction(ctx context.Context, session *gameSession, request currentExpertJobExtractionRequest) (currentExpertJobExtractionResult, error) {
	jobCatalog, items, owner, accountID, characterID, err := s.currentExpertJobMutationContext(session)
	if err != nil {
		return currentExpertJobExtractionResult{}, err
	}
	var result currentExpertJobExtractionResult
	err = owner.Extract(ctx, dnfexpertjob.Command{AccountID: accountID, CharacterID: characterID, UpdatedAt: s.gameplayNow(), Project: func(assets *dnfexpertjob.Assets) (dnfexpertjob.Changes, error) {
		jobType, experience, err := currentExpertJobCharacterState(assets.Character)
		if err != nil || jobType != request.ExtractorType {
			return dnfexpertjob.Changes{}, dnfexpertjob.ErrJobUnsupported
		}
		extractor, extractorFound := assets.Inventory.Slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, request.ExtractorSlot)]
		targetKey := currentCeraShopInventorySlotKey(request.TargetList, request.TargetSlot)
		target, targetFound := assets.Inventory.Slots[targetKey]
		if !extractorFound || extractor.ItemID <= 0 || currentNPCShopItemLocked(extractor) || !targetFound || target.ItemID <= 0 || target.ItemID > math.MaxUint32 || target.Count != 1 || currentNPCShopItemLocked(target) {
			return dnfexpertjob.Changes{}, dnfexpertjob.ErrExtractionInvalid
		}
		metadata, err := jobCatalog.Equipment(target.ItemID)
		if err != nil {
			return dnfexpertjob.Changes{}, err
		}
		if metadata.DisjointForbidden || metadata.AttachType == "trade delete" {
			return dnfexpertjob.Changes{}, dnfexpertjob.ErrExtractionInvalid
		}
		metadata.State = currentExpertJobEquipmentState(target)
		plan, err := jobCatalog.PlanExtraction(jobType, experience, extractor.ItemID, metadata, rand.IntN)
		if err != nil {
			return dnfexpertjob.Changes{}, err
		}
		delete(assets.Inventory.Slots, targetKey)
		for _, reward := range plan.Materials {
			if reward.ItemID <= 0 || reward.ItemID > math.MaxUint32 || reward.Count <= 0 || reward.Count > math.MaxUint32 {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrExtractionInvalid
			}
			definition, resolveErr := items.ResolveItem(uint32(reward.ItemID))
			if resolveErr != nil || definition.Kind != dungeonDropItemStackable {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrExtractionInvalid
			}
			slots, grantErr := grantCurrentCeraShopProduct(assets.Inventory, definition, uint32(reward.Count))
			if grantErr != nil {
				return dnfexpertjob.Changes{}, grantErr
			}
			if len(slots) == 0 {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrExtractionInvalid
			}
			result.Materials = append(result.Materials, currentExpertJobExtractionMaterial{
				Slot: int16(slots[0]), ItemID: uint32(reward.ItemID), Count: uint32(reward.Count),
			})
		}
		if assets.Character.Stats == nil {
			assets.Character.Stats = make(map[string]int64)
		}
		assets.Character.Stats["expert_job_exp"] = plan.FinalExperience
		result.JobType = jobType
		result.TargetList = request.TargetList
		result.TargetSlot = request.TargetSlot
		result.LevelChanged = plan.LevelChanged
		sort.Slice(result.Materials, func(i, j int) bool { return result.Materials[i].Slot < result.Materials[j].Slot })
		return dnfexpertjob.Changes{Character: plan.ExperienceGain != 0, Inventory: true}, nil
	}})
	return result, err
}

func (s *Service) currentExpertJobMutationContext(session *gameSession) (*dnfexpertjob.Catalog, *pvfDungeonDropCatalog, *dnfexpertjob.Owner, string, string, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return nil, nil, nil, "", "", dnfexpertjob.ErrOwnerUnavailable
	}
	jobCatalog, err := s.currentExpertJobCatalog()
	if err != nil {
		return nil, nil, nil, "", "", err
	}
	items, err := s.currentPVFItemCatalog()
	if err != nil {
		return nil, nil, nil, "", "", err
	}
	repositories, ok := s.repositoryGroup()
	if !ok {
		return nil, nil, nil, "", "", dnfexpertjob.ErrOwnerUnavailable
	}
	owner, err := dnfexpertjob.NewOwner(repositories)
	if err != nil {
		return nil, nil, nil, "", "", err
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	if accountID == "" {
		return nil, nil, nil, "", "", dnfexpertjob.ErrOwnerUnavailable
	}
	return jobCatalog, items, owner, accountID, characterID, nil
}

func currentExpertJobCharacterState(character *dnfrepo.CharacterRecord) (byte, int64, error) {
	if character == nil {
		return 0, 0, dnfexpertjob.ErrCharacterNotFound
	}
	rawJobType := character.Stats["expert_job_type"]
	if rawJobType < int64(dnfexpertjob.EnchanterType) || rawJobType > int64(dnfexpertjob.DollControllerType) {
		return 0, 0, dnfexpertjob.ErrJobUnsupported
	}
	jobType := byte(rawJobType)
	experience := character.Stats["expert_job_exp"]
	if experience < 0 {
		experience = 0
	}
	return jobType, experience, nil
}

func consumeCurrentExpertJobRecipe(inventory *dnfrepo.InventoryRecord, materials []dnfexpertjob.RecipeEntry, gold int64) error {
	if inventory == nil {
		return dnfexpertjob.ErrInventoryNotFound
	}
	required := make(map[int64]int64, len(materials))
	for _, material := range materials {
		if material.ItemID <= 0 || material.Count <= 0 || required[material.ItemID] > math.MaxInt64-material.Count {
			return dnfexpertjob.ErrRecipeUnavailable
		}
		required[material.ItemID] += material.Count
	}
	if gold < 0 {
		return dnfexpertjob.ErrRecipeUnavailable
	}
	if gold > 0 {
		wallet, found := inventory.Slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, 0)]
		if !found || wallet.ItemID != 0 || wallet.Count < gold {
			return errCurrentExpertJobInsufficientMaterials
		}
	}
	type candidate struct {
		key  string
		slot int16
	}
	candidates := make(map[int64][]candidate, len(required))
	available := make(map[int64]int64, len(required))
	for key, stack := range inventory.Slots {
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if !ok || listType != dnfrepo.MainInventoryListType || slot <= 0 || stack.Count <= 0 || currentNPCShopItemLocked(stack) {
			continue
		}
		if _, needed := required[stack.ItemID]; !needed {
			continue
		}
		available[stack.ItemID] += stack.Count
		candidates[stack.ItemID] = append(candidates[stack.ItemID], candidate{key, slot})
	}
	for itemID, count := range required {
		if available[itemID] < count {
			return errCurrentExpertJobInsufficientMaterials
		}
	}
	for itemID, count := range required {
		sort.Slice(candidates[itemID], func(i, j int) bool { return candidates[itemID][i].slot < candidates[itemID][j].slot })
		remaining := count
		for _, candidate := range candidates[itemID] {
			stack := inventory.Slots[candidate.key]
			consumed := min(remaining, stack.Count)
			stack.Count -= consumed
			remaining -= consumed
			if stack.Count == 0 {
				delete(inventory.Slots, candidate.key)
			} else {
				entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, candidate.slot, stack)
				stack.RawEntry = append([]byte(nil), entry.data[:]...)
				inventory.Slots[candidate.key] = stack
			}
			if remaining == 0 {
				break
			}
		}
	}
	if gold > 0 {
		key := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, 0)
		wallet := inventory.Slots[key]
		wallet.Count -= gold
		inventory.Slots[key] = wallet
	}
	return nil
}

func currentExpertJobEquipmentState(stack dnfrepo.ItemStack) int {
	if sceneInventoryExtraUint32(stack.Extra, "chronicle_option_count", "chronicle_count") > 0 {
		return 2
	}
	if sceneInventoryExtraByte(stack.Extra, "amplify_type", "amplification_type", "byte_13", "value_13", "value_c")&0x80 != 0 {
		return 1
	}
	return 0
}

func currentExpertJobEnchantUpgradeCount(stack dnfrepo.ItemStack) byte {
	if value := sceneInventoryExtraByte(stack.Extra, "byte_12", "value_12", "enchant_upgrade_count"); value != 0 {
		return value
	}
	if len(stack.RawEntry) > 0x12 {
		return stack.RawEntry[0x12]
	}
	return 0
}

func currentExpertJobSetEnchantUpgradeCount(stack *dnfrepo.ItemStack, value byte) {
	if stack == nil {
		return
	}
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 2)
	}
	stack.Extra["byte_12"] = strconv.Itoa(int(value))
	stack.Extra["enchant_upgrade_count"] = strconv.Itoa(int(value))
	if len(stack.RawEntry) == currentItemListEntryWireSize {
		stack.RawEntry = append([]byte(nil), stack.RawEntry...)
		stack.RawEntry[0x12] = value
	}
}

var errCurrentExpertJobInsufficientMaterials = errors.New("expert job materials are insufficient")

func currentExpertJobCompoundError(err error) byte {
	switch {
	case errors.Is(err, errCurrentExpertJobInsufficientMaterials):
		return 21
	case errors.Is(err, dnfexpertjob.ErrLevelTooLow):
		return 14
	case errors.Is(err, errDungeonPickupInventoryFull), errors.Is(err, errCurrentDisjointRewardInvalid):
		return 4
	default:
		return 19
	}
}

func currentExpertJobExtractionError(err error) byte {
	switch {
	case errors.Is(err, errDungeonPickupInventoryFull), errors.Is(err, errCurrentDisjointRewardInvalid):
		return 4
	case errors.Is(err, dnfexpertjob.ErrExtractorInvalid), errors.Is(err, dnfexpertjob.ErrExtractionInvalid):
		return 13
	default:
		return 19
	}
}

func buildCurrentExpertJobCompoundSuccessBody(plan dnfexpertjob.CompoundPlan) []byte {
	w := packetWriter{}
	w.writeByte(1)
	w.writeByte(byte(len(plan.AttemptedOutputs)))
	for _, output := range plan.AttemptedOutputs {
		w.writeUint32(uint32(output.ItemID))
		w.writeUint32(uint32(output.Count))
	}
	w.writeUint32(uint32(plan.SuccessCount))
	w.writeUint32(uint32(plan.FailureCount))
	w.writeByte(0)
	return w.bytes()
}

func buildCurrentExpertJobExtractionSuccessBody(result currentExpertJobExtractionResult) []byte {
	w := packetWriter{}
	w.writeByte(1)
	w.writeByte(result.TargetList)
	w.writeUint16(uint16(result.TargetSlot))
	w.writeByte(byte(len(result.Materials)))
	for _, material := range result.Materials {
		w.writeUint16(uint16(material.Slot))
		w.writeUint32(material.ItemID)
		w.writeUint32(material.Count)
	}
	return w.bytes()
}
