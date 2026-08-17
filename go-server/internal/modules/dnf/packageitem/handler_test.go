package packageitem

import (
	"context"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeSelectablePackageRequest(t *testing.T) {
	got, err := DecodeSelectablePackageRequest([]byte{0x05, 0x00, 0x00, 0x00, 0x44, 0x33, 0x22, 0x11, 0x01})
	if err != nil {
		t.Fatalf("DecodeSelectablePackageRequest error = %v", err)
	}
	if got.SlotIndex != 5 || got.SelectedItemTemplateID != 0x11223344 || got.SelectionFlag != 1 {
		t.Fatalf("got = %+v", got)
	}
}

func TestDecodeMagicBoxSingleRequest(t *testing.T) {
	got, err := DecodeMagicBoxSingleRequest([]byte{clientMainListType, 0x06, 0x00, 0x07, 0x00})
	if err != nil {
		t.Fatalf("DecodeMagicBoxSingleRequest error = %v", err)
	}
	if got.ListType != listTypeMain || got.SlotIndex != 6 || got.MaterialSlotIndex != 7 {
		t.Fatalf("got = %+v", got)
	}
}

func TestHandlerBlocksPackageAck(t *testing.T) {
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUseBoosterItem),
		Body:                []byte{0x05, 0x00, 0x00, 0x00, 0x44, 0x33, 0x22, 0x11, 0x01},
		AccountID:           " acc-package ",
		SelectedCharacterID: 81,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	if !strings.Contains(got.Reason, `account="acc-package"`) || !strings.Contains(got.Reason, "char=81") || !strings.Contains(got.Reason, "slot=5") || !strings.Contains(got.Reason, "selected=287454020") {
		t.Fatalf("reason should include command plan, got %q", got.Reason)
	}
}

func TestHandlerSelectableUsesOwnerButStillBlocksAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "81",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 7001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUseBoosterItem),
		Body:                []byte{0x05, 0x00, 0x00, 0x00, 0x44, 0x33, 0x22, 0x11, 0x01},
		AccountID:           " acc-package ",
		SelectedCharacterID: 81,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	for _, want := range []string{"package owner 已验证", "source=(0,5)", "item=7001", "selected=287454020", "禁止回成功包"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("reason missing %q: %s", want, got.Reason)
		}
	}
}

func TestHandlerMagicBoxUsesOwnerButStillBlocksAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "82",
		Slots: map[string]dnfrepo.ItemStack{
			"0:6": {ItemID: 8001, Count: 1},
			"0:7": {ItemID: 8002, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUseRandomboxItem),
		Body:                []byte{clientMainListType, 0x06, 0x00, 0x07, 0x00},
		AccountID:           " acc-package ",
		SelectedCharacterID: 82,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	for _, want := range []string{"resolvers unavailable", "magic box"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("reason missing %q: %s", want, got.Reason)
		}
	}
}

func TestSelectableCommandRecordsRewardOwnerGap(t *testing.T) {
	cmd := NewSelectableCommand(alignedcmd.Request{
		AccountID:           " acc-package-2 ",
		SelectedCharacterID: 82,
	}, SelectablePackageRequest{
		SlotIndex:              5,
		SelectionContext:       1,
		SelectedItemTemplateID: 1001,
		SelectionFlag:          1,
		AvatarChoiceCount:      2,
	})
	if cmd.AccountID != "acc-package-2" || cmd.SelectedCharacterID != 82 || cmd.SlotIndex != 5 || cmd.SelectedItemTemplateID != 1001 || cmd.AvatarChoiceCount != 2 {
		t.Fatalf("cmd = %+v", cmd)
	}
	if !strings.Contains(cmd.String(), "popup/NOTI order") {
		t.Fatalf("command plan must name popup/order gap: %s", cmd.String())
	}
}
