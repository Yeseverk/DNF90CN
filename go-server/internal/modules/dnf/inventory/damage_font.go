package inventory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	damageFontActionIndex               uint32 = 162
	damageFontSelectionErrorUnavailable byte   = 17
	DamageFontSelectedStatKey                  = "damage_font_selected"
	damageFontOwnershipStatPrefix              = "damage_font_skin_"
	damageFontOwnershipStatSuffix              = "_expires_at"
)

var (
	ErrDamageFontResolverRequired = errors.New("damage font requires a runtime-PVF resolver")
	ErrDamageFontDefinition       = errors.New("item is not a valid damage-font skin consumable")
	ErrDamageFontExpired          = errors.New("damage-font item expiration has passed")
)

type DamageFontUnlockResult struct {
	CharacterID     string
	SourceSlotIndex int16
	ItemID          int64
	FontIndex       uint16
	ExpiresAt       uint32
	RemainingCount  int64
	PVFPath         string
}

type DamageFontSelectionResult struct {
	Success   bool
	FontIndex uint16
}

type DamageFontEntry struct {
	FontIndex uint16
	ExpiresAt uint32
}

func DamageFontOwnershipStatKey(fontIndex uint16) string {
	return damageFontOwnershipStatPrefix + strconv.FormatUint(uint64(fontIndex), 10) + damageFontOwnershipStatSuffix
}

func DamageFontStateFromStats(stats map[string]int64, now time.Time) (uint16, []DamageFontEntry) {
	nowUnix := now.Unix()
	entries := make([]DamageFontEntry, 0)
	owned := make(map[uint16]struct{})
	for key, expiresAt := range stats {
		fontIndex, ok := parseDamageFontOwnershipStatKey(key)
		if !ok || expiresAt < 0 || expiresAt > math.MaxUint32 {
			continue
		}
		if expiresAt != 0 && expiresAt <= nowUnix {
			continue
		}
		entries = append(entries, DamageFontEntry{FontIndex: fontIndex, ExpiresAt: uint32(expiresAt)})
		owned[fontIndex] = struct{}{}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].FontIndex < entries[j].FontIndex })
	selectedValue := stats[DamageFontSelectedStatKey]
	if selectedValue <= 0 || selectedValue > math.MaxUint16 {
		return 0, entries
	}
	selected := uint16(selectedValue)
	if _, ok := owned[selected]; !ok {
		return 0, entries
	}
	return selected, entries
}

func parseDamageFontOwnershipStatKey(key string) (uint16, bool) {
	if !strings.HasPrefix(key, damageFontOwnershipStatPrefix) || !strings.HasSuffix(key, damageFontOwnershipStatSuffix) {
		return 0, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(key, damageFontOwnershipStatPrefix), damageFontOwnershipStatSuffix)
	value, err := strconv.ParseUint(raw, 10, 16)
	if err != nil || value == 0 {
		return 0, false
	}
	return uint16(value), true
}

func (o *Owner) UnlockDamageFont(ctx context.Context, cmd Command, resolver alignedcmd.DamageFontResolver, now time.Time) (DamageFontUnlockResult, error) {
	if o == nil || o.repo == nil || o.characters == nil {
		return DamageFontUnlockResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return DamageFontUnlockResult{}, ErrCharacterRequired
	}
	if cmd.SourceListType != listTypeMain || cmd.SourceSlotIndex < 0 || cmd.ActionIndex != damageFontActionIndex {
		return DamageFontUnlockResult{}, ErrDamageFontDefinition
	}
	if resolver == nil {
		return DamageFontUnlockResult{}, ErrDamageFontResolverRequired
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	if !o.inItemTransaction {
		var result DamageFontUnlockResult
		err := o.withinCharacterAssetTransaction(ctx, characterID, func(txOwner *Owner) error {
			var innerErr error
			result, innerErr = txOwner.UnlockDamageFont(ctx, cmd, resolver, now)
			return innerErr
		})
		return result, err
	}

	inventoryRecord, found, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return DamageFontUnlockResult{}, err
	}
	if !found {
		return DamageFontUnlockResult{}, ErrInventoryNotFound
	}
	inventoryRecord = dnfrepo.CloneInventory(inventoryRecord)
	key := slotKey(cmd.SourceListType, cmd.SourceSlotIndex)
	stack, found := inventoryRecord.Slots[key]
	if !found || stack.Count <= 0 || stack.ItemID <= 0 {
		return DamageFontUnlockResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
	}
	resolution, err := resolver(stack.ItemID)
	if err != nil {
		return DamageFontUnlockResult{}, err
	}
	if !resolution.Valid || resolution.FontIndex == 0 || !strings.EqualFold(strings.TrimSpace(resolution.ActionType), "[add damage font skin]") {
		return DamageFontUnlockResult{}, fmt.Errorf("%w: item=%d path=%q", ErrDamageFontDefinition, stack.ItemID, resolution.PVFPath)
	}

	character, found, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return DamageFontUnlockResult{}, err
	}
	if !found {
		return DamageFontUnlockResult{}, ErrCharacterRequired
	}
	character = dnfrepo.CloneCharacter(character)
	if character.Stats == nil {
		character.Stats = make(map[string]int64)
	}
	statKey := DamageFontOwnershipStatKey(resolution.FontIndex)
	existingExpiry, alreadyOwned := character.Stats[statKey]
	expiresAt, err := resolveDamageFontExpiration(now, existingExpiry, alreadyOwned, resolution)
	if err != nil {
		return DamageFontUnlockResult{}, err
	}

	remaining := stack.Count - 1
	if remaining == 0 {
		delete(inventoryRecord.Slots, key)
	} else {
		stack = cloneStack(stack)
		stack.Count = remaining
		updateStackRawAmount(&stack)
		inventoryRecord.Slots[key] = stack
	}
	character.Stats[statKey] = int64(expiresAt)
	inventoryRecord.UpdatedAt = now
	character.UpdatedAt = now
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, inventoryRecord, dnfrepo.InventoryFieldSlots); err != nil {
		return DamageFontUnlockResult{}, err
	}
	if err := dnfrepo.SaveCharacterFields(ctx, o.characters, character, dnfrepo.CharacterFieldStats); err != nil {
		return DamageFontUnlockResult{}, err
	}
	return DamageFontUnlockResult{
		CharacterID:     characterID,
		SourceSlotIndex: cmd.SourceSlotIndex,
		ItemID:          stack.ItemID,
		FontIndex:       resolution.FontIndex,
		ExpiresAt:       expiresAt,
		RemainingCount:  remaining,
		PVFPath:         resolution.PVFPath,
	}, nil
}

