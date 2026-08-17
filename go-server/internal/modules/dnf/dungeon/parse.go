// 本文件负责把通用 PVF Document 转成地下城领域字段。
package dungeon

import dnfpvf "longheng.io/server/internal/modules/dnf/pvf"

// parse 把单个地下城 PVF 文档收敛成稳定字段。
// 进入校验、房间生命周期和战斗结算由 dungeon/battle owner 使用这些静态字段完成。
func parse(id int64, dungeonPath string, doc *dnfpvf.Document) Dungeon {
	dungeon := Dungeon{
		ID:           id,
		Path:         dungeonPath,
		Name:         firstText(doc, "name", "display name"),
		Area:         firstText(doc, "area", "region", "town"),
		Kind:         firstText(doc, "dungeon type", "type", "kind"),
		MinLevel:     firstInt(doc, "minimum level", "min level", "required level"),
		MaxLevel:     firstInt(doc, "maximum level", "max level"),
		Fatigue:      firstInt(doc, "fatigue", "fatigue cost"),
		PartyMin:     firstInt(doc, "minimum party", "party min"),
		PartyMax:     firstInt(doc, "maximum party", "party max"),
		MapPaths:     firstTexts(doc, "maps", "map", "map path"),
		MonsterIDs:   firstInts(doc, "monsters", "monster ids", "monster list"),
		MonsterPaths: firstTexts(doc, "monster paths", "monster path"),
		BossIDs:      firstInts(doc, "boss", "boss ids"),
		BossPaths:    firstTexts(doc, "boss paths", "boss path"),
		RewardPath:   firstText(doc, "reward", "drop", "drop table"),
		Scalars:      scalars(doc),
	}
	if dungeon.Name == "" {
		dungeon.Name = dungeon.Path
	}
	// Abyss seal door positioning: [seal door map index] and [seal door pos].
	dungeon.SealDoorMapIndex = firstInt(doc, "seal door map index")
	if posValues := firstInts(doc, "seal door pos"); len(posValues) >= 2 {
		dungeon.SealDoorPosX = posValues[0]
		dungeon.SealDoorPosY = posValues[1]
	}
	return dungeon
}

// scalars 提取地下城基础数值，供进入校验和结算逻辑直接内存查询。
func scalars(doc *dnfpvf.Document) map[string]float64 {
	names := map[string][]string{
		"min_level": {"minimum level", "min level", "required level"},
		"max_level": {"maximum level", "max level"},
		"fatigue":   {"fatigue", "fatigue cost"},
		"party_min": {"minimum party", "party min"},
		"party_max": {"maximum party", "party max"},
		"clear_exp": {"clear exp", "reward exp"},
		"gold":      {"gold", "reward gold"},
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
