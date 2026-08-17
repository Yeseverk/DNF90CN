package dungeon

import (
	"context"
	"errors"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var ErrMutationRequired = errors.New("dungeon mutation callback is required")

type InventoryMutation func(*dnfrepo.InventoryRecord) (bool, error)

type InventoryMutationCommand struct {
	CharacterID string
	UpdatedAt   time.Time
	Apply       InventoryMutation
}

type InventoryMutationResult struct {
	Changed   bool
	Inventory dnfrepo.InventoryRecord
}

type OwnedInventoryMutation func(map[string]dnfrepo.ItemStack) (bool, error)

type OwnedInventoryMutationCommand struct {
	AccountID    string
	CharacterID  string
	AccountOwned bool
	UpdatedAt    time.Time
	Apply        OwnedInventoryMutation
}

// MutateOwnedInventory atomically validates character ownership and mutates
// either account-shared slots or the selected character's ordinary slots.
func (o *Owner) MutateOwnedInventory(ctx context.Context, cmd OwnedInventoryMutationCommand) error {
	if o == nil || o.accountAssets == nil || strings.TrimSpace(cmd.AccountID) == "" ||
		strings.TrimSpace(cmd.CharacterID) == "" {
		return ErrOwnerUnavailable
	}
	if cmd.Apply == nil {
		return ErrMutationRequired
	}
	ctx = contextOrBackground(ctx)
	now := updatedAtOrNow(cmd.UpdatedAt)

	return o.accountAssets.WithinAccountCharacterAssets(
		ctx,
		cmd.AccountID,
		cmd.CharacterID,
		func(
			accountInventories dnfrepo.AccountInventoryRepository,
			characters dnfrepo.CharacterRepository,
			inventories dnfrepo.InventoryRepository,
			_ dnfrepo.EquipmentRepository,
		) error {
			character, found, err := characters.Load(ctx, cmd.CharacterID)
			if err != nil {
				return err
			}
			if !found || strings.TrimSpace(character.AccountID) != strings.TrimSpace(cmd.AccountID) {
				return ErrCharacterNotFound
			}
			if cmd.AccountOwned {
				record, found, err := accountInventories.Load(ctx, cmd.AccountID)
				if err != nil {
					return err
				}
				if !found {
					return ErrInventoryNotFound
				}
				record = dnfrepo.CloneAccountInventory(record)
				if record.Slots == nil {
					record.Slots = make(map[string]dnfrepo.ItemStack)
				}
				changed, err := cmd.Apply(record.Slots)
				if err != nil {
					return err
				}
				if !changed {
					return nil
				}
				record.UpdatedAt = now
				return accountInventories.Save(ctx, record)
			}

			record, found, err := inventories.Load(ctx, cmd.CharacterID)
			if err != nil {
				return err
			}
			if !found {
				return ErrInventoryNotFound
			}
			record = dnfrepo.CloneInventory(record)
			if record.Slots == nil {
				record.Slots = make(map[string]dnfrepo.ItemStack)
			}
			changed, err := cmd.Apply(record.Slots)
			if err != nil {
				return err
			}
			if !changed {
				return nil
			}
			record.UpdatedAt = now
			return dnfrepo.SaveInventoryFields(ctx, inventories, record, dnfrepo.InventoryFieldSlots)
		},
	)
}

// MutateInventory is the transaction boundary for dungeon flows whose
// placement inputs are already resolved from runtime PVF evidence.
func (o *Owner) MutateInventory(ctx context.Context, cmd InventoryMutationCommand) (InventoryMutationResult, error) {
	if o == nil || o.items == nil || strings.TrimSpace(cmd.CharacterID) == "" {
		return InventoryMutationResult{}, ErrOwnerUnavailable
	}
	if cmd.Apply == nil {
		return InventoryMutationResult{}, ErrMutationRequired
	}
	ctx = contextOrBackground(ctx)
	now := updatedAtOrNow(cmd.UpdatedAt)

	var result InventoryMutationResult
	err := o.items.WithinCharacterItems(ctx, cmd.CharacterID, func(
		inventories dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
	) error {
		inventory, found, err := inventories.Load(ctx, cmd.CharacterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrInventoryNotFound
		}
		inventory = dnfrepo.CloneInventory(inventory)
		if inventory.Slots == nil {
			inventory.Slots = make(map[string]dnfrepo.ItemStack)
		}
		changed, err := cmd.Apply(&inventory)
		if err != nil {
			return err
		}
		if changed {
			inventory.UpdatedAt = now
			if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
		}
		result = InventoryMutationResult{
			Changed:   changed,
			Inventory: dnfrepo.CloneInventory(inventory),
		}
		return nil
	})
	if err != nil {
		return InventoryMutationResult{}, err
	}
	return result, nil
}

type TutorialCompletionCommand struct {
	CharacterID  string
	CompletedKey string
	Completed    int64
	NextLogin    map[string]int64
	UpdatedAt    time.Time
}

type TutorialCompletionResult struct {
	Previous int64
	Changed  bool
}

func (o *Owner) CompleteTutorial(ctx context.Context, cmd TutorialCompletionCommand) (TutorialCompletionResult, error) {
	if o == nil || o.characters == nil || strings.TrimSpace(cmd.CharacterID) == "" ||
		strings.TrimSpace(cmd.CompletedKey) == "" || cmd.Completed == 0 {
		return TutorialCompletionResult{}, ErrOwnerUnavailable
	}
	ctx = contextOrBackground(ctx)
	character, found, err := o.characters.Load(ctx, cmd.CharacterID)
	if err != nil {
		return TutorialCompletionResult{}, err
	}
	if !found {
		return TutorialCompletionResult{}, ErrCharacterNotFound
	}
	character = dnfrepo.CloneCharacter(character)
	if character.Stats == nil {
		character.Stats = make(map[string]int64)
	}
	result := TutorialCompletionResult{Previous: character.Stats[cmd.CompletedKey]}
	values := make(map[string]int64, len(cmd.NextLogin)+1)
	for key, value := range cmd.NextLogin {
		values[key] = value
	}
	values[cmd.CompletedKey] = cmd.Completed
	for key, value := range values {
		if current, ok := character.Stats[key]; !ok || current != value {
			character.Stats[key] = value
			result.Changed = true
		}
	}
	if !result.Changed {
		return result, nil
	}
	character.UpdatedAt = updatedAtOrNow(cmd.UpdatedAt)
	if err := dnfrepo.SaveCharacterFields(ctx, o.characters, character, dnfrepo.CharacterFieldStats); err != nil {
		return TutorialCompletionResult{}, err
	}
	return result, nil
}
