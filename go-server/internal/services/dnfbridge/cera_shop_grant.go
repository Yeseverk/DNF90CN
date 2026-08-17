package dnfbridge

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrevivecoin "longheng.io/server/internal/modules/dnf/revivecoin"
)

func currentCeraShopProductDefinitionForGrantAt(
	definition dungeonDropItemDefinition,
	product currentCeraShopProduct,
	now time.Time,
) (dungeonDropItemDefinition, error) {
	resolved, err := currentPVFItemDefinitionForGrantAt(definition, now)
	if err != nil {
		return dungeonDropItemDefinition{}, err
	}
	if product.DurationDays == 0 {
		return resolved, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nowUnix := now.Unix()
	durationSeconds := uint64(product.DurationDays) * uint64((24*time.Hour)/time.Second)
	if nowUnix <= 0 || nowUnix > math.MaxUint32 || durationSeconds > uint64(math.MaxUint32)-uint64(nowUnix) {
		return dungeonDropItemDefinition{}, fmt.Errorf(
			"%w: commodity=%d duration_days=%d now=%d",
			errCurrentCeraShopProductUnavailable,
			product.CommodityID,
			product.DurationDays,
			nowUnix,
		)
	}
	resolved.ExpirationDate = time.Unix(nowUnix+int64(durationSeconds), 0).UTC()
	return resolved, nil
}

func consumeCurrentCeraShopAvatarCoupon(inventory *dnfrepo.InventoryRecord, couponItemID int64) (currentItemListEntry, error) {
	if inventory == nil || couponItemID <= 0 {
		return currentItemListEntry{}, errCurrentCeraShopOwnerUnavailable
	}
	bestKey := ""
	bestSlot := int16(math.MaxInt16)
	var bestStack dnfrepo.ItemStack
	for key, stack := range inventory.Slots {
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if !ok || listType != dnfrepo.MainInventoryListType || slot < 0 || dnfrepo.IsAccountSharedInventorySlot(listType, slot) {
			continue
		}
		if stack.ItemID != couponItemID || stack.Count <= 0 {
			continue
		}
		if bestKey == "" || slot < bestSlot {
			bestKey = key
			bestSlot = slot
			bestStack = stack
		}
	}
	if bestKey == "" {
		return currentItemListEntry{}, fmt.Errorf("%w: avatar coupon item=%d", errCurrentCeraShopCeraInsufficient, couponItemID)
	}
	bestStack.Count--
	if bestStack.Count <= 0 {
		delete(inventory.Slots, bestKey)
		var removed currentItemListEntry
		removed.patchCore(bestSlot, math.MaxUint32, 0)
		return removed, nil
	}
	entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, bestSlot, bestStack)
	bestStack.RawEntry = append([]byte(nil), entry.data[:]...)
	inventory.Slots[bestKey] = bestStack
	return entry, nil
}

// currentCeraShopMainInventoryUpgradeStage recognizes the three main-bag
// expansion products through their runtime PVF paths. The returned value is
// the Cera-shop stage (1..3); the persisted op13 list-0 expansion is stage*8.
func currentCeraShopMainInventoryUpgradeStage(definition dungeonDropItemDefinition) (uint16, bool) {
	if definition.Kind != dungeonDropItemStackable {
		return 0, false
	}
	cleanPath := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(definition.PVFPath, "\\", "/")))
	cleanPath = strings.TrimPrefix(cleanPath, "stackable/")
	switch cleanPath {
	case "cash/inven_upgradekit1.stk":
		return 1, true
	case "cash/inven_upgradekit2.stk":
		return 2, true
	case "cash/chn_20140617_new_sales_item/chn_inventory_3rd_expansion_2683675.stk":
		return 3, true
	default:
		return 0, false
	}
}

