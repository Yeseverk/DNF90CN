package dnfbridge

import (
	"context"
	"strings"
	"time"

	dnfdungeon "longheng.io/server/internal/modules/dnf/dungeon"
)

const (
	currentLuckyStarNoticeMsgID = uint16(0x0373)
	currentRentalPanelMsgID     = uint16(0x0357)
)

// awardSuitableDungeonLuckyStar grants 1 lucky star when the cleared dungeon's
// recommended level range includes the character's pre-settlement level.
// Mirrors 86JP PR #461: notification is best-effort after settlement packets.
func (s *Service) awardSuitableDungeonLuckyStar(session *gameSession, runtime *runtimeDungeonState) {
	if session == nil || runtime == nil || runtime.Dungeon.ID <= 0 {
		return
	}
	if !currentDungeonMatchesRecommendedLevel(runtime) {
		return
	}
	levels := runtime.Dungeon.Metadata.RecommendedLevels
	characterLevel := int(runtime.Character.Level)
	minLevel := int(levels[0])
	maxLevel := int(levels[1])
	if characterLevel < minLevel || characterLevel > maxLevel {
		return
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	if accountID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repos, ok := s.repositoryGroup()
	if !ok || repos.Account == nil {
		return
	}
	owner, err := dnfdungeon.NewOwner(repos)
	if err != nil {
		return
	}
	result, err := owner.AwardLuckyStar(ctx, dnfdungeon.LuckyStarCommand{
		AccountID:      accountID,
		CharacterLevel: characterLevel,
		RecommendedMin: minLevel,
		RecommendedMax: maxLevel,
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		s.logGameEvent(session, "game-dungeon-lucky-star-save-failed",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"reason", err)
		return
	}
	if !result.Awarded {
		return
	}
	s.logGameEvent(session, "game-dungeon-lucky-star-awarded",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"character_level", characterLevel,
		"recommended_min", minLevel,
		"recommended_max", maxLevel,
		"new_total", result.After)
	// NOTI 0x0373: lucky star gain notification (8B: u32 newTotal + u32 gained).
	var notice [8]byte
	notice[0] = byte(result.After)
	notice[1] = byte(result.After >> 8)
	notice[2] = byte(result.After >> 16)
	notice[3] = byte(result.After >> 24)
	notice[4] = 1 // gained = 1
	_ = s.sendGameUpperRawClass(session, currentLuckyStarNoticeMsgID, notice[:], 0)
	// NOTI 0x0357: rental info panel refresh.
	_ = s.sendGameUpperRawClass(session, currentRentalPanelMsgID, buildCurrentRentalPanelBody(result.After), 0)
}

func buildCurrentRentalPanelBody(luckyStars uint32) []byte {
	// 0x0357 minimal body: u32 luckyStars + u32 reserved (matches initial_defaults.go pattern).
	body := make([]byte, 8)
	body[0] = byte(luckyStars)
	body[1] = byte(luckyStars >> 8)
	body[2] = byte(luckyStars >> 16)
	body[3] = byte(luckyStars >> 24)
	return body
}
