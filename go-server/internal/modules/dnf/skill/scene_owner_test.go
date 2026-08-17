package skill

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestSceneOwnerBackfillPersistsSeedAndPreservesCooldowns(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Skill.Save(ctx, dnfrepo.SkillRecord{
		CharacterID: "77",
		Cooldowns:   map[int64]time.Time{46: time.Unix(1234, 0).UTC()},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewSceneOwner(repositories)
	if err != nil {
		t.Fatalf("NewSceneOwner error = %v", err)
	}
	seed := dnfrepo.SkillRecord{
		CharacterID: "77",
		Skills:      map[int64]dnfrepo.SkillState{46: {Level: 1, Enabled: true}},
		Points:      dnfrepo.SkillPointState{TotalSP: 100, RemainingSP: 100, SyncedLevel: 1},
		Layouts:     map[int]dnfrepo.SkillLayout{0: {0: 46}},
	}
	result, persisted, err := owner.Backfill(ctx, BackfillCommand{CharacterID: "77", Seed: seed})
	if err != nil || !persisted {
		t.Fatalf("Backfill result=%+v persisted=%t error=%v", result, persisted, err)
	}
	if result.Skills[46].Level != 1 || result.Cooldowns[46].Unix() != 1234 || result.Layouts[0][0] != 46 {
		t.Fatalf("Backfill result = %+v", result)
	}
	saved, found, err := repositories.Skill.Load(ctx, "77")
	if err != nil || !found || saved.Cooldowns[46].Unix() != 1234 || saved.Skills[46].Level != 1 {
		t.Fatalf("saved=%+v found=%t error=%v", saved, found, err)
	}
}

func TestSceneOwnerSyncPointsPreservesSpentLedger(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	seedSceneSkillRecord(t, ctx, repositories)
	owner, err := NewSceneOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	result, persisted, err := owner.SyncPoints(ctx, SyncPointsCommand{
		CharacterID: "77",
		Target: dnfrepo.SkillPointState{
			TotalSP:     150,
			TotalTP:     30,
			SyncedLevel: 20,
		},
	})
	if err != nil || !persisted {
		t.Fatalf("SyncPoints result=%+v persisted=%t error=%v", result, persisted, err)
	}
	want := dnfrepo.SkillPointState{
		TotalSP:     150,
		RemainingSP: 110,
		TotalTP:     30,
		RemainingTP: 20,
		SyncedLevel: 20,
	}
	if result.Points != want {
		t.Fatalf("points=%+v want=%+v", result.Points, want)
	}
	saved, _, _ := repositories.Skill.Load(ctx, "77")
	if saved.Points != want {
		t.Fatalf("saved points=%+v want=%+v", saved.Points, want)
	}
}

func TestSceneOwnerEnsureLayoutPersistsOnceAndKeepsConcurrentExistingLayout(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	seedSceneSkillRecord(t, ctx, repositories)
	owner, err := NewSceneOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	want := dnfrepo.SkillLayout{0: 46, 4: 47}
	result, layout, persisted, err := owner.EnsureLayout(ctx, EnsureLayoutCommand{
		CharacterID: "77",
		TreeIndex:   0,
		Build: func(dnfrepo.SkillRecord) (dnfrepo.SkillLayout, error) {
			return want, nil
		},
	})
	if err != nil || !persisted || !reflect.DeepEqual(layout, want) || !reflect.DeepEqual(result.Layouts[0], want) {
		t.Fatalf("EnsureLayout result=%+v layout=%v persisted=%t error=%v", result, layout, persisted, err)
	}

	existing := dnfrepo.SkillLayout{2: 47}
	saved, _, _ := repositories.Skill.Load(ctx, "77")
	saved.Layouts[0] = existing
	if err := repositories.Skill.Save(ctx, saved); err != nil {
		t.Fatal(err)
	}
	result, layout, persisted, err = owner.EnsureLayout(ctx, EnsureLayoutCommand{
		CharacterID: "77",
		TreeIndex:   0,
		Build: func(record dnfrepo.SkillRecord) (dnfrepo.SkillLayout, error) {
			return record.Layouts[0], nil
		},
	})
	if err != nil || persisted || !reflect.DeepEqual(layout, existing) || !reflect.DeepEqual(result.Layouts[0], existing) {
		t.Fatalf("existing result=%+v layout=%v persisted=%t error=%v", result, layout, persisted, err)
	}
}

func TestSceneOwnerEnsureLayoutBuilderFailureDoesNotPersist(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	seedSceneSkillRecord(t, ctx, repositories)
	owner, err := NewSceneOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("reject scene layout")
	_, _, persisted, err := owner.EnsureLayout(ctx, EnsureLayoutCommand{
		CharacterID: "77",
		TreeIndex:   0,
		Build: func(dnfrepo.SkillRecord) (dnfrepo.SkillLayout, error) {
			return nil, wantErr
		},
	})
	if !errors.Is(err, wantErr) || persisted {
		t.Fatalf("EnsureLayout persisted=%t error=%v", persisted, err)
	}
	saved, _, _ := repositories.Skill.Load(ctx, "77")
	if len(saved.Layouts) != 0 {
		t.Fatalf("failed layout persisted: %+v", saved.Layouts)
	}
}

func seedSceneSkillRecord(t *testing.T, ctx context.Context, repositories dnfrepo.Group) {
	t.Helper()
	if err := repositories.Skill.Save(ctx, dnfrepo.SkillRecord{
		CharacterID: "77",
		Skills: map[int64]dnfrepo.SkillState{
			46: {Level: 1, Enabled: true},
			47: {Level: 1, Enabled: true},
		},
		Points: dnfrepo.SkillPointState{
			TotalSP:     100,
			RemainingSP: 60,
			TotalTP:     20,
			RemainingTP: 10,
			SyncedLevel: 10,
		},
		Layouts: map[int]dnfrepo.SkillLayout{},
	}); err != nil {
		t.Fatal(err)
	}
}
