// initial_equipment_test.go 验证 C# 初始装备解析链在 Go 侧的槽位和 raw entry 形态。
package dnfbridge

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

type initialEquipmentMemSource map[string]string

func (s initialEquipmentMemSource) ReadText(relativePath string) (string, error) {
	value, ok := s[cleanInitialPVFPath(relativePath)]
	if !ok {
		return "", errInitialEquipmentTestMissing(relativePath)
	}
	return value, nil
}

type errInitialEquipmentTestMissing string

func (e errInitialEquipmentTestMissing) Error() string { return "missing pvf text: " + string(e) }

func TestParseInitialCharacterEquipmentFromSourceUsesDOVEStarterSlotsAndPVFData(t *testing.T) {
	source := initialEquipmentMemSource{
		"character/character.lst":    "14 `old.chr`\n15 `fighter.chr`\n",
		"character/fighter.chr":      "[job]\n`[fighter]`\n[basic]\n0\n[create equipment list]\n[weapon] 101\n[coat]` 102\n[pants] 103\n[waist] 104\n[shoes] 105\n[magic stone] 106\n[/create equipment list]\n",
		"equipment/equipment.lst":    "101 `weapon/spear.equ`\n102 `armor/coat.equ`\n103 `armor/pants.equ`\n104 `armor/waist.equ`\n105 `armor/shoes.equ`\n106 `special/magicstone.equ`\n",
		"equipment/weapon/spear.equ": "[durability]\n33\n[equipment type]\n1\n[animation job]\n`[fighter]`\n[layer variation]\n2150 `ft_speara`\n[equipment ani script]\n`equipment/character/fighter.lay`\n[layer variation]\n650 `ft_spearb`\n[equipment ani script]\n`equipment/character/fighter.lay`\n[animation job]\n`[swordman]`\n[layer variation]\n999 `wrong_job`\n",
		"equipment/armor/coat.equ":   "[durability]\n40\n[equipment type]\n2\n",
		"equipment/armor/pants.equ":  "[durability]\n41\n[equipment type]\n3\n",
	}

	entries, err := parseInitialCharacterEquipmentFromSource(source, 15)
	if err != nil {
		t.Fatalf("parse initial equipment: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3: %+v", len(entries), entries)
	}
	want := []struct {
		slot       int16
		itemID     int64
		durability uint16
	}{
		{slot: 11, itemID: 101, durability: 33},
		{slot: 13, itemID: 102, durability: 40},
		{slot: 15, itemID: 103, durability: 41},
	}
	for idx, entry := range entries {
		if entry.Slot != want[idx].slot || entry.ItemID != want[idx].itemID || entry.Durability != want[idx].durability {
			t.Fatalf("entry[%d] = slot %d item %d dur %d, want %+v", idx, entry.Slot, entry.ItemID, entry.Durability, want[idx])
		}
		raw := entry.RawEntry
		if len(raw) != 43 {
			t.Fatalf("entry[%d] raw len = %d, want 43", idx, len(raw))
		}
		if raw[0] != byte(entry.Slot) ||
			binary.LittleEndian.Uint32(raw[1:5]) != uint32(entry.ItemID) ||
			binary.LittleEndian.Uint32(raw[5:9]) != initialEquipmentCreateValue ||
			binary.LittleEndian.Uint16(raw[10:12]) != entry.Durability {
			t.Fatalf("entry[%d] raw header = %x", idx, raw[:12])
		}
	}
	if entries[0].EquipType != 1 || entries[1].EquipType != 2 || entries[2].EquipType != 3 {
		t.Fatalf("equipment types = %d/%d/%d", entries[0].EquipType, entries[1].EquipType, entries[2].EquipType)
	}
	if got := entries[0].ModelLayers; len(got) != 2 ||
		got[0].Key != 2150 || got[0].Name != "ft_speara" || got[0].Script != "equipment/character/fighter.lay" ||
		got[1].Key != 650 || got[1].Name != "ft_spearb" {
		t.Fatalf("weapon model layers = %+v", got)
	}
	if len(entries[1].ModelLayers) != 0 || len(entries[2].ModelLayers) != 0 {
		t.Fatalf("non-weapon model layers = %+v / %+v", entries[1].ModelLayers, entries[2].ModelLayers)
	}
}

