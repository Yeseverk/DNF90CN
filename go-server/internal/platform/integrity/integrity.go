package integrity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var (
	ErrActionSubjectRequired = errors.New("integrity action subject is required")
	ErrActionNameRequired    = errors.New("integrity action name is required")
	ErrValidatorRequired     = errors.New("integrity validator is required")
	ErrReplayDigestRequired  = errors.New("integrity replay digest is required")
)

type Action struct {
	SubjectID string            `json:"subject_id"`
	SessionID string            `json:"session_id,omitempty"`
	Name      string            `json:"name"`
	Sequence  uint64            `json:"sequence,omitempty"`
	Payload   []byte            `json:"payload,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	At        time.Time         `json:"at,omitempty"`
}

type Verdict struct {
	Allowed  bool              `json:"allowed"`
	Risk     int               `json:"risk"`
	Reasons  []string          `json:"reasons,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ActionValidator 必须可并发调用；实现应尊重 ctx，不能把外部风控超时变成主链路无限等待。
type ActionValidator interface {
	ValidateAction(context.Context, Action) (Verdict, error)
}

type ActionValidatorFunc func(context.Context, Action) (Verdict, error)

func (f ActionValidatorFunc) ValidateAction(ctx context.Context, action Action) (Verdict, error) {
	if f == nil {
		return Verdict{}, ErrValidatorRequired
	}
	return f(ctx, action)
}

type ReplayDigest struct {
	SubjectID string `json:"subject_id"`
	SessionID string `json:"session_id"`
	Sequence  uint64 `json:"sequence"`
	Digest    string `json:"digest"`
}

type ReplayDigestProvider interface {
	DigestAction(context.Context, Action) (ReplayDigest, error)
}

// RiskScoreProvider 返回值约定为 0..100；超出范围的项目侧实现应在适配层归一化。
type RiskScoreProvider interface {
	ScoreRisk(context.Context, Action) (int, error)
}

func ValidateActionShape(action Action) error {
	if strings.TrimSpace(action.SubjectID) == "" {
		return ErrActionSubjectRequired
	}
	if strings.TrimSpace(action.Name) == "" {
		return ErrActionNameRequired
	}
	return nil
}

func Validate(ctx context.Context, validator ActionValidator, action Action) (Verdict, error) {
	if validator == nil {
		return Verdict{}, ErrValidatorRequired
	}
	if err := ValidateActionShape(action); err != nil {
		return Verdict{}, err
	}
	return validator.ValidateAction(ctx, action)
}

type SHA256DigestProvider struct{}

func (SHA256DigestProvider) DigestAction(ctx context.Context, action Action) (ReplayDigest, error) {
	if err := ctxErr(ctx); err != nil {
		return ReplayDigest{}, err
	}
	if err := ValidateActionShape(action); err != nil {
		return ReplayDigest{}, err
	}
	sum := sha256.Sum256(action.Payload)
	return ReplayDigest{SubjectID: strings.TrimSpace(action.SubjectID), SessionID: strings.TrimSpace(action.SessionID), Sequence: action.Sequence, Digest: hex.EncodeToString(sum[:])}, nil
}

func MergeVerdicts(items ...Verdict) Verdict {
	out := Verdict{Allowed: true}
	reasons := make(map[string]struct{})
	for _, item := range items {
		if !item.Allowed {
			out.Allowed = false
		}
		if item.Risk > out.Risk {
			out.Risk = item.Risk
		}
		for _, reason := range item.Reasons {
			reason = strings.TrimSpace(reason)
			if reason == "" {
				continue
			}
			if _, ok := reasons[reason]; ok {
				continue
			}
			reasons[reason] = struct{}{}
			out.Reasons = append(out.Reasons, reason)
		}
		if len(item.Metadata) > 0 {
			if out.Metadata == nil {
				out.Metadata = make(map[string]string, len(item.Metadata))
			}
			for key, value := range item.Metadata {
				if strings.TrimSpace(key) != "" {
					out.Metadata[key] = value
				}
			}
		}
	}
	return out
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
