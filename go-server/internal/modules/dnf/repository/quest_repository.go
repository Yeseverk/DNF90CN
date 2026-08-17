package repository

import (
	"context"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

type QuestRepository interface {
	db.Store[QuestRecord]
}

type QuestRecord struct {
	CharacterID string               `json:"character_id"`
	States      map[int64]QuestState `json:"states,omitempty"`
	Progress    map[int64]QuestState `json:"progress,omitempty"`
	UpdatedAt   time.Time            `json:"updated_at,omitempty"`
}

type QuestState struct {
	Status            string            `json:"status,omitempty"`
	TriggerType       byte              `json:"trigger_type,omitempty"`
	ProgressValue     int64             `json:"progress_value,omitempty"`
	RewardSelectIndex int64             `json:"reward_select_index,omitempty"`
	Multiplier        int64             `json:"multiplier,omitempty"`
	UpdatedAt         time.Time         `json:"updated_at,omitempty"`
	Extra             map[string]string `json:"extra,omitempty"`
}

type QuestField string

const (
	QuestFieldStates   QuestField = "states"
	QuestFieldProgress QuestField = "progress"
)

func SaveQuestFields(ctx context.Context, repo QuestRepository, record QuestRecord, fields ...QuestField) error {
	return db.SaveFields(ctx, repo, record, QuestFields.Normalize, fields...)
}

func CloneQuest(record QuestRecord) QuestRecord {
	record.States = cloneQuestStateMap(record.States)
	record.Progress = cloneQuestStateMap(record.Progress)
	return record
}

func QuestKey(record QuestRecord) string {
	return strings.TrimSpace(record.CharacterID)
}

func cloneQuestStateMap(in map[int64]QuestState) map[int64]QuestState {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int64]QuestState, len(in))
	for key, value := range in {
		value.Extra = cloneStringMap(value.Extra)
		out[key] = value
	}
	return out
}
