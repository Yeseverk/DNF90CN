package pet

import (
	"errors"
	"fmt"
)

var (
	ErrPetPVFArtifactTypeInvalid   = errors.New("pet artifact equipment type is invalid")
	ErrPetPVFArtifactTypeAmbiguous = errors.New("pet artifact equipment type is ambiguous")
)

// PetArtifactKind is the three-way artifact identity proven by the equipment
// document's [equipment type]. The zero value is deliberately invalid.
type PetArtifactKind uint8

const (
	PetArtifactKindInvalid PetArtifactKind = iota
	PetArtifactKindRed
	PetArtifactKindBlue
	PetArtifactKindGreen
)

func (kind PetArtifactKind) String() string {
	switch kind {
	case PetArtifactKindRed:
		return "red"
	case PetArtifactKindBlue:
		return "blue"
	case PetArtifactKindGreen:
		return "green"
	default:
		return "invalid"
	}
}

// PetArtifactDefinition contains only typed PVF domain data. Current-EXE worn
// slot semantics are intentionally absent: ordinary equipment traffic also
// uses values that old 86JP assigned to artifact slots, so those old numbers
// cannot be promoted to current-client facts.
type PetArtifactDefinition struct {
	ItemID        int64
	PVFPath       string
	EquipmentType string
	Kind          PetArtifactKind
}

// PetArtifactResolver lets a mutation owner obtain artifact identity without
// importing PVF/parser packages.
type PetArtifactResolver interface {
	ResolveArtifact(itemID int64) (PetArtifactDefinition, error)
}

// ResolveArtifact resolves an equipment.lst item and requires exactly one
// [equipment type] value. Only the current runtime PVF artifact tokens are
// accepted; path names and request slots are never used to infer the kind.
func (c *PVFCatalog) ResolveArtifact(itemID int64) (PetArtifactDefinition, error) {
	if c == nil || c.source == nil {
		return PetArtifactDefinition{}, ErrPetPVFSourceRequired
	}
	documentPath, document, err := c.resolveEquipmentDocument(itemID)
	if err != nil {
		return PetArtifactDefinition{}, err
	}
	types := document.Texts("equipment type")
	if len(types) == 0 {
		return PetArtifactDefinition{}, fmt.Errorf("%w: item_id=%d path=%s missing equipment type", ErrPetPVFArtifactTypeInvalid, itemID, documentPath)
	}
	if len(types) != 1 {
		return PetArtifactDefinition{}, fmt.Errorf("%w: item_id=%d path=%s values=%q", ErrPetPVFArtifactTypeAmbiguous, itemID, documentPath, types)
	}
	rawType := types[0]
	kind, ok := petArtifactType(normalizePetPVFType(rawType))
	if !ok {
		return PetArtifactDefinition{}, fmt.Errorf("%w: item_id=%d path=%s equipment_type=%q", ErrPetPVFArtifactTypeInvalid, itemID, documentPath, rawType)
	}
	return PetArtifactDefinition{
		ItemID:        itemID,
		PVFPath:       documentPath,
		EquipmentType: normalizePetPVFType(rawType),
		Kind:          kind,
	}, nil
}

func petArtifactType(equipmentType string) (PetArtifactKind, bool) {
	switch equipmentType {
	case "[artifact red]":
		return PetArtifactKindRed, true
	case "[artifact blue]":
		return PetArtifactKindBlue, true
	case "[artifact green]":
		return PetArtifactKindGreen, true
	default:
		return PetArtifactKindInvalid, false
	}
}

var _ PetArtifactResolver = (*PVFCatalog)(nil)
