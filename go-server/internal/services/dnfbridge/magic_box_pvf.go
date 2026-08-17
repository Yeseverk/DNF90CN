package dnfbridge

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// alignedMagicBoxResolversForCommand keeps PVF loading request-driven. Only
// the single-open magic-box command needs these dependencies; other aligned
// commands do not open or scan the item catalog. The resolvers mirror the
// 86JP StackableItemFile contract: [RANDOMBOX] [int data] groups (skip the
// leading triple, then (item, weight, count, ignored) quads), the
// [booster info] child-tag groups (optional leading DrawCount, then triples),
// and the [sealing removal item] / [need material] material requirement.
func (s *Service) alignedMagicBoxResolversForCommand(opcode dnfenum.CmdPacket) (alignedcmd.MagicBoxResolver, alignedcmd.MagicBoxRewardItemResolver, error) {
	if opcode != dnfenum.CmdPacketUseRandomboxItem && opcode != dnfenum.CmdPacketUseRandomboxItemExpand {
		return nil, nil, nil
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return nil, nil, err
	}
	if catalog == nil {
		return nil, nil, errDungeonDropSourceRequired
	}
	s.initialEquipmentMu.Lock()
	source, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	return func(boxItemID int64) (alignedcmd.MagicBoxResolution, error) {
			return resolveCurrentMagicBox(catalog, source, boxItemID)
		}, func(itemID int64) (alignedcmd.MagicBoxRewardItem, error) {
			return resolveCurrentMagicBoxRewardItem(catalog, source, itemID)
		}, nil
}

// resolveCurrentMagicBox resolves one box template through the active runtime
// PVF. Unresolvable or non-stackable templates and unsupported box families
// resolve with an empty Kind; only catalog/document read failures surface as
// errors so the open fails closed.
func resolveCurrentMagicBox(catalog *pvfDungeonDropCatalog, source dnfpvf.Source, boxItemID int64) (alignedcmd.MagicBoxResolution, error) {
	resolution := alignedcmd.MagicBoxResolution{}
	if catalog == nil || source == nil {
		return resolution, errDungeonDropSourceRequired
	}
	if boxItemID <= 0 || boxItemID > int64(^uint32(0)) {
		return resolution, nil
	}
	definition, err := catalog.ResolveItem(uint32(boxItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return resolution, nil
		}
		return alignedcmd.MagicBoxResolution{}, fmt.Errorf("resolve magic box item=%d: %w", boxItemID, err)
	}
	if definition.Kind != dungeonDropItemStackable {
		return resolution, nil
	}
	document, err := parseDungeonCardPVFDocument(source, definition.PVFPath)
	if err != nil {
		return alignedcmd.MagicBoxResolution{}, fmt.Errorf("parse magic box item=%d path=%s: %w", boxItemID, definition.PVFPath, err)
	}
	resolution.BoxPVFPath = definition.PVFPath

	switch normalizeMagicBoxPVFType(magicBoxDocumentText(document, "stackable type")) {
	case "random upgradable legacy":
		resolution.Groups = parseMagicBoxRandomGroups(document)
	case "booster", "cera booster", "booster random":
		resolution.Groups = parseMagicBoxBoosterGroups(document)
	default:
		return resolution, nil
	}
	if len(resolution.Groups) == 0 {
		return alignedcmd.MagicBoxResolution{}, nil
	}
	resolution.Kind = "random"
	resolution.MaterialItemID, resolution.MaterialCountPerUse = resolveMagicBoxNeedMaterial(document, boxItemID)
	return resolution, nil
}

