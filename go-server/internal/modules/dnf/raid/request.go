// 本文件解析攻坚队管理类 C2S 请求体。
package raid

import (
	"encoding/binary"
	"errors"
)

// InfoRequest 是 C2S 0x298/664 创建攻坚队和 0x29E/670 修改攻坚队信息的共用包体。
// MCP 证据：sub_1B49240 先写 u8，再写 dstr 名称，最后写 u8。
type InfoRequest struct {
	RouteOrRaidType byte
	NameBytes       []byte
	TailFlag        byte
}

// LeaveRequest 是 C2S 0x299/665 离开攻坚队包体。
// MCP 证据：sub_17B1E30/sub_324F850 只写一个 u16。
type LeaveRequest struct {
	RaidOrMemberKey uint16
}

// RejoinRequest 是 C2S 0x29C/668 重新加入攻坚链路的客户端回包。
// MCP 证据：sub_1D638D0 的触发分支只回写一个 u32。
type RejoinRequest struct {
	RaidKey uint32
}

// SetWaitingRequest 是 C2S 0x29B/667 的攻坚等待状态请求。
// MCP 证据：sub_1B4C2A0 分支发送 u8 0，再发送 u8 sub_17B1CD0(...)。
type SetWaitingRequest struct {
	Flag          byte
	RouteRaidType byte
}

// EntryCostInfoRequest 是 C2S 0x292/658 的 raid 入场材料/消耗信息请求。
// MCP 20260705 证据：sub_17B19C0 发送 658，正文只写 u8(a2 != 0)。
type EntryCostInfoRequest struct {
	Enabled byte
}

// SetSymbolRequest 是 C2S 0x296/662 的 raid 标记设置请求。
// MCP 20260705 证据：sub_17B1600 发送 662，正文写 u32,u32,u8；第三字段必须小于 3。
type SetSymbolRequest struct {
	SourceValue uint32
	TargetValue uint32
	Symbol      byte
}

// ManagerWorkRequest 是 C2S 0x29D/669 的小组编辑请求。
type ManagerWorkRequest struct {
	ActionOrMode  uint32
	MemberCharKey uint32
	TargetGroup   uint32
}

// OtherChannelRequestJoinRequest 是 C2S 0x334/820 的跨频道攻坚申请。
// MCP 证据：sub_17B1530 写 u8,u16,u32,u16。
type OtherChannelRequestJoinRequest struct {
	Mode          byte
	TargetKey     uint16
	ClientValue   uint32
	RouteRaidType uint16
}

// MemberChangeStateRequest 是 C2S 0x337/823 的成员状态变化请求。
// MCP 证据：sub_17B1C40 只写 u8 state。
type MemberChangeStateRequest struct {
	State byte
}

// UserMoveChannelFailRequest 是 C2S 0x338/824 的跨频道移动失败上报。
// MCP 证据：sub_17B17A0 写 u8,u16。
type UserMoveChannelFailRequest struct {
	Mode      byte
	TargetKey uint16
}

// OtherChannelListRequest 是 C2S 0x33F/831 的公共频道攻坚列表请求。
// MCP 证据：sub_17B3140 写 u8=1，或 u8=0 + raw[8] + dstr name。
type OtherChannelListRequest struct {
	Mode        byte
	Context     [8]byte
	NameBytes   []byte
	HasContext  bool
	HasNameDstr bool
}

// CheckRaidUserRequest 是 C2S 0x379/889 的攻坚用户检查请求。
// MCP 证据：sub_17B1D50 写 u8,u16；S2C 889 是复杂列表，不在解析层伪造成功。
type CheckRaidUserRequest struct {
	Mode      byte
	TargetKey uint16
}