func resolveDamageFontExpiration(now time.Time, existing int64, alreadyOwned bool, resolution alignedcmd.DamageFontResolution) (uint32, error) {
	if alreadyOwned && existing == 0 {
		return 0, nil
	}
	nowUnix := now.Unix()
	switch resolution.ExpirationMode {
	case alignedcmd.DamageFontExpirationUnlimited:
		return 0, nil
	case alignedcmd.DamageFontExpirationFixed:
		candidate := resolution.FixedExpiration.Unix()
		if candidate <= nowUnix {
			return 0, ErrDamageFontExpired
		}
		if existing > candidate {
			candidate = existing
		}
		if candidate > math.MaxUint32 {
			return 0, fmt.Errorf("damage-font fixed expiration %d exceeds u32", candidate)
		}
		return uint32(candidate), nil
	case alignedcmd.DamageFontExpirationPeriod:
		if resolution.PeriodDays <= 0 || resolution.PeriodDays > math.MaxUint32/86400 {
			return 0, fmt.Errorf("damage-font period days %d invalid", resolution.PeriodDays)
		}
		base := nowUnix
		if existing > base {
			base = existing
		}
		candidate := base + resolution.PeriodDays*86400
		if candidate <= base || candidate > math.MaxUint32 {
			return 0, fmt.Errorf("damage-font period expiration %d exceeds u32", candidate)
		}
		return uint32(candidate), nil
	default:
		return 0, ErrDamageFontDefinition
	}
}

func (o *Owner) SelectDamageFont(ctx context.Context, cmd Command, now time.Time) (DamageFontSelectionResult, error) {
	if o == nil || o.characters == nil {
		return DamageFontSelectionResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return DamageFontSelectionResult{}, ErrCharacterRequired
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	if !o.inItemTransaction {
		var result DamageFontSelectionResult
		err := o.withinCharacterAssetTransaction(ctx, characterID, func(txOwner *Owner) error {
			var innerErr error
			result, innerErr = txOwner.SelectDamageFont(ctx, cmd, now)
			return innerErr
		})
		return result, err
	}
	character, found, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return DamageFontSelectionResult{}, err
	}
	if !found {
		return DamageFontSelectionResult{}, ErrCharacterRequired
	}
	character = dnfrepo.CloneCharacter(character)
	if cmd.DamageFontIndex != 0 {
		expiresAt, owned := character.Stats[DamageFontOwnershipStatKey(cmd.DamageFontIndex)]
		if !owned || expiresAt < 0 || (expiresAt != 0 && expiresAt <= now.Unix()) {
			return DamageFontSelectionResult{FontIndex: cmd.DamageFontIndex}, nil
		}
	}
	if character.Stats == nil {
		character.Stats = make(map[string]int64)
	}
	character.Stats[DamageFontSelectedStatKey] = int64(cmd.DamageFontIndex)
	character.UpdatedAt = now
	if err := dnfrepo.SaveCharacterFields(ctx, o.characters, character, dnfrepo.CharacterFieldStats); err != nil {
		return DamageFontSelectionResult{}, err
	}
	return DamageFontSelectionResult{Success: true, FontIndex: cmd.DamageFontIndex}, nil
}
