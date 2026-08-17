package dnfbridge

import (
	"context"
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfexpertjob "longheng.io/server/internal/modules/dnf/expertjob"
	dnfinventory "longheng.io/server/internal/modules/dnf/inventory"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type currentExpertJobRecipeLearningResult struct {
	JobType   byte
	RecipeID  int64
	Remaining int64
}

func (s *Service) handleCurrentExpertJobRecipeLearning(session *gameSession, body []byte) (bool, error) {
	request, err := dnfinventory.DecodeUseStackableRequest(body)
	if err != nil {
		return false, nil
	}
	catalog, err := s.currentExpertJobCatalog()
	if err != nil {
		s.logGameEvent(session, "game-expert-job-recipe-catalog-unavailable", "error", err)
		return false, nil
	}
	jobType, _, recipe := catalog.RecipeJob(int64(request.ItemCode))
	if !recipe {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := s.commitCurrentExpertJobRecipeLearning(ctx, session, request)
	if err != nil {
		code := byte(13)
		if errors.Is(err, dnfexpertjob.ErrLevelTooLow) {
			code = 14
		}
		if sendErr := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketUseStackable), buildCurrentExpertJobRecipeLearningErrorBody(code, request.ListType, request.InstanceValue, request.ItemCode), dnfproto.DefaultChannelClassification); sendErr != nil {
			return true, sendErr
		}
		s.logGameEvent(session, "game-expert-job-recipe-learning-rejected", "job_type", jobType, "recipe", request.ItemCode, "slot", request.SlotIndex, "error", err, "error_code", code)
		return true, nil
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketUseStackable), buildCurrentExpertJobRecipeLearningSuccessBody(request.SlotIndex, request.ListType, request.InstanceValue, request.ItemCode), dnfproto.DefaultChannelClassification); err != nil {
		return true, err
	}
	if err := s.sendSelectedCurrentContainerListsRefresh(session, "expert_job_recipe_learning_after_ack"); err != nil {
		return true, err
	}
	if err := s.sendCurrentExpertJobInfoFromRepository(session, result.JobType, true); err != nil {
		return true, err
	}
	s.logGameEvent(session, "game-expert-job-recipe-learning-committed", "job_type", result.JobType, "recipe", result.RecipeID, "slot", request.SlotIndex, "remaining", result.Remaining)
	return true, nil
}

func (s *Service) commitCurrentExpertJobRecipeLearning(ctx context.Context, session *gameSession, request dnfinventory.UseStackableRequest) (currentExpertJobRecipeLearningResult, error) {
	catalog, _, owner, accountID, characterID, err := s.currentExpertJobMutationContext(session)
	if err != nil {
		return currentExpertJobRecipeLearningResult{}, err
	}
	recipeJobType, _, ok := catalog.RecipeJob(int64(request.ItemCode))
	if !ok {
		return currentExpertJobRecipeLearningResult{}, dnfexpertjob.ErrRecipeUnavailable
	}
	var result currentExpertJobRecipeLearningResult
	err = owner.LearnRecipe(ctx, dnfexpertjob.Command{
		AccountID:   accountID,
		CharacterID: characterID,
		UpdatedAt:   s.gameplayNow(),
		Project: func(assets *dnfexpertjob.Assets) (dnfexpertjob.Changes, error) {
			jobType, experience, stateErr := currentExpertJobCharacterState(assets.Character)
			if stateErr != nil || jobType != recipeJobType {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrJobUnsupported
			}
			config, configured := catalog.Config(jobType)
			if !configured {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrJobUnsupported
			}
			canLearn := config.CanLearn(experience, int64(request.ItemCode))
			if jobType == dnfexpertjob.EnchanterType {
				if enchanter, ok := catalog.Enchanter(); ok {
					if recipe, found := enchanter.CardRecipes[int64(request.ItemCode)]; found {
						canLearn = config.Level(experience) >= recipe.RequiredLevel
					}
				}
			}
			if !canLearn {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrLevelTooLow
			}
			if request.ListType != dnfrepo.MainInventoryListType || request.SlotIndex < 0 {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrRecipeUnavailable
			}
			key := currentCeraShopInventorySlotKey(request.ListType, request.SlotIndex)
			stack, found := assets.Inventory.Slots[key]
			if !found || stack.ItemID != int64(request.ItemCode) || stack.Count <= 0 || currentNPCShopItemLocked(stack) {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrRecipeUnavailable
			}
			stack.Count--
			if stack.Count == 0 {
				delete(assets.Inventory.Slots, key)
			} else {
				entry := currentItemListEntryFromStack(request.ListType, request.SlotIndex, stack)
				stack.RawEntry = append([]byte(nil), entry.data[:]...)
				assets.Inventory.Slots[key] = stack
			}
			if assets.Character.Stats == nil {
				assets.Character.Stats = make(map[string]int64)
			}
			assets.Character.Stats[currentExpertJobRecipeStatKey(jobType, int64(request.ItemCode))] = 1
			result = currentExpertJobRecipeLearningResult{JobType: jobType, RecipeID: int64(request.ItemCode), Remaining: stack.Count}
			return dnfexpertjob.Changes{Character: true, Inventory: true}, nil
		},
	})
	if err != nil {
		return currentExpertJobRecipeLearningResult{}, fmt.Errorf("learn expert job recipe item=%d: %w", request.ItemCode, err)
	}
	return result, nil
}

func buildCurrentExpertJobRecipeLearningSuccessBody(slot int16, listType byte, instanceValue, itemCode int32) []byte {
	w := packetWriter{}
	w.writeByte(1)
	w.writeUint16(uint16(slot))
	w.writeByte(listType)
	w.writeUint32(uint32(instanceValue))
	w.writeUint32(uint32(itemCode))
	return w.bytes()
}

func buildCurrentExpertJobRecipeLearningErrorBody(errorCode, listType byte, instanceValue, itemCode int32) []byte {
	w := packetWriter{}
	w.writeByte(0)
	w.writeByte(errorCode)
	w.writeByte(listType)
	w.writeUint32(uint32(instanceValue))
	w.writeUint32(uint32(itemCode))
	return w.bytes()
}