var errRaidInfoBodyShort = errors.New("raid info body too short")
var errRaidInfoNameTruncated = errors.New("raid info name dstr truncated")
var errRaidLeaveBodyShort = errors.New("raid leave body too short")
var errRaidStartBodyNotEmpty = errors.New("raid start body must be empty")
var errRaidRejoinBodyShort = errors.New("raid rejoin body too short")
var errRaidSetWaitingBodyShort = errors.New("raid set waiting body too short")
var errRaidEntryCostBodyShort = errors.New("raid entry cost info body too short")
var errRaidSetSymbolBodyShort = errors.New("raid set symbol body too short")
var errRaidSetSymbolInvalid = errors.New("raid set symbol must be less than 3")
var errManagerWorkBodyShort = errors.New("raid manager work body too short")
var errRaidOtherChannelJoinBodyShort = errors.New("raid other channel request join body too short")
var errRaidMemberStateBodyShort = errors.New("raid member change state body too short")
var errRaidMoveChannelFailBodyShort = errors.New("raid user move channel fail body too short")
var errRaidOtherChannelListBodyShort = errors.New("raid other channel list body too short")
var errRaidOtherChannelListNameTruncated = errors.New("raid other channel list name dstr truncated")
var errRaidCheckUserBodyShort = errors.New("raid check user body too short")

// DecodeCreateRaidRequest 按当前 EXE 写包顺序解析创建攻坚队请求。
func DecodeCreateRaidRequest(body []byte) (InfoRequest, error) {
	return decodeInfoRequest(body)
}

// DecodeModifyRaidInfoRequest 按当前 EXE 写包顺序解析修改攻坚队信息请求。
func DecodeModifyRaidInfoRequest(body []byte) (InfoRequest, error) {
	return decodeInfoRequest(body)
}

func decodeInfoRequest(body []byte) (InfoRequest, error) {
	if len(body) < 6 {
		return InfoRequest{}, errRaidInfoBodyShort
	}
	nameLen := int(binary.LittleEndian.Uint32(body[1:5]))
	end := 5 + nameLen
	if end+1 != len(body) {
		return InfoRequest{}, errRaidInfoNameTruncated
	}
	return InfoRequest{
		RouteOrRaidType: body[0],
		NameBytes:       append([]byte(nil), body[5:end]...),
		TailFlag:        body[end],
	}, nil
}

// DecodeLeaveRaidRequest parses the exact u16 body written by the current EXE.
func DecodeLeaveRaidRequest(body []byte) (LeaveRequest, error) {
	if len(body) != 2 {
		return LeaveRequest{}, errRaidLeaveBodyShort
	}
	return LeaveRequest{RaidOrMemberKey: binary.LittleEndian.Uint16(body[0:2])}, nil
}

// DecodeStartRaidRequest 校验开始攻坚请求；当前 EXE 没有写任何正文。
func DecodeStartRaidRequest(body []byte) error {
	if len(body) != 0 {
		return errRaidStartBodyNotEmpty
	}
	return nil
}

// DecodeRejoinRaidRequest 解析重新加入攻坚请求。
func DecodeRejoinRaidRequest(body []byte) (RejoinRequest, error) {
	if len(body) != 4 {
		return RejoinRequest{}, errRaidRejoinBodyShort
	}
	return RejoinRequest{RaidKey: binary.LittleEndian.Uint32(body[0:4])}, nil
}

// DecodeSetWaitingRequest 解析 667 攻坚等待状态请求。
func DecodeSetWaitingRequest(body []byte) (SetWaitingRequest, error) {
	if len(body) != 2 {
		return SetWaitingRequest{}, errRaidSetWaitingBodyShort
	}
	return SetWaitingRequest{Flag: body[0], RouteRaidType: body[1]}, nil
}

// DecodeEntryCostInfoRequest 解析 658 raid 入场消耗信息请求。
func DecodeEntryCostInfoRequest(body []byte) (EntryCostInfoRequest, error) {
	if len(body) != 1 {
		return EntryCostInfoRequest{}, errRaidEntryCostBodyShort
	}
	return EntryCostInfoRequest{Enabled: body[0]}, nil
}

