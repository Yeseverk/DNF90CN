package dnfbridge

import (
	"bytes"
	"testing"
)

func TestPacketWriterWriteUint64UsesLittleEndian(t *testing.T) {
	var writer packetWriter
	writer.writeUint64(0x1122334455667788)
	if want := []byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}; !bytes.Equal(writer.bytes(), want) {
		t.Fatalf("uint64 bytes=%x want=%x", writer.bytes(), want)
	}
}
