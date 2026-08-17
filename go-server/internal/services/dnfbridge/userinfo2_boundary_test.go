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

func TestCurrentUserInfoInfrastructureStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "userinfo2.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed userinfo2.go must stay removed: %v", err)
	}
	expected := map[string]string{
		"buildCSharpSelectedUserInfoBody":                                    "userinfo_current_entry.go",
		"buildCurrentActorBindingMode1BodyForSelectedWithEquipmentInContext": "userinfo_current_entry.go",
		"buildCurrentUserInfoMode3InContext":                                 "userinfo_current_modes.go",
		"writeCurrentUserInfoMode1ObjectTail":                                "userinfo_current_modes.go",
		"currentMode1EquipmentObjectRows":                                    "userinfo_current_equipment_projection.go",
		"currentMode1EquipmentCreateEnabled":                                 "userinfo_current_equipment_rules.go",
		"writeCurrentMode1EquipmentCreateRow":                                "userinfo_current_equipment_wire.go",
		"buildCSharpUserInfoSubtype0":                                        "userinfo_selected_subtype0.go",
		"buildCSharpUserInfoSubtype1":                                        "userinfo_selected_subtype1.go",
		"buildCurrentUserInfoMode1StatBlob":                                  "userinfo_current_stats_wire.go",
		"realStatInt64":                                                      "userinfo_current_stats_resolve.go",
		"statU32":                                                            "userinfo_stat_values.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range currentUserInfoProductionFiles() {
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

func TestCurrentUserInfoBridgeHasNoDirectWritesOrPrivateTimers(t *testing.T) {
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
	for _, name := range currentUserInfoProductionFiles() {
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

func currentUserInfoProductionFiles() []string {
	return []string{
		"userinfo_current_entry.go",
		"userinfo_current_modes.go",
		"userinfo_current_equipment_projection.go",
		"userinfo_current_equipment_rules.go",
		"userinfo_current_equipment_wire.go",
		"userinfo_selected_subtype0.go",
		"userinfo_selected_subtype1.go",
		"userinfo_current_stats_wire.go",
		"userinfo_current_stats_resolve.go",
		"userinfo_stat_values.go",
	}
}
