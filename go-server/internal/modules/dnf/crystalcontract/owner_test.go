package crystalcontract

import (
	"context"
	"errors"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/premium"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func newOwnerTestRuntime(
	t *testing.T,
	active bool,
	slots map[string]dnfrepo.ItemStack,
) (*Owner, dnfrepo.Group, time.Time) {
	t.Helper()
	repositories := dnfrepomemory.NewMemoryGroup()
	now := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	account := dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  make(map[string]string),
	}
	if active {
		premium.Upsert(&account, premium.TypeCrystal, int64(time.Hour/time.Second), 1, now)
	}
	ctx := context.Background()
	if err := repositories.Account.Save(ctx, account); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   account.AccountID,
		Stats:       make(map[string]int64),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: account.AccountID,
		Slots:     slots,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       make(map[string]dnfrepo.ItemStack),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries:     make(map[string]dnfrepo.EquipmentEntry),
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewCatalog([SelectionCount]int64{3033, 3034, 3035, 3036, 3037, 3262})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return owner, repositories, now
}

func testCrystalSlot(selection int8) string {
	return dnfrepo.AccountSharedInventorySlotKey(dnfrepo.CrystalWarehouseFirstSlot + int16(selection))
}

func TestSelectRequiresActiveContractAndOwnedCube(t *testing.T) {
	owner, _, now := newOwnerTestRuntime(t, false, map[string]dnfrepo.ItemStack{
		testCrystalSlot(2): {ItemID: 3035, Count: 2},
	})
	if _, err := owner.Select(context.Background(), "account-1", "19", 2, now); !errors.Is(err, ErrContractInactive) {
		t.Fatalf("inactive selection error = %v, want %v", err, ErrContractInactive)
	}

	owner, _, now = newOwnerTestRuntime(t, true, map[string]dnfrepo.ItemStack{})
	if _, err := owner.Select(context.Background(), "account-1", "19", 2, now); !errors.Is(err, ErrCubeUnavailable) {
		t.Fatalf("missing cube selection error = %v, want %v", err, ErrCubeUnavailable)
	}
}

func TestSelectPersistsRuntimePVFSelectionAndState(t *testing.T) {
	owner, repositories, now := newOwnerTestRuntime(t, true, map[string]dnfrepo.ItemStack{
		testCrystalSlot(4): {ItemID: 3037, Count: 12},
	})
	state, err := owner.Select(context.Background(), "account-1", "19", 4, now)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Active || state.Selection != 4 || state.CubeItemID != 3037 {
		t.Fatalf("selected state = %+v", state)
	}
	loaded, err := owner.State(context.Background(), "account-1", "19", now)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != state {
		t.Fatalf("loaded state = %+v, want %+v", loaded, state)
	}
	character, found, err := repositories.Character.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if got := storedSelection(character); got != 4 {
		t.Fatalf("stored selection = %d, want 4", got)
	}
}

func TestConsumeAtomicallyDecrementsSelectedCubeAndClearsLastSelection(t *testing.T) {
	owner, repositories, now := newOwnerTestRuntime(t, true, map[string]dnfrepo.ItemStack{
		testCrystalSlot(0): {ItemID: 3033, Count: 1},
	})
	if _, err := owner.Select(context.Background(), "account-1", "19", 0, now); err != nil {
		t.Fatal(err)
	}
	result, err := owner.Consume(context.Background(), "account-1", "19", 354, 3033, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.ItemID != 3033 || result.Consumed != 1 || result.Remaining != 0 ||
		result.SelectionAfter != SelectionNone {
		t.Fatalf("consume result = %+v", result)
	}
	inventory, found, err := repositories.AccountInventory.Load(context.Background(), "account-1")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, exists := inventory.Slots[testCrystalSlot(0)]; exists {
		t.Fatalf("last cube stack was not deleted: %+v", inventory.Slots)
	}
	character, found, err := repositories.Character.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if got := storedSelection(character); got != SelectionNone {
		t.Fatalf("stored selection = %d, want none", got)
	}
}

func TestConsumeKeepsSelectionWhileSelectedCubeStackRemains(t *testing.T) {
	owner, repositories, now := newOwnerTestRuntime(t, true, map[string]dnfrepo.ItemStack{
		testCrystalSlot(5): {ItemID: 3262, Count: 3, RawEntry: make([]byte, 10), Extra: map[string]string{"count": "3"}},
	})
	if _, err := owner.Select(context.Background(), "account-1", "19", 5, now); err != nil {
		t.Fatal(err)
	}
	result, err := owner.Consume(context.Background(), "account-1", "19", 359, 3262, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectionAfter != 5 {
		t.Fatalf("selection after = %d, want 5", result.SelectionAfter)
	}
	accountInventory, found, err := repositories.AccountInventory.Load(context.Background(), "account-1")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%t err=%v", found, err)
	}
	stack := accountInventory.Slots[testCrystalSlot(5)]
	if stack.Count != 2 || stack.Extra["count"] != "2" {
		t.Fatalf("remaining stack = %+v, want count 2", stack)
	}
}

func TestConsumeRejectsCubeThatDoesNotMatchPersistedSelection(t *testing.T) {
	owner, repositories, now := newOwnerTestRuntime(t, true, map[string]dnfrepo.ItemStack{
		testCrystalSlot(0): {ItemID: 3033, Count: 4},
		testCrystalSlot(1): {ItemID: 3034, Count: 4},
	})
	if _, err := owner.Select(context.Background(), "account-1", "19", 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Consume(context.Background(), "account-1", "19", 355, 3034, now); !errors.Is(err, ErrCubeRequestMismatch) {
		t.Fatalf("mismatched consume error = %v, want %v", err, ErrCubeRequestMismatch)
	}
	inventory, found, err := repositories.AccountInventory.Load(context.Background(), "account-1")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if got := inventory.Slots[testCrystalSlot(1)].Count; got != 4 {
		t.Fatalf("mismatched cube count = %d, want 4", got)
	}
}

func TestExpiredContractStateSuppressesStoredSelection(t *testing.T) {
	owner, _, now := newOwnerTestRuntime(t, true, map[string]dnfrepo.ItemStack{
		testCrystalSlot(3): {ItemID: 3036, Count: 4},
	})
	if _, err := owner.Select(context.Background(), "account-1", "19", 3, now); err != nil {
		t.Fatal(err)
	}
	state, err := owner.State(context.Background(), "account-1", "19", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if state.Active || state.Selection != SelectionNone || state.CubeItemID != 0 {
		t.Fatalf("expired state = %+v", state)
	}
}

func TestSelectDoesNotAcceptCubeFromCharacterMainBag(t *testing.T) {
	owner, repositories, now := newOwnerTestRuntime(t, true, nil)
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:8": {ItemID: 3037, Count: 12},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Select(context.Background(), "account-1", "19", 4, now); !errors.Is(err, ErrCubeUnavailable) {
		t.Fatalf("main-bag-only selection error = %v, want %v", err, ErrCubeUnavailable)
	}
}
