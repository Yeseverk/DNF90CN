// 本文件解析队伍模块已追到读取顺序的客户端请求体。
package party

import (
	"encoding/binary"
	"errors"
)

// SetPartyInfoRequest 是创建/修改队伍弹窗提交后的已知字段。
type SetPartyInfoRequest struct {
	Prefix0          byte
	Prefix1          byte
	NameBytes        []byte
	MemberSelectCode byte
	MaxMembers       byte
	SelectionID      uint32
	SelectionCode    byte
	SelectionValue   uint16
	RecruitFlag      byte
	TargetMode       byte
	TargetDungeonID  uint16
}

// QuickPartyTarget 是 0x01BB 快速组队登记里的一条目标记录。
type QuickPartyTarget struct {
	TargetID  uint32
	Available byte
}

// RegisterQuickPartyRequest 保存 0x01BB 的批量目标列表。
type RegisterQuickPartyRequest struct {
	Targets []QuickPartyTarget
}

// ReserveLeavePartyRequest 保存 0x02B3 预约离队/取消预约的本地状态位。
type ReserveLeavePartyRequest struct {
	Flag byte
}

// EntryIntoPartyRequest 保存 0x02C1 申请进入队伍时携带的目标角色/对象 ID。
type EntryIntoPartyRequest struct {
	TargetID uint32
}

// ResponsePeerRequest 保存 0x000B 的通用 peer 回应体；组队邀请只使用其中 mode=4/13 的分支。
type ResponsePeerRequest struct {
	TargetID uint16
	Mode     byte
	Value    uint32
}

// RequestPeerRequest 保存 0x000A 的通用 peer 请求体。
// 当前 EXE 按 mode 使用两种尾部：mode 0/10/12/15 写
// u32 value0,u16 value1,u32 value2；mode 1/4/13 写
// u32 value0,u32 value2。
type RequestPeerRequest struct {
	TargetID uint16
	Mode     byte
	Value0   uint32
	Value1   uint16
	Value2   uint32
}

// WalkoutPartyMemberRequest 保存 0x000E 队长踢出队员时写入的槽位号。
type WalkoutPartyMemberRequest struct {
	Slot byte
}

// ChangePartyMemberPositionRequest 保存 0x014F 队伍槽位位置调整请求。
type ChangePartyMemberPositionRequest struct {
	Slot     byte
	Position byte
}

var errSetPartyInfoBodyShort = errors.New("set party info body too short")
var errRegisterQuickPartyBodyShort = errors.New("register quick party body too short")
var errReserveLeavePartyBodyShort = errors.New("reserve leave party body too short")
var errEntryIntoPartyBodyShort = errors.New("entry into party body too short")
var errRequestPeerBodyShort = errors.New("request peer body too short")
var errResponsePeerBodyShort = errors.New("response peer body too short")
var errWalkoutPartyMemberBodyShort = errors.New("walkout party member body too short")
var errChangePartyMemberPositionBodyShort = errors.New("change party member position body too short")
var errChangePartyMemberPositionInvalid = errors.New("change party member position invalid")

var partyNamePresetTable = [][]byte{
	[]byte("\u5F3A\u5F3A\u8054\u5408~"),
	[]byte("\u75AF\u72C2\u7EC3\u7EA7\uFF01 \u6709\u610F\u8005\u901F\u5EA6\u7EC4\u961F\uFF01 "),
	[]byte("\u4EBA\u54C1\u597D\u7684\uFF0C \u8BF7\u6765\u8FD9\u91CC\u5427\u3002 "),
	[]byte("\u53D7\u8D5B\u4E3D\u4E9A\u795D\u798F\u7684\u961F\u4F0D"),
	[]byte("\u5237\u7A00\u6709\u88C5\u5907\uFF0C \u6709\u610F\u8005\u901F\u6765\u3002 "),
	[]byte("\u52DF\u96C6\u5F3A\u8005\u961F\u4F0D\u3002 "),
}

