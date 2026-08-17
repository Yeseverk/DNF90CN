package combatpower

import (
	"context"
	"os"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestInspectCurrentCombatPowerEquipmentPVF(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to inspect current combat-power equipment")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	itemIDs := []int64{
		112550232, 112560208, 112570168, 112500235, 112510219,
		112540236, 112520208, 112530202, 112580095, 101590068,
		112600041, 101590023, 101030741, 400330106, 100070551,
		100170519, 100120544, 100270515, 100220519, 100300733,
		100312425, 100322294, 100344527, 100352839, 100390011,
		400990168, 10006783, 10006784, 10006785, 100330501,
	}
	for _, itemID := range itemIDs {
		item, err := catalog.item(context.Background(), itemID)
		if err != nil {
			t.Fatalf("item %d: %v", itemID, err)
		}
		text, err := archive.ReadText(item.Path)
		if err != nil {
			t.Fatalf("read item %d at %s: %v", itemID, item.Path, err)
		}
		doc, err := dnfpvf.Parse(item.Path, text)
		if err != nil {
			t.Fatalf("parse item %d at %s: %v", itemID, item.Path, err)
		}
		t.Logf("item=%d path=%s set=%d rarity=%v min_level=%v grade=%v type=%v sub_type=%v",
			itemID, item.Path, item.PartSetID,
			doc.Numbers("rarity"), doc.Numbers("minimum level"),
			doc.Numbers("grade"), doc.Texts("equipment type"), doc.Texts("sub type"))
	}
	result, err := catalog.Aggregate(context.Background(), itemIDs)
	if err != nil {
		t.Fatal(err)
	}
	if result.EquippedItems != 30 || result.ScoredItems != 30 ||
		result.Level90EpicItems != 12 || result.PVFEquipmentScore != 38835 {
		t.Fatalf("real PVF equipment score result=%+v", result)
	}
}
