package town

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable  = errors.New("town owner is unavailable")
	ErrCharacterRequired = errors.New("town character id is required")
	ErrProjectorRequired = errors.New("town character projector is required")
	ErrCharacterNotFound = errors.New("town character is missing")
	ErrAccountMismatch   = errors.New("town character does not belong to account")
)

type CharacterProjector func(*dnfrepo.CharacterRecord) (bool, error)

type LoginLocationCommand struct {
	AccountID   string
	CharacterID string
	UpdatedAt   time.Time
	Project     CharacterProjector
}

type LoginLocationResult struct {
	Character dnfrepo.CharacterRecord
	Changed   bool
}

// Owner owns durable town-location changes. Runtime-PVF route resolution,
// session snapshots, current-client packets, and transition ordering remain in
// the bridge.
type Owner struct {
	assets dnfrepo.CharacterAssetUnitOfWork
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.CharacterAssets == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{assets: repositories.CharacterAssets}, nil
}

func (o *Owner) ApplyLoginLocation(ctx context.Context, command LoginLocationCommand) (LoginLocationResult, error) {
	if o == nil || o.assets == nil {
		return LoginLocationResult{}, ErrOwnerUnavailable
	}
	characterID := strings.TrimSpace(command.CharacterID)
	if characterID == "" {
		return LoginLocationResult{}, ErrCharacterRequired
	}
	if command.Project == nil {
		return LoginLocationResult{}, ErrProjectorRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return LoginLocationResult{}, err
	}
	now := command.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	accountID := strings.TrimSpace(command.AccountID)

	var result LoginLocationResult
	err := o.assets.WithinCharacterAssets(ctx, characterID, func(
		characters dnfrepo.CharacterRepository,
		_ dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
	) error {
		if characters == nil {
			return ErrOwnerUnavailable
		}
		character, found, err := characters.Load(ctx, characterID)
		if err != nil {
			return fmt.Errorf("load town character: %w", err)
		}
		if !found || strings.TrimSpace(character.CharacterID) != characterID {
			return fmt.Errorf("%w: character=%s", ErrCharacterNotFound, characterID)
		}
		if accountID != "" && strings.TrimSpace(character.AccountID) != accountID {
			return fmt.Errorf("%w: account=%s character=%s", ErrAccountMismatch, accountID, characterID)
		}
		character = dnfrepo.CloneCharacter(character)
		changed, err := command.Project(&character)
		if err != nil {
			return err
		}
		result = LoginLocationResult{Character: character, Changed: changed}
		if !changed {
			return nil
		}
		character.UpdatedAt = now
		result.Character.UpdatedAt = now
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return fmt.Errorf("save town character location: %w", err)
		}
		return nil
	})
	return result, err
}
