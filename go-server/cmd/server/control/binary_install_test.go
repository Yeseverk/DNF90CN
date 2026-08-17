package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverBuiltBinarySetRollsBackInterruptedSet(t *testing.T) {
	paths := newProjectPaths(t.TempDir())
	if err := os.MkdirAll(paths.runtimeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	server := filepath.Join(paths.runtimeBin, "DNF90Server.exe")
	doctor := filepath.Join(paths.runtimeBin, "DNF90Doctor.exe")
	serverTemp := server + ".build-test"
	doctorTemp := doctor + ".build-test"
	writeTestBinary(t, server, "old-server")
	writeTestBinary(t, doctor, "old-doctor")
	writeTestBinary(t, serverTemp, "new-server")
	writeTestBinary(t, doctorTemp, "new-doctor")

	state := testBinaryInstallState(t, paths, []builtBinary{
		{destination: server, temp: serverTemp, label: "server"},
		{destination: doctor, temp: doctorTemp, label: "doctor"},
	})
	serverBackup := filepath.Join(paths.runtimeBin, state.Entries[0].Backup)
	if err := os.Rename(server, serverBackup); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(serverTemp, server); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(paths.binaryInstallState, state, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := recoverBuiltBinarySet(paths); err != nil {
		t.Fatalf("recoverBuiltBinarySet() error = %v", err)
	}
	assertTestBinary(t, server, "old-server")
	assertTestBinary(t, doctor, "old-doctor")
	for _, path := range []string{
		serverTemp,
		doctorTemp,
		serverBackup,
		paths.binaryInstallState,
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("transaction artifact remains: %s", path)
		}
	}
}

func TestRecoverBuiltBinarySetCommitsCompleteSet(t *testing.T) {
	paths := newProjectPaths(t.TempDir())
	if err := os.MkdirAll(paths.runtimeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	server := filepath.Join(paths.runtimeBin, "DNF90Server.exe")
	doctor := filepath.Join(paths.runtimeBin, "DNF90Doctor.exe")
	serverTemp := server + ".build-test"
	doctorTemp := doctor + ".build-test"
	writeTestBinary(t, server, "old-server")
	writeTestBinary(t, doctor, "old-doctor")
	writeTestBinary(t, serverTemp, "new-server")
	writeTestBinary(t, doctorTemp, "new-doctor")
	state := testBinaryInstallState(t, paths, []builtBinary{
		{destination: server, temp: serverTemp, label: "server"},
		{destination: doctor, temp: doctorTemp, label: "doctor"},
	})
	for index, path := range []string{server, doctor} {
		backup := filepath.Join(paths.runtimeBin, state.Entries[index].Backup)
		if err := os.Rename(path, backup); err != nil {
			t.Fatal(err)
		}
		temp := []string{serverTemp, doctorTemp}[index]
		if err := os.Rename(temp, path); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeJSON(paths.binaryInstallState, state, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := recoverBuiltBinarySet(paths); err != nil {
		t.Fatalf("recoverBuiltBinarySet() error = %v", err)
	}
	assertTestBinary(t, server, "new-server")
	assertTestBinary(t, doctor, "new-doctor")
	for _, entry := range state.Entries {
		if _, err := os.Lstat(filepath.Join(paths.runtimeBin, entry.Backup)); !os.IsNotExist(err) {
			t.Fatalf("binary backup remains: %s", entry.Backup)
		}
	}
	if _, err := os.Lstat(paths.binaryInstallState); !os.IsNotExist(err) {
		t.Fatal("binary installation state remains")
	}
}

func TestRecoverBuiltBinarySetRefusesUnknownDestination(t *testing.T) {
	paths := newProjectPaths(t.TempDir())
	if err := os.MkdirAll(paths.runtimeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	server := filepath.Join(paths.runtimeBin, "DNF90Server.exe")
	serverTemp := server + ".build-test"
	writeTestBinary(t, server, "old-server")
	writeTestBinary(t, serverTemp, "new-server")
	state := testBinaryInstallState(t, paths, []builtBinary{
		{destination: server, temp: serverTemp, label: "server"},
	})
	backup := filepath.Join(paths.runtimeBin, state.Entries[0].Backup)
	if err := os.Rename(server, backup); err != nil {
		t.Fatal(err)
	}
	writeTestBinary(t, server, "unknown-user-file")
	if err := writeJSON(paths.binaryInstallState, state, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := recoverBuiltBinarySet(paths); err == nil {
		t.Fatal("recoverBuiltBinarySet() succeeded with an unknown destination")
	}
	assertTestBinary(t, server, "unknown-user-file")
	assertTestBinary(t, backup, "old-server")
}

func TestResolveBinaryInstallStateRejectsEscapingPath(t *testing.T) {
	paths := newProjectPaths(t.TempDir())
	_, err := resolveBinaryInstallState(paths, binaryInstallState{
		SchemaVersion: binaryInstallSchemaVersion,
		Entries: []binaryInstallStateEntry{{
			Destination:     `..\outside.exe`,
			Temporary:       "candidate.exe",
			Backup:          "backup.exe",
			CandidateSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		}},
	})
	if err == nil {
		t.Fatal("resolveBinaryInstallState() accepted a path escape")
	}
}

func testBinaryInstallState(
	t *testing.T,
	paths projectPaths,
	targets []builtBinary,
) binaryInstallState {
	t.Helper()
	state := binaryInstallState{
		SchemaVersion: binaryInstallSchemaVersion,
		Entries:       make([]binaryInstallStateEntry, 0, len(targets)),
	}
	for index, target := range targets {
		originalSHA256, err := fileSHA256(target.destination)
		if err != nil {
			t.Fatal(err)
		}
		candidateSHA256, err := fileSHA256(target.temp)
		if err != nil {
			t.Fatal(err)
		}
		state.Entries = append(state.Entries, binaryInstallStateEntry{
			Destination:     filepath.Base(target.destination),
			Temporary:       filepath.Base(target.temp),
			Backup:          filepath.Base(target.destination) + ".previous-test-" + string(rune('0'+index)),
			HadOriginal:     true,
			OriginalSHA256:  originalSHA256,
			CandidateSHA256: candidateSHA256,
		})
	}
	return state
}

func writeTestBinary(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertTestBinary(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}
