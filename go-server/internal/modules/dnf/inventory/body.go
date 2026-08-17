// 本文件负责背包类命令的旧客户端回包 body 编码。
// 这里只写 wire 字节顺序，不做背包、金币、宠物或装备业务判定。
package inventory

import (
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	msgItemListRefresh uint16 = 0x000D
	msgItemListUpdate  uint16 = 0x000E
	msgUpgradeNotice   uint16 = 0x0056

	// premiumActivatedNotifyMsgID is the NOTI 0x0042 premium-activated
	// notification emitted after a contract item activation (86JP
	// PremiumService.ActivateAndNotify).
	premiumActivatedNotifyMsgID uint16 = 0x0042

	currentItemListEntrySize        = 0x77
	currentPetRemainSecondsOffset   = 0x16
	currentItemListExpireTimeOffset = 0x38
	legacyWrongItemExpireTimeOffset = 0x2B
)

type packetWriter struct {
	data []byte
}

func (w *packetWriter) writeByte(value byte) {
	w.data = append(w.data, value)
}

func (w *packetWriter) writeUint16(value uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], value)
	w.data = append(w.data, buf[:]...)
}

func (w *packetWriter) writeInt16(value int16) {
	w.writeUint16(uint16(value))
}

func (w *packetWriter) writeInt32(value int32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(value))
	w.data = append(w.data, buf[:]...)
}

func (w *packetWriter) writeBytes(value []byte) {
	w.data = append(w.data, value...)
}

func (w *packetWriter) bytes() []byte {
	return append([]byte(nil), w.data...)
}

func buildDeleteItemAck(items []DeletedItem) []byte {
	if len(items) == 0 {
		return nil
	}
	protobuf := protowire.AppendTag(nil, 1, protowire.VarintType)
	protobuf = protowire.AppendVarint(protobuf, uint64(items[0].ListType))
	for _, item := range items {
		nested := protowire.AppendTag(nil, 1, protowire.VarintType)
		nested = protowire.AppendVarint(nested, uint64(item.SlotIndex))
		nested = protowire.AppendTag(nested, 2, protowire.VarintType)
		nested = protowire.AppendVarint(nested, uint64(clampInt32(item.AppliedCount)))
		nested = protowire.AppendTag(nested, 3, protowire.VarintType)
		nested = protowire.AppendVarint(nested, 0)
		protobuf = protowire.AppendTag(protobuf, 2, protowire.BytesType)
		protobuf = protowire.AppendBytes(protobuf, nested)
	}
	protobuf = protowire.AppendTag(protobuf, 3, protowire.VarintType)
	protobuf = protowire.AppendVarint(protobuf, 0)
	return buildCurrentProtobufAck(protobuf)
}

func buildMoveItemspaceSuccessAck(cmd Command, moveValue int32) []byte {
	// Current class1/op19 dispatch registers sub_1D2DE80. The class1 result
	// marker is consumed before the handler reads these remaining ten bytes.
	var writer packetWriter
	writer.writeByte(1)
	writer.writeByte(cmd.SourceListType)
	writer.writeInt16(cmd.SourceSlotIndex)
	writer.writeInt32(moveValue)
	writer.writeByte(cmd.DestinationListType)
	writer.writeInt16(cmd.DestinationSlotIndex)
	return writer.bytes()
}

func buildUseStackableSuccessAck(result UseStackableResult) []byte {
	// Current class1/op44 handler sub_1D14380 consumes the result byte before
	// reading u16 slot, u8 list, u32 instance value, and u32 item id.
	var writer packetWriter
	writer.writeByte(1)
	writer.writeInt16(result.SlotIndex)
	writer.writeByte(result.ListType)
	writer.writeInt32(result.InstanceValue)
	writer.writeInt32(int32(result.ItemID))
	return writer.bytes()
}

func buildUseStackableActionSuccessAck(result DamageFontUnlockResult) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeInt16(result.SourceSlotIndex)
	writer.writeInt16(0)
	writer.writeInt16(int16(result.RemainingCount))
	writer.writeInt16(0)
	writer.writeByte(1)
	return writer.bytes()
}

func buildSelectDamageFontSuccessAck(fontIndex uint16) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeUint16(fontIndex)
	return writer.bytes()
}

func buildSelectDamageFontErrorAck(errorCode byte) []byte {
	return []byte{0, errorCode}
}

