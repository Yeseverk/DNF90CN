// Package timequeue provides a process-local due-time queue.
//
// It is intentionally not a persistence layer. Callers must reconstruct durable
// state from their repositories on login/start and treat callbacks as online
// reminders or runtime session timers.
package timequeue

import (
	"container/heap"
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrNameRequired     = errors.New("timequeue name is required")
	ErrCallbackRequired = errors.New("timequeue callback is required")
	ErrInvalidMoment    = errors.New("timequeue moment hour or minute is invalid")
)

type Callback func(time.Time)

type Options struct {
	Now            func() time.Time
	Location       *time.Location
	OnPanic        func(name string, recovered any)
	StaleThreshold int
}

type Service struct {
	mu             sync.Mutex
	now            func() time.Time
	location       *time.Location
	onPanic        func(string, any)
	staleThreshold int

	nextID   uint64
	items    taskHeap
	byName   map[string]uint64
	canceled map[uint64]struct{}
	lastNow  time.Time
	wake     chan struct{}
}

type Handle struct {
	name string
	id   uint64
	q    *Service
}

type Snapshot struct {
	Pending  int
	Named    int
	Canceled int
}

type taskKind byte

const (
	kindOneShot taskKind = iota + 1
	kindMinute
	kindDaily
	kindWeekly
)

type task struct {
	id       uint64
	name     string
	due      time.Time
	kind     taskKind
	callback Callback
	hour     int
	minute   int
	weekday  time.Weekday
}

func New(options Options) *Service {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	location := options.Location
	if location == nil {
		location = time.FixedZone("UTC+8", 8*60*60)
	}
	staleThreshold := options.StaleThreshold
	if staleThreshold <= 0 {
		staleThreshold = 64
	}
	return &Service{
		now:            now,
		location:       location,
		onPanic:        options.OnPanic,
		staleThreshold: staleThreshold,
		byName:         make(map[string]uint64),
		canceled:       make(map[uint64]struct{}),
		wake:           make(chan struct{}, 1),
	}
}

func (q *Service) ScheduleAfter(name string, delay time.Duration, callback Callback) (Handle, error) {
	return q.ScheduleAt(name, q.now().Add(delay), callback)
}

func (q *Service) ScheduleAt(name string, due time.Time, callback Callback) (Handle, error) {
	if err := validate(name, callback); err != nil {
		return Handle{}, err
	}
	return q.addTask(&task{name: strings.TrimSpace(name), due: due.UTC(), kind: kindOneShot, callback: callback}, true), nil
}

func (q *Service) RegisterMinute(name string, callback Callback) (Handle, error) {
	if err := validate(name, callback); err != nil {
		return Handle{}, err
	}
	now := q.now().UTC()
	due := now.Truncate(time.Minute).Add(time.Minute)
	return q.addTask(&task{name: strings.TrimSpace(name), due: due, kind: kindMinute, callback: callback}, true), nil
}

func (q *Service) RegisterDaily(name string, hour, minute int, callback Callback) (Handle, error) {
	if err := validate(name, callback); err != nil {
		return Handle{}, err
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return Handle{}, ErrInvalidMoment
	}
	now := q.now().UTC()
	due := q.nextDaily(now, hour, minute)
	return q.addTask(&task{name: strings.TrimSpace(name), due: due, kind: kindDaily, callback: callback, hour: hour, minute: minute}, true), nil
}

func (q *Service) RegisterWeekly(name string, weekday time.Weekday, hour, minute int, callback Callback) (Handle, error) {
	if err := validate(name, callback); err != nil {
		return Handle{}, err
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return Handle{}, ErrInvalidMoment
	}
	now := q.now().UTC()
	due := q.nextWeekly(now, weekday, hour, minute)
	return q.addTask(&task{name: strings.TrimSpace(name), due: due, kind: kindWeekly, callback: callback, weekday: weekday, hour: hour, minute: minute}, true), nil
}

func (h Handle) Cancel() bool {
	if h.q == nil || h.id == 0 {
		return false
	}
	return h.q.cancelHandle(h.name, h.id)
}

func (q *Service) Cancel(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	id, ok := q.byName[name]
	if !ok {
		return false
	}
	delete(q.byName, name)
	q.canceled[id] = struct{}{}
	q.signalLocked()
	return true
}

func (q *Service) CancelPrefix(prefix string) int {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	count := 0
	for name, id := range q.byName {
		if strings.HasPrefix(name, prefix) {
			delete(q.byName, name)
			q.canceled[id] = struct{}{}
			count++
		}
	}
	if count > 0 {
		q.signalLocked()
	}
	return count
}

func (q *Service) RunDue(now time.Time) {
	callbacks := q.takeDue(now.UTC())
	for _, item := range callbacks {
		q.invoke(item)
	}
}

func (q *Service) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		delay := q.nextDelay(q.now().UTC())
		if delay < 0 {
			delay = 0
		}
		resetTimer(timer, delay)
		select {
		case <-ctx.Done():
			return
		case now := <-timer.C:
			q.RunDue(now)
		case <-q.wake:
		}
	}
}

func (q *Service) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	return Snapshot{Pending: len(q.items), Named: len(q.byName), Canceled: len(q.canceled)}
}

