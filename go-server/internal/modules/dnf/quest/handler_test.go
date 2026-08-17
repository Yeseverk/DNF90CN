package quest

import (
	"context"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeQuestIDRequestStripsEcho(t *testing.T) {
	got, err := DecodeQuestIDRequest([]byte{0x1F, 0x00, 0x34, 0x12})
	if err != nil {
		t.Fatalf("DecodeQuestIDRequest error = %v", err)
	}
	if got.QuestID != 0x1234 {
		t.Fatalf("quest = %d", got.QuestID)
	}
}

func TestDecodeSetTriggerRequest(t *testing.T) {
	got, err := DecodeSetTriggerRequest([]byte{0x21, 0x00, 0x34, 0x12, 0x10, 0x01})
	if err != nil {
		t.Fatalf("DecodeSetTriggerRequest error = %v", err)
	}
	if got.QuestID != 0x1234 || got.TriggerType != 0x10 || !got.IsIncrement {
		t.Fatalf("got = %+v", got)
	}
}

func TestDecodeFinishQuestRequestMatchesCurrentEXELiveTenByteBody(t *testing.T) {
	got, err := DecodeFinishQuestRequest([]byte{0x22, 0x00, 0x49, 0x0C, 0xFF, 0xFF, 0x01, 0x00, 0xFF, 0xFF})
	if err != nil {
		t.Fatalf("DecodeFinishQuestRequest error = %v", err)
	}
	if got.QuestID != 3145 || got.RewardSelectIndex != ^uint16(0) || got.HasRewardSelect ||
		got.Multiplier != 1 || got.Reserved != CurrentFinishQuestObservedTailMarker {
		t.Fatalf("got = %+v", got)
	}
}

func TestDecodeFinishQuestRequestRejectsProtectedOrMalformedBodies(t *testing.T) {
	for _, body := range [][]byte{
		make([]byte, 16),
		{0x21, 0x00, 0x49, 0x0C, 0xFF, 0xFF, 0x01, 0x00, 0xFF, 0xFF},
	} {
		if _, err := DecodeFinishQuestRequest(body); err == nil {
			t.Fatalf("DecodeFinishQuestRequest(%x) succeeded, want strict rejection", body)
		}
	}
}

func TestHandlerBlocksQuestAck(t *testing.T) {
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketFinishQuest),
		Body:                []byte{0x22, 0x00, 0x34, 0x12, 0xFF, 0xFF, 0x01, 0x00, 0xFF, 0xFF},
		AccountID:           " acc-quest ",
		SelectedCharacterID: 46,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	if got.Operation != "finish_quest" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if !strings.Contains(got.Reason, `account="acc-quest"`) || !strings.Contains(got.Reason, "char=46") || !strings.Contains(got.Reason, "quest=4660") || !strings.Contains(got.Reason, "mutation id") {
		t.Fatalf("reason should include command plan, got %q", got.Reason)
	}
}

func TestHandlerFinishQuestUsesOwnerButStillBlocksAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "46",
		AccountID:   "acc-quest",
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "46",
		States: map[int64]dnfrepo.QuestState{
			0x1234: {Status: "active", ProgressValue: 9},
		},
	}); err != nil {
		t.Fatalf("save quest: %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketFinishQuest),
		Body:                []byte{0x22, 0x00, 0x34, 0x12, 0x02, 0x00, 0x01, 0x00, 0xFF, 0xFF},
		AccountID:           "acc-quest",
		SelectedCharacterID: 46,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	if !strings.Contains(got.Reason, "quest owner verified") || !strings.Contains(got.Reason, "known=true") || !strings.Contains(got.Reason, "status=active") || !strings.Contains(got.Reason, "progress=9") {
		t.Fatalf("reason should include owner preflight, got %q", got.Reason)
	}
}

func TestHandlerSetTriggerType1WritesRecomputedProgressButStillBlocksAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "49",
		AccountID:   "acc-quest",
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "49",
		States: map[int64]dnfrepo.QuestState{
			0x2233: {
				Status:        "active",
				ProgressValue: 1,
				Extra:         map[string]string{"seeking_item_ids": "9010"},
			},
		},
	}); err != nil {
		t.Fatalf("save quest: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "49",
		Slots: map[string]dnfrepo.ItemStack{
			"0:12": {ItemID: 9010, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketSetQuestTrigger),
		Body:                []byte{0x21, 0x00, 0x33, 0x22, 0x01, 0x01},
		AccountID:           "acc-quest",
		SelectedCharacterID: 49,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	if !strings.Contains(got.Reason, "progress=0") || !strings.Contains(got.Reason, "success ACK is blocked") {
		t.Fatalf("reason should include recomputed progress and ACK block, got %q", got.Reason)
	}
	loaded, ok, err := repos.Quest.Load(ctx, "49")
	if err != nil || !ok {
		t.Fatalf("load quest ok=%v err=%v", ok, err)
	}
	if got := loaded.States[0x2233].ProgressValue; got != 0 {
		t.Fatalf("stored progress = %d, want 0", got)
	}
}

func TestFinishCommandRecordsRewardOwnerGap(t *testing.T) {
	cmd := NewFinishCommand(alignedcmd.Request{
		AccountID:           " acc-quest-2 ",
		SelectedCharacterID: 77,
	}, FinishQuestRequest{
		QuestID:           0x1234,
		RewardSelectIndex: 2,
		HasRewardSelect:   true,
		Multiplier:        3,
	})
	if cmd.AccountID != "acc-quest-2" || cmd.SelectedCharacterID != 77 || cmd.QuestID != 0x1234 || cmd.RewardSelectIndex != 2 || !cmd.HasRewardSelect || cmd.Multiplier != 3 {
		t.Fatalf("cmd = %+v", cmd)
	}
	if !strings.Contains(cmd.String(), "USERINFO/NOTI order") {
		t.Fatalf("command plan must name response/order gap: %s", cmd.String())
	}
}
