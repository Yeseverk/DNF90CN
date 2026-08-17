package protocol

import "encoding/binary"

const LatestGameHeaderSize = 7

// LatestGameHeader 是最新 game 业务包 7 字节包头。
//
// 格式：u8 cmd + u16 type + u32 checksum，checksum 只覆盖 body。
type LatestGameHeader struct {
	Cmd      byte
	Type     uint16
	Checksum uint32
}

// LatestGamePacket 是最新 game 业务包视图。
type LatestGamePacket struct {
	Header LatestGameHeader
	Body   []byte
}

// BuildLatestGamePacket 构造最新 game 业务包。
func BuildLatestGamePacket(cmd byte, typ uint16, body []byte) ([]byte, error) {
	total := LatestGameHeaderSize + len(body)
	if total > int(^uint16(0)) {
		return nil, ErrPacketTooLarge
	}
	packet := make([]byte, total)
	packet[0] = cmd
	binary.LittleEndian.PutUint16(packet[1:3], typ)
	binary.LittleEndian.PutUint32(packet[3:7], Checksum(body))
	copy(packet[LatestGameHeaderSize:], body)
	return packet, nil
}

// ParseLatestGamePacket 解析最新 game 业务包，并校验 body checksum。
func ParseLatestGamePacket(packet []byte) (LatestGamePacket, error) {
	if len(packet) < LatestGameHeaderSize {
		return LatestGamePacket{}, ErrPacketTooShort
	}
	header := LatestGameHeader{
		Cmd:      packet[0],
		Type:     binary.LittleEndian.Uint16(packet[1:3]),
		Checksum: binary.LittleEndian.Uint32(packet[3:7]),
	}
	body := packet[LatestGameHeaderSize:]
	if Checksum(body) != header.Checksum {
		return LatestGamePacket{}, ErrChecksumInvalid
	}
	return LatestGamePacket{
		Header: header,
		Body:   cloneBytes(body),
	}, nil
}

// VerifyLatestGamePacket 只校验最新 game 业务包 checksum。
func VerifyLatestGamePacket(packet []byte) error {
	_, err := ParseLatestGamePacket(packet)
	return err
}
