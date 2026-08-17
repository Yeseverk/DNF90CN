package quest

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

type handler struct{}

func NewHandler() alignedcmd.Handler {
	return handler{}
}

func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainQuest
}

func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketAcceptQuest:
		parsed, err := DecodeQuestIDRequest(req.Body)
		cmd := NewQuestIDCommand(req, "accept_quest", parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketGiveupQuest:
		parsed, err := DecodeQuestIDRequest(req.Body)
		cmd := NewQuestIDCommand(req, "giveup_quest", parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketSetQuestTrigger:
		parsed, err := DecodeSetTriggerRequest(req.Body)
		cmd := NewTriggerCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketFinishQuest:
		parsed, err := DecodeFinishQuestRequest(req.Body)
		cmd := NewFinishCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("quest module has no opcode %d handler", req.Opcode),
		}, nil
	}
}

func applyOwner(ctx context.Context, req alignedcmd.Request, cmd Command, parseErr error) alignedcmd.Result {
	if parseErr != nil {
		return blockedParsedResult(cmd.Operation, cmd.String(), parseErr)
	}
	owner, err := NewOwner(req.Repositories)
	if err != nil {
		return ownerBlockedResult(cmd, err)
	}
	var result PlanResult
	if cmd.Operation == "set_quest_trigger" {
		result, err = owner.ApplySetTrigger(ctx, cmd)
	} else {
		result, err = owner.Plan(ctx, cmd)
	}
	if err != nil {
		return ownerBlockedResult(cmd, planError(cmd.Operation, err))
	}
	return ownerPlannedResult(cmd, result)
}

func blockedParsedResult(operation string, summary string, err error) alignedcmd.Result {
	if err != nil {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       operation,
			Reason:          fmt.Sprintf("%s body parse failed: %v; success ACK is blocked", operation, err),
		}
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("parsed %s: %s; quest state, rewards, and NOTI order are not closed, success ACK is blocked", operation, summary),
	}
}

func ownerBlockedResult(cmd Command, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("parsed %s: %s; quest owner preflight failed: %v; success ACK is blocked", cmd.Operation, cmd.String(), err),
	}
}

func ownerPlannedResult(cmd Command, result PlanResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"parsed %s: %s; quest owner verified account=%s char=%s quest=%d known=%t status=%s triggerType=%d progress=%d rewardSelect=%d hasReward=%t multiplier=%d; reward/USERINFO/NOTI order is not closed, success ACK is blocked",
			cmd.Operation,
			cmd.String(),
			result.AccountID,
			result.CharacterID,
			result.QuestID,
			result.Known,
			result.Status,
			result.TriggerType,
			result.ProgressValue,
			result.RewardSelectIndex,
			result.HasRewardSelect,
			result.Multiplier,
		),
	}
}
