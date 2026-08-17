// 本文件负责把当前 EXE 的 0x166 容器重建包从角色库存仓储动态构造出来。
package dnfbridge

import (
	"context"
	"encoding/binary"
	"sort"
	"strconv"
	"strings"

	dnfachievement "longheng.io/server/internal/modules/dnf/achievement"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type currentRequestOverseerRow struct {
	slot       uint16
	itemID     uint32
	amount     uint32
	packed     byte
	valueB     uint16
	createFlag byte
	valueA     uint32
	unused     byte
	valueC     byte
	valueD     uint16
}

type currentRequestOverseerChunk struct {
	refresh  byte
	selector uint16
	index    uint32
	rows     []currentRequestOverseerRow
}

type currentInsertOverseerRow struct {
	achievementID uint32
	p1            uint16
	p2            uint16
	p3            uint16
}

const (
	legacyAchievementChunksTable           = "legacy_character_achievement_chunks"
	legacyAchievementCompleteTable         = "legacy_character_achievement_complete"
	currentRequestOverseerFixedRowWireSize = 22
	currentRequestOverseerRawLenWireSize   = 4
	currentInsertOverseerRowWireSize       = 10
	currentInsertOverseerTailWireSize      = 16
)

func buildCurrentRequestOverseerBodyWithRows(refresh byte, selector uint16, listIndex uint32, rows []currentRequestOverseerRow) []byte {
	var writer packetWriter
	writer.writeByte(refresh)
	writer.writeUint16(selector)
	writer.writeUint32(listIndex)
	writer.writeUint32(uint32(len(rows)))
	for _, row := range rows {
		writer.writeUint16(row.slot)
		writer.writeUint32(row.itemID)
		writer.writeUint32(row.amount)
		writer.writeByte(row.packed)
		writer.writeUint16(row.valueB)
		writer.writeByte(row.createFlag)
		writer.writeUint32(row.valueA)
		writer.writeByte(row.unused)
		writer.writeByte(row.valueC)
		writer.writeUint16(row.valueD)
		// Current EXE sub_1D76D00 calls sub_3457C50 after every fixed
		// 22-byte row. sub_3457C50 always reads u32 raw_len followed by raw.
		// No repository owns that optional block yet, so encode its proved
		// empty state instead of truncating the row at the fixed fields.
		writer.writeUint32(0)
	}
	return writer.bytes()
}

func buildCurrentInsertOverseerBodyWithRows(rows []currentInsertOverseerRow) []byte {
	var writer packetWriter
	writer.writeUint32(uint32(len(rows)))
	for _, row := range rows {
		writer.writeUint32(row.achievementID)
		writer.writeUint16(row.p1)
		writer.writeUint16(row.p2)
		writer.writeUint16(row.p3)
	}
	// Current EXE sub_1D625C0 reads this fixed 16-byte block after all
	// count*10-byte rows, including when count is zero. Each nonzero dword is
	// treated as an object/achievement id; zero is the real empty state.
	writer.writeZero(currentInsertOverseerTailWireSize)
	return writer.bytes()
}

func parseCurrentRequestOverseerHeader(body []byte) (byte, uint16, uint32, bool) {
	if len(body) < 11 {
		return 0, 0, 0, false
	}
	return body[0], binary.LittleEndian.Uint16(body[1:3]), binary.LittleEndian.Uint32(body[3:7]), true
}

func (s *Service) buildCurrentRequestOverseerBodyForSession(ctx context.Context, session *gameSession, fallback []byte) []byte {
	refresh, selector, listIndex, ok := parseCurrentRequestOverseerHeader(fallback)
	if !ok || session == nil || session.selectedCharacterID == 0 {
		return fallback
	}
	repos, ok := s.repositoryGroup()
	if !ok {
		return fallback
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	rows := currentRequestOverseerRowsFromInventory(ctx, repos.Inventory, characterID, selector, listIndex)
	if len(rows) > 0 {
		body := buildCurrentRequestOverseerBodyWithRows(refresh, selector, listIndex, rows)
		s.logPacketEvent("game-upper-current-overseer-titlebook-page-built",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"msg_id", uint16(dnfenum.CmdPacketRequestOverseer),
			"selector", selector,
			"list_index", listIndex,
			"source", "inventory_title_book",
			"row_count", len(rows),
			"body_len", len(body))
		return body
	}
	if repos.LegacyUserInfo != nil {
		if chunk, ok := s.currentRequestOverseerChunkFromLegacyAchievements(ctx, session, repos.LegacyUserInfo, characterID, listIndex); ok {
			body := buildCurrentRequestOverseerBodyWithRows(chunk.refresh, chunk.selector, chunk.index, chunk.rows)
			s.logPacketEvent("game-upper-current-overseer-titlebook-page-built",
				"conn_id", session.connID,
				"char_id", session.selectedCharacterID,
				"msg_id", uint16(dnfenum.CmdPacketRequestOverseer),
				"selector", chunk.selector,
				"list_index", chunk.index,
				"source", legacyAchievementChunksTable,
				"row_count", len(chunk.rows),
				"body_len", len(body),
				"fixed_row_size", currentRequestOverseerFixedRowWireSize,
				"row_wire_size", currentRequestOverseerFixedRowWireSize+currentRequestOverseerRawLenWireSize,
				"raw_len", 0)
			return body
		}
	}
	body := buildCurrentRequestOverseerBodyWithRows(refresh, selector, listIndex, nil)
	s.logPacketEvent("game-upper-current-overseer-titlebook-page-empty",
		"conn_id", session.connID,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketRequestOverseer),
		"selector", selector,
		"list_index", listIndex,
		"source", "durable_title_book",
		"body_len", len(body))
	return body
}

func (s *Service) buildCurrentInsertOverseerBodyForSession(ctx context.Context, session *gameSession, fallback []byte) []byte {
	if session == nil || session.selectedCharacterID == 0 {
		return fallback
	}
	repos, ok := s.repositoryGroup()
	if !ok {
		return fallback
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	if owner, err := dnfachievement.NewOwner(repos, nil); err == nil {
		if removed, repairErr := owner.RepairLegacyRows(ctx, session.selectedCharacterID); repairErr != nil {
			s.logPacketEvent("game-upper-insert-overseer-legacy-progress-repair-failed",
				"conn_id", session.connID,
				"char_id", session.selectedCharacterID,
				"err", repairErr)
		} else if removed > 0 {
			s.logPacketEvent("game-upper-insert-overseer-legacy-progress-repaired",
				"conn_id", session.connID,
				"char_id", session.selectedCharacterID,
				"removed", removed)
		}
	}
	entries := make([]currentInsertOverseerRow, 0)
	seen := make(map[uint32]struct{})
	if repos.Quest != nil {
		record, found, err := repos.Quest.Load(ctx, characterID)
		if err != nil {
			s.logPacketEvent("game-upper-insert-overseer-achievement-load-failed",
				"conn_id", session.connID,
				"char_id", session.selectedCharacterID,
				"msg_id", uint16(dnfenum.CmdPacketInsertOverseer),
				"source", "quest_progress",
				"err", err)
			return fallback
		}
		if found {
			for _, progress := range dnfachievement.Snapshot(record) {
				id := uint32(progress.QuestID)
				entries = append(entries, currentInsertOverseerRow{
					achievementID: id,
					p1:            progress.Remain1,
					p2:            progress.Remain2,
					p3:            progress.Remain3,
				})
				seen[id] = struct{}{}
			}
		}
	}
	if repos.LegacyUserInfo != nil {
		rows, err := repos.LegacyUserInfo.SelectRows(ctx, characterID, legacyAchievementCompleteTable,
			[]string{"sort_order", "achievement_id", "p1", "p2", "p3"},
			[]string{"sort_order"})
		if err == nil {
			for _, row := range rows {
				achievementID, ok := currentRequestOverseerLegacyUint(row, "achievement_id", 32)
				if !ok {
					continue
				}
				id := uint32(achievementID)
				if _, duplicate := seen[id]; duplicate {
					continue
				}
				p1, _ := currentRequestOverseerLegacyUint(row, "p1", 16)
				p2, _ := currentRequestOverseerLegacyUint(row, "p2", 16)
				p3, _ := currentRequestOverseerLegacyUint(row, "p3", 16)
				entries = append(entries, currentInsertOverseerRow{
					achievementID: id,
					p1:            uint16(p1),
					p2:            uint16(p2),
					p3:            uint16(p3),
				})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].achievementID < entries[j].achievementID })
	body := buildCurrentInsertOverseerBodyWithRows(entries)
	s.logPacketEvent("game-upper-insert-overseer-achievement-built",
		"conn_id", session.connID,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketInsertOverseer),
		"source", "quest_progress",
		"row_count", len(entries),
		"body_len", len(body),
		"row_size", currentInsertOverseerRowWireSize,
		"tail_size", currentInsertOverseerTailWireSize)
	return body
}

func (s *Service) currentRequestOverseerChunkFromLegacyAchievements(ctx context.Context, session *gameSession, repo dnfrepo.LegacyUserInfoRepository, characterID string, listIndex uint32) (currentRequestOverseerChunk, bool) {
	if repo == nil {
		return currentRequestOverseerChunk{}, false
	}
	rows, err := repo.SelectRows(ctx, characterID, legacyAchievementChunksTable,
		[]string{"chunk_index", "mode_byte", "owner_id16", "entries_blob"},
		[]string{"chunk_index"})
	if err != nil {
		if session != nil {
			s.logPacketEvent("game-upper-current-overseer-titlebook-load-failed",
				"conn_id", session.connID,
				"char_id", session.selectedCharacterID,
				"msg_id", uint16(dnfenum.CmdPacketRequestOverseer),
				"list_index", listIndex,
				"source", legacyAchievementChunksTable,
				"err", err)
		}
		return currentRequestOverseerChunk{}, false
	}
	for _, row := range rows {
		chunkIndex, ok := currentRequestOverseerLegacyUint(row, "chunk_index", 32)
		if !ok || uint32(chunkIndex) != listIndex {
			continue
		}
		refresh, _ := currentRequestOverseerLegacyUint(row, "mode_byte", 8)
		selector, _ := currentRequestOverseerLegacyUint(row, "owner_id16", 16)
		entries, trailing := currentRequestOverseerRowsFromLegacyAchievementBlob(rowBytes(row, "entries_blob"))
		if trailing > 0 && session != nil {
			s.logPacketEvent("game-upper-current-overseer-titlebook-partial-entry-ignored",
				"conn_id", session.connID,
				"char_id", session.selectedCharacterID,
				"msg_id", uint16(dnfenum.CmdPacketRequestOverseer),
				"list_index", listIndex,
				"source", legacyAchievementChunksTable,
				"trailing_bytes", trailing)
		}
		return currentRequestOverseerChunk{
			refresh:  byte(refresh),
			selector: uint16(selector),
			index:    listIndex,
			rows:     entries,
		}, true
	}
	return currentRequestOverseerChunk{}, false
}

func currentRequestOverseerRowsFromLegacyAchievementBlob(blob []byte) ([]currentRequestOverseerRow, int) {
	const rowSize = currentRequestOverseerFixedRowWireSize
	if len(blob) == 0 {
		return nil, 0
	}
	count := len(blob) / rowSize
	rows := make([]currentRequestOverseerRow, 0, count)
	for offset := 0; offset+rowSize <= len(blob); offset += rowSize {
		raw := blob[offset : offset+rowSize]
		rows = append(rows, currentRequestOverseerRow{
			slot:       binary.LittleEndian.Uint16(raw[0:2]),
			itemID:     binary.LittleEndian.Uint32(raw[2:6]),
			amount:     binary.LittleEndian.Uint32(raw[6:10]),
			packed:     raw[10],
			valueB:     binary.LittleEndian.Uint16(raw[11:13]),
			createFlag: raw[13],
			valueA:     binary.LittleEndian.Uint32(raw[14:18]),
			unused:     raw[18],
			valueC:     raw[19],
			valueD:     binary.LittleEndian.Uint16(raw[20:22]),
		})
	}
	return rows, len(blob) % rowSize
}

func currentRequestOverseerRowsFromInventory(ctx context.Context, repo dnfrepo.InventoryRepository, characterID string, selector uint16, listIndex uint32) []currentRequestOverseerRow {
	if repo == nil || selector != 0 || listIndex >= 5 {
		return nil
	}
	record, found, err := repo.Load(ctx, characterID)
	if err != nil || !found {
		return nil
	}
	rows := make([]currentRequestOverseerRow, 0)
	for key, stack := range record.Slots {
		if stack.ItemID <= 0 || dnfachievement.IsLegacyInventoryProgress(key, stack) {
			continue
		}
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if !ok || listType != 100 || slot < 0 ||
			uint32(slot/currentTitleBookSlotBase) != listIndex {
			continue
		}
		rows = append(rows, currentRequestOverseerRow{
			slot:       uint16(slot % currentTitleBookSlotBase),
			itemID:     sceneInventoryUint32FromInt64(stack.ItemID),
			amount:     sceneInventoryStackAmount(stack),
			packed:     sceneInventoryExtraByte(stack.Extra, "packed_flag_byte", "packed_flag", "packed", "ext_data0", "ext0"),
			valueB:     sceneInventoryExtraUint16(stack.Extra, "value_b_word", "durability", "max_durability"),
			createFlag: sceneInventoryExtraByteDefault(stack.Extra, 1, "create_flag"),
			valueA:     sceneInventoryExtraUint32(stack.Extra, "value_a", "instance_value", "item_uid", "serial", "count_or_instance"),
			unused:     sceneInventoryExtraByte(stack.Extra, "unused_byte_after_value_a", "unused"),
			valueC:     sceneInventoryExtraByte(stack.Extra, "value_c"),
			valueD:     sceneInventoryExtraUint16(stack.Extra, "value_d"),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].slot < rows[j].slot })
	return rows
}

func (s *Service) currentRequestOverseerRowsFromLegacy(ctx context.Context, session *gameSession, repo dnfrepo.LegacyInventoryRepository, characterID string, selector uint16, listIndex uint32) []currentRequestOverseerRow {
	if repo == nil || selector != 0 || listIndex > 255 {
		return nil
	}
	items, err := repo.SelectItems(ctx, characterID, byte(listIndex))
	if err != nil {
		s.logPacketEvent("game-upper-current-overseer-legacy-inventory-load-failed",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"list_index", listIndex,
			"err", err)
		return nil
	}
	return currentRequestOverseerRowsFromLegacyItems(items)
}

func currentRequestOverseerRows(record dnfrepo.InventoryRecord, selector uint16, listIndex uint32) []currentRequestOverseerRow {
	if selector != 0 || listIndex > 255 {
		return nil
	}
	listType := byte(listIndex)
	rows := make([]currentRequestOverseerRow, 0)
	rows = append(rows, currentRequestOverseerRowsFromMap(record.Slots, listType)...)
	if listType == 2 {
		rows = append(rows, currentRequestOverseerRowsFromMap(record.Warehouse, listType)...)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].slot < rows[j].slot
	})
	return rows
}

