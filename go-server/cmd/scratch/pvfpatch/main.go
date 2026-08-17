// pvfpatch 是一次性 PVF 脚本字符串引用补丁工具（scratch 区，不进入生产路径）。
//
// 把指定 type-1 脚本中对共享字符串池某个值的引用改写为池中另一个已存在的值，
// 不扩建字符串池；引用旧值的其他脚本保持不变。默认绝不原地覆盖输入文件：
// 必须显式给出与 -pvf 不同的 -out 输出路径。
//
// 用法：
//
//	go run ./cmd/scratch/pvfpatch -pvf D:/DNF/runtime/data/dnf/Script.pvf \
//	  -file dir/test.ani -old "old value" -new "new value" \
//	  -out <输出.pvf> -json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

type fileList []string

func (l *fileList) String() string { return strings.Join(*l, ",") }
func (l *fileList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pvfpatch:", err)
		os.Exit(1)
	}
}

func run() error {
	input := flag.String("pvf", "", "input PVF path (never modified)")
	output := flag.String("out", "", "output PVF path (required, must differ from -pvf)")
	oldValue := flag.String("old", "", "existing shared string-pool value")
	newValue := flag.String("new", "", "replacement shared string-pool value (must already exist in the pool)")
	asJSON := flag.Bool("json", false, "print a JSON report to stdout")
	var files fileList
	flag.Var(&files, "file", "archive-relative type-1 script path (repeatable)")
	flag.Parse()

	if *input == "" || *output == "" || *oldValue == "" || *newValue == "" || len(files) == 0 {
		return fmt.Errorf("-pvf, -out, -old, -new and at least one -file are required")
	}
	inputAbs, err := filepath.Abs(*input)
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if strings.EqualFold(inputAbs, outputAbs) {
		return fmt.Errorf("output must differ from input; in-place patching is not supported")
	}

	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: inputAbs})
	if err != nil {
		return err
	}
	patched, report, err := archive.PatchScriptString(files, *oldValue, *newValue)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputAbs), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outputAbs, patched, 0o644); err != nil {
		return err
	}

	result := struct {
		Output  string   `json:"output"`
		Format  string   `json:"format"`
		Files   []string `json:"files"`
		Old     string   `json:"old"`
		New     string   `json:"new"`
		OutSize int      `json:"out_size"`
		platformpvf.ScriptStringPatchReport
	}{
		Output:                  outputAbs,
		Format:                  string(archive.Format()),
		Files:                   files,
		Old:                     *oldValue,
		New:                     *newValue,
		OutSize:                 len(patched),
		ScriptStringPatchReport: report,
	}
	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	fmt.Printf("patched %s -> %s (%d files, %d tokens, %d chunks, %d bytes)\n",
		inputAbs, outputAbs, report.FilesChanged, report.TokensChanged, report.ChunksChanged, len(patched))
	return nil
}
