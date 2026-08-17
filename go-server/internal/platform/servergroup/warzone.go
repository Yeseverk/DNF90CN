package servergroup

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	defInZoneDelay = 24 * 60 * 60
	defNoticeLead  = 24 * 60 * 60
)

// WarZoneRefreshRequest 是刷新战区状态的请求。
type WarZoneRefreshRequest struct {
	Groups             []WarZoneInput       `json:"groups"`
	ShardOpenTimes     map[string]time.Time `json:"shard_open_times,omitempty"`
	At                 time.Time            `json:"at,omitempty"`
	InZoneDelaySeconds int                  `json:"in_zone_delay_seconds,omitempty"`
	NoticeLeadSeconds  int                  `json:"notice_lead_seconds,omitempty"`
	NextVersion        string               `json:"next_version,omitempty"`
	PruneMissing       bool                 `json:"prune_missing,omitempty"`
	Force              bool                 `json:"force,omitempty"`
	Reason             string               `json:"reason,omitempty"`
	OperatorID         string               `json:"operator_id,omitempty"`
	Meta               map[string]string    `json:"meta,omitempty"`
}

// WarZoneInput 是单个战区的外部时钟和配置输入。
type WarZoneInput struct {
	ID            string            `json:"id"`
	LeaderGroupID string            `json:"leader_group_id,omitempty"`
	Shards        []string          `json:"shards,omitempty"`
	State         string            `json:"state,omitempty"`
	Status        string            `json:"status,omitempty"`
	SeasonID      string            `json:"season_id,omitempty"`
	Meta          map[string]string `json:"meta,omitempty"`
}

// WarZoneRefreshPlan 是战区刷新预演后的计划变化。
type WarZoneRefreshPlan struct {
	Request           WarZoneRefreshRequest `json:"request"`
	Valid             bool                  `json:"valid"`
	Previous          Plan                  `json:"previous"`
	Next              Plan                  `json:"next"`
	RollbackPoint     RollbackPoint         `json:"rollback_point"`
	Changes           []WarZoneChange       `json:"changes,omitempty"`
	RemovedWarZones   []string              `json:"removed_war_zones,omitempty"`
	AvailableWarZones []WarZone             `json:"available_war_zones,omitempty"`
	Warnings          []string              `json:"warnings,omitempty"`
}

// WarZoneChange 是单个战区状态变化记录。
type WarZoneChange struct {
	WarZoneID      string    `json:"warzone_id"`
	PreviousStatus string    `json:"previous_status,omitempty"`
	CurrentStatus  string    `json:"current_status"`
	PreviousState  string    `json:"previous_state,omitempty"`
	CurrentState   string    `json:"current_state"`
	NoticeAt       time.Time `json:"notice_at,omitempty"`
	OpenAt         time.Time `json:"open_at,omitempty"`
	Shards         []string  `json:"shards,omitempty"`
	Reason         string    `json:"reason,omitempty"`
}

// WarZoneRuntime 抽象战区刷新所需的时间和输入来源。
type WarZoneRuntime struct {
	ID            string    `json:"id"`
	Status        string    `json:"status"`
	State         string    `json:"state"`
	LeaderGroupID string    `json:"leader_group_id,omitempty"`
	SeasonID      string    `json:"season_id,omitempty"`
	OpenAt        time.Time `json:"open_at,omitempty"`
	NoticeAt      time.Time `json:"notice_at,omitempty"`
	Shards        []string  `json:"shards,omitempty"`
	Available     bool      `json:"available"`
	Reason        string    `json:"reason,omitempty"`
}

