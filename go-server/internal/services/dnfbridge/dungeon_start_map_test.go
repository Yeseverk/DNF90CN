package dnfbridge

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestCurrentDungeonStartMapBuildMatchesCurrentEXEReader(t *testing.T) {
	body, err := (currentDungeonStartMap{
		X: 2, Y: 3, LayeredRoomFlag: 4,
		Seed: 0x08070605, HellPartyMode: 9, UnknownAfterHellPartyMode: 10,
		RoomStateValue: 0x0e0d0c0b, RoomStateFlag: 15, MapID: 0x13121110,
		Actors: []currentDungeonStartMapActor{{
			TemplateOrder: 0x2120, PacketIndex: 0x25242322, ObjectKey: 0x2726,
			Code: 0x2b2a2928, Level: 44, Type: 45, Flag0: 46, Flag1: 47, ExtraState: 0x33323130,
			Blocking: 52,
		}},
		ExtraEntries: []currentDungeonStartMapExtraEntry{{
			PassiveObjectIndex: 53, GlobalSequence: 0x39383736, ItemID: 0x3d3c3b3a,
			StackCount: 0x41403f3e, Endurance: 0x4342, AmplifyType: 68,
			AmplifyValue: 0x4645, ExtendedValue: 0x4847, ExtendedFlag: 73,
		}},
		HellPartyFogFlag: 74,
		RidableGroups: [][]currentDungeonStartMapRidableEntry{{{
			PositionX: 0x4e4d4c4b, PositionY: 0x5251504f, ObjectCode: 0x56555453,
			Faction: 0x5a595857, State: 0x5e5d5c5b,
		}}},
		PartyMemberIndex: 0x5f,
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 86 {
		t.Fatalf("body len=%d want=86 body=%x", len(body), body)
	}
	const wantHex = "02030405060708090a0b0c0d0e0f1011121301202122232425262728292a2b2c2d2e2f30313233340135363738393a3b3c3d3e3f404142434445464748494a01014b4c4d4e4f505152535455565758595a5b5c5d5e5f"
	if got := hex.EncodeToString(body); got != wantHex {
		t.Fatalf("body=%s want=%s", got, wantHex)
	}
	if got := binary.LittleEndian.Uint32(body[14:18]); got != 0x13121110 {
		t.Fatalf("current EXE map id width/order got=%08x", got)
	}
}

func TestCurrentDungeonStartMapBuildEmptyCollections(t *testing.T) {
	body, err := (currentDungeonStartMap{MapID: 1, PartyMemberIndex: 0xff}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 23 {
		t.Fatalf("body len=%d want=23 body=%x", len(body), body)
	}
	if body[18] != 0 || body[19] != 0 || body[20] != 0 || body[21] != 0 || body[22] != 0xff {
		t.Fatalf("empty collection tail=%x", body[18:])
	}
}

func TestCurrentDungeonStartMapBuildRejectsCountOverflow(t *testing.T) {
	tests := []struct {
		name   string
		packet currentDungeonStartMap
		want   error
	}{
		{name: "actors", packet: currentDungeonStartMap{Actors: make([]currentDungeonStartMapActor, 256)}, want: errDungeonStartMapActorCount},
		{name: "extras", packet: currentDungeonStartMap{ExtraEntries: make([]currentDungeonStartMapExtraEntry, 256)}, want: errDungeonStartMapExtraCount},
		{name: "groups", packet: currentDungeonStartMap{RidableGroups: make([][]currentDungeonStartMapRidableEntry, 256)}, want: errDungeonStartMapGroupCount},
		{name: "group entries", packet: currentDungeonStartMap{RidableGroups: [][]currentDungeonStartMapRidableEntry{make([]currentDungeonStartMapRidableEntry, 256)}}, want: errDungeonStartMapGroupCount},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.packet.Build()
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestCurrentDungeonMonsterType(t *testing.T) {
	tests := map[string]byte{
		"[normal]": 0, "[dummy]": 0, "champion": 1, "[super champion]": 2, "[superchampion]": 2, "[boss]": 3,
	}
	for rank, want := range tests {
		got, err := currentDungeonMonsterType(rank)
		if err != nil || got != want {
			t.Fatalf("rank=%q got=%d error=%v want=%d", rank, got, err, want)
		}
	}
	if _, err := currentDungeonMonsterType("[new rank]"); !errors.Is(err, errDungeonStartMapMonsterRank) {
		t.Fatalf("unknown rank error=%v", err)
	}
}

func TestCurrentDungeonMonsterActorTypeAcceptsBossSuffixMarker(t *testing.T) {
	got, err := currentDungeonMonsterActorType(worldmap.MonsterSpawn{Rank: "[dummy]", SuffixMarker: "[boss]"})
	if err != nil || got != 3 {
		t.Fatalf("boss suffix actor type=%d err=%v want=3", got, err)
	}
	got, err = currentDungeonMonsterActorType(worldmap.MonsterSpawn{Rank: "[dummy]"})
	if err != nil || got != 0 {
		t.Fatalf("dummy actor type=%d err=%v want=0", got, err)
	}
	got, err = currentDungeonMonsterActorType(worldmap.MonsterSpawn{Rank: "[NPC]"})
	if err != nil || got != currentDungeonNPCActorType {
		t.Fatalf("NPC actor type=%d err=%v want=%d", got, err, currentDungeonNPCActorType)
	}
}

func TestCurrentDungeonStartMapNPCIsNonBlocking(t *testing.T) {
	coordinate := worldmap.RoomCoordinate{X: 4, Y: 2}
	spawn := worldmap.MonsterSpawn{MonsterID: 62531, AutoLevel: 62, Rank: "[NPC]"}
	runtime := &runtimeDungeonState{
		Room: &runtimeDungeonRoom{
			coordinate: coordinate,
			mapID:      91548,
			monsters: []runtimeDungeonMonster{{
				ObjectKey: 444,
				Spawn:     spawn,
			}},
		},
	}
	scene := worldmap.DungeonRoomScene{
		Coordinate: coordinate,
		Map:        worldmap.ResolvedMap{Map: worldmap.Map{ID: 91548}},
		Monsters:   []worldmap.MonsterSpawn{spawn},
	}
	packet, err := currentDungeonStartMapFromRuntime(runtime, scene, currentDungeonStartMapState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Actors) != 1 || packet.Actors[0].Type != currentDungeonNPCActorType || packet.Actors[0].Blocking != 1 {
		t.Fatalf("NPC actors=%+v", packet.Actors)
	}
	if _, err := currentDungeonMonsterType(spawn.Rank); !errors.Is(err, errDungeonStartMapMonsterRank) {
		t.Fatalf("NPC unexpectedly entered ordinary reward type mapping: %v", err)
	}
}

func TestCurrentDungeonMonsterLevelUsesPVFSemantics(t *testing.T) {
	tests := []struct {
		name  string
		spawn worldmap.MonsterSpawn
		basis worldmap.OptionalInt
		want  byte
	}{
		{name: "fixed", spawn: worldmap.MonsterSpawn{AutoLevel: 17}, want: 17},
		{name: "relative", spawn: worldmap.MonsterSpawn{Level: 1, AutoLevel: -2}, basis: worldmap.OptionalInt{Value: 20, Set: true}, want: 18},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := currentDungeonMonsterLevel(test.spawn, test.basis)
			if err != nil || got != test.want {
				t.Fatalf("level=%d error=%v want=%d", got, err, test.want)
			}
		})
	}
	bad := []struct {
		spawn worldmap.MonsterSpawn
		basis worldmap.OptionalInt
	}{
		{spawn: worldmap.MonsterSpawn{Level: 1, AutoLevel: 1}},
		{spawn: worldmap.MonsterSpawn{AutoLevel: 0}},
		{spawn: worldmap.MonsterSpawn{AutoLevel: 256}},
	}
	for _, test := range bad {
		if _, err := currentDungeonMonsterLevel(test.spawn, test.basis); !errors.Is(err, errDungeonStartMapMonsterLevel) {
			t.Fatalf("spawn=%+v basis=%+v error=%v", test.spawn, test.basis, err)
		}
	}
}

func TestCurrentDungeonStartMapFromRuntimeUsesPVFOwnedFields(t *testing.T) {
	coordinate := worldmap.RoomCoordinate{X: 7, Y: 9}
	spawn := worldmap.MonsterSpawn{MonsterID: 123456, Level: 1, AutoLevel: -2, Rank: "[boss]"}
	runtime := &runtimeDungeonState{
		Dungeon: worldmap.Dungeon{Metadata: worldmap.DungeonMetadata{
			BasisLevel: worldmap.OptionalInt{Value: 30, Set: true},
		}},
		Room: &runtimeDungeonRoom{
			coordinate: coordinate,
			mapID:      70000,
			monsters: []runtimeDungeonMonster{{
				ObjectKey: 402,
				Spawn:     spawn,
			}},
			byObjectKey: make(map[uint32]int),
		},
	}
	scene := worldmap.DungeonRoomScene{
		Coordinate: coordinate,
		Map:        worldmap.ResolvedMap{Map: worldmap.Map{ID: 70000}},
		Monsters:   []worldmap.MonsterSpawn{spawn},
	}
	packet, err := currentDungeonStartMapFromRuntime(runtime, scene, currentDungeonStartMapState{
		LayeredRoomFlag: 2, Seed: 0x12345678, HellPartyMode: 3,
		UnknownAfterHellPartyMode: 4, RoomStateValue: 5, RoomStateFlag: 6,
		HellPartyFogFlag: 7, PartyMemberIndex: 0xff,
	})
	if err != nil {
		t.Fatal(err)
	}
	if packet.X != 7 || packet.Y != 9 || packet.MapID != 70000 || packet.Seed != 0x12345678 {
		t.Fatalf("packet PVF/runtime fields=%+v", packet)
	}
	if len(packet.Actors) != 1 {
		t.Fatalf("actors=%+v", packet.Actors)
	}
	actor := packet.Actors[0]
	if actor.ObjectKey != 402 || actor.Code != 123456 || actor.Level != 28 || actor.Type != 3 || actor.Blocking != 0 {
		t.Fatalf("actor=%+v", actor)
	}
	if body, buildErr := packet.Build(); buildErr != nil || len(body) != 44 {
		t.Fatalf("build len=%d error=%v body=%x", len(body), buildErr, body)
	}
}

func TestCurrentDungeonStartMapUsesScriptedRoomClearSkipByte(t *testing.T) {
	coordinate := worldmap.RoomCoordinate{X: 1, Y: 0}
	spawn := worldmap.MonsterSpawn{MonsterID: 70216, AutoLevel: 1, Rank: "[normal]"}
	runtime := &runtimeDungeonState{
		Room: &runtimeDungeonRoom{
			coordinate: coordinate,
			mapID:      70577,
			monsters: []runtimeDungeonMonster{{
				ObjectKey:         405,
				Reference:         worldmap.HostileReference{Kind: worldmap.HostileMonster, Index: 0},
				Spawn:             spawn,
				RoomClearSkipByte: 1,
			}},
		},
	}
	scene := worldmap.DungeonRoomScene{
		Coordinate: coordinate,
		Map:        worldmap.ResolvedMap{Map: worldmap.Map{ID: 70577}},
		Monsters:   []worldmap.MonsterSpawn{spawn},
	}
	packet, err := currentDungeonStartMapFromRuntime(runtime, scene, currentDungeonStartMapState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Actors) != 1 || packet.Actors[0].Blocking != 1 {
		t.Fatalf("actors=%+v", packet.Actors)
	}
}

func TestCurrentDungeonStartMapFromRuntimeRejectsUnownedActors(t *testing.T) {
	coordinate := worldmap.RoomCoordinate{X: 1, Y: 1}
	spawn := worldmap.MonsterSpawn{MonsterID: 10, AutoLevel: 10, Rank: "[normal]"}
	baseRoom := func() *runtimeDungeonRoom {
		return &runtimeDungeonRoom{
			coordinate: coordinate,
			mapID:      20,
			monsters:   []runtimeDungeonMonster{{ObjectKey: 402, Spawn: spawn}},
		}
	}
	baseScene := func() worldmap.DungeonRoomScene {
		return worldmap.DungeonRoomScene{
			Coordinate: coordinate,
			Map:        worldmap.ResolvedMap{Map: worldmap.Map{ID: 20}},
			Monsters:   []worldmap.MonsterSpawn{spawn},
		}
	}
	tests := []struct {
		name   string
		mutate func(*runtimeDungeonState, *worldmap.DungeonRoomScene)
		want   error
	}{
		{name: "opaque hostile", want: errDungeonStartMapOpaqueHostile, mutate: func(runtime *runtimeDungeonState, _ *worldmap.DungeonRoomScene) {
			runtime.Room.opaqueHostiles = []worldmap.HostileReference{{Kind: worldmap.HostileAICharacter}}
		}},
		{name: "unplanned special passive", want: errDungeonStartMapSceneMismatch, mutate: func(_ *runtimeDungeonState, scene *worldmap.DungeonRoomScene) {
			scene.SpecialPassiveObjects = []worldmap.SpecialPassiveObject{{}}
		}},
		{name: "scene mismatch", want: errDungeonStartMapSceneMismatch, mutate: func(_ *runtimeDungeonState, scene *worldmap.DungeonRoomScene) {
			scene.Map.Map.ID++
		}},
		{name: "unknown rank", want: errDungeonStartMapMonsterRank, mutate: func(runtime *runtimeDungeonState, scene *worldmap.DungeonRoomScene) {
			runtime.Room.monsters[0].Spawn.Rank = "[new rank]"
			scene.Monsters[0].Rank = "[new rank]"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := &runtimeDungeonState{Room: baseRoom()}
			scene := baseScene()
			test.mutate(runtime, &scene)
			_, err := currentDungeonStartMapFromRuntime(runtime, scene, currentDungeonStartMapState{})
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}