// buildSortItemAck 对齐 C# SortItemAckBuilder；handler 只在后续 0x000D 列表刷新可生成时使用。
func buildSortItemAck(listType byte) []byte {
	protobuf := protowire.AppendTag(nil, 1, protowire.VarintType)
	protobuf = protowire.AppendVarint(protobuf, uint64(listType))
	protobuf = protowire.AppendTag(protobuf, 2, protowire.VarintType)
	protobuf = protowire.AppendVarint(protobuf, 0)
	return buildCurrentProtobufAck(protobuf)
}

func buildCurrentProtobufAck(protobuf []byte) []byte {
	body := make([]byte, 5, 5+len(protobuf))
	body[0] = 1
	binary.LittleEndian.PutUint32(body[1:5], uint32(len(protobuf)))
	return append(body, protobuf...)
}

// buildCommonItemListRefreshBody 生成 Main/PersonalCargo 的 0x000D 全量列表刷新体。
// 当前 NoPack EXE 的 op13/op14 handler 读取 0x77 字节 entry。
func buildCommonItemListRefreshBodyWithState(listType byte, slots map[string]dnfrepo.ItemStack, state dnfrepo.CharacterContainerState) []byte {
	entries := commonItemListEntries(listType, slots)
	var writer packetWriter
	writer.writeByte(listType)
	switch listType {
	case listTypeMain:
		writer.writeUint16(state.MainSlotCount)
	case listTypeAvatar:
		writer.writeUint16(state.AvatarExpansion)
	case listTypePersonalCargo:
		writer.writeUint16(state.PersonalCargoSlotCount)
	default:
		writer.writeUint16(0)
	}
	writer.writeUint16(uint16(len(entries)))
	for _, entry := range entries {
		writeCommonItemListEntry(&writer, entry.slot, entry.stack)
		if listType == listTypeAvatar {
			// list1 appends two length-prefixed optional blobs after every
			// fixed 0x77-byte row. Their payload formats are not yet owned;
			// two zero lengths preserve the current constructor defaults.
			writer.writeInt32(0)
			writer.writeInt32(0)
		}
	}
	if listType == listTypePersonalCargo {
		// Current sub_1D72380 requires a final u8 groupCount followed by
		// groupCount*raw[8]. The repository has no group owner yet, so emit
		// the explicit empty terminator instead of leaving the reader short.
		writer.writeByte(0)
	}
	return writer.bytes()
}

// buildCommonItemListUpdateBody encodes current class0/op14: list type,
// changed-row count, then fixed 0x77-byte current item rows.
func buildCommonItemListUpdateBody(listType byte, entries []commonItemListEntry) []byte {
	var writer packetWriter
	writer.writeByte(listType)
	writer.writeUint16(uint16(len(entries)))
	for _, entry := range entries {
		writeCommonItemListEntry(&writer, entry.slot, entry.stack)
	}
	return writer.bytes()
}

func buildPetItemListUpdateBody(entries []commonItemListEntry) []byte {
	var writer packetWriter
	writer.writeByte(listTypePet)
	writer.writeUint16(uint16(len(entries)))
	for _, entry := range entries {
		writePetItemListEntry(&writer, entry.slot, entry.stack)
	}
	return writer.bytes()
}

func currentRawEntryForStack(slot int16, stack dnfrepo.ItemStack) []byte {
	var writer packetWriter
	writeCommonItemListEntry(&writer, slot, stack)
	return writer.bytes()
}

// buildPetItemListRefreshBody 生成 Pet 背包的 0x000D 全量列表刷新体。
// 当前 NoPack EXE 的 pet list 同样读取 0x77 字节 entry。
func buildPetItemListRefreshBody(slots map[string]dnfrepo.ItemStack) []byte {
	entries := commonItemListEntries(listTypePet, slots)
	var writer packetWriter
	writer.writeByte(listTypePet)
	writer.writeUint16(uint16(len(entries)))
	for _, entry := range entries {
		writePetItemListEntry(&writer, entry.slot, entry.stack)
	}
	return writer.bytes()
}

// BuildPetItemListRefreshBody exposes the single current-EXE list-7 encoder
// to the pet owner. Keeping one 0x77-row implementation prevents hatch and
// normal inventory refreshes from drifting to different wire layouts.
func BuildPetItemListRefreshBody(slots map[string]dnfrepo.ItemStack) []byte {
	return buildPetItemListRefreshBody(slots)
}

