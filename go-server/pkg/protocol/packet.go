package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

// MaxPacketBodySize 是旧 packet 协议默认 body 上限。
const MaxPacketBodySize uint32 = 1024 * 1024

// 旧 packet 协议解析错误。
var (
	ErrPacketBodyTooLarge = errors.New("protocol packet body is too large")
	ErrInvalidPacketFlag  = errors.New("protocol packet flag is invalid")
)

// Packet 是旧 gateway packet 线协议的内存表示。
type Packet struct {
	PacketID   int32
	MsgID      uint32
	Compressed bool
	Body       []byte
}

// ReadPacket 从 reader 读取旧 packet。
func ReadPacket(r io.Reader, maxBodySize uint32) (Packet, error) {
	maxBodySize = normMaxPacketBody(maxBodySize)
	var header [13]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Packet{}, err
	}

	bodySize := binary.LittleEndian.Uint32(header[0:4])
	if bodySize > maxBodySize {
		return Packet{}, fmt.Errorf("%w: %d", ErrPacketBodyTooLarge, bodySize)
	}

	body := make([]byte, bodySize)
	if bodySize > 0 {
		if _, err := io.ReadFull(r, body); err != nil {
			return Packet{}, err
		}
	}

	compressed, err := decodeCompressedFlag(header[12])
	if err != nil {
		return Packet{}, err
	}
	return Packet{
		PacketID:   packetIDFromWire(binary.LittleEndian.Uint32(header[4:8])),
		MsgID:      binary.LittleEndian.Uint32(header[8:12]),
		Compressed: compressed,
		Body:       body,
	}, nil
}

// WritePacket 把旧 packet 写入 writer。
func WritePacket(w io.Writer, pkt Packet) error {
	bodySize := len(pkt.Body)
	if bodySize > int(MaxPacketBodySize) || uint64(bodySize) > uint64(^uint32(0)) {
		return fmt.Errorf("%w: %d", ErrPacketBodyTooLarge, bodySize)
	}
	var header [13]byte
	binary.LittleEndian.PutUint32(header[0:4], uint32(bodySize))
	binary.LittleEndian.PutUint32(header[4:8], packetIDToWire(pkt.PacketID))
	binary.LittleEndian.PutUint32(header[8:12], pkt.MsgID)
	if pkt.Compressed {
		header[12] = 1
	}

	if bodySize == 0 {
		return writeFull(w, header[:])
	}
	return writeFullBuffers(w, header[:], pkt.Body)
}

func normMaxPacketBody(maxBodySize uint32) uint32 {
	if maxBodySize == 0 {
		return MaxPacketBodySize
	}
	return maxBodySize
}

func decodeCompressedFlag(flag byte) (bool, error) {
	switch flag {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("%w: compressed=%d", ErrInvalidPacketFlag, flag)
	}
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func writeFullBuffers(w io.Writer, parts ...[]byte) error {
	total := 0
	var scratch [4][]byte
	buffers := net.Buffers(scratch[:0])
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		total += len(part)
		buffers = append(buffers, part)
	}
	if total == 0 {
		return nil
	}
	if _, ok := w.(net.Conn); !ok {
		return writeFullParts(w, parts...)
	}
	written, err := buffers.WriteTo(w)
	if err != nil {
		return err
	}
	if written == int64(total) {
		return nil
	}
	if written < 0 || written > int64(total) {
		return io.ErrShortWrite
	}
	return writeBufRemainder(w, int(written), parts...)
}

func writeFullParts(w io.Writer, parts ...[]byte) error {
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if err := writeFull(w, part); err != nil {
			return err
		}
	}
	return nil
}

func writeBufRemainder(w io.Writer, written int, parts ...[]byte) error {
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if written >= len(part) {
			written -= len(part)
			continue
		}
		if written > 0 {
			part = part[written:]
			written = 0
		}
		if err := writeFull(w, part); err != nil {
			return err
		}
	}
	return nil
}
