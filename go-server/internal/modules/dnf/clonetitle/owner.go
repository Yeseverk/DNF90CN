package clonetitle

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnftitlebook "longheng.io/server/internal/modules/dnf/titlebook"
)

var (
	ErrOwnerUnavailable  = errors.New("clone title owner unavailable")
	ErrCharacterRequired = errors.New("selected character id required")
	ErrCharacterNotFound = errors.New("character record not found")
	ErrAccountMismatch   = errors.New("character account does not match session account")
	ErrTitleNotOwned     = errors.New("clone title is not owned in the title book")
)

const statKey = "clone_title_item_id"

type Command struct {
	AccountID           string
	SelectedCharacterID uint16
	ItemID              int32
	UpdatedAt           time.Time
}

type Result struct {
	CharacterID string
	ItemID      int32
}

// Owner owns clone-title selection. The bridge owns only the current EXE wire
// format and response ordering.
type Owner struct {
	characters dnfrepo.CharacterRepository
	inventory  dnfrepo.InventoryRepository
}

func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Character == nil || repos.Inventory == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{
		characters: repos.Character,
		inventory:  repos.Inventory,
	}, nil
}

func (o *Owner) Apply(ctx context.Context, cmd Command) (Result, error) {
	if o == nil || o.characters == nil || o.inventory == nil {
		return Result{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return Result{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	character, found, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return Result{}, err
	}
	if !found || strings.TrimSpace(character.CharacterID) != characterID {
		return Result{}, fmt.Errorf("%w: character=%s", ErrCharacterNotFound, characterID)
	}
	if accountID := strings.TrimSpace(cmd.AccountID); accountID != "" &&
		strings.TrimSpace(character.AccountID) != accountID {
		return Result{}, fmt.Errorf("%w: character=%s", ErrAccountMismatch, characterID)
	}
	if cmd.ItemID < 0 {
		return Result{}, fmt.Errorf("%w: character=%s item=%d", ErrTitleNotOwned, characterID, cmd.ItemID)
	}
	if cmd.ItemID != 0 {
		inventory, found, loadErr := o.inventory.Load(ctx, characterID)
		if loadErr != nil {
			return Result{}, loadErr
		}
		if !found || !ownsTitleBookItem(inventory, int64(cmd.ItemID)) {
			return Result{}, fmt.Errorf("%w: character=%s item=%d", ErrTitleNotOwned, characterID, cmd.ItemID)
		}
	}

	character = dnfrepo.CloneCharacter(character)
	if character.Stats == nil {
		character.Stats = make(map[string]int64, 1)
	}
	character.Stats[statKey] = int64(cmd.ItemID)
	if cmd.UpdatedAt.IsZero() {
		character.UpdatedAt = time.Now().UTC()
	} else {
		character.UpdatedAt = cmd.UpdatedAt.UTC()
	}
	if err := dnfrepo.SaveCharacterFields(ctx, o.characters, character, dnfrepo.CharacterFieldStats); err != nil {
		return Result{}, err
	}
	return Result{CharacterID: characterID, ItemID: cmd.ItemID}, nil
}

func ownsTitleBookItem(inventory dnfrepo.InventoryRecord, itemID int64) bool {
	if itemID <= 0 {
		return false
	}
	prefix := strconv.Itoa(int(dnftitlebook.ListType)) + ":"
	for key, stack := range inventory.Slots {
		if strings.HasPrefix(key, prefix) && stack.ItemID == itemID && stack.Count != 0 {
			return true
		}
	}
	return false
}
