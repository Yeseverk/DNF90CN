package pet

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

type petCatalogTestSource map[string]string

func (s petCatalogTestSource) ReadText(path string) (string, error) {
	if text, found := s[path]; found {
		return text, nil
	}
	return "", fmt.Errorf("missing %s", path)
}

func TestPVFCatalogResolvesHatchAndCreatureExperience(t *testing.T) {
	source := petCatalogTestSource{
		petEquipmentListPath:               "63006 `creature/egg_faras.equ`\n63000 `creature/faras.equ`\n",
		"equipment/creature/egg_faras.equ": "[equipment type] `[creature]`\n[output index] 63000\n",
		"equipment/creature/faras.equ":     "[name] `Faras`\n[equipment type] `[creature]`\n[minimum level] 3\n",
		petCreatureExperiencePath:          petCatalogTestExperienceText(),
	}
	catalog, err := NewPVFCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	var resolver PetHatchResolver = catalog
	definition, err := resolver.ResolveHatch(63006)
	if err != nil {
		t.Fatal(err)
	}
	if definition != (PetHatchDefinition{
		EggItemID:      63006,
		HatchedItemID:  63000,
		EggPVFPath:     "equipment/creature/egg_faras.equ",
		HatchedPVFPath: "equipment/creature/faras.equ",
		MinimumLevel:   3,
	}) {
		t.Fatalf("definition=%+v", definition)
	}
	creature, err := catalog.ResolveCreature(63000)
	if err != nil {
		t.Fatal(err)
	}
	if creature.Name != "Faras" || creature.PVFPath != "equipment/creature/faras.equ" {
		t.Fatalf("creature=%+v", creature)
	}
	table := catalog.ExperienceTable()
	if got := table.LevelForExperience(0); got != 1 {
		t.Fatalf("level(exp=0)=%d", got)
	}
	if got := table.LevelForExperience(2); got != 2 {
		t.Fatalf("level(exp=2)=%d", got)
	}
	if got := table.LevelForExperience(4); got != 2 {
		t.Fatalf("level(exp=4)=%d", got)
	}
	if got := table.LevelForExperience(5); got != 3 {
		t.Fatalf("level(exp=5)=%d", got)
	}
	if got := table.LevelForExperience(1 << 60); got != MaxCreatureLevel {
		t.Fatalf("level(max exp)=%d", got)
	}
	values := table.Thresholds()
	values[0] = 999999
	if catalog.ExperienceTable().Thresholds()[0] != 2 {
		t.Fatal("experience thresholds leaked mutable catalog state")
	}
}

func TestPVFCatalogDefaultsMissingCreatureMinimumLevelToOne(t *testing.T) {
	source := petCatalogTestSource{
		petEquipmentListPath:      "10 `egg.equ`\n11 `pet.equ`\n",
		"equipment/egg.equ":       "[equipment type] `[creature]`\n[output index] 11\n",
		"equipment/pet.equ":       "[equipment type] `[creature]`\n",
		petCreatureExperiencePath: petCatalogTestExperienceText(),
	}
	catalog, err := NewPVFCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := catalog.ResolveHatch(10)
	if err != nil {
		t.Fatal(err)
	}
	if definition.MinimumLevel != 1 {
		t.Fatalf("minimum level=%d", definition.MinimumLevel)
	}
}

func TestPVFCatalogRejectsUnprovedHatchMappings(t *testing.T) {
	base := petCatalogTestSource{
		petEquipmentListPath:      "10 `egg.equ`\n11 `pet.equ`\n",
		"equipment/egg.equ":       "[equipment type] `[creature]`\n[output index] 11\n",
		"equipment/pet.equ":       "[equipment type] `[creature]`\n",
		petCreatureExperiencePath: petCatalogTestExperienceText(),
	}
	tests := []struct {
		name   string
		mutate func(petCatalogTestSource)
		want   error
	}{
		{
			name: "source is not creature",
			mutate: func(source petCatalogTestSource) {
				source["equipment/egg.equ"] = "[equipment type] `[weapon]`\n[output index] 11\n"
			},
			want: ErrPetPVFNotCreature,
		},
		{
			name: "missing output",
			mutate: func(source petCatalogTestSource) {
				source["equipment/egg.equ"] = "[equipment type] `[creature]`\n"
			},
			want: ErrPetPVFHatchOutputInvalid,
		},
		{
			name: "same output",
			mutate: func(source petCatalogTestSource) {
				source["equipment/egg.equ"] = "[equipment type] `[creature]`\n[output index] 10\n"
			},
			want: ErrPetPVFHatchOutputInvalid,
		},
		{
			name: "output absent from equipment list",
			mutate: func(source petCatalogTestSource) {
				source["equipment/egg.equ"] = "[equipment type] `[creature]`\n[output index] 12\n"
			},
			want: ErrPetPVFEquipmentUnresolved,
		},
		{
			name: "output is not creature",
			mutate: func(source petCatalogTestSource) {
				source["equipment/pet.equ"] = "[equipment type] `[artifact red]`\n"
			},
			want: ErrPetPVFNotCreature,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := make(petCatalogTestSource, len(base))
			for key, value := range base {
				source[key] = value
			}
			test.mutate(source)
			catalog, err := NewPVFCatalog(source)
			if err != nil {
				t.Fatal(err)
			}
			_, err = catalog.ResolveHatch(10)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestPVFCatalogRejectsMalformedExperienceTable(t *testing.T) {
	base := petCatalogTestSource{
		petEquipmentListPath:      "10 `egg.equ`\n",
		petCreatureExperiencePath: petCatalogTestExperienceText(),
	}
	for _, text := range []string{
		"2 5 9",
		strings.Replace(petCatalogTestExperienceText(), " 5 ", " 2 ", 1),
	} {
		source := make(petCatalogTestSource, len(base))
		for key, value := range base {
			source[key] = value
		}
		source[petCreatureExperiencePath] = text
		if _, err := NewPVFCatalog(source); !errors.Is(err, ErrPetPVFExperienceTableShape) {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestRealScriptPVFPetCatalogHatchAndExperience(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the runtime pet catalog")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewPVFCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := catalog.ResolveHatch(63006)
	if err != nil {
		t.Fatal(err)
	}
	if definition.HatchedItemID != 63000 || definition.MinimumLevel != 1 {
		t.Fatalf("real hatch definition=%+v", definition)
	}
	thresholds := catalog.ExperienceTable().Thresholds()
	if len(thresholds) != MaxCreatureLevel || thresholds[0] != 2 || thresholds[len(thresholds)-1] != 3564 {
		t.Fatalf("real experience thresholds count=%d first=%d last=%d", len(thresholds), thresholds[0], thresholds[len(thresholds)-1])
	}
}

func petCatalogTestExperienceText() string {
	return "2 5 9 15 23 33 44 57 71 88 106 126 148 171 196 223 252 283 315 350 " +
		"388 429 472 517 565 615 668 725 785 849 918 993 1073 1158 1245 1335 1430 1532 1642 1759 " +
		"1879 2011 2151 2301 2464 2639 2829 3049 3294 5579\n"
}
