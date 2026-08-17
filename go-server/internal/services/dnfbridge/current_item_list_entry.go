package dnfbridge

import (
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (e *currentItemListEntry) patchCore(slot int16, itemID uint32, amount uint32) {
	binary.LittleEndian.PutUint16(e.data[0x00:0x02], uint16(slot))
	binary.LittleEndian.PutUint32(e.data[0x02:0x06], itemID)
	binary.LittleEndian.PutUint32(e.data[0x06:0x0A], amount)
}

func (e *currentItemListEntry) setByte(offset int, value byte) {
	if value == 0 || offset < 0 || offset >= len(e.data) {
		return
	}
	e.data[offset] = value
}

func (e *currentItemListEntry) setUint16(offset int, value uint16) {
	if value == 0 || offset < 0 || offset+2 > len(e.data) {
		return
	}
	binary.LittleEndian.PutUint16(e.data[offset:offset+2], value)
}

func (e *currentItemListEntry) setUint32(offset int, value uint32) {
	if value == 0 || offset < 0 || offset+4 > len(e.data) {
		return
	}
	binary.LittleEndian.PutUint32(e.data[offset:offset+4], value)
}

func (e *currentItemListEntry) clearLegacyWrongExpiration(expire uint32) {
	if expire == 0 || binary.LittleEndian.Uint32(e.data[legacyWrongCurrentItemListExpireTimeOffset:legacyWrongCurrentItemListExpireTimeOffset+4]) != expire {
		return
	}
	binary.LittleEndian.PutUint32(e.data[legacyWrongCurrentItemListExpireTimeOffset:legacyWrongCurrentItemListExpireTimeOffset+4], 0)
}

func (e *currentItemListEntry) copyFixed(offset int, value []byte) {
	if len(value) == 0 || offset < 0 || offset+len(value) > len(e.data) {
		return
	}
	copy(e.data[offset:], value)
}

func sortCurrentItemListEntries(entries []currentItemListEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return binary.LittleEndian.Uint16(entries[i].data[0x00:0x02]) < binary.LittleEndian.Uint16(entries[j].data[0x00:0x02])
	})
}

func sortCurrentEquipmentUpdateEntries(entries []currentEquipmentUpdateEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return binary.LittleEndian.Uint16(entries[i].data[0x00:0x02]) < binary.LittleEndian.Uint16(entries[j].data[0x00:0x02])
	})
}

func currentItemListBindFlag(bind bool, extra map[string]string) byte {
	value := sceneInventoryExtraByte(extra, "seal_flag", "seal", "bind_flag", "bind")
	if value != 0 {
		return value
	}
	if bind {
		return 1
	}
	return 0
}

func currentItemListStackExpire(stack dnfrepo.ItemStack) uint32 {
	if value := sceneInventoryExtraUint32(stack.Extra, "expire_time", "expire_unix"); value != 0 {
		return value
	}
	if !stack.ExpireAt.IsZero() && stack.ExpireAt.Unix() > 0 {
		return sceneInventoryUint32FromInt64(stack.ExpireAt.Unix())
	}
	return 0
}

func currentPetRemainingSecondsAt(expire uint32, now time.Time) uint32 {
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

func currentItemListStackableUsePeriod(stack dnfrepo.ItemStack) (uint16, bool) {
	if stack.ItemID <= 0 {
		return 0, false
	}
	kind := strings.TrimSpace(stack.Extra["item_kind"])
	stackableType := strings.TrimSpace(stack.Extra["stackable_type"])
	if !strings.EqualFold(kind, string(dungeonDropItemStackable)) && stackableType == "" {
		return 0, false
	}
	expire := currentItemListStackExpire(stack)
	if expire == 0 {
		return 0, false
	}
	return currentPVFStackableUsePeriodSeconds(time.Unix(int64(expire), 0).UTC(), time.Now().UTC()), true
}

func currentItemListEquipmentExpire(equipped dnfrepo.EquipmentEntry) uint32 {
	if value := sceneInventoryExtraUint32(equipped.Extra, "expire_time", "expire_unix"); value != 0 {
		return value
	}
	if !equipped.ExpireAt.IsZero() && equipped.ExpireAt.Unix() > 0 {
		return sceneInventoryUint32FromInt64(equipped.ExpireAt.Unix())
	}
	return 0
}

func currentItemListFixedExtraBytes(extra map[string]string, length int, keys ...string) []byte {
	if length <= 0 {
		return nil
	}
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
		out := make([]byte, length)
		copy(out, decoded)
		return out
	}
	return nil
}

func currentItemListAvatarSocketData(extra map[string]string) []byte {
	raw := currentItemListFixedExtraBytes(extra, currentAvatarSocketBytes, "avatar_socket_data", "reserved2", "jewel_socket", "jewelSocket")
	if len(raw) != currentAvatarSocketBytes {
		return raw
	}
	var data [currentAvatarSocketBytes]byte
	copy(data[:], raw)
	data = currentNormalizeAvatarSocketData(data)
	return append([]byte(nil), data[:]...)
}

func currentItemListAvatarColorData(extra map[string]string) []byte {
	return currentItemListVariableExtraBytes(extra, "avatar_color_data", "color_data", "tail_data", "tailData")
}

func currentItemListVariableExtraBytes(extra map[string]string, keys ...string) []byte {
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
		return decoded
	}
	return nil
}
