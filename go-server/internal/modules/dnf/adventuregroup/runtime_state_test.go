package adventuregroup

import (
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestRuntimeStateMonthlyResetAccrualPurchaseAndCapsule(t *testing.T) {
	config := RuntimeConfig{
		ShopCategories: []ShopCategory{
			{
				Index:              ShopPointBrave,
				MaxPoint:           250,
				ExperiencePerPoint: 15_000_000,
				PointPerExperience: 1,
				Products: []ShopProduct{{
					ItemID: 10, Cost: 1, MonthlyPurchaseLimit: 2,
				}},
			},
			{Index: ShopPointGlory, MaxPoint: 9999, ResetPointMonthly: true},
			{Index: ShopPointPure},
		},
		Capsule: CapsuleConfig{
			MinimumExperience: 10,
			MaximumExperience: 100,
			MaximumCount:      10,
			MinimumLevel:      50,
			MaximumLevel:      86,
			GrantedExperience: 1,
		},
	}
	account := dnfrepo.AccountRecord{
		AccountID: "dnf:1",
		Metadata: map[string]string{
			RuntimeStateMetadataKey: `{"version":1,"shop_points":[7,80,0],"month":"2026-06","purchases":{"0:10":2}}`,
		},
	}
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	state, err := ParseRuntimeState(account, config, now)
	if err != nil {
		t.Fatal(err)
	}
	if state.ShopPoints != [ShopPointTypeCount]uint32{7, 0, 0} || len(state.Purchases) != 0 {
		t.Fatalf("monthly normalized state=%+v", state)
	}
	state.AddMaximumLevelExperience(config, 30_000_005)
	if state.ShopPoints[ShopPointBrave] != 9 || state.BraveExperience != 5 || state.GrowthExperience != 100 {
		t.Fatalf("maximum-level accrual=%+v", state)
	}
	category, _ := config.Shop(ShopPointBrave)
	if err := state.Spend(category, category.Products[0]); err != nil {
		t.Fatal(err)
	}
	if state.ShopPoints[ShopPointBrave] != 8 || state.PurchaseCount(ShopPointBrave, 10) != 1 {
		t.Fatalf("purchase state=%+v", state)
	}
	if !state.ConsumeCapsule(config) || state.GrowthExperience != 90 {
		t.Fatalf("capsule state=%+v", state)
	}
}

func TestRuntimeConfigExpeditionRewardUsesRotatedPVFAttributes(t *testing.T) {
	config := RuntimeConfig{
		ExpeditionAreas: []ExpeditionArea{{
			Index: 1,
			RewardRates: map[uint32]float64{
				6 * 60 * 60: 2,
			},
			DesignatedGroups: []string{"[group A]"},
		}},
		AttributeIDs: map[string]byte{
			"[a]": 1,
		},
		AttributeGroups: map[string][]string{
			"[group a]": {"[a]"},
		},
		CharacterAttributes: map[[2]byte][]string{
			{1, 2}: {"[a]"},
		},
		AttributeRewardRates: []float64{1, 1.5},
		RotationDays:         1,
	}
	reward, ok := config.ExpeditionReward(
		1,
		6*60*60,
		[]ExpeditionMemberInput{{Level: 90, Job: 1, GrowType: 2}},
		time.Unix(1_700_000_000, 0),
	)
	if !ok || reward != 270 {
		t.Fatalf("reward=%d ok=%v", reward, ok)
	}
}
