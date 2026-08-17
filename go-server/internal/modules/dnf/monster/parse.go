// 本文件负责把通用 PVF Document 转成怪物领域字段。
package monster

import dnfpvf "longheng.io/server/internal/modules/dnf/pvf"

// parse 把单个怪物 PVF 文档收敛成稳定字段。
// 怪物出生、AI、受击和死亡事件由 dungeon/battle room 使用这些静态字段驱动。
func parse(id int64, monsterPath string, doc *dnfpvf.Document) Monster {
	monster := Monster{
		ID:       id,
		Path:     monsterPath,
		Name:     firstText(doc, "name", "display name"),
		Kind:     firstText(doc, "monster type", "type", "kind"),
		Rank:     firstText(doc, "rank", "monster rank", "grade"),
		Level:    firstInt(doc, "level", "monster level"),
		HP:       firstInt(doc, "hp", "hit point", "max hp"),
		Attack:   firstInt(doc, "attack", "physical attack"),
		Defense:  firstInt(doc, "defense", "physical defense"),
		Move:     firstNumberOrZero(doc, "move speed", "movement speed"),
		Speed:    firstNumberOrZero(doc, "attack speed"),
		Exp:      firstInt(doc, "exp", "experience"),
		AI:       firstText(doc, "ai", "ai pattern"),
		Icon:     firstText(doc, "icon", "icon path"),
		Scalars:  scalars(doc),
		Sections: sectionNames(doc),
	}
	if monster.Name == "" {
		monster.Name = monster.Path
	}
	return monster
}

// scalars 提取怪物基础数值，供战斗房间启动后直接内存查询。
func scalars(doc *dnfpvf.Document) map[string]float64 {
	names := map[string][]string{
		"hp":            {"hp", "hit point", "max hp"},
		"attack":        {"attack", "physical attack"},
		"magic_attack":  {"magical attack", "magic attack"},
		"defense":       {"defense", "physical defense"},
		"magic_defense": {"magical defense", "magic defense"},
		"move_speed":    {"move speed", "movement speed"},
		"attack_speed":  {"attack speed"},
		"hit_recovery":  {"hit recovery"},
		"exp":           {"exp", "experience"},
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

// sectionNames 保留源文档段落名，便于真实 PVF 样本接入时排查字段覆盖范围。
func sectionNames(doc *dnfpvf.Document) []string {
	if doc == nil || len(doc.Sections) == 0 {
		return nil
	}
	out := make([]string, 0, len(doc.Sections))
	for _, section := range doc.Sections {
		out = append(out, section.Name)
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
