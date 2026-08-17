package packageitem

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func staticMagicBoxResolver(resolution alignedcmd.MagicBoxResolution) alignedcmd.MagicBoxResolver {
	return func(boxItemID int64) (alignedcmd.MagicBoxResolution, error) {
		return resolution, nil
	}
}

func staticMagicBoxRewardResolver(items map[int64]alignedcmd.MagicBoxRewardItem) alignedcmd.MagicBoxRewardItemResolver {
	return func(itemID int64) (alignedcmd.MagicBoxRewardItem, error) {
		return items[itemID], nil
	}
}

func magicBoxTestCommand(boxSlot int16, materialSlot int16) MagicBoxCommand {
	return MagicBoxCommand{
		SelectedCharacterID: 77,
		RawListType:         0,
		ListType:            listTypeMain,
		SlotIndex:           boxSlot,
		MaterialSlotIndex:   materialSlot,
	}
}

func magicBoxTestResolution() alignedcmd.MagicBoxResolution {
	return alignedcmd.MagicBoxResolution{
		Kind: "random",
		Groups: []alignedcmd.MagicBoxRewardGroup{
			{DrawCount: 1, Entries: []alignedcmd.MagicBoxRewardEntry{{ItemID: 2600014, Weight: 100, Count: 2}}},
			{DrawCount: 1, Entries: []alignedcmd.MagicBoxRewardEntry{{ItemID: 2682272, Weight: 100, Count: 1}}},
		},
		MaterialItemID:      10007367,
		MaterialCountPerUse: 1,
		BoxPVFPath:          "stackable/ect/chn_random/chn_amazingbox_10007368.stk",
	}
}

func magicBoxTestRewardItems() map[int64]alignedcmd.MagicBoxRewardItem {
	return map[int64]alignedcmd.MagicBoxRewardItem{
		2600014: {ItemID: 2600014, Kind: "stackable", StackLimit: 0, SlotStart: 65, SlotEnd: 120, PVFPath: "stackable/professional/potion/ptn_instantmovement.stk"},
		2682272: {ItemID: 2682272, Kind: "stackable", StackLimit: 1, SlotStart: 65, SlotEnd: 120, Seal: true, PVFPath: "stackable/ect/chn_random/chn_blessed_box.stk"},
	}
}

func saveMagicBoxFixture(t *testing.T, ctx context.Context, repos dnfrepo.Group, slots map[string]dnfrepo.ItemStack) {
	t.Helper()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "77", Slots: slots}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
}

func magicBoxBaseSlots() map[string]dnfrepo.ItemStack {
	boxRaw := make([]byte, currentMagicBoxEntrySize)
	boxRaw[0x06] = 1
	hammerRaw := make([]byte, currentMagicBoxEntrySize)
	hammerRaw[0x06] = 3
	return map[string]dnfrepo.ItemStack{
		"0:10":  {ItemID: 10007368, Count: 1, RawEntry: boxRaw, Extra: map[string]string{"item_kind": "stackable"}},
		"0:121": {ItemID: 10007367, Count: 3, RawEntry: hammerRaw, Extra: map[string]string{"item_kind": "stackable"}},
	}
}

func TestOwnerApplyMagicBoxConsumesAndGrants(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveMagicBoxFixture(t, ctx, repos, magicBoxBaseSlots())
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyMagicBox(ctx, magicBoxTestCommand(10, 121), staticMagicBoxResolver(magicBoxTestResolution()), staticMagicBoxRewardResolver(magicBoxTestRewardItems()))
	if err != nil {
		t.Fatalf("ApplyMagicBox error = %v", err)
	}
	if !result.Success || !result.Changed || len(result.Rewards) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.Rewards[0].ItemID != 2600014 || result.Rewards[0].Count != 2 || result.Rewards[1].ItemID != 2682272 || result.Rewards[1].Count != 1 {
		t.Fatalf("rewards = %+v", result.Rewards)
	}
	loaded, ok, err := repos.Inventory.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("load inventory ok=%t err=%v", ok, err)
	}
	if _, found := loaded.Slots["0:10"]; found {
		t.Fatalf("consumed box slot should be deleted")
	}
	hammer := loaded.Slots["0:121"]
	if hammer.Count != 2 || hammer.RawEntry[0x06] != 2 {
		t.Fatalf("hammer = %+v, want count 2 with raw sync", hammer)
	}
	potion, found := loaded.Slots["0:65"]
	if !found || potion.ItemID != 2600014 || potion.Count != 2 {
		t.Fatalf("potion grant = %+v found=%t", potion, found)
	}
	seria, found := loaded.Slots["0:66"]
	if !found || seria.ItemID != 2682272 || seria.Count != 1 || seria.Extra["seal_flag"] != "1" {
		t.Fatalf("seria grant = %+v found=%t", seria, found)
	}
}

