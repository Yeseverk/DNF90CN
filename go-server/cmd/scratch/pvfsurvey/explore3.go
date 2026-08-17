//go:build ignore

// explore3.go 读取 passiveobject/passiveobject.lst 结构并抽样 .obj 定义文件内容。
package main

import (
	"fmt"
	"strings"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func main() {
	archive, err := platformpvf.Open(`D:/DNF/runtime/data/dnf/Script.pvf`)
	if err != nil {
		panic(err)
	}
	lst, err := archive.ReadText("passiveobject/passiveobject.lst")
	if err != nil {
		panic(err)
	}
	lines := strings.Split(lst, "\n")
	fmt.Printf("passiveobject.lst lines=%d bytes=%d\n", len(lines), len(lst))
	limit := 40
	if len(lines) < limit {
		limit = len(lines)
	}
	for _, line := range lines[:limit] {
		fmt.Printf("  %q\n", strings.TrimRight(line, "\r"))
	}

	// 解析 lst 的条目（简单按 id `path` 形态），抽样读 obj 文件
	type entry struct{ id, ref string }
	var entries []entry
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			entries = append(entries, entry{fields[0], strings.Trim(fields[1], "`\"'")})
		}
	}
	fmt.Printf("parsed entries=%d\n", len(entries))
	shown := 0
	for _, e := range entries {
		candidates := []string{e.ref, "passiveobject/" + e.ref}
		var text string
		var used string
		for _, c := range candidates {
			if t, err := archive.ReadText(c); err == nil {
				text, used = t, c
				break
			}
		}
		if text == "" {
			continue
		}
		fmt.Printf("----- id=%s ref=%q used=%q (%d bytes) -----\n", e.id, e.ref, used, len(text))
		tlines := strings.Split(text, "\n")
		limit := 50
		if len(tlines) < limit {
			limit = len(tlines)
		}
		for _, line := range tlines[:limit] {
			fmt.Printf("  %s\n", strings.TrimRight(line, "\r"))
		}
		shown++
		if shown >= 5 {
			break
		}
	}
}
