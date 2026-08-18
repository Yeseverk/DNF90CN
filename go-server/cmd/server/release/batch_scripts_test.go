package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// LOGIN.bat must stop the running service with the controller that is already
// installed, and only then replace the controller and the rest of the runtime.
// Rebuilding the controller first is what turned "cannot update" into "cannot
// start" on a tester machine that had no Go but did have a usable controller.
func TestLoginStopsWithInstalledControllerBeforeRebuilding(t *testing.T) {
	login := readProjectFile(t, "LOGIN.bat")
	stop := strings.Index(login, `control.bat" stop`)
	build := strings.Index(login, `control.bat" build --force=true`)
	force := strings.Index(login, "DNF90_FORCE_CONTROL_BUILD=1")
	if stop < 0 || build < 0 {
		t.Fatalf("LOGIN.bat must contain both the stop and the rebuild call")
	}
	if stop > build {
		t.Fatalf("LOGIN.bat rebuilds the runtime before stopping it")
	}
	if force < 0 {
		t.Fatalf("LOGIN.bat must force a controller rebuild; a running controller " +
			"cannot overwrite its own image, so DNF90Control.exe would stay stale")
	}
	if force < stop {
		t.Fatalf("LOGIN.bat forces the controller rebuild before the stop")
	}
}

// DNF90Build.version is written only by a controller build of the current
// source. LOGIN.bat must never write it itself: stamping the marker onto
// executables it did not build reports a stale runtime as current, and the
// install then never updates again.
func TestLoginNeverStampsRuntimeBuildVersion(t *testing.T) {
	login := readProjectFile(t, "LOGIN.bat")
	for _, line := range strings.Split(login, "\n") {
		lowered := strings.ToLower(strings.TrimSpace(line))
		if !strings.HasPrefix(lowered, "copy ") && !strings.HasPrefix(lowered, "move ") {
			continue
		}
		if strings.Contains(lowered, "dnf90build.version") ||
			strings.Contains(lowered, "dnf90_version_installed") {
			t.Fatalf("LOGIN.bat writes the build marker itself: %q", strings.TrimSpace(line))
		}
	}
	if strings.Contains(login, "continuing with the existing DNF90 runtime") {
		t.Fatalf("LOGIN.bat still carries the legacy-runtime migration path")
	}
}

// Without Go, an unmatched runtime cannot be rebuilt. LOGIN.bat has to refuse
// with a recovery path instead of starting executables that predate the
// current server fixes. The message is written for the tester who sees it, not
// for a developer: a tester cannot compile anything, so it must name the
// release package and the GitHub-source-download trap rather than build tools.
func TestLoginRefusesUnbuildableRuntimeWithRecoveryPath(t *testing.T) {
	login := readProjectFile(t, "LOGIN.bat")
	for _, required := range []string{
		":go_required",
		":runtime_incomplete",
		"无法启动",
		"完整发布包",
		"-main",
	} {
		if !strings.Contains(login, required) {
			t.Fatalf("LOGIN.bat is missing the recovery path %q", required)
		}
	}
}

// The Chinese messages above are mojibake on a GBK console unless the script
// switches the code page first.
func TestLoginSwitchesConsoleToUTF8BeforePrintingChinese(t *testing.T) {
	login := readProjectFile(t, "LOGIN.bat")
	chcp := strings.Index(login, "chcp 65001")
	if chcp < 0 {
		t.Fatal("LOGIN.bat prints Chinese without switching the console to UTF-8")
	}
	if first := strings.Index(login, "无法启动"); first >= 0 && first < chcp {
		t.Fatal("LOGIN.bat prints Chinese before the chcp 65001 switch")
	}
}

func TestControlBatchUsesResolvedGoExecutable(t *testing.T) {
	control := readProjectFile(t, filepath.Join("deploy", "windows", "control.bat"))
	for _, required := range []string{
		":resolve_go",
		"DNF90_GO_EXECUTABLE",
		`"%DNF90_GO_EXE%" build`,
	} {
		if !strings.Contains(control, required) {
			t.Fatalf("control.bat is missing Go discovery/use path %q", required)
		}
	}
}

func TestPackageReleaseUsesResolvedGoExecutable(t *testing.T) {
	packager := readProjectFile(t, "PACKAGE_RELEASE.bat")
	for _, required := range []string{
		":resolve_go",
		"DNF90_GO_EXECUTABLE",
		`"%DNF90_GO_EXE%" run`,
	} {
		if !strings.Contains(packager, required) {
			t.Fatalf("PACKAGE_RELEASE.bat is missing Go discovery/use path %q", required)
		}
	}
}

// cmd.exe expands %ProgramFiles(x86)% before it matches the parentheses of a
// block, so probing that location inside "if exist ... (" can terminate the
// block early. Every script must probe it through a single-line call instead.
func TestGoDiscoveryNeverOpensBlockOnParenthesisedVariable(t *testing.T) {
	for _, rel := range []string{
		"LOGIN.bat",
		filepath.Join("deploy", "windows", "control.bat"),
		"PACKAGE_RELEASE.bat",
	} {
		script := readProjectFile(t, rel)
		found := false
		for _, line := range strings.Split(script, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.Contains(trimmed, "ProgramFiles(x86)") {
				continue
			}
			if strings.HasPrefix(trimmed, "rem ") {
				continue
			}
			found = true
			if !strings.HasPrefix(trimmed, "call :probe_go ") {
				t.Fatalf("%s probes ProgramFiles(x86) outside a single-line call: %q", rel, trimmed)
			}
		}
		if !found {
			t.Fatalf("%s no longer probes the 32-bit Go installation directory", rel)
		}
	}
}

// A release package always ships Windows executables, whatever host produced
// it. Without an explicit target a macOS or Linux packaging run emits host
// binaries under .exe names that still pass every payload rule.
func TestRuntimeBuildEnvPinsWindowsAmd64(t *testing.T) {
	env := windowsRuntimeBuildEnv()
	for _, required := range []string{"GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0"} {
		found := false
		for _, entry := range env {
			if entry == required {
				found = true
			}
		}
		if !found {
			t.Fatalf("release build environment is missing %q", required)
		}
	}
	// exec keeps the last duplicate, so the pins must come after os.Environ().
	for index, entry := range env {
		if entry == "GOOS=windows" {
			for _, later := range env[index+1:] {
				if strings.HasPrefix(later, "GOOS=") {
					t.Fatalf("a later %q overrides the windows pin", later)
				}
			}
		}
	}
}

func readProjectFile(t *testing.T, rel string) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}
