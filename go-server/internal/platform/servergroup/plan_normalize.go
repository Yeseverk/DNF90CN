package servergroup

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func normalizePlan(plan Plan) (Plan, planIndex, error) {
	plan.Version = normalizeID(plan.Version)
	if plan.UpdatedAt.IsZero() {
		plan.UpdatedAt = time.Now().UTC()
	} else {
		plan.UpdatedAt = plan.UpdatedAt.UTC()
	}
	index := planIndex{
		shards:      make(map[string]Shard, len(plan.Shards)),
		groups:      make(map[string]Group, len(plan.Groups)),
		warZones:    make(map[string]WarZone, len(plan.WarZones)),
		mergeGroups: make(map[string]MergeGroup, len(plan.MergeGroups)),
		routes:      make(map[string]Route, len(plan.Routes)),
		conflicts:   make(map[string]Conflict, len(plan.Conflicts)),
	}
	for _, group := range plan.Groups {
		group = normalizeGroup(group)
		if group.ID == "" {
			return Plan{}, planIndex{}, fmt.Errorf("%w: group id is required", ErrInvalidPlan)
		}
		if !validState(group.State) {
			return Plan{}, planIndex{}, fmt.Errorf("%w: group %s has invalid state %s", ErrInvalidPlan, group.ID, group.State)
		}
		if _, exists := index.groups[group.ID]; exists {
			return Plan{}, planIndex{}, fmt.Errorf("%w: duplicate group %s", ErrInvalidPlan, group.ID)
		}
		index.groups[group.ID] = group
	}
	for _, shard := range plan.Shards {
		shard = normalizeShard(shard)
		if shard.ID == "" || shard.GroupID == "" {
			return Plan{}, planIndex{}, fmt.Errorf("%w: shard id and group id are required", ErrInvalidPlan)
		}
		if !validState(shard.State) {
			return Plan{}, planIndex{}, fmt.Errorf("%w: shard %s has invalid state %s", ErrInvalidPlan, shard.ID, shard.State)
		}
		if _, exists := index.shards[shard.ID]; exists {
			return Plan{}, planIndex{}, fmt.Errorf("%w: duplicate shard %s", ErrInvalidPlan, shard.ID)
		}
		group, ok := index.groups[shard.GroupID]
		if !ok {
			return Plan{}, planIndex{}, fmt.Errorf("%w: shard %s references missing group %s", ErrInvalidPlan, shard.ID, shard.GroupID)
		}
		index.shards[shard.ID] = shard
		group.Shards = appendUnique(group.Shards, shard.ID)
		index.groups[shard.GroupID] = group
	}
	for _, group := range index.groups {
		for _, shardID := range group.Shards {
			shard, ok := index.shards[shardID]
			if !ok {
				return Plan{}, planIndex{}, fmt.Errorf("%w: group %s references missing shard %s", ErrInvalidPlan, group.ID, shardID)
			}
			if shard.GroupID != group.ID {
				return Plan{}, planIndex{}, fmt.Errorf("%w: group %s references shard %s owned by group %s", ErrInvalidPlan, group.ID, shardID, shard.GroupID)
			}
		}
	}
	for _, warZone := range plan.WarZones {
		warZone = normalizeWarZone(warZone)
		if warZone.ID == "" {
			return Plan{}, planIndex{}, fmt.Errorf("%w: warzone id is required", ErrInvalidPlan)
		}
		if !validWarZoneStatus(warZone.Status) {
			return Plan{}, planIndex{}, fmt.Errorf("%w: warzone %s has invalid status %s", ErrInvalidPlan, warZone.ID, warZone.Status)
		}
		if !validState(warZone.State) {
			return Plan{}, planIndex{}, fmt.Errorf("%w: warzone %s has invalid state %s", ErrInvalidPlan, warZone.ID, warZone.State)
		}
		if !warZone.NoticeAt.IsZero() && !warZone.OpenAt.IsZero() && warZone.NoticeAt.After(warZone.OpenAt) {
			return Plan{}, planIndex{}, fmt.Errorf("%w: warzone %s notice_at must not be after open_at", ErrInvalidPlan, warZone.ID)
		}
		if _, exists := index.warZones[warZone.ID]; exists {
			return Plan{}, planIndex{}, fmt.Errorf("%w: duplicate warzone %s", ErrInvalidPlan, warZone.ID)
		}
		if warZone.LeaderGroupID != "" {
			if _, ok := index.groups[warZone.LeaderGroupID]; !ok {
				return Plan{}, planIndex{}, fmt.Errorf("%w: warzone %s references missing leader group %s", ErrInvalidPlan, warZone.ID, warZone.LeaderGroupID)
			}
		}
		for _, shardID := range warZone.Shards {
			shard, ok := index.shards[shardID]
			if !ok {
				return Plan{}, planIndex{}, fmt.Errorf("%w: warzone %s references missing shard %s", ErrInvalidPlan, warZone.ID, shardID)
			}
			if shard.WarZoneID != "" && shard.WarZoneID != warZone.ID {
				return Plan{}, planIndex{}, fmt.Errorf("%w: shard %s belongs to warzone %s and %s", ErrInvalidPlan, shardID, shard.WarZoneID, warZone.ID)
			}
			shard.WarZoneID = warZone.ID
			index.shards[shardID] = shard
		}
		index.warZones[warZone.ID] = warZone
	}
	for shardID, shard := range index.shards {
		if shard.WarZoneID == "" {
			continue
		}
		warZone, ok := index.warZones[shard.WarZoneID]
		if !ok {
			return Plan{}, planIndex{}, fmt.Errorf("%w: shard %s references missing warzone %s", ErrInvalidPlan, shardID, shard.WarZoneID)
		}
		if !containsID(warZone.Shards, shardID) {
			return Plan{}, planIndex{}, fmt.Errorf("%w: shard %s references warzone %s but is not listed by it", ErrInvalidPlan, shardID, shard.WarZoneID)
		}
	}
	for _, mergeGroup := range plan.MergeGroups {
		mergeGroup = normalizeMergeGroup(mergeGroup)
		if mergeGroup.ID == "" || mergeGroup.MainShardID == "" {
			return Plan{}, planIndex{}, fmt.Errorf("%w: merge group id and main shard id are required", ErrInvalidPlan)
		}
		if !validState(mergeGroup.State) {
			return Plan{}, planIndex{}, fmt.Errorf("%w: merge group %s has invalid state %s", ErrInvalidPlan, mergeGroup.ID, mergeGroup.State)
		}
		if _, exists := index.mergeGroups[mergeGroup.ID]; exists {
			return Plan{}, planIndex{}, fmt.Errorf("%w: duplicate merge group %s", ErrInvalidPlan, mergeGroup.ID)
		}
		if _, ok := index.shards[mergeGroup.MainShardID]; !ok {
			return Plan{}, planIndex{}, fmt.Errorf("%w: merge group %s references missing main shard %s", ErrInvalidPlan, mergeGroup.ID, mergeGroup.MainShardID)
		}
		mergeGroup.Shards = appendUnique(mergeGroup.Shards, mergeGroup.MainShardID)
		for _, shardID := range mergeGroup.Shards {
			shard, ok := index.shards[shardID]
			if !ok {
				return Plan{}, planIndex{}, fmt.Errorf("%w: merge group %s references missing shard %s", ErrInvalidPlan, mergeGroup.ID, shardID)
			}
			if shard.MergeGroupID != "" && shard.MergeGroupID != mergeGroup.ID {
				return Plan{}, planIndex{}, fmt.Errorf("%w: shard %s belongs to merge group %s and %s", ErrInvalidPlan, shardID, shard.MergeGroupID, mergeGroup.ID)
			}
			shard.MergeGroupID = mergeGroup.ID
			index.shards[shardID] = shard
		}
		index.mergeGroups[mergeGroup.ID] = mergeGroup
	}
	for shardID, shard := range index.shards {
		if shard.MergeGroupID == "" {
			continue
		}
		mergeGroup, ok := index.mergeGroups[shard.MergeGroupID]
		if !ok {
			return Plan{}, planIndex{}, fmt.Errorf("%w: shard %s references missing merge group %s", ErrInvalidPlan, shardID, shard.MergeGroupID)
		}
		if !containsID(mergeGroup.Shards, shardID) {
			return Plan{}, planIndex{}, fmt.Errorf("%w: shard %s references merge group %s but is not listed by it", ErrInvalidPlan, shardID, shard.MergeGroupID)
		}
	}
	for _, route := range plan.Routes {
		route = normalizeRoute(route)
		if route.Feature == "" || route.ShardID == "" || route.GroupID == "" {
			return Plan{}, planIndex{}, fmt.Errorf("%w: route feature, shard id and group id are required", ErrInvalidPlan)
		}
		if !validState(route.State) {
			return Plan{}, planIndex{}, fmt.Errorf("%w: route %s/%s has invalid state %s", ErrInvalidPlan, route.Feature, route.ShardID, route.State)
		}
		if _, ok := index.shards[route.ShardID]; !ok {
			return Plan{}, planIndex{}, fmt.Errorf("%w: route references missing shard %s", ErrInvalidPlan, route.ShardID)
		}
		if _, ok := index.groups[route.GroupID]; !ok {
			return Plan{}, planIndex{}, fmt.Errorf("%w: route references missing group %s", ErrInvalidPlan, route.GroupID)
		}
		key := routeKey(route.Feature, route.ShardID)
		if _, exists := index.routes[key]; exists {
			return Plan{}, planIndex{}, fmt.Errorf("%w: duplicate route %s/%s", ErrInvalidPlan, route.Feature, route.ShardID)
		}
		index.routes[key] = route
	}
	for _, conflict := range plan.Conflicts {
		conflict = normalizeConflict(conflict)
		if conflict.Feature == "" || (conflict.ShardID == "" && conflict.GroupID == "") {
			return Plan{}, planIndex{}, fmt.Errorf("%w: conflict feature and scope are required", ErrInvalidPlan)
		}
		if conflict.ShardID != "" {
			if _, ok := index.shards[conflict.ShardID]; !ok {
				return Plan{}, planIndex{}, fmt.Errorf("%w: conflict references missing shard %s", ErrInvalidPlan, conflict.ShardID)
			}
		}
		if conflict.GroupID != "" {
			if _, ok := index.groups[conflict.GroupID]; !ok {
				return Plan{}, planIndex{}, fmt.Errorf("%w: conflict references missing group %s", ErrInvalidPlan, conflict.GroupID)
			}
		}
		index.conflicts[conflictKey(conflict.Feature, conflict.ShardID, conflict.GroupID)] = conflict
	}
	normalized := Plan{
		Version:     plan.Version,
		Shards:      make([]Shard, 0, len(index.shards)),
		Groups:      make([]Group, 0, len(index.groups)),
		WarZones:    make([]WarZone, 0, len(index.warZones)),
		MergeGroups: make([]MergeGroup, 0, len(index.mergeGroups)),
		Routes:      make([]Route, 0, len(index.routes)),
		Conflicts:   make([]Conflict, 0, len(index.conflicts)),
		UpdatedAt:   plan.UpdatedAt,
	}
	for _, shard := range index.shards {
		normalized.Shards = append(normalized.Shards, cloneShard(shard))
	}
	for _, group := range index.groups {
		sort.Strings(group.Shards)
		index.groups[group.ID] = group
		normalized.Groups = append(normalized.Groups, cloneGroup(group))
	}
	for _, warZone := range index.warZones {
		sort.Strings(warZone.Shards)
		index.warZones[warZone.ID] = warZone
		normalized.WarZones = append(normalized.WarZones, cloneWarZone(warZone))
	}
	for _, mergeGroup := range index.mergeGroups {
		sort.Strings(mergeGroup.Shards)
		index.mergeGroups[mergeGroup.ID] = mergeGroup
		normalized.MergeGroups = append(normalized.MergeGroups, cloneMergeGroup(mergeGroup))
	}
	for _, route := range index.routes {
		normalized.Routes = append(normalized.Routes, cloneRoute(route))
	}
	for _, conflict := range index.conflicts {
		normalized.Conflicts = append(normalized.Conflicts, conflict)
	}
	sortPlan(&normalized)
	return normalized, index, nil
}

