package worldmap

import dnfpvf "longheng.io/server/internal/modules/dnf/pvf"

func cloneMap(in Map) Map {
	in.PlayerNumber = append([]int64(nil), in.PlayerNumber...)
	in.PVPStartArea = append([]int64(nil), in.PVPStartArea...)
	in.DungeonIDs = append([]int64(nil), in.DungeonIDs...)
	in.Tiles = append([]string(nil), in.Tiles...)
	in.TileFiles = append([]TileFile(nil), in.TileFiles...)
	in.TileMapRows = cloneInt64Rows(in.TileMapRows)
	in.BackgroundAnimations = append([]BackgroundAnimation(nil), in.BackgroundAnimations...)
	in.PathgatePositions = append([]Point(nil), in.PathgatePositions...)
	in.PathgateObjects = append([]int64(nil), in.PathgateObjects...)
	in.Portals = append([]Portal(nil), in.Portals...)
	for i := range in.Portals {
		if in.Portals[i].Position != nil {
			position := *in.Portals[i].Position
			in.Portals[i].Position = &position
		}
	}
	in.Sounds = append([]string(nil), in.Sounds...)
	in.AnimationObjects = append([]AnimationObject(nil), in.AnimationObjects...)
	in.PassiveObjects = append([]PassiveObject(nil), in.PassiveObjects...)
	in.SpecialPassiveObjects = append([]SpecialPassiveObject(nil), in.SpecialPassiveObjects...)
	for i := range in.SpecialPassiveObjects {
		in.SpecialPassiveObjects[i].Spawns = append([]SpecialObjectSpawn(nil), in.SpecialPassiveObjects[i].Spawns...)
		in.SpecialPassiveObjects[i].HellParty = append([]HellPartyEntry(nil), in.SpecialPassiveObjects[i].HellParty...)
	}
	in.Monsters = append([]MonsterSpawn(nil), in.Monsters...)
	in.EventMonsterPositions = append([]Point3(nil), in.EventMonsterPositions...)
	in.NPCs = append([]NPCSpawn(nil), in.NPCs...)
	for i := range in.NPCs {
		in.NPCs[i].Params = append([]int64(nil), in.NPCs[i].Params...)
	}
	in.AICharacters = append([]AICharacter(nil), in.AICharacters...)
	for i := range in.AICharacters {
		in.AICharacters[i].Params = append([]int64(nil), in.AICharacters[i].Params...)
	}
	in.TownMovableAreas = append([]MovableArea(nil), in.TownMovableAreas...)
	in.DungeonMovableAreas = append([]MovableArea(nil), in.DungeonMovableAreas...)
	in.Summons = append([]MapSummon(nil), in.Summons...)
	for i := range in.Summons {
		in.Summons[i].Position = append([]int64(nil), in.Summons[i].Position...)
		in.Summons[i].Info = cloneExtensions(in.Summons[i].Info)
		in.Summons[i].SourceSections = cloneSections(in.Summons[i].SourceSections)
		in.Summons[i].UnknownSections = cloneSections(in.Summons[i].UnknownSections)
		in.Summons[i].Extensions = cloneExtensions(in.Summons[i].Extensions)
	}
	in.DungeonStartArea = append([]int64(nil), in.DungeonStartArea...)
	in.ScreenPosition = append([]int64(nil), in.ScreenPosition...)
	in.MonsterTeam = append([]int64(nil), in.MonsterTeam...)
	in.PVPPracticeStartArea = append([]int64(nil), in.PVPPracticeStartArea...)
	in.VirtualMovableArea = append([]int64(nil), in.VirtualMovableArea...)
	in.OpeningBGM = append([]string(nil), in.OpeningBGM...)
	in.AbsoluteStartPath = append([]string(nil), in.AbsoluteStartPath...)
	in.SourceSections = cloneSections(in.SourceSections)
	in.OpaqueSections = cloneSections(in.OpaqueSections)
	in.UnknownSections = cloneSections(in.UnknownSections)
	in.Extensions = cloneExtensions(in.Extensions)
	in.Diagnostics = append([]Diagnostic(nil), in.Diagnostics...)
	return in
}

func cloneArea(in Area) Area {
	in.MapImage.Params = append([]int64(nil), in.MapImage.Params...)
	in.Dungeons = append([]DungeonEntry(nil), in.Dungeons...)
	in.HellQuestIDs = append([]int64(nil), in.HellQuestIDs...)
	in.HellFreePassItems = append([]TicketItem(nil), in.HellFreePassItems...)
	in.ItemConditions = append([]int64(nil), in.ItemConditions...)
	in.SourceSections = cloneSections(in.SourceSections)
	in.OpaqueSections = cloneSections(in.OpaqueSections)
	in.UnknownSections = cloneSections(in.UnknownSections)
	in.Extensions = cloneExtensions(in.Extensions)
	in.Diagnostics = append([]Diagnostic(nil), in.Diagnostics...)
	return in
}

