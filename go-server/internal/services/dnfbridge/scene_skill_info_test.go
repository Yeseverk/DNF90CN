package dnfbridge

import (
	"context"
	"encoding/binary"
	"reflect"
	"sort"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestSendCurrentSceneSkillInfoBackfillsEachJobsOwnPVFInitialSkills(t *testing.T) {
	catalog, err := buildSkillCatalogFromSource(context.Background(), initialEquipmentMemSource{
		"skill/skilllist.lst":     "0 `job0.lst`\n1 `job1.lst`\n",
		"skill/job0.lst":          "46 `job0/active_a.skl`\n47 `job0/active_b.skl`\n48 `job0/active_c.skl`\n49 `job0/passive.skl`\n",
		"skill/job1.lst":          "10 `job1/active_a.skl`\n20 `job1/active_b.skl`\n30 `job1/active_c.skl`\n40 `job1/passive.skl`\n",
		"skill/job0/active_a.skl": "[skill type]\n`active`\n",
		"skill/job0/active_b.skl": "[skill type]\n`active`\n",
		"skill/job0/active_c.skl": "[skill type]\n`active`\n",
		"skill/job0/passive.skl":  "[skill type]\n`passive`\n",
		"skill/job1/active_a.skl": "[skill type]\n`active`\n",
		"skill/job1/active_b.skl": "[skill type]\n`active`\n",
		"skill/job1/active_c.skl": "[skill type]\n`active`\n",
		"skill/job1/passive.skl":  "[skill type]\n`passive`\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		job       byte
		entries   []initialSkillEntry
		wantSlots map[int]uint16
		rowExists bool
	}{
		{
			name: "swordman missing row",
			job:  0,
			entries: []initialSkillEntry{
				{SkillID: 46, Level: 1}, {SkillID: 47, Level: 1}, {SkillID: 48, Level: 1}, {SkillID: 49, Level: 1},
			},
			wantSlots: map[int]uint16{0: 46, 1: 47, 2: 48, 54: 49},
		},
		{
			name: "fighter empty row",
			job:  1,
			entries: []initialSkillEntry{
				{SkillID: 10, Level: 1}, {SkillID: 20, Level: 1}, {SkillID: 30, Level: 1}, {SkillID: 40, Level: 1},
			},
			wantSlots: map[int]uint16{0: 10, 1: 20, 2: 30, 54: 40},
			rowExists: true,
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositories := dnfrepomemory.NewMemoryGroup()
			characterID := string(rune('1' + index))
			character := dnfrepo.CharacterRecord{CharacterID: characterID, Job: string(rune('0' + test.job)), Level: 1}
			if test.rowExists {
				if err := repositories.Skill.Save(context.Background(), dnfrepo.SkillRecord{CharacterID: characterID}); err != nil {
					t.Fatal(err)
				}
			}
			service := &Service{
				repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
				initialSkillsByJob: map[byte][]initialSkillEntry{test.job: test.entries},
				initialSPTable:     map[int]int{1: 20},
				skillCatalog:       catalog,
			}
			connection := &bufferConn{}
			session := &gameSession{conn: connection}
			if err := service.sendCurrentSceneSkillInfo(session, context.Background(), character, "test_all_job_backfill"); err != nil {
				t.Fatal(err)
			}
			packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
			if len(rest) != 0 || packet.Header.Classification != 0 || packet.Header.MsgID != currentSkillInfoMsgID {
				t.Fatalf("packet=%+v rest=%x", packet.Header, rest)
			}
			if got := currentSkillInfoFirstTreeSlots(t, packet.Body); !reflect.DeepEqual(got, test.wantSlots) {
				t.Fatalf("job %d slots=%v, want %v", test.job, got, test.wantSlots)
			}

			saved, found, err := repositories.Skill.Load(context.Background(), characterID)
			if err != nil || !found {
				t.Fatalf("saved found=%t err=%v", found, err)
			}
			if len(saved.Skills) != len(test.entries) || saved.Points.TotalSP != 20 || saved.Points.RemainingSP != 20 || saved.Points.SyncedLevel != 1 {
				t.Fatalf("saved=%+v", saved)
			}
			if !reflect.DeepEqual(saved.Layouts[currentSkillInfoTreeIndex], dnfrepo.SkillLayout(test.wantSlots)) {
				t.Fatalf("saved layout=%v, want %v", saved.Layouts[currentSkillInfoTreeIndex], test.wantSlots)
			}
		})
	}
}

func TestSendCurrentSceneSkillInfoDoesNotReplaceExistingLearnedSkills(t *testing.T) {
	catalog, err := buildSkillCatalogFromSource(context.Background(), initialEquipmentMemSource{
		"skill/skilllist.lst":    "0 `job0.lst`\n",
		"skill/job0.lst":         "46 `job0/initial.skl`\n99 `job0/learned.skl`\n",
		"skill/job0/initial.skl": "[skill type]\n`active`\n",
		"skill/job0/learned.skl": "[skill type]\n`active`\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	existing := dnfrepo.SkillRecord{
		CharacterID: "77",
		Skills:      map[int64]dnfrepo.SkillState{99: {Level: 3, Enabled: true}},
		Layouts:     map[int]dnfrepo.SkillLayout{0: {4: 99}},
	}
	if err := repositories.Skill.Save(context.Background(), existing); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		initialSkillsByJob: map[byte][]initialSkillEntry{0: {{SkillID: 46, Level: 1}}},
		initialSPTable:     map[int]int{1: 20},
		skillCatalog:       catalog,
	}
	connection := &bufferConn{}
	if err := service.sendCurrentSceneSkillInfo(&gameSession{conn: connection}, context.Background(), dnfrepo.CharacterRecord{CharacterID: "77", Job: "0", Level: 1}, "test_preserve_existing"); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("rest=%x", rest)
	}
	if got := currentSkillInfoFirstTreeSlots(t, packet.Body); !reflect.DeepEqual(got, map[int]uint16{4: 99}) {
		t.Fatalf("wire slots=%v", got)
	}
	saved, found, err := repositories.Skill.Load(context.Background(), "77")
	if err != nil || !found || !reflect.DeepEqual(saved.Skills, existing.Skills) || !reflect.DeepEqual(saved.Layouts, existing.Layouts) {
		t.Fatalf("saved found=%t err=%v record=%+v", found, err, saved)
	}
}

