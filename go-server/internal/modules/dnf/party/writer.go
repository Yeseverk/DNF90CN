// 本文件提供队伍协议 body 所需的最小二进制写入器。
package party

import "encoding/binary"

type packetWriter struct {
	buf []byte
}

func (w *packetWriter) writeByte(value byte) {
	w.buf = append(w.buf, value)
}

func (w *packetWriter) writeZero(count int) {
	if count <= 0 {
		return
	}
	w.buf = append(w.buf, make([]byte, count)...)
}

func (w *packetWriter) writeUint16(value uint16) {
	var data [2]byte
	binary.LittleEndian.PutUint16(data[:], value)
	w.buf = append(w.buf, data[:]...)
}

func (w *packetWriter) writeUint32(value uint32) {
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], value)
	w.buf = append(w.buf, data[:]...)
}

func (w *packetWriter) writeRawDstr(data []byte) {
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(data)))
	w.buf = append(w.buf, length[:]...)
	w.buf = append(w.buf, data...)
}

func (w *packetWriter) bytes() []byte {
	return append([]byte(nil), w.buf...)
}
