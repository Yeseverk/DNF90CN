package hotupdate

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/eventlog"
)

const (
	EventLogIntentStream   = "hotupdate.intent"
	EventLogProgressStream = "hotupdate.progress"
)

var (
	ErrControlLogRequired  = errors.New("hot update eventlog control requires eventlog")
	ErrControlConflict     = errors.New("hot update control idempotency key conflicts with existing intent")
	ErrInvalidControlEvent = errors.New("hot update control event payload is invalid")
	ErrControlPollInterval = errors.New("hot update control poll interval is invalid")
	ErrControlInstanceID   = errors.New("hot update control instance id generation failed")
	defControlPoll         = 2 * time.Second
)

var controlRandRead = rand.Read

type EventLogControlOptions struct {
	Log          *eventlog.Log
	PollInterval time.Duration
	Now          func() time.Time
}

type EventLogControl struct {
	log          *eventlog.Log
	pollInterval time.Duration
	now          func() time.Time

	ops              sync.RWMutex
	mu               sync.Mutex
	intents          map[string]Intent
	progress         map[string][]Progress
	watchers         map[string]map[chan Intent]struct{}
	done             chan struct{}
	closed           bool
	instanceID       string
	progressSequence uint64
}

type intentEnvelope struct {
	Target string `json:"target"`
	Intent Intent `json:"intent"`
}

type progressEnvelope struct {
	Progress Progress `json:"progress"`
}

func NewEventLogControl(options EventLogControlOptions) (*EventLogControl, error) {
	if options.Log == nil {
		return nil, ErrControlLogRequired
	}
	pollInterval := options.PollInterval
	if pollInterval == 0 {
		pollInterval = defControlPoll
	}
	if pollInterval < 0 {
		return nil, ErrControlPollInterval
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	instanceID, err := newControlInstanceID()
	if err != nil {
		return nil, err
	}
	return &EventLogControl{
		log:          options.Log,
		pollInterval: pollInterval,
		now:          now,
		intents:      make(map[string]Intent),
		progress:     make(map[string][]Progress),
		watchers:     make(map[string]map[chan Intent]struct{}),
		done:         make(chan struct{}),
		instanceID:   instanceID,
	}, nil
}

func (c *EventLogControl) Publish(ctx context.Context, target string, intent Intent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	now, err := c.ready()
	if err != nil {
		return err
	}
	target, err = normalizeTarget(target)
	if err != nil {
		return err
	}
	intent, err = normalizeIntent(intent)
	if err != nil {
		return err
	}
	release, err := c.beginWrite()
	if err != nil {
		return err
	}
	defer release()
	// EventLog 控制面把 intent 持久化为事件，再通知 watcher；watcher 丢通知时可通过轮询补回最新 intent。
	key := intentIdempotencyKey(target, intent)
	existing, ok, err := c.log.GetByIdempotencyKey(ctx, key)
	if err != nil {
		return err
	}
	if ok {
		existingIntent, err := decodeIntentEvent(existing)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(existingIntent, intent) {
			return fmt.Errorf("%w: target=%s version=%s", ErrControlConflict, target, intent.Version)
		}
		c.storeIntentAndNotify(ctx, target, existingIntent)
		return nil
	}
	payload, err := json.Marshal(intentEnvelope{Target: target, Intent: intent})
	if err != nil {
		return err
	}
	if _, err := c.log.Append(ctx, eventlog.Event{
		Stream:         EventLogIntentStream,
		Type:           string(intent.Action),
		AggregateID:    target,
		IdempotencyKey: key,
		Payload:        payload,
		Status:         eventlog.StatusPublished,
		PublishedAt:    now().UTC(),
		Headers: map[string]string{
			"hotupdate_target":  target,
			"hotupdate_action":  string(intent.Action),
			"hotupdate_version": intent.Version,
		},
	}); err != nil {
		if errors.Is(err, eventlog.ErrIdempotencyConflict) {
			return fmt.Errorf("%w: target=%s version=%s", ErrControlConflict, target, intent.Version)
		}
		return err
	}
	c.storeIntentAndNotify(ctx, target, intent)
	return nil
}

func (c *EventLogControl) Watch(ctx context.Context, target string) (<-chan Intent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := c.ready(); err != nil {
		return nil, err
	}
	target, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}
	// Watch 先注册 channel，再加载最新 intent，避免注册窗口内发布的新 intent 被漏掉。
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
	go c.watchLoop(ctx, target, ch)
	return ch, nil
}

func (c *EventLogControl) Report(ctx context.Context, progress Progress) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	now, err := c.ready()
	if err != nil {
		return err
	}
	progress, err = normalizeProgress(progress, now)
	if err != nil {
		return err
	}
	release, err := c.beginWrite()
	if err != nil {
		return err
	}
	defer release()
	payload, err := json.Marshal(progressEnvelope{Progress: progress})
	if err != nil {
		return err
	}
	instanceID, sequence := c.nextProgressKeyParts()
	if _, err := c.log.Append(ctx, eventlog.Event{
		Stream:         EventLogProgressStream,
		Type:           string(progress.Stage),
		AggregateID:    progress.Target,
		IdempotencyKey: controlProgressKey(progress, instanceID, sequence),
		Payload:        payload,
		Status:         eventlog.StatusPublished,
		PublishedAt:    now().UTC(),
		Headers: map[string]string{
			"hotupdate_target":  progress.Target,
			"hotupdate_node":    progress.NodeID,
			"hotupdate_version": progress.Version,
			"hotupdate_stage":   string(progress.Stage),
		},
	}); err != nil {
		return err
	}
	c.mu.Lock()
	if !c.closed {
		c.progress[progress.Target] = append(c.progress[progress.Target], progress)
	}
	c.mu.Unlock()
	return nil
}

