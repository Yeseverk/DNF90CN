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

func TestItemExpirationInfrastructureStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "item_expiration.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed item_expiration.go must stay removed: %v", err)
	}

	expected := map[string]string{
		"currentPVFItemCatalog":                               "item_expiration_catalog.go",
		"currentPVFItemDefinitionForGrantAt":                  "item_expiration_rules.go",
		"currentPVFItemDefinitionForNestedRewardGrantAt":      "item_expiration_rules.go",
		"applyCurrentPVFItemExpirationAt":                     "item_expiration_projection.go",
		"cleanupCurrentPVFWrongExpirationProjection":          "item_expiration_projection.go",
		"reconcileCurrentPVFItemExpirationsWithCatalog":       "item_expiration_owner.go",
		"projectCurrentPVFItemExpirations":                    "item_expiration_owner.go",
		"applyCurrentPVFUsePeriodsToEntriesWithLoadedCatalog": "item_expiration_wire.go",
	}
	files := []string{
		"item_expiration_catalog.go",
		"item_expiration_rules.go",
		"item_expiration_projection.go",
		"item_expiration_owner.go",
		"item_expiration_wire.go",
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

func TestItemExpirationBridgeHasNoDirectRepositoryWritesOrPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	files := []string{
		"item_expiration_catalog.go",
		"item_expiration_rules.go",
		"item_expiration_projection.go",
		"item_expiration_owner.go",
		"item_expiration_wire.go",
	}
	banned := []string{
		"WithinAccount",
		"WithinCharacter",
		"SaveInventoryFields(",
		"SaveEquipmentFields(",
		".Save(",
		"time.After(",
		"time.AfterFunc(",
		"time.NewTimer(",
		"time.NewTicker(",
	}
	for _, name := range files {
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
	ownerSource, err := os.ReadFile(filepath.Join(root, "item_expiration_owner.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ownerSource), "internal/modules/dnf/itemexpiration") ||
		!strings.Contains(string(ownerSource), "owner.Reconcile") {
		t.Error("item expiration reconciliation does not delegate persistence to the domain owner")
	}
}
