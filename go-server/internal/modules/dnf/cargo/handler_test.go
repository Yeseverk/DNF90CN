package cargo

import (
	"context"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeGoldRequest(t *testing.T) {
	got, err := DecodeGoldRequest([]byte{0x40, 0x42, 0x0F, 0x00})
	if err != nil {
		t.Fatalf("DecodeGoldRequest error = %v", err)
	}
	if got.Amount != 1000000 {
		t.Fatalf("amount = %d, want 1000000", got.Amount)
	}
}

func TestHandlerBlocksCargoSuccessAck(t *testing.T) {
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketDepositMoney),
		Body:                []byte{0x01, 0x00, 0x00, 0x00},
		AccountID:           " acc-1 ",
		SelectedCharacterID: 12,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	if got.Operation != "deposit_money" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if !strings.Contains(got.Reason, `account="acc-1"`) || !strings.Contains(got.Reason, "char=12") || !strings.Contains(got.Reason, "amount=1") {
		t.Fatalf("reason should include command plan, got %q", got.Reason)
	}
}

func TestHandlerDepositUsesOwnerAndReturnsAckThenGoldRefresh(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-5",
		Metadata: map[string]string{
			"account_cargo_gold":    "400",
			"account_cargo_level":   "1",
			"account_cargo_created": "true",
		},
	}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "90",
		AccountID:   "acc-5",
		Stats:       map[string]int64{"gold": 800},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketDepositMoney),
		Body:                []byte{0x2C, 0x01, 0x00, 0x00},
		AccountID:           "acc-5",
		SelectedCharacterID: 90,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled with response", got)
	}
	if !strings.Contains(got.Reason, "cargo owner applied") || !strings.Contains(got.Reason, "charGold=500") || !strings.Contains(got.Reason, "cargoGold=700") {
		t.Fatalf("reason should include owner mutation, got %q", got.Reason)
	}
	if len(got.UpperResponses) != 2 {
		t.Fatalf("response count = %d, want 2", len(got.UpperResponses))
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketDepositMoney) || ack.Classification != dnfproto.DefaultChannelClassification || !ack.AllowCodec {
		t.Fatalf("ack = %+v", ack)
	}
	if want := []byte{1, 0xBC, 0x02, 0x00, 0x00}; string(ack.Body) != string(want) {
		t.Fatalf("ack body = % X, want % X", ack.Body, want)
	}
	refresh := got.UpperResponses[1]
	if refresh.MsgID != msgItemListUpdate || refresh.Classification != 0 || !refresh.AllowCodec {
		t.Fatalf("refresh = %+v", refresh)
	}
	if len(refresh.Body) != 3+commonUpdateEntrySize || refresh.Body[0] != 0 || refresh.Body[1] != 1 || refresh.Body[2] != 0 {
		t.Fatalf("gold refresh header/len = len=%d body=% X", len(refresh.Body), refresh.Body[:min(len(refresh.Body), 12)])
	}
	if refresh.Body[9] != 0xF4 || refresh.Body[10] != 0x01 {
		t.Fatalf("gold refresh value bytes = % X, want 500", refresh.Body[9:13])
	}
	account, ok, err := repos.Account.Load(ctx, "acc-5")
	if err != nil || !ok {
		t.Fatalf("load account ok=%t err=%v", ok, err)
	}
	if got := account.Metadata["account_cargo_gold"]; got != "700" {
		t.Fatalf("cargo gold = %q, want 700", got)
	}
}

func TestHandlerCreateCargoUsesOwnerAndReturnsAckCostAndCurrentCargoPostAction(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "acc-6"}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "91",
		AccountID:   "acc-6",
		Stats:       map[string]int64{"gold": 500000},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketCreateAccountCargo),
		AccountID:           " acc-6 ",
		SelectedCharacterID: 91,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled with response", got)
	}
	if !strings.Contains(got.Reason, "cargo owner applied") || !strings.Contains(got.Reason, "cargoLevel=1") || !strings.Contains(got.Reason, "cargoCreated=true") {
		t.Fatalf("reason should include account mutation, got %q", got.Reason)
	}
	if len(got.UpperResponses) != 2 {
		t.Fatalf("response count = %d, want 2", len(got.UpperResponses))
	}
	if ack := got.UpperResponses[0]; ack.MsgID != uint16(dnfenum.CmdPacketCreateAccountCargo) || string(ack.Body) != string([]byte{1}) || ack.Classification != dnfproto.DefaultChannelClassification {
		t.Fatalf("ack = %+v", ack)
	}
	if gold := got.UpperResponses[1]; gold.MsgID != msgItemListUpdate || gold.Classification != 0 || len(gold.Body) != 3+commonUpdateEntrySize {
		t.Fatalf("gold refresh = %+v len=%d", gold, len(gold.Body))
	}
	if len(got.PostActions) != 1 || got.PostActions[0] != alignedcmd.PostActionRefreshSelectedAccountCargo {
		t.Fatalf("post actions = %+v, want current account cargo refresh", got.PostActions)
	}
	character, ok, err := repos.Character.Load(ctx, "91")
	if err != nil || !ok {
		t.Fatalf("load character ok=%t err=%v", ok, err)
	}
	if got := character.Stats["gold"]; got != 400000 {
		t.Fatalf("character gold = %d, want 400000", got)
	}
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestMoneyCommandRecordsOwnerGap(t *testing.T) {
	cmd := NewMoneyCommand(alignedcmd.Request{
		AccountID:           " acc-2 ",
		SelectedCharacterID: 99,
	}, MoneyWithdraw, GoldRequest{Amount: 500})
	if cmd.AccountID != "acc-2" || cmd.SelectedCharacterID != 99 || cmd.Amount != 500 || cmd.MoneyDirection != MoneyWithdraw {
		t.Fatalf("cmd = %+v", cmd)
	}
	if !strings.Contains(cmd.String(), "mutation id") {
		t.Fatalf("command plan must name reliable-write gap: %s", cmd.String())
	}
}
