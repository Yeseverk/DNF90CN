// 本文件负责时装和称号命令的协议分发。
// 时装合成、徽章、开孔和称号簿都会改动物品与外观状态，当前只允许 owner 只读预检，不开放成功 ACK。
package avatartitle

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

type handler struct{}

// NewHandler 创建时装称号协议处理器。
func NewHandler() alignedcmd.Handler {
	return handler{}
}

// Domain 返回时装称号业务域。
func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainAvatarTitle
}

// Handle 解析时装称号请求，并在外观、物品刷新链路闭合前禁止成功 ACK。
func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketCompoundAvatar:
		parsed, err := DecodeCompoundAvatarRequest(req.Body)
		cmd := NewCompoundCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketUseEmblem:
		parsed, err := DecodeAvatarEmblemRequest(req.Body)
		cmd := NewEmblemCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketAddAvatarSocket:
		parsed, err := DecodeAvatarSocketRequest(req.Body)
		cmd := NewSocketCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketTitleBookPut:
		parsed, err := DecodeTitleBookRequest(req.Body)
		cmd := NewTitleBookCommand(req, "title_book_put", parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketTitleBookGet:
		parsed, err := DecodeTitleBookRequest(req.Body)
		cmd := NewTitleBookCommand(req, "title_book_get", parsed)
		return applyOwner(ctx, req, cmd, err), nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("avatar_title 模块未注册 opcode %d", req.Opcode),
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
		Reason:          fmt.Sprintf("已解析 %s：%s；外观、称号簿和物品刷新顺序未闭合，禁止回包", operation, summary),
	}
}

func ownerBlockedResult(cmd Command, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；avatar/title owner 预检未通过：%v；禁止回成功包", cmd.Operation, cmd.String(), err),
	}
}

func ownerPlannedResult(cmd Command, result PlanResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"已解析 %s：%s；avatartitle owner verified account=%s char=%s inventory=%t equipmentKnown=%t equipEntries=%d avatarSources=%d/%d targetFound=%t targetItem=%d materialFound=%t materialItem=%d emblemMaterials=%d/%d titleFound=%t title=(list=%d slot=%d item=%d category=%d index=%d) requestedOutput=%d；外观/称号簿/USERINFO 刷新顺序未闭合，禁止成功 ACK",
			cmd.Operation,
			cmd.String(),
			result.AccountID,
			result.CharacterID,
			result.InventoryKnown,
			result.EquipmentKnown,
			result.EquipmentEntryCount,
			result.AvatarSourceFound,
			result.AvatarSourceTotal,
			result.TargetFound,
			result.TargetItemID,
			result.MaterialFound,
			result.MaterialItemID,
			result.EmblemMaterialFound,
			result.EmblemMaterialTotal,
			result.TitleInventoryFound,
			result.TitleInventoryList,
			result.TitleInventorySlot,
			result.TitleInventoryItemID,
			result.TitleBookCategory,
			result.TitleBookIndex,
			result.RequestedOutputItemID,
		),
	}
}
