package memory

import (
	"context"
	"database/sql"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/repository/mysql"
	"strconv"
	"strings"
	"sync"
	"testing"

	platformdb "longheng.io/server/internal/platform/db"
)

func TestMemoryRentalAssetUnitOfWorkCommitsAllAggregates(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedRentalAssets(t, ctx, repos)

	err := repos.RentalAssets.WithinRentalAssets(ctx, "dnf:1", "77", func(
		accounts repository.AccountRepository,
		characters repository.CharacterRepository,
		inventory repository.InventoryRepository,
		equipment repository.EquipmentRepository,
	) error {
		account, _, _ := accounts.Load(ctx, "dnf:1")
		character, _, _ := characters.Load(ctx, "77")
		bag, _, _ := inventory.Load(ctx, "77")
		worn, _, _ := equipment.Load(ctx, "77")
		if worn.Entries == nil {
			worn.Entries = make(map[string]repository.EquipmentEntry)
		}
		account.Metadata["rental_stars"] = "6"
		character.Stats["gold"] = 80
		bag.Slots["0:1"] = repository.ItemStack{ItemID: 9001, Count: 1}
		worn.Entries["11"] = repository.EquipmentEntry{SlotIndex: 11, ItemID: 9001}
		if err := accounts.Save(ctx, account); err != nil {
			return err
		}
		if err := repository.SaveCharacterFields(ctx, characters, character, repository.CharacterFieldStats); err != nil {
			return err
		}
		if err := repository.SaveInventoryFields(ctx, inventory, bag, repository.InventoryFieldSlots); err != nil {
			return err
		}
		return repository.SaveEquipmentFields(ctx, equipment, worn, repository.EquipmentFieldEntries)
	})
	if err != nil {
		t.Fatalf("WithinRentalAssets() error = %v", err)
	}

	account, _, _ := repos.Account.Load(ctx, "dnf:1")
	character, _, _ := repos.Character.Load(ctx, "77")
	bag, _, _ := repos.Inventory.Load(ctx, "77")
	worn, _, _ := repos.Equipment.Load(ctx, "77")
	if account.Metadata["rental_stars"] != "6" || character.Stats["gold"] != 80 {
		t.Fatalf("wallets account=%+v character=%+v", account.Metadata, character.Stats)
	}
	if bag.Slots["0:1"].ItemID != 9001 || worn.Entries["11"].ItemID != 9001 {
		t.Fatalf("rental item bag=%+v equipment=%+v", bag.Slots, worn.Entries)
	}
}

