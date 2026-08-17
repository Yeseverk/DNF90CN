package itemtrade

import (
	"context"
	"errors"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerExchangeAtomicallyMovesBothOffers(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, record := range []dnfrepo.InventoryRecord{
		{CharacterID: "1", Slots: map[string]dnfrepo.ItemStack{"0:97": {ItemID: 1001, Count: 1, RawEntry: []byte{1, 2, 3}}}},
		{CharacterID: "5", Slots: map[string]dnfrepo.ItemStack{"0:57": {ItemID: 2002, Count: 3}}},
	} {
		if err := repositories.Inventory.Save(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	owner.now = func() time.Time { return time.Unix(100, 0) }

	result, err := owner.Exchange(ctx,
		Participant{CharacterID: "1", Offers: []Offer{{TradeSlot: 0, SourceList: 0, SourceSlot: 97, Count: 1, ExpectedItem: 1001}}},
		Participant{CharacterID: "5", Offers: []Offer{{TradeSlot: 0, SourceList: 0, SourceSlot: 57, Count: 2, ExpectedItem: 2002}}},
	)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	first := result.Inventories["1"]
	second := result.Inventories["5"]
	if first.Slots["0:9"].ItemID != 2002 || first.Slots["0:9"].Count != 2 {
		t.Fatalf("first received = %+v", first.Slots)
	}
	if second.Slots["0:65"].ItemID != 1001 || second.Slots["0:65"].Count != 1 || second.Slots["0:57"].Count != 1 {
		t.Fatalf("second inventory = %+v", second.Slots)
	}
	if len(result.Received["1"]) != 1 || result.Received["1"][0].TradeSlot != 0 || result.Received["1"][0].DestinationSlot != 9 {
		t.Fatalf("first transfer = %+v", result.Received["1"])
	}
}

func TestOwnerExchangeRejectsBoundItemWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "1",
		Slots:       map[string]dnfrepo.ItemStack{"0:97": {ItemID: 1001, Count: 1, Bind: true}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "5", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repositories)
	_, err := owner.Exchange(ctx,
		Participant{CharacterID: "1", Offers: []Offer{{TradeSlot: 0, SourceList: 0, SourceSlot: 97, Count: 1, ExpectedItem: 1001}}},
		Participant{CharacterID: "5"},
	)
	if !errors.Is(err, ErrItemNotTradeable) {
		t.Fatalf("Exchange() error = %v", err)
	}
	loaded, _, _ := repositories.Inventory.Load(ctx, "1")
	if loaded.Slots["0:97"].ItemID != 1001 {
		t.Fatalf("bound source mutated: %+v", loaded.Slots)
	}
}

func TestOwnerExchangeAtomicallyMovesGoldAndItem(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, record := range []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: "a", Stats: map[string]int64{"gold": 250_000}},
		{CharacterID: "5", AccountID: "b", Stats: map[string]int64{"gold": 40_000}},
	} {
		if err := repositories.Character.Save(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "5",
		Slots:       map[string]dnfrepo.ItemStack{"0:57": {ItemID: 2002, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}

	result, err := owner.Exchange(ctx,
		Participant{CharacterID: "1", Gold: 100_000},
		Participant{CharacterID: "5", Gold: 10_000, Offers: []Offer{{TradeSlot: 1, SourceList: 0, SourceSlot: 57, Count: 1, ExpectedItem: 2002}}},
	)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if got := result.Characters["1"].Stats["gold"]; got != 160_000 {
		t.Fatalf("first gold = %d", got)
	}
	if got := result.Characters["5"].Stats["gold"]; got != 130_000 {
		t.Fatalf("second gold = %d", got)
	}
	if got := result.Inventories["1"].Slots["0:9"]; got.ItemID != 2002 || got.Count != 1 {
		t.Fatalf("first received item = %+v", got)
	}
}

func TestOwnerExchangePlacesExpandedSourceSlotAtVisiblePageStart(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "1",
		Slots:       map[string]dnfrepo.ItemStack{"0:110": {ItemID: 490702514, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "7", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}

	result, err := owner.Exchange(ctx,
		Participant{CharacterID: "1", Offers: []Offer{{TradeSlot: 1, SourceList: 0, SourceSlot: 110, Count: 1, ExpectedItem: 490702514}}},
		Participant{CharacterID: "7"},
	)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if got := result.Inventories["7"].Slots["0:65"]; got.ItemID != 490702514 || got.Count != 1 {
		t.Fatalf("recipient visible consumable slot = %+v, inventory = %+v", got, result.Inventories["7"].Slots)
	}
	if got := result.Received["7"][0].DestinationSlot; got != 65 {
		t.Fatalf("destination slot = %d, want 65", got)
	}
}

func TestOwnerExchangeRejectsInsufficientGoldWithoutMovingItem(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, record := range []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: "a", Stats: map[string]int64{"gold": 99}},
		{CharacterID: "5", AccountID: "b", Stats: map[string]int64{"gold": 0}},
	} {
		if err := repositories.Character.Save(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "5",
		Slots:       map[string]dnfrepo.ItemStack{"0:57": {ItemID: 2002, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repositories)
	_, err := owner.Exchange(ctx,
		Participant{CharacterID: "1", Gold: 100},
		Participant{CharacterID: "5", Offers: []Offer{{TradeSlot: 1, SourceList: 0, SourceSlot: 57, Count: 1, ExpectedItem: 2002}}},
	)
	if !errors.Is(err, ErrGoldUnavailable) {
		t.Fatalf("Exchange() error = %v", err)
	}
	character, _, _ := repositories.Character.Load(ctx, "1")
	if got := character.Stats["gold"]; got != 99 {
		t.Fatalf("first gold mutated to %d", got)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "5")
	if got := inventory.Slots["0:57"]; got.ItemID != 2002 || got.Count != 1 {
		t.Fatalf("offered item mutated: %+v", inventory.Slots)
	}
}
