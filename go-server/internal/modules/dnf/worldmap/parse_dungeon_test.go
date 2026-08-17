package worldmap

import (
	"testing"
)

func TestParseDungeonMetadataRepeatedMazesAndExtensions(t *testing.T) {
	doc := parseTestDocument(t, "dungeon/test.dgn", `
[name]
`+"`Test Dungeon`"+`
[explain]
`+"`Structured from local PVF`"+`
[cutscene image]
`+"`Map/Test/loading.img`"+` 3
[minimum required level]
10
[basis level]
20
[experience increasing point]
1.25
[dungeon type]
`+"`[normal]`"+`
[tutorial dungeon]
1
[recommended level]
10 30
[fatigue]
8
[raid dungeon]
1
[maze info]
[size]
3 2
[greed]
`+"`BBFFEE`"+`
[map specification]
`+"`map`"+` 0 0 100 `+"`map`"+` 1 0 101 102 `+"`boss`"+` 2 1 103
[start map]
0 0 7
[boss map]
2 1
[quest connection]
0 900 -1
[boss map specification]
2 1 103
[layered map specification]
1 1 104 105
[maze minimap ani icon]
0 1 0 `+"`ui/maze.ani`"+` 1
[randomized object creation]
[select]
1
[regenerate]
0
[minimap icon]
2
[object]
[map]
1 0
[index]
64024
[team]
[monster]
[pos]
563 197
[/object]
[/randomized object creation]
[clear condition]
[destroy object]
80458 1
[/clear condition]
[maze info]
[size]
1 1
[map specification]
`+"`map`"+` 0 0 200
[start map]
0 0
[boss map]
0 0
[fatigue result]
4
`)

	dungeon := ParseDungeon(77, "dungeon/test.dgn", doc)
	if dungeon.Metadata.Name != "Test Dungeon" || !dungeon.Metadata.MinimumRequiredLevel.Set || dungeon.Metadata.MinimumRequiredLevel.Value != 10 {
		t.Fatalf("unexpected metadata: %+v", dungeon.Metadata)
	}
	if !dungeon.Metadata.ExperienceIncreasingPoint.Set || dungeon.Metadata.ExperienceIncreasingPoint.Value != 1.25 {
		t.Fatalf("unexpected numeric metadata: %+v", dungeon.Metadata.ExperienceIncreasingPoint)
	}
	if !dungeon.Metadata.TutorialDungeon.Set || dungeon.Metadata.TutorialDungeon.Value != 1 {
		t.Fatalf("tutorial dungeon metadata missing: %+v", dungeon.Metadata.TutorialDungeon)
	}
	if dungeon.Metadata.IntegerValues["fatigue"] != 8 || dungeon.Metadata.IntegerValues["fatigue result"] != 4 {
		t.Fatalf("stable integer metadata missing: %+v", dungeon.Metadata.IntegerValues)
	}
	if len(dungeon.Mazes) != 2 || dungeon.Mazes[0].Index != 0 || dungeon.Mazes[1].Index != 1 {
		t.Fatalf("repeated mazes = %+v", dungeon.Mazes)
	}
	first := dungeon.Mazes[0]
	if first.Width.Value != 3 || first.Height.Value != 2 || first.Greed != "BBFFEE" || len(first.MapSpecifications) != 3 {
		t.Fatalf("unexpected first maze: %+v", first)
	}
	if got := first.MapSpecifications[1].MapIDs; len(got) != 2 || got[0] != 101 || got[1] != 102 {
		t.Fatalf("map candidates = %+v", got)
	}
	if first.Start == nil || first.Start.X != 0 || len(first.Start.Params) != 1 || first.Boss == nil || first.Boss.X != 2 {
		t.Fatalf("start/boss points = start=%+v boss=%+v", first.Start, first.Boss)
	}
	if len(dungeon.Extensions) != 1 || dungeon.Extensions[0].Name != "raid dungeon" || dungeon.Extensions[0].Kind != SectionIntegers || dungeon.Extensions[0].Integers[0] != 1 {
		t.Fatalf("local metadata extension = %+v", dungeon.Extensions)
	}
	if len(first.Extensions) != 1 || first.Extensions[0].Name != "maze minimap ani icon" || first.Extensions[0].Kind != SectionMixed || first.Extensions[0].Texts[0] != "ui/maze.ani" {
		t.Fatalf("local maze extension = %+v", first.Extensions)
	}
	if len(first.OpaqueSections) < 2 || len(first.SourceSections) == 0 || len(dungeon.SourceSections) != len(doc.Sections) {
		t.Fatalf("raw ownership missing: maze opaque=%+v source=%d dungeon_source=%d", first.OpaqueSections, len(first.SourceSections), len(dungeon.SourceSections))
	}
	if len(first.BossSpecifications) != 1 || first.BossSpecifications[0].MapIDs[0] != 103 || len(first.LayeredSpecifications) != 1 || len(first.LayeredSpecifications[0].MapIDs) != 2 {
		t.Fatalf("boss/layered specifications = boss=%+v layered=%+v", first.BossSpecifications, first.LayeredSpecifications)
	}
	if len(first.RandomizedObjects) != 1 || first.RandomizedObjects[0].MinimapIcon.Value != 2 || len(first.RandomizedObjects[0].Objects) != 1 {
		t.Fatalf("randomized object script = %+v", first.RandomizedObjects)
	}
	object := first.RandomizedObjects[0].Objects[0]
	if object.ObjectIndex.Value != 64024 || object.Team != "monster" || object.Position.X != 563 || object.MazeCoordinate.X != 1 {
		t.Fatalf("randomized object = %+v", object)
	}
	if len(first.ClearConditions) != 1 || first.ClearConditions[0].Type != "destroy object" || first.ClearConditions[0].TargetID != 80458 {
		t.Fatalf("clear conditions = %+v", first.ClearConditions)
	}
}

