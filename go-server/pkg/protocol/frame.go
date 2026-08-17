package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"sort"
	"strings"
)

// 帧协议基础常量定义固定头、版本和默认 body 上限。
const (
	FrameMagic        uint16 = 0x5047 // 固定帧协议探测魔数："GP"，不是品牌缩写。
	FrameVersion1     uint8  = 1
	FrameHeaderSizeV1        = 32
	MaxFrameBodySize  uint32 = 4 * 1024 * 1024
)

// 标准帧 metadata 键名；链路字段兼容 W3C trace context。
const (
	FrameMetadataTraceParent = "traceparent"
	FrameMetadataTraceState  = "tracestate"
	FrameMetadataBaggage     = "baggage"
	// FrameMetadataIdempotencyKey 由客户端为一次业务请求生成，重试和重连时保持不变。
	FrameMetadataIdempotencyKey = "idempotency-key"

	frameMetadataMagic   = "MD"
	frameMetadataVersion = 1
	frameMaxHeaderSize   = 255
)

// 帧标志位描述 body 的压缩、加密和校验状态。
const (
	FrameFlagCompressed uint16 = 1 << iota
	FrameFlagEncrypted
	FrameFlagChecksum
)

// 帧 codec 标识 body 的序列化格式。
const (
	FrameCodecRaw uint16 = iota
	FrameCodecProtobuf
	FrameCodecJSON
)

// 帧解析和校验错误。
var (
	ErrInvalidFrameMagic     = errors.New("protocol frame magic is invalid")
	ErrInvalidFrameVersion   = errors.New("protocol frame version is invalid")
	ErrInvalidFrameHeader    = errors.New("protocol frame header is invalid")
	ErrFrameBodyTooLarge     = errors.New("protocol frame body is too large")
	ErrFrameChecksumInvalid  = errors.New("protocol frame checksum is invalid")
	ErrFrameMetadataInvalid  = errors.New("protocol frame metadata is invalid")
	ErrFrameMetadataTooLarge = errors.New("protocol frame metadata is too large")
)

// Frame 是 gateway frame_v1 线协议的内存表示。
type Frame struct {
	Version  uint8
	Flags    uint16
	Codec    uint16
	PacketID int32
	MsgID    uint32
	Sequence uint64
	Checksum uint32
	Body     []byte
	Metadata map[string]string
}

// FrameFromPacket 把旧 packet 包装为 frame_v1。
func FrameFromPacket(pkt Packet, sequence uint64) Frame {
	flags := uint16(0)
	if pkt.Compressed {
		flags |= FrameFlagCompressed
	}
	return Frame{
		Version:  FrameVersion1,
		Flags:    flags,
		Codec:    FrameCodecRaw,
		PacketID: pkt.PacketID,
		MsgID:    pkt.MsgID,
		Sequence: sequence,
		Body:     append([]byte(nil), pkt.Body...),
	}
}

// Packet 把 frame 还原成旧 packet 视图并拷贝 body。
func (f Frame) Packet() Packet {
	return Packet{
		PacketID:   f.PacketID,
		MsgID:      f.MsgID,
		Compressed: f.Flags&FrameFlagCompressed != 0,
		Body:       append([]byte(nil), f.Body...),
	}
}

func packetFromFrame(frame Frame) Packet {
	return Packet{
		PacketID:   frame.PacketID,
		MsgID:      frame.MsgID,
		Compressed: frame.Flags&FrameFlagCompressed != 0,
		Body:       frame.Body,
	}
}

