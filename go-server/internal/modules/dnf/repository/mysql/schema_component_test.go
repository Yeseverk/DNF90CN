package mysql

import (
	"context"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"testing"
)

func TestSchemaComponentStartCreatesSchemaWhenEnabled(t *testing.T) {
	exec := &fakeSchemaExec{}
	component := NewSchemaComponent(exec, SchemaOptions{
		AutoCreate:      true,
		CreateDatabases: true,
		DatabasePlan: repository.DatabasePlan{
			ShardID:        "9999",
			WriteDatabases: []string{"dnf_s9999_w1"},
			ReadDatabases:  []string{"dnf_s9999_r1"},
		},
	})
	if component.Name() != "dnf-repository-schema" {
		t.Fatalf("unexpected component name: %s", component.Name())
	}
	if err := component.Preflight(context.Background()); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(exec.statements) != 55 {
		t.Fatalf("executed statements = %d, want 55", len(exec.statements))
	}
	snapshot := component.Snapshot()
	if !snapshot.Started || !snapshot.AutoCreate || snapshot.ShardID != "9999" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestSchemaComponentNoopsWhenDisabled(t *testing.T) {
	exec := &fakeSchemaExec{}
	component := NewSchemaComponent(exec, SchemaOptions{
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s9999_w1"}},
	})
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(exec.statements) != 0 {
		t.Fatalf("disabled component should not execute schema: %v", exec.statements)
	}
}

func TestSchemaComponentRejectsMissingExecutorWhenEnabled(t *testing.T) {
	component := NewSchemaComponent(nil, SchemaOptions{
		AutoCreate:   true,
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s9999_w1"}},
	})
	if err := component.Preflight(context.Background()); !errors.Is(err, ErrSchemaExecutorRequired) {
		t.Fatalf("Preflight() error = %v, want ErrSchemaExecutorRequired", err)
	}
	if err := component.Start(context.Background()); !errors.Is(err, ErrSchemaExecutorRequired) {
		t.Fatalf("Start() error = %v, want ErrSchemaExecutorRequired", err)
	}
	if snapshot := component.Snapshot(); snapshot.Started || snapshot.LastError == "" {
		t.Fatalf("expected failed snapshot, got %+v", snapshot)
	}
}
