package repository

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ItemAuditSeverity string

const (
	ItemAuditWarning ItemAuditSeverity = "warning"
	ItemAuditError   ItemAuditSeverity = "error"
)

type ItemLocationSource string

const (
	ItemSourceInventory ItemLocationSource = "inventory"
	ItemSourceWarehouse ItemLocationSource = "warehouse"
	ItemSourceEquipment ItemLocationSource = "equipment"
	ItemSourceContainer ItemLocationSource = "container_state"
)

// CharacterItemAuditInput contains the two live JSON aggregates for one
// character. Nil records mean the aggregate row is missing, not merely empty.
type CharacterItemAuditInput struct {
	CharacterID string
	Inventory   *InventoryRecord
	Equipment   *EquipmentRecord
	Container   *CharacterContainerState
}

// NormalizedItemCandidate is a read-only projection for a future Go-owned
// item table. Stable item UIDs are deliberately not inferred from wire fields.
type NormalizedItemCandidate struct {
	CharacterID   string
	Source        ItemLocationSource
	LegacyKey     string
	ListType      byte
	SlotIndex     int16
	ItemID        int64
	Count         int64
	Bind          bool
	ExpireAt      time.Time
	RawEntry      []byte
	Extra         map[string]string
	NeedsItemUID  bool
	LegacyRawUsed bool
}

type CharacterItemAuditIssue struct {
	Severity ItemAuditSeverity
	Code     string
	Source   ItemLocationSource
	Key      string
	Detail   string
}

type CharacterItemAudit struct {
	CharacterID            string
	Candidates             []NormalizedItemCandidate
	Issues                 []CharacterItemAuditIssue
	ItemRowsReady          bool
	ContainerHeadersKnown  bool
	ContainerCapacityKnown bool
}

func (a CharacterItemAudit) ErrorCount() int {
	count := 0
	for _, issue := range a.Issues {
		if issue.Severity == ItemAuditError {
			count++
		}
	}
	return count
}

