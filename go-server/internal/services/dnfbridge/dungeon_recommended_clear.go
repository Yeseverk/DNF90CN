package dnfbridge

func currentDungeonMatchesRecommendedLevel(runtime *runtimeDungeonState) bool {
	if runtime == nil || runtime.Dungeon.ID <= 0 || runtime.Character.Level <= 0 {
		return false
	}
	levels := runtime.Dungeon.Metadata.RecommendedLevels
	if len(levels) < 2 || levels[0] <= 0 || levels[1] < levels[0] {
		return false
	}
	level := int64(runtime.Character.Level)
	return level >= levels[0] && level <= levels[1]
}

func currentDungeonIsRecommendedClear(runtime *runtimeDungeonState) bool {
	return runtime != nil &&
		runtime.ordinaryFinalRoomClearAccepted &&
		currentDungeonMatchesRecommendedLevel(runtime)
}
