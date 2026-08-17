package inventory

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var ErrRandomOptionResolverRequired = errors.New("random-option mutation requires a runtime-PVF resolver")

const (
	randomOptionCountOffset        = 0x47
	randomOptionTypeOffset         = 0x48
	randomOptionValue1Offset       = 0x4B
	randomOptionValue2Offset       = 0x4E
	randomOptionStateOffset        = 0x51
	randomOptionChangedIndexOffset = 0x52
	randomOptionCandidateOffset    = 0x53
	randomOptionMaximumCount       = 3
)

type RandomOptionValue struct {
	Type   byte
	Value1 byte
	Value2 byte
}

type RandomOptionMutationResult struct {
	CharacterID     string
	Success         bool
	Mode            string
	TargetSlotIndex int16
	TargetItemID    int64
	OptionIndex     byte
	Options         []RandomOptionValue
	GoldCost        int64
	UpdatedGold     int64
	UpdatedStack    dnfrepo.ItemStack
	Changed         bool
}

func (o *Owner) UnsealRandomOption(ctx context.Context, cmd Command, resolver alignedcmd.RandomOptionResolver) (RandomOptionMutationResult, error) {
	return o.mutateRandomOption(ctx, cmd, resolver)
}

func (o *Owner) ChangeRandomOption(ctx context.Context, cmd Command, resolver alignedcmd.RandomOptionResolver) (RandomOptionMutationResult, error) {
	return o.mutateRandomOption(ctx, cmd, resolver)
}

