package emotion

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable  = errors.New("emotion owner unavailable")
	ErrCharacterRequired = errors.New("selected character id required")
	ErrCharacterNotFound = errors.New("character record not found")
	ErrAccountMismatch   = errors.New("character account does not match session account")
)

const statKey = "emotion_index"

// Command contains the transport-independent identity and emotion selected by
// the current character.
type Command struct {
	AccountID           string
	SelectedCharacterID uint16
	EmotionIndex        uint16
	UpdatedAt           time.Time
}

type Result struct {
	CharacterID  string
	EmotionIndex uint16
}

// Owner owns the durable emotion state. Packet decoding and acknowledgements
// remain in dnfbridge.
type Owner struct {
	characters dnfrepo.CharacterRepository
}

func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Character == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{characters: repos.Character}, nil
}

func (o *Owner) Apply(ctx context.Context, cmd Command) (Result, error) {
	if o == nil || o.characters == nil {
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

	character = dnfrepo.CloneCharacter(character)
	if character.Stats == nil {
		character.Stats = make(map[string]int64, 1)
	}
	character.Stats[statKey] = int64(cmd.EmotionIndex)
	character.UpdatedAt = commandTime(cmd.UpdatedAt)
	if err := dnfrepo.SaveCharacterFields(ctx, o.characters, character, dnfrepo.CharacterFieldStats); err != nil {
		return Result{}, err
	}
	return Result{CharacterID: characterID, EmotionIndex: cmd.EmotionIndex}, nil
}

func commandTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}
