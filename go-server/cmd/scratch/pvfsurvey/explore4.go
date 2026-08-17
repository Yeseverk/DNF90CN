//go:build ignore

// explore4.go 解析 passiveobject.lst，打印指定 objectID 的 .obj 定义全文，
// 并统计 .obj 文件中出现的 section 名分布（行为线索）。
package main

import (
	"fmt"
	"regexp"
	"strings"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

var lstEntryPattern = regexp.MustCompile("([0-9]+)\\s+`([^`]+)`")

func main() {
	archive, err := platformpvf.Open(`D:/DNF/runtime/data/dnf/Script.pvf`)
	if err != nil {
		panic(err)
	}
	lst, err := archive.ReadText("passiveobject/passiveobject.lst")
	if err != nil {
		panic(err)
	}
	objects := map[string]string{}
	for _, m := range lstEntryPattern.FindAllStringSubmatch(lst, -1) {
		objects[m[1]] = m[2]
	}
	fmt.Printf("lst entries=%d\n", len(objects))

	readObj := func(ref string) (string, string) {
		ref = strings.ReplaceAll(ref, "\\", "/")
		for _, c := range []string{ref, "passiveobject/" + ref, strings.ToLower("passiveobject/" + ref)} {
			if t, err := archive.ReadText(c); err == nil {
				return t, c
			}
		}
		return "", ""
	}

	// 电梯三件套 + 少量其它样本
	for _, id := range []string{"1111", "1112", "1113", "69292", "221", "340"} {
		ref, ok := objects[id]
		if !ok {
			fmt.Printf("===== id=%s NOT IN LST =====\n", id)
			continue
		}
		text, used := readObj(ref)
		fmt.Printf("===== id=%s ref=%q used=%q bytes=%d =====\n", id, ref, used, len(text))
		if text == "" {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			fmt.Printf("  %s\n", strings.TrimRight(line, "\r"))
		}
	}
}
