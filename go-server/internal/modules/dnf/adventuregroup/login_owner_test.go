package adventuregroup

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerObserveDailyLoginStartsAdvancesAndResetsStreak(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{"account_cera": "123"},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	location := time.FixedZone("test", 8*60*60)
	observe := func(at time.Time) ObserveDailyLoginResult {
		t.Helper()
		result, observeErr := owner.ObserveDailyLogin(ctx, ObserveDailyLoginCommand{
			AccountID:  "account-1",
			ObservedAt: at,
		})
		if observeErr != nil {
			t.Fatal(observeErr)
		}
		return result
	}

	first := observe(time.Date(2026, 7, 28, 8, 0, 0, 0, location))
	if !first.Changed || first.ConsecutiveDays != 1 || first.CalendarDate != "2026-07-28" {
		t.Fatalf("first=%+v", first)
	}
	same := observe(time.Date(2026, 7, 28, 23, 59, 0, 0, location))
	if same.Changed || same.ConsecutiveDays != 1 {
		t.Fatalf("same=%+v", same)
	}
	next := observe(time.Date(2026, 7, 29, 0, 1, 0, 0, location))
	if !next.Changed || next.ConsecutiveDays != 2 {
		t.Fatalf("next=%+v", next)
	}
	gap := observe(time.Date(2026, 7, 31, 0, 1, 0, 0, location))
	if !gap.Changed || gap.ConsecutiveDays != 1 {
		t.Fatalf("gap=%+v", gap)
	}
	account, found, err := repositories.Account.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("load found=%t err=%v", found, err)
	}
	if account.Metadata[DailyLoginStateMetadataKey] != "2026-07-31|1" ||
		account.Metadata["account_cera"] != "123" {
		t.Fatalf("account metadata=%v", account.Metadata)
	}
}

func TestOwnerObserveDailyLoginPreservesFutureAndSaturates(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata: map[string]string{
			DailyLoginStateMetadataKey: "2026-07-29|4294967295",
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repositories)
	result, err := owner.ObserveDailyLogin(ctx, ObserveDailyLoginCommand{
		AccountID:  "account-1",
		ObservedAt: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	})
	if err != nil || result.Changed || result.ConsecutiveDays != math.MaxUint32 {
		t.Fatalf("future result=%+v err=%v", result, err)
	}
	result, err = owner.ObserveDailyLogin(ctx, ObserveDailyLoginCommand{
		AccountID:  "account-1",
		ObservedAt: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil || !result.Changed || result.ConsecutiveDays != math.MaxUint32 {
		t.Fatalf("saturated result=%+v err=%v", result, err)
	}
}

func TestOwnerObserveDailyLoginRejectsCorruptState(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{DailyLoginStateMetadataKey: "bad"},
	}); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repositories)
	if _, err := owner.ObserveDailyLogin(ctx, ObserveDailyLoginCommand{
		AccountID:  "account-1",
		ObservedAt: time.Now(),
	}); !errors.Is(err, ErrDailyLoginStateInvalid) {
		t.Fatalf("error=%v", err)
	}
}
