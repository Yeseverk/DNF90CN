package adventuregroup

import (
	"math"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	DailyLoginStateMetadataKey     = "adventure_group_daily_login"
	RecommendedDungeonClearStatKey = "adventure_recommended_dungeon_clears"
	ContentTypeRecommendedDungeon  = 0
	ContentTypeCount               = 20
	adventureGroupContentCountMax  = math.MaxUint16
)

// Projection contains only account activity that has an authoritative local
// owner. Unsupported raid, PVP and tower counters deliberately stay zero.
type Projection struct {
	ConsecutiveLoginDays uint32
	ContentCounts        [ContentTypeCount]uint16
	Runtime              RuntimeState
}

// ProjectActivity folds durable character-scoped activity into the
// account-wide values consumed by the current adventure-group packet.
func ProjectActivity(
	consecutiveLoginDays uint32,
	characters []dnfrepo.CharacterRecord,
) Projection {
	result := Projection{ConsecutiveLoginDays: consecutiveLoginDays}
	var recommended uint64
	for _, character := range characters {
		value := character.Stats[RecommendedDungeonClearStatKey]
		if value <= 0 {
			continue
		}
		if uint64(value) >= adventureGroupContentCountMax-recommended {
			recommended = adventureGroupContentCountMax
			break
		}
		recommended += uint64(value)
	}
	result.ContentCounts[ContentTypeRecommendedDungeon] = uint16(recommended)
	return result
}
