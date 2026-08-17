package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrCharacterContainerStateInvalid = errors.New("character container state is invalid")

// CharacterContainerState is the typed view of the current EXE op13 header
// state. The field names follow current handler behavior, not the old C# wire
// model.
type CharacterContainerState struct {
	CharacterID              string
	MainSlotCount            uint16
	AvatarExpansion          uint16
	PersonalCargoSlotCount   uint16
	AccountCargoSelectionKey uint16
	AccountCargoStateValue   uint32
	Source                   string
	UpdatedAt                time.Time
}

func CharacterContainerStateScope(characterID string) string {
	return "character:" + strings.TrimSpace(characterID) + ":container_state"
}

// LoadCharacterContainerState reads the existing settings aggregate without
// mutating it or inventing state for characters that do not have a row.
func LoadCharacterContainerState(ctx context.Context, repo SettingsRepository, characterID string) (CharacterContainerState, bool, error) {
	characterID = strings.TrimSpace(characterID)
	if repo == nil || characterID == "" {
		return CharacterContainerState{}, false, nil
	}
	record, found, err := repo.Load(ctx, CharacterContainerStateScope(characterID))
	if err != nil || !found {
		return CharacterContainerState{}, found, err
	}
	state, err := characterContainerStateFromSettings(record, characterID)
	if err != nil {
		return CharacterContainerState{}, false, err
	}
	return state, true, nil
}

// CharacterContainerStateFromSettings validates one already loaded settings
// row. Transaction owners use it to avoid escaping their scoped repository.
func CharacterContainerStateFromSettings(record SettingsRecord, characterID string) (CharacterContainerState, error) {
	return characterContainerStateFromSettings(record, characterID)
}

func characterContainerStateFromSettings(record SettingsRecord, characterID string) (CharacterContainerState, error) {
	values := record.Values
	mainSlots, err := requiredContainerUint16(values, "main_list_param16")
	if err != nil {
		return CharacterContainerState{}, err
	}
	avatarExpansion, err := requiredContainerUint16(values, "avatar_list_param16")
	if err != nil {
		return CharacterContainerState{}, err
	}
	personalCargoSlots, err := requiredContainerUint16(values, "personal_cargo_list_param16")
	if err != nil {
		return CharacterContainerState{}, err
	}
	accountCargoKey, err := optionalContainerUint16(values, "account_cargo_selection_key")
	if err != nil {
		return CharacterContainerState{}, err
	}
	accountCargoValue, err := optionalContainerUint32(values, "account_cargo_value32")
	if err != nil {
		return CharacterContainerState{}, err
	}
	if !currentMainInventoryExpansion(mainSlots) {
		return CharacterContainerState{}, fmt.Errorf("%w: main_list_param16=%d is not in the current EXE expansion table", ErrCharacterContainerStateInvalid, mainSlots)
	}
	if !currentPersonalCargoSlotCount(personalCargoSlots) {
		return CharacterContainerState{}, fmt.Errorf("%w: personal_cargo_list_param16=%d is not in the current EXE slot table", ErrCharacterContainerStateInvalid, personalCargoSlots)
	}
	return CharacterContainerState{
		CharacterID:              strings.TrimSpace(characterID),
		MainSlotCount:            mainSlots,
		AvatarExpansion:          avatarExpansion,
		PersonalCargoSlotCount:   personalCargoSlots,
		AccountCargoSelectionKey: accountCargoKey,
		AccountCargoStateValue:   accountCargoValue,
		Source:                   strings.TrimSpace(values["source"]),
		UpdatedAt:                record.UpdatedAt,
	}, nil
}

func currentMainInventoryExpansion(value uint16) bool {
	return value <= 24 && value%8 == 0
}

func currentPersonalCargoSlotCount(value uint16) bool {
	return value >= 8 && value <= 200 && (value-8)%16 == 0
}

func requiredContainerUint16(values map[string]string, key string) (uint16, error) {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return 0, fmt.Errorf("%w: %s is missing", ErrCharacterContainerStateInvalid, key)
	}
	value, err := strconv.ParseUint(raw, 0, 16)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%q: %v", ErrCharacterContainerStateInvalid, key, raw, err)
	}
	return uint16(value), nil
}

func optionalContainerUint16(values map[string]string, key string) (uint16, error) {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 0, 16)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%q: %v", ErrCharacterContainerStateInvalid, key, raw, err)
	}
	return uint16(value), nil
}

func optionalContainerUint32(values map[string]string, key string) (uint32, error) {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 0, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%q: %v", ErrCharacterContainerStateInvalid, key, raw, err)
	}
	return uint32(value), nil
}
