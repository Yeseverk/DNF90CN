// 本文件按 NoPack S2C 889/sub_1D42AA0 的读取顺序构造攻坚用户检查结果体。
package raid

// RaidCheckUserResultMsgID 是当前 EXE 注册的攻坚用户检查 S2C command id。
const RaidCheckUserResultMsgID uint16 = 889

// RaidRequestMembersResultMsgID 是当前 EXE 注册的攻坚成员请求 S2C command id。
const RaidRequestMembersResultMsgID uint16 = 888

const (
	// CheckUserModeRefresh 对应 sub_1D42AA0 的 mode=0 分支：清空对象、读取三组列表并刷新 UI。
	CheckUserModeRefresh uint32 = 0
	// CheckUserModeRefreshWithHeaderState 对应 sub_1D42AA0 的 mode=1 分支：同样读取三组列表，并额外使用 25 字节头里的状态字段。
	CheckUserModeRefreshWithHeaderState uint32 = 1
)

const (
	// RequestMembersResultEnterPublicRaidState 对应 sub_1D3B0E0 的 result=2 分支：调用 sub_1CA88E0(obj, 8)。
	RequestMembersResultEnterPublicRaidState uint32 = 2
	// RequestMembersResultSkipLocalError 对应 sub_1D3B0E0 的 result=3 分支：跳过本地错误提示和 state=10 切换。
	RequestMembersResultSkipLocalError uint32 = 3
	// requestMembersUIStatePublicRaid 是 888 result=2 触发的客户端 UI 状态码，不是服务端回包字段。
	requestMembersUIStatePublicRaid uint32 = 8
	// requestMembersUIStateLocalFailure 是 888 其他失败分支触发的客户端 UI 状态码，不是服务端回包字段。
	requestMembersUIStateLocalFailure uint32 = 10
)

// CheckUserHeader 是 sub_1D42AA0 开头 upper_pkt_read_raw(0x19) 读取的 25 字节头。
// offset 13..20 暂无可靠语义名，先保留原始 8 字节，避免乱命名污染服务端 owner。
type CheckUserHeader struct {
	Mode        uint32
	Field4      uint32
	Field8      uint32
	Field12     byte
	Raw13To20   [8]byte
	Field21To24 uint32
}

// CheckUserResult 是 S2C 889 的保守构造输入。
// 三组列表分别进入对象 +0xA4/+0xB8/+0xCC；第一组还会派生 +0xE0 显示项。
type CheckUserResult struct {
	Header     CheckUserHeader
	FirstRows  [][12]byte
	SecondRows [][12]byte
	StateRows  [][5]byte
}

// BuildCheckUserResultBody 构造 S2C 889：25 字节头 + 三组 count/raw 列表。
// 该构造器不表示业务成功；是否发送必须由公共频道/raid owner 按真实状态决定。
func BuildCheckUserResultBody(result CheckUserResult) []byte {
	var writer packetWriter
	writer.writeUint32(result.Header.Mode)
	writer.writeUint32(result.Header.Field4)
	writer.writeUint32(result.Header.Field8)
	writer.writeByte(result.Header.Field12)
	writer.writeRaw(result.Header.Raw13To20[:])
	writer.writeUint32(result.Header.Field21To24)
	writeCheckUserRows12(&writer, result.FirstRows)
	writeCheckUserRows12(&writer, result.SecondRows)
	writer.writeUint32(uint32(len(result.StateRows)))
	for _, row := range result.StateRows {
		writer.writeRaw(row[:])
	}
	return writer.bytes()
}

func writeCheckUserRows12(writer *packetWriter, rows [][12]byte) {
	writer.writeUint32(uint32(len(rows)))
	for _, row := range rows {
		writer.writeRaw(row[:])
	}
}

// BuildRequestMembersResultBody 构造 S2C 888：raw[5] = u32 result + u8 flag。
// 该构造器只固定 EXE 读取顺序，不表示默认允许或成功。
func BuildRequestMembersResultBody(result uint32, flag byte) []byte {
	var writer packetWriter
	writer.writeUint32(result)
	writer.writeByte(flag)
	return writer.bytes()
}
