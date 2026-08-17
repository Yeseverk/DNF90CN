// 本文件负责礼包/随机盒子命令的协议分发。
// 礼包开启会消耗物品并发放称号、时装、宠物或道具，当前只解析请求体，不直接回成功包。
package packageitem

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

type handler struct{}

// NewHandler 创建礼包协议处理器。
func NewHandler() alignedcmd.Handler {
	return handler{}
}

// Domain 返回礼包业务域。
func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainPackage
}

// Handle 解析礼包请求，并在奖励和物品刷新链路闭合前禁止成功 ACK。
func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketUseBoosterItem:
		parsed, err := DecodeSelectablePackageRequest(req.Body)
		cmd := NewSelectableCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
		}
		result, err := owner.PlanSelectable(ctx, SelectableCommand{
			SelectedCharacterID:    req.SelectedCharacterID,
			SlotIndex:              parsed.SlotIndex,
			SelectedItemTemplateID: parsed.SelectedItemTemplateID,
		})
		if err != nil {
			return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
		}
		return ownerPlannedResult(cmd.Operation, cmd.String(), result), nil
	case dnfenum.CmdPacketUseRandomboxItem:
		parsed, err := DecodeMagicBoxSingleRequest(req.Body)
		cmd := NewMagicBoxCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		if req.MagicBoxResolver == nil || req.MagicBoxRewardItemResolver == nil {
			return alignedcmd.Result{
				Handled:         true,
				ResponseAllowed: false,
				Operation:       cmd.Operation,
				Reason:          fmt.Sprintf("magic box blocked: runtime-PVF resolvers unavailable; refusing to trust request metadata: %v", ErrMagicBoxResolverRequired),
			}, nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
		}
		result, err := owner.ApplyMagicBox(ctx, MagicBoxCommand{
			SelectedCharacterID: req.SelectedCharacterID,
			AccountID:           req.AccountID,
			RawListType:         parsed.RawListType,
			ListType:            parsed.ListType,
			SlotIndex:           parsed.SlotIndex,
			MaterialSlotIndex:   parsed.MaterialSlotIndex,
		}, req.MagicBoxResolver, req.MagicBoxRewardItemResolver)
		if err != nil {
			return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
		}
		return ownerAppliedMagicBoxResult(req.Opcode, cmd.Operation, result), nil
	case dnfenum.CmdPacketUseRandomboxItemExpand:
		parsed, err := DecodeMagicBoxExpandRequest(req.Body)
		cmd := NewMagicBoxExpandCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		if req.MagicBoxResolver == nil || req.MagicBoxRewardItemResolver == nil {
			return alignedcmd.Result{
				Handled:         true,
				ResponseAllowed: false,
				Operation:       cmd.Operation,
				Reason:          fmt.Sprintf("magic box expand blocked: runtime-PVF resolvers unavailable; refusing to trust request metadata: %v", ErrMagicBoxResolverRequired),
			}, nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
		}
		result, err := owner.ApplyMagicBoxExpand(ctx, MagicBoxExpandCommand{
			SelectedCharacterID: req.SelectedCharacterID,
			AccountID:           req.AccountID,
			RawListType:         parsed.RawListType,
			ListType:            parsed.ListType,
			SlotIndex:           parsed.SlotIndex,
			BoxItemID:           parsed.BoxItemID,
			MaterialSlotIndex:   parsed.MaterialSlotIndex,
			MaterialItemID:      parsed.MaterialItemID,
			OpenCount:           parsed.OpenCount,
		}, req.MagicBoxResolver, req.MagicBoxRewardItemResolver)
		if err != nil {
			return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
		}
		return ownerAppliedMagicBoxResult(req.Opcode, cmd.Operation, result), nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("package 模块未登记 opcode %d", req.Opcode),
		}, nil
	}
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
		Reason:          fmt.Sprintf("已解析 %s：%s；礼包奖励、物品刷新和弹窗顺序闭合前禁止回包", operation, summary),
	}
}

func ownerBlockedResult(operation string, summary string, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；package owner 预检未通过：%v；禁止回成功包", operation, summary, err),
	}
}

func ownerPlannedResult(operation string, summary string, result PlanResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；package owner 已验证 char=%s source=(%d,%d) item=%d materialSlot=%d materialItem=%d selected=%d；奖励发放/物品刷新/弹窗顺序未闭合，禁止回成功包", operation, summary, result.CharacterID, result.SourceListType, result.SourceSlotIndex, result.SourceItemID, result.MaterialSlotIndex, result.MaterialItemID, result.SelectedItemTemplateID),
	}
}