// currentCeraShopPersonalCargoUpgradeTarget recognizes the original personal
// cargo tools from their real PVF paths. The no-suffix tool is the first tier
// (8 -> 24); numeric suffixes identify later 16-slot tiers.  The later live
// catalog uses a separate numeric directory, so catalog.personalCargoUpgradeTarget
// augments this path-only recognition with the item's own PVF name/explain
// fields rather than with a copied commodity-ID table.
func currentCeraShopPersonalCargoUpgradeTarget(definition dungeonDropItemDefinition) (uint16, bool) {
	if definition.Kind != dungeonDropItemStackable {
		return 0, false
	}
	cleanPath := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(definition.PVFPath, "\\", "/")))
	if cleanPath == "" || strings.Contains(cleanPath, "cash/chn_account_cargo/") {
		return 0, false
	}
	base := path.Base(cleanPath)
	if path.Ext(base) != ".stk" {
		return 0, false
	}
	stem := strings.TrimSuffix(base, ".stk")
	const prefix = "safe_upgradekit"
	if !strings.HasPrefix(stem, prefix) {
		return 0, false
	}
	tierText := strings.TrimPrefix(stem, prefix)
	if tierText != "" {
		for _, digit := range tierText {
			if digit < '0' || digit > '9' {
				return 0, false
			}
		}
	}
	tier := 0
	if tierText != "" {
		parsed, err := strconv.ParseUint(tierText, 10, 16)
		if err != nil {
			return 0, true
		}
		tier = int(parsed)
	}
	const (
		firstPersonalCargoUpgradeSlots = 24
		personalCargoUpgradeStep       = 16
		maximumPersonalCargoUpgrade    = 200
	)
	target := firstPersonalCargoUpgradeSlots + tier*personalCargoUpgradeStep
	if target > maximumPersonalCargoUpgrade {
		return 0, true
	}
	return uint16(target), true
}

// isAccountCargoUpgradeTool recognizes the account-cargo tool through the
// active PVF path, not through a copied commodity or item-template ID.  The
// ordinary personal-cargo recognizer explicitly excludes this directory.
func (c *pvfCeraShopCatalog) isAccountCargoUpgradeTool(definition dungeonDropItemDefinition) bool {
	if c == nil || definition.Kind != dungeonDropItemStackable {
		return false
	}
	cleanPath := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(definition.PVFPath, "\\", "/")))
	return path.Ext(cleanPath) == ".stk" && strings.Contains(cleanPath, "cash/chn_account_cargo/account_cargo")
}

// personalCargoUpgradeTarget recognizes every real personal-cargo tool in
// the active PVF.  The post-152 products are not named safe_upgradekit*.stk:
// their stable contract is the stackable item's exact name plus an explicit
// target capacity in [explain].  Both values are read from the PVF that also
// supplied the requested Cera product; neither a client field nor a static
// commodity-ID map can turn an ordinary item into a warehouse upgrade.
func (c *pvfCeraShopCatalog) personalCargoUpgradeTarget(definition dungeonDropItemDefinition) (uint16, bool) {
	if target, isUpgrade := currentCeraShopPersonalCargoUpgradeTarget(definition); isUpgrade {
		return target, true
	}
	if c == nil || c.source == nil || definition.Kind != dungeonDropItemStackable {
		return 0, false
	}
	cleanPath := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(definition.PVFPath, "\\", "/")))
	if cleanPath == "" || strings.Contains(cleanPath, "cash/chn_account_cargo/") || path.Ext(cleanPath) != ".stk" {
		return 0, false
	}
	document, err := parseDungeonCardPVFDocument(c.source, definition.PVFPath)
	if err != nil {
		return 0, false
	}
	name, nameFound := document.Text("name")
	if !nameFound || strings.TrimSpace(name) != "金库升级工具" {
		return 0, false
	}
	explain, explainFound := document.Text("explain")
	target, valid := currentCeraShopPersonalCargoTargetFromExplain(explain)
	if !explainFound || !valid {
		// This is positively a warehouse tool but its target is not a current
		// EXE-valid capacity. Reject it at checkout instead of silently placing
		// an unusable tool in the bag.
		return 0, true
	}
	return target, true
}

