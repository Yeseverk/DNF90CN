//go:build ignore

// explore5.go 深挖被动对象行为字段：
//  1. 找出 readObjText 失败的 496 个 ref 的形态
//  2. 普查 [passive object type] / [passive object sub type] / [object destroy condition] 的值
//  3. 抽样 .act 文件内容（basic action 引用的动作脚本是否含事件语义）
package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

var lstEntryPattern = regexp.MustCompile("([0-9]+)\\s+`([^`]+)`")

type pair struct {
	k string
	v int
}

func sortedPairs(m map[string]int) []pair {
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

func main() {
	archive, err := platformpvf.Open(`D:/DNF/runtime/data/dnf/Script.pvf`)
	if err != nil {
		panic(err)
	}
	lst, err := archive.ReadText("passiveobject/passiveobject.lst")
	if err != nil {
		panic(err)
	}
	objects := map[int64]string{}
	for _, m := range lstEntryPattern.FindAllStringSubmatch(lst, -1) {
		var id int64
		fmt.Sscan(m[1], &id)
		if _, ok := objects[id]; !ok {
			objects[id] = strings.ReplaceAll(m[2], "\\", "/")
		}
	}

	readObj := func(ref string) (string, string, bool) {
		ref = strings.TrimSpace(ref)
		for _, c := range []string{ref, "passiveobject/" + ref} {
			if t, err := archive.ReadText(c); err == nil {
				return t, c, true
			}
		}
		return "", "", false
	}

	// 1. 失败 ref 形态
	var fails []string
	for id, ref := range objects {
		if _, _, ok := readObj(ref); !ok {
			fails = append(fails, fmt.Sprintf("id=%d ref=%q", id, ref))
		}
	}
	sort.Strings(fails)
	fmt.Printf("== read fails: %d ==\n", len(fails))
	limit := 25
	if len(fails) < limit {
		limit = len(fails)
	}
	for _, f := range fails[:limit] {
		fmt.Printf("  %s\n", f)
	}

	// 2. 行为字段值普查
	typeValues := map[string]int{}    // passive object type 值
	subTypeValues := map[string]int{} // passive object sub type 值
	destroyCond := map[string]int{}   // object destroy condition 首 token 值
	sectionValues := func(text string, section string) ([]string, bool) {
		doc, err := worldmap.ParseDocument("probe", text)
		if err != nil {
			return nil, false
		}
		tokens, ok := doc.Section(section)
		if !ok {
			return nil, false
		}
		var out []string
		for _, t := range tokens {
			if t.Kind == "string" || t.Kind == "ident" {
				out = append(out, t.Value+t.Raw)
			} else if t.Kind == "int" {
				out = append(out, fmt.Sprintf("%d", t.Int))
			}
		}
		return out, true
	}

	actRefs := map[string]bool{}
	for _, ref := range objects {
		text, _, ok := readObj(ref)
		if !ok {
			continue
		}
		if values, ok := sectionValues(text, "passive object type"); ok && len(values) > 0 {
			typeValues[strings.Join(values, " ")]++
		}
		if values, ok := sectionValues(text, "passive object sub type"); ok && len(values) > 0 {
			subTypeValues[strings.Join(values, " ")]++
		}
		if values, ok := sectionValues(text, "object destroy condition"); ok && len(values) > 0 {
			destroyCond[strings.Join(values, " ")]++
		}
		if values, ok := sectionValues(text, "basic action"); ok {
			for _, v := range values {
				if v != "" {
					actRefs[v] = true
				}
			}
		}
	}
	fmt.Printf("\n== [passive object type] values (%d distinct) ==\n", len(typeValues))
	for _, p := range sortedPairs(typeValues) {
		fmt.Printf("  %-50s %d\n", p.k, p.v)
	}
	fmt.Printf("\n== [passive object sub type] values (%d distinct) ==\n", len(subTypeValues))
	for _, p := range sortedPairs(subTypeValues) {
		fmt.Printf("  %-50s %d\n", p.k, p.v)
	}
	fmt.Printf("\n== [object destroy condition] values (%d distinct, top 30) ==\n", len(destroyCond))
	pairs := sortedPairs(destroyCond)
	if len(pairs) > 30 {
		pairs = pairs[:30]
	}
	for _, p := range pairs {
		fmt.Printf("  %-50s %d\n", p.k, p.v)
	}
	fmt.Printf("\n== distinct basic action refs: %d ==\n", len(actRefs))

	// 3. 抽样 .act 文件内容
	shown := 0
	for ref := range actRefs {
		text, used, ok := readObj(strings.Trim(ref, "`\"'"))
		if !ok {
			// .act 引用相对 obj 目录，无法直接定位；尝试常见根
			for _, root := range []string{"passiveobject/", "passiveobject/actionobject/", "passiveobject/mapobject/"} {
				if t, err := archive.ReadText(root + strings.Trim(ref, "`\"'")); err == nil {
					text, used, ok = t, root+ref, true
					break
				}
			}
		}
		if !ok || text == "" {
			continue
		}
		fmt.Printf("----- act %q (resolved %q, %d bytes) -----\n", ref, used, len(text))
		lines := strings.Split(text, "\n")
		n := 70
		if len(lines) < n {
			n = len(lines)
		}
		for _, line := range lines[:n] {
			fmt.Printf("  %s\n", strings.TrimRight(line, "\r"))
		}
		shown++
		if shown >= 2 {
			break
		}
	}
}
