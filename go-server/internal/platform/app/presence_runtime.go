package app

import (
	"fmt"
	"time"

	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/db"
	"longheng.io/server/internal/platform/metrics"
	"longheng.io/server/internal/platform/presence"
	rpckit "longheng.io/server/internal/platform/rpc"
)

func regPresenceMetrics(reg *metrics.Registry, runtime presence.Runtime, cfg config.ServiceConfig) {
	if reg == nil {
		return
	}
	observer, ok := runtime.(presence.MetricsObserver)
	if !ok {
		return
	}
	labels := map[string]string{
		"service": cfg.Service.Name,
		"node_id": cfg.Service.NodeID,
	}
	reg.RegisterObserver("presence", func(target *metrics.Registry) {
		observer.ObserveMetrics(target, labels)
	})
}

func registerRPCMetrics(reg *metrics.Registry, endpoint *rpckit.Endpoint, cfg config.ServiceConfig) {
	if reg == nil || endpoint == nil {
		return
	}
	labels := map[string]string{
		"service": cfg.Service.Name,
		"node_id": cfg.Service.NodeID,
	}
	endpoint.SetMetrics(reg, labels)
	reg.RegisterObserver("rpc", func(target *metrics.Registry) {
		endpoint.ObserveMetrics(target, labels)
	})
}

func newPresenceRuntime(cfg config.ServiceConfig) (presence.Runtime, error) {
	ttl := time.Duration(cfg.Presence.TTLSeconds) * time.Second
	switch cfg.Presence.Kind {
	case "", "memory":
		return presence.New(presence.Options{Name: cfg.Service.Name + "-presence"}), nil
	case "redis":
		executor := db.NewRedigoExecutor(db.RedigoOptions{
			Address:        cfg.Presence.RedisAddress,
			Password:       cfg.Presence.RedisPassword,
			DB:             cfg.Presence.RedisDB,
			PoolSize:       cfg.Presence.RedisPoolSize,
			Timeout:        time.Duration(cfg.Presence.RedisTimeoutSeconds) * time.Second,
			ConnectTimeout: time.Duration(cfg.Presence.RedisConnectTimeout) * time.Second,
			ReadTimeout:    time.Duration(cfg.Presence.RedisReadTimeout) * time.Second,
			WriteTimeout:   time.Duration(cfg.Presence.RedisWriteTimeout) * time.Second,
		})
		return presence.NewRedis(presence.RedisOptions{
			Name:      cfg.Service.Name + "-presence",
			Executor:  executor,
			KeyPrefix: cfg.Presence.KeyPrefix,
			TTL:       ttl,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported presence.kind %q", cfg.Presence.Kind)
	}
}
