package db

import (
	"context"
	"errors"
	"sync"
	"time"
)

type asyncFlusherOptions[T any, F comparable] struct {
	Base            Store[T]
	Queue           *asyncPendingQueue[T, F]
	Retry           asyncRetryPolicy[T, F]
	DeadLetters     *asyncDeadLetterSink[T, F]
	Clone           CloneFunc[T]
	NormalizeFields func([]F) []F
	SaveFields      SaveFieldsFunc[T, F]
	SaveBatch       SaveFieldBatchFunc[T, F]
	AutoExpireTTL   time.Duration
	Expire          ExpireFunc[T]
}

type asyncFlusher[T any, F comparable] struct {
	mu              sync.Mutex
	base            Store[T]
	queue           *asyncPendingQueue[T, F]
	retry           asyncRetryPolicy[T, F]
	deadLetters     *asyncDeadLetterSink[T, F]
	clone           CloneFunc[T]
	normalizeFields func([]F) []F
	saveFields      SaveFieldsFunc[T, F]
	saveBatch       SaveFieldBatchFunc[T, F]
	autoExpireTTL   time.Duration
	expire          ExpireFunc[T]
}

func newAsyncFlusher[T any, F comparable](options asyncFlusherOptions[T, F]) *asyncFlusher[T, F] {
	if options.Clone == nil {
		options.Clone = IdentityClone[T]
	}
	return &asyncFlusher[T, F]{
		base:            options.Base,
		queue:           options.Queue,
		retry:           options.Retry,
		deadLetters:     options.DeadLetters,
		clone:           options.Clone,
		normalizeFields: options.NormalizeFields,
		saveFields:      options.SaveFields,
		saveBatch:       options.SaveBatch,
		autoExpireTTL:   options.AutoExpireTTL,
		expire:          options.Expire,
	}
}

// Flush 刷出 pending 队列，并把失败项转入重试或死信。
func (f *asyncFlusher[T, F]) Flush(ctx context.Context, force bool) error {
	ctx = contextOrBackground(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()

	for {
		batch := f.queue.Take(force, time.Now().UTC())
		if len(batch) == 0 {
			return nil
		}
		// Take 会乐观地从 pending 队列移除记录。下面每条失败路径都必须记录失败项
		// 供重试/死信处理，或把尚未尝试的项放回队列；否则后端抖动会静默丢掉已接受的玩家状态。
		if f.saveBatch != nil {
			if err := ctx.Err(); err != nil {
				f.queue.Restore(batch)
				return err
			}
			if err := f.saveBatch(ctx, f.base, fieldSavesPending(batch, f.clone, f.normalizeFields)); err != nil {
				errs := make([]error, 0, len(batch)+1)
				errs = append(errs, err)
				for _, save := range batch {
					if recordErr := f.queue.RecordFailure(save, f.retry, f.deadLetters, err); recordErr != nil {
						errs = append(errs, recordErr)
					}
				}
				return errors.Join(errs...)
			}
			if err := f.expireBatch(ctx, batch); err != nil {
				errs := make([]error, 0, len(batch)+1)
				errs = append(errs, err)
				for _, save := range batch {
					if recordErr := f.queue.RecordFailure(save, f.retry, f.deadLetters, err); recordErr != nil {
						errs = append(errs, recordErr)
					}
				}
				return errors.Join(errs...)
			}
			continue
		}
		for i, save := range batch {
			if err := ctx.Err(); err != nil {
				f.queue.Restore(batch[i:])
				return err
			}
			// 底层 SaveFields 可能有编码或归一化副作用，单条刷盘必须使用独立快照。
			flushRecord := f.clone(save.profile)
			if err := f.saveFields(ctx, f.base, flushRecord, save.fields...); err != nil {
				recordErr := f.queue.RecordFailure(save, f.retry, f.deadLetters, err)
				f.queue.Restore(batch[i+1:])
				return errors.Join(err, recordErr)
			}
			if err := f.expireSave(ctx, save); err != nil {
				recordErr := f.queue.RecordFailure(save, f.retry, f.deadLetters, err)
				f.queue.Restore(batch[i+1:])
				return errors.Join(err, recordErr)
			}
		}
	}
}

// AutoExpireTTL 返回自动续期 TTL。
func (f *asyncFlusher[T, F]) AutoExpireTTL() time.Duration {
	if f == nil {
		return 0
	}
	return f.autoExpireTTL
}

func (f *asyncFlusher[T, F]) expireBatch(ctx context.Context, batch []pendingSave[T, F]) error {
	if f.autoExpireTTL <= 0 || f.expire == nil {
		return nil
	}
	for _, save := range batch {
		if err := f.expireSave(ctx, save); err != nil {
			return err
		}
	}
	return nil
}

func (f *asyncFlusher[T, F]) expireSave(ctx context.Context, save pendingSave[T, F]) error {
	if f.autoExpireTTL <= 0 || f.expire == nil {
		return nil
	}
	key, err := f.queue.RecordKey(save.profile)
	if err != nil {
		return err
	}
	return f.expire(ctx, f.base, key, f.autoExpireTTL)
}

func fieldSavesPending[T any, F comparable](batch []pendingSave[T, F], clone CloneFunc[T], normalize func([]F) []F) []FieldSave[T, F] {
	if len(batch) == 0 {
		return nil
	}
	out := make([]FieldSave[T, F], 0, len(batch))
	for _, save := range batch {
		save = save.clone(clone, normalize)
		out = append(out, FieldSave[T, F]{
			Record: save.profile,
			Fields: save.fields,
		})
	}
	return out
}
