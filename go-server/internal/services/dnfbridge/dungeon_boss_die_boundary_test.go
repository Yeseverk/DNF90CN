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

func TestDungeonBossDieCheckStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "dungeon_boss_die_check.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed dungeon_boss_die_check.go must stay removed: %v", err)
	}

	expected := map[string]string{
		"handleDungeonBossDieCheck":                               "dungeon_boss_die_state.go",
		"handleDungeonBossDieCheckLocked":                         "dungeon_boss_die_state.go",
		"completeValidatedDungeonBossDieCheckLocked":              "dungeon_boss_die_clear_decision.go",
		"completeCurrentDungeonBossDieCheckLocked":                "dungeon_boss_die_settlement.go",
		"completeCurrentDungeonOrdinaryFinalRoomAfterDeathLocked": "dungeon_boss_die_ordinary.go",
		"currentDungeonAnnouncedActor":                            "dungeon_boss_die_actor.go",
		"buildCurrentDungeonBossDieCheckResponse":                 "dungeon_boss_die_state.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range dungeonBossDieCheckProductionFiles() {
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

func TestDungeonBossDieCheckHasNoDirectWritesOrPrivateTimers(t *testing.T) {
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
		"SaveInventory",
		"SaveQuest",
		".Save(",
		"time.After(",
		"time.AfterFunc(",
		"time.NewTimer(",
		"time.NewTicker(",
		"time.Sleep(",
	}
	for _, name := range dungeonBossDieCheckProductionFiles() {
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

	module, err := os.ReadFile(filepath.Join(root, "gameplay_module_dungeon_combat_drop.go"))
	if err != nil {
		t.Fatalf("read dungeon gameplay module: %v", err)
	}
	moduleText := string(module)
	for _, required := range []string{`Name: "dungeon-combat-drop"`, "bossCheckOpcode", "handleDungeonBossDieCheck"} {
		if !strings.Contains(moduleText, required) {
			t.Errorf("dungeon gameplay module must contain %q", required)
		}
	}
}

func dungeonBossDieCheckProductionFiles() []string {
	return []string{
		"dungeon_boss_die_state.go",
		"dungeon_boss_die_clear_decision.go",
		"dungeon_boss_die_settlement.go",
		"dungeon_boss_die_ordinary.go",
		"dungeon_boss_die_actor.go",
	}
}
