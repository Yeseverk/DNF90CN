// Package pet owns durable creature state. Mutations that span the pet
// inventory, worn creature slots and creature record always use one
// CharacterPetUnitOfWork.
package pet

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// Current NoPack writes and reads the creature identity as a full u32 in both
// op19 and scene op105. The former 20-bit ceiling came from historical server
// assumptions and rejects real current-client instance values.
const maxCreatureSerial uint32 = ^uint32(0)

var (
	ErrPetOwnerUnavailable       = errors.New("pet owner unavailable")
	ErrPetTransactionUnavailable = errors.New("character pet transaction unavailable")
	ErrPetCatalogUnavailable     = errors.New("pet PVF catalog unavailable")
	ErrCharacterRequired         = errors.New("selected character id required")
	ErrInventoryNotFound         = errors.New("inventory record not found")
	ErrUnsupportedList           = errors.New("pet list type is not supported")
	ErrSlotNotFound              = errors.New("pet item slot not found")
	ErrPetEggStackInvalid        = errors.New("pet egg stack must contain exactly one item")
	ErrPetSerialUnavailable      = errors.New("pet serial is unavailable")
	ErrPetStateInvalid           = errors.New("pet state is invalid")
)

// HatchCommand identifies the authoritative list-7 source slot. The request
// does not carry an item id; the owner reads the item from durable inventory
// and resolves the egg-to-creature mapping from the runtime PVF catalog.
type HatchCommand struct {
	SelectedCharacterID uint16
	ListType            byte
	SlotIndex           int16
}

// ListCommand requests the current durable creature read model.
type ListCommand struct {
	SelectedCharacterID uint16
}

// HatchResult contains only committed state used by current-EXE response
// builders.
type HatchResult struct {
	CharacterID     string
	PetKey          string
	ItemID          int64
	SourceSlotIndex int16
	Changed         bool
	PetInventory    map[string]dnfrepo.ItemStack
	EntryCount      int
	Entries         []dnfrepo.PetEntry
}

// ListResult is the current durable creature list.
type ListResult struct {
	CharacterID string
	EntryCount  int
	Entries     []dnfrepo.PetEntry
	EquippedKey string
	TownDisplay bool
}

// Owner owns pet list reads and atomic hatch mutations.
type Owner struct {
	pets          dnfrepo.PetRepository
	characterPets dnfrepo.CharacterPetUnitOfWork
	hatchResolver PetHatchResolver
}

// NewOwner creates an owner. A resolver is optional for read-only operations,
// but Hatch fails closed until a runtime PVF resolver is supplied.
func NewOwner(repos dnfrepo.Group, resolver ...PetHatchResolver) (*Owner, error) {
	if repos.Pet == nil {
		return nil, ErrPetOwnerUnavailable
	}
	var hatchResolver PetHatchResolver
	if len(resolver) > 0 {
		hatchResolver = resolver[0]
	}
	return &Owner{
		pets:          repos.Pet,
		characterPets: repos.CharacterPets,
		hatchResolver: hatchResolver,
	}, nil
}

