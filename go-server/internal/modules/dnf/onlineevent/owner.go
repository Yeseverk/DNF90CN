package onlineevent

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// Owner reuses CharacterSettlement because it is the existing transaction
// that locks account metadata, the selected character wallet, and inventory
// together. That makes the claim receipt and its rewards one atomic commit.
type Owner struct {
	settlements dnfrepo.CharacterSettlementUnitOfWork
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.CharacterSettlement == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{settlements: repositories.CharacterSettlement}, nil
}

// Status returns the authoritative progress used by a future proved login/UI
// projection. Reading status also persists a service-day reset, so a stale
// prior-day claim ledger cannot survive indefinitely when no new interval has
// yet been credited.
func (o *Owner) Status(ctx context.Context, command StatusCommand) (Snapshot, error) {
	if o == nil || o.settlements == nil {
		return Snapshot{}, ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(command.AccountID)
	characterID := strings.TrimSpace(command.CharacterID)
	if accountID == "" || characterID == "" {
		return Snapshot{}, ErrObservationInvalid
	}
	if err := command.Definition.Validate(); err != nil {
		return Snapshot{}, err
	}
	observedAt := command.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	var snapshot Snapshot
	err := o.settlements.WithinCharacterSettlement(ctx, characterID, func(tx dnfrepo.Group) error {
		account, _, err := loadAccountAndCharacter(ctx, tx, accountID, characterID)
		if err != nil {
			return err
		}
		state, err := parseState(account.Metadata[metadataKey(command.Definition.ID)], command.Definition)
		if err != nil {
			return err
		}
		date, _ := calendarDay(command.Definition.calendar(), command.Definition.boundary(), observedAt)
		if state.CalendarDate != "" && date < state.CalendarDate {
			return fmt.Errorf(
				"%w: persisted=%s observed=%s",
				ErrObservationStale,
				state.CalendarDate,
				date,
			)
		}
		if state.CalendarDate != date {
			state.resetTo(date)
			if err := saveState(ctx, tx.Account, account, state, observedAt); err != nil {
				return err
			}
		}
		snapshot = state.snapshot()
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot.clone(), nil
}

// Observe credits a server-observed presence interval. It stores the union of
// intervals for the current local day, so retries, overlapping local sessions,
// and reordered ticks cannot increase progress twice. Partial seconds are
// conservatively discarded instead of rounded up.
func (o *Owner) Observe(ctx context.Context, command ObserveCommand) (ObserveResult, error) {
	if o == nil || o.settlements == nil {
		return ObserveResult{}, ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(command.AccountID)
	characterID := strings.TrimSpace(command.CharacterID)
	if accountID == "" || characterID == "" || !command.IntervalFrom.Before(command.IntervalTo) {
		return ObserveResult{}, ErrObservationInvalid
	}
	if err := command.Definition.Validate(); err != nil {
		return ObserveResult{}, err
	}
	from, to, err := activeObservationInterval(command.Definition, command.IntervalFrom, command.IntervalTo)
	if err != nil {
		return ObserveResult{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ObserveResult{}, err
	}

	var result ObserveResult
	err = o.settlements.WithinCharacterSettlement(ctx, characterID, func(tx dnfrepo.Group) error {
		account, character, err := loadAccountAndCharacter(ctx, tx, accountID, characterID)
		if err != nil {
			return err
		}
		state, err := parseState(account.Metadata[metadataKey(command.Definition.ID)], command.Definition)
		if err != nil {
			return err
		}
		date, dayStart, intervalStart, intervalEnd := dailyInterval(
			command.Definition.calendar(),
			command.Definition.boundary(),
			from,
			to,
		)
		if state.CalendarDate != "" && date < state.CalendarDate {
			return fmt.Errorf(
				"%w: persisted=%s observed=%s",
				ErrObservationStale,
				state.CalendarDate,
				date,
			)
		}
		changed := false
		if state.CalendarDate != date {
			state.resetTo(date)
			changed = true
		}
		startSecond, endSecond := intervalSeconds(dayStart, intervalStart, intervalEnd)
		credited := state.creditInterval(startSecond, endSecond)
		changed = changed || credited > 0
		if changed {
			if err := saveState(ctx, tx.Account, account, state, to); err != nil {
				return err
			}
		}
		_ = character // loadAccountAndCharacter proves account ownership.
		result = ObserveResult{
			Snapshot:        state.snapshot(),
			CreditedSeconds: credited,
			Changed:         changed,
		}
		return nil
	})
	if err != nil {
		return ObserveResult{}, err
	}
	result.Snapshot = result.Snapshot.clone()
	return result, nil
}

// Claim grants one unlocked stage. The account claim receipt is written only
// after every caller-resolved item and wallet mutation has succeeded inside
// the same CharacterSettlement transaction. Replays return the stored receipt
// and never invoke the allocator again.
func (o *Owner) Claim(ctx context.Context, command ClaimCommand) (ClaimResult, error) {
	if o == nil || o.settlements == nil {
		return ClaimResult{}, ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(command.AccountID)
	characterID := strings.TrimSpace(command.CharacterID)
	stageID := strings.TrimSpace(command.StageID)
	if accountID == "" || characterID == "" || stageID == "" {
		return ClaimResult{}, ErrRewardInvalid
	}
	if err := command.Definition.Validate(); err != nil {
		return ClaimResult{}, err
	}
	stage, found := command.Definition.stage(stageID)
	if !found {
		return ClaimResult{}, fmt.Errorf("%w: event=%s stage=%s", ErrStageNotFound, command.Definition.ID, stageID)
	}
	if len(stage.Items) > 0 && command.Allocate == nil {
		return ClaimResult{}, ErrAllocatorRequired
	}
	claimedAt := command.ClaimedAt
	if claimedAt.IsZero() {
		claimedAt = time.Now()
	}
	if !command.Definition.activeAt(claimedAt) {
		return ClaimResult{}, fmt.Errorf("%w: event=%s", ErrEventInactive, command.Definition.ID)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ClaimResult{}, err
	}

	var result ClaimResult
	err := o.settlements.WithinCharacterSettlement(ctx, characterID, func(tx dnfrepo.Group) error {
		account, character, err := loadAccountAndCharacter(ctx, tx, accountID, characterID)
		if err != nil {
			return err
		}
		state, err := parseState(account.Metadata[metadataKey(command.Definition.ID)], command.Definition)
		if err != nil {
			return err
		}
		date, _ := calendarDay(command.Definition.calendar(), command.Definition.boundary(), claimedAt)
		if state.CalendarDate != "" && date < state.CalendarDate {
			return fmt.Errorf(
				"%w: persisted=%s claimed=%s",
				ErrObservationStale,
				state.CalendarDate,
				date,
			)
		}
		if state.CalendarDate != date {
			state.resetTo(date)
		}
		if receipt, replayed := state.Claims[stageID]; replayed {
			result = resultFromReceipt(command.Definition.ID, state, receipt, true)
			return nil
		}
		if state.OnlineSeconds < stage.RequiredSeconds {
			return fmt.Errorf(
				"%w: event=%s stage=%s have=%d need=%d",
				ErrStageLocked,
				command.Definition.ID,
				stageID,
				state.OnlineSeconds,
				stage.RequiredSeconds,
			)
		}

		character = dnfrepo.CloneCharacter(character)
		if character.Stats == nil {
			character.Stats = make(map[string]int64)
		}
		goldBefore := character.Stats["gold"]
		if goldBefore < 0 || stage.Gold > math.MaxInt64-goldBefore {
			return ErrGoldOverflow
		}
		goldAfter := goldBefore + stage.Gold

		var inventory dnfrepo.InventoryRecord
		if len(stage.Items) > 0 {
			var inventoryFound bool
			inventory, inventoryFound, err = tx.Inventory.Load(ctx, characterID)
			if err != nil {
				return err
			}
			if !inventoryFound || strings.TrimSpace(inventory.CharacterID) != characterID {
				return fmt.Errorf("%w: character=%s", ErrInventoryNotFound, characterID)
			}
			inventory = dnfrepo.CloneInventory(inventory)
			if inventory.Slots == nil {
				inventory.Slots = make(map[string]dnfrepo.ItemStack)
			}
		}
		items := make([]CommittedItem, 0, len(stage.Items))
		for _, reward := range stage.Items {
			committed, err := command.Allocate(&inventory, reward)
			if err != nil {
				return err
			}
			if err := validateCommittedItem(committed, reward); err != nil {
				return err
			}
			committed.RawEntry = append([]byte(nil), committed.RawEntry...)
			items = append(items, committed)
		}

		character.Stats["gold"] = goldAfter
		character.UpdatedAt = claimedAt.UTC()
		if err := dnfrepo.SaveCharacterFields(ctx, tx.Character, character, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		if len(stage.Items) > 0 {
			inventory.UpdatedAt = claimedAt.UTC()
			if err := dnfrepo.SaveInventoryFields(ctx, tx.Inventory, inventory, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
		}
		receipt := claimReceipt{
			StageID:     stageID,
			CharacterID: characterID,
			Items:       cloneCommittedItems(items),
			GoldBefore:  goldBefore,
			GoldAfter:   goldAfter,
			ClaimedAt:   claimedAt.UTC(),
		}
		state.Claims[stageID] = receipt
		if err := saveState(ctx, tx.Account, account, state, claimedAt); err != nil {
			return err
		}
		result = resultFromReceipt(command.Definition.ID, state, receipt, false)
		return nil
	})
	if err != nil {
		return ClaimResult{}, err
	}
	result.Items = cloneCommittedItems(result.Items)
	result.PostSnapshot = result.PostSnapshot.clone()
	return result, nil
}

func loadAccountAndCharacter(
	ctx context.Context,
	tx dnfrepo.Group,
	accountID string,
	characterID string,
) (dnfrepo.AccountRecord, dnfrepo.CharacterRecord, error) {
	if tx.Account == nil || tx.Character == nil {
		return dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, ErrOwnerUnavailable
	}
	character, found, err := tx.Character.Load(ctx, characterID)
	if err != nil {
		return dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, err
	}
	if !found || strings.TrimSpace(character.CharacterID) != characterID {
		return dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, fmt.Errorf(
			"%w: character=%s",
			ErrCharacterNotFound,
			characterID,
		)
	}
	if strings.TrimSpace(character.AccountID) != accountID {
		return dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, fmt.Errorf(
			"%w: account=%s character=%s",
			ErrAccountMismatch,
			accountID,
			characterID,
		)
	}
	account, found, err := tx.Account.Load(ctx, accountID)
	if err != nil {
		return dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, err
	}
	if !found || strings.TrimSpace(account.AccountID) != accountID {
		return dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, fmt.Errorf(
			"%w: account=%s",
			ErrAccountNotFound,
			accountID,
		)
	}
	return dnfrepo.CloneAccount(account), dnfrepo.CloneCharacter(character), nil
}

func saveState(
	ctx context.Context,
	accounts dnfrepo.AccountRepository,
	account dnfrepo.AccountRecord,
	state persistedState,
	updatedAt time.Time,
) error {
	encoded, err := encodeState(state)
	if err != nil {
		return err
	}
	if account.Metadata == nil {
		account.Metadata = make(map[string]string)
	}
	account.Metadata[metadataKey(state.EventID)] = encoded
	account.UpdatedAt = updatedAt.UTC()
	return accounts.Save(ctx, account)
}

func resultFromReceipt(eventID string, state persistedState, receipt claimReceipt, replayed bool) ClaimResult {
	return ClaimResult{
		EventID:      eventID,
		CalendarDate: state.CalendarDate,
		StageID:      receipt.StageID,
		CharacterID:  receipt.CharacterID,
		Replayed:     replayed,
		Items:        cloneCommittedItems(receipt.Items),
		GoldBefore:   receipt.GoldBefore,
		GoldAfter:    receipt.GoldAfter,
		ClaimedAt:    receipt.ClaimedAt,
		PostSnapshot: state.snapshot(),
	}
}

func activeObservationInterval(definition Definition, from time.Time, to time.Time) (time.Time, time.Time, error) {
	if !definition.ActiveFrom.IsZero() && from.Before(definition.ActiveFrom) {
		from = definition.ActiveFrom
	}
	if !definition.ActiveUntil.IsZero() && to.After(definition.ActiveUntil) {
		to = definition.ActiveUntil
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: event=%s", ErrEventInactive, definition.ID)
	}
	return from, to, nil
}

func dailyInterval(
	location *time.Location,
	boundary DailyBoundary,
	from time.Time,
	to time.Time,
) (string, time.Time, time.Time, time.Time) {
	// Observe owns a half-open [from,to) interval. When to is exactly the
	// service-day boundary, its final represented instant still belongs to the
	// preceding day. Selecting by to directly would instead reset the ledger
	// before those final seconds can be credited or claimed.
	selector := to
	if from.Before(to) {
		selector = to.Add(-time.Nanosecond)
	}
	date, dayStart := calendarDay(location, boundary, selector)
	nextDay := dayStart.AddDate(0, 0, 1)
	if from.Before(dayStart) {
		from = dayStart
	}
	if to.After(nextDay) {
		to = nextDay
	}
	return date, dayStart, from, to
}

func calendarDay(location *time.Location, boundary DailyBoundary, at time.Time) (string, time.Time) {
	local := at.In(location)
	dayStart := time.Date(
		local.Year(),
		local.Month(),
		local.Day(),
		boundary.Hour,
		boundary.Minute,
		boundary.Second,
		0,
		location,
	)
	if local.Before(dayStart) {
		dayStart = dayStart.AddDate(0, 0, -1)
	}
	return dayStart.Format(time.DateOnly), dayStart
}

func intervalSeconds(dayStart time.Time, from time.Time, to time.Time) (uint32, uint32) {
	fromFloor := from.Truncate(time.Second)
	if !fromFloor.Equal(from) {
		from = fromFloor.Add(time.Second)
	}
	to = to.Truncate(time.Second)
	if !from.Before(to) {
		return 0, 0
	}
	start := uint64(from.Sub(dayStart) / time.Second)
	end := uint64(to.Sub(dayStart) / time.Second)
	if start > secondsPerDay {
		start = secondsPerDay
	}
	if end > secondsPerDay {
		end = secondsPerDay
	}
	return uint32(start), uint32(end)
}
