package main

import (
	"os"
	"testing"
)

func TestBuildUsesWrapperDiscoveredGoExecutable(t *testing.T) {
	t.Setenv("DNF90_GO_EXE", `C:\Program Files\Go\bin\go.exe`)
	cfg := instanceConfig{Build: buildConfig{GoExecutable: "go"}}
	if got := goExecutableForBuild(cfg); got != `C:\Program Files\Go\bin\go.exe` {
		t.Fatalf("go executable = %q", got)
	}
}

func TestBuildUsesConfiguredGoExecutableWhenWrapperDidNotDiscoverOne(t *testing.T) {
	t.Setenv("DNF90_GO_EXE", "")
	cfg := instanceConfig{Build: buildConfig{GoExecutable: `D:\Go\bin\go.exe`}}
	if got := goExecutableForBuild(cfg); got != `D:\Go\bin\go.exe` {
		t.Fatalf("go executable = %q", got)
	}
}

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
