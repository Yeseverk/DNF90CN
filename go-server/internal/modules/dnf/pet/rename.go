package pet

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentPetRenameCardItemID   int64 = 25
	currentPetRenameCardPVFPath        = "stackable/cash/creature/rename_card.stk"
	currentEquippedCreatureSlot        = 26
	currentMainInventoryListType byte  = 0
)

var (
	ErrPetRecordNotFound       = errors.New("pet record not found")
	ErrPetRenameNameInvalid    = errors.New("pet rename name is invalid")
	ErrPetRenameCardInvalid    = errors.New("pet rename card is invalid")
	ErrPetInventoryMismatch    = errors.New("pet inventory and creature record are inconsistent")
	ErrPetRecordOwnerMismatch  = errors.New("pet aggregate owner does not match selected character")
	ErrPetCreatureSerialAbsent = errors.New("pet inventory serial is absent")
	ErrPetEquippedAbsent       = errors.New("equipped pet creature is absent")
)

// RenameCommand identifies the main-inventory rename-card slot and carries the
// current EXE's fixed creature semantic value in ListType. Item and creature
// identity are deliberately absent: both are loaded from durable state.
type RenameCommand struct {
	SelectedCharacterID uint16
	ListType            byte
	SlotIndex           int16
	NameRaw             []byte
}

// RenameResult contains only committed identity and the proved ACK fields.
type RenameResult struct {
	CharacterID    string
	PetKey         string
	CreatureSerial uint32
	ItemID         int64
	ListType       byte
	SourceListType byte
	SlotIndex      int16
	NameRaw        []byte
	RemainingCount int64
	Changed        bool
}

