package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"longheng.io/server/internal/modules/dnf/itemexpiration"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type currentPVFExpirationReconcileSummary = itemexpiration.Summary

func (s *Service) currentPVFDefinitionForItem(catalog *pvfDungeonDropCatalog, itemID uint32) (dungeonDropItemDefinition, error) {
	if catalog == nil || itemID == 0 {
		return dungeonDropItemDefinition{}, nil
	}
	definition, err := catalog.ResolveItem(itemID)
	if errors.Is(err, errDungeonDropItemUnresolved) {
		return dungeonDropItemDefinition{}, nil
	}
	return definition, err
}

func (s *Service) reconcileCurrentPVFItemExpirations(
	ctx context.Context,
	repositories dnfrepo.Group,
	characterID string,
	accountID string,
) (currentPVFExpirationReconcileSummary, error) {
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return currentPVFExpirationReconcileSummary{}, err
	}
	return s.reconcileCurrentPVFItemExpirationsWithCatalog(ctx, repositories, characterID, accountID, catalog)
}

func (s *Service) reconcileCurrentPVFItemExpirationsWithCatalog(
	ctx context.Context,
	repositories dnfrepo.Group,
	characterID string,
	accountID string,
	catalog *pvfDungeonDropCatalog,
) (currentPVFExpirationReconcileSummary, error) {
	if catalog == nil {
		return currentPVFExpirationReconcileSummary{}, errDungeonDropSourceRequired
	}
	owner, err := itemexpiration.NewOwner(repositories)
	if err != nil {
		return currentPVFExpirationReconcileSummary{}, err
	}
	now := time.Now().UTC()
	return owner.Reconcile(ctx, itemexpiration.Command{
		AccountID:   accountID,
		CharacterID: characterID,
		UpdatedAt:   now,
		Project: func(assets *itemexpiration.Assets) (itemexpiration.Summary, error) {
			return s.projectCurrentPVFItemExpirations(catalog, assets, now)
		},
	})
}

func (s *Service) projectCurrentPVFItemExpirations(
	catalog *pvfDungeonDropCatalog,
	assets *itemexpiration.Assets,
	now time.Time,
) (itemexpiration.Summary, error) {
	var summary itemexpiration.Summary
	if assets == nil {
		return summary, itemexpiration.ErrProjectorRequired
	}
	var errs []error
	normalizeStack := func(scope string, stack dnfrepo.ItemStack) (dnfrepo.ItemStack, bool) {
		definition, resolveErr := s.currentPVFDefinitionForItem(catalog, sceneInventoryUint32FromInt64(stack.ItemID))
		if resolveErr != nil {
			errs = append(errs, fmt.Errorf("resolve %s item=%d: %w", scope, stack.ItemID, resolveErr))
			return cleanupCurrentPVFWrongExpirationProjection(stack)
		}
		return normalizeCurrentPVFItemStack(stack, definition, now)
	}

	if assets.Inventory != nil {
		for key, stack := range assets.Inventory.Slots {
			if patched, changed := normalizeStack("inventory key="+key, stack); changed {
				assets.Inventory.Slots[key] = patched
				summary.Inventory++
			}
		}
		for key, stack := range assets.Inventory.Warehouse {
			if patched, changed := normalizeStack("warehouse key="+key, stack); changed {
				assets.Inventory.Warehouse[key] = patched
				summary.Warehouse++
			}
		}
	}
	if assets.AccountInventory != nil {
		for key, stack := range assets.AccountInventory.Slots {
			if patched, changed := normalizeStack("account inventory key="+key, stack); changed {
				assets.AccountInventory.Slots[key] = patched
				summary.Account++
			}
		}
	}
	if assets.Equipment != nil {
		for key, entry := range assets.Equipment.Entries {
			definition, resolveErr := s.currentPVFDefinitionForItem(catalog, sceneInventoryUint32FromInt64(entry.ItemID))
			if resolveErr != nil {
				errs = append(errs, fmt.Errorf("resolve equipment key=%s item=%d: %w", key, entry.ItemID, resolveErr))
				patched, changed := normalizeCurrentPVFEquipmentEntry(entry, dungeonDropItemDefinition{}, now)
				if changed {
					assets.Equipment.Entries[key] = patched
					summary.Equipment++
				}
				continue
			}
			if patched, changed := normalizeCurrentPVFEquipmentEntry(entry, definition, now); changed {
				assets.Equipment.Entries[key] = patched
				summary.Equipment++
			}
		}
	}
	return summary, errors.Join(errs...)
}
