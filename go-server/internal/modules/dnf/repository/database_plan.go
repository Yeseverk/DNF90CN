// 本文件负责把框架区服路由转换为 DNF 仓储数据库计划。
package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"longheng.io/server/internal/platform/servergroup"
)

const (
	// FeatureRepository 是 servergroup 中用于解析 DNF 玩家数据仓储的功能名。
	FeatureRepository = "dnf_repository"

	metaDBPrefix       = "dnf.repository."
	metaReadDatabases  = metaDBPrefix + "read_databases"
	metaWriteDatabases = metaDBPrefix + "write_databases"
	metaReadDatabase   = metaDBPrefix + "read_database"
	metaWriteDatabase  = metaDBPrefix + "write_database"
	MetaDatabaseCount  = metaDBPrefix + "database_count"
	MetaReadDBPrefix   = metaDBPrefix + "read_database_prefix"
	MetaWriteDBPrefix  = metaDBPrefix + "write_database_prefix"
	metaDatabasePrefix = metaDBPrefix + "database_prefix"
	metaDatabaseDigits = metaDBPrefix + "database_digits"
	metaDBStart        = metaDBPrefix + "database_start_index"
)

var ErrDatabasePlanInvalid = errors.New("dnf database plan is invalid")

// DatabasePlan 描述当前区服 DNF 仓储的读库和写库。
type DatabasePlan struct {
	Feature        string
	ShardID        string
	GroupID        string
	WriteDatabases []string
	ReadDatabases  []string
}

// ResolveDatabasePlan 通过 servergroup 解析当前区服的 DNF 读写库计划。
// 它只读取区服路由和 meta，不连接 MySQL，也不创建数据库。
func ResolveDatabasePlan(ctx context.Context, manager *servergroup.Manager, shardID string) (DatabasePlan, error) {
	if manager == nil {
		return DatabasePlan{}, fmt.Errorf("%w: servergroup manager is required", ErrDatabasePlanInvalid)
	}
	shardID = strings.TrimSpace(shardID)
	if shardID == "" {
		return DatabasePlan{}, fmt.Errorf("%w: shard id is required", ErrDatabasePlanInvalid)
	}
	target, ok, err := manager.ResolveWrite(ctx, FeatureRepository, shardID)
	if err != nil {
		return DatabasePlan{}, err
	}
	if !ok {
		return DatabasePlan{}, fmt.Errorf("%w: route %s/%s not found", ErrDatabasePlanInvalid, FeatureRepository, shardID)
	}
	if target.RedirectShardID != "" {
		target, ok, err = manager.Resolve(ctx, FeatureRepository, target.RedirectShardID)
		if err != nil {
			return DatabasePlan{}, err
		}
		if !ok {
			return DatabasePlan{}, fmt.Errorf("%w: redirected route %s/%s not found", ErrDatabasePlanInvalid, FeatureRepository, target.RedirectShardID)
		}
	}
	if !target.Available {
		return DatabasePlan{}, fmt.Errorf("%w: route %s/%s unavailable: %s", ErrDatabasePlanInvalid, FeatureRepository, shardID, target.Reason)
	}
	return DatabasePlanFromTarget(target)
}

// DatabasePlanFromTarget 从 servergroup 目标 meta 中派生 DNF 仓储读写库名。
// 它支持显式库名，也支持 prefix + database_count 生成多个库名。
func DatabasePlanFromTarget(target servergroup.Target) (DatabasePlan, error) {
	plan, err := DatabasePlanFromMeta(target.Meta)
	if err != nil {
		return DatabasePlan{}, err
	}
	plan.Feature = firstValue(target.Feature, FeatureRepository)
	plan.ShardID = strings.TrimSpace(target.ShardID)
	plan.GroupID = strings.TrimSpace(target.GroupID)
	return plan, nil
}

// DatabasePlanFromMeta 解析区服配置 meta 中的 DNF 仓储数据库配置。
// 这里不使用硬编码库名，库名必须来自配置中的显式列表或 prefix/count 规则。
func DatabasePlanFromMeta(meta map[string]string) (DatabasePlan, error) {
	writeDBs, err := metaDatabaseList(meta, metaWriteDatabases, metaWriteDatabase, MetaWriteDBPrefix)
	if err != nil {
		return DatabasePlan{}, err
	}
	readDBs, err := metaDatabaseList(meta, metaReadDatabases, metaReadDatabase, MetaReadDBPrefix)
	if err != nil {
		return DatabasePlan{}, err
	}
	if len(writeDBs) == 0 {
		writeDBs, err = metaDatabaseList(meta, "", "", metaDatabasePrefix)
		if err != nil {
			return DatabasePlan{}, err
		}
	}
	if len(readDBs) == 0 {
		readDBs = append([]string(nil), writeDBs...)
	}
	if len(writeDBs) == 0 {
		return DatabasePlan{}, fmt.Errorf("%w: %s or %s is required", ErrDatabasePlanInvalid, metaWriteDatabases, metaDatabasePrefix)
	}
	return DatabasePlan{
		WriteDatabases: writeDBs,
		ReadDatabases:  uniqueDatabases(readDBs),
	}, nil
}

// SchemaDatabases 返回需要执行建库建表的数据库集合。
// 首次启动只对写库建表；读库通常由复制或 DBA 计划提供，避免误写只读副本。
func (p DatabasePlan) SchemaDatabases() []string {
	return uniqueDatabases(p.WriteDatabases)
}

func metaDatabaseList(meta map[string]string, listKey, oneKey, prefixKey string) ([]string, error) {
	if listKey != "" {
		if values := parseDatabaseList(meta[listKey]); len(values) > 0 {
			return ValidateDatabases(values)
		}
	}
	if oneKey != "" {
		if values := parseDatabaseList(meta[oneKey]); len(values) > 0 {
			return ValidateDatabases(values)
		}
	}
	prefix := strings.TrimSpace(meta[prefixKey])
	if prefix == "" {
		return nil, nil
	}
	count, err := metaPositiveInt(meta, MetaDatabaseCount, 0)
	if err != nil {
		return nil, err
	}
	if count <= 0 {
		return nil, fmt.Errorf("%w: %s is required when %s is set", ErrDatabasePlanInvalid, MetaDatabaseCount, prefixKey)
	}
	start, err := metaPositiveInt(meta, metaDBStart, 1)
	if err != nil {
		return nil, err
	}
	digits, err := metaPositiveInt(meta, metaDatabaseDigits, 0)
	if err != nil {
		return nil, err
	}
	values := make([]string, 0, count)
	for idx := 0; idx < count; idx++ {
		n := start + idx
		suffix := strconv.Itoa(n)
		if digits > 0 {
			suffix = fmt.Sprintf("%0*d", digits, n)
		}
		values = append(values, prefix+suffix)
	}
	return ValidateDatabases(values)
}

func parseDatabaseList(value string) []string {
	value = strings.NewReplacer("\n", ",", "\t", ",", ";", ",").Replace(value)
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func ValidateDatabases(values []string) ([]string, error) {
	out := uniqueDatabases(values)
	for _, value := range out {
		if !IsSQLIdentifier(value) {
			return nil, fmt.Errorf("%w: database name %q is invalid", ErrDatabasePlanInvalid, value)
		}
	}
	return out, nil
}

func uniqueDatabases(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func metaPositiveInt(meta map[string]string, key string, fallback int) (int, error) {
	value := strings.TrimSpace(meta[key])
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%w: %s must be a non-negative integer", ErrDatabasePlanInvalid, key)
	}
	return n, nil
}

func firstValue(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func IsSQLIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
