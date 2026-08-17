package worldmap

import (
	"strings"
	"testing"
)

func TestParseMapMonsterBossSuffixDoesNotHideFollowingRecord(t *testing.T) {
	doc := parseTestDocument(t, "map/monster-suffix.map", `
[monster]
1001 1 0 38 273 0 1 1 `+"`[fixed]` `[normal]`"+` 1002 1 0 1224 -263 0 1 1 `+"`[fixed]` `[dummy]` `[boss]`"+` 1003 1 0 1299 -314 0 1 1 `+"`[fixed]` `[normal]`"+`
[/monster]
`)
	parsed := ParseMap(310004, "map/monster-suffix.map", doc)
	if len(parsed.Monsters) != 3 || parsed.Monsters[0].MonsterID != 1001 ||
		parsed.Monsters[1].MonsterID != 1002 || parsed.Monsters[2].MonsterID != 1003 {
		t.Fatalf("monster suffix crossed record boundary: monsters=%+v diagnostics=%+v", parsed.Monsters, parsed.Diagnostics)
	}
	if parsed.Monsters[1].SuffixMarker != "[boss]" {
		t.Fatalf("boss suffix was not preserved as syntax: %+v", parsed.Monsters[1])
	}
	for _, entry := range parsed.Diagnostics {
		if sectionKey(entry.Section) == "monster" &&
			(strings.Contains(entry.Message, "malformed monster") || strings.Contains(entry.Message, "trailing tokens")) {
			t.Fatalf("proved boss suffix was rejected: %+v", parsed.Diagnostics)
		}
	}
}

func TestParseMapMonsterBossSuffixAtSectionEndIsNotReportedAsTrailing(t *testing.T) {
	doc := parseTestDocument(t, "map/monster-suffix-end.map", `
[monster]
1001 1 0 38 273 0 1 1 `+"`[fixed]` `[dummy]` `[boss]`"+`
[/monster]
`)
	parsed := ParseMap(76031, "map/monster-suffix-end.map", doc)
	if len(parsed.Monsters) != 1 || parsed.Monsters[0].MonsterID != 1001 {
		t.Fatalf("monster suffix changed core record: monsters=%+v diagnostics=%+v", parsed.Monsters, parsed.Diagnostics)
	}
	if parsed.Monsters[0].SuffixMarker != "[boss]" {
		t.Fatalf("terminal boss suffix was not preserved as syntax: %+v", parsed.Monsters[0])
	}
	for _, entry := range parsed.Diagnostics {
		if sectionKey(entry.Section) == "monster" {
			t.Fatalf("proved terminal boss suffix was rejected: %+v", parsed.Diagnostics)
		}
	}
}

func TestParseMapUnknownMonsterSuffixRemainsFailClosed(t *testing.T) {
	doc := parseTestDocument(t, "map/monster-unknown-suffix.map", `
[monster]
1001 1 0 38 273 0 1 1 `+"`[fixed]` `[dummy]` `[unknown suffix]`"+` 1002 1 0 1299 -314 0 1 1 `+"`[fixed]` `[normal]`"+`
[/monster]
`)
	parsed := ParseMap(1, "map/monster-unknown-suffix.map", doc)
	if len(parsed.Monsters) != 1 || parsed.Monsters[0].MonsterID != 1001 {
		t.Fatalf("unknown suffix was guessed or corrupted core record: %+v", parsed.Monsters)
	}
	if parsed.Monsters[0].SuffixMarker != "" {
		t.Fatalf("unknown suffix was promoted to a recognized marker: %+v", parsed.Monsters[0])
	}
	foundMalformed := false
	for _, entry := range parsed.Diagnostics {
		if sectionKey(entry.Section) == "monster" && strings.Contains(entry.Message, "malformed monster") {
			foundMalformed = true
		}
	}
	if !foundMalformed {
		t.Fatalf("unknown suffix did not remain fail-closed: %+v", parsed.Diagnostics)
	}
}
