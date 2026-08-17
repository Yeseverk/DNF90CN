// 本文件把已验证的队伍 C2S 命令转换成客户端期望的 upper 回包序列。
package party

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

type handler struct{}

// NewHandler 创建队伍协议处理器。
func NewHandler() alignedcmd.Handler {
	return handler{}
}

// Domain 返回队伍业务域。
func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainParty
}

// Handle 开放已验证的队伍命令；快速组队入口目前只做 ACK，不伪造进场景。
func (handler) Handle(_ context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketSetPartyInfo:
		return handleSetPartyInfo(req), nil
	case dnfenum.CmdPacketLeaveParty:
		return handleLeaveParty(req), nil
	case dnfenum.CmdPacketCallPartyMemberRealtimeInfo:
		return handleCallRealtime(req), nil
	case dnfenum.CmdPacketRegisterQuickParty:
		return handleRegisterQuickParty(req), nil
	case dnfenum.CmdPacketCancelQuickParty:
		return handleAckOnly(req, "cancel_quick_party", "MCP 20260705 证据：S2C 0x1BC 成功分支不读正文，取消快速组队只需成功 ACK"), nil
	case dnfenum.CmdPacketDirectEntranceDungeonQuickParty:
		return handleAckOnly(req, "direct_entrance_dungeon_quick_party", "MCP 20260705 证据：S2C 0x1BD 成功分支不读正文；这里只确认入口请求，后续进场景链单独发送"), nil
	case dnfenum.CmdPacketReserveLeaveParty:
		return handleReserveLeaveParty(req), nil
	case dnfenum.CmdPacketEntryIntoParty:
		return handleEntryIntoParty(req), nil
	case dnfenum.CmdPacketEntryIntoPartyFinish:
		return handleEntryIntoPartyFinish(req), nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("party 模块未登记 opcode %d", req.Opcode),
		}, nil
	}
}

func handleEntryIntoParty(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeEntryIntoPartyRequest(req.Body)
	if err != nil {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       "entry_into_party",
			Reason:          fmt.Sprintf("ENTRY_INTO_PARTY 解析失败：%v；禁止伪造成功 ACK", err),
		}
	}
	if req.SelectedCharacterID == 0 {
		return failureAckCode(req.Opcode, "entry_into_party", 3, "缺少已选择角色，按 705 失败分支返回通用失败码")
	}
	if parsed.TargetID == 0 {
		return failureAckCode(req.Opcode, "entry_into_party", 19, "705 目标 ID 为空，按 EXE 失败码集合返回 19")
	}
	return failureAckCode(req.Opcode, "entry_into_party", 3, "705 需要 dnfbridge 在线 session 协调目标端；模块兜底不伪造入队成功")
}

func handleEntryIntoPartyFinish(req alignedcmd.Request) alignedcmd.Result {
	if req.Party != nil && req.Party.PartyID > 0 && len(req.Party.Members) > 1 {
		state := req.Party.UserState
		if state == 0 {
			state = 1
		}
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: true,
			Operation:       "entry_into_party_finish",
			Reason:          "Current EXE class0/op706 reads state:u8,count:u8 and count pairs of u32/u32; runtime has no peer-value owner, so send an exact zero-count body",
			UpperResponses: []alignedcmd.UpperResponse{
				{
					MsgID:          req.Opcode,
					Body:           BuildEntryIntoPartyFinishEmptyBody(state),
					Classification: 0,
					AllowCodec:     false,
				},
			},
		}
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       "entry_into_party_finish",
		Reason:          "706 lacks a class1 result envelope in the current EXE; without party context, do not send the old malformed {0,3} body",
	}
}

