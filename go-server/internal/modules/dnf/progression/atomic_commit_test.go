package progression

import (
	"context"
	"errors"
	"math"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCommitAtomicallyPersistsPrecomputedExperienceLevelSPAndTP(t *testing.T) {
	ctx := context.Background()
	repos := seedProgressionCommitRepositories(t, ctx)
	plan := validProgressionCommitPlan()

	result, err := Commit(ctx, repos.CharacterProgression, plan)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if !result.Applied || result.Idempotent || result.Previous != plan.Expected || result.Current != plan.Next {
		t.Fatalf("Commit() result = %+v", result)
	}
	character, _, _ := repos.Character.Load(ctx, plan.CharacterID)
	skill, _, _ := repos.Skill.Load(ctx, plan.CharacterID)
	if character.Level != plan.Next.Level || character.Stats["exp"] != int64(plan.Next.Experience) {
		t.Fatalf("character progression = level %d exp %d", character.Level, character.Stats["exp"])
	}
	if skill.Points != plan.Next.SkillPoints {
		t.Fatalf("skill points = %+v, want %+v", skill.Points, plan.Next.SkillPoints)
	}
	if len(skill.Skills) != 1 || skill.Skills[46].Level != 2 {
		t.Fatalf("non-point skill state changed: %+v", skill.Skills)
	}
}

func TestCommitPlanFromExperienceSkillPointsOnlyMapsCallerPlan(t *testing.T) {
	planned := ExperienceSkillPointPlan{
		Experience: ExperienceResult{
			PreviousLevel:      2,
			PreviousExperience: 240,
			NewLevel:           4,
			NewExperience:      540,
		},
		SkillPoints: SkillPointAdvance{
			Previous: dnfrepo.SkillPointState{TotalSP: 100, RemainingSP: 75, TotalTP: 3, RemainingTP: 2, SyncedLevel: 2},
			New:      dnfrepo.SkillPointState{TotalSP: 170, RemainingSP: 145, TotalTP: 3, RemainingTP: 2, SyncedLevel: 4},
		},
	}
	got := CommitPlanFromExperienceSkillPoints("77", "dnf:1", planned)
	if got.CharacterID != "77" || got.AccountID != "dnf:1" ||
		got.Expected.Level != 2 || got.Expected.Experience != 240 || got.Expected.SkillPoints != planned.SkillPoints.Previous ||
		got.Next.Level != 4 || got.Next.Experience != 540 || got.Next.SkillPoints != planned.SkillPoints.New {
		t.Fatalf("mapped commit plan = %+v", got)
	}
}

func TestCommitExactReplayIsIdempotentWithoutWrites(t *testing.T) {
	ctx := context.Background()
	repos := seedProgressionCommitRepositories(t, ctx)
	plan := validProgressionCommitPlan()
	if _, err := Commit(ctx, repos.CharacterProgression, plan); err != nil {
		t.Fatalf("first Commit() error = %v", err)
	}

	counted := &countingProgressionUnitOfWork{
		characters: repos.Character,
		skills:     repos.Skill,
	}
	result, err := Commit(ctx, counted, plan)
	if err != nil {
		t.Fatalf("replay Commit() error = %v", err)
	}
	if result.Applied || !result.Idempotent || result.Previous != plan.Next || result.Current != plan.Next {
		t.Fatalf("replay result = %+v", result)
	}
	if counted.characterSaves != 0 || counted.skillSaves != 0 {
		t.Fatalf("idempotent replay writes character=%d skill=%d", counted.characterSaves, counted.skillSaves)
	}
}

func TestCommitAtomicallyTransitionsProtectedCharacterStats(t *testing.T) {
	ctx := context.Background()
	repos := seedProgressionCommitRepositories(t, ctx)
	plan := validProgressionCommitPlan()
	plan.ExpectedCharacterStats = map[string]int64{"honor_expert_level": 0, "honor_expert_progress_experience": 0}
	plan.NextCharacterStats = map[string]int64{"honor_expert_level": 1, "honor_expert_progress_experience": 42}

	if _, err := Commit(ctx, repos.CharacterProgression, plan); err != nil {
		t.Fatal(err)
	}
	character, _, _ := repos.Character.Load(ctx, plan.CharacterID)
	if character.Stats["honor_expert_level"] != 1 || character.Stats["honor_expert_progress_experience"] != 42 {
		t.Fatalf("expert character stats=%+v", character.Stats)
	}
	if replay, err := Commit(ctx, repos.CharacterProgression, plan); err != nil || !replay.Idempotent {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}

	stale := validProgressionCommitPlan()
	stale.ExpectedCharacterStats = map[string]int64{"honor_expert_level": 0}
	stale.NextCharacterStats = map[string]int64{"honor_expert_level": 2}
	if _, err := Commit(ctx, repos.CharacterProgression, stale); !errors.Is(err, ErrCommitStateInvalid) {
		t.Fatalf("stale protected stats error=%v", err)
	}
}

func TestCommitRejectsStaleOrPartiallyCommittedVersion(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name       string
		mutateSeed func(*dnfrepo.CharacterRecord, *dnfrepo.SkillRecord)
		want       error
	}{
		{
			name: "later progression",
			mutateSeed: func(character *dnfrepo.CharacterRecord, skill *dnfrepo.SkillRecord) {
				character.Level = 5
				character.Stats["exp"] = 800
				skill.Points = dnfrepo.SkillPointState{TotalSP: 200, RemainingSP: 175, TotalTP: 8, RemainingTP: 7, SyncedLevel: 5}
			},
			want: ErrCommitVersionPrecondition,
		},
		{
			name: "character-only partial state",
			mutateSeed: func(character *dnfrepo.CharacterRecord, _ *dnfrepo.SkillRecord) {
				character.Level = 4
				character.Stats["exp"] = 540
			},
			want: ErrCommitStateInvalid,
		},
		{
			name: "skill-only partial state",
			mutateSeed: func(_ *dnfrepo.CharacterRecord, skill *dnfrepo.SkillRecord) {
				skill.Points = validProgressionCommitPlan().Next.SkillPoints
			},
			want: ErrCommitStateInvalid,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repos := seedProgressionCommitRepositories(t, ctx)
			character, _, _ := repos.Character.Load(ctx, "77")
			skill, _, _ := repos.Skill.Load(ctx, "77")
			test.mutateSeed(&character, &skill)
			if err := repos.Character.Save(ctx, character); err != nil {
				t.Fatalf("mutate character: %v", err)
			}
			if err := repos.Skill.Save(ctx, skill); err != nil {
				t.Fatalf("mutate skill: %v", err)
			}
			beforeCharacter := dnfrepo.CloneCharacter(character)
			beforeSkill := dnfrepo.CloneSkill(skill)

			_, err := Commit(ctx, repos.CharacterProgression, validProgressionCommitPlan())
			if !errors.Is(err, test.want) {
				t.Fatalf("Commit() error = %v, want %v", err, test.want)
			}
			afterCharacter, _, _ := repos.Character.Load(ctx, "77")
			afterSkill, _, _ := repos.Skill.Load(ctx, "77")
			if afterCharacter.Level != beforeCharacter.Level || afterCharacter.Stats["exp"] != beforeCharacter.Stats["exp"] || afterSkill.Points != beforeSkill.Points {
				t.Fatalf("stale plan mutated state character=%+v skill=%+v", afterCharacter, afterSkill)
			}
		})
	}
}

