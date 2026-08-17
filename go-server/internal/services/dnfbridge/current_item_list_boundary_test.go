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

func TestCurrentItemListInfrastructureStaysSplitByResponsibility(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	if _, err := os.Stat(filepath.Join(root, "current_item_list.go")); !os.IsNotExist(err) {
		t.Fatalf("mixed current_item_list.go must stay removed: %v", err)
	}
	expected := map[string]string{
		"sendSelectedCurrentItemListsWithRefresh":        "current_item_list_send.go",
		"sendSelectedCurrentEquipmentSlotItemUpdates":    "current_item_list_send.go",
		"buildCurrentItemListBodyForSession":             "current_item_list_session.go",
		"reconcileCurrentPVFItemExpirationsBestEffort":   "current_item_list_session.go",
		"buildCurrentAccountCargoItemListBodyForSession": "current_item_list_account.go",
		"loadCurrentItemListContainerState":              "current_item_list_container_state.go",
		"buildCurrentEquippedItemListBodyForSession":     "current_item_list_equipment_snapshot.go",
		"currentItemListEntryFromEquipment":              "current_item_list_equipment_projection.go",
		"buildCurrentItemListBody":                       "current_item_list_protocol.go",
		"currentItemListEntriesFromInventory":            "current_item_list_repository.go",
		"currentItemListEntryFromStack":                  "current_item_list_stack_projection.go",
		"currentItemListStackExpire":                     "current_item_list_entry.go",
	}
	files := currentItemListProductionFiles()
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

func TestCurrentItemListBridgeHasNoDirectRepositoryWritesOrPrivateTimers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(currentFile)
	banned := []string{
		"WithinAccount",
		"WithinCharacter",
		"SaveSettingsFields(",
		".Save(",
		"time.After(",
		"time.AfterFunc(",
		"time.NewTimer(",
		"time.NewTicker(",
	}
	for _, name := range currentItemListProductionFiles() {
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
	containerSource, err := os.ReadFile(filepath.Join(root, "current_item_list_container_state.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(containerSource), "NewContainerStateOwner") ||
		!strings.Contains(string(containerSource), "owner.Ensure") {
		t.Error("container-state initialization does not delegate persistence to inventory.ContainerStateOwner")
	}
}

func currentItemListProductionFiles() []string {
	return []string{
		"current_item_list_types.go",
		"current_item_list_send.go",
		"current_item_list_session.go",
		"current_item_list_account.go",
		"current_item_list_container_state.go",
		"current_item_list_equipment_snapshot.go",
		"current_item_list_equipment_projection.go",
		"current_item_list_protocol.go",
		"current_item_list_repository.go",
		"current_item_list_stack_projection.go",
		"current_item_list_entry.go",
	}
}
