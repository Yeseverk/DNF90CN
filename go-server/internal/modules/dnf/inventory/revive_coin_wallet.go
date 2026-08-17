package inventory

import (
	"context"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrevivecoin "longheng.io/server/internal/modules/dnf/revivecoin"
)

// MigrateReviveCoinWallet atomically consolidates legacy item-1 wallet rows and
// legacy item-42 coin_general backpack stacks into the current fixed main-list
// slot-1 wallet. Repeated calls are no-ops after the canonical row has been
// persisted.
func (o *Owner) MigrateReviveCoinWallet(
	ctx context.Context,
	characterID string,
) (dnfrevivecoin.Consolidation, error) {
	if o == nil || o.repo == nil {
		return dnfrevivecoin.Consolidation{}, ErrOwnerUnavailable
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return dnfrevivecoin.Consolidation{}, ErrCharacterRequired
	}
	if !o.inItemTransaction {
		var result dnfrevivecoin.Consolidation
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.MigrateReviveCoinWallet(ctx, characterID)
			return err
		})
		return result, err
	}

	record, found, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return dnfrevivecoin.Consolidation{}, err
	}
	if !found {
		return dnfrevivecoin.Consolidation{}, ErrInventoryNotFound
	}
	record = dnfrepo.CloneInventory(record)
	result, err := dnfrevivecoin.MigrateLegacy(&record)
	if err != nil || !result.Changed {
		return result, err
	}
	record.UpdatedAt = time.Now().UTC()
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, dnfrepo.InventoryFieldSlots); err != nil {
		return dnfrevivecoin.Consolidation{}, err
	}
	return result, nil
}