func TestPreloadInitialCharacterEquipmentKeepsJobZero(t *testing.T) {
	source := initialEquipmentMemSource{
		"character/character.lst":     "0 `slayer.chr`\n15 `fighter.chr`\n",
		"character/slayer.chr":        "[create equipment list]\n[weapon] 201\n[/create equipment list]\n",
		"character/fighter.chr":       "[create equipment list]\n[coat] 301\n[/create equipment list]\n",
		"equipment/equipment.lst":     "201 `weapon/slayer.equ`\n301 `armor/fighter.equ`\n",
		"equipment/weapon/slayer.equ": "[durability]\n31\n",
		"equipment/armor/fighter.equ": "[durability]\n41\n",
	}

	byJob, err := parseInitialCharacterEquipmentAllFromSource(source)
	if err != nil {
		t.Fatalf("preload initial equipment: %v", err)
	}
	if got := len(byJob[0]); got != 1 {
		t.Fatalf("job0 entries = %d, want 1", got)
	}
	if byJob[0][0].ItemID != 201 || byJob[0][0].Durability != 31 {
		t.Fatalf("job0 entry = %+v", byJob[0][0])
	}
	if got := len(byJob[15]); got != 1 {
		t.Fatalf("job15 entries = %d, want 1", got)
	}
}

func TestRealScriptPVFInitialEquipmentPreload(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to run real Script.pvf preload smoke")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real pvf: %v", err)
	}
	byJob, err := parseInitialCharacterEquipmentAllFromSource(archive)
	if err != nil {
		t.Fatalf("preload real initial equipment: %v", err)
	}
	if len(byJob) < 16 {
		t.Fatalf("real initial equipment jobs = %d, want at least 16", len(byJob))
	}
	if len(byJob[11]) == 0 {
		t.Fatalf("female swordman job 11 initial equipment missing")
	}
	total := 0
	overU16 := 0
	for _, entries := range byJob {
		for _, entry := range entries {
			total++
			if entry.ItemID > 0xffff {
				overU16++
			}
		}
	}
	if total != 48 {
		t.Fatalf("real initial equipment total = %d, want 48 (three starter slots for 16 jobs)", total)
	}
	if overU16 != 16 {
		t.Fatalf("real initial equipment ids over u16 = %d, want 16 weapon ids", overU16)
	}
	if byJob[11][0].ItemID == int64(uint16(byJob[11][0].ItemID)) {
		t.Fatalf("female swordman weapon/item id should prove op9 u16 key is not the raw item id: %+v", byJob[11][0])
	}
	if layers := byJob[11][0].ModelLayers; len(layers) == 0 || layers[0].Key != 2150 || layers[0].Name != "at_katanaa" {
		t.Fatalf("female swordman weapon model layers = %+v", layers)
	}
	if got := byJob[11]; len(got) != 3 || got[0].Slot != 11 || got[1].Slot != 13 || got[2].Slot != 15 {
		t.Fatalf("female swordman starter equipment = %+v, want weapon/coat/pants slots 11/13/15", got)
	}
}

func TestSaveNewCharacterSeedsInitialEquipmentRecord(t *testing.T) {
	repos := testRepositoryGroup()
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
					ModelLayers: []initialEquipmentModelLayer{
						{Key: 2150, Name: "at_katanaa", Script: "equipment/character/atswordman.lay"},
					},
				},
			},
		},
	}
	prepareTestCharacterInitialization(service, 15)
	record := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Slot:        0,
		Name:        "hero",
		Job:         "15",
		Level:       1,
		Stats:       defaultCreatedCharacterStats(0),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := service.saveNewCharacter(context.Background(), repos, record); err != nil {
		t.Fatalf("save new character: %v", err)
	}
	equipment, found, err := repos.Equipment.Load(context.Background(), "77")
	if err != nil || !found {
		t.Fatalf("equipment load found=%t err=%v", found, err)
	}
	entry, ok := equipment.Entries["11"]
	if !ok {
		t.Fatalf("equipment slot 11 missing: %+v", equipment.Entries)
	}
	if entry.ItemID != 900001 || entry.SlotIndex != 11 || binary.LittleEndian.Uint16(entry.RawEntry[10:12]) != 27 {
		t.Fatalf("equipment entry = %+v raw=%x", entry, entry.RawEntry)
	}
	if entry.Extra["source"] != "pvf_create_equipment_list" ||
		entry.Extra["max_durability"] != "27" ||
		entry.Extra["equipment_type"] != "1" ||
		entry.Extra["current_exe_create_value"] != "1" {
		t.Fatalf("equipment extra = %+v", entry.Extra)
	}
	if _, ok := entry.Extra["current_exe_equipment_type"]; ok {
		t.Fatalf("PVF equipment type must not be stored as current EXE runtime type: %+v", entry.Extra)
	}
	if _, ok := entry.Extra["instance_value"]; ok {
		t.Fatalf("new PVF seed must not retain the old C# instance field: %+v", entry.Extra)
	}
	if entry.Extra["model_layer_count"] != "1" ||
		entry.Extra["model_layer_0_key"] != "2150" ||
		entry.Extra["model_layer_0_name"] != "at_katanaa" ||
		entry.Extra["model_layer_0_script"] != "equipment/character/atswordman.lay" {
		t.Fatalf("equipment model layer extra = %+v", entry.Extra)
	}
}

