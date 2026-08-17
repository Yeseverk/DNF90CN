package revivecoin

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	// WalletItemID is the current inventory wallet row stored at main-list
	// slot 1. Runtime Script.pvf maps it to stackable/coin.stk.
	WalletItemID int64 = 1
	WalletSlot   int16 = 1

	// ConsumableItemID is runtime Script.pvf
	// stackable/cash/coin_general.stk. Unlike WalletItemID, it is a [waste]
	// item whose use grants one wallet coin.
	ConsumableItemID int64 = 42
)

const (
	walletListType               byte = dnfrepo.MainInventoryListType
	walletPVFPath                     = "stackable/coin.stk"
	consumablePVFPath                 = "stackable/cash/coin_general.stk"
	normalizedWasteStackableType      = "[waste]"
)

var (
	ErrInvalidGrant       = errors.New("invalid revive-coin wallet grant")
	ErrWalletSlotConflict = errors.New("revive-coin wallet slot conflict")
	ErrWalletOverflow     = errors.New("revive-coin wallet count overflow")
)

type Consolidation struct {
	Changed                  bool
	RemovedRows              int
	ConvertedConsumableRows  int
	ConvertedConsumableUnits int64
	Total                    int64
}

func WalletKey() string {
	return fmt.Sprintf("%d:%d", walletListType, WalletSlot)
}

// IsReward identifies the two current runtime-PVF reward representations that
// must be routed into the revive-coin wallet. Item 1 is the wallet entity
// itself; item 42 is the one-coin cash consumable.
func IsReward(itemID int64) bool {
	return itemID == WalletItemID || itemID == ConsumableItemID
}

func IsConsumable(stack dnfrepo.ItemStack) bool {
	if stack.ItemID != ConsumableItemID || stack.Extra == nil {
		return false
	}
	path := normalizePath(stack.Extra["pvf_path"])
	stackableType := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(stack.Extra["stackable_type"], "`", "")))
	return path == consumablePVFPath && strings.HasPrefix(stackableType, normalizedWasteStackableType)
}

// Consolidate moves every misplaced main-list item-1 row into the fixed
// main-list slot 1 wallet. It never overwrites an unrelated row occupying the
// reserved wallet slot.
func Consolidate(record *dnfrepo.InventoryRecord) (Consolidation, error) {
	return consolidate(record, false)
}

// MigrateLegacy converts legacy main-list item-42 rows whose stored PVF path is
// exactly cash/coin_general.stk while consolidating the item-1 wallet. Those
// rows were emitted by an older reward path as ordinary backpack stacks, but
// the current PVF/C# wire model treats each unit as one wallet coin; exposing
// them to the client makes its local right-click path stall before op44 is sent.
func MigrateLegacy(record *dnfrepo.InventoryRecord) (Consolidation, error) {
	return consolidate(record, true)
}

