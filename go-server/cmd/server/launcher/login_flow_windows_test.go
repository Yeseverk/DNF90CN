package main

import (
	"context"
	"reflect"
	"testing"
)

func TestAuthenticatedClientLaunchDoesNotStopOrSwitchServerAccount(t *testing.T) {
	original := launcherRunControl
	t.Cleanup(func() {
		launcherRunControl = original
	})

	type call struct {
		args  []string
		stdin string
	}
	var calls []call
	launcherRunControl = func(
		_ context.Context,
		args []string,
		stdin string,
	) (string, error) {
		calls = append(calls, call{
			args:  append([]string(nil), args...),
			stdin: stdin,
		})
		return "", nil
	}

	if _, err := runAuthenticatedClientLaunch(
		context.Background(),
		"second-account",
		"second-password",
	); err != nil {
		t.Fatalf("runAuthenticatedClientLaunch: %v", err)
	}

	want := []call{
		{args: []string{"start"}},
		{
			args: []string{
				"launch-client",
				"--multi-instance",
				"--username",
				"second-account",
				"--password-stdin",
			},
			stdin: "second-password",
		},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("control calls = %#v, want %#v", calls, want)
	}
}
