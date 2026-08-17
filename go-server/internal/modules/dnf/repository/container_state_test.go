package repository

import (
	"errors"
	"testing"
)

func TestCharacterContainerStateFromSettingsAcceptsCurrentExeInitialState(t *testing.T) {
	state, err := characterContainerStateFromSettings(SettingsRecord{
		Values: map[string]string{
			"source":                      "current_exe_86jp_op13_container_state",
			"main_list_param16":           "24",
			"avatar_list_param16":         "0",
			"personal_cargo_list_param16": "8",
			"account_cargo_selection_key": "3",
			"account_cargo_value32":       "0x1234",
		},
	}, "27")
	if err != nil {
		t.Fatalf("decode container state: %v", err)
	}
	if state.CharacterID != "27" || state.MainSlotCount != 24 || state.AvatarExpansion != 0 ||
		state.PersonalCargoSlotCount != 8 || state.AccountCargoSelectionKey != 3 ||
		state.AccountCargoStateValue != 0x1234 {
		t.Fatalf("container state = %+v", state)
	}
}

func TestCurrentMainInventoryExpansionMatchesPythonWireTable(t *testing.T) {
	for _, value := range []uint16{0, 8, 16, 24} {
		if !currentMainInventoryExpansion(value) {
			t.Fatalf("main expansion %d rejected", value)
		}
	}
	for _, value := range []uint16{1, 7, 23, 25, 32} {
		if currentMainInventoryExpansion(value) {
			t.Fatalf("invalid main expansion %d accepted", value)
		}
	}
}

func TestCharacterContainerStateRejectsValuesOutsideCurrentExeShape(t *testing.T) {
	tests := []map[string]string{
		{
			"main_list_param16":           "23",
			"avatar_list_param16":         "0",
			"personal_cargo_list_param16": "8",
		},
		{
			"main_list_param16":           "24",
			"avatar_list_param16":         "0",
			"personal_cargo_list_param16": "9",
		},
	}
	for _, values := range tests {
		_, err := characterContainerStateFromSettings(SettingsRecord{Values: values}, "27")
		if !errors.Is(err, ErrCharacterContainerStateInvalid) {
			t.Fatalf("values=%v err=%v", values, err)
		}
	}
}

func TestCurrentPersonalCargoSlotCountMatchesCurrentExeTable(t *testing.T) {
	for _, value := range []uint16{8, 24, 40, 56, 72, 88, 104, 120, 136, 152, 168, 184, 200} {
		if !currentPersonalCargoSlotCount(value) {
			t.Fatalf("current EXE slot count %d rejected", value)
		}
	}
	for _, value := range []uint16{0, 7, 9, 199, 201} {
		if currentPersonalCargoSlotCount(value) {
			t.Fatalf("non-table slot count %d accepted", value)
		}
	}
}
