package dnfbridge

import (
	"context"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestCharacterPVFStatsForUserInfoAppliesCSharpHPMPCompatibility(t *testing.T) {
	service := Service{characterStats: testCharacterStatTable(t)}
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
	}

	first, ok := service.characterPVFStatsForUserInfo(context.Background(), nil, character, true)
	if !ok {
		t.Fatal("first PVF stat calculation failed")
	}
	second, ok := service.characterPVFStatsForUserInfo(context.Background(), nil, character, true)
	if !ok {
		t.Fatal("second PVF stat calculation failed")
	}
	if first.HPMax != 11000 || first.MPMax != 11800 {
		t.Fatalf("first HP/MP = %d/%d, want 11000/11800", first.HPMax, first.MPMax)
	}
	if second.HPMax != first.HPMax || second.MPMax != first.MPMax {
		t.Fatalf("recalculation HP/MP = %d/%d, want idempotent %d/%d", second.HPMax, second.MPMax, first.HPMax, first.MPMax)
	}
	if first.Strength != 11 || first.Intelligence != 12 || first.Vitality != 13 || first.Spirit != 14 {
		t.Fatalf("non-HP/MP stats changed: %+v", first)
	}

	applyCharacterPVFStats(&character, first)
	if character.Stats["stat_hp_max"] != 11000 || character.Stats["stat_mp_max"] != 11800 {
		t.Fatalf("persisted HP/MP = %d/%d, want 11000/11800", character.Stats["stat_hp_max"], character.Stats["stat_mp_max"])
	}
}
