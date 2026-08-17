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

func TestSocketEmblemGameplaysStayInIndependentFiles(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "inventory_socket_emblem.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed inventory_socket_emblem.go must stay removed: %v", err)
	}

	expected := map[string]string{
		"handleCurrentEquipmentSocketOpen":   "gameplay_module_equipment_socket.go",
		"commitCurrentEquipmentSocketOpen":   "gameplay_module_equipment_socket.go",
		"handleCurrentNoBody796":             "gameplay_module_equipment_emblem_control.go",
		"commitCurrentEquipmentEmblemAttach": "gameplay_module_equipment_emblem_control.go",
		"handleCurrentAvatarSocketOpen":      "gameplay_module_avatar_socket.go",
		"commitCurrentAvatarSocketOpen":      "gameplay_module_avatar_socket.go",
		"handleCurrentAvatarEmblemAttach":    "gameplay_module_avatar_emblem.go",
		"commitCurrentAvatarEmblemAttach":    "gameplay_module_avatar_emblem.go",
	}
	files := []string{
		"gameplay_module_equipment_socket.go",
		"gameplay_module_equipment_emblem_control.go",
		"gameplay_module_avatar_socket.go",
		"gameplay_module_avatar_emblem.go",
		"socket_emblem_owner.go",
		"socket_emblem_protocol.go",
		"socket_emblem_pvf.go",
		"socket_emblem_projection.go",
		"socket_emblem_refresh.go",
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

func TestSocketEmblemBridgeHasNoDirectRepositoryWrites(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	files := []string{
		"gameplay_module_equipment_socket.go",
		"gameplay_module_equipment_emblem_control.go",
		"gameplay_module_avatar_socket.go",
		"gameplay_module_avatar_emblem.go",
		"socket_emblem_owner.go",
		"socket_emblem_protocol.go",
		"socket_emblem_pvf.go",
		"socket_emblem_projection.go",
		"socket_emblem_refresh.go",
	}
	banned := []string{
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
	for _, name := range files[:4] {
		source, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(source), "internal/modules/dnf/socketemblem") {
			t.Errorf("%s does not delegate to the socket/emblem owner", name)
		}
	}
}