func TestAttachEquipmentSummaryReplacesStaleRosterFromEquipmentRepository(t *testing.T) {
	repos := testRepositoryGroup()
	ctx := context.Background()
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"12": {SlotIndex: 12, ItemID: 900012},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}
	record := dnfrepo.CharacterRecord{
		CharacterID: "77",
		Job:         "15",
		Roster: dnfrepo.CharacterRoster{Entry: dnfrepo.CharacterRosterEntry{
			EquipSummary: []dnfrepo.CharacterRosterEquipSummary{{Slot: 11, ItemIDOrIcon: 800011}},
		}},
	}

	(&Service{}).attachEquipmentSummary(ctx, repos, &record)

	if got := record.Roster.Entry.EquipSummary; len(got) != 1 || got[0].Slot != 12 || got[0].ItemIDOrIcon != 900012 {
		t.Fatalf("attached equipment summary = %+v, want repository slot 12 item 900012", got)
	}
}

func TestEquipmentRosterSummaryUsesCurrentWeaponAppearancePrecedence(t *testing.T) {
	starterWeapon := dnfrepo.EquipmentEntry{
		SlotIndex: 11,
		ItemID:    1001,
		Extra:     map[string]string{"source": "pvf_create_equipment_list"},
	}
	weaponAvatar := dnfrepo.EquipmentEntry{
		SlotIndex: 10,
		ItemID:    2001,
		Extra: map[string]string{
			"current_exe_equipment_type": "10",
			"current_exe_runtime_move":   "1",
		},
	}

	tests := []struct {
		name    string
		entries map[string]dnfrepo.EquipmentEntry
		want    []dnfrepo.CharacterRosterEquipSummary
	}{
		{
			name:    "starter weapon is mapped to current weapon appearance slot",
			entries: map[string]dnfrepo.EquipmentEntry{"starter": starterWeapon},
			want:    []dnfrepo.CharacterRosterEquipSummary{{Slot: 12, ItemIDOrIcon: 1001}},
		},
		{
			name:    "weapon avatar hides normal weapon appearance",
			entries: map[string]dnfrepo.EquipmentEntry{"starter": starterWeapon, "avatar": weaponAvatar},
			want:    []dnfrepo.CharacterRosterEquipSummary{{Slot: 10, ItemIDOrIcon: 2001}},
		},
		{
			name:    "empty equipment keeps only the implicit job weapon",
			entries: map[string]dnfrepo.EquipmentEntry{},
			want:    nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := equipmentRosterSummary(dnfrepo.EquipmentRecord{CharacterID: "77", Entries: test.entries})
			if len(got) != len(test.want) {
				t.Fatalf("summary length=%d want=%d summary=%+v", len(got), len(test.want), got)
			}
			for index := range test.want {
				if got[index].Slot != test.want[index].Slot || got[index].ItemIDOrIcon != test.want[index].ItemIDOrIcon {
					t.Fatalf("summary[%d]=%+v want=%+v", index, got[index], test.want[index])
				}
			}
		})
	}
}

