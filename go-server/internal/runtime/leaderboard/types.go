package leaderboard

import (
	"context"
	"errors"
	"time"
)

var (
	ErrLeaderboardExists   = errors.New("leaderboard already exists")
	ErrLeaderboardNotFound = errors.New("leaderboard not found")
	ErrRecordNotFound      = errors.New("leaderboard record not found")
	ErrRecordTrimmed       = errors.New("leaderboard record was trimmed")
	ErrInvalidDefinition   = errors.New("leaderboard definition is invalid")
	ErrInvalidSubmission   = errors.New("leaderboard submission is invalid")
	ErrScoreOverflow       = errors.New("leaderboard score overflow")
	ErrManagerRequired     = errors.New("leaderboard manager is required")
)

const (
	SortDescending = "desc"
	SortAscending  = "asc"

	OperatorBest      = "best"
	OperatorSet       = "set"
	OperatorIncrement = "increment"
)

const (
	defaultListLimit    = 50
	defaultAroundLimit  = 5
	maxListLimit        = 500
	defaultHistoryLimit = 1000
)

type Runtime interface {
	Create(context.Context, Definition) (Definition, error)
	Delete(context.Context, string) (Definition, error)
	Definition(string) (Definition, bool)
	Definitions() []Definition
	Submit(context.Context, string, Submission) (Record, error)
	DeleteRecord(context.Context, string, string) (Record, error)
	Repair(context.Context, string, RepairRequest) (RepairReceipt, error)
	Record(string, string) (Record, bool)
	List(context.Context, string, ListOptions) ([]Record, error)
	MergedView(context.Context, MergedViewOptions) (MergedView, error)
	AroundOwner(context.Context, string, string, int) ([]Record, error)
	Rank(context.Context, string, string) (int, bool, error)
	Reset(context.Context, string) (Definition, error)
	Capture(context.Context, string, CaptureOptions) (Capture, error)
	History(context.Context, string, int) ([]HistoryEntry, error)
	Snapshot() Snapshot
}

type Options struct {
	Name          string
	Now           func() time.Time
	IDGenerator   func(prefix string) string
	HistoryLimit  int
	HistoryStore  HistoryStore
	HistoryStrict bool
}

type Definition struct {
	ID        string            `json:"id"`
	Title     string            `json:"title,omitempty"`
	SortOrder string            `json:"sort_order"`
	Operator  string            `json:"operator"`
	MaxSize   int               `json:"max_size,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type Submission struct {
	OwnerID  string            `json:"owner_id"`
	Score    int64             `json:"score"`
	Subscore int64             `json:"subscore,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Record struct {
	LeaderboardID string            `json:"leaderboard_id"`
	OwnerID       string            `json:"owner_id"`
	Score         int64             `json:"score"`
	Subscore      int64             `json:"subscore,omitempty"`
	Rank          int               `json:"rank,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

const (
	HistoryActionCreate       = "create"
	HistoryActionSubmit       = "submit"
	HistoryActionDeleteRecord = "delete_record"
	HistoryActionTrimRecord   = "trim_record"
	HistoryActionReset        = "reset"
	HistoryDeleteBoard        = "delete_leaderboard"
	HistoryActionRepairRecord = "repair_record"
	HistoryActionCapture      = "capture"
)

type HistoryEntry struct {
	Action        string            `json:"action"`
	LeaderboardID string            `json:"leaderboard_id"`
	OwnerID       string            `json:"owner_id,omitempty"`
	Record        *Record           `json:"record,omitempty"`
	Records       []Record          `json:"records,omitempty"`
	Definition    *Definition       `json:"definition,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	OperatorID    string            `json:"operator_id,omitempty"`
	RequestID     string            `json:"request_id,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	At            time.Time         `json:"at"`
}

type HistoryDetails struct {
	Reason     string            `json:"reason,omitempty"`
	OperatorID string            `json:"operator_id,omitempty"`
	RequestID  string            `json:"request_id,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Records    []Record          `json:"records,omitempty"`
}

type HistoryStore interface {
	Append(context.Context, HistoryEntry) error
	List(context.Context, string, int) ([]HistoryEntry, error)
}

type ListOptions struct {
	Offset int
	Limit  int
}

type MergedViewOptions struct {
	IDs         []string `json:"ids"`
	SortOrder   string   `json:"sort_order,omitempty"`
	Offset      int      `json:"offset,omitempty"`
	Limit       int      `json:"limit,omitempty"`
	DedupeOwner bool     `json:"dedupe_owner,omitempty"`
}

type MergedView struct {
	IDs         []string  `json:"ids"`
	SortOrder   string    `json:"sort_order"`
	Records     []Record  `json:"records"`
	RecordCount int       `json:"record_count"`
	GeneratedAt time.Time `json:"generated_at"`
}

type RepairRequest struct {
	OwnerID        string            `json:"owner_id"`
	Score          int64             `json:"score,omitempty"`
	Subscore       int64             `json:"subscore,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Delete         bool              `json:"delete,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	OperatorID     string            `json:"operator_id,omitempty"`
	RequestID      string            `json:"request_id,omitempty"`
	ResetCreatedAt bool              `json:"reset_created_at,omitempty"`
}

type RepairReceipt struct {
	LeaderboardID string            `json:"leaderboard_id"`
	OwnerID       string            `json:"owner_id"`
	Action        string            `json:"action"`
	Before        *Record           `json:"before,omitempty"`
	After         *Record           `json:"after,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	OperatorID    string            `json:"operator_id,omitempty"`
	RequestID     string            `json:"request_id,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	At            time.Time         `json:"at"`
}

type CaptureOptions struct {
	Limit      int               `json:"limit,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	OperatorID string            `json:"operator_id,omitempty"`
	RequestID  string            `json:"request_id,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type Capture struct {
	LeaderboardID string            `json:"leaderboard_id"`
	Records       []Record          `json:"records"`
	RecordCount   int               `json:"record_count"`
	Reason        string            `json:"reason,omitempty"`
	OperatorID    string            `json:"operator_id,omitempty"`
	RequestID     string            `json:"request_id,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	CapturedAt    time.Time         `json:"captured_at"`
}

type Snapshot struct {
	Name               string         `json:"name"`
	LeaderboardCount   int            `json:"leaderboard_count"`
	RecordCount        int            `json:"record_count"`
	RecordsByBoard     map[string]int `json:"records_by_board,omitempty"`
	HistoryStoreErrors int            `json:"history_store_errors,omitempty"`
	EventLogErrors     int            `json:"eventlog_errors,omitempty"`
}
