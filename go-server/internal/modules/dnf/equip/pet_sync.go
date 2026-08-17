package equip

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// syncPetEquipmentMove runs after the list-7/slot-26 mutation but before its
// CharacterPetUnitOfWork callback returns. It updates only location/equipped
// ownership fields in PetRecord; name, level, experience, satiety, evolution
// mode, and all opaque persisted metadata remain untouched.
func (o *Owner) syncPetEquipmentMove(ctx context.Context, characterID string, result MoveResult) error {
	if o == nil || o.pets == nil {
		return ErrPetRepositoryRequired
	}
	petSlot, ok := petInventorySlotFromMoveResult(result)
	if !ok {
		return fmt.Errorf("%w: result endpoints=(%d,%d)->(%d,%d)",
			ErrPetOwnershipMismatch,
			result.SourceListType,
			result.SourceSlotIndex,
			result.DestinationListType,
			result.DestinationSlotIndex,
		)
	}

	inventory, equipment, err := o.loadMoveRecords(ctx, characterID)
	if err != nil {
		return err
	}
	record, found, err := o.pets.Load(ctx, characterID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: character=%s", ErrPetRecordNotFound, characterID)
	}
	record = dnfrepo.ClonePet(record)
	record.CharacterID = characterID
	if len(record.Entries) == 0 {
		return fmt.Errorf("%w: character=%s", ErrPetEntryNotFound, characterID)
	}

	petStack, petStackFound := inventory.Slots[inventoryKey(wireListPet, petSlot)]
	equipped, equippedFound := equipment.Entries[entryKey(petCreatureWornSlot)]
	if equippedFound && equipped.SlotIndex != petCreatureWornSlot {
		return fmt.Errorf("%w: equipped key=%d entry=%d", ErrMoveSlotMismatch, petCreatureWornSlot, equipped.SlotIndex)
	}

	var inventoryKey string
	if petStackFound {
		serial := petStackSerial(petStack)
		inventoryKey, err = findPetEntryKey(record, serial, petStack.ItemID)
		if err != nil {
			return err
		}
	}
	var equippedKey string
	if equippedFound {
		serial := petEquipmentSerial(equipped)
		equippedKey, err = findPetEntryKey(record, serial, equipped.ItemID)
		if err != nil {
			return err
		}
	}
	if inventoryKey != "" && inventoryKey == equippedKey {
		return fmt.Errorf("%w: pet=%s occupies list7 and slot%d", ErrPetOwnershipMismatch, inventoryKey, petCreatureWornSlot)
	}

	if err := validatePetMoveProjection(
		result,
		record.EquippedKey,
		inventoryKey,
		equippedKey,
		petStackFound,
		equippedFound,
		petStack.ItemID,
		equipped.ItemID,
	); err != nil {
		return err
	}
	if petStackFound {
		entry := record.Entries[inventoryKey]
		entry.SourceListType = wireListPet
		entry.SourceSlotIndex = petSlot
		record.Entries[inventoryKey] = entry
	}
	if equippedFound {
		entry := record.Entries[equippedKey]
		entry.SourceListType = wireListEquipment
		entry.SourceSlotIndex = petCreatureWornSlot
		record.Entries[equippedKey] = entry
		record.EquippedKey = equippedKey
	} else {
		record.EquippedKey = ""
	}
	record.UpdatedAt = time.Now()
	return dnfrepo.SavePetFields(ctx, o.pets, record, dnfrepo.PetFieldEntries, dnfrepo.PetFieldEquipped)
}

func petInventorySlotFromMoveResult(result MoveResult) (int16, bool) {
	if result.SourceListType == wireListPet && result.DestinationListType != wireListPet {
		return result.SourceSlotIndex, result.SourceSlotIndex >= 0
	}
	if result.DestinationListType == wireListPet && result.SourceListType != wireListPet {
		return result.DestinationSlotIndex, result.DestinationSlotIndex >= 0
	}
	return 0, false
}

