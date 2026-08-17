package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func preparePortableMySQL(
	ctx context.Context,
	paths projectPaths,
	out io.Writer,
) error {
	manifest, err := loadPortableMySQLManifest(paths)
	if err != nil {
		return err
	}
	if isRegularFile(paths.mysqlInstallState) {
		if err := validatePortableMySQLFiles(paths.mysqlServer, manifest.Files); err == nil {
			if err := installAppLocalVCRuntime(paths, paths.mysqlServer); err != nil {
				return err
			}
			if err := verifyPortableMySQLExecutable(
				ctx,
				paths.mysqlServer,
				manifest.Version,
			); err != nil {
				return err
			}
			fmt.Fprintf(out, "Bundled MySQL %s: verified\n", manifest.Version)
			return nil
		}
	}
	if info, err := os.Stat(paths.mysqlServer); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("portable MySQL target is not a directory: %s", paths.mysqlServer)
		}
		if err := validatePortableMySQLFiles(paths.mysqlServer, manifest.Files); err != nil {
			return fmt.Errorf(
				"existing portable MySQL directory failed verification and was not overwritten: %w",
				err,
			)
		}
		if err := installAppLocalVCRuntime(paths, paths.mysqlServer); err != nil {
			return err
		}
		if err := verifyPortableMySQLExecutable(
			ctx,
			paths.mysqlServer,
			manifest.Version,
		); err != nil {
			return err
		}
		state := portableMySQLInstallState{
			SchemaVersion: 1,
			Product:       manifest.Product,
			Version:       manifest.Version,
			Platform:      manifest.Platform,
			ArchiveSHA256: strings.ToUpper(manifest.Archive.SHA256),
			InstalledAt:   time.Now().UTC(),
		}
		return writeJSON(paths.mysqlInstallState, state, 0o600)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect portable MySQL target: %w", err)
	}

	archiveRelative, err := safeManifestPath(manifest.Archive.Path)
	if err != nil {
		return fmt.Errorf("portable MySQL archive path: %w", err)
	}
	archivePath := filepath.Join(paths.projectRoot, archiveRelative)
	info, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("bundled MySQL archive is missing: %s", archivePath)
	}
	if !info.Mode().IsRegular() || info.Size() != manifest.Archive.Size {
		return fmt.Errorf("bundled MySQL archive size mismatch: %s", archivePath)
	}
	digest, err := fileSHA256(archivePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(digest, manifest.Archive.SHA256) {
		return fmt.Errorf("bundled MySQL archive SHA256 mismatch: %s", archivePath)
	}

	staging := filepath.Join(
		paths.mysqlRoot,
		"server.extracting-"+strconv.Itoa(os.Getpid()),
	)
	if err := safeRemoveRuntimeTree(paths.runtimeRoot, staging); err != nil {
		return err
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("create portable MySQL extraction directory: %w", err)
	}
	installed := false
	defer func() {
		if !installed {
			_ = safeRemoveRuntimeTree(paths.runtimeRoot, staging)
		}
	}()

	fmt.Fprintf(out, "Extracting bundled MySQL %s (first start only)...\n", manifest.Version)
	if err := extractPortableMySQLArchive(
		archivePath,
		manifest.Archive.Root,
		staging,
	); err != nil {
		return err
	}
	if err := validatePortableMySQLFiles(staging, manifest.Files); err != nil {
		return err
	}
	if err := installAppLocalVCRuntime(paths, staging); err != nil {
		return err
	}
	if err := verifyPortableMySQLExecutable(ctx, staging, manifest.Version); err != nil {
		return err
	}
	state := portableMySQLInstallState{
		SchemaVersion: 1,
		Product:       manifest.Product,
		Version:       manifest.Version,
		Platform:      manifest.Platform,
		ArchiveSHA256: strings.ToUpper(manifest.Archive.SHA256),
		InstalledAt:   time.Now().UTC(),
	}
	if err := writeJSON(
		filepath.Join(staging, ".dnf90-install.json"),
		state,
		0o600,
	); err != nil {
		return err
	}
	if err := os.Rename(staging, paths.mysqlServer); err != nil {
		return fmt.Errorf("install extracted portable MySQL: %w", err)
	}
	installed = true
	fmt.Fprintf(out, "Bundled MySQL %s extracted and verified.\n", manifest.Version)
	return nil
}

