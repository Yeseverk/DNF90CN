package pet

import (
	"errors"
	"os"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestPVFCatalogResolvesArtifactTypes(t *testing.T) {
	catalog := newPetArtifactTestCatalog(t)
	tests := []struct {
		itemID int64
		kind   PetArtifactKind
		token  string
		path   string
	}{
		{itemID: 20, kind: PetArtifactKindRed, token: "[artifact red]", path: "equipment/creature/artifact_red/red.equ"},
		{itemID: 21, kind: PetArtifactKindBlue, token: "[artifact blue]", path: "equipment/creature/artifact_blue/blue.equ"},
		{itemID: 22, kind: PetArtifactKindGreen, token: "[artifact green]", path: "equipment/creature/artifact_green/green.equ"},
	}
	for _, test := range tests {
		definition, err := catalog.ResolveArtifact(test.itemID)
		if err != nil {
			t.Fatalf("item_id=%d: %v", test.itemID, err)
		}
		if definition.ItemID != test.itemID || definition.Kind != test.kind ||
			definition.EquipmentType != test.token || definition.PVFPath != test.path {
			t.Fatalf("item_id=%d definition=%+v", test.itemID, definition)
		}
	}
}

func TestPVFCatalogArtifactTypeResolutionFailsClosed(t *testing.T) {
	catalog := newPetArtifactTestCatalog(t)
	if _, err := catalog.ResolveArtifact(23); !errors.Is(err, ErrPetPVFArtifactTypeInvalid) {
		t.Fatalf("creature error=%v", err)
	}
	if _, err := catalog.ResolveArtifact(24); !errors.Is(err, ErrPetPVFArtifactTypeInvalid) {
		t.Fatalf("unknown equipment error=%v", err)
	}
	if _, err := catalog.ResolveArtifact(29); !errors.Is(err, ErrPetPVFArtifactTypeAmbiguous) {
		t.Fatalf("ambiguous equipment error=%v", err)
	}
	if _, err := catalog.ResolveArtifact(999); !errors.Is(err, ErrPetPVFEquipmentUnresolved) {
		t.Fatalf("unresolved equipment error=%v", err)
	}
}

func TestRealScriptPVFArtifactKinds(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify runtime pet artifact PVF")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewPVFCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		itemID int64
		kind   PetArtifactKind
		path   string
	}{
		{itemID: 63500, kind: PetArtifactKindRed, path: "equipment/creature/artifact_red/artifact_red.equ"},
		{itemID: 64000, kind: PetArtifactKindBlue, path: "equipment/creature/artifact_blue/artifact_blue.equ"},
		{itemID: 64500, kind: PetArtifactKindGreen, path: "equipment/creature/artifact_green/artifact_green.equ"},
	} {
		definition, err := catalog.ResolveArtifact(test.itemID)
		if err != nil {
			t.Fatalf("runtime item_id=%d: %v", test.itemID, err)
		}
		if definition.Kind != test.kind || definition.PVFPath != test.path {
			t.Fatalf("item_id=%d definition=%+v", test.itemID, definition)
		}
		t.Logf("runtime artifact kind=%s item_id=%d path=%s", test.kind, test.itemID, definition.PVFPath)
	}
}

func newPetArtifactTestCatalog(t *testing.T) *PVFCatalog {
	t.Helper()
	source := petCatalogTestSource{
		petEquipmentListPath: "20 `creature/artifact_red/red.equ`\n" +
			"21 `creature/artifact_blue/blue.equ`\n" +
			"22 `creature/artifact_green/green.equ`\n" +
			"23 `creature/creature.equ`\n" +
			"24 `weapon/unknown.equ`\n" +
			"29 `creature/artifact_red/ambiguous.equ`\n",
		"equipment/creature/artifact_red/red.equ":       "[equipment type] `[artifact red]`\n",
		"equipment/creature/artifact_blue/blue.equ":     "[equipment type] `[artifact blue]`\n",
		"equipment/creature/artifact_green/green.equ":   "[equipment type] `[artifact green]`\n",
		"equipment/creature/creature.equ":               "[equipment type] `[creature]`\n",
		"equipment/weapon/unknown.equ":                  "[equipment type] `[weapon]`\n",
		"equipment/creature/artifact_red/ambiguous.equ": "[equipment type] `[artifact red]` `[artifact blue]`\n",
		petCreatureExperiencePath:                       petCatalogTestExperienceText(),
	}
	catalog, err := NewPVFCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
