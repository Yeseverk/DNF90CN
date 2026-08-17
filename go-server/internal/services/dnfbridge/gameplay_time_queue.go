package dnfbridge

import (
	"context"
	"time"

	"longheng.io/server/internal/platform/timequeue"
)

// gameplayTimeQueue is the single process-local scheduler used by gameplay.
// Durable expiry timestamps remain repository state and are reconstructed by
// the owning gameplay when a session starts. Transport deadlines and database
// request timeouts are infrastructure concerns and deliberately stay outside
// this queue.
type gameplayTimeQueue interface {
	Now() time.Time
	ScheduleAfter(string, time.Duration, timequeue.Callback) error
	Cancel(string) bool
	Start(context.Context)
}

type processGameplayTimeQueue struct {
	queue *timequeue.Service
	now   func() time.Time
}

func newProcessGameplayTimeQueue(onPanic func(string, any)) *processGameplayTimeQueue {
	now := time.Now
	return &processGameplayTimeQueue{
		queue: timequeue.New(timequeue.Options{Now: now, OnPanic: onPanic}),
		now:   now,
	}
}

func (q *processGameplayTimeQueue) Now() time.Time {
	if q == nil || q.now == nil {
		return time.Now()
	}
	return q.now()
}

func (q *processGameplayTimeQueue) ScheduleAfter(
	name string,
	delay time.Duration,
	callback timequeue.Callback,
) error {
	if q == nil || q.queue == nil {
		return errGameplayTimeQueueUnavailable
	}
	_, err := q.queue.ScheduleAfter(name, delay, callback)
	return err
}

func (q *processGameplayTimeQueue) Cancel(name string) bool {
	return q != nil && q.queue != nil && q.queue.Cancel(name)
}

func (q *processGameplayTimeQueue) Start(ctx context.Context) {
	if q == nil || q.queue == nil {
		return
	}
	q.queue.Start(ctx)
}

func (s *Service) gameplayNow() time.Time {
	if queue := s.ensureGameplayTimeQueue(); queue != nil {
		return queue.Now()
	}
	return time.Now()
}

func (s *Service) ensureGameplayTimeQueue() gameplayTimeQueue {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gameplayTimers == nil {
		s.gameplayTimers = newProcessGameplayTimeQueue(func(name string, recovered any) {
			s.logWarn("dnfbridge gameplay timer callback panic", "timer_name", name, "recovered", recovered)
		})
	}
	return s.gameplayTimers
}

func (s *Service) startGameplayTimeQueue(ctx context.Context) {
	queue := s.ensureGameplayTimeQueue()
	if queue == nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		queue.Start(ctx)
	}()
}
