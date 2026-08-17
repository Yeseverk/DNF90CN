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

func TestNPCShopGameplayStaysInIndependentFiles(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "npc_shop.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed npc_shop.go must stay removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "npc_shop_owner.go")); !os.IsNotExist(err) {
		t.Fatalf("thin npc_shop_owner.go must stay merged into projection: %v", err)
	}

	expected := map[string]string{
		"handleCurrentNPCShopBuy":                 "gameplay_module_npc_shop.go",
		"handleCurrentNPCShopPurchaseCount":       "gameplay_module_npc_shop.go",
		"handleCurrentNPCShopSell":                "gameplay_module_npc_shop.go",
		"commitCurrentNPCShopBuy":                 "npc_shop_projection.go",
		"commitCurrentNPCShopMaterialExchange":    "npc_shop_projection.go",
		"commitCurrentNPCShopSell":                "npc_shop_projection.go",
		"parseCurrentNPCShopBuyRequest":           "npc_shop_protocol.go",
		"parseCurrentNPCShopPurchaseCountRequest": "npc_shop_protocol.go",
		"normalizeCurrentNPCShopBuyCount":         "npc_shop_protocol.go",
		"buildCurrentNPCShopPurchaseCountBody":    "npc_shop_protocol.go",
		"newCurrentNPCShopCatalog":                "npc_shop_pvf.go",
		"currentNPCShopMutationOwner":             "npc_shop_projection.go",
	}
	files := []string{
		"gameplay_module_npc_shop.go",
		"npc_shop_protocol.go",
		"npc_shop_pvf.go",
		"npc_shop_projection.go",
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

func TestNPCShopBridgeHasNoDirectRepositoryWritesOrPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	files := []string{
		"gameplay_module_npc_shop.go",
		"npc_shop_protocol.go",
		"npc_shop_pvf.go",
		"npc_shop_projection.go",
	}
	banned := []string{
		"WithinAccount",
		"WithinCharacter",
		"SaveCharacterFields(",
		"SaveInventoryFields(",
		"SaveEquipmentFields(",
		".Save(",
		"time.After(",
		"time.NewTimer(",
		"time.NewTicker(",
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

	ownerSource, err := os.ReadFile(filepath.Join(root, "npc_shop_projection.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ownerSource), "internal/modules/dnf/npcshop") {
		t.Error("npc_shop_projection.go does not use the NPC shop domain owner")
	}
	projectionSource, err := os.ReadFile(filepath.Join(root, "npc_shop_projection.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(projectionSource), "owner.Mutate") {
		t.Error("NPC shop projection does not delegate persistence through owner.Mutate")
	}
}
