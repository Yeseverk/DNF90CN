package dnfbridge

import (
	"bytes"
	"context"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	dnfpet "longheng.io/server/internal/modules/dnf/pet"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestAwardCurrentPetRoomClearCommitsOnceAndSendsExactSceneOp102(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "19",
		EquippedKey: "37",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {
				PetKey:          "37",
				CreatureKey:     37,
				ItemID:          63000,
				SourceListType:  3,
				SourceSlotIndex: 26,
				Satiety:         100,
				SatietyMicros:   100_000_000,
				Level:           1,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {SlotIndex: 26, ItemID: 63000, RawEntry: testCurrentEquippedCreatureRaw(26, 63000, 37)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfpet.NewPVFCatalog(bridgePetCatalogFixture())
	if err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "dnf:1",
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		petCatalog:         catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	anchor := time.Now()
	session := &gameSession{
		conn:                connection,
		connID:              "pet-growth-runtime",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 19,
		petGrowth: petGrowthClockState{
			mode:        currentPetGrowthClockDungeon,
			characterID: 19,
			anchor:      anchor,
		},
	}
	runtime := &runtimeDungeonState{
		Dungeon:        worldmap.Dungeon{ID: 1001},
		MazeIndex:      2,
		startedAt:      anchor.Add(-time.Minute),
		lifecycleToken: 7,
	}
	scene := worldmap.DungeonRoomScene{Coordinate: worldmap.RoomCoordinate{X: 3, Y: 4}, Cleared: true}

	service.awardCurrentPetRoomClearLocked(session, runtime, scene, "test_authoritative_clear")
	packet, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if packet.Header.MsgID != currentCreatureGrowthMsgID || packet.Header.Classification != 0 ||
		!bytes.Equal(packet.Body, []byte{0, 0, 0, 1, 0, 1, 0, 0, 0}) {
		t.Fatalf("packet header=%+v body=%x", packet.Header, packet.Body)
	}
	if len(trailing) != 0 {
		t.Fatalf("trailing=%x", trailing)
	}
	record, found, err := repositories.Pet.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load pet found=%t err=%v", found, err)
	}
	entry := record.Entries["37"]
	if entry.Exp != 1 || entry.Level != 1 || len(entry.AppliedClearTokens) != 1 {
		t.Fatalf("entry=%+v", entry)
	}

	beforeLen := connection.write.Len()
	service.awardCurrentPetRoomClearLocked(session, runtime, scene, "test_authoritative_clear_replay")
	if connection.write.Len() != beforeLen {
		t.Fatalf("replay sent another packet: before=%d after=%d", beforeLen, connection.write.Len())
	}
	record, _, _ = repositories.Pet.Load(ctx, "19")
	if record.Entries["37"].Exp != 1 || len(record.Entries["37"].AppliedClearTokens) != 1 {
		t.Fatalf("replay mutated entry=%+v", record.Entries["37"])
	}
}

func TestCurrentPetMoveTouchesGrowthStateOnlyForProvedList7Targets(t *testing.T) {
	body := make([]byte, 28)
	body[0] = 7
	body[11] = 17
	body[12] = 26
	if !currentPetMoveTouchesGrowthState(body) {
		t.Fatal("proved list7 -> endpoint17 target26 was not recognized")
	}
	body[12] = 29
	if !currentPetMoveTouchesGrowthState(body) {
		t.Fatal("proved artifact target29 was not recognized")
	}
	body[12] = 25
	if currentPetMoveTouchesGrowthState(body) {
		t.Fatal("ordinary target25 was treated as pet growth mutation")
	}
}
