package main

import (
	"fmt"
	"sort"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

// runQ5 列出"电梯型"候选：≥1 EventMonsterPositions 且 special passive object 生成 monster。
// 并借助 objProfile 判断候选地图涉及的对象行为是数据驱动还是呈现壳（EXE 硬编码）。
func runQ5(
	table *worldmap.Table,
	usages map[int64][]mapUsage,
	dungeonNames map[int64]string,
	objRefs map[int64]string,
	profiles map[int64]*objProfile,
) map[string]any {
	var candidates []q5Candidate
	classShapeCount := map[string]int{}
	for _, m := range table.Maps() {
		if len(m.EventMonsterPositions) == 0 {
			continue
		}
		var specials []string
		for _, obj := range m.SpecialPassiveObjects {
			for _, spawn := range obj.Spawns {
				if normSymbol(spawn.Kind) != "monster" {
					continue
				}
				specials = append(specials, fmt.Sprintf("object=%d -> monster=%d level=%d ref=%s",
					obj.ObjectID, spawn.Code, spawn.Level, objRefs[obj.ObjectID]))
			}
		}
		if len(specials) == 0 {
			continue
		}
		var passiveIDs []int64
		seen := map[int64]bool{}
		involved := map[int64]bool{}
		for _, o := range m.PassiveObjects {
			involved[o.ObjectID] = true
			if !seen[o.ObjectID] {
				seen[o.ObjectID] = true
				passiveIDs = append(passiveIDs, o.ObjectID)
			}
		}
		for _, o := range m.SpecialPassiveObjects {
			involved[o.ObjectID] = true
		}
		sort.Slice(passiveIDs, func(i, j int) bool { return passiveIDs[i] < passiveIDs[j] })
		sort.Strings(specials)
		isD53 := false
		for _, u := range usages[m.ID] {
			if u.DungeonID == 53 {
				isD53 = true
			}
		}
		// 涉及对象的行为分类汇总
		classCount := map[string]int{}
		for id := range involved {
			if p, ok := profiles[id]; ok {
				classCount[p.BehaviorClass]++
			} else {
				classCount["unprofiled"]++
			}
		}
		classKey := fmt.Sprintf("presentation=%d/declared=%d/scripted=%d/other=%d",
			classCount["presentation_only"], classCount["declared_behavior"],
			classCount["scripted_action"], classCount["missing"]+classCount["not_in_lst"]+classCount["parse_error"]+classCount["unprofiled"])
		classShapeCount[classKey]++
		candidates = append(candidates, q5Candidate{
			MapID: m.ID, Path: m.Path, Name: m.Name,
			Positions: len(m.EventMonsterPositions), PassiveIDs: passiveIDs,
			SpecialObjects: specials, Dungeons: usageStrings(usages[m.ID]),
			IsDungeon53: isD53, ObjectClasses: classKey,
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].MapID < candidates[j].MapID })

	var lines []string
	lines = append(lines, "map_id\tpositions\tpath\tname\tpassive_ids\tspecial_objects\tobject_classes\tdungeons\tknown_dungeon_53")
	for _, c := range candidates {
		lines = append(lines, fmt.Sprintf("%d\t%d\t%s\t%s\t%v\t%s\t%s\t%s\t%t",
			c.MapID, c.Positions, c.Path, c.Name, c.PassiveIDs,
			joinOr(c.SpecialObjects, "; "), c.ObjectClasses, joinOr(c.Dungeons, "; "), c.IsDungeon53))
	}
	writeLines("q5_elevator_candidates.tsv", lines)

	shapeCount := map[string]int{}
	for _, c := range candidates {
		shapeCount[fmt.Sprintf("positions=%d/specials=%d", c.Positions, len(c.SpecialObjects))]++
	}

	d53 := 0
	purePresentation := 0
	for _, c := range candidates {
		if c.IsDungeon53 {
			d53++
		}
		if c.ObjectClasses == classKeyOf(1, 0, 0, 0) || isAllPresentation(c.ObjectClasses) {
			purePresentation++
		}
	}
	fmt.Printf("[Q5] elevator_type_candidates=%d (known_dungeon_53=%d)\n", len(candidates), d53)
	fmt.Printf("[Q5] shapes=%v\n", shapeCount)
	fmt.Printf("[Q5] object_class_shapes=%v\n", classShapeCount)
	return map[string]any{
		"candidate_count":      len(candidates),
		"known_dungeon_53":     d53,
		"shape_census":         shapeCount,
		"object_class_census":  classShapeCount,
		"candidates":           candidates,
		"full_list_file":       "out/q5_elevator_candidates.tsv",
		"candidate_rule":       "len(EventMonsterPositions)>=1 且存在 SpecialPassiveObject 的 spawn Kind 归一化为 monster",
		"object_class_meaning": "presentation=presentation_only declared=declared_behavior scripted=scripted_action other=missing/not_in_lst/parse_error",
		"dungeon_name_index":   dungeonNames,
	}
}

func classKeyOf(presentation, declared, scripted, other int) string {
	return fmt.Sprintf("presentation=%d/declared=%d/scripted=%d/other=%d", presentation, declared, scripted, other)
}

func isAllPresentation(classKey string) bool {
	var p, d, s, o int
	if _, err := fmt.Sscanf(classKey, "presentation=%d/declared=%d/scripted=%d/other=%d", &p, &d, &s, &o); err != nil {
		return false
	}
	return p > 0 && d == 0 && s == 0 && o == 0
}