func installAppLocalVCRuntime(paths projectPaths, mysqlServer string) error {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return fmt.Errorf(
			"bundled MySQL requires Windows x64; current runtime is %s/%s",
			runtime.GOOS,
			runtime.GOARCH,
		)
	}
	var manifest appLocalRuntimeManifest
	if err := readStrictJSON(paths.vcRuntimeManifest, &manifest); err != nil {
		return fmt.Errorf("VC++ app-local manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 ||
		!strings.EqualFold(manifest.Platform, "winx64") ||
		strings.TrimSpace(manifest.Version) == "" ||
		len(manifest.Files) == 0 {
		return errors.New("VC++ app-local manifest is invalid")
	}
	destinationRoot := filepath.Join(mysqlServer, "bin")
	for _, entry := range manifest.Files {
		relative, err := safeManifestPath(entry.Path)
		if err != nil || filepath.Dir(relative) != "." {
			return fmt.Errorf("unsafe VC++ app-local filename %q", entry.Path)
		}
		source := filepath.Join(paths.vcRuntimeRoot, relative)
		if err := validatePortableMySQLFiles(
			paths.vcRuntimeRoot,
			[]portableMySQLManifestFile{entry},
		); err != nil {
			return fmt.Errorf("bundled VC++ app-local source: %w", err)
		}
		destination := filepath.Join(destinationRoot, relative)
		if isRegularFile(destination) {
			if err := validatePortableMySQLFiles(
				destinationRoot,
				[]portableMySQLManifestFile{entry},
			); err != nil {
				return fmt.Errorf(
					"existing MySQL app-local runtime failed verification and was not overwritten: %w",
					err,
				)
			}
			continue
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read bundled VC++ app-local file: %w", err)
		}
		if err := writeFile(destination, data, 0o644); err != nil {
			return fmt.Errorf("install VC++ app-local file: %w", err)
		}
		if err := validatePortableMySQLFiles(
			destinationRoot,
			[]portableMySQLManifestFile{entry},
		); err != nil {
			return fmt.Errorf("verify installed VC++ app-local file: %w", err)
		}
	}
	return nil
}

func verifyPortableMySQLExecutable(
	ctx context.Context,
	mysqlServer string,
	expectedVersion string,
) error {
	executable := filepath.Join(mysqlServer, "bin", "mysqld.exe")
	cmd := exec.CommandContext(ctx, executable, "--version")
	cmd.Dir = mysqlServer
	configureBackgroundProcess(cmd)
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"bundled MySQL executable check failed: %w: %s",
			err,
			strings.TrimSpace(output.String()),
		)
	}
	if !strings.Contains(output.String(), "Ver "+strings.TrimSpace(expectedVersion)) {
		return fmt.Errorf(
			"bundled MySQL executable reported an unexpected version: %s",
			strings.TrimSpace(output.String()),
		)
	}
	return nil
}

