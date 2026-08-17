package auraskin

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	FlagStat      = "aura_flag"
	TicketPVFPath = "stackable/cash/chn_490700411.stk"
	ItemStackable = "stackable"
)

var (
	ErrOwnerUnavailable  = errors.New("aura skin owner unavailable")
	ErrCharacterRequired = errors.New("selected character id required")
	ErrCharacterNotFound = errors.New("character record not found")
	ErrAccountMismatch   = errors.New("character account does not match session account")
	ErrInventoryNotFound = errors.New("inventory record not found")
	ErrSourceMissing     = errors.New("aura skin source item missing")
	ErrTicketInvalid     = errors.New("aura skin source is not the PVF expansion ticket")
)

type ItemDefinition struct {
	Kind    string
	PVFPath string
}

type ItemCatalog interface {
	ResolveItem(uint32) (ItemDefinition, error)
}

type Command struct {
	AccountID           string
	SelectedCharacterID uint16
	SourceSlot          int16
	UpdatedAt           time.Time
}

type Result struct {
	CharacterID    string
	SourceSlot     int16
	SourceItemID   int64
	SourcePVFPath  string
	Consumed       bool
	AlreadyOpen    bool
	SourceChanged  bool
	SourceRemoved  bool
	RemainingStack dnfrepo.ItemStack
}

// Owner owns the aura-slot rule and the atomic character/inventory mutation.
// It deliberately has no dependency on packet writers or current-client rows.
type Owner struct {
	assets  dnfrepo.CharacterAssetUnitOfWork
	catalog ItemCatalog
}

func NewOwner(repos dnfrepo.Group, catalog ItemCatalog) (*Owner, error) {
	if repos.CharacterAssets == nil || catalog == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{assets: repos.CharacterAssets, catalog: catalog}, nil
}

func (o *Owner) Open(ctx context.Context, cmd Command) (Result, error) {
	if o == nil || o.assets == nil || o.catalog == nil {
		return Result{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return Result{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	accountID := strings.TrimSpace(cmd.AccountID)
	result := Result{CharacterID: characterID, SourceSlot: cmd.SourceSlot}
	err := o.assets.WithinCharacterAssets(ctx, characterID, func(
		characters dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
	) error {
		character, found, err := characters.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(character.CharacterID) != characterID {
			return fmt.Errorf("%w: character=%s", ErrCharacterNotFound, characterID)
		}
		if accountID != "" && strings.TrimSpace(character.AccountID) != accountID {
			return fmt.Errorf("%w: character=%s", ErrAccountMismatch, characterID)
		}
		character = dnfrepo.CloneCharacter(character)
		if character.Stats == nil {
			character.Stats = make(map[string]int64, 1)
		}
		if character.Stats[FlagStat] != 0 {
			result.AlreadyOpen = true
			return nil
		}

		inventory, found, err := inventories.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(inventory.CharacterID) != characterID {
			return fmt.Errorf("%w: character=%s", ErrInventoryNotFound, characterID)
		}
		inventory = dnfrepo.CloneInventory(inventory)
		sourceKey := inventoryKey(cmd.SourceSlot)
		sourceStack, found := inventory.Slots[sourceKey]
		if !found || sourceStack.ItemID <= 0 || sourceStack.ItemID > math.MaxUint32 || sourceStack.Count <= 0 {
			return fmt.Errorf("%w: slot=%d", ErrSourceMissing, cmd.SourceSlot)
		}
		definition, err := o.resolveTicket(uint32(sourceStack.ItemID))
		if err != nil {
			return err
		}
		result.SourceItemID = sourceStack.ItemID
		result.SourcePVFPath = definition.PVFPath

		sourceStack.Count--
		if sourceStack.Count == 0 {
			delete(inventory.Slots, sourceKey)
			result.SourceRemoved = true
		} else {
			inventory.Slots[sourceKey] = sourceStack
			result.RemainingStack = sourceStack
		}
		result.SourceChanged = true
		result.Consumed = true

		now := cmd.UpdatedAt
		if now.IsZero() {
			now = time.Now().UTC()
		} else {
			now = now.UTC()
		}
		character.Stats[FlagStat] = 1
		character.UpdatedAt = now
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		inventory.UpdatedAt = now
		return dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots)
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func (o *Owner) resolveTicket(itemID uint32) (ItemDefinition, error) {
	definition, err := o.catalog.ResolveItem(itemID)
	if err != nil {
		return ItemDefinition{}, fmt.Errorf("%w: item_id=%d resolve=%v", ErrTicketInvalid, itemID, err)
	}
	if strings.ToLower(strings.TrimSpace(definition.Kind)) != ItemStackable ||
		cleanPVFPath(definition.PVFPath) != TicketPVFPath {
		return ItemDefinition{}, fmt.Errorf(
			"%w: item_id=%d kind=%s path=%s",
			ErrTicketInvalid,
			itemID,
			definition.Kind,
			definition.PVFPath,
		)
	}
	return definition, nil
}

func inventoryKey(slot int16) string {
	return fmt.Sprintf("%d:%d", dnfrepo.MainInventoryListType, slot)
}

func cleanPVFPath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	return strings.ToLower(strings.TrimPrefix(path.Clean("/"+value), "/"))
}
