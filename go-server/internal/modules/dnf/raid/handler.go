// 本文件把已确认的攻坚队 C2S 命令纳入模块化分流。
package raid

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

type handler struct{}

// NewHandler 创建攻坚队协议处理器。
func NewHandler() alignedcmd.Handler {
	return handler{}
}

// Domain 返回攻坚队业务域。
func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainRaid
}

// Handle 解析已确认的攻坚队 C2S 命令，并禁止模块层伪造成攻坚成功；在线刷新由 dnfbridge/后续 raid owner 推 0x24F。
func (handler) Handle(_ context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketCreateRaid:
		return handleCreateRaid(req), nil
	case dnfenum.CmdPacketLeaveRaid:
		return handleLeaveRaid(req), nil
	case dnfenum.CmdPacketStartRaid:
		return handleStartRaid(req), nil
	case dnfenum.CmdPacketRaidEntryCostInfo:
		return handleEntryCostInfo(req), nil
	case dnfenum.CmdPacketRaidSetSymbol:
		return handleSetSymbol(req), nil
	case dnfenum.CmdPacketSetRaidWaiting:
		return handleSetWaiting(req), nil
	case dnfenum.CmdPacketRejoinRaid:
		return handleRejoinRaid(req), nil
	case dnfenum.CmdPacketRaidManagerWork:
		return handleManagerWork(req), nil
	case dnfenum.CmdPacketModifyRaidInfo:
		return handleModifyRaidInfo(req), nil
	case dnfenum.CmdPacketRaidOtherChannelRequestJoin:
		return handleOtherChannelRequestJoin(req), nil
	case dnfenum.CmdPacketRaidMemberChangeState:
		return handleMemberChangeState(req), nil
	case dnfenum.CmdPacketRaidUserMoveChannelFail:
		return handleUserMoveChannelFail(req), nil
	case dnfenum.CmdPacketRaidOtherChannelList:
		return handleOtherChannelList(req), nil
	case dnfenum.CmdPacketRaidCheckRaidUser:
		return handleCheckRaidUser(req), nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("raid 模块未登记 opcode %d", req.Opcode),
		}, nil
	}
}

func handleCreateRaid(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeCreateRaidRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_create", err)
	}
	return raidPending("raid_create", fmt.Sprintf(
		"MCP 20260705：664 创建攻坚队 body=u8,dstr name,u8 tail；route=%d name_len=%d tail=%d；真实成功后应推 0x24F mode=3",
		parsed.RouteOrRaidType,
		len(parsed.NameBytes),
		parsed.TailFlag,
	))
}

func handleLeaveRaid(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeLeaveRaidRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_leave", err)
	}
	return raidPending("raid_leave", fmt.Sprintf(
		"MCP 20260705：665 离开攻坚队 body=u16 key=%d；真实成功后应推 0x24F mode=3 或清空攻坚 UI",
		parsed.RaidOrMemberKey,
	))
}

func handleStartRaid(req alignedcmd.Request) alignedcmd.Result {
	if err := DecodeStartRaidRequest(req.Body); err != nil {
		return raidPendingParseError("raid_start", err)
	}
	return raidPending("raid_start", "MCP 20260705：666 开始攻坚为空包；服务端开始后按 group_index/slot_order 自动创建普通 4 人小队")
}

func handleRejoinRaid(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeRejoinRaidRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_rejoin", err)
	}
	return raidPending("raid_rejoin", fmt.Sprintf(
		"MCP 20260705：668 重新加入攻坚 body=u32 raid_key=0x%08X；后续由服务端重推 0x24F mode=3",
		parsed.RaidKey,
	))
}

func handleSetWaiting(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeSetWaitingRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_set_waiting", err)
	}
	return raidPending("raid_set_waiting", fmt.Sprintf(
		"MCP 20260705：667 攻坚等待状态 body=u8 flag,u8 route；flag=%d route=%d；对象级刷新待 0x24F mode=2 继续确认",
		parsed.Flag,
		parsed.RouteRaidType,
	))
}

