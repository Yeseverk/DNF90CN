package characterdata

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var ErrChildCharacterMismatch = errors.New("character child record belongs to another character")

// Creation contains the complete durable state produced by new-character
// defaults and PVF initialization before any write is attempted.
type Creation struct {
	Account   *dnfrepo.AccountRecord
	Character dnfrepo.CharacterRecord
	Inventory *dnfrepo.InventoryRecord
	Equipment *dnfrepo.EquipmentRecord
	Skill     *dnfrepo.SkillRecord
	Quest     *dnfrepo.QuestRecord
	Pet       *dnfrepo.PetRecord
	Settings  []dnfrepo.SettingsRecord
}

// Creator persists one Creation through the repository transaction boundary.
type Creator struct {
	repositories dnfrepo.Group
}

func NewCreator(repositories dnfrepo.Group) *Creator {
	return &Creator{repositories: repositories}
}

func (c *Creator) Create(ctx context.Context, creation Creation) error {
	if c == nil {
		return dnfrepo.ErrCharacterCreationTransactionUnavailable
	}
	creation, err := normalizeCreation(creation)
	if err != nil {
		return err
	}
	if c.repositories.CharacterCreate == nil {
		return dnfrepo.ErrCharacterCreationTransactionUnavailable
	}
	ctx = ctxOrBackground(ctx)
	characterID := creation.Character.CharacterID
	err = c.repositories.CharacterCreate.WithinCharacterCreation(ctx, characterID, c.repositories, func(tx dnfrepo.Group) error {
		if creation.Account != nil {
			if tx.Account == nil {
				return repositoryError("account")
			}
			account := dnfrepo.CloneAccount(*creation.Account)
			existing, found, err := tx.Account.Load(ctx, account.AccountID)
			if err != nil {
				return fmt.Errorf("load account before character creation: %w", err)
			}
			if found {
				// Character creation owns bootstrap fields, not account-wide
				// progression or unrelated metadata. Use the locked current row as
				// the base and overlay only the caller's explicit bootstrap state.
				if account.State != "" {
					existing.State = account.State
				}
				if existing.Metadata == nil && len(account.Metadata) > 0 {
					existing.Metadata = make(map[string]string, len(account.Metadata))
				}
				for key, value := range account.Metadata {
					existing.Metadata[key] = value
				}
				if existing.CreatedAt.IsZero() {
					existing.CreatedAt = account.CreatedAt
				}
				if !account.UpdatedAt.IsZero() {
					existing.UpdatedAt = account.UpdatedAt
				}
				account = existing
			}
			if err := tx.Account.Save(ctx, account); err != nil {
				return fmt.Errorf("save account: %w", err)
			}
		}
		if tx.Character == nil {
			return repositoryError("character")
		}
		if err := dnfrepo.CreateCharacter(ctx, tx.Character, creation.Character); err != nil {
			return fmt.Errorf("create character: %w", err)
		}
		if creation.Inventory != nil {
			if tx.Inventory == nil {
				return repositoryError("inventory")
			}
			if err := tx.Inventory.Save(ctx, *creation.Inventory); err != nil {
				return fmt.Errorf("save inventory: %w", err)
			}
		}
		if creation.Equipment != nil {
			if tx.Equipment == nil {
				return repositoryError("equipment")
			}
			if err := tx.Equipment.Save(ctx, *creation.Equipment); err != nil {
				return fmt.Errorf("save equipment: %w", err)
			}
		}
		if creation.Skill != nil {
			if tx.Skill == nil {
				return repositoryError("skill")
			}
			if err := tx.Skill.Save(ctx, *creation.Skill); err != nil {
				return fmt.Errorf("save skill: %w", err)
			}
		}
		if creation.Quest != nil {
			if tx.Quest == nil {
				return repositoryError("quest")
			}
			if err := tx.Quest.Save(ctx, *creation.Quest); err != nil {
				return fmt.Errorf("save quest: %w", err)
			}
		}
		if creation.Pet != nil {
			if tx.Pet == nil {
				return repositoryError("pet")
			}
			if err := tx.Pet.Save(ctx, *creation.Pet); err != nil {
				return fmt.Errorf("save pet: %w", err)
			}
		}
		if len(creation.Settings) > 0 {
			if tx.Settings == nil {
				return repositoryError("settings")
			}
			for _, record := range creation.Settings {
				if err := tx.Settings.Save(ctx, record); err != nil {
					return fmt.Errorf("save setting %q: %w", record.Scope, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist character aggregate: %w", err)
	}
	return nil
}

func normalizeCreation(creation Creation) (Creation, error) {
	characterID := strings.TrimSpace(creation.Character.CharacterID)
	if characterID == "" {
		return Creation{}, ErrCharacterIDRequired
	}
	creation.Character = dnfrepo.CloneCharacter(creation.Character)
	creation.Character.CharacterID = characterID
	if creation.Account != nil {
		copy := dnfrepo.CloneAccount(*creation.Account)
		if strings.TrimSpace(copy.AccountID) == "" {
			copy.AccountID = strings.TrimSpace(creation.Character.AccountID)
		}
		creation.Account = &copy
	}
	if creation.Inventory != nil {
		copy := dnfrepo.CloneInventory(*creation.Inventory)
		if err := normalizeChildID(characterID, &copy.CharacterID); err != nil {
			return Creation{}, err
		}
		creation.Inventory = &copy
	}
	if creation.Equipment != nil {
		copy := dnfrepo.CloneEquipment(*creation.Equipment)
		if err := normalizeChildID(characterID, &copy.CharacterID); err != nil {
			return Creation{}, err
		}
		creation.Equipment = &copy
	}
	if creation.Skill != nil {
		copy := dnfrepo.CloneSkill(*creation.Skill)
		if err := normalizeChildID(characterID, &copy.CharacterID); err != nil {
			return Creation{}, err
		}
		creation.Skill = &copy
	}
	if creation.Quest != nil {
		copy := dnfrepo.CloneQuest(*creation.Quest)
		if err := normalizeChildID(characterID, &copy.CharacterID); err != nil {
			return Creation{}, err
		}
		creation.Quest = &copy
	}
	if creation.Pet != nil {
		copy := dnfrepo.ClonePet(*creation.Pet)
		if err := normalizeChildID(characterID, &copy.CharacterID); err != nil {
			return Creation{}, err
		}
		creation.Pet = &copy
	}
	creation.Settings = append([]dnfrepo.SettingsRecord(nil), creation.Settings...)
	for index := range creation.Settings {
		creation.Settings[index] = dnfrepo.CloneSettings(creation.Settings[index])
		if strings.TrimSpace(creation.Settings[index].Scope) == "" {
			return Creation{}, fmt.Errorf("setting scope is required at index %d", index)
		}
	}
	return creation, nil
}

func normalizeChildID(characterID string, value *string) error {
	current := strings.TrimSpace(*value)
	if current != "" && current != characterID {
		return fmt.Errorf("%w: expected=%s actual=%s", ErrChildCharacterMismatch, characterID, current)
	}
	*value = characterID
	return nil
}
