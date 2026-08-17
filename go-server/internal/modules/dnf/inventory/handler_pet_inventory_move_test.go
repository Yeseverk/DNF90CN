package inventory

import (
	"context"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestHandlerMovesPetArtifactWithinArtifactSegment(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, 148): {ItemID: 10006783, Count: 1},
		},
	})

	result, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypePet, 148, 10006783, 1, listTypePet, 141, 0, 0, -1),
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !result.ResponseAllowed || len(result.PostActions) != 1 || result.PostActions[0] != alignedcmd.PostActionRefreshSelectedItemContainers {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, ok := loaded.Slots[slotKey(listTypePet, 148)]; ok || loaded.Slots[slotKey(listTypePet, 141)].ItemID != 10006783 {
		t.Fatalf("slots = %+v", loaded.Slots)
	}
}

func TestHandlerMergesPetConsumableWithinConsumableSegment(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	extra := map[string]string{"item_kind": "stackable", "stack_limit": "100", "pvf_path": "stackable/creature/feed.stk"}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, 189): {ItemID: 500100, Count: 2, Extra: extra},
			slotKey(listTypePet, 200): {ItemID: 500100, Count: 3, Extra: extra},
		},
	})

	result, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypePet, 189, 500100, 2, listTypePet, 200, 500100, 3, -1),
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !result.ResponseAllowed || !strings.Contains(result.Reason, "moveMode=stack") {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, ok := loaded.Slots[slotKey(listTypePet, 189)]; ok || loaded.Slots[slotKey(listTypePet, 200)].Count != 5 {
		t.Fatalf("slots = %+v", loaded.Slots)
	}
}

func TestHandlerRejectsPetInventoryCrossSegmentMove(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, 140): {ItemID: 10006783, Count: 1},
		},
	})

	result, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypePet, 140, 10006783, 1, listTypePet, 189, 0, 0, -1),
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if result.ResponseAllowed || !strings.Contains(result.Reason, "crosses container segments") {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if loaded.Slots[slotKey(listTypePet, 140)].ItemID != 10006783 {
		t.Fatalf("slots mutated = %+v", loaded.Slots)
	}
}

func TestHandlerEquipsDirectGrantedPetWithoutExistingPetRecord(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, 0): {
				ItemID: 400990199,
				Count:  1,
				Extra: map[string]string{
					"source":         "booster_item",
					"item_kind":      "equipment",
					"equipment_type": "[creature]",
					"pvf_path":       "equipment/creature/2018Summer/chn_2018_summer_400990199.equ",
				},
			},
		},
	})
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}}); err != nil {
		t.Fatalf("Save equipment error = %v", err)
	}

	result, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypePet, 0, 400990199, 1, listTypeActorWornAlt, 26, 0, 0, -1),
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !result.ResponseAllowed || len(result.PostActions) != 3 ||
		result.PostActions[0] != alignedcmd.PostActionRefreshSelectedItemContainers ||
		result.PostActions[1] != alignedcmd.PostActionRefreshSelectedCreatureState ||
		result.PostActions[2] != alignedcmd.PostActionRefreshSelectedActorAppearance {
		t.Fatalf("result = %+v", result)
	}
	if len(result.ItemSlotRefreshes) != 0 {
		t.Fatalf("pet slot26 wear emitted duplicate item slot refreshes=%v", result.ItemSlotRefreshes)
	}
	if len(result.UpperResponses) != 1 ||
		len(result.UpperResponses[0].Body) != 11 ||
		result.UpperResponses[0].Body[8] != listTypeActorWornAlt {
		t.Fatalf("pet op19 ACK lost raw actor-worn endpoint: %+v", result.UpperResponses)
	}
	equipment, ok, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !ok || equipment.Entries["26"].ItemID != 400990199 {
		t.Fatalf("equipment ok=%t err=%v record=%+v", ok, err, equipment)
	}
	petRecord, ok, err := repos.Pet.Load(ctx, "77")
	if err != nil || !ok || len(petRecord.Entries) != 1 || petRecord.EquippedKey == "" {
		t.Fatalf("pet ok=%t err=%v record=%+v", ok, err, petRecord)
	}
	entry := petRecord.Entries[petRecord.EquippedKey]
	if entry.ItemID != 400990199 || entry.SourceListType != listTypeEquipment || entry.SourceSlotIndex != 26 {
		t.Fatalf("equipped pet entry = %+v", entry)
	}
}
