package dnfbridge

import (
	"path"
	"strings"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	atSwordmanTutorialJob         = "11"
	atSwordmanTutorialDungeonPath = "dungeon/cataclysm/newtutorial/tutorial_atswordman.dgn"
	atSwordmanTutorialMapPrefix   = "map/cataclysm/newtutorial/atswordman/"
)

// isATSwordmanTutorialScene deliberately keys the compatibility behavior to
// the female Slayer tutorial's real PVF ownership. Other professions have
// different tutorial dungeons and must keep their ordinary protocol flow.
func isATSwordmanTutorialScene(runtime *runtimeDungeonState, scene worldmap.DungeonRoomScene) bool {
	if runtime == nil || strings.TrimSpace(runtime.Character.Job) != atSwordmanTutorialJob {
		return false
	}
	if normalizeDungeonRuntimePath(runtime.Dungeon.Path) != atSwordmanTutorialDungeonPath {
		return false
	}
	return strings.HasPrefix(normalizeDungeonRuntimePath(scene.Map.Map.Path), atSwordmanTutorialMapPrefix)
}

// isPVFTutorialDungeonScene identifies tutorial ownership from the dungeon
// document itself. It deliberately does not key on a profession, dungeon ID,
// map ID, path, or monster ID: every profession owns a different tutorial
// graph, while [tutorial dungeon] is their shared PVF contract.
func isPVFTutorialDungeonScene(runtime *runtimeDungeonState, scene worldmap.DungeonRoomScene) bool {
	if runtime == nil || runtime.Room == nil || runtime.Session == nil {
		return false
	}
	if !isPVFTutorialDungeon(runtime) {
		return false
	}
	return currentDungeonRoomOwnsScene(runtime, scene)
}

func isPVFTutorialDungeon(runtime *runtimeDungeonState) bool {
	if runtime == nil {
		return false
	}
	tutorial := runtime.Dungeon.Metadata.TutorialDungeon
	return tutorial.Set && tutorial.Value == 1
}

// isPVFTutorialScriptedMonsterDeath accepts only an ordinary PVF [monster]
// that is currently announced by the owned room and whose static map index is
// an explicit CMT [DESTROY] target for this map. AI/APC actors live in the
// extended-actor table, so their sentinel-owner retirement remains separate.
func (s *Service) isPVFTutorialScriptedMonsterDeath(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	objectKey uint32,
) (runtimeDungeonMonster, dungeonTutorialScriptEvidence, bool) {
	if !isPVFTutorialDungeonScene(runtime, scene) {
		return runtimeDungeonMonster{}, dungeonTutorialScriptEvidence{}, false
	}
	monster, ok := runtime.Room.AnnouncedMonster(objectKey)
	if !ok || monster.Reference.Kind != worldmap.HostileMonster {
		return runtimeDungeonMonster{}, dungeonTutorialScriptEvidence{}, false
	}
	catalog, ok := s.dungeonTutorialScriptCatalog()
	if !ok {
		return runtimeDungeonMonster{}, dungeonTutorialScriptEvidence{}, false
	}
	evidence, ok := catalog.FindMonsterDestroy(scene.Map.Map.ID, monster.Reference.Index)
	if !ok {
		return runtimeDungeonMonster{}, dungeonTutorialScriptEvidence{}, false
	}
	return monster, evidence, true
}

func currentDungeonRoomOwnsScene(runtime *runtimeDungeonState, scene worldmap.DungeonRoomScene) bool {
	if runtime == nil || runtime.Room == nil || runtime.Session == nil {
		return false
	}
	current, ok := runtime.Session.Scene()
	if !ok || current.Coordinate != scene.Coordinate || current.Map.Map.ID != scene.Map.Map.ID {
		return false
	}
	room := runtime.Room.Snapshot()
	return room.Coordinate == scene.Coordinate && room.MapID == scene.Map.Map.ID
}

func normalizeDungeonRuntimePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	if value == "" {
		return ""
	}
	return strings.ToLower(path.Clean(value))
}