func TestAttachEquipmentSummaryTreatsExistingEmptyEquipmentAsAuthoritative(t *testing.T) {
	repos := testRepositoryGroup()
	ctx := context.Background()
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries:     map[string]dnfrepo.EquipmentEntry{},
	}); err != nil {
		t.Fatalf("save empty equipment: %v", err)
	}
	record := dnfrepo.CharacterRecord{
		CharacterID: "77",
		Job:         "15",
		Roster: dnfrepo.CharacterRoster{Entry: dnfrepo.CharacterRosterEntry{
			EquipSummary: []dnfrepo.CharacterRosterEquipSummary{{Slot: 11, ItemIDOrIcon: 800011}},
		}},
	}
	service := &Service{initialEquipmentByJob: map[byte][]initialEquipmentEntry{
		15: {{Slot: 11, ItemID: 900011, Durability: 27}},
	}}

	service.attachEquipmentSummary(ctx, repos, &record)

	if got := record.Roster.Entry.EquipSummary; len(got) != 0 {
		t.Fatalf("empty equipped set retained stale roster rows: %+v", got)
	}
	equipment, found, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load empty equipment found=%t err=%v", found, err)
	}
	if len(equipment.Entries) != 0 {
		t.Fatalf("empty equipped set was reseeded: %+v", equipment.Entries)
	}
}

func TestLegacyCSharpInitialEquipmentValueIsNormalizedOnlyForPVFSeeds(t *testing.T) {
	legacyRaw := buildInitialEquipmentRawEntry(11, 900001, 27)
	binary.LittleEndian.PutUint32(legacyRaw[5:9], legacyCSharpInitialEquipmentInstanceValue)
	seed := dnfrepo.EquipmentEntry{
		SlotIndex: 11,
		ItemID:    900001,
		RawEntry:  legacyRaw,
		Extra: map[string]string{
			"source":         "pvf_create_equipment_list",
			"instance_value": "999999998",
		},
	}
	if got := currentItemListEquipmentInstance(seed); got != initialEquipmentCreateValue {
		t.Fatalf("legacy PVF seed create value = %d, want %d", got, initialEquipmentCreateValue)
	}
	if got := currentItemListEquipmentValueA(seed); got != 0 {
		t.Fatalf("legacy PVF seed valueA = %d, want sparse current default 0", got)
	}
	normalizedRaw := currentEquipmentRawEntry(seed)
	if got := binary.LittleEndian.Uint32(normalizedRaw[5:9]); got != initialEquipmentCreateValue {
		t.Fatalf("normalized raw create value = %d, want %d", got, initialEquipmentCreateValue)
	}
	if got := binary.LittleEndian.Uint32(seed.RawEntry[5:9]); got != legacyCSharpInitialEquipmentInstanceValue {
		t.Fatalf("normalization mutated stored input raw = %d", got)
	}

	nonSeed := seed
	nonSeed.Extra = map[string]string{"instance_value": "999999998"}
	if got := currentItemListEquipmentInstance(nonSeed); got != legacyCSharpInitialEquipmentInstanceValue {
		t.Fatalf("non-seed instance = %d, want preserved %d", got, legacyCSharpInitialEquipmentInstanceValue)
	}
	if got := currentItemListEquipmentValueA(nonSeed); got != legacyCSharpInitialEquipmentInstanceValue {
		t.Fatalf("non-seed valueA = %d, want preserved %d", got, legacyCSharpInitialEquipmentInstanceValue)
	}
}

func TestRewriteCurrentSceneObjectTailEquipSummary(t *testing.T) {
	tail := append([]byte{1, 2, 3, 4, 5, 6, 1}, make([]byte, currentSceneObjectEquipSummaryRowBytes)...)
	tail = append(tail, 0xaa, 0xbb)
	rows := []dnfrepo.CharacterRosterEquipSummary{
		{Slot: 22, ItemIDOrIcon: 103},
		{Slot: 11, ItemIDOrIcon: 101},
	}

	rewritten, ok := rewriteCurrentSceneObjectTailEquipSummary(tail, rows)
	if !ok {
		t.Fatalf("rewrite returned false")
	}
	if rewritten[6] != 2 {
		t.Fatalf("summary count = %d, want 2", rewritten[6])
	}
	if rewritten[7] != 11 || binary.LittleEndian.Uint32(rewritten[8:12]) != 101 {
		t.Fatalf("first summary row = %x", rewritten[7:7+currentSceneObjectEquipSummaryRowBytes])
	}
	if rawLen := binary.LittleEndian.Uint32(rewritten[12:16]); rawLen != 0 {
		t.Fatalf("first summary raw len = %d, want 0", rawLen)
	}
	second := 7 + currentSceneObjectEquipSummaryRowBytes
	if rewritten[second] != 22 || binary.LittleEndian.Uint32(rewritten[second+1:second+5]) != 103 {
		t.Fatalf("second summary row = %x", rewritten[second:second+currentSceneObjectEquipSummaryRowBytes])
	}
	if got := rewritten[len(rewritten)-2:]; got[0] != 0xaa || got[1] != 0xbb {
		t.Fatalf("tail suffix = %x", got)
	}
}

