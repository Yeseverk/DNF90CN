package packageitem

import (
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

// magicBoxExpandCapturedBody 是 2026-07-25 在当前 NoPack 客户端上实测抓到
// 的 0x0468 请求体：主背包 70 号槽的 10007368（泰迪礼盒）连开 100 次，材料
// 槽 151 的 10007367（幸运魔锤）。
var magicBoxExpandCapturedBody = []byte{
	0x00,
	0x46, 0x00,
	0x48, 0xB3, 0x98, 0x00,
	0x97, 0x00,
	0x47, 0xB3, 0x98, 0x00,
	0x64, 0x00,
}

func TestDecodeMagicBoxExpandRequestMatchesCapturedClientBody(t *testing.T) {
	req, err := DecodeMagicBoxExpandRequest(magicBoxExpandCapturedBody)
	if err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if req.ListType != listTypeMain || req.SlotIndex != 70 || req.BoxItemID != 10007368 ||
		req.MaterialSlotIndex != 151 || req.MaterialItemID != 10007367 || req.OpenCount != 100 {
		t.Fatalf("request = %+v", req)
	}
}

func TestDecodeMagicBoxExpandRequestRejectsMalformed(t *testing.T) {
	for name, body := range map[string][]byte{
		"short":      {0, 1, 0},
		"bad list":   {9, 0x46, 0, 0x48, 0xB3, 0x98, 0, 0x97, 0, 0x47, 0xB3, 0x98, 0, 0x64, 0},
		"zero box":   {0, 0x46, 0, 0, 0, 0, 0, 0x97, 0, 0x47, 0xB3, 0x98, 0, 0x64, 0},
		"zero count": {0, 0x46, 0, 0x48, 0xB3, 0x98, 0, 0x97, 0, 0x47, 0xB3, 0x98, 0, 0, 0},
	} {
		if _, err := DecodeMagicBoxExpandRequest(body); err == nil {
			t.Fatalf("%s: malformed body accepted", name)
		}
	}
}

func magicBoxExpandTestCommand(boxSlot int16, materialSlot int16, count uint16) MagicBoxExpandCommand {
	return MagicBoxExpandCommand{
		SelectedCharacterID: 77,
		RawListType:         0,
		ListType:            listTypeMain,
		SlotIndex:           boxSlot,
		BoxItemID:           10007368,
		MaterialSlotIndex:   materialSlot,
		MaterialItemID:      10007367,
		OpenCount:           count,
	}
}

func magicBoxExpandBaseSlots() map[string]dnfrepo.ItemStack {
	slots := magicBoxBaseSlots()
	box := slots["0:10"]
	box.Count = 3
	slots["0:10"] = box
	hammer := slots["0:121"]
	hammer.Count = 5
	slots["0:121"] = hammer
	return slots
}

func TestOwnerApplyMagicBoxExpandConsumesRequestedOpens(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveMagicBoxFixture(t, ctx, repos, magicBoxExpandBaseSlots())
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyMagicBoxExpand(ctx, magicBoxExpandTestCommand(10, 121, 2), staticMagicBoxResolver(magicBoxTestResolution()), staticMagicBoxRewardResolver(magicBoxTestRewardItems()))
	if err != nil {
		t.Fatalf("ApplyMagicBoxExpand error = %v", err)
	}
	if !result.Success || result.OpenCount != 2 || len(result.Rewards) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.Rewards[0].ItemID != 2600014 || result.Rewards[0].Count != 4 || result.Rewards[1].ItemID != 2682272 || result.Rewards[1].Count != 2 {
		t.Fatalf("rewards = %+v", result.Rewards)
	}
	loaded, _, _ := repos.Inventory.Load(ctx, "77")
	if box := loaded.Slots["0:10"]; box.Count != 1 {
		t.Fatalf("box = %+v, want 1 left", box)
	}
	if hammer := loaded.Slots["0:121"]; hammer.Count != 3 {
		t.Fatalf("hammer = %+v, want 3 left", hammer)
	}
}

func TestOwnerApplyMagicBoxExpandConvergesToAvailableMaterials(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveMagicBoxFixture(t, ctx, repos, magicBoxExpandBaseSlots()) // 3 boxes, 5 hammers -> cap 3
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyMagicBoxExpand(ctx, magicBoxExpandTestCommand(10, 121, 100), staticMagicBoxResolver(magicBoxTestResolution()), staticMagicBoxRewardResolver(magicBoxTestRewardItems()))
	if err != nil {
		t.Fatalf("ApplyMagicBoxExpand error = %v", err)
	}
	if !result.Success || result.OpenCount != 3 {
		t.Fatalf("result = %+v, want converged opens 3", result)
	}
	loaded, _, _ := repos.Inventory.Load(ctx, "77")
	if _, found := loaded.Slots["0:10"]; found {
		t.Fatalf("box slot should be consumed")
	}
	if hammer := loaded.Slots["0:121"]; hammer.Count != 2 {
		t.Fatalf("hammer = %+v, want 2 left", hammer)
	}
}

func TestOwnerApplyMagicBoxExpandRejectsItemMismatch(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveMagicBoxFixture(t, ctx, repos, magicBoxExpandBaseSlots())
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	cmd := magicBoxExpandTestCommand(10, 121, 1)
	cmd.BoxItemID = 999999
	result, err := owner.ApplyMagicBoxExpand(ctx, cmd, staticMagicBoxResolver(magicBoxTestResolution()), staticMagicBoxRewardResolver(magicBoxTestRewardItems()))
	if err != nil {
		t.Fatalf("ApplyMagicBoxExpand error = %v", err)
	}
	if result.Success || result.Changed || result.Reason == "" {
		t.Fatalf("result = %+v, want rejected without mutation", result)
	}
	loaded, _, _ := repos.Inventory.Load(ctx, "77")
	if box := loaded.Slots["0:10"]; box.Count != 3 {
		t.Fatalf("box mutated: %+v", box)
	}
}

func handleMagicBoxExpandRequest(t *testing.T, repos dnfrepo.Group, body []byte) alignedcmd.Result {
	t.Helper()
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:                     uint16(dnfenum.CmdPacketUseRandomboxItemExpand),
		Body:                       body,
		AccountID:                  "acc",
		SelectedCharacterID:        77,
		Repositories:               repos,
		MagicBoxResolver:           staticMagicBoxResolver(magicBoxTestResolution()),
		MagicBoxRewardItemResolver: staticMagicBoxRewardResolver(magicBoxTestRewardItems()),
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	return got
}

func TestHandlerMagicBoxExpandSendsAggregatedAckThenRefresh(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	slots := magicBoxBaseSlots()
	slots["0:70"] = dnfrepo.ItemStack{ItemID: 10007368, Count: 1, Extra: map[string]string{"item_kind": "stackable"}}
	slots["0:151"] = dnfrepo.ItemStack{ItemID: 10007367, Count: 1, Extra: map[string]string{"item_kind": "stackable"}}
	delete(slots, "0:10")
	delete(slots, "0:121")
	saveMagicBoxFixture(t, ctx, repos, slots)

	body := make([]byte, 0, 15)
	body = append(body, 0x00, 0x46, 0x00, 0x48, 0xB3, 0x98, 0x00, 0x97, 0x00, 0x47, 0xB3, 0x98, 0x00, 0x64, 0x00)
	got := handleMagicBoxExpandRequest(t, repos, body)
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	display := got.UpperResponses[0]
	if display.MsgID != uint16(dnfenum.CmdPacketUseRandomboxItem) {
		t.Fatalf("display msg = 0x%04X, want 0x00D0", display.MsgID)
	}
	if len(got.PostActions) == 0 {
		t.Fatalf("expected container refresh post action")
	}
}

func TestHandlerMagicBoxExpandFailureAcksZeroByte(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveMagicBoxFixture(t, ctx, repos, magicBoxBaseSlots()) // captured slots 70/151 empty here
	got := handleMagicBoxExpandRequest(t, repos, magicBoxExpandCapturedBody)
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	if string(got.UpperResponses[0].Body) != string([]byte{0x00}) {
		t.Fatalf("failure ack = %x, want {00}", got.UpperResponses[0].Body)
	}
}

// TestHandlerMagicBoxExpandSeriaBatchAckMatchesCurrentEXELayout 锁定赛丽亚源
// 连开的原生批量回包（结构以当前 EXE sub_1D074C0 实读顺序为准）：variant=4
// 跳过材料校验，doubleFlag 反映翻倍，主列表 + u16 0 + 双倍列表，0x77 行。
// 幸运值从 0 开 10 个时第 9 轮必触发一次翻倍（86JP max=8）。
func TestHandlerMagicBoxExpandSeriaBatchAckMatchesCurrentEXELayout(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	slots := map[string]dnfrepo.ItemStack{
		"0:67": {ItemID: 2682272, Count: 10, Extra: map[string]string{"item_kind": "stackable"}},
	}
	saveMagicBoxFixture(t, ctx, repos, slots)
	resolution := alignedcmd.MagicBoxResolution{
		Kind:   "random",
		Groups: []alignedcmd.MagicBoxRewardGroup{{DrawCount: 1, Entries: []alignedcmd.MagicBoxRewardEntry{{ItemID: 2600014, Weight: 100, Count: 2}}}},
	}
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUseRandomboxItemExpand),
		Body:                []byte{0x04, 0x43, 0x00, 0xA0, 0xED, 0x28, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x0A, 0x00},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		MagicBoxResolver:    staticMagicBoxResolver(resolution),
		MagicBoxRewardItemResolver: staticMagicBoxRewardResolver(map[int64]alignedcmd.MagicBoxRewardItem{
			2600014: {ItemID: 2600014, Kind: "stackable", SlotStart: 65, SlotEnd: 120},
		}),
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketUseRandomboxItemExpand) {
		t.Fatalf("ack msg = 0x%04X, want 0x0468", ack.MsgID)
	}
	row := func(slot uint16, itemID uint32, count uint32) []byte {
		r := make([]byte, currentMagicBoxEntrySize)
		binary.LittleEndian.PutUint16(r[0x00:0x02], slot)
		binary.LittleEndian.PutUint32(r[0x02:0x06], itemID)
		binary.LittleEndian.PutUint32(r[0x06:0x0A], count)
		return r
	}
	want := []byte{0x04, 0x04, 0x01, 0x0A, 0x00, 0x43, 0x00, 0xFF, 0xFF, 0x01, 0x00}
	want = append(want, row(65, 2600014, 20)...)
	want = append(want, 0x00, 0x00, 0x01, 0x00)
	want = append(want, row(65, 2600014, 2)...)
	if string(ack.Body) != string(want) {
		t.Fatalf("ack body = %x, want %x", ack.Body, want)
	}
	loaded, _, _ := repos.Inventory.Load(ctx, "77")
	if _, found := loaded.Slots["0:67"]; found {
		t.Fatalf("10 seria boxes should be consumed: %+v", loaded.Slots)
	}
	if potion := loaded.Slots["0:65"]; potion.ItemID != 2600014 || potion.Count != 22 {
		t.Fatalf("granted potion = %+v, want 22", potion)
	}
}

