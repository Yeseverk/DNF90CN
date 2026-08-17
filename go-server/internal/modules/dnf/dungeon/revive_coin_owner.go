package dungeon

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrReviveCoinInvalid  = errors.New("dungeon revive coin command is invalid")
	ErrReviveCoinConflict = errors.New("dungeon revive coin inventory state conflicts")
)

type ReviveCoinProjector func(int16, dnfrepo.ItemStack) (dnfrepo.ItemStack, error)

type ReviveCoinCommand struct {
	CharacterID string
	ItemID      int64
	WalletSlot  int16
	AllowFree   bool
	UpdatedAt   time.Time
	Project     ReviveCoinProjector
}

type ReviveCoinResult struct {
	Consumed   bool
	FreeRevive bool
	SlotKey    string
	Slot       int16
	ItemID     int64
	CountAfter int64
	Removed    bool
	Stack      dnfrepo.ItemStack
}

// ConsumeReviveCoin locates the lowest occupied main-list revive-coin slot and
// consumes it inside the character-items transaction. The permanent wallet
// slot is retained at zero; ordinary depleted stacks are removed.
func (o *Owner) ConsumeReviveCoin(ctx context.Context, cmd ReviveCoinCommand) (ReviveCoinResult, error) {
	if o == nil || o.items == nil || strings.TrimSpace(cmd.CharacterID) == "" ||
		cmd.ItemID <= 0 || cmd.WalletSlot < 0 || cmd.Project == nil {
		return ReviveCoinResult{}, ErrReviveCoinInvalid
	}
	ctx = contextOrBackground(ctx)
	now := updatedAtOrNow(cmd.UpdatedAt)

	var result ReviveCoinResult
	err := o.items.WithinCharacterItems(ctx, cmd.CharacterID, func(
		inventories dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
	) error {
		inventory, found, err := inventories.Load(ctx, cmd.CharacterID)
		if err != nil {
			return err
		}
		if !found {
			result.FreeRevive = cmd.AllowFree
			return nil
		}
		inventory = dnfrepo.CloneInventory(inventory)
		if inventory.Slots == nil {
			inventory.Slots = make(map[string]dnfrepo.ItemStack)
		}

		bestKey := ""
		bestSlot := int16(math.MaxInt16)
		var bestStack dnfrepo.ItemStack
		for key, stack := range inventory.Slots {
			slot, parsed := parseMainInventorySlotKey(key)
			if !parsed || stack.ItemID != cmd.ItemID || stack.Count <= 0 {
				continue
			}
			if bestKey == "" || slot < bestSlot {
				bestKey = key
				bestSlot = slot
				bestStack = cloneStack(stack)
			}
		}
		if bestKey == "" {
			result.FreeRevive = cmd.AllowFree
			return nil
		}

		bestStack.Count--
		if bestStack.Extra == nil {
			bestStack.Extra = make(map[string]string, 4)
		}
		bestStack.Extra["count"] = strconv.FormatInt(bestStack.Count, 10)
		bestStack.Extra["amount_or_count"] = strconv.FormatInt(bestStack.Count, 10)
		bestStack.Extra["last_consume_source"] = "dungeon_use_coin_revive"

		result = ReviveCoinResult{
			Consumed:   true,
			SlotKey:    bestKey,
			Slot:       bestSlot,
			ItemID:     cmd.ItemID,
			CountAfter: bestStack.Count,
		}
		if bestStack.Count <= 0 && bestSlot != cmd.WalletSlot {
			delete(inventory.Slots, bestKey)
			result.Removed = true
			result.CountAfter = 0
		} else {
			if bestStack.Count < 0 {
				return ErrReviveCoinConflict
			}
			bestStack, err = cmd.Project(bestSlot, bestStack)
			if err != nil {
				return err
			}
			inventory.Slots[bestKey] = cloneStack(bestStack)
			result.Stack = cloneStack(bestStack)
		}
		inventory.UpdatedAt = now
		return dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots)
	})
	if err != nil {
		return ReviveCoinResult{}, err
	}
	return result, nil
}

func parseMainInventorySlotKey(key string) (int16, bool) {
	listRaw, slotRaw, ok := strings.Cut(strings.TrimSpace(key), ":")
	if !ok {
		return 0, false
	}
	listType, err := strconv.ParseInt(listRaw, 10, 16)
	if err != nil || byte(listType) != dnfrepo.MainInventoryListType {
		return 0, false
	}
	slot, err := strconv.ParseInt(slotRaw, 10, 16)
	if err != nil || slot < 0 {
		return 0, false
	}
	return int16(slot), true
}
