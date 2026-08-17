// 本文件负责把通用 PVF Document 转成奖励领域字段。
package reward

import dnfpvf "longheng.io/server/internal/modules/dnf/pvf"

// parse 把单个奖励 PVF 文档收敛成稳定字段。
// 奖励真正提交必须在 reward/bag/equip owner 中完成，不能在 loader 里产生副作用。
func parse(id int64, rewardPath string, doc *dnfpvf.Document) Reward {
	reward := Reward{
		ID:        id,
		Path:      rewardPath,
		Name:      firstText(doc, "name", "display name"),
		Kind:      firstText(doc, "reward type", "type", "kind"),
		DropIDs:   firstInts(doc, "drops", "drop ids", "drop table", "drop tables"),
		DropPaths: firstTexts(doc, "drop paths", "drop path", "drop table paths"),
		ItemIDs:   firstInts(doc, "items", "item ids", "item list", "reward items"),
		ItemPaths: firstTexts(doc, "item paths", "item path", "reward item paths"),
		Gold:      firstInt(doc, "gold", "reward gold", "money"),
		Exp:       firstInt(doc, "exp", "experience", "reward exp"),
		Scalars:   scalars(doc),
	}
	if reward.Name == "" {
		reward.Name = reward.Path
	}
	return reward
}

// scalars 提取奖励结算常用数值，启动后直接走内存查询。
func scalars(doc *dnfpvf.Document) map[string]float64 {
	names := map[string][]string{
		"gold":       {"gold", "reward gold", "money"},
		"exp":        {"exp", "experience", "reward exp"},
		"clear_exp":  {"clear exp"},
		"fatigue":    {"fatigue", "fatigue cost"},
		"item_count": {"item count", "reward item count"},
		"drop_count": {"drop count"},
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

func firstTexts(doc *dnfpvf.Document, names ...string) []string {
	for _, name := range names {
		values := doc.Texts(name)
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

func firstInt(doc *dnfpvf.Document, names ...string) int64 {
	for _, name := range names {
		if value, ok := doc.Int(name); ok {
			return value
		}
	}
	return 0
}

func firstInts(doc *dnfpvf.Document, names ...string) []int64 {
	for _, name := range names {
		values := doc.Ints(name)
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

func firstNumber(doc *dnfpvf.Document, names ...string) (float64, bool) {
	for _, name := range names {
		if value, ok := doc.Number(name); ok {
			return value, true
		}
	}
	return 0, false
}
