package datatable

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	ErrRegionRequired      = errors.New("data table region is required")
	ErrBaselineNotFound    = errors.New("data table baseline region not found")
	ErrRegionDiffRequires2 = errors.New("data table region diff requires at least two regions")
)

type RegionInput struct {
	Name    string `json:"name"`
	Root    string `json:"root"`
	Version string `json:"version,omitempty"`
}

type RegionSnapshot struct {
	Name     string   `json:"name"`
	Root     string   `json:"root"`
	Version  string   `json:"version,omitempty"`
	Manifest Manifest `json:"manifest"`
}

type RegionDiff struct {
	Region           string   `json:"region"`
	Baseline         string   `json:"baseline"`
	Manifest         Manifest `json:"manifest"`
	BaselineManifest Manifest `json:"baseline_manifest"`
	Diff             Diff     `json:"diff"`
}

type RegionDiffSummary struct {
	Regions          int      `json:"regions"`
	Compared         int      `json:"compared"`
	Baseline         string   `json:"baseline"`
	BaselineChecksum string   `json:"baseline_checksum,omitempty"`
	Consistent       bool     `json:"consistent"`
	DriftedRegions   []string `json:"drifted_regions,omitempty"`
	Added            int      `json:"added"`
	Changed          int      `json:"changed"`
	Removed          int      `json:"removed"`
	Unchanged        int      `json:"unchanged"`
}

type RegionReport struct {
	Baseline    string            `json:"baseline"`
	Regions     []RegionSnapshot  `json:"regions"`
	Diffs       []RegionDiff      `json:"diffs"`
	Summary     RegionDiffSummary `json:"summary"`
	GeneratedAt time.Time         `json:"generated_at"`
}

func CompareRegions(ctx context.Context, regions []RegionInput, baseline string) (RegionReport, error) {
	if err := contextErr(ctx); err != nil {
		return RegionReport{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(regions) == 0 {
		return RegionReport{}, ErrRegionRequired
	}
	if len(regions) < 2 {
		return RegionReport{}, ErrRegionDiffRequires2
	}

	datasets := make(map[string]Dataset, len(regions))
	snapshots := make([]RegionSnapshot, 0, len(regions))
	for _, input := range regions {
		normalized, err := normalizeRegionInput(input)
		if err != nil {
			return RegionReport{}, err
		}
		if _, exists := datasets[normalized.Name]; exists {
			return RegionReport{}, fmt.Errorf("%w: duplicate region %s", ErrRegionRequired, normalized.Name)
		}
		dataset, err := loadDirectory(ctx, normalized.Root, normalized.Version)
		if err != nil {
			return RegionReport{}, fmt.Errorf("load data table region %s: %w", normalized.Name, err)
		}
		datasets[normalized.Name] = dataset
		snapshots = append(snapshots, RegionSnapshot{
			Name:     normalized.Name,
			Root:     normalized.Root,
			Version:  normalized.Version,
			Manifest: dataset.Manifest(),
		})
		if strings.TrimSpace(baseline) == "" {
			baseline = normalized.Name
		}
	}

	baseline = strings.TrimSpace(baseline)
	base, ok := datasets[baseline]
	if !ok {
		return RegionReport{}, fmt.Errorf("%w: %s", ErrBaselineNotFound, baseline)
	}

	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Name < snapshots[j].Name })
	report := RegionReport{
		Baseline:    baseline,
		Regions:     snapshots,
		GeneratedAt: time.Now().UTC(),
	}
	report.Summary = RegionDiffSummary{
		Regions:          len(snapshots),
		Baseline:         baseline,
		BaselineChecksum: base.Manifest().Checksum,
		Consistent:       true,
	}

	for _, snapshot := range snapshots {
		if snapshot.Name == baseline {
			continue
		}
		dataset := datasets[snapshot.Name]
		diff := diffTables(base.tables, dataset.tables)
		regionDiff := RegionDiff{
			Region:           snapshot.Name,
			Baseline:         baseline,
			Manifest:         dataset.Manifest(),
			BaselineManifest: base.Manifest(),
			Diff:             cloneDiff(diff),
		}
		report.Diffs = append(report.Diffs, regionDiff)
		report.Summary.Compared++
		report.Summary.Added += len(diff.Added)
		report.Summary.Changed += len(diff.Changed)
		report.Summary.Removed += len(diff.Removed)
		report.Summary.Unchanged += len(diff.Unchanged)
		if diff.HasChanges() {
			report.Summary.Consistent = false
			report.Summary.DriftedRegions = append(report.Summary.DriftedRegions, snapshot.Name)
		}
	}
	sort.Strings(report.Summary.DriftedRegions)
	return report, nil
}

func normalizeRegionInput(input RegionInput) (RegionInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Root = strings.TrimSpace(input.Root)
	input.Version = strings.TrimSpace(input.Version)
	if input.Root == "" {
		return RegionInput{}, ErrDirectoryRequired
	}
	if input.Name == "" {
		input.Name = filepath.Base(filepath.Clean(input.Root))
	}
	if input.Name == "" || input.Name == "." || input.Name == string(filepath.Separator) {
		return RegionInput{}, ErrRegionRequired
	}
	return input, nil
}
