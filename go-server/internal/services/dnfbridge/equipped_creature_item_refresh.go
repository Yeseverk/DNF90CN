package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// sendSelectedEquippedCreatureItemRefresh replays only the existing creature
// object in actor equipment slot 26. Full mode1 and op13 create that object,
// but the current EXE applies a creature enchant's dynamic card state only on
// its same-template op14 path. Never widen this to generic list-3 equipment:
// ordinary equipment updates have different native lifetime requirements.
func (s *Service) sendSelectedEquippedCreatureItemRefresh(session *gameSession, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return fmt.Errorf("equipped creature item refresh requires an active character")
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Equipment == nil {
		return fmt.Errorf("equipped creature item refresh repository is unavailable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	record, found, err := repositories.Equipment.Load(ctx, characterID)
	if err != nil {
		return fmt.Errorf("load equipped creature item refresh: %w", err)
	}
	equipped, worn := currentEquippedCreatureEquipmentEntry(record)
	if !found || !worn {
		s.logPacketEvent("game-upper-current-equipped-creature-item-refresh-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "equipment_slot26_absent")
		return nil
	}
	creature := currentEquippedCreatureFromEquipment(equipped)
	if !creature.valid() || int64(creature.itemID) != equipped.ItemID {
		return fmt.Errorf(
			"equipped creature item refresh rejected invalid slot 26: item=%d serial=%d",
			equipped.ItemID,
			creature.serialOrHandle,
		)
	}
	entry := currentEquippedCreatureItemRefreshEntry(equipped)
	if binary.LittleEndian.Uint16(entry.data[0:2]) != 26 {
		return fmt.Errorf("equipped creature item refresh could not project slot 26")
	}
	body := buildCurrentItemUpdateBody(currentSocketListEquipment, []currentItemListEntry{entry})
	s.logPacketEvent("game-upper-current-equipped-creature-item-refresh-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"classification", 0,
		"list_type", currentSocketListEquipment,
		"slot", 26,
		"item_id", creature.itemID,
		"creature_serial", creature.serialOrHandle,
		"enchant_card_id", binary.LittleEndian.Uint32(entry.data[0x0E:0x12]),
		"enchant_upgrade_count", entry.data[0x12],
		"entry_count", 1,
		"body_len", len(body),
		"sequence", "existing_slot26_object_then_pet_only_op14_dynamic_state_refresh")
	return s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketWalkoutPartyMember),
		body,
		0,
	)
}

// currentEquippedCreatureItemRefreshEntry must not use the legacy equipment
// raw row as a 0x77-byte base. The legacy card begins at +16 and its serial is
// repeated at +24; copying those bytes verbatim makes the card's third byte an
// enchant upgrade and serial<<16 a fake remaining duration (serial 41 appears
// as roughly 32 days). Build the list-3 row from zero and map only fields with
// proved equivalents in the current item-update grammar.
func currentEquippedCreatureItemRefreshEntry(equipped dnfrepo.EquipmentEntry) currentItemListEntry {
	var entry currentItemListEntry
	entry.patchCore(26, sceneInventoryUint32FromInt64(equipped.ItemID), currentItemListEquipmentInstance(equipped))
	entry.data[0x0A] = currentItemListEquipmentExtData(equipped)
	binary.LittleEndian.PutUint16(entry.data[0x0B:0x0D], currentItemListEquipmentDurability(equipped))
	entry.data[0x0D] = currentItemListBindFlag(equipped.Bind, equipped.Extra)
	binary.LittleEndian.PutUint32(entry.data[0x0E:0x12], currentEquippedCreatureEnchantCardID(equipped))
	entry.data[0x12] = currentEquippedCreatureEnchantUpgrade(equipped)
	entry.data[0x13] = currentEquippedCreatureEnchantAmplifyType(equipped)
	binary.LittleEndian.PutUint16(entry.data[0x14:0x16], currentEquippedCreatureEnchantAmplifyValue(equipped))
	expire := currentItemListEquipmentExpire(equipped)
	binary.LittleEndian.PutUint32(
		entry.data[currentPetRemainSecondsOffset:currentPetRemainSecondsOffset+4],
		currentPetRemainingSecondsAt(expire, time.Now().UTC()),
	)
	binary.LittleEndian.PutUint32(
		entry.data[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4],
		expire,
	)
	entry.data[0x57] = sceneInventoryExtraByte(equipped.Extra, "byte_57", "value_57")
	entry.data[0x58] = sceneInventoryExtraByte(equipped.Extra, "byte_58", "value_58")
	entry.data[0x59] = sceneInventoryExtraByte(equipped.Extra, "byte_59", "value_59")
	entry.copyFixed(0x72, currentItemListFixedExtraBytes(equipped.Extra, 5, "tail_data_72", "tail72"))
	return entry
}

func currentEquippedCreatureEnchantCardID(equipped dnfrepo.EquipmentEntry) uint32 {
	if value, ok := sceneInventoryExtraUint(
		equipped.Extra,
		"pet_enchant_card_item_id",
		"enchant_card_id",
		"value_a",
	); ok {
		return sceneInventoryClampUint32(value)
	}
	if currentEquipmentEntryHasCurrentRaw77(equipped) {
		return binary.LittleEndian.Uint32(equipped.RawEntry[0x0E:0x12])
	}
	if len(equipped.RawEntry) >= 20 {
		return binary.LittleEndian.Uint32(equipped.RawEntry[16:20])
	}
	return 0
}

func currentEquippedCreatureEnchantUpgrade(equipped dnfrepo.EquipmentEntry) byte {
	if value, ok := sceneInventoryExtraUint(
		equipped.Extra,
		"pet_enchant_upgrade_count",
		"enchant_upgrade_count",
		"byte_12",
		"value_12",
	); ok {
		return byte(min(value, 0xFF))
	}
	if currentEquipmentEntryHasCurrentRaw77(equipped) {
		return equipped.RawEntry[0x12]
	}
	if len(equipped.RawEntry) > 20 {
		return equipped.RawEntry[20]
	}
	return 0
}

func currentEquippedCreatureEnchantAmplifyType(equipped dnfrepo.EquipmentEntry) byte {
	if value, ok := sceneInventoryExtraUint(equipped.Extra, "byte_13", "value_13", "value_c"); ok {
		return byte(min(value, 0xFF))
	}
	if currentEquipmentEntryHasCurrentRaw77(equipped) {
		return equipped.RawEntry[0x13]
	}
	if len(equipped.RawEntry) > 21 {
		return equipped.RawEntry[21]
	}
	return 0
}

func currentEquippedCreatureEnchantAmplifyValue(equipped dnfrepo.EquipmentEntry) uint16 {
	if value, ok := sceneInventoryExtraUint(equipped.Extra, "marker_16", "marker16", "value_d"); ok {
		return uint16(min(value, 0xFFFF))
	}
	if currentEquipmentEntryHasCurrentRaw77(equipped) {
		return binary.LittleEndian.Uint16(equipped.RawEntry[0x14:0x16])
	}
	if len(equipped.RawEntry) >= 24 {
		return binary.LittleEndian.Uint16(equipped.RawEntry[22:24])
	}
	return 0
}