func currentRequestOverseerRowsFromLegacyItems(items []dnfrepo.LegacyInventoryItem) []currentRequestOverseerRow {
	if len(items) == 0 {
		return nil
	}
	rows := make([]currentRequestOverseerRow, 0, len(items))
	for _, item := range items {
		if item.SlotIndex < 0 || item.ItemTemplateID <= 0 {
			continue
		}
		extra := item.Extra
		rows = append(rows, currentRequestOverseerRow{
			slot:       uint16(item.SlotIndex),
			itemID:     sceneInventoryUint32FromInt64(item.ItemTemplateID),
			amount:     sceneInventoryLegacyAmount(item),
			packed:     sceneInventoryLegacyPacked(item),
			valueB:     sceneInventoryLegacyUint16(item.Durability, extra, "value_b_word", "durability", "max_durability"),
			createFlag: sceneInventoryExtraByteDefault(extra, 1, "create_flag"),
			valueA:     sceneInventoryLegacyUint32(item.InstanceValue, extra, "value_a", "instance_value", "item_uid", "serial", "count_or_instance"),
			unused:     sceneInventoryExtraByte(extra, "unused_byte_after_value_a", "unused"),
			valueC:     sceneInventoryLegacyByte(item.OptionValue, extra, "value_c"),
			valueD:     sceneInventoryLegacyUint16(item.Marker16, extra, "value_d"),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].slot < rows[j].slot
	})
	return rows
}

