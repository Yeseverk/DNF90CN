package repository

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"longheng.io/server/internal/platform/servergroup"
)

func TestDatabasePlanFromMetaGeneratesReadWriteDatabases(t *testing.T) {
	plan, err := DatabasePlanFromMeta(map[string]string{
		MetaWriteDBPrefix:  "dnf_s9999_w",
		MetaReadDBPrefix:   "dnf_s9999_r",
		MetaDatabaseCount:  "2",
		metaDatabaseDigits: "2",
	})
	if err != nil {
		t.Fatalf("DatabasePlanFromMeta() error = %v", err)
	}
	if want := []string{"dnf_s9999_w01", "dnf_s9999_w02"}; !reflect.DeepEqual(plan.WriteDatabases, want) {
		t.Fatalf("write databases = %v, want %v", plan.WriteDatabases, want)
	}
	if want := []string{"dnf_s9999_r01", "dnf_s9999_r02"}; !reflect.DeepEqual(plan.ReadDatabases, want) {
		t.Fatalf("read databases = %v, want %v", plan.ReadDatabases, want)
	}
}

func TestDatabasePlanFromMetaUsesExplicitNames(t *testing.T) {
	plan, err := DatabasePlanFromMeta(map[string]string{
		metaWriteDatabases: "dnf_write_2,dnf_write_1,dnf_write_1",
		metaReadDatabases:  "dnf_read_2;dnf_read_1",
	})
	if err != nil {
		t.Fatalf("DatabasePlanFromMeta() error = %v", err)
	}
	if want := []string{"dnf_write_1", "dnf_write_2"}; !reflect.DeepEqual(plan.WriteDatabases, want) {
		t.Fatalf("write databases = %v, want %v", plan.WriteDatabases, want)
	}
	if want := []string{"dnf_read_1", "dnf_read_2"}; !reflect.DeepEqual(plan.ReadDatabases, want) {
		t.Fatalf("read databases = %v, want %v", plan.ReadDatabases, want)
	}
}

func TestDatabasePlanFromMetaRejectsUnsafeDatabaseName(t *testing.T) {
	_, err := DatabasePlanFromMeta(map[string]string{
		metaWriteDatabases: "dnf_ok,dnf/drop",
	})
	if !errors.Is(err, ErrDatabasePlanInvalid) {
		t.Fatalf("DatabasePlanFromMeta() error = %v, want ErrDatabasePlanInvalid", err)
	}
}

func TestResolveDatabasePlanUsesServerGroupRouteMeta(t *testing.T) {
	manager, err := servergroup.New(servergroup.Plan{
		Shards: []servergroup.Shard{{ID: "1", GroupID: "logic-1", State: servergroup.StateOpen}},
		Groups: []servergroup.Group{{ID: "logic-1", State: servergroup.StateOpen}},
		Routes: []servergroup.Route{{
			Feature: FeatureRepository,
			ShardID: "1",
			GroupID: "logic-1",
			State:   servergroup.StateOpen,
			Meta: map[string]string{
				metaDatabasePrefix: "dnf_s1_",
				MetaDatabaseCount:  "2",
			},
		}},
	})
	if err != nil {
		t.Fatalf("servergroup.New() error = %v", err)
	}
	plan, err := ResolveDatabasePlan(context.Background(), manager, "1")
	if err != nil {
		t.Fatalf("ResolveDatabasePlan() error = %v", err)
	}
	if plan.Feature != FeatureRepository || plan.ShardID != "1" || plan.GroupID != "logic-1" {
		t.Fatalf("unexpected plan identity: %+v", plan)
	}
	if want := []string{"dnf_s1_1", "dnf_s1_2"}; !reflect.DeepEqual(plan.WriteDatabases, want) {
		t.Fatalf("write databases = %v, want %v", plan.WriteDatabases, want)
	}
}
