package db

import (
	"sort"
	"sync"
	"time"
)

type pendingSave[T any, F comparable] struct {
	profile       T
	fields        []F
	attempts      int
	nextAttemptAt time.Time
	firstFailedAt time.Time
	lastFailedAt  time.Time
	lastError     string
	sequence      uint64
}

type asyncPendingQueue[T any, F comparable] struct {
	mu              sync.Mutex
	pending         map[string]pendingSave[T, F]
	recordKey       KeyFunc[T]
	normalizeKey    func(string) string
	clone           CloneFunc[T]
	normalizeFields func([]F) []F
	nextSequence    uint64
}

func newAsyncPendingQueue[T any, F comparable](
	recordKey KeyFunc[T],
	normalizeKey func(string) string,
	clone CloneFunc[T],
	normalizeFields func([]F) []F,
) *asyncPendingQueue[T, F] {
	if clone == nil {
		clone = IdentityClone[T]
	}
	return &asyncPendingQueue[T, F]{
		pending:         make(map[string]pendingSave[T, F]),
		recordKey:       recordKey,
		normalizeKey:    normalizeKey,
		clone:           clone,
		normalizeFields: normalizeFields,
	}
}

// Get 返回 pending 队列里的记录副本。
func (q *asyncPendingQueue[T, F]) Get(accountID string) (pendingSave[T, F], bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	save, ok := q.pending[accountID]
	if !ok {
		return pendingSave[T, F]{}, false
	}
	return save.clone(q.clone, q.normalizeFields), true
}

// Add 添加或合并 pending 保存请求。
func (q *asyncPendingQueue[T, F]) Add(accountID string, save pendingSave[T, F]) int {
	accountID = q.NormalizeRecordKey(accountID)
	save = save.clone(q.clone, q.normalizeFields)

	q.mu.Lock()
	defer q.mu.Unlock()
	if existing, ok := q.pending[accountID]; ok {
		merged := existing.clone(q.clone, q.normalizeFields)
		merged.profile = q.clone(save.profile)
		merged.fields = q.Merge(merged.fields, save.fields)
		save = merged
	}
	if save.sequence == 0 {
		q.nextSequence++
		save.sequence = q.nextSequence
	}
	q.pending[accountID] = save
	return len(q.pending)
}

// Requeue 把失败保存重新放回 pending 队列。
func (q *asyncPendingQueue[T, F]) Requeue(accountID string, save pendingSave[T, F]) int {
	accountID = q.NormalizeRecordKey(accountID)
	save = save.clone(q.clone, q.normalizeFields)

	q.mu.Lock()
	defer q.mu.Unlock()
	if existing, ok := q.pending[accountID]; ok {
		merged := existing.clone(q.clone, q.normalizeFields)
		merged.fields = q.Merge(merged.fields, save.fields)
		if merged.sequence == 0 {
			q.nextSequence++
			merged.sequence = q.nextSequence
		}
		q.pending[accountID] = merged
		return len(q.pending)
	}
	if save.sequence == 0 {
		q.nextSequence++
		save.sequence = q.nextSequence
	}
	q.pending[accountID] = save
	return len(q.pending)
}

// Take 取出当前到期的 pending 保存请求。
func (q *asyncPendingQueue[T, F]) Take(force bool, now time.Time) []pendingSave[T, F] {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil
	}

	out := make([]keyedPendingSave[T, F], 0, len(q.pending))
	for accountID, save := range q.pending {
		if !force && !save.nextAttemptAt.IsZero() && now.Before(save.nextAttemptAt) {
			continue
		}
		out = append(out, keyedPendingSave[T, F]{
			key:  accountID,
			save: save.clone(q.clone, q.normalizeFields),
		})
		delete(q.pending, accountID)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].save.sequence == out[j].save.sequence {
			return out[i].key < out[j].key
		}
		return out[i].save.sequence < out[j].save.sequence
	})

	saves := make([]pendingSave[T, F], 0, len(out))
	for _, item := range out {
		saves = append(saves, item.save)
	}
	return saves
}

