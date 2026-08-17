//go:build ignore

// explore2.go 抽样打印 PVF 文件条目原始 Path 字段，确认扩展名与大小写形态。
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
	count := 0
	for _, f := range archive.Files() {
		p := strings.ToLower(f.Path)
		if strings.HasPrefix(p, "passiveobject") {
			fmt.Printf("idx=%d path=%q name=%q archive_path=%q size=%d data_type=%d\n",
				f.Index, f.Path, f.Name, f.ArchivePath, f.Size, f.DataType)
			count++
			if count >= 15 {
				break
			}
		}
	}
	fmt.Println("---- map lst ----")
	for _, probe := range []string{"map/map.lst", "passiveobject/passiveobject.lst", "dungeon/dungeon.lst"} {
		file, ok := archive.FindFile(probe)
		fmt.Printf("probe %q -> ok=%t file=%+v\n", probe, ok, file)
	}
}
