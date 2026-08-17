package dnfbridge

import (
	"fmt"
	"math"
	"sort"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// grantCurrentBoosterRewardByOwner routes the fixed crystal/soul identities
// into the account-shared list-0 warehouse before falling back to the normal
// reward placement. The account and character writes are committed by the
// same AccountAssets transaction.
func grantCurrentBoosterRewardByOwner(
	inventory *dnfrepo.InventoryRecord,
	account *dnfrepo.AccountInventoryRecord,
	source dnfpvf.Source,
	reward currentBoosterReward,
) ([]string, bool, error) {
	if inventory == nil || account == nil || reward.Definition.ItemID == 0 || reward.Count == 0 {
		return nil, false, errCurrentBoosterPVFInvalid
	}
	remaining := int64(reward.Count)
	accountChanged := false
	if slot, shared := currentDisjointWarehouseFixedSlots[reward.Definition.ItemID]; shared {
		left, err := grantCurrentDisjointWarehouseStack(account, reward.Definition, slot, remaining)
		if err != nil {
			return nil, false, err
		}
		if left < remaining {
			accountChanged = true
			key := dnfrepo.AccountSharedInventorySlotKey(slot)
			stack := account.Slots[key]
			if stack.Extra == nil {
				stack.Extra = make(map[string]string, 6)
			}
			stack.Extra["source"] = "booster_item"
			stack.Extra["last_grant_source"] = "booster_item"
			account.Slots[key] = stack
		}
		remaining = left
	}
	if remaining == 0 {
		return nil, accountChanged, nil
	}
	if remaining > math.MaxUint32 {
		return nil, false, errCurrentBoosterPVFInvalid
	}
	overflow := reward
	overflow.Count = uint32(remaining)
	keys, err := grantCurrentBoosterReward(inventory, source, overflow)
	return keys, accountChanged, err
}
func grantCurrentBoosterReward(
	inventory *dnfrepo.InventoryRecord,
	source dnfpvf.Source,
	reward currentBoosterReward,
) ([]string, error) {
	if inventory == nil || reward.Definition.ItemID == 0 || reward.Count == 0 {
		return nil, errCurrentBoosterPVFInvalid
	}
	keys := make([]string, 0, 1)
	switch reward.Placement {
	case currentBoosterRewardAvatar:
		for index := uint32(0); index < reward.Count; index++ {
			slot, err := grantCurrentCeraShopAvatar(
				inventory,
				source,
				reward.Definition,
				currentCeraShopProduct{ItemID: reward.Definition.ItemID, Count: 1, Section: "avatar"},
				reward.Option,
			)
			if err != nil {
				return nil, err
			}
			keys = append(keys, currentCeraShopInventorySlotKey(1, slot))
		}
	case currentBoosterRewardPetBody:
		for index := uint32(0); index < reward.Count; index++ {
			slot, err := grantCurrentCeraShopPet(inventory, reward.Definition)
			if err != nil {
				return nil, err
			}
			keys = append(keys, currentCeraShopInventorySlotKey(currentPetInventoryListType, slot))
		}
	case currentBoosterRewardPetArtifact:
		for index := uint32(0); index < reward.Count; index++ {
			slot, err := grantCurrentBoosterPetArtifact(inventory, reward)
			if err != nil {
				return nil, err
			}
			keys = append(keys, currentCeraShopInventorySlotKey(currentPetInventoryListType, slot))
		}
	case currentBoosterRewardPetConsumable:
		slots, err := grantCurrentCeraShopPetConsumable(inventory, reward.Definition, reward.Count)
		if err != nil {
			return nil, err
		}
		for _, slot := range slots {
			keys = append(keys, currentCeraShopInventorySlotKey(currentPetInventoryListType, slot))
		}
	default:
		slots, err := grantCurrentCeraShopProduct(inventory, reward.Definition, reward.Count)
		if err != nil {
			return nil, err
		}
		for _, slot := range slots {
			keys = append(keys, currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, int16(slot)))
		}
	}
	for _, key := range keys {
		stack := inventory.Slots[key]
		if stack.Extra == nil {
			stack.Extra = make(map[string]string, 4)
		}
		if reward.Seal {
			stack.Extra["seal_flag"] = "1"
		}
		inventory.Slots[key] = stack
	}
	return keys, nil
}

func grantCurrentBoosterPetArtifact(
	inventory *dnfrepo.InventoryRecord,
	reward currentBoosterReward,
) (int16, error) {
	if inventory == nil || reward.Definition.Kind != dungeonDropItemEquipment || reward.Placement != currentBoosterRewardPetArtifact {
		return 0, errCurrentBoosterPVFInvalid
	}
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	for slot := currentBoosterPetArtifactStart; slot <= currentBoosterPetArtifactEnd; slot++ {
		key := currentCeraShopInventorySlotKey(currentPetInventoryListType, slot)
		if _, occupied := inventory.Slots[key]; occupied {
			continue
		}
		extra := map[string]string{
			"source":          "booster_item",
			"item_kind":       "equipment",
			"pvf_path":        reward.Definition.PVFPath,
			"equipment_type":  reward.Definition.EquipmentType,
			"amount_or_count": "0",
		}
		if reward.Seal {
			extra["seal_flag"] = "1"
		}
		stack := dnfrepo.ItemStack{ItemID: int64(reward.Definition.ItemID), Count: 1, Extra: extra}
		if !reward.Definition.ExpirationDate.IsZero() {
			stack, _ = applyCurrentPVFItemExpiration(stack, reward.Definition)
		}
		entry := currentItemListEntryFromStack(currentPetInventoryListType, slot, stack)
		stack.RawEntry = append([]byte(nil), entry.data[:]...)
		inventory.Slots[key] = stack
		return slot, nil
	}
	return 0, fmt.Errorf("%w: pet artifact item=%d range=%d..%d", errDungeonPickupInventoryFull, reward.Definition.ItemID, currentBoosterPetArtifactStart, currentBoosterPetArtifactEnd)
}

func findCurrentBoosterMaterial(
	items map[string]dnfrepo.ItemStack,
	itemID int64,
	count int64,
	sourceKey string,
) string {
	keys := make([]string, 0, len(items))
	for key, stack := range items {
		listType, _, ok := parseSceneInventorySlotKey(key)
		if !ok || listType != dnfrepo.MainInventoryListType || key == sourceKey || stack.ItemID != itemID || stack.Count < count {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}
