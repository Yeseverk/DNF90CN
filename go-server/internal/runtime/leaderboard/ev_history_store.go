package leaderboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/eventlog"
)

var (
	ErrNilEventLogHistoryStore = errors.New("leaderboard eventlog history store is required")
	ErrNilEventLog             = errors.New("leaderboard eventlog is required")
)

const (
	LeaderboardEventStream             = "leaderboard"
	DefaultLeaderboardHistoryEventType = "history.appended"
	histUndoTimeout                    = 3 * time.Second
)

type HistoryStoreOptions struct {
	Stream string
	Type   string
	Strict bool
}

type EventLogHistoryStore struct {
	next   HistoryStore
	log    *eventlog.Log
	stream string
	typ    string
	strict bool
	errors uint64
}

type evErrorReporter interface {
	EventLogErrors() uint64
}

type histErrorReporter interface {
	HistoryErrors() uint64
}

type histUndo func(context.Context) error

type histUndoer interface {
	appendWithRollback(context.Context, HistoryEntry) (histUndo, error)
}

func NewEventLogHistoryStore(next HistoryStore, log *eventlog.Log, options HistoryStoreOptions) (*EventLogHistoryStore, error) {
	if next == nil {
		return nil, ErrNilEventLogHistoryStore
	}
	if log == nil {
		return nil, ErrNilEventLog
	}
	stream := strings.TrimSpace(options.Stream)
	if stream == "" {
		stream = LeaderboardEventStream
	}
	typ := strings.TrimSpace(options.Type)
	if typ == "" {
		typ = DefaultLeaderboardHistoryEventType
	}
	return &EventLogHistoryStore{
		next:   next,
		log:    log,
		stream: stream,
		typ:    typ,
		strict: options.Strict,
	}, nil
}

type eventHistoryStore struct {
	log    *eventlog.Log
	stream string
	typ    string
}

func newEventHistoryStore(log *eventlog.Log, stream string, typ string) *eventHistoryStore {
	return &eventHistoryStore{
		log:    log,
		stream: normalizeEventStream(stream),
		typ:    normHistoryEventType(typ),
	}
}

func (s *EventLogHistoryStore) Append(ctx context.Context, entry HistoryEntry) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.next == nil {
		return ErrNilEventLogHistoryStore
	}
	if s.log == nil {
		return ErrNilEventLog
	}
	entry = normHistoryEntry(entry)
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	rollback, err := appendHist(ctx, s.next, entry)
	if err != nil {
		return err
	}
	_, err = s.log.Append(ctx, eventlog.Event{
		Stream:         s.stream,
		Type:           s.typ,
		AggregateID:    entry.LeaderboardID,
		IdempotencyKey: historyIdemKey(entry, payload),
		Payload:        payload,
		Headers: map[string]string{
			"action":         entry.Action,
			"leaderboard_id": entry.LeaderboardID,
			"owner_id":       entry.OwnerID,
		},
	})
	if err == nil {
		return nil
	}
	atomic.AddUint64(&s.errors, 1)
	if s.strict {
		if rollbackErr := undoHist(ctx, rollback); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	return nil
}

func (s *EventLogHistoryStore) List(ctx context.Context, leaderboardID string, limit int) ([]HistoryEntry, error) {
	if s == nil || s.next == nil {
		return nil, ErrNilEventLogHistoryStore
	}
	return s.next.List(ctx, leaderboardID, limit)
}

func (s *EventLogHistoryStore) EventLogErrors() uint64 {
	if s == nil {
		return 0
	}
	return atomic.LoadUint64(&s.errors)
}

func (s *eventHistoryStore) Append(ctx context.Context, entry HistoryEntry) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.log == nil {
		return ErrNilEventLog
	}
	entry = normHistoryEntry(entry)
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = s.log.Append(ctx, eventlog.Event{
		Stream:         s.stream,
		Type:           s.typ,
		AggregateID:    entry.LeaderboardID,
		IdempotencyKey: historyIdemKey(entry, payload),
		Payload:        payload,
		Headers: map[string]string{
			"action":         entry.Action,
			"leaderboard_id": entry.LeaderboardID,
			"owner_id":       entry.OwnerID,
		},
	})
	return err
}

func (s *eventHistoryStore) List(context.Context, string, int) ([]HistoryEntry, error) {
	return nil, ErrNilEventLogHistoryStore
}

func historyStoreErrors(store HistoryStore) uint64 {
	reporter, ok := store.(evErrorReporter)
	if !ok {
		return 0
	}
	return reporter.EventLogErrors()
}

func historyErrors(store HistoryStore) uint64 {
	reporter, ok := store.(histErrorReporter)
	if !ok {
		return 0
	}
	return reporter.HistoryErrors()
}

func appendHist(ctx context.Context, store HistoryStore, entry HistoryEntry) (histUndo, error) {
	if rollbacker, ok := store.(histUndoer); ok {
		return rollbacker.appendWithRollback(ctx, entry)
	}
	if err := store.Append(ctx, entry); err != nil {
		return nil, err
	}
	return nil, nil
}

func undoHist(parent context.Context, rollback histUndo) error {
	if rollback == nil {
		return nil
	}
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(base, histUndoTimeout)
	defer cancel()
	return rollback(ctx)
}

func normHistoryEntry(entry HistoryEntry) HistoryEntry {
	entry = cloneHistoryEntry(entry)
	entry.Action = strings.TrimSpace(entry.Action)
	entry.LeaderboardID = strings.TrimSpace(entry.LeaderboardID)
	entry.OwnerID = strings.TrimSpace(entry.OwnerID)
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	} else {
		entry.At = entry.At.UTC()
	}
	return entry
}

func historyIdemKey(entry HistoryEntry, payload []byte) string {
	raw := fmt.Sprintf("%s|%s|%s|%d|%x",
		entry.LeaderboardID,
		entry.Action,
		entry.OwnerID,
		entry.At.UTC().UnixNano(),
		payload,
	)
	hash := sha256.Sum256([]byte(raw))
	return "leaderboard:history:" + hex.EncodeToString(hash[:16])
}
