//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"
)

func TestConfiguredClientExecutables(t *testing.T) {
	root := t.TempDir()
	clientRoot := filepath.Join(root, "client")
	if err := os.MkdirAll(clientRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	dnfPath := filepath.Join(clientRoot, "DNF.exe")
	if err := os.WriteFile(dnfPath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	configRoot := filepath.Join(root, "runtime", "config")
	if err := os.MkdirAll(configRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	config := []byte(
		"{\"client\":{\"directory\":" +
			quoteJSON(clientRoot) +
			"}}",
	)
	if err := os.WriteFile(
		filepath.Join(configRoot, "instance.json"),
		config,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	executables, err := configuredClientExecutables(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(executables) != 1 ||
		!sameExecutablePath(executables[0], dnfPath) {
		t.Fatalf("configured executables = %q", executables)
	}
}

func TestHideAndRestoreOwnedTopLevelWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	window, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(utf16Ptr("DNF90 launcher window test"))),
		wsOverlapped|wsCaption,
		0,
		0,
		200,
		100,
		0,
		0,
		0,
		0,
	)
	if window == 0 {
		t.Fatalf("create test window: %v", callErr)
	}
	defer procDestroyWindow.Call(window)
	procShowWindow.Call(window, swShow)
	procUpdateWindow.Call(window)

	hidden, err := hideVisibleWindowsForExecutables([]string{executable})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range hidden {
		if entry.handle == window {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("test window was not hidden")
	}
	if !waitForWindowVisibility(window, false) {
		t.Fatal("test window is still visible")
	}
	restored, remaining, err := restoreHiddenWindows(hidden)
	if err != nil {
		t.Fatal(err)
	}
	if restored < 1 || len(remaining) != 0 {
		t.Fatalf(
			"restore result = %d restored, %d remaining",
			restored,
			len(remaining),
		)
	}
	if !waitForWindowVisibility(window, true) {
		t.Fatal("test window was not restored")
	}
}

func waitForWindowVisibility(window uintptr, wantVisible bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		visible, _, _ := procIsWindowVisible.Call(window)
		if (visible != 0) == wantVisible {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func quoteJSON(value string) string {
	quoted := "\""
	for _, character := range value {
		switch character {
		case '\\', '"':
			quoted += "\\" + string(character)
		default:
			quoted += string(character)
		}
	}
	return quoted + "\""
}
