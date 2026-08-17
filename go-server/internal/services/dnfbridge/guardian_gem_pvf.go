package dnfbridge

import (
	"errors"
	"fmt"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

type currentGuardianGemDefinition struct {
	ItemID        uint32
	PVFPath       string
	Grade         byte
	EnchantFamily string
}

// currentGuardianGemMedalDefinition retains just the server-verified medal
// identity needed by the socket owner.
type currentGuardianGemMedalDefinition struct {
	ItemID  uint32
	PVFPath string
}

// currentGuardianGemUseSnapshot is deliberately observational. The current
// C2S request carries the source slot but no source list because sub_E9C340
// resolves the selected gem through list 38 before writing it.

func resolveCurrentGuardianGem(catalog *pvfDungeonDropCatalog, itemID uint32) (currentGuardianGemDefinition, error) {
	if catalog == nil || catalog.source == nil {
		return currentGuardianGemDefinition{}, errCurrentGuardianGemCatalogRequired
	}
	definition, err := catalog.ResolveItem(itemID)
	if err != nil {
		return currentGuardianGemDefinition{}, err
	}
	if definition.Kind != dungeonDropItemStackable || !currentGuardianGemStackableType(definition.StackableType) {
		return currentGuardianGemDefinition{}, fmt.Errorf(
			"%w: item=%d kind=%s stackable_type=%q",
			errCurrentGuardianGemNotFlagGem,
			itemID,
			definition.Kind,
			definition.StackableType,
		)
	}

	docPath, text, err := readDungeonDropText(catalog.source, "stackable", definition.PVFPath)
	if err != nil {
		return currentGuardianGemDefinition{}, fmt.Errorf("read guardian gem item=%d path=%q: %w", itemID, definition.PVFPath, err)
	}
	document, err := dnfpvf.Parse(docPath, text)
	if err != nil {
		return currentGuardianGemDefinition{}, fmt.Errorf("parse guardian gem item=%d path=%q: %w", itemID, docPath, err)
	}
	grade, ok := document.Int("grade")
	if !ok || grade < 0 || grade > 3 {
		return currentGuardianGemDefinition{}, fmt.Errorf("%w: item=%d path=%q grade=%d present=%t", errCurrentGuardianGemGradeInvalid, itemID, docPath, grade, ok)
	}
	family, err := currentGuardianGemEnchantFamily(document)
	if err != nil {
		return currentGuardianGemDefinition{}, fmt.Errorf("%w: item=%d path=%q: %v", errCurrentGuardianGemEnchantAmbiguous, itemID, docPath, err)
	}
	return currentGuardianGemDefinition{
		ItemID:        itemID,
		PVFPath:       docPath,
		Grade:         byte(grade),
		EnchantFamily: family,
	}, nil
}

func resolveCurrentGuardianGemMedal(catalog *pvfDungeonDropCatalog, itemID uint32) (currentGuardianGemMedalDefinition, error) {
	if catalog == nil || catalog.source == nil {
		return currentGuardianGemMedalDefinition{}, errCurrentGuardianGemCatalogRequired
	}
	definition, err := catalog.ResolveItem(itemID)
	if err != nil {
		return currentGuardianGemMedalDefinition{}, err
	}
	if definition.Kind != dungeonDropItemEquipment {
		return currentGuardianGemMedalDefinition{}, fmt.Errorf(
			"%w: item=%d kind=%s",
			errCurrentGuardianGemTargetNotMedal,
			itemID,
			definition.Kind,
		)
	}
	docPath, text, err := readDungeonDropText(catalog.source, "equipment", definition.PVFPath)
	if err != nil {
		return currentGuardianGemMedalDefinition{}, fmt.Errorf("read guardian medal item=%d path=%q: %w", itemID, definition.PVFPath, err)
	}
	document, err := dnfpvf.Parse(docPath, text)
	if err != nil {
		return currentGuardianGemMedalDefinition{}, fmt.Errorf("parse guardian medal item=%d path=%q: %w", itemID, docPath, err)
	}
	pvfType, ok := document.Text("equipment type")
	if !ok || normalizeEquipmentPlacementPVFType(pvfType) != "[flag]" {
		return currentGuardianGemMedalDefinition{}, fmt.Errorf(
			"%w: item=%d path=%q type=%q present=%t",
			errCurrentGuardianGemTargetNotMedal,
			itemID,
			docPath,
			pvfType,
			ok,
		)
	}
	return currentGuardianGemMedalDefinition{ItemID: itemID, PVFPath: docPath}, nil
}

func currentGuardianGemStackableType(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "[flag gem]")
}

func currentGuardianGemEnchantFamily(document *dnfpvf.Document) (string, error) {
	if document == nil {
		return "", errors.New("PVF document is nil")
	}
	var family string
	insideEnchant := false
	for _, section := range document.Sections {
		name := strings.ToLower(strings.TrimSpace(section.Name))
		switch name {
		case "enchant":
			insideEnchant = true
			continue
		case "/enchant":
			insideEnchant = false
			continue
		}
		if !insideEnchant {
			continue
		}
		if len(document.Numbers(section.Name)) == 0 {
			continue
		}
		if family != "" {
			return "", fmt.Errorf("multiple attribute sections %q and %q", family, name)
		}
		family = name
	}
	if family == "" {
		return "", errors.New("no numeric child section inside [enchant]")
	}
	return family, nil
}
