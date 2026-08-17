package eventlog

import (
	"context"
	"sort"
	"time"

	"longheng.io/server/internal/platform/metrics"
)

const (
	// MetricEvents 记录 eventlog 各状态事件数量。
	MetricEvents = "eventlog_events"

	// MetricOldestDueUnixSeconds 记录最早到期待发布事件的 Unix 时间。
	MetricOldestDueUnixSeconds = "eventlog_oldest_due_unix_seconds"

	// MetricPublishLagSeconds 记录待发布事件相对当前时间的滞后秒数。
	MetricPublishLagSeconds = "eventlog_publish_lag_seconds"

	// MetricPublishEventsTotal 记录发布结果分类累计数。
	MetricPublishEventsTotal = "eventlog_publish_events_total"

	// MetricPublishBatchesTotal 记录发布扫描批次累计数。
	MetricPublishBatchesTotal = "eventlog_publish_batches_total"

	// MetricPublishBatchSize 记录最近一次发布扫描领取的事件数。
	MetricPublishBatchSize = "eventlog_publish_batch_size"

	// MetricPublishLastRunUnixTime 记录最近一次发布扫描运行时间。
	MetricPublishLastRunUnixTime = "eventlog_publish_last_run_unix_seconds"
)

// ObserveSnapshot 读取快照并写入指标注册表。
func (l *Log) ObserveSnapshot(ctx context.Context, registry *metrics.Registry) error {
	if l == nil {
		return ErrStoreRequired
	}
	snapshot, err := l.Snapshot(ctx)
	if err != nil {
		return err
	}
	RecordSnapshotMetrics(registry, snapshot)
	return nil
}

// RecordSnapshotMetrics 把快照中的状态数量和最早到期时间写入指标。
func RecordSnapshotMetrics(registry *metrics.Registry, snapshot Snapshot) {
	recordSnapMetrics(registry, snapshot, time.Now().UTC())
}

func recordSnapMetrics(registry *metrics.Registry, snapshot Snapshot, now time.Time) {
	if registry == nil {
		return
	}
	logName := snapshot.Name
	if logName == "" {
		logName = "eventlog"
	}
	statuses := map[string]int{
		StatusPending:    0,
		StatusProcessing: 0,
		StatusFailed:     0,
		StatusPublished:  0,
		StatusDeadLetter: 0,
	}
	for status, count := range snapshot.ByStatus {
		statuses[status] = count
	}
	ordered := make([]string, 0, len(statuses))
	for status := range statuses {
		ordered = append(ordered, status)
	}
	sort.Strings(ordered)
	for _, status := range ordered {
		registry.SetGauge(MetricEvents, map[string]string{"log": logName, "status": status}, int64(statuses[status]))
	}
	registry.SetGauge(MetricOldestDueUnixSeconds, map[string]string{"log": logName}, unixOrZero(snapshot.OldestDue))
	registry.SetGauge(MetricPublishLagSeconds, map[string]string{"log": logName}, publishLagSeconds(now, snapshot.OldestDue))
}

// RecordPublishMetrics 记录一次发布扫描的批量、结果和运行时间指标。
func RecordPublishMetrics(registry *metrics.Registry, logName string, stats PublishStats, ranAt time.Time) {
	if registry == nil {
		return
	}
	if logName == "" {
		logName = "eventlog"
	}
	labels := map[string]string{"log": logName}
	registry.Inc(MetricPublishBatchesTotal, labels)
	registry.SetGauge(MetricPublishBatchSize, labels, int64(stats.Fetched))
	registry.SetGauge(MetricPublishLastRunUnixTime, labels, unixOrZero(ranAt))
	if stats.Published > 0 {
		registry.Add(MetricPublishEventsTotal, map[string]string{"log": logName, "result": "published"}, int64(stats.Published))
	}
	failed := stats.Failed - stats.DeadLettered
	if failed < 0 {
		failed = 0
	}
	if failed > 0 {
		registry.Add(MetricPublishEventsTotal, map[string]string{"log": logName, "result": "failed"}, int64(failed))
	}
	if stats.DeadLettered > 0 {
		registry.Add(MetricPublishEventsTotal, map[string]string{"log": logName, "result": "dead_lettered"}, int64(stats.DeadLettered))
	}
	skipped := stats.Fetched - stats.Published - stats.Failed
	if skipped > 0 {
		registry.Add(MetricPublishEventsTotal, map[string]string{"log": logName, "result": "skipped"}, int64(skipped))
	}
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().Unix()
}

func publishLagSeconds(now, oldestDue time.Time) int64 {
	if oldestDue.IsZero() {
		return 0
	}
	lag := now.UTC().Sub(oldestDue.UTC())
	if lag <= 0 {
		return 0
	}
	return int64(lag / time.Second)
}
