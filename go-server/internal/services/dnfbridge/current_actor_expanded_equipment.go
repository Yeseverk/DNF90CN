package dnfbridge

import (
	"context"
	"fmt"
	"strconv"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	// Current NoPack registers class0/op342 at 0x1D7F470 and dispatches it to
	// sub_1D7A3B0. The matching 86JP NOTI name is EXPAND_EQUIPSLOT_INFO.
	currentActorExpandedEquipmentMsgID uint16 = 342

	// sub_2005720 selects group zero for the current EXE's town actor path.
	// sub_1D7A3B0 accepts three group selectors; values >=4 skip that group
	// without consuming a sub_1D77560 equipment block.
	currentActorExpandedEquipmentTownGroup    byte = 0
	currentActorExpandedEquipmentSkippedGroup byte = 0xff
)

// buildCurrentActorExpandedEquipmentBody builds the exact body consumed by
// sub_1D7A3B0:
//
//	u16 actor key
//	repeated three times:
//	  u8 equipment group
//	  when group < 4: one complete sub_1D77560 equipment block
//
// Only group zero is populated. The two 0xff selectors are parser-defined
// skipped groups, so this packet updates the existing town actor without
// entering class0/op2 actor creation or replacement.
func buildCurrentActorExpandedEquipmentBody(
	actorKey uint16,
	rows []currentMode1EquipmentObjectRow,
) []byte {
	createRows := currentMode1EquipmentCreateRows(rows)

	var writer packetWriter
	writer.writeUint16(actorKey)
	writer.writeByte(currentActorExpandedEquipmentTownGroup)
	writer.writeByte(byte(len(createRows)))
	for _, row := range createRows {
		writeCurrentMode1EquipmentCreateRow(&writer, row)
	}
	writer.writeUint32(0) // sub_1D77560 final state dword.
	writer.writeByte(currentActorExpandedEquipmentSkippedGroup)
	writer.writeByte(currentActorExpandedEquipmentSkippedGroup)
	return writer.bytes()
}

func validateCurrentActorExpandedEquipmentProjection(
	record dnfrepo.EquipmentRecord,
	rows []currentMode1EquipmentObjectRow,
) ([]currentMode1EquipmentObjectRow, int, error) {
	expected := make(map[byte]uint32)
	unmapped := 0
	for key, entry := range record.Entries {
		runtimeSlot, ok := currentEXEActorEquipmentSlot(entry)
		if !ok {
			if entry.SlotIndex >= 26 && entry.SlotIndex <= 29 && entry.ItemID > 0 {
				return nil, unmapped, fmt.Errorf(
					"pet equipment entry %q at slot %d lacks an explicit current-EXE runtime slot",
					key,
					entry.SlotIndex,
				)
			}
			unmapped++
			continue
		}
		if runtimeSlot > 32 {
			return nil, unmapped, fmt.Errorf(
				"equipment entry %q runtime slot %d is outside current actor slots 0..32",
				key,
				runtimeSlot,
			)
		}
		if entry.ItemID <= 0 || entry.ItemID > int64(^uint32(0)) {
			return nil, unmapped, fmt.Errorf(
				"equipment entry %q item id %d is outside current actor uint32 identity",
				key,
				entry.ItemID,
			)
		}
		slot := byte(runtimeSlot)
		if previous, exists := expected[slot]; exists {
			return nil, unmapped, fmt.Errorf(
				"equipment entries collide at current actor slot %d: item %d and %d",
				slot,
				previous,
				entry.ItemID,
			)
		}
		expected[slot] = uint32(entry.ItemID)
	}

	createRows := currentMode1EquipmentCreateRows(rows)
	actual := make(map[byte]uint32, len(createRows))
	for _, row := range createRows {
		slot, ok := currentMode1EquipmentActorSlot(row)
		if !ok {
			return nil, unmapped, fmt.Errorf("verified equipment row has no current actor slot")
		}
		if previous, exists := actual[slot]; exists {
			return nil, unmapped, fmt.Errorf(
				"verified equipment rows collide at current actor slot %d: item %d and %d",
				slot,
				previous,
				row.itemID,
			)
		}
		actual[slot] = row.itemID
	}
	if len(actual) != len(expected) {
		return nil, unmapped, fmt.Errorf(
			"expanded equipment projection is incomplete: verified=%d expected=%d",
			len(actual),
			len(expected),
		)
	}
	for slot, itemID := range expected {
		if actualItemID, exists := actual[slot]; !exists || actualItemID != itemID {
			return nil, unmapped, fmt.Errorf(
				"expanded equipment projection slot %d item=%d want=%d present=%t",
				slot,
				actualItemID,
				itemID,
				exists,
			)
		}
	}
	return createRows, unmapped, nil
}

