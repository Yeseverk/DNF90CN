package repository

import (
	"context"
	"errors"
	"strings"
)

const (
	// CharacterStoryDigestLastLevelStatKey is the per-character scalar consumed
	// by the current EXE class0/op1378 reader. The client compares the story
	// segment selected by this level with the segment selected by the actor's
	// current level before deciding whether a legacy summary must play.
	CharacterStoryDigestLastLevelStatKey = "story_digest_last_level"

	// CharacterStoryDigestMigrationVersionStatKey makes the creation/migration
	// boundary explicit. Existing rows migrate with zero; new characters are
	// created at CurrentCharacterStoryDigestMigrationVersion.
	CharacterStoryDigestMigrationVersionStatKey = "story_digest_migration_version"

	CurrentCharacterStoryDigestMigrationVersion uint32 = 1
)

var (
	ErrCharacterStoryDigestAdvanceUnavailable = errors.New("dnf character story digest advance is unavailable")
	ErrCharacterStoryDigestCharacterMissing   = errors.New("dnf character story digest character is missing")
)

// CharacterStoryDigestAdvancer owns the monotonic durability boundary for the
// current EXE's bodyless op1445 notification. Implementations must never lower
// either value when requests race or replay.
type CharacterStoryDigestAdvancer interface {
	AdvanceStoryDigest(context.Context, string, uint32, uint32) error
}

// AdvanceCharacterStoryDigest advances a character's accepted story-summary
// level without widening CharacterRepository and breaking unrelated wrappers.
func AdvanceCharacterStoryDigest(ctx context.Context, repo CharacterRepository, characterID string, level, migrationVersion uint32) error {
	if repo == nil {
		return ErrCharacterStoryDigestAdvanceUnavailable
	}
	if strings.TrimSpace(characterID) == "" {
		return ErrCharacterStoryDigestCharacterMissing
	}
	if ctx == nil {
		ctx = context.Background()
	}
	advancer, ok := repo.(CharacterStoryDigestAdvancer)
	if !ok {
		return ErrCharacterStoryDigestAdvanceUnavailable
	}
	return advancer.AdvanceStoryDigest(ctx, characterID, level, migrationVersion)
}
