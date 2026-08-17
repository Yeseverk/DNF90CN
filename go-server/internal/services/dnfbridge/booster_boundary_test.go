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

func TestBoosterGameplayStaysInIndependentFiles(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "booster_item.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed booster_item.go must stay removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "booster_owner.go")); !os.IsNotExist(err) {
		t.Fatalf("thin booster_owner.go must stay merged into gameplay module: %v", err)
	}

	expected := map[string]string{
		"handleCurrentBoosterItem":        "gameplay_module_booster.go",
		"commitCurrentBooster":            "gameplay_module_booster.go",
		"prepareCurrentBooster":           "gameplay_module_booster.go",
		"parseCurrentBoosterOpenRequest":  "booster_protocol.go",
		"resolveCurrentBoosterDefinition": "booster_pvf.go",
		"grantCurrentBoosterReward":       "booster_projection.go",
	}
	files := []string{
		"gameplay_module_booster.go",
		"booster_protocol.go",
		"booster_pvf.go",
		"booster_projection.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, name), nil, 0)
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

func TestBoosterBridgeHasNoDirectRepositoryWrites(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	files := []string{
		"gameplay_module_booster.go",
		"booster_protocol.go",
		"booster_pvf.go",
		"booster_projection.go",
	}
	banned := []string{
		"WithinAccount",
		"WithinCharacter",
		"SaveInventoryFields(",
		"SaveEquipmentFields(",
		".Save(",
		"time.After(",
		"time.NewTimer(",
		"time.NewTicker(",
	}
	for _, name := range files {
		source, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(source)
		for _, token := range banned {
			if strings.Contains(text, token) {
				t.Errorf("%s contains direct repository mutation %q", name, token)
			}
		}
	}
	moduleSource, err := os.ReadFile(filepath.Join(root, "gameplay_module_booster.go"))
	if err != nil {
		t.Fatal(err)
	}
	moduleText := string(moduleSource)
	for _, delegation := range []string{
		"internal/modules/dnf/booster",
		"owner.Open",
	} {
		if !strings.Contains(moduleText, delegation) {
			t.Errorf("gameplay_module_booster.go does not delegate through %s", delegation)
		}
	}
}
