package progression

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestPVFTablesDriveExperienceLevelUpsAndSPTP(t *testing.T) {
	tables, err := Load(context.Background(), progressionTestSource())
	if err != nil {
		t.Fatal(err)
	}
	if got := tables.Snapshot(); got.ExperienceThresholds != 4 || got.SkillPointLevels != 5 ||
		got.TechniquePointLevels != 3 || got.QuestExperienceLevels != 5 || got.QuestDifficulties != 2 {
		t.Fatalf("snapshot = %+v", got)
	}

	experience, err := tables.ApplyExperience(1, 90, 180, 5)
	if err != nil {
		t.Fatal(err)
	}
	if experience.NewLevel != 3 || experience.NewExperience != 270 || experience.LevelsGained != 2 || experience.Saturated {
		t.Fatalf("experience result = %+v", experience)
	}

	points := dnfrepo.SkillPointState{
		TotalSP: 40, RemainingSP: 15, TotalTP: 3, RemainingTP: 1, SyncedLevel: 2,
	}
	advance, err := tables.AdvanceSkillPoints(points, experience.NewLevel)
	if err != nil {
		t.Fatal(err)
	}
	if advance.SPGain != 30 || advance.New.TotalSP != 70 || advance.New.RemainingSP != 45 || advance.New.SyncedLevel != 3 {
		t.Fatalf("SP advance = %+v", advance)
	}
	if advance.TPGain != 2 || advance.New.TotalTP != 5 || advance.New.RemainingTP != 3 {
		t.Fatalf("TP advance = %+v", advance)
	}
	if spentBefore, spentAfter := points.TotalSP-points.RemainingSP, advance.New.TotalSP-advance.New.RemainingSP; spentBefore != spentAfter {
		t.Fatalf("spent SP changed: before=%d after=%d", spentBefore, spentAfter)
	}
}

func TestPlanExperienceAndSkillPointsCouplesLevelAndSPTP(t *testing.T) {
	tables, err := Load(context.Background(), progressionTestSource())
	if err != nil {
		t.Fatal(err)
	}
	points := dnfrepo.SkillPointState{TotalSP: 40, RemainingSP: 15, TotalTP: 3, RemainingTP: 1, SyncedLevel: 2}
	plan, err := tables.PlanExperienceAndSkillPoints(2, 240, 300, 5, points)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Experience.NewLevel != 4 || plan.Experience.NewExperience != 540 {
		t.Fatalf("experience = %+v", plan.Experience)
	}
	if plan.SkillPoints.SPGain != 70 || plan.SkillPoints.New.TotalSP != 110 || plan.SkillPoints.New.RemainingSP != 85 || plan.SkillPoints.New.SyncedLevel != 4 {
		t.Fatalf("skill points = %+v", plan.SkillPoints)
	}
	if plan.SkillPoints.TPGain != 3 || plan.SkillPoints.New.TotalTP != 6 || plan.SkillPoints.New.RemainingTP != 4 {
		t.Fatalf("technique points = %+v", plan.SkillPoints)
	}
	if _, err := tables.PlanExperienceAndSkillPoints(2, 240, 300, 5, dnfrepo.SkillPointState{SyncedLevel: 1}); !errors.Is(err, ErrSkillPointLedger) {
		t.Fatalf("stale ledger error = %v", err)
	}
}

func TestApplyExperienceSaturatesCurrentEXEU32(t *testing.T) {
	tables, err := Load(context.Background(), progressionTestSource())
	if err != nil {
		t.Fatal(err)
	}
	result, err := tables.ApplyExperience(4, math.MaxUint32-3, 10, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.NewExperience != math.MaxUint32 || result.NewLevel != 5 || !result.Saturated {
		t.Fatalf("result = %+v", result)
	}
}

func TestProgressionRejectsMissingSPLevelInsteadOfUnderpaying(t *testing.T) {
	source := progressionTestSource()
	source[SkillPointTablePath] = "[sp table]\n1 10\n3 30\n[/sp table]\n[tp table]\n3 1\n[/tp table]\n"
	tables, err := Load(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tables.TotalSkillPoints(3); !errors.Is(err, ErrTableMalformed) {
		t.Fatalf("TotalSkillPoints error = %v, want ErrTableMalformed", err)
	}
	points := dnfrepo.SkillPointState{TotalSP: 10, RemainingSP: 10, SyncedLevel: 1}
	if _, err := tables.AdvanceSkillPoints(points, 3); !errors.Is(err, ErrTableMalformed) {
		t.Fatalf("AdvanceSkillPoints error = %v, want ErrTableMalformed", err)
	}
}

func TestQuestExperienceUsesPVFDifficultyAndPenalty(t *testing.T) {
	tables, err := Load(context.Background(), progressionTestSource())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		playerLevel int
		questLevel  int
		difficulty  rune
		ignore      bool
		want        uint32
	}{
		{name: "same level", playerLevel: 1, questLevel: 1, difficulty: 'N', want: 100},
		{name: "green penalty", playerLevel: 8, questLevel: 1, difficulty: 'N', want: 80},
		{name: "grey penalty", playerLevel: 13, questLevel: 1, difficulty: 'E', want: 60},
		{name: "ignore level uses player row", playerLevel: 5, questLevel: 1, difficulty: 'E', ignore: true, want: 1000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := tables.QuestExperience(test.playerLevel, test.questLevel, test.difficulty, test.ignore)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("QuestExperience = %d, want %d", got, test.want)
			}
		})
	}
	if _, err := tables.QuestExperience(1, 1, 'X', false); !errors.Is(err, ErrQuestDifficulty) {
		t.Fatalf("unknown difficulty error = %v", err)
	}
}

