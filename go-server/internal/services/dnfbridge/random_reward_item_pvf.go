package dnfbridge

import (
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// alignedRandomRewardItemResolverForCommand keeps the ordinary op44 route
// PVF-backed. These consumables use [chn random image percent], not the
// random-box result-window protocol.
func (s *Service) alignedRandomRewardItemResolverForCommand(opcode dnfenum.CmdPacket) (alignedcmd.RandomRewardItemResolver, error) {
	if opcode != dnfenum.CmdPacketUseStackable {
		return nil, nil
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, errDungeonDropSourceRequired
	}
	s.initialEquipmentMu.Lock()
	source, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, err
	}
	return func(itemID int64) (alignedcmd.RandomRewardItemResolution, error) {
		return resolveCurrentRandomRewardItem(catalog, source, itemID)
	}, nil
}

// resolveCurrentRandomRewardItem reads the exact two outcome kinds encoded by
// [chn random image percent]: a non-positive item ID is a visual-only result;
// a positive item ID is one granted template. The item's own consumption is
// handled by the shared op44 inventory transaction.
func resolveCurrentRandomRewardItem(catalog *pvfDungeonDropCatalog, source dnfpvf.Source, itemID int64) (alignedcmd.RandomRewardItemResolution, error) {
	resolution := alignedcmd.RandomRewardItemResolution{}
	if catalog == nil || source == nil {
		return resolution, errDungeonDropSourceRequired
	}
	if itemID <= 0 || itemID > int64(^uint32(0)) {
		return resolution, nil
	}
	definition, err := catalog.ResolveItem(uint32(itemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return resolution, nil
		}
		return resolution, fmt.Errorf("resolve random reward item=%d: %w", itemID, err)
	}
	if definition.Kind != dungeonDropItemStackable {
		return resolution, nil
	}
	document, err := parseDungeonCardPVFDocument(source, definition.PVFPath)
	if err != nil {
		return resolution, fmt.Errorf("parse random reward item=%d path=%s: %w", itemID, definition.PVFPath, err)
	}
	stackableType := normalizeMagicBoxPVFType(magicBoxDocumentText(document, "stackable type"))
	if stackableType != "random reward item" {
		return resolution, nil
	}
	tokens, found := document.Section("chn random image percent")
	if !found || len(tokens) == 0 {
		return resolution, fmt.Errorf("random reward item=%d has no [chn random image percent] outcomes", itemID)
	}
	resolution = alignedcmd.RandomRewardItemResolution{
		SourceItemID:  itemID,
		SourcePVFPath: definition.PVFPath,
		StackableType: stackableType,
		Outcomes:      make([]alignedcmd.RandomRewardItemOutcome, 0, 4),
	}
	for index := 0; index < len(tokens); {
		if index+2 >= len(tokens) || tokens[index].Kind != dnfpvf.TokenInt || tokens[index+1].Kind != dnfpvf.TokenInt || tokens[index+2].Kind != dnfpvf.TokenInt {
			return alignedcmd.RandomRewardItemResolution{}, fmt.Errorf("random reward item=%d malformed outcome at token=%d", itemID, index)
		}
		weight := tokens[index+1].Int
		rewardID := tokens[index+2].Int
		index += 3
		stringCount := 0
		for index < len(tokens) && tokens[index].Kind == dnfpvf.TokenString {
			index++
			stringCount++
		}
		if stringCount < 2 {
			return alignedcmd.RandomRewardItemResolution{}, fmt.Errorf("random reward item=%d outcome has incomplete effect metadata", itemID)
		}
		if weight <= 0 {
			continue
		}
		outcome := alignedcmd.RandomRewardItemOutcome{Weight: weight}
		if rewardID > 0 {
			reward, err := resolveCurrentMagicBoxRewardItem(catalog, source, rewardID)
			if err != nil {
				return alignedcmd.RandomRewardItemResolution{}, fmt.Errorf("resolve random reward item=%d reward=%d: %w", itemID, rewardID, err)
			}
			if reward.ItemID != rewardID || reward.Kind == "" {
				return alignedcmd.RandomRewardItemResolution{}, fmt.Errorf("random reward item=%d reward=%d is unresolved", itemID, rewardID)
			}
			outcome.Reward = reward
		}
		resolution.Outcomes = append(resolution.Outcomes, outcome)
	}
	if len(resolution.Outcomes) == 0 {
		return alignedcmd.RandomRewardItemResolution{}, fmt.Errorf("random reward item=%d has no positive-weight outcomes", itemID)
	}
	return resolution, nil
}
