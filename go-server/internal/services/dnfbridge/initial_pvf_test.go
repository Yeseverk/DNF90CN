package dnfbridge

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestBuildCharacterPVFInitializationFollowsCSharpSources(t *testing.T) {
	source := initialEquipmentMemSource{
		"character/character.lst": "15 `fighter.chr`\n",
		"character/fighter.chr": `
[job]
` + "`[fighter]`" + `
[initial value]
[HP MAX] 100
[MP MAX] 50
[strength] 8
[physical attack] 3
[inventory limit] 48000
[skill]
1001 1
1002 2
[/skill]
[create equipment list]
[weapon] 101
[coat] 102
[/create equipment list]
[growtype 1]
[HP MAX] 10
`,
		"equipment/equipment.lst":    "101 `weapon/spear.equ`\n102 `armor/coat.equ`\n",
		"equipment/weapon/spear.equ": "[durability]\n33\n[equipment type]\n1\n",
		"equipment/armor/coat.equ":   "[durability]\n40\n[equipment type]\n2\n",
		"skill/skilllist.lst":        "15 `fighter_skill.lst`\n",
		"skill/fighter_skill.lst":    "1001 `fighter/active.skl`\n1002 `fighter/passive.skl`\n",
		"skill/fighter/active.skl":   "[skill type]\n`active`\n",
		"skill/fighter/passive.skl":  "[skill type]\n`passive`\n",
		"Etc/spTable.etc":            "[sp table]\n1 20\n2 30\n[/sp table]\n[tp table]\n50 1\n[/tp table]\n",
	}

	init, err := buildCharacterPVFInitializationFromSource(context.Background(), source, 15, 2, 0)
	if err != nil {
		t.Fatalf("build initialization: %v", err)
	}
	if !init.HasStats || init.Stats.HPMax != 1100 || init.Stats.MPMax != 500 || init.Stats.InventoryLimit != 480000 {
		t.Fatalf("stats = %+v has=%t", init.Stats, init.HasStats)
	}
	if len(init.Equipment) != 2 || init.Equipment[0].Slot != 11 || init.Equipment[0].ItemID != 101 || init.Equipment[0].Durability != 33 {
		t.Fatalf("equipment = %+v", init.Equipment)
	}
	if len(init.Skills) != 2 {
		t.Fatalf("skills = %+v", init.Skills)
	}
	if init.Skills[0].SkillID != 1001 || init.Skills[0].Level != 1 {
		t.Fatalf("active skill = %+v", init.Skills[0])
	}
	if init.Skills[1].SkillID != 1002 || init.Skills[1].Level != 2 {
		t.Fatalf("passive skill = %+v", init.Skills[1])
	}
	if init.SkillPoints.TotalSP != 50 || init.SkillPoints.RemainingSP != 50 || init.SkillPoints.TotalTP != 0 || init.SkillPoints.SyncedLevel != 2 {
		t.Fatalf("skill points = %+v", init.SkillPoints)
	}
}

func TestParseInitialSkillPointsUsesPVFTPTable(t *testing.T) {
	points, err := parseInitialSkillPointsFromSource(initialEquipmentMemSource{
		"Etc/spTable.etc": "[sp table]\n1 0\n2 30\n50 60\n[/sp table]\n[tp table]\n50 1\n51 2\n[/tp table]\n",
	}, 51)
	if err != nil {
		t.Fatal(err)
	}
	if points.TotalSP != 90 || points.RemainingSP != 90 || points.TotalTP != 3 || points.RemainingTP != 3 || points.SyncedLevel != 51 {
		t.Fatalf("skill points=%+v", points)
	}
}

