package dnfbridge

import (
	"context"
	"encoding/binary"
	"math"
	"strconv"
	"strings"

	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// The generated enum's name for 0x0562 belongs to an older protocol table.
// Current NoPack statically registers this opcode to sub_11DBE40.
const currentStoryDigestLastLevelMsgID uint16 = 0x0562

func buildCurrentStoryDigestLastLevelBody(character dnfrepo.CharacterRecord, hasCharacter bool) []byte {
	level := currentStoryDigestLastLevel(character, hasCharacter)
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, level)
	return body
}

func currentStoryDigestLastLevel(character dnfrepo.CharacterRecord, hasCharacter bool) uint32 {
	if !hasCharacter || character.Stats == nil {
		return 0
	}
	value := character.Stats[dnfrepo.CharacterStoryDigestLastLevelStatKey]
	if value <= 0 {
		return 0
	}
	if value > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value)
}

func (s *Service) sendCurrentStoryDigestLastLevel(session *gameSession, character dnfrepo.CharacterRecord, hasCharacter bool, source string) error {
	body := buildCurrentStoryDigestLastLevelBody(character, hasCharacter)
	s.logGameEvent(session, "game-current-story-digest-last-level-send",
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", currentStoryDigestLastLevelMsgID,
		"classification", 0,
		"last_level", binary.LittleEndian.Uint32(body),
		"migration_version", characterStoryDigestMigrationVersion(character, hasCharacter),
		"body_len", len(body),
		"body_source", "current_exe_sub_11DBE40_raw_u32_before_scene_finalizer")
	return s.sendGameUpperRawClass(session, currentStoryDigestLastLevelMsgID, body, 0)
}

func characterStoryDigestMigrationVersion(character dnfrepo.CharacterRecord, hasCharacter bool) uint32 {
	if !hasCharacter || character.Stats == nil {
		return 0
	}
	value := character.Stats[dnfrepo.CharacterStoryDigestMigrationVersionStatKey]
	if value <= 0 {
		return 0
	}
	if value > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value)
}

func (s *Service) handleCurrentStoryDigestAccepted(session *gameSession, classification byte, body []byte) error {
	if classification != dnfproto.DefaultChannelClassification {
		s.logGameEvent(session, "game-current-story-digest-advance-blocked",
			"classification", classification,
			"body_len", len(body),
			"reason", "current_exe_op1445_command_class_mismatch")
		return nil
	}
	if len(body) != 0 {
		s.logGameEvent(session, "game-current-story-digest-advance-blocked",
			"classification", classification,
			"body_len", len(body),
			"reason", "current_exe_op1445_is_bodyless")
		return nil
	}
	if session == nil || session.selectedCharacterID == 0 {
		s.logGameEvent(session, "game-current-story-digest-advance-blocked",
			"body_len", len(body),
			"reason", "selected_character_missing")
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		s.logGameEvent(session, "game-current-story-digest-advance-failed",
			"char_id", session.selectedCharacterID,
			"reason", "character_repository_missing")
		return dnfrepo.ErrCharacterStoryDigestAdvanceUnavailable
	}

	characterID := strconv.Itoa(int(session.selectedCharacterID))
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	record, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		s.logGameEvent(session, "game-current-story-digest-character-load-failed",
			"char_id", session.selectedCharacterID,
			"error", err)
		return err
	}
	if !found || strings.TrimSpace(record.AccountID) != strings.TrimSpace(s.accountIDForSession(session)) {
		s.logGameEvent(session, "game-current-story-digest-advance-blocked",
			"char_id", session.selectedCharacterID,
			"found", found,
			"record_account_id", record.AccountID,
			"session_account_id", s.accountIDForSession(session),
			"reason", "selected_character_owner_mismatch")
		return nil
	}
	if record.Level <= 0 || uint64(record.Level) > uint64(math.MaxUint32) {
		s.logGameEvent(session, "game-current-story-digest-advance-blocked",
			"char_id", session.selectedCharacterID,
			"character_level", record.Level,
			"reason", "character_level_out_of_u32_range")
		return nil
	}
	previousLevel := currentStoryDigestLastLevel(record, true)
	previousVersion := characterStoryDigestMigrationVersion(record, true)
	if err := dnfrepo.AdvanceCharacterStoryDigest(ctx, repositories.Character, characterID, uint32(record.Level), dnfrepo.CurrentCharacterStoryDigestMigrationVersion); err != nil {
		s.logGameEvent(session, "game-current-story-digest-advance-failed",
			"char_id", session.selectedCharacterID,
			"character_level", record.Level,
			"previous_level", previousLevel,
			"previous_migration_version", previousVersion,
			"error", err)
		return err
	}
	s.logGameEvent(session, "game-current-story-digest-advanced",
		"char_id", session.selectedCharacterID,
		"previous_level", previousLevel,
		"current_level", record.Level,
		"previous_migration_version", previousVersion,
		"migration_version", dnfrepo.CurrentCharacterStoryDigestMigrationVersion,
		"wire_body_len", 0,
		"response", "none_current_exe_has_no_proven_ack",
		"persistence_boundary", "current_exe_accepted_playback_start_notification")
	return nil
}