func currentCeraShopPersonalCargoTargetFromExplain(explain string) (uint16, bool) {
	const (
		prefix = "可以使金库的空间增加到"
		suffix = "格"
		max    = uint64(200)
	)
	clean := strings.TrimSpace(explain)
	if !strings.HasPrefix(clean, prefix) {
		return 0, false
	}
	remaining := strings.TrimPrefix(clean, prefix)
	end := strings.Index(remaining, suffix)
	if end <= 0 {
		return 0, false
	}
	target, err := strconv.ParseUint(strings.TrimSpace(remaining[:end]), 10, 16)
	if err != nil || target > max || target < uint64(currentExeInitialPersonalCargoSlotCount) {
		return 0, false
	}
	value := uint16(target)
	if (value-uint16(currentExeInitialPersonalCargoSlotCount))%16 != 0 {
		return 0, false
	}
	return value, true
}

// currentCeraShopPrepareContainerUpgrades validates and projects the persisted
// current EXE list-0/list-2 headers inside the Cera-shop asset transaction.
func currentCeraShopPrepareContainerUpgrades(
	record *dnfrepo.SettingsRecord,
	found bool,
	characterID string,
	purchases []currentCeraShopResolvedPurchase,
	now time.Time,
) (dnfrepo.SettingsRecord, uint16, uint16, bool, error) {
	hasUpgrade := false
	for _, purchase := range purchases {
		if purchase.MainInventoryUpgradeStage != 0 || purchase.PersonalCargoUpgradeTarget != 0 {
			hasUpgrade = true
			break
		}
	}
	if !hasUpgrade {
		return dnfrepo.SettingsRecord{}, 0, 0, false, nil
	}
	if record == nil || strings.TrimSpace(characterID) == "" {
		return dnfrepo.SettingsRecord{}, 0, 0, false, errCurrentCeraShopOwnerUnavailable
	}

	before := dnfrepo.CloneSettings(*record)
	currentMainExpansion := uint16(currentExeInitialMainSlotCount)
	currentSlots := uint16(currentExeInitialPersonalCargoSlotCount)
	if !found {
		before = newCharacterContainerStateSettings(characterID, now)
	} else {
		state, stateErr := dnfrepo.CharacterContainerStateFromSettings(before, characterID)
		if stateErr != nil {
			return dnfrepo.SettingsRecord{}, 0, 0, false, stateErr
		}
		currentMainExpansion = state.MainSlotCount
		currentSlots = state.PersonalCargoSlotCount
	}

	targetMainExpansion := currentMainExpansion
	targetSlots := currentSlots
	lastMainCommodityID := uint32(0)
	lastPersonalCargoCommodityID := uint32(0)
	for _, purchase := range purchases {
		if purchase.MainInventoryUpgradeStage != 0 {
			targetExpansion := purchase.MainInventoryUpgradeStage * 8
			if purchase.MainInventoryUpgradeStage > 3 || targetExpansion != targetMainExpansion+8 || targetExpansion > 24 {
				return dnfrepo.SettingsRecord{}, 0, 0, false, fmt.Errorf("%w: commodity=%d target_main_expansion=%d current_main_expansion=%d", errCurrentCeraShopProductUnavailable, purchase.Product.CommodityID, targetExpansion, targetMainExpansion)
			}
			targetMainExpansion = targetExpansion
			lastMainCommodityID = purchase.Product.CommodityID
		}
		if purchase.PersonalCargoUpgradeTarget != 0 {
			if purchase.PersonalCargoUpgradeTarget <= targetSlots {
				return dnfrepo.SettingsRecord{}, 0, 0, false, fmt.Errorf("%w: commodity=%d target_slots=%d current_slots=%d", errCurrentCeraShopProductUnavailable, purchase.Product.CommodityID, purchase.PersonalCargoUpgradeTarget, targetSlots)
			}
			targetSlots = purchase.PersonalCargoUpgradeTarget
			lastPersonalCargoCommodityID = purchase.Product.CommodityID
		}
	}

	after := dnfrepo.CloneSettings(before)
	if after.Values == nil {
		after.Values = make(map[string]string)
	}
	mainInventoryExpansion := uint16(0)
	personalCargoSlotCount := uint16(0)
	if targetMainExpansion != currentMainExpansion {
		after.Values["main_list_param16"] = strconv.Itoa(int(targetMainExpansion))
		after.Values["main_inventory_last_upgrade_commodity"] = strconv.FormatUint(uint64(lastMainCommodityID), 10)
		after.Values["main_inventory_last_upgrade_from"] = strconv.Itoa(int(currentMainExpansion))
		after.Values["main_inventory_last_upgrade_to"] = strconv.Itoa(int(targetMainExpansion))
		mainInventoryExpansion = targetMainExpansion
	}
	if targetSlots != currentSlots {
		after.Values["personal_cargo_list_param16"] = strconv.Itoa(int(targetSlots))
		after.Values["personal_cargo_last_upgrade_commodity"] = strconv.FormatUint(uint64(lastPersonalCargoCommodityID), 10)
		after.Values["personal_cargo_last_upgrade_from"] = strconv.Itoa(int(currentSlots))
		after.Values["personal_cargo_last_upgrade_to"] = strconv.Itoa(int(targetSlots))
		personalCargoSlotCount = targetSlots
	}
	switch {
	case mainInventoryExpansion != 0 && personalCargoSlotCount != 0:
		after.Values["source"] = "current_exe_cera_shop_container_upgrade"
	case mainInventoryExpansion != 0:
		after.Values["source"] = "current_exe_cera_shop_main_inventory_upgrade"
	default:
		after.Values["source"] = "current_exe_cera_shop_personal_cargo_upgrade"
	}
	after.UpdatedAt = now.UTC()
	return after, mainInventoryExpansion, personalCargoSlotCount, true, nil
}