func buildSellItemAck(item DeletedItem, updatedGold int64) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeInt32(clampInt32(updatedGold))
	writer.writeByte(item.ListType)
	writer.writeInt16(item.SlotIndex)
	writer.writeInt16(clampInt16(item.AppliedCount))
	return writer.bytes()
}

// buildRepairEquipmentAck 对齐 C# RepairEquipmentAckBuilder：成功标记、当前金币、原 invenType、slot 和尾部 u16。
func buildRepairEquipmentAck(invenType byte, slotIndex int16, updatedGold int64) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeInt32(clampInt32(updatedGold))
	writer.writeByte(invenType)
	writer.writeInt16(slotIndex)
	writer.writeInt16(0)
	return writer.bytes()
}

func buildUpgradeItemSuccessAck(result UpgradeResult) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeByte(upgradeModeByte(result.Mode))
	writer.writeInt16(result.MaterialSlotIndex)
	writer.writeInt32(clampInt32(result.MaterialRemainingStackCount))
	writer.writeInt16(result.OptionalTicketSlotIndex)
	writer.writeByte(0)
	writer.writeByte(result.OldLevel)
	writer.writeByte(result.ResultCode)
	writer.writeByte(result.NewLevel)
	writer.writeByte(0)
	writer.writeInt16(result.TargetSlotIndex)
	writer.writeInt16(result.OptionalTicketSlotIndex)
	if result.ResultCode == upgradeResultCodeDestroy && result.DestroyBonusItemID > 0 {
		writer.writeByte(1)
		writer.writeInt16(result.DestroyBonusSlot)
		writer.writeInt32(result.DestroyBonusItemID)
		writer.writeInt32(result.DestroyBonusCount)
	} else if result.ResultCode == upgradeResultCodeDestroy {
		writer.writeByte(0)
	}
	return writer.bytes()
}

func buildUpgradeItemErrorAck(errorCode byte) []byte {
	return []byte{0, errorCode}
}

// buildUpgradeNoticeBody builds NOTI 0x0056 subtype 1 upgrade announcement.
// The current client resolves userID against the active town actor table and
// suppresses the chat message when that lookup fails.
// Layout: u8 subtype, u8 successFlag, u16 userID, i32 itemID, u8 level.
func buildUpgradeNoticeBody(success bool, userID uint16, itemID int32, level byte) []byte {
	var writer packetWriter
	writer.writeByte(1) // subtype
	if success {
		writer.writeByte(1)
	} else {
		writer.writeByte(0)
	}
	writer.writeUint16(userID)
	writer.writeInt32(itemID)
	writer.writeByte(level)
	return writer.bytes()
}

// buildUpgradeTicketSuccessAck matches the 86JP ItemUpgradeAckBuilder layout
// consumed by the current EXE op50 handler: u8 0x01, u8 mode, i16 material
// slot, i32 material remaining, i16 optional ticket slot (always -1), u8 0,
// u8 old level, u8 result code, u8 new level, u8 0, i16 target slot, i16
// optional ticket slot (always -1).
func buildUpgradeTicketSuccessAck(result UpgradeTicketResult) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeByte(upgradeModeByte(result.Mode))
	writer.writeInt16(result.MaterialSlotIndex)
	writer.writeInt32(clampInt32(result.MaterialRemainingStackCount))
	writer.writeInt16(-1)
	writer.writeByte(0)
	writer.writeByte(result.OldLevel)
	writer.writeByte(result.ResultCode)
	writer.writeByte(result.NewLevel)
	writer.writeByte(0)
	writer.writeInt16(result.TargetSlotIndex)
	writer.writeInt16(-1)
	return writer.bytes()
}

func buildUpgradeTicketErrorAck(errorCode byte) []byte {
	return []byte{0, errorCode}
}

// buildEnchantByBeadSuccessAck matches the current EXE S2C 0x0110 success
// reader (sub_1D0B960): u8 nonzero flag, u8 target list type, i16 target
// slot. The reference 86JP server emits the identical four-byte body.
func buildEnchantByBeadSuccessAck(result EnchantResult) []byte {
	return []byte{
		1,
		result.TargetListType,
		byte(result.TargetSlotIndex),
		byte(result.TargetSlotIndex >> 8),
	}
}

