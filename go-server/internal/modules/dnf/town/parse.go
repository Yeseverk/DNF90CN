package town

import (
	"fmt"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func parseTown(townID int64, documentPath string, doc *dnfpvf.Document) (Town, error) {
	if doc == nil || len(doc.Tokens) == 0 {
		return Town{}, fmt.Errorf("%w: %s", ErrDocumentEmpty, documentPath)
	}
	value := Town{ID: townID, Path: documentPath}
	if name, ok := doc.Text("name"); ok {
		value.Name = strings.TrimSpace(name)
	}
	for idx := 0; idx < len(doc.Tokens); idx++ {
		token := doc.Tokens[idx]
		if token.Kind != dnfpvf.TokenSection || sectionName(token.Value) != "area" {
			continue
		}
		end := areaEnd(doc.Tokens, idx+1)
		area, ok := parseArea(doc.Tokens[idx+1 : end])
		if ok {
			value.Areas = append(value.Areas, area)
		}
		idx = end
	}
	return value, nil
}

func parseArea(tokens []dnfpvf.Token) (Area, bool) {
	if len(tokens) < 2 || tokens[0].Kind != dnfpvf.TokenInt ||
		(tokens[1].Kind != dnfpvf.TokenString && tokens[1].Kind != dnfpvf.TokenIdent) {
		return Area{}, false
	}
	area := Area{ID: tokens[0].Int, MapPath: strings.TrimSpace(strings.ReplaceAll(tokens[1].Value, "\\", "/"))}
	for idx := 2; idx < len(tokens); idx++ {
		token := tokens[idx]
		if token.Kind == dnfpvf.TokenSection {
			switch sectionName(token.Value) {
			case "need level":
				if value, ok := nextInt(tokens, idx+1); ok {
					area.MinLevel = value
				}
			case "need quest":
				area.NeedQuests = append(area.NeedQuests, intsUntilSection(tokens, idx+1)...)
			}
			continue
		}
		if token.Kind != dnfpvf.TokenString && token.Kind != dnfpvf.TokenIdent {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(token.Value)) {
		case "[normal]":
			area.Kind = "normal"
		case "[gate]":
			values := followingInts(tokens, idx+1, 2)
			if len(values) == 2 {
				area.Kind = "gate"
				area.Gate = &Gate{X: values[0], Y: values[1]}
			}
		case "[dungeon gate]":
			values := followingInts(tokens, idx+1, 1)
			if len(values) == 1 {
				area.Kind = "dungeon_gate"
				area.DungeonGate = &values[0]
			}
		}
	}
	return area, area.MapPath != ""
}

func areaEnd(tokens []dnfpvf.Token, start int) int {
	for idx := start; idx < len(tokens); idx++ {
		if tokens[idx].Kind == dnfpvf.TokenSection && sectionName(tokens[idx].Value) == "/area" {
			return idx
		}
	}
	return len(tokens)
}

func sectionName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func nextInt(tokens []dnfpvf.Token, start int) (int64, bool) {
	for idx := start; idx < len(tokens); idx++ {
		if tokens[idx].Kind == dnfpvf.TokenSection {
			return 0, false
		}
		if tokens[idx].Kind == dnfpvf.TokenInt {
			return tokens[idx].Int, true
		}
	}
	return 0, false
}

func intsUntilSection(tokens []dnfpvf.Token, start int) []int64 {
	values := make([]int64, 0, 1)
	for idx := start; idx < len(tokens); idx++ {
		if tokens[idx].Kind == dnfpvf.TokenSection {
			break
		}
		if tokens[idx].Kind == dnfpvf.TokenInt {
			values = append(values, tokens[idx].Int)
		}
	}
	return values
}

func followingInts(tokens []dnfpvf.Token, start, count int) []int64 {
	values := make([]int64, 0, count)
	for idx := start; idx < len(tokens) && len(values) < count; idx++ {
		if tokens[idx].Kind == dnfpvf.TokenSection {
			break
		}
		if tokens[idx].Kind == dnfpvf.TokenInt {
			values = append(values, tokens[idx].Int)
		}
	}
	return values
}
