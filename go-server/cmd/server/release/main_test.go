package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathWithin(t *testing.T) {
	root := filepath.Join(t.TempDir(), "releases")
	if !pathWithin(root, filepath.Join(root, "DNF90.zip")) {
		t.Fatal("expected ZIP under releases to be accepted")
	}
	if pathWithin(root, filepath.Join(root, "..", "DNF90.zip")) {
		t.Fatal("expected path outside releases to be rejected")
	}
}

func TestValidatePayloadRejectsGeneratedRuntimeState(t *testing.T) {
	payload := t.TempDir()
	required := []string{
		"LOGIN.bat",
		"START.bat",
		"runtime/bin/DNF90Control.exe",
		"runtime/bin/DNF90Doctor.exe",
		"runtime/bin/DNF90Launcher.exe",
		"runtime/bin/DNF90Server.exe",
		"runtime/bin/DNF90Build.version",
		"runtime/data/dnf/Script.pvf",
		"deploy/vendor/mysql/mysql-8.4.10-winx64.zip",
		"client-patch/90CN.cpp",
		"client-patch/bin/90CN.dll",
		"go-server/go.mod",
		"deploy/windows/runtime.version",
	}
	for _, rel := range required {
		writeTestFile(t, filepath.Join(payload, filepath.FromSlash(rel)))
	}
	if err := validatePayload(payload); err != nil {
		t.Fatalf("clean payload rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(payload, "runtime", "bin", "DNF90Build.version"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validatePayload(payload); err == nil || !strings.Contains(err.Error(), "runtime.version does not match") {
		t.Fatalf("runtime version mismatch error=%v", err)
	}
	writeTestFile(t, filepath.Join(payload, "runtime", "bin", "DNF90Build.version"))

	writeTestFile(t, filepath.Join(payload, "runtime", "config", "instance.json"))
	err := validatePayload(payload)
	if err == nil || !strings.Contains(err.Error(), "forbidden runtime state") {
		t.Fatalf("generated runtime state error=%v", err)
	}
}

func TestReleaseSourceFilterKeepsPackagerSourceAndPatchDLL(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "bin", "90CN.dll")
	releaseSource := filepath.Join(root, "release", "main.go")
	drop := filepath.Join(root, "backups", "90CN.dll")
	writeTestFile(t, keep)
	writeTestFile(t, releaseSource)
	writeTestFile(t, drop)

	keepInfo, err := os.ReadDir(filepath.Dir(keep))
	if err != nil {
		t.Fatal(err)
	}
	if len(keepInfo) != 1 || !releaseSourceFileAllowed(filepath.ToSlash("bin/90CN.dll"), keepInfo[0]) {
		t.Fatal("packaged patch DLL should be retained")
	}
	if !releaseSourceFileAllowed("release", directoryEntry(t, filepath.Dir(releaseSource))) {
		t.Fatal("Go release packager source directory should be retained")
	}
	if releaseSourceFileAllowed("backups", directoryEntry(t, filepath.Dir(drop))) {
		t.Fatal("backup directory should be excluded")
	}
}

func TestClientPatchFilterExcludesVisualStudioReleaseDirectory(t *testing.T) {
	root := t.TempDir()
	releaseOutput := filepath.Join(root, "Release", "90CN.dll")
	writeTestFile(t, releaseOutput)
	if clientPatchSourceFileAllowed("Release", directoryEntry(t, filepath.Dir(releaseOutput))) {
		t.Fatal("Visual Studio Release output directory should be excluded")
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func directoryEntry(t *testing.T, path string) os.DirEntry {
	t.Helper()
	parent := filepath.Dir(path)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	for _, entry := range entries {
		if entry.Name() == name {
			return entry
		}
	}
	t.Fatalf("directory entry not found: %s", path)
	return nil
}