func grantCurrentCeraShopProduct(
	inventory *dnfrepo.InventoryRecord,
	definition dungeonDropItemDefinition,
	count uint32,
) ([]uint16, error) {
	if inventory == nil || count == 0 {
		return nil, errCurrentCeraShopProductUnavailable
	}
	// Current runtime PVF and the 86JP domain reference agree that item 1 is
	// the fixed slot-1 revive-coin wallet, while item 42
	// (cash/coin_general.stk) grants one wallet unit. Neither reward may be
	// spilled into an ordinary backpack slot.
	if dnfrevivecoin.IsReward(int64(definition.ItemID)) {
		if _, err := dnfrevivecoin.Grant(
			inventory,
			int64(count),
			"current_cera_reward_revive_coin_wallet",
		); err != nil {
			return nil, fmt.Errorf("%w: revive coin reward item=%d: %v", errCurrentCeraShopProductUnavailable, definition.ItemID, err)
		}
		return []uint16{uint16(dnfrevivecoin.WalletSlot)}, nil
	}
	remaining := count
	slots := make([]uint16, 0, 1)
	touched := make(map[uint16]struct{})
	appendTouched := func(slot uint16) {
		if _, duplicate := touched[slot]; duplicate {
			return
		}
		touched[slot] = struct{}{}
		slots = append(slots, slot)
	}
	// Package and booster rewards can be a full PVF stack while the player
	// already owns a partial stack. Fill every existing compatible stack
	// first, then spill only the remainder into new slots. Passing the full
	// reward directly to addCurrentDungeonPickupToInventory required the whole
	// chunk to fit and therefore left the partial row untouched.
	fillRange := func(start, end int16) {
		if definition.Kind != dungeonDropItemStackable || remaining == 0 {
			return
		}
		for slot := start; slot <= end && remaining > 0; slot++ {
			key := currentDungeonPickupMainSlotKey(slot)
			stack, exists := inventory.Slots[key]
			if !exists ||
				stack.ItemID != int64(definition.ItemID) ||
				stack.Bind ||
				stack.Count <= 0 ||
				!currentItemStackExpirationMatches(stack, definition.ExpirationDate) {
				continue
			}
			available := int64(math.MaxInt32) - stack.Count
			if definition.StackLimit > 0 {
				if stack.Count >= definition.StackLimit {
					continue
				}
				available = definition.StackLimit - stack.Count
			}
			if available <= 0 {
				continue
			}
			added := int64(remaining)
			if added > available {
				added = available
			}
			stack.Count += added
			if !definition.ExpirationDate.IsZero() {
				stack, _ = applyCurrentPVFItemExpiration(stack, definition)
			}
			entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, slot, stack)
			stack.RawEntry = append([]byte(nil), entry.data[:]...)
			inventory.Slots[key] = stack
			remaining -= uint32(added)
			appendTouched(uint16(slot))
		}
	}
	if definition.Kind == dungeonDropItemStackable &&
		dungeonDropStackablePrefersItemQuickSlots(definition.StackableType) {
		fillRange(currentDungeonPickupQuickSlotStart, currentDungeonPickupQuickSlotEnd)
	}
	fillRange(definition.SlotStart, definition.SlotEnd)
	for remaining > 0 {
		chunk := remaining
		switch definition.Kind {
		case dungeonDropItemEquipment:
			chunk = 1
		case dungeonDropItemStackable:
			if definition.StackLimit > 0 && int64(chunk) > definition.StackLimit {
				chunk = uint32(definition.StackLimit)
			}
		default:
			return nil, errCurrentCeraShopProductUnavailable
		}
		slot, err := addCurrentDungeonPickupToInventory(inventory, definition, chunk)
		if err != nil {
			return nil, err
		}
		appendTouched(slot)
		remaining -= chunk
	}
	return slots, nil
}

