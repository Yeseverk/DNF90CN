package main

import (
	"fmt"
	"os"
	"path"
	"sort"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

// runQ1 统计含 EventMonsterPositions 的地图总量、分布直方图与 dungeon 归属。
func runQ1(table *worldmap.Table, usages map[int64][]mapUsage, dungeonNames map[int64]string) map[string]any {
	var all []q1Map
	histogram := map[int]int{}
	dungeonSet := map[int64]bool{}
	dungeonMaps := 0
	for _, m := range table.Maps() {
		n := len(m.EventMonsterPositions)
		if n == 0 {
			continue
		}
		histogram[n]++
		entry := q1Map{MapID: m.ID, Path: m.Path, Name: m.Name, Positions: n, Dungeon: usageStrings(usages[m.ID])}
		all = append(all, entry)
		if len(usages[m.ID]) > 0 {
			dungeonMaps++
			for _, u := range usages[m.ID] {
				dungeonSet[u.DungeonID] = true
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].MapID < all[j].MapID })

	// TSV 完整清单
	var lines []string
	lines = append(lines, "map_id\tpositions\tpath\tname\tdungeons")
	for _, e := range all {
		lines = append(lines, fmt.Sprintf("%d\t%d\t%s\t%s\t%s", e.MapID, e.Positions, e.Path, e.Name, joinOr(e.Dungeon, "; ")))
	}
	writeLines("q1_event_monster_maps.tsv", lines)

	// 涉及 dungeon 清单（ID + 名称）
	type dg struct {
		ID   int64  `json:"id"`
		Name string `json:"name,omitempty"`
	}
	var dungeons []dg
	for _, id := range sortedIDs(dungeonSet) {
		dungeons = append(dungeons, dg{ID: id, Name: dungeonNames[id]})
	}

	fmt.Printf("[Q1] maps_with_event_monster_positions=%d (dungeon-associated=%d) dungeons=%d histogram=%v\n",
		len(all), dungeonMaps, len(dungeons), histogram)
	return map[string]any{
		"total_maps":             len(all),
		"dungeon_associated":     dungeonMaps,
		"histogram":              histogram,
		"dungeons":               dungeons,
		"maps":                   all,
		"full_list_file":         "out/q1_event_monster_maps.tsv",
		"histogram_description":  "key = EventMonsterPositions 个数, value = 地图数",
		"association_definition": "经 maze specification/boss/layered 引用或 map [dungeon] ownership 关联到 dungeon",
	}
}

func joinOr(values []string, sep string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, v := range values[1:] {
		out += sep + v
	}
	return out
}

func writeLines(name string, lines []string) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		panic(err)
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(path.Join(outDir, name), []byte(content), 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("[survey] wrote %s (%d lines)\n", path.Join(outDir, name), len(lines))
}
