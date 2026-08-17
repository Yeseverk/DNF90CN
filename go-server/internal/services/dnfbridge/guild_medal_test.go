package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestDecodeCurrentGuardianGemUseRequestMatchesCurrentEXEWriter(t *testing.T) {
	// Captured from the current client on 2026-07-22 while selecting the gem
	// at list38:57 for socket 3 of the worn medal.
	body := []byte{0x9c, 0xad, 0xfb, 0x05, 0x39, 0x00, 0xe3, 0x5f, 0x01, 0x00, 0x03}

	request, err := decodeCurrentGuardianGemUseRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.TargetMedalItemID != 100380060 || request.GuardianGemSourceSlot != 57 || request.GuardianGemItemID != 90083 || request.SocketIndex != 3 {
		t.Fatalf("guardian-gem request = %+v", request)
	}
}

func TestDecodeCurrentGuardianGemUseRequestRejectsUnprovenBodies(t *testing.T) {
	valid := make([]byte, currentGuardianGemUseRequestWireSize)
	binary.LittleEndian.PutUint32(valid[0:4], 100380017)
	binary.LittleEndian.PutUint16(valid[4:6], 49)
	binary.LittleEndian.PutUint32(valid[6:10], 90002)
	for _, body := range [][]byte{
		valid[:10],
		append(append([]byte(nil), valid...), 0, 0, 0, 0),
		func() []byte { value := append([]byte(nil), valid...); value[10] = 4; return value }(),
		func() []byte {
			value := append([]byte(nil), valid...)
			binary.LittleEndian.PutUint16(value[4:6], 48)
			return value
		}(),
		func() []byte {
			value := append([]byte(nil), valid...)
			binary.LittleEndian.PutUint32(value[0:4], 0)
			return value
		}(),
	} {
		if _, err := decodeCurrentGuardianGemUseRequest(body); err == nil {
			t.Fatalf("body %x unexpectedly decoded", body)
		}
	}
}

func TestResolveCurrentGuardianGemUsesPVFTypeGradeAndEnchantFamily(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":     "",
		"equipment/equipment.lst": "",
		"stackable/stackable.lst": "90002 `flaggem/physical_defense_grade2.stk`\n90010 `material/not_a_gem.stk`\n",
		"stackable/flaggem/physical_defense_grade2.stk": `
[stackable type]
` + "`[flag gem]`" + `
[grade]
2
[enchant]
[equipment physical defense]
360 360
[/enchant]
`,
		"stackable/material/not_a_gem.stk": `
[stackable type]
` + "`[material]`" + `
[grade]
2
[enchant]
[equipment physical defense]
360 360
[/enchant]
`,
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	gem, err := resolveCurrentGuardianGem(catalog, 90002)
	if err != nil {
		t.Fatal(err)
	}
	if gem.ItemID != 90002 || gem.Grade != 2 || gem.EnchantFamily != "equipment physical defense" || gem.PVFPath != "stackable/flaggem/physical_defense_grade2.stk" {
		t.Fatalf("guardian gem = %+v", gem)
	}
	if _, err := resolveCurrentGuardianGem(catalog, 90010); !errors.Is(err, errCurrentGuardianGemNotFlagGem) {
		t.Fatalf("non-flag-gem error=%v", err)
	}
}

