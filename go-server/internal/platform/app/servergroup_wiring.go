package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/servergroup"
)

func newGroupManager(ctx context.Context, cfg config.ServiceConfig) (*servergroup.Manager, servergroup.Store, servergroup.MergeArchiveStore, error) {
	if !cfg.ServerGroup.Enabled {
		return nil, nil, nil, nil
	}
	store, err := servergroup.NewFileStore(cfg.ServerGroup.PlanFile)
	if err != nil {
		return nil, nil, nil, err
	}
	manager, err := servergroup.LoadManager(ctx, store, defaultGroupPlan(cfg))
	if err != nil {
		return nil, nil, nil, err
	}
	archives, err := servergroup.NewFileMergeArchiveStore(cfg.ServerGroup.MergeArchiveDir)
	if err != nil {
		return nil, nil, nil, err
	}
	return manager, store, archives, nil
}

func defaultGroupPlan(cfg config.ServiceConfig) servergroup.Plan {
	shardID := strconv.FormatInt(cfg.Cluster.ShardID, 10)
	if cfg.Cluster.ShardID <= 0 {
		shardID = strings.TrimSpace(cfg.Service.Name)
	}
	groupID := strings.TrimSpace(cfg.Service.Name + "-" + cfg.Service.NodeID)
	now := time.Now().UTC()
	openAt := now
	if cfg.Cluster.ShardID <= 0 {
		openAt = time.Time{}
	}
	return servergroup.Plan{
		Version: firstConfigString(cfg.Cluster.Version, "local-servergroup"),
		Shards: []servergroup.Shard{{
			ID:           shardID,
			GroupID:      groupID,
			State:        servergroup.StateOpen,
			OpenAt:       openAt,
			PublicOpenAt: openAt,
			UpdatedAt:    now,
			Meta: map[string]string{
				"cluster_id": cfg.Cluster.ID,
				"gid":        fmt.Sprint(cfg.Cluster.GID),
			},
		}},
		Groups: []servergroup.Group{{
			ID:        groupID,
			Service:   cfg.Service.Name,
			MemberID:  cfg.Service.NodeID,
			State:     servergroup.StateOpen,
			UpdatedAt: now,
		}},
		UpdatedAt: now,
	}
}

func firstConfigString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
