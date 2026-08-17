package dnfbridge

import (
	"os"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealPVFUpgradeTableCarriesNPCReinforceMaterial(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to inspect runtime upgrade.etc")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open PVF: %v", err)
	}
	text, err := archive.ReadText(currentUpgradePVFPath)
	if err != nil {
		t.Fatalf("read %s: %v", currentUpgradePVFPath, err)
	}
	doc, err := dnfpvf.Parse(currentUpgradePVFPath, text)
	if err != nil {
		t.Fatalf("parse %s: %v", currentUpgradePVFPath, err)
	}
	table := parseCurrentUpgradeTable(doc, text)
	if len(table.Rows) < 13 {
		t.Fatalf("upgrade table rows = %d, want at least 13", len(table.Rows))
	}
	for _, level := range []int{1, 10, 13} {
		row, ok := table.RowForLevel(level)
		if !ok {
			t.Fatalf("missing row for target level %d", level)
		}
		t.Logf("target_level=%d failure=%d penalty=(%d,%d) material=%d x%d", level, row.FailureWeight, row.PenaltyType, row.PenaltyValue, row.MaterialItemID, row.MaterialCount)
		if row.MaterialItemID != 3037 || row.MaterialCount <= 0 {
			t.Fatalf("target level %d material = %d x%d, want clear cube material", level, row.MaterialItemID, row.MaterialCount)
		}
	}
}
