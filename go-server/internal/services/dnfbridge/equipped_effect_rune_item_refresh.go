package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

// sendSelectedEquippedEquipmentEffectRuneRefresh sends the complete current
// list-3 row for equipped weapons with a persisted effect rune. Mode-1 creates
// the ordinary equipment object, while this accepted op14 path hydrates the
// object's rune state after that creation.
func (s *Service) sendSelectedEquippedEquipmentEffectRuneRefresh(session *gameSession, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return fmt.Errorf("equipped equipment-effect refresh requires an active character")
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Equipment == nil {
		return fmt.Errorf("equipped equipment-effect refresh repository is unavailable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	record, found, err := repositories.Equipment.Load(ctx, characterID)
	if err != nil {
		return fmt.Errorf("load equipped equipment-effect refresh: %w", err)
	}
	if !found {
		return nil
	}

	entries := make([]currentItemListEntry, 0, len(record.Entries))
	for _, equipped := range record.Entries {
		runeID := sceneInventoryExtraUint16(equipped.Extra, "equipment_effect_id")
		if runeID == 0 {
			continue
		}
		entry, ok := currentItemListEntryFromEquipment(equipped)
		if !ok {
			continue
		}
		if got := binary.LittleEndian.Uint16(entry.data[currentEquipmentEffectRuneWireOffset : currentEquipmentEffectRuneWireOffset+2]); got != runeID {
			return fmt.Errorf("equipped equipment-effect refresh rune projection mismatch slot=%d got=%d want=%d", equipped.SlotIndex, got, runeID)
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		s.logPacketEvent("game-upper-current-equipped-equipment-effect-rune-refresh-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "no_equipped_effect_runes")
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return binary.LittleEndian.Uint16(entries[i].data[0:2]) < binary.LittleEndian.Uint16(entries[j].data[0:2])
	})
	body := buildCurrentItemUpdateBody(currentSocketListEquipment, entries)
	s.logPacketEvent("game-upper-current-equipped-equipment-effect-rune-refresh-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"classification", 0,
		"list_type", currentSocketListEquipment,
		"entry_count", len(entries),
		"body_len", len(body),
		"sequence", "mode1_created_equipment_then_effect_rune_only_op14_hydration")
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0)
}
