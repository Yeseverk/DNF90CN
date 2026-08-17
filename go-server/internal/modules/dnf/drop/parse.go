// 本文件负责把通用 PVF Document 转成掉落领域字段。
package drop

import dnfpvf "longheng.io/server/internal/modules/dnf/pvf"

// parse 把单个掉落 PVF 文档收敛成稳定字段。
// 没识别出的项目私有字段仍保留在通用 Document 里，后续可以按真实 PVF 样本继续扩展。
func parse(id int64, dropPath string, doc *dnfpvf.Document) Entry {
	entry := Entry{
		ID:        id,
		Path:      dropPath,
		Name:      firstText(doc, "name", "display name"),
		Kind:      firstText(doc, "drop type", "type", "kind"),
		ItemIDs:   firstInts(doc, "items", "item ids", "item list", "drops", "drop items"),
		ItemPaths: firstTexts(doc, "item paths", "item path", "drop paths", "drop path"),
		Items:     weightedItems(doc),
		Gold:      firstInt(doc, "gold", "money", "coin"),
		MinCount:  firstInt(doc, "min count", "minimum count"),
		MaxCount:  firstInt(doc, "max count", "maximum count"),
		Scalars:   scalars(doc),
	}
	if entry.Name == "" {
		entry.Name = entry.Path
	}
	return entry
}

// weightedItems 解析常见的“物品 id/路径 + 权重”段落。
// 不在这里执行概率随机，掉落随机应由 battle/dungeon 结算后的 reward owner 处理。
func weightedItems(doc *dnfpvf.Document) []WeightedItem {
	for _, name := range []string{"weighted items", "item weights", "drop rate", "drop rates", "probability"} {
		tokens, ok := doc.Section(name)
		if !ok {
			continue
		}
		items := parseWeighted(tokens)
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

// parseWeighted 兼容 `1001 0.75` 和 `item/path 0.25` 两类引用写法。
func parseWeighted(tokens []dnfpvf.Token) []WeightedItem {
	out := make([]WeightedItem, 0)
	for idx := 0; idx < len(tokens); idx++ {
		item, ok := itemRef(tokens[idx])
		if !ok {
			continue
		}
		weight, next, ok := numberAfter(tokens, idx+1)
		if !ok {
			continue
		}
		item.Weight = weight
		out = append(out, item)
		idx = next
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func itemRef(token dnfpvf.Token) (WeightedItem, bool) {
	switch token.Kind {
	case dnfpvf.TokenInt:
		return WeightedItem{ID: token.Int}, true
	case dnfpvf.TokenString, dnfpvf.TokenIdent:
		if token.Value == "" {
			return WeightedItem{}, false
		}
		return WeightedItem{Path: token.Value}, true
	default:
		return WeightedItem{}, false
	}
}

func numberAfter(tokens []dnfpvf.Token, start int) (float64, int, bool) {
	for idx := start; idx < len(tokens); idx++ {
		token := tokens[idx]
		if token.Kind == dnfpvf.TokenSymbol {
			continue
		}
		if value, ok := tokenNumber(token); ok {
			return value, idx, true
		}
		return 0, start, false
	}
	return 0, start, false
}

func tokenNumber(token dnfpvf.Token) (float64, bool) {
	switch token.Kind {
	case dnfpvf.TokenInt:
		return float64(token.Int), true
	case dnfpvf.TokenFloat:
		return token.Float, true
	default:
		return 0, false
	}
}

// scalars 提取结算可能用到的数值字段，方便启动后直接走内存查询。
func scalars(doc *dnfpvf.Document) map[string]float64 {
	names := map[string][]string{
		"gold":        {"gold", "money", "coin"},
		"min_count":   {"min count", "minimum count"},
		"max_count":   {"max count", "maximum count"},
		"drop_rate":   {"drop rate", "drop rates", "probability"},
		"party_bonus": {"party bonus"},
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
