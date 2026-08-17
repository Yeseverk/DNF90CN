package memory

import (
	"context"
	"database/sql"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/repository/mysql"
	"strings"
	"testing"

	platformdb "longheng.io/server/internal/platform/db"
)

func TestMemoryAccountInventoryIsSharedWhileOrdinarySlotsRemainCharacterScoped(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	shared := repository.ItemStack{
		ItemID:   3033,
		Count:    25,
		Bind:     true,
		RawEntry: []byte{1, 2, 3, 4},
		Extra:    map[string]string{"kind": "cube"},
	}
	if err := repos.AccountInventory.Save(ctx, repository.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots:     map[string]repository.ItemStack{repository.AccountSharedInventorySlotKey(354): shared},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Inventory.Save(ctx, repository.InventoryRecord{CharacterID: "77", Slots: map[string]repository.ItemStack{"0:9": {ItemID: 700, Count: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Inventory.Save(ctx, repository.InventoryRecord{CharacterID: "78", Slots: map[string]repository.ItemStack{"0:9": {ItemID: 800, Count: 1}}}); err != nil {
		t.Fatal(err)
	}

	account, _, _ := repos.AccountInventory.Load(ctx, "dnf:1")
	first, _, _ := repos.Inventory.Load(ctx, "77")
	second, _, _ := repos.Inventory.Load(ctx, "78")
	first = repository.MergeAccountSharedInventory(first, account)
	second = repository.MergeAccountSharedInventory(second, account)
	if got := first.Slots["0:354"]; got.ItemID != 3033 || got.Count != 25 || !got.Bind || string(got.RawEntry) != string(shared.RawEntry) || got.Extra["kind"] != "cube" {
		t.Fatalf("first shared slot = %+v", got)
	}
	if got := second.Slots["0:354"]; got.ItemID != 3033 || got.Count != 25 {
		t.Fatalf("second shared slot = %+v", got)
	}
	if first.Slots["0:9"].ItemID != 700 || second.Slots["0:9"].ItemID != 800 {
		t.Fatalf("ordinary slots leaked first=%+v second=%+v", first.Slots, second.Slots)
	}

	mutated := first.Slots["0:354"]
	mutated.RawEntry = []byte{9}
	mutated.Extra = map[string]string{"kind": "mutated"}
	first.Slots["0:354"] = mutated
	reloaded, _, _ := repos.AccountInventory.Load(ctx, "dnf:1")
	if got := reloaded.Slots["0:354"]; got.ItemID != 3033 || string(got.RawEntry) != string(shared.RawEntry) || got.Extra["kind"] != "cube" {
		t.Fatalf("merged view aliased account record: %+v", got)
	}
}

func TestMemoryAccountCharacterItemUnitOfWorkCommitsBothOwners(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedAccountCharacterInventory(t, ctx, repos)

	err := repos.AccountItems.WithinAccountCharacterItems(ctx, "dnf:1", "77", func(accounts repository.AccountInventoryRepository, characters repository.InventoryRepository) error {
		account, _, err := accounts.Load(ctx, "dnf:1")
		if err != nil {
			return err
		}
		character, _, err := characters.Load(ctx, "77")
		if err != nil {
			return err
		}
		stack := character.Slots["0:9"]
		delete(character.Slots, "0:9")
		account.Slots["0:354"] = stack
		if err := characters.Save(ctx, character); err != nil {
			return err
		}
		return accounts.Save(ctx, account)
	})
	if err != nil {
		t.Fatal(err)
	}
	account, _, _ := repos.AccountInventory.Load(ctx, "dnf:1")
	character, _, _ := repos.Inventory.Load(ctx, "77")
	if account.Slots["0:354"].ItemID != 700 || account.Slots["0:354"].Extra["origin"] != "character" {
		t.Fatalf("account inventory = %+v", account.Slots)
	}
	if _, ok := character.Slots["0:9"]; ok {
		t.Fatalf("character source was not removed: %+v", character.Slots)
	}
}

func TestMemoryAccountCharacterItemUnitOfWorkRollsBackBothOwners(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedAccountCharacterInventory(t, ctx, repos)
	wantErr := errors.New("reject both item owners")

	err := repos.AccountItems.WithinAccountCharacterItems(ctx, "dnf:1", "77", func(accounts repository.AccountInventoryRepository, characters repository.InventoryRepository) error {
		account, _, _ := accounts.Load(ctx, "dnf:1")
		character, _, _ := characters.Load(ctx, "77")
		delete(account.Slots, "0:354")
		delete(character.Slots, "0:9")
		if err := accounts.Save(ctx, account); err != nil {
			return err
		}
		if err := characters.Save(ctx, character); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	assertAccountCharacterInventorySeed(t, ctx, repos)
}

func TestMemoryAccountCharacterItemUnitOfWorkRejectsCrossOwnerAccess(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedAccountCharacterInventory(t, ctx, repos)

	err := repos.AccountItems.WithinAccountCharacterItems(ctx, "dnf:1", "77", func(accounts repository.AccountInventoryRepository, characters repository.InventoryRepository) error {
		if _, _, err := accounts.Load(ctx, "dnf:2"); err == nil {
			return errors.New("cross-account read unexpectedly succeeded")
		} else if !errors.Is(err, platformdb.ErrRecordKeyRequired) {
			return err
		}
		return characters.Save(ctx, repository.InventoryRecord{CharacterID: "78"})
	})
	if !errors.Is(err, platformdb.ErrRecordKeyRequired) {
		t.Fatalf("error = %v, want ErrRecordKeyRequired", err)
	}
	assertAccountCharacterInventorySeed(t, ctx, repos)
}

func TestMySQLAccountCharacterItemUnitOfWorkCommitsTwoOwners(t *testing.T) {
	state := &progressionSQLState{}
	database := sql.OpenDB(progressionSQLConnector{state: state})
	t.Cleanup(func() { _ = database.Close() })
	repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}}})
	if err != nil {
		t.Fatal(err)
	}

	err = repos.AccountItems.WithinAccountCharacterItems(context.Background(), "dnf:1", "77", func(accounts repository.AccountInventoryRepository, characters repository.InventoryRepository) error {
		if err := accounts.Save(context.Background(), repository.AccountInventoryRecord{AccountID: "dnf:1", Slots: map[string]repository.ItemStack{"0:354": {ItemID: 3033, Count: 1}}}); err != nil {
			return err
		}
		return characters.Save(context.Background(), repository.InventoryRecord{CharacterID: "77", Slots: map[string]repository.ItemStack{"0:9": {ItemID: 700, Count: 1}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	begin, commit, rollback, queries := state.snapshot()
	if begin != 1 || commit != 1 || rollback != 0 {
		t.Fatalf("begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
	}
	joined := strings.Join(queries, "\n")
	if !strings.Contains(joined, "`dnf_s1_w1`.`dnf_account_inventories`") ||
		!strings.Contains(joined, "`dnf_s1_w1`.`dnf_account_inventory_items`") ||
		!strings.Contains(joined, "`dnf_s1_w1`.`dnf_inventories`") ||
		!strings.Contains(joined, "`dnf_s1_w1`.`dnf_inventory_items`") {
		t.Fatalf("queries = %v", queries)
	}
}

func TestMySQLAccountCharacterItemUnitOfWorkRollsBackExecutedWrites(t *testing.T) {
	state := &progressionSQLState{}
	database := sql.OpenDB(progressionSQLConnector{state: state})
	t.Cleanup(func() { _ = database.Close() })
	repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}}})
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("rollback account and character items")
	err = repos.AccountItems.WithinAccountCharacterItems(context.Background(), "dnf:1", "77", func(accounts repository.AccountInventoryRepository, characters repository.InventoryRepository) error {
		if err := accounts.Save(context.Background(), repository.AccountInventoryRecord{AccountID: "dnf:1"}); err != nil {
			return err
		}
		if err := characters.Save(context.Background(), repository.InventoryRecord{CharacterID: "77"}); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	begin, commit, rollback, queries := state.snapshot()
	if begin != 1 || commit != 0 || rollback != 1 {
		t.Fatalf("begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
	}
}

func TestMySQLSchemaIncludesAccountInventoryTable(t *testing.T) {
	schema, err := mysql.MySQLSchema(mysql.SchemaOptions{DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}}})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(schema, "\n")
	if !strings.Contains(joined, "CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_account_inventories`") ||
		!strings.Contains(joined, "CREATE TABLE IF NOT EXISTS `dnf_s1_w1`.`dnf_account_inventory_items`") ||
		strings.Contains(joined, "slots_json JSON NULL") ||
		!strings.Contains(joined, "PRIMARY KEY (account_id)") {
		t.Fatalf("account inventory schema missing:\n%s", joined)
	}
}

func seedAccountCharacterInventory(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	if err := repos.AccountInventory.Save(ctx, repository.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots:     map[string]repository.ItemStack{"0:354": {ItemID: 3033, Count: 5, RawEntry: []byte{3, 5, 4}, Extra: map[string]string{"origin": "account"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Inventory.Save(ctx, repository.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]repository.ItemStack{"0:9": {ItemID: 700, Count: 1, RawEntry: []byte{7, 0, 0}, Extra: map[string]string{"origin": "character"}}},
	}); err != nil {
		t.Fatal(err)
	}
}

func assertAccountCharacterInventorySeed(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	account, _, _ := repos.AccountInventory.Load(ctx, "dnf:1")
	character, _, _ := repos.Inventory.Load(ctx, "77")
	if got := account.Slots["0:354"]; got.ItemID != 3033 || got.Count != 5 || got.Extra["origin"] != "account" {
		t.Fatalf("account rollback failed: %+v", got)
	}
	if got := character.Slots["0:9"]; got.ItemID != 700 || got.Count != 1 || got.Extra["origin"] != "character" {
		t.Fatalf("character rollback failed: %+v", got)
	}
}