func validate(name string, callback Callback) error {
	if strings.TrimSpace(name) == "" {
		return ErrNameRequired
	}
	if callback == nil {
		return ErrCallbackRequired
	}
	return nil
}

func (q *Service) addTask(item *task, replace bool) Handle {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextID++
	item.id = q.nextID
	if replace {
		if old, ok := q.byName[item.name]; ok {
			q.canceled[old] = struct{}{}
		}
		q.byName[item.name] = item.id
	}
	heap.Push(&q.items, item)
	q.compactLocked()
	q.signalLocked()
	return Handle{name: item.name, id: item.id, q: q}
}

func (q *Service) cancelHandle(name string, id uint64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.byName[name] != id {
		return false
	}
	delete(q.byName, name)
	q.canceled[id] = struct{}{}
	q.signalLocked()
	return true
}

func (q *Service) takeDue(now time.Time) []*task {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.lastNow.IsZero() && now.Before(q.lastNow) {
		q.reanchorLocked(now)
		q.lastNow = now
		q.signalLocked()
		return nil
	}
	q.lastNow = now

	var callbacks []*task
	for len(q.items) > 0 {
		next := q.items[0]
		if next.due.After(now) {
			break
		}
		heap.Pop(&q.items)
		if q.isCanceledLocked(next) {
			continue
		}
		if q.byName[next.name] != next.id {
			q.canceled[next.id] = struct{}{}
			continue
		}
		callbacks = append(callbacks, next.cloneForCallback())
		if next.kind == kindOneShot {
			delete(q.byName, next.name)
			continue
		}
		q.rescheduleRecurringLocked(next, now)
	}
	q.compactLocked()
	q.signalLocked()
	return callbacks
}

func (q *Service) isCanceledLocked(item *task) bool {
	if _, ok := q.canceled[item.id]; ok {
		delete(q.canceled, item.id)
		return true
	}
	return false
}

func (q *Service) rescheduleRecurringLocked(item *task, now time.Time) {
	q.nextID++
	item.id = q.nextID
	switch item.kind {
	case kindMinute:
		item.due = now.Truncate(time.Minute).Add(time.Minute)
	case kindDaily:
		item.due = q.nextDaily(now, item.hour, item.minute)
	case kindWeekly:
		item.due = q.nextWeekly(now, item.weekday, item.hour, item.minute)
	}
	q.byName[item.name] = item.id
	heap.Push(&q.items, item)
}

func (q *Service) reanchorLocked(now time.Time) {
	for _, item := range q.items {
		if q.byName[item.name] != item.id || item.kind == kindOneShot {
			continue
		}
		switch item.kind {
		case kindMinute:
			item.due = now.Truncate(time.Minute).Add(time.Minute)
		case kindDaily:
			item.due = q.nextDaily(now, item.hour, item.minute)
		case kindWeekly:
			item.due = q.nextWeekly(now, item.weekday, item.hour, item.minute)
		}
	}
	heap.Init(&q.items)
}

func (q *Service) compactLocked() {
	if len(q.canceled) < q.staleThreshold {
		return
	}
	filtered := q.items[:0]
	for _, item := range q.items {
		if _, ok := q.canceled[item.id]; ok {
			delete(q.canceled, item.id)
			continue
		}
		if q.byName[item.name] != item.id {
			continue
		}
		filtered = append(filtered, item)
	}
	q.items = filtered
	heap.Init(&q.items)
}

func (q *Service) nextDelay(now time.Time) time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) > 0 {
		item := q.items[0]
		if q.isCanceledLocked(item) || q.byName[item.name] != item.id {
			heap.Pop(&q.items)
			continue
		}
		return item.due.Sub(now)
	}
	return time.Hour
}

func (q *Service) invoke(item *task) {
	defer func() {
		if recovered := recover(); recovered != nil && q.onPanic != nil {
			q.onPanic(item.name, recovered)
		}
	}()
	item.callback(item.due)
}

func (q *Service) signalLocked() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *Service) nextDaily(now time.Time, hour, minute int) time.Time {
	local := now.In(q.location)
	dueLocal := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, q.location)
	if !dueLocal.After(local) {
		dueLocal = dueLocal.Add(24 * time.Hour)
	}
	return dueLocal.UTC()
}

func (q *Service) nextWeekly(now time.Time, weekday time.Weekday, hour, minute int) time.Time {
	local := now.In(q.location)
	dueLocal := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, q.location)
	days := (int(weekday) - int(local.Weekday()) + 7) % 7
	dueLocal = dueLocal.AddDate(0, 0, days)
	if !dueLocal.After(local) {
		dueLocal = dueLocal.AddDate(0, 0, 7)
	}
	return dueLocal.UTC()
}

func resetTimer(timer *time.Timer, delay time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func (t *task) cloneForCallback() *task {
	copy := *t
	return &copy
}

type taskHeap []*task

func (h taskHeap) Len() int { return len(h) }
func (h taskHeap) Less(i, j int) bool {
	return h[i].due.Before(h[j].due) || h[i].due.Equal(h[j].due) && h[i].id < h[j].id
}
func (h taskHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *taskHeap) Push(x any) {
	*h = append(*h, x.(*task))
}

func (h *taskHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
