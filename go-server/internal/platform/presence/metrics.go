package presence

import (
	"context"
	"strings"
	"time"

	"longheng.io/server/internal/platform/metrics"
)

const (
	// MetricPresenceCount 是在线 session 数 gauge 名称。
	MetricPresenceCount = "presence_count"
	// MetricUserCount 是在线用户数 gauge 名称。
	MetricUserCount = "presence_user_count"
	// MetricSubscriptionCount 是订阅关系数 gauge 名称。
	MetricSubscriptionCount = "presence_subscription_count"
)

// MetricsObserver 表示底层 runtime 暴露了适合 Prometheus scrape 的 gauge。
// Manager 和 RedisManager 都实现该接口，wiring 代码可以通过类型断言挂到
// metrics.Registry，而不需要绑定具体实现。
type MetricsObserver interface {
	ObserveMetrics(reg *metrics.Registry, labels map[string]string)
}

const defRedisObsTimeout = 2 * time.Second

// ObserveMetrics 基于内存快照把 presence_count / presence_user_count /
// presence_subscription_count 发布为 gauge。它只持有 RLock、没有 IO，
// 可以安全用于 scrape 路径。
func (m *Manager) ObserveMetrics(reg *metrics.Registry, labels map[string]string) {
	if m == nil || reg == nil {
		return
	}
	snapshot := m.Snapshot()
	setPresenceGauges(reg, labels, int64(snapshot.PresenceCount), int64(snapshot.UserCount), int64(snapshot.SubscriptionCount))
}

// ObserveMetrics 对共享 Redis presence key 执行两次 SCARD。
// SCARD 是 O(1)，适合 scrape 路径；这里仍用短超时包住 Redis 访问：
// Redis 失败时本次 gauge 不更新，而不是阻塞 metrics endpoint。
func (m *RedisManager) ObserveMetrics(reg *metrics.Registry, labels map[string]string) {
	if m == nil || reg == nil {
		return
	}
	m.ensureReady()
	if m.executor == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), defRedisObsTimeout)
	defer cancel()
	presences, err := m.redisInt(ctx, "SCARD", m.sessionsIndexKey())
	if err != nil {
		return
	}
	users, err := m.redisInt(ctx, "SCARD", m.usersIndexKey())
	if err != nil {
		return
	}
	subscriptions, err := m.redisInt(ctx, "SCARD", m.subIndexKey())
	if err != nil {
		return
	}
	setPresenceGauges(reg, labels, presences, users, subscriptions)
}

func setPresenceGauges(reg *metrics.Registry, labels map[string]string, presences, users, subscriptions int64) {
	gaugeLabels := normObserverLabels(labels)
	reg.SetGauge(MetricPresenceCount, gaugeLabels, presences)
	reg.SetGauge(MetricUserCount, gaugeLabels, users)
	reg.SetGauge(MetricSubscriptionCount, gaugeLabels, subscriptions)
}

func normObserverLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