// DecodeSetPartyInfoRequest follows the current NoPack.exe op12 writer:
// mode,u8 namePreset,[DSTR customName],u8 memberCode,u32 selectionID,
// u8 selectionCode,u16 selectionValue,u8 recruitFlag,u8 targetMode,u16 targetID.
func DecodeSetPartyInfoRequest(body []byte) (SetPartyInfoRequest, error) {
	const fixedTailSize = 12
	if len(body) < 2+fixedTailSize {
		return SetPartyInfoRequest{}, errSetPartyInfoBodyShort
	}
	req := SetPartyInfoRequest{
		Prefix0:    body[0],
		Prefix1:    body[1],
		MaxMembers: 4,
	}
	tailOffset := 2
	if req.Prefix1 == 0 {
		if len(body) < 6+fixedTailSize {
			return SetPartyInfoRequest{}, errSetPartyInfoBodyShort
		}
		nameLen := int(int32(binary.LittleEndian.Uint32(body[2:6])))
		if nameLen < 0 || nameLen > 128 || 6+nameLen+fixedTailSize != len(body) {
			return SetPartyInfoRequest{}, errSetPartyInfoBodyShort
		}
		req.NameBytes = append([]byte(nil), body[6:6+nameLen]...)
		tailOffset = 6 + nameLen
	} else if len(body) != 2+fixedTailSize {
		return SetPartyInfoRequest{}, errSetPartyInfoBodyShort
	}
	if len(body)-tailOffset != fixedTailSize {
		return SetPartyInfoRequest{}, errSetPartyInfoBodyShort
	}
	req.MemberSelectCode = body[tailOffset]
	req.SelectionID = binary.LittleEndian.Uint32(body[tailOffset+1 : tailOffset+5])
	req.SelectionCode = body[tailOffset+5]
	req.SelectionValue = binary.LittleEndian.Uint16(body[tailOffset+6 : tailOffset+8])
	req.RecruitFlag = body[tailOffset+8]
	req.TargetMode = body[tailOffset+9]
	req.TargetDungeonID = binary.LittleEndian.Uint16(body[tailOffset+10 : tailOffset+12])
	req.MaxMembers = decodePartyMaxMembers(req.MemberSelectCode)
	if len(req.NameBytes) == 0 {
		req.NameBytes = resolvePartyNamePreset(req.Prefix1)
	}
	return req, nil
}

// DecodeRegisterQuickPartyRequest 按 EXE 0x21B20C0 读取顺序解析：u32 count + count*(u32 target,u8 available)。
func DecodeRegisterQuickPartyRequest(body []byte) (RegisterQuickPartyRequest, error) {
	if len(body) < 4 {
		return RegisterQuickPartyRequest{}, errRegisterQuickPartyBodyShort
	}
	count := binary.LittleEndian.Uint32(body[:4])
	if count > uint32((len(body)-4)/5) {
		return RegisterQuickPartyRequest{}, errRegisterQuickPartyBodyShort
	}
	need := 4 + int(count)*5
	if len(body) != need {
		return RegisterQuickPartyRequest{}, errRegisterQuickPartyBodyShort
	}
	req := RegisterQuickPartyRequest{Targets: make([]QuickPartyTarget, 0, int(count))}
	offset := 4
	for i := uint32(0); i < count; i++ {
		req.Targets = append(req.Targets, QuickPartyTarget{
			TargetID:  binary.LittleEndian.Uint32(body[offset : offset+4]),
			Available: body[offset+4],
		})
		offset += 5
	}
	return req, nil
}

// DecodeReserveLeavePartyRequest 按 EXE 0x2B3 发送点解析单字节 flag。
func DecodeReserveLeavePartyRequest(body []byte) (ReserveLeavePartyRequest, error) {
	if len(body) != 1 {
		return ReserveLeavePartyRequest{}, errReserveLeavePartyBodyShort
	}
	return ReserveLeavePartyRequest{Flag: body[0]}, nil
}

