package socketemblem

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable  = errors.New("socket and emblem owner is unavailable")
	ErrCharacterRequired = errors.New("socket and emblem character id is required")
	ErrInventoryNotFound = errors.New("socket and emblem inventory is not found")
	ErrProjectorRequired = errors.New("socket and emblem mutation projector is required")
)

// Assets are transaction-local clones. The bridge projector may translate
// current-client raw rows, but only this owner commits the resulting aggregate
// changes.
type Assets struct {
	Inventory      *dnfrepo.InventoryRecord
	Equipment      *dnfrepo.EquipmentRecord
	EquipmentFound bool
}

type Changes struct {
	Inventory bool
	Equipment bool
}

type Projector func(*Assets) (Changes, error)

type Command struct {
	CharacterID string
	UpdatedAt   time.Time
	Project     Projector
}

// Owner is the durable boundary shared by the four independently registered
// socket/emblem gameplays.
type Owner struct {
	items dnfrepo.CharacterItemUnitOfWork
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.CharacterItems == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{items: repositories.CharacterItems}, nil
}

func (o *Owner) OpenEquipmentSocket(ctx context.Context, command Command) error {
	return o.mutate(ctx, "equipment-socket-open", command)
}

func (o *Owner) AttachEquipmentEmblems(ctx context.Context, command Command) error {
	return o.mutate(ctx, "equipment-emblem-attach", command)
}

func (o *Owner) OpenAvatarSocket(ctx context.Context, command Command) error {
	return o.mutate(ctx, "avatar-socket-open", command)
}

func (o *Owner) AttachAvatarEmblems(ctx context.Context, command Command) error {
	return o.mutate(ctx, "avatar-emblem-attach", command)
}

func (o *Owner) mutate(ctx context.Context, operation string, command Command) error {
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
		equipment dnfrepo.EquipmentRepository,
	) error {
		if inventories == nil || equipment == nil {
			return ErrOwnerUnavailable
		}
		inventory, found, err := inventories.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(inventory.CharacterID) != characterID {
			return fmt.Errorf("%w: character=%s operation=%s", ErrInventoryNotFound, characterID, operation)
		}
		equipmentRecord, equipmentFound, err := equipment.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if equipmentFound && strings.TrimSpace(equipmentRecord.CharacterID) != characterID {
			return ErrOwnerUnavailable
		}

		inventory = dnfrepo.CloneInventory(inventory)
		equipmentRecord = dnfrepo.CloneEquipment(equipmentRecord)
		changes, err := command.Project(&Assets{
			Inventory:      &inventory,
			Equipment:      &equipmentRecord,
			EquipmentFound: equipmentFound,
		})
		if err != nil {
			return err
		}
		if changes.Inventory {
			inventory.UpdatedAt = now
			if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
		}
		if changes.Equipment {
			if !equipmentFound {
				return fmt.Errorf("%w: character=%s operation=%s equipment missing", ErrOwnerUnavailable, characterID, operation)
			}
			equipmentRecord.UpdatedAt = now
			if err := dnfrepo.SaveEquipmentFields(ctx, equipment, equipmentRecord, dnfrepo.EquipmentFieldEntries); err != nil {
				return err
			}
		}
		return nil
	})
}