// Restore 把未尝试的保存请求恢复到 pending 队列。
func (q *asyncPendingQueue[T, F]) Restore(saves []pendingSave[T, F]) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, save := range saves {
		accountID, err := q.RecordKey(save.profile)
		if err != nil {
			continue
		}
		if existing, exists := q.pending[accountID]; exists {
			merged := existing.clone(q.clone, q.normalizeFields)
			merged.fields = q.Merge(merged.fields, save.fields)
			q.pending[accountID] = merged
			continue
		}
		if save.sequence == 0 {
			q.nextSequence++
			save.sequence = q.nextSequence
		}
		q.pending[accountID] = save.clone(q.clone, q.normalizeFields)
	}
}

// RecordFailure 记录保存失败，并按重试策略放回队列或转入死信。
func (q *asyncPendingQueue[T, F]) RecordFailure(save pendingSave[T, F], retry asyncRetryPolicy[T, F], sink *asyncDeadLetterSink[T, F], err error) error {
	save = retry.RecordFailure(save, err, time.Now().UTC())
	accountID, keyErr := q.RecordKey(save.profile)
	if keyErr != nil {
		return keyErr
	}

	q.mu.Lock()
	if existing, exists := q.pending[accountID]; exists {
		merged := existing.clone(q.clone, q.normalizeFields)
		merged.fields = q.Merge(merged.fields, save.fields)
		if save.sequence == 0 || merged.sequence <= save.sequence {
			merged.attempts = save.attempts
			merged.firstFailedAt = save.firstFailedAt
			merged.lastFailedAt = save.lastFailedAt
			merged.lastError = save.lastError
			merged.nextAttemptAt = save.nextAttemptAt
		}
		if merged.sequence == 0 {
			q.nextSequence++
			merged.sequence = q.nextSequence
		}
		if retry.Exhausted(merged) {
			delete(q.pending, accountID)
			q.mu.Unlock()
			if err := sink.Add(merged); err != nil {
				q.Requeue(accountID, merged)
				return err
			}
			return nil
		}
		q.pending[accountID] = merged
		q.mu.Unlock()
		return nil
	}
	if retry.Exhausted(save) {
		q.mu.Unlock()
		if err := sink.Add(save); err != nil {
			q.Requeue(accountID, save)
			return err
		}
		return nil
	}
	if save.sequence == 0 {
		q.nextSequence++
		save.sequence = q.nextSequence
	}
	q.pending[accountID] = save.clone(q.clone, q.normalizeFields)
	q.mu.Unlock()
	return nil
}

// Stats 返回 pending 总数和当前可重试数量。
func (q *asyncPendingQueue[T, F]) Stats(now time.Time) (int, int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	due := 0
	for _, save := range q.pending {
		if save.nextAttemptAt.IsZero() || !now.Before(save.nextAttemptAt) {
			due++
		}
	}
	return len(q.pending), due
}

// RecordKey 计算并归一化记录主键。
func (q *asyncPendingQueue[T, F]) RecordKey(record T) (string, error) {
	key, err := RecordKey(q.recordKey, record)
	if err != nil {
		return "", err
	}
	return q.NormalizeRecordKey(key), nil
}

// NormalizeRecordKey 归一化记录主键。
func (q *asyncPendingQueue[T, F]) NormalizeRecordKey(key string) string {
	if q.normalizeKey == nil {
		return key
	}
	return q.normalizeKey(key)
}

// NormalizeFields 归一化字段列表。
func (q *asyncPendingQueue[T, F]) NormalizeFields(fields []F) []F {
	if q.normalizeFields == nil {
		return append([]F(nil), fields...)
	}
	return q.normalizeFields(fields)
}

// Merge 合并两组字段并归一化。
func (q *asyncPendingQueue[T, F]) Merge(left, right []F) []F {
	merged := append(append([]F(nil), left...), right...)
	return q.NormalizeFields(merged)
}

// Clone 拷贝记录。
func (q *asyncPendingQueue[T, F]) Clone(record T) T {
	return q.clone(record)
}

type keyedPendingSave[T any, F comparable] struct {
	key  string
	save pendingSave[T, F]
}

func (s pendingSave[T, F]) clone(clone CloneFunc[T], normalize func([]F) []F) pendingSave[T, F] {
	if clone == nil {
		clone = IdentityClone[T]
	}
	s.profile = clone(s.profile)
	if normalize != nil {
		s.fields = normalize(s.fields)
	} else {
		s.fields = append([]F(nil), s.fields...)
	}
	return s
}
