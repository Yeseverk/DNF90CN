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

func TestGameSelectRequestInfrastructureStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "game_select_request.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed game_select_request.go must stay removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "game_select_name_tag_owner.go")); !os.IsNotExist(err) {
		t.Fatalf("thin game_select_name_tag_owner.go must stay removed: %v", err)
	}
	expected := map[string]string{
		"parseSelectCharacterRequest":    "game_select_request_parse.go",
		"resolveSelectedCharacter":       "game_select_resolve.go",
		"selectedCharacterForEnter":      "game_select_resolve.go",
		"sendEnterSelectDungeonState":    "game_enter_select_dungeon.go",
		"buildEnterSelectDungeonAckBody": "game_select_response.go",
		"cleanupExpiredNameTagOnSelect":  "game_select_state.go",
		"sendSelectCharacterState":       "game_select_state.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range gameSelectRequestProductionFiles() {
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

func TestGameSelectRequestBridgeHasNoDirectRepositoryWritesOrPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	banned := []string{
		"WithinCharacter",
		"SaveCharacterFields(",
		"SaveEquipmentFields(",
		".Save(",
		"time.After(",
		"time.AfterFunc(",
		"time.NewTimer(",
		"time.NewTicker(",
		"time.Sleep(",
	}
	for _, name := range gameSelectRequestProductionFiles() {
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
	ownerSource, err := os.ReadFile(filepath.Join(root, "game_select_state.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ownerSource), "internal/modules/dnf/equip") ||
		!strings.Contains(string(ownerSource), "owner.CleanupExpiredNameTag") {
		t.Error("select-time name-tag cleanup does not delegate to equip.Owner")
	}
}

func gameSelectRequestProductionFiles() []string {
	return []string{
		"game_select_request_parse.go",
		"game_select_resolve.go",
		"game_enter_select_dungeon.go",
		"game_select_response.go",
		"game_select_state.go",
	}
}
