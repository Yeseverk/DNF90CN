//go:build ignore

// explore.go 是一次性 PVF 目录探查工具：列出顶层目录、passiveobject/ 与 etc/
// 目录文件分布，并抽样打印被动对象定义文件的文本内容。
package main

import (
	"fmt"
	"sort"
	"strings"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func main() {
	archive, err := platformpvf.Open(`D:/DNF/runtime/data/dnf/Script.pvf`)
	if err != nil {
		panic(err)
	}
	snapshot := archive.Snapshot()
	fmt.Printf("format=%s files=%d groups=%d size=%d\n", snapshot.Format, snapshot.FileCount, snapshot.GroupCount, snapshot.Size)

	topDirs := map[string]int{}
	secondDirs := map[string]int{}
	for _, f := range archive.Files() {
		p := strings.ReplaceAll(strings.ToLower(f.Path), "\\", "/")
		parts := strings.Split(p, "/")
		if len(parts) > 1 {
			topDirs[parts[0]]++
			if len(parts) > 2 {
				secondDirs[parts[0]+"/"+parts[1]]++
			}
		}
	}
	fmt.Println("\n== top-level directories ==")
	keys := make([]string, 0, len(topDirs))
	for k := range topDirs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-40s %d\n", k, topDirs[k])
	}

	for _, prefix := range []string{"passiveobject", "etc", "map", "dungeon"} {
		fmt.Printf("\n== %s/ subdirectories ==\n", prefix)
		sub := map[string]int{}
		exts := map[string]int{}
		for _, f := range archive.Files() {
			p := strings.ReplaceAll(strings.ToLower(f.Path), "\\", "/")
			if !strings.HasPrefix(p, prefix+"/") {
				continue
			}
			rest := strings.TrimPrefix(p, prefix+"/")
			parts := strings.Split(rest, "/")
			if len(parts) > 1 {
				sub[parts[0]]++
			} else {
				sub["<root>"]++
			}
			if idx := strings.LastIndex(rest, "."); idx >= 0 {
				exts[rest[idx:]]++
			}
		}
		skeys := make([]string, 0, len(sub))
		for k := range sub {
			skeys = append(skeys, k)
		}
		sort.Strings(skeys)
		for _, k := range skeys {
			fmt.Printf("  %-50s %d\n", k, sub[k])
		}
		fmt.Printf("  extensions: %v\n", exts)
	}

	// 抽样打印被动对象定义文件内容
	fmt.Println("\n== sample passiveobject files ==")
	shown := 0
	for _, f := range archive.Files() {
		p := strings.ReplaceAll(strings.ToLower(f.Path), "\\", "/")
		if !strings.HasPrefix(p, "passiveobject/") || !strings.HasSuffix(p, ".obj") {
			continue
		}
		text, err := archive.ReadText(f.Path)
		if err != nil {
			fmt.Printf("  %s read error: %v\n", f.Path, err)
			continue
		}
		fmt.Printf("----- %s (%d bytes) -----\n", f.Path, len(text))
		lines := strings.Split(text, "\n")
		limit := 60
		if len(lines) < limit {
			limit = len(lines)
		}
		for _, line := range lines[:limit] {
			fmt.Printf("  %s\n", strings.TrimRight(line, "\r"))
		}
		shown++
		if shown >= 4 {
			break
		}
	}

	// 是否有 passiveobject lst
	fmt.Println("\n== passiveobject lst candidates ==")
	for _, f := range archive.Files() {
		p := strings.ReplaceAll(strings.ToLower(f.Path), "\\", "/")
		if strings.Contains(p, "passiveobject") && (strings.HasSuffix(p, ".lst") || strings.HasSuffix(p, ".etc")) {
			fmt.Printf("  %s (%d bytes)\n", f.Path, f.Size)
		}
	}
}
