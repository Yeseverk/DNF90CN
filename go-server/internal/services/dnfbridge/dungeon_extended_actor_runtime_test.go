package dnfbridge

import (
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestPlanRuntimeDungeonExtendedActorsUsesTypedPVFOwners(t *testing.T) {
	monsterCatalog, err := newPVFDungeonMonsterCatalog(bridgePVFSource{
		"monster/monster.lst": "3001 `Special.mob`\n",
		"monster/Special.mob": "[name]\n`Special Monster`\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	aiCatalog, err := newPVFDungeonAICharacterCatalog(dungeonAICatalogSource{
		defaultDungeonAICharacterList: "4001 `Enemy.aic`\n4002 `Friend.aic`\n",
		"AICharacter/Enemy.aic":       "[minimum info]\n`Enemy APC` 1 2 3 4 31\n[future]\n9\n",
		"AICharacter/Friend.aic":      "[minimum info]\n`Friend APC` 1 2 3 4 22\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	scene := worldmap.DungeonRoomScene{
		SpecialPassiveObjects: []worldmap.SpecialPassiveObject{{
			PassiveObject: worldmap.PassiveObject{ObjectID: 7001},
			Spawns: []worldmap.SpecialObjectSpawn{
				{Kind: "[item]", Code: 8001, Level: 1},
				{Kind: "[monster]", Code: 3001, Level: 0},
			},
		}},
		AICharacters: []worldmap.AICharacter{
			{Code: 4001, Faction: "[monster]", AIType: "[boss]", Params: []int64{1, 2}},
			{Code: 4002, Faction: "[character]", AIType: "[normal]"},
		},
	}
	plan, err := planRuntimeDungeonExtendedActors(
		scene,
		monsterCatalog,
		aiCatalog,
		worldmap.OptionalInt{Value: 27, Set: true},
		402,
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.NextObjectKey != 406 || plan.SpecialObjectCount != 1 || plan.AICharacterCount != 2 {
		t.Fatalf("plan summary=%+v", plan)
	}
	if len(plan.Actors) != 4 || len(plan.RetainedSpecialSpawns) != 1 || plan.RetainedSpecialSpawns[0].Spawn.Kind != "[item]" {
		t.Fatalf("actors=%+v retained=%+v", plan.Actors, plan.RetainedSpecialSpawns)
	}
	object := plan.Actors[0]
	if object.Kind != runtimeDungeonActorSpecialObject || object.ObjectKey != 402 || object.Packet.Type != 9 || object.Packet.Code != 7001 || object.Packet.Blocking != 0 {
		t.Fatalf("special object=%+v", object)
	}
	spawn := plan.Actors[1]
	if spawn.Kind != runtimeDungeonActorSpecialMonster || spawn.ObjectKey != 403 || spawn.Packet.Code != 3001 || spawn.Packet.Level != 27 || spawn.Packet.Flag0 != 1 || spawn.Packet.Flag1 != 0 || spawn.Packet.Blocking != 0 {
		t.Fatalf("special monster=%+v", spawn)
	}
	enemy := plan.Actors[2]
	if enemy.Kind != runtimeDungeonActorAICharacter || enemy.ObjectKey != 404 || enemy.Packet.Type != 8 || enemy.Packet.Level != 31 || enemy.Packet.Blocking != 0 || enemy.HostileReference == nil || *enemy.HostileReference != (worldmap.HostileReference{Kind: worldmap.HostileAICharacter, Index: 0}) {
		t.Fatalf("enemy APC=%+v", enemy)
	}
	friend := plan.Actors[3]
	if friend.ObjectKey != 405 || friend.Packet.Type != 5 || friend.Packet.Level != 22 || friend.Packet.Blocking != 0 || friend.HostileReference != nil {
		t.Fatalf("friend APC=%+v", friend)
	}
	if enemy.AICharacter == nil || len(enemy.AICharacter.Params) != 2 || enemy.AICharacterMetadata == nil || len(enemy.AICharacterMetadata.Sections) != 2 {
		t.Fatalf("APC ownership not retained: %+v", enemy)
	}
}

func TestAttachRuntimeDungeonExtendedActorsResolvesOpaqueHostilesAndFeedsStartMap(t *testing.T) {
	reference := worldmap.HostileReference{Kind: worldmap.HostileAICharacter, Index: 0}
	room := &runtimeDungeonRoom{
		coordinate:     worldmap.RoomCoordinate{X: 1, Y: 2},
		mapID:          100,
		opaqueHostiles: []worldmap.HostileReference{reference},
	}
	plan := runtimeDungeonExtendedActorPlan{
		Actors: []runtimeDungeonExtendedActor{{
			Kind:             runtimeDungeonActorAICharacter,
			ObjectKey:        402,
			Packet:           currentDungeonStartMapActor{ObjectKey: 402, Code: 4001, Level: 20, Type: 5, Blocking: 0},
			HostileReference: &reference,
		}},
		SpecialObjectCount: 0,
		AICharacterCount:   1,
	}
	if err := room.AttachExtendedActors(plan); err != nil {
		t.Fatal(err)
	}
	scene := worldmap.DungeonRoomScene{
		Coordinate:   worldmap.RoomCoordinate{X: 1, Y: 2},
		Map:          worldmap.ResolvedMap{Map: worldmap.Map{ID: 100}},
		AICharacters: []worldmap.AICharacter{{Code: 4001, Faction: "[monster]", AIType: "[normal]"}},
	}
	runtime := &runtimeDungeonState{Room: room}
	actors, err := currentDungeonStartMapActors(runtime, scene)
	if err != nil {
		t.Fatal(err)
	}
	if len(actors) != 1 || actors[0] != plan.Actors[0].Packet {
		t.Fatalf("actors=%+v", actors)
	}
	snapshot := room.Snapshot()
	if len(snapshot.OpaqueHostiles) != 0 || len(snapshot.ExtendedActors) != 1 || snapshot.AICharacterCount != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestPlanRuntimeDungeonExtendedActorsRejectsUnownedPVFValues(t *testing.T) {
	monsterCatalog, err := newPVFDungeonMonsterCatalog(bridgePVFSource{
		"monster/monster.lst": "3001 `Special.mob`\n",
		"monster/Special.mob": "[name]\n`Special Monster`\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		scene worldmap.DungeonRoomScene
		want  error
	}{
		{name: "unknown special kind", scene: worldmap.DungeonRoomScene{SpecialPassiveObjects: []worldmap.SpecialPassiveObject{{Spawns: []worldmap.SpecialObjectSpawn{{Kind: "[future actor]", Code: 1}}}}}, want: errDungeonSpecialSpawnKind},
		{name: "missing special monster", scene: worldmap.DungeonRoomScene{SpecialPassiveObjects: []worldmap.SpecialPassiveObject{{Spawns: []worldmap.SpecialObjectSpawn{{Kind: "[monster]", Code: 9999, Level: 1}}}}}, want: errDungeonMonsterDefinitionMiss},
		{name: "AI catalog required", scene: worldmap.DungeonRoomScene{AICharacters: []worldmap.AICharacter{{Code: 4001, Faction: "[monster]", AIType: "[normal]"}}}, want: errDungeonAICharacterUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := planRuntimeDungeonExtendedActors(test.scene, monsterCatalog, nil, worldmap.OptionalInt{Value: 20, Set: true}, 402)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestCurrentDungeonAICharacterMappingsRejectUnknownValues(t *testing.T) {
	types := map[string]byte{"[normal]": 5, "[champion]": 6, "[super champion]": 7, "[boss]": 8}
	for value, want := range types {
		got, err := currentDungeonAICharacterType(value)
		if err != nil || got != want {
			t.Fatalf("type %q got=%d error=%v want=%d", value, got, err, want)
		}
	}
	if _, err := currentDungeonAICharacterType("[future]"); !errors.Is(err, errDungeonAICharacterType) {
		t.Fatalf("unknown type error=%v", err)
	}
	for value, want := range map[string]byte{"[monster]": 0, "[character]": 0, "[neutral]": 0} {
		got, err := currentDungeonAICharacterBlocking(value)
		if err != nil || got != want {
			t.Fatalf("faction %q got=%d error=%v want=%d", value, got, err, want)
		}
	}
	if _, err := currentDungeonAICharacterBlocking("[future]"); !errors.Is(err, errDungeonAICharacterFaction) {
		t.Fatalf("unknown faction error=%v", err)
	}
	for value, want := range map[string]bool{"[monster]": true, "[character]": false, "[neutral]": false} {
		got, err := currentDungeonAICharacterHostile(value)
		if err != nil || got != want {
			t.Fatalf("hostility %q got=%t error=%v want=%t", value, got, err, want)
		}
	}
	if _, err := currentDungeonAICharacterHostile("[future]"); !errors.Is(err, errDungeonAICharacterFaction) {
		t.Fatalf("unknown hostility error=%v", err)
	}
}
