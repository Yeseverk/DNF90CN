package repository

import (
	"testing"
)

func TestAuditCharacterItemsAcceptsCurrentThreePieceShape(t *testing.T) {
	inventory := InventoryRecord{
		CharacterID: "27",
		Slots: map[string]ItemStack{
			"0:1": {ItemID: 1, Count: 0},
		},
	}
	equipment := EquipmentRecord{
		CharacterID: "27",
		Entries: map[string]EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 101030782, RawEntry: []byte{1, 2, 3}},
			"13": {SlotIndex: 13, ItemID: 10400, RawEntry: []byte{4, 5, 6}},
			"15": {SlotIndex: 15, ItemID: 12400, RawEntry: []byte{7, 8, 9}},
		},
	}

	audit := AuditCharacterItems(CharacterItemAuditInput{
		CharacterID: "27",
		Inventory:   &inventory,
		Equipment:   &equipment,
	})
	if !audit.ItemRowsReady || audit.ErrorCount() != 0 {
		t.Fatalf("audit not ready: %+v", audit)
	}
	if audit.ContainerCapacityKnown {
		t.Fatal("live JSON unexpectedly proves container capacity")
	}
	if len(audit.Candidates) != 4 {
		t.Fatalf("candidate count = %d, want 4", len(audit.Candidates))
	}
	if len(audit.Issues) != 1 || audit.Issues[0].Code != "zero_count" || audit.Issues[0].Severity != ItemAuditWarning {
		t.Fatalf("issues = %+v", audit.Issues)
	}
	for _, candidate := range audit.Candidates {
		if !candidate.NeedsItemUID {
			t.Fatalf("candidate inferred a stable UID: %+v", candidate)
		}
	}
}

func TestAuditCharacterItemsAcceptsTypedCurrentExeContainerState(t *testing.T) {
	inventory := InventoryRecord{CharacterID: "27"}
	equipment := EquipmentRecord{CharacterID: "27"}
	container := CharacterContainerState{
		CharacterID:            "27",
		MainSlotCount:          24,
		AvatarExpansion:        0,
		PersonalCargoSlotCount: 8,
	}
	audit := AuditCharacterItems(CharacterItemAuditInput{
		CharacterID: "27",
		Inventory:   &inventory,
		Equipment:   &equipment,
		Container:   &container,
	})
	if !audit.ItemRowsReady || !audit.ContainerHeadersKnown || !audit.ContainerCapacityKnown {
		t.Fatalf("container-aware audit = %+v", audit)
	}
}

func TestAuditCharacterItemsDetectsOwnershipAndLocationConflicts(t *testing.T) {
	inventory := InventoryRecord{
		CharacterID: "27",
		Slots: map[string]ItemStack{
			"3:11":   {ItemID: 700, Count: 1, RawEntry: []byte{1}},
			"broken": {ItemID: 701, Count: 1},
		},
		Warehouse: map[string]ItemStack{
			"0:2": {ItemID: 702, Count: 1},
		},
	}
	equipment := EquipmentRecord{
		CharacterID: "27",
		Entries: map[string]EquipmentEntry{
			"11": {SlotIndex: 12, ItemID: 800, RawEntry: []byte{2}},
		},
	}

	audit := AuditCharacterItems(CharacterItemAuditInput{
		CharacterID: "27",
		Inventory:   &inventory,
		Equipment:   &equipment,
	})
	if audit.ItemRowsReady || audit.ErrorCount() < 5 {
		t.Fatalf("conflicting audit unexpectedly ready: %+v", audit)
	}
	for _, code := range []string{
		"duplicate_location",
		"equipment_item_in_inventory_map",
		"equipment_slot_mismatch",
		"invalid_location_key",
		"non_personal_cargo_in_warehouse_map",
	} {
		if !auditHasIssue(audit, code) {
			t.Fatalf("audit missing %s: %+v", code, audit.Issues)
		}
	}
}

func TestAuditCharacterItemsRecoversLegacyRawWithoutTreatingItAsUID(t *testing.T) {
	inventory := InventoryRecord{CharacterID: "77"}
	equipment := EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    700,
				Extra:     map[string]string{"raw_entry_hex": "01 02 03", "instance_value": "999999998"},
			},
		},
	}

	audit := AuditCharacterItems(CharacterItemAuditInput{Inventory: &inventory, Equipment: &equipment})
	if !audit.ItemRowsReady || audit.ErrorCount() != 0 {
		t.Fatalf("legacy-compatible audit not ready: %+v", audit)
	}
	if len(audit.Candidates) != 1 || !audit.Candidates[0].LegacyRawUsed || len(audit.Candidates[0].RawEntry) != 3 {
		t.Fatalf("candidate = %+v", audit.Candidates)
	}
	if !audit.Candidates[0].NeedsItemUID {
		t.Fatal("instance_value was incorrectly promoted to stable item UID")
	}
	if !auditHasIssue(audit, "legacy_raw_entry_used") {
		t.Fatalf("issues = %+v", audit.Issues)
	}
}

func TestAuditCharacterItemsRejectsMissingAggregateRows(t *testing.T) {
	audit := AuditCharacterItems(CharacterItemAuditInput{CharacterID: "99"})
	if audit.ItemRowsReady || audit.ErrorCount() != 2 {
		t.Fatalf("missing rows audit = %+v", audit)
	}
	if !auditHasIssue(audit, "inventory_record_missing") || !auditHasIssue(audit, "equipment_record_missing") {
		t.Fatalf("issues = %+v", audit.Issues)
	}
}

func auditHasIssue(audit CharacterItemAudit, code string) bool {
	for _, issue := range audit.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
