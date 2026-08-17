// 本文件按当前 EXE/C# 证据构造队伍 S2C body。
package party

import "longheng.io/server/internal/modules/dnf/alignedcmd"

const (
	fullPartyMemberPercent = 100
	defaultTownUserState   = 1
	partyPeerMTU           = 1500
	partyPeerEndpointSize  = 22
	directoryMemberSlots   = 6
	directoryOrdinaryState = 0xff
	directoryEmptyState    = 0xff
	directoryEmptyUserID   = 0xffff

	// DirectoryTypeOrdinary selects the first current-client party directory.
	DirectoryTypeOrdinary byte = 2
)

// PeerEndpoint is one current-client PARTY_IP_INFO member row. IPv4 is kept
// as four network-order octets while Port is written in network byte order;
// the remaining numeric fields use the packet's ordinary little-endian order.
type PeerEndpoint struct {
	UserID    uint16
	IPv4      [4]byte
	Port      uint16
	AccountID uint32
}

// DirectoryRecord is the server-owned public party-directory projection used
// by the current client class0/op87 reader.
type DirectoryRecord struct {
	PartyID     uint16
	SelectionID uint32
	MemberIDs   []uint16
}

// BuildDirectorySnapshot constructs the current NoPack class0/op87 body:
//
//	u16 count
//	repeat count {
//	  u16 partyID
//	  u8 reservedA
//	  u8 directoryType
//	  u8 reservedB
//	  u8 specialState
//	  u32 selectionID
//	  repeat 6 { u8 memberState; u16 characterID }
//	}
//
// Current EXE sub_1D3F040 subtracts its protected logical base value 2 from
// directoryType before indexing a five-element directory container. Therefore
// this byte is a directory discriminator, not the resident channel ID. This
// builder currently publishes only ordinary-dungeon parties and deliberately
// fixes directoryType to 2 (container index zero). sub_1D3F040 reads but does
// not retain reservedA/reservedB. Ordinary parties must use specialState FF:
// any other value enters the optional complete-display subsystem. Occupied
// slots use neutral state zero; unused slots use the exact FF/FFFF sentinels.
func BuildDirectorySnapshot(records []DirectoryRecord) []byte {
	if len(records) > int(^uint16(0)) {
		records = records[:int(^uint16(0))]
	}
	var writer packetWriter
	writer.writeUint16(uint16(len(records)))
	for _, record := range records {
		writer.writeUint16(record.PartyID)
		writer.writeByte(0)
		writer.writeByte(DirectoryTypeOrdinary)
		writer.writeByte(0)
		writer.writeByte(directoryOrdinaryState)
		writer.writeUint32(record.SelectionID)
		for slot := 0; slot < directoryMemberSlots; slot++ {
			if slot < len(record.MemberIDs) &&
				record.MemberIDs[slot] != 0 &&
				record.MemberIDs[slot] != directoryEmptyUserID {
				writer.writeByte(0)
				writer.writeUint16(record.MemberIDs[slot])
				continue
			}
			writer.writeByte(directoryEmptyState)
			writer.writeUint16(directoryEmptyUserID)
		}
	}
	return writer.bytes()
}

// BuildSingleMemberRealtimeInfo 构造 NOTI 0x0099 PARTY_MEMBER_REALTIME_INFO。
func BuildSingleMemberRealtimeInfo(state alignedcmd.PartyState) []byte {
	if state.PartyID <= 0 || state.UserID == 0 {
		return []byte{0}
	}
	members := partyMembers(state)
	var writer packetWriter
	writer.writeByte(byte(len(members)))
	for slot, member := range members {
		writer.writeUint16(member.UserID)
		writer.writeByte(memberPercentByte(member.HPPercent))
		writer.writeByte(0)
		writer.writeByte(byte(slot))
	}
	return writer.bytes()
}

// BuildEmptyRealtimeInfo 构造退出队伍后用于清空实时队员 UI 的空列表。
func BuildEmptyRealtimeInfo() []byte {
	return []byte{0}
}

// BuildPeerEndpointInfo constructs current-client class0/op11
// PARTY_IP_INFO. The current reader consumes one count byte followed by 22
// bytes per member:
//
//	u16 uid, 4B inner IPv4, 4B outer IPv4, u16be port,
//	u32 account ID, u8 NAT type, u32 MTU, u8 character attribute.
//
// The local/LAN runtime deliberately publishes the observed endpoint as both
// inner and outer IPv4, NAT type zero, MTU 1500, and character attribute zero.
func BuildPeerEndpointInfo(endpoints []PeerEndpoint) []byte {
	if len(endpoints) > 4 {
		endpoints = endpoints[:4]
	}
	var writer packetWriter
	writer.writeByte(byte(len(endpoints)))
	for _, endpoint := range endpoints {
		writer.writeUint16(endpoint.UserID)
		for _, octet := range endpoint.IPv4 {
			writer.writeByte(octet)
		}
		for _, octet := range endpoint.IPv4 {
			writer.writeByte(octet)
		}
		writer.writeByte(byte(endpoint.Port >> 8))
		writer.writeByte(byte(endpoint.Port))
		writer.writeUint32(endpoint.AccountID)
		writer.writeByte(0)
		writer.writeUint32(partyPeerMTU)
		writer.writeByte(0)
	}
	return writer.bytes()
}

// BuildReserveLeavePartyAck 构造 0x02B3 成功响应：success,u8 flag,u16 targetChar。
func BuildReserveLeavePartyAck(flag byte, targetChar uint16) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeByte(flag)
	writer.writeUint16(targetChar)
	return writer.bytes()
}

