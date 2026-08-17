package booster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable  = errors.New("booster owner is unavailable")
	ErrAccountRequired   = errors.New("booster account id is required")
	ErrCharacterRequired = errors.New("booster character id is required")
	ErrCharacterNotFound = errors.New("booster character is not found")
	ErrAccountMismatch   = errors.New("booster character account mismatch")
	ErrInventoryNotFound = errors.New("booster inventory is not found")
	ErrProjectorRequired = errors.New("booster mutation projector is required")
)

type Assets struct {
	AccountInventory *dnfrepo.AccountInventoryRecord
	Character        *dnfrepo.CharacterRecord
	Inventory        *dnfrepo.InventoryRecord
}

type Changes struct {
	AccountInventory bool
	Character        bool
	Inventory        bool
}

type Projector func(*Assets) (Changes, error)

type Command struct {
	AccountID   string
	CharacterID string
	UpdatedAt   time.Time
	Project     Projector
}

// Owner owns booster authorization and the atomic account-warehouse plus
// character-inventory commit. Packet and raw-row projection stay in bridge.
type Owner struct {
	assets dnfrepo.AccountCharacterAssetUnitOfWork
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.AccountAssets == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{assets: repositories.AccountAssets}, nil
}

func (o *Owner) Open(ctx context.Context, command Command) error {
	if o == nil || o.assets == nil {
		return ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(command.AccountID)
	characterID := strings.TrimSpace(command.CharacterID)
	if accountID == "" {
		return ErrAccountRequired
	}
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

	return o.assets.WithinAccountCharacterAssets(ctx, accountID, characterID, func(
		accounts dnfrepo.AccountInventoryRepository,
		characters dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
	) error {
		if accounts == nil || characters == nil || inventories == nil {
			return ErrOwnerUnavailable
		}
		character, found, err := characters.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(character.CharacterID) != characterID {
			return fmt.Errorf("%w: character=%s", ErrCharacterNotFound, characterID)
		}
		if strings.TrimSpace(character.AccountID) != accountID {
			return fmt.Errorf("%w: account=%s character=%s", ErrAccountMismatch, accountID, characterID)
		}
		character = dnfrepo.CloneCharacter(character)
		inventory, found, err := inventories.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(inventory.CharacterID) != characterID || inventory.Slots == nil {
			return fmt.Errorf("%w: character=%s", ErrInventoryNotFound, characterID)
		}
		accountInventory, _, err := accounts.Load(ctx, accountID)
		if err != nil {
			return err
		}
		accountInventory = dnfrepo.CloneAccountInventory(accountInventory)
		accountInventory.AccountID = accountID
		if accountInventory.Slots == nil {
			accountInventory.Slots = make(map[string]dnfrepo.ItemStack)
		}
		inventory = dnfrepo.CloneInventory(inventory)

		changes, err := command.Project(&Assets{
			AccountInventory: &accountInventory,
			Character:        &character,
			Inventory:        &inventory,
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
		if changes.Character {
			character.UpdatedAt = now
			if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
				return err
			}
		}
		if changes.AccountInventory {
			accountInventory.UpdatedAt = now
			if err := accounts.Save(ctx, accountInventory); err != nil {
				return err
			}
		}
		return nil
	})
}
