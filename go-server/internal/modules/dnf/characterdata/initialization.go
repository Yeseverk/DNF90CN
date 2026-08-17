package characterdata

import (
	"context"
	"fmt"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// Initialization is the durable, protocol-independent state used to repair a
// legacy character that predates the normal new-character creation aggregate.
// Every supplied child is inserted only when its complete record is absent.
// A present-but-empty record is intentional player state and is never changed.
type Initialization struct {
	CharacterID string
	Inventory   *dnfrepo.InventoryRecord
	Equipment   *dnfrepo.EquipmentRecord
	Skill       *dnfrepo.SkillRecord
	Settings    []dnfrepo.SettingsRecord
}

// InitializationResult reports the records inserted by one idempotent repair.
// It deliberately has no "updated" state: existing data is never overwritten.
type InitializationResult struct {
	Inventory bool
	Equipment bool
	Skill     bool
	Settings  []string
}

func (r InitializationResult) Changed() bool {
	return r.Inventory || r.Equipment || r.Skill || len(r.Settings) > 0
}

// Initializer applies an Initialization through the same aggregate transaction
// used for creation.  This mirrors the 86JP inventory bootstrap model while
// preserving established character state on every subsequent login.
type Initializer struct {
	repositories dnfrepo.Group
}

func NewInitializer(repositories dnfrepo.Group) *Initializer {
	return &Initializer{repositories: repositories}
}

func (i *Initializer) Ensure(ctx context.Context, initialization Initialization) (InitializationResult, error) {
	if i == nil {
		return InitializationResult{}, dnfrepo.ErrCharacterCreationTransactionUnavailable
	}
	initialization, err := normalizeInitialization(initialization)
	if err != nil {
		return InitializationResult{}, err
	}
	if !initializationHasRecords(initialization) {
		return InitializationResult{}, nil
	}
	if i.repositories.CharacterCreate == nil {
		return InitializationResult{}, dnfrepo.ErrCharacterCreationTransactionUnavailable
	}

	ctx = ctxOrBackground(ctx)
	var result InitializationResult
	err = i.repositories.CharacterCreate.WithinCharacterCreation(ctx, initialization.CharacterID, i.repositories, func(tx dnfrepo.Group) error {
		if initialization.Inventory != nil {
			if tx.Inventory == nil {
				return repositoryError("inventory")
			}
			_, found, loadErr := tx.Inventory.Load(ctx, initialization.CharacterID)
			if loadErr != nil {
				return fmt.Errorf("load inventory before initialization: %w", loadErr)
			}
			if !found {
				if saveErr := tx.Inventory.Save(ctx, *initialization.Inventory); saveErr != nil {
					return fmt.Errorf("save initialization inventory: %w", saveErr)
				}
				result.Inventory = true
			}
		}

		if initialization.Equipment != nil {
			if tx.Equipment == nil {
				return repositoryError("equipment")
			}
			_, found, loadErr := tx.Equipment.Load(ctx, initialization.CharacterID)
			if loadErr != nil {
				return fmt.Errorf("load equipment before initialization: %w", loadErr)
			}
			if !found {
				if saveErr := tx.Equipment.Save(ctx, *initialization.Equipment); saveErr != nil {
					return fmt.Errorf("save initialization equipment: %w", saveErr)
				}
				result.Equipment = true
			}
		}

		if initialization.Skill != nil {
			if tx.Skill == nil {
				return repositoryError("skill")
			}
			_, found, loadErr := tx.Skill.Load(ctx, initialization.CharacterID)
			if loadErr != nil {
				return fmt.Errorf("load skill before initialization: %w", loadErr)
			}
			if !found {
				if saveErr := tx.Skill.Save(ctx, *initialization.Skill); saveErr != nil {
					return fmt.Errorf("save initialization skill: %w", saveErr)
				}
				result.Skill = true
			}
		}

		if len(initialization.Settings) > 0 {
			if tx.Settings == nil {
				return repositoryError("settings")
			}
			for _, setting := range initialization.Settings {
				_, found, loadErr := tx.Settings.Load(ctx, setting.Scope)
				if loadErr != nil {
					return fmt.Errorf("load initialization setting %q: %w", setting.Scope, loadErr)
				}
				if found {
					continue
				}
				if saveErr := tx.Settings.Save(ctx, setting); saveErr != nil {
					return fmt.Errorf("save initialization setting %q: %w", setting.Scope, saveErr)
				}
				result.Settings = append(result.Settings, setting.Scope)
			}
		}
		return nil
	})
	if err != nil {
		return InitializationResult{}, fmt.Errorf("persist character initialization: %w", err)
	}
	return result, nil
}

func normalizeInitialization(initialization Initialization) (Initialization, error) {
	characterID := strings.TrimSpace(initialization.CharacterID)
	if characterID == "" {
		return Initialization{}, ErrCharacterIDRequired
	}
	initialization.CharacterID = characterID
	if initialization.Inventory != nil {
		copy := dnfrepo.CloneInventory(*initialization.Inventory)
		if err := normalizeChildID(characterID, &copy.CharacterID); err != nil {
			return Initialization{}, err
		}
		initialization.Inventory = &copy
	}
	if initialization.Equipment != nil {
		copy := dnfrepo.CloneEquipment(*initialization.Equipment)
		if err := normalizeChildID(characterID, &copy.CharacterID); err != nil {
			return Initialization{}, err
		}
		initialization.Equipment = &copy
	}
	if initialization.Skill != nil {
		copy := dnfrepo.CloneSkill(*initialization.Skill)
		if err := normalizeChildID(characterID, &copy.CharacterID); err != nil {
			return Initialization{}, err
		}
		initialization.Skill = &copy
	}
	initialization.Settings = append([]dnfrepo.SettingsRecord(nil), initialization.Settings...)
	for index := range initialization.Settings {
		initialization.Settings[index] = dnfrepo.CloneSettings(initialization.Settings[index])
		if strings.TrimSpace(initialization.Settings[index].Scope) == "" {
			return Initialization{}, fmt.Errorf("setting scope is required at index %d", index)
		}
	}
	return initialization, nil
}

func initializationHasRecords(initialization Initialization) bool {
	return initialization.Inventory != nil ||
		initialization.Equipment != nil ||
		initialization.Skill != nil ||
		len(initialization.Settings) > 0
}
