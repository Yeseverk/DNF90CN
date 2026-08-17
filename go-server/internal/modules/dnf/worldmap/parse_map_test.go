package worldmap

import (
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func TestParseMapKnownFieldsRepeatedSectionsAndRawPreservation(t *testing.T) {
	doc := parseTestDocument(t, "map/test.map", `
[map name]
`+"`training town`"+`
[dungeon]
7001 7002
[tile]
`+"`tile/a.til`"+`
[/tile]
[tile]
`+"`tile/b.til`"+`
[/tile]
[background animation]
[ani info]
[filename]
`+"`animation/far.ani`"+`
[layer]
`+"`[distantback]`"+`
[order]
`+"`[below]`"+`
[/ani info]
[/background animation]
[pathgate pos]
10 20 30 40
[pathgate object]
5001
[npc]
1000 `+"`[left]`"+` -62 -125 0
[npc]
1001 `+"`[right]`"+` 50 60 1
[animation]
`+"`animation/tree.ani` `[normal]`"+` 1 2 3
[passive object]
5 100 200 7
[monster]
3001 20 0 100 200 0 1 2 `+"`[fixed]` `[boss]`"+`
[monster team]
100
[event monster position]
7 8 9
[ai character]
4001 11 12 1 `+"`[neutral]` `[boss]`"+` 9 10
[town movable area]
1 2 300 400 8 1
[block use active skill]
[is movie end]
[map animation]
`+"`future nested grammar`"+` 3
[future map feature]
`+"`kept verbatim`"+` 77
`)

	parsed := ParseMap(91, "map/test.map", doc)
	if parsed.ID != 91 || parsed.Name != "training town" || !parsed.DungeonID.Set || parsed.DungeonID.Value != 7001 {
		t.Fatalf("unexpected map identity: %+v", parsed)
	}
	if len(parsed.DungeonIDs) != 2 || parsed.DungeonIDs[0] != 7001 || parsed.DungeonIDs[1] != 7002 {
		t.Fatalf("map dungeon ownership = %v", parsed.DungeonIDs)
	}
	if len(parsed.Tiles) != 2 || parsed.Tiles[0] != "tile/a.til" || parsed.Tiles[1] != "tile/b.til" {
		t.Fatalf("repeated tiles were not accumulated: %+v", parsed.Tiles)
	}
	if len(parsed.BackgroundAnimations) != 1 || parsed.BackgroundAnimations[0].Filename != "animation/far.ani" {
		t.Fatalf("unexpected background animation: %+v", parsed.BackgroundAnimations)
	}
	if len(parsed.Portals) != 2 || parsed.Portals[0].Position.X != 10 || !parsed.Portals[0].ObjectID.Set || parsed.Portals[1].ObjectID.Set {
		t.Fatalf("unexpected portal graph: %+v", parsed.Portals)
	}
	if len(parsed.NPCs) != 2 || parsed.NPCs[0].Direction != "[left]" || parsed.NPCs[1].Position.Z != 1 {
		t.Fatalf("unexpected npc nodes: %+v", parsed.NPCs)
	}
	if len(parsed.AnimationObjects) != 1 || len(parsed.PassiveObjects) != 1 || len(parsed.Monsters) != 1 || parsed.Monsters[0].Rank != "[boss]" {
		t.Fatalf("unexpected object graph: animation=%+v passive=%+v monsters=%+v", parsed.AnimationObjects, parsed.PassiveObjects, parsed.Monsters)
	}
	if len(parsed.AICharacters) != 1 || len(parsed.AICharacters[0].Params) != 2 || len(parsed.TownMovableAreas) != 1 {
		t.Fatalf("unexpected apc/town data: apc=%+v areas=%+v", parsed.AICharacters, parsed.TownMovableAreas)
	}
	if len(parsed.MonsterTeam) != 1 || parsed.MonsterTeam[0] != 100 {
		t.Fatalf("unexpected monster teams: %v", parsed.MonsterTeam)
	}
	if !parsed.Flags.BlockUseActiveSkill || !parsed.Flags.IsMovieEnd {
		t.Fatalf("expected map flags: %+v", parsed.Flags)
	}
	if len(parsed.OpaqueSections) != 1 || sectionKey(parsed.OpaqueSections[0].Name) != "map animation" {
		t.Fatalf("opaque section not retained: %+v", parsed.OpaqueSections)
	}
	if len(parsed.UnknownSections) != 1 || parsed.UnknownSections[0].Tokens[0].Raw != "`kept verbatim`" || parsed.UnknownSections[0].Tokens[1].Int != 77 {
		t.Fatalf("unknown section not retained: %+v", parsed.UnknownSections)
	}
	var tileOccurrences []int
	for _, section := range parsed.SourceSections {
		if sectionKey(section.Name) == "tile" {
			tileOccurrences = append(tileOccurrences, section.Occurrence)
		}
	}
	if len(tileOccurrences) != 2 || tileOccurrences[0] != 1 || tileOccurrences[1] != 2 {
		t.Fatalf("section occurrences = %+v", tileOccurrences)
	}
}

func TestParseMapMalformedValuesKeepSourceAndReportDiagnostics(t *testing.T) {
	doc := parseTestDocument(t, "map/bad.map", `
[dungeon]
`+"`not-an-id`"+`
[pathgate pos]
1 2 3
[npc]
1000 `+"`[left]`"+` 10
[monster]
1 2 3 4 5 6 7 8 `+"`[fixed]`"+`
[unknown malformed]
{ `+"`payload`"+` = 4 }
`)

	parsed := ParseMap(1, "map/bad.map", doc)
	if parsed.DungeonID.Set {
		t.Fatalf("malformed dungeon id became valid: %+v", parsed.DungeonID)
	}
	if len(parsed.PathgatePositions) != 1 || len(parsed.NPCs) != 0 || len(parsed.Monsters) != 0 {
		t.Fatalf("malformed records crossed boundaries: portals=%+v npcs=%+v monsters=%+v", parsed.PathgatePositions, parsed.NPCs, parsed.Monsters)
	}
	if len(parsed.Diagnostics) < 4 {
		t.Fatalf("missing diagnostics: %+v", parsed.Diagnostics)
	}
	if len(parsed.SourceSections) != 5 || len(parsed.UnknownSections) != 1 || len(parsed.UnknownSections[0].Tokens) != 5 {
		t.Fatalf("raw malformed source was lost: source=%+v unknown=%+v", parsed.SourceSections, parsed.UnknownSections)
	}
}

func TestParseMapLocalPVFExtensionsAndNestedSummons(t *testing.T) {
	doc := parseTestDocument(t, "map/local.map", `
[tile files]
0 `+"`Tile/Act1.til`"+` 1 `+"`Tile/Common.til`"+`
[tile map]
0 1 1 0
[dungeon movable area]
13 122 1110 360
[random start map]
1
[pathgate recognize range]
240
[summon]
[summon key]
7
[position]
1500 160 0 0
[type]
`+"`gold`"+`
[itemcount]
2
[index]
19000
[life time]
-1
[info]
[distance]
10000000 1500 0
[future summon field]
`+"`retained`"+`
[/info]
[/summon]
[future top level]
9 `+"`typed`"+`
`)

	parsed := ParseMap(99, "map/local.map", doc)
	if len(parsed.TileFiles) != 2 || parsed.TileFiles[1].Index != 1 || parsed.TileFiles[1].Path != "Tile/Common.til" {
		t.Fatalf("tile files = %+v", parsed.TileFiles)
	}
	if len(parsed.TileMapRows) != 1 || len(parsed.TileMapRows[0]) != 4 || parsed.TileMapRows[0][2] != 1 {
		t.Fatalf("tile map rows = %+v", parsed.TileMapRows)
	}
	if len(parsed.DungeonMovableAreas) != 1 || parsed.DungeonMovableAreas[0].Width != 1110 {
		t.Fatalf("dungeon movable areas = %+v", parsed.DungeonMovableAreas)
	}
	if !parsed.RandomStartMap.Set || parsed.RandomStartMap.Value != 1 || !parsed.PathgateRecognizeRange.Set || parsed.PathgateRecognizeRange.Value != 240 {
		t.Fatalf("local scalar extensions = random=%+v range=%+v", parsed.RandomStartMap, parsed.PathgateRecognizeRange)
	}
	if len(parsed.Summons) != 1 {
		t.Fatalf("summons = %+v", parsed.Summons)
	}
	summon := parsed.Summons[0]
	if !summon.Key.Set || summon.Key.Value != 7 || summon.Type != "gold" || !summon.Index.Set || summon.Index.Value != 19000 || len(summon.Info) < 2 {
		t.Fatalf("summon = %+v", summon)
	}
	if len(summon.UnknownSections) != 1 || len(summon.UnknownSections[0].Scope) != 2 || summon.UnknownSections[0].Scope[0] != "summon" || summon.UnknownSections[0].Scope[1] != "info" {
		t.Fatalf("summon unknown scope = %+v", summon.UnknownSections)
	}
	if len(parsed.UnknownSections) != 1 || len(parsed.Extensions) != 1 || parsed.Extensions[0].Kind != SectionMixed {
		t.Fatalf("top-level extension = unknown=%+v typed=%+v", parsed.UnknownSections, parsed.Extensions)
	}
}

func TestParseSpecialPassiveObjectFlatAndExtendedRecords(t *testing.T) {
	doc := parseTestDocument(t, "map/special.map", `
[special passive object]
10 1 2 3 11 4 5 6
[special passive object]
20 7 8 9 1 `+"`[monster]`"+` 3001 50 1 2 3
[hellparty]
4 100 0
[/hellparty]
`)
	parsed := ParseMap(2, "map/special.map", doc)
	if len(parsed.SpecialPassiveObjects) != 3 {
		t.Fatalf("special objects = %+v", parsed.SpecialPassiveObjects)
	}
	if parsed.SpecialPassiveObjects[0].ObjectID != 10 || parsed.SpecialPassiveObjects[1].ObjectID != 11 {
		t.Fatalf("flat objects = %+v", parsed.SpecialPassiveObjects)
	}
	last := parsed.SpecialPassiveObjects[2]
	if len(last.Spawns) != 1 || last.Spawns[0].Code != 3001 || len(last.HellParty) != 1 {
		t.Fatalf("extended object = %+v", last)
	}
}

func TestParseSpecialPassiveObjectPrefersFiveFieldZeroSpawnRows(t *testing.T) {
	doc := parseTestDocument(t, "map/special-zero-spawn.map", `
[special passive object]
1 10 20 30 0
2 11 21 31 0
3 12 22 32 0
4 13 23 33 0
`)
	parsed := ParseMap(3, "map/special-zero-spawn.map", doc)
	if len(parsed.SpecialPassiveObjects) != 4 {
		t.Fatalf("special objects=%+v want four five-field headers", parsed.SpecialPassiveObjects)
	}
	for index, object := range parsed.SpecialPassiveObjects {
		if object.ObjectID != int64(index+1) || object.Flags != int64(30+index) || len(object.Spawns) != 0 {
			t.Fatalf("special object[%d]=%+v", index, object)
		}
	}
}

func parseTestDocument(t *testing.T, docPath, text string) *dnfpvf.Document {
	t.Helper()
	doc, err := dnfpvf.Parse(docPath, text)
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	return doc
}
