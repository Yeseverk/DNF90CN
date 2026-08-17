package hotreload

import (
	"context"
	"sync"
)

// DrainBarrier 在热切换前停止接收新调用，并等待已进入的调用自然结束。
type DrainBarrier struct {
	mu           sync.Mutex
	initialized  bool
	accepting    bool
	active       int
	drainWaiters []chan struct{}
	enterWaiters []chan struct{}
}

// BarrierSnapshot 描述热切换屏障当前的接入、活跃调用和等待队列状态。
type BarrierSnapshot struct {
	Accepting    bool `json:"accepting"`
	Active       int  `json:"active"`
	Waiters      int  `json:"waiters"`
	DrainWaiters int  `json:"drain_waiters,omitempty"`
	EnterWaiters int  `json:"enter_waiters,omitempty"`
}

// NewDrainBarrier 创建默认允许进入的热切换屏障。
func NewDrainBarrier() *DrainBarrier {
	return &DrainBarrier{initialized: true, accepting: true}
}

// Enter 在屏障接受新调用时进入活跃区，并返回幂等的离开函数。
func (b *DrainBarrier) Enter(ctx context.Context) (func(), error) {
	if b == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.ensureReadyLocked()
	defer b.mu.Unlock()
	if !b.accepting {
		return nil, ErrBarrierDraining
	}
	b.active++
	once := sync.Once{}
	return func() {
		once.Do(func() {
			b.mu.Lock()
			if b.active > 0 {
				b.active--
			}
			b.notifyDrainLocked()
			b.mu.Unlock()
		})
	}, nil
}

// EnterWhenReady 会等待屏障恢复接受新调用后进入活跃区。
func (b *DrainBarrier) EnterWhenReady(ctx context.Context) (func(), error) {
	if b == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b.mu.Lock()
		b.ensureReadyLocked()
		if b.accepting {
			b.active++
			b.mu.Unlock()
			once := sync.Once{}
			return func() {
				once.Do(func() {
					b.mu.Lock()
					if b.active > 0 {
						b.active--
					}
					b.notifyDrainLocked()
					b.mu.Unlock()
				})
			}, nil
		}
		waiter := make(chan struct{})
		b.enterWaiters = append(b.enterWaiters, waiter)
		b.mu.Unlock()
		select {
		case <-waiter:
		case <-ctx.Done():
			b.mu.Lock()
			b.enterWaiters = removeWaiter(b.enterWaiters, waiter)
			b.mu.Unlock()
			return nil, ctx.Err()
		}
	}
}

// Drain 停止接收新调用，并等待已进入的调用全部离开。
func (b *DrainBarrier) Drain(ctx context.Context) error {
	if b == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	b.ensureReadyLocked()
	b.accepting = false
	for b.active > 0 {
		waiter := make(chan struct{})
		b.drainWaiters = append(b.drainWaiters, waiter)
		b.mu.Unlock()
		select {
		case <-waiter:
		case <-ctx.Done():
			b.mu.Lock()
			b.drainWaiters = removeWaiter(b.drainWaiters, waiter)
			b.mu.Unlock()
			return ctx.Err()
		}
		b.mu.Lock()
	}
	b.mu.Unlock()
	return nil
}

// Resume 重新允许调用进入，并唤醒等待恢复的调用方。
func (b *DrainBarrier) Resume() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.ensureReadyLocked()
	b.accepting = true
	b.notifyEnterLocked()
	b.mu.Unlock()
}

// Snapshot 返回屏障状态快照，便于管理端观测热切换过程。
func (b *DrainBarrier) Snapshot() BarrierSnapshot {
	if b == nil {
		return BarrierSnapshot{Accepting: true}
	}
	b.mu.Lock()
	b.ensureReadyLocked()
	snapshot := BarrierSnapshot{
		Accepting:    b.accepting,
		Active:       b.active,
		DrainWaiters: len(b.drainWaiters),
		EnterWaiters: len(b.enterWaiters),
	}
	snapshot.Waiters = snapshot.DrainWaiters + snapshot.EnterWaiters
	b.mu.Unlock()
	return snapshot
}

func (b *DrainBarrier) ensureReadyLocked() {
	if b.initialized {
		return
	}
	b.initialized = true
	b.accepting = true
}

func (b *DrainBarrier) notifyDrainLocked() {
	if b.active > 0 {
		return
	}
	waiters := b.drainWaiters
	b.drainWaiters = nil
	for _, waiter := range waiters {
		close(waiter)
	}
}

func (b *DrainBarrier) notifyEnterLocked() {
	waiters := b.enterWaiters
	b.enterWaiters = nil
	for _, waiter := range waiters {
		close(waiter)
	}
}

func removeWaiter(waiters []chan struct{}, target chan struct{}) []chan struct{} {
	for i, waiter := range waiters {
		if waiter != target {
			continue
		}
		copy(waiters[i:], waiters[i+1:])
		waiters[len(waiters)-1] = nil
		return waiters[:len(waiters)-1]
	}
	return waiters
}
