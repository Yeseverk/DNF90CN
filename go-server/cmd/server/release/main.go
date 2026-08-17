package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const releaseRootName = "DNF90-source-oneclick"

type options struct {
	root      string
	output    string
	skipBuild bool
}

type fileManifest struct {
	Files []manifestFile `json:"files"`
}

type manifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type mysqlManifest struct {
	Archive manifestFile `json:"archive"`
}

type clientManifest struct {
	Files []struct {
		Names  []string `json:"names"`
		SHA256 string   `json:"sha256"`
	} `json:"files"`
}

type releaseInfo struct {
	SchemaVersion int       `json:"schemaVersion"`
	Name          string    `json:"name"`
	GeneratedAt   time.Time `json:"generatedAt"`
	Source        bool      `json:"sourceIncluded"`
	OneClick      bool      `json:"oneClickRuntimeIncluded"`
	DatabaseHost  string    `json:"databaseHost"`
	Client        string    `json:"clientDistribution"`
}

func main() {
	var opts options
	flag.StringVar(&opts.root, "root", "", "DNF90 project root")
	flag.StringVar(&opts.output, "output", "", "output ZIP path")
	flag.BoolVar(&opts.skipBuild, "skip-build", false, "reuse the four runtime binaries instead of rebuilding")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "FAILED:", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	root, err := resolveProjectRoot(opts.root)
	if err != nil {
		return err
	}
	releasesDir := filepath.Join(root, "releases")
	if err := os.MkdirAll(releasesDir, 0o755); err != nil {
		return fmt.Errorf("create releases directory: %w", err)
	}

	if err := validateReleaseInputs(root); err != nil {
		return err
	}

	generatedAt := time.Now().UTC()
	if strings.TrimSpace(opts.output) == "" {
		opts.output = filepath.Join(
			releasesDir,
			fmt.Sprintf("DNF90-source-oneclick-%s.zip", generatedAt.Local().Format("20060102-150405")),
		)
	} else if !filepath.IsAbs(opts.output) {
		opts.output = filepath.Join(root, opts.output)
	}
	opts.output, err = filepath.Abs(opts.output)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if filepath.Ext(opts.output) != ".zip" {
		return fmt.Errorf("release output must be a .zip file: %s", opts.output)
	}
	if !pathWithin(releasesDir, opts.output) {
		return fmt.Errorf("release output must stay under %s", releasesDir)
	}

	stage, err := os.MkdirTemp(releasesDir, ".release-stage-")
	if err != nil {
		return fmt.Errorf("create release staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	payload := filepath.Join(stage, releaseRootName)
	if err := os.MkdirAll(payload, 0o755); err != nil {
		return fmt.Errorf("create release payload: %w", err)
	}

	if err := copyReleaseFiles(root, payload); err != nil {
		return err
	}
	if opts.skipBuild {
		if err := copyRuntimeBinaries(root, payload); err != nil {
			return err
		}
	} else if err := buildRuntimeBinaries(root, payload); err != nil {
		return err
	}

	info := releaseInfo{
		SchemaVersion: 1,
		Name:          releaseRootName,
		GeneratedAt:   generatedAt,
		Source:        true,
		OneClick:      true,
		DatabaseHost:  "127.0.0.1:13306",
		Client:        "client executables are not included; compatibility patch source and DLL are included",
	}
	if err := writeJSON(filepath.Join(payload, "RELEASE_INFO.json"), info); err != nil {
		return err
	}
	if err := validatePayload(payload); err != nil {
		return err
	}
	if err := writeSHA256Manifest(payload); err != nil {
		return err
	}

	tempZip, err := os.CreateTemp(releasesDir, ".release-*.zip")
	if err != nil {
		return fmt.Errorf("reserve release archive: %w", err)
	}
	tempZipPath := tempZip.Name()
	if err := tempZip.Close(); err != nil {
		return fmt.Errorf("close release archive placeholder: %w", err)
	}
	defer os.Remove(tempZipPath)
	if err := zipDirectory(stage, tempZipPath); err != nil {
		return err
	}
	if err := replaceFile(tempZipPath, opts.output); err != nil {
		return err
	}

	hash, size, err := fileDigest(opts.output)
	if err != nil {
		return err
	}
	count, err := countFiles(payload)
	if err != nil {
		return err
	}
	fmt.Println("Release package created.")
	fmt.Println("ZIP:", opts.output)
	fmt.Println("Files:", count)
	fmt.Println("Bytes:", size)
	fmt.Println("SHA256:", hash)
	return nil
}

func resolveProjectRoot(configured string) (string, error) {
	if strings.TrimSpace(configured) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
		configured = filepath.Join(cwd, "..")
	}
	root, err := filepath.Abs(configured)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	if !regularFile(filepath.Join(root, "go-server", "go.mod")) ||
		!regularFile(filepath.Join(root, "deploy", "templates", "instance.example.json")) {
		return "", fmt.Errorf("not a DNF90 project root: %s", root)
	}
	return filepath.Clean(root), nil
}

func validateReleaseInputs(root string) error {
	var assets fileManifest
	if err := readJSON(filepath.Join(root, "deploy", "assets", "manifest.json"), &assets); err != nil {
		return err
	}
	for _, entry := range assets.Files {
		if err := validateManifestFile(
			filepath.Join(root, "runtime", filepath.FromSlash(entry.Path)),
			entry,
		); err != nil {
			return err
		}
	}

	var mysql mysqlManifest
	if err := readJSON(filepath.Join(root, "deploy", "assets", "mysql-portable.json"), &mysql); err != nil {
		return err
	}
	if err := validateManifestFile(filepath.Join(root, filepath.FromSlash(mysql.Archive.Path)), mysql.Archive); err != nil {
		return err
	}

	var vcRuntime fileManifest
	if err := readJSON(filepath.Join(root, "deploy", "assets", "vcruntime-app-local.json"), &vcRuntime); err != nil {
		return err
	}
	for _, entry := range vcRuntime.Files {
		if err := validateManifestFile(
			filepath.Join(root, "deploy", "vendor", "vcruntime", "x64", filepath.FromSlash(entry.Path)),
			entry,
		); err != nil {
			return err
		}
	}

	var client clientManifest
	if err := readJSON(filepath.Join(root, "deploy", "assets", "client-compatibility.json"), &client); err != nil {
		return err
	}
	expectedDLL := ""
	for _, entry := range client.Files {
		for _, name := range entry.Names {
			if strings.EqualFold(name, "90CN.dll") {
				expectedDLL = entry.SHA256
			}
		}
	}
	if expectedDLL == "" {
		return errors.New("client manifest does not declare 90CN.dll")
	}
	patchDLL := filepath.Join(root, "client-patch", "bin", "90CN.dll")
	actualDLL, _, err := fileDigest(patchDLL)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actualDLL, expectedDLL) {
		return fmt.Errorf("client-patch DLL hash mismatch: got %s want %s", actualDLL, expectedDLL)
	}
	return nil
}