// AuditCharacterItems validates current JSON ownership and projects item rows.
// It never allocates UIDs, mutates records, or fills missing container capacity.
func AuditCharacterItems(input CharacterItemAuditInput) CharacterItemAudit {
	audit := CharacterItemAudit{CharacterID: strings.TrimSpace(input.CharacterID)}
	if audit.CharacterID == "" {
		audit.CharacterID = firstRecordCharacterID(input.Inventory, input.Equipment)
	}
	if input.Inventory == nil {
		audit.addIssue(ItemAuditError, "inventory_record_missing", ItemSourceInventory, "", "live inventory aggregate row is missing")
	} else {
		audit.checkRecordCharacterID(input.Inventory.CharacterID, ItemSourceInventory)
		audit.addInventoryMap(input.Inventory.Slots, ItemSourceInventory)
		audit.addInventoryMap(input.Inventory.Warehouse, ItemSourceWarehouse)
	}
	if input.Equipment == nil {
		audit.addIssue(ItemAuditError, "equipment_record_missing", ItemSourceEquipment, "", "live equipment aggregate row is missing")
	} else {
		audit.checkRecordCharacterID(input.Equipment.CharacterID, ItemSourceEquipment)
		audit.addEquipmentMap(input.Equipment.Entries)
	}
	if input.Container != nil {
		audit.addContainerState(*input.Container)
	}

	audit.detectDuplicateLocations()
	sort.Slice(audit.Candidates, func(i, j int) bool {
		left, right := audit.Candidates[i], audit.Candidates[j]
		if left.ListType != right.ListType {
			return left.ListType < right.ListType
		}
		if left.SlotIndex != right.SlotIndex {
			return left.SlotIndex < right.SlotIndex
		}
		return left.Source < right.Source
	})
	sort.SliceStable(audit.Issues, func(i, j int) bool {
		left, right := audit.Issues[i], audit.Issues[j]
		if left.Severity != right.Severity {
			return left.Severity == ItemAuditError
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		return left.Key < right.Key
	})
	audit.ItemRowsReady = audit.ErrorCount() == 0
	return audit
}

func (a *CharacterItemAudit) addContainerState(state CharacterContainerState) {
	stateCharacterID := strings.TrimSpace(state.CharacterID)
	if stateCharacterID != "" && a.CharacterID != "" && stateCharacterID != a.CharacterID {
		a.addIssue(ItemAuditError, "character_id_mismatch", ItemSourceContainer, "", fmt.Sprintf("audit=%q record=%q", a.CharacterID, stateCharacterID))
		return
	}
	if !currentMainInventoryExpansion(state.MainSlotCount) {
		a.addIssue(ItemAuditError, "invalid_main_slot_count", ItemSourceContainer, "main", fmt.Sprintf("slot_count=%d", state.MainSlotCount))
		return
	}
	if !currentPersonalCargoSlotCount(state.PersonalCargoSlotCount) {
		a.addIssue(ItemAuditError, "invalid_personal_cargo_slot_count", ItemSourceContainer, "personal_cargo", fmt.Sprintf("slot_count=%d", state.PersonalCargoSlotCount))
		return
	}
	a.ContainerHeadersKnown = true
	a.ContainerCapacityKnown = true
}

func (a *CharacterItemAudit) addInventoryMap(items map[string]ItemStack, source ItemLocationSource) {
	for _, key := range sortedItemKeys(items) {
		stack := items[key]
		listType, slot, ok := parseAuditLocationKey(key)
		if !ok {
			a.addIssue(ItemAuditError, "invalid_location_key", source, key, "expected list_type:slot_index")
			continue
		}
		if !currentItemListType(listType) {
			a.addIssue(ItemAuditError, "unknown_list_type", source, key, fmt.Sprintf("list_type=%d", listType))
		}
		switch source {
		case ItemSourceInventory:
			if listType == 2 || listType == 12 {
				a.addIssue(ItemAuditError, "warehouse_item_in_inventory_map", source, key, fmt.Sprintf("list_type=%d", listType))
			}
			if listType == 3 {
				a.addIssue(ItemAuditError, "equipment_item_in_inventory_map", source, key, "equipped items belong to the equipment aggregate")
			}
		case ItemSourceWarehouse:
			if listType != 2 {
				a.addIssue(ItemAuditError, "non_personal_cargo_in_warehouse_map", source, key, fmt.Sprintf("list_type=%d", listType))
			}
		}
		if stack.ItemID <= 0 {
			a.addIssue(ItemAuditError, "invalid_item_id", source, key, fmt.Sprintf("item_id=%d", stack.ItemID))
		}
		if stack.Count < 0 {
			a.addIssue(ItemAuditError, "negative_count", source, key, fmt.Sprintf("count=%d", stack.Count))
		} else if stack.Count == 0 {
			a.addIssue(ItemAuditWarning, "zero_count", source, key, "zero-count wallet or placeholder state requires explicit normalized semantics")
		}
		raw, legacyRaw, rawIssue := auditRawEntry(stack.RawEntry, stack.Extra)
		if rawIssue != "" {
			a.addIssue(ItemAuditError, "invalid_legacy_raw_entry", source, key, rawIssue)
		}
		if legacyRaw {
			a.addIssue(ItemAuditWarning, "legacy_raw_entry_used", source, key, "raw bytes were recovered from Extra instead of typed RawEntry")
		}
		a.Candidates = append(a.Candidates, NormalizedItemCandidate{
			CharacterID:   a.CharacterID,
			Source:        source,
			LegacyKey:     key,
			ListType:      listType,
			SlotIndex:     slot,
			ItemID:        stack.ItemID,
			Count:         stack.Count,
			Bind:          stack.Bind,
			ExpireAt:      stack.ExpireAt,
			RawEntry:      raw,
			Extra:         cloneStringMap(stack.Extra),
			NeedsItemUID:  true,
			LegacyRawUsed: legacyRaw,
		})
	}
}

func (a *CharacterItemAudit) addEquipmentMap(entries map[string]EquipmentEntry) {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry := entries[key]
		slot64, err := strconv.ParseInt(strings.TrimSpace(key), 10, 16)
		if err != nil {
			a.addIssue(ItemAuditError, "invalid_equipment_key", ItemSourceEquipment, key, "expected numeric worn slot")
			continue
		}
		slot := int16(slot64)
		if entry.SlotIndex != slot {
			a.addIssue(ItemAuditError, "equipment_slot_mismatch", ItemSourceEquipment, key, fmt.Sprintf("key_slot=%d entry_slot=%d", slot, entry.SlotIndex))
		}
		if entry.ItemID <= 0 {
			a.addIssue(ItemAuditError, "invalid_item_id", ItemSourceEquipment, key, fmt.Sprintf("item_id=%d", entry.ItemID))
		}
		raw, legacyRaw, rawIssue := auditRawEntry(entry.RawEntry, entry.Extra)
		if rawIssue != "" {
			a.addIssue(ItemAuditError, "invalid_legacy_raw_entry", ItemSourceEquipment, key, rawIssue)
		}
		if len(raw) == 0 {
			a.addIssue(ItemAuditError, "equipment_raw_entry_missing", ItemSourceEquipment, key, "current equipment projection requires proven raw state")
		}
		if legacyRaw {
			a.addIssue(ItemAuditWarning, "legacy_raw_entry_used", ItemSourceEquipment, key, "raw bytes were recovered from Extra instead of typed RawEntry")
		}
		a.Candidates = append(a.Candidates, NormalizedItemCandidate{
			CharacterID:   a.CharacterID,
			Source:        ItemSourceEquipment,
			LegacyKey:     key,
			ListType:      3,
			SlotIndex:     slot,
			ItemID:        entry.ItemID,
			Count:         1,
			Bind:          entry.Bind,
			ExpireAt:      entry.ExpireAt,
			RawEntry:      raw,
			Extra:         cloneStringMap(entry.Extra),
			NeedsItemUID:  true,
			LegacyRawUsed: legacyRaw,
		})
	}
}

