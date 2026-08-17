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

func TestMigratedGameplayBridgeFilesKeepRepositoryWritesInDomainOwners(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	files := map[string]string{
		"emotion_change.go":                  "internal/modules/dnf/emotion",
		"gameplay_module_clone_title.go":     "internal/modules/dnf/clonetitle",
		"aura_skin_slot.go":                  "internal/modules/dnf/auraskin",
		"item_grade_adjust.go":               "internal/modules/dnf/itemgrade",
		"gameplay_module_achievement.go":     "internal/modules/dnf/achievement",
		"title_book.go":                      "internal/modules/dnf/titlebook",
		"dungeon_card_asset.go":              "internal/modules/dnf/dungeon",
		"dungeon_death_return.go":            "internal/modules/dnf/dungeon",
		"dungeon_discard.go":                 "internal/modules/dnf/dungeon",
		"dungeon_drop_pickup.go":             "internal/modules/dnf/dungeon",
		"dungeon_lucky_star.go":              "internal/modules/dnf/dungeon",
		"dungeon_pickup_asset.go":            "internal/modules/dnf/dungeon",
		"dungeon_quest_drop.go":              "internal/modules/dnf/dungeon",
		"dungeon_settlement_reward_owner.go": "internal/modules/dnf/dungeon",
		"dungeon_tutorial_finish.go":         "internal/modules/dnf/dungeon",
		"dungeon_tutorial_reward.go":         "internal/modules/dnf/dungeon",
	}
	banned := []string{
		"WithinCharacter",
		"SaveCharacterFields(",
		"SaveInventoryFields(",
		"SaveEquipmentFields(",
		".Save(ctx",
	}
	for name, ownerImport := range files {
		source, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(source)
		if !strings.Contains(text, ownerImport) {
			t.Errorf("%s does not delegate to %s", name, ownerImport)
		}
		for _, token := range banned {
			if strings.Contains(text, token) {
				t.Errorf("%s contains direct repository mutation %q", name, token)
			}
		}
	}
}

func TestDungeonBridgeFilesHaveNoDirectRepositoryWrites(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	matches, err := filepath.Glob(filepath.Join(root, "dungeon*.go"))
	if err != nil {
		t.Fatalf("glob dungeon bridge files: %v", err)
	}
	banned := []string{
		"WithinCharacter",
		"WithinAccount",
		"SaveCharacterFields(",
		"SaveInventoryFields(",
		"SaveEquipmentFields(",
		"SaveSkillFields(",
		".Save(",
	}
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(path), err)
		}
		for _, token := range banned {
			if strings.Contains(string(source), token) {
				t.Errorf("%s contains direct repository mutation %q", filepath.Base(path), token)
			}
		}
	}
}

func TestDungeonRuntimeGameplayHandlersStayInOwnedFiles(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "dungeon_runtime.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed dungeon_runtime.go must stay removed: %v", err)
	}

	expected := map[string]string{
		"handleDungeonSelectUpper":               "dungeon_navigation_runtime.go",
		"replayDungeonSelectAckForActiveRuntime": "dungeon_navigation_runtime.go",
		"handleDungeonMoveMap":                   "dungeon_navigation_runtime.go",
		"handleDungeonMoveRequestLocked":         "dungeon_navigation_runtime.go",
		"handleDungeonMonsterDeath":              "dungeon_combat_runtime.go",
	}
	files := []string{
		"dungeon_navigation_runtime.go",
		"dungeon_combat_runtime.go",
		"dungeon_runtime_state.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range files {
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

	delegations := map[string][]string{
		"dungeon_navigation_runtime.go": {"dnfdungeon.SameSelectRequest"},
		"dungeon_runtime_state.go": {
			"dnfdungeon.RuntimeOwnsCharacter",
			"dnfdungeon.ValidateEntry",
		},
	}
	for name, tokens := range delegations {
		source, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, delegation := range tokens {
			if !strings.Contains(string(source), delegation) {
				t.Errorf("%s does not delegate through %s", name, delegation)
			}
		}
	}
}