// TestOwnerApplyMagicBoxExpandSeriaLuckDoublePersistsPerAccount 验证 86JP
// SeriaLuck 账号级持久化：已有幸运值 7 时，开 2 个的第 1 轮翻倍（满 8），
// 第 2 轮正常，落库后幸运值为 1。
func TestOwnerApplyMagicBoxExpandSeriaLuckDoublePersistsPerAccount(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "acc", Metadata: map[string]string{currentSeriaLuckMetadataKey: "7"}}); err != nil {
		t.Fatal(err)
	}
	saveMagicBoxFixture(t, ctx, repos, map[string]dnfrepo.ItemStack{
		"0:67": {ItemID: 2682272, Count: 2, Extra: map[string]string{"item_kind": "stackable"}},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	resolution := alignedcmd.MagicBoxResolution{
		Kind:   "random",
		Groups: []alignedcmd.MagicBoxRewardGroup{{DrawCount: 1, Entries: []alignedcmd.MagicBoxRewardEntry{{ItemID: 2600014, Weight: 100, Count: 2}}}},
	}
	result, err := owner.ApplyMagicBoxExpand(ctx, MagicBoxExpandCommand{
		SelectedCharacterID: 77,
		AccountID:           "acc",
		ListType:            listTypeMain,
		SlotIndex:           67,
		BoxItemID:           2682272,
		MaterialSlotIndex:   -1,
		OpenCount:           2,
	}, staticMagicBoxResolver(resolution), staticMagicBoxRewardResolver(map[int64]alignedcmd.MagicBoxRewardItem{
		2600014: {ItemID: 2600014, Kind: "stackable", SlotStart: 65, SlotEnd: 120},
	}))
	if err != nil {
		t.Fatalf("ApplyMagicBoxExpand error = %v", err)
	}
	if !result.Success || !result.SeriaLuckDoubleTriggered || result.SeriaLuckBefore != 7 || result.SeriaLuckAfter != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.DoubleRewards) != 1 || result.DoubleRewards[0].ItemID != 2600014 || result.DoubleRewards[0].Count != 2 {
		t.Fatalf("double rewards = %+v", result.DoubleRewards)
	}
	if len(result.Rewards) != 1 || result.Rewards[0].Count != 6 {
		t.Fatalf("rewards = %+v, want aggregated 6", result.Rewards)
	}
	account, found, err := repos.Account.Load(ctx, "acc")
	if err != nil || !found || account.Metadata[currentSeriaLuckMetadataKey] != "1" {
		t.Fatalf("persisted luck found=%t err=%v metadata=%+v", found, err, account.Metadata)
	}
}