func normalizeShard(shard Shard) Shard {
	shard.ID = normalizeID(shard.ID)
	shard.GroupID = normalizeID(shard.GroupID)
	shard.State = normalizeState(shard.State)
	shard.OpenAt = normalizeTime(shard.OpenAt)
	shard.PublicOpenAt = normalizeTime(shard.PublicOpenAt)
	shard.WarZoneID = normalizeID(shard.WarZoneID)
	shard.MergeGroupID = normalizeID(shard.MergeGroupID)
	shard.UpdatedAt = normalizeTime(shard.UpdatedAt)
	shard.Meta = normalizeStringMap(shard.Meta)
	return shard
}

func normalizeGroup(group Group) Group {
	group.ID = normalizeID(group.ID)
	group.Service = normalizeID(group.Service)
	group.MemberID = normalizeID(group.MemberID)
	group.State = normalizeState(group.State)
	group.Weight = normalizeWeight(group.Weight)
	group.UpdatedAt = normalizeTime(group.UpdatedAt)
	group.Shards = normalizeIDs(group.Shards)
	group.Meta = normalizeStringMap(group.Meta)
	return group
}

func normalizeWarZone(warZone WarZone) WarZone {
	warZone.ID = normalizeID(warZone.ID)
	warZone.LeaderGroupID = normalizeID(warZone.LeaderGroupID)
	warZone.Shards = normalizeIDs(warZone.Shards)
	warZone.State = normalizeState(warZone.State)
	warZone.Status = normWarStatus(warZone.Status)
	warZone.SeasonID = normalizeID(warZone.SeasonID)
	warZone.NoticeAt = normalizeTime(warZone.NoticeAt)
	warZone.OpenAt = normalizeTime(warZone.OpenAt)
	warZone.UpdatedAt = normalizeTime(warZone.UpdatedAt)
	warZone.Meta = normalizeStringMap(warZone.Meta)
	return warZone
}

