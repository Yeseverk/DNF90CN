package dnfbridge

import (
	"context"
	"sort"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func initialEquipmentRosterSummary(entries []initialEquipmentEntry) []dnfrepo.CharacterRosterEquipSummary {
	if len(entries) == 0 {
		return nil
	}
	out := make([]dnfrepo.CharacterRosterEquipSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.Slot <= 0 || entry.ItemID <= 0 {
			continue
		}
		out = append(out, dnfrepo.CharacterRosterEquipSummary{
			Slot:         int64(entry.Slot),
			ItemIDOrIcon: entry.ItemID,
			RawEntry:     append([]byte(nil), entry.RawEntry...),
		})
	}
	sortRosterEquipSummary(out)
	return out
}

func equipmentRosterSummary(record dnfrepo.EquipmentRecord) []dnfrepo.CharacterRosterEquipSummary {
	if len(record.Entries) == 0 {
		return nil
	}
	out := make([]dnfrepo.CharacterRosterEquipSummary, 0, len(record.Entries))
	for _, entry := range record.Entries {
		if entry.SlotIndex < 0 || entry.ItemID <= 0 {
			continue
		}
		// The roster actor uses the same current appearance-slot table as mode0.
		// In particular, PVF starter equipment stores the old worn weapon slot 11,
		// while the current client renders a real weapon from appearance slot 12.
		// Sending the old value makes the client keep its job-default weapon layer
		// and add the equipped model as a second layer.
		appearanceSlot, ok := currentActorMode0AppearanceSlot(entry)
		if !ok {
			continue
		}
		out = append(out, dnfrepo.CharacterRosterEquipSummary{
			Slot:         int64(appearanceSlot),
			ItemIDOrIcon: entry.ItemID,
			RawEntry:     append([]byte(nil), entry.RawEntry...),
		})
	}
	sortRosterEquipSummary(out)
	return filterRosterWeaponAppearance(out)
}

// filterRosterWeaponAppearance implements the client-side weapon display
// precedence in the packet we own. The base job weapon is implicit when no
// row is sent. A weapon avatar (slot 10) must therefore be the only explicit
// weapon layer; otherwise a real weapon (slot 12) replaces that implicit base.
func filterRosterWeaponAppearance(rows []dnfrepo.CharacterRosterEquipSummary) []dnfrepo.CharacterRosterEquipSummary {
	if len(rows) == 0 {
		return nil
	}
	hasWeaponAvatar := false
	for _, row := range rows {
		if row.Slot == 10 && row.ItemIDOrIcon > 0 {
			hasWeaponAvatar = true
			break
		}
	}
	if !hasWeaponAvatar {
		return rows
	}
	filtered := make([]dnfrepo.CharacterRosterEquipSummary, 0, len(rows))
	for _, row := range rows {
		if row.Slot == 12 {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func sortRosterEquipSummary(rows []dnfrepo.CharacterRosterEquipSummary) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Slot == rows[j].Slot {
			return rows[i].ItemIDOrIcon < rows[j].ItemIDOrIcon
		}
		return rows[i].Slot < rows[j].Slot
	})
}

func characterEquipSummary(character dnfrepo.CharacterRecord) []dnfrepo.CharacterRosterEquipSummary {
	if len(character.Roster.Entry.EquipSummary) == 0 {
		return nil
	}
	out := cloneRosterEquipSummary(character.Roster.Entry.EquipSummary)
	sortRosterEquipSummary(out)
	return out
}

func currentSceneObjectEquipSummary(character dnfrepo.CharacterRecord, hasCharacter bool) []dnfrepo.CharacterRosterEquipSummary {
	if !hasCharacter {
		return nil
	}
	out := cloneRosterEquipSummary(character.Roster.Entry.EquipSummary)
	sortRosterEquipSummary(out)
	return out
}

func cloneRosterEquipSummary(rows []dnfrepo.CharacterRosterEquipSummary) []dnfrepo.CharacterRosterEquipSummary {
	if len(rows) == 0 {
		return nil
	}
	out := make([]dnfrepo.CharacterRosterEquipSummary, len(rows))
	for idx, row := range rows {
		row.RawEntry = append([]byte(nil), row.RawEntry...)
		out[idx] = row
	}
	return out
}

func (s *Service) attachEquipmentSummary(ctx context.Context, repos dnfrepo.Group, record *dnfrepo.CharacterRecord) {
	if s == nil || record == nil || strings.TrimSpace(record.CharacterID) == "" {
		return
	}
	if _, err := s.ensureCharacterInitializationSnapshot(ctx, repos, *record); err != nil {
		s.logPacketEvent("dnf-character-initialization-backfill-failed",
			"character_id", record.CharacterID,
			"job", record.Job,
			"error", err)
	}
	if repos.Equipment != nil {
		equipment, found, err := repos.Equipment.Load(ctx, record.CharacterID)
		if err != nil {
			s.logPacketEvent("dnf-character-equipment-load-failed", "character_id", record.CharacterID, "error", err)
		} else if found {
			// The equipment repository is the durable authority after character
			// creation. Replace any cached roster summary even when the current
			// equipped set is empty, so unequip-all clears the selector appearance
			// and cannot accidentally trigger starter-equipment backfill.
			record.Roster.Entry.EquipSummary = equipmentRosterSummary(equipment)
			return
		}
	}
}