func cloneDungeon(in Dungeon) Dungeon {
	in.Metadata.CutsceneImage.Params = append([]int64(nil), in.Metadata.CutsceneImage.Params...)
	in.Metadata.Champion = append([]int64(nil), in.Metadata.Champion...)
	in.Metadata.PathgateObjects = append([]int64(nil), in.Metadata.PathgateObjects...)
	in.Metadata.WorldMapPatternInfo = cloneSections(in.Metadata.WorldMapPatternInfo)
	in.Metadata.WorldMapInfo = cloneSections(in.Metadata.WorldMapInfo)
	in.Metadata.DifficultyLevels = append([]int64(nil), in.Metadata.DifficultyLevels...)
	in.Metadata.DesignatedDifficulties = append([]int64(nil), in.Metadata.DesignatedDifficulties...)
	in.Metadata.NamedMonsters = append([]int64(nil), in.Metadata.NamedMonsters...)
	in.Metadata.RecommendedLevels = append([]int64(nil), in.Metadata.RecommendedLevels...)
	in.Metadata.IntegerValues = cloneInt64Map(in.Metadata.IntegerValues)
	in.Metadata.NumberValues = cloneFloat64Map(in.Metadata.NumberValues)
	in.Metadata.Flags = cloneBoolMap(in.Metadata.Flags)
	if len(in.Metadata.TextValues) > 0 {
		textValues := make(map[string][]string, len(in.Metadata.TextValues))
		for key, value := range in.Metadata.TextValues {
			textValues[key] = append([]string(nil), value...)
		}
		in.Metadata.TextValues = textValues
	}
	in.Mazes = append([]Maze(nil), in.Mazes...)
	for i := range in.Mazes {
		in.Mazes[i] = cloneMaze(in.Mazes[i])
	}
	in.SourceSections = cloneSections(in.SourceSections)
	in.OpaqueSections = cloneSections(in.OpaqueSections)
	in.UnknownSections = cloneSections(in.UnknownSections)
	in.Extensions = cloneExtensions(in.Extensions)
	in.Diagnostics = append([]Diagnostic(nil), in.Diagnostics...)
	return in
}

func cloneMaze(in Maze) Maze {
	in.MapSpecifications = append([]MapSpecification(nil), in.MapSpecifications...)
	for i := range in.MapSpecifications {
		in.MapSpecifications[i].MapIDs = append([]int64(nil), in.MapSpecifications[i].MapIDs...)
	}
	if in.Start != nil {
		start := *in.Start
		start.Params = append([]int64(nil), start.Params...)
		in.Start = &start
	}
	if in.Boss != nil {
		boss := *in.Boss
		boss.Params = append([]int64(nil), boss.Params...)
		in.Boss = &boss
	}
	in.HitCount = append([]int64(nil), in.HitCount...)
	in.SealDoorPosition = append([]int64(nil), in.SealDoorPosition...)
	in.QuestConnection = append([]int64(nil), in.QuestConnection...)
	in.BossMapSpecifications = cloneSections(in.BossMapSpecifications)
	in.LayeredMapSpecifications = cloneSections(in.LayeredMapSpecifications)
	in.BossSpecifications = cloneMapSpecifications(in.BossSpecifications)
	in.LayeredSpecifications = cloneMapSpecifications(in.LayeredSpecifications)
	in.RandomizedObjects = append([]RandomizedObjectScript(nil), in.RandomizedObjects...)
	for index := range in.RandomizedObjects {
		in.RandomizedObjects[index] = cloneRandomizedObjectScript(in.RandomizedObjects[index])
	}
	in.ClearConditions = append([]ClearCondition(nil), in.ClearConditions...)
	in.SourceSections = cloneSections(in.SourceSections)
	in.OpaqueSections = cloneSections(in.OpaqueSections)
	in.UnknownSections = cloneSections(in.UnknownSections)
	in.Extensions = cloneExtensions(in.Extensions)
	in.Diagnostics = append([]Diagnostic(nil), in.Diagnostics...)
	return in
}

func cloneMapSpecifications(in []MapSpecification) []MapSpecification {
	if len(in) == 0 {
		return nil
	}
	out := append([]MapSpecification(nil), in...)
	for index := range out {
		out[index].MapIDs = append([]int64(nil), out[index].MapIDs...)
	}
	return out
}

func cloneRandomizedObjectScript(in RandomizedObjectScript) RandomizedObjectScript {
	in.Objects = append([]RidableObject(nil), in.Objects...)
	for index := range in.Objects {
		object := in.Objects[index]
		if object.MazeCoordinate != nil {
			point := *object.MazeCoordinate
			object.MazeCoordinate = &point
		}
		if object.Position != nil {
			point := *object.Position
			object.Position = &point
		}
		object.SourceSections = cloneSections(object.SourceSections)
		object.UnknownSections = cloneSections(object.UnknownSections)
		object.Extensions = cloneExtensions(object.Extensions)
		in.Objects[index] = object
	}
	in.SourceSections = cloneSections(in.SourceSections)
	in.UnknownSections = cloneSections(in.UnknownSections)
	in.Extensions = cloneExtensions(in.Extensions)
	return in
}

func cloneExtensions(in []ExtensionSection) []ExtensionSection {
	if len(in) == 0 {
		return nil
	}
	out := make([]ExtensionSection, len(in))
	for i, extension := range in {
		out[i] = extension
		out[i].Integers = append([]int64(nil), extension.Integers...)
		out[i].Numbers = append([]float64(nil), extension.Numbers...)
		out[i].Texts = append([]string(nil), extension.Texts...)
		out[i].Symbols = append([]string(nil), extension.Symbols...)
		out[i].Tokens = append([]dnfpvf.Token(nil), extension.Tokens...)
		out[i].Scope = append([]string(nil), extension.Scope...)
	}
	return out
}

func cloneInt64Map(in map[string]int64) map[string]int64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneFloat64Map(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneSections(in []RawSection) []RawSection {
	if len(in) == 0 {
		return nil
	}
	out := make([]RawSection, len(in))
	for i, section := range in {
		out[i] = section
		out[i].Tokens = append([]dnfpvf.Token(nil), section.Tokens...)
		out[i].Scope = append([]string(nil), section.Scope...)
	}
	return out
}

func cloneInt64Rows(in [][]int64) [][]int64 {
	if len(in) == 0 {
		return nil
	}
	out := make([][]int64, len(in))
	for i := range in {
		out[i] = append([]int64(nil), in[i]...)
	}
	return out
}
