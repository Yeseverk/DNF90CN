package dnfbridge

import "longheng.io/server/internal/modules/dnf/worldmap"

func currentDungeonAnnouncedActor(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	objectKey uint32,
) (worldmap.HostileReference, bool, bool) {
	if runtime == nil || runtime.Room == nil || objectKey == 0 {
		return worldmap.HostileReference{}, false, false
	}
	reference, bound := scene.RuntimeObjects[objectKey]
	if !bound {
		return worldmap.HostileReference{}, false, false
	}
	room := runtime.Room.Snapshot()
	if room.Coordinate != scene.Coordinate || room.MapID != scene.Map.Map.ID {
		return worldmap.HostileReference{}, false, false
	}
	for _, monster := range room.Monsters {
		if monster.ObjectKey == objectKey && monster.Reference == reference &&
			monster.State == runtimeDungeonMonsterAnnounced {
			return reference, true, true
		}
	}
	for _, actor := range room.ExtendedActors {
		if actor.ObjectKey == objectKey && actor.HostileReference != nil &&
			*actor.HostileReference == reference && actor.State == runtimeDungeonMonsterAnnounced {
			return reference, false, true
		}
	}
	return worldmap.HostileReference{}, false, false
}

// currentDungeonBossDieCheckCanWaitForTarget permits the narrow op117/op39
// race only after the room is already clear, or when the requested actor is the
// sole remaining blocker. This prevents an early request from opening a room
// while unrelated monsters are still alive.
func currentDungeonBossDieCheckCanWaitForTarget(
	scene worldmap.DungeonRoomScene,
	targetObjectKey uint32,
	targetReference worldmap.HostileReference,
) bool {
	if scene.Cleared {
		return true
	}
	targetIsRemainingBlocker := false
	for _, blockingReference := range scene.BlockingHostiles {
		boundObjectKey := uint32(0)
		for candidate, reference := range scene.RuntimeObjects {
			if reference != blockingReference {
				continue
			}
			if boundObjectKey != 0 {
				return false
			}
			boundObjectKey = candidate
		}
		if boundObjectKey == 0 {
			return false
		}
		if blockingReference == targetReference && boundObjectKey == targetObjectKey {
			targetIsRemainingBlocker = true
			continue
		}
		if !dungeonSceneObjectDefeated(scene.DefeatedObjects, boundObjectKey) {
			return false
		}
	}
	return targetIsRemainingBlocker
}

func currentDungeonDefeatedActor(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	objectKey uint32,
) (worldmap.HostileReference, bool, bool) {
	if runtime == nil || runtime.Room == nil || objectKey == 0 {
		return worldmap.HostileReference{}, false, false
	}
	reference, bound := scene.RuntimeObjects[objectKey]
	if !bound || !dungeonSceneObjectDefeated(scene.DefeatedObjects, objectKey) {
		return worldmap.HostileReference{}, false, false
	}
	room := runtime.Room.Snapshot()
	if room.Coordinate != scene.Coordinate || room.MapID != scene.Map.Map.ID {
		return worldmap.HostileReference{}, false, false
	}
	for _, monster := range room.Monsters {
		if monster.ObjectKey == objectKey && monster.Reference == reference &&
			monster.State == runtimeDungeonMonsterDefeated {
			return reference, true, true
		}
	}
	for _, actor := range room.ExtendedActors {
		if actor.ObjectKey == objectKey && actor.HostileReference != nil &&
			*actor.HostileReference == reference && actor.State == runtimeDungeonMonsterDefeated {
			return reference, false, true
		}
	}
	return worldmap.HostileReference{}, false, false
}

func dungeonSceneObjectDefeated(values []uint32, objectKey uint32) bool {
	for _, value := range values {
		if value == objectKey {
			return true
		}
	}
	return false
}

func currentDungeonRemainingBlockingMonsterIndexes(scene worldmap.DungeonRoomScene) ([]int, bool) {
	remaining := make([]int, 0, len(scene.BlockingHostiles))
	for _, reference := range scene.BlockingHostiles {
		if reference.Kind != worldmap.HostileMonster || reference.Index < 0 {
			return nil, false
		}
		var objectKey uint32
		for candidate, boundReference := range scene.RuntimeObjects {
			if boundReference == reference {
				if objectKey != 0 {
					return nil, false
				}
				objectKey = candidate
			}
		}
		if objectKey == 0 {
			return nil, false
		}
		if !dungeonSceneObjectDefeated(scene.DefeatedObjects, objectKey) {
			remaining = append(remaining, reference.Index)
		}
	}
	return remaining, true
}
