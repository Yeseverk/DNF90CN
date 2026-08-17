package dnfbridge

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func currentFindMainInventorySocketTarget(inventory dnfrepo.InventoryRecord, requestedSlot int16, itemID int64) (string, int16, dnfrepo.ItemStack, error) {
	if itemID <= 0 {
		return "", 0, dnfrepo.ItemStack{}, fmt.Errorf("%w: item=%d", errCurrentSocketTargetMissing, itemID)
	}
	if requestedSlot >= 0 {
		key := currentSocketInventoryKey(currentSocketListMain, requestedSlot)
		if stack, ok := inventory.Slots[key]; ok && stack.ItemID == itemID && stack.ItemID > 0 {
			return key, requestedSlot, stack, nil
		}
	}

	var (
		foundKey   string
		foundSlot  int16
		foundStack dnfrepo.ItemStack
		matches    int
	)
	for key, stack := range inventory.Slots {
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if !ok || listType != currentSocketListMain || stack.ItemID != itemID || stack.ItemID <= 0 {
			continue
		}
		foundKey = key
		foundSlot = slot
		foundStack = stack
		matches++
	}
	switch matches {
	case 0:
		return "", 0, dnfrepo.ItemStack{}, fmt.Errorf("%w: client_key=%d item=%d", errCurrentSocketTargetMissing, requestedSlot, itemID)
	case 1:
		return foundKey, foundSlot, foundStack, nil
	default:
		return "", 0, dnfrepo.ItemStack{}, fmt.Errorf("%w: client_key=%d item=%d matches=%d", errCurrentSocketTargetAmbiguous, requestedSlot, itemID, matches)
	}
}

func currentOpenEquipmentSocketWithMaterial(catalog *pvfDungeonDropCatalog, inventory *dnfrepo.InventoryRecord, materialSlot int16, rule currentEquipmentPlacementRule) (int, error) {
	if inventory == nil {
		return 0, errCurrentSocketInventoryMissing
	}
	material, ok := inventory.Slots[currentSocketInventoryKey(currentSocketListMain, materialSlot)]
	if !ok || material.ItemID <= 0 || material.Count <= 0 {
		return 0, fmt.Errorf("%w: list=%d slot=%d", errCurrentSocketMaterialMissing, currentSocketListMain, materialSlot)
	}
	if err := currentEquipmentSocketOpenMaterialRule(catalog, material.ItemID); err != nil {
		return 0, err
	}
	if _, _, err := consumeCurrentSocketStack(inventory, currentSocketListMain, materialSlot, material.ItemID, true); err != nil {
		return 0, err
	}
	return currentEquipmentSocketOpenCount(rule), nil
}

func currentEquipmentSocketOpenCount(rule currentEquipmentPlacementRule) int {
	if rule.targetSlot == 22 || rule.targetSlot == 23 {
		return 1
	}
	return 2
}

func currentEquipmentJewelSocketType(rule currentEquipmentPlacementRule) byte {
	switch rule.targetSlot {
	case 14, 16:
		return 0x04
	case 15, 19:
		return 0x02
	case 18, 21:
		return 0x01
	case 17, 20:
		return 0x08
	default:
		return 0x10
	}
}

func currentCanAttachEmblem(socketType, emblemType byte) bool {
	return socketType == 0 || emblemType == 0 || (socketType&emblemType) != 0
}

