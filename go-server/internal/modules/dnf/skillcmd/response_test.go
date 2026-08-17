package skillcmd

import (
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestBuildChangeSkillSlotSuccessMatchesCurrentEXEReader(t *testing.T) {
	body, err := BuildChangeSkillSlotSuccess(SlotMutationResult{SkillTree: 0, From: 3, To: 198})
	if err != nil {
		t.Fatalf("BuildChangeSkillSlotSuccess() error = %v", err)
	}
	if string(body) != string([]byte{1, 0, 3, 198}) {
		t.Fatalf("body = % X", body)
	}
}

func TestBuildChangeAnotherSkillTreeResponsesMatchCurrentEXEReader(t *testing.T) {
	body, err := BuildChangeAnotherSkillTreeSuccess(TreeSwitchMutationResult{Current: 0, Target: 1})
	if err != nil {
		t.Fatalf("BuildChangeAnotherSkillTreeSuccess() error = %v", err)
	}
	if string(body) != string([]byte{1, 1}) {
		t.Fatalf("success body = % X", body)
	}
	if string(BuildChangeAnotherSkillTreeFailure()) != string([]byte{0, 19}) {
		t.Fatalf("failure body = % X", BuildChangeAnotherSkillTreeFailure())
	}
	if _, err := BuildChangeAnotherSkillTreeSuccess(TreeSwitchMutationResult{Current: 1, Target: 1}); !errors.Is(err, ErrSkillTree) {
		t.Fatalf("same-tree success error = %v, want %v", err, ErrSkillTree)
	}
	if _, err := BuildChangeAnotherSkillTreeSuccess(TreeSwitchMutationResult{Current: 2, Target: 1}); !errors.Is(err, ErrSkillTree) {
		t.Fatalf("invalid-current success error = %v, want %v", err, ErrSkillTree)
	}
}

func TestBuildBuySkillSuccessMatchesCurrentEXEReader(t *testing.T) {
	body, err := BuildBuySkillSuccess(MutationResult{
		SkillTree: 0,
		FinalMode: 3,
		Points:    dnfrepo.SkillPointState{RemainingSP: 0x1234, RemainingTP: 0x5678},
		Entries: []MutationEntry{
			{Slot: 6, SkillID: 0x1122, Level: 4},
			{Slot: 54, SkillID: 0x3344, Level: 5, CommandData: []byte{'A', 'B'}},
		},
	})
	if err != nil {
		t.Fatalf("BuildBuySkillSuccess() error = %v", err)
	}
	want := []byte{
		1, 0,
		0x34, 0x12, 0x78, 0x56,
		2,
		6, 0x22, 0x11, 4, 0,
		54, 0x44, 0x33, 5, 1, 2, 'A', 'B',
		3,
	}
	if string(body) != string(want) {
		t.Fatalf("body = % X, want % X", body, want)
	}
}

func TestBuildBuySkillSuccessRejectsUnprovenTree(t *testing.T) {
	_, err := BuildBuySkillSuccess(MutationResult{SkillTree: 1})
	if !errors.Is(err, ErrSkillTree) {
		t.Fatalf("BuildBuySkillSuccess() error = %v, want ErrSkillTree", err)
	}
}

func TestBuildBuySkillSuccessRejectsUnresolvedSlot(t *testing.T) {
	_, err := BuildBuySkillSuccess(MutationResult{
		Points:  dnfrepo.SkillPointState{},
		Entries: []MutationEntry{{Slot: -1, SkillID: 46, Level: 2}},
	})
	if !errors.Is(err, ErrResponseSlot) {
		t.Fatalf("BuildBuySkillSuccess() error = %v, want ErrResponseSlot", err)
	}
}

func TestBuildBuySkillFailure(t *testing.T) {
	if got := BuildBuySkillFailure(0x12); string(got) != string([]byte{0, 0x12}) {
		t.Fatalf("failure body = % X", got)
	}
}

func TestBuildSkillInitSuccessMatchesCurrentEXEReader(t *testing.T) {
	body, err := BuildSkillInitSuccess(ResetMutationResult{SkillTree: 0})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string([]byte{1, 0, 1}) {
		t.Fatalf("body = % X", body)
	}
}
