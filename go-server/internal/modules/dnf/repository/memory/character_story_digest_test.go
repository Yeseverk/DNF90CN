// 本文件由 character_story_digest_test.go 按后端拆分而来。
package memory

import (
	"context"
	"sync"
	"testing"

	"longheng.io/server/internal/modules/dnf/repository"
)

func TestMemoryCharacterStoryDigestAdvanceIsMonotonicAndConcurrent(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryCharStore()
	if err := repo.Save(ctx, repository.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Level:       90,
		Stats:       map[string]int64{},
	}); err != nil {
		t.Fatal(err)
	}

	levels := []uint32{1, 42, 7, 90, 18, 89, 2, 90, 55, 3}
	var wg sync.WaitGroup
	for _, level := range levels {
		level := level
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repository.AdvanceCharacterStoryDigest(ctx, repo, "77", level, repository.CurrentCharacterStoryDigestMigrationVersion); err != nil {
				t.Errorf("advance level %d: %v", level, err)
			}
		}()
	}
	wg.Wait()

	// A stale retry must not lower either durable scalar.
	if err := repository.AdvanceCharacterStoryDigest(ctx, repo, "77", 4, 0); err != nil {
		t.Fatal(err)
	}
	record, found, err := repo.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load found=%t err=%v", found, err)
	}
	if got := record.Stats[repository.CharacterStoryDigestLastLevelStatKey]; got != 90 {
		t.Fatalf("last story level = %d, want 90", got)
	}
	if got := record.Stats[repository.CharacterStoryDigestMigrationVersionStatKey]; got != int64(repository.CurrentCharacterStoryDigestMigrationVersion) {
		t.Fatalf("migration version = %d, want %d", got, repository.CurrentCharacterStoryDigestMigrationVersion)
	}
	stale := record
	stale.Stats = map[string]int64{"exp": 123}
	if err := repo.Save(ctx, stale); err != nil {
		t.Fatal(err)
	}
	record, found, err = repo.Load(ctx, "77")
	if err != nil || !found || record.Stats[repository.CharacterStoryDigestLastLevelStatKey] != 90 ||
		record.Stats[repository.CharacterStoryDigestMigrationVersionStatKey] != int64(repository.CurrentCharacterStoryDigestMigrationVersion) {
		t.Fatalf("stale generic save regressed story state found=%t err=%v stats=%#v", found, err, record.Stats)
	}
}

func TestRedisCachedCharacterStoryDigestAdvancesAuthoritativeBacking(t *testing.T) {
	ctx := context.Background()
	backing := newMemoryCharStore()
	if err := backing.Save(ctx, repository.CharacterRecord{CharacterID: "88", AccountID: "dnf:1", Level: 60}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewRedisCachedCharacterRepository(backing, newFakeRedisExecutor(), repository.RedisCacheOptions{KeyPrefix: "story:test"})
	if err := repository.AdvanceCharacterStoryDigest(ctx, repo, "88", 60, repository.CurrentCharacterStoryDigestMigrationVersion); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := backing.Load(ctx, "88")
	if err != nil || !found {
		t.Fatalf("load backing found=%t err=%v", found, err)
	}
	if loaded.Stats[repository.CharacterStoryDigestLastLevelStatKey] != 60 ||
		loaded.Stats[repository.CharacterStoryDigestMigrationVersionStatKey] != int64(repository.CurrentCharacterStoryDigestMigrationVersion) {
		t.Fatalf("backing story stats = %#v", loaded.Stats)
	}
}