func TestSaveNewCharacterPVFFailureLeavesNoPartialRecords(t *testing.T) {
	repos := testRepositoryGroup()
	service := &Service{options: options{pvfPath: filepath.Join(t.TempDir(), "missing.pvf")}}
	now := time.Now().UTC()
	record := dnfrepo.CharacterRecord{
		CharacterID: "199",
		AccountID:   "dnf:1",
		Slot:        0,
		Name:        "hero",
		Job:         "15",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := service.saveNewCharacter(context.Background(), repos, record); err == nil {
		t.Fatal("save new character succeeded without PVF initialization")
	}
	if _, found, err := repos.Character.Load(context.Background(), "199"); err != nil || found {
		t.Fatalf("partial character found=%t err=%v", found, err)
	}
	if _, found, err := repos.Inventory.Load(context.Background(), "199"); err != nil || found {
		t.Fatalf("orphan inventory found=%t err=%v", found, err)
	}
	if _, found, err := repos.Equipment.Load(context.Background(), "199"); err != nil || found {
		t.Fatalf("orphan equipment found=%t err=%v", found, err)
	}
	if _, found, err := repos.Skill.Load(context.Background(), "199"); err != nil || found {
		t.Fatalf("orphan skills found=%t err=%v", found, err)
	}
	if _, found, err := repos.Settings.Load(context.Background(), newCharacterContainerStateSettingsScope("199")); err != nil || found {
		t.Fatalf("orphan settings found=%t err=%v", found, err)
	}
}

func TestSaveNewCharacterSeedsPVFInitializationSnapshot(t *testing.T) {
	repos := testRepositoryGroup()
	now := time.Now().UTC()
	service := &Service{
		initialEquipmentByJob: map[byte][]initialEquipmentEntry{
			15: {
				{
					Slot:       11,
					ItemID:     900001,
					Durability: 27,
					EquipType:  1,
					PVFPath:    "equipment/weapon/test.equ",
					RawEntry:   buildInitialEquipmentRawEntry(11, 900001, 27),
				},
			},
		},
		initialSkillsByJob: map[byte][]initialSkillEntry{
			15: {
				{SkillID: 1001, Level: 1},
				{SkillID: 1002, Level: 2},
			},
		},
		initialSPTable: map[int]int{1: 20},
	}
	record := dnfrepo.CharacterRecord{
		CharacterID: "177",
		AccountID:   "dnf:1",
		Slot:        0,
		Name:        "hero",
		Job:         "15",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := service.saveNewCharacter(context.Background(), repos, record); err != nil {
		t.Fatalf("save new character: %v", err)
	}
	equipment, found, err := repos.Equipment.Load(context.Background(), "177")
	if err != nil || !found || len(equipment.Entries) != 1 {
		t.Fatalf("equipment found=%t err=%v entries=%+v", found, err, equipment.Entries)
	}
	skills, found, err := repos.Skill.Load(context.Background(), "177")
	if err != nil || !found {
		t.Fatalf("skills found=%t err=%v", found, err)
	}
	if got := skills.Skills[1001]; got.Level != 1 || !got.Enabled {
		t.Fatalf("skill 1001 = %+v", got)
	}
	if got := skills.Skills[1002]; got.Level != 2 || !got.Enabled {
		t.Fatalf("skill 1002 = %+v", got)
	}
	if skills.Points.TotalSP != 20 || skills.Points.RemainingSP != 20 || skills.Points.TotalTP != 0 || skills.Points.SyncedLevel != 1 {
		t.Fatalf("skill points = %+v", skills.Points)
	}
}

func TestEnsureCharacterInitializationSnapshotBackfillsLegacyRecordsOnce(t *testing.T) {
	repos := testRepositoryGroup()
	service := &Service{}
	prepareTestCharacterInitialization(service, 15)
	record := dnfrepo.CharacterRecord{
		CharacterID: "178",
		AccountID:   "dnf:1",
		Job:         "15",
		Level:       1,
	}

	result, err := service.ensureCharacterInitializationSnapshot(context.Background(), repos, record)
	if err != nil {
		t.Fatalf("backfill legacy snapshot: %v", err)
	}
	if !result.Inventory || !result.Equipment || !result.Skill || len(result.Settings) != 3 {
		t.Fatalf("backfill result = %+v", result)
	}
	inventory, found, err := repos.Inventory.Load(context.Background(), "178")
	if err != nil || !found || inventory.Slots[csharpReviveCoinSlotKey()].ItemID != csharpReviveCoinItemID {
		t.Fatalf("inventory initialized found=%t err=%v inventory=%+v", found, err, inventory)
	}
	equipment, found, err := repos.Equipment.Load(context.Background(), "178")
	if err != nil || !found || len(equipment.Entries) != 0 {
		t.Fatalf("empty equipment record initialized found=%t err=%v equipment=%+v", found, err, equipment)
	}

	// Deliberately change durable state, then ensure a second time.  It must
	// behave like 86JP's existing-seed branch and make no replacement write.
	inventory.Slots["0:1"] = dnfrepo.ItemStack{ItemID: 999, Count: 3}
	if err := repos.Inventory.Save(context.Background(), inventory); err != nil {
		t.Fatal(err)
	}
	if equipment.Entries == nil {
		equipment.Entries = make(map[string]dnfrepo.EquipmentEntry)
	}
	equipment.Entries["11"] = dnfrepo.EquipmentEntry{SlotIndex: 11, ItemID: 888}
	if err := repos.Equipment.Save(context.Background(), equipment); err != nil {
		t.Fatal(err)
	}
	result, err = service.ensureCharacterInitializationSnapshot(context.Background(), repos, record)
	if err != nil {
		t.Fatalf("repeat legacy snapshot ensure: %v", err)
	}
	if result.Changed() {
		t.Fatalf("present legacy state was reseeded: %+v", result)
	}
	inventory, _, _ = repos.Inventory.Load(context.Background(), "178")
	equipment, _, _ = repos.Equipment.Load(context.Background(), "178")
	if inventory.Slots["0:1"].ItemID != 999 || equipment.Entries["11"].ItemID != 888 {
		t.Fatalf("durable state overwritten inventory=%+v equipment=%+v", inventory, equipment)
	}
}

func TestSaveNewCharacterSeedsCSharpDefaultState(t *testing.T) {
	repos := testRepositoryGroup()
	now := time.Now().UTC()
	service := &Service{}
	prepareTestCharacterInitialization(service, 15)
	record := dnfrepo.CharacterRecord{
		CharacterID: "188",
		AccountID:   "dnf:1",
		Slot:        0,
		Name:        "hero",
		Job:         "15",
		Level:       newCharacterInitialLevel,
		Stats:       defaultCreatedCharacterStatsFromRequest(createCharacterRequest{}),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := service.saveNewCharacter(context.Background(), repos, record); err != nil {
		t.Fatalf("save new character: %v", err)
	}
	saved, found, err := repos.Character.Load(context.Background(), "188")
	if err != nil || !found {
		t.Fatalf("character found=%t err=%v", found, err)
	}
	if saved.Level != newCharacterInitialLevel ||
		saved.Stats["stat_level"] != csharpSubtype1ProtocolStatLevel ||
		saved.Stats["town_id"] != newCharacterInitialTownID ||
		saved.Stats["area_id"] != newCharacterInitialAreaID ||
		saved.Stats["pos_x"] != newCharacterInitialPosX ||
		saved.Stats["pos_y"] != newCharacterInitialPosY ||
		saved.Stats["area_state"] != newCharacterInitialAreaState ||
		saved.Stats[dnfrepo.CharacterStoryDigestLastLevelStatKey] != newCharacterInitialLevel ||
		saved.Stats[dnfrepo.CharacterStoryDigestMigrationVersionStatKey] != int64(dnfrepo.CurrentCharacterStoryDigestMigrationVersion) {
		t.Fatalf("saved defaults level=%d stats=%+v", saved.Level, saved.Stats)
	}

	inventory, found, err := repos.Inventory.Load(context.Background(), "188")
	if err != nil || !found {
		t.Fatalf("inventory found=%t err=%v", found, err)
	}
	coin := inventory.Slots[csharpReviveCoinSlotKey()]
	if coin.ItemID != csharpReviveCoinItemID || coin.Count != 0 || coin.Extra["amount_or_count"] != "0" {
		t.Fatalf("revive coin slot = %+v", coin)
	}

	container, found, err := repos.Settings.Load(context.Background(), newCharacterContainerStateSettingsScope("188"))
	if err != nil || !found {
		t.Fatalf("container settings found=%t err=%v", found, err)
	}
	if container.Values["main_list_param16"] != "0" ||
		container.Values["avatar_list_param16"] != "0" ||
		container.Values["personal_cargo_list_param16"] != "8" ||
		container.Values["revive_coin_wallet_slot"] != "1" {
		t.Fatalf("container settings = %+v", container.Values)
	}

	initBodies, found, err := repos.Settings.Load(context.Background(), newCharacterInitBodiesSettingsScope("188"))
	if err != nil || !found {
		t.Fatalf("init body settings found=%t err=%v", found, err)
	}
	if initBodies.Values["noti_0035_occurrence_0_len"] != "13" ||
		initBodies.Values["noti_0357_occurrence_0_hex"] != "7b03000000000000" ||
		initBodies.Values["noti_03d8_occurrence_0_len"] != "204" ||
		initBodies.Values["noti_0077_occurrence_1_hex"] != "00" {
		t.Fatalf("init bodies = %+v", initBodies.Values)
	}

	hotkeys, found, err := repos.Settings.Load(context.Background(), newCharacterHotkeySettingsScope("188"))
	if err != nil || !found {
		t.Fatalf("hotkey settings found=%t err=%v", found, err)
	}
	if hotkeys.Values["key_type"] != "0" ||
		hotkeys.Values["slot_count"] != "99" ||
		len(hotkeys.Values["slots_hex"]) < 16 ||
		hotkeys.Values["slots_hex"][:16] != "0200000003000100" {
		t.Fatalf("hotkeys = %+v", hotkeys.Values)
	}
}

func TestCSharpUserInfoStatLevelUsesProtocolField(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		Level: newCharacterInitialLevel,
		Stats: map[string]int64{"stat_level": int64(newCharacterInitialLevel)},
	}
	if got := csharpUserInfoStatLevel(nil, character); got != csharpSubtype1ProtocolStatLevel {
		t.Fatalf("stat level from old Go level fallback = %d, want %d", got, csharpSubtype1ProtocolStatLevel)
	}
	if got := csharpUserInfoStatLevel(dnfrepo.LegacyUserInfoRow{"stat_level": "56"}, character); got != 56 {
		t.Fatalf("legacy row stat level = %d, want 56", got)
	}
	character.Stats["stat_level"] = csharpSubtype1ProtocolStatLevel
	if got := csharpUserInfoStatLevel(nil, character); got != csharpSubtype1ProtocolStatLevel {
		t.Fatalf("stat level = %d, want %d", got, csharpSubtype1ProtocolStatLevel)
	}
}

func TestParseCSharpCreatorHotkeyValues(t *testing.T) {
	keys := parseCSharpCreatorHotkeyValues("[key] `a` 0 `b` `c` 42\n[key] `x` 0 `y` `z` -1\n")
	if len(keys) != 2 || keys[0] != 42 || keys[1] != csharpHotkeyUnassignedKey {
		t.Fatalf("creator hotkeys = %+v", keys)
	}
}