func loadPortableMySQLManifest(paths projectPaths) (portableMySQLManifest, error) {
	var manifest portableMySQLManifest
	if err := readStrictJSON(paths.mysqlManifest, &manifest); err != nil {
		return manifest, fmt.Errorf("portable MySQL manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return manifest, errors.New("portable MySQL manifest schemaVersion must be 1")
	}
	if strings.TrimSpace(manifest.Product) == "" ||
		strings.TrimSpace(manifest.Version) == "" ||
		!strings.EqualFold(strings.TrimSpace(manifest.Platform), "winx64") {
		return manifest, errors.New("portable MySQL manifest identity is invalid")
	}
	if manifest.Archive.Size <= 0 ||
		strings.TrimSpace(manifest.Archive.SHA256) == "" ||
		strings.TrimSpace(manifest.Archive.Root) == "" {
		return manifest, errors.New("portable MySQL archive manifest is incomplete")
	}
	if len(manifest.Files) == 0 {
		return manifest, errors.New("portable MySQL file manifest is empty")
	}
	return manifest, nil
}

func validatePortableMySQLFiles(
	root string,
	files []portableMySQLManifestFile,
) error {
	for _, entry := range files {
		relative, err := safeManifestPath(entry.Path)
		if err != nil {
			return fmt.Errorf("portable MySQL file path: %w", err)
		}
		absolute := filepath.Join(root, relative)
		info, err := os.Stat(absolute)
		if err != nil {
			return fmt.Errorf("portable MySQL file is missing: %s", absolute)
		}
		if !info.Mode().IsRegular() || info.Size() != entry.Size {
			return fmt.Errorf("portable MySQL file size mismatch: %s", absolute)
		}
		digest, err := fileSHA256(absolute)
		if err != nil {
			return err
		}
		if !strings.EqualFold(digest, entry.SHA256) {
			return fmt.Errorf("portable MySQL file SHA256 mismatch: %s", absolute)
		}
	}
	return nil
}

func extractPortableMySQLArchive(archivePath, archiveRoot, destination string) error {
	archiveRoot = strings.Trim(strings.TrimSpace(filepath.ToSlash(archiveRoot)), "/")
	if archiveRoot == "" || strings.Contains(archiveRoot, "/") ||
		archiveRoot == "." || archiveRoot == ".." {
		return fmt.Errorf("unsafe portable MySQL archive root %q", archiveRoot)
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open bundled MySQL archive: %w", err)
	}
	defer func() { _ = reader.Close() }()

	prefix := archiveRoot + "/"
	foundFile := false
	for _, entry := range reader.File {
		name := strings.ReplaceAll(entry.Name, `\`, "/")
		clean := path.Clean(name)
		if clean == archiveRoot {
			continue
		}
		if path.IsAbs(clean) || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, "../") || !strings.HasPrefix(clean, prefix) {
			return fmt.Errorf("unsafe path in bundled MySQL archive: %q", entry.Name)
		}
		relative := strings.TrimPrefix(clean, prefix)
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if !pathInside(destination, target) {
			return fmt.Errorf("bundled MySQL archive entry escapes target: %q", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create bundled MySQL directory: %w", err)
			}
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 || !entry.Mode().IsRegular() {
			return fmt.Errorf("unsupported bundled MySQL archive entry: %q", entry.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create bundled MySQL parent directory: %w", err)
		}
		source, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open bundled MySQL archive entry %q: %w", entry.Name, err)
		}
		destinationFile, err := os.OpenFile(
			target,
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
			0o644,
		)
		if err != nil {
			_ = source.Close()
			return fmt.Errorf("create bundled MySQL file %q: %w", target, err)
		}
		_, copyErr := io.Copy(destinationFile, source)
		closeErr := errors.Join(destinationFile.Close(), source.Close())
		if copyErr != nil || closeErr != nil {
			return fmt.Errorf(
				"extract bundled MySQL file %q: %w",
				entry.Name,
				errors.Join(copyErr, closeErr),
			)
		}
		foundFile = true
	}
	if !foundFile {
		return errors.New("bundled MySQL archive contained no files under its declared root")
	}
	return nil
}

func safeRemoveRuntimeTree(runtimeRoot, target string) error {
	if !pathInside(runtimeRoot, target) {
		return fmt.Errorf("refusing to remove path outside runtime: %s", target)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove temporary runtime path %s: %w", target, err)
	}
	return nil
}

func pathInside(root, target string) bool {
	rootAbs, rootErr := filepath.Abs(root)
	targetAbs, targetErr := filepath.Abs(target)
	if rootErr != nil || targetErr != nil {
		return false
	}
	relative, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
