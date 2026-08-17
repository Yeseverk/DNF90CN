package dnfbridge

import (
	"os"
	"strings"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealPVFDamageFontDefinitionsAreAuditable(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("DNFBRIDGE_REAL_PVF_SMOKE not set")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open PVF: %v", err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatalf("build item catalog: %v", err)
	}
	listText, err := archive.ReadText(dungeonDropStackableList)
	if err != nil {
		t.Fatalf("read stackable list: %v", err)
	}
	listDocument, err := dnfpvf.Parse(dungeonDropStackableList, listText)
	if err != nil {
		t.Fatalf("parse stackable list: %v", err)
	}

	count := 0
	foundProbe := false
	for _, entry := range dnfpvf.ParseList(listDocument) {
		text, err := archive.ReadText("stackable/" + strings.TrimPrefix(entry.Path, "stackable/"))
		if err != nil {
			continue
		}
		document, err := dnfpvf.Parse(entry.Path, text)
		if err != nil {
			continue
		}
		action, _ := document.Text("action type")
		if !strings.EqualFold(strings.TrimSpace(action), "[add damage font skin]") {
			continue
		}
		count++
		definition, resolveErr := catalog.ResolveItem(uint32(entry.ID))
		if resolveErr != nil {
			t.Fatalf("resolve damage font item=%d path=%q: %v", entry.ID, entry.Path, resolveErr)
		}
		if definition.DamageFontIndex == 0 || definition.DamageFontExpirationMode == 0 {
			t.Fatalf("incomplete damage font definition item=%d: %+v", entry.ID, definition)
		}
		if entry.ID == 10160911 {
			foundProbe = true
			if definition.DamageFontIndex != 2 || definition.DamageFontPeriodDays != 90 {
				t.Fatalf("probe item 10160911 = %+v, want font=2 period=90", definition)
			}
		}
	}
	if count == 0 {
		t.Fatal("runtime PVF has no [add damage font skin] item")
	}
	if !foundProbe {
		t.Fatal("runtime PVF is missing probe damage-font item 10160911")
	}
}
