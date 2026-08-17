package protocol

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"strings"
)

// 支持的 gateway wire format 名称。
const (
	WireFormatPacket  = "packet"
	WireFormatFrameV1 = "frame_v1"
)

// ErrInvalidWireFormat 表示 envelope 使用了不支持的线协议格式。
var ErrInvalidWireFormat = errors.New("protocol wire format is invalid")

// Envelope 是 packet 与 frame_v1 的统一传输容器。
type Envelope struct {
	Format string
	Packet Packet
	Frame  Frame
}

// ReadEnvelope 自动探测并读取 packet 或 frame_v1。
func ReadEnvelope(r *bufio.Reader, maxBodySize uint32) (Envelope, error) {
	if r == nil {
		return Envelope{}, io.ErrUnexpectedEOF
	}
	peek, err := r.Peek(4)
	if err != nil {
		return Envelope{}, err
	}
	// 读侧兼容两种线协议：先探测 frame_v1 magic，未命中则退回旧 packet 格式，方便客户端灰度升级。
	if binary.LittleEndian.Uint16(peek[0:2]) == FrameMagic &&
		peek[2] == FrameVersion1 &&
		int(peek[3]) >= FrameHeaderSizeV1 {
		frame, err := ReadFrame(r, maxBodySize)
		if err != nil {
			return Envelope{}, err
		}
		return Envelope{
			Format: WireFormatFrameV1,
			Packet: packetFromFrame(frame),
			Frame:  frame,
		}, nil
	}
	packet, err := ReadPacket(r, maxBodySize)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		Format: WireFormatPacket,
		Packet: packet,
	}, nil
}

// WriteEnvelope 按指定 wire format 写出 envelope。
func WriteEnvelope(w io.Writer, envelope Envelope) error {
	format := strings.TrimSpace(envelope.Format)
	if format == "" {
		format = WireFormatPacket
	} else {
		format = NormalizeWireFormat(format)
	}
	switch format {
	case WireFormatPacket:
		return WritePacket(w, envelope.Packet)
	case WireFormatFrameV1:
		// frame_v1 的压缩标记以 Packet.Compressed 为准，避免调用方同时维护 Packet 和 Frame 两份状态时产生漂移。
		flags := envelope.Frame.Flags &^ FrameFlagCompressed
		if envelope.Packet.Compressed {
			flags |= FrameFlagCompressed
		}
		frame := Frame{
			Version:  envelope.Frame.Version,
			Flags:    flags,
			Codec:    envelope.Frame.Codec,
			PacketID: envelope.Packet.PacketID,
			MsgID:    envelope.Packet.MsgID,
			Sequence: envelope.Frame.Sequence,
			Body:     envelope.Packet.Body,
			Metadata: CloneFrameMetadata(envelope.Frame.Metadata),
		}
		return WriteFrame(w, frame)
	default:
		return ErrInvalidWireFormat
	}
}

// NormalizeWireFormat 归一化 wire format 文本。
func NormalizeWireFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "":
		return WireFormatPacket
	case WireFormatPacket:
		return WireFormatPacket
	case WireFormatFrameV1:
		return WireFormatFrameV1
	default:
		return ""
	}
}