func handleRegisterQuickParty(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeRegisterQuickPartyRequest(req.Body)
	if err != nil {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       "register_quick_party",
			Reason:          fmt.Sprintf("REGISTER_QUICK_PARTY 解析失败：%v；不能按旧枚举伪造 ACK", err),
		}
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       "register_quick_party",
		Reason: fmt.Sprintf(
			"MCP 20260705：C2S 0x1BB 已解析 %d 个目标；S2C 同号 handler 读 u16+u8，不是本请求 ACK，等待真实快速组队推送结构",
			len(parsed.Targets),
		),
	}
}

func handleReserveLeaveParty(req alignedcmd.Request) alignedcmd.Result {
	parsed, err := DecodeReserveLeavePartyRequest(req.Body)
	if err != nil {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       "reserve_leave_party",
			Reason:          fmt.Sprintf("RESERVE_LEAVE_PARTY 解析失败：%v；禁止伪造成功 ACK", err),
		}
	}
	if req.SelectedCharacterID == 0 {
		return failureAck(req.Opcode, "reserve_leave_party", "缺少已选择角色，返回失败 ACK 避免客户端读取不存在的 targetChar")
	}
	if req.Party != nil {
		req.Party.ReserveLeaveFlag = parsed.Flag
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       "reserve_leave_party",
		Reason:          "MCP 20260705：class1 0x02B3 成功响应读取 u8 flag + u16 targetChar",
		UpperResponses: []alignedcmd.UpperResponse{
			{
				MsgID:          req.Opcode,
				Body:           BuildReserveLeavePartyAck(parsed.Flag, req.SelectedCharacterID),
				Classification: dnfproto.DefaultChannelClassification,
				AllowCodec:     true,
			},
		},
	}
}

func handleAckOnly(req alignedcmd.Request, operation string, reason string) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       operation,
		Reason:          reason,
		UpperResponses:  []alignedcmd.UpperResponse{successAck(req.Opcode)},
	}
}

func handleSetPartyInfo(req alignedcmd.Request) alignedcmd.Result {
	if req.Party == nil || req.SelectedCharacterID == 0 || req.SelectedCharacterID == ^uint16(0) {
		return failureAck(req.Opcode, "set_party_info", "缺少已选择角色或队伍会话态")
	}
	parsed, err := DecodeSetPartyInfoRequest(req.Body)
	if err != nil {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       "set_party_info",
			Reason:          fmt.Sprintf("SET_PARTY_INFO 请求体解析失败：%v；禁止伪造成功 ACK", err),
		}
	}
	state := req.Party
	if state.PartyID <= 0 || state.PartyID >= int(^uint16(0)) {
		// Current EXE op87/op90 carries the party key as u16. Use the
		// online leader character ID as the stable active-party key instead
		// of the historical 100000+ IDs that truncated on wire.
		state.PartyID = int(req.SelectedCharacterID)
	}
	state.IsLeader = true
	state.UserID = req.SelectedCharacterID
	if state.UserState == 0 {
		state.UserState = defaultTownUserState
	}
	state.RequestPrefix0 = parsed.Prefix0
	state.RequestPrefix1 = parsed.Prefix1
	state.NameBytes = append(state.NameBytes[:0], parsed.NameBytes...)
	state.MemberSelectCode = parsed.MemberSelectCode
	state.MaxMembers = parsed.MaxMembers
	state.SelectionID = parsed.SelectionID
	state.SelectionCode = parsed.SelectionCode
	state.SelectionValue = parsed.SelectionValue
	state.RecruitFlag = parsed.RecruitFlag
	state.TargetMode = parsed.TargetMode
	state.TargetDungeonID = parsed.TargetDungeonID
	upsertCurrentMember(state)

	responses := []alignedcmd.UpperResponse{
		successAck(req.Opcode),
	}

	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       "set_party_info",
		Reason: fmt.Sprintf(
			"当前 NoPack.exe：class1/op12 成功 ACK 后，由 bridge 重建 sub_1D64CA0 的 class0/op9 kind0 角色队伍槽位表，再发送 class0/op153 与 class0/op7；party=%d memberCode=0x%02X max=%d selection=%d/%d/%d recruit=%d targetMode=%d dungeon=0x%04X",
			state.PartyID, state.MemberSelectCode, state.MaxMembers, state.SelectionID, state.SelectionCode,
			state.SelectionValue, state.RecruitFlag, state.TargetMode, state.TargetDungeonID,
		),
		UpperResponses: responses,
		PostActions:    []alignedcmd.PostAction{alignedcmd.PostActionRefreshSelectedPartyFrame},
	}
}

