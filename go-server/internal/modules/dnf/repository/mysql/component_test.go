package mysql

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"testing"
	"time"

	"longheng.io/server/internal/platform/servergroup"
)

func TestComponentStartBuildsGroup(t *testing.T) {
	sqlDB := &fakeSQLDB{}
	component := NewComponent(sqlDB, ComponentOptions{
		AutoCreateSchema: true,
		CreateDatabases:  true,
		DatabasePlan: repository.DatabasePlan{
			ShardID:        "9999",
			GroupID:        "logic-1",
			WriteDatabases: []string{"dnf_s9999_w1"},
			ReadDatabases:  []string{"dnf_s9999_r1"},
		},
		Now: func() time.Time {
			return time.Date(2026, 6, 29, 4, 0, 0, 0, time.UTC)
		},
	})
	if component.Name() != "dnf-repository" {
		t.Fatalf("unexpected component name: %s", component.Name())
	}
	if err := component.Preflight(context.Background()); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(sqlDB.execs) < 54 {
		t.Fatalf("schema/migration call count = %d, want at least 54", len(sqlDB.execs))
	}
	if sqlDB.pings != 1 {
		t.Fatalf("pings = %d, want 1", sqlDB.pings)
	}
	group, ok := component.Group()
	if !ok {
		t.Fatal("repository group should be available after start")
	}
	if err := group.Character.Save(context.Background(), repository.CharacterRecord{CharacterID: "char-1", AccountID: "acc-1"}); err != nil {
		t.Fatalf("character save: %v", err)
	}
	if got := strings.Join(execQueries(sqlDB), "\n"); !strings.Contains(got, "`dnf_s9999_w1`.`dnf_characters`") {
		t.Fatalf("character save table mismatch:\n%s", got)
	}
	snapshot := component.Snapshot()
	if !snapshot.Started || snapshot.ShardID != "9999" || !snapshot.AutoCreateSchema {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestComponentFromServerGroup(t *testing.T) {
	manager, err := servergroup.New(servergroup.Plan{
		Shards: []servergroup.Shard{{ID: "1", GroupID: "logic-1", State: servergroup.StateOpen}},
		Groups: []servergroup.Group{{ID: "logic-1", State: servergroup.StateOpen}},
		Routes: []servergroup.Route{{
			Feature: repository.FeatureRepository,
			ShardID: "1",
			GroupID: "logic-1",
			State:   servergroup.StateOpen,
			Meta: map[string]string{
				repository.MetaWriteDBPrefix: "dnf_s1_w",
				repository.MetaReadDBPrefix:  "dnf_s1_r",
				repository.MetaDatabaseCount: "1",
			},
		}},
	})
	if err != nil {
		t.Fatalf("servergroup.New() error = %v", err)
	}
	sqlDB := &fakeSQLDB{}
	component := NewComponentFromServerGroup(sqlDB, manager, ComponentOptions{ShardID: "1"})
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	group, ok := component.Group()
	if !ok {
		t.Fatal("repository group should be available")
	}
	if err := group.Account.Save(context.Background(), repository.AccountRecord{AccountID: "acc-1", State: "open"}); err != nil {
		t.Fatalf("account save: %v", err)
	}
	queries := strings.Join(execQueries(sqlDB), "\n")
	if !strings.Contains(queries, "`dnf_s1_w1`.`dnf_accounts`") {
		t.Fatalf("account save table mismatch:\n%s", queries)
	}
	if snapshot := component.Snapshot(); snapshot.ShardID != "1" || snapshot.GroupID != "logic-1" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestComponentRejectsMissingPlan(t *testing.T) {
	component := NewComponent(&fakeSQLDB{}, ComponentOptions{ShardID: "1"})
	if err := component.Preflight(context.Background()); !errors.Is(err, repository.ErrDatabasePlanInvalid) {
		t.Fatalf("Preflight() error = %v, want ErrDatabasePlanInvalid", err)
	}
	if err := component.Start(context.Background()); !errors.Is(err, repository.ErrDatabasePlanInvalid) {
		t.Fatalf("Start() error = %v, want ErrDatabasePlanInvalid", err)
	}
	if snapshot := component.Snapshot(); snapshot.Started || snapshot.LastError == "" {
		t.Fatalf("expected failed snapshot, got %+v", snapshot)
	}
}

func TestComponentRecordsSchemaFailure(t *testing.T) {
	boom := errors.New("schema boom")
	component := NewComponent(&fakeSQLDB{execErr: boom}, ComponentOptions{
		AutoCreateSchema: true,
		DatabasePlan:     repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err := component.Start(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("Start() error = %v, want schema boom", err)
	}
	if snapshot := component.Snapshot(); snapshot.Started || snapshot.LastError == "" {
		t.Fatalf("expected schema failure snapshot, got %+v", snapshot)
	}
}

func TestComponentStopClearsGroup(t *testing.T) {
	component := NewComponent(&fakeSQLDB{}, ComponentOptions{
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, ok := component.Group(); !ok {
		t.Fatal("repository group should be available")
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, ok := component.Group(); ok {
		t.Fatal("repository group should be cleared after stop")
	}
	if snapshot := component.Snapshot(); snapshot.Started || len(snapshot.WriteDatabases) != 0 {
		t.Fatalf("unexpected stopped snapshot: %+v", snapshot)
	}
}
