//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

func acquireControllerLock(ctx context.Context, projectRoot string) (func(), error) {
	lockDirectory := filepath.Join(projectRoot, "runtime", "state")
	if err := os.MkdirAll(lockDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create controller lock directory: %w", err)
	}
	lockPath := filepath.Join(lockDirectory, "control.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open controller lock file: %w", err)
	}
	handle := windows.Handle(file.Fd())
	overlapped := &windows.Overlapped{}
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	for {
		err = windows.LockFileEx(handle, flags, 0, 1, 0, overlapped)
		if err == nil {
			var once sync.Once
			return func() {
				once.Do(func() {
					_ = windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
					_ = file.Close()
				})
			}, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			_ = file.Close()
			return nil, fmt.Errorf("lock controller state: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, fmt.Errorf("wait for another DNF90 command: %w", ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}