func TestCommitRequiresExactAccountAndBothOwnedRows(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name    string
		prepare func(t *testing.T, repos dnfrepo.Group)
		plan    func() CommitPlan
		want    error
	}{
		{
			name:    "account mismatch",
			prepare: func(*testing.T, dnfrepo.Group) {},
			plan: func() CommitPlan {
				plan := validProgressionCommitPlan()
				plan.AccountID = "dnf:other"
				return plan
			},
			want: ErrCommitOwnershipMismatch,
		},
		{
			name: "skill row missing",
			prepare: func(t *testing.T, repos dnfrepo.Group) {
				deleter, ok := repos.Skill.(interface {
					Delete(context.Context, string) error
				})
				if !ok {
					t.Fatal("memory skill store does not support Delete")
				}
				if err := deleter.Delete(ctx, "77"); err != nil {
					t.Fatalf("delete skill: %v", err)
				}
			},
			plan: validProgressionCommitPlan,
			want: ErrCommitStateMissing,
		},
		{
			name: "exp state missing",
			prepare: func(t *testing.T, repos dnfrepo.Group) {
				character, _, _ := repos.Character.Load(ctx, "77")
				delete(character.Stats, "exp")
				if err := repos.Character.Save(ctx, character); err != nil {
					t.Fatalf("remove exp: %v", err)
				}
			},
			plan: validProgressionCommitPlan,
			want: ErrCommitStateMissing,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repos := seedProgressionCommitRepositories(t, ctx)
			test.prepare(t, repos)
			_, err := Commit(ctx, repos.CharacterProgression, test.plan())
			if !errors.Is(err, test.want) {
				t.Fatalf("Commit() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCommitRejectsInvalidCallerPlansBeforeTransaction(t *testing.T) {
	valid := validProgressionCommitPlan()
	for _, test := range []struct {
		name   string
		mutate func(*CommitPlan)
	}{
		{name: "missing owner", mutate: func(plan *CommitPlan) { plan.AccountID = "" }},
		{name: "zero level", mutate: func(plan *CommitPlan) { plan.Expected.Level = 0; plan.Expected.SkillPoints.SyncedLevel = 0 }},
		{name: "level overflow", mutate: func(plan *CommitPlan) {
			plan.Next.Level = math.MaxUint8 + 1
			plan.Next.SkillPoints.SyncedLevel = math.MaxUint8 + 1
		}},
		{name: "synced level mismatch", mutate: func(plan *CommitPlan) { plan.Next.SkillPoints.SyncedLevel-- }},
		{name: "negative points", mutate: func(plan *CommitPlan) { plan.Next.SkillPoints.RemainingSP = -1 }},
		{name: "point overflow", mutate: func(plan *CommitPlan) {
			plan.Next.SkillPoints.TotalTP = math.MaxUint16 + 1
			plan.Next.SkillPoints.RemainingTP = math.MaxUint16 + 1
		}},
		{name: "remaining exceeds total", mutate: func(plan *CommitPlan) { plan.Next.SkillPoints.RemainingSP = plan.Next.SkillPoints.TotalSP + 1 }},
		{name: "level regression", mutate: func(plan *CommitPlan) { plan.Next.Level = 1; plan.Next.SkillPoints.SyncedLevel = 1 }},
		{name: "experience regression", mutate: func(plan *CommitPlan) { plan.Next.Experience = plan.Expected.Experience - 1 }},
		{name: "total SP regression", mutate: func(plan *CommitPlan) {
			plan.Next.SkillPoints.TotalSP = plan.Expected.SkillPoints.TotalSP - 1
			plan.Next.SkillPoints.RemainingSP = plan.Expected.SkillPoints.RemainingSP - 1
		}},
		{name: "spent SP changed", mutate: func(plan *CommitPlan) { plan.Next.SkillPoints.RemainingSP-- }},
		{name: "spent TP changed", mutate: func(plan *CommitPlan) { plan.Next.SkillPoints.RemainingTP-- }},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := valid
			test.mutate(&plan)
			counted := &countingProgressionUnitOfWork{}
			_, err := Commit(context.Background(), counted, plan)
			if !errors.Is(err, ErrCommitPlanInvalid) {
				t.Fatalf("Commit() error = %v, want ErrCommitPlanInvalid", err)
			}
			if counted.calls != 0 {
				t.Fatalf("invalid plan entered transaction %d times", counted.calls)
			}
		})
	}
}

func TestCommitRejectsPersistedNegativeOrOverflowExperience(t *testing.T) {
	ctx := context.Background()
	for _, experience := range []int64{-1, int64(math.MaxUint32) + 1} {
		t.Run("invalid persisted exp", func(t *testing.T) {
			repos := seedProgressionCommitRepositories(t, ctx)
			character, _, _ := repos.Character.Load(ctx, "77")
			character.Stats["exp"] = experience
			if err := repos.Character.Save(ctx, character); err != nil {
				t.Fatalf("seed invalid exp: %v", err)
			}
			_, err := Commit(ctx, repos.CharacterProgression, validProgressionCommitPlan())
			if !errors.Is(err, ErrCommitStateInvalid) {
				t.Fatalf("Commit() error = %v, want ErrCommitStateInvalid", err)
			}
		})
	}
}

func TestCommitRejectsMissingUnitOfWorkAndCancelledContext(t *testing.T) {
	if _, err := Commit(context.Background(), nil, validProgressionCommitPlan()); !errors.Is(err, dnfrepo.ErrCharacterProgressionTransactionUnavailable) {
		t.Fatalf("nil UoW error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	counted := &countingProgressionUnitOfWork{}
	if _, err := Commit(ctx, counted, validProgressionCommitPlan()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Commit() error = %v", err)
	}
	if counted.calls != 0 {
		t.Fatalf("cancelled commit entered transaction %d times", counted.calls)
	}
}

func seedProgressionCommitRepositories(t *testing.T, ctx context.Context) dnfrepo.Group {
	t.Helper()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Name:        "owner",
		Level:       2,
		Stats:       map[string]int64{"exp": 240, "gold": 50},
	}); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	if err := repos.Skill.Save(ctx, dnfrepo.SkillRecord{
		CharacterID: "77",
		Skills:      map[int64]dnfrepo.SkillState{46: {Level: 2, Enabled: true}},
		Points:      validProgressionCommitPlan().Expected.SkillPoints,
	}); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	return repos
}

func validProgressionCommitPlan() CommitPlan {
	return CommitPlan{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Expected: CommitState{
			Level:      2,
			Experience: 240,
			SkillPoints: dnfrepo.SkillPointState{
				TotalSP: 100, RemainingSP: 75,
				TotalTP: 3, RemainingTP: 2,
				SyncedLevel: 2,
			},
		},
		Next: CommitState{
			Level:      4,
			Experience: 540,
			SkillPoints: dnfrepo.SkillPointState{
				TotalSP: 170, RemainingSP: 145,
				TotalTP: 5, RemainingTP: 4,
				SyncedLevel: 4,
			},
		},
	}
}

type countingProgressionUnitOfWork struct {
	characters     dnfrepo.CharacterRepository
	skills         dnfrepo.SkillRepository
	calls          int
	characterSaves int
	skillSaves     int
}

func (u *countingProgressionUnitOfWork) WithinCharacterProgression(
	ctx context.Context,
	_ string,
	apply func(dnfrepo.CharacterRepository, dnfrepo.SkillRepository) error,
) error {
	u.calls++
	if u.characters == nil || u.skills == nil {
		return dnfrepo.ErrCharacterProgressionTransactionUnavailable
	}
	return apply(
		&countingProgressionCharacterRepository{CharacterRepository: u.characters, saves: &u.characterSaves},
		&countingProgressionSkillRepository{SkillRepository: u.skills, saves: &u.skillSaves},
	)
}

type countingProgressionCharacterRepository struct {
	dnfrepo.CharacterRepository
	saves *int
}

func (r *countingProgressionCharacterRepository) Save(ctx context.Context, record dnfrepo.CharacterRecord) error {
	*r.saves++
	return r.CharacterRepository.Save(ctx, record)
}

func (r *countingProgressionCharacterRepository) SaveFields(ctx context.Context, record dnfrepo.CharacterRecord, fields ...dnfrepo.CharacterField) error {
	*r.saves++
	return dnfrepo.SaveCharacterFields(ctx, r.CharacterRepository, record, fields...)
}

type countingProgressionSkillRepository struct {
	dnfrepo.SkillRepository
	saves *int
}

func (r *countingProgressionSkillRepository) Save(ctx context.Context, record dnfrepo.SkillRecord) error {
	*r.saves++
	return r.SkillRepository.Save(ctx, record)
}

func (r *countingProgressionSkillRepository) SaveFields(ctx context.Context, record dnfrepo.SkillRecord, fields ...dnfrepo.SkillField) error {
	*r.saves++
	return dnfrepo.SaveSkillFields(ctx, r.SkillRepository, record, fields...)
}