func currentApplyEquipmentEmblems(catalog *pvfDungeonDropCatalog, rule currentEquipmentPlacementRule, inventory *dnfrepo.InventoryRecord, data *[currentEquipmentEmblemDataBytes]byte, emblems []currentEmblemApplyRequest) error {
	if inventory == nil || data == nil {
		return errCurrentSocketInventoryMissing
	}
	openCount := int(data[0])
	maxOpen := currentEquipmentSocketOpenCount(rule)
	if openCount <= 0 {
		return errCurrentSocketNoOpenSockets
	}
	currentEnsureEquipmentEmblemSocketsOpen(data, maxOpen, openCount)
	socketType := currentEquipmentJewelSocketType(rule)
	for _, emblem := range emblems {
		logicalIndex, ok := currentResolveEquipmentSocketRequest(rule, openCount, emblem.SocketIndex)
		if !ok {
			return fmt.Errorf("%w: socket=%d open=%d", errCurrentSocketIndexInvalid, emblem.SocketIndex, openCount)
		}
		emblemType := currentSocketEmblemType(catalog, emblem.EmblemItemID)
		if !currentCanAttachEmblem(socketType, emblemType) {
			return fmt.Errorf("%w: socket_type=0x%02x emblem_type=0x%02x emblem=%d", errCurrentSocketTypeMismatch, socketType, emblemType, emblem.EmblemItemID)
		}
		if _, _, err := consumeCurrentSocketStack(inventory, currentSocketListMain, emblem.EmblemSlot, emblem.EmblemItemID, true); err != nil {
			return err
		}
		currentWriteEquipmentEmblem(data, logicalIndex, emblem.EmblemItemID)
	}
	return nil
}

func currentResolveEquipmentSocketRequest(rule currentEquipmentPlacementRule, openCount int, requestSocketIndex byte) (byte, bool) {
	visibleOpenCount := openCount
	if visibleOpenCount > 2 {
		visibleOpenCount = 2
	}
	if requestSocketIndex >= currentAvatarSocketCount || visibleOpenCount <= 0 {
		return 0, false
	}
	if currentEquipmentSocketOpenCount(rule) == 1 {
		if requestSocketIndex > 1 {
			return 0, false
		}
		return requestSocketIndex, true
	}
	if int(requestSocketIndex) >= visibleOpenCount {
		return 0, false
	}
	return requestSocketIndex, true
}

func currentApplyAvatarEmblems(catalog *pvfDungeonDropCatalog, inventory *dnfrepo.InventoryRecord, data *[currentAvatarSocketBytes]byte, emblems []currentEmblemApplyRequest) error {
	if inventory == nil || data == nil {
		return errCurrentSocketInventoryMissing
	}
	if currentAvatarSocketOpenCount(*data) <= 0 {
		return errCurrentSocketNoOpenSockets
	}
	for _, emblem := range emblems {
		if emblem.SocketIndex >= currentAvatarSocketCount {
			return fmt.Errorf("%w: socket=%d", errCurrentSocketIndexInvalid, emblem.SocketIndex)
		}
		socketType := currentAvatarSocketType(*data, emblem.SocketIndex)
		if socketType == 0 {
			return fmt.Errorf("%w: socket=%d unopened", errCurrentSocketIndexInvalid, emblem.SocketIndex)
		}
		emblemType := currentSocketEmblemType(catalog, emblem.EmblemItemID)
		if !currentCanAttachEmblem(socketType, emblemType) {
			return fmt.Errorf("%w: socket_type=0x%02x emblem_type=0x%02x emblem=%d", errCurrentSocketTypeMismatch, socketType, emblemType, emblem.EmblemItemID)
		}
		if _, _, err := consumeCurrentSocketStack(inventory, currentSocketListMain, emblem.EmblemSlot, emblem.EmblemItemID, true); err != nil {
			return err
		}
		currentSetAvatarSocketEmblem(data, emblem.SocketIndex, emblem.EmblemItemID)
	}
	return nil
}

func consumeCurrentSocketStack(inventory *dnfrepo.InventoryRecord, listType byte, slot int16, itemID int64, requireItemID bool) (dnfrepo.ItemStack, int64, error) {
	if inventory == nil {
		return dnfrepo.ItemStack{}, 0, errCurrentSocketInventoryMissing
	}
	key := currentSocketInventoryKey(listType, slot)
	stack, ok := inventory.Slots[key]
	if !ok || stack.ItemID <= 0 || stack.Count <= 0 {
		return dnfrepo.ItemStack{}, 0, fmt.Errorf("%w: list=%d slot=%d", errCurrentSocketEmblemMissing, listType, slot)
	}
	if requireItemID && stack.ItemID != itemID {
		return dnfrepo.ItemStack{}, 0, fmt.Errorf("%w: list=%d slot=%d want=%d got=%d", errCurrentSocketEmblemMissing, listType, slot, itemID, stack.ItemID)
	}
	remaining := stack.Count - 1
	if remaining > 0 {
		stack.Count = remaining
		currentRefreshStackRawEntry(&stack, listType, slot)
		inventory.Slots[key] = stack
	} else {
		delete(inventory.Slots, key)
	}
	return stack, remaining, nil
}

