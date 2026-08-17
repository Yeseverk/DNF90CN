package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractPortableMySQLArchiveRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "mysql.zip")
	writePortableMySQLTestZIP(t, archivePath, []portableMySQLTestZIPEntry{
		{name: "mysql-test/bin/mysqld.exe", data: []byte("fixture")},
		{name: "mysql-test/../../escaped.txt", data: []byte("escape")},
	})

	destination := filepath.Join(root, "destination")
	err := extractPortableMySQLArchive(archivePath, "mysql-test", destination)
	if err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("extractPortableMySQLArchive() error = %v, want unsafe path rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "escaped.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("path traversal created an outside file; stat error = %v", statErr)
	}
}

func TestExtractAndValidatePortableMySQLArchive(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "mysql.zip")
	files := []portableMySQLTestZIPEntry{
		{name: "mysql-test/bin/mysqld.exe", data: []byte("mysqld-fixture")},
		{name: "mysql-test/LICENSE", data: []byte("license-fixture")},
	}
	writePortableMySQLTestZIP(t, archivePath, files)

	destination := filepath.Join(root, "destination")
	if err := extractPortableMySQLArchive(
		archivePath,
		"mysql-test",
		destination,
	); err != nil {
		t.Fatalf("extractPortableMySQLArchive() error = %v", err)
	}

	manifest := make([]portableMySQLManifestFile, 0, len(files))
	for _, file := range files {
		relative := strings.TrimPrefix(file.name, "mysql-test/")
		manifest = append(manifest, portableMySQLManifestFile{
			Path:   relative,
			Size:   int64(len(file.data)),
			SHA256: portableMySQLTestSHA256(file.data),
		})
		got, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("read extracted file %q: %v", relative, err)
		}
		if string(got) != string(file.data) {
			t.Fatalf("extracted file %q = %q, want %q", relative, got, file.data)
		}
	}
	if err := validatePortableMySQLFiles(destination, manifest); err != nil {
		t.Fatalf("validatePortableMySQLFiles() error = %v", err)
	}

	mysqldPath := filepath.Join(destination, "bin", "mysqld.exe")
	tampered, err := os.ReadFile(mysqldPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered[0] ^= 0xff
	if err := os.WriteFile(mysqldPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	err = validatePortableMySQLFiles(destination, manifest)
	if err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("validatePortableMySQLFiles(tampered) error = %v, want SHA256 mismatch", err)
	}
}

