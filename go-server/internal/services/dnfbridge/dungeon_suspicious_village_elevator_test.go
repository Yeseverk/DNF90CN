package dnfbridge

import (
	"context"
	"os"
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestCurrentDungeonElevatorNotificationBody(t *testing.T) {
	body := buildCurrentDungeonElevatorNotificationBody(currentDungeonElevatorNotification{Counter: 3, State: 1})
	if len(body) != currentSuspiciousVillageElevatorPacketSize || body[0] != 3 || body[1] != 1 {
		t.Fatalf("elevator body = %v, want [3 1]", body)
	}
}

func TestCurrentDungeonElevatorTimerStateMachine(t *testing.T) {
	state := newCurrentDungeonElevatorState()
	if !state.Started || state.Completed || state.Counter != 0 || state.State != 2 || state.Generation == 0 {
		t.Fatalf("initial elevator state = %+v", state)
	}

	want := []struct {
		counter      byte
		state        byte
		scheduleNext bool
	}{
		{counter: 1, state: 0, scheduleNext: true},
		{counter: 2, state: 0, scheduleNext: true},
		{counter: 3, state: 0, scheduleNext: true},
		{counter: 4, state: 0, scheduleNext: false},
	}
	for index, expected := range want {
		notification, send, scheduleNext := state.tick()
		if !send || notification.Counter != expected.counter || notification.State != expected.state || scheduleNext != expected.scheduleNext {
			t.Fatalf("tick %d = notification=%+v send=%t schedule_next=%t", index+1, notification, send, scheduleNext)
		}
	}
	if _, send, scheduleNext := state.tick(); send || scheduleNext {
		t.Fatalf("fifth tick was not suppressed: send=%t schedule_next=%t state=%+v", send, scheduleNext, state)
	}
}

func TestCurrentDungeonElevatorCompletionStates(t *testing.T) {
	tests := []struct {
		name      string
		counter   byte
		wantState byte
	}{
		{name: "control monster retires before final timer", counter: 3, wantState: 1},
		{name: "control monster retires after final timer", counter: 4, wantState: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newCurrentDungeonElevatorState()
			state.Counter = test.counter
			generation := state.Generation
			notification := state.complete()
			if notification.Counter != test.counter || notification.State != test.wantState ||
				!state.Completed || state.State != test.wantState || state.Generation == generation {
				t.Fatalf("completed elevator state=%+v notification=%+v", state, notification)
			}
		})
	}
}

func TestCurrentSuspiciousVillageElevatorScope(t *testing.T) {
	runtime, scene, room := syntheticCurrentSuspiciousVillageElevatorScope()
	if !currentSuspiciousVillageElevatorScope(runtime, scene, room) {
		t.Fatal("elevator PVF object structure did not match")
	}
	if !currentSuspiciousVillageElevatorMoveBlocked(runtime, scene, room) {
		t.Fatal("unfinished elevator did not block room movement")
	}
	runtime.suspiciousVillageElevator.Completed = true
	if currentSuspiciousVillageElevatorMoveBlocked(runtime, scene, room) {
		t.Fatal("completed elevator still blocked room movement")
	}

	tests := []struct {
		name   string
		mutate func(*runtimeDungeonState, *worldmap.DungeonRoomScene, *runtimeDungeonRoomSnapshot)
		want   bool
	}{
		{name: "other dungeon map and coordinate with same elevator", mutate: func(runtime *runtimeDungeonState, scene *worldmap.DungeonRoomScene, room *runtimeDungeonRoomSnapshot) {
			runtime.Dungeon.ID = 1000
			runtime.MazeIndex = 7
			scene.Coordinate = worldmap.RoomCoordinate{X: 9, Y: 3}
			scene.Map.Map.ID = 400263
			scene.Map.Map.Path = "map/SuspiciousVillage/M3786_(2,5)start.map"
			room.Coordinate = scene.Coordinate
			room.MapID = scene.Map.Map.ID
		}, want: true},
		{name: "missing event monster positions", mutate: func(_ *runtimeDungeonState, scene *worldmap.DungeonRoomScene, _ *runtimeDungeonRoomSnapshot) {
			scene.Map.Map.EventMonsterPositions = nil
		}},
		{name: "missing scroll passive object", mutate: func(_ *runtimeDungeonState, scene *worldmap.DungeonRoomScene, _ *runtimeDungeonRoomSnapshot) {
			scene.PassiveObjects = []worldmap.PassiveObject{{ObjectID: currentSuspiciousVillageElevatorControlID}}
		}},
		{name: "missing controller passive object", mutate: func(_ *runtimeDungeonState, scene *worldmap.DungeonRoomScene, _ *runtimeDungeonRoomSnapshot) {
			scene.PassiveObjects = []worldmap.PassiveObject{{ObjectID: currentSuspiciousVillageElevatorScrollID}}
		}},
		{name: "missing summon passive object", mutate: func(_ *runtimeDungeonState, scene *worldmap.DungeonRoomScene, _ *runtimeDungeonRoomSnapshot) {
			scene.SpecialPassiveObjects = nil
		}},
		{name: "missing control monster actor", mutate: func(_ *runtimeDungeonState, _ *worldmap.DungeonRoomScene, room *runtimeDungeonRoomSnapshot) {
			room.ExtendedActors = nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, scene, room := syntheticCurrentSuspiciousVillageElevatorScope()
			test.mutate(runtime, &scene, &room)
			if got := currentSuspiciousVillageElevatorScope(runtime, scene, room); got != test.want {
				t.Fatalf("elevator scope = %t, want %t: runtime=%+v scene=%+v room=%+v", got, test.want, runtime, scene, room)
			}
		})
	}
}

