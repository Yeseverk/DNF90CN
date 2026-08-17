package creaturestate

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrInventoryNotFound = errors.New("creature inventory record not found")
	ErrStateInvalid      = errors.New("creature inventory state is invalid")
)

const (
	petListType         byte  = 7
	petInventorySlotMax int16 = 139
	petEquipmentSlot    int16 = 26
	fullSatietyMicros         = int64(100_000_000)
)

type inventoryCreature struct {
	slot  int16
	stack dnfrepo.ItemStack
}

// ReconcileInventory creates typed state only for PVF-proven direct creature
// grants, then synchronizes every known inventory creature's durable slot.
// Callers must provide transaction-scoped repositories.
func ReconcileInventory(
	ctx context.Context,
	characterID string,
	inventories dnfrepo.InventoryRepository,
	equipment dnfrepo.EquipmentRepository,
	pets dnfrepo.PetRepository,
) (bool, error) {
	if inventories == nil || equipment == nil || pets == nil {
		return false, fmt.Errorf("%w: repositories unavailable", ErrStateInvalid)
	}
	inventory, found, err := inventories.Load(ctx, characterID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, ErrInventoryNotFound
	}
	inventory = dnfrepo.CloneInventory(inventory)
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}

	equipmentRecord, found, err := equipment.Load(ctx, characterID)
	if err != nil {
		return false, err
	}
	if !found {
		equipmentRecord = dnfrepo.EquipmentRecord{CharacterID: characterID, Entries: make(map[string]dnfrepo.EquipmentEntry)}
	} else {
		equipmentRecord = dnfrepo.CloneEquipment(equipmentRecord)
	}

	petRecord, petRecordFound, err := pets.Load(ctx, characterID)
	if err != nil {
		return false, err
	}
	if !petRecordFound {
		petRecord = dnfrepo.PetRecord{CharacterID: characterID, Entries: make(map[string]dnfrepo.PetEntry)}
	} else {
		petRecord = dnfrepo.ClonePet(petRecord)
		petRecord.CharacterID = characterID
		if petRecord.Entries == nil {
			petRecord.Entries = make(map[string]dnfrepo.PetEntry)
		}
	}

	creatures := inventoryCreatures(inventory)
	entriesBySerial := make(map[uint32]string, len(petRecord.Entries))
	used := make(map[uint32]struct{}, len(creatures)+len(petRecord.Entries)+1)
	for key, entry := range petRecord.Entries {
		serial, ok := petEntrySerial(key, entry)
		if !ok {
			continue
		}
		if previous, duplicate := entriesBySerial[serial]; duplicate && previous != key {
			return false, fmt.Errorf("%w: duplicate pet serial=%d keys=%s,%s", ErrStateInvalid, serial, previous, key)
		}
		entriesBySerial[serial] = key
		used[serial] = struct{}{}
	}

	containerSerials := make(map[uint32]string, len(creatures)+1)
	for _, creature := range creatures {
		if serial, ok := stackSerial(creature.stack.Extra); ok {
			location := fmt.Sprintf("7:%d", creature.slot)
			if previous, duplicate := containerSerials[serial]; duplicate && previous != location {
				return false, fmt.Errorf("%w: duplicate creature serial=%d locations=%s,%s", ErrStateInvalid, serial, previous, location)
			}
			containerSerials[serial] = location
			used[serial] = struct{}{}
		}
	}
	if worn, ok := equipmentRecord.Entries[strconv.Itoa(int(petEquipmentSlot))]; ok && worn.ItemID > 0 {
		if serial, ok := equipmentSerial(worn); ok {
			location := "3:26"
			if previous, duplicate := containerSerials[serial]; duplicate && previous != location {
				return false, fmt.Errorf("%w: duplicate creature serial=%d locations=%s,%s", ErrStateInvalid, serial, previous, location)
			}
			containerSerials[serial] = location
			used[serial] = struct{}{}
		}
	}

	inventoryChanged := false
	petChanged := false
	for _, creature := range creatures {
		stack := creature.stack
		serial, hasSerial := stackSerial(stack.Extra)
		entryKey := ""
		if hasSerial {
			entryKey = entriesBySerial[serial]
		}
		if entryKey == "" {
			if !isDirectCreatureGrant(stack) {
				continue
			}
			if !hasSerial {
				serial, err = allocateSerial(used)
				if err != nil {
					return false, err
				}
				stack.Extra = cloneExtra(stack.Extra)
				stack.Extra["creature_serial_or_handle"] = strconv.FormatUint(uint64(serial), 10)
				stack.Extra["creature_key"] = strconv.FormatUint(uint64(serial), 10)
				inventory.Slots[slotKey(creature.slot)] = stack
				inventoryChanged = true
			}
			entryKey = strconv.FormatUint(uint64(serial), 10)
			if existing, occupied := petRecord.Entries[entryKey]; occupied && existing.ItemID != stack.ItemID {
				return false, fmt.Errorf("%w: pet key=%s item=%d inventory_item=%d", ErrStateInvalid, entryKey, existing.ItemID, stack.ItemID)
			}
			petRecord.Entries[entryKey] = dnfrepo.PetEntry{
				PetKey:          entryKey,
				CreatureKey:     serial,
				ItemID:          stack.ItemID,
				SourceListType:  petListType,
				SourceSlotIndex: creature.slot,
				Satiety:         100,
				SatietyMicros:   fullSatietyMicros,
				Level:           1,
				Extra:           cloneExtra(stack.Extra),
			}
			entriesBySerial[serial] = entryKey
			used[serial] = struct{}{}
			petChanged = true
			continue
		}

		entry := petRecord.Entries[entryKey]
		if entry.ItemID != stack.ItemID {
			return false, fmt.Errorf("%w: serial=%d pet_item=%d inventory_item=%d", ErrStateInvalid, serial, entry.ItemID, stack.ItemID)
		}
		if entry.SourceListType != petListType || entry.SourceSlotIndex != creature.slot {
			entry.SourceListType = petListType
			entry.SourceSlotIndex = creature.slot
			petRecord.Entries[entryKey] = entry
			petChanged = true
		}
	}

	if inventoryChanged {
		inventory.CharacterID = characterID
		inventory.UpdatedAt = time.Now()
		if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
			return false, err
		}
	}
	if petChanged {
		petRecord.UpdatedAt = time.Now()
		if err := dnfrepo.SavePetFields(ctx, pets, petRecord, dnfrepo.PetFieldEntries); err != nil {
			return false, err
		}
	}
	return inventoryChanged || petChanged, nil
}

