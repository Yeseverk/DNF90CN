package workerpool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

var (
	ErrClosed      = errors.New("worker pool is closed")
	ErrNotStarted  = errors.New("worker pool is not started")
	ErrQueueFull   = errors.New("worker pool queue is full")
	ErrInvalidTask = errors.New("worker pool task is invalid")
	// ErrInvalidPool 表示调用方传入了 nil worker pool。
	ErrInvalidPool = errors.New("worker pool is invalid")
)

type Pool struct {
	name   string
	size   int
	queue  int
	logger *slog.Logger

	submitMu sync.RWMutex
	mu       sync.Mutex
	started  bool
	closed   bool
	tasks    chan func()
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopDone chan struct{}
	stopping chan struct{}
}

type Result struct {
	Err error
}

type Snapshot struct {
	Name     string `json:"name"`
	Workers  int    `json:"workers"`
	QueueLen int    `json:"queue_len"`
	QueueCap int    `json:"queue_cap"`
	Started  bool   `json:"started"`
	Closed   bool   `json:"closed"`
}

func New(name string, size, queue int, logger *slog.Logger) *Pool {
	if size <= 0 {
		size = 4
	}
	if queue <= 0 {
		queue = 128
	}
	return &Pool{
		name:     name,
		size:     size,
		queue:    queue,
		logger:   logger,
		tasks:    make(chan func(), queue),
		stopDone: make(chan struct{}),
		stopping: make(chan struct{}),
	}
}

func (p *Pool) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

func (p *Pool) Start(ctx context.Context) error {
	if p == nil {
		return ErrInvalidPool
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureReadyLocked()
	if p.started {
		return nil
	}
	if p.closed {
		return ErrClosed
	}
	if p.isStopping() {
		return ErrClosed
	}
	p.started = true
	for i := 0; i < p.size; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for task := range p.tasks {
				if task != nil {
					func() {
						defer func() {
							if rec := recover(); rec != nil && p.logger != nil {
								p.logger.Error("worker task panic recovered", "name", p.name, "panic", rec)
							}
						}()
						task()
					}()
				}
			}
		}()
	}
	if p.logger != nil {
		p.logger.Info("worker pool started", "workers", p.size, "queue", p.queue)
	}
	return nil
}

func (p *Pool) Submit(task func()) error {
	if p == nil {
		return ErrInvalidPool
	}
	if task == nil {
		return ErrInvalidTask
	}
	p.submitMu.RLock()
	defer p.submitMu.RUnlock()
	p.mu.Lock()
	p.ensureReadyLocked()
	if p.closed {
		p.mu.Unlock()
		return ErrClosed
	}
	if p.isStopping() {
		p.mu.Unlock()
		return ErrClosed
	}
	if !p.started {
		p.mu.Unlock()
		return ErrNotStarted
	}
	tasks := p.tasks
	stopping := p.stopping
	p.mu.Unlock()
	select {
	case <-stopping:
		return ErrClosed
	default:
	}
	select {
	case tasks <- task:
		return nil
	case <-stopping:
		return ErrClosed
	default:
		return ErrQueueFull
	}
}

func (p *Pool) SubmitWait(ctx context.Context, task func()) error {
	if p == nil {
		return ErrInvalidPool
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if task == nil {
		return ErrInvalidTask
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.submitMu.RLock()
	defer p.submitMu.RUnlock()
	p.mu.Lock()
	p.ensureReadyLocked()
	if p.closed {
		p.mu.Unlock()
		return ErrClosed
	}
	if p.isStopping() {
		p.mu.Unlock()
		return ErrClosed
	}
	if !p.started {
		p.mu.Unlock()
		return ErrNotStarted
	}
	tasks := p.tasks
	stopping := p.stopping
	p.mu.Unlock()
	select {
	case <-stopping:
		return ErrClosed
	default:
	}
	select {
	case tasks <- task:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-stopping:
		return ErrClosed
	}
}

func (p *Pool) SubmitContext(ctx context.Context, task func(context.Context) error) (<-chan Result, error) {
	if p == nil {
		return nil, ErrInvalidPool
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if task == nil {
		return nil, ErrInvalidTask
	}
	result := make(chan Result, 1)
	completed := make(chan struct{})
	var once sync.Once
	var started atomic.Bool
	complete := func(res Result) {
		once.Do(func() {
			result <- res
			close(result)
		})
	}
	err := p.Submit(func() {
		started.Store(true)
		res := Result{}
		defer func() {
			close(completed)
			if rec := recover(); rec != nil {
				res.Err = fmt.Errorf("worker pool task panic: %v", rec)
			}
			complete(res)
		}()
		if err := ctx.Err(); err != nil {
			res.Err = err
			return
		}
		res.Err = task(ctx)
	})
	if err != nil {
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			if !started.Load() {
				complete(Result{Err: ctx.Err()})
			}
		case <-completed:
		}
	}()
	return result, nil
}

func (p *Pool) Snapshot() Snapshot {
	if p == nil {
		return Snapshot{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return Snapshot{
		Name:     p.name,
		Workers:  p.size,
		QueueLen: len(p.tasks),
		QueueCap: cap(p.tasks),
		Started:  p.started,
		Closed:   p.closed,
	}
}

func (p *Pool) Stop(ctx context.Context) error {
	if p == nil {
		return ErrInvalidPool
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.stopOnce.Do(func() {
		p.submitMu.Lock()
		p.mu.Lock()
		p.ensureReadyLocked()
		close(p.stopping)
		if !p.closed {
			p.closed = true
			close(p.tasks)
		}
		p.mu.Unlock()
		p.submitMu.Unlock()
		go func() {
			p.wg.Wait()
			if p.logger != nil {
				p.logger.Info("worker pool stopped", "name", p.name)
			}
			close(p.stopDone)
		}()
	})

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.stopDone:
		return nil
	}
}

func (p *Pool) ensureReadyLocked() {
	if p.size <= 0 {
		p.size = 4
	}
	if p.queue <= 0 {
		p.queue = 128
	}
	if p.tasks == nil {
		p.tasks = make(chan func(), p.queue)
	}
	if p.stopDone == nil {
		p.stopDone = make(chan struct{})
	}
	if p.stopping == nil {
		p.stopping = make(chan struct{})
	}
}

func (p *Pool) isStopping() bool {
	if p == nil || p.stopping == nil {
		return false
	}
	select {
	case <-p.stopping:
		return true
	default:
		return false
	}
}
