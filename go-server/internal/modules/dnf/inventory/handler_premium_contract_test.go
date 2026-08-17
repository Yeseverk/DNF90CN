package inventory

import (
	"bytes"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/premium"
)

func TestUseStackableCrystalActivationRequestsNativeStateRefresh(t *testing.T) {
	result := ownerAppliedUseStackableResult(44, Command{Operation: "use_stackable"}, UseStackableResult{
		PremiumActivated:        true,
		PremiumType:             premium.TypeCrystal,
		PremiumRemainingSeconds: 7 * 24 * 60 * 60,
		ItemID:                  10000389,
		Changed:                 true,
	})
	if len(result.PostActions) != 1 ||
		result.PostActions[0] != alignedcmd.PostActionRefreshCrystalContractState {
		t.Fatalf("crystal post actions = %v", result.PostActions)
	}
}

func TestUseStackableOverEquipAndOverSkillDoNotEmitCrystalState(t *testing.T) {
	for _, premiumType := range []int64{premium.TypeOverEquip, premium.TypeOverSkill} {
		result := ownerAppliedUseStackableResult(44, Command{Operation: "use_stackable"}, UseStackableResult{
			PremiumActivated:        true,
			PremiumType:             premiumType,
			PremiumRemainingSeconds: 7 * 24 * 60 * 60,
			ItemID:                  30,
			Changed:                 true,
		})
		if len(result.PostActions) != 0 {
			t.Fatalf("premium type %d post actions = %v, want none", premiumType, result.PostActions)
		}
	}
}

func TestUseStackableCouponContractsNotifyAndRefreshTheActiveCount(t *testing.T) {
	// Runtime premiumlist_new.etc maps Black Diamond to type 29. The other
	// three values are the visible Overlord, Expert, and Growth entries.
	for _, premiumType := range []int64{
		29,
		premium.TypeOverEquip,
		premium.TypeOverSkill,
		premium.TypeBonusExp,
	} {
		remaining := int64(3 * 24 * 60 * 60)
		result := ownerAppliedUseStackableResult(44, Command{Operation: "use_stackable"}, UseStackableResult{
			PremiumActivated:        true,
			PremiumType:             premiumType,
			PremiumRemainingSeconds: remaining,
			ItemID:                  31,
			ListType:                0,
			SlotIndex:               69,
			Changed:                 true,
		})
		if len(result.UpperResponses) != 2 {
			t.Fatalf("premium type %d response count = %d, want op44 ACK + op66", premiumType, len(result.UpperResponses))
		}
		if len(result.ItemSlotRefreshes) != 1 ||
			result.ItemSlotRefreshes[0] != (alignedcmd.ItemSlotRefresh{ListType: 0, SlotIndex: 69}) {
			t.Fatalf("premium type %d item refreshes = %v, want one-row list0/slot69 op14", premiumType, result.ItemSlotRefreshes)
		}
		noti := result.UpperResponses[1]
		if noti.Classification != 0 || noti.MsgID != premiumActivatedNotifyMsgID ||
			!bytes.Equal(noti.Body, premium.BuildActivatedBody(premiumType, remaining)) {
			t.Fatalf("premium type %d activation notification = %+v, want class0/op66", premiumType, noti)
		}
		if len(result.PostActions) != 0 {
			t.Fatalf("premium type %d post actions = %v, want no crystal action", premiumType, result.PostActions)
		}
	}
}
