package hotupdate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrVersionRequired = errors.New("hot update version is required")
	ErrSourceRequired  = errors.New("hot update source is required")
	ErrTargetRequired  = errors.New("hot update target is required")
	ErrApplyRequired   = errors.New("hot update applier is required")
	ErrActionRequired  = errors.New("hot update action is required")
	ErrRestoreRequired = errors.New("hot update restorer is required")
	ErrProgressInvalid = errors.New("hot update progress is invalid")
	ErrControlClosed   = errors.New("hot update control is closed")
)

type Action string

const (
	ActionApply   Action = "apply"
	ActionRestore Action = "restore"
)

type Stage string

const (
	StageAccepted    Stage = "accepted"
	StageDownloading Stage = "downloading"
	StageScheduled   Stage = "scheduled"
	StageApplying    Stage = "applying"
	StageApplied     Stage = "applied"
	StageRestoring   Stage = "restoring"
	StageRestored    Stage = "restored"
	StageFailed      Stage = "failed"
	StageSkipped     Stage = "skipped"
)

type Intent struct {
	Action      Action    `json:"action,omitempty"`
	Version     string    `json:"version"`
	SourceURI   string    `json:"source_uri"`
	Checksum    string    `json:"checksum,omitempty"`
	AvailableAt time.Time `json:"available_at,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	RequestedBy string    `json:"requested_by,omitempty"`
	Sequence    int64     `json:"sequence,omitempty"`
}

type Package struct {
	Version      string `json:"version"`
	SourceURI    string `json:"source_uri"`
	Directory    string `json:"directory"`
	Checksum     string `json:"checksum"`
	Bytes        int64  `json:"bytes"`
	SignatureAlg string `json:"signature_alg,omitempty"`
	Signature    string `json:"signature,omitempty"`
}

type ApplyResult struct {
	Version string         `json:"version"`
	Applied []string       `json:"applied,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

type Progress struct {
	Target    string    `json:"target"`
	NodeID    string    `json:"node_id"`
	Version   string    `json:"version"`
	Stage     Stage     `json:"stage"`
	Status    string    `json:"status"`
	Detail    string    `json:"detail,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Snapshot struct {
	Target        string    `json:"target"`
	NodeID        string    `json:"node_id"`
	Started       bool      `json:"started"`
	LastIntent    Intent    `json:"last_intent,omitempty"`
	LastProgress  Progress  `json:"last_progress,omitempty"`
	LastPackage   Package   `json:"last_package,omitempty"`
	LastApplied   time.Time `json:"last_applied,omitempty"`
	AutoRestoreAt time.Time `json:"auto_restore_at,omitempty"`
	Failures      int64     `json:"failures"`
}

type Control interface {
	Publish(context.Context, string, Intent) error
	Watch(context.Context, string) (<-chan Intent, error)
	Report(context.Context, Progress) error
	Snapshot() ControlSnapshot
	Close() error
}

type ControlSnapshot struct {
	Intents  map[string]Intent     `json:"intents,omitempty"`
	Progress map[string][]Progress `json:"progress,omitempty"`
	Watchers map[string]int        `json:"watchers,omitempty"`
	Closed   bool                  `json:"closed"`
}

type Source interface {
	Fetch(context.Context, Intent, string) (Package, error)
}

type Applier interface {
	Apply(context.Context, Package) (ApplyResult, error)
}

type Restorer interface {
	Restore(context.Context, Intent) (ApplyResult, error)
}

func normalizeTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", ErrTargetRequired
	}
	return target, nil
}

func normalizeIntent(intent Intent) (Intent, error) {
	intent.Action = normalizeAction(intent.Action)
	intent.Version = strings.TrimSpace(intent.Version)
	intent.SourceURI = strings.TrimSpace(intent.SourceURI)
	intent.Checksum = strings.TrimSpace(intent.Checksum)
	intent.Reason = strings.TrimSpace(intent.Reason)
	intent.RequestedBy = strings.TrimSpace(intent.RequestedBy)
	if intent.Action == "" {
		return Intent{}, ErrActionRequired
	}
	if intent.Action != ActionApply && intent.Action != ActionRestore {
		return Intent{}, fmt.Errorf("%w: %s", ErrActionRequired, intent.Action)
	}
	if intent.Action == ActionRestore && intent.Version == "" {
		intent.Version = "restore"
	}
	if intent.Version == "" {
		return Intent{}, ErrVersionRequired
	}
	if intent.Action == ActionApply && intent.SourceURI == "" {
		return Intent{}, ErrSourceRequired
	}
	return intent, nil
}

func normalizeAction(action Action) Action {
	action = Action(strings.TrimSpace(string(action)))
	if action == "" {
		return ActionApply
	}
	return action
}

func normalizeProgress(progress Progress, now func() time.Time) (Progress, error) {
	target, err := normalizeTarget(progress.Target)
	if err != nil {
		return Progress{}, err
	}
	progress.Target = target
	progress.NodeID = strings.TrimSpace(progress.NodeID)
	progress.Version = strings.TrimSpace(progress.Version)
	progress.Stage = Stage(strings.TrimSpace(string(progress.Stage)))
	progress.Status = strings.TrimSpace(progress.Status)
	progress.Detail = strings.TrimSpace(progress.Detail)
	if progress.NodeID == "" {
		return Progress{}, fmt.Errorf("%w: node_id is required", ErrProgressInvalid)
	}
	if progress.Version == "" {
		return Progress{}, ErrVersionRequired
	}
	if !knownStage(progress.Stage) {
		return Progress{}, fmt.Errorf("%w: unsupported stage %q", ErrProgressInvalid, progress.Stage)
	}
	if progress.Status == "" {
		progress.Status = "ok"
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if progress.UpdatedAt.IsZero() {
		progress.UpdatedAt = now().UTC()
	} else {
		progress.UpdatedAt = progress.UpdatedAt.UTC()
	}
	return progress, nil
}

func knownStage(stage Stage) bool {
	switch stage {
	case StageAccepted, StageDownloading, StageScheduled, StageApplying, StageApplied, StageRestoring, StageRestored, StageFailed, StageSkipped:
		return true
	default:
		return false
	}
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
