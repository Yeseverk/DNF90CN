package protocol

import "encoding/binary"

const LegacyGameHeaderSize = 13

// LegacyGameHeader 是当前 90 级客户端在 game 端口上行仍会发送的 13 字节业务头。
// 这里只做 wire 结构解析；checksum 字段受客户端会话编码影响，暂不作为丢包依据。
type LegacyGameHeader struct {
	Cmd      byte
	Type     uint16
	Length   uint32
	Checksum uint32
	Sequence uint16
}

// LegacyGamePacket 是 13 字节 game 业务包视图。
type LegacyGamePacket struct {
	Header LegacyGameHeader
	Body   []byte
}

// BuildFixed15GameServerPacket builds the fixed server-side header used by
// the GET_USERINFO/NOTI character-select exchange.  It is deliberately kept
// separate from the 13-byte client legacy header: byte 7 is the server route
// discriminator and bytes 8..14 are reserved.
func BuildFixed15GameServerPacket(cmd byte, typ uint16, body []byte, route byte) ([]byte, error) {
	const headerSize = 15
	total := headerSize + len(body)
	if uint64(total) > uint64(^uint32(0)) {
		return nil, ErrPacketTooLarge
	}
	packet := make([]byte, total)
	packet[0] = cmd
	binary.LittleEndian.PutUint16(packet[1:3], typ)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(total))
	packet[7] = route
	copy(packet[headerSize:], body)
	return packet, nil
}

// ParseLegacyGamePacket 解析客户端上行的 13 字节 game 业务包。
// 它不写业务状态，只返回 cmd/type/body 给服务端分发层继续判定。
func ParseLegacyGamePacket(packet []byte) (LegacyGamePacket, error) {
	if len(packet) < LegacyGameHeaderSize {
		return LegacyGamePacket{}, ErrPacketTooShort
	}
	length := binary.LittleEndian.Uint32(packet[3:7])
	if length < LegacyGameHeaderSize || int(length) != len(packet) {
		return LegacyGamePacket{}, ErrPacketLength
	}
	header := LegacyGameHeader{
		Cmd:      packet[0],
		Type:     binary.LittleEndian.Uint16(packet[1:3]),
		Length:   length,
		Checksum: binary.LittleEndian.Uint32(packet[7:11]),
		Sequence: binary.LittleEndian.Uint16(packet[11:13]),
	}
	return LegacyGamePacket{
		Header: header,
		Body:   cloneBytes(packet[LegacyGameHeaderSize:]),
	}, nil
}