func currentEquipmentEmblemData(extra map[string]string, raw []byte) [currentEquipmentEmblemDataBytes]byte {
	var best [currentEquipmentEmblemDataBytes]byte
	bestScore := -1
	consider := func(data [currentEquipmentEmblemDataBytes]byte) {
		score := currentEquipmentEmblemDataScore(data)
		if score > bestScore {
			best = data
			bestScore = score
		}
	}
	if data, ok := currentEquipmentEmblemDataFromExtra(extra); ok {
		consider(data)
	}
	if data, ok := currentEquipmentEmblemDataFromCurrentRaw(raw); ok {
		consider(data)
	}
	if tail := currentEquipmentTailDataFromExtra(extra); len(tail) >= currentEquipmentEmblemDataBytes {
		var data [currentEquipmentEmblemDataBytes]byte
		copy(data[:], tail[:len(data)])
		consider(data)
	}
	if data, ok := currentEquipmentLegacyEmblemDataFromRaw(raw); ok {
		consider(data)
	}
	return best
}

func currentEquipmentEmblemDataFromCurrentRaw(raw []byte) ([currentEquipmentEmblemDataBytes]byte, bool) {
	var data [currentEquipmentEmblemDataBytes]byte
	if len(raw) < currentEquipmentVectorOffset+currentEquipmentEmblemDataBytes {
		return data, false
	}
	count := int(raw[currentEquipmentVectorOffset])
	if count > 2 {
		return data, false
	}
	data[0] = byte(count)
	if count > 0 {
		copy(data[1:5], raw[currentEquipmentVectorOffset+1:currentEquipmentVectorOffset+5])
	}
	if count > 1 {
		copy(data[5:9], raw[currentEquipmentVectorOffset+5:currentEquipmentVectorOffset+9])
	}
	return data, true
}

func currentEquipmentEmblemDataScore(data [currentEquipmentEmblemDataBytes]byte) int {
	score := 0
	openCount := int(data[0])
	if openCount > 2 {
		return -1
	}
	if openCount > 0 {
		score += 10 + openCount
	}
	if openCount > 0 && binary.LittleEndian.Uint32(data[1:5]) != 0 {
		score += 10
	}
	if openCount > 1 && binary.LittleEndian.Uint32(data[5:9]) != 0 {
		score += 10
	}
	return score
}

func currentEquipmentEmblemDataFromExtra(extra map[string]string) ([currentEquipmentEmblemDataBytes]byte, bool) {
	var data [currentEquipmentEmblemDataBytes]byte
	if len(extra) == 0 {
		return data, false
	}
	for _, key := range currentEquipmentEmblemDataExtraKeys {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		raw = strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", "0x", "", "0X", "").Replace(raw)
		decoded, err := hex.DecodeString(raw)
		if err != nil || len(decoded) < len(data) {
			continue
		}
		copy(data[:], decoded[:len(data)])
		if currentEquipmentEmblemDataScore(data) >= 0 {
			return data, true
		}
	}
	return data, false
}

func currentEquipmentLegacyEmblemDataFromRaw(raw []byte) ([currentEquipmentEmblemDataBytes]byte, bool) {
	var data [currentEquipmentEmblemDataBytes]byte
	if len(raw) < currentEquipmentLegacyEmblemDataOffset+currentEquipmentEmblemDataBytes {
		return data, false
	}
	// If the current collection is already populated, row+0x2F is part of its
	// second primary dword and must not be reinterpreted as old 86JP tail data.
	if len(raw) >= currentEquipmentVectorOffset+1 && raw[currentEquipmentVectorOffset] > 0 {
		return data, false
	}
	copy(data[:], raw[currentEquipmentLegacyEmblemDataOffset:currentEquipmentLegacyEmblemDataOffset+currentEquipmentEmblemDataBytes])
	return data, currentEquipmentEmblemDataScore(data) >= 0
}

