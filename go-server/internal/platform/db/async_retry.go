package db

import "time"

type asyncRetryPolicy[T any, F comparable] struct {
	backoff    time.Duration
	maxRetries int
}

// RecordFailure 更新失败次数、失败时间和下一次重试时间。
func (p asyncRetryPolicy[T, F]) RecordFailure(save pendingSave[T, F], err error, now time.Time) pendingSave[T, F] {
	save.attempts++
	if save.firstFailedAt.IsZero() {
		save.firstFailedAt = now
	}
	save.lastFailedAt = now
	if err != nil {
		save.lastError = err.Error()
	}
	save.nextAttemptAt = now.Add(p.backoff * time.Duration(save.attempts))
	return save
}

// Exhausted 判断保存请求是否已耗尽重试次数。
func (p asyncRetryPolicy[T, F]) Exhausted(save pendingSave[T, F]) bool {
	return p.maxRetries > 0 && save.attempts >= p.maxRetries
}