func TestRealScriptPVFSuspiciousVillageElevatorScope(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify suspicious-village elevator passive objects")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	table, err := worldmap.LoadSource(context.Background(), archive, worldmap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dungeon, ok := table.FindDungeon(53)
	if !ok {
		t.Fatal("real PVF dungeon 53 is missing")
	}
	for index, mapID := range []int64{16408, 400263} {
		mapValue, found := table.FindMap(mapID)
		if !found {
			t.Fatalf("real PVF elevator map %d is missing", mapID)
		}
		coordinate := worldmap.RoomCoordinate{X: int64(2 + index), Y: int64(5 + index)}
		scene := worldmap.DungeonRoomScene{
			Coordinate:            coordinate,
			Map:                   worldmap.ResolvedMap{Map: mapValue},
			PassiveObjects:        append([]worldmap.PassiveObject(nil), mapValue.PassiveObjects...),
			SpecialPassiveObjects: append([]worldmap.SpecialPassiveObject(nil), mapValue.SpecialPassiveObjects...),
		}
		runtime := &runtimeDungeonState{Dungeon: dungeon, MazeIndex: index}
		room := runtimeDungeonRoomSnapshot{
			Coordinate: coordinate,
			MapID:      mapValue.ID,
			ExtendedActors: []runtimeDungeonExtendedActor{{
				Kind:      runtimeDungeonActorSpecialMonster,
				ObjectKey: 403,
				Packet:    currentDungeonStartMapActor{Code: uint32(currentSuspiciousVillageElevatorMonsterID)},
			}},
		}
		if !currentSuspiciousVillageElevatorScope(runtime, scene, room) {
			t.Fatalf(
				"real elevator scope mismatch: map=%d path=%q passive=%+v special=%+v event_positions=%d",
				scene.Map.Map.ID,
				scene.Map.Map.Path,
				scene.PassiveObjects,
				scene.SpecialPassiveObjects,
				len(scene.Map.Map.EventMonsterPositions),
			)
		}
	}
}

func syntheticCurrentSuspiciousVillageElevatorScope() (
	*runtimeDungeonState,
	worldmap.DungeonRoomScene,
	runtimeDungeonRoomSnapshot,
) {
	coordinate := worldmap.RoomCoordinate{X: 2, Y: 5}
	runtime := &runtimeDungeonState{
		Dungeon:   worldmap.Dungeon{ID: 53},
		MazeIndex: 0,
	}
	scene := worldmap.DungeonRoomScene{
		Coordinate: coordinate,
		Map: worldmap.ResolvedMap{Map: worldmap.Map{
			ID:                    16408,
			Path:                  "SuspiciousVillage/(2,5)start.map",
			EventMonsterPositions: make([]worldmap.Point3, 4),
		}},
		PassiveObjects: []worldmap.PassiveObject{
			{ObjectID: currentSuspiciousVillageElevatorScrollID},
			{ObjectID: currentSuspiciousVillageElevatorControlID},
		},
		SpecialPassiveObjects: []worldmap.SpecialPassiveObject{{
			PassiveObject: worldmap.PassiveObject{ObjectID: currentSuspiciousVillageElevatorSummonID},
			Spawns: []worldmap.SpecialObjectSpawn{{
				Kind: "[monster]",
				Code: currentSuspiciousVillageElevatorMonsterID,
			}},
		}},
	}
	room := runtimeDungeonRoomSnapshot{
		Coordinate: coordinate,
		MapID:      16408,
		ExtendedActors: []runtimeDungeonExtendedActor{{
			Kind:      runtimeDungeonActorSpecialMonster,
			ObjectKey: 403,
			Packet:    currentDungeonStartMapActor{Code: uint32(currentSuspiciousVillageElevatorMonsterID)},
		}},
	}
	return runtime, scene, room
}