func currentEnsureEquipmentEmblemSocketsOpen(data *[currentEquipmentEmblemDataBytes]byte, maxOpen, openCount int) {
	if data == nil || maxOpen <= 0 {
		return
	}
	if maxOpen > 2 {
		maxOpen = 2
	}
	visible := openCount
	if visible < 0 {
		visible = 0
	}
	if visible > maxOpen {
		visible = maxOpen
	}
	data[0] = byte(visible)
	if visible > 0 && binary.LittleEndian.Uint32(data[1:5]) == 0 {
		binary.LittleEndian.PutUint32(data[1:5], math.MaxUint32)
	}
	if visible > 1 && binary.LittleEndian.Uint32(data[5:9]) == 0 {
		binary.LittleEndian.PutUint32(data[5:9], math.MaxUint32)
	}
}

func currentWriteEquipmentEmblem(data *[currentEquipmentEmblemDataBytes]byte, socketIndex byte, emblemItemID int64) {
	if data == nil || emblemItemID <= 0 || emblemItemID > math.MaxUint32 {
		return
	}
	switch socketIndex {
	case 0:
		binary.LittleEndian.PutUint32(data[1:5], uint32(emblemItemID))
	case 1:
		binary.LittleEndian.PutUint32(data[5:9], uint32(emblemItemID))
	}
}

func currentApplyEquipmentEmblemDataToStack(stack *dnfrepo.ItemStack, listType byte, slot int16, data [currentEquipmentEmblemDataBytes]byte, socketType byte) {
	if stack == nil {
		return
	}
	currentEnsureExtra(&stack.Extra)
	currentSetEquipmentEmblemDataExtra(stack.Extra, data)
	currentSetEquipmentSocketTypeExtra(stack.Extra, socketType)
	currentRefreshStackRawEntry(stack, listType, slot)
}

// currentApplyEquipmentSocketVectorToEntry writes the Extra-stored emblem data
// into the current EXE's raw[0x77] ordinary-equipment vector. sub_225CD00 reads
// count@0x3C and two contiguous u32 emblem IDs at +0x3D/+0x41; sub_22D2280
// installs every nonzero entry in the runtime equipment wrapper.
func currentApplyEquipmentSocketVectorToEntry(entry *currentItemListEntry, extra map[string]string) {
	if entry == nil || len(extra) == 0 {
		return
	}
	// Only apply to equipment items: check item_kind or equipment-specific
	// Extra keys to avoid corrupting stackable/material raw rows.
	if extra["item_kind"] != "equipment" &&
		extra["equipment_emblem_data"] == "" && extra["equipment_socket_data"] == "" &&
		extra["current_equipment_emblem_data"] == "" && extra["current_equipment_socket_data"] == "" &&
		extra["equipment_socket_type"] == "" && extra["equipment_jewel_socket_type"] == "" &&
		extra["current_exe_equipment_type"] == "" {
		return
	}
	data, ok := currentEquipmentEmblemDataFromExtra(extra)
	if !ok {
		// Fall back to tailData2F / raw_data_2f keys (used by equipped entries
		// and legacy 86JP storage).
		if tail := currentEquipmentTailDataFromExtra(extra); len(tail) >= currentEquipmentEmblemDataBytes {
			copy(data[:], tail[:currentEquipmentEmblemDataBytes])
			ok = currentEquipmentEmblemDataScore(data) >= 0
		}
	}
	if !ok {
		return
	}
	count := int(data[0])
	if count <= 0 {
		return
	}
	if count > 2 {
		count = 2
	}
	currentClearLegacyWrongEquipmentSocketProjection(entry, data, count)

	// Current EXE ordinary-equipment emblem vector at raw+0x3C.
	entry.data[currentEquipmentVectorOffset] = byte(count)
	// emblem[0] (0xFFFFFFFF = open and empty).
	copy(entry.data[currentEquipmentVectorOffset+1:currentEquipmentVectorOffset+5], data[1:5])
	binary.LittleEndian.PutUint32(entry.data[currentEquipmentVectorOffset+5:currentEquipmentVectorOffset+9], 0)
	if count > 1 {
		copy(entry.data[currentEquipmentVectorOffset+5:currentEquipmentVectorOffset+9], data[5:9])
	}
}

