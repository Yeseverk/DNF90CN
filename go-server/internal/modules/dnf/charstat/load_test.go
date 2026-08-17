package charstat

import (
	"context"
	"testing"
)

type memSource map[string]string

func (s memSource) ReadText(relativePath string) (string, error) {
	if text, ok := s[cleanPath(relativePath)]; ok {
		return text, nil
	}
	return "", errMissing(relativePath)
}

type errMissing string

func (e errMissing) Error() string { return "missing pvf text: " + string(e) }

func TestLoadComputesJobGrowLevelFromPVF(t *testing.T) {
	source := memSource{
		"character/character.lst": "11 `female_swordman.chr`\n",
		"character/female_swordman.chr": `
[initial value]
[HP MAX] 100
[MP MAX] 50
[strength] 8
[intelligence] 9
[vitality] 7
[spirit] 6
[physical attack] 3
[magical attack] 4
[inventory limit] 48000
[move speed] 850
[growtype 1]
[HP MAX] 10
[MP MAX] 20
[strength] 1
[growtype 2]
[HP MAX] 100
[MP MAX] 200
[strength] 10
[awakening 1]
[HP MAX] 1000
[MP MAX] 2000
[strength] 100
`,
	}

	table, err := Load(context.Background(), source, Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, err := table.Compute(11, 0x11, 51)
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}

	if got.HPMax != 1000+14*100+35*1000+10000 {
		t.Fatalf("HPMax = %d", got.HPMax)
	}
	if got.MPMax != 500+14*200+35*2000+20000 {
		t.Fatalf("MPMax = %d", got.MPMax)
	}
	if got.Strength != 8+14*1+35*10+100 {
		t.Fatalf("Strength = %d", got.Strength)
	}
	if got.InventoryLimit != 480000 {
		t.Fatalf("InventoryLimit = %d", got.InventoryLimit)
	}
	if got.MoveSpeed != 8500 {
		t.Fatalf("MoveSpeed = %d", got.MoveSpeed)
	}
}

func TestLoadMapsLegacyAttackDefenseTagsToPrimaryStats(t *testing.T) {
	source := memSource{
		"character/character.lst": "11 `female_swordman.chr`\n",
		"character/female_swordman.chr": `
[initial value]
[HP MAX] 170
[MP MAX] 150
[physical attack] 7
[physical defense] 6
[magical attack] 5
[magical defense] 4
[growtype 1]
[HP MAX] 1
`,
	}
	table, err := Load(context.Background(), source, Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, err := table.Compute(11, 0, 1)
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if got.Strength != 7 || got.Vitality != 6 || got.Intelligence != 5 || got.Spirit != 4 {
		t.Fatalf("primary stats = str:%d vit:%d int:%d spr:%d", got.Strength, got.Vitality, got.Intelligence, got.Spirit)
	}
	if got.PhysicalAttack != 7 || got.PhysicalDefense != 6 || got.MagicalAttack != 5 || got.MagicalDefense != 4 {
		t.Fatalf("combat stats = pa:%d pd:%d ma:%d md:%d", got.PhysicalAttack, got.PhysicalDefense, got.MagicalAttack, got.MagicalDefense)
	}
}

func TestComputeReturnsErrorWhenGrowthMissing(t *testing.T) {
	source := memSource{
		"character/character.lst": "1 `fighter.chr`\n",
		"character/fighter.chr":   "[initial value]\n[HP MAX] 1\n[growtype 1]\n[HP MAX] 1\n",
	}
	table, err := Load(context.Background(), source, Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, err := table.Compute(1, 0x01, 16); err == nil {
		t.Fatalf("Compute() expected missing growth error")
	}
}
