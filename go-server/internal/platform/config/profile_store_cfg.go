package config

import (
	"fmt"
	"strconv"
	"strings"
)

func validateMySQLShards(errs *[]error, section ProfileStoreSection) {
	if !section.MySQLShardingEnabled && len(section.MySQLShards) == 0 {
		return
	}
	if len(section.MySQLShards) == 0 {
		*errs = append(*errs, fmt.Errorf("profile_store.mysql_shards is required when mysql_sharding_enabled is true"))
		return
	}
	seen := map[string]struct{}{}
	useHashSlots := false
	hashSlotOwners := make([]string, 1024)
	for idx, shard := range section.MySQLShards {
		field := fmt.Sprintf("profile_store.mysql_shards[%d]", idx)
		requireSafeName(errs, field+".id", shard.ID)
		if _, ok := seen[shard.ID]; ok {
			*errs = append(*errs, fmt.Errorf("%s.id %q is duplicated", field, shard.ID))
		}
		seen[shard.ID] = struct{}{}
		if strings.TrimSpace(shard.DSN) == "" && strings.TrimSpace(section.MySQLDSN) == "" {
			*errs = append(*errs, fmt.Errorf("%s.dsn is required when profile_store.mysql_dsn is empty", field))
		}
		if strings.TrimSpace(shard.TableName) != "" {
			requireSQLIdentifier(errs, field+".table_name", shard.TableName)
		}
		if strings.TrimSpace(shard.TablePrefix) != "" {
			requireSQLIdentifier(errs, field+".table_prefix", shard.TablePrefix)
		}
		if strings.TrimSpace(shard.TableName) != "" && strings.TrimSpace(shard.TablePrefix) != "" {
			*errs = append(*errs, fmt.Errorf("%s can set table_name or table_prefix, not both", field))
		}
		if strings.TrimSpace(shard.HashSlots) != "" {
			useHashSlots = true
			ranges, err := parseHashSlotRanges(shard.HashSlots)
			if err != nil {
				*errs = append(*errs, fmt.Errorf("%s.hash_slots %w", field, err))
			} else {
				for _, item := range ranges {
					for slot := item.start; slot <= item.end; slot++ {
						if hashSlotOwners[slot] != "" {
							*errs = append(*errs, fmt.Errorf("%s.hash_slots overlaps slot %d with shard %q", field, slot, hashSlotOwners[slot]))
						}
						hashSlotOwners[slot] = shard.ID
					}
				}
			}
		}
		if shard.MaxOpenConns < 0 {
			*errs = append(*errs, fmt.Errorf("%s.max_open_conns must be non-negative", field))
		}
		if shard.MaxIdleConns < 0 {
			*errs = append(*errs, fmt.Errorf("%s.max_idle_conns must be non-negative", field))
		}
		if shard.ConnMaxLifetime < 0 {
			*errs = append(*errs, fmt.Errorf("%s.conn_max_lifetime_seconds must be non-negative", field))
		}
	}
	if useHashSlots {
		for idx, shard := range section.MySQLShards {
			if strings.TrimSpace(shard.HashSlots) == "" {
				*errs = append(*errs, fmt.Errorf("profile_store.mysql_shards[%d].hash_slots is required when any shard uses slot routing", idx))
			}
		}
		for slot, owner := range hashSlotOwners {
			if owner == "" {
				*errs = append(*errs, fmt.Errorf("profile_store.mysql_shards hash slot %d is not assigned", slot))
				break
			}
		}
	}
}

type profileHashSlots struct {
	start int
	end   int
}

func parseHashSlotRanges(value string) ([]profileHashSlots, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("is required")
	}
	parts := strings.Split(value, ",")
	out := make([]profileHashSlots, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("contains empty range")
		}
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return nil, fmt.Errorf("range %q is invalid", part)
		}
		start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
		if err != nil {
			return nil, fmt.Errorf("range %q has invalid start", part)
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("range %q has invalid end", part)
			}
		}
		if start < 0 || end < start || end >= 1024 {
			return nil, fmt.Errorf("range %q must be within 0-1023", part)
		}
		out = append(out, profileHashSlots{start: start, end: end})
	}
	return out, nil
}
