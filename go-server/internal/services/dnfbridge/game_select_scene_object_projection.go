package dnfbridge

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strconv"
	"strings"
)

func (s *Service) selectedSceneStageCharacter(session *gameSession) (dnfrepo.CharacterRecord, bool, string) {
	if session == nil || session.selectedCharacterID == 0 {
		return dnfrepo.CharacterRecord{}, false, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	_, charName, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	return character, hasCharacter, charName
}

func (s *Service) buildCurrentSceneObjectListBodyForSession(ctx context.Context, session *gameSession, charID uint16, charName string, character dnfrepo.CharacterRecord, hasCharacter bool) []byte {
	return s.buildCurrentSceneObjectListBodyForSessionInContext(
		ctx,
		session,
		charID,
		charName,
		character,
		hasCharacter,
		currentSceneObjectContext,
	)
}

func (s *Service) buildCurrentSceneObjectListBodyForSessionInContext(ctx context.Context, session *gameSession, charID uint16, charName string, character dnfrepo.CharacterRecord, hasCharacter bool, ownerChannel byte) []byte {
	body, _ := s.buildCurrentSceneObjectListBodyForSessionInContextWithPolicy(
		ctx,
		session,
		charID,
		charName,
		character,
		hasCharacter,
		ownerChannel,
		false,
	)
	return body
}

func (s *Service) buildCurrentSceneObjectListBodyForSessionInContextStrict(
	ctx context.Context,
	session *gameSession,
	charID uint16,
	charName string,
	character dnfrepo.CharacterRecord,
	hasCharacter bool,
	ownerChannel byte,
) ([]byte, error) {
	return s.buildCurrentSceneObjectListBodyForSessionInContextWithPolicy(
		ctx,
		session,
		charID,
		charName,
		character,
		hasCharacter,
		ownerChannel,
		true,
	)
}

func (s *Service) buildCurrentSceneObjectListBodyForSessionInContextWithPolicy(
	ctx context.Context,
	session *gameSession,
	charID uint16,
	charName string,
	character dnfrepo.CharacterRecord,
	hasCharacter bool,
	ownerChannel byte,
	strict bool,
) ([]byte, error) {
	if charID == 0 {
		charID, charName, character, hasCharacter = s.selectedCharacterForEnter(ctx, session)
	}
	if charName == "" {
		charName = csharpSelectCharacterName(charID)
	}
	mode0EquipSummarySource := "character_roster_partial_current_exe_typed_tail"
	if fullRows, source, found, err := s.loadCurrentSelectedActorMode0AppearanceSummary(ctx, session, charID); err != nil {
		if session != nil {
			s.logGameEvent(session, "game-upper-current-scene-object-mode0-appearance-load-failed",
				"char_id", charID,
				"error", err)
		}
		if strict {
			return nil, fmt.Errorf("load selected actor mode0 equipment appearance: %w", err)
		}
	} else if found {
		// Work on the value copy only. The selected character repository remains
		// unchanged; this replaces the packet projection with all slots 0..13.
		character.Roster.Entry.EquipSummary = fullRows
		mode0EquipSummarySource = source
	}
	// PR #239: inject name tag state from equipment slot 30 into character
	// stats so the mode0 tail carries the correct item ID and expiry for
	// the client's ReadAndApplyActorNameTagState (sub_2008D80).
	if itemID, expireTime, found, err := s.loadCurrentSelectedActorNameTagState(ctx, session, charID); err != nil {
		if session != nil {
			s.logGameEvent(session, "game-upper-current-scene-object-name-tag-load-failed",
				"char_id", charID,
				"error", err)
		}
		if strict {
			return nil, fmt.Errorf("load selected actor mode0 name-tag appearance: %w", err)
		}
	} else if found {
		character = dnfrepo.CloneCharacter(character)
		if character.Stats == nil {
			character.Stats = make(map[string]int64)
		}
		character.Stats["name_tag_item_id"] = int64(itemID)
		character.Stats["name_tag_expire_time"] = int64(expireTime)
	}
	objectKey := currentSceneActorObjectKey(charID)
	creature, creatureErr := s.currentEquippedCreatureForCharacterWithError(ctx, strconv.Itoa(int(charID)))
	if creatureErr != nil {
		if session != nil {
			s.logGameEvent(session, "game-upper-current-scene-object-creature-load-failed",
				"char_id", charID,
				"error", creatureErr)
		}
		if strict {
			return nil, fmt.Errorf("load selected actor mode0 creature appearance: %w", creatureErr)
		}
		creature = currentEquippedCreatureSnapshot{}
	}
	adventureName := s.currentSceneObjectAdventureOverheadName(ctx, session, charID, character, hasCharacter)
	body := buildCurrentSceneObjectListBodyWithCreatureAndAdventureNameInContext(objectKey, character, hasCharacter, charName, creature, adventureName, ownerChannel)
	rows := currentSceneObjectEquipSummary(character, hasCharacter)
	rawBytes := 0
	for _, row := range rows {
		rawBytes += len(row.RawEntry)
	}
	objectLevel, tailStart, levelOK := currentSceneObjectLevelForLog(body)
	mode0EquipRows, mode0V113, mode0TailOK := currentSceneObjectTailSummaryForLog(body)
	mode0NoAttachedUIResource, mode0NoAttachedUIResourceOK := currentSceneObjectTailNoAttachedUIResourceForLog(body)
	mode0ActorStateEventArg, mode0ActorStateEventArgOK := currentSceneObjectTailActorStateEventArgForLog(body)
	mode0VisualState, mode0VisualStateOK := currentSceneObjectTailVisualStateForLog(body)
	mode0RawSource, mode0RawHeadHex, mode0RawWord2, mode0RawWord5, mode0RawDword0F, mode0RawDword19, mode0Raw25, mode0Raw26, mode0Raw2C, mode0Raw43, mode0RawOK := currentSceneObjectRawStateForLog(body)
	headLen := len(body)
	if headLen > 96 {
		headLen = 96
	}
	if session != nil {
		s.logPacketEvent("game-upper-current-scene-object-list-built",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"char_id", charID,
			"object_key", objectKey,
			"owner_channel", ownerChannel,
			"adventure_overhead_name_set", adventureName != "",
			"body_len", len(body),
			"object_level", objectLevel,
			"object_level_ok", levelOK,
			"object_tail_start", tailStart,
			"body_head_hex", hex.EncodeToString(body[:headLen]),
			"equip_rows", len(rows),
			"equip_raw_source_bytes", rawBytes,
			"mode0_equip_summary_source", mode0EquipSummarySource,
			"mode0_equip_summary_rows", mode0EquipRows,
			"mode0_v113", mode0V113,
			"mode0_tail_ok", mode0TailOK,
			"mode0_sub2008d80_no_attached_ui_resource", mode0NoAttachedUIResource,
			"mode0_sub2008d80_no_attached_ui_resource_ok", mode0NoAttachedUIResourceOK,
			"mode0_actor_state_event_arg", mode0ActorStateEventArg,
			"mode0_actor_state_event_arg_ok", mode0ActorStateEventArgOK,
			"mode0_sub20042f0_visual_state", mode0VisualState,
			"mode0_sub20042f0_normal_town", mode0VisualStateOK && mode0VisualState == currentSceneNormalTownVisualState,
			"mode0_raw_state_source", mode0RawSource,
			"mode0_raw_state_ok", mode0RawOK,
			"mode0_raw_state_head_hex", mode0RawHeadHex,
			"mode0_raw_word2", mode0RawWord2,
			"mode0_raw_word5", mode0RawWord5,
			"mode0_raw_dword0f", mode0RawDword0F,
			"mode0_raw_dword19", mode0RawDword19,
			"mode0_raw25", mode0Raw25,
			"mode0_raw26_dword", mode0Raw26,
			"mode0_raw2c", mode0Raw2C,
			"mode0_raw43_dword", mode0Raw43,
			"mode0_creature_item_id", creature.itemID,
			"mode0_creature_serial_or_handle", creature.serialOrHandle,
			"mode0_creature_level", creature.level,
			"mode0_creature_town_display", creature.townDisplay,
			"mode0_creature_source", creature.source,
			"type44_raw_block", "empty_until_current_variant_traced")
	}
	return body, nil
}

// currentSceneObjectAdventureOverheadName projects only the selected local
// actor's persisted adventure-group name. Peer actor packets must never reuse
// the viewer's account label; unavailable account data leaves this optional
// display field empty without blocking town-object creation.
func (s *Service) currentSceneObjectAdventureOverheadName(
	ctx context.Context,
	session *gameSession,
	charID uint16,
	character dnfrepo.CharacterRecord,
	hasCharacter bool,
) string {
	if s == nil || session == nil || !hasCharacter || charID == 0 ||
		charID != session.selectedCharacterID {
		return ""
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	if accountID == "" || accountID != strings.TrimSpace(character.AccountID) {
		return ""
	}
	name, _, _, err := s.currentRepresentAccountIdentity(ctx, session)
	if err != nil || strings.TrimSpace(name) == "" {
		return ""
	}
	return name
}

func currentSceneObjectLevelForLog(body []byte) (byte, int, bool) {
	const nameLenOffset = 5 + 0x47 + 2
	if len(body) < nameLenOffset+4 {
		return 0, 0, false
	}
	nameLen := int(binary.LittleEndian.Uint32(body[nameLenOffset : nameLenOffset+4]))
	tailStart := nameLenOffset + 4 + nameLen
	if nameLen < 0 || tailStart+2 >= len(body) {
		return 0, tailStart, false
	}
	return body[tailStart+2], tailStart, true
}

func currentSceneObjectRawStateForLog(body []byte) (string, string, uint16, uint16, uint32, uint32, byte, uint32, byte, uint32, bool) {
	const rawOffset = 5
	const rawLen = 0x47
	if len(body) < rawOffset+rawLen {
		return "", "", 0, 0, 0, 0, 0, 0, 0, 0, false
	}
	raw := body[rawOffset : rawOffset+rawLen]
	source := "current_exe_structured_raw"
	if bytesAllZero(raw) {
		source = "current_exe_zero_initialized_raw"
	}
	headLen := 16
	if len(raw) < headLen {
		headLen = len(raw)
	}
	return source,
		hex.EncodeToString(raw[:headLen]),
		binary.LittleEndian.Uint16(raw[0x02:0x04]),
		binary.LittleEndian.Uint16(raw[0x05:0x07]),
		binary.LittleEndian.Uint32(raw[0x0f:0x13]),
		binary.LittleEndian.Uint32(raw[0x19:0x1d]),
		raw[0x25],
		binary.LittleEndian.Uint32(raw[0x26:0x2a]),
		raw[0x2c],
		binary.LittleEndian.Uint32(raw[0x43:0x47]),
		true
}

func bytesAllZero(raw []byte) bool {
	for _, value := range raw {
		if value != 0 {
			return false
		}
	}
	return true
}

func currentSceneObjectTailSummaryForLog(body []byte) (int, uint32, bool) {
	_, tailStart, ok := currentSceneObjectLevelForLog(body)
	if !ok || tailStart >= len(body) {
		return 0, 0, false
	}
	tail := body[tailStart:]
	if len(tail) <= 6 {
		return 0, 0, false
	}
	equipEnd, ok := currentSceneObjectEquipSummaryEnd(tail, 6)
	if !ok || equipEnd+4 > len(tail) {
		return int(tail[6]), 0, false
	}
	return int(tail[6]), binary.LittleEndian.Uint32(tail[equipEnd : equipEnd+4]), true
}

func currentSceneObjectTailNoAttachedUIResourceForLog(body []byte) (bool, bool) {
	_, tailStart, ok := currentSceneObjectLevelForLog(body)
	if !ok || tailStart >= len(body) {
		return false, false
	}
	tail := body[tailStart:]
	if len(tail) <= 6 {
		return false, false
	}
	equipEnd, ok := currentSceneObjectEquipSummaryEnd(tail, 6)
	if !ok {
		return false, false
	}
	rawOffset := equipEnd + 8
	if rawOffset+8 > len(tail) {
		return false, false
	}
	return binary.LittleEndian.Uint32(tail[rawOffset:rawOffset+4]) == currentSceneNoAttachedUIResourceID &&
		binary.LittleEndian.Uint32(tail[rawOffset+4:rawOffset+8]) == currentSceneNoAttachedUIResourceID, true
}

func currentSceneObjectTailActorStateEventArgForLog(body []byte) (uint32, bool) {
	_, tailStart, ok := currentSceneObjectLevelForLog(body)
	if !ok || tailStart >= len(body) {
		return 0, false
	}
	tail := body[tailStart:]
	if len(tail) <= 6 {
		return 0, false
	}
	equipEnd, ok := currentSceneObjectEquipSummaryEnd(tail, 6)
	if !ok {
		return 0, false
	}
	// After the variable equipment summary, consume the current EXE's typed
	// fields up to v121: v113, four flags, raw8, resource flags, creature,
	// three single-byte states, v120, and the native organization
	// {guild-level, DSTR, chaos-point} block.
	pos := equipEnd + 4 + 4 + 8 + 1 + 4 + 1
	if pos+4 > len(tail) {
		return 0, false
	}
	pos += 4 // creature template
	if pos+4 > len(tail) {
		return 0, false
	}
	creatureNameLen := int(binary.LittleEndian.Uint32(tail[pos : pos+4]))
	pos += 4 + creatureNameLen + 1
	if creatureNameLen < 0 || pos+1+1+4+4 > len(tail) {
		return 0, false
	}
	pos += 1 + 1 + 4 // sub_2003B80, sub_2003C90, v120
	if pos+4 > len(tail) {
		return 0, false
	}
	pos++ // guild level
	if pos+4 > len(tail) {
		return 0, false
	}
	guildNameLen := int(binary.LittleEndian.Uint32(tail[pos : pos+4]))
	pos += 4 + guildNameLen + 4 // DSTR plus sub_2003D40 trailing u32
	if guildNameLen < 0 || pos+4 > len(tail) {
		return 0, false
	}
	return binary.LittleEndian.Uint32(tail[pos : pos+4]), true
}

func currentSceneObjectTailVisualStateForLog(body []byte) (byte, bool) {
	_, tailStart, ok := currentSceneObjectLevelForLog(body)
	if !ok || tailStart >= len(body) {
		return 0, false
	}
	tail := body[tailStart:]
	// In the current typed mode0 writer, sub_20042F0's u8 is followed by the
	// fixed 19-byte finalizer state (u32/u8/u16/u8/u16/u8/u16/u8/u8/u32).
	const finalizerBytesAfterVisualState = 19
	if len(tail) <= finalizerBytesAfterVisualState {
		return 0, false
	}
	return tail[len(tail)-1-finalizerBytesAfterVisualState], true
}
