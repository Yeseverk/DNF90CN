package hotupdate

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"
)

var ErrControlRequired = errors.New("hot update control is required")

type MemoryControl struct {
	mu       sync.RWMutex
	intents  map[string]Intent
	keys     map[string]string
	progress map[string][]Progress
	watchers map[string]map[chan Intent]struct{}
	done     chan struct{}
	closed   bool
}

func NewMemoryControl() *MemoryControl {
	return &MemoryControl{
		intents:  make(map[string]Intent),
		keys:     make(map[string]string),
		progress: make(map[string][]Progress),
		watchers: make(map[string]map[chan Intent]struct{}),
		done:     make(chan struct{}),
	}
}

func (c *MemoryControl) Publish(ctx context.Context, target string, intent Intent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := c.ready(); err != nil {
		return err
	}
	target, err := normalizeTarget(target)
	if err != nil {
		return err
	}
	intent, err = normalizeIntent(intent)
	if err != nil {
		return err
	}
	key := intentIdempotencyKey(target, intent)
	// 内存控制面只保留每个 target 的最新 intent，适合本地演练，不适合多节点生产推进。
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrControlClosed
	}
	if existingKey, ok := c.keys[target]; ok && existingKey == key {
		existing := c.intents[target]
		if !reflect.DeepEqual(existing, intent) {
			c.mu.Unlock()
			return ErrControlConflict
		}
		c.mu.Unlock()
		return nil
	}
	c.intents[target] = intent
	c.keys[target] = key
	for ch := range c.watchers[target] {
		sendLatestIntent(ch, intent)
	}
	c.mu.Unlock()
	return nil
}

func (c *MemoryControl) Watch(ctx context.Context, target string) (<-chan Intent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.ready(); err != nil {
		return nil, err
	}
	target, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}
	// watcher channel 容量为 1，新 intent 会覆盖旧待处理通知，避免慢 watcher 堆积内存。
	ch := make(chan Intent, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		close(ch)
		return ch, nil
	}
	if c.watchers[target] == nil {
		c.watchers[target] = make(map[chan Intent]struct{})
	}
	c.watchers[target][ch] = struct{}{}
	if intent, ok := c.intents[target]; ok {
		ch <- intent
	}
	c.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
		case <-c.done:
		}
		c.mu.Lock()
		if watchers := c.watchers[target]; watchers != nil {
			if _, ok := watchers[ch]; ok {
				delete(watchers, ch)
				close(ch)
			}
			if len(watchers) == 0 {
				delete(c.watchers, target)
			}
		}
		c.mu.Unlock()
	}()
	return ch, nil
}

func (c *MemoryControl) Report(ctx context.Context, progress Progress) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := c.ready(); err != nil {
		return err
	}
	progress, err := normalizeProgress(progress, time.Now)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrControlClosed
	}
	c.progress[progress.Target] = append(c.progress[progress.Target], progress)
	c.mu.Unlock()
	return nil
}

func sendLatestIntent(ch chan Intent, intent Intent) {
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

func (c *MemoryControl) Snapshot() ControlSnapshot {
	if c == nil {
		return ControlSnapshot{}
	}
	c.mu.RLock()
	intents := make(map[string]Intent, len(c.intents))
	for target, intent := range c.intents {
		intents[target] = intent
	}
	progress := make(map[string][]Progress, len(c.progress))
	for target, items := range c.progress {
		progress[target] = append([]Progress(nil), items...)
	}
	watchers := make(map[string]int, len(c.watchers))
	for target, list := range c.watchers {
		watchers[target] = len(list)
	}
	snapshot := ControlSnapshot{Intents: intents, Progress: progress, Watchers: watchers, Closed: c.closed}
	c.mu.RUnlock()
	if len(snapshot.Intents) == 0 {
		snapshot.Intents = nil
	}
	if len(snapshot.Progress) == 0 {
		snapshot.Progress = nil
	}
	if len(snapshot.Watchers) == 0 {
		snapshot.Watchers = nil
	}
	return snapshot
}

func (c *MemoryControl) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	// Close 会关闭所有 watcher；之后 Publish/Watch 均拒绝，防止停机后继续推进热更。
	if c.done == nil {
		c.done = make(chan struct{})
	}
	close(c.done)
	for target, watchers := range c.watchers {
		for ch := range watchers {
			close(ch)
		}
		delete(c.watchers, target)
	}
	c.mu.Unlock()
	return nil
}

func (c *MemoryControl) ready() error {
	if c == nil {
		return ErrControlRequired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.intents == nil {
		c.intents = make(map[string]Intent)
	}
	if c.keys == nil {
		c.keys = make(map[string]string)
	}
	if c.progress == nil {
		c.progress = make(map[string][]Progress)
	}
	if c.watchers == nil {
		c.watchers = make(map[string]map[chan Intent]struct{})
	}
	if c.done == nil {
		c.done = make(chan struct{})
	}
	return nil
}
