package dnfbridge

import "longheng.io/server/internal/modules/dnf/worldmap"

func currentDungeonStoryStageMatches(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	stageIndex int,
) bool {
	if runtime == nil || stageIndex < 0 || stageIndex >= len(runtime.StoryStages) {
		return false
	}
	stage := runtime.StoryStages[stageIndex]
	return stage.Index == stageIndex && stage.MapID > 0 &&
		scene.Coordinate == stage.Coordinate && scene.Map.Map.ID == stage.MapID
}

func currentDungeonPendingStoryAdvance(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
) (worldmap.RoomCoordinate, int, bool) {
	if runtime == nil || !currentDungeonStoryStageMatches(runtime, scene, runtime.StoryStageIndex) {
		return worldmap.RoomCoordinate{}, -1, false
	}
	nextIndex := runtime.StoryStageIndex + 1
	if nextIndex < 0 || nextIndex >= len(runtime.StoryStages) {
		return worldmap.RoomCoordinate{}, nextIndex, false
	}
	next := runtime.StoryStages[nextIndex]
	if next.Index != nextIndex || next.MapID <= 0 ||
		absDungeonStoryCoordinateDelta(scene.Coordinate, next.Coordinate) != 1 {
		return worldmap.RoomCoordinate{}, nextIndex, false
	}
	return next.Coordinate, nextIndex, true
}

func currentDungeonStoryFinalStageMatches(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
) bool {
	if runtime == nil || len(runtime.StoryStages) == 0 {
		return true
	}
	finalIndex := len(runtime.StoryStages) - 1
	return runtime.StoryStageIndex == finalIndex &&
		currentDungeonStoryStageMatches(runtime, scene, finalIndex)
}

func absDungeonStoryCoordinateDelta(left, right worldmap.RoomCoordinate) int64 {
	dx := left.X - right.X
	if dx < 0 {
		dx = -dx
	}
	dy := left.Y - right.Y
	if dy < 0 {
		dy = -dy
	}
	return dx + dy
}
