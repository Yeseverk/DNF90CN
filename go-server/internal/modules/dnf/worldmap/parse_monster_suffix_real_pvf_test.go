package worldmap

import (
	"os"
	"strings"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFThiefTutorialMonsterBossSuffixPreservesThirdRecord(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run the real Script.pvf monster-suffix regression")
	}
	const mapPath = "map/Cataclysm/NewTutorial/Thief_F/310004(3,0).map"
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open real Script.pvf: %v", err)
	}
	text, err := archive.ReadText(mapPath)
	if err != nil {
		t.Fatalf("read %s: %v", mapPath, err)
	}
	document, err := ParseDocument(mapPath, text)
	if err != nil {
		t.Fatalf("parse %s: %v", mapPath, err)
	}
	parsed := ParseMap(310004, mapPath, document)
	want := []int64{107004985, 107004981, 107004986}
	if len(parsed.Monsters) != len(want) {
		t.Fatalf("real tutorial monsters=%+v want_ids=%v diagnostics=%+v", parsed.Monsters, want, parsed.Diagnostics)
	}
	for index, monster := range parsed.Monsters {
		if monster.MonsterID != want[index] {
			t.Fatalf("real tutorial monster[%d]=%+v want_id=%d", index, monster, want[index])
		}
	}
	if parsed.Monsters[1].SuffixMarker != "[boss]" {
		t.Fatalf("real tutorial boss suffix was not preserved as syntax: %+v", parsed.Monsters[1])
	}
	for _, entry := range parsed.Diagnostics {
		if sectionKey(entry.Section) == "monster" &&
			(strings.Contains(entry.Message, "malformed monster") || strings.Contains(entry.Message, "trailing tokens")) {
			t.Fatalf("real tutorial boss suffix rejected: %+v", parsed.Diagnostics)
		}
	}
}
