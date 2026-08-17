package dnfbridge

import "longheng.io/server/internal/modules/dnf/worldmap"

// currentDungeonStoryAIBossGate describes the PVF story-room shape where a
// hostile type-8 AI boss and a blocking dummy monster marked [boss] are
// separate authoritative death owners. Neither death may fabricate the other.
type currentDungeonStoryAIBossGate struct {
	DummyBossObjectKeys []uint32
	AIBossObjectKeys    []uint32
	Ready               bool
}

func currentDungeonStoryAIBossDeathGate(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
) (currentDungeonStoryAIBossGate, bool) {
	if runtime == nil || runtime.Room == nil {
		return currentDungeonStoryAIBossGate{}, false
	}
	room := runtime.Room.Snapshot()
	if room.Coordinate != scene.Coordinate || room.MapID != scene.Map.Map.ID {
		return currentDungeonStoryAIBossGate{}, false
	}

	blocking := make(map[worldmap.HostileReference]struct{}, len(scene.BlockingHostiles))
	for _, reference := range scene.BlockingHostiles {
		blocking[reference] = struct{}{}
	}
	defeated := make(map[uint32]struct{}, len(scene.DefeatedObjects))
	for _, objectKey := range scene.DefeatedObjects {
		defeated[objectKey] = struct{}{}
	}

	gate := currentDungeonStoryAIBossGate{Ready: true}
	for _, monster := range room.Monsters {
		if normalizeDungeonPVFSymbol(monster.Spawn.Rank) != "dummy" ||
			normalizeDungeonPVFSymbol(monster.Spawn.SuffixMarker) != "boss" {
			continue
		}
		if _, ok := blocking[monster.Reference]; !ok {
			continue
		}
		gate.DummyBossObjectKeys = append(gate.DummyBossObjectKeys, monster.ObjectKey)
		if monster.State != runtimeDungeonMonsterDefeated {
			gate.Ready = false
			continue
		}
		if _, ok := defeated[monster.ObjectKey]; !ok {
			gate.Ready = false
		}
	}
	for _, actor := range room.ExtendedActors {
		if actor.Kind != runtimeDungeonActorAICharacter || actor.HostileReference == nil ||
			actor.HostileReference.Kind != worldmap.HostileAICharacter || actor.Packet.Type != 8 ||
			actor.AICharacter == nil || normalizeDungeonPVFSymbol(actor.AICharacter.Faction) != "monster" ||
			normalizeDungeonPVFSymbol(actor.AICharacter.AIType) != "boss" {
			continue
		}
		gate.AIBossObjectKeys = append(gate.AIBossObjectKeys, actor.ObjectKey)
		if actor.State != runtimeDungeonMonsterDefeated {
			gate.Ready = false
			continue
		}
		if _, ok := defeated[actor.ObjectKey]; !ok {
			gate.Ready = false
		}
	}
	if len(gate.DummyBossObjectKeys) == 0 || len(gate.AIBossObjectKeys) == 0 {
		return currentDungeonStoryAIBossGate{}, false
	}
	return gate, true
}