func TestResolveCurrentGuardianGemRejectsAmbiguousEnchantAndInvalidGrade(t *testing.T) {
	for name, text := range map[string]string{
		"ambiguous": `
[stackable type]
` + "`[flag gem]`" + `
[grade]
1
[enchant]
[equipment physical defense]
270 270
[equipment magical defense]
270 270
[/enchant]
`,
		"invalid_grade": `
[stackable type]
` + "`[flag gem]`" + `
[grade]
4
[enchant]
[equipment physical defense]
270 270
[/enchant]
`,
	} {
		t.Run(name, func(t *testing.T) {
			source := dungeonDropCatalogTestSource{
				"monster/monster.lst":        "",
				"equipment/equipment.lst":    "",
				"stackable/stackable.lst":    "90000 `flaggem/test.stk`\n",
				"stackable/flaggem/test.stk": text,
			}
			catalog, err := newPVFDungeonDropCatalog(source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := resolveCurrentGuardianGem(catalog, 90000); err == nil {
				t.Fatal("invalid guardian gem unexpectedly resolved")
			}
		})
	}
}

func TestResolveCurrentGuardianGemMedal(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":     "",
		"stackable/stackable.lst": "",
		"equipment/equipment.lst": "100380017 `flag/100380017.equ`\n100000001 `weapon/100000001.equ`\n",
		"equipment/flag/100380017.equ": `
[equipment type]
` + "`[flag]`" + `
`,
		"equipment/weapon/100000001.equ": `
[equipment type]
` + "`[weapon]`" + `
`,
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveCurrentGuardianGemMedal(catalog, 100380017)
	if err != nil {
		t.Fatalf("resolve guild medal: %v", err)
	}
	if got.ItemID != 100380017 || got.PVFPath != "equipment/flag/100380017.equ" {
		t.Fatalf("resolved guild medal = %#v", got)
	}
	if _, err := resolveCurrentGuardianGemMedal(catalog, 100000001); !errors.Is(err, errCurrentGuardianGemTargetNotMedal) {
		t.Fatalf("wrong equipment type error=%v, want %v", err, errCurrentGuardianGemTargetNotMedal)
	}
}

func TestCurrentGuardianGemUseSnapshotFromRealInventoryLocations(t *testing.T) {
	request := currentGuardianGemUseRequest{TargetMedalItemID: 100380017, GuardianGemSourceSlot: 49, GuardianGemItemID: 90000}
	var snapshot currentGuardianGemUseSnapshot
	currentGuardianGemInspectInventoryMap(&snapshot, map[string]dnfrepo.ItemStack{
		"38:49": {ItemID: 90000, Count: 3},
		"38:32": {ItemID: 100380017, Count: 1},
		"2:7":   {ItemID: 90000, Count: 4},
	}, request, false)
	currentGuardianGemInspectInventoryMap(&snapshot, map[string]dnfrepo.ItemStack{
		"2:9":  {ItemID: 90000, Count: 5},
		"2:32": {ItemID: 100380017, Count: 1},
	}, request, true)
	if snapshot.GemMainStackCount != 3 || snapshot.GemWarehouseStackCount != 0 {
		t.Fatalf("gem stack snapshot = %#v", snapshot)
	}
	if snapshot.TargetMainMatches != 0 || snapshot.TargetWarehouseMatches != 0 || snapshot.TargetEquippedMatches != 0 {
		t.Fatalf("target snapshot = %#v", snapshot)
	}
}

func TestCurrentGuardianGemCommitWritesExactWornMedalRawSocketAndConsumesExactSourceSlot(t *testing.T) {
	service, repositories := newCurrentGuardianGemTestService(t)
	targetRaw := currentGuardianGemTestRaw(32, 100380017, 1)
	targetRaw[109] = 0x5A // adjacent byte is not a guardian socket and must survive.
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"38:49": {ItemID: 90002, Count: 2},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repositories.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"32": {SlotIndex: 32, ItemID: 100380017, RawEntry: targetRaw},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}

	result, err := service.commitCurrentGuardianGemUse(
		&gameSession{selectedCharacterID: 77},
		currentGuardianGemUseRequest{TargetMedalItemID: 100380017, GuardianGemSourceSlot: 49, GuardianGemItemID: 90002, SocketIndex: 2},
	)
	if err != nil {
		t.Fatalf("commit guardian gem: %v", err)
	}
	if result.Target.Container != currentGuardianGemTargetEquipped || result.Target.ListType != 3 || result.Target.Slot != 32 || result.Source != (currentSocketChangedSlot{ListType: 38, Slot: 49}) {
		t.Fatalf("guardian gem result=%+v", result)
	}

	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if got := inventory.Slots["38:49"].Count; got != 1 {
		t.Fatalf("gem count=%d, want=1", got)
	}
	equipment, found, err := repositories.Equipment.Load(context.Background(), "77")
	if err != nil || !found {
		t.Fatalf("load equipment found=%v err=%v", found, err)
	}
	raw := equipment.Entries["32"].RawEntry
	if len(raw) != currentItemListEntryWireSize {
		t.Fatalf("target raw length=%d", len(raw))
	}
	if got := binary.LittleEndian.Uint16(raw[105:107]); got != 3 {
		t.Fatalf("socket 2 raw value=%d, want itemID-base=3 raw=%x", got, raw[101:110])
	}
	if got := binary.LittleEndian.Uint16(raw[101:103]); got != 0 {
		t.Fatalf("socket 0 unexpectedly changed=%d raw=%x", got, raw[101:110])
	}
	if raw[109] != 0x5A {
		t.Fatalf("adjacent raw byte=%02x, want 5a", raw[109])
	}
	if row, ok := currentItemListEntryFromEquipment(equipment.Entries["32"]); !ok || !bytes.Equal(row.data[101:110], raw[101:110]) {
		t.Fatalf("refreshed target raw socket state=%x persisted=%x", row.data[101:110], raw[101:110])
	}
}