func grantCurrentCeraShopAvatar(
	inventory *dnfrepo.InventoryRecord,
	source dnfpvf.Source,
	definition dungeonDropItemDefinition,
	product currentCeraShopProduct,
	attributeValue byte,
) (int16, error) {
	if inventory == nil || source == nil || definition.ItemID == 0 || definition.Kind != dungeonDropItemEquipment || product.Section != "avatar" {
		return 0, errCurrentCeraShopProductUnavailable
	}
	document, err := parseDungeonCardPVFDocument(source, definition.PVFPath)
	if err != nil {
		return 0, fmt.Errorf("%w: avatar item=%d PVF=%s: %v", errCurrentCeraShopProductUnavailable, definition.ItemID, definition.PVFPath, err)
	}
	equipmentType, found := document.Text("equipment type")
	if !found {
		return 0, fmt.Errorf("%w: avatar item=%d missing equipment type", errCurrentCeraShopProductUnavailable, definition.ItemID)
	}
	rule, ok := currentEquipmentPlacementRuleForPVFType(equipmentType)
	if !ok || rule.class != currentEquipmentPlacementClassAvatar {
		return 0, fmt.Errorf("%w: avatar item=%d type=%q", errCurrentCeraShopProductUnavailable, definition.ItemID, equipmentType)
	}
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	const avatarSlotStart int16 = 0
	const avatarSlotEnd int16 = 500
	var slot int16 = -1
	for candidate := avatarSlotStart; candidate <= avatarSlotEnd; candidate++ {
		if _, occupied := inventory.Slots[currentCeraShopInventorySlotKey(1, candidate)]; !occupied {
			slot = candidate
			break
		}
	}
	if slot < 0 {
		return 0, fmt.Errorf("%w: avatar item=%d range=%d..%d", errDungeonPickupInventoryFull, definition.ItemID, avatarSlotStart, avatarSlotEnd)
	}

	extra := map[string]string{
		"source":                         "cera_shop_avatar",
		"item_kind":                      "avatar",
		"pvf_path":                       definition.PVFPath,
		"equipment_type":                 rule.pvfType,
		"ext_data0":                      strconv.FormatUint(uint64(attributeValue), 10),
		"amount_or_count":                "0", // permanent avatars have no remaining-seconds countdown.
		"avatar_duration_days":           strconv.FormatUint(uint64(product.AvatarDurationDays), 10),
		"avatar_duration_selector_index": strconv.FormatUint(uint64(product.AvatarDurationIndex), 10),
	}
	// Logical count is 1 like every other equipment-kind stack; the wire
	// amount still renders as 0 through the amount_or_count override above
	// (permanent avatars have no remaining-seconds countdown). Consumers such
	// as avatar disjoint require Count == 1 for a single equipment instance.
	stack := dnfrepo.ItemStack{ItemID: int64(definition.ItemID), Count: 1, Extra: extra}
	now := time.Now().UTC()
	if product.AvatarDurationDays > 0 {
		stack.ExpireAt = now.Add(time.Duration(product.AvatarDurationDays) * 24 * time.Hour)
		expireUnix := strconv.FormatInt(stack.ExpireAt.Unix(), 10)
		stack.Extra["expire_time"] = expireUnix
		stack.Extra["expire_unix"] = expireUnix
	} else if !definition.ExpirationDate.IsZero() {
		stack, _ = applyCurrentPVFItemExpirationAt(stack, definition, now)
	}
	// Runtime PVF marks the National Day aura choices as [aurora avatar]
	// with two [emblem socket default] M sockets.  Those are creation-time
	// sockets: project them when the aura is granted, without consuming the
	// manual avatar socket-opening material.  Do not interpret [avatar type
	// select] as a default, and do not pre-open ordinary avatar parts.
	if rule.targetSlot == 9 {
		text, readErr := source.ReadText(definition.PVFPath)
		if readErr != nil {
			return 0, fmt.Errorf("%w: aura item=%d PVF=%s: %v", errCurrentCeraShopProductUnavailable, definition.ItemID, definition.PVFPath, readErr)
		}
		if socketTypes := currentParseAvatarDefaultSocketTypes(text); len(socketTypes) > 0 {
			var socketData [currentAvatarSocketBytes]byte
			currentSetAvatarSocketTypes(&socketData, socketTypes)
			currentApplyAvatarSocketDataToStack(&stack, 1, slot, socketData)
		}
	}
	entry := currentItemListEntryFromStack(1, slot, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)
	inventory.Slots[currentCeraShopInventorySlotKey(1, slot)] = stack
	return slot, nil
}

