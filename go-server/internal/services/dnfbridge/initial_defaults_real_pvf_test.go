package dnfbridge

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFMatchesInitialTownDefaults(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the current initial town defaults")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}

	townList, err := archive.ReadText("town/town.lst")
	if err != nil {
		t.Fatalf("read town/town.lst: %v", err)
	}
	townEntry := regexp.MustCompile(fmt.Sprintf(`(?:^|\s)%d\s+`+"`"+`new_Elvengard\.twn`+"`"+`(?:\s|$)`, newCharacterInitialTownID))
	if !townEntry.MatchString(townList) {
		t.Fatalf("town/town.lst does not map initial town %d to new_Elvengard.twn", newCharacterInitialTownID)
	}

	town, err := archive.ReadText("town/new_Elvengard.twn")
	if err != nil {
		t.Fatalf("read town/new_Elvengard.twn: %v", err)
	}
	areaEntry := regexp.MustCompile(fmt.Sprintf(`(?s)\[area\]\s*%d\s+`+"`"+`Cataclysm/Town/Elvengard/new_seria_room\.map`+"`"+`.*?`+"`"+`\[gate\]`+"`"+`\s+%d\s+%d\s+\[/area\]`,
		newCharacterInitialAreaID, newCharacterInitialPosX, newCharacterInitialPosY))
	if !areaEntry.MatchString(town) {
		t.Fatalf("new_Elvengard.twn does not define initial area=%d as new_seria_room.map with gate=(%d,%d)",
			newCharacterInitialAreaID, newCharacterInitialPosX, newCharacterInitialPosY)
	}
	if !strings.Contains(town, "[name]") || !strings.Contains(town, "`艾尔文防线`") {
		t.Fatal("new_Elvengard.twn does not identify the town as 艾尔文防线")
	}
}
