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

func TestGuardianGemGameplayStaysInIndependentFiles(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "guild_medal.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed guild_medal.go must stay removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "guardian_gem_owner.go")); !os.IsNotExist(err) {
		t.Fatalf("thin guardian_gem_owner.go must stay removed: %v", err)
	}

	expected := map[string]string{
		"handleCurrentGuardianGemUse":             "gameplay_module_guardian_gem.go",
		"commitCurrentGuardianGemUse":             "gameplay_module_guardian_gem.go",
		"currentGuardianGemMutationContext":       "gameplay_module_guardian_gem.go",
		"decodeCurrentGuardianGemUseRequest":      "guardian_gem_protocol.go",
		"resolveCurrentGuardianGem":               "guardian_gem_pvf.go",
		"resolveCurrentGuardianGemMedal":          "guardian_gem_pvf.go",
		"currentGuardianGemFindTarget":            "guardian_gem_projection.go",
		"currentGuardianGemWriteRawSocket":        "guardian_gem_projection.go",
		"sendCurrentGuardianGemMutationRefresh":   "guardian_gem_refresh.go",
		"sendSelectedGuardianGemWornMedalRefresh": "guardian_gem_refresh.go",
		"currentGuardianGemOwner":                 "gameplay_module_guardian_gem.go",
	}
	files := []string{
		"gameplay_module_guardian_gem.go",
		"guardian_gem_protocol.go",
		"guardian_gem_pvf.go",
		"guardian_gem_projection.go",
		"guardian_gem_refresh.go",
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

func TestGuardianGemBridgeHasNoDirectRepositoryWritesOrPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	files := []string{
		"gameplay_module_guardian_gem.go",
		"guardian_gem_protocol.go",
		"guardian_gem_pvf.go",
		"guardian_gem_projection.go",
		"guardian_gem_refresh.go",
	}
	banned := []string{
		"WithinCharacter",
		"SaveInventoryFields(",
		"SaveEquipmentFields(",
		".Save(",
		"time.After(",
		"time.AfterFunc(",
		"time.NewTimer(",
		"time.NewTicker(",
		"time.Sleep(",
	}
	for _, name := range files {
		source, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(source)
		for _, token := range banned {
			if strings.Contains(text, token) {
				t.Errorf("%s contains forbidden bridge operation %q", name, token)
			}
		}
	}

	ownerSource, err := os.ReadFile(filepath.Join(root, "gameplay_module_guardian_gem.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ownerSource), "internal/modules/dnf/guardiangem") {
		t.Error("gameplay_module_guardian_gem.go does not use the guardian-gem domain owner")
	}
	gameplaySource, err := os.ReadFile(filepath.Join(root, "gameplay_module_guardian_gem.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gameplaySource), "owner.Insert") {
		t.Error("guardian-gem gameplay does not delegate persistence through owner.Insert")
	}
}