// currentClearLegacyWrongEquipmentSocketProjection removes only the exact byte
// pattern emitted by the interim raw+0x27 projection. A nonmatching record is
// preserved because raw+0x27 belongs to a separate current-client collection.
func currentClearLegacyWrongEquipmentSocketProjection(entry *currentItemListEntry, data [currentEquipmentEmblemDataBytes]byte, count int) {
	if entry == nil || count <= 0 || count > 2 {
		return
	}
	offset := currentLegacyWrongEquipmentVectorOffset
	if entry.data[offset] != byte(count) ||
		!bytes.Equal(entry.data[offset+1:offset+5], data[1:5]) {
		return
	}
	var second [4]byte
	if count > 1 {
		copy(second[:], data[5:9])
	}
	if !bytes.Equal(entry.data[offset+5:offset+9], second[:]) {
		return
	}
	for _, value := range entry.data[offset+9 : offset+17] {
		if value != 0 {
			return
		}
	}
	clear(entry.data[offset : offset+17])
}

func currentApplyEquipmentEmblemDataToEquipment(entry *dnfrepo.EquipmentEntry, data [currentEquipmentEmblemDataBytes]byte, socketType byte) {
	if entry == nil {
		return
	}
	currentEnsureExtra(&entry.Extra)
	currentSetEquipmentEmblemDataExtra(entry.Extra, data)
	currentSetEquipmentSocketTypeExtra(entry.Extra, socketType)
	if row, ok := currentItemListEntryFromEquipment(*entry); ok {
		entry.RawEntry = append([]byte(nil), row.data[:]...)
	}
}

func currentEquipmentTailData(extra map[string]string, raw []byte) []byte {
	tail := currentEquipmentTailDataFromExtra(extra)
	if len(tail) == currentEquipmentTailDataBytes {
		return tail
	}
	tail = make([]byte, currentEquipmentTailDataBytes)
	if len(raw) >= currentEquipmentLegacyEmblemDataOffset+len(tail) && (len(raw) <= currentEquipmentVectorOffset || raw[currentEquipmentVectorOffset] == 0) {
		copy(tail, raw[currentEquipmentLegacyEmblemDataOffset:currentEquipmentLegacyEmblemDataOffset+len(tail)])
	}
	return tail
}

func currentEquipmentTailDataFromExtra(extra map[string]string) []byte {
	if len(extra) == 0 {
		return nil
	}
	var fallback []byte
	for _, key := range currentEquipmentTailDataExtraKeys {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		raw = strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", "0x", "", "0X", "").Replace(raw)
		decoded, err := hex.DecodeString(raw)
		if err != nil || len(decoded) == 0 {
			continue
		}
		out := make([]byte, currentEquipmentTailDataBytes)
		copy(out, decoded)
		if currentEquipmentTailHasSocketData(out) {
			return out
		}
		if fallback == nil {
			fallback = out
		}
	}
	return fallback
}

func currentEquipmentTailHasSocketData(tail []byte) bool {
	if len(tail) == 0 {
		return false
	}
	if tail[0] != 0 {
		return true
	}
	if len(tail) >= currentEquipmentEmblemDataBytes {
		return binary.LittleEndian.Uint32(tail[1:5]) != 0 || binary.LittleEndian.Uint32(tail[5:9]) != 0
	}
	for _, value := range tail {
		if value != 0 {
			return true
		}
	}
	return false
}

func currentSetEquipmentTailDataExtra(extra map[string]string, tail []byte) {
	if extra == nil {
		return
	}
	for _, key := range currentEquipmentTailDataExtraKeys {
		currentSetHexExtra(extra, key, tail)
	}
}

func currentSetEquipmentEmblemDataExtra(extra map[string]string, data [currentEquipmentEmblemDataBytes]byte) {
	if extra == nil {
		return
	}
	for _, key := range currentEquipmentEmblemDataExtraKeys {
		currentSetHexExtra(extra, key, data[:])
	}
}

func currentSetEquipmentSocketTypeExtra(extra map[string]string, socketType byte) {
	if extra == nil || socketType == 0 {
		return
	}
	value := strconv.Itoa(int(socketType))
	extra["equipment_socket_type"] = value
	extra["equipment_jewel_socket_type"] = value
}

