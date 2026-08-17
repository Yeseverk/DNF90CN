package dnfbridge

import (
	"context"
	"strconv"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentActorMode0AppearanceBodySource = "character_equipment_repository_current_exe_full_14_slot_typed_mode0_tail"

// currentNameTagAppearanceSlot is the equipment worn-slot used by name tag
// cards (schema: sub_F546B0 i64 low32 → slot 28).
const currentNameTagAppearanceSlot = 28

// loadCurrentSelectedActorMode0AppearanceSummary loads only the authoritative
// 14-slot data block for the selected actor. The caller must embed it in the
// current raw47/typed mode0 writer; the removed legacy subtype0 whole-packet
// writer is not compatible with the current EXE scene-object reader.
// PR #239: if the character has an active name tag card, an additional row at
// slot 28 is appended so the client renders the name decoration.
func (s *Service) loadCurrentSelectedActorMode0AppearanceSummary(
	ctx context.Context,
	session *gameSession,
	charID uint16,
) ([]dnfrepo.CharacterRosterEquipSummary, string, bool, error) {
	if s == nil || session == nil || charID == 0 || session.selectedCharacterID != charID {
		return nil, "", false, nil
	}
	repos, ok := s.repositoryGroup()
	if !ok || repos.Equipment == nil {
		return nil, "", false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	characterID := strconv.FormatUint(uint64(charID), 10)
	equipment, found, err := repos.Equipment.Load(ctx, characterID)
	if err != nil || !found {
		return nil, "", false, err
	}
	rows, err := buildCurrentActorMode0AppearanceSummaryFromEquipment(equipment)
	if err != nil {
		return nil, "", false, err
	}

	// PR #239: append name tag card row when active.
	if repos.Character != nil {
		if character, charFound, charErr := repos.Character.Load(ctx, characterID); charErr == nil && charFound && character.Stats != nil {
			applyCurrentCloneTitleAppearance(rows, character.Stats["clone_title_item_id"])
			nameTagItemID := character.Stats["name_tag_item_id"]
			nameTagExpire := character.Stats["name_tag_expire_time"]
			if nameTagItemID > 0 && (nameTagExpire == 0 || nameTagExpire > currentNameTagExpireNow()) {
				rows = append(rows, dnfrepo.CharacterRosterEquipSummary{
					Slot:         currentNameTagAppearanceSlot,
					ItemIDOrIcon: nameTagItemID,
				})
			}
		}
	}

	if session.selectedCharacterID != charID {
		return nil, "", false, nil
	}
	return rows, currentActorMode0AppearanceBodySource, true, nil
}

func applyCurrentCloneTitleAppearance(rows []dnfrepo.CharacterRosterEquipSummary, cloneTitleItemID int64) bool {
	const titleAppearanceSlot = 13
	if cloneTitleItemID <= 0 || cloneTitleItemID > int64(^uint32(0)) {
		return false
	}
	for index := range rows {
		if rows[index].Slot != titleAppearanceSlot ||
			uint32(rows[index].ItemIDOrIcon) == currentActorMode0AppearanceEmptyItem {
			continue
		}
		rows[index].ItemIDOrIcon = cloneTitleItemID
		return true
	}
	return false
}

func (s *Service) loadCurrentSelectedActorNameTagState(
	ctx context.Context,
	session *gameSession,
	charID uint16,
) (uint32, uint32, bool, error) {
	if s == nil || session == nil || charID == 0 || session.selectedCharacterID != charID {
		return 0, 0, false, nil
	}
	repos, ok := s.repositoryGroup()
	if !ok || repos.Equipment == nil {
		return 0, 0, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	record, found, err := repos.Equipment.Load(ctx, strconv.FormatUint(uint64(charID), 10))
	if err != nil || !found {
		return 0, 0, false, err
	}
	entry, equipped := record.Entries["30"]
	if !equipped || entry.ItemID <= 0 {
		return 0, 0, true, nil
	}
	return sceneInventoryUint32FromInt64(entry.ItemID), currentItemListEquipmentExpire(entry), true, nil
}
