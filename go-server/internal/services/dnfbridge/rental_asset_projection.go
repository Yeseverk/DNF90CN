package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfrental "longheng.io/server/internal/modules/dnf/rental"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentRentalPointMetadataKey = dnfrental.PointMetadataKey
	currentRentalItemSource       = "pvf_rental"
	currentRentalDuration         = 24 * time.Hour
)

var (
	errCurrentRentalOwnerMismatch = dnfrental.ErrOwnerMismatch
	errCurrentRentalStateMissing  = dnfrental.ErrStateMissing
	errCurrentRentalPoints        = dnfrental.ErrPointsInsufficient
	errCurrentRentalGold          = dnfrental.ErrGoldInsufficient
	errCurrentRentalLimit         = dnfrental.ErrPointLimit
	errCurrentRentalInventoryFull = errors.New("dnf rental equipment inventory is full")
)

type currentRentalAssetResult struct {
	Points   uint32
	Gold     int64
	ExpireAt time.Time
	Slot     int16
	Equipped bool
}

func cleanupExpiredCurrentRentalEquipment(
	ctx context.Context,
	owner *dnfrental.Owner,
	accountID string,
	characterID string,
	now time.Time,
) (int, error) {
	if owner == nil {
		return 0, dnfrepo.ErrRentalAssetTransactionUnavailable
	}
	removed := 0
	err := owner.Cleanup(ctx, dnfrental.CleanupCommand{
		AccountID:   accountID,
		CharacterID: characterID,
		UpdatedAt:   now,
		Project: func(assets *dnfrental.Assets) (dnfrental.Changes, error) {
			inventoryDirty := false
			for key, stack := range assets.Inventory.Slots {
				if !currentRentalItemExpired(stack.Extra, stack.ExpireAt, now) {
					continue
				}
				delete(assets.Inventory.Slots, key)
				inventoryDirty = true
				removed++
			}
			equipmentDirty := false
			for key, entry := range assets.Equipment.Entries {
				if !currentRentalItemExpired(entry.Extra, entry.ExpireAt, now) {
					continue
				}
				delete(assets.Equipment.Entries, key)
				equipmentDirty = true
				removed++
			}
			return dnfrental.Changes{Inventory: inventoryDirty, Equipment: equipmentDirty}, nil
		},
	})
	if err != nil {
		return 0, currentRentalMutationError(err)
	}
	return removed, nil
}

func purchaseCurrentRentalPoints(
	ctx context.Context,
	owner *dnfrental.Owner,
	accountID string,
	characterID string,
	count uint32,
	limit uint32,
	goldPerPoint int64,
	now time.Time,
) (currentRentalAssetResult, error) {
	if owner == nil {
		return currentRentalAssetResult{}, dnfrepo.ErrRentalAssetTransactionUnavailable
	}
	result, err := owner.Charge(ctx, dnfrental.ChargeCommand{
		AccountID:    accountID,
		CharacterID:  characterID,
		Count:        count,
		Limit:        limit,
		GoldPerPoint: goldPerPoint,
		UpdatedAt:    now,
	})
	if err != nil {
		return currentRentalAssetResult{}, currentRentalMutationError(err)
	}
	return currentRentalAssetResult{Points: result.Points, Gold: result.Gold}, nil
}

