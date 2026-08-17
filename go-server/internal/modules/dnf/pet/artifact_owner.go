package pet

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	petArtifactInventorySlotStart int16 = 140
	petArtifactInventorySlotEnd   int16 = 188
)

var (
	ErrPetArtifactOwnerUnavailable = errors.New("pet artifact owner unavailable")
	ErrPetArtifactStackInvalid     = errors.New("pet artifact stack is invalid")
	ErrPetArtifactStateInvalid     = errors.New("equipped pet artifact state is invalid")
	ErrPetArtifactNotEquipped      = errors.New("pet artifact is not equipped")
	ErrPetArtifactTargetOccupied   = errors.New("pet artifact target slot is occupied")
)

// ArtifactEquipCommand names only the authoritative list-7 inventory source.
// The artifact color is resolved from runtime PVF, never from a client slot.
type ArtifactEquipCommand struct {
	SelectedCharacterID uint16
	ListType            byte
	SlotIndex           int16
}

// ArtifactUnequipCommand names the semantic color and an empty list-7 target.
// A later protocol adapter may construct it only after the current EXE endpoint
// has been captured; the domain itself has no historical worn-slot numbers.
type ArtifactUnequipCommand struct {
	SelectedCharacterID uint16
	ListType            byte
	SlotIndex           int16
	Kind                PetArtifactKind
}

type ArtifactMoveResult struct {
	CharacterID  string
	Kind         PetArtifactKind
	ItemID       int64
	SwappedItem  int64
	SlotIndex    int16
	Changed      bool
	PetInventory map[string]dnfrepo.ItemStack
	Artifacts    map[string]dnfrepo.ItemStack
}

// ArtifactOwner owns the 86JP-equivalent artifact rules (one item per typed
// color, atomic replace/unequip) while leaving current-EXE wire endpoints to a
// separately proved protocol adapter.
type ArtifactOwner struct {
	characterPets dnfrepo.CharacterPetUnitOfWork
	resolver      PetArtifactResolver
}

func NewArtifactOwner(repos dnfrepo.Group, resolver PetArtifactResolver) (*ArtifactOwner, error) {
	if repos.CharacterPets == nil || resolver == nil {
		return nil, ErrPetArtifactOwnerUnavailable
	}
	return &ArtifactOwner{characterPets: repos.CharacterPets, resolver: resolver}, nil
}

func (o *ArtifactOwner) Equip(ctx context.Context, cmd ArtifactEquipCommand) (ArtifactMoveResult, error) {
	if o == nil || o.characterPets == nil || o.resolver == nil {
		return ArtifactMoveResult{}, ErrPetArtifactOwnerUnavailable
	}
	if err := validateArtifactInventoryEndpoint(cmd.SelectedCharacterID, cmd.ListType, cmd.SlotIndex); err != nil {
		return ArtifactMoveResult{}, err
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	var committed ArtifactMoveResult
	err := o.characterPets.WithinCharacterPets(ctx, characterID, func(
		inventoryRepo dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
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

		petRecord, ok, err := petRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !ok {
			petRecord = dnfrepo.PetRecord{CharacterID: characterID}
		}
		petRecord = ensurePetRecord(dnfrepo.ClonePet(petRecord), characterID)
		if petRecord.Artifacts == nil {
			petRecord.Artifacts = make(map[string]dnfrepo.ItemStack)
		}
		if err := validateEquippedArtifacts(petRecord.Artifacts, o.resolver); err != nil {
			return err
		}

		sourceKey := slotKey(listTypePet, cmd.SlotIndex)
		source, found := inventory.Slots[sourceKey]
		if !found {
			return fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, listTypePet, cmd.SlotIndex)
		}
		if source.ItemID <= 0 || source.Count != 1 {
			return fmt.Errorf("%w: item=%d count=%d", ErrPetArtifactStackInvalid, source.ItemID, source.Count)
		}
		definition, err := o.resolver.ResolveArtifact(source.ItemID)
		if err != nil {
			return err
		}
		kindKey, err := artifactKindKey(definition.Kind)
		if err != nil {
			return err
		}

		delete(inventory.Slots, sourceKey)
		swapped := petRecord.Artifacts[kindKey]
		if swapped.ItemID != 0 {
			inventory.Slots[sourceKey] = cloneArtifactStack(swapped)
		}
		petRecord.Artifacts[kindKey] = cloneArtifactStack(source)

		if err := dnfrepo.SaveInventoryFields(ctx, inventoryRepo, inventory, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}
		if err := dnfrepo.SavePetFields(ctx, petRepo, petRecord, dnfrepo.PetFieldEntries); err != nil {
			return err
		}
		committed = ArtifactMoveResult{
			CharacterID:  characterID,
			Kind:         definition.Kind,
			ItemID:       source.ItemID,
			SwappedItem:  swapped.ItemID,
			SlotIndex:    cmd.SlotIndex,
			Changed:      true,
			PetInventory: cloneItemMap(inventory.Slots),
			Artifacts:    cloneItemMap(petRecord.Artifacts),
		}
		return nil
	})
	if err != nil {
		return ArtifactMoveResult{}, err
	}
	return committed, nil
}

