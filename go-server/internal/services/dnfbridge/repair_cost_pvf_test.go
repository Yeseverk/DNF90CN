package dnfbridge

import (
	"os"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestNewCurrentRepairCostCatalogReadsGlobalRatesAndItemEvidence(t *testing.T) {
	source := bridgePVFSource{
		"equipment/pricetable.tbl": "[rate]\n200 150 30\n[repair cost]\n0.08\n[quick repair cost rate]\n150\n",
		"etc/upgrade.etc":          "[repair cost rate by upgrade level]\n1 1 1 1 2\n[/repair cost rate by upgrade level]\n",
		"equipment/coat/test.equ":  "[equipment type]\n`[coat]` 0\n[durability]\n28\n[repair price]\n6400\n[grade]\n20\n",
	}
	catalog, err := newCurrentRepairCostCatalog(source, map[int64]string{10018: "coat/test.equ"})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.repairCostRate != 0.08 || catalog.quickRepairRate != 1.5 {
		t.Fatalf("global rates = %v/%v", catalog.repairCostRate, catalog.quickRepairRate)
	}
	if len(catalog.upgradeRates) != 5 || catalog.upgradeRates[4] != 2 {
		t.Fatalf("upgrade rates = %+v", catalog.upgradeRates)
	}
	evidence, err := catalog.resolve(10018)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.MaxDurability != 28 || evidence.RepairPrice != 6400 || evidence.Grade != 20 || evidence.EquipmentType != "[coat]" {
		t.Fatalf("evidence = %+v", evidence)
	}
	// Unknown item: non-repairable marker, no error.
	unknown, err := catalog.resolve(99999999)
	if err != nil || unknown.MaxDurability != -1 {
		t.Fatalf("unknown evidence = %+v err=%v", unknown, err)
	}
	// Cached second read returns the same evidence.
	again, err := catalog.resolve(10018)
	if err != nil || again.MaxDurability != evidence.MaxDurability || again.RepairPrice != evidence.RepairPrice || again.EquipmentType != evidence.EquipmentType {
		t.Fatalf("cached resolve mismatch: %+v vs %+v", again, evidence)
	}
}

func TestNewCurrentRepairCostCatalogFailsClosedOnMissingPriceTable(t *testing.T) {
	if _, err := newCurrentRepairCostCatalog(bridgePVFSource{}, map[int64]string{}); err == nil {
		t.Fatal("missing pricetable.tbl must fail the catalog load")
	}
}

func TestRealPVFRepairCostCatalogCoversStarterEquipment(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("DNFBRIDGE_REAL_PVF_SMOKE not set")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open PVF: %v", err)
	}
	paths, err := initialEquipmentPathMap(archive)
	if err != nil {
		t.Fatalf("equipment paths: %v", err)
	}
	catalog, err := newCurrentRepairCostCatalog(archive, paths)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	// Runtime pricetable.tbl: [repair cost] 0.08, [quick repair cost rate] 150.
	if catalog.repairCostRate != 0.08 || catalog.quickRepairRate != 1.5 {
		t.Fatalf("real rates = %v/%v, want 0.08/1.5", catalog.repairCostRate, catalog.quickRepairRate)
	}
	if len(catalog.upgradeRates) == 0 {
		t.Fatal("real upgrade.etc [repair cost rate by upgrade level] missing")
	}
	// vest_owool.equ (item 10018): [durability] 28, [repair price] 6400,
	// [grade] 20, [equipment type] [coat].
	evidence, err := catalog.resolve(10018)
	if err != nil {
		t.Fatalf("resolve 10018: %v", err)
	}
	if evidence.MaxDurability != 28 || evidence.RepairPrice != 6400 || evidence.Grade != 20 || evidence.EquipmentType != "[coat]" {
		t.Fatalf("vest_owool evidence = %+v", evidence)
	}
}
