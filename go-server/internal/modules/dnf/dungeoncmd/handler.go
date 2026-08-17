// 本文件负责副本命令的协议分发。
// 当前只把已经对齐的 C2S 解析成命令计划；场景 owner、结算和加载门闸闭合前不回成功 ACK。
package dungeoncmd

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

type handler struct{}

// NewHandler 创建副本协议处理器。
func NewHandler() alignedcmd.Handler {
	return handler{}
}

// Domain 返回副本业务域。
func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainDungeon
}

// Handle 解析副本请求，并在场景 owner 和 S2C 顺序闭合前禁止成功 ACK。
func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketEnterSelectDungeon:
		cmd := NewRawCommand(req, "enter_select_dungeon", "scene bootstrap owner + channel/party gate + response order")
		return applyOwner(ctx, req, cmd, nil), nil
	case dnfenum.CmdPacketSelectDungeon:
		parsed, err := DecodeSelectDungeonRequest(req.Body)
		cmd := NewSelectCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketGetItem:
		parsed, err := DecodeGetItemRequest(req.Body)
		cmd := NewGetItemCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketMoveMap:
		parsed, err := DecodeMoveMapRequest(req.Body)
		cmd := NewMoveMapCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketDieMonster:
		parsed, err := DecodeDieMonsterRequest(req.Body)
		cmd := NewRawCommand(req, "die_monster", parsed.String())
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketDieCharacter:
		cmd := NewRawCommand(req, "die_character", "room combat owner + death/revive state + current EXE response order")
		return applyOwner(ctx, req, cmd, nil), nil
	case dnfenum.CmdPacketUseCoin:
		cmd := NewRawCommand(req, "use_coin", "account wallet/coin owner + revive state + scene refresh order")
		return applyOwner(ctx, req, cmd, nil), nil
	case dnfenum.CmdPacketSetPlayResult:
		cmd := NewRawCommand(req, "set_play_result", "dungeon settlement owner + reward owner + back-to-town flush order")
		return applyOwner(ctx, req, cmd, nil), nil
	case dnfenum.CmdPacketChangeTutorialFlag:
		parsed, err := DecodeChangeTutorialFlagRequest(req.Body)
		cmd := NewTutorialCommand(req, parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketDungeonEventStoryPause:
		cmd := NewRawCommand(req, "dungeon_event_story_pause", "dungeon story owner + pause state + current EXE response evidence")
		return applyOwner(ctx, req, cmd, nil), nil
	case dnfenum.CmdPacketRequestDisjointItem:
		cmd := NewRawCommand(req, "request_disjoint_item", "dungeon disjoint machine context + inventory owner + response evidence")
		return applyOwner(ctx, req, cmd, nil), nil
	case dnfenum.CmdPacketDropItem:
		cmd := NewRawCommand(req, "drop_item", "inventory owner + scene drop notification + pickup registration")
		return applyOwner(ctx, req, cmd, nil), nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("dungeon 模块未登记 opcode %d", req.Opcode),
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
		Reason:          fmt.Sprintf("已解析 %s：%s；副本场景状态、奖励和加载门闸闭合前禁止回包", operation, summary),
	}
}

func ownerBlockedResult(cmd Command, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；dungeon owner 预检未通过：%v；禁止回成功包", cmd.Operation, cmd.String(), err),
	}
}

func ownerPlannedResult(cmd Command, result PlanResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"已解析 %s：%s；dungeon owner verified account=%s char=%s level=%d fatigue=%d location=(town=%d dungeon=%d room=%q) inventory=%t slots=%d warehouse=%d requestDungeon=%d difficulty=%d dropObject=%d next=(%d,%d) tutorialProgress=%d tutorialCommit=%d rawLen=%d；房间/掉落/结算/加载门闸未闭合，禁止成功 ACK",
			cmd.Operation,
			cmd.String(),
			result.AccountID,
			result.CharacterID,
			result.Level,
			result.Fatigue,
			result.TownID,
			result.DungeonID,
			result.RoomID,
			result.InventoryKnown,
			result.InventorySlotCount,
			result.WarehouseSlotCount,
			result.RequestedDungeonID,
			result.Difficulty,
			result.DropObjectKey,
			result.NextX,
			result.NextY,
			result.TutorialProgress,
			result.TutorialCommit,
			result.RawLen,
		),
	}
}
