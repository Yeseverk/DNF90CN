package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

// runQ3 勘察被动对象定义文件是否声明行为类型/事件脚本。
//   - 对 passiveobject.lst 全部条目做路径归类（无需读文件）
//   - 读取 lst 引用的 .obj 定义（可用 objLimit 截断），做 section 普查与行为字段值普查
//   - 对"实际被地图摆放"的对象集合（profiles）做行为分类与签名分组
func runQ3(
	archive *platformpvf.Archive,
	_ *worldmap.Table,
	_ map[int64][]mapUsage,
	objRefs map[int64]string,
	profiles map[int64]*objProfile,
	usedOnMaps map[int64]bool,
	usedOnDungeonMaps map[int64]bool,
	objLimit int,
) map[string]any {
	// 1. 路径归类
	taxonomy1 := map[string]int{}
	taxonomy2 := map[string]int{}
	for _, ref := range objRefs {
		parts := strings.Split(strings.ToLower(ref), "/")
		if len(parts) > 0 {
			taxonomy1[parts[0]]++
		}
		if len(parts) > 1 {
			taxonomy2[parts[0]+"/"+parts[1]]++
		}
	}

	// 2. 全 lst 的 .obj section 普查 + 行为字段值普查
	ids := sortedIDsBoolKeys(objRefs)
	if objLimit > 0 && objLimit < len(ids) {
		ids = ids[:objLimit]
	}
	sectionFiles := map[string]int{}
	typeValues := map[string]int{}
	subTypeValues := map[string]int{}
	destroyCondKinds := map[string]int{} // destroy condition 种类（首 token）
	readOK, readFail, parseFail := 0, 0, 0
	var missingIDs []int64
	started := time.Now()
	for i, id := range ids {
		ref := objRefs[id]
		text, ok := readObjText(archive, ref)
		if !ok {
			readFail++
			missingIDs = append(missingIDs, id)
			continue
		}
		readOK++
		doc, err := worldmap.ParseDocument(ref, text)
		if err != nil {
			parseFail++
			continue
		}
		seen := map[string]bool{}
		for _, section := range doc.Sections {
			name := strings.ToLower(strings.TrimSpace(section.Name))
			if name == "" || strings.HasPrefix(name, "/") || seen[name] {
				continue
			}
			seen[name] = true
			sectionFiles[name]++
			switch name {
			case "passive object type":
				typeValues[strings.Join(sectionTexts(doc, section), " ")]++
			case "passive object sub type":
				subTypeValues[strings.Join(sectionTexts(doc, section), " ")]++
			case "object destroy condition":
				values := sectionTexts(doc, section)
				if len(values) > 0 {
					destroyCondKinds[values[0]]++
				}
			}
		}
		if i > 0 && i%10000 == 0 {
			fmt.Printf("[Q3] ... %d/%d obj files (%.1fs)\n", i, len(ids), time.Since(started).Seconds())
		}
	}
	sort.Slice(missingIDs, func(i, j int) bool { return missingIDs[i] < missingIDs[j] })

	// 3. 地图摆放对象的行为分类与签名分组
	classCount := map[string]int{}
	classCountDungeon := map[string]int{}
	sigCount := map[string][]int64{}
	for id, profile := range profiles {
		classCount[profile.BehaviorClass]++
		if usedOnDungeonMaps[id] {
			classCountDungeon[profile.BehaviorClass]++
		}
		sig := signatureOf(profile)
		sigCount[sig] = append(sigCount[sig], id)
	}
	type sigEntry struct {
		Signature string  `json:"signature"`
		Count     int     `json:"count"`
		Examples  []int64 `json:"examples"`
	}
	var sigs []sigEntry
	for sig, list := range sigCount {
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
		examples := list
		if len(examples) > 10 {
			examples = examples[:10]
		}
		sigs = append(sigs, sigEntry{Signature: sig, Count: len(list), Examples: examples})
	}
	sort.Slice(sigs, func(i, j int) bool { return sigs[i].Count > sigs[j].Count })

	var sigLines []string
	sigLines = append(sigLines, "count\tsignature\texample_object_ids")
	for _, s := range sigs {
		sigLines = append(sigLines, fmt.Sprintf("%d\t%s\t%v", s.Count, s.Signature, s.Examples))
	}
	writeLines("q3_obj_signatures.tsv", sigLines)

	var sectionLines []string
	sectionLines = append(sectionLines, "section\tfiles")
	for _, kv := range sortedPair(sectionFiles) {
		sectionLines = append(sectionLines, fmt.Sprintf("%s\t%d", kv.k, kv.v))
	}
	writeLines("q3_obj_section_census.tsv", sectionLines)

	var missingLines []string
	missingLines = append(missingLines, "object_id\tref")
	for _, id := range missingIDs {
		missingLines = append(missingLines, fmt.Sprintf("%d\t%s", id, objRefs[id]))
	}
	writeLines("q3_missing_obj_refs.tsv", missingLines)

	// 电梯三件套定义画像（EXE 硬编码证据）
	elevatorTrio := map[int64]*objProfile{}
	for _, id := range []int64{1111, 1112, 1113} {
		if p, ok := profiles[id]; ok {
			elevatorTrio[id] = p
		} else {
			sub := buildObjProfiles(archive, objRefs, map[int64]bool{id: true})
			elevatorTrio[id] = sub[id]
		}
	}

	fmt.Printf("[Q3] lst=%d read_ok=%d read_fail=%d parse_fail=%d used_on_maps=%d used_on_dungeon_maps=%d\n",
		len(objRefs), readOK, readFail, parseFail, len(usedOnMaps), len(usedOnDungeonMaps))
	fmt.Printf("[Q3] behavior_class(all_used)=%v (dungeon_used)=%v sigs=%d (%.1fs)\n",
		classCount, classCountDungeon, len(sigs), time.Since(started).Seconds())
	fmt.Printf("[Q3] top sections: %v\n", topPairs(sectionFiles, 12))
	fmt.Printf("[Q3] passive_object_type values=%d sub_type values=%d destroy_condition kinds=%v\n",
		len(typeValues), len(subTypeValues), destroyCondKinds)
	return map[string]any{
		"lst_entries":              len(objRefs),
		"obj_read_ok":              readOK,
		"obj_read_fail":            readFail,
		"obj_parse_fail":           parseFail,
		"missing_ref_ids":          missingIDs,
		"taxonomy_level1":          taxonomy1,
		"taxonomy_level2":          taxonomy2,
		"used_on_any_map":          len(usedOnMaps),
		"used_on_dungeon_maps":     len(usedOnDungeonMaps),
		"behavior_class_used":      classCount,
		"behavior_class_dungeon":   classCountDungeon,
		"section_census":           sectionFiles,
		"passive_object_type":      typeValues,
		"passive_object_sub_type":  subTypeValues,
		"destroy_condition_kinds":  destroyCondKinds,
		"signatures":               sigs,
		"elevator_trio_profiles":   elevatorTrio,
		"behavior_class_semantics": "scripted_action=含basic/etc/last action(.act脚本); declared_behavior=有行为字段但无动作脚本; presentation_only=仅呈现; missing/not_in_lst=定义缺失",
		"full_list_files":          []string{"out/q3_obj_section_census.tsv", "out/q3_obj_signatures.tsv", "out/q3_missing_obj_refs.tsv"},
		"obj_limit":                objLimit,
	}
}

func signatureOf(profile *objProfile) string {
	if profile == nil {
		return "(unknown)"
	}
	if !profile.Found {
		return "(" + profile.BehaviorClass + ")"
	}
	var names []string
	for _, name := range profile.Sections {
		if presentationSections[name] {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "(presentation-only)"
	}
	sort.Strings(names)
	return strings.Join(names, "+")
}

type pair struct {
	k string
	v int
}

func sortedPair(m map[string]int) []pair {
	out := make([]pair, 0, len(m))
	for k, v := range m {
		out = append(out, pair{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].v != out[j].v {
			return out[i].v > out[j].v
		}
		return out[i].k < out[j].k
	})
	return out
}

func topPairs(m map[string]int, n int) []pair {
	out := sortedPair(m)
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func sortedIDsBoolKeys[V any](m map[int64]V) []int64 {
	out := make([]int64, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func readObjText(archive *platformpvf.Archive, ref string) (string, bool) {
	ref = strings.ReplaceAll(strings.TrimSpace(ref), "\\", "/")
	for _, candidate := range []string{ref, "passiveobject/" + ref} {
		if text, err := archive.ReadText(candidate); err == nil {
			return text, true
		}
	}
	return "", false
}
