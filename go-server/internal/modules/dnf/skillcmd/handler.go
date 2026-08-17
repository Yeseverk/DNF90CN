package skillcmd

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

type handler struct{}

func NewHandler() alignedcmd.Handler {
	return handler{}
}

func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainSkill
}

func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketChangeSkillslot:
		parsed, err := DecodeChangeSkillSlotRequest(req.Body)
		cmd := NewSlotCommand(req, parsed)
		return applySlotOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketBuySkill:
		parsed, err := DecodeBuySkillRequest(req.Body)
		cmd := NewBuyCommand(req, parsed)
		return applyBuyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketSkillInit:
		parsed, err := DecodeSkillInitRequest(req.Body)
		cmd := NewResetCommand(req, parsed)
		return applyResetOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketChangeAnotherSkillTree:
		parsed, err := DecodeChangeAnotherSkillTreeRequest(req.Body)
		cmd := NewTreeCommand(req, parsed)
		return applyTreeOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketSkillCommandCustomizing:
		parsed, err := DecodeSkillCommandRequest(req.Body)
		cmd := NewSkillCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketSkillCommandAllDefault:
		cmd := NewSimpleCommand(req, "skill_command_all_default", "skill owner + command reset persistence + NOTI 0x13 order")
		if len(req.Body) != 0 {
			return applyOwner(ctx, req, cmd, fmt.Errorf("invalid body length: got %d want 0", len(req.Body))), nil
		}
		return applyOwner(ctx, req, cmd, nil), nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("skill module has no opcode %d handler", req.Opcode),
		}, nil
	}
}

func applyTreeOwner(ctx context.Context, req alignedcmd.Request, cmd Command, parseErr error) alignedcmd.Result {
	if parseErr != nil {
		return treeSwitchFailureResult(req, cmd, parseErr)
	}
	owner, err := NewOwner(req.Repositories)
	if err != nil {
		return treeSwitchFailureResult(req, cmd, err)
	}
	result, err := owner.ApplyTreeSwitch(ctx, cmd)
	if err != nil {
		return treeSwitchFailureResult(req, cmd, planError(cmd.Operation, err))
	}
	body, err := BuildChangeAnotherSkillTreeSuccess(result)
	if err != nil {
		return treeSwitchFailureResult(req, cmd, planError(cmd.Operation, err))
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"applied %s: char=%s current=%d target=%d; persisted before current EXE op260 success",
			cmd.Operation, result.CharacterID, result.Current, result.Target,
		),
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          req.Opcode,
			Body:           body,
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
	}
}

func treeSwitchFailureResult(req alignedcmd.Request, cmd Command, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("rejected %s: %v; current EXE op260 failure 19 returned", cmd.Operation, err),
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          req.Opcode,
			Body:           BuildChangeAnotherSkillTreeFailure(),
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
	}
}

func applyResetOwner(ctx context.Context, req alignedcmd.Request, cmd Command, parseErr error) alignedcmd.Result {
	if parseErr != nil {
		return blockedParsedResult(cmd.Operation, cmd.String(), parseErr)
	}
	owner, err := NewOwner(req.Repositories, OwnerOptions{
		Catalog:       req.SkillCatalog,
		InitialLevels: req.InitialSkillLevels,
		PointBaseline: req.SkillPointBaseline,
	})
	if err != nil {
		return ownerBlockedResult(cmd, err)
	}
	result, err := owner.ApplyReset(ctx, cmd)
	if err != nil {
		return ownerBlockedResult(cmd, planError(cmd.Operation, err))
	}
	body, err := BuildSkillInitSuccess(result)
	if err != nil {
		return ownerBlockedResult(cmd, planError(cmd.Operation, err))
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"applied %s: char=%s tree=%d mode=%d initial_skills=%d remainSP=%d remainTP=%d; current EXE op491 response opened",
			cmd.Operation, result.CharacterID, result.SkillTree, result.Mode, result.SkillCount,
			result.Points.RemainingSP, result.Points.RemainingTP,
		),
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          req.Opcode,
			Body:           body,
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		PostActions: []alignedcmd.PostAction{alignedcmd.PostActionRefreshSelectedActorSkills},
	}
}