// ReadFrame 从 reader 读取并校验一个 frame_v1 帧。
func ReadFrame(r io.Reader, maxBodySize uint32) (Frame, error) {
	maxBodySize = normMaxFrameBody(maxBodySize)
	var header [FrameHeaderSizeV1]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, err
	}
	if binary.LittleEndian.Uint16(header[0:2]) != FrameMagic {
		return Frame{}, ErrInvalidFrameMagic
	}
	version := header[2]
	if version != FrameVersion1 {
		return Frame{}, fmt.Errorf("%w: %d", ErrInvalidFrameVersion, version)
	}
	headerSize := int(header[3])
	if headerSize < FrameHeaderSizeV1 {
		return Frame{}, fmt.Errorf("%w: size=%d", ErrInvalidFrameHeader, headerSize)
	}
	var metadataHeader []byte
	if headerSize > FrameHeaderSizeV1 {
		metadataHeader = make([]byte, headerSize-FrameHeaderSizeV1)
		if _, err := io.ReadFull(r, metadataHeader); err != nil {
			return Frame{}, err
		}
	}
	metadata, err := decodeFrameMeta(metadataHeader)
	if err != nil {
		return Frame{}, err
	}

	bodySize := binary.LittleEndian.Uint32(header[24:28])
	if bodySize > maxBodySize {
		return Frame{}, fmt.Errorf("%w: %d", ErrFrameBodyTooLarge, bodySize)
	}
	body := make([]byte, bodySize)
	if bodySize > 0 {
		if _, err := io.ReadFull(r, body); err != nil {
			return Frame{}, err
		}
	}

	frame := Frame{
		Version:  version,
		Flags:    binary.LittleEndian.Uint16(header[4:6]),
		Codec:    binary.LittleEndian.Uint16(header[6:8]),
		PacketID: packetIDFromWire(binary.LittleEndian.Uint32(header[8:12])),
		MsgID:    binary.LittleEndian.Uint32(header[12:16]),
		Sequence: binary.LittleEndian.Uint64(header[16:24]),
		Checksum: binary.LittleEndian.Uint32(header[28:32]),
		Body:     body,
		Metadata: metadata,
	}
	if frame.Flags&FrameFlagChecksum != 0 && crc32.ChecksumIEEE(body) != frame.Checksum {
		return Frame{}, ErrFrameChecksumInvalid
	}
	return frame, nil
}

// WriteFrame 把 frame_v1 帧写入 writer。
func WriteFrame(w io.Writer, frame Frame) error {
	version := frame.Version
	if version == 0 {
		version = FrameVersion1
	}
	if version != FrameVersion1 {
		return fmt.Errorf("%w: %d", ErrInvalidFrameVersion, version)
	}
	bodySize := len(frame.Body)
	if bodySize > int(MaxFrameBodySize) || uint64(bodySize) > uint64(^uint32(0)) {
		return fmt.Errorf("%w: %d", ErrFrameBodyTooLarge, bodySize)
	}
	metadataHeader, err := encodeFrameMeta(frame.Metadata)
	if err != nil {
		return err
	}
	headerSize := FrameHeaderSizeV1 + len(metadataHeader)
	if headerSize > frameMaxHeaderSize {
		return fmt.Errorf("%w: header_size=%d", ErrFrameMetadataTooLarge, headerSize)
	}
	if frame.Flags&FrameFlagChecksum != 0 {
		frame.Checksum = crc32.ChecksumIEEE(frame.Body)
	} else {
		frame.Checksum = 0
	}

	var header [FrameHeaderSizeV1]byte
	binary.LittleEndian.PutUint16(header[0:2], FrameMagic)
	header[2] = version
	header[3] = uint8(headerSize) //nolint:gosec // G115：headerSize 已校验不超过 frameMaxHeaderSize。
	binary.LittleEndian.PutUint16(header[4:6], frame.Flags)
	binary.LittleEndian.PutUint16(header[6:8], frame.Codec)
	binary.LittleEndian.PutUint32(header[8:12], packetIDToWire(frame.PacketID))
	binary.LittleEndian.PutUint32(header[12:16], frame.MsgID)
	binary.LittleEndian.PutUint64(header[16:24], frame.Sequence)
	binary.LittleEndian.PutUint32(header[24:28], uint32(bodySize))
	binary.LittleEndian.PutUint32(header[28:32], frame.Checksum)

	if bodySize == 0 {
		return writeFullBuffers(w, header[:], metadataHeader)
	}
	return writeFullBuffers(w, header[:], metadataHeader, frame.Body)
}