func (o *Owner) mutateRandomOption(ctx context.Context, cmd Command, resolver alignedcmd.RandomOptionResolver) (RandomOptionMutationResult, error) {
	base := RandomOptionMutationResult{
		Mode:            cmd.Operation,
		TargetSlotIndex: cmd.TargetSlotIndex,
		OptionIndex:     cmd.RandomOptionIndex,
	}
	if o == nil || o.repo == nil || o.characters == nil {
		return RandomOptionMutationResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return RandomOptionMutationResult{}, ErrCharacterRequired
	}
	if resolver == nil {
		return RandomOptionMutationResult{}, ErrRandomOptionResolverRequired
	}
	if cmd.Operation != "unseal_random_option" && cmd.Operation != "change_random_option" {
		return RandomOptionMutationResult{}, fmt.Errorf("unsupported random-option operation %q", cmd.Operation)
	}
	if cmd.TargetSlotIndex < 0 {
		return base, nil
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	base.CharacterID = characterID
	if !o.inItemTransaction {
		var result RandomOptionMutationResult
		err := o.withinCharacterAssetTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.mutateRandomOption(ctx, cmd, resolver)
			return err
		})
		if err != nil {
			return RandomOptionMutationResult{}, err
		}
		if result.CharacterID == "" {
			result = base
		}
		return result, nil
	}

	record, found, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return RandomOptionMutationResult{}, err
	}
	if !found {
		return RandomOptionMutationResult{}, ErrInventoryNotFound
	}
	record = dnfrepo.CloneInventory(record)
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}

	result := base
	targetKey := slotKey(listTypeMain, cmd.TargetSlotIndex)
	target, found := record.Slots[targetKey]
	if !found || target.Count <= 0 || target.ItemID <= 0 || isEquipmentLocked(target) {
		return result, nil
	}
	result.TargetItemID = target.ItemID

	resolution, err := resolver(target.ItemID)
	if err != nil {
		return RandomOptionMutationResult{}, err
	}
	if !resolution.Eligible || !strings.EqualFold(strings.TrimSpace(resolution.TargetKind), "equipment") || strings.TrimSpace(resolution.TargetPVFPath) == "" {
		return result, nil
	}
	if resolution.TargetMinimumLevel < 0 || (resolution.TargetRarity != 2 && resolution.TargetRarity != 3) {
		return result, nil
	}

	raw := currentRawEntryForStack(cmd.TargetSlotIndex, target)
	if len(raw) != currentItemListEntrySize {
		return RandomOptionMutationResult{}, fmt.Errorf("random-option target row length %d, want %d", len(raw), currentItemListEntrySize)
	}
	current, valid := randomOptionsFromRaw(raw)
	var next []RandomOptionValue
	var goldCost int64
	switch cmd.Operation {
	case "unseal_random_option":
		if valid && len(current) > 0 {
			return result, nil
		}
		quantity, err := rollRandomOptionQuantity(resolution.QuantityWeights)
		if err != nil {
			return RandomOptionMutationResult{}, err
		}
		if quantity == 0 || int(quantity) > len(resolution.InitialGroups) || quantity > randomOptionMaximumCount {
			return result, nil
		}
		next, err = rollRandomOptionSet(resolution.InitialGroups[:quantity], nil)
		if err != nil {
			return RandomOptionMutationResult{}, err
		}
		goldCost = resolution.BreakSealGoldCost
	case "change_random_option":
		if !valid || len(current) == 0 || int(cmd.RandomOptionIndex) >= len(current) || int(cmd.RandomOptionIndex) >= len(resolution.ModifiedGroups) {
			return result, nil
		}
		used := make(map[byte]struct{}, len(current)-1)
		for index, option := range current {
			if index != int(cmd.RandomOptionIndex) {
				used[option.Type] = struct{}{}
			}
		}
		candidate, err := rollRandomOptionCandidate(resolution.ModifiedGroups[cmd.RandomOptionIndex], used)
		if err != nil {
			return RandomOptionMutationResult{}, err
		}
		next = append([]RandomOptionValue(nil), current...)
		next[cmd.RandomOptionIndex] = candidate
		goldCost = resolution.ModificationGoldCost
	}
	if goldCost < 0 {
		return RandomOptionMutationResult{}, fmt.Errorf("random-option negative PVF gold cost %d", goldCost)
	}

	character, found, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return RandomOptionMutationResult{}, err
	}
	if !found {
		return RandomOptionMutationResult{}, fmt.Errorf("%w: character=%s", ErrWalletTxnRequired, characterID)
	}
	if character.Stats == nil {
		character.Stats = make(map[string]int64, 1)
	}
	result.UpdatedGold = character.Stats["gold"]
	result.GoldCost = goldCost
	if result.UpdatedGold < goldCost {
		return result, nil
	}

	target = cloneStack(target)
	setRandomOptions(&target, cmd.TargetSlotIndex, next)
	record.Slots[targetKey] = target
	character = dnfrepo.CloneCharacter(character)
	if character.Stats == nil {
		character.Stats = make(map[string]int64, 1)
	}
	character.Stats["gold"] -= goldCost
	character.UpdatedAt = time.Now()
	if err := dnfrepo.SaveCharacterFields(ctx, o.characters, character, dnfrepo.CharacterFieldStats); err != nil {
		return RandomOptionMutationResult{}, err
	}
	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, dnfrepo.InventoryFieldSlots); err != nil {
		return RandomOptionMutationResult{}, err
	}

	result.Success = true
	result.Options = append([]RandomOptionValue(nil), next...)
	result.UpdatedGold = character.Stats["gold"]
	result.UpdatedStack = cloneStack(target)
	result.Changed = true
	return result, nil
}

func randomOptionsFromRaw(raw []byte) ([]RandomOptionValue, bool) {
	if len(raw) != currentItemListEntrySize {
		return nil, false
	}
	count := int(raw[randomOptionCountOffset])
	if count < 0 || count > randomOptionMaximumCount {
		return nil, false
	}
	options := make([]RandomOptionValue, 0, count)
	seen := make(map[byte]struct{}, count)
	for index := 0; index < count; index++ {
		option := RandomOptionValue{
			Type:   raw[randomOptionTypeOffset+index],
			Value1: raw[randomOptionValue1Offset+index],
			Value2: raw[randomOptionValue2Offset+index],
		}
		if option.Type == 0 {
			return nil, false
		}
		if _, duplicate := seen[option.Type]; duplicate {
			return nil, false
		}
		seen[option.Type] = struct{}{}
		options = append(options, option)
	}
	return options, true
}