func TestCurrentGuardianGemCommitCanTargetWornMedal(t *testing.T) {
	service, repositories := newCurrentGuardianGemTestService(t)
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]dnfrepo.ItemStack{"38:49": {ItemID: 90002, Count: 1}},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repositories.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"32": {SlotIndex: 32, ItemID: 100380017, RawEntry: currentGuardianGemTestRaw(32, 100380017, 1)},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}

	result, err := service.commitCurrentGuardianGemUse(
		&gameSession{selectedCharacterID: 77},
		currentGuardianGemUseRequest{TargetMedalItemID: 100380017, GuardianGemSourceSlot: 49, GuardianGemItemID: 90002, SocketIndex: 3},
	)
	if err != nil {
		t.Fatalf("commit guardian gem on worn medal: %v", err)
	}
	if result.Target.Container != currentGuardianGemTargetEquipped || result.Target.Slot != 32 {
		t.Fatalf("worn guardian gem result=%+v", result)
	}
	if inventory := mustLoadCurrentSocketInventory(t, repositories, "77"); len(inventory.Slots) != 0 {
		t.Fatalf("one gem was not consumed from medal inventory: %+v", inventory.Slots)
	}
	equipment, found, err := repositories.Equipment.Load(context.Background(), "77")
	if err != nil || !found {
		t.Fatalf("load equipment found=%v err=%v", found, err)
	}
	if got := binary.LittleEndian.Uint16(equipment.Entries["32"].RawEntry[107:109]); got != 3 {
		t.Fatalf("worn medal socket 3=%d, want=3", got)
	}
}

func TestCurrentGuardianGemCommitReplacesOccupiedSocketAndConsumesOnlyNewGem(t *testing.T) {
	service, repositories := newCurrentGuardianGemTestService(t)
	targetRaw := currentGuardianGemTestRaw(32, 100380017, 1)
	binary.LittleEndian.PutUint16(targetRaw[101:103], 1)
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"38:49": {ItemID: 90002, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repositories.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"32": {SlotIndex: 32, ItemID: 100380017, RawEntry: targetRaw},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}

	_, err := service.commitCurrentGuardianGemUse(
		&gameSession{selectedCharacterID: 77},
		currentGuardianGemUseRequest{TargetMedalItemID: 100380017, GuardianGemSourceSlot: 49, GuardianGemItemID: 90002, SocketIndex: 0},
	)
	if err != nil {
		t.Fatalf("replace occupied guardian gem: %v", err)
	}
	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if _, found := inventory.Slots["38:49"]; found {
		t.Fatalf("replacement source gem was not consumed: %+v", inventory.Slots)
	}
	equipment, found, loadErr := repositories.Equipment.Load(context.Background(), "77")
	if loadErr != nil || !found {
		t.Fatalf("load equipment found=%v err=%v", found, loadErr)
	}
	if got := binary.LittleEndian.Uint16(equipment.Entries["32"].RawEntry[101:103]); got != 3 {
		t.Fatalf("replacement socket value=%d, want new item id delta 3", got)
	}
}