func consolidate(record *dnfrepo.InventoryRecord, includeLegacyConsumables bool) (Consolidation, error) {
	if record == nil {
		return Consolidation{}, ErrInvalidGrant
	}
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}

	walletKey := WalletKey()
	if occupied, found := record.Slots[walletKey]; found &&
		occupied.ItemID != WalletItemID &&
		(!includeLegacyConsumables || !isLegacyConsumableWalletRow(occupied)) {
		return Consolidation{}, fmt.Errorf(
			"%w: key=%s item=%d",
			ErrWalletSlotConflict,
			walletKey,
			occupied.ItemID,
		)
	}

	var total int64
	sourceRows := make([]string, 0, 1)
	convertedConsumableRows := 0
	var convertedConsumableUnits int64
	for key, stack := range record.Slots {
		listType, _, ok := parseSlotKey(key)
		if !ok || listType != walletListType {
			continue
		}
		walletRow := stack.ItemID == WalletItemID
		legacyConsumableRow := includeLegacyConsumables && isLegacyConsumableWalletRow(stack)
		if !walletRow && !legacyConsumableRow {
			continue
		}
		if stack.Count < 0 || total > math.MaxInt64-stack.Count {
			return Consolidation{}, fmt.Errorf("%w: key=%s count=%d", ErrWalletOverflow, key, stack.Count)
		}
		total += stack.Count
		sourceRows = append(sourceRows, key)
		if legacyConsumableRow {
			if convertedConsumableUnits > math.MaxInt64-stack.Count {
				return Consolidation{}, fmt.Errorf("%w: key=%s count=%d", ErrWalletOverflow, key, stack.Count)
			}
			convertedConsumableRows++
			convertedConsumableUnits += stack.Count
		}
	}
	if len(sourceRows) == 0 {
		return Consolidation{}, nil
	}

	if len(sourceRows) == 1 && sourceRows[0] == walletKey && convertedConsumableRows == 0 {
		stack := record.Slots[walletKey]
		if walletCountMetadataMatches(stack, total) {
			return Consolidation{Total: total}, nil
		}
		record.Slots[walletKey] = walletStack(total, "revive_coin_wallet_metadata_repair")
		return Consolidation{Changed: true, Total: total}, nil
	}

	for _, key := range sourceRows {
		delete(record.Slots, key)
	}
	record.Slots[walletKey] = walletStack(total, "revive_coin_wallet_consolidation")
	return Consolidation{
		Changed:                  true,
		RemovedRows:              len(sourceRows),
		ConvertedConsumableRows:  convertedConsumableRows,
		ConvertedConsumableUnits: convertedConsumableUnits,
		Total:                    total,
	}, nil
}

// Grant adds count to the fixed wallet after consolidating legacy misplaced
// rows. The caller owns the surrounding repository transaction.
func Grant(record *dnfrepo.InventoryRecord, count int64, source string) (int64, error) {
	if count <= 0 {
		return 0, fmt.Errorf("%w: count=%d", ErrInvalidGrant, count)
	}
	consolidated, err := Consolidate(record)
	if err != nil {
		return 0, err
	}
	if consolidated.Total > math.MaxInt64-count {
		return 0, fmt.Errorf("%w: current=%d grant=%d", ErrWalletOverflow, consolidated.Total, count)
	}
	total := consolidated.Total + count
	record.Slots[WalletKey()] = walletStack(total, source)
	return total, nil
}

func walletStack(total int64, source string) dnfrepo.ItemStack {
	if strings.TrimSpace(source) == "" {
		source = "revive_coin_wallet"
	}
	value := strconv.FormatInt(total, 10)
	return dnfrepo.ItemStack{
		ItemID: WalletItemID,
		Count:  total,
		Extra: map[string]string{
			"source":             source,
			"item_kind":          "stackable",
			"pvf_path":           walletPVFPath,
			"stackable_type":     "[etc]",
			"revive_coin_wallet": "1",
			"amount_or_count":    value,
			"count":              value,
			"value_a":            value,
			"instance_value":     value,
			"durability":         "0",
			"marker_16":          "0",
		},
	}
}

func walletCountMetadataMatches(stack dnfrepo.ItemStack, total int64) bool {
	if stack.ItemID != WalletItemID || stack.Count != total || stack.Extra == nil {
		return false
	}
	want := strconv.FormatInt(total, 10)
	return stack.Extra["amount_or_count"] == want &&
		stack.Extra["count"] == want &&
		stack.Extra["value_a"] == want
}

func isLegacyConsumableWalletRow(stack dnfrepo.ItemStack) bool {
	if stack.ItemID != ConsumableItemID || stack.Extra == nil {
		return false
	}
	return normalizePath(stack.Extra["pvf_path"]) == consumablePVFPath
}

func parseSlotKey(key string) (byte, int16, bool) {
	listRaw, slotRaw, ok := strings.Cut(strings.TrimSpace(key), ":")
	if !ok {
		return 0, 0, false
	}
	listValue, err := strconv.ParseUint(listRaw, 10, 8)
	if err != nil {
		return 0, 0, false
	}
	slotValue, err := strconv.ParseInt(slotRaw, 10, 16)
	if err != nil {
		return 0, 0, false
	}
	return byte(listValue), int16(slotValue), true
}

func normalizePath(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")))
}
