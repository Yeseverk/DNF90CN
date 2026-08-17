package dnfbridge

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	newCharacterInitialLevel    = 1
	newCharacterInitialGrowType = 0
	// These are the durable first-login / tutorial-return location only. Current
	// runtime Script.pvf maps town 38 to new_Elvengard.twn; its area 1 is
	// new_seria_room.map and declares the gate at (450, 234). They must never
	// be used as a dungeon player-spawn fallback: the client owns that position
	// from the selected dungeon map's real PVF [dungeon start area].
	newCharacterInitialTownID    = 38
	newCharacterInitialAreaID    = 1
	newCharacterInitialPosX      = 450
	newCharacterInitialPosY      = 234
	newCharacterInitialDirection = 5
	newCharacterInitialAreaState = 3
	newCharacterInitialChannelID = 2
	newCharacterInitialFatigue   = 156
	newCharacterFatigueLimit     = 156

	csharpSubtype1ProtocolStatLevel = 100
	csharpSubtype1StatBlockMarker   = 83

	currentExeInventoryMainListType          = 0
	currentExeInventoryAvatarListType        = 1
	currentExeInventoryPersonalCargoListType = 2
	// The working Python new-character default is bag_expand_level=0, encoded
	// directly as op13 list0 param16=0. Existing characters retain their saved
	// 8/16/24 expansion value.
	currentExeInitialMainSlotCount          = 0
	currentExeInitialAvatarExpansion        = 0
	currentExeInitialPersonalCargoSlotCount = 8
	csharpAvatarUnknownFixed30              = 0x00001E00
	csharpAvatarUnknownFixed4               = 0x0400

	csharpReviveCoinItemID     = 1
	csharpReviveCoinWalletSlot = 1

	csharpCreatorMageJob              = 10
	csharpHotkeyUnassignedKey         = 0x86
	csharpCreatorHotkeyHeaderSlots    = 4
	csharpHotkeyAccountScopedSlotSize = 1
	csharpCreatorHotkeyPVFPath        = "clientonly/hotkeysystemforcreator.co"
)

var csharpDefaultHotkeySlots = []byte{
	0x02, 0x00, 0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x4E, 0x00, 0x39, 0x00, 0x50, 0x00, 0x0D, 0x00,
	0x4C, 0x00, 0x43, 0x00, 0x41, 0x00, 0x3F, 0x00, 0x45, 0x00, 0x52, 0x00, 0x4B, 0x00, 0x55, 0x00,
	0x44, 0x00, 0x56, 0x00, 0x54, 0x00, 0x51, 0x00, 0x37, 0x00, 0x49, 0x00, 0x3A, 0x00, 0x3C, 0x00,
	0x3D, 0x00, 0x3E, 0x00, 0x47, 0x00, 0x4D, 0x00, 0x3B, 0x00, 0x48, 0x00, 0x4A, 0x00, 0x4F, 0x00,
	0x2E, 0x00, 0x2F, 0x00, 0x30, 0x00, 0x31, 0x00, 0x32, 0x00, 0x33, 0x00, 0x86, 0x00, 0x1C, 0x00,
	0x53, 0x00, 0x57, 0x00, 0x86, 0x00, 0x6C, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00,
	0x1A, 0x00, 0x46, 0x00, 0x68, 0x00, 0x69, 0x00, 0x42, 0x00, 0x1B, 0x00, 0x6A, 0x00, 0x86, 0x00,
	0x86, 0x00, 0x86, 0x00, 0x38, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00,
	0x86, 0x00, 0x86, 0x00, 0x07, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00,
	0x86, 0x00, 0x86, 0x00, 0x59, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x58, 0x00,
	0x86, 0x00, 0x5A, 0x00, 0x5B, 0x00, 0x5C, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00,
	0x1F, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x86, 0x00, 0x15, 0x00,
	0x86, 0x00, 0x18, 0x00, 0x86, 0x00,
}

var csharpCreatorHotkeyRE = regexp.MustCompile("(?i)\\[key\\]\\s*`[^`]*`\\s+-?\\d+\\s+`[^`]*`\\s+`[^`]*`\\s+(-?\\d+)")