func currentCeraShopInventorySlotKey(listType byte, slot int16) string {
	return strconv.Itoa(int(listType)) + ":" + strconv.FormatInt(int64(slot), 10)
}

// isCurrentCeraShopCreatureItem returns true when the item is a pet/creature
// egg that belongs in the Pet list (list 7) rather than the main backpack.
// PVF rule: equipment type contains [creature].
func isCurrentCeraShopCreatureItem(definition dungeonDropItemDefinition) bool {
	if definition.Kind != dungeonDropItemEquipment {
		return false
	}
	return strings.Contains(strings.ToLower(definition.EquipmentType), "creature")
}

// isCurrentCeraShopPetConsumable identifies the runtime-PVF pet-consumable
// class.  In the active Script.pvf, creature food uses exactly
// [stackable type] [feed]; this is a type rule rather than an item-id/name
// exception.
func isCurrentCeraShopPetConsumable(definition dungeonDropItemDefinition) bool {
	return definition.Kind == dungeonDropItemStackable &&
		normalizeDungeonDropStackableType(definition.StackableType) == "[feed]"
}

// grantCurrentCeraShopPetConsumable places a PVF [feed] stack into list 7's
// current-EXE pet-consumable segment.  It deliberately cannot merge with an
// identically numbered row in list 0.
func grantCurrentCeraShopPetConsumable(
	inventory *dnfrepo.InventoryRecord,
	definition dungeonDropItemDefinition,
	count uint32,
) ([]int16, error) {
	if inventory == nil || count == 0 || definition.ItemID == 0 || !isCurrentCeraShopPetConsumable(definition) {
		return nil, errCurrentCeraShopProductUnavailable
	}
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}

	remaining := count
	changedSlots := make([]int16, 0, 1)
	for remaining > 0 {
		chunk := remaining
		if definition.StackLimit > 0 && int64(chunk) > definition.StackLimit {
			chunk = uint32(definition.StackLimit)
		}

		if definition.StackLimit > 0 {
			for slot := currentCeraShopPetConsumableSlotStart; slot <= currentCeraShopPetConsumableSlotEnd; slot++ {
				key := currentCeraShopInventorySlotKey(currentPetInventoryListType, slot)
				stack, found := inventory.Slots[key]
				if !found || stack.ItemID != int64(definition.ItemID) || stack.Bind || stack.Count < 0 {
					continue
				}
				if !currentItemStackExpirationMatches(stack, definition.ExpirationDate) {
					continue
				}
				if !definition.ExpirationDate.IsZero() {
					stack, _ = applyCurrentPVFItemExpiration(stack, definition)
				}
				if stack.Count > definition.StackLimit-int64(chunk) || stack.Count > math.MaxInt64-int64(chunk) {
					continue
				}
				stack.Count += int64(chunk)
				inventory.Slots[key] = stack
				changedSlots = append(changedSlots, slot)
				remaining -= chunk
				chunk = 0
				break
			}
			if chunk == 0 {
				continue
			}
		}

		var slot int16 = -1
		for candidate := currentCeraShopPetConsumableSlotStart; candidate <= currentCeraShopPetConsumableSlotEnd; candidate++ {
			if _, occupied := inventory.Slots[currentCeraShopInventorySlotKey(currentPetInventoryListType, candidate)]; !occupied {
				slot = candidate
				break
			}
		}
		if slot < 0 {
			return nil, fmt.Errorf(
				"%w: pet consumable item=%d range=%d..%d",
				errDungeonPickupInventoryFull,
				definition.ItemID,
				currentCeraShopPetConsumableSlotStart,
				currentCeraShopPetConsumableSlotEnd,
			)
		}

		extra := map[string]string{
			"source":         "cera_shop_pet_consumable",
			"item_kind":      string(definition.Kind),
			"pvf_path":       definition.PVFPath,
			"stackable_type": definition.StackableType,
		}
		if definition.StackLimit > 0 {
			extra["stack_limit"] = strconv.FormatInt(definition.StackLimit, 10)
		}
		stack := dnfrepo.ItemStack{ItemID: int64(definition.ItemID), Count: int64(chunk), Extra: extra}
		if !definition.ExpirationDate.IsZero() {
			stack, _ = applyCurrentPVFItemExpiration(stack, definition)
		}
		entry := currentItemListEntryFromStack(currentPetInventoryListType, slot, stack)
		stack.RawEntry = append([]byte(nil), entry.data[:]...)
		inventory.Slots[currentCeraShopInventorySlotKey(currentPetInventoryListType, slot)] = stack
		changedSlots = append(changedSlots, slot)
		remaining -= chunk
	}
	return changedSlots, nil
}

