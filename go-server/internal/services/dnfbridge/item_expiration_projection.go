package dnfbridge

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentPVFWrongExpirationSource = "runtime_pvf"

const (
	currentJoustEventItemFirst uint32 = 490005585
	currentJoustEventItemLast  uint32 = 490005593
)

// currentJoustEventHasRetiredInstanceExpiration identifies the nine items
// that form the 2017 horse-joust shop/reward chain.  Their original PVF date
// was in 2017, so clients that bought one before the local all-day repair
// retain that stale deadline in the item instance even after the current PVF
// definition is moved forward.  These are static event items, not rentals or
// duration grants: a smaller per-instance deadline can only be the retired
// event date and must not win over the repaired PVF definition.
func currentJoustEventHasRetiredInstanceExpiration(
	itemID uint32,
	instanceExpire uint32,
	definitionExpire time.Time,
) bool {
	if itemID < currentJoustEventItemFirst || itemID > currentJoustEventItemLast ||
		instanceExpire == 0 || definitionExpire.IsZero() {
		return false
	}
	definitionUnix := currentPVFExpirationUnix(definitionExpire)
	return definitionUnix != 0 && instanceExpire < definitionUnix
}

func currentPVFWrongExpirationUnix(stack dnfrepo.ItemStack) uint32 {
	for _, key := range []string{"expire_time", "expire_unix"} {
		raw := strings.TrimSpace(stack.Extra[key])
		value, err := strconv.ParseUint(raw, 10, 32)
		if err == nil && value != 0 {
			return uint32(value)
		}
	}
	if !stack.ExpireAt.IsZero() && stack.ExpireAt.Unix() > 0 && stack.ExpireAt.Unix() <= math.MaxUint32 {
		return uint32(stack.ExpireAt.Unix())
	}
	return 0
}

// cleanupCurrentPVFWrongExpirationProjection removes only the synthetic state
// written by the superseded +0x6E implementation. A genuine rental expiration
// has no runtime_pvf marker and is preserved. The row tail is cleared only when
// it exactly equals the synthetic timestamp, so an unrelated real price value
// is never erased merely because the item also has a PVF expiration date.
func cleanupCurrentPVFWrongExpirationProjection(stack dnfrepo.ItemStack) (dnfrepo.ItemStack, bool) {
	if !strings.EqualFold(strings.TrimSpace(stack.Extra["expiration_source"]), currentPVFWrongExpirationSource) {
		return stack, false
	}
	wrongUnix := currentPVFWrongExpirationUnix(stack)
	changed := false
	if !stack.ExpireAt.IsZero() {
		stack.ExpireAt = time.Time{}
		changed = true
	}
	if stack.Extra != nil {
		cloned := make(map[string]string, len(stack.Extra))
		for key, value := range stack.Extra {
			cloned[key] = value
		}
		for _, key := range []string{"expire_time", "expire_unix", "expiration_source"} {
			if _, exists := cloned[key]; exists {
				delete(cloned, key)
				changed = true
			}
		}
		stack.Extra = cloned
	}
	if wrongUnix != 0 && len(stack.RawEntry) == currentItemListEntryWireSize &&
		binary.LittleEndian.Uint32(stack.RawEntry[0x6E:0x72]) == wrongUnix {
		stack.RawEntry = append([]byte(nil), stack.RawEntry...)
		binary.LittleEndian.PutUint32(stack.RawEntry[0x6E:0x72], 0)
		changed = true
	}
	return stack, changed
}

