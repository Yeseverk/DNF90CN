package dnfbridge

import (
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func buildCurrentItemListBody(listType byte, entries []currentItemListEntry, state dnfrepo.CharacterContainerState) []byte {
	var writer packetWriter
	writer.writeByte(listType)
	switch listType {
	case 0:
		writer.writeUint16(state.MainSlotCount)
	case 1:
		writer.writeUint16(state.AvatarExpansion)
	case 2:
		writer.writeUint16(state.PersonalCargoSlotCount)
	case 12:
		writer.writeUint16(state.AccountCargoSelectionKey)
		writer.writeUint32(state.AccountCargoStateValue)
	}
	writer.writeUint16(uint16(len(entries)))
	for _, entry := range entries {
		writer.writeBytes(entry.data[:])
		if listType == 1 {
			// Current sub_1D72380 always calls both avatar optional-blob
			// readers after the fixed 0x77-byte row. The first blob carries the
			// avatar socket layout when it exists; otherwise keep the
			// constructor-safe zero-length defaults so old rows still parse.
			writeCurrentAvatarItemListOptionalBlobs(&writer, entry)
		}
	}
	if listType == 2 {
		// Current sub_1D72380 always consumes a cargo grouping trailer after
		// the ordinary rows: u8 groupCount followed by groupCount*raw[8].
		// Group persistence is not owned yet, so terminate the proved grammar
		// with the constructor-safe empty group list.
		writer.writeByte(0)
	}
	return writer.bytes()
}

func currentItemListEntrySizeForType(listType byte) int {
	if listType == 1 {
		return currentItemListEntryWireSize + currentAvatarItemListOptionalBlobCount*4
	}
	return currentItemListEntryWireSize
}

func writeCurrentAvatarItemListOptionalBlobs(writer *packetWriter, entry currentItemListEntry) {
	if writer == nil {
		return
	}
	writeCurrentItemListOptionalBlob(writer, entry.avatarSocketData)
	writeCurrentItemListOptionalBlob(writer, entry.avatarColorData)
}

func writeCurrentItemListOptionalBlob(writer *packetWriter, data []byte) {
	if writer == nil {
		return
	}
	if len(data) == 0 {
		writer.writeUint32(0)
		return
	}
	writer.writeUint32(uint32(len(data)))
	writer.writeBytes(data)
}

func buildCurrentEquipmentUpdateBody(listType byte, entries []currentEquipmentUpdateEntry) []byte {
	var writer packetWriter
	writer.writeByte(listType)
	writer.writeUint16(uint16(len(entries)))
	for _, entry := range entries {
		writer.writeBytes(entry.data[:])
	}
	return writer.bytes()
}
