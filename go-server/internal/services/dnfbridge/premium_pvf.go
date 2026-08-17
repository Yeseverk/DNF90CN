package dnfbridge

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

// currentPremiumContractInfo is one contract item's activation mapping from
// the runtime PVF premiumlist_new.etc: premium type plus duration in seconds.
type currentPremiumContractInfo struct {
	ItemID          int64
	PremiumType     int64
	DurationSeconds int64
}

// currentPremiumEffectInfo is the server-side effect block owned by one
// premiumlist_new.etc [type].
type currentPremiumEffectInfo struct {
	BonusExperiencePercent      int64
	QuestItemDropRatePercent    int64
	IndependentDropRatePercents []int64
}

func (info currentPremiumEffectInfo) independentDropRatePercent(partyMembers int) int64 {
	if partyMembers <= 0 || len(info.IndependentDropRatePercents) == 0 {
		return 0
	}
	index := partyMembers - 1
	if index >= len(info.IndependentDropRatePercents) {
		index = len(info.IndependentDropRatePercents) - 1
	}
	value := info.IndependentDropRatePercents[index]
	if value < 0 {
		return 0
	}
	return value
}

// currentPremiumDevilSlotInfo is one 魔王 selectable perk commodity from
// cerashop.etc [selectable character premium]: commodity id, perk slot,
// duration and Cera price.
type currentPremiumDevilSlotInfo struct {
	CommodityID uint32
	ItemID      int64
	Slot        int64
	Days        int64
	CeraPrice   int64
}

type currentPremiumCatalog struct {
	contractsByItem map[int64]currentPremiumContractInfo
	effectsByType   map[int64]currentPremiumEffectInfo
	devilSlots      map[uint32]currentPremiumDevilSlotInfo
	crystalCubeIDs  [6]int64
}

func (s *Service) currentPremiumCatalog() (*currentPremiumCatalog, error) {
	if s == nil {
		return nil, fmt.Errorf("premium catalog: service required")
	}
	s.premiumCatalogMu.Lock()
	defer s.premiumCatalogMu.Unlock()
	if s.premiumCatalog != nil || s.premiumCatalogLoadErr != nil {
		return s.premiumCatalog, s.premiumCatalogLoadErr
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err == nil {
		s.premiumCatalog, err = buildCurrentPremiumCatalog(archive)
	}
	s.premiumCatalogLoadErr = err
	return s.premiumCatalog, s.premiumCatalogLoadErr
}

func buildCurrentPremiumCatalog(archive *platformpvf.Archive) (*currentPremiumCatalog, error) {
	if archive == nil {
		return nil, fmt.Errorf("premium catalog: PVF archive required")
	}
	catalog := &currentPremiumCatalog{
		contractsByItem: make(map[int64]currentPremiumContractInfo),
		effectsByType:   make(map[int64]currentPremiumEffectInfo),
		devilSlots:      make(map[uint32]currentPremiumDevilSlotInfo),
	}
	text, err := archive.ReadText("etc/premiumlist_new.etc")
	if err != nil {
		return nil, fmt.Errorf("read etc/premiumlist_new.etc: %w", err)
	}
	document, err := dnfpvf.Parse("etc/premiumlist_new.etc", text)
	if err != nil {
		return nil, fmt.Errorf("parse etc/premiumlist_new.etc: %w", err)
	}
	parseCurrentPremiumListDocument(document, catalog)
	ceraText, err := archive.ReadText("etc/cerashop.etc")
	if err != nil {
		return nil, fmt.Errorf("read etc/cerashop.etc: %w", err)
	}
	ceraDocument, err := dnfpvf.Parse("etc/cerashop.etc", ceraText)
	if err != nil {
		return nil, fmt.Errorf("parse etc/cerashop.etc: %w", err)
	}
	parseCurrentPremiumDevilSlots(ceraDocument, catalog)
	stackableText, err := archive.ReadText("stackable/stackable.lst")
	if err != nil {
		return nil, fmt.Errorf("read stackable/stackable.lst for crystal contract: %w", err)
	}
	stackableDocument, err := dnfpvf.Parse("stackable/stackable.lst", stackableText)
	if err != nil {
		return nil, fmt.Errorf("parse stackable/stackable.lst for crystal contract: %w", err)
	}
	if err := parseCurrentCrystalContractCubes(stackableDocument, catalog); err != nil {
		return nil, err
	}
	return catalog, nil
}

// parseCurrentCrystalContractCubes resolves the six native selection indexes
// from the active runtime PVF. NoPack sub_1E7E8E0/sub_1E7F810 use indexes 0..5
// in black, white, red, blue, clear, gold order; stackable.lst owns the actual
// item IDs, so the server never trusts or hard-codes a request-side item ID.
func parseCurrentCrystalContractCubes(document *dnfpvf.Document, catalog *currentPremiumCatalog) error {
	if document == nil || catalog == nil {
		return fmt.Errorf("crystal contract catalog: stackable list and destination are required")
	}
	paths := [6]string{
		"material/cubepiece_black.stk",
		"material/cubepiece_white.stk",
		"material/cubepiece_red.stk",
		"material/cubepiece_blue.stk",
		"material/cubepiece_clear.stk",
		"material/cubepiece_gold.stk",
	}
	byPath := make(map[string]int64, len(paths))
	for _, entry := range dnfpvf.ParseList(document) {
		path := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(entry.Path, "\\", "/")))
		if path != "" && entry.ID > 0 {
			byPath[path] = entry.ID
		}
	}
	for index, path := range paths {
		itemID := byPath[path]
		if itemID <= 0 {
			return fmt.Errorf("crystal contract catalog: runtime PVF is missing stackable/%s", path)
		}
		catalog.crystalCubeIDs[index] = itemID
	}
	return nil
}

