package memory

import (
	"context"
	"testing"
)

func TestMemoryDungeonPermissionUpsertMax(t *testing.T) {
	store := newMemoryDungeonPermissionStore()
	entry, updated, err := store.UpsertMax(context.Background(), "19", 7145, 2)
	if err != nil {
		t.Fatalf("first UpsertMax error = %v", err)
	}
	if !updated || entry.DungeonID != 7145 || entry.ClearState != 2 {
		t.Fatalf("first entry=%+v updated=%v", entry, updated)
	}

	entry, updated, err = store.UpsertMax(context.Background(), "19", 7145, 1)
	if err != nil {
		t.Fatalf("lower UpsertMax error = %v", err)
	}
	if updated || entry.ClearState != 2 {
		t.Fatalf("lower entry=%+v updated=%v, want unchanged state 2", entry, updated)
	}

	entry, updated, err = store.UpsertMax(context.Background(), "19", 7145, 4)
	if err != nil {
		t.Fatalf("higher UpsertMax error = %v", err)
	}
	if !updated || entry.ClearState != 4 {
		t.Fatalf("higher entry=%+v updated=%v, want state 4", entry, updated)
	}

	record, found, err := store.Load(context.Background(), "19")
	if err != nil || !found {
		t.Fatalf("Load found=%v err=%v", found, err)
	}
	if len(record.Entries) != 1 || record.Entries[0].DungeonID != 7145 || record.Entries[0].ClearState != 4 {
		t.Fatalf("record=%+v", record)
	}
}