// List reads the current creature record without mutating it.
func (o *Owner) List(ctx context.Context, cmd ListCommand) (ListResult, error) {
	if o == nil || o.pets == nil {
		return ListResult{}, ErrPetOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return ListResult{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	record, ok, err := o.pets.Load(ctx, characterID)
	if err != nil {
		return ListResult{}, err
	}
	if !ok {
		return ListResult{CharacterID: characterID}, nil
	}
	record = ensurePetRecord(dnfrepo.ClonePet(record), characterID)
	entries, err := validatedCreatureEntries(record)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{
		CharacterID: characterID,
		EntryCount:  len(entries),
		Entries:     entries,
		EquippedKey: record.EquippedKey,
		TownDisplay: record.TownDisplay,
	}, nil
}

// Hatch atomically replaces one real creature egg in list 7 with the PVF
// output creature and creates its typed level-1 growth record.
func (o *Owner) Hatch(ctx context.Context, cmd HatchCommand) (HatchResult, error) {
	if o == nil || o.pets == nil {
		return HatchResult{}, ErrPetOwnerUnavailable
	}
	if o.characterPets == nil {
		return HatchResult{}, ErrPetTransactionUnavailable
	}
	if o.hatchResolver == nil {
		return HatchResult{}, ErrPetCatalogUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return HatchResult{}, ErrCharacterRequired
	}
	if cmd.ListType != listTypePet {
		return HatchResult{}, fmt.Errorf("%w: %d", ErrUnsupportedList, cmd.ListType)
	}
	if cmd.SlotIndex < 0 || cmd.SlotIndex > 139 {
		return HatchResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.ListType, cmd.SlotIndex)
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	var committed HatchResult
	err := o.characterPets.WithinCharacterPets(ctx, characterID, func(
		inventoryRepo dnfrepo.InventoryRepository,
		equipmentRepo dnfrepo.EquipmentRepository,
		petRepo dnfrepo.PetRepository,
	) error {
		inventory, ok, err := inventoryRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInventoryNotFound
		}
		inventory = ensureInventoryRecord(dnfrepo.CloneInventory(inventory), characterID)

		equipment, ok, err := equipmentRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !ok {
			equipment = dnfrepo.EquipmentRecord{CharacterID: characterID, Entries: make(map[string]dnfrepo.EquipmentEntry)}
		} else {
			equipment = ensureEquipmentRecord(dnfrepo.CloneEquipment(equipment), characterID)
		}

		petRecord, ok, err := petRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !ok {
			petRecord = dnfrepo.PetRecord{CharacterID: characterID, Entries: make(map[string]dnfrepo.PetEntry)}
		} else {
			petRecord = ensurePetRecord(dnfrepo.ClonePet(petRecord), characterID)
		}

		slot := slotKey(cmd.ListType, cmd.SlotIndex)
		egg, found := inventory.Slots[slot]
		if !found || egg.Count <= 0 {
			return fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.ListType, cmd.SlotIndex)
		}
		if egg.Count != 1 {
			return fmt.Errorf("%w: list=%d slot=%d count=%d", ErrPetEggStackInvalid, cmd.ListType, cmd.SlotIndex, egg.Count)
		}

		definition, err := o.hatchResolver.ResolveHatch(egg.ItemID)
		if err != nil {
			return fmt.Errorf("resolve pet hatch item_id=%d: %w", egg.ItemID, err)
		}
		if definition.EggItemID != egg.ItemID || definition.HatchedItemID <= 0 || definition.HatchedItemID == egg.ItemID {
			return fmt.Errorf("%w: source=%d resolved_source=%d output=%d", ErrPetStateInvalid, egg.ItemID, definition.EggItemID, definition.HatchedItemID)
		}

		serial, err := allocateCreatureSerial(inventory, equipment, petRecord, slot, egg)
		if err != nil {
			return err
		}
		petKey := strconv.FormatUint(uint64(serial), 10)

		now := time.Now()
		extra := map[string]string{
			"creature_serial_or_handle": strconv.FormatUint(uint64(serial), 10),
			"creature_key":              petKey,
			"hatched_from_item_id":      strconv.FormatInt(egg.ItemID, 10),
		}
		if definition.EggPVFPath != "" {
			extra["hatch_egg_pvf_path"] = definition.EggPVFPath
		}
		if definition.HatchedPVFPath != "" {
			extra["creature_pvf_path"] = definition.HatchedPVFPath
		}
		inventory.Slots[slot] = dnfrepo.ItemStack{
			ItemID:   definition.HatchedItemID,
			Count:    1,
			Bind:     egg.Bind,
			ExpireAt: egg.ExpireAt,
			Extra:    extra,
		}
		inventory.UpdatedAt = now

		petRecord.Entries[petKey] = dnfrepo.PetEntry{
			PetKey:          petKey,
			CreatureKey:     serial,
			ItemID:          definition.HatchedItemID,
			SourceListType:  listTypePet,
			SourceSlotIndex: cmd.SlotIndex,
			Satiety:         100,
			SatietyMicros:   100 * petSatietyScale,
			ModeFlag:        0,
			Level:           1,
			Exp:             0,
			TailFlag:        0,
			Extra:           cloneExtra(extra),
		}
		petRecord.UpdatedAt = now

		entries, err := validatedCreatureEntries(petRecord)
		if err != nil {
			return err
		}
		if err := dnfrepo.SaveInventoryFields(ctx, inventoryRepo, inventory, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}
		if err := dnfrepo.SavePetFields(ctx, petRepo, petRecord, dnfrepo.PetFieldEntries); err != nil {
			return err
		}

		committed = HatchResult{
			CharacterID:     characterID,
			PetKey:          petKey,
			ItemID:          definition.HatchedItemID,
			SourceSlotIndex: cmd.SlotIndex,
			Changed:         true,
			PetInventory:    cloneItemMap(inventory.Slots),
			EntryCount:      len(entries),
			Entries:         entries,
		}
		return nil
	})
	if err != nil {
		return HatchResult{}, err
	}
	return committed, nil
}

func ensureInventoryRecord(record dnfrepo.InventoryRecord, characterID string) dnfrepo.InventoryRecord {
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if record.Warehouse == nil {
		record.Warehouse = make(map[string]dnfrepo.ItemStack)
	}
	return record
}

func ensureEquipmentRecord(record dnfrepo.EquipmentRecord, characterID string) dnfrepo.EquipmentRecord {
	record.CharacterID = characterID
	if record.Entries == nil {
		record.Entries = make(map[string]dnfrepo.EquipmentEntry)
	}
	return record
}

func ensurePetRecord(record dnfrepo.PetRecord, characterID string) dnfrepo.PetRecord {
	record.CharacterID = characterID
	if record.Entries == nil {
		record.Entries = make(map[string]dnfrepo.PetEntry)
	}
	return record
}

