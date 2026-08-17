package nativepatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrMemoryControlStore       = errors.New("native patch memory control store is required")
	ErrMemoryControlStoreClosed = errors.New("native patch memory control store is closed")
)

type ControlAction string

const (
	ControlApply   ControlAction = "apply"
	ControlRestore ControlAction = "restore"
)

type ControlIntent struct {
	Action      ControlAction `json:"action"`
	Target      string        `json:"target"`
	Version     string        `json:"version"`
	PackageDir  string        `json:"package_dir,omitempty"`
	RequestedBy string        `json:"requested_by,omitempty"`
	Reason      string        `json:"reason,omitempty"`
	AvailableAt time.Time     `json:"available_at,omitempty"`
	Sequence    int64         `json:"sequence,omitempty"`
}

type ControlProgress struct {
	Target    string        `json:"target"`
	NodeID    string        `json:"node_id"`
	Version   string        `json:"version"`
	Action    ControlAction `json:"action"`
	Status    string        `json:"status"`
	Detail    string        `json:"detail,omitempty"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type ControlStore interface {
	Publish(context.Context, string, ControlIntent) error
	Watch(context.Context, string) (<-chan ControlIntent, error)
	Report(context.Context, ControlProgress) error
	ListProgress(context.Context, string) ([]ControlProgress, error)
}

type MemoryControlStore struct {
	mu        sync.RWMutex
	intents   map[string]ControlIntent
	progress  map[string][]ControlProgress
	watchers  map[string]map[int]chan ControlIntent
	nextWatch int
	now       func() time.Time
	done      chan struct{}
	closed    bool
}

func NewMemoryControlStore() *MemoryControlStore {
	return NewMemoryCtrlClock(time.Now)
}

func NewMemoryCtrlClock(now func() time.Time) *MemoryControlStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryControlStore{
		intents:  make(map[string]ControlIntent),
		progress: make(map[string][]ControlProgress),
		watchers: make(map[string]map[int]chan ControlIntent),
		now:      now,
		done:     make(chan struct{}),
	}
}

func (s *MemoryControlStore) Publish(ctx context.Context, target string, intent ControlIntent) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := s.ready(); err != nil {
		return err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("native patch control target is required")
	}
	intent.Target = target
	if intent.Action == "" {
		intent.Action = ControlApply
	}
	if intent.Action != ControlApply && intent.Action != ControlRestore {
		return fmt.Errorf("unsupported native patch control action %s", intent.Action)
	}
	if strings.TrimSpace(intent.Version) == "" {
		return ErrVersionRequired
	}
	if intent.Action == ControlApply && strings.TrimSpace(intent.PackageDir) == "" {
		return ErrPlanRequired
	}
	// 内存控制面只保留每个 target 的最新意图，适合本地演练，不提供跨进程持久化。
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrMemoryControlStoreClosed
	}
	s.intents[target] = intent
	for _, watcher := range s.watchers[target] {
		// watcher 通道容量为 1；慢消费者只保留最新意图，避免执行过期补丁。
		sendLatestIntent(watcher, intent)
	}
	s.mu.Unlock()
	return nil
}

func sendLatestIntent(ch chan ControlIntent, intent ControlIntent) {
	select {
	case ch <- intent:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- intent:
	default:
	}
}

func (s *MemoryControlStore) Watch(ctx context.Context, target string) (<-chan ControlIntent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := s.ready(); err != nil {
		return nil, err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("native patch control target is required")
	}
	ch := make(chan ControlIntent, 1)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		close(ch)
		return ch, nil
	}
	s.nextWatch++
	id := s.nextWatch
	if s.watchers[target] == nil {
		s.watchers[target] = make(map[int]chan ControlIntent)
	}
	s.watchers[target][id] = ch
	if intent, ok := s.intents[target]; ok {
		// 先发送当前快照，再等待后续发布，避免节点重启后错过最近一次补丁指令。
		ch <- intent
	}
	s.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
		case <-s.done:
		}
		s.mu.Lock()
		removed := false
		if watchers := s.watchers[target]; watchers != nil {
			if _, ok := watchers[id]; ok {
				delete(watchers, id)
				removed = true
			}
			if len(watchers) == 0 {
				delete(s.watchers, target)
			}
		}
		if removed {
			close(ch)
		}
		s.mu.Unlock()
	}()
	return ch, nil
}

func (s *MemoryControlStore) Report(ctx context.Context, progress ControlProgress) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	now, err := s.ready()
	if err != nil {
		return err
	}
	progress.Target = strings.TrimSpace(progress.Target)
	progress.NodeID = strings.TrimSpace(progress.NodeID)
	if progress.Target == "" || progress.NodeID == "" {
		return fmt.Errorf("native patch progress target and node_id are required")
	}
	if progress.UpdatedAt.IsZero() {
		progress.UpdatedAt = now().UTC()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrMemoryControlStoreClosed
	}
	s.progress[progress.Target] = append(s.progress[progress.Target], progress)
	s.mu.Unlock()
	return nil
}

func (s *MemoryControlStore) ListProgress(ctx context.Context, target string) ([]ControlProgress, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := s.ready(); err != nil {
		return nil, err
	}
	target = strings.TrimSpace(target)
	s.mu.RLock()
	out := append([]ControlProgress(nil), s.progress[target]...)
	s.mu.RUnlock()
	return out, nil
}

// Close 关闭内存控制面并收敛所有 watcher。
func (s *MemoryControlStore) Close() error {
	if s == nil {
		return nil
	}
	if _, err := s.ready(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.done == nil {
		s.done = make(chan struct{})
	}
	close(s.done)
	for target, watchers := range s.watchers {
		for id, ch := range watchers {
			delete(watchers, id)
			close(ch)
		}
		delete(s.watchers, target)
	}
	return nil
}

func (s *MemoryControlStore) ready() (func() time.Time, error) {
	if s == nil {
		return nil, ErrMemoryControlStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.intents == nil {
		s.intents = make(map[string]ControlIntent)
	}
	if s.progress == nil {
		s.progress = make(map[string][]ControlProgress)
	}
	if s.watchers == nil {
		s.watchers = make(map[string]map[int]chan ControlIntent)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.done == nil {
		s.done = make(chan struct{})
	}
	return s.now, nil
}
