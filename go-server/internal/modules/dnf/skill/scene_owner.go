package skill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrSceneOwnerUnavailable = errors.New("scene skill owner is unavailable")
	ErrCharacterRequired     = errors.New("scene skill character id is required")
	ErrSkillRecordMissing    = errors.New("scene skill record is missing")
	ErrSeedInvalid           = errors.New("scene skill seed is invalid")
	ErrLayoutBuilderRequired = errors.New("scene skill layout builder is required")
)

type BackfillCommand struct {
	CharacterID string
	Seed        dnfrepo.SkillRecord
	UpdatedAt   time.Time
}

type SyncPointsCommand struct {
	CharacterID string
	Target      dnfrepo.SkillPointState
	UpdatedAt   time.Time
}

type LayoutBuilder func(dnfrepo.SkillRecord) (dnfrepo.SkillLayout, error)

type EnsureLayoutCommand struct {
	CharacterID string
	TreeIndex   int
	UpdatedAt   time.Time
	Build       LayoutBuilder
}

// SceneOwner owns missing-record backfill, SP/TP ledger repair, and missing
// quick-layout persistence. Current-client protobuf projection and PVF layout
// selection remain outside this repository boundary.
type SceneOwner struct {
	skills dnfrepo.CharacterSkillUnitOfWork
}

func NewSceneOwner(repositories dnfrepo.Group) (*SceneOwner, error) {
	if repositories.CharacterSkills == nil {
		return nil, ErrSceneOwnerUnavailable
	}
	return &SceneOwner{skills: repositories.CharacterSkills}, nil
}

func (o *SceneOwner) Backfill(ctx context.Context, command BackfillCommand) (dnfrepo.SkillRecord, bool, error) {
	characterID := strings.TrimSpace(command.CharacterID)
	if characterID == "" {
		return dnfrepo.SkillRecord{}, false, ErrCharacterRequired
	}
	if strings.TrimSpace(command.Seed.CharacterID) != characterID || len(command.Seed.Skills) == 0 {
		return dnfrepo.SkillRecord{}, false, ErrSeedInvalid
	}
	if o == nil || o.skills == nil {
		return dnfrepo.SkillRecord{}, false, ErrSceneOwnerUnavailable
	}
	ctx = normalizedSceneContext(ctx)
	if err := ctx.Err(); err != nil {
		return dnfrepo.SkillRecord{}, false, err
	}
	seed := dnfrepo.CloneSkill(command.Seed)
	seed.UpdatedAt = normalizedSceneTime(command.UpdatedAt)
	selected := dnfrepo.SkillRecord{}
	persisted := false
	err := o.skills.WithinCharacterSkill(ctx, characterID, func(skills dnfrepo.SkillRepository) error {
		current, found, err := skills.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if found && len(current.Skills) > 0 {
			selected = dnfrepo.CloneSkill(current)
			return nil
		}
		if found {
			seed.Cooldowns = current.Cooldowns
		}
		if err := dnfrepo.SaveSkillFields(
			ctx,
			skills,
			seed,
			dnfrepo.SkillFieldSkills,
			dnfrepo.SkillFieldPoints,
			dnfrepo.SkillFieldLayouts,
		); err != nil {
			return err
		}
		selected = dnfrepo.CloneSkill(seed)
		persisted = true
		return nil
	})
	if err != nil {
		return dnfrepo.SkillRecord{}, false, err
	}
	if len(selected.Skills) == 0 {
		return dnfrepo.SkillRecord{}, false, ErrSkillRecordMissing
	}
	return selected, persisted, nil
}

func (o *SceneOwner) SyncPoints(ctx context.Context, command SyncPointsCommand) (dnfrepo.SkillRecord, bool, error) {
	characterID := strings.TrimSpace(command.CharacterID)
	if characterID == "" {
		return dnfrepo.SkillRecord{}, false, ErrCharacterRequired
	}
	if o == nil || o.skills == nil {
		return dnfrepo.SkillRecord{}, false, ErrSceneOwnerUnavailable
	}
	ctx = normalizedSceneContext(ctx)
	if err := ctx.Err(); err != nil {
		return dnfrepo.SkillRecord{}, false, err
	}
	selected := dnfrepo.SkillRecord{}
	persisted := false
	err := o.skills.WithinCharacterSkill(ctx, characterID, func(skills dnfrepo.SkillRepository) error {
		current, found, err := skills.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || len(current.Skills) == 0 {
			return fmt.Errorf("%w: character=%s", ErrSkillRecordMissing, characterID)
		}
		current.Points, persisted, err = SyncPointState(current.Points, command.Target)
		if err != nil {
			return err
		}
		if persisted {
			current.UpdatedAt = normalizedSceneTime(command.UpdatedAt)
			if err := dnfrepo.SaveSkillFields(ctx, skills, current, dnfrepo.SkillFieldPoints); err != nil {
				return err
			}
		}
		selected = dnfrepo.CloneSkill(current)
		return nil
	})
	if err != nil {
		return dnfrepo.SkillRecord{}, false, err
	}
	return selected, persisted, nil
}

