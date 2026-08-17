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

func TestDisjointCompoundGameplaysStayInIndependentFiles(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "disjoint_compound.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed disjoint_compound.go must stay removed: %v", err)
	}

	expected := map[string]string{
		"handleCurrentDisjointItem":   "gameplay_module_equipment_disjoint.go",
		"commitCurrentDisjointItem":   "gameplay_module_equipment_disjoint.go",
		"handleCurrentAvatarDisjoint": "gameplay_module_avatar_disjoint.go",
		"commitCurrentAvatarDisjoint": "gameplay_module_avatar_disjoint.go",
		"handleCurrentEmblemCompound": "gameplay_module_emblem_compound.go",
		"commitCurrentEmblemCompound": "gameplay_module_emblem_compound.go",
		"commitCurrentDisjoint":       "disjoint_compound_owner.go",
	}
	files := []string{
		"gameplay_module_equipment_disjoint.go",
		"gameplay_module_avatar_disjoint.go",
		"gameplay_module_emblem_compound.go",
		"disjoint_compound_owner.go",
		"disjoint_compound_protocol.go",
		"disjoint_compound_projection.go",
		"disjoint_compound_pvf.go",
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

func TestDisjointCompoundBridgeHasNoDirectRepositoryWrites(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	files := []string{
		"gameplay_module_equipment_disjoint.go",
		"gameplay_module_avatar_disjoint.go",
		"gameplay_module_emblem_compound.go",
		"disjoint_compound_owner.go",
		"disjoint_compound_protocol.go",
		"disjoint_compound_projection.go",
		"disjoint_compound_pvf.go",
	}
	banned := []string{
		"WithinAccount",
		"WithinCharacter",
		"SaveInventoryFields(",
		"SaveEquipmentFields(",
		".Save(",
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

	ownerSource, err := os.ReadFile(filepath.Join(root, "disjoint_compound_owner.go"))
	if err != nil {
		t.Fatal(err)
	}
	ownerText := string(ownerSource)
	for _, delegation := range []string{
		"internal/modules/dnf/disjoint",
		"owner.DisjointEquipment",
		"owner.DisjointAvatar",
	} {
		if !strings.Contains(ownerText, delegation) {
			t.Errorf("disjoint_compound_owner.go does not delegate through %s", delegation)
		}
	}
	emblemSource, err := os.ReadFile(filepath.Join(root, "gameplay_module_emblem_compound.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(emblemSource), "owner.CompoundEmblems") {
		t.Error("emblem compound gameplay does not delegate through the domain owner")
	}
}