func validateManifestFile(path string, entry manifestFile) error {
	hash, size, err := fileDigest(path)
	if err != nil {
		return err
	}
	if entry.Size != 0 && size != entry.Size {
		return fmt.Errorf("asset size mismatch for %s: got %d want %d", path, size, entry.Size)
	}
	if !strings.EqualFold(hash, entry.SHA256) {
		return fmt.Errorf("asset hash mismatch for %s: got %s want %s", path, hash, entry.SHA256)
	}
	return nil
}

func copyReleaseFiles(root, payload string) error {
	topFiles := []string{
		".gitattributes",
		".gitignore",
		"AGENTS.md",
		"INSTALL_CLIENT_PATCH.bat",
		"LAUNCH_CLIENT.bat",
		"LOGIN.bat",
		"PACKAGE_RELEASE.bat",
		"README.md",
		"REBUILD.bat",
		"REBUILD_CLIENT_PATCH.bat",
		"START.bat",
		"STATUS.bat",
		"STOP.bat",
		"changlog.md",
	}
	for _, rel := range topFiles {
		if err := copyFile(
			filepath.Join(root, filepath.FromSlash(rel)),
			filepath.Join(payload, filepath.FromSlash(rel)),
		); err != nil {
			return err
		}
	}

	for _, rel := range []string{"go-server", "docs"} {
		if err := copyTree(
			filepath.Join(root, rel),
			filepath.Join(payload, rel),
			releaseSourceFileAllowed,
		); err != nil {
			return err
		}
	}
	if err := copyTree(
		filepath.Join(root, "client-patch"),
		filepath.Join(payload, "client-patch"),
		clientPatchSourceFileAllowed,
	); err != nil {
		return err
	}

	deployFiles := []string{
		"deploy/assets/channel_info.etc",
		"deploy/assets/client-compatibility.json",
		"deploy/assets/manifest.json",
		"deploy/assets/mysql-portable.json",
		"deploy/assets/vcruntime-app-local.json",
		"deploy/templates/instance.example.json",
		"deploy/vendor/README.md",
		"deploy/vendor/mysql/mysql-8.4.10-winx64.zip",
		"deploy/vendor/vcruntime/x64/MSVCP140.dll",
		"deploy/vendor/vcruntime/x64/VCRUNTIME140.dll",
		"deploy/vendor/vcruntime/x64/VCRUNTIME140_1.dll",
		"deploy/windows/control.bat",
		"runtime/data/dnf/Script.pvf",
		"runtime/data/dnf/channel_info.etc",
	}
	for _, rel := range deployFiles {
		if err := copyFile(
			filepath.Join(root, filepath.FromSlash(rel)),
			filepath.Join(payload, filepath.FromSlash(rel)),
		); err != nil {
			return err
		}
	}
	return nil
}

