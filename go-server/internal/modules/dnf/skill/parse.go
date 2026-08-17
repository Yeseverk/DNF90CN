// This file maps one current PVF .skl document into domain fields.
package skill

import (
	"fmt"
	"math"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func parseSkill(job byte, id uint16, skillPath string, doc *dnfpvf.Document) (Skill, error) {
	kind := firstText(doc, "skill type", "type", "kind")
	fixedLevelBase, fixedLevelSkill := doc.Int("fixed level skill")
	fixedLevelInterval := firstIntValue(doc, "interval level")
	if fixedLevelInterval <= 0 {
		fixedLevelInterval = 1
	}
	fixedLevelIncrement := firstIntValue(doc, "add level per interval")
	if fixedLevelIncrement <= 0 {
		fixedLevelIncrement = 1
	}
	prerequisites, err := parsePrerequisites(doc)
	if err != nil {
		return Skill{}, err
	}
	definition := Skill{
		Job:                 job,
		ID:                  id,
		Path:                skillPath,
		Name:                firstText(doc, "name", "display name"),
		Kind:                kind,
		Active:              strings.Contains(strings.ToLower(kind), "active"),
		RequiredLevel:       firstIntValue(doc, "required level", "minimum level", "level"),
		MaximumLevel:        firstIntValue(doc, "maximum level", "max level"),
		FixedLevelSkill:     fixedLevelSkill,
		FixedLevelBase:      int(fixedLevelBase),
		FixedLevelInterval:  fixedLevelInterval,
		FixedLevelIncrement: fixedLevelIncrement,
		SkillClass:          firstIntValue(doc, "skill class"),
		GrowTypes:           intValues(doc, "skill fitness growtype"),
		SecondGrowTypes:     intValues(doc, "skill fitness second growtype"),
		FeatureSkillType:    firstIntValue(doc, "feature skill type"),
		Prerequisites:       prerequisites,
		PurchaseCost:        intValues(doc, "purchase cost"),
		SpecialPurchaseCost: intValues(doc, "special purchase cost"),
		CoolTime:            firstNumberOrZero(doc, "cool time", "cooltime", "cool down time"),
		MPCost:              firstInt(doc, "mp consume", "mp cost", "consume mp"),
		CastTime:            firstNumberOrZero(doc, "casting time", "cast time"),
		Command:             firstText(doc, "command", "input command"),
		Icon:                firstText(doc, "icon", "icon path"),
		Scalars:             scalars(doc),
	}
	if definition.Name == "" {
		definition.Name = definition.Path
	}
	return definition, nil
}

func parsePrerequisites(doc *dnfpvf.Document) ([]Prerequisite, error) {
	values := doc.Ints("pre required skill")
	if len(values) == 0 {
		return nil, nil
	}
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("%w: odd value count %d", ErrPrerequisite, len(values))
	}
	out := make([]Prerequisite, 0, len(values)/2)
	for idx := 0; idx < len(values); idx += 2 {
		if values[idx] < 0 || values[idx] > math.MaxUint16 || values[idx+1] < 0 {
			return nil, fmt.Errorf("%w: skill=%d level=%d", ErrPrerequisite, values[idx], values[idx+1])
		}
		out = append(out, Prerequisite{SkillID: uint16(values[idx]), Level: int(values[idx+1])})
	}
	return out, nil
}

func scalars(doc *dnfpvf.Document) map[string]float64 {
	names := map[string][]string{
		"power":       {"power", "attack power"},
		"hit_count":   {"hit count", "multi hit count"},
		"range":       {"range", "attack range"},
		"duration":    {"duration", "buff duration"},
		"cool_time":   {"cool time", "cooltime"},
		"cast_time":   {"casting time", "cast time"},
		"consume_mp":  {"mp consume", "mp cost", "consume mp"},
		"required_sp": {"required sp", "sp cost"},
	}
	out := make(map[string]float64)
	for key, aliases := range names {
		if value, ok := firstNumber(doc, aliases...); ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstText(doc *dnfpvf.Document, names ...string) string {
	for _, name := range names {
		if value, ok := doc.Text(name); ok {
			return value
		}
	}
	return ""
}

func firstInt(doc *dnfpvf.Document, names ...string) int64 {
	for _, name := range names {
		if value, ok := doc.Int(name); ok {
			return value
		}
	}
	return 0
}

func firstIntValue(doc *dnfpvf.Document, names ...string) int {
	return int(firstInt(doc, names...))
}

func intValues(doc *dnfpvf.Document, name string) []int {
	values := doc.Ints(name)
	if len(values) == 0 {
		return nil
	}
	out := make([]int, len(values))
	for idx, value := range values {
		out[idx] = int(value)
	}
	return out
}

func firstNumber(doc *dnfpvf.Document, names ...string) (float64, bool) {
	for _, name := range names {
		if value, ok := doc.Number(name); ok {
			return value, true
		}
	}
	return 0, false
}

func firstNumberOrZero(doc *dnfpvf.Document, names ...string) float64 {
	value, _ := firstNumber(doc, names...)
	return value
}
