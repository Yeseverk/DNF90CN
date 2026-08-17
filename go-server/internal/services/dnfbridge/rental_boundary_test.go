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

func TestRentalGameplayStaysInIndependentFiles(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "rental_asset.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed rental_asset.go must stay removed: %v", err)
	}

	expected := map[string]string{
		"handleCurrentRentEquipment":                   "gameplay_module_rental.go",
		"handleCurrentChargeRentalPoint":               "gameplay_module_rental.go",
		"decodeCurrentRentEquipmentRequest":            "rental_protocol.go",
		"buildCurrentRentalPointStateBody":             "rental_protocol.go",
		"sendSelectedCurrentRentalPointState":          "rental_state.go",
		"currentRentalActiveEntries":                   "rental_state.go",
		"rentCurrentEquipment":                         "rental_asset_projection.go",
		"purchaseCurrentRentalPoints":                  "rental_asset_projection.go",
		"cleanupExpiredCurrentRentalEquipment":         "rental_asset_projection.go",
		"currentRentalAssetOwner":                      "rental_owner.go",
		"buildCurrentItemUpdateBody":                   "current_item_update_protocol.go",
		"currentRentalSelectedCharacter":               "rental_state.go",
		"sendSelectedCurrentRentalInventoryItemUpdate": "rental_state.go",
	}
	files := []string{
		"gameplay_module_rental.go",
		"rental_protocol.go",
		"rental_state.go",
		"rental_catalog.go",
		"rental_asset_projection.go",
		"rental_owner.go",
		"current_item_update_protocol.go",
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

func TestRentalBridgeHasNoDirectRepositoryWritesOrPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	files := []string{
		"gameplay_module_rental.go",
		"rental_protocol.go",
		"rental_state.go",
		"rental_catalog.go",
		"rental_asset_projection.go",
		"rental_owner.go",
	}
	banned := []string{
		"WithinRental",
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

	ownerSource, err := os.ReadFile(filepath.Join(root, "rental_owner.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ownerSource), "internal/modules/dnf/rental") {
		t.Error("rental_owner.go does not use the rental domain owner")
	}
	projectionSource, err := os.ReadFile(filepath.Join(root, "rental_asset_projection.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, delegation := range []string{"owner.Rent", "owner.Charge", "owner.Cleanup"} {
		if !strings.Contains(string(projectionSource), delegation) {
			t.Errorf("rental projection does not delegate through %s", delegation)
		}
	}
}
