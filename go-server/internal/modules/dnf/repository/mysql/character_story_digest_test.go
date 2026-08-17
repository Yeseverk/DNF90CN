// 本文件由 character_story_digest_test.go 按后端拆分而来。
package mysql

import (
	"context"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/repository"
)

func TestMySQLCharacterStatsSaveCannotRegressStoryDigest(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repositories := newTestMySQLGroup(t, sqlDB)
	err := repository.SaveCharacterFields(context.Background(), repositories.Character, repository.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Stats:       map[string]int64{"exp": 123},
	}, repository.CharacterFieldStats)
	if err != nil {
		t.Fatal(err)
	}
	call := requireExecContaining(t, sqlDB, "`dnf_s1_w1`.`dnf_characters`")
	assertContains(t, call.Query, "`story_digest_last_level` = GREATEST(`story_digest_last_level`, VALUES(`story_digest_last_level`))")
	assertContains(t, call.Query, "`story_digest_migration_version` = GREATEST(`story_digest_migration_version`, VALUES(`story_digest_migration_version`))")
}

func TestMySQLCharacterStoryDigestAdvanceUsesOneAtomicMonotonicUpdate(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repositories := newTestMySQLGroup(t, sqlDB)
	if err := repository.AdvanceCharacterStoryDigest(context.Background(), repositories.Character, "77", 42, repository.CurrentCharacterStoryDigestMigrationVersion); err != nil {
		t.Fatal(err)
	}
	call := requireOneExec(t, sqlDB)
	assertContains(t, call.Query, "UPDATE `dnf_s1_w1`.`dnf_characters`")
	assertContains(t, call.Query, "`story_digest_last_level` = GREATEST(`story_digest_last_level`, ?)")
	assertContains(t, call.Query, "`story_digest_migration_version` = GREATEST(`story_digest_migration_version`, ?)")
	assertContains(t, call.Query, "WHERE `character_id` = ?")
	if len(call.Args) != 4 {
		t.Fatalf("atomic advance args = %#v, want four", call.Args)
	}
	if call.Args[0] != int64(42) || call.Args[1] != int64(repository.CurrentCharacterStoryDigestMigrationVersion) || call.Args[3] != "77" {
		t.Fatalf("atomic advance args = %#v", call.Args)
	}
	wantTime := time.Date(2026, 6, 29, 3, 0, 0, 0, time.UTC)
	if got, ok := call.Args[2].(time.Time); !ok || !got.Equal(wantTime) {
		t.Fatalf("updated_at = %#v, want %s", call.Args[2], wantTime)
	}
}

func TestMySQLCharacterCreatePersistsOriginStoryDigestInitialization(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	repositories := newTestMySQLGroup(t, sqlDB)
	err := repository.CreateCharacter(context.Background(), repositories.Character, repository.CharacterRecord{
		CharacterID: "91",
		AccountID:   "dnf:1",
		Level:       1,
		Stats: map[string]int64{
			repository.CharacterStoryDigestLastLevelStatKey:        1,
			repository.CharacterStoryDigestMigrationVersionStatKey: int64(repository.CurrentCharacterStoryDigestMigrationVersion),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	call := requireOneExec(t, sqlDB)
	levelIndex := requireMySQLCharacterStatIndex(t, repository.CharacterStoryDigestLastLevelStatKey)
	versionIndex := requireMySQLCharacterStatIndex(t, repository.CharacterStoryDigestMigrationVersionStatKey)
	if got := call.Args[8+levelIndex]; got != int64(1) {
		t.Fatalf("created story digest level arg=%#v, want 1", got)
	}
	if got := call.Args[8+versionIndex]; got != int64(repository.CurrentCharacterStoryDigestMigrationVersion) {
		t.Fatalf("created story migration version arg=%#v, want %d", got, repository.CurrentCharacterStoryDigestMigrationVersion)
	}
}