func currentRequestOverseerRowsFromMap(items map[string]dnfrepo.ItemStack, listType byte) []currentRequestOverseerRow {
	if len(items) == 0 {
		return nil
	}
	rows := make([]currentRequestOverseerRow, 0, len(items))
	for key, stack := range items {
		keyListType, slot, ok := parseSceneInventorySlotKey(key)
		if !ok || keyListType != listType || slot < 0 || stack.ItemID <= 0 {
			continue
		}
		rows = append(rows, currentRequestOverseerRow{
			slot:       uint16(slot),
			itemID:     sceneInventoryUint32FromInt64(stack.ItemID),
			amount:     sceneInventoryStackAmount(stack),
			packed:     sceneInventoryExtraByte(stack.Extra, "packed_flag_byte", "packed_flag", "packed", "ext_data0", "ext0"),
			valueB:     sceneInventoryExtraUint16(stack.Extra, "value_b_word", "durability", "max_durability"),
			createFlag: sceneInventoryExtraByteDefault(stack.Extra, 1, "create_flag"),
			valueA:     sceneInventoryExtraUint32(stack.Extra, "value_a", "instance_value", "item_uid", "serial", "count_or_instance"),
			unused:     sceneInventoryExtraByte(stack.Extra, "unused_byte_after_value_a", "unused"),
			valueC:     sceneInventoryExtraByte(stack.Extra, "value_c"),
			valueD:     sceneInventoryExtraUint16(stack.Extra, "value_d"),
		})
	}
	return rows
}

