package dnfbridge

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func currentSocketEquipmentRule(catalog *pvfDungeonDropCatalog, itemID int64) (currentEquipmentPlacementRule, error) {
	if catalog == nil || itemID <= 0 || itemID > math.MaxUint32 {
		return currentEquipmentPlacementRule{}, errCurrentSocketPVFInvalid
	}
	definition, err := catalog.ResolveItem(uint32(itemID))
	if err != nil {
		return currentEquipmentPlacementRule{}, err
	}
	if definition.Kind != dungeonDropItemEquipment {
		return currentEquipmentPlacementRule{}, fmt.Errorf("%w: item=%d kind=%s", errCurrentSocketTargetKindMismatch, itemID, definition.Kind)
	}
	document, err := parseDungeonCardPVFDocument(catalog.source, definition.PVFPath)
	if err != nil {
		return currentEquipmentPlacementRule{}, fmt.Errorf("%w: item=%d path=%s: %v", errCurrentSocketPVFInvalid, itemID, definition.PVFPath, err)
	}
	pvfType, ok := document.Text("equipment type")
	if !ok {
		return currentEquipmentPlacementRule{}, fmt.Errorf("%w: item=%d path=%s missing equipment type", errCurrentSocketPVFInvalid, itemID, definition.PVFPath)
	}
	rule, ok := currentEquipmentPlacementRuleForPVFType(pvfType)
	if !ok {
		return currentEquipmentPlacementRule{}, fmt.Errorf("%w: item=%d type=%q", errCurrentSocketPVFInvalid, itemID, pvfType)
	}
	return rule, nil
}

func currentEquipmentSocketOpenMaterialRule(catalog *pvfDungeonDropCatalog, itemID int64) error {
	if catalog == nil || itemID <= 0 || itemID > math.MaxUint32 {
		return errCurrentSocketMaterialInvalid
	}
	definition, err := catalog.ResolveItem(uint32(itemID))
	if err != nil {
		return fmt.Errorf("%w: item=%d: %v", errCurrentSocketMaterialInvalid, itemID, err)
	}
	if definition.Kind != dungeonDropItemStackable || !strings.EqualFold(strings.TrimSpace(definition.PVFPath), currentEquipmentSocketOpenToolPVFPath) {
		return fmt.Errorf("%w: item=%d kind=%s path=%s", errCurrentSocketMaterialInvalid, itemID, definition.Kind, definition.PVFPath)
	}
	return nil
}

func currentSocketAvatarSocketTypes(catalog *pvfDungeonDropCatalog, itemID int64) ([]byte, error) {
	if catalog == nil || itemID <= 0 || itemID > math.MaxUint32 {
		return nil, errCurrentSocketPVFInvalid
	}
	definition, err := catalog.ResolveItem(uint32(itemID))
	if err != nil {
		return nil, err
	}
	if definition.Kind != dungeonDropItemEquipment {
		return nil, fmt.Errorf("%w: avatar item=%d kind=%s", errCurrentSocketTargetKindMismatch, itemID, definition.Kind)
	}
	text, err := catalog.source.ReadText(definition.PVFPath)
	if err != nil {
		return nil, fmt.Errorf("%w: item=%d path=%s: %v", errCurrentSocketPVFInvalid, itemID, definition.PVFPath, err)
	}
	socketTypes := currentParseAvatarSocketTypes(text)
	if len(socketTypes) == 0 {
		return nil, nil
	}
	return socketTypes, nil
}

func currentSocketEmblemType(catalog *pvfDungeonDropCatalog, itemID int64) byte {
	if catalog == nil || itemID <= 0 || itemID > math.MaxUint32 {
		return 0
	}
	definition, err := catalog.ResolveItem(uint32(itemID))
	if err != nil || definition.Kind != dungeonDropItemStackable {
		return 0
	}
	document, err := parseDungeonCardPVFDocument(catalog.source, definition.PVFPath)
	if err != nil {
		return 0
	}
	return currentMapAvatarEmblemTargetTypes(document.Texts("avatar emblem target type"))
}

func currentParseAvatarSocketTypes(text string) []byte {
	socketTypes := currentParseAvatarSocketTypesFromSection(currentExtractPVFTextSection(text, "[avatar type select]", "[/avatar type select]"), currentAvatarSocketCount)
	if len(socketTypes) > 0 {
		return socketTypes
	}
	return currentParseAvatarDefaultSocketTypes(text)
}

// currentParseAvatarDefaultSocketTypes resolves only the PVF-declared sockets
// that an avatar owns immediately when it is created.  [avatar type select]
// describes the types available to the manual socket-opening flow and must not
// make an ordinary avatar arrive pre-opened.
func currentParseAvatarDefaultSocketTypes(text string) []byte {
	return currentParseAvatarSocketTypesFromSection(
		currentExtractPVFTextSection(text, "[emblem socket default]", "[/emblem socket default]"),
		currentResolveAvatarSocketNum(text),
	)
}

func currentParseAvatarSocketTypesFromSection(section string, maxCount int) []byte {
	if strings.TrimSpace(section) == "" || maxCount <= 0 {
		return nil
	}
	if maxCount > currentAvatarSocketCount {
		maxCount = currentAvatarSocketCount
	}
	out := make([]byte, 0, maxCount)
	for _, match := range currentAvatarSocketRE.FindAllStringSubmatch(section, -1) {
		if len(match) < 2 || match[1] == "" {
			continue
		}
		if socketType, ok := currentMapAvatarSocketCode(rune(match[1][0])); ok {
			out = append(out, socketType)
			if len(out) >= maxCount {
				break
			}
		}
	}
	return out
}

func currentExtractPVFTextSection(text, startTag, endTag string) string {
	lower := strings.ToLower(text)
	start := strings.Index(lower, strings.ToLower(startTag))
	if start < 0 {
		return ""
	}
	start += len(startTag)
	end := strings.Index(lower[start:], strings.ToLower(endTag))
	if end < 0 {
		return text[start:]
	}
	return text[start : start+end]
}

func currentResolveAvatarSocketNum(text string) int {
	lower := strings.ToLower(text)
	tag := "[avatar emblem socket num]"
	start := strings.Index(lower, tag)
	if start < 0 {
		return currentAvatarSocketCount
	}
	start += len(tag)
	end := strings.Index(text[start:], "[")
	section := text[start:]
	if end >= 0 {
		section = text[start : start+end]
	}
	for _, token := range strings.Fields(section) {
		value, err := strconv.Atoi(token)
		if err != nil {
			continue
		}
		if value < 0 {
			return 0
		}
		if value > currentAvatarSocketCount {
			return currentAvatarSocketCount
		}
		return value
	}
	return currentAvatarSocketCount
}

func currentMapAvatarEmblemTargetTypes(values []string) byte {
	var out byte
	for _, value := range values {
		for _, match := range currentAvatarSocketRE.FindAllStringSubmatch(value, -1) {
			if len(match) < 2 || match[1] == "" {
				continue
			}
			if socketType, ok := currentMapAvatarSocketCode(rune(match[1][0])); ok {
				out |= socketType
			}
		}
	}
	return out
}

func currentMapAvatarSocketCode(code rune) (byte, bool) {
	switch code {
	case 'A', 'a':
		return 0x01, true
	case 'B', 'b':
		return 0x02, true
	case 'C', 'c':
		return 0x04, true
	case 'D', 'd':
		return 0x08, true
	case 'S', 's':
		return 0x10, true
	case 'M', 'm':
		return 0xEF, true
	default:
		return 0, false
	}
}
