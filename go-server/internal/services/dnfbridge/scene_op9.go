package dnfbridge

import (
	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentSceneOp9ActorDisplayKind byte   = 0
	currentSceneOp9ActorRemoveKind  byte   = 3
	currentSceneOp9StableSceneValue uint16 = 9999
)

func buildCurrentSceneOp9NoopBody() []byte {
	var writer packetWriter
	writer.writeUint16(0)
	writer.writeUint16(currentSceneOp9StableSceneValue)
	return writer.bytes()
}

func buildCurrentSceneOp9ActorRemovalBody(sceneObjectKey uint16) []byte {
	return buildCurrentSceneOp9ActorRemovalBodyInContext(
		sceneObjectKey,
		currentSceneObjectContext,
	)
}

func buildCurrentSceneOp9ActorRemovalBodyInContext(
	sceneObjectKey uint16,
	ownerChannel byte,
) []byte {
	var writer packetWriter
	writer.writeUint16(1)
	writer.writeUint16(currentSceneOp9StableSceneValue)
	writer.writeUint16(sceneObjectKey)
	writer.writeByte(0)
	writer.writeByte(ownerChannel)
	writer.writeByte(0)
	writer.writeByte(currentSceneOp9ActorRemoveKind)
	return writer.bytes()
}

func (s *Service) sendCurrentSceneOp9PreviewActorRemovalOnce(session *gameSession, source string) error {
	if session == nil || !session.selectPreviewObjectStateSent || session.selectPreviewActorRemoved {
		return nil
	}
	objectKey := currentSceneActorObjectKey(session.selectedCharacterID)
	if objectKey == 0 {
		return nil
	}
	body := buildCurrentSceneOp9ActorRemovalBodyInContext(
		objectKey,
		currentSceneObjectContext,
	)
	s.logGameEvent(session, "game-upper-current-scene-op9-preview-actor-remove-send",
		"source", source,
		"char_id", session.selectedCharacterID,
		"object_key", objectKey,
		"msg_id", uint16(dnfenum.CmdPacketRecoverStamina),
		"classification", 0,
		"record_kind", currentSceneOp9ActorRemoveKind,
		"body_len", len(body),
		"body_source", "current_exe_sub_1D64CA0_kind3_clear_slots_and_remove_target",
		"reason", "evict_transition_preview_before_final_actor_mode0_mode1_bind")
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketRecoverStamina), body, 0); err != nil {
		return err
	}
	session.selectPreviewActorRemoved = true
	session.currentSceneObjectListSent = false
	return nil
}

func buildCurrentSceneOp9ActorDisplayBody(sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string) []byte {
	return buildCurrentSceneOp9ActorDisplayBodyInContext(sceneObjectKey, character, hasCharacter, fallbackName, currentSceneObjectContext)
}

func buildCurrentSceneOp9ActorDisplayBodyInContext(sceneObjectKey uint16, character dnfrepo.CharacterRecord, hasCharacter bool, fallbackName string, ownerChannel byte) []byte {
	return buildCurrentSceneOp9ActorPartyDisplayBodyInContext(
		sceneObjectKey,
		character,
		hasCharacter,
		fallbackName,
		ownerChannel,
		ownerChannel,
		nil,
		0,
	)
}

type currentSceneOp9PartyMemberProjection struct {
	State alignedcmd.PartyMemberState
	Name  string
	Job   byte
	Level byte
	Grow  byte
}

func buildCurrentSceneOp9ActorPartyDisplayBodyInContext(
	sceneObjectKey uint16,
	character dnfrepo.CharacterRecord,
	hasCharacter bool,
	fallbackName string,
	ownerChannel byte,
	localChannel byte,
	members []currentSceneOp9PartyMemberProjection,
	selectedMemberSlot byte,
) []byte {
	if sceneObjectKey == 0 {
		sceneObjectKey = currentSceneBootstrapObjectKey
	}
	name := fallbackName
	if name == "" && hasCharacter {
		name = character.Name
	}
	level := byte(1)
	job := byte(0)
	grow := byte(0)
	if hasCharacter {
		if character.Level > 0 && character.Level < 256 {
			level = byte(character.Level)
		}
		job = byte(numericCharacterStat(character.Job))
		grow = byte(numericCharacterStatValue(character, "grow_type"))
	}

	var writer packetWriter
	writer.writeUint16(1)
	writer.writeUint16(currentSceneOp9StableSceneValue)

	writer.writeUint16(sceneObjectKey)
	writer.writeByte(0)
	writer.writeByte(ownerChannel)
	writer.writeByte(0)
	writer.writeByte(currentSceneOp9ActorDisplayKind)

	writer.writeByte(currentSceneObjectRoute)
	// sub_1D64CA0 uses this nested byte as the inline-identity discriminator,
	// not as the actor-owner key. Zero is required for the following DSTR name
	// branch; the real actor owner is already carried by the outer record.
	writer.writeByte(currentSceneObjectContext)
	writer.writeRawDstr(rosterNameBytes(name))
	writer.writeByte(job)
	writer.writeByte(level)
	writer.writeUint32(0)
	writer.writeByte(grow)
	writer.writeUint16(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeUint16(0)

	members = currentSceneOp9PartyMembers(members)
	writer.writeByte(byte(len(members)))
	for slot, member := range members {
		writer.writeByte(byte(slot))
		writer.writeUint16(member.State.UserID)
		if ownerChannel != localChannel {
			writer.writeByte(member.Job)
			writer.writeByte(member.Level)
			writer.writeByte(member.Grow)
			writer.writeByte(0)
			writer.writeRawDstr(rosterNameBytes(member.Name))
		}
		writer.writeByte(0)
		writer.writeByte(currentSceneOp9PartyMemberState(member.State.UserState))
		writer.writeByte(0)
	}
	writer.writeByte(0)
	writer.writeByte(selectedMemberSlot)
	writer.writeByte(0)
	writer.writeByte(0)
	return writer.bytes()
}

func currentSceneOp9SelectedPartySlot(members []currentSceneOp9PartyMemberProjection, selectedMemberID uint16) (byte, bool) {
	for slot, member := range members {
		if member.State.UserID == selectedMemberID {
			return byte(slot), true
		}
	}
	return 0, false
}

func currentSceneOp9PartyMembers(members []currentSceneOp9PartyMemberProjection) []currentSceneOp9PartyMemberProjection {
	const maxCurrentScenePartySlots = 8
	capacity := len(members)
	if capacity > maxCurrentScenePartySlots {
		capacity = maxCurrentScenePartySlots
	}
	filtered := make([]currentSceneOp9PartyMemberProjection, 0, capacity)
	for _, member := range members {
		if member.State.UserID == 0 {
			continue
		}
		duplicate := false
		for _, existing := range filtered {
			if existing.State.UserID == member.State.UserID {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		filtered = append(filtered, member)
		if len(filtered) == maxCurrentScenePartySlots {
			break
		}
	}
	return filtered
}

func currentSceneOp9PartyMemberState(value byte) byte {
	if value == 0 {
		return 1
	}
	return value
}
