package admincmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrMissingOperation      = errors.New("admin command operation is required")
	ErrMissingActor          = errors.New("admin command actor is required")
	ErrMissingTarget         = errors.New("admin command target is required")
	ErrMissingReason         = errors.New("admin command reason is required")
	ErrMissingIdempotencyKey = errors.New("admin command idempotency key is required")
	ErrMissingConfirmation   = errors.New("admin command confirmation is required")
)

type Command struct {
	Operation      string         `json:"operation"`
	Scope          string         `json:"scope,omitempty"`
	Environment    string         `json:"environment,omitempty"`
	ShardID        string         `json:"shard_id,omitempty"`
	Target         string         `json:"target,omitempty"`
	Actor          string         `json:"actor"`
	Reason         string         `json:"reason,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Confirmation   string         `json:"confirmation,omitempty"`
	OfflineSafe    bool           `json:"offline_safe,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
}

type Policy struct {
	RequireActor          bool
	RequireTarget         bool
	RequireReason         bool
	RequireIdempotencyKey bool
	RequireConfirmation   bool
}

type Receipt struct {
	ID             string    `json:"id"`
	Operation      string    `json:"operation"`
	Scope          string    `json:"scope,omitempty"`
	Environment    string    `json:"environment,omitempty"`
	ShardID        string    `json:"shard_id,omitempty"`
	Target         string    `json:"target,omitempty"`
	Actor          string    `json:"actor,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	ParamsHash     string    `json:"params_hash,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

func DangerousPolicy() Policy {
	return Policy{
		RequireActor:          true,
		RequireTarget:         true,
		RequireReason:         true,
		RequireIdempotencyKey: true,
		RequireConfirmation:   true,
	}
}

func Validate(command Command, policy Policy) error {
	command = Normalize(command)
	if command.Operation == "" {
		return ErrMissingOperation
	}
	if policy.RequireActor && command.Actor == "" {
		return ErrMissingActor
	}
	if policy.RequireTarget && command.Target == "" {
		return ErrMissingTarget
	}
	if policy.RequireReason && command.Reason == "" {
		return ErrMissingReason
	}
	if policy.RequireIdempotencyKey && command.IdempotencyKey == "" {
		return ErrMissingIdempotencyKey
	}
	if policy.RequireConfirmation && command.Confirmation == "" {
		return ErrMissingConfirmation
	}
	return nil
}

func Normalize(command Command) Command {
	command.Operation = strings.TrimSpace(command.Operation)
	command.Scope = strings.Trim(strings.ToLower(strings.TrimSpace(command.Scope)), ":")
	command.Environment = strings.TrimSpace(command.Environment)
	command.ShardID = strings.TrimSpace(command.ShardID)
	command.Target = strings.TrimSpace(command.Target)
	command.Actor = strings.TrimSpace(command.Actor)
	command.Reason = strings.TrimSpace(command.Reason)
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	command.Confirmation = strings.TrimSpace(command.Confirmation)
	return command
}

func ParamsHash(params map[string]any) (string, error) {
	if len(params) == 0 {
		return "", nil
	}
	payload, err := json.Marshal(canonicalParams(params))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalParams(params map[string]any) map[string]any {
	out := make(map[string]any, len(params))
	type paramPair struct {
		key   string
		value any
	}
	pairs := make([]paramPair, 0, len(params))
	for key, value := range params {
		normalized := strings.TrimSpace(key)
		if normalized == "" {
			continue
		}
		pairs = append(pairs, paramPair{key: normalized, value: value})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })
	for _, pair := range pairs {
		out[pair.key] = canonicalParamValue(pair.value)
	}
	return out
}

func canonicalParamValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return canonicalParams(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = canonicalParamValue(item)
		}
		return out
	default:
		return value
	}
}

func NewReceipt(command Command, status string, now time.Time) (Receipt, error) {
	command = Normalize(command)
	if command.Operation == "" {
		return Receipt{}, ErrMissingOperation
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	paramsHash, err := ParamsHash(command.Params)
	if err != nil {
		return Receipt{}, err
	}
	idSeed := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s", command.Operation, command.Scope, command.Environment, command.ShardID, command.Target, command.IdempotencyKey, paramsHash)
	sum := sha256.Sum256([]byte(idSeed))
	return Receipt{
		ID:             hex.EncodeToString(sum[:16]),
		Operation:      command.Operation,
		Scope:          command.Scope,
		Environment:    command.Environment,
		ShardID:        command.ShardID,
		Target:         command.Target,
		Actor:          command.Actor,
		IdempotencyKey: command.IdempotencyKey,
		ParamsHash:     paramsHash,
		Status:         strings.TrimSpace(status),
		CreatedAt:      now,
	}, nil
}