func TestCurrentGuardianGemHandlerSendsOnlyRealItemRefreshes(t *testing.T) {
	service, repositories := newCurrentGuardianGemTestService(t)
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"38:49": {ItemID: 90002, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repositories.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"32": {SlotIndex: 32, ItemID: 100380017, RawEntry: currentGuardianGemTestRaw(32, 100380017, 1)},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	connection := &bufferConn{}
	session := &gameSession{selectedCharacterID: 77, connID: "guardian-gem-handler-test", conn: connection}
	body := make([]byte, currentGuardianGemUseRequestWireSize)
	binary.LittleEndian.PutUint32(body[0:4], 100380017)
	binary.LittleEndian.PutUint16(body[4:6], 49)
	binary.LittleEndian.PutUint32(body[6:10], 90002)
	body[10] = 0
	if err := service.handleCurrentGuardianGemUse(session, body); err != nil {
		t.Fatalf("handle guardian gem: %v", err)
	}

	target, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if target.Header.Classification != 0 || target.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(target.Body) != 3+currentItemListEntryWireSize || target.Body[0] != 3 ||
		binary.LittleEndian.Uint16(target.Body[1:3]) != 1 || binary.LittleEndian.Uint16(target.Body[3:5]) != 32 ||
		binary.LittleEndian.Uint32(target.Body[5:9]) != 100380017 || binary.LittleEndian.Uint16(target.Body[3+101:3+103]) != 3 {
		t.Fatalf("guardian target refresh=%+v body=%x", target.Header, target.Body)
	}
	consumed, trailing := splitCurrentGameServerUpperPacketAuto(t, rest)
	if consumed.Header.Classification != 0 || consumed.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) ||
		!bytes.Equal(consumed.Body, []byte{currentGuardianGemInventoryListType, 0, 0}) {
		t.Fatalf("guardian list38 refresh=%+v body=%x", consumed.Header, consumed.Body)
	}
	if len(trailing) != 0 {
		t.Fatalf("unexpected guardian-gem ACK/state packet=%x", trailing)
	}
}

func TestSelectedGuardianGemWornMedalRefreshReplaysPersistedRawSocketWords(t *testing.T) {
	service, repositories := newCurrentGuardianGemTestService(t)
	medalRaw := currentGuardianGemTestRaw(32, 100380017, 1)
	binary.LittleEndian.PutUint16(medalRaw[101:103], 3)
	if err := repositories.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"32": {SlotIndex: 32, ItemID: 100380017, RawEntry: medalRaw},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	connection := &bufferConn{}
	session := &gameSession{selectedCharacterID: 77, connID: "guardian-gem-select-rehydrate", conn: connection}
	if err := service.sendSelectedGuardianGemWornMedalRefresh(session, "test"); err != nil {
		t.Fatalf("rehydrate guardian-gem worn medal: %v", err)
	}
	packet, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || packet.Header.Classification != 0 || packet.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(packet.Body) != 3+currentItemListEntryWireSize || packet.Body[0] != currentSocketListEquipment ||
		binary.LittleEndian.Uint16(packet.Body[3:5]) != 32 || binary.LittleEndian.Uint32(packet.Body[5:9]) != 100380017 ||
		binary.LittleEndian.Uint16(packet.Body[3+101:3+103]) != 3 {
		t.Fatalf("guardian-gem select rehydrate=%+v body=%x", packet.Header, packet.Body)
	}
}

func TestCurrentGuardianGemRawSocketOccupied(t *testing.T) {
	raw := currentGuardianGemTestRaw(32, 100380017, 1)
	if currentGuardianGemRawSocketOccupied(raw) {
		t.Fatal("empty raw socket state reported occupied")
	}
	binary.LittleEndian.PutUint16(raw[107:109], 7)
	if !currentGuardianGemRawSocketOccupied(raw) {
		t.Fatal("nonzero raw socket state reported empty")
	}
}

func TestCurrentGuardianGemCommitRejectsGemStoredOnMedalPage(t *testing.T) {
	service, repositories := newCurrentGuardianGemTestService(t)
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"38:7": {ItemID: 90002, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repositories.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"32": {SlotIndex: 32, ItemID: 100380017, RawEntry: currentGuardianGemTestRaw(32, 100380017, 1)},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}

	_, err := service.commitCurrentGuardianGemUse(
		&gameSession{selectedCharacterID: 77},
		currentGuardianGemUseRequest{TargetMedalItemID: 100380017, GuardianGemSourceSlot: 49, GuardianGemItemID: 90002, SocketIndex: 0},
	)
	if !errors.Is(err, errCurrentGuardianGemSourceMissing) {
		t.Fatalf("medal-page guardian gem error=%v, want %v", err, errCurrentGuardianGemSourceMissing)
	}
	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if got := inventory.Slots["38:7"].Count; got != 1 {
		t.Fatalf("rejected medal-page gem count=%d, want=1", got)
	}
}

