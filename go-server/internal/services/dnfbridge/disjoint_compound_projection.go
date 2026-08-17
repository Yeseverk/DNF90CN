package dnfbridge

import (
	"math"
	"sort"
	"strconv"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// currentGrantDisjointRewards places one resolved reward set per its fixed
// warehouse identity: the six cube fragment types stack into their fixed
// crystal warehouse slots (354..359) and the six soul types into their fixed
// soul warehouse slots (360..365); every other reward lands in the character
// backpack. Warehouse overflow falls back to the backpack so no reward is
// ever lost. It returns the ACK rows (slot/item/count/granted), whether the
// account warehouse record changed, and an error.
func currentGrantDisjointRewards(inventory *dnfrepo.InventoryRecord, account *dnfrepo.AccountInventoryRecord, catalog *pvfDungeonDropCatalog, rewards []currentDisjointReward) ([]currentDisjointRewardSlot, bool, error) {
	if inventory == nil || account == nil || catalog == nil || len(rewards) == 0 {
		return nil, false, errCurrentDisjointRewardInvalid
	}
	previousCounts := make(map[int16]int64)
	for key, stack := range inventory.Slots {
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if ok && listType == dnfrepo.MainInventoryListType && stack.Count > 0 {
			previousCounts[slot] = stack.Count
		}
	}
	for key, stack := range account.Slots {
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if ok && listType == dnfrepo.MainInventoryListType && stack.Count > 0 {
			previousCounts[slot] = stack.Count
		}
	}
	changed := make(map[int16]uint32)
	accountDirty := false
	for _, reward := range rewards {
		if reward.ItemID == 0 || reward.Count == 0 {
			return nil, false, errCurrentDisjointRewardInvalid
		}
		definition, err := catalog.ResolveItem(reward.ItemID)
		if err != nil || definition.Kind != dungeonDropItemStackable {
			return nil, false, errCurrentDisjointRewardInvalid
		}
		remaining := int64(reward.Count)
		if warehouseSlot, warehouse := currentDisjointWarehouseFixedSlots[reward.ItemID]; warehouse {
			left, grantErr := grantCurrentDisjointWarehouseStack(account, definition, warehouseSlot, remaining)
			if grantErr != nil {
				return nil, false, grantErr
			}
			if left < remaining {
				accountDirty = true
				changed[warehouseSlot] = reward.ItemID
			}
			remaining = left
		}
		if remaining > 0 {
			slots, err := grantCurrentCeraShopProduct(inventory, definition, uint32(remaining))
			if err != nil {
				return nil, false, err
			}
			for _, slotValue := range slots {
				slot := int16(slotValue)
				stack, found := inventory.Slots[currentCeraShopInventorySlotKey(0, slot)]
				if !found || stack.ItemID != int64(reward.ItemID) || stack.Count <= 0 {
					return nil, false, errCurrentDisjointRewardInvalid
				}
				entry := currentItemListEntryFromStack(0, slot, stack)
				stack.RawEntry = append([]byte(nil), entry.data[:]...)
				inventory.Slots[currentCeraShopInventorySlotKey(0, slot)] = stack
				changed[slot] = reward.ItemID
			}
		}
	}
	out := make([]currentDisjointRewardSlot, 0, len(changed))
	for slot, itemID := range changed {
		stack, found := inventory.Slots[currentCeraShopInventorySlotKey(0, slot)]
		if !found {
			stack, found = account.Slots[currentCeraShopInventorySlotKey(0, slot)]
		}
		if !found || stack.Count <= 0 || stack.Count > math.MaxUint32 {
			return nil, false, errCurrentDisjointRewardInvalid
		}
		added := stack.Count - previousCounts[slot]
		if added <= 0 || added > math.MaxUint32 {
			return nil, false, errCurrentDisjointRewardInvalid
		}
		out = append(out, currentDisjointRewardSlot{Slot: slot, ItemID: itemID, Count: uint32(stack.Count), Granted: uint32(added)})
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Slot < out[right].Slot })
	return out, accountDirty, nil
}

// currentDisjointWarehouseFixedSlots maps each warehouse-owned disjoint
// reward item to its single fixed slot. The mapping is the 90-client layout
// verified live on 2026-07-15 (py90 scene.py notes, user-confirmed in game):
// the six cube fragments own crystal warehouse slots 354..359 and the six
// souls own soul warehouse slots 360..365, one fixed item id per slot.
// Element crystals (3166/3167) and every other stackable have no fixed slot
// and stay in the character backpack.
var currentDisjointWarehouseFixedSlots = map[uint32]int16{
	3033:     354, // cube_black
	3034:     355, // cube_white
	3035:     356, // cube_red
	3036:     357, // cube_blue
	3037:     358, // cube_clear
	3262:     359, // cube_gold
	10100115: 360, // common soul
	10100116: 361, // advanced soul
	10099773: 362, // rare soul
	10099774: 363, // unique soul
	10099775: 364, // legendary soul
	10158124: 365, // epic soul
}

// grantCurrentDisjointWarehouseStack stacks count into the item's single
// fixed warehouse slot and returns the unplaced remainder (stack-limit
// overflow, or a foreign item squatting on the fixed slot from a legacy
// misplacement), which the caller overflows to the backpack so no reward is
// ever lost. A squatting foreign stack is never merged into or evicted.
func grantCurrentDisjointWarehouseStack(account *dnfrepo.AccountInventoryRecord, definition dungeonDropItemDefinition, slot int16, count int64) (int64, error) {
	key := dnfrepo.AccountSharedInventorySlotKey(slot)
	stack, exists := account.Slots[key]
	if exists && stack.ItemID > 0 && stack.ItemID != int64(definition.ItemID) {
		return count, nil
	}
	room := count
	if definition.StackLimit > 0 {
		if space := definition.StackLimit - stack.Count; space < room {
			room = space
		}
	}
	if room <= 0 {
		return count, nil
	}
	if !exists || stack.ItemID <= 0 {
		stack = dnfrepo.ItemStack{
			ItemID: int64(definition.ItemID),
			Extra: map[string]string{
				"source":    "dungeon_pvf_disjoint",
				"item_kind": string(definition.Kind),
				"pvf_path":  definition.PVFPath,
			},
		}
		if definition.StackableType != "" {
			stack.Extra["stackable_type"] = definition.StackableType
		}
		if definition.StackLimit > 0 {
			stack.Extra["stack_limit"] = strconv.FormatInt(definition.StackLimit, 10)
		}
	}
	stack.Count += room
	entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, slot, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)
	account.Slots[key] = stack
	return count - room, nil
}
