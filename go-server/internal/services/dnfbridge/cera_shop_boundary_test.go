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

func TestCeraShopGameplayFilesKeepOwnedBoundaries(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "cera_shop.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed cera_shop.go must stay removed: %v", err)
	}
	expected := map[string]string{
		"newPVFCeraShopCatalog":                   "cera_shop_catalog.go",
		"handleCurrentCeraShopPurchase":           "cera_shop_purchase.go",
		"commitCurrentCeraShopPurchase":           "cera_shop_purchase.go",
		"grantCurrentCeraShopProduct":             "cera_shop_grant.go",
		"currentCeraShopPrepareContainerUpgrades": "cera_shop_grant.go",
		"buildCurrentCeraShopPurchaseSuccessBody": "cera_shop_protocol.go",
		"buildCurrentCeraShopPurchaseFailureBody": "cera_shop_protocol.go",
		"buildCurrentCeraShopBalanceBody":         "cera_shop_protocol.go",
	}
	files := []string{
		"cera_shop_catalog.go",
		"cera_shop_purchase.go",
		"cera_shop_grant.go",
		"cera_shop_protocol.go",
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

	purchaseSource, err := os.ReadFile(filepath.Join(root, "cera_shop_purchase.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"dnfcerashop.NewOwner", "checkoutOwner.Checkout"} {
		if !strings.Contains(string(purchaseSource), token) {
			t.Errorf("cera_shop_purchase.go does not delegate through %s", token)
		}
	}
}

func TestCeraShopBridgeFilesHaveNoDirectRepositoryWrites(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	paths, err := filepath.Glob(filepath.Join(root, "cera_shop_*.go"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, filepath.Join(root, "name_tag_card.go"))
	banned := []string{
		"WithinCharacter",
		"WithinAccount",
		"WithinRental",
		"SaveCharacterFields(",
		"SaveInventoryFields(",
		"SaveEquipmentFields(",
		"SaveSettingsFields(",
		".Save(",
	}
	for _, path := range paths {
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
