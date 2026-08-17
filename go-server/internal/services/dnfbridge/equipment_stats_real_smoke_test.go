package dnfbridge

import (
	"context"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	dnfcharstat "longheng.io/server/internal/modules/dnf/charstat"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/skillcmd"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFAllJobsStarterSkillLayouts(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to run real Script.pvf skill smoke")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := buildSkillCatalogFromSource(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{initialEquipmentArchive: archive, skillCatalog: catalog}
	for job := 0; job < 16; job++ {
		entries, err := service.initialCharacterSkills(context.Background(), byte(job))
		if err != nil {
			t.Fatalf("job %d initial skills: %v", job, err)
		}
		if len(entries) == 0 {
			t.Fatalf("job %d has no PVF initial skills", job)
		}
		states := make(map[int64]dnfrepo.SkillState, len(entries))
		for _, entry := range entries {
			states[entry.SkillID] = dnfrepo.SkillState{Level: entry.Level, Enabled: true}
			definition, _ := catalog.Find(byte(job), uint16(entry.SkillID))
			t.Logf("job=%d skill=%d level=%d name=%q kind=%q active=%t class=%d grow=%v command=%q", job, entry.SkillID, entry.Level, definition.Name, definition.Kind, definition.Active, definition.SkillClass, definition.GrowTypes, definition.Command)
		}
		layout, err := skillcmd.BuildInitialSkillLayout(catalog, byte(job), currentSkillInfoTreeIndex, states)
		if err != nil {
			t.Fatalf("job %d layout: %v", job, err)
		}
		activeCount := 0
		primaryCount := 0
		for slot, skillID := range layout {
			definition, ok := catalog.Find(byte(job), skillID)
			if !ok {
				t.Fatalf("job %d layout skill %d missing", job, skillID)
			}
			if definition.Active {
				activeCount++
			}
			if slot >= 0 && slot < 6 {
				primaryCount++
				if !definition.Active {
					t.Fatalf("job %d passive skill %d entered primary slot %d", job, skillID, slot)
				}
			}
		}
		wantPrimary := activeCount
		if wantPrimary > 3 {
			wantPrimary = 3
		}
		if activeCount == 0 || primaryCount != wantPrimary {
			t.Fatalf("job %d starter active=%d primary=%d entries=%v layout=%v", job, activeCount, primaryCount, entries, layout)
		}
		for slot := 3; slot < 6; slot++ {
			if skillID, ok := layout[slot]; ok {
				t.Fatalf("job %d unexpected starter quickbar slot=%d skill=%d", job, slot, skillID)
			}
		}
		t.Logf("job=%d entries=%v layout=%v", job, entries, layout)
	}
}

