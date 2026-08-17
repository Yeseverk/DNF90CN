package expertjob

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable  = errors.New("expert job owner is unavailable")
	ErrCharacterRequired = errors.New("expert job character is required")
	ErrCharacterNotFound = errors.New("expert job character is not found")
	ErrAccountMismatch   = errors.New("expert job character account mismatch")
	ErrInventoryNotFound = errors.New("expert job inventory is not found")
	ErrProjectorRequired = errors.New("expert job projector is required")
)

type Assets struct {
	Character        *dnfrepo.CharacterRecord
	AccountInventory *dnfrepo.AccountInventoryRecord
	Inventory        *dnfrepo.InventoryRecord
	Quest            *dnfrepo.QuestRecord
}

type Changes struct {
	Character        bool
	AccountInventory bool
	Inventory        bool
	Quest            bool
}

type Command struct {
	AccountID    string
	CharacterID  string
	UpdatedAt    time.Time
	IncludeQuest bool
	Project      func(*Assets) (Changes, error)
}

type Owner struct {
	settlements dnfrepo.CharacterSettlementUnitOfWork
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.CharacterSettlement == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{settlements: repositories.CharacterSettlement}, nil
}

func (o *Owner) Compound(ctx context.Context, command Command) error {
	return o.mutate(ctx, "compound", command)
}

func (o *Owner) Extract(ctx context.Context, command Command) error {
	return o.mutate(ctx, "extract", command)
}

func (o *Owner) LearnRecipe(ctx context.Context, command Command) error {
	return o.mutate(ctx, "learn-recipe", command)
}

func (o *Owner) Machine(ctx context.Context, command Command) error {
	return o.mutate(ctx, "machine", command)
}

func (o *Owner) GiveUp(ctx context.Context, command Command) error {
	command.IncludeQuest = true
	return o.mutate(ctx, "give-up", command)
}

func (o *Owner) mutate(ctx context.Context, operation string, command Command) error {
	if o == nil || o.settlements == nil {
		return ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(command.AccountID)
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
	return o.settlements.WithinCharacterSettlement(ctx, characterID, func(tx dnfrepo.Group) error {
		if tx.Character == nil || tx.Inventory == nil || tx.AccountInventory == nil || (command.IncludeQuest && tx.Quest == nil) {
			return ErrOwnerUnavailable
		}
		character, found, err := tx.Character.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(character.CharacterID) != characterID {
			return fmt.Errorf("%w: character=%s operation=%s", ErrCharacterNotFound, characterID, operation)
		}
		if accountID == "" {
			accountID = strings.TrimSpace(character.AccountID)
		}
		if strings.TrimSpace(character.AccountID) != accountID {
			return fmt.Errorf("%w: account=%s character=%s operation=%s", ErrAccountMismatch, accountID, characterID, operation)
		}
		inventory, found, err := tx.Inventory.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(inventory.CharacterID) != characterID || inventory.Slots == nil {
			return fmt.Errorf("%w: character=%s operation=%s", ErrInventoryNotFound, characterID, operation)
		}
		accountInventory, _, err := tx.AccountInventory.Load(ctx, accountID)
		if err != nil {
			return err
		}
		character = dnfrepo.CloneCharacter(character)
		inventory = dnfrepo.CloneInventory(inventory)
		accountInventory = dnfrepo.CloneAccountInventory(accountInventory)
		accountInventory.AccountID = accountID
		if accountInventory.Slots == nil {
			accountInventory.Slots = make(map[string]dnfrepo.ItemStack)
		}
		var quest *dnfrepo.QuestRecord
		if command.IncludeQuest {
			record, _, loadErr := tx.Quest.Load(ctx, characterID)
			if loadErr != nil {
				return loadErr
			}
			record = dnfrepo.CloneQuest(record)
			record.CharacterID = characterID
			if record.States == nil {
				record.States = make(map[int64]dnfrepo.QuestState)
			}
			if record.Progress == nil {
				record.Progress = make(map[int64]dnfrepo.QuestState)
			}
			quest = &record
		}
		changes, err := command.Project(&Assets{
			Character:        &character,
			AccountInventory: &accountInventory,
			Inventory:        &inventory,
			Quest:            quest,
		})
		if err != nil {
			return err
		}
		if changes.Character {
			character.UpdatedAt = now
			if err := dnfrepo.SaveCharacterFields(ctx, tx.Character, character, dnfrepo.CharacterFieldStats); err != nil {
				return err
			}
		}
		if changes.Inventory {
			inventory.UpdatedAt = now
			if err := dnfrepo.SaveInventoryFields(ctx, tx.Inventory, inventory, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
		}
		if changes.AccountInventory {
			accountInventory.UpdatedAt = now
			if err := tx.AccountInventory.Save(ctx, accountInventory); err != nil {
				return err
			}
		}
		if changes.Quest {
			if quest == nil {
				return ErrProjectorRequired
			}
			quest.UpdatedAt = now
			if err := dnfrepo.SaveQuestFields(ctx, tx.Quest, *quest, dnfrepo.QuestFieldStates, dnfrepo.QuestFieldProgress); err != nil {
				return err
			}
		}
		return nil
	})
}