func ownerAppliedMagicBoxResult(opcode uint16, operation string, result MagicBoxResult) alignedcmd.Result {
	if !result.Success {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: true,
			Operation:       operation,
			Reason:          fmt.Sprintf("magic box owner rejected: %s; returned {0x00} failure in family order", result.Reason),
			UpperResponses: []alignedcmd.UpperResponse{
				class1Response(opcode, buildMagicBoxFailureAck()),
			},
		}
	}
	ackBody := buildMagicBoxSingleAck(result)
	responses := []alignedcmd.UpperResponse{
		class1Response(opcode, ackBody),
	}
	if dnfenum.CmdPacket(opcode) == dnfenum.CmdPacketUseRandomboxItemExpand {
		if result.BoxItemID == currentSeriaLuckItemID {
			// 赛丽亚十连由原生批量回包独占结果展示。额外发送 op208
			// 会用单开“三格获得道具”弹窗覆盖双栏十连结果。
			responses = []alignedcmd.UpperResponse{
				class1Response(opcode, buildMagicBoxSeriaBatchAck(result)),
			}
		} else {
			// 非赛丽亚源（泰迪礼盒等）：0x0468 原生回包在客户端 sub_1D074C0
			// 末尾固定弹一条收尾公告（当前 2018 字符串表把它渲染成错误文案），
			// 而其结果窗口由 208 通道完整承载，故只发 208 展示回包（2026-07-26
			// 实测：1128 未实现时客户端也无等待卡死，可安全省略）。
			responses = []alignedcmd.UpperResponse{
				class1Response(uint16(dnfenum.CmdPacketUseRandomboxItem), buildMagicBoxSingleAck(result)),
			}
		}
	}
	out := alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       operation,
		Reason: fmt.Sprintf(
			"package owner applied: %s box=%d opens=%d rewards=%d material=%d changed=%t; opened ack then container refresh",
			operation,
			result.BoxItemID,
			result.OpenCount,
			len(result.Rewards),
			result.MaterialItemID,
			result.Changed,
		),
		UpperResponses: responses,
		PostActions: []alignedcmd.PostAction{
			alignedcmd.PostActionRefreshSelectedItemContainers,
		},
	}
	if result.OverflowMailID != "" {
		// The durable system mail is already committed. Send the exact current
		// client alarm only after the normal box ACK and item refresh complete.
		out.MailboxAlarmRecipientID = uint16(parseMagicBoxCharacterID(result.CharacterID))
	}
	return out
}

func parseMagicBoxCharacterID(raw string) uint64 {
	value, err := strconv.ParseUint(raw, 10, 16)
	if err != nil {
		return 0
	}
	return value
}

func class1Response(opcode uint16, body []byte) alignedcmd.UpperResponse {
	return alignedcmd.UpperResponse{
		MsgID:          opcode,
		Body:           body,
		Classification: dnfproto.DefaultChannelClassification,
		AllowCodec:     true,
	}
}

// buildMagicBoxFailureAck is the magic-box family's uniform failure body:
// one zero byte, no error-code subdivision (86JP family rule).
func buildMagicBoxFailureAck() []byte {
	return []byte{0x00}
}

// buildMagicBoxSingleAck follows the current EXE's sub_1D05A60 read order.
// The class1 dispatcher consumes result=1 first; the handler then always
// reads u8 clientType and u8 doubleFlag before switching on clientType.
// Direct Seria opens use case 4, which reads i16 boxSlot and materialSlot at
// 0x01D06815/0x01D06821, then sub_1D057B0 reads u16 rewardCount and complete
// 0x77-byte rows at 0x01D0698A. Omitting doubleFlag shifts both slots and the
// reward count, leaving every result row in the client's "loading" state.
func buildMagicBoxSingleAck(result MagicBoxResult) []byte {
	doubleFlag := byte(0)
	if result.SeriaLuckDoubleTriggered {
		doubleFlag = 1
	}
	out := []byte{1, result.ClientType, doubleFlag}
	out = appendI16(out, result.BoxSlotIndex)
	out = appendI16(out, result.MaterialSlotIndex)
	out = appendI16(out, int16(len(result.Rewards)))
	for _, reward := range result.Rewards {
		out = append(out, buildMagicBoxBatchRewardRow(reward)...)
	}
	return out
}

func appendI16(out []byte, value int16) []byte {
	return append(out, byte(value), byte(value>>8))
}

func appendI32(out []byte, value int32) []byte {
	return append(out, byte(value), byte(value>>8), byte(value>>16), byte(value>>24))
}

