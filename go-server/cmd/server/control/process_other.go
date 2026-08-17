//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"time"
)

type liveProcess struct {
	running    bool
	executable string
	createdAt  time.Time
}

func inspectProcess(pid int) (liveProcess, error) {
	return liveProcess{}, fmt.Errorf("PID executable verification is supported only on Windows (PID %d)", pid)
}

func forceTerminateProcess(pid int, expectedExecutable string, expectedCreatedAt time.Time) error {
	return fmt.Errorf(
		"verified forced termination is supported only on Windows (PID %d, expected %s, created %s)",
		pid,
		expectedExecutable,
		expectedCreatedAt.Format(time.RFC3339Nano),
	)
}

func processParentPID(pid int) (int, error) {
	return 0, fmt.Errorf("parent PID verification is supported only on Windows (PID %d)", pid)
}

func configureServerProcess(cmd *exec.Cmd) {}

func configureBackgroundProcess(cmd *exec.Cmd) {}

func configureClientProcess(cmd *exec.Cmd) {}

func sameExecutable(actual, expected string) bool {
	return actual == expected
}
