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

func TestAccountAdventureInfrastructureStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "account_adventure_info.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed account_adventure_info.go must stay removed: %v", err)
	}
	expected := map[string]string{
		"sendCurrentAdventureActorRefreshFromAccount":      "account_adventure_push.go",
		"sendCurrentSelectorAdventureInfoAfterHiddenProbe": "account_adventure_push.go",
		"persistCurrentSelectorAdventureInfoSlot":          "account_adventure_selector_owner.go",
		"buildCurrentAdventureInfoBodyWithIdentity":        "account_adventure_protocol.go",
		"currentRepresentAccountIdentity":                  "account_adventure_identity.go",
		"handleCurrentRequestAdventureInfo":                "gameplay_module_adventure.go",
	}
	found := make(map[string]string, len(expected))
	for _, name := range accountAdventureProductionFiles() {
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

func TestAccountAdventureBridgeHasNoDirectRepositoryWritesOrPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	banned := []string{
		"WithinAccount",
		"SaveAccountMetadataEntry(",
		".Save(",
		"time.After(",
		"time.AfterFunc(",
		"time.NewTimer(",
		"time.NewTicker(",
		"time.Sleep(",
	}
	for _, name := range accountAdventureProductionFiles() {
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
	ownerSource, err := os.ReadFile(filepath.Join(root, "account_adventure_selector_owner.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ownerSource), "adventuregroup.NewOwner") ||
		!strings.Contains(string(ownerSource), "owner.RememberSelectorSlot") {
		t.Error("selector-slot persistence does not delegate to adventuregroup.Owner")
	}
	moduleSource, err := os.ReadFile(filepath.Join(root, "gameplay_module_adventure.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(moduleSource), `"adventure-info"`) ||
		!strings.Contains(string(moduleSource), "handleCurrentRequestAdventureInfo") {
		t.Error("op1403 handler is not owned by the adventure-info gameplay module")
	}
}

func accountAdventureProductionFiles() []string {
	return []string{
		"account_adventure_constants.go",
		"account_adventure_push.go",
		"account_adventure_selector_owner.go",
		"account_adventure_protocol.go",
		"account_adventure_identity.go",
		"gameplay_module_adventure.go",
	}
}
