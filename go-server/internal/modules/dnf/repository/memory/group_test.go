package memory

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"testing"

	"longheng.io/server/internal/platform/db"
)

func TestMemoryGroupStoresDetachedRecords(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	if err := repos.Check(ctx); err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	account := repository.AccountRecord{
		AccountID: "acc-1",
		HonorExp:  987654321,
		Metadata:  map[string]string{"source": "test"},
	}
	if err := repos.Account.Save(ctx, account); err != nil {
		t.Fatalf("save account: %v", err)
	}
	account.Metadata["source"] = "caller-mutated"

	loaded, ok, err := repos.Account.Load(ctx, "acc-1")
	if err != nil || !ok {
		t.Fatalf("load account: ok=%v err=%v", ok, err)
	}
	if loaded.Metadata["source"] != "test" || loaded.HonorExp != 987654321 {
		t.Fatalf("account metadata should be detached: %+v", loaded.Metadata)
	}
	loaded.Metadata["source"] = "loaded-mutated"
	again, _, _ := repos.Account.Load(ctx, "acc-1")
	if again.Metadata["source"] != "test" {
		t.Fatalf("loaded mutation polluted store: %+v", again.Metadata)
	}
}

func TestMemoryAccountRepresentNameIsUnique(t *testing.T) {
	repos := NewMemoryGroup()
	ctx := context.Background()
	if err := repos.Account.Save(ctx, repository.AccountRecord{AccountID: "acc-1", RepresentAccountName: "group-one"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Account.Save(ctx, repository.AccountRecord{AccountID: "acc-2", RepresentAccountName: "GROUP-ONE"}); !errors.Is(err, repository.ErrRepresentAccountNameExists) {
		t.Fatalf("duplicate represent name error = %v", err)
	}
	finder := repos.Account.(repository.RepresentAccountNameFinder)
	accountID, found, err := finder.FindAccountIDByRepresentName(ctx, "group-one")
	if err != nil || !found || accountID != "acc-1" {
		t.Fatalf("represent-name lookup account=%q found=%v err=%v", accountID, found, err)
	}
}

func TestMemoryGroupStoresDetachedQuestRecords(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	record := repository.QuestRecord{
		CharacterID: "quest-char-1",
		States: map[int64]repository.QuestState{
			100: {Status: "active", Extra: map[string]string{"source": "unit"}},
		},
	}
	if err := repos.Quest.Save(ctx, record); err != nil {
		t.Fatalf("save quest: %v", err)
	}
	record.States[100] = repository.QuestState{Status: "mutated"}

	loaded, ok, err := repos.Quest.Load(ctx, "quest-char-1")
	if err != nil || !ok {
		t.Fatalf("load quest: ok=%v err=%v", ok, err)
	}
	if loaded.States[100].Status != "active" || loaded.States[100].Extra["source"] != "unit" {
		t.Fatalf("quest state should be detached: %+v", loaded.States[100])
	}
	loaded.States[100].Extra["source"] = "loaded-mutated"
	again, _, _ := repos.Quest.Load(ctx, "quest-char-1")
	if again.States[100].Extra["source"] != "unit" {
		t.Fatalf("loaded quest mutation polluted store: %+v", again.States[100])
	}
}

func TestSaveFieldsFallbacksToWholeRecord(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	record := repository.CharacterRecord{
		CharacterID: "char-1",
		AccountID:   "acc-1",
		Name:        "fighter",
		Stats:       map[string]int64{"str": 10},
	}
	if err := repository.SaveCharacterFields(ctx, repos.Character, record, repository.CharacterFieldStats); err != nil {
		t.Fatalf("SaveCharacterFields() error = %v", err)
	}
	record.Stats["str"] = 1

	loaded, ok, err := repos.Character.Load(ctx, "char-1")
	if err != nil || !ok {
		t.Fatalf("load character: ok=%v err=%v", ok, err)
	}
	if loaded.Stats["str"] != 10 {
		t.Fatalf("character stats should be detached: %+v", loaded.Stats)
	}
}

func TestMemoryCharacterStatsPersistTutorialCompleted(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	record := repository.CharacterRecord{
		CharacterID: "tutorial-char-1",
		AccountID:   "acc-1",
		Stats:       map[string]int64{"tutorial_completed": 0},
	}
	if err := repos.Character.Save(ctx, record); err != nil {
		t.Fatalf("save character: %v", err)
	}

	record.Stats["tutorial_completed"] = 1
	if err := repository.SaveCharacterFields(ctx, repos.Character, record, repository.CharacterFieldStats); err != nil {
		t.Fatalf("SaveCharacterFields() error = %v", err)
	}
	record.Stats["tutorial_completed"] = 0

	loaded, ok, err := repos.Character.Load(ctx, record.CharacterID)
	if err != nil || !ok {
		t.Fatalf("load character: ok=%v err=%v", ok, err)
	}
	if got := loaded.Stats["tutorial_completed"]; got != 1 {
		t.Fatalf("tutorial_completed = %d, want 1", got)
	}
}

func TestGroupCheckRejectsMissingRepo(t *testing.T) {
	repos := NewMemoryGroup()
	repos.Skill = nil
	if err := repos.Check(context.Background()); !errors.Is(err, repository.ErrRepoMissing) {
		t.Fatalf("Check() error = %v, want ErrRepoMissing", err)
	}
}

func TestRepositoryKeysRejectEmptyID(t *testing.T) {
	repos := NewMemoryGroup()
	err := repos.Inventory.Save(context.Background(), repository.InventoryRecord{})
	if !errors.Is(err, db.ErrRecordKeyRequired) {
		t.Fatalf("Save() error = %v, want ErrRecordKeyRequired", err)
	}
}