// DecodeEntryIntoPartyRequest 按 EXE 0x12C8B47 发送点解析：body 只写入一个 u32 目标 ID。
func DecodeEntryIntoPartyRequest(body []byte) (EntryIntoPartyRequest, error) {
	if len(body) != 4 {
		return EntryIntoPartyRequest{}, errEntryIntoPartyBodyShort
	}
	return EntryIntoPartyRequest{TargetID: binary.LittleEndian.Uint32(body[:4])}, nil
}

// DecodeRequestPeerRequest 按当前 EXE sub_325F9D0 等发送点解析：
// u16 target + u8 mode + u32 value0 + u16 value1 + u32 value2。
func DecodeRequestPeerRequest(body []byte) (RequestPeerRequest, error) {
	if len(body) < 7 {
		return RequestPeerRequest{}, errRequestPeerBodyShort
	}
	req := RequestPeerRequest{
		TargetID: binary.LittleEndian.Uint16(body[:2]),
		Mode:     body[2],
		Value0:   binary.LittleEndian.Uint32(body[3:7]),
	}
	switch req.Mode {
	case 0, 10, 12, 15:
		if len(body) != 13 {
			return RequestPeerRequest{}, errRequestPeerBodyShort
		}
		req.Value1 = binary.LittleEndian.Uint16(body[7:9])
		req.Value2 = binary.LittleEndian.Uint32(body[9:13])
	case 1, 4, 13:
		if len(body) != 11 {
			return RequestPeerRequest{}, errRequestPeerBodyShort
		}
		req.Value2 = binary.LittleEndian.Uint32(body[7:11])
	default:
		if len(body) != 7 {
			return RequestPeerRequest{}, errRequestPeerBodyShort
		}
	}
	return req, nil
}

// DecodeResponsePeerRequest 按 EXE 发送点解析：u16 target + u8 mode + u32 value。
func DecodeResponsePeerRequest(body []byte) (ResponsePeerRequest, error) {
	if len(body) != 7 {
		return ResponsePeerRequest{}, errResponsePeerBodyShort
	}
	return ResponsePeerRequest{
		TargetID: binary.LittleEndian.Uint16(body[:2]),
		Mode:     body[2],
		Value:    binary.LittleEndian.Uint32(body[3:7]),
	}, nil
}

// DecodeWalkoutPartyMemberRequest 按 EXE sub_32513D0 case 6 解析：body 只写入 u8 slot。
func DecodeWalkoutPartyMemberRequest(body []byte) (WalkoutPartyMemberRequest, error) {
	if len(body) != 1 {
		return WalkoutPartyMemberRequest{}, errWalkoutPartyMemberBodyShort
	}
	return WalkoutPartyMemberRequest{Slot: body[0]}, nil
}

// DecodeChangePartyMemberPositionRequest 按 EXE sub_30D0320 解析：u8 slot,u8 pos；pos 只接受 1 或 3。
func DecodeChangePartyMemberPositionRequest(body []byte) (ChangePartyMemberPositionRequest, error) {
	if len(body) != 2 {
		return ChangePartyMemberPositionRequest{}, errChangePartyMemberPositionBodyShort
	}
	req := ChangePartyMemberPositionRequest{
		Slot:     body[0],
		Position: body[1],
	}
	if req.Slot > 7 || (req.Position != 1 && req.Position != 3) {
		return ChangePartyMemberPositionRequest{}, errChangePartyMemberPositionInvalid
	}
	return req, nil
}

func decodePartyMaxMembers(code byte) byte {
	switch code {
	case 1, 2, 3, 4:
		return code
	case 6:
		return 4
	default:
		return 4
	}
}

func resolvePartyNamePreset(code byte) []byte {
	if code < 1 || int(code) > len(partyNamePresetTable) {
		return nil
	}
	return append([]byte(nil), partyNamePresetTable[int(code)-1]...)
}