func currentRequestOverseerLegacyUint(row dnfrepo.LegacyUserInfoRow, column string, bits int) (uint64, bool) {
	if row == nil {
		return 0, false
	}
	raw := strings.TrimSpace(row[column])
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseUint(raw, 0, bits)
	if err != nil {
		return 0, false
	}
	return value, true
}

func sceneInventoryLegacyAmount(item dnfrepo.LegacyInventoryItem) uint32 {
	if value, ok := sceneInventoryExtraUint(item.Extra, "amount_or_count", "amount", "count", "stack", "quantity"); ok {
		return sceneInventoryClampUint32(value)
	}
	if item.StackCount > 0 {
		return sceneInventoryUint32FromInt64(item.StackCount)
	}
	return 1
}

func sceneInventoryLegacyPacked(item dnfrepo.LegacyInventoryItem) byte {
	if value := sceneInventoryExtraByte(item.Extra, "packed_flag_byte", "packed_flag", "packed", "ext_data0", "ext0"); value != 0 {
		return value
	}
	if item.SealFlag > 0 && item.SealFlag <= 255 {
		return byte(item.SealFlag)
	}
	return 0
}

func sceneInventoryLegacyByte(fallback int64, extra map[string]string, keys ...string) byte {
	if value := sceneInventoryExtraByte(extra, keys...); value != 0 {
		return value
	}
	if fallback > 0 && fallback <= 255 {
		return byte(fallback)
	}
	return 0
}