func TestOwnerApplyMagicBoxRoutesCreatureRewardToPetInventory(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveMagicBoxFixture(t, ctx, repos, magicBoxBaseSlots())
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	resolution := alignedcmd.MagicBoxResolution{
		Kind: "random",
		Groups: []alignedcmd.MagicBoxRewardGroup{{
			DrawCount: 1,
			Entries:   []alignedcmd.MagicBoxRewardEntry{{ItemID: 702, Weight: 100, Count: 1}},
		}},
		MaterialItemID:      10007367,
		MaterialCountPerUse: 1,
	}
	reward := alignedcmd.MagicBoxRewardItem{
		ItemID:         702,
		Kind:           "equipment",
		TargetListType: 7,
		EquipmentType:  "[creature]",
		StackLimit:     1,
		SlotStart:      0,
		SlotEnd:        139,
		PVFPath:        "equipment/creature/pet.equ",
	}
	result, err := owner.ApplyMagicBox(
		ctx,
		magicBoxTestCommand(10, 121),
		staticMagicBoxResolver(resolution),
		staticMagicBoxRewardResolver(map[int64]alignedcmd.MagicBoxRewardItem{702: reward}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || len(result.Rewards) != 1 || result.Rewards[0].Slot != 0 {
		t.Fatalf("result=%+v", result)
	}
	loaded, found, err := repos.Inventory.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	pet := loaded.Slots["7:0"]
	if pet.ItemID != 702 || pet.Count != 1 || pet.Extra["equipment_type"] != "[creature]" {
		t.Fatalf("pet reward=%+v", pet)
	}
	for key, stack := range loaded.Slots {
		if key != "7:0" && stack.ItemID == 702 {
			t.Fatalf("creature reward leaked into ordinary inventory key=%s stack=%+v", key, stack)
		}
	}
}

func TestOwnerApplyMagicBoxMergesExistingStacks(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	slots := magicBoxBaseSlots()
	slots["0:4"] = dnfrepo.ItemStack{ItemID: 2600014, Count: 1, Extra: map[string]string{"item_kind": "stackable"}}
	saveMagicBoxFixture(t, ctx, repos, slots)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyMagicBox(ctx, magicBoxTestCommand(10, 121), staticMagicBoxResolver(magicBoxTestResolution()), staticMagicBoxRewardResolver(magicBoxTestRewardItems()))
	if err != nil {
		t.Fatalf("ApplyMagicBox error = %v", err)
	}
	if !result.Success {
		t.Fatalf("result = %+v", result)
	}
	loaded, _, _ := repos.Inventory.Load(ctx, "77")
	quick := loaded.Slots["0:4"]
	if quick.Count != 3 {
		t.Fatalf("quick-slot stack = %+v, want merged count 3", quick)
	}
}

func TestOwnerApplyMagicBoxTimedRewardDoesNotMergeLegacyExpireZero(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	slots := magicBoxBaseSlots()
	slots["0:65"] = dnfrepo.ItemStack{ItemID: 2600014, Count: 1, Extra: map[string]string{"item_kind": "stackable"}}
	saveMagicBoxFixture(t, ctx, repos, slots)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	expireAt := time.Unix(1_900_000_000, 0).UTC()
	resolution := alignedcmd.MagicBoxResolution{
		Kind: "random",
		Groups: []alignedcmd.MagicBoxRewardGroup{{
			DrawCount: 1,
			Entries:   []alignedcmd.MagicBoxRewardEntry{{ItemID: 2600014, Weight: 100, Count: 2}},
		}},
		MaterialItemID:      10007367,
		MaterialCountPerUse: 1,
	}
	rewards := map[int64]alignedcmd.MagicBoxRewardItem{
		2600014: {
			ItemID:           2600014,
			Kind:             "stackable",
			StackLimit:       100,
			SlotStart:        65,
			SlotEnd:          120,
			ExpireAt:         expireAt,
			UsablePeriodDays: 30,
		},
	}
	result, err := owner.ApplyMagicBox(ctx, magicBoxTestCommand(10, 121), staticMagicBoxResolver(resolution), staticMagicBoxRewardResolver(rewards))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("result=%+v", result)
	}
	loaded, found, err := repos.Inventory.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	legacy := loaded.Slots["0:65"]
	if legacy.Count != 1 || magicBoxStackExpireUnix(legacy) != 0 {
		t.Fatalf("legacy stack was mutated: %+v", legacy)
	}
	timed, found := loaded.Slots["0:66"]
	wantUnix := strconv.FormatInt(expireAt.Unix(), 10)
	if !found || timed.ItemID != 2600014 || timed.Count != 2 || !timed.ExpireAt.Equal(expireAt) ||
		timed.Extra["expire_time"] != wantUnix || timed.Extra["expire_unix"] != wantUnix ||
		timed.Extra["usable_period_days"] != "30" || timed.Extra["expiration_source"] != "runtime_pvf_usable_period_grant" {
		t.Fatalf("timed reward=%+v found=%t", timed, found)
	}
}

func TestOwnerApplyMagicBoxSplitsByStackLimit(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	slots := magicBoxBaseSlots()
	slots["0:65"] = dnfrepo.ItemStack{ItemID: 2600014, Count: 2, Extra: map[string]string{"item_kind": "stackable"}}
	saveMagicBoxFixture(t, ctx, repos, slots)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	items := magicBoxTestRewardItems()
	limited := items[2600014]
	limited.StackLimit = 3
	items[2600014] = limited
	result, err := owner.ApplyMagicBox(ctx, magicBoxTestCommand(10, 121), staticMagicBoxResolver(magicBoxTestResolution()), staticMagicBoxRewardResolver(items))
	if err != nil {
		t.Fatalf("ApplyMagicBox error = %v", err)
	}
	if !result.Success {
		t.Fatalf("result = %+v", result)
	}
	loaded, _, _ := repos.Inventory.Load(ctx, "77")
	first := loaded.Slots["0:65"]
	second := loaded.Slots["0:66"]
	if first.Count != 3 || second.ItemID != 2600014 || second.Count != 1 {
		t.Fatalf("split grant first=%+v second=%+v", first, second)
	}
}

func TestOwnerApplyMagicBoxRoutesMainInventoryOverflowToSystemMail(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	slots := magicBoxBaseSlots()
	slots["0:65"] = dnfrepo.ItemStack{ItemID: 9999, Count: 1}
	saveMagicBoxFixture(t, ctx, repos, slots)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	resolution := alignedcmd.MagicBoxResolution{
		Kind: "random",
		Groups: []alignedcmd.MagicBoxRewardGroup{{
			DrawCount: 1,
			Entries:   []alignedcmd.MagicBoxRewardEntry{{ItemID: 2600014, Weight: 100, Count: 2}},
		}},
		MaterialItemID:      10007367,
		MaterialCountPerUse: 1,
	}
	rewards := map[int64]alignedcmd.MagicBoxRewardItem{
		2600014: {ItemID: 2600014, Kind: "stackable", SlotStart: 65, SlotEnd: 65, PVFPath: "stackable/test/reward.stk"},
	}
	result, err := owner.ApplyMagicBox(ctx, magicBoxTestCommand(10, 121), staticMagicBoxResolver(resolution), staticMagicBoxRewardResolver(rewards))
	if err != nil {
		t.Fatalf("ApplyMagicBox: %v", err)
	}
	if !result.Success || result.OverflowMailID == "" || len(result.Rewards) != 0 {
		t.Fatalf("result = %+v", result)
	}
	loaded, _, err := repos.Inventory.Load(ctx, "77")
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if _, found := loaded.Slots["0:10"]; found || loaded.Slots["0:121"].Count != 2 {
		t.Fatalf("source consumption did not commit with overflow: %+v", loaded.Slots)
	}
	if stack := loaded.Slots["0:65"]; stack.ItemID != 9999 || stack.Count != 1 {
		t.Fatalf("full target slot changed: %+v", stack)
	}
	mailbox, found, err := repos.Mailbox.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load mailbox found=%t err=%v", found, err)
	}
	mail := mailbox.Mails[result.OverflowMailID]
	if len(mail.Attachments) != 1 || mail.Attachments[0].ItemID != 2600014 || mail.Attachments[0].Count != 2 ||
		mail.Attachments[0].Extra["pvf_path"] != "stackable/test/reward.stk" {
		t.Fatalf("overflow mail = %+v", mail)
	}
	if mail.SenderName != "系统" || mail.Title != "背包已满：礼盒奖励" ||
		mail.Body != "背包空间不足，礼盒奖励已通过邮件发送。请清理对应道具分页后领取。" {
		t.Fatalf("overflow mail prompt = %+v", mail)
	}
}