// parseMagicBoxRandomGroups walks the [RANDOMBOX] child [int data] sections.
// Each section is one independent group with DrawCount 1; per the 86JP
// ParseRandomBoxRewards rule the leading triple is skipped and every later
// four ints form one (item, weight, count, ignored) entry. A section with no
// quad entries and at least two ints degrades to one default-weight entry.
func parseMagicBoxRandomGroups(document *dnfpvf.Document) []alignedcmd.MagicBoxRewardGroup {
	groups := make([]alignedcmd.MagicBoxRewardGroup, 0, 4)
	inRandombox := false
	for _, section := range document.Sections {
		name := normalizeMagicBoxSectionName(section.Name)
		switch name {
		case "randombox":
			inRandombox = true
			continue
		case "/randombox":
			inRandombox = false
		}
		if !inRandombox || name != "int data" {
			continue
		}
		ints := magicBoxSectionInts(document, section)
		group := alignedcmd.MagicBoxRewardGroup{DrawCount: 1}
		if len(ints) >= 7 {
			for index := 3; index+3 < len(ints); index += 4 {
				if ints[index] <= 0 {
					continue
				}
				group.Entries = append(group.Entries, alignedcmd.MagicBoxRewardEntry{
					ItemID: ints[index],
					Weight: maxMagicBoxInt(ints[index+1], 0),
					Count:  maxMagicBoxInt(ints[index+2], 1),
				})
			}
		}
		if len(group.Entries) == 0 && len(ints) >= 2 && ints[0] > 0 {
			group.Entries = append(group.Entries, alignedcmd.MagicBoxRewardEntry{
				ItemID: ints[0],
				Weight: 10000,
				Count:  maxMagicBoxInt(ints[1], 1),
			})
		}
		if len(group.Entries) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

// parseMagicBoxBoosterGroups walks the [booster info] child tags. Each direct
// child tag is one independent group; when its ints are (n-1)%3==0 shaped the
// leading int is the group's DrawCount, and the rest form
// (item, weight, count) triples (86JP ParseBoosterInfo/AddWeightedRewards).
func parseMagicBoxBoosterGroups(document *dnfpvf.Document) []alignedcmd.MagicBoxRewardGroup {
	groups := make([]alignedcmd.MagicBoxRewardGroup, 0, 4)
	inInfo := false
	for _, section := range document.Sections {
		name := normalizeMagicBoxSectionName(section.Name)
		switch name {
		case "booster info":
			inInfo = true
			continue
		case "/booster info":
			inInfo = false
		}
		if !inInfo || strings.HasPrefix(name, "/") {
			continue
		}
		ints := magicBoxSectionInts(document, section)
		group := alignedcmd.MagicBoxRewardGroup{DrawCount: 1}
		start := 0
		if len(ints) >= 4 && (len(ints)-1)%3 == 0 {
			group.DrawCount = maxMagicBoxInt(ints[0], 1)
			start = 1
		}
		for index := start; index+2 < len(ints); index += 3 {
			if ints[index] <= 0 {
				continue
			}
			group.Entries = append(group.Entries, alignedcmd.MagicBoxRewardEntry{
				ItemID: ints[index],
				Weight: maxMagicBoxInt(ints[index+1], 0),
				Count:  maxMagicBoxInt(ints[index+2], 1),
			})
		}
		if len(group.Entries) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

// resolveMagicBoxNeedMaterial reads the per-open material requirement. The
// [sealing removal item] section is preferred: when its ints are
// 1+header*2 shaped the leading int is an entry count and skipped, the rest
// form (item, count) pairs; the first pair with a positive count that is not
// the box itself wins. Otherwise the top-level [need material] "id count"
// field is used (86JP ResolveNeedMaterial).
func resolveMagicBoxNeedMaterial(document *dnfpvf.Document, boxItemID int64) (int64, int64) {
	ints := document.Ints("sealing removal item")
	if len(ints) > 0 && int64(len(ints)) == 1+ints[0]*2 {
		ints = ints[1:]
	}
	for index := 0; index+1 < len(ints); index += 2 {
		itemID, count := ints[index], ints[index+1]
		if itemID <= 0 || count <= 0 || itemID == boxItemID {
			continue
		}
		return itemID, count
	}
	raw, found := document.Text("need material")
	if !found {
		return 0, 0
	}
	var itemID, count int64
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d %d", &itemID, &count); err != nil || itemID <= 0 || count <= 0 {
		return 0, 0
	}
	return itemID, count
}

// resolveCurrentMagicBoxRewardItem resolves one reward template for the
// durable grant step: inventory kind, stack limit (0 = unlimited), PVF slot
// range, and the seal flag from its [attach type].
func resolveCurrentMagicBoxRewardItem(catalog *pvfDungeonDropCatalog, source dnfpvf.Source, itemID int64) (alignedcmd.MagicBoxRewardItem, error) {
	if catalog == nil || source == nil {
		return alignedcmd.MagicBoxRewardItem{}, errDungeonDropSourceRequired
	}
	if itemID <= 0 || itemID > int64(^uint32(0)) {
		return alignedcmd.MagicBoxRewardItem{}, nil
	}
	definition, err := catalog.ResolveItem(uint32(itemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return alignedcmd.MagicBoxRewardItem{}, nil
		}
		return alignedcmd.MagicBoxRewardItem{}, fmt.Errorf("resolve magic box reward item=%d: %w", itemID, err)
	}
	definition, err = currentPVFItemDefinitionForGrantAt(definition, time.Now().UTC())
	if err != nil {
		return alignedcmd.MagicBoxRewardItem{}, fmt.Errorf("resolve magic box reward item=%d expiration: %w", itemID, err)
	}
	item := alignedcmd.MagicBoxRewardItem{
		ItemID:           itemID,
		Kind:             string(definition.Kind),
		EquipmentType:    definition.EquipmentType,
		StackLimit:       definition.StackLimit,
		SlotStart:        definition.SlotStart,
		SlotEnd:          definition.SlotEnd,
		PVFPath:          definition.PVFPath,
		ExpireAt:         definition.ExpirationDate,
		UsablePeriodDays: definition.UsablePeriodDays,
	}
	if isCurrentCeraShopCreatureItem(definition) {
		item.TargetListType = currentPetInventoryListType
		item.SlotStart = 0
		item.SlotEnd = 139
		item.StackLimit = 1
	} else if isCurrentCeraShopPetConsumable(definition) {
		item.TargetListType = currentPetInventoryListType
		item.SlotStart = currentCeraShopPetConsumableSlotStart
		item.SlotEnd = currentCeraShopPetConsumableSlotEnd
	}
	document, err := parseDungeonCardPVFDocument(source, definition.PVFPath)
	if err != nil {
		return alignedcmd.MagicBoxRewardItem{}, fmt.Errorf("parse magic box reward item=%d path=%s: %w", itemID, definition.PVFPath, err)
	}
	attachType := normalizeMagicBoxPVFType(magicBoxDocumentText(document, "attach type"))
	item.Seal = attachType == "sealing"
	if durability, found := document.Int("durability"); found && durability >= 0 && durability <= 65535 {
		item.Durability = uint16(durability)
	}
	return item, nil
}

func magicBoxDocumentText(document *dnfpvf.Document, name string) string {
	values := document.Texts(name)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func magicBoxSectionInts(document *dnfpvf.Document, section dnfpvf.Section) []int64 {
	if section.Start < 0 || section.End > len(document.Tokens) || section.Start > section.End {
		return nil
	}
	out := make([]int64, 0, section.End-section.Start)
	for _, token := range document.Tokens[section.Start:section.End] {
		if token.Kind == dnfpvf.TokenInt {
			out = append(out, token.Int)
		}
	}
	return out
}

func normalizeMagicBoxSectionName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeMagicBoxPVFType(value string) string {
	return strings.ToLower(strings.Trim(value, "` []"))
}

func maxMagicBoxInt(value int64, floor int64) int64 {
	if value < floor {
		return floor
	}
	return value
}
