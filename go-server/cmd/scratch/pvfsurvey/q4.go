package main

import (
	"fmt"
	"sort"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

// runQ4 统计 dungeon maze 的 [destroy object] clear 条件与被动对象摆放的关联。
// 参照 2026-07-31 空房结算修复的形状：地图恰好摆放 count 个该对象。
func runQ4(table *worldmap.Table, usages map[int64][]mapUsage, dungeonNames map[int64]string, objRefs map[int64]string) map[string]any {
	mapsByID := map[int64]worldmap.Map{}
	for _, m := range table.Maps() {
		if _, ok := mapsByID[m.ID]; !ok {
			mapsByID[m.ID] = m
		}
	}

	var conditions []q4Condition
	statusCount := map[string]int{}
	totalConditions := 0
	for _, d := range table.Dungeons() {
		// 该 dungeon 下所有地图（任意 usage 来源）
		dungeonMaps := map[int64]bool{}
		for mapID, list := range usages {
			for _, u := range list {
				if u.DungeonID == d.ID {
					dungeonMaps[mapID] = true
				}
			}
		}
		for _, maze := range d.Mazes {
			for _, cond := range maze.ClearConditions {
				if normSymbol(cond.Type) != "destroy object" {
					continue
				}
				totalConditions++
				entry := q4Condition{
					DungeonID: d.ID, DungeonName: dungeonNames[d.ID], MazeIndex: maze.Index,
					TargetID: cond.TargetID, Count: cond.Count, ObjRef: objRefs[cond.TargetID],
				}
				matchMaps := 0
				placementTotal := 0
				for mapID := range dungeonMaps {
					m := mapsByID[mapID]
					passive, special := 0, 0
					for _, o := range m.PassiveObjects {
						if o.ObjectID == cond.TargetID {
							passive++
						}
					}
					for _, o := range m.SpecialPassiveObjects {
						if o.ObjectID == cond.TargetID {
							special++
						}
					}
					if passive+special == 0 {
						continue
					}
					placementTotal += passive + special
					entry.Placements = append(entry.Placements,
						fmt.Sprintf("map=%d passive=%d special=%d", mapID, passive, special))
					if int64(passive+special) == cond.Count {
						matchMaps++
					}
				}
				sort.Strings(entry.Placements)
				switch {
				case len(entry.Placements) == 0:
					entry.Status = "no_placement_in_dungeon_maps"
				case matchMaps > 0:
					entry.Status = "count_match"
				default:
					entry.Status = "count_mismatch"
				}
				statusCount[entry.Status]++
				conditions = append(conditions, entry)
			}
		}
	}
	sort.Slice(conditions, func(i, j int) bool {
		if conditions[i].DungeonID != conditions[j].DungeonID {
			return conditions[i].DungeonID < conditions[j].DungeonID
		}
		if conditions[i].MazeIndex != conditions[j].MazeIndex {
			return conditions[i].MazeIndex < conditions[j].MazeIndex
		}
		return conditions[i].TargetID < conditions[j].TargetID
	})

	var lines []string
	lines = append(lines, "dungeon_id\tdungeon_name\tmaze_index\ttarget_id\tcount\tstatus\tplacements\tobj_ref")
	for _, c := range conditions {
		lines = append(lines, fmt.Sprintf("%d\t%s\t%d\t%d\t%d\t%s\t%s\t%s",
			c.DungeonID, c.DungeonName, c.MazeIndex, c.TargetID, c.Count, c.Status,
			joinOr(c.Placements, "; "), c.ObjRef))
	}
	writeLines("q4_destroy_object_conditions.tsv", lines)

	fmt.Printf("[Q4] destroy_object_conditions=%d status=%v\n", totalConditions, statusCount)
	return map[string]any{
		"total_conditions": totalConditions,
		"status_census":    statusCount,
		"conditions":       conditions,
		"full_list_file":   "out/q4_destroy_object_conditions.tsv",
		"status_semantics": "count_match = 该 dungeon 至少一张地图摆放数量与 clear count 相等; no_placement = dungeon 地图中找不到该对象摆放",
	}
}
