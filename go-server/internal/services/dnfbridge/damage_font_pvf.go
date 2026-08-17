package dnfbridge

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func (s *Service) alignedDamageFontResolverForCommand(opcode dnfenum.CmdPacket) (alignedcmd.DamageFontResolver, error) {
	if opcode != dnfenum.CmdPacketUseStackableAction {
		return nil, nil
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, errDungeonDropSourceRequired
	}
	return func(itemID int64) (alignedcmd.DamageFontResolution, error) {
		if itemID <= 0 || itemID > math.MaxUint32 {
			return alignedcmd.DamageFontResolution{}, nil
		}
		definition, err := catalog.ResolveItem(uint32(itemID))
		if err != nil {
			if errors.Is(err, errDungeonDropItemUnresolved) {
				return alignedcmd.DamageFontResolution{}, nil
			}
			return alignedcmd.DamageFontResolution{}, fmt.Errorf("resolve damage-font item=%d: %w", itemID, err)
		}
		valid := definition.Kind == dungeonDropItemStackable &&
			strings.EqualFold(strings.TrimSpace(definition.ActionType), "[add damage font skin]") &&
			definition.DamageFontIndex != 0 &&
			definition.DamageFontExpirationMode != alignedcmd.DamageFontExpirationUnknown
		return alignedcmd.DamageFontResolution{
			Valid:           valid,
			PVFPath:         definition.PVFPath,
			ActionType:      definition.ActionType,
			FontIndex:       definition.DamageFontIndex,
			ExpirationMode:  definition.DamageFontExpirationMode,
			PeriodDays:      definition.DamageFontPeriodDays,
			FixedExpiration: definition.DamageFontFixedExpiration,
		}, nil
	}, nil
}