func TestMemoryRentalAssetUnitOfWorkRollsBackCallback(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedRentalAssets(t, ctx, repos)
	wantErr := errors.New("reject rental")

	err := repos.RentalAssets.WithinRentalAssets(ctx, "dnf:1", "77", func(
		accounts repository.AccountRepository,
		characters repository.CharacterRepository,
		inventory repository.InventoryRepository,
		equipment repository.EquipmentRepository,
	) error {
		account, _, _ := accounts.Load(ctx, "dnf:1")
		character, _, _ := characters.Load(ctx, "77")
		bag, _, _ := inventory.Load(ctx, "77")
		worn, _, _ := equipment.Load(ctx, "77")
		if worn.Entries == nil {
			worn.Entries = make(map[string]repository.EquipmentEntry)
		}
		account.Metadata["rental_stars"] = "0"
		character.Stats["gold"] = 0
		bag.Slots["0:1"] = repository.ItemStack{ItemID: 9001, Count: 1}
		worn.Entries["11"] = repository.EquipmentEntry{SlotIndex: 11, ItemID: 9001}
		if err := accounts.Save(ctx, account); err != nil {
			return err
		}
		if err := characters.Save(ctx, character); err != nil {
			return err
		}
		if err := inventory.Save(ctx, bag); err != nil {
			return err
		}
		if err := equipment.Save(ctx, worn); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithinRentalAssets() error = %v, want %v", err, wantErr)
	}
	assertOriginalRentalAssets(t, ctx, repos)
}

func TestMemoryRentalAssetUnitOfWorkRejectsCrossOwnerAccess(t *testing.T) {
	tests := []struct {
		name  string
		apply func(context.Context, repository.AccountRepository, repository.CharacterRepository, repository.InventoryRepository, repository.EquipmentRepository) error
	}{
		{"account", func(ctx context.Context, accounts repository.AccountRepository, _ repository.CharacterRepository, _ repository.InventoryRepository, _ repository.EquipmentRepository) error {
			_, _, err := accounts.Load(ctx, "dnf:2")
			return err
		}},
		{"character", func(ctx context.Context, _ repository.AccountRepository, characters repository.CharacterRepository, _ repository.InventoryRepository, _ repository.EquipmentRepository) error {
			return characters.Save(ctx, repository.CharacterRecord{CharacterID: "78"})
		}},
		{"inventory", func(ctx context.Context, _ repository.AccountRepository, _ repository.CharacterRepository, inventory repository.InventoryRepository, _ repository.EquipmentRepository) error {
			_, _, err := inventory.Load(ctx, "78")
			return err
		}},
		{"equipment", func(ctx context.Context, _ repository.AccountRepository, _ repository.CharacterRepository, _ repository.InventoryRepository, equipment repository.EquipmentRepository) error {
			return equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "78"})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repos := NewMemoryGroup()
			seedRentalAssets(t, ctx, repos)
			err := repos.RentalAssets.WithinRentalAssets(ctx, "dnf:1", "77", func(a repository.AccountRepository, c repository.CharacterRepository, i repository.InventoryRepository, e repository.EquipmentRepository) error {
				return test.apply(ctx, a, c, i, e)
			})
			if !errors.Is(err, platformdb.ErrRecordKeyRequired) {
				t.Fatalf("cross-owner error = %v, want ErrRecordKeyRequired", err)
			}
			assertOriginalRentalAssets(t, ctx, repos)
		})
	}
}