func TestSendCurrentSceneSkillInfoPersistsInitialLayoutWithAtMostThreeActiveQuickSlots(t *testing.T) {
	catalog, err := buildSkillCatalogFromSource(context.Background(), initialEquipmentMemSource{
		"skill/skilllist.lst":    "9 `job9.lst`\n",
		"skill/job9.lst":         "5 `job9/a.skl`\n8 `job9/b.skl`\n46 `job9/c.skl`\n108 `job9/d.skl`\n174 `job9/passive.skl`\n",
		"skill/job9/a.skl":       "[skill type]\n`active`\n",
		"skill/job9/b.skl":       "[skill type]\n`active`\n",
		"skill/job9/c.skl":       "[skill type]\n`active`\n",
		"skill/job9/d.skl":       "[skill type]\n`active`\n",
		"skill/job9/passive.skl": "[skill type]\n`passive`\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := []initialSkillEntry{
		{SkillID: 5, Level: 1}, {SkillID: 8, Level: 1}, {SkillID: 46, Level: 1}, {SkillID: 108, Level: 1}, {SkillID: 174, Level: 1},
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	record := dnfrepo.SkillRecord{CharacterID: "99", Skills: map[int64]dnfrepo.SkillState{}}
	for _, entry := range entries {
		record.Skills[entry.SkillID] = dnfrepo.SkillState{Level: entry.Level, Enabled: true}
	}
	if err := repositories.Skill.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		initialSkillsByJob: map[byte][]initialSkillEntry{9: entries},
		initialSPTable:     map[int]int{1: 20},
		skillCatalog:       catalog,
	}
	connection := &bufferConn{}
	if err := service.sendCurrentSceneSkillInfo(&gameSession{conn: connection}, context.Background(), dnfrepo.CharacterRecord{CharacterID: "99", Job: "9", Level: 1}, "test_initial_layout"); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("rest=%x", rest)
	}
	want := map[int]uint16{0: 5, 1: 8, 2: 46, 54: 174, 150: 108}
	if got := currentSkillInfoFirstTreeSlots(t, packet.Body); !reflect.DeepEqual(got, want) {
		t.Fatalf("wire slots=%v, want %v", got, want)
	}
	saved, found, err := repositories.Skill.Load(context.Background(), "99")
	if err != nil || !found || !reflect.DeepEqual(saved.Layouts[0], dnfrepo.SkillLayout(want)) {
		t.Fatalf("saved found=%t err=%v layout=%v", found, err, saved.Layouts[0])
	}
}

