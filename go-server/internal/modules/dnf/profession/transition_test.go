package profession

import (
	"errors"
	"testing"
)

func TestPlanRewardClassChangeAndAwakenings(t *testing.T) {
	profiles := transitionTestProfiles()
	changeRequest, err := ParseReward(1, "[grow type]", []int64{3})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := profiles.PlanTransition(2, 0, changeRequest)
	if err != nil || changed.Kind != KindClassChange || changed.ChainType != 1 || changed.NewGrowType != 0x03 {
		t.Fatalf("class change = %+v, %v", changed, err)
	}
	firstRequest, err := ParseReward(2, "[awakening type]", []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	first, err := profiles.PlanTransition(2, changed.NewGrowType, firstRequest)
	if err != nil || first.Kind != KindAwakening || first.ChainType != 2 || first.NewGrowType != 0x13 {
		t.Fatalf("first awakening = %+v, %v", first, err)
	}
	secondRequest, err := ParseReward(3, "[awakening type]", []int64{2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := profiles.PlanTransition(2, first.NewGrowType, secondRequest)
	if err != nil || second.NewGrowType != 0x23 || second.GrowNumber != 2 {
		t.Fatalf("second awakening = %+v, %v", second, err)
	}
}

func TestPlanRewardRejectsSkippedOrMixedTransitions(t *testing.T) {
	profiles := transitionTestProfiles()
	changeRequest, _ := ParseReward(1, "[grow type]", []int64{2})
	if _, err := profiles.PlanTransition(2, 0x03, changeRequest); !errors.Is(err, ErrTransitionOutOfStep) {
		t.Fatalf("reclass error = %v", err)
	}
	secondRequest, _ := ParseReward(3, "[awakening type]", []int64{2})
	if _, err := profiles.PlanTransition(2, 0x03, secondRequest); !errors.Is(err, ErrTransitionOutOfStep) {
		t.Fatalf("skipped awakening error = %v", err)
	}
	firstRequest, _ := ParseReward(2, "[awakening type]", []int64{1})
	if _, err := profiles.PlanTransition(2, 0x13, firstRequest); !errors.Is(err, ErrTransitionOutOfStep) {
		t.Fatalf("replayed awakening error = %v", err)
	}
}

func TestPlanRewardUsesPVFBranchZeroAwakeningCapabilities(t *testing.T) {
	profiles := transitionTestProfiles()
	firstRequest, _ := ParseReward(2, "[awakening type]", []int64{1})
	first, err := profiles.PlanTransition(9, 0, firstRequest)
	if err != nil || first.NewGrowType != 0x10 || first.FirstGrowType != 0 || first.AwakeningStage != 1 {
		t.Fatalf("branch-zero first awakening = %+v, %v", first, err)
	}
	secondRequest, _ := ParseReward(3, "[awakening type]", []int64{2})
	second, err := profiles.PlanTransition(9, first.NewGrowType, secondRequest)
	if err != nil || second.NewGrowType != 0x20 || second.AwakeningStage != 2 {
		t.Fatalf("branch-zero second awakening = %+v, %v", second, err)
	}
	if _, err := profiles.PlanTransition(2, 0, firstRequest); !errors.Is(err, ErrTransitionInvalid) {
		t.Fatalf("ordinary job branch-zero awakening error = %v", err)
	}
	if _, err := profiles.PlanTransition(9, 0, secondRequest); !errors.Is(err, ErrTransitionOutOfStep) {
		t.Fatalf("branch-zero skipped awakening error = %v", err)
	}
}

func transitionTestProfiles() *Profiles {
	return &Profiles{jobs: map[byte]jobProfile{
		2: {
			growSupported:      map[byte]bool{3: true},
			awakeningSupported: map[byte]map[byte]bool{3: {1: true, 2: true}},
		},
		9: {
			growSupported:      map[byte]bool{0: true},
			awakeningSupported: map[byte]map[byte]bool{0: {1: true, 2: true}},
		},
	}}
}