// DryRunWarZoneRefresh 预演战区状态刷新。
func DryRunWarZoneRefresh(ctx context.Context, current Plan, request WarZoneRefreshRequest) (WarZoneRefreshPlan, error) {
	if err := ctxErr(ctx); err != nil {
		return WarZoneRefreshPlan{}, err
	}
	request = normWarzoneRefresh(request)
	if len(request.Groups) == 0 {
		return WarZoneRefreshPlan{}, fmt.Errorf("%w: warzone refresh requires at least one group", ErrInvalidPlan)
	}
	normalized, _, err := normalizePlan(current)
	if err != nil {
		return WarZoneRefreshPlan{}, err
	}
	next := clonePlan(normalized)
	next.Version = warZoneNextVersion(normalized.Version, request.NextVersion, request.At)
	next.UpdatedAt = request.At
	clearShardWarZones(&next)

	previousByID := warZonesByID(normalized.WarZones)
	inputIDs := make(map[string]struct{}, len(request.Groups))
	var changes []WarZoneChange
	var warnings []string
	for _, input := range request.Groups {
		input = normWarInput(input)
		if input.ID == "" {
			return WarZoneRefreshPlan{}, fmt.Errorf("%w: warzone id is required", ErrInvalidPlan)
		}
		if _, exists := inputIDs[input.ID]; exists {
			return WarZoneRefreshPlan{}, fmt.Errorf("%w: duplicate warzone refresh input %s", ErrInvalidPlan, input.ID)
		}
		inputIDs[input.ID] = struct{}{}
		warZone, change, itemWarnings, err := buildWarRefresh(normalized, previousByID[input.ID], input, request)
		if err != nil {
			return WarZoneRefreshPlan{}, err
		}
		next.WarZones = upsertWarZone(next.WarZones, warZone)
		changes = append(changes, change)
		warnings = append(warnings, itemWarnings...)
	}

	var removed []string
	if request.PruneMissing {
		kept := make([]WarZone, 0, len(next.WarZones))
		for _, warZone := range next.WarZones {
			if _, ok := inputIDs[warZone.ID]; ok {
				kept = append(kept, warZone)
				continue
			}
			removed = append(removed, warZone.ID)
		}
		next.WarZones = kept
		sort.Strings(removed)
	}
	manager, err := New(next)
	if err != nil {
		return WarZoneRefreshPlan{}, err
	}
	normalizedNext := manager.Snapshot()
	return WarZoneRefreshPlan{
		Request:           request,
		Valid:             true,
		Previous:          normalized,
		Next:              normalizedNext,
		RollbackPoint:     newRollbackPoint(RollbackOperationWarZoneRefresh, normalized.Version, normalizedNext.Version, request.Reason, request.OperatorID, normalized, normalizedNext.UpdatedAt),
		Changes:           changes,
		RemovedWarZones:   removed,
		AvailableWarZones: AvailableWarZones(normalizedNext),
		Warnings:          dedupeStrings(warnings),
	}, nil
}

// ApplyWarZoneRefresh 应用战区刷新计划到管理器。
func ApplyWarZoneRefresh(ctx context.Context, manager *Manager, request WarZoneRefreshRequest) (WarZoneRefreshPlan, error) {
	if manager == nil {
		return WarZoneRefreshPlan{}, ErrNotFound
	}
	plan, err := DryRunWarZoneRefresh(ctx, manager.Snapshot(), request)
	if err != nil {
		return WarZoneRefreshPlan{}, err
	}
	if err := manager.Replace(ctx, plan.Next); err != nil {
		return WarZoneRefreshPlan{}, err
	}
	return plan, nil
}

