package playerloop

import (
	"context"
	"errors"
	"time"
)

// TickPayloadFunc 为每个账号构造一次 tick 事件负载。
type TickPayloadFunc func(accountID string, at time.Time) any

// TickResult 记录一次 tick 广播覆盖账号数、成功提交数和丢弃数。
type TickResult struct {
	Accounts  int `json:"accounts"`
	Submitted int `json:"submitted"`
	Dropped   int `json:"dropped"`
}

// Tick 向当前所有账号循环提交同一个 tick 负载。
func (m *Manager) Tick(ctx context.Context, payload any) (TickResult, error) {
	return m.TickFunc(ctx, func(string, time.Time) any { return payload })
}

// TickFunc 向当前所有账号循环提交由回调构造的 tick 负载。
func (m *Manager) TickFunc(ctx context.Context, build TickPayloadFunc) (TickResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TickResult{}, err
	}
	if build == nil {
		build = func(string, time.Time) any { return nil }
	}
	now := time.Now().UTC()

	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return TickResult{}, ErrStopped
	}
	loops := make([]*loop, 0, len(m.loops))
	for _, playerLoop := range m.loops {
		loops = append(loops, playerLoop)
	}
	m.mu.Unlock()

	result := TickResult{Accounts: len(loops)}
	for _, playerLoop := range loops {
		event := Event{
			AccountID:  playerLoop.accountID,
			Payload:    build(playerLoop.accountID, now),
			ReceivedAt: now,
		}
		switch err := playerLoop.submit(ctx, event); {
		case err == nil:
			result.Submitted++
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return result, ctx.Err()
		default:
			result.Dropped++
		}
	}
	return result, nil
}
