package onlineevent

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	stateVersion      = 1
	metadataKeyPrefix = "online_event_daily_v1:"
	secondsPerDay     = uint64(24 * 60 * 60)
)

var (
	ErrOwnerUnavailable   = errors.New("online-event owner is unavailable")
	ErrDefinitionInvalid  = errors.New("online-event definition is invalid")
	ErrEventInactive      = errors.New("online-event is inactive")
	ErrObservationInvalid = errors.New("online-event observation is invalid")
	ErrObservationStale   = errors.New("online-event observation is older than persisted progress")
	ErrStateInvalid       = errors.New("online-event persisted state is invalid")
	ErrAccountNotFound    = errors.New("online-event account is not found")
	ErrCharacterNotFound  = errors.New("online-event character is not found")
	ErrAccountMismatch    = errors.New("online-event character account mismatch")
	ErrInventoryNotFound  = errors.New("online-event inventory is not found")
	ErrStageNotFound      = errors.New("online-event reward stage is not found")
	ErrStageLocked        = errors.New("online-event reward stage is not yet unlocked")
	ErrAllocatorRequired  = errors.New("online-event item allocator is required")
	ErrRewardInvalid      = errors.New("online-event reward is invalid")
	ErrGoldOverflow       = errors.New("online-event gold reward would overflow")
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ChinaCalendar is the local DNF profile's UTC+8 event calendar. Callers may
// provide a different fixed/event location in Definition.Calendar.
var ChinaCalendar = time.FixedZone("Asia/Shanghai", 8*60*60)

// DailyBoundary is one local wall-clock service-day boundary. A nil
// Definition.Boundary uses the 86JP ClockService default of 06:00:00. Keeping
// it explicit lets another proved event opt into midnight without overloading
// a zero value as both "unset" and "00:00".
type DailyBoundary struct {
	Hour   int
	Minute int
	Second int
}

var defaultDailyBoundary = DailyBoundary{Hour: 6}

// ItemReward is a catalog-resolved item delta. The event domain never invents
// item IDs, category ranges, stack limits, expiry, or raw entries.
type ItemReward struct {
	ItemID int64
	Count  int64
}

type Stage struct {
	ID              string
	RequiredSeconds uint64
	Gold            int64
	Items           []ItemReward
}

// Definition is a server-owned activity definition. ActiveFrom is inclusive;
// ActiveUntil is exclusive. Zero bounds mean unbounded. Daily stages must fit
// inside one service day because progress and claims reset at the configured
// local wall-clock boundary (06:00 by default).
type Definition struct {
	ID          string
	Calendar    *time.Location
	Boundary    *DailyBoundary
	ActiveFrom  time.Time
	ActiveUntil time.Time
	Stages      []Stage
}

func (d Definition) Validate() error {
	if !validIdentifier(d.ID) {
		return fmt.Errorf("%w: event_id=%q", ErrDefinitionInvalid, d.ID)
	}
	if !d.ActiveFrom.IsZero() && !d.ActiveUntil.IsZero() && !d.ActiveFrom.Before(d.ActiveUntil) {
		return fmt.Errorf("%w: event=%s active range", ErrDefinitionInvalid, d.ID)
	}
	boundary := d.boundary()
	if boundary.Hour < 0 || boundary.Hour > 23 || boundary.Minute < 0 || boundary.Minute > 59 ||
		boundary.Second < 0 || boundary.Second > 59 {
		return fmt.Errorf("%w: event=%s boundary=%+v", ErrDefinitionInvalid, d.ID, boundary)
	}
	if len(d.Stages) == 0 {
		return fmt.Errorf("%w: event=%s has no stages", ErrDefinitionInvalid, d.ID)
	}
	seen := make(map[string]struct{}, len(d.Stages))
	var previous uint64
	for index, stage := range d.Stages {
		if !validIdentifier(stage.ID) {
			return fmt.Errorf("%w: event=%s stage_id=%q", ErrDefinitionInvalid, d.ID, stage.ID)
		}
		if _, duplicate := seen[stage.ID]; duplicate {
			return fmt.Errorf("%w: event=%s duplicate_stage=%s", ErrDefinitionInvalid, d.ID, stage.ID)
		}
		seen[stage.ID] = struct{}{}
		if stage.RequiredSeconds == 0 || stage.RequiredSeconds > secondsPerDay ||
			(index > 0 && stage.RequiredSeconds <= previous) {
			return fmt.Errorf(
				"%w: event=%s stage=%s required_seconds=%d",
				ErrDefinitionInvalid,
				d.ID,
				stage.ID,
				stage.RequiredSeconds,
			)
		}
		previous = stage.RequiredSeconds
		if stage.Gold < 0 || (stage.Gold == 0 && len(stage.Items) == 0) {
			return fmt.Errorf("%w: event=%s stage=%s empty/negative reward", ErrDefinitionInvalid, d.ID, stage.ID)
		}
		for _, item := range stage.Items {
			if item.ItemID <= 0 || item.Count <= 0 {
				return fmt.Errorf(
					"%w: event=%s stage=%s item=%d count=%d",
					ErrDefinitionInvalid,
					d.ID,
					stage.ID,
					item.ItemID,
					item.Count,
				)
			}
		}
	}
	return nil
}

func (d Definition) calendar() *time.Location {
	if d.Calendar != nil {
		return d.Calendar
	}
	return ChinaCalendar
}

func (d Definition) boundary() DailyBoundary {
	if d.Boundary != nil {
		return *d.Boundary
	}
	return defaultDailyBoundary
}

func (d Definition) stage(stageID string) (Stage, bool) {
	stageID = strings.TrimSpace(stageID)
	for _, stage := range d.Stages {
		if stage.ID == stageID {
			stage.Items = append([]ItemReward(nil), stage.Items...)
			return stage, true
		}
	}
	return Stage{}, false
}

func (d Definition) activeAt(at time.Time) bool {
	return (d.ActiveFrom.IsZero() || !at.Before(d.ActiveFrom)) &&
		(d.ActiveUntil.IsZero() || at.Before(d.ActiveUntil))
}

func validIdentifier(value string) bool {
	return value == strings.TrimSpace(value) && identifierPattern.MatchString(value)
}

type ObserveCommand struct {
	AccountID    string
	CharacterID  string
	Definition   Definition
	IntervalFrom time.Time
	IntervalTo   time.Time
}

type ObserveResult struct {
	Snapshot        Snapshot
	CreditedSeconds uint64
	Changed         bool
}

type StatusCommand struct {
	AccountID   string
	CharacterID string
	Definition  Definition
	ObservedAt  time.Time
}

type ClaimCommand struct {
	AccountID   string
	CharacterID string
	Definition  Definition
	StageID     string
	ClaimedAt   time.Time
	Allocate    ItemAllocator
}

// CommittedItem is the detached post-allocation receipt. RawEntry must be the
// current-client row after the supplied allocator mutated the transaction
// clone; it is retained so an idempotent replay can return the same receipt.
type CommittedItem struct {
	SlotKey   string `json:"slot_key"`
	SlotIndex uint16 `json:"slot_index"`
	ItemID    int64  `json:"item_id"`
	Delta     int64  `json:"delta"`
	PostCount int64  `json:"post_count"`
	RawEntry  []byte `json:"raw_entry,omitempty"`
}

type ItemAllocator func(*dnfrepo.InventoryRecord, ItemReward) (CommittedItem, error)

type ClaimResult struct {
	EventID      string
	CalendarDate string
	StageID      string
	CharacterID  string
	Replayed     bool
	Items        []CommittedItem
	GoldBefore   int64
	GoldAfter    int64
	ClaimedAt    time.Time
	PostSnapshot Snapshot
}

type Snapshot struct {
	EventID         string
	CalendarDate    string
	OnlineSeconds   uint64
	ClaimedStageIDs []string
}

func (s Snapshot) clone() Snapshot {
	s.ClaimedStageIDs = append([]string(nil), s.ClaimedStageIDs...)
	return s
}

func sortedClaimIDs(claims map[string]claimReceipt) []string {
	ids := make([]string, 0, len(claims))
	for stageID := range claims {
		ids = append(ids, stageID)
	}
	sort.Strings(ids)
	return ids
}

func cloneCommittedItems(items []CommittedItem) []CommittedItem {
	cloned := make([]CommittedItem, len(items))
	for index, item := range items {
		cloned[index] = item
		cloned[index].RawEntry = append([]byte(nil), item.RawEntry...)
	}
	return cloned
}

func validateCommittedItem(receipt CommittedItem, reward ItemReward) error {
	if strings.TrimSpace(receipt.SlotKey) == "" || receipt.ItemID != reward.ItemID ||
		receipt.Delta != reward.Count || receipt.PostCount < receipt.Delta {
		return fmt.Errorf(
			"%w: item=%d count=%d receipt=%+v",
			ErrRewardInvalid,
			reward.ItemID,
			reward.Count,
			receipt,
		)
	}
	return nil
}

func metadataKey(eventID string) string {
	return metadataKeyPrefix + eventID
}