func handleLeaveParty(req alignedcmd.Request) alignedcmd.Result {
	if req.Party == nil || req.SelectedCharacterID == 0 {
		return failureAck(req.Opcode, "leave_party", "缺少已选择角色或队伍会话态")
	}
	*req.Party = alignedcmd.PartyState{}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       "leave_party",
		Reason:          "当前 NoPack.exe：退队 ACK 后，由 bridge 先清空 class0/op9 kind0 角色队伍槽位表，再以 class0/op153 count=0 清空实时成员状态",
		UpperResponses: []alignedcmd.UpperResponse{
			successAck(req.Opcode),
		},
		PostActions: []alignedcmd.PostAction{alignedcmd.PostActionRefreshSelectedPartyFrame},
	}
}

func handleCallRealtime(req alignedcmd.Request) alignedcmd.Result {
	if req.Party == nil || req.Party.PartyID <= 0 || req.Party.UserID == 0 {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: true,
			Operation:       "call_party_member_realtime_info",
			Reason:          "当前不在队伍中，按当前 NoPack.exe sub_1D800C0 布局返回 class0/op153 count=0",
			UpperResponses:  []alignedcmd.UpperResponse{partyRealtimeResponse(BuildEmptyRealtimeInfo())},
		}
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       "call_party_member_realtime_info",
		Reason:          "Current NoPack.exe sub_1D800C0 consumes each class0/op153 row as uid, HP percent, helper flag, and slot index; one exact op153 snapshot is complete",
		UpperResponses:  []alignedcmd.UpperResponse{partyRealtimeResponse(BuildSingleMemberRealtimeInfo(*req.Party))},
	}
}

func upsertCurrentMember(state *alignedcmd.PartyState) {
	for i := range state.Members {
		if state.Members[i].UserID == state.UserID {
			state.Members[i].UserState = state.UserState
			state.Members[i].HPPercent = 100
			state.Members[i].MPPercent = 100
			return
		}
	}
	if len(state.Members) >= 4 {
		return
	}
	state.Members = append(state.Members, alignedcmd.PartyMemberState{
		UserID:    state.UserID,
		UserState: state.UserState,
		HPPercent: 100,
		MPPercent: 100,
	})
}

func successAck(opcode uint16) alignedcmd.UpperResponse {
	return alignedcmd.UpperResponse{
		MsgID:          opcode,
		Body:           []byte{1},
		Classification: dnfproto.DefaultChannelClassification,
		AllowCodec:     true,
	}
}

func failureAck(opcode uint16, operation string, reason string) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       operation,
		Reason:          reason,
		UpperResponses: []alignedcmd.UpperResponse{
			{
				MsgID:          opcode,
				Body:           []byte{0},
				Classification: dnfproto.DefaultChannelClassification,
				AllowCodec:     true,
			},
		},
	}
}

func failureAckCode(opcode uint16, operation string, code byte, reason string) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       operation,
		Reason:          reason,
		UpperResponses: []alignedcmd.UpperResponse{
			{
				MsgID:          opcode,
				Body:           []byte{0, code},
				Classification: dnfproto.DefaultChannelClassification,
				AllowCodec:     true,
			},
		},
	}
}

func partyRealtimeResponse(body []byte) alignedcmd.UpperResponse {
	return alignedcmd.UpperResponse{
		MsgID:          0x0099,
		Body:           body,
		Classification: 0,
		AllowCodec:     true,
	}
}
