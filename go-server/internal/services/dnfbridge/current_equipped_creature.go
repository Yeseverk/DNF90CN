package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// currentEquippedCreatureSnapshot is the one durable creature projection used
// by all current-EXE class0/op2 actor modes. Equipment slot 26 owns whether a
// creature is worn; PetRecord supplies the hatched creature's name and level.
//
// Current EXE sub_20077D0 consumes {itemID, serialOrHandle}. The second dword
// is the same value stored at raw offset 24 in the slot-26 equipment entry and
// passed as constructor field a10 by sub_1D77560.
type currentEquippedCreatureSnapshot struct {
	itemID         uint32
	serialOrHandle uint32
	name           string
	level          byte
	aliveState     byte
	townDisplay    bool
	source         string
}

func (snapshot currentEquippedCreatureSnapshot) valid() bool {
	return snapshot.itemID != 0 && snapshot.serialOrHandle != 0
}

func (s *Service) currentEquippedCreatureForCharacter(ctx context.Context, characterID string) currentEquippedCreatureSnapshot {
	snapshot, _ := s.currentEquippedCreatureForCharacterWithError(ctx, characterID)
	return snapshot
}

// currentEquippedCreatureForCharacterWithError distinguishes an authoritative
// "nothing worn" result from a transient repository failure. The staged
// post-op24 chain must not permanently skip op102 merely because one database
// read failed; ordinary snapshot readers retain the compatibility wrapper
// above when they can safely render an empty optional creature.
func (s *Service) currentEquippedCreatureForCharacterWithError(
	ctx context.Context,
	characterID string,
) (currentEquippedCreatureSnapshot, error) {
	if s == nil || strings.TrimSpace(characterID) == "" {
		return currentEquippedCreatureSnapshot{}, fmt.Errorf("equipped creature owner is unavailable")
	}
	repos, ok := s.repositoryGroup()
	if !ok {
		return currentEquippedCreatureSnapshot{}, fmt.Errorf("equipped creature repositories are unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var petRecord dnfrepo.PetRecord
	petFound := false
	var petLoadErr error
	if repos.Pet != nil {
		if loaded, found, err := repos.Pet.Load(ctx, characterID); err != nil {
			petLoadErr = fmt.Errorf("load equipped creature metadata for character %s: %w", characterID, err)
		} else if found {
			petRecord = dnfrepo.ClonePet(loaded)
			petFound = true
		}
	}

	if repos.Equipment != nil {
		equipment, found, err := repos.Equipment.Load(ctx, characterID)
		if err != nil {
			return currentEquippedCreatureSnapshot{}, fmt.Errorf(
				"load equipped creature slot for character %s: %w",
				characterID,
				err,
			)
		}
		if found {
			entry, worn := currentEquippedCreatureEquipmentEntry(equipment)
			if !worn {
				// A present equipment record is authoritative. Do not resurrect
				// a stale PetRecord.EquippedKey after slot 26 was removed.
				return currentEquippedCreatureSnapshot{source: "equipment_slot26_absent"}, nil
			}
			snapshot := currentEquippedCreatureFromEquipment(entry)
			if !snapshot.valid() {
				return currentEquippedCreatureSnapshot{source: "equipment_slot26_invalid_item_or_serial"}, nil
			}
			if petLoadErr != nil {
				return currentEquippedCreatureSnapshot{}, petLoadErr
			}
			snapshot.source = "equipment_slot26_raw"
			if petFound {
				currentEquippedCreatureEnrichFromPetRecord(&snapshot, petRecord)
			}
			s.currentEquippedCreatureEnrichNameFromPVF(&snapshot)
			return snapshot, nil
		}
	}

	// Compatibility for imported records which predate the equipment owner.
	if petLoadErr != nil {
		return currentEquippedCreatureSnapshot{}, petLoadErr
	}
	// This fallback is used only when no EquipmentRecord exists at all.
	if petFound {
		if entry, found := petRecord.Entries[strings.TrimSpace(petRecord.EquippedKey)]; found {
			snapshot := currentEquippedCreatureFromPetEntry(entry)
			if snapshot.valid() {
				snapshot.townDisplay = petRecord.TownDisplay
				snapshot.source = "legacy_pet_equipped_key"
				s.currentEquippedCreatureEnrichNameFromPVF(&snapshot)
				return snapshot, nil
			}
		}
	}
	return currentEquippedCreatureSnapshot{}, nil
}

func currentEquippedCreatureEquipmentEntry(record dnfrepo.EquipmentRecord) (dnfrepo.EquipmentEntry, bool) {
	if entry, ok := record.Entries["26"]; ok && entry.SlotIndex == 26 && entry.ItemID > 0 {
		return entry, true
	}
	for _, entry := range record.Entries {
		if entry.SlotIndex == 26 && entry.ItemID > 0 {
			return entry, true
		}
	}
	return dnfrepo.EquipmentEntry{}, false
}

func currentEquippedCreatureFromEquipment(entry dnfrepo.EquipmentEntry) currentEquippedCreatureSnapshot {
	itemID, itemOK := currentCreatureWireUint32(entry.ItemID)
	serial := currentEquippedCreatureSerialFromRaw(entry.RawEntry)
	if serial == 0 {
		serial = currentCreatureExtraUint32(entry.Extra,
			"creature_serial_or_handle", "creature_serial", "pet_serial")
	}
	if serial == 0 && currentCreatureEquipmentSourceIsPet(entry.Extra) {
		serial = currentCreatureExtraUint32(entry.Extra, "serial", "handle", "instance_value", "item_uid")
	}
	if !itemOK || serial == 0 {
		return currentEquippedCreatureSnapshot{}
	}
	return currentEquippedCreatureSnapshot{
		itemID:         itemID,
		serialOrHandle: serial,
		name:           currentCreatureExtraString(entry.Extra, "creature_name", "pet_name"),
		level:          currentCreatureExtraByte(entry.Extra, "creature_level", "pet_level"),
		aliveState:     1,
	}
}

func currentEquippedCreatureFromPetEntry(entry dnfrepo.PetEntry) currentEquippedCreatureSnapshot {
	itemID, itemOK := currentCreatureWireUint32(entry.ItemID)
	serial := currentCreatureExtraUint32(entry.Extra,
		"creature_serial_or_handle", "creature_serial", "pet_serial",
		"serial", "handle", "instance_value", "item_uid")
	if serial == 0 {
		if parsed, err := strconv.ParseUint(strings.TrimSpace(entry.PetKey), 0, 32); err == nil {
			serial = uint32(parsed)
		}
	}
	if !itemOK || serial == 0 {
		return currentEquippedCreatureSnapshot{}
	}
	return currentEquippedCreatureSnapshot{
		itemID:         itemID,
		serialOrHandle: serial,
		name:           strings.TrimSpace(entry.Name),
		level:          currentCreatureLevelByte(entry.Level),
		aliveState:     1,
	}
}

func currentEquippedCreatureEnrichFromPetRecord(snapshot *currentEquippedCreatureSnapshot, record dnfrepo.PetRecord) {
	if snapshot == nil || !snapshot.valid() {
		return
	}
	entry, found := currentEquippedCreatureMetadataEntry(*snapshot, record)
	if found {
		if strings.TrimSpace(entry.Name) != "" {
			snapshot.name = strings.TrimSpace(entry.Name)
		}
		if entry.Level > 0 {
			snapshot.level = currentCreatureLevelByte(entry.Level)
		}
	}
	snapshot.townDisplay = record.TownDisplay
}

func currentEquippedCreatureMetadataEntry(snapshot currentEquippedCreatureSnapshot, record dnfrepo.PetRecord) (dnfrepo.PetEntry, bool) {
	if key := strings.TrimSpace(record.EquippedKey); key != "" {
		if entry, ok := record.Entries[key]; ok && currentCreatureItemMatches(entry.ItemID, snapshot.itemID) {
			entrySerial := currentCreaturePetEntrySerial(entry)
			if entrySerial == 0 || entrySerial == snapshot.serialOrHandle {
				return entry, true
			}
		}
	}
	serialKey := strconv.FormatUint(uint64(snapshot.serialOrHandle), 10)
	if entry, ok := record.Entries[serialKey]; ok && currentCreatureItemMatches(entry.ItemID, snapshot.itemID) {
		return entry, true
	}
	var candidate dnfrepo.PetEntry
	matchCount := 0
	for _, entry := range record.Entries {
		if !currentCreatureItemMatches(entry.ItemID, snapshot.itemID) {
			continue
		}
		entrySerial := currentCreaturePetEntrySerial(entry)
		if entrySerial == snapshot.serialOrHandle {
			return entry, true
		}
		candidate = entry
		matchCount++
	}
	return candidate, matchCount == 1
}

// currentEquippedCreatureEnrichNameFromPVF repairs only an absent custom name
// at the scene projection boundary. The final mode0 actor packet creates the
// live town creature object and must carry a non-empty DSTR there; op105 alone
// only populates the creature-state table. Durable renamed values remain
// authoritative, and the PVF fallback is never persisted.
func (s *Service) currentEquippedCreatureEnrichNameFromPVF(snapshot *currentEquippedCreatureSnapshot) {
	if s == nil || snapshot == nil || !snapshot.valid() || strings.TrimSpace(snapshot.name) != "" {
		return
	}
	catalog, err := s.currentPetPVFCatalog()
	if err != nil || catalog == nil {
		return
	}
	definition, err := catalog.ResolveCreature(int64(snapshot.itemID))
	if err != nil {
		return
	}
	snapshot.name = strings.TrimSpace(definition.Name)
}

func currentEquippedCreatureSerialFromRaw(raw []byte) uint32 {
	// op2/sub_20077D0 consumes the constructor-a10 field repeated at +24 in a
	// current pet equipment row. Do not fall back to the ordinary +5 equipment
	// instance: ordinary equipment instance values are not creature serials.
	if len(raw) >= 28 {
		return binary.LittleEndian.Uint32(raw[24:28])
	}
	return 0
}

func currentCreatureEquipmentSourceIsPet(extra map[string]string) bool {
	source := strings.ToLower(currentCreatureExtraString(extra, "source", "item_kind", "equipment_kind"))
	return strings.Contains(source, "pet") || strings.Contains(source, "creature")
}

func currentCreaturePetEntrySerial(entry dnfrepo.PetEntry) uint32 {
	return currentCreatureExtraUint32(entry.Extra,
		"creature_serial_or_handle", "creature_serial", "pet_serial",
		"serial", "handle", "instance_value", "item_uid")
}

func currentCreatureWireUint32(value int64) (uint32, bool) {
	if value <= 0 || value > int64(^uint32(0)) {
		return 0, false
	}
	return uint32(value), true
}

func currentCreatureItemMatches(value int64, wire uint32) bool {
	itemID, ok := currentCreatureWireUint32(value)
	return ok && itemID == wire
}

func currentCreatureExtraUint32(extra map[string]string, keys ...string) uint32 {
	for _, key := range keys {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		if value, err := strconv.ParseUint(raw, 0, 32); err == nil && value != 0 {
			return uint32(value)
		}
	}
	return 0
}

func currentCreatureExtraString(extra map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(extra[key]); value != "" {
			return value
		}
	}
	return ""
}

func currentCreatureExtraByte(extra map[string]string, keys ...string) byte {
	for _, key := range keys {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		if value, err := strconv.ParseInt(raw, 0, 64); err == nil && value > 0 {
			return currentCreatureLevelByte(value)
		}
	}
	return 0
}

func currentCreatureLevelByte(value int64) byte {
	if value <= 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return byte(value)
}

func (r *csharpLegacyUserInfoReader) currentEquippedCreature() currentEquippedCreatureSnapshot {
	if r == nil || r.service == nil {
		return currentEquippedCreatureSnapshot{}
	}
	return r.service.currentEquippedCreatureForCharacter(r.ctx, r.characterID)
}