func (c *EventLogControl) beginWrite() (func(), error) {
	c.ops.RLock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.ops.RUnlock()
		return nil, ErrControlClosed
	}
	c.mu.Unlock()
	return c.ops.RUnlock, nil
}

func (c *EventLogControl) ready() (func() time.Time, error) {
	if c == nil || c.log == nil {
		return nil, ErrControlLogRequired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pollInterval < 0 {
		return nil, ErrControlPollInterval
	}
	if c.pollInterval == 0 {
		c.pollInterval = defControlPoll
	}
	if c.now == nil {
		c.now = func() time.Time { return time.Now().UTC() }
	}
	if c.intents == nil {
		c.intents = make(map[string]Intent)
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
	if c.instanceID == "" {
		instanceID, err := newControlInstanceID()
		if err != nil {
			return nil, err
		}
		c.instanceID = instanceID
	}
	return c.now, nil
}

func (c *EventLogControl) Snapshot() ControlSnapshot {
	if c == nil {
		return ControlSnapshot{}
	}
	c.mu.Lock()
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
	c.mu.Unlock()
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

func (c *EventLogControl) Close() error {
	if c == nil {
		return nil
	}
	c.ops.Lock()
	defer c.ops.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	// Close 关闭所有 watcher channel；EventLog 里的 intent/progress 仍保留用于审计。
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

func (c *EventLogControl) watchLoop(ctx context.Context, target string, ch chan Intent) {
	defer c.removeWatcher(target, ch)
	// watchLoop 定期从 EventLog 读取最新 intent，作为内存通知丢失后的补偿。
	if c.isDone() {
		return
	}
	_ = c.reloadLatestIntent(ctx, target)
	if c.pollInterval <= 0 {
		select {
		case <-ctx.Done():
		case <-c.done:
		}
		return
	}
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			_ = c.reloadLatestIntent(ctx, target)
		}
	}
}

func (c *EventLogControl) isDone() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *EventLogControl) removeWatcher(target string, ch chan Intent) {
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
}

func (c *EventLogControl) reloadLatestIntent(ctx context.Context, target string) error {
	events, err := c.log.ListByStreamAggregate(ctx, EventLogIntentStream, target, 1)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	intent, err := decodeIntentEvent(events[0])
	if err != nil {
		return err
	}
	c.storeIntentAndNotify(ctx, target, intent)
	return nil
}

func (c *EventLogControl) storeIntentAndNotify(_ context.Context, target string, intent Intent) {
	c.mu.Lock()
	// 内存 latest 是读优化，权威记录仍是 EventLog；通知失败不会删除持久化 intent。
	if c.closed {
		c.mu.Unlock()
		return
	}
	if existing, ok := c.intents[target]; ok && reflect.DeepEqual(existing, intent) {
		c.mu.Unlock()
		return
	}
	c.intents[target] = intent
	for ch := range c.watchers[target] {
		sendLatestIntent(ch, intent)
	}
	c.mu.Unlock()
}

func decodeIntentEvent(event eventlog.Event) (Intent, error) {
	var envelope intentEnvelope
	if len(event.Payload) == 0 {
		return Intent{}, ErrInvalidControlEvent
	}
	if err := json.Unmarshal(event.Payload, &envelope); err != nil {
		return Intent{}, fmt.Errorf("%w: %w", ErrInvalidControlEvent, err)
	}
	intent, err := normalizeIntent(envelope.Intent)
	if err != nil {
		return Intent{}, err
	}
	return intent, nil
}

func intentIdempotencyKey(target string, intent Intent) string {
	if intent.Sequence > 0 {
		return fmt.Sprintf("hotupdate:intent:v1:%s:%d", encodeEventKeyPart(target), intent.Sequence)
	}
	return fmt.Sprintf("hotupdate:intent:v1:%s:%s:%s", encodeEventKeyPart(target), encodeEventKeyPart(string(intent.Action)), encodeEventKeyPart(intent.Version))
}

func progressIdemKey(progress Progress) string {
	parts := []string{
		"hotupdate:progress",
		"v1",
		encodeEventKeyPart(progress.Target),
		encodeEventKeyPart(progress.NodeID),
		encodeEventKeyPart(progress.Version),
		encodeEventKeyPart(string(progress.Stage)),
		progressFingerprint(progress),
	}
	return strings.Join(parts, ":")
}

func controlProgressKey(progress Progress, instanceID string, sequence uint64) string {
	parts := []string{
		progressIdemKey(progress),
		encodeEventKeyPart(instanceID),
		fmt.Sprint(sequence),
	}
	return strings.Join(parts, ":")
}

func (c *EventLogControl) nextProgressKeyParts() (string, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progressSequence++
	return c.instanceID, c.progressSequence
}

func newControlInstanceID() (string, error) {
	var raw [8]byte
	if _, err := controlRandRead(raw[:]); err != nil {
		return "", fmt.Errorf("%w: %w", ErrControlInstanceID, err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func encodeEventKeyPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func progressFingerprint(progress Progress) string {
	payload := strings.Join([]string{
		strings.TrimSpace(progress.Target),
		strings.TrimSpace(progress.NodeID),
		strings.TrimSpace(progress.Version),
		strings.TrimSpace(string(progress.Stage)),
		strings.TrimSpace(progress.Status),
		strings.TrimSpace(progress.Detail),
		progress.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:8])
}