func currentEquipmentSocketTypeFromExtra(extra map[string]string) byte {
	if value := sceneInventoryExtraByte(extra, "equipment_socket_type", "equipment_jewel_socket_type", "jewel_socket_type"); value != 0 {
		return value
	}
	if value, ok := sceneInventoryExtraUint(extra, "current_exe_equipment_type", "equipped_slot", "equipment_slot"); ok && value <= math.MaxInt16 {
		return currentEquipmentJewelSocketType(currentEquipmentPlacementRule{targetSlot: int16(value)})
	}
	return 0
}

func currentAvatarSocketData(extra map[string]string) [currentAvatarSocketBytes]byte {
	var data [currentAvatarSocketBytes]byte
	if value := currentItemListAvatarSocketData(extra); len(value) == currentAvatarSocketBytes {
		copy(data[:], value)
	}
	return currentNormalizeAvatarSocketData(data)
}

func currentNormalizeAvatarSocketData(data [currentAvatarSocketBytes]byte) [currentAvatarSocketBytes]byte {
	for index := 0; index < currentAvatarSocketCount; index++ {
		offset := index * currentAvatarSocketStride
		if data[offset] == 0 && currentKnownAvatarSocketType(data[offset+1]) {
			var normalized [currentAvatarSocketBytes]byte
			for inner := 0; inner < currentAvatarSocketCount; inner++ {
				src := inner * currentAvatarSocketStride
				if data[src] == 0 && currentKnownAvatarSocketType(data[src+1]) {
					normalized[src] = data[src+1]
					normalized[src+1] = data[src+2]
					copy(normalized[src+2:src+5], data[src+3:src+6])
					continue
				}
				copy(normalized[src:src+currentAvatarSocketStride], data[src:src+currentAvatarSocketStride])
			}
			data = normalized
			break
		}
	}
	// The current client represents the universal M socket as the special
	// ushort 0xFFEF.  The low byte is still the PVF socket-family value, but
	// 0x00EF is rejected by the avatar tooltip as a closed/invalid socket.
	// Normalize rows emitted by the earlier server build while they are read,
	// so already-granted auras become visible without granting the item again.
	for index := 0; index < currentAvatarSocketCount; index++ {
		offset := index * currentAvatarSocketStride
		if binary.LittleEndian.Uint16(data[offset:offset+2]) == 0x00EF {
			binary.LittleEndian.PutUint16(data[offset:offset+2], 0xFFEF)
		}
	}
	return data
}

func currentKnownAvatarSocketType(socketType byte) bool {
	switch socketType {
	case 0x01, 0x02, 0x04, 0x08, 0x10, 0xEF:
		return true
	default:
		return false
	}
}

func currentAvatarSocketOpenCount(data [currentAvatarSocketBytes]byte) int {
	count := 0
	for index := 0; index < currentAvatarSocketCount; index++ {
		if currentAvatarSocketType(data, byte(index)) != 0 {
			count++
		}
	}
	return count
}

func currentAvatarSocketType(data [currentAvatarSocketBytes]byte, socketIndex byte) byte {
	if socketIndex >= currentAvatarSocketCount {
		return 0
	}
	return data[int(socketIndex)*currentAvatarSocketStride]
}

func currentSetAvatarSocketTypes(data *[currentAvatarSocketBytes]byte, socketTypes []byte) {
	if data == nil {
		return
	}
	for index := 0; index < currentAvatarSocketCount; index++ {
		offset := index * currentAvatarSocketStride
		if index < len(socketTypes) {
			socketType := uint16(socketTypes[index])
			if socketTypes[index] == 0xEF {
				socketType = 0xFFEF
			}
			binary.LittleEndian.PutUint16(data[offset:offset+2], socketType)
			continue
		}
		for pos := 0; pos < currentAvatarSocketStride; pos++ {
			data[offset+pos] = 0
		}
	}
}

func currentSetAvatarSocketEmblem(data *[currentAvatarSocketBytes]byte, socketIndex byte, emblemItemID int64) {
	if data == nil || socketIndex >= currentAvatarSocketCount || emblemItemID <= 0 || emblemItemID > math.MaxUint32 {
		return
	}
	offset := int(socketIndex)*currentAvatarSocketStride + 2
	binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(emblemItemID))
}

