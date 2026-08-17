package dnfbridge

import (
	"context"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
)

func TestCurrentQuestDropMatchMonsterIncludesTypeOneEnemyRewardItem(t *testing.T) {
	ctx := context.Background()
	source := bridgePVFSource{
		dnfquest.DefaultList: "3233 `enemy_reward.qst`\n3234 `monster_reward.qst`\n",
		"n_quest/enemy_reward.qst": "[grade]\n" +
			"`[epic]`\n" +
			"[type]\n" +
			"`[seeking]`\n" +
			"[int data]\n" +
			"10164861 30\n" +
			"[enemy reward item]\n" +
			"420 1 32 -1 10164861 1 100 30\n" +
			"421 1 32 2 10164861 2 50 30\n" +
			"420 2 32 -1 10164862 1 100 30\n" +
			"[/enemy reward item]\n",
		"n_quest/monster_reward.qst": "[grade]\n" +
			"`[normal]`\n" +
			"[type]\n" +
			"`[seeking]`\n" +
			"[int data]\n" +
			"10164863 1\n" +
			"[monster reward item]\n" +
			"420 32 -1 10164863 1 75 1\n" +
			"[/monster reward item]\n",
	}
	index, err := dnfpvf.Build(ctx, source, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(ctx, index)
	if err != nil {
		t.Fatal(err)
	}

	candidates := currentQuestDropMatchMonster(catalog, []int64{3233, 3234}, 32, 0, 420)
	if len(candidates) != 2 {
		t.Fatalf("candidates=%+v", candidates)
	}
	if candidates[0] != (currentQuestDropCandidate{QuestID: 3233, ItemID: 10164861, Count: 1, DropRate: 100, MaxStack: 30}) {
		t.Fatalf("enemy reward candidate=%+v", candidates[0])
	}
	if candidates[1] != (currentQuestDropCandidate{QuestID: 3234, ItemID: 10164863, Count: 1, DropRate: 75, MaxStack: 1}) {
		t.Fatalf("monster reward candidate=%+v", candidates[1])
	}
	if got := currentQuestDropMatchMonster(catalog, []int64{3233}, 32, 0, 421); len(got) != 0 {
		t.Fatalf("difficulty-specific enemy reward matched wrong difficulty: %+v", got)
	}
	if got := currentQuestDropMatchMonster(catalog, []int64{3233}, 32, 2, 421); len(got) != 1 || got[0].Count != 2 || got[0].DropRate != 50 {
		t.Fatalf("difficulty-specific enemy reward=%+v", got)
	}
	if got := currentQuestDropMatchMonster(catalog, []int64{3233}, 31, 0, 420); len(got) != 0 {
		t.Fatalf("enemy reward matched wrong dungeon: %+v", got)
	}
}

func TestCurrentQuestHelperDropRateRaisesOneFifthAndCaps(t *testing.T) {
	tests := []struct {
		base   int64
		active bool
		want   int64
	}{
		{base: 50, active: false, want: 50},
		{base: 50, active: true, want: 60},
		{base: 83, active: true, want: 100},
		{base: 100, active: true, want: 100},
		{base: 0, active: true, want: 0},
	}
	for _, test := range tests {
		if got := currentQuestHelperDropRate(test.base, test.active); got != test.want {
			t.Fatalf("base=%d active=%t got=%d want=%d", test.base, test.active, got, test.want)
		}
	}
}

func TestCurrentQuestItemDropRateCombinesGrowthContractAndQuestHelper(t *testing.T) {
	tests := []struct {
		base          int64
		growthPercent int64
		questHelper   bool
		want          int64
	}{
		{base: 50, growthPercent: 0, questHelper: false, want: 50},
		{base: 50, growthPercent: 20, questHelper: false, want: 60},
		{base: 50, growthPercent: 20, questHelper: true, want: 70},
		{base: 83, growthPercent: 20, questHelper: false, want: 100},
		{base: 99, growthPercent: -1, questHelper: false, want: 99},
		{base: 0, growthPercent: 20, questHelper: true, want: 0},
	}
	for _, test := range tests {
		if got := currentQuestItemDropRate(test.base, test.growthPercent, test.questHelper); got != test.want {
			t.Fatalf(
				"base=%d growth=%d helper=%t got=%d want=%d",
				test.base,
				test.growthPercent,
				test.questHelper,
				got,
				test.want,
			)
		}
	}
}
