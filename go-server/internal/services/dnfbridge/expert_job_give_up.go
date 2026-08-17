package dnfbridge

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfexpertjob "longheng.io/server/internal/modules/dnf/expertjob"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

const (
	currentExpertJobGiveUpInsufficientGold byte = 10
	currentExpertJobGiveUpInvalidState     byte = 19
	currentExpertJobGiveUpStateStat             = "expert_job_give_up_state"
)

type currentExpertJobGiveUpResult struct {
	FinalGold int64
	State     byte
	Cost      int64
}

func (s *Service) handleCurrentExpertJobGiveUp(session *gameSession, body []byte) error {
	opcode := uint16(dnfenum.CmdPacketGiveupExpertJob)
	if len(body) != 0 || s == nil || session == nil || session.selectedCharacterID == 0 || currentExpertJobSessionInDungeon(session) {
		return s.sendGameUpperFailure(session, opcode, currentExpertJobGiveUpInvalidState)
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	result, err := s.commitCurrentExpertJobGiveUp(ctx, session)
	cancel()
	if err != nil {
		code := currentExpertJobGiveUpInvalidState
		if errors.Is(err, dnfexpertjob.ErrInsufficientGold) {
			code = currentExpertJobGiveUpInsufficientGold
		}
		s.logGameEvent(session, "game-expert-job-give-up-rejected", "reason", err, "error_code", code)
		return s.sendGameUpperFailure(session, opcode, code)
	}

	if store := s.removeCurrentExpertJobStore(session.selectedCharacterID, session); store != nil {
		s.broadcastCurrentExpertJobStoreClose(store, true)
	}
	if err := s.sendGameUpperRawClass(session, opcode, buildCurrentExpertJobGiveUpSuccessBody(result), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	if err := s.sendCurrentExpertJobClearedInfo(session, result.State); err != nil {
		return err
	}
	if err := s.sendCurrentActiveQuestSnapshotForSession(session, "current_exe_op239_after_atomic_give_up"); err != nil {
		return err
	}
	if err := s.sendCurrentAcceptableQuestListOnlyForSession(session, "current_exe_op239_after_active_snapshot"); err != nil {
		return err
	}
	s.logGameEvent(session, "game-expert-job-give-up-committed",
		"final_gold", result.FinalGold,
		"give_up_state", result.State,
		"cost", result.Cost,
		"quest_source", "runtime_pvf_job_change_quest_20",
		"body_source", "current_exe_op239_success_exact")
	return nil
}

func (s *Service) commitCurrentExpertJobGiveUp(ctx context.Context, session *gameSession) (currentExpertJobGiveUpResult, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return currentExpertJobGiveUpResult{}, dnfexpertjob.ErrOwnerUnavailable
	}
	catalog, err := s.currentExpertJobCatalog()
	if err != nil {
		return currentExpertJobGiveUpResult{}, err
	}
	questCatalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		return currentExpertJobGiveUpResult{}, err
	}
	transitionQuestIDs := questCatalog.ExpertJobTransitionQuestIDs()
	if len(transitionQuestIDs) == 0 {
		return currentExpertJobGiveUpResult{}, dnfexpertjob.ErrJobUnsupported
	}
	repositories, ok := s.repositoryGroup()
	if !ok {
		return currentExpertJobGiveUpResult{}, dnfexpertjob.ErrOwnerUnavailable
	}
	owner, err := dnfexpertjob.NewOwner(repositories)
	if err != nil {
		return currentExpertJobGiveUpResult{}, err
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	if accountID == "" || characterID == "0" {
		return currentExpertJobGiveUpResult{}, dnfexpertjob.ErrOwnerUnavailable
	}

	var committed currentExpertJobGiveUpResult
	err = owner.GiveUp(ctx, dnfexpertjob.Command{
		AccountID: accountID, CharacterID: characterID, UpdatedAt: s.gameplayNow(),
		Project: func(assets *dnfexpertjob.Assets) (dnfexpertjob.Changes, error) {
			jobType, _, stateErr := currentExpertJobCharacterState(assets.Character)
			if stateErr != nil {
				return dnfexpertjob.Changes{}, stateErr
			}
			costs, found := catalog.GiveUpCosts(jobType)
			if !found {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrJobUnsupported
			}
			gold, goldErr := currentExpertJobWalletGold(assets.Inventory)
			if goldErr != nil {
				return dnfexpertjob.Changes{}, goldErr
			}
			state := assets.Character.Stats[currentExpertJobGiveUpStateStat]
			plan, planErr := dnfexpertjob.PlanGiveUp(gold, state, costs)
			if planErr != nil {
				return dnfexpertjob.Changes{}, planErr
			}
			if plan.FinalGold < 0 || plan.FinalGold > math.MaxUint32 {
				return dnfexpertjob.Changes{}, dnfexpertjob.ErrMachineInvalid
			}
			currentExpertJobSetWalletGold(assets.Inventory, plan.FinalGold)
			if assets.Character.Stats == nil {
				assets.Character.Stats = make(map[string]int64)
			}
			assets.Character.Stats["expert_job_type"] = 0
			assets.Character.Stats["expert_job_exp"] = 0
			assets.Character.Stats[currentExpertJobGiveUpStateStat] = int64(plan.FinalState)
			delete(assets.Character.Stats, currentExpertJobMachineGradeStat)
			delete(assets.Character.Stats, currentExpertJobMachineEnduranceStat)
			for key := range assets.Character.Stats {
				if strings.HasPrefix(key, "expert_job_recipe_") {
					delete(assets.Character.Stats, key)
				}
			}
			questChanged := false
			for _, questID := range transitionQuestIDs {
				if _, exists := assets.Quest.States[questID]; exists {
					delete(assets.Quest.States, questID)
					questChanged = true
				}
				if _, exists := assets.Quest.Progress[questID]; exists {
					delete(assets.Quest.Progress, questID)
					questChanged = true
				}
			}
			committed = currentExpertJobGiveUpResult{FinalGold: plan.FinalGold, State: plan.FinalState, Cost: plan.Cost}
			return dnfexpertjob.Changes{Character: true, Inventory: true, Quest: questChanged}, nil
		},
	})
	return committed, err
}

func buildCurrentExpertJobGiveUpSuccessBody(result currentExpertJobGiveUpResult) []byte {
	w := packetWriter{}
	w.writeByte(1)
	w.writeUint32(uint32(result.FinalGold))
	w.writeByte(result.State)
	w.writeByte(0)
	return w.bytes()
}
