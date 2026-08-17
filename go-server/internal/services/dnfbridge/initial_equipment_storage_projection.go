package dnfbridge

import (
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func buildInitialEquipmentRawEntry(slot int16, itemID int64, durability uint16) []byte {
	var writer packetWriter
	// Current EXE constructors initialize the third equipment-detail scalar to one.
	// This stored summary is not a current mode1 sub_1D77560 create row.
	writer.writeByte(byte(slot))
	writer.writeUint32(uint32(itemID))
	writer.writeUint32(initialEquipmentCreateValue)
	writer.writeByte(0)
	writer.writeUint16(durability)
	writer.writeUint32(0)
	writer.writeUint32(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeUint16(0)
	// 初始穿戴槽 11/13/14...23 不走武器宝珠槽和宠物 extra 分支。
	writer.writeByte(0)
	writer.writeUint32(0)
	writer.writeByte(0)
	writer.writeUint16(0)
	writer.writeByte(0)
	writer.writeZero(10)
	return writer.bytes()
}

func initialEquipmentRecord(characterID string, entries []initialEquipmentEntry, now time.Time) dnfrepo.EquipmentRecord {
	record := dnfrepo.EquipmentRecord{
		CharacterID: strings.TrimSpace(characterID),
		Entries:     make(map[string]dnfrepo.EquipmentEntry, len(entries)),
		UpdatedAt:   now,
	}
	for _, entry := range entries {
		if entry.Slot <= 0 || entry.ItemID <= 0 {
			continue
		}
		durability := strconv.FormatUint(uint64(entry.Durability), 10)
		extra := map[string]string{
			"source":                   "pvf_create_equipment_list",
			"pvf_path":                 entry.PVFPath,
			"current_exe_create_value": strconv.FormatUint(initialEquipmentCreateValue, 10),
			"durability":               durability,
			"max_durability":           durability,
			"repair_gold":              "0",
		}
		if entry.EquipType > 0 {
			extra["equipment_type"] = strconv.FormatInt(entry.EquipType, 10)
		}
		addInitialEquipmentModelLayerExtra(extra, entry.ModelLayers)
		record.Entries[strconv.Itoa(int(entry.Slot))] = dnfrepo.EquipmentEntry{
			SlotIndex: entry.Slot,
			ItemID:    entry.ItemID,
			RawEntry:  append([]byte(nil), entry.RawEntry...),
			Extra:     extra,
		}
	}
	return record
}

func addInitialEquipmentModelLayerExtra(extra map[string]string, layers []initialEquipmentModelLayer) {
	if len(extra) == 0 || len(layers) == 0 {
		return
	}
	extra["model_layer_count"] = strconv.Itoa(len(layers))
	for idx, layer := range layers {
		prefix := "model_layer_" + strconv.Itoa(idx) + "_"
		extra[prefix+"key"] = strconv.Itoa(int(layer.Key))
		extra[prefix+"name"] = layer.Name
		if strings.TrimSpace(layer.Script) != "" {
			extra[prefix+"script"] = layer.Script
		}
	}
}