func newCharacterInventoryRecord(characterID string, now time.Time) dnfrepo.InventoryRecord {
	return dnfrepo.InventoryRecord{
		CharacterID: characterID,
		Slots: map[string]dnfrepo.ItemStack{
			csharpReviveCoinSlotKey(): csharpReviveCoinStack(),
		},
		Warehouse: map[string]dnfrepo.ItemStack{},
		UpdatedAt: now,
	}
}

func csharpReviveCoinSlotKey() string {
	return fmt.Sprintf("%d:%d", currentExeInventoryMainListType, csharpReviveCoinWalletSlot)
}

func csharpReviveCoinStack() dnfrepo.ItemStack {
	return dnfrepo.ItemStack{
		ItemID: csharpReviveCoinItemID,
		Count:  0,
		Extra: map[string]string{
			"source":          "csharp_revive_coin_wallet_slot",
			"item_kind":       "stackable",
			"amount_or_count": "0",
			"count":           "0",
			"value_a":         "0",
			"durability":      "0",
			"marker_16":       "0",
		},
	}
}

func (s *Service) newCharacterCSharpDefaultSettings(ctx context.Context, record dnfrepo.CharacterRecord, now time.Time) []dnfrepo.SettingsRecord {
	if strings.TrimSpace(record.CharacterID) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	job, _ := characterJobByte(record)
	return []dnfrepo.SettingsRecord{
		newCharacterContainerStateSettings(record.CharacterID, now),
		newCharacterInitBodiesSettings(record.CharacterID, now),
		s.newCharacterHotkeySettings(ctx, record.CharacterID, job, now),
	}
}

func newCharacterContainerStateSettings(characterID string, now time.Time) dnfrepo.SettingsRecord {
	return dnfrepo.SettingsRecord{
		Scope: dnfrepo.CharacterContainerStateScope(characterID),
		Values: map[string]string{
			"source":                         "current_exe_86jp_op13_container_state",
			"main_list_type":                 strconv.Itoa(currentExeInventoryMainListType),
			"main_list_param16":              strconv.Itoa(currentExeInitialMainSlotCount),
			"avatar_list_type":               strconv.Itoa(currentExeInventoryAvatarListType),
			"avatar_list_param16":            strconv.Itoa(currentExeInitialAvatarExpansion),
			"personal_cargo_list_type":       strconv.Itoa(currentExeInventoryPersonalCargoListType),
			"personal_cargo_list_param16":    strconv.Itoa(currentExeInitialPersonalCargoSlotCount),
			"avatar_unknown_fixed30":         strconv.Itoa(csharpAvatarUnknownFixed30),
			"avatar_unknown_fixed4":          strconv.Itoa(csharpAvatarUnknownFixed4),
			"revive_coin_item_id":            strconv.Itoa(csharpReviveCoinItemID),
			"revive_coin_wallet_slot":        strconv.Itoa(csharpReviveCoinWalletSlot),
			"account_cargo_selection_key":    "0",
			"account_cargo_item_count":       "0",
			"account_cargo_value32":          "0",
			"account_cargo_create_on_signup": "false",
		},
		UpdatedAt: now,
	}
}

func newCharacterContainerStateSettingsScope(characterID string) string {
	return dnfrepo.CharacterContainerStateScope(characterID)
}

