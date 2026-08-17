package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type launcherWorkflowCall struct {
	args  []string
	stdin string
}

func TestLauncherLoginWorkflow(t *testing.T) {
	var calls []launcherWorkflowCall
	var stages []launcherStage
	_, err := runLauncherWorkflow(
		context.Background(),
		launcherActionLogin,
		"developer",
		"password",
		func() error { return nil },
		func(_ context.Context, args []string, stdin string) (string, error) {
			calls = append(calls, launcherWorkflowCall{
				args:  append([]string(nil), args...),
				stdin: stdin,
			})
			return "", nil
		},
		func(stage launcherStage, _ string) {
			stages = append(stages, stage)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []launcherWorkflowCall{
		{args: []string{"start"}},
		{
			args: []string{
				"launch-client",
				"--multi-instance",
				"--username",
				"developer",
				"--password-stdin",
			},
			stdin: "password",
		},
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	wantStages := []launcherStage{
		launcherStageEnvironment,
		launcherStageServer,
		launcherStageAccount,
		launcherStageClient,
		launcherStageComplete,
	}
	if !reflect.DeepEqual(stages, wantStages) {
		t.Fatalf("stages = %v, want %v", stages, wantStages)
	}
}

func TestLauncherRegisterWorkflowRegistersAndEnters(t *testing.T) {
	var calls []launcherWorkflowCall
	_, err := runLauncherWorkflow(
		context.Background(),
		launcherActionRegister,
		"new-account",
		"password",
		func() error { return nil },
		func(_ context.Context, args []string, stdin string) (string, error) {
			calls = append(calls, launcherWorkflowCall{
				args:  append([]string(nil), args...),
				stdin: stdin,
			})
			return "", nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []launcherWorkflowCall{
		{args: []string{"start"}},
		{
			args: []string{
				"account",
				"register",
				"--username",
				"new-account",
				"--password-stdin",
				"--keep-database",
			},
			stdin: "password",
		},
		{
			args: []string{
				"launch-client",
				"--multi-instance",
				"--username",
				"new-account",
				"--password-stdin",
			},
			stdin: "password",
		},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestLauncherWorkflowStopsBeforeServiceWhenClientIsInvalid(t *testing.T) {
	wantErr := errors.New("client is not configured")
	called := false
	_, err := runLauncherWorkflow(
		context.Background(),
		launcherActionLogin,
		"developer",
		"password",
		func() error { return wantErr },
		func(context.Context, []string, string) (string, error) {
			called = true
			return "", nil
		},
		nil,
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if called {
		t.Fatal("controller was called after client validation failed")
	}
}
