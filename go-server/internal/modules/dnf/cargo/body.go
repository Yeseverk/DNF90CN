package cargo

import "encoding/binary"

const (
	listTypeMain      byte   = 0
	msgItemListUpdate uint16 = 0x000E
	msgCeraUpdate     uint16 = 0x0035

	commonUpdateEntrySize = 0x77
)

type packetWriter struct {
	data []byte
}

func (w *packetWriter) writeByte(value byte) {
	w.data = append(w.data, value)
}

func (w *packetWriter) writeZero(count int) {
	if count <= 0 {
		return
	}
	w.data = append(w.data, make([]byte, count)...)
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

func buildSuccessAck() []byte {
	return []byte{1}
}

func buildCargoGoldAck(cargoGold int64) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeInt32(clampInt32(cargoGold))
	return writer.bytes()
}

func buildGoldUpdateBody(gold int64) []byte {
	var writer packetWriter
	writer.writeByte(listTypeMain)
	writer.writeUint16(1)
	writeCommonUpdateEntry(&writer, 0, 0, clampInt32(gold))
	return writer.bytes()
}

func buildCeraUpdateBody(cera int64) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeInt32(clampInt32(cera))
	writer.writeInt32(0)
	writer.writeInt32(0)
	return writer.bytes()
}

func writeCommonUpdateEntry(writer *packetWriter, slot int16, itemID int32, countOrValue int32) {
	writer.writeInt16(slot)
	writer.writeInt32(itemID)
	writer.writeInt32(countOrValue)
	writer.writeZero(commonUpdateEntrySize - 2 - 4 - 4)
}

func clampInt32(value int64) int32 {
	if value > 2147483647 {
		return 2147483647
	}
	if value < -2147483648 {
		return -2147483648
	}
	return int32(value)
}