// grantCurrentCeraShopPet places a creature/pet egg into the Pet list (list 7)
// at the first available slot in the pet-body range (0-139).
func grantCurrentCeraShopPet(inventory *dnfrepo.InventoryRecord, definition dungeonDropItemDefinition) (int16, error) {
	if inventory == nil || definition.ItemID == 0 {
		return 0, errCurrentCeraShopProductUnavailable
	}
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	const petBodySlotStart int16 = 0
	const petBodySlotEnd int16 = 139
	for slot := petBodySlotStart; slot <= petBodySlotEnd; slot++ {
		key := fmt.Sprintf("%d:%d", currentPetInventoryListType, slot)
		if _, occupied := inventory.Slots[key]; occupied {
			continue
		}
		stack := dnfrepo.ItemStack{
			ItemID: int64(definition.ItemID),
			Count:  1,
			Extra: map[string]string{
				"source":         "cera_shop_creature",
				"item_kind":      string(dungeonDropItemEquipment),
				"pvf_path":       definition.PVFPath,
				"equipment_type": definition.EquipmentType,
			},
		}
		if !definition.ExpirationDate.IsZero() {
			stack.ExpireAt = definition.ExpirationDate
			stack.Extra["expire_time"] = strconv.FormatInt(definition.ExpirationDate.Unix(), 10)
		}
		inventory.Slots[key] = stack
		return slot, nil
	}
	return 0, fmt.Errorf("%w: pet body slots 0-139 full", errCurrentCeraShopProductUnavailable)
}