func newCharacterInitBodiesSettings(characterID string, now time.Time) dnfrepo.SettingsRecord {
	values := map[string]string{
		"source": "csharp_SeedNewCharacterStructuredData",
	}
	for _, body := range []struct {
		noti       int
		occurrence int
		data       []byte
	}{
		{noti: 0x0035, occurrence: 0, data: make([]byte, 13)},
		{noti: 0x0077, occurrence: 0, data: []byte{0x00}},
		{noti: 0x0111, occurrence: 0, data: make([]byte, 8)},
		{noti: 0x019F, occurrence: 0, data: []byte{0x00, 0x00}},
		{noti: 0x0300, occurrence: 0, data: []byte{0x00, 0x00}},
		{noti: 0x0357, occurrence: 0, data: []byte{0x7B, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
		{noti: 0x03D8, occurrence: 0, data: make([]byte, 204)},
		{noti: 0x0077, occurrence: 1, data: []byte{0x00}},
	} {
		prefix := fmt.Sprintf("noti_%04x_occurrence_%d_", body.noti, body.occurrence)
		values[prefix+"hex"] = hex.EncodeToString(body.data)
		values[prefix+"len"] = strconv.Itoa(len(body.data))
	}
	return dnfrepo.SettingsRecord{
		Scope:     newCharacterInitBodiesSettingsScope(characterID),
		Values:    values,
		UpdatedAt: now,
	}
}

func newCharacterInitBodiesSettingsScope(characterID string) string {
	return "character:" + strings.TrimSpace(characterID) + ":init_bodies"
}

func (s *Service) newCharacterHotkeySettings(ctx context.Context, characterID string, job byte, now time.Time) dnfrepo.SettingsRecord {
	slots, keyType, source := s.csharpHotkeySlotsForJob(ctx, job)
	return dnfrepo.SettingsRecord{
		Scope: newCharacterHotkeySettingsScope(characterID),
		Values: map[string]string{
			"source":                    source,
			"job":                       strconv.Itoa(int(job)),
			"key_type":                  strconv.Itoa(int(keyType)),
			"slot_count":                strconv.Itoa(len(slots) / 2),
			"account_scoped_slot_count": strconv.Itoa(csharpHotkeyAccountScopedSlotSize),
			"slots_hex":                 hex.EncodeToString(slots),
		},
		UpdatedAt: now,
	}
}

func newCharacterHotkeySettingsScope(characterID string) string {
	return "character:" + strings.TrimSpace(characterID) + ":hotkeys"
}

func (s *Service) csharpHotkeySlotsForJob(ctx context.Context, job byte) ([]byte, byte, string) {
	if job != csharpCreatorMageJob {
		return cloneCSharpDefaultHotkeySlots(), 0, "csharp_account_settings_default_hotkey_slots"
	}
	slots, err := s.csharpCreatorHotkeySlots(ctx)
	if err != nil || len(slots) == 0 {
		if s != nil {
			s.logPacketEvent("dnf-character-creator-hotkey-pvf-missing", "error", err)
		}
		return cloneCSharpDefaultHotkeySlots(), 1, "csharp_creator_default_hotkey_fallback"
	}
	return slots, 1, "pvf_clientonly_hotkeysystemforcreator"
}

func (s *Service) csharpCreatorHotkeySlots(ctx context.Context) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("dnfbridge service is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, err
	}
	text, _, err := readInitialPVFText(archive, csharpCreatorHotkeyPVFPath, strings.ToLower(csharpCreatorHotkeyPVFPath))
	if err != nil {
		return nil, err
	}
	keys := parseCSharpCreatorHotkeyValues(text)
	if len(keys) == 0 {
		return nil, fmt.Errorf("creator hotkey pvf has no [key] defaults")
	}
	headerBytes := csharpCreatorHotkeyHeaderSlots * 2
	out := make([]byte, headerBytes+len(keys)*2)
	copy(out, csharpDefaultHotkeySlots[:minInt(headerBytes, len(csharpDefaultHotkeySlots))])
	for idx, key := range keys {
		binary.LittleEndian.PutUint16(out[headerBytes+idx*2:headerBytes+idx*2+2], key)
	}
	return out, nil
}

func parseCSharpCreatorHotkeyValues(text string) []uint16 {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	matches := csharpCreatorHotkeyRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]uint16, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		switch {
		case value < 0:
			out = append(out, csharpHotkeyUnassignedKey)
		case value > 0xffff:
			out = append(out, 0xffff)
		default:
			out = append(out, uint16(value))
		}
	}
	return out
}

func cloneCSharpDefaultHotkeySlots() []byte {
	return append([]byte(nil), csharpDefaultHotkeySlots...)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
