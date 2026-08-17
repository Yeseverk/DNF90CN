package worldmap

import (
	"errors"
	"fmt"
)

var (
	ErrMapChoiceRequired = errors.New("dnf dungeon map choice is required")
	ErrMapChoiceRejected = errors.New("dnf dungeon map choice is not a candidate")
)

type DungeonMapChoice struct {
	DungeonID  int64            `json:"dungeon_id"`
	MazeIndex  int              `json:"maze_index"`
	Coordinate RoomCoordinate   `json:"coordinate"`
	FileType   MapFileType      `json:"file_type"`
	Source     ResolutionSource `json:"source"`
	Candidates []Map            `json:"candidates"`
}

type DungeonMapChooser func(choice DungeonMapChoice) (mapID int64, err error)

func BuildDungeonLayout(
	resolver *Resolver,
	dungeonID int64,
	mazeIndex int,
	chooser DungeonMapChooser,
) (*DungeonTopology, error) {
	topology, err := BuildDungeonTopology(resolver, dungeonID, mazeIndex)
	if err != nil {
		return nil, err
	}
	dungeonIndex := resolver.byDungeon[dungeonID]
	maze := resolver.table.dungeons[dungeonIndex.dungeonPos].Mazes[mazeIndex]

	for key, room := range topology.rooms {
		if room.Map != nil {
			continue
		}
		request := ResolveRequest{
			DungeonID: dungeonID, MazeIndex: mazeIndex,
			X: room.Coordinate.X, Y: room.Coordinate.Y,
		}
		candidates, candidateErr := resolver.ResolveCandidates(request)
		if candidateErr != nil && !errors.Is(candidateErr, ErrMapUnresolved) {
			return nil, candidateErr
		}
		fileType := expectedMapFileType(maze, key)
		source := ResolutionSource("")
		if len(candidates) > 0 {
			source = candidates[0].Source
		} else {
			pool, poolFileType, poolErr := dungeonLayoutPoolCandidates(
				resolver,
				dungeonID,
				mazeIndex,
				fileType,
				room,
			)
			if poolErr != nil {
				return nil, fmt.Errorf(
					"assign dungeon room map %s: coordinate_error=%v pool_error=%w",
					room.Coordinate, candidateErr, poolErr,
				)
			}
			source = ResolutionTypePool
			fileType = poolFileType
			candidates = make([]ResolvedMap, 0, len(pool))
			for _, mapValue := range pool {
				candidates = append(candidates, ResolvedMap{Map: mapValue, Source: source})
			}
		}

		choice := DungeonMapChoice{
			DungeonID: dungeonID, MazeIndex: mazeIndex, Coordinate: room.Coordinate,
			FileType: fileType, Source: source, Candidates: resolvedCandidateMaps(candidates),
		}
		selected, selectErr := selectDungeonMapCandidate(choice, chooser)
		if selectErr != nil {
			return nil, selectErr
		}
		room.Map = &selected
		room.ResolutionError = ""
		topology.rooms[key] = room
		delete(topology.resolutionErrors, key)
		topology.Diagnostics = removeRoomResolutionDiagnostic(topology.Diagnostics, room.Coordinate)
	}
	return topology, nil
}

func dungeonLayoutPoolCandidates(
	resolver *Resolver,
	dungeonID int64,
	mazeIndex int,
	fileType MapFileType,
	room DungeonRoom,
) ([]Map, MapFileType, error) {
	request := MapPoolRequest{DungeonID: dungeonID, MazeIndex: mazeIndex, FileType: fileType}
	pool, err := resolver.PoolCandidates(request)
	if err == nil {
		return pool, fileType, nil
	}
	for _, fallbackType := range dungeonLayoutFallbackPoolFileTypes(fileType, room) {
		fallbackRequest := MapPoolRequest{DungeonID: dungeonID, MazeIndex: mazeIndex, FileType: fallbackType}
		fallbackPool, fallbackErr := resolver.PoolCandidates(fallbackRequest)
		if fallbackErr == nil {
			return fallbackPool, fallbackType, nil
		}
	}
	return nil, fileType, err
}

func dungeonLayoutFallbackPoolFileTypes(fileType MapFileType, room DungeonRoom) []MapFileType {
	if fileType != MapFileBoss || !room.Start || !room.Boss {
		return nil
	}
	// Tower-style single-room floors declare the only room as both start and
	// boss in the .dgn, while the owned .map is a normal uncoordinated map
	// (for example towerofdespair_down/15130despair001.map). Prefer the real
	// dungeon-owned normal pool; accept a start pool as a narrow fallback for
	// the same start==boss shape.
	return []MapFileType{MapFileNormal, MapFileStart}
}

func selectDungeonMapCandidate(choice DungeonMapChoice, chooser DungeonMapChooser) (ResolvedMap, error) {
	if len(choice.Candidates) == 0 {
		return ResolvedMap{}, fmt.Errorf(
			"%w: dungeon=%d maze=%d room=%s file_type=%s",
			ErrMapPoolEmpty, choice.DungeonID, choice.MazeIndex, choice.Coordinate, choice.FileType,
		)
	}
	if len(choice.Candidates) == 1 && chooser == nil {
		return ResolvedMap{Map: cloneMap(choice.Candidates[0]), Source: choice.Source}, nil
	}
	if chooser == nil {
		return ResolvedMap{}, fmt.Errorf(
			"%w: dungeon=%d maze=%d room=%s file_type=%s candidates=%v",
			ErrMapChoiceRequired, choice.DungeonID, choice.MazeIndex, choice.Coordinate,
			choice.FileType, mapIDs(choice.Candidates),
		)
	}
	mapID, err := chooser(cloneDungeonMapChoice(choice))
	if err != nil {
		return ResolvedMap{}, err
	}
	for _, candidate := range choice.Candidates {
		if candidate.ID == mapID {
			return ResolvedMap{Map: cloneMap(candidate), Source: choice.Source}, nil
		}
	}
	return ResolvedMap{}, fmt.Errorf(
		"%w: dungeon=%d maze=%d room=%s map_id=%d candidates=%v",
		ErrMapChoiceRejected, choice.DungeonID, choice.MazeIndex, choice.Coordinate,
		mapID, mapIDs(choice.Candidates),
	)
}

func resolvedCandidateMaps(values []ResolvedMap) []Map {
	out := make([]Map, 0, len(values))
	for _, value := range values {
		out = append(out, cloneMap(value.Map))
	}
	return out
}

func cloneDungeonMapChoice(in DungeonMapChoice) DungeonMapChoice {
	candidates := in.Candidates
	in.Candidates = make([]Map, len(candidates))
	for index, candidate := range candidates {
		in.Candidates[index] = cloneMap(candidate)
	}
	return in
}

func mapIDs(values []Map) []int64 {
	out := make([]int64, len(values))
	for index, value := range values {
		out[index] = value.ID
	}
	return out
}

func removeRoomResolutionDiagnostic(values []TopologyDiagnostic, coordinate RoomCoordinate) []TopologyDiagnostic {
	out := values[:0]
	for _, diagnostic := range values {
		if diagnostic.Code == "room_map_unresolved" && diagnostic.Coordinate != nil &&
			diagnostic.Coordinate.X == coordinate.X && diagnostic.Coordinate.Y == coordinate.Y {
			continue
		}
		out = append(out, diagnostic)
	}
	return out
}
