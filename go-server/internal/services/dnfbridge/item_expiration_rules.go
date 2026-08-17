package dnfbridge

import (
	"errors"
	"fmt"
	"math"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var errCurrentPVFUsablePeriodOutOfRange = errors.New("dnf runtime PVF usable period exceeds current item wire")

func currentPVFExpirationUnix(expirationDate time.Time) uint32 {
	if expirationDate.IsZero() || expirationDate.Unix() <= 0 {
		return 0
	}
	return sceneInventoryUint32FromInt64(expirationDate.Unix())
}

// currentPVFItemDefinitionForGrantAt resolves a relative PVF [usable period]
// exactly once, when a new item instance is granted.  Reconciliation must not
// call this helper: recomputing it on login would silently extend the item.
// Runtime PVF uses days for this field; the current EXE persists the resulting
// absolute Unix second in the 0x77 row.
func currentPVFItemDefinitionForGrantAt(
	definition dungeonDropItemDefinition,
	now time.Time,
) (dungeonDropItemDefinition, error) {
	if (definition.Kind != dungeonDropItemStackable && definition.Kind != dungeonDropItemEquipment) ||
		definition.UsablePeriodDays <= 0 {
		return definition, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nowUnix := now.Unix()
	if nowUnix <= 0 || nowUnix > math.MaxUint32 ||
		definition.UsablePeriodDays > (math.MaxUint32-nowUnix)/int64((24*time.Hour)/time.Second) {
		return dungeonDropItemDefinition{}, fmt.Errorf(
			"%w: item=%d days=%d now=%d",
			errCurrentPVFUsablePeriodOutOfRange,
			definition.ItemID,
			definition.UsablePeriodDays,
			nowUnix,
		)
	}
	expireUnix := nowUnix + definition.UsablePeriodDays*int64((24*time.Hour)/time.Second)
	definition.ExpirationDate = time.Unix(expireUnix, 0).UTC()
	return definition, nil
}

// currentPVFItemDefinitionForNestedRewardGrantAt resolves the reward's own
// runtime PVF period first. If the reward only has an already-expired static
// event date, but the package/booster instance that produced it carries a
// future per-instance deadline, the child reward inherits that live deadline.
// This keeps newly opened historical event packages from creating already
// expired inner boxes while still rejecting old source items that have no real
// instance deadline.
func currentPVFItemDefinitionForNestedRewardGrantAt(
	definition dungeonDropItemDefinition,
	sourceStack dnfrepo.ItemStack,
	now time.Time,
) (dungeonDropItemDefinition, error) {
	grantDefinition, err := currentPVFItemDefinitionForGrantAt(definition, now)
	if err != nil {
		return dungeonDropItemDefinition{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if grantDefinition.ExpirationDate.IsZero() || now.Before(grantDefinition.ExpirationDate) {
		return grantDefinition, nil
	}
	sourceExpire := currentItemListStackExpire(sourceStack)
	if sourceExpire == 0 || uint64(sourceExpire) <= uint64(now.Unix()) {
		return grantDefinition, nil
	}
	grantDefinition.ExpirationDate = time.Unix(int64(sourceExpire), 0).UTC()
	return grantDefinition, nil
}

func currentItemStackExpirationMatches(stack dnfrepo.ItemStack, expirationDate time.Time) bool {
	actual := currentItemListStackExpire(stack)
	if expirationDate.IsZero() {
		return actual == 0
	}
	expected := expirationDate.Unix()
	return expected > 0 && expected <= math.MaxUint32 && actual == uint32(expected)
}

// currentPVFStackableUsePeriodSeconds mirrors the current client's 0x77-row
// reader. For item model types 26..29, row+0x0B is copied into the item's
// remaining-use-period field and counted down in seconds. The wire field is a
// u16, so a future absolute PVF date is renewed to the largest representable
// session horizon until the final 65,535 seconds.
func currentPVFStackableUsePeriodSeconds(expirationDate time.Time, now time.Time) uint16 {
	if expirationDate.IsZero() {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	remaining := expirationDate.Sub(now)
	if remaining <= 0 {
		return 0
	}
	seconds := int64(remaining / time.Second)
	if remaining%time.Second != 0 {
		seconds++
	}
	if seconds > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(seconds)
}
