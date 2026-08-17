package alignedcmd

import (
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func TestClassifyPendingModule(t *testing.T) {
	got, ok := Classify(uint16(dnfenum.CmdPacketDeleteItem))
	if !ok {
		t.Fatalf("Classify(DeleteItem) not found")
	}
	if got.Action != ActionPendingModule {
		t.Fatalf("action = %q, want %q", got.Action, ActionPendingModule)
	}
	if got.Domain != dnfenum.AlignedDomainInventory {
		t.Fatalf("domain = %q, want %q", got.Domain, dnfenum.AlignedDomainInventory)
	}
	if got.Support != dnfenum.AlignedSupportDirect {
		t.Fatalf("support = %q, want %q", got.Support, dnfenum.AlignedSupportDirect)
	}
}

func TestClassifyBlocked(t *testing.T) {
	got, ok := Classify(uint16(dnfenum.CmdPacketGetItembox))
	if !ok {
		t.Fatalf("Classify(519) not found")
	}
	if got.Action != ActionBlocked {
		t.Fatalf("action = %q, want %q", got.Action, ActionBlocked)
	}
	if got.Domain != dnfenum.AlignedDomainPackage {
		t.Fatalf("domain = %q, want %q", got.Domain, dnfenum.AlignedDomainPackage)
	}
}

func TestRegistryRoutePendingWithoutHandler(t *testing.T) {
	registry := DefaultRegistry()
	got, ok, err := registry.Route(context.Background(), Request{Opcode: uint16(dnfenum.CmdPacketMailboxOpen), Body: []byte{1, 2, 3}})
	if err != nil {
		t.Fatalf("Route error = %v", err)
	}
	if !ok {
		t.Fatalf("Route(MailboxOpen) not classified")
	}
	if !got.Handled {
		t.Fatalf("pending command should be handled by router")
	}
	if got.ResponseAllowed {
		t.Fatalf("pending command without handler must not allow response")
	}
	if got.Decision.Domain != dnfenum.AlignedDomainMail {
		t.Fatalf("domain = %q, want %q", got.Decision.Domain, dnfenum.AlignedDomainMail)
	}
}

func TestRegistryRouteBlocked(t *testing.T) {
	registry := DefaultRegistry()
	got, ok, err := registry.Route(context.Background(), Request{Opcode: uint16(dnfenum.CmdPacketGetItembox)})
	if err != nil {
		t.Fatalf("Route error = %v", err)
	}
	if !ok {
		t.Fatalf("Route(519) not classified")
	}
	if got.ResponseAllowed {
		t.Fatalf("blocked command must not allow response")
	}
	if got.Decision.Action != ActionBlocked {
		t.Fatalf("action = %q, want %q", got.Decision.Action, ActionBlocked)
	}
}

func TestRegistryRouteRequiresCurrentCommandDispatcher(t *testing.T) {
	registry := DefaultRegistry()
	_, ok, err := registry.Route(context.Background(), Request{
		Command:      byte(dnfenum.GameCmdNotice),
		CommandKnown: true,
		Opcode:       uint16(dnfenum.CmdPacketBuySkill),
	})
	if err != nil {
		t.Fatalf("Route error = %v", err)
	}
	if ok {
		t.Fatal("notice dispatcher type 29 was misrouted as a skill command")
	}
}

type postActionCloneHandler struct {
	actions []PostAction
}

func (postActionCloneHandler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainMail
}

func (h postActionCloneHandler) Handle(context.Context, Request) (Result, error) {
	return Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       "post_action_clone",
		PostActions:     h.actions,
	}, nil
}

func TestRegistryRouteClonesPostActions(t *testing.T) {
	actions := []PostAction{PostActionRefreshSelectedActorAppearance}
	registry := NewRegistry(postActionCloneHandler{actions: actions})
	got, ok, err := registry.Route(context.Background(), Request{Opcode: uint16(dnfenum.CmdPacketMailboxOpen)})
	if err != nil {
		t.Fatalf("Route error = %v", err)
	}
	if !ok || len(got.PostActions) != 1 || got.PostActions[0] != PostActionRefreshSelectedActorAppearance {
		t.Fatalf("post actions = %#v", got.PostActions)
	}
	actions[0] = "mutated_after_route"
	if got.PostActions[0] != PostActionRefreshSelectedActorAppearance {
		t.Fatalf("route result retained handler slice alias: %#v", got.PostActions)
	}
}