// buildMagicBoxSeriaBatchAck 是赛丽亚源连开（0x0468）的原生批量回包，结构
// 以当前 EXE sub_1D074C0 的实读顺序为准（IDA/x32dbg 证据）：u8 success=4,
// u8 variant=4
// （无材料盒子，跳过客户端材料扣减校验，否则对 0xffff 材料槽误报 11031），
// u8 doubleFlag（86JP SeriaLuck 翻倍触发时为 1），u16 openCount,
// i16 boxSlot, i16 materialSlot(无材料=-1)，随后第一奖励列表（u16 count +
// count × 0x77 完整物品条目：u16 slot, u32 itemID, u32 count，其余字节为
// 零），u16 0，第二（双倍）奖励列表同构。variant=4 且 doubleFlag!=0 时
// 客户端走 0x36B 赛丽亚抽奖双栏窗口；doubleFlag=0 时走 0x145 单栏结果。
// 行宽 0x77 是当前 EXE 列表读取器 sub_1D057B0 的硬编码。
func buildMagicBoxSeriaBatchAck(result MagicBoxResult) []byte {
	doubleFlag := byte(0)
	if len(result.DoubleRewards) > 0 {
		doubleFlag = 1
	}
	out := []byte{4, 4, doubleFlag}
	out = appendI16(out, int16(result.OpenCount))
	out = appendI16(out, result.BoxSlotIndex)
	out = appendI16(out, result.MaterialSlotIndex)
	// 左栏（普通）= 基础抽取 DisplayRewards，右栏（双倍）= DoubleRewards；
	// 两个列表重复会让客户端合并清空左栏（86JP GetPrimaryRewards 契约）。
	out = appendI16(out, int16(len(result.DisplayRewards)))
	for _, reward := range result.DisplayRewards {
		out = append(out, buildMagicBoxBatchRewardRow(reward)...)
	}
	out = appendI16(out, 0)
	out = appendI16(out, int16(len(result.DoubleRewards)))
	for _, reward := range result.DoubleRewards {
		out = append(out, buildMagicBoxBatchRewardRow(reward)...)
	}
	return out
}

func buildMagicBoxBatchRewardRow(reward MagicBoxGrantedReward) []byte {
	row := make([]byte, currentMagicBoxEntrySize)
	binary.LittleEndian.PutUint16(row[0x00:0x02], uint16(reward.Slot))
	binary.LittleEndian.PutUint32(row[0x02:0x06], uint32(reward.ItemID))
	if reward.Kind == "equipment" {
		// 86JP 契约：装备行的 0x06 字段是品质种子而非数量，0x0B 是耐久；
		// 写成数量会让客户端结果窗口渲染为"加载中"（2026-07-26 实测）。
		seed := reward.QualitySeed
		if seed == 0 {
			seed = currentMagicBoxTopQualitySeed
		}
		binary.LittleEndian.PutUint32(row[0x06:0x0A], seed)
		binary.LittleEndian.PutUint16(row[0x0B:0x0D], reward.Durability)
	} else {
		binary.LittleEndian.PutUint32(row[0x06:0x0A], uint32(reward.Count))
	}
	return row
}

// buildMagicBoxExpandAck 是非赛丽亚源连开（0x0468）的原生回包。结构以当前
// EXE sub_1D074C0 的实读顺序为准：u8 variant=1, u8 clientType=4（走
// 0x1FC 分支），u16 openCount, i16 boxSlot, i16 materialSlot（无材料回填
// 盒子槽，配合随后的 op14 刷新对齐），空第一列表 + u16 0 + 第二奖励列表
// （0x77 完整物品条目：u16 slot, u32 itemID, u32 count，其余字节为零）。
func buildMagicBoxExpandAck(result MagicBoxResult) []byte {
	materialSlot := result.MaterialSlotIndex
	if result.MaterialItemID == 0 {
		materialSlot = result.BoxSlotIndex
	}
	out := []byte{1, 4}
	out = appendI16(out, int16(result.OpenCount))
	out = appendI16(out, result.BoxSlotIndex)
	out = appendI16(out, materialSlot)
	out = appendI16(out, 0)
	out = appendI16(out, 0)
	out = appendI16(out, int16(len(result.Rewards)))
	for _, reward := range result.Rewards {
		out = append(out, buildMagicBoxBatchRewardRow(reward)...)
	}
	return out
}

// buildCurrentBoosterPopupAck 是非赛丽亚源连开的 0x00A0 传统获得物品弹窗。
// 布局以当前 EXE 处理器 sub_1D2BFF0 的实读顺序为准（2026-07-26 IDA）：
// i32 1, i16 sourceSlot, i32 0, i32 0, u16 count，随后每行
// (i32 itemID, i32 count)。86JP 的 u8 头布局与该客户端读取器不对齐，
// 会渲染出乱码行，故按 IDA 证据构造。
func buildCurrentBoosterPopupAck(result MagicBoxResult) []byte {
	var writer []byte
	writer = appendI32(writer, 1)
	writer = appendI16(writer, result.BoxSlotIndex)
	writer = appendI32(writer, 0)
	writer = appendI32(writer, 0)
	writer = appendI16(writer, int16(len(result.Rewards)))
	for _, reward := range result.Rewards {
		writer = appendI32(writer, int32(reward.ItemID))
		writer = appendI32(writer, int32(reward.Count))
	}
	return writer
}
