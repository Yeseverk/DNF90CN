package avatartitle

import (
	"context"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeTitleBookRequest(t *testing.T) {
	got, err := DecodeTitleBookRequest([]byte{
		1, 0, 0, 0,
		5, 0, 0, 0,
		0x44, 0x33, 0x22, 0x11,
		2, 0, 0, 0,
		3, 0, 0, 0,
	})
	if err != nil {
		t.Fatalf("DecodeTitleBookRequest error = %v", err)
	}
	if got.ItemSpaceRaw != 1 || got.Slot != 5 || got.ItemID != 0x11223344 || got.Category != 2 || got.Index != 3 {
		t.Fatalf("got = %+v", got)
	}
}

func TestDecodeAvatarEmblemRequest(t *testing.T) {
	got, err := DecodeAvatarEmblemRequest([]byte{
		listTypeAvatar,
		0x05, 0x00,
		0x44, 0x33, 0x22, 0x11,
		1,
		0x06, 0x00,
		0x88, 0x77, 0x66, 0x55,
		2,
	})
	if err != nil {
		t.Fatalf("DecodeAvatarEmblemRequest error = %v", err)
	}
	if got.TargetSlot != 5 || got.TargetItemID != 0x11223344 || len(got.Emblems) != 1 {
		t.Fatalf("got = %+v", got)
	}
}

func TestHandlerBlocksAvatarTitleAck(t *testing.T) {
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketAddAvatarSocket),
		Body:   []byte{0x05, 0x00, 0x44, 0x33, 0x22, 0x11, 0x06, 0x00},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
}

func TestHandlerUsesOwnerPreflightAndStillBlocksAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "88", AccountID: "acc"}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "88",
		Slots: map[string]dnfrepo.ItemStack{
			"1:5": {ItemID: 0x11223344, Count: 1},
			"0:6": {ItemID: 7001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("Inventory.Save error = %v", err)
	}
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "88",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"0": {SlotIndex: 0, ItemID: 9001},
		},
	}); err != nil {
		t.Fatalf("Equipment.Save error = %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketAddAvatarSocket),
		SelectedCharacterID: 88,
		Repositories:        repos,
		Body:                []byte{0x05, 0x00, 0x44, 0x33, 0x22, 0x11, 0x06, 0x00},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	for _, want := range []string{"avatartitle owner verified", "targetFound=true", "materialFound=true", "equipEntries=1"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("reason %q missing %q", got.Reason, want)
		}
	}
}

func TestCommandRecordsAvatarOwnerGap(t *testing.T) {
	cmd := NewEmblemCommand(alignedcmd.Request{
		AccountID:           " acc ",
		SelectedCharacterID: 88,
	}, AvatarEmblemRequest{
		TargetSlot:   5,
		TargetItemID: 0x11223344,
		Emblems:      []EmblemApply{{SocketIndex: 2}},
	})
	summary := cmd.String()
	for _, want := range []string{`account="acc"`, "char=88", "target=(5,287454020)", "emblems=1", "avatar owner", "USERINFO"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
}