func (o *ArtifactOwner) Unequip(ctx context.Context, cmd ArtifactUnequipCommand) (ArtifactMoveResult, error) {
	if o == nil || o.characterPets == nil || o.resolver == nil {
		return ArtifactMoveResult{}, ErrPetArtifactOwnerUnavailable
	}
	if err := validateArtifactInventoryEndpoint(cmd.SelectedCharacterID, cmd.ListType, cmd.SlotIndex); err != nil {
		return ArtifactMoveResult{}, err
	}
	kindKey, err := artifactKindKey(cmd.Kind)
	if err != nil {
		return ArtifactMoveResult{}, err
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	var committed ArtifactMoveResult
	err = o.characterPets.WithinCharacterPets(ctx, characterID, func(
		inventoryRepo dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
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

		petRecord, ok, err := petRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrPetArtifactNotEquipped
		}
		petRecord = ensurePetRecord(dnfrepo.ClonePet(petRecord), characterID)
		if err := validateEquippedArtifacts(petRecord.Artifacts, o.resolver); err != nil {
			return err
		}

		equipped, found := petRecord.Artifacts[kindKey]
		if !found {
			return fmt.Errorf("%w: kind=%s", ErrPetArtifactNotEquipped, kindKey)
		}
		targetKey := slotKey(listTypePet, cmd.SlotIndex)
		if _, occupied := inventory.Slots[targetKey]; occupied {
			return fmt.Errorf("%w: list=%d slot=%d", ErrPetArtifactTargetOccupied, listTypePet, cmd.SlotIndex)
		}

		inventory.Slots[targetKey] = cloneArtifactStack(equipped)
		delete(petRecord.Artifacts, kindKey)
		if err := dnfrepo.SaveInventoryFields(ctx, inventoryRepo, inventory, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}
		if err := dnfrepo.SavePetFields(ctx, petRepo, petRecord, dnfrepo.PetFieldEntries); err != nil {
			return err
		}
		committed = ArtifactMoveResult{
			CharacterID:  characterID,
			Kind:         cmd.Kind,
			ItemID:       equipped.ItemID,
			SlotIndex:    cmd.SlotIndex,
			Changed:      true,
			PetInventory: cloneItemMap(inventory.Slots),
			Artifacts:    cloneItemMap(petRecord.Artifacts),
		}
		return nil
	})
	if err != nil {
		return ArtifactMoveResult{}, err
	}
	return committed, nil
}

func EquippedArtifactItemIDs(record dnfrepo.PetRecord) ([]int64, error) {
	ids := make([]int64, 0, 3)
	for _, kind := range []PetArtifactKind{PetArtifactKindRed, PetArtifactKindBlue, PetArtifactKindGreen} {
		key, _ := artifactKindKey(kind)
		if stack, ok := record.Artifacts[key]; ok {
			if stack.ItemID <= 0 || stack.Count != 1 {
				return nil, fmt.Errorf("%w: kind=%s item=%d count=%d", ErrPetArtifactStateInvalid, key, stack.ItemID, stack.Count)
			}
			ids = append(ids, stack.ItemID)
		}
	}
	return ids, nil
}

func validateArtifactInventoryEndpoint(characterID uint16, listType byte, slotIndex int16) error {
	if characterID == 0 {
		return ErrCharacterRequired
	}
	if listType != listTypePet {
		return fmt.Errorf("%w: %d", ErrUnsupportedList, listType)
	}
	if slotIndex < petArtifactInventorySlotStart || slotIndex > petArtifactInventorySlotEnd {
		return fmt.Errorf("%w: artifact list=%d slot=%d range=%d..%d", ErrSlotNotFound, listType, slotIndex, petArtifactInventorySlotStart, petArtifactInventorySlotEnd)
	}
	return nil
}

func validateEquippedArtifacts(artifacts map[string]dnfrepo.ItemStack, resolver PetArtifactResolver) error {
	if len(artifacts) == 0 {
		return nil
	}
	if resolver == nil {
		return ErrPetArtifactOwnerUnavailable
	}
	for key, stack := range artifacts {
		kind, ok := artifactKindFromKey(key)
		if !ok || stack.ItemID <= 0 || stack.Count != 1 {
			return fmt.Errorf("%w: key=%q item=%d count=%d", ErrPetArtifactStateInvalid, key, stack.ItemID, stack.Count)
		}
		definition, err := resolver.ResolveArtifact(stack.ItemID)
		if err != nil {
			return fmt.Errorf("%w: key=%s item=%d: %v", ErrPetArtifactStateInvalid, key, stack.ItemID, err)
		}
		if definition.Kind != kind {
			return fmt.Errorf("%w: key=%s item=%d resolves=%s", ErrPetArtifactStateInvalid, key, stack.ItemID, definition.Kind)
		}
	}
	return nil
}

func artifactKindKey(kind PetArtifactKind) (string, error) {
	if kind < PetArtifactKindRed || kind > PetArtifactKindGreen {
		return "", fmt.Errorf("%w: kind=%d", ErrPetArtifactStateInvalid, kind)
	}
	return kind.String(), nil
}

func artifactKindFromKey(key string) (PetArtifactKind, bool) {
	switch key {
	case PetArtifactKindRed.String():
		return PetArtifactKindRed, true
	case PetArtifactKindBlue.String():
		return PetArtifactKindBlue, true
	case PetArtifactKindGreen.String():
		return PetArtifactKindGreen, true
	default:
		return PetArtifactKindInvalid, false
	}
}

func cloneArtifactStack(stack dnfrepo.ItemStack) dnfrepo.ItemStack {
	stack.RawEntry = append([]byte(nil), stack.RawEntry...)
	stack.Extra = cloneExtra(stack.Extra)
	return stack
}
