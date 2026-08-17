package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestBuildCurrentSceneObjectListBodyForSessionProjectsAdventureNameToNativeOrganizationLine(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID:            "dnf:1",
		State:                "active",
		RepresentAccountName: "冒险团名称",
	}); err != nil {
		t.Fatal(err)
	}
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "15",
		Level:       90,
	}
	if err := repos.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}

	service := &Service{
		options:            options{accountID: "dnf:1"},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
	}
	session := &gameSession{accountID: "dnf:1", selectedCharacterID: 19}
	body := service.buildCurrentSceneObjectListBodyForSession(ctx, session, 19, character.Name, character, true)
	if level, got := currentSceneObjectOrganizationNameForTest(t, body); level != 1 || !bytes.Equal(got, rosterNameBytes("冒险团名称")) {
		t.Fatalf("organization level=%d name=%x want level=1 name=%x", level, got, rosterNameBytes("冒险团名称"))
	}

	// A peer projection cannot inherit the current viewer's account label.
	peer := dnfrepo.CharacterRecord{CharacterID: "20", AccountID: "dnf:2", Name: "Peer", Job: "0", Level: 90}
	peerBody := service.buildCurrentSceneObjectListBodyForSession(ctx, session, 20, peer.Name, peer, true)
	if level, got := currentSceneObjectOrganizationNameForTest(t, peerBody); level != 0 || len(got) != 0 {
		t.Fatalf("peer organization level=%d name=%x, want empty", level, got)
	}
}

func TestBuildCurrentSceneObjectListBodyForSessionUsesTypedFullAppearanceSnapshot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Name:        "Actor19",
		Job:         "15",
		Level:       90,
	}
	if err := repos.Character.Save(ctx, character); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"coat": {SlotIndex: 3, ItemID: 414500098},
		},
	}); err != nil {
		t.Fatalf("save equipment: %v", err)
	}

	service := &Service{repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true }}
	session := &gameSession{selectedCharacterID: 19}
	body := service.buildCurrentSceneObjectListBodyForSession(ctx, session, 19, character.Name, character, true)
	assertCurrentTypedMode0FullAppearanceForTest(t, body, character.Name, 3, 414500098)

	// Reuse the stale character value deliberately. The packet builder must
	// reload the authoritative equipment record and send explicit empty rows.
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "19"}); err != nil {
		t.Fatalf("save empty equipment: %v", err)
	}
	cleared := service.buildCurrentSceneObjectListBodyForSession(ctx, session, 19, character.Name, character, true)
	assertCurrentTypedMode0FullAppearanceForTest(t, cleared, character.Name, 3, currentActorMode0AppearanceEmptyItem)
}

func TestLoadCurrentSelectedActorMode0AppearanceSummaryRequiresSelectedEquipmentRecord(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	service := &Service{repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true }}
	session := &gameSession{selectedCharacterID: 19}

	if rows, source, found, err := service.loadCurrentSelectedActorMode0AppearanceSummary(ctx, session, 19); err != nil || found || len(rows) != 0 || source != "" {
		t.Fatalf("missing record rows=%v source=%q found=%v err=%v", rows, source, found, err)
	}
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "19"}); err != nil {
		t.Fatalf("save empty equipment: %v", err)
	}
	if rows, source, found, err := service.loadCurrentSelectedActorMode0AppearanceSummary(ctx, session, 20); err != nil || found || len(rows) != 0 || source != "" {
		t.Fatalf("unselected actor rows=%v source=%q found=%v err=%v", rows, source, found, err)
	}
	rows, source, found, err := service.loadCurrentSelectedActorMode0AppearanceSummary(ctx, session, 19)
	if err != nil || !found || len(rows) != currentActorMode0AppearanceSlotCount || source != currentActorMode0AppearanceBodySource {
		t.Fatalf("selected actor rows=%d source=%q found=%v err=%v", len(rows), source, found, err)
	}
	for slot, row := range rows {
		if row.Slot != int64(slot) || uint32(row.ItemIDOrIcon) != currentActorMode0AppearanceEmptyItem {
			t.Fatalf("empty row %d = %+v", slot, row)
		}
	}
}