func buildEnchantByBeadErrorAck(errorCode byte) []byte {
	return []byte{0, errorCode}
}

func upgradeModeByte(mode string) byte {
	if strings.EqualFold(strings.TrimSpace(mode), "amplify") {
		return 1
	}
	return 0
}

type commonItemListEntry struct {
	slot  int16
	stack dnfrepo.ItemStack
}

func commonItemListEntries(listType byte, slots map[string]dnfrepo.ItemStack) []commonItemListEntry {
	entries := make([]commonItemListEntry, 0, len(slots))
	for key, stack := range slots {
		keyListType, slot, ok := parseSlotKey(key)
		if !ok || keyListType != listType {
			continue
		}
		entries = append(entries, commonItemListEntry{slot: slot, stack: stack})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].slot < entries[j].slot
	})
	return entries
}

func writeCommonItemListEntry(writer *packetWriter, slot int16, stack dnfrepo.ItemStack) {
	extra := stack.Extra
	entry := makeCurrentItemListEntry(
		slot,
		stack.ItemID,
		firstExtraInt(extra, stack.Count, "amount_or_count", "amount", "count", "stack", "quantity"),
		stack.RawEntry,
	)
	entry[0x0A] = clampByte(firstExtraInt(extra, 0, "ext_data0", "ext0", "packed_flag_byte", "packed_flag", "packed"))
	binary.LittleEndian.PutUint16(entry[0x0B:0x0D], clampUint16(firstExtraInt(extra, 0, "durability", "max_durability")))
	entry[0x0D] = clampByte(firstExtraInt(extra, boolInt64(stack.Bind), "seal_flag", "seal", "bind_flag", "bind"))
	binary.LittleEndian.PutUint32(entry[0x0E:0x12], uint32(clampInt32(commonItemListStackValueA(stack))))
	entry[0x12] = clampByte(firstExtraInt(extra, 0, "byte_12", "value_12"))
	entry[0x13] = clampByte(firstExtraInt(extra, 0, "byte_13", "value_13", "value_c"))
	binary.LittleEndian.PutUint16(entry[0x14:0x16], clampUint16(firstExtraInt(extra, 0, "marker_16", "marker16", "value_d")))
	if expire := itemListStackExpireUnix(stack); expire != 0 {
		if binary.LittleEndian.Uint32(entry[legacyWrongItemExpireTimeOffset:legacyWrongItemExpireTimeOffset+4]) == expire {
			binary.LittleEndian.PutUint32(entry[legacyWrongItemExpireTimeOffset:legacyWrongItemExpireTimeOffset+4], 0)
		}
		binary.LittleEndian.PutUint32(entry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4], expire)
	}
	entry[0x57] = clampByte(firstExtraInt(extra, 0, "byte_57", "value_57"))
	entry[0x58] = clampByte(firstExtraInt(extra, 0, "byte_58", "value_58"))
	entry[0x59] = clampByte(firstExtraInt(extra, 0, "byte_59", "value_59"))
	copy(entry[0x65:0x6E], fixedExtraBytes(extra, 9, "raw_data_65", "raw65"))
	copy(entry[0x72:0x77], fixedExtraBytes(extra, 5, "tail_data_72", "tail72"))
	writer.writeBytes(entry)
}

func commonItemListStackValueA(stack dnfrepo.ItemStack) int64 {
	if value := firstExtraInt(stack.Extra, 0, "value_a", "instance_value", "item_uid", "serial", "count_or_instance"); value != 0 {
		return value
	}
	// Keep op14 row refreshes aligned with dnfbridge's login/full-list
	// projection: current op44 requires a nonzero raw+0x0E identity before the
	// client will emit a right-click request. Imported stackables without an
	// explicit historical identity use their stable item id on the wire.
	if strings.EqualFold(strings.TrimSpace(stack.Extra["item_kind"]), "stackable") && stack.ItemID > 0 {
		return stack.ItemID
	}
	return 0
}

