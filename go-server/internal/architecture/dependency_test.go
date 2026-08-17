// Package architecture_test 用 AST 静态断言 AGENTS.md 的 Source Boundaries。
// 测试只解析本模块源码 import，离线可运行、结果确定：
//  1. internal/modules/dnf/** 不得 import internal/services/**（领域层不依赖传输/装配层）；
//  2. internal/services/dnfbridge 不得 import internal/platform 的数据库/仓储实现层
//     （只允许经 internal/modules/dnf/repository 的 dnfrepo 接口路径）；
//  3. internal/app/dnf90 之外不得 import internal/services/logic/dnf（组合根唯一），
//     internal/services/logic/dnf/** 自身子包不受限，既有例外必须登记在 allowedExceptions。
package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "longheng.io/server/"

// allowedExceptions 登记现状盘点发现的既有违规，格式为 文件相对路径 -> import 前缀 -> 保留原因。
// 断言规则：清单之外不得新增违规；清单内条目一旦不再被引用（stale）同样失败，
// 保证允许清单始终最小且与代码同步。新增例外必须在注释中说明原因。
var allowedExceptions = map[string]map[string]string{
	"cmd/server/doctor/main.go": {
		// doctor 是部署预检工具，需复用 logic/dnf 的配置解析与仓储装配做启动前校验（2026-07 盘点时的既有例外）。
		modulePath + "internal/services/logic/dnf": "doctor preflight reuses logic/dnf config loading and repository assembly",
	},
}

func TestSourceBoundaries(t *testing.T) {
	root := moduleRoot(t)
	matched := map[string]map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", relative, err)
			return nil
		}
		for _, imported := range file.Imports {
			pathValue, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Errorf("decode import in %s: %v", relative, err)
				continue
			}
			reason := forbiddenImport(relative, pathValue)
			if reason == "" {
				continue
			}
			if _, ok := allowedExceptions[relative][pathValue]; ok {
				if matched[relative] == nil {
					matched[relative] = map[string]bool{}
				}
				matched[relative][pathValue] = true
				continue
			}
			t.Errorf("%s imports %q: %s", relative, pathValue, reason)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for file, imports := range allowedExceptions {
		for prefix, why := range imports {
			if !matched[file][prefix] {
				t.Errorf("stale allowlist entry %s -> %q (%s): remove it or restore the import", file, prefix, why)
			}
		}
	}
}

func forbiddenImport(relative, imported string) string {
	// 规则 1：DNF 领域层不依赖传输/装配层。
	if strings.HasPrefix(relative, "internal/modules/dnf/") {
		if strings.HasPrefix(imported, modulePath+"internal/services/") {
			return "internal/modules/dnf owns domain rules and must not import internal/services"
		}
	}
	// 规则 2：bridge 只允许经 dnfrepo 接口访问持久化，不得直连平台数据库/仓储实现层。
	if strings.HasPrefix(relative, "internal/services/dnfbridge/") {
		if imported == modulePath+"internal/platform/db" || strings.HasPrefix(imported, modulePath+"internal/platform/db/") {
			return "internal/services/dnfbridge must reach persistence through internal/modules/dnf/repository interfaces, not platform db implementations"
		}
	}
	// 规则 3：组合根唯一。logic/dnf 树内部（含 game 子包）不受限，其余例外须登记 allowedExceptions。
	if !strings.HasPrefix(relative, "internal/app/dnf90/") && !strings.HasPrefix(relative, "internal/services/logic/dnf/") {
		if imported == modulePath+"internal/services/logic/dnf" || strings.HasPrefix(imported, modulePath+"internal/services/logic/dnf/") {
			return "only the internal/app/dnf90 composition root may import internal/services/logic/dnf"
		}
	}
	return ""
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