func TestMemoryRentalAssetUnitOfWorkRestoresEarlierCommits(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedRentalAssets(t, ctx, repos)
	wantErr := errors.New("equipment commit failed")
	failingEquipment := &mutatingEquipmentStore{EquipmentRepository: repos.Equipment, saveErr: wantErr}
	uow := &memoryRentalAssetUnitOfWork{
		account:   repos.Account,
		character: repos.Character,
		inventory: repos.Inventory,
		equipment: failingEquipment,
	}

	err := uow.WithinRentalAssets(ctx, "dnf:1", "77", func(accounts repository.AccountRepository, characters repository.CharacterRepository, inventory repository.InventoryRepository, equipment repository.EquipmentRepository) error {
		account, _, _ := accounts.Load(ctx, "dnf:1")
		character, _, _ := characters.Load(ctx, "77")
		bag, _, _ := inventory.Load(ctx, "77")
		worn, _, _ := equipment.Load(ctx, "77")
		if worn.Entries == nil {
			worn.Entries = make(map[string]repository.EquipmentEntry)
		}
		account.Metadata["rental_stars"] = "0"
		character.Stats["gold"] = 0
		bag.Slots["0:1"] = repository.ItemStack{ItemID: 9001, Count: 1}
		worn.Entries["11"] = repository.EquipmentEntry{SlotIndex: 11, ItemID: 9001}
		if err := accounts.Save(ctx, account); err != nil {
			return err
		}
		if err := characters.Save(ctx, character); err != nil {
			return err
		}
		if err := inventory.Save(ctx, bag); err != nil {
			return err
		}
		return equipment.Save(ctx, worn)
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithinRentalAssets() error = %v, want %v", err, wantErr)
	}
	assertOriginalRentalAssets(t, ctx, repos)
}

func TestMemoryRentalAssetUnitOfWorkSerializesWalletUpdates(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedRentalAssets(t, ctx, repos)

	var wg sync.WaitGroup
	errs := make(chan error, 30)
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repos.RentalAssets.WithinRentalAssets(ctx, "dnf:1", "77", func(accounts repository.AccountRepository, characters repository.CharacterRepository, _ repository.InventoryRepository, _ repository.EquipmentRepository) error {
				account, _, err := accounts.Load(ctx, "dnf:1")
				if err != nil {
					return err
				}
				character, _, err := characters.Load(ctx, "77")
				if err != nil {
					return err
				}
				stars, err := strconv.Atoi(account.Metadata["rental_stars"])
				if err != nil {
					return err
				}
				account.Metadata["rental_stars"] = strconv.Itoa(stars + 1)
				character.Stats["gold"]++
				if err := accounts.Save(ctx, account); err != nil {
					return err
				}
				return characters.Save(ctx, character)
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent rental update: %v", err)
		}
	}
	account, _, _ := repos.Account.Load(ctx, "dnf:1")
	character, _, _ := repos.Character.Load(ctx, "77")
	if account.Metadata["rental_stars"] != "40" || character.Stats["gold"] != 130 {
		t.Fatalf("serialized wallets account=%+v character=%+v", account.Metadata, character.Stats)
	}
}

func TestMySQLRentalAssetUnitOfWorkCommitsFourWrites(t *testing.T) {
	state := &progressionSQLState{}
	database := sql.OpenDB(progressionSQLConnector{state: state})
	t.Cleanup(func() { _ = database.Close() })
	repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}}})
	if err != nil {
		t.Fatalf("NewMySQLGroupFromDB() error = %v", err)
	}

	err = repos.RentalAssets.WithinRentalAssets(context.Background(), "dnf:1", "77", func(accounts repository.AccountRepository, characters repository.CharacterRepository, inventory repository.InventoryRepository, equipment repository.EquipmentRepository) error {
		if err := accounts.Save(context.Background(), repository.AccountRecord{AccountID: "dnf:1", Metadata: map[string]string{"rental_stars": "6"}}); err != nil {
			return err
		}
		if err := repository.SaveCharacterFields(context.Background(), characters, repository.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 80}}, repository.CharacterFieldStats); err != nil {
			return err
		}
		if err := repository.SaveInventoryFields(context.Background(), inventory, repository.InventoryRecord{CharacterID: "77", Slots: map[string]repository.ItemStack{"0:1": {ItemID: 9001, Count: 1}}}, repository.InventoryFieldSlots); err != nil {
			return err
		}
		return repository.SaveEquipmentFields(context.Background(), equipment, repository.EquipmentRecord{CharacterID: "77", Entries: map[string]repository.EquipmentEntry{"11": {SlotIndex: 11, ItemID: 9001}}}, repository.EquipmentFieldEntries)
	})
	if err != nil {
		t.Fatalf("WithinRentalAssets() error = %v", err)
	}

	begin, commit, rollback, queries := state.snapshot()
	if begin != 1 || commit != 1 || rollback != 0 {
		t.Fatalf("mysql transaction begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
	}
	wantTables := []string{"dnf_accounts", "dnf_characters", "dnf_inventories", "dnf_equipments"}
	joined := strings.Join(queries, "\n")
	for _, table := range wantTables {
		if !strings.Contains(joined, "`dnf_s1_w1`.`"+table+"`") {
			t.Fatalf("queries = %q, want table %s", queries, table)
		}
	}
}

