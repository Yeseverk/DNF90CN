package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

func (s *Service) applyCurrentPVFUsePeriodsToEntriesAt(entries []currentItemListEntry, now time.Time) (int, error) {
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return 0, err
	}
	return s.applyCurrentPVFUsePeriodsToEntriesWithCatalog(context.Background(), entries, catalog, now)
}

func (s *Service) applyCurrentPVFUsePeriodsToEntriesWithLoadedCatalog(ctx context.Context, entries []currentItemListEntry) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return 0, err
	}
	return s.applyCurrentPVFUsePeriodsToEntriesWithCatalog(ctx, entries, catalog, time.Now().UTC())
}

func (s *Service) applyCurrentPVFUsePeriodsToEntriesWithCatalog(ctx context.Context, entries []currentItemListEntry, catalog *pvfDungeonDropCatalog, now time.Time) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if catalog == nil {
		return 0, errDungeonDropSourceRequired
	}
	patched := 0
	var errs []error
	for index := range entries {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		itemID := binary.LittleEndian.Uint32(entries[index].data[0x02:0x06])
		definition, resolveErr := s.currentPVFDefinitionForItem(catalog, itemID)
		if resolveErr != nil {
			errs = append(errs, fmt.Errorf("resolve wire item=%d: %w", itemID, resolveErr))
			continue
		}
		rowChanged := false
		pvfUnix := currentPVFExpirationUnix(definition.ExpirationDate)
		rowUnix := binary.LittleEndian.Uint32(entries[index].data[currentItemListExpireTimeOffset : currentItemListExpireTimeOffset+4])
		effectiveUnix := pvfUnix
		if rowUnix != 0 {
			effectiveUnix = rowUnix
		}
		if pvfUnix != 0 && binary.LittleEndian.Uint32(entries[index].data[legacyWrongCurrentItemListExpireTimeOffset:legacyWrongCurrentItemListExpireTimeOffset+4]) == pvfUnix {
			binary.LittleEndian.PutUint32(entries[index].data[legacyWrongCurrentItemListExpireTimeOffset:legacyWrongCurrentItemListExpireTimeOffset+4], 0)
			rowChanged = true
		}
		if pvfUnix != 0 && binary.LittleEndian.Uint32(entries[index].data[0x6E:0x72]) == pvfUnix {
			binary.LittleEndian.PutUint32(entries[index].data[0x6E:0x72], 0)
			rowChanged = true
		}
		if effectiveUnix != 0 && rowUnix != effectiveUnix {
			binary.LittleEndian.PutUint32(entries[index].data[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4], effectiveUnix)
			rowChanged = true
		}
		if isCurrentCeraShopCreatureItem(definition) {
			remainingSeconds := currentPetRemainingSecondsAt(effectiveUnix, now)
			if binary.LittleEndian.Uint32(entries[index].data[currentPetRemainSecondsOffset:currentPetRemainSecondsOffset+4]) != remainingSeconds {
				binary.LittleEndian.PutUint32(entries[index].data[currentPetRemainSecondsOffset:currentPetRemainSecondsOffset+4], remainingSeconds)
				rowChanged = true
			}
		}
		if definition.Kind == dungeonDropItemStackable && effectiveUnix != 0 {
			usePeriod := currentPVFStackableUsePeriodSeconds(time.Unix(int64(effectiveUnix), 0).UTC(), now)
			if binary.LittleEndian.Uint16(entries[index].data[0x0B:0x0D]) != usePeriod {
				binary.LittleEndian.PutUint16(entries[index].data[0x0B:0x0D], usePeriod)
				rowChanged = true
			}
		}
		if rowChanged {
			patched++
		}
	}
	return patched, errors.Join(errs...)
}

func (s *Service) applyCurrentPVFUsePeriodsToEntries(entries []currentItemListEntry) (int, error) {
	return s.applyCurrentPVFUsePeriodsToEntriesWithLoadedCatalog(context.Background(), entries)
}