func rentCurrentEquipment(
	ctx context.Context,
	owner *dnfrental.Owner,
	accountID string,
	characterID string,
	itemID uint32,
	pointCost uint32,
	definition dungeonDropItemDefinition,
	now time.Time,
) (currentRentalAssetResult, error) {
	if owner == nil || itemID == 0 || pointCost == 0 || definition.ItemID != itemID ||
		definition.Kind != dungeonDropItemEquipment || definition.Durability == 0 {
		return currentRentalAssetResult{}, dnfrepo.ErrRentalAssetTransactionUnavailable
	}
	expireAt := now.UTC().Add(currentRentalDuration)
	result := currentRentalAssetResult{ExpireAt: expireAt}
	wallet, err := owner.Rent(ctx, dnfrental.RentCommand{
		AccountID:   accountID,
		CharacterID: characterID,
		PointCost:   pointCost,
		UpdatedAt:   now,
		Project: func(assets *dnfrental.Assets) (dnfrental.Changes, error) {
			inventory := assets.Inventory
			equipment := assets.Equipment
			inventoryDirty, equipmentDirty := false, false
			foundExisting := false
			for key, stack := range inventory.Slots {
				listType, slot, ok := parseSceneInventorySlotKey(key)
				if !ok || listType != 0 || slot < definition.SlotStart || slot > definition.SlotEnd ||
					stack.ItemID != int64(itemID) ||
					!strings.EqualFold(strings.TrimSpace(stack.Extra["source"]), currentRentalItemSource) {
					continue
				}
				stack = currentRentalStack(stack, itemID, definition, expireAt)
				inventory.Slots[key] = stack
				inventoryDirty = true
				foundExisting = true
				result.Slot = slot
			}
			for key, entry := range equipment.Entries {
				if entry.ItemID != int64(itemID) ||
					!strings.EqualFold(strings.TrimSpace(entry.Extra["source"]), currentRentalItemSource) {
					continue
				}
				entry.ExpireAt = expireAt
				entry.Extra = currentRentalExtra(entry.Extra, definition, expireAt)
				equipment.Entries[key] = entry
				equipmentDirty = true
				foundExisting = true
				result.Slot = entry.SlotIndex
				result.Equipped = true
			}
			if !foundExisting {
				slot, ok := firstCurrentRentalInventorySlot(inventory.Slots, definition.SlotStart, definition.SlotEnd)
				if !ok {
					return dnfrental.Changes{}, errCurrentRentalInventoryFull
				}
				stack := currentRentalStack(dnfrepo.ItemStack{}, itemID, definition, expireAt)
				inventory.Slots[fmt.Sprintf("0:%d", slot)] = stack
				inventoryDirty = true
				result.Slot = slot
			}
			return dnfrental.Changes{Inventory: inventoryDirty, Equipment: equipmentDirty}, nil
		},
	})
	if err != nil {
		return currentRentalAssetResult{}, currentRentalMutationError(err)
	}
	result.Points = wallet.Points
	result.Gold = wallet.Gold
	return result, nil
}

func currentRentalPoints(account dnfrepo.AccountRecord) (uint32, error) {
	return dnfrental.Points(account)
}

func currentRentalStack(stack dnfrepo.ItemStack, itemID uint32, definition dungeonDropItemDefinition, expireAt time.Time) dnfrepo.ItemStack {
	stack.ItemID = int64(itemID)
	stack.Count = 1
	stack.Bind = false
	stack.ExpireAt = expireAt
	stack.Extra = currentRentalExtra(stack.Extra, definition, expireAt)
	entry := currentItemListEntryFromStack(0, 0, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)
	return stack
}

func currentRentalExtra(extra map[string]string, definition dungeonDropItemDefinition, expireAt time.Time) map[string]string {
	out := make(map[string]string, len(extra)+8)
	for key, value := range extra {
		out[key] = value
	}
	out["source"] = currentRentalItemSource
	out["item_kind"] = string(dungeonDropItemEquipment)
	out["pvf_path"] = definition.PVFPath
	out["durability"] = strconv.FormatUint(uint64(definition.Durability), 10)
	out["max_durability"] = strconv.FormatUint(uint64(definition.Durability), 10)
	out["expire_time"] = strconv.FormatInt(expireAt.Unix(), 10)
	out["rental_duration_hours"] = "24"
	return out
}

func firstCurrentRentalInventorySlot(slots map[string]dnfrepo.ItemStack, first, last int16) (int16, bool) {
	occupied := make(map[int16]struct{}, len(slots))
	for key := range slots {
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if ok && listType == 0 {
			occupied[slot] = struct{}{}
		}
	}
	for slot := first; slot <= last; slot++ {
		if _, used := occupied[slot]; !used {
			return slot, true
		}
	}
	return 0, false
}

func currentRentalItemExpired(extra map[string]string, expireAt time.Time, now time.Time) bool {
	if !strings.EqualFold(strings.TrimSpace(extra["source"]), currentRentalItemSource) {
		return false
	}
	if raw := strings.TrimSpace(extra["expire_time"]); raw != "" {
		if unix, err := strconv.ParseInt(raw, 10, 64); err == nil && unix > 0 {
			return unix <= now.Unix()
		}
	}
	return !expireAt.IsZero() && !expireAt.After(now)
}