func releaseSourceFileAllowed(rel string, entry fs.DirEntry) bool {
	base := strings.ToLower(entry.Name())
	if entry.IsDir() {
		switch base {
		case ".git", ".idea", ".vscode", "_codex_verify", "backups":
			return false
		default:
			return true
		}
	}
	if strings.HasSuffix(base, "~") {
		return false
	}
	for _, suffix := range []string{
		".bak", ".dmp", ".exe", ".exp", ".i64", ".idb", ".ilk", ".lib",
		".log", ".obj", ".orig", ".pdb", ".rej", ".test", ".tmp",
	} {
		if strings.HasSuffix(base, suffix) {
			if strings.EqualFold(filepath.ToSlash(rel), "bin/90CN.dll") {
				return true
			}
			return false
		}
	}
	return true
}

func clientPatchSourceFileAllowed(rel string, entry fs.DirEntry) bool {
	first := strings.Split(filepath.ToSlash(rel), "/")[0]
	if entry.IsDir() && strings.EqualFold(first, "release") {
		return false
	}
	return releaseSourceFileAllowed(rel, entry)
}

func buildRuntimeBinaries(root, payload string) error {
	goExe, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("Go is required to build release binaries: %w", err)
	}
	binDir := filepath.Join(payload, "runtime", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create release binary directory: %w", err)
	}
	targets := []struct {
		name    string
		pkg     string
		ldflags string
	}{
		{name: "DNF90Control.exe", pkg: `.\cmd\server\control`},
		{name: "DNF90Doctor.exe", pkg: `.\cmd\server\doctor`},
		{name: "DNF90Launcher.exe", pkg: `.\cmd\server\launcher`, ldflags: "-H=windowsgui"},
		{name: "DNF90Server.exe", pkg: `.\cmd\server\dnf90`},
	}
	for _, target := range targets {
		fmt.Println("Building", target.name)
		args := []string{
			"build",
			"-buildvcs=false",
			"-mod=readonly",
			"-trimpath",
		}
		if target.ldflags != "" {
			args = append(args, "-ldflags", target.ldflags)
		}
		args = append(
			args,
			"-o", filepath.Join(binDir, target.name),
			target.pkg,
		)
		cmd := exec.Command(goExe, args...)
		cmd.Dir = filepath.Join(root, "go-server")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build %s: %w", target.name, err)
		}
	}
	return nil
}

func copyRuntimeBinaries(root, payload string) error {
	for _, name := range []string{
		"DNF90Control.exe",
		"DNF90Doctor.exe",
		"DNF90Launcher.exe",
		"DNF90Server.exe",
	} {
		if err := copyFile(
			filepath.Join(root, "runtime", "bin", name),
			filepath.Join(payload, "runtime", "bin", name),
		); err != nil {
			return err
		}
	}
	return nil
}

