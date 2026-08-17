package worldmap

import (
	"fmt"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

var opaqueMapSections = stringSet(
	"monster condition", "monster spawn pos", "blood monster", "blood phase time",
	"ultimate monster", "ultimate phase time", "darkness", "static player start pos",
	"belt scroll map", "move layered map", "customized screen edge", "extended tile",
	"scroll animation", "conditional summon monster", "map over move ani", "camera force move",
	"camera edge exception", "revive with dlg", "zone defence", "tournament enemies",
	"tournament start area", "before rendering info", "time line", "summon start area",
	"map frame", "tile option", "background effect", "block effect", "item", "quest",
	"apc create condition", "map animation", "revival map", "block path",
	"monster specific ai", "buff", "map dialog", "dust",
)

var knownMapSections = stringSet(
	"map name", "player number", "pvp start area", "dungeon", "type", "greed", "tile", "import script",
	"tile files", "tile map", "summon", "summon key", "position", "itemcount", "index",
	"life time", "info", "distance", "dungeon movable area", "random start map",
	"pathgate recognize range",
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
	"is moive end", "is movie end", "quest start map", "hide monster", "show dust",
	"dungeon start area", "screen pos", "monster team", "pvp practice start area",
	"virtual movable area", "town movable area", "pathgate object", "opening bgm",
	"map loading image path", "basic action", "map dialog", "dust", "absolute start path",
	"monster condition", "monster spawn pos", "blood monster", "blood phase time",
	"ultimate monster", "ultimate phase time", "darkness", "static player start pos",
	"belt scroll map", "move layered map", "customized screen edge", "extended tile",
	"scroll animation", "conditional summon monster", "map over move ani", "camera force move",
	"camera edge exception", "revive with dlg", "zone defence", "tournament enemies",
	"tournament start area", "before rendering info", "time line", "summon start area",
	"map frame", "tile option", "background effect", "block effect", "item", "quest",
	"apc create condition", "map animation", "revival map", "block path",
	"ani info", "filename", "layer", "order", "hellparty",
)

var knownAreaSections = stringSet(
	"map image", "ui path", "dungeon", "name", "hell dungeon", "hell quest",
	"hell freepass item", "item condition", "in progress",
)

func stringSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[sectionKey(value)] = struct{}{}
	}
	return out
}

func sectionKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func rawSections(doc *dnfpvf.Document) ([]RawSection, []Diagnostic) {
	if doc == nil {
		return nil, []Diagnostic{{Message: "document is nil"}}
	}
	occurrences := make(map[string]int)
	blocks := make(map[string]struct{})
	for _, section := range doc.Sections {
		key := sectionKey(section.Name)
		if isClosingSection(key) {
			blocks[strings.TrimPrefix(key, "/")] = struct{}{}
		}
	}
	var scope []string
	out := make([]RawSection, 0, len(doc.Sections))
	var diagnostics []Diagnostic
	for _, section := range doc.Sections {
		key := sectionKey(section.Name)
		if isClosingSection(key) {
			closing := strings.TrimPrefix(key, "/")
			match := -1
			for index := len(scope) - 1; index >= 0; index-- {
				if scope[index] == closing {
					match = index
					break
				}
			}
			if match >= 0 {
				scope = scope[:match]
			} else {
				diagnostics = append(diagnostics, Diagnostic{Section: section.Name, Message: "unmatched closing section"})
			}
		}
		occurrences[key]++
		_, block := blocks[key]
		raw := RawSection{
			Name: section.Name, Occurrence: occurrences[key], Scope: append([]string(nil), scope...), Block: block,
		}
		if section.Start < 0 || section.End < section.Start || section.End > len(doc.Tokens) {
			diagnostics = append(diagnostics, Diagnostic{
				Section: section.Name, Occurrence: raw.Occurrence,
				Message: fmt.Sprintf("invalid token range [%d,%d) for %d tokens", section.Start, section.End, len(doc.Tokens)),
			})
			out = append(out, raw)
			continue
		}
		raw.Tokens = append([]dnfpvf.Token(nil), doc.Tokens[section.Start:section.End]...)
		out = append(out, raw)
		if block && !isClosingSection(key) {
			scope = append(scope, key)
		}
	}
	if len(scope) > 0 {
		diagnostics = append(diagnostics, Diagnostic{Section: scope[len(scope)-1], Message: "section block is not closed"})
	}
	return out, diagnostics
}

func isClosingSection(name string) bool {
	return strings.HasPrefix(sectionKey(name), "/")
}

func ints(section RawSection) ([]int64, bool) {
	out := make([]int64, 0, len(section.Tokens))
	valid := true
	for _, token := range section.Tokens {
		switch token.Kind {
		case dnfpvf.TokenInt:
			out = append(out, token.Int)
		case dnfpvf.TokenSymbol:
		default:
			valid = false
		}
	}
	return out, valid
}

func values(section RawSection) []dnfpvf.Token {
	out := make([]dnfpvf.Token, 0, len(section.Tokens))
	for _, token := range section.Tokens {
		if token.Kind != dnfpvf.TokenSymbol {
			out = append(out, token)
		}
	}
	return out
}

func texts(section RawSection) []string {
	var out []string
	for _, token := range section.Tokens {
		if token.Kind == dnfpvf.TokenString || token.Kind == dnfpvf.TokenIdent {
			out = append(out, token.Value)
		}
	}
	return out
}

func firstText(section RawSection) (string, bool) {
	all := texts(section)
	if len(all) == 0 {
		return "", false
	}
	return all[0], true
}

func firstInt(section RawSection) (int64, bool) {
	for _, token := range section.Tokens {
		if token.Kind == dnfpvf.TokenInt {
			return token.Int, true
		}
	}
	return 0, false
}

func diagnostic(section RawSection, message string) Diagnostic {
	return Diagnostic{Section: section.Name, Occurrence: section.Occurrence, Message: message}
}