func setRandomOptions(stack *dnfrepo.ItemStack, slot int16, options []RandomOptionValue) {
	if stack == nil || len(options) == 0 || len(options) > randomOptionMaximumCount {
		return
	}
	raw := currentRawEntryForStack(slot, *stack)
	for index := randomOptionCountOffset; index <= randomOptionCandidateOffset+3; index++ {
		raw[index] = 0
	}
	raw[randomOptionCountOffset] = byte(len(options))
	for index, option := range options {
		raw[randomOptionTypeOffset+index] = option.Type
		raw[randomOptionValue1Offset+index] = option.Value1
		raw[randomOptionValue2Offset+index] = option.Value2
	}
	raw[randomOptionStateOffset] = 0
	raw[randomOptionChangedIndexOffset] = math.MaxUint8
	stack.RawEntry = raw
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 16)
	}
	stack.Extra["random_option_count"] = strconv.Itoa(len(options))
	stack.Extra["random_option_state"] = "0"
	stack.Extra["random_option_changed_index"] = "255"
	for index := 0; index < randomOptionMaximumCount; index++ {
		prefix := fmt.Sprintf("random_option_%d_", index+1)
		if index < len(options) {
			stack.Extra[prefix+"type"] = strconv.Itoa(int(options[index].Type))
			stack.Extra[prefix+"value1"] = strconv.Itoa(int(options[index].Value1))
			stack.Extra[prefix+"value2"] = strconv.Itoa(int(options[index].Value2))
		} else {
			stack.Extra[prefix+"type"] = "0"
			stack.Extra[prefix+"value1"] = "0"
			stack.Extra[prefix+"value2"] = "0"
		}
	}
}

func rollRandomOptionQuantity(entries []alignedcmd.RandomOptionWeightedQuantity) (byte, error) {
	total := int64(0)
	for _, entry := range entries {
		if entry.Quantity == 0 || entry.Quantity > randomOptionMaximumCount || entry.Weight <= 0 {
			continue
		}
		if entry.Weight > math.MaxInt64-total {
			return 0, fmt.Errorf("random-option quantity weights overflow")
		}
		total += entry.Weight
	}
	roll, err := secureRandomOptionInt(total)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if entry.Quantity == 0 || entry.Quantity > randomOptionMaximumCount || entry.Weight <= 0 {
			continue
		}
		if roll < entry.Weight {
			return entry.Quantity, nil
		}
		roll -= entry.Weight
	}
	return 0, fmt.Errorf("random-option quantity selection exhausted")
}

func rollRandomOptionSet(groups [][]alignedcmd.RandomOptionCandidate, used map[byte]struct{}) ([]RandomOptionValue, error) {
	if used == nil {
		used = make(map[byte]struct{}, len(groups))
	}
	options := make([]RandomOptionValue, 0, len(groups))
	for _, group := range groups {
		option, err := rollRandomOptionCandidate(group, used)
		if err != nil {
			return nil, err
		}
		used[option.Type] = struct{}{}
		options = append(options, option)
	}
	return options, nil
}

func rollRandomOptionCandidate(entries []alignedcmd.RandomOptionCandidate, excluded map[byte]struct{}) (RandomOptionValue, error) {
	total := int64(0)
	for _, entry := range entries {
		if entry.Type == 0 || entry.Weight <= 0 {
			continue
		}
		if _, skip := excluded[entry.Type]; skip {
			continue
		}
		if entry.Weight > math.MaxInt64-total {
			return RandomOptionValue{}, fmt.Errorf("random-option candidate weights overflow")
		}
		total += entry.Weight
	}
	roll, err := secureRandomOptionInt(total)
	if err != nil {
		return RandomOptionValue{}, err
	}
	for _, entry := range entries {
		if entry.Type == 0 || entry.Weight <= 0 {
			continue
		}
		if _, skip := excluded[entry.Type]; skip {
			continue
		}
		if roll < entry.Weight {
			return RandomOptionValue{Type: entry.Type, Value1: entry.Value1, Value2: entry.Value2}, nil
		}
		roll -= entry.Weight
	}
	return RandomOptionValue{}, fmt.Errorf("random-option candidate selection exhausted")
}

func secureRandomOptionInt(upper int64) (int64, error) {
	if upper <= 0 {
		return 0, fmt.Errorf("random-option random upper bound %d invalid", upper)
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(upper))
	if err != nil {
		return 0, fmt.Errorf("random-option random: %w", err)
	}
	return value.Int64(), nil
}
