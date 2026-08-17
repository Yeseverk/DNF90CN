package sessioncontract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

var ErrAuthStateEventInvalid = errors.New("auth state event is invalid")

type AuthState string

const (
	AuthStateBound      AuthState = "bound"
	AuthStateOnline     AuthState = "online"
	AuthStateOffline    AuthState = "offline"
	AuthStateKicked     AuthState = "kicked"
	AuthStateBanned     AuthState = "banned"
	AuthStateRestricted AuthState = "restricted"
	AuthStateDraining   AuthState = "draining"
)

// AuthStateEvent 是龙恒向外部账号/认证控制面回写在线状态的通用事件。
// 它只携带通用身份和状态，不绑定任何工作室账号库、渠道 SDK 或风控供应商。
type AuthStateEvent struct {
	AccountID     string            `json:"account_id"`
	SessionID     string            `json:"session_id,omitempty"`
	GatewayNodeID string            `json:"gateway_node_id,omitempty"`
	LogicNodeID   string            `json:"logic_node_id,omitempty"`
	State         AuthState         `json:"state"`
	Reason        string            `json:"reason,omitempty"`
	At            time.Time         `json:"at"`
	TraceID       string            `json:"trace_id,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type AuthStateNotifier interface {
	NotifyAuthState(context.Context, AuthStateEvent) error
}

func NotifyAuthState(ctx context.Context, notifier AuthStateNotifier, event AuthStateEvent) error {
	if notifier == nil {
		return nil
	}
	return notifier.NotifyAuthState(ctx, event)
}

type MemoryAuthStateNotifier struct {
	mu        sync.Mutex
	maxEvents int
	events    []AuthStateEvent
}

func NewMemoryAuthStateNotifier(maxEvents int) *MemoryAuthStateNotifier {
	return &MemoryAuthStateNotifier{maxEvents: maxEvents}
}

func (n *MemoryAuthStateNotifier) NotifyAuthState(ctx context.Context, event AuthStateEvent) error {
	if n == nil {
		return nil
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	event, err := normAuthEvent(event, time.Now().UTC())
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.events = append(n.events, event)
	if n.maxEvents > 0 && len(n.events) > n.maxEvents {
		n.events = append([]AuthStateEvent(nil), n.events[len(n.events)-n.maxEvents:]...)
	}
	n.mu.Unlock()
	return nil
}

func (n *MemoryAuthStateNotifier) Events() []AuthStateEvent {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]AuthStateEvent, len(n.events))
	for idx, event := range n.events {
		out[idx] = cloneAuthStateEvent(event)
	}
	return out
}

type AuthNotifierOptions struct {
	Endpoint string
	Token    string
	Headers  map[string]string
	Client   *http.Client
	Timeout  time.Duration
	Now      func() time.Time
}

type HTTPAuthStateNotifier struct {
	endpoint string
	token    string
	headers  map[string]string
	client   *http.Client
	timeout  time.Duration
	now      func() time.Time
}

func NewHTTPAuthStateNotifier(options AuthNotifierOptions) (*HTTPAuthStateNotifier, error) {
	endpoint := strings.TrimSpace(options.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("%w: endpoint is required", ErrAuthStateEventInvalid)
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &HTTPAuthStateNotifier{
		endpoint: endpoint,
		token:    strings.TrimSpace(options.Token),
		headers:  cloneStringMap(options.Headers),
		client:   client,
		timeout:  timeout,
		now:      now,
	}, nil
}

func (n *HTTPAuthStateNotifier) NotifyAuthState(ctx context.Context, event AuthStateEvent) error {
	if n == nil {
		return nil
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	event, err := normAuthEvent(event, n.now().UTC())
	if err != nil {
		return err
	}
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal auth state event: %w", err)
	}
	ctx, cancel := context.WithTimeout(contextOrBackground(ctx), n.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create auth state request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if n.token != "" {
		req.Header.Set("Authorization", "Bearer "+n.token)
	}
	for key, value := range n.headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send auth state request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("auth state request failed: status=%d", resp.StatusCode)
	}
	return nil
}

func normAuthEvent(event AuthStateEvent, now time.Time) (AuthStateEvent, error) {
	event.AccountID = strings.TrimSpace(event.AccountID)
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.GatewayNodeID = strings.TrimSpace(event.GatewayNodeID)
	event.LogicNodeID = strings.TrimSpace(event.LogicNodeID)
	event.Reason = strings.TrimSpace(event.Reason)
	event.TraceID = strings.TrimSpace(event.TraceID)
	event.State = AuthState(strings.TrimSpace(string(event.State)))
	if event.AccountID == "" || event.State == "" {
		return AuthStateEvent{}, fmt.Errorf("%w: account_id and state are required", ErrAuthStateEventInvalid)
	}
	if !validAuthState(event.State) {
		return AuthStateEvent{}, fmt.Errorf("%w: unsupported state %q", ErrAuthStateEventInvalid, event.State)
	}
	if event.At.IsZero() {
		event.At = now
	} else {
		event.At = event.At.UTC()
	}
	event.Metadata = cloneStringMap(event.Metadata)
	return event, nil
}

func validAuthState(state AuthState) bool {
	switch state {
	case AuthStateBound, AuthStateOnline, AuthStateOffline, AuthStateKicked, AuthStateBanned, AuthStateRestricted, AuthStateDraining:
		return true
	default:
		return false
	}
}

func cloneAuthStateEvent(event AuthStateEvent) AuthStateEvent {
	event.Metadata = cloneStringMap(event.Metadata)
	return event
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
