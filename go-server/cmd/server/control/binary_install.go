package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const binaryInstallSchemaVersion = 1

type resolvedBinaryInstallEntry struct {
	state       binaryInstallStateEntry
	destination string
	temporary   string
	backup      string
}

func installBuiltBinarySet(paths projectPaths, targets []builtBinary) error {
	if len(targets) == 0 {
		return errors.New("binary installation set is empty")
	}
	if isRegularFile(paths.binaryInstallState) {
		if err := recoverBuiltBinarySet(paths); err != nil {
			return fmt.Errorf("recover previous binary installation: %w", err)
		}
	}
	state := binaryInstallState{
		SchemaVersion: binaryInstallSchemaVersion,
		Entries:       make([]binaryInstallStateEntry, 0, len(targets)),
	}
	seenDestinations := make(map[string]struct{}, len(targets))
	for index, target := range targets {
		if !isRegularFile(target.temp) {
			return fmt.Errorf("built DNF90 %s binary is missing: %s", target.label, target.temp)
		}
		if err := os.MkdirAll(filepath.Dir(target.destination), 0o755); err != nil {
			return err
		}
		if !sameDirectoryPath(filepath.Dir(target.destination), paths.runtimeBin) ||
			!sameDirectoryPath(filepath.Dir(target.temp), paths.runtimeBin) {
			return fmt.Errorf("DNF90 %s binary installation escaped runtime/bin", target.label)
		}
		destinationName := filepath.Base(target.destination)
		temporaryName := filepath.Base(target.temp)
		destinationKey := strings.ToLower(destinationName)
		if _, exists := seenDestinations[destinationKey]; exists {
			return fmt.Errorf("duplicate DNF90 binary installation target: %s", destinationName)
		}
		seenDestinations[destinationKey] = struct{}{}
		candidateSHA256, err := fileSHA256(target.temp)
		if err != nil {
			return err
		}
		entry := binaryInstallStateEntry{
			Destination:     destinationName,
			Temporary:       temporaryName,
			Backup:          destinationName + ".previous-" + strconv.Itoa(os.Getpid()) + "-" + strconv.Itoa(index),
			CandidateSHA256: candidateSHA256,
		}
		destinationInfo, err := os.Stat(target.destination)
		switch {
		case err == nil:
			if !destinationInfo.Mode().IsRegular() {
				return fmt.Errorf("DNF90 %s destination is not a regular file: %s", target.label, target.destination)
			}
			entry.HadOriginal = true
			entry.OriginalSHA256, err = fileSHA256(target.destination)
			if err != nil {
				return err
			}
		case os.IsNotExist(err):
		default:
			return fmt.Errorf("inspect DNF90 %s destination: %w", target.label, err)
		}
		backupPath := filepath.Join(paths.runtimeBin, entry.Backup)
		if _, err := os.Lstat(backupPath); err == nil {
			return fmt.Errorf("binary backup path already exists: %s", backupPath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect binary backup path %s: %w", backupPath, err)
		}
		state.Entries = append(state.Entries, entry)
	}
	if err := writeJSON(paths.binaryInstallState, state, 0o600); err != nil {
		return fmt.Errorf("record binary installation transaction: %w", err)
	}

	var mutationErr error
	for index, target := range targets {
		entry := state.Entries[index]
		backupPath := filepath.Join(paths.runtimeBin, entry.Backup)
		if entry.HadOriginal {
			if err := os.Rename(target.destination, backupPath); err != nil {
				mutationErr = fmt.Errorf("preserve current DNF90 %s binary: %w", target.label, err)
				break
			}
		}
		if err := os.Rename(target.temp, target.destination); err != nil {
			mutationErr = fmt.Errorf("install built DNF90 %s binary: %w", target.label, err)
			break
		}
	}
	if mutationErr != nil {
		return errors.Join(mutationErr, recoverBuiltBinarySet(paths))
	}
	if err := recoverBuiltBinarySet(paths); err != nil {
		return fmt.Errorf("finalize binary installation transaction: %w", err)
	}
	return nil
}

func recoverBuiltBinarySet(paths projectPaths) error {
	var state binaryInstallState
	if err := readStrictJSON(paths.binaryInstallState, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	entries, err := resolveBinaryInstallState(paths, state)
	if err != nil {
		return err
	}

	allCandidates := true
	for _, entry := range entries {
		exists, digest, err := regularFileDigest(entry.destination)
		if err != nil {
			return err
		}
		if !exists || !strings.EqualFold(digest, entry.state.CandidateSHA256) {
			allCandidates = false
			break
		}
	}
	if allCandidates {
		for _, entry := range entries {
			if err := removeKnownBinaryArtifact(
				entry.backup,
				entry.state.HadOriginal,
				entry.state.OriginalSHA256,
			); err != nil {
				return err
			}
			if err := removeKnownBinaryArtifact(
				entry.temporary,
				true,
				entry.state.CandidateSHA256,
			); err != nil {
				return err
			}
		}
		return removeBinaryInstallState(paths.binaryInstallState)
	}

	for _, entry := range entries {
		destinationExists, destinationDigest, err := regularFileDigest(entry.destination)
		if err != nil {
			return err
		}
		backupExists, backupDigest, err := regularFileDigest(entry.backup)
		if err != nil {
			return err
		}
		if entry.state.HadOriginal {
			switch {
			case destinationExists && strings.EqualFold(destinationDigest, entry.state.OriginalSHA256):
				if backupExists {
					if !strings.EqualFold(backupDigest, entry.state.OriginalSHA256) {
						return fmt.Errorf("refusing to remove unrecognized binary backup: %s", entry.backup)
					}
					if err := os.Remove(entry.backup); err != nil {
						return fmt.Errorf("remove redundant binary backup %s: %w", entry.backup, err)
					}
				}
			default:
				if !backupExists || !strings.EqualFold(backupDigest, entry.state.OriginalSHA256) {
					return fmt.Errorf("cannot safely restore original binary %s", entry.destination)
				}
				if destinationExists {
					if !strings.EqualFold(destinationDigest, entry.state.CandidateSHA256) {
						return fmt.Errorf("refusing to replace unrecognized binary: %s", entry.destination)
					}
					if err := os.Remove(entry.destination); err != nil {
						return fmt.Errorf("remove interrupted candidate %s: %w", entry.destination, err)
					}
				}
				if err := os.Rename(entry.backup, entry.destination); err != nil {
					return fmt.Errorf("restore original binary %s: %w", entry.destination, err)
				}
			}
		} else {
			if backupExists {
				return fmt.Errorf("unexpected backup for newly installed binary: %s", entry.backup)
			}
			if destinationExists {
				if !strings.EqualFold(destinationDigest, entry.state.CandidateSHA256) {
					return fmt.Errorf("refusing to remove unrecognized binary: %s", entry.destination)
				}
				if err := os.Remove(entry.destination); err != nil {
					return fmt.Errorf("remove interrupted new binary %s: %w", entry.destination, err)
				}
			}
		}
		if err := removeKnownBinaryArtifact(
			entry.temporary,
			true,
			entry.state.CandidateSHA256,
		); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		exists, digest, err := regularFileDigest(entry.destination)
		if err != nil {
			return err
		}
		if entry.state.HadOriginal {
			if !exists || !strings.EqualFold(digest, entry.state.OriginalSHA256) {
				return fmt.Errorf("binary rollback verification failed: %s", entry.destination)
			}
		} else if exists {
			return fmt.Errorf("new binary remained after rollback: %s", entry.destination)
		}
	}
	return removeBinaryInstallState(paths.binaryInstallState)
}

func resolveBinaryInstallState(
	paths projectPaths,
	state binaryInstallState,
) ([]resolvedBinaryInstallEntry, error) {
	if state.SchemaVersion != binaryInstallSchemaVersion {
		return nil, fmt.Errorf("unsupported binary installation state schema %d", state.SchemaVersion)
	}
	if len(state.Entries) == 0 || len(state.Entries) > 16 {
		return nil, fmt.Errorf("invalid binary installation entry count %d", len(state.Entries))
	}
	seen := make(map[string]struct{}, len(state.Entries)*3)
	resolved := make([]resolvedBinaryInstallEntry, 0, len(state.Entries))
	for _, entry := range state.Entries {
		if !validSHA256(entry.CandidateSHA256) {
			return nil, fmt.Errorf("invalid candidate SHA256 for %q", entry.Destination)
		}
		if entry.HadOriginal != validSHA256(entry.OriginalSHA256) {
			return nil, fmt.Errorf("invalid original SHA256 for %q", entry.Destination)
		}
		pathsByName := make([]string, 0, 3)
		for _, name := range []string{entry.Destination, entry.Temporary, entry.Backup} {
			cleaned, err := safeManifestPath(name)
			if err != nil || filepath.Base(cleaned) != cleaned {
				return nil, fmt.Errorf("unsafe binary installation filename %q", name)
			}
			key := strings.ToLower(cleaned)
			if _, exists := seen[key]; exists {
				return nil, fmt.Errorf("duplicate binary installation filename %q", cleaned)
			}
			seen[key] = struct{}{}
			absolute := filepath.Join(paths.runtimeBin, cleaned)
			if !pathInside(paths.runtimeBin, absolute) {
				return nil, fmt.Errorf("binary installation filename escaped runtime/bin: %q", cleaned)
			}
			pathsByName = append(pathsByName, absolute)
		}
		resolved = append(resolved, resolvedBinaryInstallEntry{
			state:       entry,
			destination: pathsByName[0],
			temporary:   pathsByName[1],
			backup:      pathsByName[2],
		})
	}
	return resolved, nil
}

func regularFileDigest(path string) (bool, string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("inspect binary transaction path %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, "", fmt.Errorf("binary transaction path is not a regular file: %s", path)
	}
	digest, err := fileSHA256(path)
	if err != nil {
		return false, "", err
	}
	return true, digest, nil
}

func removeKnownBinaryArtifact(path string, allowed bool, expectedSHA256 string) error {
	exists, digest, err := regularFileDigest(path)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !allowed || !strings.EqualFold(digest, expectedSHA256) {
		return fmt.Errorf("refusing to remove unrecognized binary transaction file: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove binary transaction file %s: %w", path, err)
	}
	return nil
}

func removeBinaryInstallState(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove completed binary installation state: %w", err)
	}
	return nil
}

func sameDirectoryPath(left, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	return leftErr == nil &&
		rightErr == nil &&
		strings.EqualFold(filepath.Clean(leftAbsolute), filepath.Clean(rightAbsolute))
}

func validSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
