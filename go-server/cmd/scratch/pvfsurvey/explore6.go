//go:build ignore

// explore6.go 验证 .act 动作脚本的内容形态（相对 .obj 目录解析引用）。
package main

import (
	"fmt"
	"path"
	"strings"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func main() {
	archive, err := platformpvf.Open(`D:/DNF/runtime/data/dnf/Script.pvf`)
	if err != nil {
		panic(err)
	}
	// object 69292 = ActionObject/Monster/Cataclysm/questdummy/dummy2.obj, basic action = Action/dummy2.act
	candidates := []string{
		"passiveobject/ActionObject/Monster/Cataclysm/questdummy/Action/dummy2.act",
		"passiveobject/MapObject/BreakableObject/Action/Barrel.act",
		"passiveobject/MapObject/Trap/Action/MinePressure.act",
	}
	for _, p := range candidates {
		text, err := archive.ReadText(p)
		if err != nil {
			fmt.Printf("----- %s MISS: %v\n", p, err)
			continue
		}
		fmt.Printf("----- %s (%d bytes) -----\n", p, len(text))
		lines := strings.Split(text, "\n")
		n := 90
		if len(lines) < n {
			n = len(lines)
		}
		for _, line := range lines[:n] {
			fmt.Printf("  %s\n", strings.TrimRight(line, "\r"))
		}
	}
	_ = path.Dir
}
