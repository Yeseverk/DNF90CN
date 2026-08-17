package main

import (
	"fmt"
	"sort"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

// runQ2 统计 SpecialPassiveObject 的 Spawns 中 Kind 归一化为 monster 的
// (ObjectID, monsterCode) 组合、出现次数与所在地图/dungeon。
func runQ2(table *worldmap.Table, usages map[int64][]mapUsage, _ map[int64]string, objRefs map[int64]string) map[string]any {
	combos := map[comboKey]*comboInfo{}
	kindCensus := map[string]int{}
	totalSpecial := 0
	totalWithSpawns := 0

	for _, m := range table.Maps() {
		for _, obj := range m.SpecialPassiveObjects {
			totalSpecial++
			if len(obj.Spawns) > 0 {
				totalWithSpawns++
			}
			for _, spawn := range obj.Spawns {
				kind := normSymbol(spawn.Kind)
				kindCensus[kind]++
				if kind != "monster" {
					continue
				}
				key := comboKey{ObjectID: obj.ObjectID, Code: spawn.Code}
				info, ok := combos[key]
				if !ok {
					info = &comboInfo{
						ObjectID: obj.ObjectID, Code: spawn.Code,
						Maps: map[int64]bool{}, Dungeons: map[int64]bool{},
						Levels: map[int64]bool{}, RawKinds: map[string]bool{},
						ObjRef: objRefs[obj.ObjectID],
					}
					combos[key] = info
				}
				info.Count++
				info.Maps[m.ID] = true
				info.Levels[spawn.Level] = true
				info.RawKinds[spawn.Kind] = true
				for _, u := range usages[m.ID] {
					info.Dungeons[u.DungeonID] = true
				}
			}
		}
	}

	var list []*comboInfo
	for _, info := range combos {
		info.MapList = sortedIDs(info.Maps)
		info.DgList = sortedIDs(info.Dungeons)
		info.LevelVal = sortedIDs(info.Levels)
		list = append(list, info)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].ObjectID != list[j].ObjectID {
			return list[i].ObjectID < list[j].ObjectID
		}
		return list[i].Code < list[j].Code
	})

	var lines []string
	lines = append(lines, "object_id\tmonster_code\tcount\tmaps\tdungeons\tobj_ref")
	for _, info := range list {
		lines = append(lines, fmt.Sprintf("%d\t%d\t%d\t%v\t%v\t%s",
			info.ObjectID, info.Code, info.Count, info.MapList, info.DgList, info.ObjRef))
	}
	writeLines("q2_special_spawn_combos.tsv", lines)

	fmt.Printf("[Q2] special_objects=%d with_spawns=%d monster_combos=%d kind_census=%v\n",
		totalSpecial, totalWithSpawns, len(list), kindCensus)
	return map[string]any{
		"total_special_objects": totalSpecial,
		"with_spawns":           totalWithSpawns,
		"kind_census":           kindCensus,
		"monster_combo_count":   len(list),
		"combos":                list,
		"full_list_file":        "out/q2_special_spawn_combos.tsv",
		"count_semantics":       "count = 全 PVF 地图上该 (ObjectID, monsterCode) spawn 条目出现次数（每次摆放计入）",
	}
}
