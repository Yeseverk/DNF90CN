package datatable

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrPipelineInvalid = errors.New("data table pipeline is invalid")

type PipelinePlan struct {
	Name                 string        `json:"name,omitempty"`
	Baseline             RegionInput   `json:"baseline"`
	Candidate            RegionInput   `json:"candidate"`
	Regions              []RegionInput `json:"regions,omitempty"`
	RequireVersionChange bool          `json:"require_version_change,omitempty"`
	MaxAdded             *int          `json:"max_added,omitempty"`
	MaxChanged           *int          `json:"max_changed,omitempty"`
	MaxRemoved           *int          `json:"max_removed,omitempty"`
	RequireRegionClean   bool          `json:"require_region_clean,omitempty"`
}

type PipelineReport struct {
	Name         string         `json:"name,omitempty"`
	Baseline     RegionSnapshot `json:"baseline"`
	Candidate    RegionSnapshot `json:"candidate"`
	Diff         Diff           `json:"diff"`
	RegionReport *RegionReport  `json:"region_report,omitempty"`
	Accepted     bool           `json:"accepted"`
	Violations   []string       `json:"violations,omitempty"`
	GeneratedAt  time.Time      `json:"generated_at"`
}

func ValidatePipeline(ctx context.Context, plan PipelinePlan) (PipelineReport, error) {
	if err := contextErr(ctx); err != nil {
		return PipelineReport{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	plan = normPipelinePlan(plan)
	if plan.Baseline.Root == "" || plan.Candidate.Root == "" {
		return PipelineReport{}, fmt.Errorf("%w: baseline and candidate roots are required", ErrPipelineInvalid)
	}

	baseline, err := loadDirectory(ctx, plan.Baseline.Root, plan.Baseline.Version)
	if err != nil {
		return PipelineReport{}, fmt.Errorf("load baseline data tables: %w", err)
	}
	candidate, err := loadDirectory(ctx, plan.Candidate.Root, plan.Candidate.Version)
	if err != nil {
		return PipelineReport{}, fmt.Errorf("load candidate data tables: %w", err)
	}

	diff := diffTables(baseline.tables, candidate.tables)
	report := PipelineReport{
		Name: plan.Name,
		Baseline: RegionSnapshot{
			Name:     plan.Baseline.Name,
			Root:     plan.Baseline.Root,
			Version:  plan.Baseline.Version,
			Manifest: baseline.Manifest(),
		},
		Candidate: RegionSnapshot{
			Name:     plan.Candidate.Name,
			Root:     plan.Candidate.Root,
			Version:  plan.Candidate.Version,
			Manifest: candidate.Manifest(),
		},
		Diff:        cloneDiff(diff),
		GeneratedAt: time.Now().UTC(),
	}
	report.Violations = append(report.Violations, validatePipelineDiff(plan, baseline, candidate, diff)...)

	if len(plan.Regions) > 0 {
		regionReport, err := CompareRegions(ctx, plan.Regions, plan.Baseline.Name)
		if err != nil {
			return PipelineReport{}, err
		}
		report.RegionReport = &regionReport
		if plan.RequireRegionClean && !regionReport.Summary.Consistent {
			report.Violations = append(report.Violations, "region diff must be clean")
		}
	}
	report.Accepted = len(report.Violations) == 0
	return report, nil
}

func normPipelinePlan(plan PipelinePlan) PipelinePlan {
	plan.Name = strings.TrimSpace(plan.Name)
	plan.Baseline.Name = firstPipelineString(strings.TrimSpace(plan.Baseline.Name), "baseline")
	plan.Baseline.Root = strings.TrimSpace(plan.Baseline.Root)
	plan.Baseline.Version = strings.TrimSpace(plan.Baseline.Version)
	plan.Candidate.Name = firstPipelineString(strings.TrimSpace(plan.Candidate.Name), "candidate")
	plan.Candidate.Root = strings.TrimSpace(plan.Candidate.Root)
	plan.Candidate.Version = strings.TrimSpace(plan.Candidate.Version)
	for idx := range plan.Regions {
		plan.Regions[idx].Name = strings.TrimSpace(plan.Regions[idx].Name)
		plan.Regions[idx].Root = strings.TrimSpace(plan.Regions[idx].Root)
		plan.Regions[idx].Version = strings.TrimSpace(plan.Regions[idx].Version)
	}
	return plan
}

func validatePipelineDiff(plan PipelinePlan, baseline, candidate Dataset, diff Diff) []string {
	var violations []string
	if plan.RequireVersionChange && baseline.Manifest().Version == candidate.Manifest().Version {
		violations = append(violations, "candidate version must differ from baseline")
	}
	if plan.MaxAdded != nil && len(diff.Added) > *plan.MaxAdded {
		violations = append(violations, fmt.Sprintf("added tables %d exceeds max %d", len(diff.Added), *plan.MaxAdded))
	}
	if plan.MaxChanged != nil && len(diff.Changed) > *plan.MaxChanged {
		violations = append(violations, fmt.Sprintf("changed tables %d exceeds max %d", len(diff.Changed), *plan.MaxChanged))
	}
	if plan.MaxRemoved != nil && len(diff.Removed) > *plan.MaxRemoved {
		violations = append(violations, fmt.Sprintf("removed tables %d exceeds max %d", len(diff.Removed), *plan.MaxRemoved))
	}
	return violations
}

func firstPipelineString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
