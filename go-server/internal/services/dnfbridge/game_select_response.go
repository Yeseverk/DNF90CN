package dnfbridge

func buildEnterSelectDungeonBody(characterID uint16) []byte {
	var writer packetWriter
	writer.writeByte(3)
	writer.writeUint16(currentSceneActorObjectKey(characterID))
	return writer.bytes()
}

func buildEnterSelectDungeonAckBody() []byte {
	// Current EXE op15 success handler reads one flag byte and then a count.
	return []byte{1, 0}
}

func buildSelectDungeonBodyForCharacter(characterID uint16) []byte {
	var writer packetWriter
	writer.writeByte(3)
	writer.writeUint16(currentSceneActorObjectKey(characterID))
	return writer.bytes()
}

func buildCSharpItemListBody(listType byte) []byte {
	var writer packetWriter
	writer.writeByte(listType)
	writer.writeUint16(0)
	writer.writeUint16(0)
	return writer.bytes()
}

func buildCSharpSelectLoadEmptyBody() []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeUint32(0)
	writer.writeUint16(0)
	writer.writeByte(0)
	return writer.bytes()
}

func buildCSharpUpperSuccessOnlyBody() []byte {
	return []byte{1}
}

func buildCSharpUpperSuccessByteBody(value byte) []byte {
	return []byte{1, value}
}

func buildCSharpUpperFailurePairBody(first byte, second byte) []byte {
	return []byte{0, first, second}
}

func buildCSharpPetItemListBody() []byte {
	var writer packetWriter
	writer.writeByte(7)
	writer.writeUint16(0)
	return writer.bytes()
}

func buildCSharpUserInfoBody(occurrence int, charID uint16, charName string) []byte {
	var writer packetWriter
	if occurrence == 1 {
		writer.writeByte(1)
		writer.writeUint16(1)
		writer.writeUint16(charID)
		writer.writeZero(64)
		return writer.bytes()
	}
	writer.writeByte(0)
	writer.writeUint16(1)
	writer.writeUint16(charID)
	writer.writeAsciiDstr(charName)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(1)
	writer.writeZero(64)
	return writer.bytes()
}

func buildCSharpDailyChallengeSelectBody() []byte {
	var writer packetWriter
	writer.writeUint32(0)
	writer.writeUint32(0)
	writer.writeUint32(6)
	writer.writeZero(6)
	writer.writeUint32(0)
	return writer.bytes()
}

func buildCSharpTitleBookBody(occurrence int) []byte {
	var writer packetWriter
	writer.writeByte(0)
	writer.writeUint16(0)
	writer.writeInt32(occurrence)
	writer.writeInt32(0)
	return writer.bytes()
}

func buildCSharpDailyScheduleBody(_ int) []byte {
	var writer packetWriter
	writer.writeInt32(0)
	writer.writeInt32(0)
	writer.writeInt32(0)
	writer.writeInt32(0)
	return writer.bytes()
}