// sendSelectedActorExpandedEquipmentRefresh sends the current EXE's dedicated
// occupied-row equipment projection. sub_1D7A3B0 first resolves actorKey and
// requires it to equal the already-selected actor. Live current-client evidence
// shows that this group0 path applies present rows but does not reliably clear
// an omitted worn slot when the active and mirror group pointers are identical,
// so it must not be used as the post-op19 unequip redraw boundary.
func (s *Service) sendSelectedActorExpandedEquipmentRefresh(
	session *gameSession,
	source string,
) error {
	if session == nil || session.selectedCharacterID == 0 {
		return fmt.Errorf("selected actor expanded equipment refresh requires an active character")
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, _, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if charID == 0 || charID != session.selectedCharacterID || !hasCharacter {
		return fmt.Errorf("selected actor expanded equipment refresh could not load selected actor")
	}

	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Equipment == nil {
		return fmt.Errorf("selected actor expanded equipment repository is unavailable")
	}
	equipmentRecord, found, err := repositories.Equipment.Load(ctx, strconv.Itoa(int(charID)))
	if err != nil {
		return fmt.Errorf("load selected actor expanded equipment: %w", err)
	}
	if !found {
		equipmentRecord = dnfrepo.EquipmentRecord{CharacterID: strconv.Itoa(int(charID))}
	}
	var legacyRepo dnfrepo.LegacyUserInfoRepository
	if repositories.LegacyUserInfo != nil {
		legacyRepo = repositories.LegacyUserInfo
	}
	pvfStats, hasPVFStats := s.characterPVFStatsForUserInfo(ctx, session, character, hasCharacter)
	reader := csharpLegacyUserInfoReader{
		ctx:         ctx,
		repo:        legacyRepo,
		characterID: strconv.Itoa(int(charID)),
		service:     s,
		session:     session,
		pvfStats:    pvfStats,
		hasPVFStats: hasPVFStats,
	}
	equipmentRows := reader.currentMode1EquipmentObjectRows()
	createRows, unmappedRows, err := validateCurrentActorExpandedEquipmentProjection(
		equipmentRecord,
		equipmentRows,
	)
	if err != nil {
		return fmt.Errorf("validate selected actor expanded equipment: %w", err)
	}
	body := buildCurrentActorExpandedEquipmentBody(
		currentSceneActorObjectKey(charID),
		createRows,
	)

	s.logPacketEvent("game-upper-selected-actor-expanded-equipment-refresh-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"actor_object_key", currentSceneActorObjectKey(charID),
		"msg_id", currentActorExpandedEquipmentMsgID,
		"classification", 0,
		"town_group", currentActorExpandedEquipmentTownGroup,
		"equipment_entry_count", len(equipmentRows),
		"create_entry_count", len(createRows),
		"unmapped_non_actor_entry_count", unmappedRows,
		"body_len", len(body),
		"body_source", "current_exe_sub_1D7A3B0_actor_key_group0_full_sub_1D77560_two_skipped_groups_existing_actor_only")
	return s.sendGameUpperRawClass(session, currentActorExpandedEquipmentMsgID, body, 0)
}