// BuildEntryIntoPartyAck 构造 0x02C1 成功响应：success,u32 target,u32 currentChar。
func BuildEntryIntoPartyAck(targetID uint32, currentCharID uint32) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeUint32(targetID)
	writer.writeUint32(currentCharID)
	return writer.bytes()
}

// BuildEntryIntoPartyFinishEmptyBody constructs class0/op706 when the bridge
// has no peer-value rows to publish. Current EXE sub_1D56F60 always consumes
// state:u8 and count:u8 before reading count pairs of u32 key/u32 value.
func BuildEntryIntoPartyFinishEmptyBody(state byte) []byte {
	return []byte{state, 0}
}

// BuildRequestPeerAck 构造 0x000A 的 class1 最小成功响应。
// 当前 EXE sub_2261E30 先消费 success:u8；success 非零时
// sub_1D11A80 立即返回，不再读取业务字段。
func BuildRequestPeerAck() []byte {
	return []byte{1}
}

// BuildRequestPeerSelectionNotice constructs the class0/op7 mode-15 reply
// that completes a town-player click. Current EXE sub_1D82CF0 reads
// target:u16, mode:u8, value0:u32 and party_marker:u16. The marker is not an
// opaque echo: 0xffff clears the selected actor's party-present flag, while
// every other value sets it, before sub_325FA70 opens the interaction menu.
// The trailing value2:u32 from the class1/op10 request is not consumed by
// this mode and therefore must not be appended to the notification.
func BuildRequestPeerSelectionNotice(targetID uint16, value0 uint32, targetHasParty bool) []byte {
	var writer packetWriter
	writer.writeUint16(targetID)
	writer.writeByte(15)
	writer.writeUint32(value0)
	if targetHasParty {
		writer.writeUint16(0)
	} else {
		writer.writeUint16(^uint16(0))
	}
	return writer.bytes()
}

// BuildRequestPeerNotice constructs current NoPack class0/op7 for an online
// peer request. sub_1D82CF0 consumes a mode-specific tail: ordinary party
// mode 0 has three u16 values and one u32 server value, trade mode 1 has two
// u32 values, quick-party modes 4/13 consume only the common prefix, and
// mode 15 is the local selection-menu completion handled above.
func BuildRequestPeerNotice(sourceID uint16, req RequestPeerRequest) []byte {
	var writer packetWriter
	writer.writeUint16(sourceID)
	writer.writeByte(req.Mode)
	writer.writeUint32(req.Value0)
	switch req.Mode {
	case 0, 12:
		writer.writeUint16(req.Value1)
		writer.writeUint16(0)
		writer.writeUint16(0)
		writer.writeUint32(req.Value2)
	case 1:
		writer.writeUint32(req.Value2)
	case 10:
		writer.writeUint16(req.Value1)
		writer.writeUint32(req.Value2)
	}
	return writer.bytes()
}

// BuildResponsePeerAckPayload is appended after the class1/op11 success byte.
// Current sub_1D12480 always consumes target:u16 and mode:u8; trade mode 1
// consumes one additional state byte before entering its local trade path.
func BuildResponsePeerAckPayload(targetID uint16, mode byte) []byte {
	var writer packetWriter
	writer.writeUint16(targetID)
	writer.writeByte(mode)
	if mode == 1 {
		writer.writeByte(0)
	}
	return writer.bytes()
}

// BuildResponsePeerNotice constructs class0/op8 for the original requester.
// Current sub_1D83250 consumes responder:u16, mode:u8 and value:u32.
func BuildResponsePeerNotice(responderID uint16, mode byte, value uint32) []byte {
	var writer packetWriter
	writer.writeUint16(responderID)
	writer.writeByte(mode)
	writer.writeUint32(value)
	return writer.bytes()
}

// BuildQuickPartyInvite 构造 0x01BB/443 目标端邀请提示：u16 inviter,u8 mode。
func BuildQuickPartyInvite(inviterID uint16, mode byte) []byte {
	var writer packetWriter
	writer.writeUint16(inviterID)
	writer.writeByte(mode)
	return writer.bytes()
}

// BuildWalkoutPartyMemberNotice 构造 0x000A 队伍踢人/返回城镇通知：u8 slot,u8 mode。
func BuildWalkoutPartyMemberNotice(slot byte, mode byte) []byte {
	var writer packetWriter
	writer.writeByte(slot)
	writer.writeByte(mode)
	return writer.bytes()
}

// BuildChangePartyMemberPositionAck constructs the exact current-EXE op335
// result body: requested position followed by result code (1=success).
func BuildChangePartyMemberPositionAck(position byte, result byte) []byte {
	var writer packetWriter
	writer.writeByte(position)
	writer.writeByte(result)
	return writer.bytes()
}

func partyMembers(state alignedcmd.PartyState) []alignedcmd.PartyMemberState {
	members := make([]alignedcmd.PartyMemberState, 0, 4)
	for _, member := range state.Members {
		if member.UserID == 0 {
			continue
		}
		members = append(members, member)
		if len(members) == 4 {
			break
		}
	}
	if len(members) == 0 && state.UserID != 0 {
		members = append(members, alignedcmd.PartyMemberState{
			UserID:    state.UserID,
			UserState: state.UserState,
			HPPercent: fullPartyMemberPercent,
			MPPercent: fullPartyMemberPercent,
		})
	}
	return members
}

func memberUserState(member alignedcmd.PartyMemberState, fallback byte) byte {
	if member.UserState != 0 {
		return member.UserState
	}
	if fallback != 0 {
		return fallback
	}
	return defaultTownUserState
}

func memberPercentByte(value byte) byte {
	if value == 0 {
		return fullPartyMemberPercent
	}
	if value > fullPartyMemberPercent {
		return fullPartyMemberPercent
	}
	return value
}