func (a *CharacterItemAudit) detectDuplicateLocations() {
	seen := make(map[string]NormalizedItemCandidate, len(a.Candidates))
	for _, candidate := range a.Candidates {
		key := fmt.Sprintf("%d:%d", candidate.ListType, candidate.SlotIndex)
		previous, ok := seen[key]
		if !ok {
			seen[key] = candidate
			continue
		}
		a.addIssue(ItemAuditError, "duplicate_location", candidate.Source, candidate.LegacyKey, fmt.Sprintf("location=%s already owned by %s/%s", key, previous.Source, previous.LegacyKey))
	}
}

func (a *CharacterItemAudit) checkRecordCharacterID(recordID string, source ItemLocationSource) {
	recordID = strings.TrimSpace(recordID)
	if a.CharacterID == "" {
		a.CharacterID = recordID
		return
	}
	if recordID != a.CharacterID {
		a.addIssue(ItemAuditError, "character_id_mismatch", source, "", fmt.Sprintf("audit=%q record=%q", a.CharacterID, recordID))
	}
}

func (a *CharacterItemAudit) addIssue(severity ItemAuditSeverity, code string, source ItemLocationSource, key string, detail string) {
	a.Issues = append(a.Issues, CharacterItemAuditIssue{
		Severity: severity,
		Code:     code,
		Source:   source,
		Key:      key,
		Detail:   detail,
	})
}

func firstRecordCharacterID(inventory *InventoryRecord, equipment *EquipmentRecord) string {
	if inventory != nil && strings.TrimSpace(inventory.CharacterID) != "" {
		return strings.TrimSpace(inventory.CharacterID)
	}
	if equipment != nil {
		return strings.TrimSpace(equipment.CharacterID)
	}
	return ""
}

func sortedItemKeys(items map[string]ItemStack) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func parseAuditLocationKey(key string) (byte, int16, bool) {
	listRaw, slotRaw, ok := strings.Cut(strings.TrimSpace(key), ":")
	if !ok {
		return 0, 0, false
	}
	listValue, err := strconv.ParseUint(strings.TrimSpace(listRaw), 10, 8)
	if err != nil {
		return 0, 0, false
	}
	slotValue, err := strconv.ParseInt(strings.TrimSpace(slotRaw), 10, 16)
	if err != nil {
		return 0, 0, false
	}
	return byte(listValue), int16(slotValue), true
}

func currentItemListType(listType byte) bool {
	switch listType {
	case 0, 1, 2, 3, 7, 12, 38:
		return true
	default:
		return false
	}
}

func auditRawEntry(rawEntry []byte, extra map[string]string) ([]byte, bool, string) {
	if len(rawEntry) > 0 {
		return append([]byte(nil), rawEntry...), false, ""
	}
	if len(extra) == 0 {
		return nil, false, ""
	}
	for _, key := range []string{"raw_entry_hex", "equipment_raw_entry_hex", "current_raw_entry_77", "raw_entry_77"} {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		raw = strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", "0x", "", "0X", "").Replace(raw)
		if len(raw)%2 != 0 {
			return nil, true, fmt.Sprintf("%s has odd hex length", key)
		}
		decoded, err := hex.DecodeString(raw)
		if err != nil || len(decoded) == 0 {
			return nil, true, fmt.Sprintf("%s is not valid non-empty hex", key)
		}
		return decoded, true, ""
	}
	return nil, false, ""
}