func writePetItemListEntry(writer *packetWriter, slot int16, stack dnfrepo.ItemStack) {
	extra := stack.Extra
	entry := makeCurrentItemListEntry(
		slot,
		stack.ItemID,
		firstExtraInt(extra, stack.Count, "creature_serial_or_handle", "serial", "handle", "instance_value", "item_uid"),
		stack.RawEntry,
	)
	entry[0x0A] = clampByte(firstExtraInt(extra, 0, "ext_data0", "ext0", "packed_flag_byte", "packed_flag", "packed"))
	binary.LittleEndian.PutUint16(entry[0x0B:0x0D], clampUint16(firstExtraInt(extra, 0, "durability", "max_durability")))
	entry[0x0D] = clampByte(firstExtraInt(extra, boolInt64(stack.Bind), "seal_flag", "seal", "bind_flag", "bind"))
	binary.LittleEndian.PutUint32(entry[0x0E:0x12], uint32(clampInt32(firstExtraInt(extra, 0, "value_a"))))
	copy(entry[0x12:0x77], fixedExtraBytes(extra, currentItemListEntrySize-0x12, "tail_data_12", "tail12", "pet_tail_12"))
	entry[0x12] = clampByte(firstExtraInt(extra, 0, "byte_12", "value_12", "enchant_upgrade_count", "pet_enchant_upgrade_count"))
	expire := itemListStackExpireUnix(stack)
	binary.LittleEndian.PutUint32(
		entry[currentPetRemainSecondsOffset:currentPetRemainSecondsOffset+4],
		petRemainingSecondsAt(expire, time.Now().UTC()),
	)
	if expire != 0 {
		if binary.LittleEndian.Uint32(entry[legacyWrongItemExpireTimeOffset:legacyWrongItemExpireTimeOffset+4]) == expire {
			binary.LittleEndian.PutUint32(entry[legacyWrongItemExpireTimeOffset:legacyWrongItemExpireTimeOffset+4], 0)
		}
		binary.LittleEndian.PutUint32(entry[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4], expire)
	}
	writer.writeBytes(entry)
}

func petRemainingSecondsAt(expire uint32, now time.Time) uint32 {
	if expire == 0 {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	remaining := int64(expire) - now.Unix()
	if remaining <= 0 {
		return 0
	}
	return uint32(remaining)
}

func itemListStackExpireUnix(stack dnfrepo.ItemStack) uint32 {
	if value := firstExtraInt(stack.Extra, 0, "expire_time", "expire_unix"); value > 0 {
		return uint32(clampInt32(value))
	}
	if !stack.ExpireAt.IsZero() && stack.ExpireAt.Unix() > 0 {
		return uint32(clampInt32(stack.ExpireAt.Unix()))
	}
	return 0
}

func makeCurrentItemListEntry(slot int16, itemID int64, amount int64, rawEntries ...[]byte) []byte {
	entry := make([]byte, currentItemListEntrySize)
	if len(rawEntries) > 0 && len(rawEntries[0]) == currentItemListEntrySize {
		copy(entry, rawEntries[0])
	}
	binary.LittleEndian.PutUint16(entry[0x00:0x02], uint16(slot))
	binary.LittleEndian.PutUint32(entry[0x02:0x06], uint32(clampInt32(itemID)))
	binary.LittleEndian.PutUint32(entry[0x06:0x0A], uint32(clampInt32(amount)))
	return entry
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func firstExtraInt(extra map[string]string, fallback int64, keys ...string) int64 {
	for _, key := range keys {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		value, err := strconv.ParseInt(raw, 0, 64)
		if err == nil {
			return value
		}
	}
	return fallback
}

func fixedExtraBytes(extra map[string]string, length int, keys ...string) []byte {
	out := make([]byte, length)
	for _, key := range keys {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		raw = strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", "0x", "", "0X", "").Replace(raw)
		decoded, err := hex.DecodeString(raw)
		if err != nil || len(decoded) == 0 {
			continue
		}
		copy(out, decoded)
		return out
	}
	return out
}

func clampInt32(value int64) int32 {
	if value > 2147483647 {
		return 2147483647
	}
	if value < -2147483648 {
		return -2147483648
	}
	return int32(value)
}

func clampByte(value int64) byte {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return byte(value)
}

func clampUint16(value int64) uint16 {
	if value < 0 {
		return 0
	}
	if value > 65535 {
		return 65535
	}
	return uint16(value)
}

func clampInt16(value int64) int16 {
	if value > 32767 {
		return 32767
	}
	if value < -32768 {
		return -32768
	}
	return int16(value)
}
