package inventory

import (
	"context"
	"fmt"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// ReservedMainSlotRelocation records a one-time repair of an ordinary item
// that was imported into a main-list slot owned by a client wallet.
//
// The caller supplies the item's runtime-PVF page bounds.  Inventory owns the
// atomic mutation, while the bridge remains responsible for resolving those
// bounds from the active PVF rather than guessing a destination page.
type ReservedMainSlotRelocation struct {
	Changed  bool
	ItemID   int64
	FromSlot int16
	ToSlot   int16
}

// RelocateReservedMainSlot moves a non-wallet main-list item out of one fixed
// client-reserved slot into the first empty slot in its proven PVF page.  It
// preserves the entire durable stack (raw row, bind, expiration, and extras)
// and never overwrites an occupied destination.
func (o *Owner) RelocateReservedMainSlot(
	ctx context.Context,
	characterID string,
	reservedSlot int16,
	pageStart int16,
	pageEnd int16,
) (ReservedMainSlotRelocation, error) {
	if o == nil || o.repo == nil {
		return ReservedMainSlotRelocation{}, ErrOwnerUnavailable
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return ReservedMainSlotRelocation{}, ErrCharacterRequired
	}
	if reservedSlot < 0 || pageStart < 0 || pageEnd < pageStart || (reservedSlot >= pageStart && reservedSlot <= pageEnd) {
		return ReservedMainSlotRelocation{}, fmt.Errorf("%w: reserved=%d page=%d..%d", ErrReservedSlotRelocationFull, reservedSlot, pageStart, pageEnd)
	}
	if !o.inItemTransaction {
		var result ReservedMainSlotRelocation
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.RelocateReservedMainSlot(ctx, characterID, reservedSlot, pageStart, pageEnd)
			return err
		})
		return result, err
	}

	record, found, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return ReservedMainSlotRelocation{}, err
	}
	if !found {
		return ReservedMainSlotRelocation{}, ErrInventoryNotFound
	}
	record = dnfrepo.CloneInventory(record)
	if record.Slots == nil {
		return ReservedMainSlotRelocation{}, nil
	}
	sourceKey := slotKey(listTypeMain, reservedSlot)
	stack, found := record.Slots[sourceKey]
	if !found || stack.ItemID == 0 {
		return ReservedMainSlotRelocation{}, nil
	}
	for slot := pageStart; slot <= pageEnd; slot++ {
		destinationKey := slotKey(listTypeMain, slot)
		if _, occupied := record.Slots[destinationKey]; occupied {
			continue
		}
		delete(record.Slots, sourceKey)
		record.Slots[destinationKey] = stack
		record.UpdatedAt = time.Now().UTC()
		if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, dnfrepo.InventoryFieldSlots); err != nil {
			return ReservedMainSlotRelocation{}, err
		}
		return ReservedMainSlotRelocation{Changed: true, ItemID: stack.ItemID, FromSlot: reservedSlot, ToSlot: slot}, nil
	}
	return ReservedMainSlotRelocation{ItemID: stack.ItemID, FromSlot: reservedSlot}, fmt.Errorf("%w: item=%d reserved=%d page=%d..%d", ErrReservedSlotRelocationFull, stack.ItemID, reservedSlot, pageStart, pageEnd)
}
