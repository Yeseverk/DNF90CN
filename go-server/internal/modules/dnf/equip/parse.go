// 本文件负责把通用 PVF Document 转成装备领域字段。
package equip

import dnfpvf "longheng.io/server/internal/modules/dnf/pvf"

// parseItem 把单个装备 PVF 文档收敛成稳定字段。
// 装备穿戴、耐久、强化和交易限制不在 loader 中判断，后续交给 equip owner。
func parseItem(id int64, itemPath string, doc *dnfpvf.Document) Item {
	item := Item{
		ID:    id,
		Path:  itemPath,
		Name:  firstText(doc, "name", "display name"),
		Kind:  firstText(doc, "equipment type", "type", "kind"),
		Slot:  firstText(doc, "attach type", "slot", "part"),
		Level: firstInt(doc, "minimum level", "required level", "level"),
		Icon:  firstText(doc, "icon", "icon path"),
		Stats: stats(doc),
	}
	item.Rarity = firstText(doc, "rarity", "grade")
	if item.Name == "" {
		item.Name = item.Path
	}
	return item
}

// stats 提取装备基础数值，启动后查询装备属性不再重新解析 PVF 文本。
func stats(doc *dnfpvf.Document) map[string]float64 {
	names := map[string][]string{
		"physical_attack":    {"physical attack", "physical weapon attack"},
		"magical_attack":     {"magical attack", "magical weapon attack"},
		"independent_attack": {"independent attack"},
		"strength":           {"strength"},
		"intelligence":       {"intelligence"},
		"vitality":           {"vitality"},
		"spirit":             {"spirit"},
		"attack_speed":       {"attack speed"},
		"cast_speed":         {"cast speed"},
		"move_speed":         {"move speed"},
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

func firstNumber(doc *dnfpvf.Document, names ...string) (float64, bool) {
	for _, name := range names {
		if value, ok := doc.Number(name); ok {
			return value, true
		}
	}
	return 0, false
}
