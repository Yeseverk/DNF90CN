package protocol

import (
	"bytes"
	"encoding/binary"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const (
	LegacyChannelHeaderSize = 11
	DofLoginTokenSize       = 32
	DofLoginPrefaceSize     = LegacyChannelHeaderSize + DofLoginTokenSize
	DofAskChannelSize       = 35
)

// IsDofLoginPrefacePrefix 判断连接开头是否为 NoPack/DOF 登录预握手。
func IsDofLoginPrefacePrefix(data []byte) bool {
	return hasLegacyPrefix(data, 0, dnfenum.LegacyMsgDofLoginPreface)
}

// IsDofChannelProbePrefix 判断是否为 NoPack/DOF 预登录中的 0x09 脚本请求。
func IsDofChannelProbePrefix(data []byte) bool {
	return hasLegacyPrefix(data, 0, dnfenum.LegacyMsgDofGetScript)
}

// IsDofAskChannelPrefix 判断是否为 DOF 预登录后的频道目录请求。
func IsDofAskChannelPrefix(data []byte) bool {
	return hasLegacyPrefix(data, 0, dnfenum.LegacyMsgAskChannelInfo)
}

// BuildDofLoginAck 构造 DOF 预登录 ack。
//
// 真实客户端先只读前 11 字节 ack，后续 4 字节保留位和 32 字节 key
// 仍按旧服格式一次写出，避免后续读取阶段缺数据。
func BuildDofLoginAck(aesKey string) []byte {
	body := make([]byte, 36)
	copy(body[4:], []byte(aesKey))
	return BuildLegacyChannelPacket(dnfenum.LegacyMsgDofLoginAck, body)
}

// BuildLegacyChannelPacket 构造 DOF 旧式频道包。
func BuildLegacyChannelPacket(msg dnfenum.LegacyChannelMsg, body []byte) []byte {
	return buildLegacyPacket(dnfenum.ChannelPacketClass, msg, body)
}

// BuildLegacyClientPacket 构造测试和兼容入口使用的客户端旧式频道包。
func BuildLegacyClientPacket(msg dnfenum.LegacyChannelMsg, body []byte) []byte {
	total := LegacyChannelHeaderSize + len(body)
	if fixed := legacyFixedLength(msg); fixed > total {
		total = fixed
	}
	packet := make([]byte, total)
	writeLegacyHeader(packet[:LegacyChannelHeaderSize], 0, byte(msg), total)
	copy(packet[LegacyChannelHeaderSize:], body)
	return packet
}

func buildLegacyPacket(command byte, msg dnfenum.LegacyChannelMsg, body []byte) []byte {
	packet := make([]byte, LegacyChannelHeaderSize+len(body))
	writeLegacyHeader(packet[:LegacyChannelHeaderSize], command, byte(msg), len(packet))
	copy(packet[LegacyChannelHeaderSize:], body)
	return packet
}

// LegacyPayload 返回旧式频道包的 payload 副本。
func LegacyPayload(packet []byte) []byte {
	if len(packet) <= LegacyChannelHeaderSize {
		return nil
	}
	return cloneBytes(packet[LegacyChannelHeaderSize:])
}

func hasLegacyPrefix(data []byte, command byte, msg dnfenum.LegacyChannelMsg) bool {
	if len(data) < LegacyChannelHeaderSize {
		return false
	}
	var want [LegacyChannelHeaderSize]byte
	writeLegacyHeader(want[:], command, byte(msg), legacyFixedLength(msg))
	return bytes.Equal(data[:LegacyChannelHeaderSize], want[:])
}

func writeLegacyHeader(header []byte, command byte, msgID byte, totalLen int) {
	header[0] = command
	header[1] = msgID
	binary.LittleEndian.PutUint32(header[2:6], uint32(totalLen))
	header[10] = 1
}

func legacyFixedLength(msg dnfenum.LegacyChannelMsg) int {
	switch msg {
	case dnfenum.LegacyMsgDofLoginPreface:
		return DofLoginPrefaceSize
	case dnfenum.LegacyMsgDofGetScript:
		return LegacyChannelHeaderSize
	case dnfenum.LegacyMsgAskChannelInfo:
		return DofAskChannelSize
	default:
		length := int(uint16(msg) >> 8)
		if length > 0 {
			return length
		}
		return LegacyChannelHeaderSize
	}
}
