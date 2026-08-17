package combatpower

import (
	"math"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func parseDocumentAffixes(doc *dnfpvf.Document) Affixes {
	var out Affixes
	if doc == nil {
		return out
	}
	for _, section := range doc.Sections {
		applyAffixSection(&out, section.Name, sectionTokens(doc, section))
	}
	return out
}

func parseSetAbilities(doc *dnfpvf.Document) []SetAbility {
	if doc == nil {
		return nil
	}
	var abilities []SetAbility
	var current *SetAbility
	for _, section := range doc.Sections {
		name := normalizeName(section.Name)
		switch name {
		case "piece set ability":
			if current != nil && current.RequiredPieces > 0 {
				abilities = append(abilities, *current)
			}
			current = &SetAbility{RequiredPieces: firstInteger(sectionTokens(doc, section))}
		case "/piece set ability":
			if current != nil && current.RequiredPieces > 0 {
				abilities = append(abilities, *current)
			}
			current = nil
		default:
			if current != nil {
				applyAffixSection(&current.Affixes, section.Name, sectionTokens(doc, section))
			}
		}
	}
	if current != nil && current.RequiredPieces > 0 {
		abilities = append(abilities, *current)
	}
	return abilities
}

func applyAffixSection(out *Affixes, sectionName string, tokens []dnfpvf.Token) {
	if out == nil || !hasPercent(tokens) {
		return
	}
	value, ok := lastNumber(tokens)
	if !ok || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	name := normalizeName(sectionName)
	if name == "stat by condition" || name == "stat" {
		if statName := firstText(tokens); statName != "" {
			name = normalizeName(statName)
		}
	}
	switch name {
	case "add absolute damage", "additional damage":
		// One item or one set threshold may contain mutually-exclusive fire,
		// water, light, and dark branches. They describe one white-damage
		// option, so keep the greatest branch here; independent documents are
		// combined later by Affixes.Add.
		if value > out.WhiteDamage {
			out.WhiteDamage = value
		}
	case "unique increase damage", "increase damage":
		if value > out.YellowDamage {
			out.YellowDamage = value
		}
	case "unique increase critical damage", "critical damage":
		if value > out.CriticalDamage {
			out.CriticalDamage = value
		}
	case "add increase damage":
		out.YellowAdditional += value
	case "add increase critical damage":
		out.CriticalAdditional += value
	case "all attack bonus rate":
		// Reinforcement-dependent equipment commonly repeats this stat for
		// every mutually-exclusive threshold in one document. Until the
		// projection carries the proved condition selector, never sum those
		// alternatives into an impossible value.
		if value > out.AllAttack {
			out.AllAttack = value
		}
	}
}

func sectionTokens(doc *dnfpvf.Document, section dnfpvf.Section) []dnfpvf.Token {
	if doc == nil || section.Start < 0 || section.End < section.Start || section.End > len(doc.Tokens) {
		return nil
	}
	return doc.Tokens[section.Start:section.End]
}

func normalizeName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func firstText(tokens []dnfpvf.Token) string {
	for _, token := range tokens {
		if token.Kind == dnfpvf.TokenString || token.Kind == dnfpvf.TokenIdent {
			value := strings.TrimSpace(token.Value)
			if value != "" && value != "%" {
				return value
			}
		}
	}
	return ""
}

func firstInteger(tokens []dnfpvf.Token) int {
	for _, token := range tokens {
		if token.Kind == dnfpvf.TokenInt && token.Int > 0 && token.Int <= math.MaxInt32 {
			return int(token.Int)
		}
	}
	return 0
}

func lastNumber(tokens []dnfpvf.Token) (float64, bool) {
	for index := len(tokens) - 1; index >= 0; index-- {
		switch tokens[index].Kind {
		case dnfpvf.TokenInt:
			return float64(tokens[index].Int), true
		case dnfpvf.TokenFloat:
			return tokens[index].Float, true
		}
	}
	return 0, false
}

func hasPercent(tokens []dnfpvf.Token) bool {
	for _, token := range tokens {
		if strings.TrimSpace(token.Value) == "%" || strings.TrimSpace(token.Raw) == "%" {
			return true
		}
	}
	return false
}
