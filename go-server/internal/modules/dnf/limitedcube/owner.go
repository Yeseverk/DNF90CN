// Package limitedcube owns the durable mutation for PVF [limited cube]
// stackables. Wire dispatch and PVF parsing remain in dnfbridge.
package limitedcube

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable      = errors.New("limited-cube owner unavailable")
	ErrAccountRequired       = errors.New("limited-cube account id required")
	ErrCharacterRequired     = errors.New("limited-cube character id required")
	ErrInventoryNotFound     = errors.New("limited-cube inventory record not found")
	ErrPolicyInvalid         = errors.New("limited-cube PVF policy is invalid")
	ErrTicketMissing         = errors.New("limited-cube ticket is missing")
	ErrTicketMismatch        = errors.New("limited-cube ticket does not match request")
	ErrTicketLocked          = errors.New("limited-cube ticket is locked")
	ErrConditionItemMissing  = errors.New("limited-cube condition item is missing")
	ErrConditionItemMismatch = errors.New("limited-cube condition item does not match request")
	ErrConditionItemLocked   = errors.New("limited-cube condition item is locked")
	ErrMaterialInsufficient  = errors.New("limited-cube material is insufficient")
	ErrResultSelectionFailed = errors.New("limited-cube result selection failed")
)

const (
	mainInventoryListType byte  = 0
	consumeTicketCount    int64 = 1
)

// Requirement is one PVF item/count requirement from a condition section.
type Requirement struct {
	ItemID int64
	Count  int64
}

// WeightedResult is one exact [result item] candidate. Stack carries the
// resolved PVF instance metadata prepared by the bridge; its count is the
// PVF result count rather than client-controlled data.
type WeightedResult struct {
	Stack  dnfrepo.ItemStack
	Weight int64
}

// Policy is the fully resolved, server-authoritative rule set for one ticket.
// The bridge builds it exclusively from the active PVF document.
type Policy struct {
	TicketItemID int64
	Conditions   []Requirement
	Materials    []Requirement
	Results      []WeightedResult
}

// Command is the trusted portion of the current-client op338 request. The
// client sends the selected bead slot and ID first, then the ticket slot.
type Command struct {
	AccountID    string
	CharacterID  string
	TicketSlot   int16
	TicketItemID int64
	TargetSlot   int16
	TargetItemID int64
}

// Result reports the committed inventory changes. CharacterChangedSlots and
// AccountChangedSlots remain at their native list-0 wire addresses, but have
// distinct durable owners. Both are sorted for deterministic client refresh.
type Result struct {
	TicketSlot          int16
	TicketItemID        int64
	TicketRemaining     int64
	InputSlot           int16
	InputItemID         int64
	ResultItemID        int64
	ChangedSlots        []int16
	AccountChangedSlots []int16
}

// Owner executes one limited-cube change in the account-and-character item
// transaction supplied by the repository group.
type Owner struct {
	items dnfrepo.AccountCharacterItemUnitOfWork
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.Inventory == nil || repositories.AccountInventory == nil || repositories.AccountItems == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{items: repositories.AccountItems}, nil
}

