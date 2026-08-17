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

func TestInitialTownRouteStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "initial_town_route.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed initial_town_route.go must stay removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "initial_town_actor_scene_snapshot.go")); !os.IsNotExist(err) {
		t.Fatalf("thin initial_town_actor_scene_snapshot.go must stay merged into route packets: %v", err)
	}
	expected := map[string]string{
		"isCurrentChannelReconnectLifecycleBody":                   "initial_town_route_state.go",
		"sendCurrentChannelReconnectTownEntry":                     "initial_town_reconnect.go",
		"currentSceneActorReadyForState":                           "initial_town_scene_ready.go",
		"sendCurrentTownPlayerStateLocked":                         "initial_town_player_state.go",
		"sendCurrentInitialTownRoute":                              "initial_town_route_progress.go",
		"buildCurrentTownActorSceneSnapshotBody":                   "initial_town_route_packets.go",
		"sendCurrentInitialTownActorRoutePacketsWithOptionsLocked": "initial_town_route_packets.go",
		"currentPersistedTownTransition":                           "initial_town_transition.go",
		"currentCharacterListLoginTransition":                      "initial_town_login_owner.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range initialTownRouteProductionFiles() {
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

func TestInitialTownRouteBridgeHasNoDirectRepositoryWritesOrPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	banned := []string{
		"WithinCharacter",
		"SaveCharacterFields(",
		".Save(",
		"time.After(",
		"time.AfterFunc(",
		"time.NewTimer(",
		"time.NewTicker(",
		"time.Sleep(",
	}
	for _, name := range initialTownRouteProductionFiles() {
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

	ownerSource, err := os.ReadFile(filepath.Join(root, "initial_town_login_owner.go"))
	if err != nil {
		t.Fatal(err)
	}
	ownerText := string(ownerSource)
	if !strings.Contains(ownerText, "internal/modules/dnf/town") ||
		!strings.Contains(ownerText, "owner.ApplyLoginLocation") {
		t.Error("initial town login persistence does not delegate to town.Owner")
	}

	stateSource, err := os.ReadFile(filepath.Join(root, "initial_town_route_state.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stateSource), "time.Duration(0)") {
		t.Error("initial town entry test seam must remain zero-duration")
	}
}

func initialTownRouteProductionFiles() []string {
	return []string{
		"initial_town_route_state.go",
		"initial_town_reconnect.go",
		"initial_town_scene_ready.go",
		"initial_town_player_state.go",
		"initial_town_route_progress.go",
		"initial_town_route_packets.go",
		"initial_town_transition.go",
		"initial_town_login_owner.go",
	}
}
