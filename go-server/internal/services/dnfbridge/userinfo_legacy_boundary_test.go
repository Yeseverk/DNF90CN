package dnfbridge

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLegacyUserInfoInfrastructureStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "userinfo_legacy.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed userinfo_legacy.go must stay removed: %v", err)
	}
	expected := map[string]string{
		"csharpLegacyUserInfoInitPackets": "userinfo_legacy_dispatch.go",
		"buildCSharpLegacyUserInfoBody":   "userinfo_legacy_dispatch.go",
		"rows":                            "userinfo_legacy_reader.go",
		"build23":                         "userinfo_legacy_body_0023.go",
		"build47":                         "userinfo_legacy_build_0047_0060.go",
		"build6A":                         "userinfo_legacy_build_006a_0098.go",
		"buildA0":                         "userinfo_legacy_build_00a0_00fe.go",
		"build109":                        "userinfo_legacy_build_0109_0184.go",
		"build1BF":                        "userinfo_legacy_build_01bf_02bc.go",
		"build2C1":                        "userinfo_legacy_build_02c1_03cd.go",
		"build22D":                        "userinfo_legacy_build_022d_029f.go",
		"buildRawFixed":                   "userinfo_legacy_raw_registry.go",
		"build374":                        "userinfo_legacy_raw_0374.go",
		"rawOne":                          "userinfo_legacy_raw_reader.go",
		"legacyGroupKey":                  "userinfo_legacy_collections.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range legacyUserInfoProductionFiles() {
		path := filepath.Join(root, name)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			wantFile, tracked := expected[function.Name.Name]
			if !tracked {
				continue
			}
			if previous, duplicate := found[function.Name.Name]; duplicate {
				t.Errorf("%s is duplicated in %s and %s", function.Name.Name, previous, name)
			}
			found[function.Name.Name] = name
			if name != wantFile {
				t.Errorf("%s owner=%s want=%s", function.Name.Name, name, wantFile)
			}
		}
	}
	for function, wantFile := range expected {
		if _, ok := found[function]; !ok {
			t.Errorf("%s is missing from %s", function, wantFile)
		}
	}
}

func TestLegacyUserInfoBridgeHasNoDirectWritesOrPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	banned := []string{
		"WithinAccount",
		"WithinCharacter",
		"SaveAccount",
		"SaveCharacter",
		"SaveEquipment",
		".Save(",
		"time.After(",
		"time.AfterFunc(",
		"time.NewTimer(",
		"time.NewTicker(",
		"time.Sleep(",
	}
	for _, name := range legacyUserInfoProductionFiles() {
		source, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(source)
		for _, forbidden := range banned {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains forbidden bridge operation %q", name, forbidden)
			}
		}
	}
}

func legacyUserInfoProductionFiles() []string {
	return []string{
		"userinfo_legacy_dispatch.go",
		"userinfo_legacy_reader.go",
		"userinfo_legacy_body_0023.go",
		"userinfo_legacy_build_0047_0060.go",
		"userinfo_legacy_build_006a_0098.go",
		"userinfo_legacy_build_00a0_00fe.go",
		"userinfo_legacy_build_0109_0184.go",
		"userinfo_legacy_build_01bf_02bc.go",
		"userinfo_legacy_build_02c1_03cd.go",
		"userinfo_legacy_build_022d_029f.go",
		"userinfo_legacy_raw_registry.go",
		"userinfo_legacy_raw_0374.go",
		"userinfo_legacy_raw_reader.go",
		"userinfo_legacy_collections.go",
	}
}
