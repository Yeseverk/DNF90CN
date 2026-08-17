package equip

import (
	"context"
	"fmt"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// syncPetArtifactEquipmentMove projects the already-persisted list-7 / actor
// target 27..29 mutation into PetRecord.Artifacts before the surrounding
// CharacterPetUnitOfWork commits. The actor EquipmentRecord remains the worn
// endpoint authority; PetRecord is its durable pet-status projection.
func (o *Owner) syncPetArtifactEquipmentMove(ctx context.Context, characterID string, result MoveResult) error {
	if o == nil || o.pets == nil {
		return ErrPetRepositoryRequired
	}
	target, ok := petArtifactTargetFromMoveResult(result)
	if !ok {
		return fmt.Errorf("%w: artifact result endpoints=(%d,%d)->(%d,%d)",
			ErrPetOwnershipMismatch,
			result.SourceListType,
			result.SourceSlotIndex,
			result.DestinationListType,
			result.DestinationSlotIndex,
		)
	}
	kind, ok := petArtifactProjectionKey(target)
	if !ok {
		return fmt.Errorf("%w: artifact target=%d", ErrPetOwnershipMismatch, target)
	}

	_, equipment, err := o.loadMoveRecords(ctx, characterID)
	if err != nil {
		return err
	}
	record, found, err := o.pets.Load(ctx, characterID)
	if err != nil {
		return err
	}
	if !found {
		record = dnfrepo.PetRecord{CharacterID: characterID}
	}
	record = dnfrepo.ClonePet(record)
	record.CharacterID = characterID
	if record.Artifacts == nil {
		record.Artifacts = make(map[string]dnfrepo.ItemStack)
	}

	equipped, equippedFound := equipment.Entries[entryKey(target)]
	if equippedFound {
		if equipped.SlotIndex != target || equipped.ItemID <= 0 || len(equipped.RawEntry) == 0 {
			return fmt.Errorf("%w: artifact target=%d entry=%+v", ErrPetOwnershipMismatch, target, equipped)
		}
		if result.Mode != "equip" && result.Mode != "equip_swap" && result.Mode != "unequip_swap" {
			return fmt.Errorf("%w: artifact target=%d mode=%s remains occupied", ErrPetOwnershipMismatch, target, result.Mode)
		}
		if equipped.ItemID != result.ItemID && equipped.ItemID != result.SwappedItemID {
			return fmt.Errorf("%w: artifact target=%d item=%d result=(%d,%d)", ErrPetOwnershipMismatch, target, equipped.ItemID, result.ItemID, result.SwappedItemID)
		}
		record.Artifacts[kind] = stackFromEntry(equipped)
	} else {
		if result.Mode != "unequip" {
			return fmt.Errorf("%w: artifact target=%d mode=%s is empty", ErrPetOwnershipMismatch, target, result.Mode)
		}
		delete(record.Artifacts, kind)
	}
	record.UpdatedAt = time.Now()
	return dnfrepo.SavePetFields(ctx, o.pets, record, dnfrepo.PetFieldEntries)
}

func petArtifactTargetFromMoveResult(result MoveResult) (int16, bool) {
	if result.SourceListType == wireListEquipment && isPetArtifactTarget(result.SourceSlotIndex) && result.DestinationListType == wireListPet {
		return result.SourceSlotIndex, true
	}
	if result.SourceListType == wireListPet && result.DestinationListType == wireListEquipment && isPetArtifactTarget(result.DestinationSlotIndex) {
		return result.DestinationSlotIndex, true
	}
	return 0, false
}

func petArtifactProjectionKey(target int16) (string, bool) {
	switch target {
	case 27:
		return "red", true
	case 28:
		return "blue", true
	case 29:
		return "green", true
	default:
		return "", false
	}
}
