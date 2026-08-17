// 本文件验证 Go 侧职业映射和 C# QuestData/PVF 证据一致。
package jobmap

import "testing"

func TestQuestTagsMatchPVF2315(t *testing.T) {
	want := map[int]string{
		0:  "[swordman]",
		1:  "[fighter]",
		2:  "[gunner]",
		3:  "[mage]",
		4:  "[priest]",
		5:  "[at gunner]",
		6:  "[thief]",
		7:  "[at fighter]",
		8:  "[at mage]",
		9:  "[demonic swordman]",
		10: "[creator mage]",
		11: "[at swordman]",
		12: "[knight]",
		13: "[demonic lancer]",
		14: "[at priest]",
		15: "[gun blader]",
	}
	if MaxID != 15 {
		t.Fatalf("MaxID = %d, want 15", MaxID)
	}
	for job := 0; job <= MaxID; job++ {
		tags := QuestTags(job)
		if len(tags) != 1 || tags[0] != want[job] {
			t.Fatalf("job %d tags = %#v, want %q", job, tags, want[job])
		}
		if !Valid(job) {
			t.Fatalf("job %d should be valid", job)
		}
		if !MatchesQuestTag("prefix "+want[job]+" suffix", job) {
			t.Fatalf("job %d should match %q", job, want[job])
		}
	}
	if Valid(MaxID + 1) {
		t.Fatalf("job %d should be invalid", MaxID+1)
	}
	if MatchesQuestTag("[at swordman]", 9) {
		t.Fatal("job 9 must not use the stale [at swordman] mapping")
	}
	if MatchesQuestTag("[at mage]", 10) {
		t.Fatal("job 10 must not use the stale [at mage] mapping")
	}
}