func TestMySQLRentalAssetUnitOfWorkRollsBackAndScopesBothOwners(t *testing.T) {
	tests := []struct {
		name  string
		apply func(repository.AccountRepository, repository.CharacterRepository) error
	}{
		{"account", func(accounts repository.AccountRepository, _ repository.CharacterRepository) error {
			return accounts.Save(context.Background(), repository.AccountRecord{AccountID: "dnf:2"})
		}},
		{"character", func(_ repository.AccountRepository, characters repository.CharacterRepository) error {
			return characters.Save(context.Background(), repository.CharacterRecord{CharacterID: "78"})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := &progressionSQLState{}
			database := sql.OpenDB(progressionSQLConnector{state: state})
			t.Cleanup(func() { _ = database.Close() })
			repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}}})
			if err != nil {
				t.Fatalf("NewMySQLGroupFromDB() error = %v", err)
			}
			err = repos.RentalAssets.WithinRentalAssets(context.Background(), "dnf:1", "77", func(accounts repository.AccountRepository, characters repository.CharacterRepository, _ repository.InventoryRepository, _ repository.EquipmentRepository) error {
				return test.apply(accounts, characters)
			})
			if !errors.Is(err, platformdb.ErrRecordKeyRequired) {
				t.Fatalf("cross-owner error = %v, want ErrRecordKeyRequired", err)
			}
			begin, commit, rollback, queries := state.snapshot()
			if begin != 1 || commit != 0 || rollback != 1 || len(queries) != 0 {
				t.Fatalf("mysql rollback begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
			}
		})
	}
}

func TestMySQLRentalAssetUnitOfWorkRollsBackExecutedWrites(t *testing.T) {
	state := &progressionSQLState{}
	database := sql.OpenDB(progressionSQLConnector{state: state})
	t.Cleanup(func() { _ = database.Close() })
	repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}}})
	if err != nil {
		t.Fatalf("NewMySQLGroupFromDB() error = %v", err)
	}
	wantErr := errors.New("reject after rental writes")

	err = repos.RentalAssets.WithinRentalAssets(context.Background(), "dnf:1", "77", func(accounts repository.AccountRepository, characters repository.CharacterRepository, inventory repository.InventoryRepository, equipment repository.EquipmentRepository) error {
		if err := accounts.Save(context.Background(), repository.AccountRecord{AccountID: "dnf:1"}); err != nil {
			return err
		}
		if err := characters.Save(context.Background(), repository.CharacterRecord{CharacterID: "77"}); err != nil {
			return err
		}
		if err := inventory.Save(context.Background(), repository.InventoryRecord{CharacterID: "77"}); err != nil {
			return err
		}
		if err := equipment.Save(context.Background(), repository.EquipmentRecord{CharacterID: "77"}); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithinRentalAssets() error = %v, want %v", err, wantErr)
	}
	begin, commit, rollback, queries := state.snapshot()
	if begin != 1 || commit != 0 || rollback != 1 {
		t.Fatalf("mysql rollback begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
	}
}

func seedRentalAssets(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	if repos.RentalAssets == nil {
		t.Fatal("rental asset unit of work is missing")
	}
	if err := repos.Account.Save(ctx, repository.AccountRecord{AccountID: "dnf:1", Metadata: map[string]string{"rental_stars": "10"}}); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if err := repos.Character.Save(ctx, repository.CharacterRecord{CharacterID: "77", AccountID: "dnf:1", Stats: map[string]int64{"gold": 100}}); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, repository.InventoryRecord{CharacterID: "77", Slots: map[string]repository.ItemStack{"0:0": {ItemID: 700, Count: 1}}}); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	if err := repos.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "77", Entries: map[string]repository.EquipmentEntry{}}); err != nil {
		t.Fatalf("seed equipment: %v", err)
	}
}

func assertOriginalRentalAssets(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	account, _, _ := repos.Account.Load(ctx, "dnf:1")
	character, _, _ := repos.Character.Load(ctx, "77")
	bag, _, _ := repos.Inventory.Load(ctx, "77")
	worn, _, _ := repos.Equipment.Load(ctx, "77")
	if account.Metadata["rental_stars"] != "10" || character.Stats["gold"] != 100 {
		t.Fatalf("wallet mutation escaped rollback account=%+v character=%+v", account.Metadata, character.Stats)
	}
	if len(bag.Slots) != 1 || bag.Slots["0:0"].ItemID != 700 {
		t.Fatalf("inventory mutation escaped rollback: %+v", bag.Slots)
	}
	if len(worn.Entries) != 0 {
		t.Fatalf("equipment mutation escaped rollback: %+v", worn.Entries)
	}
}