func handleEntryCostInfo(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeEntryCostInfoRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_entry_cost_info", err)
	}
	return raidPending("raid_entry_cost_info", fmt.Sprintf(
		"MCP 20260705: 658 RaidEntryCostInfo body=u8 enabled=%d；这是入场消耗/材料信息请求，未确认 S2C 成功结构前不自动回包",
		parsed.Enabled,
	))
}

func handleSetSymbol(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeSetSymbolRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_set_symbol", err)
	}
	return raidPending("raid_set_symbol", fmt.Sprintf(
		"MCP 20260705: 662 RaidSetSymbol body=u32,u32,u8；source=0x%08X target=0x%08X symbol=%d；先登记，不伪造 raid 标记同步",
		parsed.SourceValue,
		parsed.TargetValue,
		parsed.Symbol,
	))
}

func handleManagerWork(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeManagerWorkRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_manager_work", err)
	}
	return raidPending("raid_manager_work", fmt.Sprintf(
		"MCP 20260705：669 仅是攻坚队长编辑请求，S2C 669 为 DoNothing；需更新成员 %d 到小组 %d 后推 0x24F mode=3",
		parsed.MemberCharKey,
		parsed.TargetGroup,
	))
}

func handleOtherChannelRequestJoin(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeOtherChannelRequestJoinRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_other_channel_request_join", err)
	}
	return raidPending("raid_other_channel_request_join", fmt.Sprintf(
		"MCP 20260705：820 跨频道攻坚申请 body=u8,u16,u32,u16；mode=%d target=%d client=0x%08X route=%d；需要公共频道 owner 决定回包",
		parsed.Mode,
		parsed.TargetKey,
		parsed.ClientValue,
		parsed.RouteRaidType,
	))
}

func handleMemberChangeState(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeMemberChangeStateRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_member_change_state", err)
	}
	return raidPending("raid_member_change_state", fmt.Sprintf(
		"MCP 20260705：823 成员状态变化 body=u8 state=%d；在线实现更新 member state 后推 0x24F mode=3",
		parsed.State,
	))
}

func handleUserMoveChannelFail(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeUserMoveChannelFailRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_user_move_channel_fail", err)
	}
	return raidPending("raid_user_move_channel_fail", fmt.Sprintf(
		"MCP 20260705：824 跨频道移动失败 body=u8,u16；mode=%d target=%d；只记录，不伪造成功",
		parsed.Mode,
		parsed.TargetKey,
	))
}

func handleOtherChannelList(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeOtherChannelListRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_other_channel_list", err)
	}
	return raidPending("raid_other_channel_list", fmt.Sprintf(
		"MCP 20260705：831 公共频道攻坚列表 body=u8 或 u8,raw8,dstr；mode=%d has_ctx=%v name_len=%d；列表回包待公共频道 owner",
		parsed.Mode,
		parsed.HasContext,
		len(parsed.NameBytes),
	))
}

func handleCheckRaidUser(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeCheckRaidUserRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_check_user", err)
	}
	return raidPending("raid_check_user", fmt.Sprintf(
		"MCP 20260705：889 攻坚用户检查 body=u8,u16；mode=%d target=%d；S2C 889 是复杂列表，禁止最小假成功",
		parsed.Mode,
		parsed.TargetKey,
	))
}

func handleModifyRaidInfo(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeModifyRaidInfoRequest(req.Body)
	if err != nil {
		return raidPendingParseError("raid_modify_info", err)
	}
	return raidPending("raid_modify_info", fmt.Sprintf(
		"MCP 20260705：670 修改攻坚信息 body=u8,dstr name,u8 tail；route=%d name_len=%d tail=%d；真实成功后应推 0x24F mode=3",
		parsed.RouteOrRaidType,
		len(parsed.NameBytes),
		parsed.TailFlag,
	))
}

func raidPendingParseError(operation string, err error) alignedcmd.Result {
	return raidPending(operation, fmt.Sprintf("%s 请求体解析失败：%v；禁止伪造成攻坚成功", operation, err))
}

func raidPending(operation string, reason string) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          reason,
	}
}
