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

func TestSceneSkillProjectionStaysInOwnedFiles(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "scene_skill_owner.go")); !os.IsNotExist(err) {
		t.Fatalf("thin scene_skill_owner.go must stay removed: %v", err)
	}
	expected := map[string]string{
		"sendCurrentSceneSkillInfo":             "scene_skill_info.go",
		"loadOrBackfillCurrentSceneSkillRecord": "scene_skill_mutation.go",
		"syncCurrentSceneSkillPointLedger":      "scene_skill_mutation.go",
		"ensureCurrentSceneSkillLayout":         "scene_skill_mutation.go",
		"skillRecordMatchesPVFInitialSkills":    "scene_skill_mutation.go",
		"buildCurrentSceneSkillInfoBody":        "scene_skill_protocol.go",
		"appendProtoVarint":                     "scene_skill_protocol.go",
		"currentSceneSkillOwner":                "scene_skill_mutation.go",
		"syncCurrentSkillPointState":            "scene_skill_mutation.go",
	}
	files := []string{
		"scene_skill_info.go",
		"scene_skill_mutation.go",
		"scene_skill_protocol.go",
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

func TestSceneSkillBridgeHasNoDirectRepositoryWritesOrPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	files := []string{
		"scene_skill_info.go",
		"scene_skill_mutation.go",
		"scene_skill_protocol.go",
	}
	banned := []string{
		"WithinCharacter",
		"SaveSkillFields(",
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

	ownerSource, err := os.ReadFile(filepath.Join(root, "scene_skill_mutation.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ownerSource), "internal/modules/dnf/skill") {
		t.Error("scene_skill_mutation.go does not use the skill domain owner")
	}
	mutationSource, err := os.ReadFile(filepath.Join(root, "scene_skill_mutation.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, delegation := range []string{"owner.Backfill", "owner.SyncPoints", "owner.EnsureLayout"} {
		if !strings.Contains(string(mutationSource), delegation) {
			t.Errorf("scene skill mutation does not delegate through %s", delegation)
		}
	}
}
