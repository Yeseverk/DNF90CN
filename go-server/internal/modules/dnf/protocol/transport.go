package protocol

import "encoding/binary"

const (
	TCPOuterHeaderSize = 12
	InnerHeaderSize    = 9
	MinTCPFrameSize    = TCPOuterHeaderSize + InnerHeaderSize + LatestGameHeaderSize
	NormalInnerKind    = 2
)

// TransportOptions 控制最新 game TCP/UDP 外壳字段。
type TransportOptions struct {
	Sequence    uint32
	Route       byte
	Flag        byte
	OuterKind   uint16
	OuterValue4 uint32
	OuterValue8 uint32
}

// LatestGameTransportHeader 是最新 game transport 外壳视图。
type LatestGameTransportHeader struct {
	OuterKind      uint16
	TotalLength    uint16
	OuterValue4    uint32
	OuterValue8    uint32
	InnerKind      byte
	Sequence       uint32
	BusinessLength uint16
	Route          byte
	Flag           byte
}

// LatestGameTransportRecord 是一个 transport inner record。
type LatestGameTransportRecord struct {
	TransportHeader LatestGameTransportHeader
	GameHeader      LatestGameHeader
	Body            []byte
}

// BuildLatestGameTCP 按最新 game TCP transport 封包。
//
// 对应 IDA：0x35F27E0 按 frame+2 的 u16 totalLen 拆 TCP 帧；
// 0x35EC7A0 跳过 12 字节外层；0x35EDED0 按 u8 kind、u32 seq、+5 长度、+9 payload 解析 kind=2 inner record。
func BuildLatestGameTCP(cmd byte, typ uint16, body []byte, options TransportOptions) ([]byte, error) {
	businessPacket, err := BuildLatestGamePacket(cmd, typ, body)
	if err != nil {
		return nil, err
	}
	return BuildLatestGameTCPFromBusinessPacket(businessPacket, options)
}

// BuildLatestGameTCPFromBusinessPacket 把 7 字节业务包包进最新 TCP transport。
func BuildLatestGameTCPFromBusinessPacket(businessPacket []byte, options TransportOptions) ([]byte, error) {
	if len(businessPacket) < LatestGameHeaderSize {
		return nil, ErrPacketTooShort
	}
	if len(businessPacket) > int(^uint16(0)) {
		return nil, ErrPacketTooLarge
	}
	total := TCPOuterHeaderSize + InnerHeaderSize + len(businessPacket)
	if total > int(^uint16(0)) {
		return nil, ErrPacketTooLarge
	}
	packet := make([]byte, total)
	binary.LittleEndian.PutUint16(packet[0:2], options.OuterKind)
	binary.LittleEndian.PutUint16(packet[2:4], uint16(total))
	binary.LittleEndian.PutUint32(packet[4:8], options.OuterValue4)
	binary.LittleEndian.PutUint32(packet[8:12], options.OuterValue8)
	writeInnerRecord(packet[TCPOuterHeaderSize:], businessPacket, options)
	return packet, nil
}

// BuildLatestGameUDP 按最新 game UDP inner record 封包。
func BuildLatestGameUDP(cmd byte, typ uint16, body []byte, options TransportOptions) ([]byte, error) {
	businessPacket, err := BuildLatestGamePacket(cmd, typ, body)
	if err != nil {
		return nil, err
	}
	return BuildLatestGameUDPFromBusinessPacket(businessPacket, options)
}

// BuildLatestGameUDPFromBusinessPacket 把 7 字节业务包包进 UDP inner record。
func BuildLatestGameUDPFromBusinessPacket(businessPacket []byte, options TransportOptions) ([]byte, error) {
	if len(businessPacket) < LatestGameHeaderSize {
		return nil, ErrPacketTooShort
	}
	if len(businessPacket) > int(^uint16(0)) {
		return nil, ErrPacketTooLarge
	}
	packet := make([]byte, InnerHeaderSize+len(businessPacket))
	writeInnerRecord(packet, businessPacket, options)
	return packet, nil
}

