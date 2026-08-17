package servergroup

import "sort"

func sortPlan(plan *Plan) {
	sort.Slice(plan.Shards, func(i, j int) bool {
		return plan.Shards[i].ID < plan.Shards[j].ID
	})
	sort.Slice(plan.Groups, func(i, j int) bool {
		return plan.Groups[i].ID < plan.Groups[j].ID
	})
	sort.Slice(plan.WarZones, func(i, j int) bool {
		return plan.WarZones[i].ID < plan.WarZones[j].ID
	})
	sort.Slice(plan.MergeGroups, func(i, j int) bool {
		return plan.MergeGroups[i].ID < plan.MergeGroups[j].ID
	})
	sort.Slice(plan.Routes, func(i, j int) bool {
		if plan.Routes[i].Feature != plan.Routes[j].Feature {
			return plan.Routes[i].Feature < plan.Routes[j].Feature
		}
		return plan.Routes[i].ShardID < plan.Routes[j].ShardID
	})
	sort.Slice(plan.Conflicts, func(i, j int) bool {
		if plan.Conflicts[i].Feature != plan.Conflicts[j].Feature {
			return plan.Conflicts[i].Feature < plan.Conflicts[j].Feature
		}
		if plan.Conflicts[i].ShardID != plan.Conflicts[j].ShardID {
			return plan.Conflicts[i].ShardID < plan.Conflicts[j].ShardID
		}
		return plan.Conflicts[i].GroupID < plan.Conflicts[j].GroupID
	})
}

func clonePlan(plan Plan) Plan {
	shards := plan.Shards
	groups := plan.Groups
	warZones := plan.WarZones
	mergeGroups := plan.MergeGroups
	routes := plan.Routes
	conflicts := plan.Conflicts
	plan.Shards = make([]Shard, len(shards))
	for i, shard := range shards {
		plan.Shards[i] = cloneShard(shard)
	}
	plan.Groups = make([]Group, len(groups))
	for i, group := range groups {
		plan.Groups[i] = cloneGroup(group)
	}
	plan.WarZones = make([]WarZone, len(warZones))
	for i, warZone := range warZones {
		plan.WarZones[i] = cloneWarZone(warZone)
	}
	plan.MergeGroups = make([]MergeGroup, len(mergeGroups))
	for i, mergeGroup := range mergeGroups {
		plan.MergeGroups[i] = cloneMergeGroup(mergeGroup)
	}
	plan.Routes = make([]Route, len(routes))
	for i, route := range routes {
		plan.Routes[i] = cloneRoute(route)
	}
	plan.Conflicts = append([]Conflict(nil), conflicts...)
	return plan
}

func cloneShard(shard Shard) Shard {
	shard.Meta = cloneStringMap(shard.Meta)
	return shard
}

func cloneGroup(group Group) Group {
	group.Shards = append([]string(nil), group.Shards...)
	group.Meta = cloneStringMap(group.Meta)
	return group
}

func cloneWarZone(warZone WarZone) WarZone {
	warZone.Shards = append([]string(nil), warZone.Shards...)
	warZone.Meta = cloneStringMap(warZone.Meta)
	return warZone
}

func cloneMergeGroup(mergeGroup MergeGroup) MergeGroup {
	mergeGroup.Shards = append([]string(nil), mergeGroup.Shards...)
	mergeGroup.Meta = cloneStringMap(mergeGroup.Meta)
	return mergeGroup
}

func cloneRoute(route Route) Route {
	route.Meta = cloneStringMap(route.Meta)
	return route
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
