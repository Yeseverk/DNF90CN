package itemlock

import "encoding/binary"

const (
	msgItemLockList     uint16 = 0x00FB
	msgItemUnlockNotice uint16 = 0x00FC

	itemLockStateActive  byte = 1
	itemLockStatePending byte = 2
)

// LockListMessageID is the class-0 notification that replaces the client's
// complete item-lock cache for the selected character.
const LockListMessageID = msgItemLockList

type packetWriter struct {
	data []byte
}

func (w *packetWriter) writeByte(value byte) {
	w.data = append(w.data, value)
}

func (w *packetWriter) writeUint16(value uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], value)
	w.data = append(w.data, buf[:]...)
}

func (w *packetWriter) writeInt16(value int16) {
	w.writeUint16(uint16(value))
}

func (w *packetWriter) writeInt32(value int32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(value))
	w.data = append(w.data, buf[:]...)
}

func (w *packetWriter) bytes() []byte {
	return append([]byte(nil), w.data...)
}

func buildLockAck(listType byte, slotIndex int16) []byte {
	var writer packetWriter
	writer.writeByte(listType)
	writer.writeInt16(slotIndex)
	return writer.bytes()
}

func buildUnlockAck(listType byte, slotIndex int16, remainingSeconds int32) []byte {
	var writer packetWriter
	writer.writeByte(listType)
	writer.writeInt16(slotIndex)
	writer.writeInt32(remainingSeconds)
	return writer.bytes()
}

func buildLockListDelta(listType byte, slotIndex int16, state byte, remainingSeconds int32) []byte {
	var writer packetWriter
	writer.writeUint16(1)
	writer.writeByte(listType)
	writer.writeInt16(slotIndex)
	writer.writeByte(state)
	if state == 2 {
		writer.writeInt32(remainingSeconds)
	}
	return writer.bytes()
}
