package premium

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrInventoryActivationAccountRequired   = errors.New("premium inventory activation requires an account")
	ErrInventoryActivationInventoryRequired = errors.New("premium inventory activation requires an inventory")
	ErrInventoryActivationContractInvalid   = errors.New("premium inventory activation contract is invalid")
	ErrInventoryActivationDurationOverflow  = errors.New("premium inventory activation duration overflows")
)

// InventoryContract is one runtime-PVF premiumlist item mapping. The bridge
// resolves these values from the active PVF; inventory metadata is never an
// authority for either type or duration.
type InventoryContract struct {
	ItemID          int64
	PremiumType     int64
	DurationSeconds int64
}

// InventoryActivation describes the final account entitlement produced by
// consuming one or more inventory contract stacks of the same premium type.
type InventoryActivation struct {
	PremiumType     int64
	Units           int64
	DurationSeconds int64
	ExpireAt        int64
}

// InventoryActivationResult is the deterministic result of consuming every
// PVF-proven contract item in the selected character's main inventory.
type InventoryActivationResult struct {
	Activations  []InventoryActivation
	RemovedSlots []int16
}

func (r InventoryActivationResult) Changed() bool {
	return len(r.RemovedSlots) != 0
}

type inventoryActivationPlan struct {
	key  string
	slot int16
}

// ActivateInventoryContracts converts all PVF-proven contract stacks in list
// 0 into account expiry time. Validation is completed before either aggregate
// is mutated, so callers can safely use this inside their repository unit of
// work without exposing a partial result.
func ActivateInventoryContracts(
	account *dnfrepo.AccountRecord,
	inventory *dnfrepo.InventoryRecord,
	contracts map[int64]InventoryContract,
	now time.Time,
) (InventoryActivationResult, error) {
	if account == nil || strings.TrimSpace(account.AccountID) == "" {
		return InventoryActivationResult{}, ErrInventoryActivationAccountRequired
	}
	if inventory == nil || strings.TrimSpace(inventory.CharacterID) == "" {
		return InventoryActivationResult{}, ErrInventoryActivationInventoryRequired
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	plans := make([]inventoryActivationPlan, 0)
	secondsByType := make(map[int64]int64)
	unitsByType := make(map[int64]int64)
	for key, stack := range inventory.Slots {
		slot, main := premiumMainInventorySlot(key)
		if !main || stack.ItemID <= 0 || stack.Count <= 0 {
			continue
		}
		contract, exists := contracts[stack.ItemID]
		if !exists {
			continue
		}
		if contract.ItemID != stack.ItemID || contract.PremiumType <= 0 || contract.DurationSeconds <= 0 {
			return InventoryActivationResult{}, ErrInventoryActivationContractInvalid
		}
		if stack.Count > math.MaxInt64/contract.DurationSeconds {
			return InventoryActivationResult{}, ErrInventoryActivationDurationOverflow
		}
		delta := stack.Count * contract.DurationSeconds
		if secondsByType[contract.PremiumType] > math.MaxInt64-delta ||
			unitsByType[contract.PremiumType] > math.MaxInt64-stack.Count {
			return InventoryActivationResult{}, ErrInventoryActivationDurationOverflow
		}
		secondsByType[contract.PremiumType] += delta
		unitsByType[contract.PremiumType] += stack.Count
		plans = append(plans, inventoryActivationPlan{key: key, slot: slot})
	}
	if len(plans) == 0 {
		return InventoryActivationResult{}, nil
	}

	for premiumType, seconds := range secondsByType {
		base := now.Unix()
		if current := ExpireAt(*account, premiumType); current > base {
			base = current
		}
		if seconds > math.MaxInt64-base {
			return InventoryActivationResult{}, ErrInventoryActivationDurationOverflow
		}
	}

	sort.Slice(plans, func(i, j int) bool {
		if plans[i].slot == plans[j].slot {
			return plans[i].key < plans[j].key
		}
		return plans[i].slot < plans[j].slot
	})
	result := InventoryActivationResult{RemovedSlots: make([]int16, 0, len(plans))}
	for _, plan := range plans {
		delete(inventory.Slots, plan.key)
		result.RemovedSlots = append(result.RemovedSlots, plan.slot)
	}
	types := make([]int64, 0, len(secondsByType))
	for premiumType := range secondsByType {
		types = append(types, premiumType)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	for _, premiumType := range types {
		Upsert(account, premiumType, secondsByType[premiumType], 1, now)
		result.Activations = append(result.Activations, InventoryActivation{
			PremiumType:     premiumType,
			Units:           unitsByType[premiumType],
			DurationSeconds: secondsByType[premiumType],
			ExpireAt:        ExpireAt(*account, premiumType),
		})
	}
	return result, nil
}

func premiumMainInventorySlot(key string) (int16, bool) {
	if !strings.HasPrefix(key, "0:") {
		return 0, false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(key, "0:"))
	value, err := strconv.ParseInt(raw, 10, 16)
	if err != nil || value < 0 {
		return 0, false
	}
	return int16(value), true
}
