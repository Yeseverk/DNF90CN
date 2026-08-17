package hotupdate

import (
	"context"
	"sort"
	"time"
)

type BundleDiff struct {
	PreviousVersion string       `json:"previous_version,omitempty"`
	CurrentVersion  string       `json:"current_version,omitempty"`
	Added           []BundleFile `json:"added,omitempty"`
	Modified        []BundleFile `json:"modified,omitempty"`
	Removed         []BundleFile `json:"removed,omitempty"`
	Unchanged       []BundleFile `json:"unchanged,omitempty"`
	ChangedBytes    int64        `json:"changed_bytes,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
}

func ScanBundleManifest(ctx context.Context, sourceDir, version string) (BundleManifest, error) {
	if err := contextErr(ctx); err != nil {
		return BundleManifest{}, err
	}
	files, err := bundleFiles(ctx, sourceDir)
	if err != nil {
		return BundleManifest{}, err
	}
	bytes, checksum := bundleSummary(files)
	return BundleManifest{
		Version:   version,
		Files:     cloneBundleFiles(files),
		Bytes:     bytes,
		Checksum:  checksum,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func DiffBundleManifests(previous, current BundleManifest) BundleDiff {
	previousByPath := bundleFilesByPath(previous.Files)
	currentByPath := bundleFilesByPath(current.Files)
	diff := BundleDiff{
		PreviousVersion: previous.Version,
		CurrentVersion:  current.Version,
		CreatedAt:       time.Now().UTC(),
	}
	for path, file := range currentByPath {
		old, ok := previousByPath[path]
		switch {
		case !ok:
			diff.Added = append(diff.Added, file)
			diff.ChangedBytes += file.Bytes
		case old.Checksum != file.Checksum || old.Bytes != file.Bytes:
			diff.Modified = append(diff.Modified, file)
			diff.ChangedBytes += file.Bytes
		default:
			diff.Unchanged = append(diff.Unchanged, file)
		}
	}
	for path, file := range previousByPath {
		if _, ok := currentByPath[path]; ok {
			continue
		}
		diff.Removed = append(diff.Removed, file)
	}
	sortBundleFiles(diff.Added)
	sortBundleFiles(diff.Modified)
	sortBundleFiles(diff.Removed)
	sortBundleFiles(diff.Unchanged)
	return diff
}

func bundleFilesByPath(files []BundleFile) map[string]BundleFile {
	out := make(map[string]BundleFile, len(files))
	for _, file := range files {
		if err := validateBundlePath(file.Path); err != nil {
			continue
		}
		out[file.Path] = file
	}
	return out
}

func cloneBundleFiles(files []BundleFile) []BundleFile {
	if len(files) == 0 {
		return nil
	}
	return append([]BundleFile(nil), files...)
}

func sortBundleFiles(files []BundleFile) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}
