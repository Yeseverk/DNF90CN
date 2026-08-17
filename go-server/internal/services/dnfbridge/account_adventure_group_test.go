package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestCurrentAccountAdventureGroupSummaryUsesWholeRealRoster(t *testing.T) {
	tables := loadAdventureGroupTestTables(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, character := range []dnfrepo.CharacterRecord{
		{CharacterID: "19", AccountID: "dnf:1", Slot: 0, Level: 1},
		{CharacterID: "20", AccountID: "dnf:1", Slot: 1, Level: 2},
		{CharacterID: "99", AccountID: "dnf:2", Slot: 0, Level: 100},
	} {
		if err := repositories.Character.Save(context.Background(), character); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{
		options:             options{accountID: "dnf:1"},
		adventureGroupTable: tables,
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	selected, ok, err := repositories.Character.Load(context.Background(), "19")
	if err != nil || !ok {
		t.Fatalf("load selected character: ok=%v err=%v", ok, err)
	}

	summary, err := service.currentAccountAdventureGroupSummary(context.Background(), selected, true)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalPoint != 40 || summary.ManageLevel != 2 || summary.ManageOption != 9 {
		t.Fatalf("adventure-group summary = %+v", summary)
	}

	selected.AccountID = "dnf:2"
	if _, err := service.currentAccountAdventureGroupSummary(context.Background(), selected, true); !errors.Is(err, errAdventureGroupOwnerMismatch) {
		t.Fatalf("owner mismatch error = %v", err)
	}
}

func TestCurrentAccountAdventureGroupSummaryForPacketUsesSessionAccount(t *testing.T) {
	tables := loadAdventureGroupTestTables(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, character := range []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: "fallback-account", Slot: 0, Level: 90},
		{CharacterID: "2", AccountID: "session-account", Slot: 0, Level: 10},
	} {
		if err := repositories.Character.Save(context.Background(), character); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{
		options:             options{accountID: "fallback-account"},
		adventureGroupTable: tables,
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	session := &gameSession{accountID: "session-account"}
	selected, found, err := repositories.Character.Load(context.Background(), "2")
	if err != nil || !found {
		t.Fatalf("load selected found=%t err=%v", found, err)
	}

	got := service.currentAccountAdventureGroupSummaryForPacket(
		context.Background(),
		session,
		selected,
		true,
	)
	want, err := tables.Calculate([]adventuregroup.Character{{Level: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("summary=%+v, want %+v", got, want)
	}
}

func TestCurrentUserInfoModesCarrySameAdventureLevelAndKeepHonorExpertSeparate(t *testing.T) {
	service := &Service{}
	character := dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "dnf:1", Job: "11", Level: 1}
	const adventureLevel uint32 = 0x01020304

	mode1 := service.buildCurrentSelectedUserInfoMode1BodyWithAdventureLevel(context.Background(), nil, nil, character, true, 19, adventureLevel)
	if len(mode1) < 23 {
		t.Fatalf("mode1 body too short: %d", len(mode1))
	}
	if got := binary.LittleEndian.Uint32(mode1[len(mode1)-10 : len(mode1)-6]); got != adventureLevel {
		t.Fatalf("mode1 adventure level = 0x%08x", got)
	}
	if got := binary.LittleEndian.Uint32(mode1[7:11]); got != 0 {
		t.Fatalf("mode1 HonorExpert level = %d, want 0", got)
	}
	for index, value := range mode1[11:19] {
		if value != 0 {
			t.Fatalf("mode1 HonorExpert progress byte %d = 0x%02x", index, value)
		}
	}

	mode3 := service.buildCurrentSelectedUserInfoMode3BodyWithAdventureLevel(context.Background(), nil, nil, character, true, 19, adventureLevel)
	if len(mode3) < 13 {
		t.Fatalf("mode3 body too short: %d", len(mode3))
	}
	if got := binary.LittleEndian.Uint32(mode3[5:9]); got != adventureLevel {
		t.Fatalf("mode3 adventure level = 0x%08x", got)
	}
	for index, value := range mode3[9:13] {
		if value != 0 {
			t.Fatalf("mode3 auxiliary byte %d = 0x%02x", index, value)
		}
	}
}

type adventureGroupServiceTestSource map[string]string

func (source adventureGroupServiceTestSource) ReadText(path string) (string, error) {
	value, ok := source[path]
	if !ok {
		return "", fmt.Errorf("%w: %s", platformpvf.ErrFileNotFound, path)
	}
	return value, nil
}

func loadAdventureGroupTestTables(t *testing.T) *adventuregroup.Tables {
	t.Helper()
	text := `[point bonus]
1 1 10 2 2 20
[/point bonus]
[manage level point]
10 40 100
[/manage level point]
[manage level max]
3
[exp bonus]
[/exp bonus]
[gold bonus]
[/gold bonus]
[manage option]
1 5 2 9 3 15
[/manage option]
`
	tables, err := adventuregroup.Load(context.Background(), adventureGroupServiceTestSource{
		adventuregroup.CharacterManagementPath: text,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tables
}