func applyCurrentPVFStackableUsePeriodAt(stack dnfrepo.ItemStack, expirationDate time.Time, now time.Time) (dnfrepo.ItemStack, bool) {
	stack, changed := cleanupCurrentPVFWrongExpirationProjection(stack)
	patchedExpire, expireChanged := applyCurrentItemListExpireTimeRaw(stack, expirationDate)
	stack = patchedExpire
	changed = changed || expireChanged
	usePeriod := currentPVFStackableUsePeriodSeconds(expirationDate, now)
	if len(stack.RawEntry) == 0 {
		stack.RawEntry = make([]byte, currentItemListEntryWireSize)
		changed = true
	}
	if len(stack.RawEntry) == currentItemListEntryWireSize && binary.LittleEndian.Uint16(stack.RawEntry[0x0B:0x0D]) != usePeriod {
		stack.RawEntry = append([]byte(nil), stack.RawEntry...)
		binary.LittleEndian.PutUint16(stack.RawEntry[0x0B:0x0D], usePeriod)
		changed = true
	}
	return stack, changed
}

func applyCurrentItemListExpireTimeRaw(stack dnfrepo.ItemStack, expirationDate time.Time) (dnfrepo.ItemStack, bool) {
	if expirationDate.IsZero() || expirationDate.Unix() <= 0 {
		return stack, false
	}
	changed := false
	expireUnix := sceneInventoryUint32FromInt64(expirationDate.Unix())
	if len(stack.RawEntry) == 0 {
		stack.RawEntry = make([]byte, currentItemListEntryWireSize)
		changed = true
	}
	if len(stack.RawEntry) == currentItemListEntryWireSize && (binary.LittleEndian.Uint32(stack.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4]) != expireUnix ||
		binary.LittleEndian.Uint32(stack.RawEntry[legacyWrongCurrentItemListExpireTimeOffset:legacyWrongCurrentItemListExpireTimeOffset+4]) == expireUnix) {
		stack.RawEntry = append([]byte(nil), stack.RawEntry...)
		if binary.LittleEndian.Uint32(stack.RawEntry[legacyWrongCurrentItemListExpireTimeOffset:legacyWrongCurrentItemListExpireTimeOffset+4]) == expireUnix {
			binary.LittleEndian.PutUint32(stack.RawEntry[legacyWrongCurrentItemListExpireTimeOffset:legacyWrongCurrentItemListExpireTimeOffset+4], 0)
		}
		binary.LittleEndian.PutUint32(stack.RawEntry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4], expireUnix)
		changed = true
	}
	return stack, changed
}

func applyCurrentPVFStackableUsePeriod(stack dnfrepo.ItemStack, expirationDate time.Time) (dnfrepo.ItemStack, bool) {
	return applyCurrentPVFStackableUsePeriodAt(stack, expirationDate, time.Now().UTC())
}

func applyCurrentStackableExpirationAt(stack dnfrepo.ItemStack, expirationDate time.Time, now time.Time) (dnfrepo.ItemStack, bool) {
	stack, changed := cleanupCurrentPVFWrongExpirationProjection(stack)
	if expirationDate.IsZero() {
		return stack, changed
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 4)
		changed = true
	} else {
		cloned := make(map[string]string, len(stack.Extra)+4)
		for key, value := range stack.Extra {
			cloned[key] = value
		}
		stack.Extra = cloned
	}
	expireUnix := strconv.FormatInt(expirationDate.Unix(), 10)
	for _, key := range []string{"expire_time", "expire_unix"} {
		if stack.Extra[key] != expireUnix {
			stack.Extra[key] = expireUnix
			changed = true
		}
	}
	if stack.Extra["item_kind"] == "" {
		stack.Extra["item_kind"] = string(dungeonDropItemStackable)
		changed = true
	}
	if !stack.ExpireAt.Equal(expirationDate) {
		stack.ExpireAt = expirationDate
		changed = true
	}
	patched, patchedChanged := applyCurrentPVFStackableUsePeriodAt(stack, expirationDate, now)
	return patched, changed || patchedChanged
}

func applyCurrentStackableExpiration(stack dnfrepo.ItemStack, expirationDate time.Time) (dnfrepo.ItemStack, bool) {
	return applyCurrentStackableExpirationAt(stack, expirationDate, time.Now().UTC())
}