func TestEnsurePortableMySQLDataRejectsUnownedNonEmptyDirectory(t *testing.T) {
	paths := newProjectPaths(t.TempDir())
	if err := paths.ensureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(paths.mysqldExe, []byte("mysqld-fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.mysqlData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(paths.mysqlData, "unowned.dat"),
		[]byte("do not adopt"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	control := newController(paths, io.Discard, io.Discard)
	_, err := control.ensurePortableMySQLData(context.Background(), validTestInstance())
	if err == nil || !strings.Contains(err.Error(), "non-empty") ||
		!strings.Contains(err.Error(), "ownership state") {
		t.Fatalf(
			"ensurePortableMySQLData() error = %v, want non-empty ownership-state rejection",
			err,
		)
	}
	if isRegularFile(paths.mysqlDataState) {
		t.Fatal("rejected unowned data directory unexpectedly gained ownership state")
	}
}

func TestEnsurePortableMySQLDataValidatesReadyOwnershipAndHash(t *testing.T) {
	paths := newProjectPaths(t.TempDir())
	if err := paths.ensureDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(paths.mysqldExe, []byte("mysqld-fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(paths.mysqlData, "mysql"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mysql.ibd", "auto.cnf"} {
		if err := os.WriteFile(
			filepath.Join(paths.mysqlData, name),
			[]byte("initialized-fixture"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
	mysqldSHA256, err := fileSHA256(paths.mysqldExe)
	if err != nil {
		t.Fatal(err)
	}
	autoCNFSHA256, err := fileSHA256(filepath.Join(paths.mysqlData, "auto.cnf"))
	if err != nil {
		t.Fatal(err)
	}

	cfg := validTestInstance()
	readyState := portableMySQLDataState{
		SchemaVersion:  1,
		InstallationID: cfg.InstallationID,
		Phase:          portableMySQLPhaseReady,
		DataDirectory:  portableMySQLDataRelative,
		MysqldSHA256:   mysqldSHA256,
		AutoCNFSHA256:  autoCNFSHA256,
		Database:       cfg.Database.Name,
		InitializedAt:  time.Now().Add(-time.Minute).UTC(),
		ReadyAt:        time.Now().UTC(),
	}
	writeReadyState := func(t *testing.T, state portableMySQLDataState) {
		t.Helper()
		if err := writeJSON(paths.mysqlDataState, state, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	writeReadyState(t, readyState)
	control := newController(paths, io.Discard, io.Discard)
	got, err := control.ensurePortableMySQLData(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ensurePortableMySQLData(ready) error = %v", err)
	}
	if got.Phase != portableMySQLPhaseReady ||
		got.InstallationID != cfg.InstallationID ||
		!strings.EqualFold(got.MysqldSHA256, mysqldSHA256) {
		t.Fatalf("ensurePortableMySQLData(ready) state = %+v", got)
	}

	tests := []struct {
		name   string
		mutate func(*portableMySQLDataState)
		want   string
	}{
		{
			name: "installation ID",
			mutate: func(state *portableMySQLDataState) {
				state.InstallationID = "inst_other"
			},
			want: "ownership state does not match",
		},
		{
			name: "mysqld hash",
			mutate: func(state *portableMySQLDataState) {
				state.MysqldSHA256 = strings.Repeat("0", 64)
			},
			want: "ownership state does not match",
		},
		{
			name: "ready database",
			mutate: func(state *portableMySQLDataState) {
				state.Database = "other_database"
			},
			want: "ready state does not match",
		},
		{
			name: "ready timestamp",
			mutate: func(state *portableMySQLDataState) {
				state.ReadyAt = time.Time{}
			},
			want: "ready state does not match",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := readyState
			test.mutate(&invalid)
			writeReadyState(t, invalid)
			_, err := control.ensurePortableMySQLData(context.Background(), cfg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf(
					"ensurePortableMySQLData() error = %v, want substring %q",
					err,
					test.want,
				)
			}
		})
	}
}

func TestPortableTemplateAndLogicConfigExcludeRedisAndDocker(t *testing.T) {
	t.Setenv("DNF90_PROJECT_ROOT", "")
	paths, err := discoverPaths()
	if err != nil {
		t.Fatalf("discoverPaths() error = %v", err)
	}
	templateData, err := os.ReadFile(paths.instanceExample)
	if err != nil {
		t.Fatal(err)
	}
	template := strings.ToLower(string(templateData))
	for _, forbidden := range []string{"redis", "docker"} {
		if strings.Contains(template, forbidden) {
			t.Fatalf("instance template contains forbidden dependency %q", forbidden)
		}
	}
	if !strings.Contains(template, `"advertiseip": "auto_detect"`) {
		t.Fatal("instance template must auto-detect the private LAN IPv4 used by 90cn game channels")
	}

	logic := strings.ToLower(renderLogicConfig(validTestInstance()))
	if strings.Contains(logic, "redis") {
		t.Fatalf("logic config unexpectedly contains Redis configuration:\n%s", logic)
	}
	if !strings.Contains(logic, "mysql_dsn") {
		t.Fatalf("logic config is missing the MySQL repository DSN:\n%s", logic)
	}
}

func TestReadPortableMySQLServerUUID(t *testing.T) {
	autoCNF := filepath.Join(t.TempDir(), "auto.cnf")
	if err := os.WriteFile(
		autoCNF,
		[]byte("[auto]\r\nserver-uuid=12345678-1234-4abc-9def-1234567890ab\r\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	got, err := readPortableMySQLServerUUID(autoCNF)
	if err != nil {
		t.Fatalf("readPortableMySQLServerUUID() error = %v", err)
	}
	if got != "12345678-1234-4abc-9def-1234567890ab" {
		t.Fatalf("readPortableMySQLServerUUID() = %q", got)
	}

	if err := os.WriteFile(
		autoCNF,
		[]byte("[auto]\nserver-uuid=not-a-uuid\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := readPortableMySQLServerUUID(autoCNF); err == nil {
		t.Fatal("invalid server UUID was accepted")
	}
}

func TestDatabaseRuntimeConfigSHA256DetectsCredentialDrift(t *testing.T) {
	paths := newProjectPaths(t.TempDir())
	if err := paths.ensureDirectories(); err != nil {
		t.Fatal(err)
	}
	cfg := validTestInstance()
	if err := writeFile(
		paths.mysqlConfig,
		[]byte(renderMySQLConfig(paths, cfg)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(
		filepath.Join(paths.runtimeConfigs, "dnf", "logic.toml"),
		[]byte(renderLogicConfig(cfg)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	before, err := databaseRuntimeConfigSHA256(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.Password = "db_changed1234"
	if err := writeFile(
		filepath.Join(paths.runtimeConfigs, "dnf", "logic.toml"),
		[]byte(renderLogicConfig(cfg)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	after, err := databaseRuntimeConfigSHA256(paths)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(before, after) {
		t.Fatal("database runtime configuration digest ignored credential drift")
	}
}

type portableMySQLTestZIPEntry struct {
	name string
	data []byte
}

func writePortableMySQLTestZIP(
	t *testing.T,
	archivePath string,
	entries []portableMySQLTestZIPEntry,
) {
	t.Helper()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		target, err := writer.Create(entry.name)
		if err != nil {
			_ = writer.Close()
			_ = file.Close()
			t.Fatal(err)
		}
		if _, err := target.Write(entry.data); err != nil {
			_ = writer.Close()
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func portableMySQLTestSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return fmt.Sprintf("%X", digest[:])
}