func assertCurrentTypedMode0FullAppearanceForTest(t *testing.T, body []byte, name string, wantSlot int, wantItemID uint32) {
	assertCurrentTypedMode0FullAppearanceInContextForTest(
		t,
		body,
		name,
		wantSlot,
		wantItemID,
		currentSceneObjectContext,
	)
}

func currentSceneObjectOrganizationNameForTest(t *testing.T, body []byte) (byte, []byte) {
	t.Helper()
	_, tailStart, ok := currentSceneObjectLevelForLog(body)
	if !ok || tailStart >= len(body) {
		t.Fatalf("mode0 tail start=%d ok=%t body_len=%d", tailStart, ok, len(body))
	}
	tail := body[tailStart:]
	equipEnd, ok := currentSceneObjectEquipSummaryEnd(tail, 6)
	if !ok {
		t.Fatalf("mode0 organization name equipment parse failed tail=%x", tail)
	}
	pos := equipEnd + 4 + 4 + 8 + 1 + 4 + 1 + 4
	if pos+4 > len(tail) {
		t.Fatalf("mode0 creature name length truncated pos=%d tail_len=%d", pos, len(tail))
	}
	creatureNameLength := int(binary.LittleEndian.Uint32(tail[pos : pos+4]))
	pos += 4 + creatureNameLength + 1 + 1 + 1 + 4
	if creatureNameLength < 0 || pos+4 > len(tail) {
		t.Fatalf("mode0 organization name offset invalid creature_len=%d pos=%d tail_len=%d", creatureNameLength, pos, len(tail))
	}
	organizationLevel := tail[pos]
	pos++
	if pos+4 > len(tail) {
		t.Fatalf("mode0 organization name length truncated pos=%d tail_len=%d", pos, len(tail))
	}
	nameLength := int(binary.LittleEndian.Uint32(tail[pos : pos+4]))
	pos += 4
	if nameLength < 0 || pos+nameLength > len(tail) {
		t.Fatalf("mode0 organization name truncated len=%d pos=%d tail_len=%d", nameLength, pos, len(tail))
	}
	return organizationLevel, append([]byte(nil), tail[pos:pos+nameLength]...)
}

func assertCurrentTypedMode0FullAppearanceInContextForTest(
	t *testing.T,
	body []byte,
	name string,
	wantSlot int,
	wantItemID uint32,
	ownerChannel byte,
) {
	t.Helper()
	if len(body) < 0x4e || body[0] != 0 || binary.LittleEndian.Uint16(body[1:3]) != 1 ||
		body[3] != currentSceneObjectRoute || body[4] != ownerChannel ||
		binary.LittleEndian.Uint16(body[0x4c:0x4e]) != 19 {
		t.Fatalf("typed mode0 head=%x", body)
	}
	tailStart := currentSceneObjectTailStartForTest(name)
	if len(body) <= tailStart+6 {
		t.Fatalf("typed mode0 tail truncated len=%d tail=%d", len(body), tailStart)
	}
	appearance := body[tailStart+6:]
	if appearance[0] != currentActorMode0AppearanceSlotCount {
		t.Fatalf("appearance count=%d want=%d body=%x", appearance[0], currentActorMode0AppearanceSlotCount, body)
	}
	if got := currentActorMode0AppearanceGoldenItemID(t, appearance, wantSlot); got != wantItemID {
		t.Fatalf("appearance slot %d item=%#x want=%#x", wantSlot, got, wantItemID)
	}
	for slot := 0; slot < currentActorMode0AppearanceSlotCount; slot++ {
		if slot == wantSlot {
			continue
		}
		if got := currentActorMode0AppearanceGoldenItemID(t, appearance, slot); got != currentActorMode0AppearanceEmptyItem {
			t.Fatalf("appearance empty slot %d item=%#x want=%#x", slot, got, currentActorMode0AppearanceEmptyItem)
		}
	}
	if rows, _, ok := currentSceneObjectTailSummaryForLog(body); !ok || rows != currentActorMode0AppearanceSlotCount {
		t.Fatalf("typed mode0 tail rows=%d ok=%v", rows, ok)
	}
}
