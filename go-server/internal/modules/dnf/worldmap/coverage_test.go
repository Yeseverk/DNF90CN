package worldmap

import "testing"

func TestMapFileCompatibilitySectionCoverage(t *testing.T) {
	sections := []string{
		"map name", "player number", "pvp start area", "dungeon", "type", "greed", "tile",
		"far sight scroll", "middle sight scroll", "near sight scroll", "background animation",
		"pathgate pos", "sound", "animation", "passive object", "special passive object",
		"monster", "event monster position", "npc", "monster specific ai", "buff", "ai character",
		"fix champion", "heroes mode map index", "background correction", "background pos",
		"foreground pattern alpha", "apc random point", "monster lock", "draw monster count",
		"sort bottom", "add gravity", "jump power rate", "block use stackable item",
		"block use active skill", "visible on dungeon clear", "loop y axis", "all dead case passable",
		"disable item escape stuck", "disable character escape stuck", "cannot use coin map",
		"no revival timer limit", "ignore diehard", "disable rebirth", "preserve player corpse",
		"cannot use resolution change zoom", "center fixed camera", "force draw pattern", "is revival",
		"is moive end", "quest start map", "hide monster", "show dust", "dungeon start area",
		"screen pos", "monster team", "pvp practice start area", "virtual movable area",
		"town movable area", "pathgate object", "opening bgm", "map loading image path",
		"basic action", "map dialog", "dust", "absolute start path", "monster condition",
		"monster spawn pos", "blood monster", "blood phase time", "ultimate monster",
		"ultimate phase time", "darkness", "static player start pos", "belt scroll map",
		"move layered map", "customized screen edge", "extended tile", "scroll animation",
		"conditional summon monster", "map over move ani", "camera force move",
		"camera edge exception", "revive with dlg", "zone defence", "tournament enemies",
		"tournament start area", "before rendering info", "time line", "summon start area",
		"map frame", "tile option", "background effect", "block effect", "item", "quest",
		"apc create condition", "map animation", "revival map", "block path",
		// The C# source spells this tag "is moive end". Accept the corrected PVF spelling too.
		"is movie end",
	}
	if len(sections) != 101 {
		t.Fatalf("compatibility section count = %d, want 101", len(sections))
	}
	for _, section := range sections {
		if _, ok := knownMapSections[sectionKey(section)]; !ok {
			t.Errorf("map section %q is not classified", section)
		}
	}
	for _, nested := range []string{"ani info", "filename", "layer", "order", "hellparty"} {
		if _, ok := knownMapSections[nested]; !ok {
			t.Errorf("nested map section %q is not classified", nested)
		}
	}
}
