package guardiangem

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable  = errors.New("guardian gem owner is unavailable")
	ErrCharacterRequired = errors.New("guardian gem character id is required")
	ErrInventoryMissing  = errors.New("guardian gem inventory is missing")
	ErrProjectorRequired = errors.New("guardian gem mutation projector is required")
)

type Assets struct {
	Inventory *dnfrepo.InventoryRecord
	Equipment *dnfrepo.EquipmentRecord
}

type Changes struct {
	InventorySlots     bool
	InventoryWarehouse bool
	Equipment          bool
}

type Projector func(*Assets) (Changes, error)

type Command struct {
	CharacterID string
	UpdatedAt   time.Time
	Project     Projector
}

// Owner owns the atomic character inventory/equipment commit for guardian-gem
// insertion. PVF validation and current-client raw socket projection remain
// in the bridge.
type Owner struct {
	items dnfrepo.CharacterItemUnitOfWork
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.CharacterItems == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{items: repositories.CharacterItems}, nil
}

func (o *Owner) Insert(ctx context.Context, command Command) error {
	if o == nil || o.items == nil {
		return ErrOwnerUnavailable
	}
	characterID := strings.TrimSpace(command.CharacterID)
	if characterID == "" {
		return ErrCharacterRequired
	}
	if command.Project == nil {
		return ErrProjectorRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	now := command.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	return o.items.WithinCharacterItems(ctx, characterID, func(
		inventories dnfrepo.InventoryRepository,
		equipmentRepo dnfrepo.EquipmentRepository,
	) error {
		if inventories == nil || equipmentRepo == nil {
			return ErrOwnerUnavailable
		}
		inventory, found, err := inventories.Load(ctx, characterID)
		if err != nil {
			return fmt.Errorf("load guardian gem inventory: %w", err)
		}
		if !found || strings.TrimSpace(inventory.CharacterID) != characterID {
			return fmt.Errorf("%w: character=%s", ErrInventoryMissing, characterID)
		}
		inventory = dnfrepo.CloneInventory(inventory)
		equipment, equipmentFound, err := equipmentRepo.Load(ctx, characterID)
		if err != nil {
			return fmt.Errorf("load guardian gem equipment: %w", err)
		}
		if equipmentFound {
			equipment = dnfrepo.CloneEquipment(equipment)
		} else {
			equipment = dnfrepo.EquipmentRecord{
				CharacterID: characterID,
				Entries:     make(map[string]dnfrepo.EquipmentEntry),
			}
		}
		if equipment.Entries == nil {
			equipment.Entries = make(map[string]dnfrepo.EquipmentEntry)
		}

		changes, err := command.Project(&Assets{
			Inventory: &inventory,
			Equipment: &equipment,
		})
		if err != nil {
			return err
		}
		inventoryFields := make([]dnfrepo.InventoryField, 0, 2)
		if changes.InventorySlots {
			inventoryFields = append(inventoryFields, dnfrepo.InventoryFieldSlots)
		}
		if changes.InventoryWarehouse {
			inventoryFields = append(inventoryFields, dnfrepo.InventoryFieldWarehouse)
		}
		if len(inventoryFields) > 0 {
			inventory.UpdatedAt = now
			if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, inventoryFields...); err != nil {
				return err
			}
		}
		if changes.Equipment {
			equipment.UpdatedAt = now
			if err := dnfrepo.SaveEquipmentFields(ctx, equipmentRepo, equipment, dnfrepo.EquipmentFieldEntries); err != nil {
				return err
			}
		}
		return nil
	})
}
