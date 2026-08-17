package inventory

import (
	"context"
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func staticUpgradeTicketResolver(resolution alignedcmd.UpgradeTicketResolution) alignedcmd.UpgradeTicketResolver {
	return func(materialItemID int64, targetItemID int64) (alignedcmd.UpgradeTicketResolution, error) {
		return resolution, nil
	}
}

func reinforceTicketResolver(level int64, weight int64) alignedcmd.UpgradeTicketResolver {
	return staticUpgradeTicketResolver(alignedcmd.UpgradeTicketResolution{
		TicketMode:    "reinforce",
		TargetLevel:   level,
		SuccessWeight: weight,
		TargetKind:    "equipment",
		TicketPVFPath: "stackable/ticket/plus10.stk",
	})
}

func newUpgradeTicketCommand(mode string, targetSlot int16, targetItemID int32, materialSlot int16) Command {
	return NewUpgradeCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UpgradeItemRequest{
		Mode:                    mode,
		TargetSlotIndex:         targetSlot,
		TargetItemTemplateID:    targetItemID,
		MaterialSlotIndex:       materialSlot,
		OptionalTicketSlotIndex: -1,
		TargetItemName:          "fixture",
	})
}

func saveUpgradeTicketFixture(t *testing.T, ctx context.Context, repos dnfrepo.Group, targetExtra map[string]string, targetLevel byte, ticketCount int64) {
	t.Helper()
	targetRaw := make([]byte, currentItemListEntrySize)
	targetRaw[0x0A] = targetLevel
	ticketRaw := make([]byte, currentItemListEntrySize)
	extra := map[string]string{"item_kind": "equipment"}
	for key, value := range targetExtra {
		extra[key] = value
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID:   700,
				Count:    1,
				RawEntry: targetRaw,
				Extra:    extra,
			},
			slotKey(listTypeMain, 121): {
				ItemID:   9100,
				Count:    ticketCount,
				RawEntry: ticketRaw,
				Extra: map[string]string{
					"item_kind": "stackable",
					"pvf_path":  "stackable/ticket/plus10.stk",
				},
			},
		},
	})
}

func TestOwnerUpgradeTicketSuccessJumpsToTargetLevel(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveUpgradeTicketFixture(t, ctx, repos, nil, 2, 2)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.UpgradeTicket(ctx, newUpgradeTicketCommand("reinforce", 9, 700, 121), reinforceTicketResolver(10, 100000))
	if err != nil {
		t.Fatalf("UpgradeTicket error = %v", err)
	}
	if !result.Success || !result.TicketResolved || !result.UpgradeSucceeded || result.ResultCode != upgradeTicketResultCodeSuccess || result.OldLevel != 2 || result.NewLevel != 10 || result.MaterialRemainingStackCount != 1 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	target := loaded.Slots[slotKey(listTypeMain, 9)]
	if got := target.Extra["reinforce"]; got != "10" {
		t.Fatalf("reinforce = %q, want 10", got)
	}
	if got := target.Extra["upgrade_level"]; got != "10" {
		t.Fatalf("upgrade_level = %q, want 10", got)
	}
	if got := target.RawEntry[0x0A] & 0x1F; got != 10 {
		t.Fatalf("raw packed level = %d, want 10", got)
	}
	ticket := loaded.Slots[slotKey(listTypeMain, 121)]
	if ticket.Count != 1 {
		t.Fatalf("ticket count = %d, want 1", ticket.Count)
	}
	if got := ticket.RawEntry[0x06]; got != 1 {
		t.Fatalf("ticket raw amount = %d, want 1", got)
	}
}

