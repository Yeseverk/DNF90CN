package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func validateAssets(paths projectPaths, out io.Writer) error {
	var manifest assetManifest
	if err := readStrictJSON(paths.assetManifest, &manifest); err != nil {
		return fmt.Errorf("asset manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("asset manifest schemaVersion must be 1")
	}
	for _, entry := range manifest.Files {
		if !entry.Required {
			continue
		}
		relative, err := safeManifestPath(entry.Path)
		if err != nil {
			return err
		}
		absolute := filepath.Join(paths.runtimeRoot, relative)
		info, err := os.Stat(absolute)
		if err != nil {
			return fmt.Errorf("required runtime asset is missing: %s", absolute)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("runtime asset is not a regular file: %s", absolute)
		}
		if info.Size() != entry.Size {
			return fmt.Errorf(
				"runtime asset size mismatch: %s (got %d, expected %d)",
				filepath.ToSlash(relative),
				info.Size(),
				entry.Size,
			)
		}
		digest, err := fileSHA256(absolute)
		if err != nil {
			return err
		}
		if !strings.EqualFold(digest, strings.TrimSpace(entry.SHA256)) {
			return fmt.Errorf("runtime asset SHA256 mismatch: %s", filepath.ToSlash(relative))
		}
		fmt.Fprintln(out, "Asset OK:", filepath.ToSlash(relative))
	}
	return nil
}

func validateClient(paths projectPaths, cfg instanceConfig, override string, out io.Writer) (string, string, error) {
	directory := strings.TrimSpace(override)
	if directory == "" {
		directory = strings.TrimSpace(cfg.Client.Directory)
	}
	if directory == "" {
		return "", "", fmt.Errorf(
			"client directory is not configured; set client.directory in runtime/config/instance.json or pass --client-directory",
		)
	}
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(paths.projectRoot, directory)
	}
	clientRoot, err := filepath.Abs(directory)
	if err != nil {
		return "", "", fmt.Errorf("resolve client directory: %w", err)
	}
	info, err := os.Stat(clientRoot)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("client directory does not exist: %s", clientRoot)
	}

	var manifest clientManifest
	if err := readStrictJSON(paths.clientManifest, &manifest); err != nil {
		return "", "", fmt.Errorf("client compatibility manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return "", "", fmt.Errorf("client compatibility manifest schemaVersion must be 1")
	}
	if manifest.Profile != cfg.Protocol.Profile {
		return "", "", fmt.Errorf(
			"client compatibility profile %q does not match instance profile %q",
			manifest.Profile,
			cfg.Protocol.Profile,
		)
	}

	var clientExecutable string
	for _, entry := range manifest.Files {
		var match string
		for _, name := range entry.Names {
			if name == "" || filepath.Base(name) != name {
				return "", "", fmt.Errorf("unsafe client manifest filename %q", name)
			}
			candidate := filepath.Join(clientRoot, name)
			if isRegularFile(candidate) {
				match = candidate
				break
			}
		}
		if match == "" {
			if entry.Required {
				return "", "", fmt.Errorf(
					"client file is missing; expected one of: %s",
					strings.Join(entry.Names, ", "),
				)
			}
			continue
		}
		digest, err := fileSHA256(match)
		if err != nil {
			return "", "", err
		}
		if !strings.EqualFold(digest, strings.TrimSpace(entry.SHA256)) {
			return "", "", fmt.Errorf("client file is incompatible: %s", match)
		}
		fmt.Fprintln(out, "Client file OK:", match)
		base := strings.ToLower(filepath.Base(match))
		if base == "dnf.exe" || base == "nopack.exe" {
			clientExecutable = match
		}
	}
	if clientExecutable == "" {
		return "", "", fmt.Errorf("compatible DNF.exe or NoPack.exe was not found")
	}
	fmt.Fprintln(out, "OK: client profile", manifest.Profile)
	return clientRoot, clientExecutable, nil
}

func writeAssetState(paths projectPaths) error {
	state := assetState{GeneratedAt: time.Now().UTC()}
	for _, relative := range []string{
		filepath.Join("data", "dnf", "Script.pvf"),
		filepath.Join("data", "dnf", "channel_info.etc"),
		filepath.Join("bin", "DNF90Server.exe"),
		filepath.Join("mysql", "server", "bin", "mysqld.exe"),
	} {
		absolute := filepath.Join(paths.runtimeRoot, relative)
		info, err := os.Stat(absolute)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat asset-state file %s: %w", absolute, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		digest, err := fileSHA256(absolute)
		if err != nil {
			return err
		}
		state.Files = append(state.Files, assetStateRecord{
			Path:   filepath.ToSlash(relative),
			Size:   info.Size(),
			SHA256: strings.ToUpper(digest),
		})
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode asset state: %w", err)
	}
	return writeFile(filepath.Join(paths.runtimeState, "asset-state.json"), append(data, '\n'), 0o600)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s for SHA256: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return strings.ToUpper(hex.EncodeToString(hash.Sum(nil))), nil
}

func safeManifestPath(path string) (string, error) {
	path = filepath.FromSlash(strings.TrimSpace(path))
	if path == "" || filepath.IsAbs(path) || escapesRoot(path) {
		return "", fmt.Errorf("unsafe manifest path %q", path)
	}
	return filepath.Clean(path), nil
}

func readStrictJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode %s: multiple JSON values", path)
		}
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
