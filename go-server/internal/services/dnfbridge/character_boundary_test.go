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

func TestCharacterInfrastructureStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "character.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed character.go must stay removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "character_roster_legacy.go")); !os.IsNotExist(err) {
		t.Fatalf("thin character_roster_legacy.go must stay merged into roster body: %v", err)
	}
	expected := map[string]string{
		"handleUpperCreateCharacter":   "character_create_transport.go",
		"handleChangeCharacterSlot":    "character_slot_change.go",
		"createCharacter":              "character_create_workflow.go",
		"repositoryGroup":              "repository_access.go",
		"parseCreateCharacter":         "character_create_protocol.go",
		"listCharacters":               "character_repository.go",
		"saveNewCharacter":             "character_create_owner.go",
		"sendCharacterBootstrap":       "character_roster_send.go",
		"buildNoPackRosterBody":        "character_roster_body.go",
		"writeNoPackRosterEntry":       "character_roster_entries.go",
		"defaultCreatedCharacterStats": "character_created_defaults.go",
		"rosterNameBytes":              "character_roster_values.go",
		"buildRosterSlots":             "character_roster_body.go",
		"buildCurrentSceneObjectListBodyWithCreatureInContext": "character_scene_object.go",
		"writeCurrentSceneObjectEntryTailWithCreature":         "character_scene_object_tail.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range characterProductionFiles() {
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

func TestCharacterBridgeDelegatesWritesAndHasNoPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	banned := []string{
		"dnfrepo.SwapCharacterSlots(",
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
	for _, name := range characterProductionFiles() {
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
	assertFileContains(t, root, "character_slot_change.go", "dnfcharacterdata.NewRosterOwner", "owner.SwapSlots")
	assertFileContains(t, root, "character_create_owner.go", "dnfcharacterdata.NewCreator", ".Create(")
}

func assertFileContains(t *testing.T, root, name string, required ...string) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	text := string(source)
	for _, token := range required {
		if !strings.Contains(text, token) {
			t.Errorf("%s must contain %q", name, token)
		}
	}
}

func characterProductionFiles() []string {
	return []string{
		"character_protocol_constants.go",
		"repository_timeouts.go",
		"repository_access.go",
		"character_create_transport.go",
		"character_slot_change.go",
		"character_create_workflow.go",
		"character_create_protocol.go",
		"character_repository.go",
		"character_create_owner.go",
		"character_roster_send.go",
		"character_roster_body.go",
		"character_roster_entries.go",
		"character_created_defaults.go",
		"character_roster_values.go",
		"character_scene_object.go",
		"character_scene_object_tail.go",
	}
}