func TestOwnerUpgradeTicketAmplifySuccess(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveUpgradeTicketFixture(t, ctx, repos, map[string]string{"amplify_type": "1", "amplify_value": "5"}, 3, 1)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.UpgradeTicket(ctx, newUpgradeTicketCommand("amplify", 9, 700, 121), staticUpgradeTicketResolver(alignedcmd.UpgradeTicketResolution{
		TicketMode: "amplify", TargetLevel: 12, SuccessWeight: 100000, TargetKind: "equipment",
	}))
	if err != nil {
		t.Fatalf("UpgradeTicket error = %v", err)
	}
	if !result.Success || !result.UpgradeSucceeded || result.NewLevel != 12 {
		t.Fatalf("result = %+v, want amplify jump to 12", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	target := loaded.Slots[slotKey(listTypeMain, 9)]
	if got := target.Extra["upgrade_level"]; got != "12" {
		t.Fatalf("upgrade_level = %q, want 12", got)
	}
	if got := target.Extra["amplify_value"]; got != "5" {
		t.Fatalf("amplify state must be preserved, got %q", got)
	}
}

func TestOwnerUpgradeTicketFailedRollRetainsLevelAndDeletesExhaustedTicket(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveUpgradeTicketFixture(t, ctx, repos, nil, 7, 1)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.UpgradeTicket(ctx, newUpgradeTicketCommand("reinforce", 9, 700, 121), reinforceTicketResolver(12, 0))
	if err != nil {
		t.Fatalf("UpgradeTicket error = %v", err)
	}
	if !result.Success || result.UpgradeSucceeded || result.ResultCode != upgradeTicketResultCodeFailureRetain || result.NewLevel != 7 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, ok := loaded.Slots[slotKey(listTypeMain, 121)]; ok {
		t.Fatalf("exhausted ticket slot should be deleted")
	}
	target := loaded.Slots[slotKey(listTypeMain, 9)]
	if got := target.RawEntry[0x0A] & 0x1F; got != 7 {
		t.Fatalf("level changed on failed roll: %d", got)
	}
}

func TestOwnerUpgradeTicketClampTargetLevel(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveUpgradeTicketFixture(t, ctx, repos, nil, 29, 1)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.UpgradeTicket(ctx, newUpgradeTicketCommand("reinforce", 9, 700, 121), reinforceTicketResolver(99, 100000))
	if err != nil {
		t.Fatalf("UpgradeTicket error = %v", err)
	}
	if !result.Success || result.NewLevel != 31 {
		t.Fatalf("result = %+v, want clamped level 31", result)
	}
}

func TestOwnerUpgradeTicketNonTicketMaterialStaysUnresolved(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveUpgradeTicketFixture(t, ctx, repos, nil, 2, 2)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.UpgradeTicket(ctx, newUpgradeTicketCommand("reinforce", 9, 700, 121), staticUpgradeTicketResolver(alignedcmd.UpgradeTicketResolution{TargetKind: "equipment"}))
	if err != nil {
		t.Fatalf("UpgradeTicket error = %v", err)
	}
	if result.TicketResolved || result.Success || result.Changed {
		t.Fatalf("non-ticket material must stay on the pending path: %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 121)].Count; got != 2 {
		t.Fatalf("ticket mutated on non-ticket path: %d", got)
	}
}

func TestOwnerUpgradeTicketRejections(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name        string
		cmd         Command
		resolver    alignedcmd.UpgradeTicketResolver
		targetExtra map[string]string
		targetLevel byte
		wantCode    byte
	}{
		{
			name:     "mode mismatch",
			cmd:      newUpgradeTicketCommand("amplify", 9, 700, 121),
			resolver: reinforceTicketResolver(10, 100000),
			wantCode: upgradeTicketErrorWrongMode,
		},
		{
			name:        "target above level cap",
			cmd:         newUpgradeTicketCommand("reinforce", 9, 700, 121),
			resolver:    reinforceTicketResolver(10, 100000),
			targetLevel: 31,
			wantCode:    upgradeTicketErrorMaxLevel,
		},
		{
			name:        "durability not full",
			cmd:         newUpgradeTicketCommand("reinforce", 9, 700, 121),
			resolver:    reinforceTicketResolver(10, 100000),
			targetExtra: map[string]string{"durability": "10", "max_durability": "45"},
			wantCode:    upgradeTicketErrorDurability,
		},
		{
			name:        "equipment locked",
			cmd:         newUpgradeTicketCommand("reinforce", 9, 700, 121),
			resolver:    reinforceTicketResolver(10, 100000),
			targetExtra: map[string]string{"equipment_lock_state": "locked"},
			wantCode:    upgradeTicketErrorLocked,
		},
		{
			name: "reinforce on amplified equipment",
			cmd:  newUpgradeTicketCommand("reinforce", 9, 700, 121),
			resolver: staticUpgradeTicketResolver(alignedcmd.UpgradeTicketResolution{
				TicketMode: "reinforce", TargetLevel: 10, SuccessWeight: 100000, TargetKind: "equipment",
			}),
			targetExtra: map[string]string{"amplify_type": "1", "amplify_value": "5"},
			wantCode:    upgradeTicketErrorWrongMode,
		},
		{
			name: "amplify without amplify state",
			cmd:  newUpgradeTicketCommand("amplify", 9, 700, 121),
			resolver: staticUpgradeTicketResolver(alignedcmd.UpgradeTicketResolution{
				TicketMode: "amplify", TargetLevel: 10, SuccessWeight: 100000, TargetKind: "equipment",
			}),
			wantCode: upgradeTicketErrorWrongMode,
		},
		{
			name: "amplify not identified",
			cmd:  newUpgradeTicketCommand("amplify", 9, 700, 121),
			resolver: staticUpgradeTicketResolver(alignedcmd.UpgradeTicketResolution{
				TicketMode: "amplify", TargetLevel: 10, SuccessWeight: 100000, TargetKind: "equipment",
			}),
			targetExtra: map[string]string{"amplify_type": "0x81", "amplify_value": "5"},
			wantCode:    upgradeTicketErrorAmplifyNotIdentified,
		},
		{
			name: "target forbidden by PVF",
			cmd:  newUpgradeTicketCommand("reinforce", 9, 700, 121),
			resolver: staticUpgradeTicketResolver(alignedcmd.UpgradeTicketResolution{
				TicketMode: "reinforce", TargetLevel: 10, SuccessWeight: 100000, TargetKind: "equipment", TargetUpgradeForbidden: true,
			}),
			wantCode: upgradeTicketErrorForbidden,
		},
		{
			name: "random ticket family",
			cmd:  newUpgradeTicketCommand("reinforce", 9, 700, 121),
			resolver: staticUpgradeTicketResolver(alignedcmd.UpgradeTicketResolution{
				TicketMode: "reinforce", TicketRandom: true, TargetKind: "equipment",
			}),
			wantCode: upgradeTicketErrorForbidden,
		},
		{
			name: "target not equipment kind",
			cmd:  newUpgradeTicketCommand("reinforce", 9, 700, 121),
			resolver: staticUpgradeTicketResolver(alignedcmd.UpgradeTicketResolution{
				TicketMode: "reinforce", TargetLevel: 10, SuccessWeight: 100000, TargetKind: "stackable",
			}),
			wantCode: upgradeTicketErrorInvalidTarget,
		},
		{
			name:     "material slot missing",
			cmd:      newUpgradeTicketCommand("reinforce", 9, 700, -1),
			resolver: reinforceTicketResolver(10, 100000),
			wantCode: upgradeTicketErrorInvalidMaterial,
		},
		{
			name:     "material equals target slot",
			cmd:      newUpgradeTicketCommand("reinforce", 9, 700, 9),
			resolver: reinforceTicketResolver(10, 100000),
			wantCode: upgradeTicketErrorInvalidMaterial,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repos := dnfrepomemory.NewMemoryGroup()
			saveUpgradeTicketFixture(t, ctx, repos, tc.targetExtra, tc.targetLevel, 2)
			owner, err := NewOwner(repos)
			if err != nil {
				t.Fatal(err)
			}
			result, err := owner.UpgradeTicket(ctx, tc.cmd, tc.resolver)
			if err != nil {
				t.Fatalf("UpgradeTicket error = %v", err)
			}
			if result.Success || result.ErrorCode != tc.wantCode {
				t.Fatalf("result = %+v, want error code %d", result, tc.wantCode)
			}
			if result.Changed {
				t.Fatalf("rejected ticket flow must not mutate state")
			}
		})
	}
}

func TestOwnerUpgradeTicketNilResolverFailsClosed(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpgradeTicket(ctx, newUpgradeTicketCommand("reinforce", 9, 700, 121), nil); !errors.Is(err, ErrUpgradeTicketResolverRequired) {
		t.Fatalf("UpgradeTicket error = %v, want ErrUpgradeTicketResolverRequired", err)
	}
}
