package protocol

import "encoding/binary"

const (
	ChannelHeaderSize            = 13
	GameServerUpperHeaderSize    = ChannelHeaderSize
	GameServerUpperHeaderSize16  = 16
	DefaultChannelClassification = 1
)

// ChannelHeader 是客户端发来的 raw upper 13 字节包头。
type ChannelHeader struct {
	Classification byte
	MsgID          uint16
	Length         uint32
	Checksum       uint32
	Seq            uint16
}

type ChannelPacket struct {
	Header ChannelHeader
	Body   []byte
}

// BuildChannelPacket 封装客户端到服务端方向使用的 13 字节 raw upper 包。
func BuildChannelPacket(msgID uint16, body []byte, seq uint16, classification byte) ([]byte, error) {
	if classification == 0 {
		classification = DefaultChannelClassification
	}
	total := ChannelHeaderSize + len(body)
	if total > int(^uint32(0)) {
		return nil, ErrPacketTooLarge
	}
	packet := make([]byte, total)
	packet[0] = classification
	binary.LittleEndian.PutUint16(packet[1:3], msgID)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(total))
	binary.LittleEndian.PutUint16(packet[11:13], seq)
	copy(packet[ChannelHeaderSize:], body)
	sum, err := ChecksumRange(packet, 11, total-11)
	if err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint32(packet[7:11], sum)
	return packet, nil
}

// BuildGameServerUpperPacket 封装 game 端口服务端发给客户端的 raw upper 包。
// 最新 C# 重写文件与 7001 upper 一致使用 13 字节头；16 字节头只保留为兼容实验。
func BuildGameServerUpperPacket(msgID uint16, body []byte, seq uint16, classification byte) ([]byte, error) {
	return BuildGameServerUpperPacketWithHeaderSize(msgID, body, seq, classification, GameServerUpperHeaderSize)
}

// BuildGameServerUpperPacketWithHeaderSize 按指定头长封装 game raw upper。
//
// headerSize 只允许 13 或 16。16 字节模式会在 seq 后补 3 个保留字节，
// 用于回退旧实验；主路径必须保持 13 字节，避免客户端按 ChannelPacket 读体时错位。
func BuildGameServerUpperPacketWithHeaderSize(msgID uint16, body []byte, seq uint16, classification byte, headerSize int) ([]byte, error) {
	if headerSize != ChannelHeaderSize && headerSize != GameServerUpperHeaderSize16 {
		return nil, ErrPacketLength
	}
	total := headerSize + len(body)
	if total > int(^uint32(0)) {
		return nil, ErrPacketTooLarge
	}
	packet := make([]byte, total)
	packet[0] = classification
	binary.LittleEndian.PutUint16(packet[1:3], msgID)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(total))
	binary.LittleEndian.PutUint16(packet[11:13], seq)
	copy(packet[headerSize:], body)
	sum, err := ChecksumRange(packet, 11, total-11)
	if err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint32(packet[7:11], sum)
	return packet, nil
}

func ParseChannelPacket(packet []byte) (ChannelPacket, error) {
	return parseChannelPacket(packet, true)
}

// ParseChannelPacketUnchecked 只按 13 字节 upper 包头切 body，不校验 checksum。
//
// 最新 game 端口上行的部分 raw upper 包会在 body 里携带 codec/encrypted payload，
// 线上的 0x02B2 样本显示同 msg/length 下 body 每次变化但 header checksum 固定。
// 分流阶段必须先保住 msg/length 语义，checksum 只能作为后续诊断信号。
func ParseChannelPacketUnchecked(packet []byte) (ChannelPacket, error) {
	return parseChannelPacket(packet, false)
}

func parseChannelPacket(packet []byte, verifyChecksum bool) (ChannelPacket, error) {
	if len(packet) < ChannelHeaderSize {
		return ChannelPacket{}, ErrPacketTooShort
	}
	header := ChannelHeader{
		Classification: packet[0],
		MsgID:          binary.LittleEndian.Uint16(packet[1:3]),
		Length:         binary.LittleEndian.Uint32(packet[3:7]),
		Checksum:       binary.LittleEndian.Uint32(packet[7:11]),
		Seq:            binary.LittleEndian.Uint16(packet[11:13]),
	}
	if header.Length < ChannelHeaderSize || int(header.Length) != len(packet) {
		return ChannelPacket{}, ErrPacketLength
	}
	if verifyChecksum {
		sum, err := ChecksumRange(packet, 11, len(packet)-11)
		if err != nil {
			return ChannelPacket{}, err
		}
		if sum != header.Checksum {
			return ChannelPacket{}, ErrChecksumInvalid
		}
	}
	return ChannelPacket{
		Header: header,
		Body:   cloneBytes(packet[ChannelHeaderSize:]),
	}, nil
}

func cloneBytes(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	return append([]byte(nil), data...)
}
