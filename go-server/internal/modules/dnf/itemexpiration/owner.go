package itemexpiration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable  = errors.New("item expiration owner is unavailable")
	ErrAccountRequired   = errors.New("item expiration account id is required")
	ErrCharacterRequired = errors.New("item expiration character id is required")
	ErrProjectorRequired = errors.New("item expiration projector is required")
	ErrCharacterNotFound = errors.New("item expiration character is missing")
	ErrAccountMismatch   = errors.New("item expiration character does not belong to account")
)

type Summary struct {
	Inventory int
	Warehouse int
	Account   int
	Equipment int
}

func (summary Summary) Total() int {
	return summary.Inventory + summary.Warehouse + summary.Account + summary.Equipment
}

type Assets struct {
	Inventory        *dnfrepo.InventoryRecord
	AccountInventory *dnfrepo.AccountInventoryRecord
	Equipment        *dnfrepo.EquipmentRecord
}

type Projector func(*Assets) (Summary, error)

type Command struct {
	AccountID   string
	CharacterID string
	UpdatedAt   time.Time
	Project     Projector
}

// Owner owns the atomic reconciliation commit for every durable item
// container affected by the shared PVF expiration projection. PVF lookup and
// current-client row encoding remain in the bridge.
type Owner struct {
	assets dnfrepo.AccountCharacterAssetUnitOfWork
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.AccountAssets == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{assets: repositories.AccountAssets}, nil
}

func (o *Owner) Reconcile(ctx context.Context, command Command) (Summary, error) {
	if o == nil || o.assets == nil {
		return Summary{}, ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(command.AccountID)
	if accountID == "" {
		return Summary{}, ErrAccountRequired
	}
	characterID := strings.TrimSpace(command.CharacterID)
	if characterID == "" {
		return Summary{}, ErrCharacterRequired
	}
	if command.Project == nil {
		return Summary{}, ErrProjectorRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}
	now := command.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var result Summary
	err := o.assets.WithinAccountCharacterAssets(ctx, accountID, characterID, func(
		accountInventories dnfrepo.AccountInventoryRepository,
		characters dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		equipmentRepo dnfrepo.EquipmentRepository,
	) error {
		assets, err := loadAssets(ctx, accountID, characterID, accountInventories, characters, inventories, equipmentRepo)
		if err != nil {
			return err
		}
		result, err = command.Project(&assets)
		if err != nil {
			return err
		}
		if assets.Inventory != nil && (result.Inventory > 0 || result.Warehouse > 0) {
			fields := make([]dnfrepo.InventoryField, 0, 2)
			if result.Inventory > 0 {
				fields = append(fields, dnfrepo.InventoryFieldSlots)
			}
			if result.Warehouse > 0 {
				fields = append(fields, dnfrepo.InventoryFieldWarehouse)
			}
			assets.Inventory.UpdatedAt = now
			if err := dnfrepo.SaveInventoryFields(ctx, inventories, *assets.Inventory, fields...); err != nil {
				return fmt.Errorf("save item expiration inventory: %w", err)
			}
		}
		if assets.AccountInventory != nil && result.Account > 0 {
			assets.AccountInventory.UpdatedAt = now
			if err := accountInventories.Save(ctx, *assets.AccountInventory); err != nil {
				return fmt.Errorf("save item expiration account inventory: %w", err)
			}
		}
		if assets.Equipment != nil && result.Equipment > 0 {
			assets.Equipment.UpdatedAt = now
			if err := dnfrepo.SaveEquipmentFields(ctx, equipmentRepo, *assets.Equipment, dnfrepo.EquipmentFieldEntries); err != nil {
				return fmt.Errorf("save item expiration equipment: %w", err)
			}
		}
		return nil
	})
	return result, err
}

func loadAssets(
	ctx context.Context,
	accountID string,
	characterID string,
	accountInventories dnfrepo.AccountInventoryRepository,
	characters dnfrepo.CharacterRepository,
	inventories dnfrepo.InventoryRepository,
	equipmentRepo dnfrepo.EquipmentRepository,
) (Assets, error) {
	if accountInventories == nil || characters == nil || inventories == nil || equipmentRepo == nil {
		return Assets{}, ErrOwnerUnavailable
	}
	var assets Assets
	character, found, err := characters.Load(ctx, characterID)
	if err != nil {
		return Assets{}, fmt.Errorf("load item expiration character: %w", err)
	}
	if !found || strings.TrimSpace(character.CharacterID) != characterID {
		return Assets{}, fmt.Errorf("%w: character=%s", ErrCharacterNotFound, characterID)
	}
	if strings.TrimSpace(character.AccountID) != accountID {
		return Assets{}, fmt.Errorf("%w: account=%s character=%s", ErrAccountMismatch, accountID, characterID)
	}
	inventory, found, err := inventories.Load(ctx, characterID)
	if err != nil {
		return Assets{}, fmt.Errorf("load item expiration inventory: %w", err)
	}
	if found {
		inventory = dnfrepo.CloneInventory(inventory)
		assets.Inventory = &inventory
	}
	accountInventory, found, err := accountInventories.Load(ctx, accountID)
	if err != nil {
		return Assets{}, fmt.Errorf("load item expiration account inventory: %w", err)
	}
	if found {
		accountInventory = dnfrepo.CloneAccountInventory(accountInventory)
		assets.AccountInventory = &accountInventory
	}
	equipment, found, err := equipmentRepo.Load(ctx, characterID)
	if err != nil {
		return Assets{}, fmt.Errorf("load item expiration equipment: %w", err)
	}
	if found {
		equipment = dnfrepo.CloneEquipment(equipment)
		assets.Equipment = &equipment
	}
	return assets, nil
}
