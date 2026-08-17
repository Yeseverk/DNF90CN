package character

import (
	"context"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeCreateCharacterRequest(t *testing.T) {
	got, err := DecodeCreateCharacterRequest([]byte{15, 3, 0, 0, 0, 'a', 'b', 'c', 0})
	if err != nil {
		t.Fatalf("DecodeCreateCharacterRequest error = %v", err)
	}
	if got.Job != 15 || got.Name != "abc" {
		t.Fatalf("got = %+v", got)
	}
}

func TestDecodeDeleteCharacterRequest(t *testing.T) {
	got, err := DecodeDeleteCharacterRequest([]byte{2, 3, 0, 0, 0, 'a', 'b', 'c'})
	if err != nil {
		t.Fatalf("DecodeDeleteCharacterRequest error = %v", err)
	}
	if got.Slot != 2 || got.Name != "abc" {
		t.Fatalf("got = %+v", got)
	}
}

func TestHandlerBlocksCharacterFallbackAck(t *testing.T) {
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketReturnSelectCharacter),
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
}

func TestHandlerUsesOwnerPreflightAndStillBlocksCharacterAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "acc",
		Slot:        2,
		Name:        "hero",
		Job:         "15",
		Level:       86,
	}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:       uint16(dnfenum.CmdPacketCheckDoubleCharacterName),
		AccountID:    "acc",
		Repositories: repos,
		Body:         []byte{4, 0, 0, 0, 'h', 'e', 'r', 'o'},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	for _, want := range []string{"character owner verified", "roster=1", `requested=(slotOrChar=0 slot=0 job=0 name="hero")`, "nameTaken=true", "owner=77"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("reason %q missing %q", got.Reason, want)
		}
	}
}

func TestCreateCommandRecordsCharacterOwnerGap(t *testing.T) {
	cmd := NewCreateCommand(alignedcmd.Request{
		AccountID:           " acc ",
		SelectedCharacterID: 0,
	}, CreateCharacterRequest{Job: 15, Name: "abc"})
	summary := cmd.String()
	for _, want := range []string{`account="acc"`, "job=15", `name="abc"`, "character owner", "roster op2"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
}
