package dnfbridge

import (
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestDungeonRuntimeContainsDeathOwnerObjectKey(t *testing.T) {
	currentMonsterKey := uint32(407)
	currentRoom := dungeonDeathOwnerTestRoom(
		[]runtimeDungeonMonster{{ObjectKey: currentMonsterKey, State: runtimeDungeonMonsterAnnounced}},
		nil,
	)
	previousMonsterKey := uint32(402)
	previousFriendlyAPCKey := uint32(405)
	previousHostileAPCKey := uint32(406)
	previousUnannouncedAPCKey := uint32(412)
	previousHostileReference := worldmap.HostileReference{Kind: worldmap.HostileAICharacter, Index: 0}
	previousRoom := dungeonDeathOwnerTestRoom(
		[]runtimeDungeonMonster{{ObjectKey: previousMonsterKey, State: runtimeDungeonMonsterDefeated}},
		[]runtimeDungeonExtendedActor{
			{
				Kind:      runtimeDungeonActorAICharacter,
				ObjectKey: previousFriendlyAPCKey,
				State:     runtimeDungeonMonsterAnnounced,
			},
			{
				Kind:             runtimeDungeonActorAICharacter,
				ObjectKey:        previousHostileAPCKey,
				HostileReference: &previousHostileReference,
				State:            runtimeDungeonMonsterAnnounced,
			},
			{
				Kind:      runtimeDungeonActorAICharacter,
				ObjectKey: previousUnannouncedAPCKey,
				State:     runtimeDungeonMonsterPlanned,
			},
		},
	)
	runtime := &runtimeDungeonState{
		Room: currentRoom,
		Rooms: map[runtimeDungeonRoomKey]*runtimeDungeonRoomVisit{
			{X: 3, Y: 1, MapID: 57880}: {Room: currentRoom},
			{X: 4, Y: 1, MapID: 57879}: {Room: previousRoom},
		},
	}

	tests := []struct {
		name      string
		objectKey uint32
		want      bool
	}{
		{name: "current room actor", objectKey: currentMonsterKey, want: true},
		{name: "previous room friendly APC", objectKey: previousFriendlyAPCKey, want: true},
		{name: "previous room monster", objectKey: previousMonsterKey, want: false},
		{name: "previous room hostile APC", objectKey: previousHostileAPCKey, want: false},
		{name: "previous room unannounced APC", objectKey: previousUnannouncedAPCKey, want: false},
		{name: "unknown object", objectKey: 65000, want: false},
		{name: "zero object", objectKey: 0, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := dungeonRuntimeContainsDeathOwnerObjectKey(runtime, test.objectKey); got != test.want {
				t.Fatalf("owner object key %d accepted=%t want=%t", test.objectKey, got, test.want)
			}
		})
	}
}

func dungeonDeathOwnerTestRoom(
	monsters []runtimeDungeonMonster,
	extendedActors []runtimeDungeonExtendedActor,
) *runtimeDungeonRoom {
	room := &runtimeDungeonRoom{
		monsters:            monsters,
		byObjectKey:         make(map[uint32]int, len(monsters)),
		extendedActors:      extendedActors,
		extendedByObjectKey: make(map[uint32]int, len(extendedActors)),
	}
	for index, monster := range monsters {
		room.byObjectKey[monster.ObjectKey] = index
	}
	for index, actor := range extendedActors {
		room.extendedByObjectKey[actor.ObjectKey] = index
	}
	return room
}