// Rename validates and consumes the exact current-PVF rename card from the
// main inventory, then renames the creature installed in equipment slot 26 in
// one character-scoped transaction. Current EXE sub_31F5220 sends the card
// slot, a fixed creature semantic value 7 and the new DSTR; it does not send a
// durable inventory list or creature inventory slot. Its op100 success handler
// sub_1D1D5C0 consumes the echoed main-inventory slot by one.
func (o *Owner) Rename(ctx context.Context, cmd RenameCommand) (RenameResult, error) {
	if o == nil || o.pets == nil {
		return RenameResult{}, ErrPetOwnerUnavailable
	}
	if o.characterPets == nil {
		return RenameResult{}, ErrPetTransactionUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return RenameResult{}, ErrCharacterRequired
	}
	if cmd.ListType != listTypePet {
		return RenameResult{}, fmt.Errorf("%w: %d", ErrUnsupportedList, cmd.ListType)
	}
	if cmd.SlotIndex < 0 || cmd.SlotIndex > 139 {
		return RenameResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.ListType, cmd.SlotIndex)
	}
	if err := validateCreatureRenameName(cmd.NameRaw); err != nil {
		return RenameResult{}, fmt.Errorf("%w: %v", ErrPetRenameNameInvalid, err)
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	var committed RenameResult
	err := o.characterPets.WithinCharacterPets(ctx, characterID, func(
		inventoryRepo dnfrepo.InventoryRepository,
		equipmentRepo dnfrepo.EquipmentRepository,
		petRepo dnfrepo.PetRepository,
	) error {
		inventory, found, err := inventoryRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrInventoryNotFound
		}
		if strings.TrimSpace(inventory.CharacterID) != characterID {
			return fmt.Errorf("%w: inventory=%q selected=%q", ErrPetRecordOwnerMismatch, inventory.CharacterID, characterID)
		}
		inventory = dnfrepo.CloneInventory(inventory)
		// Current EXE sub_31F5220 always writes semantic creature value 7
		// after the rename-card slot. The slot itself is a main-inventory
		// index; 7 is not the durable source collection.
		cardSlot := slotKey(currentMainInventoryListType, cmd.SlotIndex)
		card, found := inventory.Slots[cardSlot]
		if !found || card.Count <= 0 {
			return fmt.Errorf("%w: list=%d slot=%d", ErrPetRenameCardInvalid, cmd.ListType, cmd.SlotIndex)
		}
		if !isCurrentPetRenameCard(card) {
			return fmt.Errorf(
				"%w: list=%d slot=%d item=%d path=%q type=%q",
				ErrPetRenameCardInvalid,
				cmd.ListType,
				cmd.SlotIndex,
				card.ItemID,
				card.Extra["pvf_path"],
				card.Extra["stackable_type"],
			)
		}

		equipment, found, err := equipmentRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrPetEquippedAbsent
		}
		if strings.TrimSpace(equipment.CharacterID) != characterID {
			return fmt.Errorf("%w: equipment=%q selected=%q", ErrPetRecordOwnerMismatch, equipment.CharacterID, characterID)
		}
		equippedItemID, equippedSerial, err := equippedCreatureIdentity(equipment)
		if err != nil {
			return err
		}

		petRecord, found, err := petRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrPetRecordNotFound
		}
		if strings.TrimSpace(petRecord.CharacterID) != characterID {
			return fmt.Errorf("%w: pet=%q selected=%q", ErrPetRecordOwnerMismatch, petRecord.CharacterID, characterID)
		}
		petRecord = dnfrepo.ClonePet(petRecord)
		if _, err := validatedCreatureEntries(petRecord); err != nil {
			return err
		}

		petKey := strconv.FormatUint(uint64(equippedSerial), 10)
		entry, found := petRecord.Entries[petKey]
		if !found {
			return fmt.Errorf("%w: serial=%d has no PetRecord entry", ErrPetInventoryMismatch, equippedSerial)
		}
		if entry.PetKey != petKey || entry.CreatureKey != equippedSerial || entry.ItemID != equippedItemID {
			return fmt.Errorf(
				"%w: equipped_item=%d serial=%d entry_key=%q entry_serial=%d entry_item=%d",
				ErrPetInventoryMismatch,
				equippedItemID,
				equippedSerial,
				entry.PetKey,
				entry.CreatureKey,
				entry.ItemID,
			)
		}
		if equippedKey := strings.TrimSpace(petRecord.EquippedKey); equippedKey != petKey {
			return fmt.Errorf("%w: equipment_serial=%d equipped_key=%q", ErrPetInventoryMismatch, equippedSerial, equippedKey)
		}

		currentName := entry.NameRaw
		if len(currentName) == 0 && entry.Name != "" {
			currentName = []byte(entry.Name)
		}
		changed := !bytes.Equal(currentName, cmd.NameRaw)
		if changed {
			entry.NameRaw = append([]byte(nil), cmd.NameRaw...)
			entry.Name = decodeCurrentCreatureName(cmd.NameRaw)
			petRecord.Entries[petKey] = entry
			petRecord.UpdatedAt = time.Now()
		}
		remaining := card.Count - 1
		if remaining == 0 {
			delete(inventory.Slots, cardSlot)
		} else {
			card.Count = remaining
			inventory.Slots[cardSlot] = card
		}
		inventory.UpdatedAt = time.Now()
		if err := dnfrepo.SaveInventoryFields(ctx, inventoryRepo, inventory, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}
		if changed {
			if err := dnfrepo.SavePetFields(ctx, petRepo, petRecord, dnfrepo.PetFieldEntries); err != nil {
				return err
			}
		}

		committed = RenameResult{
			CharacterID:    characterID,
			PetKey:         petKey,
			CreatureSerial: equippedSerial,
			ItemID:         equippedItemID,
			ListType:       cmd.ListType,
			SourceListType: currentMainInventoryListType,
			SlotIndex:      cmd.SlotIndex,
			NameRaw:        append([]byte(nil), cmd.NameRaw...),
			RemainingCount: remaining,
			Changed:        changed,
		}
		return nil
	})
	if err != nil {
		return RenameResult{}, err
	}
	return committed, nil
}

func isCurrentPetRenameCard(stack dnfrepo.ItemStack) bool {
	if stack.ItemID != currentPetRenameCardItemID || stack.Count <= 0 || stack.Extra == nil {
		return false
	}
	path := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(stack.Extra["pvf_path"]), "\\", "/"))
	stackableType := strings.ToLower(strings.TrimSpace(stack.Extra["stackable_type"]))
	return path == currentPetRenameCardPVFPath && strings.Contains(stackableType, "[creature]")
}

func equippedCreatureIdentity(record dnfrepo.EquipmentRecord) (int64, uint32, error) {
	entry, found := record.Entries[strconv.Itoa(currentEquippedCreatureSlot)]
	if !found || entry.SlotIndex != currentEquippedCreatureSlot || entry.ItemID <= 0 {
		for _, candidate := range record.Entries {
			if candidate.SlotIndex == currentEquippedCreatureSlot && candidate.ItemID > 0 {
				entry = candidate
				found = true
				break
			}
		}
	}
	if !found || len(entry.RawEntry) < 28 {
		return 0, 0, ErrPetEquippedAbsent
	}
	serial := binary.LittleEndian.Uint32(entry.RawEntry[24:28])
	if serial == 0 || serial > maxCreatureSerial {
		return 0, 0, fmt.Errorf("%w: equipped serial=%d", ErrPetCreatureSerialAbsent, serial)
	}
	return entry.ItemID, serial, nil
}

func decodeCurrentCreatureName(raw []byte) string {
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
	if err != nil {
		return ""
	}
	return string(decoded)
}
