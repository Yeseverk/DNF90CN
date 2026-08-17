package itemgrade

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/itemquality"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable  = errors.New("item grade owner unavailable")
	ErrCharacterRequired = errors.New("selected character id required")
	ErrTargetMissing     = errors.New("item grade target missing")
	ErrMaterialMissing   = errors.New("item grade material missing")
)

type ItemDefinition struct {
	StackableType string
}

type ItemCatalog interface {
	ResolveItem(uint32) (ItemDefinition, error)
}

type SeedGenerator func() (uint32, error)

type Command struct {
	SelectedCharacterID uint16
	TargetSlot          int16
	TargetItemID        int32
	MaterialSlot        int16
	UpdatedAt           time.Time
}

type Result struct {
	CharacterID       string
	TargetSlot        int16
	TargetItemID      int32
	TargetStack       dnfrepo.ItemStack
	MaterialSlot      int16
	MaterialItemID    int64
	MaterialRemaining int64
	MaterialRemoved   bool
	NewSeed           uint32
	GoldKaleido       bool
}

// Owner owns kaleido validation, quality selection, material consumption, and
// the durable inventory write.
type Owner struct {
	inventory dnfrepo.InventoryRepository
	catalog   ItemCatalog
	newSeed   SeedGenerator
}

func NewOwner(repos dnfrepo.Group, catalog ItemCatalog, newSeed SeedGenerator) (*Owner, error) {
	if repos.Inventory == nil {
		return nil, ErrOwnerUnavailable
	}
	if newSeed == nil {
		newSeed = itemquality.NewRandomSeed
	}
	return &Owner{inventory: repos.Inventory, catalog: catalog, newSeed: newSeed}, nil
}

func (o *Owner) Adjust(ctx context.Context, cmd Command) (Result, error) {
	if o == nil || o.inventory == nil || o.newSeed == nil {
		return Result{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return Result{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	inventory, found, err := o.inventory.Load(ctx, characterID)
	if err != nil {
		return Result{}, err
	}
	if !found || strings.TrimSpace(inventory.CharacterID) != characterID {
		return Result{}, ErrOwnerUnavailable
	}
	inventory = dnfrepo.CloneInventory(inventory)

	targetKey := inventoryKey(cmd.TargetSlot)
	target, found := inventory.Slots[targetKey]
	if !found || target.ItemID != int64(cmd.TargetItemID) || target.ItemID <= 0 {
		return Result{}, fmt.Errorf("%w: slot=%d item=%d", ErrTargetMissing, cmd.TargetSlot, cmd.TargetItemID)
	}
	materialKey := inventoryKey(cmd.MaterialSlot)
	material, found := inventory.Slots[materialKey]
	if !found || material.ItemID <= 0 || material.Count <= 0 {
		return Result{}, fmt.Errorf("%w: slot=%d", ErrMaterialMissing, cmd.MaterialSlot)
	}

	goldKaleido := o.isGoldKaleido(material.ItemID)
	seed := itemquality.TopSeed
	if !goldKaleido {
		seed, err = o.newSeed()
		if err != nil {
			return Result{}, err
		}
	}
	if err := itemquality.Apply(&target, seed); err != nil {
		return Result{}, err
	}
	// A grade-adjust target is necessarily equipment. Normalize legacy rows so
	// every later inventory projection keeps using the quality seed instead of
	// falling back to the ordinary stack count.
	target.Extra["item_kind"] = "equipment"
	now := cmd.UpdatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	target.Extra["last_grade_adjust"] = now.Format(time.RFC3339)
	if goldKaleido {
		target.Extra["grade_adjust_type"] = "gold_kaleido"
	} else {
		target.Extra["grade_adjust_type"] = "standard_kaleido"
	}
	inventory.Slots[targetKey] = target

	material.Count--
	materialRemoved := material.Count == 0
	if materialRemoved {
		delete(inventory.Slots, materialKey)
	} else {
		inventory.Slots[materialKey] = material
	}
	inventory.UpdatedAt = now
	if err := dnfrepo.SaveInventoryFields(ctx, o.inventory, inventory, dnfrepo.InventoryFieldSlots); err != nil {
		return Result{}, err
	}
	return Result{
		CharacterID:       characterID,
		TargetSlot:        cmd.TargetSlot,
		TargetItemID:      cmd.TargetItemID,
		TargetStack:       target,
		MaterialSlot:      cmd.MaterialSlot,
		MaterialItemID:    material.ItemID,
		MaterialRemaining: material.Count,
		MaterialRemoved:   materialRemoved,
		NewSeed:           seed,
		GoldKaleido:       goldKaleido,
	}, nil
}

func (o *Owner) isGoldKaleido(itemID int64) bool {
	if o.catalog == nil || itemID <= 0 {
		return false
	}
	definition, err := o.catalog.ResolveItem(uint32(itemID))
	if err != nil {
		return false
	}
	normalized := strings.ToLower(strings.ReplaceAll(definition.StackableType, "`", ""))
	// Preserve the current-client/PVF rule: the gold marker alone selects the
	// top-quality sentinel, including localized type names without "kaleido".
	return strings.Contains(normalized, "gold")
}

func inventoryKey(slot int16) string {
	return fmt.Sprintf("%d:%d", dnfrepo.MainInventoryListType, slot)
}