func TestOwnerApplyMagicBoxRejections(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		cmd      MagicBoxCommand
		resolver alignedcmd.MagicBoxResolution
		setup    func(slots map[string]dnfrepo.ItemStack)
	}{
		{
			name:     "box slot missing",
			cmd:      magicBoxTestCommand(99, 121),
			resolver: magicBoxTestResolution(),
		},
		{
			name:     "material missing",
			cmd:      magicBoxTestCommand(10, 121),
			resolver: magicBoxTestResolution(),
			setup: func(slots map[string]dnfrepo.ItemStack) {
				delete(slots, "0:121")
			},
		},
		{
			name: "not an openable box",
			cmd:  magicBoxTestCommand(10, 121),
			resolver: alignedcmd.MagicBoxResolution{
				Kind:       "",
				BoxPVFPath: "stackable/other/plain.stk",
			},
		},
		{
			name: "empty reward pool",
			cmd:  magicBoxTestCommand(10, 121),
			resolver: alignedcmd.MagicBoxResolution{
				Kind:                "random",
				MaterialItemID:      10007367,
				MaterialCountPerUse: 1,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repos := dnfrepomemory.NewMemoryGroup()
			slots := magicBoxBaseSlots()
			if tc.setup != nil {
				tc.setup(slots)
			}
			saveMagicBoxFixture(t, ctx, repos, slots)
			owner, err := NewOwner(repos)
			if err != nil {
				t.Fatal(err)
			}
			result, err := owner.ApplyMagicBox(ctx, tc.cmd, staticMagicBoxResolver(tc.resolver), staticMagicBoxRewardResolver(magicBoxTestRewardItems()))
			if err != nil {
				t.Fatalf("ApplyMagicBox error = %v", err)
			}
			if result.Success || result.Changed {
				t.Fatalf("result = %+v, want rejected without mutation", result)
			}
			if result.Reason == "" {
				t.Fatalf("rejection must carry a reason")
			}
		})
	}
}

func TestOwnerApplyMagicBoxMaterialSlotFallback(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	slots := magicBoxBaseSlots()
	saveMagicBoxFixture(t, ctx, repos, slots)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	// Material slot points at the box itself; the owner must fall back to an
	// id-based scan, matching the 86JP slot-mismatch fallback.
	result, err := owner.ApplyMagicBox(ctx, magicBoxTestCommand(10, 10), staticMagicBoxResolver(magicBoxTestResolution()), staticMagicBoxRewardResolver(magicBoxTestRewardItems()))
	if err != nil {
		t.Fatalf("ApplyMagicBox error = %v", err)
	}
	if !result.Success || result.MaterialSlotIndex != 121 {
		t.Fatalf("result = %+v, want fallback material slot 121", result)
	}
}

func TestOwnerApplyMagicBoxNilResolversFailClosed(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ApplyMagicBox(ctx, magicBoxTestCommand(10, 121), nil, nil); !errors.Is(err, ErrMagicBoxResolverRequired) {
		t.Fatalf("ApplyMagicBox error = %v, want ErrMagicBoxResolverRequired", err)
	}
}