func validatePayload(payload string) error {
	forbiddenPrefixes := []string{
		"runtime/backups/",
		"runtime/config/",
		"runtime/configs/",
		"runtime/logs/",
		"runtime/mysql/",
		"runtime/state/",
	}
	required := []string{
		"LOGIN.bat",
		"START.bat",
		"runtime/bin/DNF90Control.exe",
		"runtime/bin/DNF90Doctor.exe",
		"runtime/bin/DNF90Launcher.exe",
		"runtime/bin/DNF90Server.exe",
		"runtime/data/dnf/Script.pvf",
		"deploy/vendor/mysql/mysql-8.4.10-winx64.zip",
		"client-patch/90CN.cpp",
		"client-patch/bin/90CN.dll",
		"go-server/go.mod",
	}
	for _, rel := range required {
		if !regularFile(filepath.Join(payload, filepath.FromSlash(rel))) {
			return fmt.Errorf("required release file is missing: %s", rel)
		}
	}
	return filepath.WalkDir(payload, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(payload, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		lower := strings.ToLower(rel)
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(lower, prefix) {
				return fmt.Errorf("forbidden runtime state entered release: %s", rel)
			}
		}
		for _, token := range []string{".dmp", ".idb", ".i64", ".log", ".tmp", ".bak"} {
			if strings.Contains(lower, token) {
				return fmt.Errorf("forbidden diagnostic or temporary file entered release: %s", rel)
			}
		}
		return nil
	})
}

func writeSHA256Manifest(payload string) error {
	var files []string
	if err := filepath.WalkDir(payload, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.EqualFold(entry.Name(), "SOURCE_MANIFEST.sha256") {
			return nil
		}
		rel, err := filepath.Rel(payload, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return fmt.Errorf("enumerate release manifest files: %w", err)
	}
	sort.Strings(files)
	manifestPath := filepath.Join(payload, "SOURCE_MANIFEST.sha256")
	file, err := os.Create(manifestPath)
	if err != nil {
		return fmt.Errorf("create release manifest: %w", err)
	}
	writer := bufio.NewWriter(file)
	for _, rel := range files {
		hash, _, err := fileDigest(filepath.Join(payload, filepath.FromSlash(rel)))
		if err != nil {
			file.Close()
			return err
		}
		if _, err := fmt.Fprintf(writer, "%s *%s\n", hash, rel); err != nil {
			file.Close()
			return fmt.Errorf("write release manifest: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		return fmt.Errorf("flush release manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close release manifest: %w", err)
	}
	return nil
}

func zipDirectory(root, destination string) error {
	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create ZIP: %w", err)
	}
	archive := zip.NewWriter(file)
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = rel
		header.Method = zip.Deflate
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".dll", ".exe", ".pvf", ".zip":
			header.Method = zip.Store
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeArchiveErr := archive.Close()
	closeFileErr := file.Close()
	if walkErr != nil {
		return fmt.Errorf("write ZIP: %w", walkErr)
	}
	if closeArchiveErr != nil {
		return fmt.Errorf("finalize ZIP: %w", closeArchiveErr)
	}
	if closeFileErr != nil {
		return fmt.Errorf("close ZIP: %w", closeFileErr)
	}
	return nil
}

func copyTree(sourceRoot, destinationRoot string, allowed func(string, fs.DirEntry) bool) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(destinationRoot, 0o755)
		}
		if !allowed(rel, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destination := filepath.Join(destinationRoot, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		return copyFile(path, destination)
	})
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open release input %s: %w", source, err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("stat release input %s: %w", source, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("release input is not a regular file: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create release directory: %w", err)
	}
	output, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create release file %s: %w", destination, err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return fmt.Errorf("copy release file %s: %w", source, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close release file %s: %w", destination, closeErr)
	}
	return os.Chtimes(destination, info.ModTime(), info.ModTime())
}

func readJSON(path string, destination any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(content, destination); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func fileDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, fmt.Errorf("hash %s: %w", path, err)
	}
	return strings.ToUpper(hex.EncodeToString(hash.Sum(nil))), size, nil
}

func countFiles(root string) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	return count, err
}

func replaceFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create release output directory: %w", err)
	}
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace existing release %s: %w", destination, err)
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("install release ZIP: %w", err)
	}
	return nil
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