// WarZoneSnapshot 返回计划中的战区列表快照。
func WarZoneSnapshot(plan Plan) []WarZoneRuntime {
	normalized, _, err := normalizePlan(plan)
	if err != nil {
		return nil
	}
	out := make([]WarZoneRuntime, 0, len(normalized.WarZones))
	for _, warZone := range normalized.WarZones {
		out = append(out, warZoneRuntime(warZone))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// AvailableWarZones 返回可用战区 ID 列表。
func AvailableWarZones(plan Plan) []WarZone {
	normalized, _, err := normalizePlan(plan)
	if err != nil {
		return nil
	}
	out := make([]WarZone, 0, len(normalized.WarZones))
	for _, warZone := range normalized.WarZones {
		if warZone.State == StateOpen && (warZone.Status == "" || warZone.Status == WarZoneStatusOpened) {
			out = append(out, cloneWarZone(warZone))
		}
	}
	return out
}

// IsWarZoneOpen 判断指定战区是否已开放。
func IsWarZoneOpen(plan Plan, warZoneID string) bool {
	warZoneID = normalizeID(warZoneID)
	for _, item := range WarZoneSnapshot(plan) {
		if item.ID == warZoneID {
			return item.Available
		}
	}
	return false
}

func normWarzoneRefresh(request WarZoneRefreshRequest) WarZoneRefreshRequest {
	request.NextVersion = normalizeID(request.NextVersion)
	request.Reason = strings.TrimSpace(request.Reason)
	request.OperatorID = strings.TrimSpace(request.OperatorID)
	request.Meta = normalizeStringMap(request.Meta)
	request.At = normalizeTime(request.At)
	if request.At.IsZero() {
		request.At = time.Now().UTC()
	}
	if request.InZoneDelaySeconds <= 0 {
		request.InZoneDelaySeconds = defInZoneDelay
	}
	if request.NoticeLeadSeconds < 0 {
		request.NoticeLeadSeconds = 0
	}
	if request.NoticeLeadSeconds == 0 {
		request.NoticeLeadSeconds = defNoticeLead
	}
	openTimes := make(map[string]time.Time, len(request.ShardOpenTimes))
	for shardID, openAt := range request.ShardOpenTimes {
		shardID = normalizeID(shardID)
		openAt = normalizeTime(openAt)
		if shardID == "" || openAt.IsZero() {
			continue
		}
		openTimes[shardID] = openAt
	}
	if len(openTimes) > 0 {
		request.ShardOpenTimes = openTimes
	} else {
		request.ShardOpenTimes = nil
	}
	for idx := range request.Groups {
		request.Groups[idx] = normWarInput(request.Groups[idx])
	}
	return request
}

func normWarInput(input WarZoneInput) WarZoneInput {
	input.ID = normalizeID(input.ID)
	input.LeaderGroupID = normalizeID(input.LeaderGroupID)
	input.Shards = normalizeIDs(input.Shards)
	input.State = normalizeID(input.State)
	input.Status = normWarStatus(input.Status)
	input.SeasonID = normalizeID(input.SeasonID)
	input.Meta = normalizeStringMap(input.Meta)
	return input
}

func buildWarRefresh(current Plan, previous WarZone, input WarZoneInput, request WarZoneRefreshRequest) (WarZone, WarZoneChange, []string, error) {
	status, noticeAt, openAt, warnings, err := computeWarZoneStatus(current, input, request)
	if err != nil {
		return WarZone{}, WarZoneChange{}, nil, err
	}
	if input.Status != "" {
		status = input.Status
	}
	if !request.Force && previous.ID != "" && !canSwitchWarStatus(previous.Status, status) {
		return WarZone{}, WarZoneChange{}, nil, fmt.Errorf("%w: warzone %s cannot switch from %s to %s without force", ErrInvalidPlan, input.ID, previous.Status, status)
	}
	state := input.State
	if state == "" {
		state = stateWarStatus(status)
	}
	meta := mergeWarZoneMeta(input.Meta, request)
	warZone := WarZone{
		ID:            input.ID,
		LeaderGroupID: input.LeaderGroupID,
		Shards:        append([]string(nil), input.Shards...),
		State:         state,
		Status:        status,
		SeasonID:      input.SeasonID,
		NoticeAt:      noticeAt,
		OpenAt:        openAt,
		UpdatedAt:     request.At,
		Meta:          meta,
	}
	if warZone.LeaderGroupID == "" && status == WarZoneStatusOpened {
		warnings = append(warnings, "opened warzone has no leader group:"+warZone.ID)
	}
	if previous.ID != "" && !sameStringSet(previous.Shards, warZone.Shards) {
		warnings = append(warnings, "warzone membership changed:"+warZone.ID)
	}
	change := WarZoneChange{
		WarZoneID:      warZone.ID,
		PreviousStatus: previous.Status,
		CurrentStatus:  warZone.Status,
		PreviousState:  previous.State,
		CurrentState:   warZone.State,
		NoticeAt:       warZone.NoticeAt,
		OpenAt:         warZone.OpenAt,
		Shards:         append([]string(nil), warZone.Shards...),
		Reason:         request.Reason,
	}
	return warZone, change, warnings, nil
}

func computeWarZoneStatus(plan Plan, input WarZoneInput, request WarZoneRefreshRequest) (string, time.Time, time.Time, []string, error) {
	if len(input.Shards) == 0 {
		return WarZoneStatusNoConfig, time.Time{}, time.Time{}, nil, nil
	}
	var warnings []string
	var latestOpen time.Time
	for _, shardID := range input.Shards {
		shard, ok := findShard(plan, shardID)
		if !ok {
			return "", time.Time{}, time.Time{}, nil, fmt.Errorf("%w: warzone %s references missing shard %s", ErrInvalidPlan, input.ID, shardID)
		}
		openAt := request.ShardOpenTimes[shardID]
		if openAt.IsZero() {
			openAt = firstTime(shard.PublicOpenAt, shard.OpenAt)
		}
		if openAt.IsZero() {
			warnings = append(warnings, "shard has no open time:"+shardID)
			return WarZoneStatusNotStart, time.Time{}, time.Time{}, warnings, nil
		}
		if latestOpen.IsZero() || openAt.After(latestOpen) {
			latestOpen = openAt
		}
	}
	if latestOpen.After(request.At) {
		return WarZoneStatusNotOpen, time.Time{}, latestOpen.Add(time.Duration(request.InZoneDelaySeconds) * time.Second), warnings, nil
	}
	openAt := latestOpen.Add(time.Duration(request.InZoneDelaySeconds) * time.Second)
	noticeAt := openAt.Add(-time.Duration(request.NoticeLeadSeconds) * time.Second)
	switch {
	case request.At.Before(noticeAt):
		return WarZoneStatusNotNotice, noticeAt, openAt, warnings, nil
	case request.At.Before(openAt):
		return WarZoneStatusNoticed, noticeAt, openAt, warnings, nil
	default:
		return WarZoneStatusOpened, noticeAt, openAt, warnings, nil
	}
}

func validWarZoneStatus(status string) bool {
	switch status {
	case "", WarZoneStatusNotStart, WarZoneStatusNotOpen, WarZoneStatusNotNotice, WarZoneStatusNoticed, WarZoneStatusOpened, WarZoneStatusNoConfig, WarZoneStatusReverted:
		return true
	default:
		return false
	}
}

func canSwitchWarStatus(previous, current string) bool {
	previous = normWarStatus(previous)
	current = normWarStatus(current)
	if previous == "" || previous == current {
		return true
	}
	switch previous {
	case WarZoneStatusNotStart:
		return current != WarZoneStatusReverted
	case WarZoneStatusNotOpen:
		return current == WarZoneStatusNotNotice || current == WarZoneStatusNoticed || current == WarZoneStatusOpened || current == WarZoneStatusNoConfig
	case WarZoneStatusNotNotice:
		return current == WarZoneStatusNoticed || current == WarZoneStatusOpened || current == WarZoneStatusNoConfig
	case WarZoneStatusNoticed:
		return current == WarZoneStatusOpened
	case WarZoneStatusOpened:
		return false
	case WarZoneStatusNoConfig:
		return current != WarZoneStatusReverted
	case WarZoneStatusReverted:
		return false
	default:
		return false
	}
}

func stateWarStatus(status string) string {
	switch status {
	case "", WarZoneStatusOpened:
		return StateOpen
	case WarZoneStatusNoticed:
		return StateDraining
	default:
		return StateClosed
	}
}

func warZoneRuntime(warZone WarZone) WarZoneRuntime {
	item := WarZoneRuntime{
		ID:            warZone.ID,
		Status:        firstNonEmpty(warZone.Status, WarZoneStatusOpened),
		State:         warZone.State,
		LeaderGroupID: warZone.LeaderGroupID,
		SeasonID:      warZone.SeasonID,
		OpenAt:        warZone.OpenAt,
		NoticeAt:      warZone.NoticeAt,
		Shards:        append([]string(nil), warZone.Shards...),
		Available:     warZone.State == StateOpen && (warZone.Status == "" || warZone.Status == WarZoneStatusOpened),
	}
	if !item.Available {
		if warZone.Status != "" && warZone.Status != WarZoneStatusOpened {
			item.Reason = "warzone_status:" + warZone.Status
		} else {
			item.Reason = "warzone_state:" + warZone.State
		}
	}
	return item
}

func mergeWarZoneMeta(base map[string]string, request WarZoneRefreshRequest) map[string]string {
	out := cloneStringMap(base)
	if out == nil {
		out = make(map[string]string)
	}
	for key, value := range request.Meta {
		out[key] = value
	}
	if request.Reason != "" {
		out["operation.reason"] = request.Reason
	}
	if request.OperatorID != "" {
		out["operation.operator_id"] = request.OperatorID
	}
	out["operation.kind"] = "warzone_refresh"
	return out
}

func clearShardWarZones(plan *Plan) {
	for idx := range plan.Shards {
		plan.Shards[idx].WarZoneID = ""
	}
}

func warZonesByID(warZones []WarZone) map[string]WarZone {
	out := make(map[string]WarZone, len(warZones))
	for _, warZone := range warZones {
		out[warZone.ID] = warZone
	}
	return out
}

func upsertWarZone(warZones []WarZone, next WarZone) []WarZone {
	for idx := range warZones {
		if warZones[idx].ID == next.ID {
			warZones[idx] = next
			return warZones
		}
	}
	return append(warZones, next)
}

func findShard(plan Plan, shardID string) (Shard, bool) {
	shardID = normalizeID(shardID)
	for _, shard := range plan.Shards {
		if shard.ID == shardID {
			return shard, true
		}
	}
	return Shard{}, false
}

func sameStringSet(a, b []string) bool {
	a = normalizeIDs(a)
	b = normalizeIDs(b)
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx] != b[idx] {
			return false
		}
	}
	return true
}

func warZoneNextVersion(current, requested string, at time.Time) string {
	if requested = normalizeID(requested); requested != "" {
		return requested
	}
	current = normalizeID(current)
	suffix := "warzone-" + at.UTC().Format("20060102T150405Z")
	if current == "" {
		return suffix
	}
	return current + "-" + suffix
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
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
	sort.Strings(out)
	return out
}