func validatedCreatureEntries(record dnfrepo.PetRecord) ([]dnfrepo.PetEntry, error) {
	entries := creatureEntries(record)
	for index := range entries {
		entry := &entries[index]
		if entry.CreatureKey == 0 {
			parsed, err := strconv.ParseUint(strings.TrimSpace(entry.PetKey), 10, 32)
			if err != nil || parsed == 0 || parsed > uint64(maxCreatureSerial) {
				return nil, fmt.Errorf("%w: invalid key %q", ErrPetStateInvalid, entry.PetKey)
			}
			entry.CreatureKey = uint32(parsed)
		}
		if entry.CreatureKey > maxCreatureSerial || entry.ItemID <= 0 {
			return nil, fmt.Errorf("%w: key=%d item=%d", ErrPetStateInvalid, entry.CreatureKey, entry.ItemID)
		}
		if entry.ModeFlag > 1 || entry.TailFlag > 1 || entry.Level < 1 || entry.Level > MaxCreatureLevel || entry.Exp < 0 || len(entry.NameRaw) >= 30 {
			return nil, fmt.Errorf("%w: key=%d mode=%d tail=%d level=%d exp=%d name_bytes=%d", ErrPetStateInvalid, entry.CreatureKey, entry.ModeFlag, entry.TailFlag, entry.Level, entry.Exp, len(entry.NameRaw))
		}
		if len(entry.NameRaw) == 0 && entry.Name != "" {
			nameRaw := []byte(entry.Name)
			if len(nameRaw) >= 30 {
				return nil, fmt.Errorf("%w: key=%d legacy_name_bytes=%d", ErrPetStateInvalid, entry.CreatureKey, len(nameRaw))
			}
			entry.NameRaw = nameRaw
		}
	}
	return entries, nil
}

func creatureEntries(record dnfrepo.PetRecord) []dnfrepo.PetEntry {
	if len(record.Entries) == 0 {
		return nil
	}
	entries := make([]dnfrepo.PetEntry, 0, len(record.Entries))
	for _, entry := range record.Entries {
		entry.NameRaw = append([]byte(nil), entry.NameRaw...)
		entry.RawEntry = append([]byte(nil), entry.RawEntry...)
		entry.Extra = cloneExtra(entry.Extra)
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		left, leftOK := petEntrySerial(entries[i])
		right, rightOK := petEntrySerial(entries[j])
		if leftOK != rightOK {
			return leftOK
		}
		if left != right {
			return left < right
		}
		return entries[i].PetKey < entries[j].PetKey
	})
	return entries
}

func allocateCreatureSerial(
	inventory dnfrepo.InventoryRecord,
	equipment dnfrepo.EquipmentRecord,
	petRecord dnfrepo.PetRecord,
	sourceSlot string,
	source dnfrepo.ItemStack,
) (uint32, error) {
	used := make(map[uint32]struct{}, len(inventory.Slots)+len(equipment.Entries)+len(petRecord.Entries))
	for slot, stack := range inventory.Slots {
		if slot == sourceSlot {
			continue
		}
		if serial, ok := stackCreatureSerial(stack.Extra); ok {
			used[serial] = struct{}{}
		}
	}
	for _, entry := range equipment.Entries {
		if serial, ok := stackCreatureSerial(entry.Extra); ok {
			used[serial] = struct{}{}
		}
	}
	for _, entry := range petRecord.Entries {
		if serial, ok := petEntrySerial(entry); ok {
			used[serial] = struct{}{}
		}
	}
	if preferred, ok := stackCreatureSerial(source.Extra); ok {
		if _, exists := used[preferred]; !exists {
			return preferred, nil
		}
	}
	for serial := uint32(1); serial != 0; serial++ {
		if _, exists := used[serial]; !exists {
			return serial, nil
		}
	}
	return 0, ErrPetSerialUnavailable
}

func stackCreatureSerial(extra map[string]string) (uint32, bool) {
	for _, key := range []string{"creature_serial_or_handle", "creature_serial", "pet_serial", "serial", "handle", "instance_value", "item_uid"} {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		value, err := strconv.ParseUint(raw, 0, 32)
		if err == nil && value > 0 && value <= uint64(maxCreatureSerial) {
			return uint32(value), true
		}
	}
	return 0, false
}

func petEntrySerial(entry dnfrepo.PetEntry) (uint32, bool) {
	if entry.CreatureKey > 0 && entry.CreatureKey <= maxCreatureSerial {
		return entry.CreatureKey, true
	}
	value, err := strconv.ParseUint(strings.TrimSpace(entry.PetKey), 10, 32)
	if err != nil || value == 0 || value > uint64(maxCreatureSerial) {
		return 0, false
	}
	return uint32(value), true
}

func slotKey(listType byte, slotIndex int16) string {
	return fmt.Sprintf("%d:%d", listType, slotIndex)
}

func cloneExtra(extra map[string]string) map[string]string {
	if len(extra) == 0 {
		return nil
	}
	out := make(map[string]string, len(extra))
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func cloneItemMap(in map[string]dnfrepo.ItemStack) map[string]dnfrepo.ItemStack {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]dnfrepo.ItemStack, len(in))
	for key, value := range in {
		value.RawEntry = append([]byte(nil), value.RawEntry...)
		value.Extra = cloneExtra(value.Extra)
		out[key] = value
	}
	return out
}