func normalizeMergeGroup(mergeGroup MergeGroup) MergeGroup {
	mergeGroup.ID = normalizeID(mergeGroup.ID)
	mergeGroup.MainShardID = normalizeID(mergeGroup.MainShardID)
	mergeGroup.Shards = normalizeIDs(mergeGroup.Shards)
	mergeGroup.State = normalizeState(mergeGroup.State)
	mergeGroup.UpdatedAt = normalizeTime(mergeGroup.UpdatedAt)
	mergeGroup.Meta = normalizeStringMap(mergeGroup.Meta)
	return mergeGroup
}

func normalizeRoute(route Route) Route {
	route.Feature = normalizeID(route.Feature)
	route.ShardID = normalizeID(route.ShardID)
	route.GroupID = normalizeID(route.GroupID)
	route.Service = normalizeID(route.Service)
	route.MemberID = normalizeID(route.MemberID)
	route.State = normalizeState(route.State)
	route.UpdatedAt = normalizeTime(route.UpdatedAt)
	route.Meta = normalizeStringMap(route.Meta)
	return route
}

func normalizeConflict(conflict Conflict) Conflict {
	conflict.Feature = normalizeID(conflict.Feature)
	conflict.ShardID = normalizeID(conflict.ShardID)
	conflict.GroupID = normalizeID(conflict.GroupID)
	conflict.Reason = strings.TrimSpace(conflict.Reason)
	return conflict
}

func normalizeState(state string) string {
	state = normalizeID(state)
	if state == "" {
		return StateOpen
	}
	return state
}

func validState(state string) bool {
	switch state {
	case StateOpen, StateClosed, StateDraining:
		return true
	default:
		return false
	}
}

func normWarStatus(status string) string {
	status = normalizeID(status)
	if status == "" {
		return ""
	}
	return status
}

func normalizeWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return weight
}

func normalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func normalizeID(value string) string {
	return strings.TrimSpace(value)
}

func normalizeIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeID(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func appendUnique(values []string, value string) []string {
	value = normalizeID(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func containsID(values []string, value string) bool {
	value = normalizeID(value)
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func targetFromGroup(group Group) Target {
	target := Target{
		GroupID:   group.ID,
		Service:   group.Service,
		MemberID:  group.MemberID,
		Shards:    append([]string(nil), group.Shards...),
		Available: group.State == StateOpen,
		Meta:      cloneStringMap(group.Meta),
	}
	if !target.Available {
		target.Reason = "group_state:" + group.State
	}
	return target
}

func routeKey(feature, shardID string) string {
	return feature + "\x00" + shardID
}

func conflictKey(feature, shardID, groupID string) string {
	return feature + "\x00" + shardID + "\x00" + groupID
}
