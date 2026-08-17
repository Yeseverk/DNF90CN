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

var ErrAmplifyItemResolverRequired = errors.New("amplification mutation requires a runtime-PVF resolver")

const (
	investAmplifyErrorUnsupported      byte = 8
	investAmplifyErrorInvalid          byte = 17
	investAmplifyErrorAlreadyUpgraded  byte = 18
	investAmplifyErrorAlreadyHasOption byte = 20
	investAmplifyErrorNoOption         byte = 21
	investAmplifyErrorSameOption       byte = 23
	unidentifiedAmplifyFlag            byte = 0x80
	amplifyOptionAll                   byte = 5
)

type AmplifyMutationResult struct {
	CharacterID            string
	Success                bool
	ErrorCode              byte
	Mode                   string
	Action                 byte
	TargetSlotIndex        int16
	TargetItemID           int64
	MaterialSlotIndex      int16
	MaterialItemID         int64
	MaterialRemainingCount int64
	AmplifyType            byte
	AmplifyValue           uint16
	AmplifyLevel           byte
	Changed                bool
}

func (o *Owner) PurifyAmplifyItem(ctx context.Context, cmd Command, resolver alignedcmd.AmplifyItemResolver) (AmplifyMutationResult, error) {
	return o.mutateAmplifyItem(ctx, cmd, resolver)
}

func (o *Owner) InvestAmplifyOption(ctx context.Context, cmd Command, resolver alignedcmd.AmplifyItemResolver) (AmplifyMutationResult, error) {
	return o.mutateAmplifyItem(ctx, cmd, resolver)
}

func (o *Owner) mutateAmplifyItem(ctx context.Context, cmd Command, resolver alignedcmd.AmplifyItemResolver) (AmplifyMutationResult, error) {
	base := AmplifyMutationResult{
		ErrorCode:         investAmplifyErrorInvalid,
		Action:            cmd.AmplifyAction,
		TargetSlotIndex:   cmd.TargetSlotIndex,
		MaterialSlotIndex: cmd.MaterialSlotIndex,
	}
	if o == nil || o.repo == nil {
		return AmplifyMutationResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return AmplifyMutationResult{}, ErrCharacterRequired
	}
	if resolver == nil {
		return AmplifyMutationResult{}, ErrAmplifyItemResolverRequired
	}
	if cmd.Operation != "purify_item" && cmd.Operation != "invest_item_amplify_option" {
		return AmplifyMutationResult{}, fmt.Errorf("unsupported amplification operation %q", cmd.Operation)
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	base.CharacterID = characterID
	if !o.inItemTransaction {
		var result AmplifyMutationResult
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.mutateAmplifyItem(ctx, cmd, resolver)
			return err
		})
		if err != nil {
			return AmplifyMutationResult{}, err
		}
		if result.CharacterID == "" {
			result = base
		}
		return result, nil
	}

	record, found, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return AmplifyMutationResult{}, err
	}
	if !found {
		return AmplifyMutationResult{}, ErrInventoryNotFound
	}
	record = dnfrepo.CloneInventory(record)
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}

	result := base
	if cmd.TargetSlotIndex < 0 || cmd.MaterialSlotIndex < 0 || cmd.TargetSlotIndex == cmd.MaterialSlotIndex || cmd.TargetItemTemplateID <= 0 || cmd.MaterialItemTemplateID <= 0 {
		return result, nil
	}
	targetKey := slotKey(listTypeMain, cmd.TargetSlotIndex)
	target, found := record.Slots[targetKey]
	if !found || target.Count <= 0 || target.ItemID != int64(cmd.TargetItemTemplateID) {
		return result, nil
	}
	result.TargetItemID = target.ItemID
	if isEquipmentLocked(target) {
		return result, nil
	}
	materialKey := slotKey(listTypeMain, cmd.MaterialSlotIndex)
	material, found := record.Slots[materialKey]
	if !found || material.Count <= 0 || material.ItemID != int64(cmd.MaterialItemTemplateID) {
		return result, nil
	}
	result.MaterialItemID = material.ItemID

	resolution, err := resolver(material.ItemID, target.ItemID)
	if err != nil {
		return AmplifyMutationResult{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(resolution.TargetKind), "equipment") || strings.TrimSpace(resolution.TargetPVFPath) == "" || strings.TrimSpace(resolution.MaterialPVFPath) == "" {
		result.ErrorCode = investAmplifyErrorUnsupported
		return result, nil
	}
	if resolution.EquipLevelConst <= 0 || resolution.TargetMinimumLevel < resolution.EquipLevelConst || resolution.TargetRarity < 2 {
		result.ErrorCode = investAmplifyErrorUnsupported
		return result, nil
	}

	currentType, _ := upgradeAmplifyState(target)
	currentLevel := upgradeLevelOf(target)
	consumeCount := int64(0)
	newType := byte(0)
	newValue := uint16(0)
	newLevel := currentLevel
	mode := ""

	if cmd.Operation == "purify_item" {
		if currentType&unidentifiedAmplifyFlag == 0 {
			return result, nil
		}
		switch {
		case resolution.PurifyMaterialCount > 0:
			consumeCount = resolution.PurifyMaterialCount
			rolled, err := secureAmplifyRandom(4)
			if err != nil {
				return AmplifyMutationResult{}, err
			}
			newType = byte(rolled + 1)
			newValue = resolution.InitialValues[newType]
			if newValue == 0 {
				result.ErrorCode = investAmplifyErrorUnsupported
				return result, nil
			}
			mode = "purify"
		case resolution.ClearMaterialCount > 0:
			consumeCount = resolution.ClearMaterialCount
			mode = "clear"
		default:
			return result, nil
		}
	} else {
		configuredOption := byte(0)
		switch cmd.AmplifyAction {
		case investAmplifyActionInvest:
			configuredOption = resolution.InvestOption
			consumeCount = resolution.InvestMaterialCount
			mode = "invest"
		case investAmplifyActionTwist:
			configuredOption = resolution.ReinvestOption
			consumeCount = resolution.ReinvestMaterialCount
			mode = "twist"
		case investAmplifyActionPureGold:
			configuredOption = resolution.PureGoldOption
			consumeCount = resolution.PureGoldMaterialCount
			mode = "pure_gold"
		default:
			return result, nil
		}
		if consumeCount <= 0 {
			return result, nil
		}
		newType = resolveAmplifyOption(configuredOption, cmd.SelectedAmplifyOption)
		if newType == 0 {
			return result, nil
		}
		unidentified := currentType&unidentifiedAmplifyFlag != 0
		identifiedType := currentType &^ unidentifiedAmplifyFlag
		switch cmd.AmplifyAction {
		case investAmplifyActionInvest:
			if unidentified || identifiedType != 0 {
				result.ErrorCode = investAmplifyErrorAlreadyHasOption
				return result, nil
			}
			if currentLevel != 0 {
				result.ErrorCode = investAmplifyErrorAlreadyUpgraded
				return result, nil
			}
		case investAmplifyActionTwist:
			if unidentified || identifiedType == 0 {
				result.ErrorCode = investAmplifyErrorNoOption
				return result, nil
			}
			if currentLevel != 0 {
				result.ErrorCode = investAmplifyErrorAlreadyUpgraded
				return result, nil
			}
		case investAmplifyActionPureGold:
			if unidentified {
				result.ErrorCode = investAmplifyErrorNoOption
				return result, nil
			}
		}
		if identifiedType == newType {
			result.ErrorCode = investAmplifyErrorSameOption
			return result, nil
		}
		newValue = resolution.InitialValues[newType]
		if newValue == 0 {
			result.ErrorCode = investAmplifyErrorUnsupported
			return result, nil
		}
		if cmd.AmplifyAction == investAmplifyActionPureGold {
			newLevel, err = rollPureGoldAmplifyLevel(resolution.PureGoldLevels)
			if err != nil {
				return AmplifyMutationResult{}, err
			}
		}
	}
	if consumeCount <= 0 || material.Count < consumeCount {
		return result, nil
	}

	target = cloneStack(target)
	setAmplifyState(&target, newType, newValue)
	if cmd.Operation == "invest_item_amplify_option" && cmd.AmplifyAction == investAmplifyActionPureGold {
		setUpgradeLevel(&target, newLevel)
	}
	record.Slots[targetKey] = target

	material = cloneStack(material)
	material.Count -= consumeCount
	result.MaterialRemainingCount = material.Count
	if material.Count <= 0 {
		delete(record.Slots, materialKey)
		result.MaterialRemainingCount = 0
	} else {
		updateStackRawAmount(&material)
		record.Slots[materialKey] = material
	}

	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, dnfrepo.InventoryFieldSlots); err != nil {
		return AmplifyMutationResult{}, err
	}
	result.Success = true
	result.ErrorCode = 0
	result.Mode = mode
	result.AmplifyType = newType
	result.AmplifyValue = newValue
	result.AmplifyLevel = newLevel
	result.Changed = true
	return result, nil
}