func currentApplyAvatarSocketDataToStack(stack *dnfrepo.ItemStack, listType byte, slot int16, data [currentAvatarSocketBytes]byte) {
	if stack == nil {
		return
	}
	currentEnsureExtra(&stack.Extra)
	currentSetHexExtra(stack.Extra, "avatar_socket_data", data[:])
	currentSetHexExtra(stack.Extra, "reserved2", data[:])
	currentSetHexExtra(stack.Extra, "jewel_socket", data[:])
	currentRefreshStackRawEntry(stack, listType, slot)
}

func currentApplyAvatarSocketDataToEquipment(entry *dnfrepo.EquipmentEntry, data [currentAvatarSocketBytes]byte) {
	if entry == nil {
		return
	}
	currentEnsureExtra(&entry.Extra)
	currentSetHexExtra(entry.Extra, "avatar_socket_data", data[:])
	currentSetHexExtra(entry.Extra, "reserved2", data[:])
	currentSetHexExtra(entry.Extra, "jewel_socket", data[:])
	if row, ok := currentItemListEntryFromEquipment(*entry); ok {
		entry.RawEntry = append([]byte(nil), row.data[:]...)
	}
}

func currentRefreshStackRawEntry(stack *dnfrepo.ItemStack, listType byte, slot int16) {
	if stack == nil || stack.ItemID <= 0 {
		return
	}
	entry := currentItemListEntryFromStack(listType, slot, *stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)
}

func currentEnsureExtra(extra *map[string]string) {
	if extra == nil {
		return
	}
	if *extra == nil {
		*extra = make(map[string]string)
	}
}

func currentSetHexExtra(extra map[string]string, key string, data []byte) {
	if extra == nil || key == "" {
		return
	}
	extra[key] = hex.EncodeToString(data)
}

func currentEmblemConsumedSlots(emblems []currentEmblemApplyRequest) []currentSocketChangedSlot {
	seen := make(map[int16]struct{}, len(emblems))
	out := make([]currentSocketChangedSlot, 0, len(emblems))
	for _, emblem := range emblems {
		if _, ok := seen[emblem.EmblemSlot]; ok {
			continue
		}
		seen[emblem.EmblemSlot] = struct{}{}
		out = append(out, currentSocketChangedSlot{ListType: currentSocketListMain, Slot: emblem.EmblemSlot})
	}
	return out
}

func currentFindEquippedEntry(record dnfrepo.EquipmentRecord, requestedSlot int16, itemID int64, class currentEquipmentPlacementClass) (string, dnfrepo.EquipmentEntry, bool) {
	for key, entry := range record.Entries {
		if entry.ItemID != itemID || entry.ItemID <= 0 {
			continue
		}
		entryClass := currentEquippedEntryPlacementClass(entry)
		if entryClass != class {
			continue
		}
		if entry.SlotIndex == requestedSlot {
			return key, entry, true
		}
		if actorSlot, ok := currentEXEActorEquipmentSlot(entry); ok && int16(actorSlot) == requestedSlot {
			return key, entry, true
		}
	}
	return "", dnfrepo.EquipmentEntry{}, false
}

func currentEquippedEntryPlacementClass(entry dnfrepo.EquipmentEntry) currentEquipmentPlacementClass {
	if actorSlot, ok := currentEXEActorEquipmentSlot(entry); ok {
		switch {
		case actorSlot <= uint64(currentEquipmentPlacementAvatarSlotMax):
			return currentEquipmentPlacementClassAvatar
		case actorSlot >= uint64(currentEquipmentPlacementNormalSlotMin) && actorSlot <= uint64(currentEquipmentPlacementNormalSlotMax):
			return currentEquipmentPlacementClassNormal
		}
	}
	entryClass, err := currentEquipmentPlacementClassForSource(currentSocketListEquipment, entry.SlotIndex)
	if err != nil {
		return currentEquipmentPlacementClassUnknown
	}
	return entryClass
}

func currentSocketInventoryKey(listType byte, slot int16) string {
	return strconv.Itoa(int(listType)) + ":" + strconv.FormatInt(int64(slot), 10)
}