// ParseLatestGameTCPRecords 解析一个完整最新 game TCP frame。
func ParseLatestGameTCPRecords(frame []byte) ([]LatestGameTransportRecord, error) {
	if len(frame) < TCPOuterHeaderSize {
		return nil, ErrPacketTooShort
	}
	total := int(binary.LittleEndian.Uint16(frame[2:4]))
	if total < MinTCPFrameSize || total > len(frame) {
		return nil, ErrPacketLength
	}
	outerKind := binary.LittleEndian.Uint16(frame[0:2])
	outerValue4 := binary.LittleEndian.Uint32(frame[4:8])
	outerValue8 := binary.LittleEndian.Uint32(frame[8:12])
	records := make([]LatestGameTransportRecord, 0, 1)
	offset := TCPOuterHeaderSize
	for offset < total {
		record, consumed, err := parseInnerRecord(frame, offset, total-offset, outerKind, uint16(total), outerValue4, outerValue8)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
		offset += consumed
	}
	if len(records) == 0 {
		return nil, ErrPacketLength
	}
	return records, nil
}

// ParseLatestGameUDPRecords 解析一个或多个 UDP inner record。
func ParseLatestGameUDPRecords(packet []byte) ([]LatestGameTransportRecord, error) {
	if len(packet) < InnerHeaderSize+LatestGameHeaderSize {
		return nil, ErrPacketTooShort
	}
	records := make([]LatestGameTransportRecord, 0, 1)
	offset := 0
	for offset < len(packet) {
		record, consumed, err := parseInnerRecord(packet, offset, len(packet)-offset, 0, 0, 0, 0)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
		offset += consumed
	}
	if len(records) == 0 {
		return nil, ErrPacketLength
	}
	return records, nil
}

// SplitLatestGameTCPFrames 从流式 buffer 中切出完整最新 game TCP frame。
//
// 如果 frame+2 的长度小于最小包长，按客户端拆帧逻辑跳 1 字节重同步。
func SplitLatestGameTCPFrames(buffer []byte) (frames [][]byte, remaining []byte, skipped int) {
	offset := 0
	for offset <= len(buffer)-4 {
		frameLength := int(binary.LittleEndian.Uint16(buffer[offset+2 : offset+4]))
		if frameLength < MinTCPFrameSize {
			offset++
			skipped++
			continue
		}
		if len(buffer)-offset < frameLength {
			break
		}
		frames = append(frames, cloneBytes(buffer[offset:offset+frameLength]))
		offset += frameLength
	}
	return frames, cloneBytes(buffer[offset:]), skipped
}

func writeInnerRecord(target []byte, businessPacket []byte, options TransportOptions) {
	target[0] = NormalInnerKind
	binary.LittleEndian.PutUint32(target[1:5], options.Sequence)
	binary.LittleEndian.PutUint16(target[5:7], uint16(len(businessPacket)))
	target[7] = options.Route
	target[8] = options.Flag
	copy(target[InnerHeaderSize:], businessPacket)
}

func parseInnerRecord(data []byte, offset int, available int, outerKind uint16, tcpTotal uint16, outerValue4 uint32, outerValue8 uint32) (LatestGameTransportRecord, int, error) {
	if available < InnerHeaderSize {
		return LatestGameTransportRecord{}, 0, ErrPacketTooShort
	}
	innerKind := data[offset]
	if innerKind != NormalInnerKind {
		return LatestGameTransportRecord{}, 0, ErrInnerKind
	}
	sequence := binary.LittleEndian.Uint32(data[offset+1 : offset+5])
	businessLength := int(binary.LittleEndian.Uint16(data[offset+5 : offset+7]))
	route := data[offset+7]
	flag := data[offset+8]
	recordLength := InnerHeaderSize + businessLength
	if businessLength < LatestGameHeaderSize || recordLength > available {
		return LatestGameTransportRecord{}, 0, ErrPacketLength
	}
	businessOffset := offset + InnerHeaderSize
	businessEnd := businessOffset + businessLength
	packet, err := ParseLatestGamePacket(data[businessOffset:businessEnd])
	if err != nil {
		return LatestGameTransportRecord{}, 0, err
	}
	record := LatestGameTransportRecord{
		TransportHeader: LatestGameTransportHeader{
			OuterKind:      outerKind,
			TotalLength:    tcpTotal,
			OuterValue4:    outerValue4,
			OuterValue8:    outerValue8,
			InnerKind:      innerKind,
			Sequence:       sequence,
			BusinessLength: uint16(businessLength),
			Route:          route,
			Flag:           flag,
		},
		GameHeader: packet.Header,
		Body:       packet.Body,
	}
	return record, recordLength, nil
}