func TestRewriteCurrentSceneObjectTailEquipSummaryKeepsType44RawBlockEmpty(t *testing.T) {
	raw := buildInitialEquipmentRawEntry(11, 900001, 27)
	tail := append([]byte{1, 2, 3, 4, 5, 6, 0}, 0xaa, 0xbb)
	rows := []dnfrepo.CharacterRosterEquipSummary{
		{Slot: 11, ItemIDOrIcon: 900001, RawEntry: raw},
	}

	rewritten, ok := rewriteCurrentSceneObjectTailEquipSummary(tail, rows)
	if !ok {
		t.Fatalf("rewrite returned false")
	}
	if rewritten[6] != 1 {
		t.Fatalf("summary count = %d, want 1", rewritten[6])
	}
	row := rewritten[7:]
	if row[0] != 11 || binary.LittleEndian.Uint32(row[1:5]) != 900001 {
		t.Fatalf("summary row head = %x", row[:5])
	}
	rawLen := int(binary.LittleEndian.Uint32(row[5:9]))
	if rawLen != 0 {
		t.Fatalf("summary raw len = %d, want 0", rawLen)
	}
	suffix := rewritten[7+currentSceneObjectEquipSummaryRowBytes:]
	if len(suffix) != 2 || suffix[0] != 0xaa || suffix[1] != 0xbb {
		t.Fatalf("tail suffix = %x", suffix)
	}
}

func TestCurrentSceneObjectListBodyUsesPVFBackedEquipSummaryRows(t *testing.T) {
	name := "hero"
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		Name:        name,
		Job:         "11",
		Level:       1,
		Roster: dnfrepo.CharacterRoster{
			Entry: dnfrepo.CharacterRosterEntry{
				EquipSummary: []dnfrepo.CharacterRosterEquipSummary{
					{Slot: 22, ItemIDOrIcon: 900022, PackedFlags: 3, OptionalIDOrExpire: 44, AuxValue: 55, AuxFlag: 6},
					{Slot: 11, ItemIDOrIcon: 900011, PackedFlags: 1, OptionalIDOrExpire: 22, AuxValue: 33, AuxFlag: 2},
				},
			},
		},
	}

	body := buildCurrentSceneObjectListBody(currentSceneBootstrapObjectKey, character, true, "")
	nameLen := len(rosterNameBytes(name))
	tailStart := 5 + 0x47 + 2 + 4 + nameLen
	if len(body) <= tailStart+7 {
		t.Fatalf("scene object body too short len=%d tailStart=%d body=%x", len(body), tailStart, body)
	}
	tail := body[tailStart:]
	if len(tail) <= 6 {
		t.Fatalf("mode0 tail too short len=%d tail=%x", len(tail), tail)
	}
	if tail[6] != 2 {
		t.Fatalf("mode0 equipment summary count = %d, want 2", tail[6])
	}
	first := tail[7 : 7+currentSceneObjectEquipSummaryRowBytes]
	if first[0] != 11 || binary.LittleEndian.Uint32(first[1:5]) != 900011 {
		t.Fatalf("first mode0 equipment summary row = %x", first)
	}
	if rawLen := binary.LittleEndian.Uint32(first[5:9]); rawLen != 0 {
		t.Fatalf("first mode0 equipment type44 raw len = %d, want 0", rawLen)
	}
	second := tail[7+currentSceneObjectEquipSummaryRowBytes : 7+2*currentSceneObjectEquipSummaryRowBytes]
	if second[0] != 22 || binary.LittleEndian.Uint32(second[1:5]) != 900022 {
		t.Fatalf("second mode0 equipment summary row = %x", second)
	}
}
