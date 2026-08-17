package main

import (
	"os"
	"testing"
)

func TestInstallRuntimeBuildVersion(t *testing.T) {
	paths := newProjectPaths(t.TempDir())
	want := []byte("test-version\n")
	if err := writeFile(paths.runtimeVersionSource, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installRuntimeBuildVersion(paths); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(paths.runtimeVersion)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("installed version = %q, want %q", got, want)
	}
	current, err := runtimeBuildVersionCurrent(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !current {
		t.Fatal("newly installed runtime version is not current")
	}
	if err := writeFile(paths.runtimeVersionSource, []byte("next-version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err = runtimeBuildVersionCurrent(paths)
	if err != nil {
		t.Fatal(err)
	}
	if current {
		t.Fatal("stale installed runtime version was accepted")
	}
}

func TestInstallRuntimeBuildVersionRejectsEmptySource(t *testing.T) {
	paths := newProjectPaths(t.TempDir())
	if err := writeFile(paths.runtimeVersionSource, []byte(" \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installRuntimeBuildVersion(paths); err == nil {
		t.Fatal("empty runtime version was accepted")
	}
}
