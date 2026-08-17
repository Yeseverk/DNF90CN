package dnfbridge

import "math"

func buildCurrentGoldStateBody(gold int64) []byte {
	if gold < 0 {
		gold = 0
	}
	if gold > math.MaxInt32 {
		gold = math.MaxInt32
	}
	var entry currentItemListEntry
	entry.patchCore(0, 0, uint32(gold))
	return buildCurrentItemUpdateBody(0, []currentItemListEntry{entry})
}

func buildCurrentItemUpdateBody(listType byte, entries []currentItemListEntry) []byte {
	var body packetWriter
	body.writeByte(listType)
	body.writeUint16(uint16(len(entries)))
	for _, entry := range entries {
		body.writeBytes(entry.data[:])
		if listType == 1 {
			// List-1 is not an ordinary raw item row.  Current EXE list readers
			// consume two length-prefixed avatar extension blobs after every
			// 0x77-byte item record.  The first blob carries avatar socket data
			// after open/embed mutations; omitting both length fields shifts the
			// next packet reader and can terminate the client immediately after
			// an avatar (especially a weapon-avatar) purchase.
			writeCurrentAvatarItemListOptionalBlobs(&body, entry)
		}
		// Do not append default blobs to normal list-3 equipment. Current
		// sub_225C960/sub_225C9B0 read them only when the item-table type is
		// <= 11; normal worn equipment starts at type 12. Extra length fields
		// on a gate-false row become bytes of the following packet.
	}
	return body.bytes()
}
