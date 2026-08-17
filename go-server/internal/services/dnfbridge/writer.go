// writer.go 提供 DNF bridge 协议编码所需的最小二进制写入 helper。
// 它只负责字节序、定长和 DSTR 形态，不做业务校验。
package dnfbridge

import (
	"bytes"
	"encoding/binary"
)

type packetWriter struct {
	buffer bytes.Buffer
}

func (w *packetWriter) writeByte(value byte) {
	_ = w.buffer.WriteByte(value)
}

func (w *packetWriter) writeZero(count int) {
	if count <= 0 {
		return
	}
	w.buffer.Write(make([]byte, count))
}

func (w *packetWriter) writeInt32(value int) {
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], uint32(value))
	w.buffer.Write(data[:])
}

func (w *packetWriter) writeUint16(value uint16) {
	var data [2]byte
	binary.LittleEndian.PutUint16(data[:], value)
	w.buffer.Write(data[:])
}

func (w *packetWriter) writeUint32(value uint32) {
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], value)
	w.buffer.Write(data[:])
}

func (w *packetWriter) writeUint64(value uint64) {
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], value)
	w.buffer.Write(data[:])
}

func (w *packetWriter) writeAsciiDstr(value string) {
	data := []byte(value)
	w.writeInt32(len(data))
	w.buffer.Write(data)
}

func (w *packetWriter) writeBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	w.buffer.Write(data)
}

func (w *packetWriter) writeRawDstr(data []byte) {
	w.writeInt32(len(data))
	w.writeBytes(data)
}

func (w *packetWriter) bytes() []byte {
	return append([]byte(nil), w.buffer.Bytes()...)
}

func fixedASCII(value string, size int) []byte {
	data := make([]byte, size)
	copy(data, []byte(value))
	return data
}