func TestRealScriptPVFAllJobsStarterSkillsPersistAndRoundTripThroughOp19(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to run real Script.pvf skill round-trip smoke")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := buildSkillCatalogFromSource(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	service := &Service{
		repositoryProvider:      func() (dnfrepo.Group, bool) { return repositories, true },
		initialEquipmentArchive: archive,
		skillCatalog:            catalog,
	}

	for job := 0; job < 16; job++ {
		t.Run("job_"+strconv.Itoa(job), func(t *testing.T) {
			characterID := "real-pvf-job-" + strconv.Itoa(job)
			character := dnfrepo.CharacterRecord{
				CharacterID: characterID,
				Job:         strconv.Itoa(job),
				Level:       1,
			}
			connection := &bufferConn{}
			if err := service.sendCurrentSceneSkillInfo(
				&gameSession{conn: connection},
				context.Background(),
				character,
				"test_real_pvf_all_job_op19_round_trip",
			); err != nil {
				t.Fatal(err)
			}

			packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
			if len(rest) != 0 || packet.Header.Classification != 0 || packet.Header.MsgID != currentSkillInfoMsgID {
				t.Fatalf("packet=%+v rest=%x", packet.Header, rest)
			}
			saved, found, err := repositories.Skill.Load(context.Background(), characterID)
			if err != nil || !found {
				t.Fatalf("saved found=%t err=%v", found, err)
			}
			initial, err := service.initialCharacterSkills(context.Background(), byte(job))
			if err != nil {
				t.Fatal(err)
			}
			if !skillRecordMatchesPVFInitialSkills(saved.Skills, initial) {
				t.Fatalf("job %d persisted skills=%v, want PVF initial=%v", job, saved.Skills, initial)
			}
			layout := saved.Layouts[currentSkillInfoTreeIndex]
			if len(layout) == 0 {
				t.Fatalf("job %d persisted an empty layout", job)
			}
			wantWire := make(map[int]uint16, len(layout))
			primaryCount := 0
			for slot, skillID := range layout {
				wantWire[slot] = skillID
				definition, ok := catalog.Find(byte(job), skillID)
				if !ok {
					t.Fatalf("job %d persisted unknown skill=%d slot=%d", job, skillID, slot)
				}
				if slot >= 0 && slot < 6 {
					primaryCount++
					if slot > 2 || !definition.Active {
						t.Fatalf("job %d invalid primary slot=%d skill=%d active=%t", job, slot, skillID, definition.Active)
					}
				}
			}
			if primaryCount == 0 || primaryCount > 3 {
				t.Fatalf("job %d primary starter count=%d layout=%v", job, primaryCount, layout)
			}
			if got := currentSkillInfoFirstTreeSlots(t, packet.Body); !reflect.DeepEqual(got, wantWire) {
				t.Fatalf("job %d op19 slots=%v, want persisted=%v", job, got, wantWire)
			}
		})
	}
}

func TestRealScriptPVFJob15StarterStatVectors(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to run real Script.pvf stat smoke")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	table, err := dnfcharstat.Load(context.Background(), archive, dnfcharstat.Options{})
	if err != nil {
		t.Fatal(err)
	}
	base, err := table.Compute(15, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	skillCatalog, err := buildSkillCatalogFromSource(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, skillID := range []uint16{1, 50, 169, 174, 179, 511} {
		definition, ok := skillCatalog.Find(15, skillID)
		if !ok {
			t.Fatalf("job15 skill missing id=%d", skillID)
		}
		t.Logf("job15 skill id=%d name=%q kind=%q active=%t class=%d required=%d grow=%v feature=%d command=%q", skillID, definition.Name, definition.Kind, definition.Active, definition.SkillClass, definition.RequiredLevel, definition.GrowTypes, definition.FeatureSkillType, definition.Command)
	}
	entries, err := parseInitialCharacterEquipmentFromSource(archive, 15)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		initialEquipmentArchive: archive,
		equipmentStats:          make(map[int64]dnfcharstat.Vector),
	}
	combined := base
	for _, entry := range entries {
		text, actualPath, err := readInitialPVFText(archive, initialPVFPath("equipment", entry.PVFPath), entry.PVFPath)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := dnfpvf.Parse(actualPath, text)
		if err != nil {
			t.Fatal(err)
		}
		for _, section := range doc.Sections {
			name := strings.ToLower(section.Name)
			if strings.Contains(name, "defense") || strings.Contains(name, "attack") || strings.Contains(name, "stat") {
				t.Logf("item=%d section=%q numbers=%v texts=%v", entry.ItemID, section.Name, doc.Numbers(section.Name), doc.Texts(section.Name))
			}
		}
		stats, ok := service.equipmentPVFStat(context.Background(), entry.ItemID, map[string]string{"pvf_path": entry.PVFPath})
		if !ok {
			t.Fatalf("equipment stat missing item=%d path=%s", entry.ItemID, entry.PVFPath)
		}
		t.Logf("item=%d slot=%d stats=%+v", entry.ItemID, entry.Slot, stats)
		combined.Add(stats)
	}
	t.Logf("job15 base=%+v", base)
	t.Logf("job15 combined=%+v", combined)
}