func TestParseDungeonMalformedMazeKeepsRawAndDiagnostics(t *testing.T) {
	doc := parseTestDocument(t, "dungeon/bad.dgn", `
[name]
`+"`Bad Dungeon`"+`
[maze info]
[size]
`+"`wide`"+` 2
[map specification]
`+"`map`"+` 0 `+"`bad-y`"+` 100
[start map]
1
[future maze node]
{ `+"`payload`"+` = 4 }
`)
	dungeon := ParseDungeon(78, "dungeon/bad.dgn", doc)
	if len(dungeon.Mazes) != 1 || len(dungeon.Mazes[0].Diagnostics) < 3 {
		t.Fatalf("missing malformed diagnostics: %+v", dungeon.Mazes)
	}
	maze := dungeon.Mazes[0]
	if len(maze.MapSpecifications) != 0 || maze.Start != nil || len(maze.UnknownSections) != 1 || len(maze.Extensions) != 1 {
		t.Fatalf("malformed values crossed boundaries: %+v", maze)
	}
	if maze.Extensions[0].Kind != SectionMixed || len(maze.Extensions[0].Tokens) != 5 {
		t.Fatalf("future extension tokens lost: %+v", maze.Extensions)
	}
}

func TestParseDungeonTutorialMetadataAfterMazeKeepsDungeonOwnership(t *testing.T) {
	doc := parseTestDocument(t, "dungeon/tutorial_after_maze.dgn", `
[name]
`+"`Profession Tutorial`"+`
[maze info]
[size]
1 1
[greed]
`+"`A`"+`
[map specification]
`+"`map`"+` 0 0 100
[start map]
0 0
[boss map]
0 0
[tutorial dungeon]
1
[tutorial dungeon]
1
`)
	dungeon := ParseDungeon(7110, "dungeon/tutorial_after_maze.dgn", doc)
	if !dungeon.Metadata.TutorialDungeon.Set || dungeon.Metadata.TutorialDungeon.Value != 1 {
		t.Fatalf("post-maze tutorial metadata missing: %+v", dungeon.Metadata.TutorialDungeon)
	}
	if len(dungeon.Mazes) != 1 {
		t.Fatalf("tutorial mazes=%+v", dungeon.Mazes)
	}
	for _, section := range dungeon.Mazes[0].UnknownSections {
		if sectionKey(section.Name) == "tutorial dungeon" {
			t.Fatalf("tutorial metadata leaked into maze unknown sections: %+v", dungeon.Mazes[0].UnknownSections)
		}
	}
	for _, extension := range dungeon.Mazes[0].Extensions {
		if sectionKey(extension.Name) == "tutorial dungeon" {
			t.Fatalf("tutorial metadata leaked into maze extensions: %+v", dungeon.Mazes[0].Extensions)
		}
	}
}

func TestDungeonExtensionsClassifyLocalPVFValueShapes(t *testing.T) {
	doc := parseTestDocument(t, "dungeon/extensions.dgn", `
[local flag]
[local integers]
1 -2 3
[local numbers]
1.5 2
[local texts]
`+"`alpha` beta"+`
[local mixed]
1 `+"`alpha`"+` { 2 }
`)
	dungeon := ParseDungeon(79, "dungeon/extensions.dgn", doc)
	if len(dungeon.Extensions) != 5 || len(dungeon.UnknownSections) != 5 {
		t.Fatalf("extensions/raw = %d/%d", len(dungeon.Extensions), len(dungeon.UnknownSections))
	}
	want := []SectionValueKind{SectionFlag, SectionIntegers, SectionNumbers, SectionTexts, SectionMixed}
	for i, kind := range want {
		if dungeon.Extensions[i].Kind != kind {
			t.Errorf("extension %d kind = %s, want %s: %+v", i, dungeon.Extensions[i].Kind, kind, dungeon.Extensions[i])
		}
	}
	if len(dungeon.Extensions[4].Tokens) != 5 || len(dungeon.Extensions[4].Symbols) != 2 {
		t.Fatalf("mixed extension did not retain structure: %+v", dungeon.Extensions[4])
	}
}

func TestDungeonNestedMetadataBlockAfterMazeKeepsDungeonOwnership(t *testing.T) {
	doc := parseTestDocument(t, "dungeon/scoped.dgn", `
[maze info]
[size]
1 1
[greed]
`+"`A`"+`
[start map]
0 0
[boss map]
0 0
[monsterapc diff table]
[normal]
[attack]
1.25
[future balance field]
7
[/normal]
[/monsterapc diff table]
`)
	dungeon := ParseDungeon(80, "dungeon/scoped.dgn", doc)
	if len(dungeon.Mazes) != 1 {
		t.Fatalf("mazes = %+v", dungeon.Mazes)
	}
	if len(dungeon.OpaqueSections) != 1 || sectionKey(dungeon.OpaqueSections[0].Name) != "monsterapc diff table" {
		t.Fatalf("dungeon metadata block = %+v", dungeon.OpaqueSections)
	}
	if len(dungeon.UnknownSections) != 3 || len(dungeon.Extensions) != 4 {
		t.Fatalf("dungeon scoped extensions = unknown=%+v extensions=%+v", dungeon.UnknownSections, dungeon.Extensions)
	}
	for _, section := range dungeon.UnknownSections {
		if len(section.Scope) == 0 || section.Scope[0] != "monsterapc diff table" {
			t.Fatalf("dungeon extension scope = %+v", section)
		}
	}
	if len(dungeon.Mazes[0].UnknownSections) != 0 {
		t.Fatalf("metadata children leaked into maze = %+v", dungeon.Mazes[0].UnknownSections)
	}
}
