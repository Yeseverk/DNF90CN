package dnfbridge

import (
	"os"
	"reflect"
	"slices"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealPVF2018SpringPetDarkBeadKeepsCreatureWhitelist(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to validate the 2018 Spring pet bead")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}

	const (
		beadItemID   int64 = 490007596
		cardItemID   int64 = 10008663
		validPetID   int64 = 400990168
		invalidPetID int64 = 400990006
	)
	definition, err := catalog.ResolveItem(uint32(beadItemID))
	if err != nil {
		t.Fatal(err)
	}
	if definition.StackableType != "[enchant waste]" {
		t.Fatalf("bead stackable type = %q, want [enchant waste]", definition.StackableType)
	}
	document, err := parseDungeonCardPVFDocument(archive, definition.PVFPath)
	if err != nil {
		t.Fatal(err)
	}
	if name, _ := document.Text("name"); name != "远古精灵的祝福宝珠 [武力][暗属性增幅]" {
		t.Fatalf("bead name = %q", name)
	}
	if got, found := document.Int("monster card id"); !found || got != cardItemID {
		t.Fatalf("monster card id = %d found=%t, want %d", got, found, cardItemID)
	}
	wantTargets := []int64{400990167, 400990168, 400990170, 400990171}
	if got := document.Ints("bead limited usable item"); !reflect.DeepEqual(got, wantTargets) {
		t.Fatalf("bead limited usable item = %v, want %v", got, wantTargets)
	}

	resolution, err := resolveCurrentEnchantBeadMetadata(catalog, archive, beadItemID, validPetID)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.CardItemID != cardItemID || resolution.TargetKind != "equipment" ||
		resolution.TargetEquipmentType != "[creature]" ||
		!slices.Contains(resolution.AllowedEquipmentTypes, "[creature]") ||
		!reflect.DeepEqual(resolution.TargetWhitelist, wantTargets) {
		t.Fatalf("valid pet resolution = %+v", resolution)
	}

	invalidResolution, err := resolveCurrentEnchantBeadMetadata(catalog, archive, beadItemID, invalidPetID)
	if err != nil {
		t.Fatal(err)
	}
	if invalidResolution.CardItemID != cardItemID || slices.Contains(invalidResolution.TargetWhitelist, invalidPetID) {
		t.Fatalf("invalid pet resolution = %+v", invalidResolution)
	}
}
