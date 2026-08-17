package inventory

import (
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestIsProvenPetEquipmentMoveCoversCreatureAndArtifactFamilies(t *testing.T) {
	tests := []struct {
		name                        string
		sourceList, destinationList byte
		sourceSlot, destinationSlot int16
		want                        bool
	}{
		{name: "creature equip", sourceList: listTypePet, destinationList: listTypeEquipment, sourceSlot: 48, destinationSlot: 26, want: true},
		{name: "creature alternate actor endpoint", sourceList: listTypePet, destinationList: listTypeActorWornAlt, sourceSlot: 48, destinationSlot: 26, want: true},
		{name: "artifact equip red", sourceList: listTypePet, destinationList: listTypeEquipment, sourceSlot: 140, destinationSlot: 27, want: true},
		{name: "artifact alternate actor endpoint", sourceList: listTypePet, destinationList: listTypeActorWornAlt, sourceSlot: 188, destinationSlot: 28, want: true},
		{name: "reversed creature endpoints are not emitted by current EXE", sourceList: listTypeEquipment, destinationList: listTypePet, sourceSlot: 26, destinationSlot: 48, want: false},
		{name: "reversed artifact endpoints are not emitted by current EXE", sourceList: listTypeActorWornAlt, destinationList: listTypePet, sourceSlot: 29, destinationSlot: 140, want: false},
		{name: "creature range to artifact target", sourceList: listTypePet, destinationList: listTypeEquipment, sourceSlot: 139, destinationSlot: 27, want: false},
		{name: "artifact range to creature target", sourceList: listTypePet, destinationList: listTypeEquipment, sourceSlot: 140, destinationSlot: 26, want: false},
		{name: "artifact target out of range", sourceList: listTypePet, destinationList: listTypeEquipment, sourceSlot: 140, destinationSlot: 30, want: false},
		{name: "ordinary main move", sourceList: listTypeMain, destinationList: listTypeMain, sourceSlot: 1, destinationSlot: 2, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := Command{SourceListType: test.sourceList, SourceSlotIndex: test.sourceSlot, DestinationListType: test.destinationList, DestinationSlotIndex: test.destinationSlot}
			if got := isProvenPetEquipmentMove(cmd); got != test.want {
				t.Fatalf("isProvenPetEquipmentMove = %t, want %t", got, test.want)
			}
		})
	}
}

func TestHandlerMoveArtifactThroughActorEndpoint17CommitsAllProjections(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, 140): {ItemID: 2747155, Count: 1, RawEntry: make([]byte, currentItemListEntrySize)},
		},
	})
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}}); err != nil {
		t.Fatalf("Save equipment error = %v", err)
	}

	result, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypePet, 140, 0x1234, 1, listTypeActorWornAlt, 27, 0, 0, -1),
		SelectedCharacterID: 77,
		Repositories:        repos,
		EquipmentPlacement: func(_ context.Context, placement alignedcmd.EquipmentPlacementRequest) error {
			if placement.ItemID != 2747155 || placement.SourceListType != listTypePet || placement.SourceSlotIndex != 140 || placement.TargetSlotIndex != 27 {
				t.Fatalf("placement = %+v", placement)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !result.Handled || !result.ResponseAllowed || len(result.UpperResponses) != 1 ||
		len(result.PostActions) != 1 ||
		result.PostActions[0] != alignedcmd.PostActionRefreshSelectedItemContainers ||
		len(result.ItemSlotRefreshes) != 0 {
		t.Fatalf("result = %+v", result)
	}
	wantACK := []byte{1, listTypePet, 0x8C, 0x00, 1, 0, 0, 0, listTypeActorWornAlt, 27, 0}
	if got := result.UpperResponses[0].Body; string(got) != string(wantACK) {
		t.Fatalf("ACK = % X, want % X", got, wantACK)
	}
	if _, occupied := loadTestInventory(t, ctx, repos, "77").Slots[slotKey(listTypePet, 140)]; occupied {
		t.Fatal("artifact list-7 source remained occupied")
	}
	equipment, ok, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !ok || equipment.Entries["27"].ItemID != 2747155 {
		t.Fatalf("equipment ok=%t err=%v record=%+v", ok, err, equipment)
	}
	petRecord, ok, err := repos.Pet.Load(ctx, "77")
	if err != nil || !ok || petRecord.Artifacts["red"].ItemID != 2747155 {
		t.Fatalf("pet projection ok=%t err=%v record=%+v", ok, err, petRecord)
	}
}
