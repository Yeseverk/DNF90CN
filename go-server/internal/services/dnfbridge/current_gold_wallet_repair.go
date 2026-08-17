package dnfbridge

import (
	"context"
	"errors"
	"fmt"

	dnfinventory "longheng.io/server/internal/modules/dnf/inventory"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentGoldWalletReservedSlot int16 = 0

// repairCurrentGoldWalletReservedSlot moves only a persisted ordinary item
// that occupies the current client's gold-wallet row.  Its destination comes
// from the active PVF definition, so this compatibility repair cannot turn an
// equipment/material/quest item into an arbitrary backpack-page item.
func (s *Service) repairCurrentGoldWalletReservedSlot(
	ctx context.Context,
	repositories dnfrepo.Group,
	characterID string,
	catalog *pvfDungeonDropCatalog,
) (dnfinventory.ReservedMainSlotRelocation, error) {
	if catalog == nil {
		return dnfinventory.ReservedMainSlotRelocation{}, errDungeonDropSourceRequired
	}
	if repositories.Inventory == nil {
		return dnfinventory.ReservedMainSlotRelocation{}, dnfinventory.ErrOwnerUnavailable
	}
	record, found, err := repositories.Inventory.Load(ctx, characterID)
	if err != nil || !found {
		return dnfinventory.ReservedMainSlotRelocation{}, err
	}
	stack, found := record.Slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, currentGoldWalletReservedSlot)]
	if !found || stack.ItemID == 0 {
		return dnfinventory.ReservedMainSlotRelocation{}, nil
	}
	itemID := sceneInventoryUint32FromInt64(stack.ItemID)
	if itemID == 0 {
		return dnfinventory.ReservedMainSlotRelocation{}, fmt.Errorf("gold wallet reserved-slot item id exceeds current PVF range: %d", stack.ItemID)
	}
	definition, err := catalog.ResolveItem(itemID)
	if err != nil {
		return dnfinventory.ReservedMainSlotRelocation{}, fmt.Errorf("resolve reserved-slot item=%d: %w", stack.ItemID, err)
	}
	if definition.SlotStart <= currentGoldWalletReservedSlot || definition.SlotEnd < definition.SlotStart {
		return dnfinventory.ReservedMainSlotRelocation{}, fmt.Errorf("reserved-slot item=%d has unusable PVF page=%d..%d", stack.ItemID, definition.SlotStart, definition.SlotEnd)
	}
	owner, err := dnfinventory.NewOwner(repositories)
	if err != nil {
		return dnfinventory.ReservedMainSlotRelocation{}, err
	}
	result, err := owner.RelocateReservedMainSlot(ctx, characterID, currentGoldWalletReservedSlot, definition.SlotStart, definition.SlotEnd)
	if errors.Is(err, dnfinventory.ErrReservedSlotRelocationFull) {
		return result, fmt.Errorf("reserved gold wallet slot item=%d cannot be moved within PVF page=%d..%d: %w", stack.ItemID, definition.SlotStart, definition.SlotEnd, err)
	}
	return result, err
}