func resolveAmplifyOption(configured byte, selected byte) byte {
	if configured == amplifyOptionAll {
		if selected >= 1 && selected <= 4 {
			return selected
		}
		return 0
	}
	if configured >= 1 && configured <= 4 {
		return configured
	}
	return 0
}

func setAmplifyState(stack *dnfrepo.ItemStack, amplifyType byte, amplifyValue uint16) {
	if stack == nil {
		return
	}
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 10)
	}
	typeText := strconv.Itoa(int(amplifyType))
	valueText := strconv.Itoa(int(amplifyValue))
	for _, key := range []string{"amplify_type", "amplification_type", "byte_13", "value_13", "value_c"} {
		stack.Extra[key] = typeText
	}
	for _, key := range []string{"amplify_value", "amplification_value", "marker_16", "marker16", "value_d"} {
		stack.Extra[key] = valueText
	}
	if len(stack.RawEntry) == currentItemListEntrySize {
		stack.RawEntry = append([]byte(nil), stack.RawEntry...)
		stack.RawEntry[0x13] = amplifyType
		stack.RawEntry[0x14] = byte(amplifyValue)
		stack.RawEntry[0x15] = byte(amplifyValue >> 8)
	}
}

func rollPureGoldAmplifyLevel(entries []alignedcmd.AmplifyWeightedLevel) (byte, error) {
	total := int64(0)
	for _, entry := range entries {
		if entry.Weight <= 0 {
			continue
		}
		if entry.Level > 31 {
			return 0, fmt.Errorf("Pure Gold amplification level %d exceeds packed current-EXE maximum", entry.Level)
		}
		if entry.Weight > math.MaxInt64-total {
			return 0, fmt.Errorf("Pure Gold amplification weights overflow")
		}
		total += entry.Weight
	}
	if total <= 0 {
		return 0, fmt.Errorf("Pure Gold material has no runtime-PVF [amplification random value]")
	}
	roll, err := secureAmplifyRandom(total)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if entry.Weight <= 0 {
			continue
		}
		if roll < entry.Weight {
			return entry.Level, nil
		}
		roll -= entry.Weight
	}
	return 0, fmt.Errorf("Pure Gold amplification weighted selection exhausted")
}

func secureAmplifyRandom(upper int64) (int64, error) {
	if upper <= 0 {
		return 0, fmt.Errorf("amplification random upper bound %d invalid", upper)
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(upper))
	if err != nil {
		return 0, fmt.Errorf("amplification random: %w", err)
	}
	return value.Int64(), nil
}