// Use consumes one ticket, the client-selected A-condition bead, and all
// B-condition material before replacing that bead with one weighted PVF
// result. Binding and an existing instance deadline stay with the transformed
// item; result-template metadata replaces the old template metadata.
func (o *Owner) Use(ctx context.Context, command Command, policy Policy) (Result, error) {
	if o == nil || o.items == nil {
		return Result{}, ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(command.AccountID)
	if accountID == "" {
		return Result{}, ErrAccountRequired
	}
	characterID := strings.TrimSpace(command.CharacterID)
	if characterID == "" {
		return Result{}, ErrCharacterRequired
	}
	if err := validatePolicy(policy); err != nil {
		return Result{}, err
	}
	if command.TicketSlot < 0 || command.TargetSlot < 0 || command.TicketSlot == command.TargetSlot ||
		command.TicketItemID != policy.TicketItemID {
		return Result{}, fmt.Errorf("%w: slot=%d item=%d policy_item=%d", ErrTicketMismatch, command.TicketSlot, command.TicketItemID, policy.TicketItemID)
	}

	var result Result
	err := o.items.WithinAccountCharacterItems(ctx, accountID, characterID, func(accounts dnfrepo.AccountInventoryRepository, inventory dnfrepo.InventoryRepository) error {
		record, found, err := inventory.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrInventoryNotFound
		}
		record = dnfrepo.CloneInventory(record)
		record.CharacterID = characterID
		if record.Slots == nil {
			return ErrTicketMissing
		}
		account, accountFound, err := accounts.Load(ctx, accountID)
		if err != nil {
			return err
		}
		if !accountFound {
			account = dnfrepo.AccountInventoryRecord{AccountID: accountID}
		}
		account = dnfrepo.CloneAccountInventory(account)
		account.AccountID = accountID
		if account.Slots == nil {
			account.Slots = make(map[string]dnfrepo.ItemStack)
		}

		ticketKey := inventorySlotKey(command.TicketSlot)
		ticket, found := record.Slots[ticketKey]
		if !found || ticket.Count <= 0 {
			return fmt.Errorf("%w: slot=%d", ErrTicketMissing, command.TicketSlot)
		}
		if ticket.ItemID != command.TicketItemID {
			return fmt.Errorf("%w: slot=%d want=%d got=%d", ErrTicketMismatch, command.TicketSlot, command.TicketItemID, ticket.ItemID)
		}
		if limitedCubeItemLocked(ticket) {
			return fmt.Errorf("%w: slot=%d", ErrTicketLocked, command.TicketSlot)
		}

		inputKey := inventorySlotKey(command.TargetSlot)
		input, found := record.Slots[inputKey]
		if !found || input.Count <= 0 {
			return fmt.Errorf("%w: slot=%d", ErrConditionItemMissing, command.TargetSlot)
		}
		if input.ItemID != command.TargetItemID {
			return fmt.Errorf("%w: slot=%d want=%d got=%d", ErrConditionItemMismatch, command.TargetSlot, command.TargetItemID, input.ItemID)
		}
		if limitedCubeItemLocked(input) {
			return fmt.Errorf("%w: slot=%d", ErrConditionItemLocked, command.TargetSlot)
		}
		conditionCounts, err := normalizedRequirements(policy.Conditions)
		if err != nil {
			return err
		}
		requiredCount, allowed := conditionCounts[input.ItemID]
		if !allowed || input.Count != requiredCount {
			return fmt.Errorf("%w: slot=%d item=%d count=%d required=%d", ErrConditionItemMismatch, command.TargetSlot, input.ItemID, input.Count, requiredCount)
		}
		materialUses, err := planMaterialConsumption(record.Slots, account.Slots, policy.Materials, ticketKey, inputKey)
		if err != nil {
			return err
		}
		selected, err := selectWeightedResult(policy.Results, input.ItemID)
		if err != nil {
			return err
		}

		ticketRemaining := ticket.Count - consumeTicketCount
		if ticketRemaining == 0 {
			delete(record.Slots, ticketKey)
		} else {
			ticket.Count = ticketRemaining
			record.Slots[ticketKey] = ticket
		}
		for _, use := range materialUses {
			items := record.Slots
			if use.accountOwned {
				items = account.Slots
			}
			stack := items[use.key]
			stack.Count -= use.count
			if stack.Count <= 0 {
				delete(items, use.key)
				continue
			}
			items[use.key] = stack
		}

		resultStack := cloneStack(selected.Stack)
		resultStack.Bind = input.Bind
		if resultStack.ExpireAt.IsZero() {
			resultStack.ExpireAt = input.ExpireAt
		}
		// A new item template cannot reuse the old raw item row: item-specific
		// fields in that row would otherwise survive a PVF template change.
		resultStack.RawEntry = nil
		record.Slots[inputKey] = resultStack

		now := time.Now().UTC()
		record.UpdatedAt = now
		if err := dnfrepo.SaveInventoryFields(ctx, inventory, record, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}
		changed := map[int16]struct{}{command.TicketSlot: {}, command.TargetSlot: {}}
		accountChanged := make(map[int16]struct{})
		for _, use := range materialUses {
			if use.accountOwned {
				accountChanged[use.slot] = struct{}{}
			} else {
				changed[use.slot] = struct{}{}
			}
		}
		if len(accountChanged) > 0 {
			account.UpdatedAt = now
			if err := accounts.Save(ctx, account); err != nil {
				return err
			}
		}
		result = Result{
			TicketSlot:          command.TicketSlot,
			TicketItemID:        command.TicketItemID,
			TicketRemaining:     ticketRemaining,
			InputSlot:           command.TargetSlot,
			InputItemID:         input.ItemID,
			ResultItemID:        resultStack.ItemID,
			ChangedSlots:        sortedSlots(changed),
			AccountChangedSlots: sortedSlots(accountChanged),
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func validatePolicy(policy Policy) error {
	if policy.TicketItemID <= 0 || len(policy.Conditions) == 0 || len(policy.Results) == 0 {
		return ErrPolicyInvalid
	}
	if _, err := normalizedRequirements(policy.Conditions); err != nil {
		return err
	}
	if _, err := normalizedRequirements(policy.Materials); err != nil {
		return err
	}
	var total int64
	for _, candidate := range policy.Results {
		if candidate.Stack.ItemID <= 0 || candidate.Stack.Count <= 0 || candidate.Weight <= 0 || candidate.Weight > math.MaxInt64-total {
			return ErrPolicyInvalid
		}
		total += candidate.Weight
	}
	if total <= 0 {
		return ErrPolicyInvalid
	}
	return nil
}

func normalizedRequirements(requirements []Requirement) (map[int64]int64, error) {
	out := make(map[int64]int64, len(requirements))
	for _, requirement := range requirements {
		if requirement.ItemID <= 0 || requirement.Count <= 0 || requirement.Count > math.MaxInt64-out[requirement.ItemID] {
			return nil, ErrPolicyInvalid
		}
		out[requirement.ItemID] += requirement.Count
	}
	return out, nil
}

type materialUse struct {
	key          string
	slot         int16
	count        int64
	accountOwned bool
}

func planMaterialConsumption(
	characterSlots map[string]dnfrepo.ItemStack,
	accountSlots map[string]dnfrepo.ItemStack,
	materials []Requirement,
	excludedKeys ...string,
) ([]materialUse, error) {
	needed, err := normalizedRequirements(materials)
	if err != nil {
		return nil, err
	}
	if len(needed) == 0 {
		return nil, nil
	}
	excluded := make(map[string]struct{}, len(excludedKeys))
	for _, key := range excludedKeys {
		excluded[key] = struct{}{}
	}
	type candidate struct {
		key          string
		slot         int16
		stack        dnfrepo.ItemStack
		accountOwned bool
	}
	characterCandidates := make([]candidate, 0, len(characterSlots))
	for key, stack := range characterSlots {
		if _, skip := excluded[key]; skip {
			continue
		}
		listType, slot, ok := parseInventorySlotKey(key)
		if !ok || listType != mainInventoryListType || dnfrepo.IsAccountSharedInventorySlot(listType, slot) ||
			stack.Count <= 0 || limitedCubeItemLocked(stack) {
			continue
		}
		if _, need := needed[stack.ItemID]; need {
			characterCandidates = append(characterCandidates, candidate{key: key, slot: slot, stack: stack})
		}
	}
	accountCandidates := make([]candidate, 0, len(accountSlots))
	for key, stack := range accountSlots {
		listType, slot, ok := parseInventorySlotKey(key)
		if !ok || !dnfrepo.IsAccountSharedInventorySlot(listType, slot) || stack.Count <= 0 || limitedCubeItemLocked(stack) {
			continue
		}
		if _, need := needed[stack.ItemID]; need {
			accountCandidates = append(accountCandidates, candidate{key: key, slot: slot, stack: stack, accountOwned: true})
		}
	}
	sort.Slice(characterCandidates, func(i, j int) bool { return characterCandidates[i].slot < characterCandidates[j].slot })
	sort.Slice(accountCandidates, func(i, j int) bool { return accountCandidates[i].slot < accountCandidates[j].slot })
	candidates := append(characterCandidates, accountCandidates...)
	uses := make([]materialUse, 0, len(candidates))
	for _, candidate := range candidates {
		remaining := needed[candidate.stack.ItemID]
		if remaining <= 0 {
			continue
		}
		consume := minInt64(candidate.stack.Count, remaining)
		if consume <= 0 {
			continue
		}
		uses = append(uses, materialUse{
			key:          candidate.key,
			slot:         candidate.slot,
			count:        consume,
			accountOwned: candidate.accountOwned,
		})
		needed[candidate.stack.ItemID] -= consume
	}
	for itemID, count := range needed {
		if count > 0 {
			return nil, fmt.Errorf("%w: item=%d count=%d", ErrMaterialInsufficient, itemID, count)
		}
	}
	return uses, nil
}

// selectWeightedResult excludes the input item so a limited-cube use always
// changes the eligible item's PVF template rather than consuming resources for
// an identical result.
func selectWeightedResult(candidates []WeightedResult, excludedItemID int64) (WeightedResult, error) {
	var total int64
	for _, candidate := range candidates {
		if candidate.Stack.ItemID == excludedItemID {
			continue
		}
		if candidate.Stack.ItemID <= 0 || candidate.Stack.Count <= 0 || candidate.Weight <= 0 || candidate.Weight > math.MaxInt64-total {
			return WeightedResult{}, ErrResultSelectionFailed
		}
		total += candidate.Weight
	}
	if total <= 0 {
		return WeightedResult{}, ErrResultSelectionFailed
	}
	roll, err := cryptorand.Int(cryptorand.Reader, big.NewInt(total))
	if err != nil {
		return WeightedResult{}, fmt.Errorf("%w: %v", ErrResultSelectionFailed, err)
	}
	value := roll.Int64()
	for _, candidate := range candidates {
		if candidate.Stack.ItemID == excludedItemID {
			continue
		}
		if value < candidate.Weight {
			return candidate, nil
		}
		value -= candidate.Weight
	}
	return WeightedResult{}, ErrResultSelectionFailed
}

func inventorySlotKey(slot int16) string {
	return "0:" + strconv.FormatInt(int64(slot), 10)
}

func parseInventorySlotKey(key string) (byte, int16, bool) {
	var list, slot int64
	if _, err := fmt.Sscanf(key, "%d:%d", &list, &slot); err != nil || list < 0 || list > math.MaxUint8 || slot < math.MinInt16 || slot > math.MaxInt16 {
		return 0, 0, false
	}
	return byte(list), int16(slot), true
}

func sortedSlots(slots map[int16]struct{}) []int16 {
	out := make([]int16, 0, len(slots))
	for slot := range slots {
		out = append(out, slot)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func cloneStack(stack dnfrepo.ItemStack) dnfrepo.ItemStack {
	stack.RawEntry = append([]byte(nil), stack.RawEntry...)
	if len(stack.Extra) > 0 {
		extra := make(map[string]string, len(stack.Extra))
		for key, value := range stack.Extra {
			extra[key] = value
		}
		stack.Extra = extra
	}
	return stack
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func limitedCubeItemLocked(stack dnfrepo.ItemStack) bool {
	if stack.Extra == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(stack.Extra["equipment_lock_state"])) {
	case "1", "2", "active", "locked", "unlocking", "pending_unlock":
		return true
	default:
		return false
	}
}
