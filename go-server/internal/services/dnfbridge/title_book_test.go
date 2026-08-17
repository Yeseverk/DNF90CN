package dnfbridge

import (
	"encoding/binary"
	"testing"

	dnfquest "longheng.io/server/internal/modules/dnf/quest"
)

func TestParseTitleBookEtcUsesSlotMarkerFiveTuple(t *testing.T) {
	text := `
[title collection info]
	` + "`general`" + `	200
	0	0	6501	1	26596
	1	-1
	2	0	6503	1	26598
[/title collection info]
[title collection info]
	` + "`specific`" + `	160
	0	0	6532	1	26648
	1	0	6533	1	26649
[/title collection info]
`
	mapping := parseTitleBookEtc(text)
	if len(mapping) != 4 {
		t.Fatalf("mapping count = %d, want 4: %+v", len(mapping), mapping)
	}
	if got := mapping[6503]; got.category != 0 || got.bookIndex != 2 || got.titleItemID != 26598 {
		t.Fatalf("general mapping = %+v", got)
	}
	if got := mapping[6533]; got.category != 1 || got.bookIndex != 1 || got.titleItemID != 26649 || got.rewardCount != 1 {
		t.Fatalf("specific mapping = %+v", got)
	}
}

func TestBuildCurrentTitleBookListBodyIncludesRawLengthPerRow(t *testing.T) {
	body := buildCurrentTitleBookListBody(19, 1, []currentTitleBookEntry{{
		SlotIndex: 7,
		ItemID:    26649,
		Value:     1,
	}})
	if len(body) != 11+22+4 {
		t.Fatalf("body len = %d, want 37: %x", len(body), body)
	}
	if binary.LittleEndian.Uint32(body[7:11]) != 1 ||
		binary.LittleEndian.Uint16(body[11:13]) != 7 ||
		binary.LittleEndian.Uint32(body[13:17]) != 26649 ||
		binary.LittleEndian.Uint32(body[len(body)-4:]) != 0 {
		t.Fatalf("body mismatch: %x", body)
	}
}

func TestCurrentAchievementTargetsUsesPVFCheckCount(t *testing.T) {
	got := currentAchievementTargets(dnfquest.Definition{CheckCount: []int64{10000}})
	if got != [3]uint16{10000, 0, 0} {
		t.Fatalf("targets = %v", got)
	}
	got = currentAchievementTargets(dnfquest.Definition{
		Type:    "[seeking]",
		IntData: []int64{-7, 5},
	})
	if got != [3]uint16{5, 0, 0} {
		t.Fatalf("seeking targets = %v", got)
	}
}