func applySlotOwner(ctx context.Context, req alignedcmd.Request, cmd Command, parseErr error) alignedcmd.Result {
	if parseErr != nil {
		return blockedParsedResult(cmd.Operation, cmd.String(), parseErr)
	}
	owner, err := NewOwner(req.Repositories, OwnerOptions{
		Catalog:       req.SkillCatalog,
		InitialLevels: req.InitialSkillLevels,
	})
	if err != nil {
		return ownerBlockedResult(cmd, err)
	}
	result, err := owner.ApplySlot(ctx, cmd)
	if err != nil {
		return ownerBlockedResult(cmd, planError(cmd.Operation, err))
	}
	body, err := BuildChangeSkillSlotSuccess(result)
	if err != nil {
		return ownerBlockedResult(cmd, planError(cmd.Operation, err))
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"applied %s: char=%s tree=%d from=%d to=%d source_skill=%d destination_skill=%d destination_occupied=%t; current EXE op28 response opened",
			cmd.Operation,
			result.CharacterID,
			result.SkillTree,
			result.From,
			result.To,
			result.FromSkillID,
			result.ToSkillID,
			result.ToOccupied,
		),
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          req.Opcode,
			Body:           body,
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
	}
}

func applyBuyOwner(ctx context.Context, req alignedcmd.Request, cmd Command, parseErr error) alignedcmd.Result {
	if parseErr != nil {
		return blockedParsedResult(cmd.Operation, cmd.String(), parseErr)
	}
	if err := validateBuySkillTree(cmd.SkillTree); err != nil {
		return ownerBlockedResult(cmd, err)
	}
	owner, err := NewOwner(req.Repositories, OwnerOptions{
		Catalog:       req.SkillCatalog,
		InitialLevels: req.InitialSkillLevels,
		PointBaseline: req.SkillPointBaseline,
	})
	if err != nil {
		return ownerBlockedResult(cmd, err)
	}
	result, err := owner.ApplyBuy(ctx, cmd)
	if err != nil {
		return ownerBlockedResult(cmd, planError(cmd.Operation, err))
	}
	body, err := BuildBuySkillSuccess(result)
	if err != nil {
		return ownerBlockedResult(cmd, planError(cmd.Operation, err))
	}
	postActions := []alignedcmd.PostAction{}
	if result.ConsumedRefundItem {
		// The 遗忘河之水 stack changed in the same transaction; the main list
		// must refresh or the client keeps showing the old count (86JP sends
		// SendUpdateItemList for the consumed water slot).
		postActions = append(postActions, alignedcmd.PostActionRefreshSelectedItemContainers)
	}
	if result.ExpiredContractSkillsReset {
		// The 达人契约 expiry sweep removed skills before this batch; the buy
		// ACK only carries batch entries, so schedule the full skill refresh.
		postActions = append(postActions, alignedcmd.PostActionRefreshSelectedActorSkills)
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"applied %s: char=%s tree=%d entries=%d remainSP=%d remainTP=%d refund_water=%t; current EXE op29 response opened",
			cmd.Operation,
			result.CharacterID,
			result.SkillTree,
			len(result.Entries),
			result.Points.RemainingSP,
			result.Points.RemainingTP,
			result.ConsumedRefundItem,
		),
		UpperResponses: []alignedcmd.UpperResponse{{
			MsgID:          req.Opcode,
			Body:           body,
			Classification: dnfproto.DefaultChannelClassification,
			AllowCodec:     true,
		}},
		PostActions: postActions,
	}
}

func applyOwner(ctx context.Context, req alignedcmd.Request, cmd Command, parseErr error) alignedcmd.Result {
	if parseErr != nil {
		return blockedParsedResult(cmd.Operation, cmd.String(), parseErr)
	}
	owner, err := NewOwner(req.Repositories, OwnerOptions{
		Catalog:       req.SkillCatalog,
		InitialLevels: req.InitialSkillLevels,
	})
	if err != nil {
		return ownerBlockedResult(cmd, err)
	}
	result, err := owner.Plan(ctx, cmd)
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
		Reason:          fmt.Sprintf("parsed %s: %s; skill state, SP/TP, and NOTI 0x13 refresh are not closed, success ACK is blocked", operation, summary),
	}
}

func ownerBlockedResult(cmd Command, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("parsed %s: %s; skill owner preflight failed: %v; success ACK is blocked", cmd.Operation, cmd.String(), err),
	}
}

func ownerPlannedResult(cmd Command, result PlanResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"parsed %s: %s; skill owner verified account=%s char=%s known=%t skillCount=%d cooldownCount=%d requested=%v refunds=%d; SP/TP persistence and raw 0x13 refresh are not closed, success ACK is blocked",
			cmd.Operation,
			cmd.String(),
			result.AccountID,
			result.CharacterID,
			result.Known,
			result.SkillCount,
			result.CooldownCount,
			result.RequestedSkillIDs,
			result.RefundCount,
		),
	}
}