func applyCurrentPVFItemExpirationAt(stack dnfrepo.ItemStack, definition dungeonDropItemDefinition, now time.Time) (dnfrepo.ItemStack, bool) {
	stack, changed := cleanupCurrentPVFWrongExpirationProjection(stack)
	if definition.ExpirationDate.IsZero() {
		return stack, changed
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expirationDate := definition.ExpirationDate
	if instanceExpire := currentItemListStackExpire(stack); instanceExpire != 0 &&
		!currentJoustEventHasRetiredInstanceExpiration(
			sceneInventoryUint32FromInt64(stack.ItemID), instanceExpire, definition.ExpirationDate) {
		expirationDate = time.Unix(int64(instanceExpire), 0).UTC()
	}
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 5)
		changed = true
	} else {
		cloned := make(map[string]string, len(stack.Extra)+5)
		for key, value := range stack.Extra {
			cloned[key] = value
		}
		stack.Extra = cloned
	}
	expireUnix := strconv.FormatInt(expirationDate.Unix(), 10)
	for _, key := range []string{"expire_time", "expire_unix"} {
		if stack.Extra[key] != expireUnix {
			stack.Extra[key] = expireUnix
			changed = true
		}
	}
	if definition.Kind != "" && stack.Extra["item_kind"] == "" {
		stack.Extra["item_kind"] = string(definition.Kind)
		changed = true
	}
	if definition.PVFPath != "" && stack.Extra["pvf_path"] == "" {
		stack.Extra["pvf_path"] = definition.PVFPath
		changed = true
	}
	if definition.StackableType != "" && stack.Extra["stackable_type"] == "" {
		stack.Extra["stackable_type"] = definition.StackableType
		changed = true
	}
	if definition.StackLimit > 0 && stack.Extra["stack_limit"] == "" {
		stack.Extra["stack_limit"] = strconv.FormatInt(definition.StackLimit, 10)
		changed = true
	}
	if definition.UsablePeriodDays > 0 {
		usablePeriod := strconv.FormatInt(definition.UsablePeriodDays, 10)
		if stack.Extra["usable_period_days"] != usablePeriod {
			stack.Extra["usable_period_days"] = usablePeriod
			changed = true
		}
		if stack.Extra["expiration_source"] != "runtime_pvf_usable_period_grant" {
			stack.Extra["expiration_source"] = "runtime_pvf_usable_period_grant"
			changed = true
		}
	}
	if !stack.ExpireAt.Equal(expirationDate) {
		stack.ExpireAt = expirationDate
		changed = true
	}
	patchedExpire, expireChanged := applyCurrentItemListExpireTimeRaw(stack, expirationDate)
	stack = patchedExpire
	changed = changed || expireChanged
	if definition.Kind == dungeonDropItemStackable {
		patched, patchedChanged := applyCurrentPVFStackableUsePeriodAt(stack, expirationDate, now)
		stack = patched
		changed = changed || patchedChanged
	}
	return stack, changed
}

func applyCurrentPVFItemExpiration(stack dnfrepo.ItemStack, definition dungeonDropItemDefinition) (dnfrepo.ItemStack, bool) {
	return applyCurrentPVFItemExpirationAt(stack, definition, time.Now().UTC())
}

func normalizeCurrentPVFItemStack(stack dnfrepo.ItemStack, definition dungeonDropItemDefinition, now time.Time) (dnfrepo.ItemStack, bool) {
	return applyCurrentPVFItemExpirationAt(stack, definition, now)
}

func normalizeCurrentPVFEquipmentEntry(entry dnfrepo.EquipmentEntry, definition dungeonDropItemDefinition, now time.Time) (dnfrepo.EquipmentEntry, bool) {
	stack, changed := applyCurrentPVFItemExpirationAt(dnfrepo.ItemStack{
		ItemID:   entry.ItemID,
		Bind:     entry.Bind,
		ExpireAt: entry.ExpireAt,
		RawEntry: entry.RawEntry,
		Extra:    entry.Extra,
	}, definition, now)
	entry.ExpireAt = stack.ExpireAt
	entry.RawEntry = stack.RawEntry
	entry.Extra = stack.Extra
	return entry, changed
}