func TestBuildCurrentSceneSkillInfoBodyDuplicatesDirectLevelsAcrossBothTrees(t *testing.T) {
	record := dnfrepo.SkillRecord{
		Skills: map[int64]dnfrepo.SkillState{
			1:   {Level: 1, Enabled: true},
			50:  {Level: 1, Enabled: true},
			169: {Level: 1, Enabled: true},
			174: {Level: 1, Enabled: true},
			179: {Level: 7, Enabled: true},
			511: {Level: 1, Enabled: true},
		},
		Points: dnfrepo.SkillPointState{RemainingSP: 12, RemainingTP: 3},
	}
	layout := dnfrepo.SkillLayout{0: 1, 1: 50, 2: 169, 102: 174, 103: 179, 104: 511}
	body, activeSlots, err := buildCurrentSceneSkillInfoBody(record, layout)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 4 || int(binary.LittleEndian.Uint32(body[:4])) != len(body)-4 {
		t.Fatalf("skill-info length prefix=%x body_len=%d", body[:4], len(body))
	}
	if !reflect.DeepEqual(activeSlots, []int{0, 1, 2}) {
		t.Fatalf("active quick slots=%v, want [0 1 2]", activeSlots)
	}

	outerVarints, outerMessages := consumeCurrentSkillInfoFields(t, body[4:])
	if got := outerVarints[1]; !reflect.DeepEqual(got, []uint64{currentSkillInfoMessageType}) {
		t.Fatalf("outer field1=%v", got)
	}
	if len(outerMessages[2]) != currentSkillInfoTreeCount {
		t.Fatalf("skill tree count=%d, want %d", len(outerMessages[2]), currentSkillInfoTreeCount)
	}
	want := map[int][2]uint64{
		0: {1, 1}, 1: {50, 1}, 2: {169, 1},
		102: {174, 1}, 103: {179, 7}, 104: {511, 1},
	}
	for treeIndex, rawTree := range outerMessages[2] {
		treeVarints, treeMessages := consumeCurrentSkillInfoFields(t, rawTree)
		if !reflect.DeepEqual(treeVarints[1], []uint64{12}) || !reflect.DeepEqual(treeVarints[2], []uint64{3}) {
			t.Fatalf("tree %d points sp=%v tp=%v", treeIndex, treeVarints[1], treeVarints[2])
		}
		if len(treeMessages[3]) != 6 {
			t.Fatalf("tree %d learned skill count=%d, want 6", treeIndex, len(treeMessages[3]))
		}
		got := make(map[int][2]uint64, 6)
		for _, raw := range treeMessages[3] {
			fields, nested := consumeCurrentSkillInfoFields(t, raw)
			if len(nested) != 0 || len(fields[1]) != 1 || len(fields[2]) != 1 || len(fields[3]) != 1 || len(fields[4]) != 0 {
				t.Fatalf("tree %d invalid skill fields=%v nested=%v", treeIndex, fields, nested)
			}
			got[int(fields[1][0])] = [2]uint64{fields[2][0], fields[3][0]}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("tree %d skill slot map=%v, want %v", treeIndex, got, want)
		}
	}
	primary := make([]int, 0, 3)
	for slot := range want {
		if slot < 6 {
			primary = append(primary, slot)
		}
	}
	sort.Ints(primary)
	if !reflect.DeepEqual(primary, []int{0, 1, 2}) {
		t.Fatalf("primary quick slots=%v", primary)
	}
}