func sceneInventoryLegacyUint16(fallback int64, extra map[string]string, keys ...string) uint16 {
	if value := sceneInventoryExtraUint16(extra, keys...); value != 0 {
		return value
	}
	if fallback > 0 && fallback <= 65535 {
		return uint16(fallback)
	}
	return 0
}

func sceneInventoryLegacyUint32(fallback int64, extra map[string]string, keys ...string) uint32 {
	if value := sceneInventoryExtraUint32(extra, keys...); value != 0 {
		return value
	}
	return sceneInventoryUint32FromInt64(fallback)
}

func parseSceneInventorySlotKey(key string) (byte, int16, bool) {
	listRaw, slotRaw, ok := strings.Cut(key, ":")
	if !ok {
		return 0, 0, false
	}
	listValue, err := strconv.ParseInt(strings.TrimSpace(listRaw), 10, 16)
	if err != nil || listValue < 0 || listValue > 255 {
		return 0, 0, false
	}
	slotValue, err := strconv.ParseInt(strings.TrimSpace(slotRaw), 10, 16)
	if err != nil || slotValue < 0 || slotValue > 32767 {
		return 0, 0, false
	}
	return byte(listValue), int16(slotValue), true
}

func sceneInventoryStackAmount(stack dnfrepo.ItemStack) uint32 {
	if value, ok := sceneInventoryExtraUint(stack.Extra, "amount_or_count", "amount", "count", "stack", "quantity"); ok {
		return sceneInventoryClampUint32(value)
	}
	if stack.Count > 0 {
		return sceneInventoryUint32FromInt64(stack.Count)
	}
	return 1
}

func sceneInventoryExtraByte(extra map[string]string, keys ...string) byte {
	return sceneInventoryExtraByteDefault(extra, 0, keys...)
}

func sceneInventoryExtraByteDefault(extra map[string]string, fallback byte, keys ...string) byte {
	value, ok := sceneInventoryExtraUint(extra, keys...)
	if !ok || value > 255 {
		return fallback
	}
	return byte(value)
}

func sceneInventoryExtraUint16(extra map[string]string, keys ...string) uint16 {
	value, ok := sceneInventoryExtraUint(extra, keys...)
	if !ok || value > 65535 {
		return 0
	}
	return uint16(value)
}

func sceneInventoryExtraUint32(extra map[string]string, keys ...string) uint32 {
	value, ok := sceneInventoryExtraUint(extra, keys...)
	if !ok {
		return 0
	}
	return sceneInventoryClampUint32(value)
}

func sceneInventoryExtraUint(extra map[string]string, keys ...string) (uint64, bool) {
	for _, key := range keys {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		value, err := strconv.ParseUint(raw, 0, 64)
		if err != nil {
			continue
		}
		return value, true
	}
	return 0, false
}

func sceneInventoryUint32FromInt64(value int64) uint32 {
	if value <= 0 {
		return 0
	}
	if value > int64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(value)
}

func sceneInventoryClampUint32(value uint64) uint32 {
	if value > uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(value)
}