// DecodeSetSymbolRequest 解析 662 raid 标记设置请求。
func DecodeSetSymbolRequest(body []byte) (SetSymbolRequest, error) {
	if len(body) != 9 {
		return SetSymbolRequest{}, errRaidSetSymbolBodyShort
	}
	if body[8] >= 3 {
		return SetSymbolRequest{}, errRaidSetSymbolInvalid
	}
	return SetSymbolRequest{
		SourceValue: binary.LittleEndian.Uint32(body[0:4]),
		TargetValue: binary.LittleEndian.Uint32(body[4:8]),
		Symbol:      body[8],
	}, nil
}

// DecodeManagerWorkRequest 按 NoPack sub_1B4D3D0/sub_1B480E0 的发送顺序解析 669。
func DecodeManagerWorkRequest(body []byte) (ManagerWorkRequest, error) {
	if len(body) != 12 {
		return ManagerWorkRequest{}, errManagerWorkBodyShort
	}
	return ManagerWorkRequest{
		ActionOrMode:  binary.LittleEndian.Uint32(body[0:4]),
		MemberCharKey: binary.LittleEndian.Uint32(body[4:8]),
		TargetGroup:   binary.LittleEndian.Uint32(body[8:12]),
	}, nil
}

// DecodeOtherChannelRequestJoinRequest 解析 820 跨频道攻坚申请请求。
func DecodeOtherChannelRequestJoinRequest(body []byte) (OtherChannelRequestJoinRequest, error) {
	if len(body) != 9 {
		return OtherChannelRequestJoinRequest{}, errRaidOtherChannelJoinBodyShort
	}
	return OtherChannelRequestJoinRequest{
		Mode:          body[0],
		TargetKey:     binary.LittleEndian.Uint16(body[1:3]),
		ClientValue:   binary.LittleEndian.Uint32(body[3:7]),
		RouteRaidType: binary.LittleEndian.Uint16(body[7:9]),
	}, nil
}

// DecodeMemberChangeStateRequest 解析 823 攻坚成员状态变化请求。
func DecodeMemberChangeStateRequest(body []byte) (MemberChangeStateRequest, error) {
	if len(body) != 1 {
		return MemberChangeStateRequest{}, errRaidMemberStateBodyShort
	}
	return MemberChangeStateRequest{State: body[0]}, nil
}

// DecodeUserMoveChannelFailRequest 解析 824 跨频道移动失败上报。
func DecodeUserMoveChannelFailRequest(body []byte) (UserMoveChannelFailRequest, error) {
	if len(body) != 3 {
		return UserMoveChannelFailRequest{}, errRaidMoveChannelFailBodyShort
	}
	return UserMoveChannelFailRequest{Mode: body[0], TargetKey: binary.LittleEndian.Uint16(body[1:3])}, nil
}

// DecodeOtherChannelListRequest 解析 831 公共频道攻坚列表请求。
func DecodeOtherChannelListRequest(body []byte) (OtherChannelListRequest, error) {
	if len(body) < 1 {
		return OtherChannelListRequest{}, errRaidOtherChannelListBodyShort
	}
	out := OtherChannelListRequest{Mode: body[0]}
	if out.Mode != 0 {
		if len(body) != 1 {
			return OtherChannelListRequest{}, errRaidOtherChannelListBodyShort
		}
		return out, nil
	}
	if len(body) < 13 {
		return OtherChannelListRequest{}, errRaidOtherChannelListBodyShort
	}
	copy(out.Context[:], body[1:9])
	out.HasContext = true
	nameLen := int(binary.LittleEndian.Uint32(body[9:13]))
	end := 13 + nameLen
	if end != len(body) {
		return OtherChannelListRequest{}, errRaidOtherChannelListNameTruncated
	}
	out.NameBytes = append([]byte(nil), body[13:end]...)
	out.HasNameDstr = true
	return out, nil
}

// DecodeCheckRaidUserRequest 解析 889 攻坚用户检查请求。
func DecodeCheckRaidUserRequest(body []byte) (CheckRaidUserRequest, error) {
	if len(body) != 3 {
		return CheckRaidUserRequest{}, errRaidCheckUserBodyShort
	}
	return CheckRaidUserRequest{Mode: body[0], TargetKey: binary.LittleEndian.Uint16(body[1:3])}, nil
}
