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

func TestGameSelectSceneInfrastructureStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "game_select_scene.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed game_select_scene.go must stay removed: %v", err)
	}
	expected := map[string]string{
		"sendUpperCSharpSelectInit":                          "game_select_scene_entry.go",
		"sendCSharpSelectInitPacket":                         "game_select_scene_transport.go",
		"buildCurrentSceneObjectListBodyForSessionInContext": "game_select_scene_object_projection.go",
		"currentSceneObjectRawStateForLog":                   "game_select_scene_object_projection.go",
		"sendDeferredSelectSceneTail":                        "game_select_scene_deferred_tail.go",
		"sendSelectedSceneUserInfoMode3RefreshOnce":          "game_select_scene_userinfo.go",
		"sendRuntimeSceneReadySequence":                      "game_select_scene_runtime_gates.go",
		"sendFinishLoadingStatus":                            "game_select_scene_request_handlers.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range gameSelectSceneProductionFiles() {
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

func TestGameSelectSceneBridgeHasNoDirectWritesOrPrivateTimers(t *testing.T) {
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
	for _, name := range gameSelectSceneProductionFiles() {
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

func gameSelectSceneProductionFiles() []string {
	return []string{
		"game_select_scene_entry.go",
		"game_select_scene_transport.go",
		"game_select_scene_object_projection.go",
		"game_select_scene_deferred_tail.go",
		"game_select_scene_userinfo.go",
		"game_select_scene_runtime_gates.go",
		"game_select_scene_request_handlers.go",
	}
}
