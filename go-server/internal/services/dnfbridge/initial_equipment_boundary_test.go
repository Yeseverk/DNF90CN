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

func TestInitialEquipmentInfrastructureStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "initial_equipment.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed initial_equipment.go must stay removed: %v", err)
	}
	expected := map[string]string{
		"initialCharacterEquipment":                "initial_equipment_catalog.go",
		"readInitialPVFText":                       "initial_equipment_catalog.go",
		"parseInitialCharacterEquipmentFromSource": "initial_equipment_plan.go",
		"initialCharacterPVFPath":                  "initial_equipment_plan.go",
		"initialEquipmentMetadata":                 "initial_equipment_metadata.go",
		"parseInitialEquipmentModelLayers":         "initial_equipment_metadata.go",
		"initialEquipmentRecord":                   "initial_equipment_storage_projection.go",
		"equipmentRosterSummary":                   "initial_equipment_roster_projection.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range initialEquipmentProductionFiles() {
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

func TestInitialEquipmentBridgeHasNoDirectWritesOrPrivateTimers(t *testing.T) {
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
	for _, name := range initialEquipmentProductionFiles() {
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
	assertInitialEquipmentFileContains(
		t,
		root,
		"character_create_owner.go",
		"initialEquipmentRecord",
		"dnfcharacterdata.NewCreator",
		".Create(",
	)
}

func assertInitialEquipmentFileContains(t *testing.T, root, name string, required ...string) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	text := string(source)
	for _, value := range required {
		if !strings.Contains(text, value) {
			t.Errorf("%s must contain %q", name, value)
		}
	}
}

func initialEquipmentProductionFiles() []string {
	return []string{
		"initial_equipment_catalog.go",
		"initial_equipment_plan.go",
		"initial_equipment_metadata.go",
		"initial_equipment_storage_projection.go",
		"initial_equipment_roster_projection.go",
	}
}