func validatePetMoveProjection(
	result MoveResult,
	previousEquippedKey string,
	inventoryKey string,
	equippedKey string,
	inventoryFound bool,
	equippedFound bool,
	inventoryItemID int64,
	equippedItemID int64,
) error {
	previousEquippedKey = strings.TrimSpace(previousEquippedKey)
	switch result.Mode {
	case "equip":
		if inventoryFound || !equippedFound || result.ItemID <= 0 || result.SwappedItemID != 0 || equippedItemID != result.ItemID {
			return fmt.Errorf("%w: invalid equip projection", ErrPetOwnershipMismatch)
		}
		if previousEquippedKey != "" {
			return fmt.Errorf("%w: persisted equipped=%s but slot%d was empty", ErrPetOwnershipMismatch, previousEquippedKey, petCreatureWornSlot)
		}
	case "equip_swap":
		if !inventoryFound || !equippedFound || inventoryKey == "" || equippedKey == "" || result.SwappedItemID <= 0 ||
			inventoryItemID != result.SwappedItemID || equippedItemID != result.ItemID {
			return fmt.Errorf("%w: invalid equip swap projection", ErrPetOwnershipMismatch)
		}
		if previousEquippedKey != "" && previousEquippedKey != inventoryKey {
			return fmt.Errorf("%w: persisted equipped=%s swapped=%s", ErrPetOwnershipMismatch, previousEquippedKey, inventoryKey)
		}
	case "unequip":
		if !inventoryFound || equippedFound || inventoryKey == "" || result.ItemID <= 0 || result.SwappedItemID != 0 || inventoryItemID != result.ItemID {
			return fmt.Errorf("%w: invalid unequip projection", ErrPetOwnershipMismatch)
		}
		if previousEquippedKey != "" && previousEquippedKey != inventoryKey {
			return fmt.Errorf("%w: persisted equipped=%s removed=%s", ErrPetOwnershipMismatch, previousEquippedKey, inventoryKey)
		}
	case "unequip_swap":
		if !inventoryFound || !equippedFound || inventoryKey == "" || equippedKey == "" || result.SwappedItemID <= 0 ||
			inventoryItemID != result.ItemID || equippedItemID != result.SwappedItemID {
			return fmt.Errorf("%w: invalid unequip swap projection", ErrPetOwnershipMismatch)
		}
		if previousEquippedKey != "" && previousEquippedKey != inventoryKey {
			return fmt.Errorf("%w: persisted equipped=%s removed=%s", ErrPetOwnershipMismatch, previousEquippedKey, inventoryKey)
		}
	default:
		return fmt.Errorf("%w: unsupported pet move mode=%q", ErrPetOwnershipMismatch, result.Mode)
	}
	return nil
}

func petStackSerial(stack dnfrepo.ItemStack) int64 {
	return firstExtraInt(stack.Extra, 0,
		"creature_serial_or_handle",
		"creature_serial",
		"pet_serial",
		"serial",
		"handle",
		"instance_value",
		"item_uid",
	)
}

func petEquipmentSerial(entry dnfrepo.EquipmentEntry) int64 {
	if serial := petSerialFromEquippedRaw(entry.RawEntry); serial > 0 {
		return serial
	}
	return firstExtraInt(entry.Extra, 0,
		"creature_serial_or_handle",
		"creature_serial",
		"pet_serial",
		"serial",
		"handle",
		"instance_value",
		"item_uid",
	)
}

func findPetEntryKey(record dnfrepo.PetRecord, serial int64, itemID int64) (string, error) {
	if serial <= 0 || serial > int64(^uint32(0)) || itemID <= 0 {
		return "", fmt.Errorf("%w: serial=%d item=%d", ErrPetOwnershipMismatch, serial, itemID)
	}
	directKey := strconv.FormatInt(serial, 10)
	if entry, ok := record.Entries[directKey]; ok {
		entrySerial, valid := petEntrySerial(entry)
		if entry.ItemID != itemID || (valid && entrySerial != serial) {
			return "", fmt.Errorf("%w: key=%s serial=%d item=%d", ErrPetOwnershipMismatch, directKey, serial, itemID)
		}
	}

	matchedKey := ""
	for key, entry := range record.Entries {
		entrySerial, valid := petEntrySerial(entry)
		if key == directKey && !valid {
			entrySerial, valid = serial, true
		}
		if !valid || entrySerial != serial {
			continue
		}
		if entry.ItemID != itemID {
			return "", fmt.Errorf("%w: serial=%d inventory_item=%d pet_item=%d", ErrPetOwnershipMismatch, serial, itemID, entry.ItemID)
		}
		if matchedKey != "" {
			return "", fmt.Errorf("%w: duplicate serial=%d keys=%s,%s", ErrPetOwnershipMismatch, serial, matchedKey, key)
		}
		matchedKey = key
	}
	if matchedKey == "" {
		return "", fmt.Errorf("%w: serial=%d item=%d", ErrPetEntryNotFound, serial, itemID)
	}
	return matchedKey, nil
}

func petEntrySerial(entry dnfrepo.PetEntry) (int64, bool) {
	if entry.CreatureKey != 0 {
		return int64(entry.CreatureKey), true
	}
	if parsed, err := strconv.ParseUint(strings.TrimSpace(entry.PetKey), 0, 32); err == nil && parsed != 0 {
		return int64(parsed), true
	}
	value := firstExtraInt(entry.Extra, 0,
		"creature_serial_or_handle",
		"creature_serial",
		"pet_serial",
		"serial",
		"handle",
		"instance_value",
		"item_uid",
	)
	return value, value > 0 && value <= int64(^uint32(0))
}
