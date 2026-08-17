package dnfbridge

import (
	"context"
	"os"
	"strings"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

// TestRealScriptPVFJob11SelectedSkillRules records the runtime PVF rules used
// by the skill owner for the live female-slayer character. It is opt-in so the
// ordinary test suite does not depend on a 100 MB local archive.
func TestRealScriptPVFJob11SelectedSkillRules(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to run real Script.pvf skill smoke")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := buildSkillCatalogFromSource(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	selected := map[uint16]struct{}{2: {}, 3: {}, 31: {}, 161: {}, 186: {}}
	for _, definition := range catalog.SkillsForJob(11) {
		_, byID := selected[definition.ID]
		byName := strings.Contains(definition.Name, "蛇腹剑")
		if !byID && !byName {
			continue
		}
		t.Logf("id=%d name=%q path=%q kind=%q active=%t required=%d max=%d class=%d grow=%v feature=%d prerequisites=%+v purchase=%v special=%v",
			definition.ID, definition.Name, definition.Path, definition.Kind, definition.Active,
			definition.RequiredLevel, definition.MaximumLevel, definition.SkillClass,
			definition.GrowTypes, definition.FeatureSkillType, definition.Prerequisites,
			definition.PurchaseCost, definition.SpecialPurchaseCost)
	}
}