func inventoryCreatures(record dnfrepo.InventoryRecord) []inventoryCreature {
	out := make([]inventoryCreature, 0)
	for key, stack := range record.Slots {
		listType, slot, ok := parseSlotKey(key)
		if !ok || listType != petListType || slot < 0 || slot > petInventorySlotMax || stack.ItemID <= 0 || stack.Count <= 0 {
			continue
		}
		out = append(out, inventoryCreature{slot: slot, stack: stack})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].slot < out[j].slot })
	return out
}

func isDirectCreatureGrant(stack dnfrepo.ItemStack) bool {
	if stack.Count != 1 {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(stack.Extra["item_kind"]))
	equipmentType := strings.ToLower(strings.Trim(strings.TrimSpace(stack.Extra["equipment_type"]), "[]"))
	path := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(stack.Extra["pvf_path"]), "\\", "/"))
	return kind == "equipment" && equipmentType == "creature" && strings.HasPrefix(path, "equipment/creature/")
}

func allocateSerial(used map[uint32]struct{}) (uint32, error) {
	for serial := uint32(1); serial != 0; serial++ {
		if _, exists := used[serial]; !exists {
			used[serial] = struct{}{}
			return serial, nil
		}
	}
	return 0, fmt.Errorf("%w: no creature serial available", ErrStateInvalid)
}

func equipmentSerial(entry dnfrepo.EquipmentEntry) (uint32, bool) {
	if len(entry.RawEntry) >= 28 {
		left := binary.LittleEndian.Uint32(entry.RawEntry[5:9])
		right := binary.LittleEndian.Uint32(entry.RawEntry[24:28])
		if left != 0 && left == right {
			return left, true
		}
	}
	return stackSerial(entry.Extra)
}

func stackSerial(extra map[string]string) (uint32, bool) {
	for _, key := range []string{"creature_serial_or_handle", "creature_serial", "pet_serial", "serial", "handle", "instance_value", "item_uid"} {
		value, err := strconv.ParseUint(strings.TrimSpace(extra[key]), 0, 32)
		if err == nil && value != 0 {
			return uint32(value), true
		}
	}
	return 0, false
}

func petEntrySerial(key string, entry dnfrepo.PetEntry) (uint32, bool) {
	if entry.CreatureKey != 0 {
		return entry.CreatureKey, true
	}
	value, err := strconv.ParseUint(strings.TrimSpace(entry.PetKey), 0, 32)
	if err != nil || value == 0 {
		value, err = strconv.ParseUint(strings.TrimSpace(key), 0, 32)
	}
	if err != nil || value == 0 {
		return 0, false
	}
	return uint32(value), true
}

func parseSlotKey(key string) (byte, int16, bool) {
	listRaw, slotRaw, ok := strings.Cut(key, ":")
	if !ok {
		return 0, 0, false
	}
	listType, err := strconv.ParseUint(listRaw, 10, 8)
	if err != nil {
		return 0, 0, false
	}
	slot, err := strconv.ParseInt(slotRaw, 10, 16)
	if err != nil {
		return 0, 0, false
	}
	return byte(listType), int16(slot), true
}

func slotKey(slot int16) string {
	return fmt.Sprintf("%d:%d", petListType, slot)
}

func cloneExtra(extra map[string]string) map[string]string {
	out := make(map[string]string, len(extra)+2)
	for key, value := range extra {
		out[key] = value
	}
	return out
}
