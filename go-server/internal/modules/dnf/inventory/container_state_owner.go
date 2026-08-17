package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrContainerStateOwnerUnavailable  = errors.New("inventory container-state owner is unavailable")
	ErrContainerStateCharacterRequired = errors.New("inventory container-state character id is required")
	ErrContainerStateFactoryRequired   = errors.New("inventory container-state factory is required")
	ErrContainerStateScopeMismatch     = errors.New("inventory container-state scope mismatch")
)

type InitialContainerStateFactory func(characterID string, now time.Time) dnfrepo.SettingsRecord

type EnsureContainerStateCommand struct {
	CharacterID string
	UpdatedAt   time.Time
	Initial     InitialContainerStateFactory
}

type EnsureContainerStateResult struct {
	State   dnfrepo.CharacterContainerState
	Created bool
}

// ContainerStateOwner owns missing-only persistence for the current-client
// item-container header. The bridge supplies the evidence-backed initial row;
// this owner validates its scope and typed values before it becomes durable.
type ContainerStateOwner struct {
	settings dnfrepo.SettingsRepository
}

func NewContainerStateOwner(settings dnfrepo.SettingsRepository) (*ContainerStateOwner, error) {
	if settings == nil {
		return nil, ErrContainerStateOwnerUnavailable
	}
	return &ContainerStateOwner{settings: settings}, nil
}

func (o *ContainerStateOwner) Ensure(ctx context.Context, command EnsureContainerStateCommand) (EnsureContainerStateResult, error) {
	if o == nil || o.settings == nil {
		return EnsureContainerStateResult{}, ErrContainerStateOwnerUnavailable
	}
	characterID := strings.TrimSpace(command.CharacterID)
	if characterID == "" {
		return EnsureContainerStateResult{}, ErrContainerStateCharacterRequired
	}
	if command.Initial == nil {
		return EnsureContainerStateResult{}, ErrContainerStateFactoryRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return EnsureContainerStateResult{}, err
	}
	state, found, err := dnfrepo.LoadCharacterContainerState(ctx, o.settings, characterID)
	if err != nil {
		return EnsureContainerStateResult{}, err
	}
	if found {
		return EnsureContainerStateResult{State: state}, nil
	}

	now := command.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	initial := command.Initial(characterID, now)
	wantScope := dnfrepo.CharacterContainerStateScope(characterID)
	if strings.TrimSpace(initial.Scope) != wantScope {
		return EnsureContainerStateResult{}, fmt.Errorf(
			"%w: got=%q want=%q",
			ErrContainerStateScopeMismatch,
			initial.Scope,
			wantScope,
		)
	}
	initial.UpdatedAt = now
	if _, err := dnfrepo.CharacterContainerStateFromSettings(initial, characterID); err != nil {
		return EnsureContainerStateResult{}, err
	}
	if err := o.settings.Save(ctx, initial); err != nil {
		return EnsureContainerStateResult{}, fmt.Errorf("save initial inventory container state: %w", err)
	}
	state, found, err = dnfrepo.LoadCharacterContainerState(ctx, o.settings, characterID)
	if err != nil {
		return EnsureContainerStateResult{}, err
	}
	if !found {
		return EnsureContainerStateResult{}, fmt.Errorf("%w: initialized row is missing", dnfrepo.ErrCharacterContainerStateInvalid)
	}
	return EnsureContainerStateResult{State: state, Created: true}, nil
}
