package adventuregroup

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const SelectorSlotMetadataKey = "selector_adventure_info_slot"

var (
	ErrOwnerUnavailable  = errors.New("adventure-group owner is unavailable")
	ErrAccountRequired   = errors.New("adventure-group account id is required")
	ErrAccountNotFound   = errors.New("adventure-group account is missing")
	ErrSelectorSlotRange = errors.New("adventure-group selector slot is outside the verified range")
)

type RememberSelectorSlotCommand struct {
	AccountID string
	Slot      int
	SlotLimit int
	UpdatedAt time.Time
}

type RememberSelectorSlotResult struct {
	AccountID string
	Slot      int
	Changed   bool
}

type Owner struct {
	accounts dnfrepo.AccountRepository
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.Account == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{accounts: repositories.Account}, nil
}

// RememberSelectorSlot persists only a slot already verified against the
// account roster by the bridge. The metadata write is key-scoped so unrelated
// account fields and metadata cannot be replaced.
func (o *Owner) RememberSelectorSlot(
	ctx context.Context,
	command RememberSelectorSlotCommand,
) (RememberSelectorSlotResult, error) {
	if o == nil || o.accounts == nil {
		return RememberSelectorSlotResult{}, ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(command.AccountID)
	if accountID == "" {
		return RememberSelectorSlotResult{}, ErrAccountRequired
	}
	if command.SlotLimit <= 0 || command.Slot < 0 || command.Slot >= command.SlotLimit {
		return RememberSelectorSlotResult{}, fmt.Errorf(
			"%w: slot=%d limit=%d",
			ErrSelectorSlotRange,
			command.Slot,
			command.SlotLimit,
		)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RememberSelectorSlotResult{}, err
	}
	account, found, err := o.accounts.Load(ctx, accountID)
	if err != nil {
		return RememberSelectorSlotResult{}, fmt.Errorf("load adventure-group account: %w", err)
	}
	if !found || strings.TrimSpace(account.AccountID) != accountID {
		return RememberSelectorSlotResult{}, fmt.Errorf("%w: account=%s", ErrAccountNotFound, accountID)
	}
	result := RememberSelectorSlotResult{AccountID: accountID, Slot: command.Slot}
	value := strconv.Itoa(command.Slot)
	if strings.TrimSpace(account.Metadata[SelectorSlotMetadataKey]) == value {
		return result, nil
	}
	if err := dnfrepo.SaveAccountMetadataEntry(
		ctx,
		o.accounts,
		account,
		SelectorSlotMetadataKey,
		value,
		command.UpdatedAt,
	); err != nil {
		return RememberSelectorSlotResult{}, fmt.Errorf("save adventure-group selector slot: %w", err)
	}
	result.Changed = true
	return result, nil
}
