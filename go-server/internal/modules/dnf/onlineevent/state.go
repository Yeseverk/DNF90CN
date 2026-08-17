package onlineevent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

type creditedInterval struct {
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}

type claimReceipt struct {
	StageID     string          `json:"stage_id"`
	CharacterID string          `json:"character_id"`
	Items       []CommittedItem `json:"items,omitempty"`
	GoldBefore  int64           `json:"gold_before,omitempty"`
	GoldAfter   int64           `json:"gold_after,omitempty"`
	ClaimedAt   time.Time       `json:"claimed_at"`
}

type persistedState struct {
	Version       int                     `json:"version"`
	EventID       string                  `json:"event_id"`
	CalendarDate  string                  `json:"calendar_date,omitempty"`
	OnlineSeconds uint64                  `json:"online_seconds,omitempty"`
	Intervals     []creditedInterval      `json:"intervals,omitempty"`
	Claims        map[string]claimReceipt `json:"claims,omitempty"`
}

func newState(eventID string) persistedState {
	return persistedState{
		Version: stateVersion,
		EventID: eventID,
		Claims:  make(map[string]claimReceipt),
	}
}

func parseState(raw string, definition Definition) (persistedState, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return newState(definition.ID), nil
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var state persistedState
	if err := decoder.Decode(&state); err != nil {
		return persistedState{}, fmt.Errorf("%w: decode: %v", ErrStateInvalid, err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return persistedState{}, err
	}
	if err := validateState(state, definition); err != nil {
		return persistedState{}, err
	}
	if state.Claims == nil {
		state.Claims = make(map[string]claimReceipt)
	}
	state.Intervals = append([]creditedInterval(nil), state.Intervals...)
	for stageID, receipt := range state.Claims {
		receipt.Items = cloneCommittedItems(receipt.Items)
		state.Claims[stageID] = receipt
	}
	return state, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("%w: trailing json", ErrStateInvalid)
	}
	return fmt.Errorf("%w: trailing json: %v", ErrStateInvalid, err)
}

func validateState(state persistedState, definition Definition) error {
	if state.Version != stateVersion || state.EventID != definition.ID {
		return fmt.Errorf(
			"%w: version=%d event=%q",
			ErrStateInvalid,
			state.Version,
			state.EventID,
		)
	}
	if state.CalendarDate == "" {
		if state.OnlineSeconds != 0 || len(state.Intervals) != 0 || len(state.Claims) != 0 {
			return fmt.Errorf("%w: undated state contains progress", ErrStateInvalid)
		}
		return nil
	}
	if _, err := time.ParseInLocation(time.DateOnly, state.CalendarDate, definition.calendar()); err != nil {
		return fmt.Errorf("%w: calendar_date=%q", ErrStateInvalid, state.CalendarDate)
	}
	var total uint64
	var previousEnd uint32
	for index, interval := range state.Intervals {
		if interval.Start >= interval.End || uint64(interval.End) > secondsPerDay ||
			(index > 0 && interval.Start <= previousEnd) {
			return fmt.Errorf("%w: interval[%d]=%+v", ErrStateInvalid, index, interval)
		}
		total += uint64(interval.End - interval.Start)
		previousEnd = interval.End
	}
	if total != state.OnlineSeconds {
		return fmt.Errorf(
			"%w: seconds=%d interval_total=%d",
			ErrStateInvalid,
			state.OnlineSeconds,
			total,
		)
	}
	for stageID, receipt := range state.Claims {
		stage, found := definition.stage(stageID)
		if !found || receipt.StageID != stageID || strings.TrimSpace(receipt.CharacterID) == "" ||
			receipt.ClaimedAt.IsZero() || receipt.GoldBefore < 0 || receipt.GoldAfter < receipt.GoldBefore ||
			receipt.GoldAfter-receipt.GoldBefore != stage.Gold || len(receipt.Items) != len(stage.Items) {
			return fmt.Errorf("%w: claim=%q", ErrStateInvalid, stageID)
		}
		claimDate, _ := calendarDay(definition.calendar(), definition.boundary(), receipt.ClaimedAt)
		if claimDate != state.CalendarDate {
			return fmt.Errorf("%w: claim=%q date=%s", ErrStateInvalid, stageID, claimDate)
		}
		for index, item := range receipt.Items {
			if err := validateCommittedItem(item, stage.Items[index]); err != nil {
				return fmt.Errorf("%w: claim=%q item=%d", ErrStateInvalid, stageID, index)
			}
		}
	}
	return nil
}

func encodeState(state persistedState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("%w: encode: %v", ErrStateInvalid, err)
	}
	return string(encoded), nil
}

func (s persistedState) snapshot() Snapshot {
	return Snapshot{
		EventID:         s.EventID,
		CalendarDate:    s.CalendarDate,
		OnlineSeconds:   s.OnlineSeconds,
		ClaimedStageIDs: sortedClaimIDs(s.Claims),
	}
}

func (s *persistedState) resetTo(calendarDate string) {
	s.CalendarDate = calendarDate
	s.OnlineSeconds = 0
	s.Intervals = nil
	s.Claims = make(map[string]claimReceipt)
}

// creditInterval merges one half-open second interval into the day's union.
// Keeping the compact union makes retries, overlapping sessions, and
// out-of-order server ticks idempotent without trusting a client counter.
func (s *persistedState) creditInterval(start uint32, end uint32) uint64 {
	if start >= end {
		return 0
	}
	before := s.OnlineSeconds
	intervals := append(append([]creditedInterval(nil), s.Intervals...), creditedInterval{Start: start, End: end})
	sort.Slice(intervals, func(left, right int) bool {
		if intervals[left].Start == intervals[right].Start {
			return intervals[left].End < intervals[right].End
		}
		return intervals[left].Start < intervals[right].Start
	})
	merged := make([]creditedInterval, 0, len(intervals))
	for _, interval := range intervals {
		if len(merged) == 0 || interval.Start > merged[len(merged)-1].End {
			merged = append(merged, interval)
			continue
		}
		if interval.End > merged[len(merged)-1].End {
			merged[len(merged)-1].End = interval.End
		}
	}
	var total uint64
	for _, interval := range merged {
		total += uint64(interval.End - interval.Start)
	}
	s.Intervals = merged
	s.OnlineSeconds = total
	return total - before
}