func normMaxFrameBody(maxBodySize uint32) uint32 {
	if maxBodySize == 0 {
		return MaxFrameBodySize
	}
	return maxBodySize
}

func packetIDFromWire(value uint32) int32 {
	return int32(value) //nolint:gosec // G115：协议线格式用 uint32 保存 int32 的补码位模式。
}

func packetIDToWire(value int32) uint32 {
	return uint32(value) //nolint:gosec // G115：协议线格式用 uint32 保存 int32 的补码位模式。
}

// TraceContextMetadata 构造 frame metadata 中的链路追踪字段。
func TraceContextMetadata(traceParent, traceState string) map[string]string {
	out := make(map[string]string, 2)
	if traceParent = strings.TrimSpace(traceParent); traceParent != "" {
		out[FrameMetadataTraceParent] = traceParent
	}
	if traceState = strings.TrimSpace(traceState); traceState != "" {
		out[FrameMetadataTraceState] = traceState
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CloneFrameMetadata 规范化并拷贝 frame metadata。
func CloneFrameMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func encodeFrameMeta(metadata map[string]string) ([]byte, error) {
	metadata = CloneFrameMetadata(metadata)
	if len(metadata) == 0 {
		return nil, nil
	}
	if len(metadata) > 255 {
		return nil, fmt.Errorf("%w: entries=%d", ErrFrameMetadataTooLarge, len(metadata))
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := []byte{frameMetadataMagic[0], frameMetadataMagic[1], frameMetadataVersion, byte(len(keys))} //nolint:gosec // G115：metadata 条目数已限制在 255 以内。
	for _, key := range keys {
		value := metadata[key]
		if len(key) > 255 || len(value) > 255 {
			return nil, fmt.Errorf("%w: %s", ErrFrameMetadataTooLarge, key)
		}
		out = append(out, byte(len(key)), byte(len(value))) //nolint:gosec // G115：key/value 长度已限制在 255 以内。
		out = append(out, key...)
		out = append(out, value...)
		if FrameHeaderSizeV1+len(out) > frameMaxHeaderSize {
			return nil, fmt.Errorf("%w: header_size=%d", ErrFrameMetadataTooLarge, FrameHeaderSizeV1+len(out))
		}
	}
	return out, nil
}

func decodeFrameMeta(extra []byte) (map[string]string, error) {
	if len(extra) == 0 {
		return nil, nil
	}
	if len(extra) < 4 || string(extra[:2]) != frameMetadataMagic {
		return nil, ErrFrameMetadataInvalid
	}
	if extra[2] != frameMetadataVersion {
		return nil, fmt.Errorf("%w: version=%d", ErrFrameMetadataInvalid, extra[2])
	}
	count := int(extra[3])
	pos := 4
	out := make(map[string]string, count)
	for idx := 0; idx < count; idx++ {
		if pos+2 > len(extra) {
			return nil, ErrFrameMetadataInvalid
		}
		keyLen := int(extra[pos])
		valueLen := int(extra[pos+1])
		pos += 2
		if keyLen == 0 || valueLen == 0 || pos+keyLen+valueLen > len(extra) {
			return nil, ErrFrameMetadataInvalid
		}
		key := strings.ToLower(strings.TrimSpace(string(extra[pos : pos+keyLen])))
		pos += keyLen
		value := strings.TrimSpace(string(extra[pos : pos+valueLen]))
		pos += valueLen
		if key == "" || value == "" {
			return nil, ErrFrameMetadataInvalid
		}
		out[key] = value
	}
	if pos != len(extra) {
		return nil, ErrFrameMetadataInvalid
	}
	return out, nil
}
