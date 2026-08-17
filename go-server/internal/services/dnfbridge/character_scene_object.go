package dnfbridge

import (
	"encoding/binary"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentSceneBootstrapObjectKey uint16 = 0x0191

const (
	// The current client resolves UINT32_MAX to runtime creature type 8.  A
	// live mode0 trace then shows the empty overhead marker.  Zero is the
	// C# no-equipped-creature value and must be kept for an absent pet.
	currentSceneNoCreatureTemplateID   uint32 = 0
	currentSceneNoAttachedUIResourceID uint32 = 0xffffffff
	// sub_2009160 passes this field to the selected actor's 17/6599 state
	// event.  It is an initialization marker, not a character statistic: the
	// same-version DOVE entry carries one and the current EXE always dispatches
	// the event for the local actor.  Leaving it at zero suppresses that state
	// initialization path.
	currentSceneActorStateEventArg uint32 = 1
	// Current EXE sub_20042F0 applies this normal-town visual-state mask to
	// the selected actor. The same-version DOVE scene entry carries 0x06;
	// zero is not the neutral state and leaves its visual refresh path unset.
	currentSceneNormalTownVisualState byte = 0x06
)

func currentSceneActorObjectKey(characterID uint16) uint16 {
	if characterID != 0 {
		return characterID
	}
	return currentSceneBootstrapObjectKey
}

func buildCurrentSceneObjectListBody(sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string) []byte {
	return buildCurrentSceneObjectListBodyInContext(sceneObjectKey, character, hasCharacter, fallbackName, currentSceneObjectContext)
}

func buildCurrentSceneObjectListBodyInContext(sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string, ownerChannel byte) []byte {
	return buildCurrentSceneObjectListBodyWithCreatureInContext(sceneObjectKey, character, hasCharacter, fallbackName, currentEquippedCreatureSnapshot{}, ownerChannel)
}

func buildCurrentSceneObjectListBodyWithCreature(sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string, creature currentEquippedCreatureSnapshot) []byte {
	return buildCurrentSceneObjectListBodyWithCreatureInContext(sceneObjectKey, character, hasCharacter, fallbackName, creature, currentSceneObjectContext)
}

func buildCurrentSceneObjectListBodyWithCreatureInContext(sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string, creature currentEquippedCreatureSnapshot, ownerChannel byte) []byte {
	if sceneObjectKey == 0 {
		sceneObjectKey = currentSceneBootstrapObjectKey
	}
	if fallbackName == "" && hasCharacter {
		fallbackName = character.Name
	}

	var writer packetWriter
	writer.writeByte(0)
	writer.writeUint16(1)
	writer.writeByte(currentSceneObjectRoute)
	writer.writeByte(ownerChannel)
	writeCurrentSceneObjectEntryWithCreature(&writer, sceneObjectKey, character, hasCharacter, fallbackName, creature)
	return writer.bytes()
}

// buildCurrentSceneObjectListBodyWithCreatureAndAdventureNameInContext writes
// the account's durable adventure-group name through the current EXE's native
// organization-name display field. This display projection does not create a
// guild, assign a guild identifier, or alter any social state.
func buildCurrentSceneObjectListBodyWithCreatureAndAdventureNameInContext(sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string, creature currentEquippedCreatureSnapshot, adventureName string, ownerChannel byte) []byte {
	if sceneObjectKey == 0 {
		sceneObjectKey = currentSceneBootstrapObjectKey
	}
	if fallbackName == "" && hasCharacter {
		fallbackName = character.Name
	}

	var writer packetWriter
	writer.writeByte(0)
	writer.writeUint16(1)
	// MCP/live hook: mode0 对象 entry 使用 00/00；sub_2009160 会用第二个字节
	// 和 dword_51B0EF0 当前容器比较，匹配时才走 sub_20036C0 创建主场景对象。
	writer.writeByte(currentSceneObjectRoute)
	writer.writeByte(ownerChannel)
	writeCurrentSceneObjectEntryWithCreatureAndAdventureName(&writer, sceneObjectKey, character, hasCharacter, fallbackName, creature, adventureName)
	return writer.bytes()
}

func writeCurrentSceneObjectEntry(writer *packetWriter, sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string) {
	writeCurrentSceneObjectEntryWithCreature(writer, sceneObjectKey, character, hasCharacter, fallbackName, currentEquippedCreatureSnapshot{})
}

func writeCurrentSceneObjectEntryWithCreature(writer *packetWriter, sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string, creature currentEquippedCreatureSnapshot) {
	writeCurrentSceneObjectEntryWithCreatureAndAdventureName(writer, sceneObjectKey, character, hasCharacter, fallbackName, creature, "")
}

func writeCurrentSceneObjectEntryWithCreatureAndAdventureName(writer *packetWriter, sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string, creature currentEquippedCreatureSnapshot, adventureName string) {
	name := fallbackName
	if name == "" && hasCharacter {
		name = character.Name
	}

	if rawState, ok := buildCurrentSceneObjectRawState(character, hasCharacter, name); ok {
		// MCP/live hook：sub_34DBD90 会校验这段对象状态的编码值；全 0 会跳到 0x18181818。
		writer.writeBytes(rawState)
	} else {
		writer.writeZero(0x47)
	}
	writer.writeUint16(sceneObjectKey)
	writeCurrentSceneObjectName(writer, name)
	writer.writeBytes(buildCurrentSceneObjectEntryTailForCurrentExeWithCreatureAndAdventureName(character, hasCharacter, creature, adventureName))
}

func buildCurrentSceneObjectRawState(character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string) ([]byte, bool) {
	// Current EXE sub_2009160 reads this as a fixed 0x47-byte actor-state
	// record before the separately typed key/name/job/grow fields.  It is not a
	// USERINFO subtype and must never be copied from a captured scene packet.
	//
	// The fields which have a durable DB/PVF source are emitted later in this
	// entry (name/job/grow/level/equipment) or in the authoritative mode1/3
	// refresh.  Only the offsets below are proven to be consumed by the current
	// EXE (see .ida-mcp/scan_raw47_20260718): offset 0x07 is the required
	// boolean read by sub_26908A0 marking the newly-created scene actor ready;
	// offset 0x0A is the actor level read by sub_2059450/sub_269E330; offset
	// 0x2C keeps the actor's own list/overhead entries visible in
	// sub_11B6FF0/sub_11B7C90; offset 0x43 is the overhead icon record id that
	// sub_1F60CE0 skips only when -1.  Every other consumed offset is
	// zero-safe, and the never-read bytes stay zero rather than an
	// account-specific value from DOVE.
	_ = fallbackName
	const rawLen = 0x47
	const actorReadyOffset = 0x07
	const actorLevelOffset = 0x0A
	const actorSelfVisibleOffset = 0x2C
	const actorOverheadIconOffset = 0x43
	raw := make([]byte, rawLen)
	raw[actorReadyOffset] = 1
	if hasCharacter {
		raw[actorLevelOffset] = rosterLevel(character)
	}
	raw[actorSelfVisibleOffset] = 1
	binary.LittleEndian.PutUint32(raw[actorOverheadIconOffset:actorOverheadIconOffset+4], 0xFFFFFFFF)
	return raw, true
}

func buildCurrentSceneObjectEntryTail(character dnfrepo.CharacterRecord, hasCharacter bool) []byte {
	var writer packetWriter
	writeCurrentSceneObjectEntryTailWithCreature(&writer, character, hasCharacter, currentEquippedCreatureSnapshot{})
	return writer.bytes()
}

func buildCurrentSceneObjectEntryTailForCurrentExe(character dnfrepo.CharacterRecord, hasCharacter bool) []byte {
	return buildCurrentSceneObjectEntryTailForCurrentExeWithCreature(character, hasCharacter, currentEquippedCreatureSnapshot{})
}

func buildCurrentSceneObjectEntryTailForCurrentExeWithCreature(character dnfrepo.CharacterRecord, hasCharacter bool, creature currentEquippedCreatureSnapshot) []byte {
	return buildCurrentSceneObjectEntryTailForCurrentExeWithCreatureAndAdventureName(character, hasCharacter, creature, "")
}

func buildCurrentSceneObjectEntryTailForCurrentExeWithCreatureAndAdventureName(character dnfrepo.CharacterRecord, hasCharacter bool, creature currentEquippedCreatureSnapshot, adventureName string) []byte {
	var writer packetWriter
	writeCurrentSceneObjectEntryTailWithCreatureAndAdventureName(&writer, character, hasCharacter, creature, adventureName)
	tail := writer.bytes()
	if len(tail) < currentSceneObjectPostNameTailLength() {
		padded := make([]byte, currentSceneObjectPostNameTailLength())
		copy(padded, tail)
		return padded
	}
	return tail
}

func patchCurrentSceneObjectTailTransientItemRaw(tail []byte) bool {
	const equipOffset = 6
	equipEnd, ok := currentSceneObjectEquipSummaryEnd(tail, equipOffset)
	if !ok {
		return false
	}
	rawOffset := equipEnd + 8
	if rawOffset < 0 || rawOffset+8 > len(tail) {
		return false
	}
	for idx := 0; idx < 8; idx++ {
		tail[rawOffset+idx] = 0
	}
	return true
}
