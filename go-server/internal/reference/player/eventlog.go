package player

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/eventlog"
)

const (
	// ProfileEventStream 是玩家档案事件写入 eventlog 的流名。
	ProfileEventStream = "profile"
	// ProfileEventTypeSaved 表示玩家档案保存事件。
	ProfileEventTypeSaved = "saved"
	// EventStateChanged 表示玩家在线状态变化事件。
	EventStateChanged = "state.changed"
	// ProfileEventTypeMutation 表示玩家档案关键字段变更事件。
	ProfileEventTypeMutation = "mutation.committed"
)

// ProfileEventAppender 是玩家模块写 profile outbox 的最小接口。
type ProfileEventAppender interface {
	Append(context.Context, eventlog.Event) (eventlog.Event, error)
}

// ProfileEventPayload 是玩家档案保存或状态变化事件的载荷。
type ProfileEventPayload struct {
	AccountID string    `json:"account_id"`
	RoleID    string    `json:"role_id,omitempty"`
	Name      string    `json:"name,omitempty"`
	Level     int       `json:"level,omitempty"`
	State     string    `json:"state,omitempty"`
	Version   int64     `json:"version"`
	Reason    string    `json:"reason,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	SavedAt   time.Time `json:"saved_at,omitempty"`
}

// MutationEventPayload 是玩家档案关键字段变更事件的载荷。
type MutationEventPayload struct {
	AccountID string         `json:"account_id"`
	RoleID    string         `json:"role_id,omitempty"`
	Operation string         `json:"operation"`
	Version   int64          `json:"version"`
	Fields    []ProfileField `json:"fields,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Mutation  map[string]any `json:"mutation,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
}

// UseEventLog 为玩家模块接入 eventlog，strict 为 true 时写事件失败会返回错误。
func (m *Module) UseEventLog(log *eventlog.Log, strict bool) *Module {
	return m.UseEventAppender(log, strict)
}

// UseEventAppender 为玩家模块接入自定义 profile 事件追加器。
func (m *Module) UseEventAppender(appender ProfileEventAppender, strict bool) *Module {
	if m == nil {
		return nil
	}
	m.eventsStrict = strict
	m.eventAsync = nil
	if appender != nil && !strict {
		async := newAsyncEvents(appender, m.logger, &m.eventLogErrors)
		m.events = async
		m.eventAsync = async
		return m
	}
	m.events = appender
	return m
}

// EventLogErrors 返回玩家模块写 eventlog 失败的累计次数。
func (m *Module) EventLogErrors() uint64 {
	if m == nil {
		return 0
	}
	return atomic.LoadUint64(&m.eventLogErrors)
}

func (m *Module) emitProfileSaved(ctx context.Context, profile Profile, reason string) error {
	return m.emitProfileEvent(ctx, ProfileEventTypeSaved, profile, reason)
}

func (m *Module) emitProfileChanged(ctx context.Context, profile Profile, reason string) error {
	return m.emitProfileEvent(ctx, EventStateChanged, profile, reason)
}

func (m *Module) emitProfileMutation(ctx context.Context, profile Profile, operation, reason string, fields []ProfileField, mutation map[string]any) error {
	if m == nil || m.events == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "profile.mutation"
	}
	reason = strings.TrimSpace(reason)
	payload, err := json.Marshal(MutationEventPayload{
		AccountID: profile.AccountID,
		RoleID:    profile.RoleID,
		Operation: operation,
		Version:   profile.Version,
		Fields:    normProfileFields(fields),
		Reason:    reason,
		Mutation:  cloneMutationMap(mutation),
		UpdatedAt: profile.UpdatedAt.UTC(),
	})
	if err != nil {
		return err
	}
	_, err = m.events.Append(ctx, eventlog.Event{
		Stream:         ProfileEventStream,
		Type:           ProfileEventTypeMutation,
		AggregateID:    profile.AccountID,
		IdempotencyKey: profileIdemKey(profile, operation, mutation),
		Payload:        payload,
		Headers: map[string]string{
			"account_id": profile.AccountID,
			"role_id":    profile.RoleID,
			"operation":  operation,
			"reason":     reason,
		},
	})
	if err == nil {
		return nil
	}
	atomic.AddUint64(&m.eventLogErrors, 1)
	if m.logger != nil {
		m.logger.Error("player profile mutation eventlog append failed", "account_id", profile.AccountID, "operation", operation, "error", err)
	}
	if m.eventsStrict {
		return err
	}
	return nil
}

func (m *Module) emitProfileEvent(ctx context.Context, eventType string, profile Profile, reason string) error {
	if m == nil || m.events == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		eventType = ProfileEventTypeSaved
	}
	reason = strings.TrimSpace(reason)
	payload, err := json.Marshal(ProfileEventPayload{
		AccountID: profile.AccountID,
		RoleID:    profile.RoleID,
		Name:      profile.Name,
		Level:     profile.Level,
		State:     profile.State,
		Version:   profile.Version,
		Reason:    reason,
		UpdatedAt: profile.UpdatedAt.UTC(),
		SavedAt:   profile.SavedAt.UTC(),
	})
	if err != nil {
		return err
	}
	_, err = m.events.Append(ctx, eventlog.Event{
		Stream:         ProfileEventStream,
		Type:           eventType,
		AggregateID:    profile.AccountID,
		IdempotencyKey: profileEventIdemKey(profile, eventType, reason),
		Payload:        payload,
		Headers: map[string]string{
			"account_id": profile.AccountID,
			"role_id":    profile.RoleID,
			"reason":     reason,
		},
	})
	if err == nil {
		return nil
	}
	atomic.AddUint64(&m.eventLogErrors, 1)
	if m.logger != nil {
		m.logger.Error("player profile eventlog append failed", "account_id", profile.AccountID, "event_type", eventType, "error", err)
	}
	if m.eventsStrict {
		return err
	}
	return nil
}

func profileEventIdemKey(profile Profile, eventType string, reason string) string {
	ts := profile.UpdatedAt.UTC().UnixNano()
	if ts == 0 {
		ts = profile.SavedAt.UTC().UnixNano()
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%d|%s", profile.AccountID, eventType, profile.Version, ts, reason)))
	return "profile:" + hex.EncodeToString(sum[:16])
}

func profileIdemKey(profile Profile, operation string, mutation map[string]any) string {
	ts := profile.UpdatedAt.UTC().UnixNano()
	mutationJSON, _ := json.Marshal(mutation)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%d|%s", profile.AccountID, operation, profile.Version, ts, mutationJSON)))
	return "profile-mut:" + hex.EncodeToString(sum[:16])
}

func cloneMutationMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
