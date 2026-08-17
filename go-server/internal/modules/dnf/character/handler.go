// 本文件负责角色选择链命令的协议边界。
// dnfbridge 已有专门登录、选角和 USERINFO 链路；这里只补齐 aligned registry 的 character 域解析入口。
package character

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

type handler struct{}

// NewHandler 创建角色协议处理器。
func NewHandler() alignedcmd.Handler {
	return handler{}
}

// Domain 返回角色业务域。
func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainCharacter
}

// Handle 解析角色相关请求；现有专门链路优先处理，落到这里的命令不直接回成功包。
func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketSelectCharacter:
		parsed, err := DecodeSelectCharacterRequest(req.Body)
		cmd := NewSelectCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketCreateCharacter:
		parsed, err := DecodeCreateCharacterRequest(req.Body)
		cmd := NewCreateCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketDeleteCharacter:
		parsed, err := DecodeDeleteCharacterRequest(req.Body)
		cmd := NewDeleteCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketReturnSelectCharacter:
		cmd := NewRawCommand(req, "return_select_character", "login chain owner + roster op2 refresh order")
		return applyOwner(ctx, req, cmd, nil), nil
	case dnfenum.CmdPacketGetUserinfo:
		cmd := NewRawCommand(req, "get_userinfo", "USERINFO builder + current EXE subtype order + scene bootstrap gate")
		return applyOwner(ctx, req, cmd, nil), nil
	case dnfenum.CmdPacketCheckDoubleCharacterName:
		parsed, err := DecodeCheckNameRequest(req.Body)
		cmd := NewCheckNameCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("character 模块未登记 opcode %d", req.Opcode),
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
			Reason:          fmt.Sprintf("%s 请求体解析失败：%v；禁止回成功包", operation, err),
		}
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；角色链现在由 dnfbridge 专门链路处理，aligned fallback 不直接回包", operation, summary),
	}
}

func ownerBlockedResult(cmd Command, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；character owner 预检未通过：%v；aligned fallback 禁止成功 ACK", cmd.Operation, cmd.String(), err),
	}
}

func ownerPlannedResult(cmd Command, result PlanResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"已解析 %s：%s；character owner verified account=%s char=%s known=%t name=%q slot=%d job=%q level=%d roster=%d requested=(slotOrChar=%d slot=%d job=%d name=%q) nameTaken=%t owner=%s；角色列表/USERINFO/进场专门链路未闭合，aligned fallback 禁止成功 ACK",
			cmd.Operation,
			cmd.String(),
			result.AccountID,
			result.CharacterID,
			result.CharacterKnown,
			result.Name,
			result.Slot,
			result.Job,
			result.Level,
			result.RosterCount,
			result.SelectedOrRequested,
			result.RequestedSlot,
			result.RequestedJob,
			result.RequestedName,
			result.NameTaken,
			result.NameOwnerID,
		),
	}
}