func (o *SceneOwner) EnsureLayout(ctx context.Context, command EnsureLayoutCommand) (dnfrepo.SkillRecord, dnfrepo.SkillLayout, bool, error) {
	characterID := strings.TrimSpace(command.CharacterID)
	if characterID == "" {
		return dnfrepo.SkillRecord{}, nil, false, ErrCharacterRequired
	}
	if command.Build == nil {
		return dnfrepo.SkillRecord{}, nil, false, ErrLayoutBuilderRequired
	}
	if o == nil || o.skills == nil {
		return dnfrepo.SkillRecord{}, nil, false, ErrSceneOwnerUnavailable
	}
	ctx = normalizedSceneContext(ctx)
	if err := ctx.Err(); err != nil {
		return dnfrepo.SkillRecord{}, nil, false, err
	}
	selected := dnfrepo.SkillRecord{}
	var selectedLayout dnfrepo.SkillLayout
	persisted := false
	err := o.skills.WithinCharacterSkill(ctx, characterID, func(skills dnfrepo.SkillRepository) error {
		current, found, err := skills.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || len(current.Skills) == 0 {
			return fmt.Errorf("%w: character=%s", ErrSkillRecordMissing, characterID)
		}
		layout, err := command.Build(dnfrepo.CloneSkill(current))
		if err != nil {
			return err
		}
		selected = dnfrepo.CloneSkill(current)
		selectedLayout = cloneSceneLayout(layout)
		if existing := current.Layouts[command.TreeIndex]; len(existing) > 0 {
			return nil
		}
		if current.Layouts == nil {
			current.Layouts = make(map[int]dnfrepo.SkillLayout)
		}
		current.Layouts[command.TreeIndex] = cloneSceneLayout(layout)
		current.UpdatedAt = normalizedSceneTime(command.UpdatedAt)
		if err := dnfrepo.SaveSkillFields(ctx, skills, current, dnfrepo.SkillFieldLayouts); err != nil {
			return err
		}
		selected = dnfrepo.CloneSkill(current)
		persisted = true
		return nil
	})
	if err != nil {
		return dnfrepo.SkillRecord{}, nil, false, err
	}
	return selected, selectedLayout, persisted, nil
}

func SyncPointState(current dnfrepo.SkillPointState, target dnfrepo.SkillPointState) (dnfrepo.SkillPointState, bool, error) {
	if current.TotalSP < 0 || current.RemainingSP < 0 || current.RemainingSP > current.TotalSP ||
		current.TotalTP < 0 || current.RemainingTP < 0 || current.RemainingTP > current.TotalTP ||
		target.TotalSP < 0 || target.TotalTP < 0 || target.SyncedLevel <= 0 {
		return dnfrepo.SkillPointState{}, false, fmt.Errorf(
			"invalid SP/TP ledger sync current=%+v target=%+v",
			current,
			target,
		)
	}
	spentSP := current.TotalSP - current.RemainingSP
	spentTP := current.TotalTP - current.RemainingTP
	next := target
	next.RemainingSP = next.TotalSP - spentSP
	if next.RemainingSP < 0 {
		next.RemainingSP = 0
	}
	next.RemainingTP = next.TotalTP - spentTP
	if next.RemainingTP < 0 {
		next.RemainingTP = 0
	}
	return next, next != current, nil
}

func normalizedSceneContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func normalizedSceneTime(value time.Time) time.Time {
	value = value.UTC()
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value
}

func cloneSceneLayout(layout dnfrepo.SkillLayout) dnfrepo.SkillLayout {
	if layout == nil {
		return nil
	}
	cloned := make(dnfrepo.SkillLayout, len(layout))
	for slot, skillID := range layout {
		cloned[slot] = skillID
	}
	return cloned
}