func TestSyncCurrentSkillPointStateRepairsHistoricalLedgerAndPreservesSpentPoints(t *testing.T) {
	tests := []struct {
		name    string
		current dnfrepo.SkillPointState
		target  dnfrepo.SkillPointState
		want    dnfrepo.SkillPointState
	}{
		{
			name:    "forced level90 historical zero ledger",
			current: dnfrepo.SkillPointState{SyncedLevel: 1},
			target:  dnfrepo.SkillPointState{TotalSP: 11970, RemainingSP: 11970, TotalTP: 41, RemainingTP: 41, SyncedLevel: 90},
			want:    dnfrepo.SkillPointState{TotalSP: 11970, RemainingSP: 11970, TotalTP: 41, RemainingTP: 41, SyncedLevel: 90},
		},
		{
			name:    "spent points remain spent",
			current: dnfrepo.SkillPointState{TotalSP: 100, RemainingSP: 70, TotalTP: 10, RemainingTP: 8, SyncedLevel: 50},
			target:  dnfrepo.SkillPointState{TotalSP: 200, RemainingSP: 200, TotalTP: 20, RemainingTP: 20, SyncedLevel: 60},
			want:    dnfrepo.SkillPointState{TotalSP: 200, RemainingSP: 170, TotalTP: 20, RemainingTP: 18, SyncedLevel: 60},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, changed, err := syncCurrentSkillPointState(test.current, test.target)
			if err != nil {
				t.Fatal(err)
			}
			if !changed || got != test.want {
				t.Fatalf("sync got=%+v changed=%t, want=%+v", got, changed, test.want)
			}
		})
	}
}

func consumeCurrentSkillInfoFields(t *testing.T, raw []byte) (map[protowire.Number][]uint64, map[protowire.Number][][]byte) {
	t.Helper()
	varints := make(map[protowire.Number][]uint64)
	messages := make(map[protowire.Number][][]byte)
	for len(raw) > 0 {
		number, wireType, n := protowire.ConsumeTag(raw)
		if n < 0 {
			t.Fatalf("consume tag: %v", protowire.ParseError(n))
		}
		raw = raw[n:]
		switch wireType {
		case protowire.VarintType:
			value, consumed := protowire.ConsumeVarint(raw)
			if consumed < 0 {
				t.Fatalf("consume field %d varint: %v", number, protowire.ParseError(consumed))
			}
			varints[number] = append(varints[number], value)
			raw = raw[consumed:]
		case protowire.BytesType:
			value, consumed := protowire.ConsumeBytes(raw)
			if consumed < 0 {
				t.Fatalf("consume field %d bytes: %v", number, protowire.ParseError(consumed))
			}
			messages[number] = append(messages[number], append([]byte(nil), value...))
			raw = raw[consumed:]
		default:
			t.Fatalf("unexpected field %d wire type %d", number, wireType)
		}
	}
	return varints, messages
}

func currentSkillInfoFirstTreeSlots(t *testing.T, body []byte) map[int]uint16 {
	t.Helper()
	if len(body) < 4 || int(binary.LittleEndian.Uint32(body[:4])) != len(body)-4 {
		t.Fatalf("invalid skill-info body=%x", body)
	}
	_, outerMessages := consumeCurrentSkillInfoFields(t, body[4:])
	if len(outerMessages[2]) != currentSkillInfoTreeCount {
		t.Fatalf("skill tree count=%d", len(outerMessages[2]))
	}
	_, treeMessages := consumeCurrentSkillInfoFields(t, outerMessages[2][0])
	out := make(map[int]uint16, len(treeMessages[3]))
	for _, raw := range treeMessages[3] {
		fields, _ := consumeCurrentSkillInfoFields(t, raw)
		if len(fields[1]) != 1 || len(fields[2]) != 1 {
			t.Fatalf("invalid skill row fields=%v", fields)
		}
		out[int(fields[1][0])] = uint16(fields[2][0])
	}
	return out
}