func TestQuestGoldUsesPVFBaseMultiplierBeforeIntegerDivision(t *testing.T) {
	tables, err := Load(context.Background(), progressionTestSource())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		playerLevel  int
		questLevel   int
		goldMultiple int
		ignore       bool
		want         uint32
	}{
		{name: "default hundred percent keeps low base", playerLevel: 1, questLevel: 1, want: 10},
		{name: "explicit hundred fifty percent", playerLevel: 1, questLevel: 1, goldMultiple: 150, want: 15},
		{name: "green penalty applies after multiplier", playerLevel: 8, questLevel: 1, want: 8},
		{name: "ignore level uses player row", playerLevel: 1, questLevel: 1, goldMultiple: 150, ignore: true, want: 15},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := tables.QuestGold(test.playerLevel, test.questLevel, test.goldMultiple, test.ignore)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("QuestGold = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRealScriptPVFProgressionTables(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify real progression tables")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Load(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tables.Snapshot()
	if snapshot.ExperienceThresholds < 90 || snapshot.SkillPointLevels < 90 || snapshot.TechniquePointLevels < 41 ||
		snapshot.QuestExperienceLevels < 90 || snapshot.QuestDifficulties == 0 {
		t.Fatalf("real progression snapshot = %+v", snapshot)
	}
	thresholds := []uint32{1000, 2035, 3315}
	for level, want := range thresholds {
		got, err := tables.ThresholdToNext(level + 1)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("real threshold level %d = %d, want %d", level+1, got, want)
		}
	}
	if value, ok := tables.SkillPointsAtLevel(1); !ok || value != 0 {
		t.Fatalf("real level-1 SP = %d,%t, want 0,true", value, ok)
	}
	if value, ok := tables.SkillPointsAtLevel(2); !ok || value != 30 {
		t.Fatalf("real level-2 SP = %d,%t, want 30,true", value, ok)
	}
	advance, err := tables.AdvanceSkillPoints(dnfrepo.SkillPointState{SyncedLevel: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if advance.SPGain != 30 || advance.New.TotalSP != 30 || advance.New.RemainingSP != 30 {
		t.Fatalf("real SP advance = %+v", advance)
	}
	if totalTP, err := tables.TotalTechniquePoints(90); err != nil || totalTP != 41 {
		t.Fatalf("real level-90 TP total=%d err=%v, want 41", totalTP, err)
	}
	t.Logf("real progression snapshot=%+v level2_threshold=%d level2_sp=%d", snapshot, thresholds[0], advance.SPGain)
}

type progressionMemorySource map[string]string

func (s progressionMemorySource) ReadText(path string) (string, error) {
	value, ok := s[path]
	if !ok {
		return "", fmt.Errorf("%w: %s", platformpvf.ErrFileNotFound, path)
	}
	return value, nil
}

func progressionTestSource() progressionMemorySource {
	return progressionMemorySource{
		ExperienceTablePath: "100 250 500 900\n",
		SkillPointTablePath: "[sp table]\n1 10\n2 30\n3 30\n4 40\n5 50\n[/sp table]\n[tp table]\n3 2\n4 1\n5 1\n[/tp table]\n",
		QuestParameterPath: `[difficulty]
` + "`N` 100\n`E` 200\n" + `[/difficulty]
[exp reward table]
100 -1
200 -1
300 -1
400 -1
500 -1
[gold reward table]
10 -1
[green level penalty]
80
[grey level penalty]
30
`,
	}
}
