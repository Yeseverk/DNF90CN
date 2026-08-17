package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

type Job struct {
	Name     string
	Interval time.Duration
	Jitter   time.Duration
	Run      func(context.Context) error
}

type JobSnapshot struct {
	Name        string     `json:"name"`
	IntervalMS  int64      `json:"interval_ms"`
	JitterMS    int64      `json:"jitter_ms,omitempty"`
	Running     bool       `json:"running"`
	Runs        int64      `json:"runs"`
	Errors      int64      `json:"errors"`
	LastStarted *time.Time `json:"last_started,omitempty"`
	LastEnded   *time.Time `json:"last_ended,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	NextRun     *time.Time `json:"next_run,omitempty"`
}

type Scheduler struct {
	name   string
	logger *slog.Logger

	mu      sync.Mutex
	jobs    map[string]*jobState
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	now     func() time.Time
	jitter  func(time.Duration) time.Duration
}

type jobState struct {
	job Job

	running     bool
	runs        int64
	errors      int64
	lastStarted time.Time
	lastEnded   time.Time
	lastError   string
	nextRun     time.Time
}

func New(name string, logger *slog.Logger) *Scheduler {
	if name == "" {
		name = "scheduler"
	}
	return &Scheduler{
		name:   name,
		logger: logger,
		jobs:   make(map[string]*jobState),
		now:    time.Now,
		jitter: randomJitter,
	}
}

func (s *Scheduler) Name() string {
	if s == nil {
		return "scheduler"
	}
	return s.name
}

func (s *Scheduler) Add(job Job) error {
	if s == nil {
		return fmt.Errorf("scheduler is nil")
	}
	job.Name = strings.TrimSpace(job.Name)
	if job.Name == "" {
		return fmt.Errorf("scheduler job name is required")
	}
	if job.Interval <= 0 {
		return fmt.Errorf("scheduler job %q interval must be positive", job.Name)
	}
	if job.Jitter < 0 {
		return fmt.Errorf("scheduler job %q jitter must not be negative", job.Name)
	}
	if job.Run == nil {
		return fmt.Errorf("scheduler job %q run function is required", job.Name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return fmt.Errorf("scheduler job %q cannot be added after start", job.Name)
	}
	if _, exists := s.jobs[job.Name]; exists {
		return fmt.Errorf("scheduler job %q already exists", job.Name)
	}
	s.jobs[job.Name] = &jobState{job: job}
	return nil
}

func (s *Scheduler) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	jobs := make([]*jobState, 0, len(s.jobs))
	for _, state := range s.jobs {
		jobs = append(jobs, state)
	}
	// worker 必须在 running 对 Stop 可见前全部登记，禁止 Wait 与 Add 交错。
	s.wg.Add(len(jobs))
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()

	for _, state := range jobs {
		go func() {
			defer s.wg.Done()
			s.loop(runCtx, state)
		}()
	}
	return nil
}

func (s *Scheduler) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	cancel := s.cancel
	if cancel == nil && !s.running {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		s.mu.Lock()
		s.cancel = nil
		s.running = false
		s.mu.Unlock()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scheduler) Snapshot() []JobSnapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobSnapshot, 0, len(s.jobs))
	for _, state := range s.jobs {
		out = append(out, state.snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Scheduler) loop(ctx context.Context, state *jobState) {
	interval := state.job.Interval
	delay := s.nextDelay(state)
	s.setNextRun(state, s.now().Add(delay))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.runJob(ctx, state)
			delay = interval + s.jitterDelay(state)
			next := s.now().Add(delay)
			s.setNextRun(state, next)
			timer.Reset(delay)
		}
	}
}

func (s *Scheduler) nextDelay(state *jobState) time.Duration {
	delay := state.job.Interval + s.jitterDelay(state)
	if delay <= 0 {
		return time.Nanosecond
	}
	return delay
}

func (s *Scheduler) jitterDelay(state *jobState) time.Duration {
	if state == nil || state.job.Jitter <= 0 || s.jitter == nil {
		return 0
	}
	delay := s.jitter(state.job.Jitter)
	if delay < 0 {
		return 0
	}
	if delay > state.job.Jitter {
		return state.job.Jitter
	}
	return delay
}

func (s *Scheduler) runJob(ctx context.Context, state *jobState) {
	now := s.now().UTC()
	s.mu.Lock()
	if state.running {
		s.mu.Unlock()
		return
	}
	state.running = true
	state.lastStarted = now
	s.mu.Unlock()

	err := state.job.Run(ctx)
	ended := s.now().UTC()

	s.mu.Lock()
	state.running = false
	state.runs++
	state.lastEnded = ended
	if err != nil {
		state.errors++
		state.lastError = err.Error()
		if s.logger != nil {
			s.logger.Error("scheduler job failed", "job", state.job.Name, "error", err)
		}
	} else {
		state.lastError = ""
	}
	s.mu.Unlock()
}

func (s *Scheduler) setNextRun(state *jobState, next time.Time) {
	s.mu.Lock()
	state.nextRun = next.UTC()
	s.mu.Unlock()
}

func (s *jobState) snapshot() JobSnapshot {
	snap := JobSnapshot{
		Name:       s.job.Name,
		IntervalMS: s.job.Interval.Milliseconds(),
		JitterMS:   s.job.Jitter.Milliseconds(),
		Running:    s.running,
		Runs:       s.runs,
		Errors:     s.errors,
		LastError:  s.lastError,
	}
	if !s.lastStarted.IsZero() {
		t := s.lastStarted
		snap.LastStarted = &t
	}
	if !s.lastEnded.IsZero() {
		t := s.lastEnded
		snap.LastEnded = &t
	}
	if !s.nextRun.IsZero() {
		t := s.nextRun
		snap.NextRun = &t
	}
	return snap
}

func randomJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(max) + 1)) // #nosec G404 -- 非安全用途：调度抖动，不用于凭证或加密。
}
