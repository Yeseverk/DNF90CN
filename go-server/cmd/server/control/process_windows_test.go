//go:build windows

package main

import (
	"os"
	"testing"
)

func TestProcessExistsInSnapshot(t *testing.T) {
	exists, err := processExistsInSnapshot(os.Getpid())
	if err != nil {
		t.Fatalf("processExistsInSnapshot(current) error = %v", err)
	}
	if !exists {
		t.Fatal("current process is absent from the process snapshot")
	}

	exists, err = processExistsInSnapshot(int(^uint32(0)))
	if err != nil {
		t.Fatalf("processExistsInSnapshot(impossible) error = %v", err)
	}
	if exists {
		t.Fatal("impossible PID appears in the process snapshot")
	}
}