// alignedPremiumContractResolverForCommand keeps PVF loading request-driven.
// Only use-stackable can carry a premiumlist_new.etc contract item; the
// resolver maps item id to (premium type, duration) from the runtime-PVF
// [item]/[term] entries and reports zero for non-contract items.
func (s *Service) alignedPremiumContractResolverForCommand(opcode dnfenum.CmdPacket) (alignedcmd.PremiumContractResolver, error) {
	if opcode != dnfenum.CmdPacketUseStackable {
		return nil, nil
	}
	catalog, err := s.currentPremiumCatalog()
	if err != nil {
		return nil, err
	}
	return func(itemID int64) (alignedcmd.PremiumContractResolution, error) {
		info, ok := catalog.contractsByItem[itemID]
		if !ok {
			return alignedcmd.PremiumContractResolution{}, nil
		}
		return alignedcmd.PremiumContractResolution{
			ItemID:          info.ItemID,
			PremiumType:     info.PremiumType,
			DurationSeconds: info.DurationSeconds,
		}, nil
	}, nil
}

// parseCurrentPremiumListDocument walks [type] blocks. Each block owns
// [item]/[term] pairs (days, or minutes when the same item entry sets
// [is term unit minute]); the activation mapping is item -> (type, term).
func parseCurrentPremiumListDocument(document *dnfpvf.Document, catalog *currentPremiumCatalog) {
	if document == nil || catalog == nil {
		return
	}
	if catalog.contractsByItem == nil {
		catalog.contractsByItem = make(map[int64]currentPremiumContractInfo)
	}
	if catalog.effectsByType == nil {
		catalog.effectsByType = make(map[int64]currentPremiumEffectInfo)
	}
	type itemDraft struct {
		term       int64
		minuteUnit bool
	}
	var premiumType int64
	var itemID int64
	draft := itemDraft{}
	flushItem := func() {
		if premiumType <= 0 || itemID <= 0 || draft.term <= 0 {
			itemID = 0
			draft = itemDraft{}
			return
		}
		seconds := draft.term * 86400
		if draft.minuteUnit {
			seconds = draft.term * 60
		}
		catalog.contractsByItem[itemID] = currentPremiumContractInfo{
			ItemID:          itemID,
			PremiumType:     premiumType,
			DurationSeconds: seconds,
		}
		itemID = 0
		draft = itemDraft{}
	}
	pendingSection := ""
	for _, token := range document.Tokens {
		if token.Kind == dnfpvf.TokenSection {
			name := strings.ToLower(strings.TrimSpace(token.Value))
			switch name {
			case "type":
				premiumType = 0
			case "/type":
				flushItem()
				premiumType = 0
			case "item":
				flushItem()
			case "/item":
				flushItem()
			}
			pendingSection = name
			continue
		}
		if token.Kind != dnfpvf.TokenInt {
			continue
		}
		switch pendingSection {
		case "type":
			premiumType = token.Int
		case "item":
			itemID = token.Int
		case "term":
			draft.term = token.Int
		case "is term unit minute":
			draft.minuteUnit = token.Int != 0
		case "bonus exp":
			if premiumType > 0 && token.Int >= 0 {
				effect := catalog.effectsByType[premiumType]
				effect.BonusExperiencePercent = token.Int
				catalog.effectsByType[premiumType] = effect
			}
		case "quest item drop rate":
			if premiumType > 0 && token.Int >= 0 {
				effect := catalog.effectsByType[premiumType]
				effect.QuestItemDropRatePercent = token.Int
				catalog.effectsByType[premiumType] = effect
			}
		case "independent drop rate":
			if premiumType > 0 && token.Int >= 0 {
				effect := catalog.effectsByType[premiumType]
				effect.IndependentDropRatePercents = append(effect.IndependentDropRatePercents, token.Int)
				catalog.effectsByType[premiumType] = effect
			}
			continue
		}
		pendingSection = ""
	}
	flushItem()
}

// parseCurrentPremiumDevilSlots parses cerashop.etc [selectable character
// premium]: rows of 9 words (commodity, itemID, slot, days, unused, price,
// label, unused, unused). The leading slot=-1 row is the contract display
// name and is skipped.
func parseCurrentPremiumDevilSlots(document *dnfpvf.Document, catalog *currentPremiumCatalog) {
	tokens, found := document.Section("selectable character premium")
	if !found {
		return
	}
	values := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		switch token.Kind {
		case dnfpvf.TokenInt:
			values = append(values, token.Int)
		case dnfpvf.TokenString, dnfpvf.TokenIdent:
			values = append(values, -1)
		}
	}
	const width = 9
	for offset := 0; offset+width <= len(values); offset += width {
		commodity := values[offset]
		itemID := values[offset+1]
		slot := values[offset+2]
		days := values[offset+3]
		price := values[offset+5]
		if commodity <= 0 || itemID <= 0 || slot < 0 || slot >= currentPremiumDevilSlotCount {
			continue
		}
		catalog.devilSlots[uint32(commodity)] = currentPremiumDevilSlotInfo{
			CommodityID: uint32(commodity),
			ItemID:      itemID,
			Slot:        slot,
			Days:        days,
			CeraPrice:   price,
		}
	}
}