func TestCurrentGuardianGemCommitConsumesOnlyRequestedSourceSlot(t *testing.T) {
	service, repositories := newCurrentGuardianGemTestService(t)
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"38:49": {ItemID: 90002, Count: 4},
			"38:50": {ItemID: 90002, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repositories.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"32": {SlotIndex: 32, ItemID: 100380017, RawEntry: currentGuardianGemTestRaw(32, 100380017, 1)},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}

	result, err := service.commitCurrentGuardianGemUse(
		&gameSession{selectedCharacterID: 77},
		currentGuardianGemUseRequest{TargetMedalItemID: 100380017, GuardianGemSourceSlot: 50, GuardianGemItemID: 90002, SocketIndex: 0},
	)
	if err != nil {
		t.Fatalf("commit exact source slot: %v", err)
	}
	if result.Source.Slot != 50 {
		t.Fatalf("consumed source slot=%d, want=50", result.Source.Slot)
	}
	inventory := mustLoadCurrentSocketInventory(t, repositories, "77")
	if got := inventory.Slots["38:49"].Count; got != 4 {
		t.Fatalf("unselected same-id stack count=%d, want=4", got)
	}
	if _, found := inventory.Slots["38:50"]; found {
		t.Fatalf("selected one-count source stack was not removed: %+v", inventory.Slots["38:50"])
	}
}

func TestCurrentGuardianGemCommitRejectsUnwornMedalWithoutConsumingGem(t *testing.T) {
	service, repositories := newCurrentGuardianGemTestService(t)
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"38:32": {ItemID: 100380017, Count: 1, RawEntry: currentGuardianGemTestRaw(32, 100380017, 1)},
			"38:49": {ItemID: 90002, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}

	_, err := service.commitCurrentGuardianGemUse(
		&gameSession{selectedCharacterID: 77},
		currentGuardianGemUseRequest{TargetMedalItemID: 100380017, GuardianGemSourceSlot: 49, GuardianGemItemID: 90002, SocketIndex: 0},
	)
	if !errors.Is(err, errCurrentGuardianGemTargetMissing) {
		t.Fatalf("unworn medal error=%v, want %v", err, errCurrentGuardianGemTargetMissing)
	}
	if got := mustLoadCurrentSocketInventory(t, repositories, "77").Slots["38:49"].Count; got != 1 {
		t.Fatalf("unworn target consumed source gem count=%d", got)
	}
}

func newCurrentGuardianGemTestService(t *testing.T) (*Service, dnfrepo.Group) {
	t.Helper()
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":     "",
		"stackable/stackable.lst": "90002 `flaggem/test.stk`\n",
		"equipment/equipment.lst": "100380017 `flag/test_medal.equ`\n",
		"stackable/flaggem/test.stk": `
[stackable type]
` + "`[flag gem]`" + `
[grade]
2
[enchant]
[equipment physical defense]
360 360
[/enchant]
`,
		"equipment/flag/test_medal.equ": `
[equipment type]
` + "`[flag]`" + `
`,
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatalf("new guardian gem test catalog: %v", err)
	}
	repositories := testRepositoryGroup()
	return &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		pvfItemCatalog:     catalog,
	}, repositories
}

func currentGuardianGemTestRaw(slot int16, itemID uint32, count uint32) []byte {
	var entry currentItemListEntry
	entry.patchCore(slot, itemID, count)
	return append([]byte(nil), entry.data[:]...)
}

func TestRealPVFGuardianGemCatalog(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to the runtime Script.pvf")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}

	countByGrade := make(map[byte]int)
	countByFamily := make(map[string]int)
	count := 0
	for itemID, reference := range catalog.itemRefs {
		if reference.kind != dungeonDropItemStackable || !strings.HasPrefix(strings.ToLower(reference.path), "flaggem/") {
			continue
		}
		gem, err := resolveCurrentGuardianGem(catalog, itemID)
		if err != nil {
			t.Fatalf("runtime guardian gem item=%d path=%q: %v", itemID, reference.path, err)
		}
		count++
		countByGrade[gem.Grade]++
		countByFamily[gem.EnchantFamily]++
	}
	if count != 52 || len(countByGrade) != 4 || len(countByFamily) != 13 {
		t.Fatalf("runtime guardian gem catalog count=%d grades=%v families=%v", count, countByGrade, countByFamily)
	}
	for grade := byte(0); grade < 4; grade++ {
		if countByGrade[grade] != 13 {
			t.Fatalf("runtime guardian gem grade=%d count=%d, want 13", grade, countByGrade[grade])
		}
	}
	for family, familyCount := range countByFamily {
		if familyCount != 4 {
			t.Fatalf("runtime guardian gem family=%q count=%d, want 4", family, familyCount)
		}
	}
}
