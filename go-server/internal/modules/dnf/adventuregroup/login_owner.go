package adventuregroup

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var ErrDailyLoginStateInvalid = errors.New("adventure-group daily-login state is invalid")

var adventureGroupCalendarLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type ObserveDailyLoginCommand struct {
	AccountID  string
	ObservedAt time.Time
}

type ObserveDailyLoginResult struct {
	AccountID       string
	CalendarDate    string
	ConsecutiveDays uint32
	Changed         bool
}

// ObserveDailyLogin advances one account's UTC+8 calendar-day streak. The date
// and count share one metadata value so the key-level write cannot persist a
// partially updated streak.
func (o *Owner) ObserveDailyLogin(
	ctx context.Context,
	command ObserveDailyLoginCommand,
) (ObserveDailyLoginResult, error) {
	if o == nil || o.accounts == nil {
		return ObserveDailyLoginResult{}, ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(command.AccountID)
	if accountID == "" {
		return ObserveDailyLoginResult{}, ErrAccountRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ObserveDailyLoginResult{}, err
	}
	observedAt := command.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	today := calendarDate(observedAt)
	account, found, err := o.accounts.Load(ctx, accountID)
	if err != nil {
		return ObserveDailyLoginResult{}, fmt.Errorf("load adventure-group account: %w", err)
	}
	if !found || strings.TrimSpace(account.AccountID) != accountID {
		return ObserveDailyLoginResult{}, fmt.Errorf("%w: account=%s", ErrAccountNotFound, accountID)
	}

	previousDate, previousCount, hasState, err := parseDailyLoginState(account.Metadata[DailyLoginStateMetadataKey])
	if err != nil {
		return ObserveDailyLoginResult{}, err
	}
	result := ObserveDailyLoginResult{
		AccountID:       accountID,
		CalendarDate:    today.Format(time.DateOnly),
		ConsecutiveDays: previousCount,
	}
	if hasState && !today.After(previousDate) {
		return result, nil
	}
	switch {
	case !hasState:
		result.ConsecutiveDays = 1
	case today.Equal(previousDate.AddDate(0, 0, 1)):
		if previousCount < math.MaxUint32 {
			result.ConsecutiveDays = previousCount + 1
		}
	default:
		result.ConsecutiveDays = 1
	}
	value := result.CalendarDate + "|" + strconv.FormatUint(uint64(result.ConsecutiveDays), 10)
	if err := dnfrepo.SaveAccountMetadataEntry(
		ctx,
		o.accounts,
		account,
		DailyLoginStateMetadataKey,
		value,
		observedAt,
	); err != nil {
		return ObserveDailyLoginResult{}, fmt.Errorf("save adventure-group daily login: %w", err)
	}
	result.Changed = true
	return result, nil
}

func calendarDate(value time.Time) time.Time {
	local := value.In(adventureGroupCalendarLocation)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, adventureGroupCalendarLocation)
}

func parseDailyLoginState(value string) (time.Time, uint32, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, 0, false, nil
	}
	dateText, countText, ok := strings.Cut(value, "|")
	if !ok {
		return time.Time{}, 0, false, fmt.Errorf("%w: %q", ErrDailyLoginStateInvalid, value)
	}
	date, err := time.ParseInLocation(time.DateOnly, dateText, adventureGroupCalendarLocation)
	if err != nil {
		return time.Time{}, 0, false, fmt.Errorf("%w: date=%q", ErrDailyLoginStateInvalid, dateText)
	}
	count, err := strconv.ParseUint(countText, 10, 32)
	if err != nil || count == 0 {
		return time.Time{}, 0, false, fmt.Errorf("%w: count=%q", ErrDailyLoginStateInvalid, countText)
	}
	return date, uint32(count), true, nil
}
