package worldmap

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

var (
	ErrResolverTableRequired  = errors.New("dnf worldmap resolver table is required")
	ErrDungeonNotIndexed      = errors.New("dnf dungeon is not indexed")
	ErrMazeNotFound           = errors.New("dnf dungeon maze is not found")
	ErrMapReferenceMissing    = errors.New("dnf dungeon map reference is missing")
	ErrMapUnresolved          = errors.New("dnf dungeon map is unresolved")
	ErrMapAmbiguous           = errors.New("dnf dungeon map is ambiguous")
	ErrMapPoolEmpty           = errors.New("dnf dungeon map pool is empty")
	ErrMapPoolChoiceInvalid   = errors.New("dnf dungeon map pool choice is invalid")
	ErrLayeredSpecMissing     = errors.New("dnf dungeon layered map specification is missing")
	ErrLayeredSpecAmbiguous   = errors.New("dnf dungeon layered map specification is ambiguous")
	ErrLayeredIndexInvalid    = errors.New("dnf dungeon layered map index is invalid")
	ErrStoryStageMissing      = errors.New("dnf dungeon story stage is missing")
	ErrStoryStageIndexInvalid = errors.New("dnf dungeon story stage index is invalid")
)

type MapFileType string

const (
	MapFileNormal  MapFileType = "normal"
	MapFileStart   MapFileType = "start"
	MapFileBoss    MapFileType = "boss"
	MapFileNamed   MapFileType = "named"
	MapFileEnd     MapFileType = "end"
	MapFileHidden  MapFileType = "hidden"
	MapFileQuest   MapFileType = "quest"
	MapFileDefault MapFileType = "default"
)

type ResolutionSource string

const (
	ResolutionMapSpecification ResolutionSource = "map_specification"
	ResolutionDungeonOwnership ResolutionSource = "dungeon_ownership"
	ResolutionDirectoryPath    ResolutionSource = "directory_coordinate"
	ResolutionTypePool         ResolutionSource = "type_pool"
)

type ResolveRequest struct {
	DungeonID int64 `json:"dungeon_id"`
	MazeIndex int   `json:"maze_index"`
	X         int64 `json:"x"`
	Y         int64 `json:"y"`
}

type ResolvedMap struct {
	Map               Map              `json:"map"`
	Source            ResolutionSource `json:"source"`
	SpecificationType string           `json:"specification_type,omitempty"`
}

// DungeonStoryStage is one client-side PVF map-candidate advance in an
// activity story maze. Index and map order are derived from the repeated
// [boss map] coordinate sequence and the matching [map specification] rows.
type DungeonStoryStage struct {
	Index             int            `json:"index"`
	Coordinate        RoomCoordinate `json:"coordinate"`
	MapID             int64          `json:"map_id"`
	SpecificationType string         `json:"specification_type"`
}

type DungeonMapIndexSnapshot struct {
	DungeonID         int64 `json:"dungeon_id"`
	Mazes             int   `json:"mazes"`
	Specifications    int   `json:"specifications"`
	OwnershipEntries  int   `json:"ownership_entries"`
	CoordinateEntries int   `json:"coordinate_entries"`
	PathEntries       int   `json:"path_entries"`
}

type Resolver struct {
	table     *Table
	byDungeon map[int64]dungeonMapIndex
}

type coordinateKey struct {
	x int64
	y int64
}

type indexedMap struct {
	mapPos            int
	fileType          MapFileType
	specificationType string
}

type mazeMapIndex struct {
	bySpecification map[coordinateKey][]indexedMap
	byOwnership     map[coordinateKey][]indexedMap
	byCoordinate    map[coordinateKey][]indexedMap
	byPool          map[MapFileType][]indexedMap
	byPath          map[string]indexedMap
	layeredByRoom   map[coordinateKey]map[int]struct{}
	layeredMaps     map[int]struct{}
	storyStages     []indexedStoryStage
	storyMapsByRoom map[coordinateKey]map[int]struct{}
	specifications  int
}

type indexedStoryStage struct {
	coordinate        coordinateKey
	mapPos            int
	specificationType string
}

type dungeonMapIndex struct {
	dungeonPos int
	mazes      []mazeMapIndex
}

var mapCoordinatePattern = regexp.MustCompile(`\((-?\d+)[,.](-?\d+)\)`)

func NewResolver(table *Table) (*Resolver, error) {
	if table == nil {
		return nil, ErrResolverTableRequired
	}
	resolver := &Resolver{table: table, byDungeon: make(map[int64]dungeonMapIndex, len(table.dungeons))}
	mapsByDirectory := indexMapsByDirectory(table.maps)
	mapsByDungeon := indexMapsByDungeon(table.maps)
	for dungeonPos, dungeon := range table.dungeons {
		if _, exists := resolver.byDungeon[dungeon.ID]; exists {
			continue
		}
		index := dungeonMapIndex{dungeonPos: dungeonPos, mazes: make([]mazeMapIndex, len(dungeon.Mazes))}
		for mazePos, maze := range dungeon.Mazes {
			mazeIndex, err := buildMazeMapIndex(table, dungeon, maze, mapsByDirectory, mapsByDungeon[dungeon.ID])
			if err != nil {
				return nil, err
			}
			index.mazes[mazePos] = mazeIndex
		}
		resolver.byDungeon[dungeon.ID] = index
	}
	return resolver, nil
}

func buildMazeMapIndex(
	table *Table,
	dungeon Dungeon,
	maze Maze,
	mapsByDirectory map[string][]int,
	ownedMaps []int,
) (mazeMapIndex, error) {
	index := mazeMapIndex{
		bySpecification: make(map[coordinateKey][]indexedMap),
		byOwnership:     make(map[coordinateKey][]indexedMap),
		byCoordinate:    make(map[coordinateKey][]indexedMap),
		byPool:          make(map[MapFileType][]indexedMap),
		byPath:          make(map[string]indexedMap),
		layeredByRoom:   make(map[coordinateKey]map[int]struct{}),
		layeredMaps:     make(map[int]struct{}),
		storyMapsByRoom: make(map[coordinateKey]map[int]struct{}),
	}
	if err := buildMazeStoryStageIndex(table, dungeon, maze, &index); err != nil {
		return mazeMapIndex{}, err
	}
	directories := make(map[string]struct{})
	for _, specification := range maze.MapSpecifications {
		if isLayeredSpecification(specification) {
			if err := addLayeredSpecificationToMazeIndex(table, dungeon, maze, specification, &index, directories); err != nil {
				return mazeMapIndex{}, err
			}
			continue
		}
		if err := addSpecificationToMazeIndex(table, dungeon, maze, specification, &index, directories); err != nil {
			return mazeMapIndex{}, err
		}
	}
	for _, specification := range maze.BossSpecifications {
		if isLayeredSpecification(specification) {
			if err := addLayeredSpecificationToMazeIndex(table, dungeon, maze, specification, &index, directories); err != nil {
				return mazeMapIndex{}, err
			}
			continue
		}
		if err := addSpecificationToMazeIndex(table, dungeon, maze, specification, &index, directories); err != nil {
			return mazeMapIndex{}, err
		}
	}
	for _, specification := range maze.LayeredSpecifications {
		if err := addLayeredSpecificationToMazeIndex(table, dungeon, maze, specification, &index, directories); err != nil {
			return mazeMapIndex{}, err
		}
	}
	filterStoryStageSpecificationCandidates(&index)
	for directory := range directories {
		for _, mapPos := range mapsByDirectory[directory] {
			mapValue := table.maps[mapPos]
			ref := indexedMap{mapPos: mapPos, fileType: ClassifyMapFileType(mapValue.Path)}
			index.byPath[pathKey(mapValue.Path)] = ref
			x, y, ok := ParseMapFileCoordinate(mapValue.Path)
			if !ok {
				continue
			}
			key := coordinateKey{x: x, y: y}
			if index.isLayeredMapAt(key, mapPos) {
				continue
			}
			if index.isStoryMapAt(key, mapPos) {
				continue
			}
			index.byCoordinate[key] = appendUniqueIndexedMap(index.byCoordinate[key], ref)
		}
	}
	for _, mapPos := range ownedMaps {
		mapValue := table.maps[mapPos]
		ref := indexedMap{mapPos: mapPos, fileType: ClassifyMapFileType(mapValue.Path)}
		index.byPath[pathKey(mapValue.Path)] = ref
		x, y, ok := ParseMapFileCoordinate(mapValue.Path)
		if !ok {
			if _, layered := index.layeredMaps[mapPos]; layered {
				continue
			}
			index.byPool[ref.fileType] = appendUniqueIndexedMap(index.byPool[ref.fileType], ref)
			continue
		}
		key := coordinateKey{x: x, y: y}
		if index.isLayeredMapAt(key, mapPos) {
			continue
		}
		if index.isStoryMapAt(key, mapPos) {
			continue
		}
		index.byOwnership[key] = appendUniqueIndexedMap(index.byOwnership[key], ref)
	}
	return index, nil
}

func buildMazeStoryStageIndex(
	table *Table,
	dungeon Dungeon,
	maze Maze,
	index *mazeMapIndex,
) error {
	coordinates := mazeBossCoordinates(maze.Boss)
	if len(coordinates) < 2 || !hasRepeatedRoomCoordinate(coordinates) || !mazeQuestConnected(maze) {
		return nil
	}
	specificationsByRoom := make(map[coordinateKey][]MapSpecification)
	appendSpecifications := func(specifications []MapSpecification) {
		for _, specification := range specifications {
			if isLayeredSpecification(specification) || len(specification.MapIDs) == 0 {
				continue
			}
			key := coordinateKey{x: specification.Coordinate.X, y: specification.Coordinate.Y}
			specificationsByRoom[key] = append(specificationsByRoom[key], specification)
		}
	}
	appendSpecifications(maze.MapSpecifications)
	appendSpecifications(maze.BossSpecifications)
	used := make(map[coordinateKey]int)
	stages := make([]indexedStoryStage, 0, len(coordinates))
	for _, coordinate := range coordinates {
		key := coordinateKey{x: coordinate.X, y: coordinate.Y}
		position := used[key]
		specifications := specificationsByRoom[key]
		if position >= len(specifications) {
			return nil
		}
		specification := specifications[position]
		used[key] = position + 1
		mapID := specification.MapIDs[0]
		mapPos, ok := table.mapByID[mapID]
		if !ok {
			return fmt.Errorf(
				"%w: dungeon=%d maze=%d story_stage=%d coordinate=(%d,%d) map_id=%d",
				ErrMapReferenceMissing, dungeon.ID, maze.Index, len(stages), key.x, key.y, mapID,
			)
		}
		stages = append(stages, indexedStoryStage{
			coordinate: key, mapPos: mapPos, specificationType: specification.Type,
		})
	}
	for _, stage := range stages {
		if index.storyMapsByRoom[stage.coordinate] == nil {
			index.storyMapsByRoom[stage.coordinate] = make(map[int]struct{})
		}
		index.storyMapsByRoom[stage.coordinate][stage.mapPos] = struct{}{}
	}
	index.storyStages = stages
	return nil
}

func hasRepeatedRoomCoordinate(coordinates []RoomCoordinate) bool {
	seen := make(map[RoomCoordinate]struct{}, len(coordinates))
	for _, coordinate := range coordinates {
		if _, ok := seen[coordinate]; ok {
			return true
		}
		seen[coordinate] = struct{}{}
	}
	return false
}

func filterStoryStageSpecificationCandidates(index *mazeMapIndex) {
	if index == nil || len(index.storyStages) == 0 {
		return
	}
	firstStageByRoom := make(map[coordinateKey]int)
	for _, stage := range index.storyStages {
		if _, exists := firstStageByRoom[stage.coordinate]; !exists {
			firstStageByRoom[stage.coordinate] = stage.mapPos
		}
	}
	for key, storyMaps := range index.storyMapsByRoom {
		candidates := index.bySpecification[key]
		ordinary := make([]indexedMap, 0, len(candidates))
		for _, candidate := range candidates {
			if _, story := storyMaps[candidate.mapPos]; !story {
				ordinary = appendUniqueIndexedMap(ordinary, candidate)
			}
		}
		if len(ordinary) != 0 {
			index.bySpecification[key] = ordinary
			continue
		}
		fallbackMapPos, ok := firstStageByRoom[key]
		if !ok {
			continue
		}
		for _, candidate := range candidates {
			if candidate.mapPos == fallbackMapPos {
				index.bySpecification[key] = []indexedMap{candidate}
				break
			}
		}
	}
}

func isLayeredSpecification(specification MapSpecification) bool {
	return sectionKey(strings.Trim(specification.Type, "[]")) == "layered"
}

func addLayeredSpecificationToMazeIndex(
	table *Table,
	dungeon Dungeon,
	maze Maze,
	specification MapSpecification,
	index *mazeMapIndex,
	directories map[string]struct{},
) error {
	key := coordinateKey{x: specification.Coordinate.X, y: specification.Coordinate.Y}
	index.specifications++
	if index.layeredByRoom[key] == nil {
		index.layeredByRoom[key] = make(map[int]struct{})
	}
	for _, mapID := range specification.MapIDs {
		mapPos, ok := table.mapByID[mapID]
		if !ok {
			return fmt.Errorf(
				"%w: dungeon=%d maze=%d coordinate=(%d,%d) map_id=%d",
				ErrMapReferenceMissing, dungeon.ID, maze.Index, key.x, key.y, mapID,
			)
		}
		index.layeredByRoom[key][mapPos] = struct{}{}
		index.layeredMaps[mapPos] = struct{}{}
		directory := pathKey(path.Dir(strings.ReplaceAll(table.maps[mapPos].Path, "\\", "/")))
		if directory != "" && directory != "." {
			directories[directory] = struct{}{}
		}
	}
	return nil
}

func (index mazeMapIndex) isLayeredMapAt(key coordinateKey, mapPos int) bool {
	_, ok := index.layeredByRoom[key][mapPos]
	return ok
}

func (index mazeMapIndex) isStoryMapAt(key coordinateKey, mapPos int) bool {
	_, ok := index.storyMapsByRoom[key][mapPos]
	return ok
}

func indexMapsByDirectory(maps []Map) map[string][]int {
	out := make(map[string][]int)
	for mapPos, mapValue := range maps {
		directory := pathKey(path.Dir(strings.ReplaceAll(mapValue.Path, "\\", "/")))
		out[directory] = append(out[directory], mapPos)
	}
	return out
}

func indexMapsByDungeon(maps []Map) map[int64][]int {
	out := make(map[int64][]int)
	for mapPos, mapValue := range maps {
		for _, dungeonID := range mapValue.DungeonIDs {
			out[dungeonID] = append(out[dungeonID], mapPos)
		}
	}
	return out
}

func addSpecificationToMazeIndex(
	table *Table,
	dungeon Dungeon,
	maze Maze,
	specification MapSpecification,
	index *mazeMapIndex,
	directories map[string]struct{},
) error {
	key := coordinateKey{x: specification.Coordinate.X, y: specification.Coordinate.Y}
	index.specifications++
	for _, mapID := range specification.MapIDs {
		mapPos, ok := table.mapByID[mapID]
		if !ok {
			return fmt.Errorf(
				"%w: dungeon=%d maze=%d coordinate=(%d,%d) map_id=%d",
				ErrMapReferenceMissing, dungeon.ID, maze.Index, key.x, key.y, mapID,
			)
		}
		mapValue := table.maps[mapPos]
		ref := indexedMap{
			mapPos: mapPos, fileType: specificationFileType(specification.Type),
			specificationType: specification.Type,
		}
		index.bySpecification[key] = appendUniqueIndexedMap(index.bySpecification[key], ref)
		index.byPath[pathKey(mapValue.Path)] = ref
		directory := pathKey(path.Dir(strings.ReplaceAll(mapValue.Path, "\\", "/")))
		if directory != "" && directory != "." {
			directories[directory] = struct{}{}
		}
	}
	return nil
}

func (r *Resolver) Resolve(request ResolveRequest) (ResolvedMap, error) {
	candidates, source, err := r.resolveCandidateSet(request)
	if err != nil {
		return ResolvedMap{}, err
	}
	return r.resolveCandidates(request, candidates, source)
}

// ResolveLayered resolves exactly one PVF-declared layered map at a maze
// coordinate. layerIndex is positional in the specification's MapIDs list;
// directory ownership and filename-coordinate fallbacks are deliberately not
// consulted. Both inline `layered` rows in [map specification] and typed
// [layered map specification] rows are explicit PVF declarations.
func (r *Resolver) ResolveLayered(
	dungeonID int64,
	mazeIndex int,
	coordinate RoomCoordinate,
	layerIndex int,
) (ResolvedMap, error) {
	if r == nil || r.table == nil {
		return ResolvedMap{}, ErrResolverTableRequired
	}
	dungeonIndex, ok := r.byDungeon[dungeonID]
	if !ok {
		return ResolvedMap{}, fmt.Errorf("%w: dungeon=%d", ErrDungeonNotIndexed, dungeonID)
	}
	if mazeIndex < 0 || mazeIndex >= len(dungeonIndex.mazes) {
		return ResolvedMap{}, fmt.Errorf("%w: dungeon=%d maze=%d", ErrMazeNotFound, dungeonID, mazeIndex)
	}
	dungeon := r.table.dungeons[dungeonIndex.dungeonPos]
	maze := dungeon.Mazes[mazeIndex]
	specifications := explicitLayeredSpecificationsAt(maze, coordinate)
	if len(specifications) == 0 {
		return ResolvedMap{}, fmt.Errorf(
			"%w: dungeon=%d maze=%d coordinate=%s",
			ErrLayeredSpecMissing,
			dungeonID,
			mazeIndex,
			coordinate,
		)
	}
	if len(specifications) != 1 {
		return ResolvedMap{}, fmt.Errorf(
			"%w: dungeon=%d maze=%d coordinate=%s specifications=%d",
			ErrLayeredSpecAmbiguous,
			dungeonID,
			mazeIndex,
			coordinate,
			len(specifications),
		)
	}
	specification := specifications[0]
	if layerIndex < 0 || layerIndex >= len(specification.MapIDs) {
		return ResolvedMap{}, fmt.Errorf(
			"%w: dungeon=%d maze=%d coordinate=%s layer=%d count=%d",
			ErrLayeredIndexInvalid,
			dungeonID,
			mazeIndex,
			coordinate,
			layerIndex,
			len(specification.MapIDs),
		)
	}
	mapID := specification.MapIDs[layerIndex]
	mapPos, ok := r.table.mapByID[mapID]
	if !ok {
		return ResolvedMap{}, fmt.Errorf(
			"%w: dungeon=%d maze=%d coordinate=%s layer=%d map_id=%d",
			ErrMapReferenceMissing,
			dungeonID,
			mazeIndex,
			coordinate,
			layerIndex,
			mapID,
		)
	}
	return ResolvedMap{
		Map:               cloneMap(r.table.maps[mapPos]),
		Source:            ResolutionMapSpecification,
		SpecificationType: "layered",
	}, nil
}

// StoryStages returns the ordered activity-story candidate chain declared by
// the active maze. A maze is treated as staged only when its quest-connected
// [boss map] sequence repeats a coordinate and every occurrence can be paired
// with a matching non-layered map specification in declaration order.
func (r *Resolver) StoryStages(dungeonID int64, mazeIndex int) ([]DungeonStoryStage, error) {
	index, err := r.mazeIndex(dungeonID, mazeIndex)
	if err != nil {
		return nil, err
	}
	if len(index.storyStages) == 0 {
		return nil, fmt.Errorf("%w: dungeon=%d maze=%d", ErrStoryStageMissing, dungeonID, mazeIndex)
	}
	out := make([]DungeonStoryStage, len(index.storyStages))
	for stageIndex, stage := range index.storyStages {
		out[stageIndex] = DungeonStoryStage{
			Index:             stageIndex,
			Coordinate:        RoomCoordinate{X: stage.coordinate.x, Y: stage.coordinate.y},
			MapID:             r.table.maps[stage.mapPos].ID,
			SpecificationType: stage.specificationType,
		}
	}
	return out, nil
}

func (r *Resolver) ResolveStoryStage(dungeonID int64, mazeIndex int, stageIndex int) (ResolvedMap, DungeonStoryStage, error) {
	index, err := r.mazeIndex(dungeonID, mazeIndex)
	if err != nil {
		return ResolvedMap{}, DungeonStoryStage{}, err
	}
	if len(index.storyStages) == 0 {
		return ResolvedMap{}, DungeonStoryStage{}, fmt.Errorf(
			"%w: dungeon=%d maze=%d", ErrStoryStageMissing, dungeonID, mazeIndex,
		)
	}
	if stageIndex < 0 || stageIndex >= len(index.storyStages) {
		return ResolvedMap{}, DungeonStoryStage{}, fmt.Errorf(
			"%w: dungeon=%d maze=%d stage=%d count=%d",
			ErrStoryStageIndexInvalid, dungeonID, mazeIndex, stageIndex, len(index.storyStages),
		)
	}
	stage := index.storyStages[stageIndex]
	descriptor := DungeonStoryStage{
		Index:             stageIndex,
		Coordinate:        RoomCoordinate{X: stage.coordinate.x, Y: stage.coordinate.y},
		MapID:             r.table.maps[stage.mapPos].ID,
		SpecificationType: stage.specificationType,
	}
	return ResolvedMap{
		Map:               cloneMap(r.table.maps[stage.mapPos]),
		Source:            ResolutionMapSpecification,
		SpecificationType: stage.specificationType,
	}, descriptor, nil
}

func explicitLayeredSpecificationsAt(maze Maze, coordinate RoomCoordinate) []MapSpecification {
	specifications := make([]MapSpecification, 0, 1)
	appendMatching := func(values []MapSpecification, requireLayeredType bool) {
		for _, specification := range values {
			if requireLayeredType && sectionKey(strings.Trim(specification.Type, "[]")) != "layered" {
				continue
			}
			if specification.Coordinate.X != coordinate.X || specification.Coordinate.Y != coordinate.Y {
				continue
			}
			specifications = append(specifications, specification)
		}
	}
	appendMatching(maze.MapSpecifications, true)
	appendMatching(maze.LayeredSpecifications, false)
	return specifications
}

func (r *Resolver) ResolveCandidates(request ResolveRequest) ([]ResolvedMap, error) {
	candidates, source, err := r.resolveCandidateSet(request)
	if err != nil {
		return nil, err
	}
	candidates = uniqueIndexedMaps(candidates)
	out := make([]ResolvedMap, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, ResolvedMap{
			Map:               cloneMap(r.table.maps[candidate.mapPos]),
			Source:            source,
			SpecificationType: candidate.specificationType,
		})
	}
	return out, nil
}

func (r *Resolver) resolveCandidateSet(request ResolveRequest) ([]indexedMap, ResolutionSource, error) {
	if r == nil || r.table == nil {
		return nil, "", ErrResolverTableRequired
	}
	dungeonIndex, ok := r.byDungeon[request.DungeonID]
	if !ok {
		return nil, "", fmt.Errorf("%w: dungeon=%d", ErrDungeonNotIndexed, request.DungeonID)
	}
	if request.MazeIndex < 0 || request.MazeIndex >= len(dungeonIndex.mazes) {
		return nil, "", fmt.Errorf("%w: dungeon=%d maze=%d", ErrMazeNotFound, request.DungeonID, request.MazeIndex)
	}
	dungeon := r.table.dungeons[dungeonIndex.dungeonPos]
	maze := dungeon.Mazes[request.MazeIndex]
	index := dungeonIndex.mazes[request.MazeIndex]
	key := coordinateKey{x: request.X, y: request.Y}
	expectedType := expectedMapFileType(maze, key)

	if !mazeQuestConnected(maze) && (expectedType == MapFileStart || expectedType == MapFileBoss) {
		if candidates := exactFileTypeCandidates(index.byOwnership[key], expectedType); len(candidates) > 0 {
			return candidates, ResolutionDungeonOwnership, nil
		}
		if candidates := exactFileTypeCandidates(index.byCoordinate[key], expectedType); len(candidates) > 0 {
			return candidates, ResolutionDirectoryPath, nil
		}
	}
	if candidates := selectSpecificationCandidates(index.bySpecification[key], maze, key); len(candidates) > 0 {
		return candidates, ResolutionMapSpecification, nil
	}
	if candidates := exactFileTypeCandidates(index.byOwnership[key], expectedType); len(candidates) > 0 {
		return candidates, ResolutionDungeonOwnership, nil
	}
	var exact []indexedMap
	for _, candidate := range index.byCoordinate[key] {
		if candidate.fileType == expectedType {
			exact = appendUniqueIndexedMap(exact, candidate)
		}
	}
	if len(exact) > 0 {
		return exact, ResolutionDirectoryPath, nil
	}
	start, boss := mazeRoomRoles(maze, key)
	for _, fallbackType := range startBossCoordinateFallbackFileTypes(expectedType, start, boss) {
		if candidates := exactFileTypeCandidates(index.byOwnership[key], fallbackType); len(candidates) > 0 {
			return candidates, ResolutionDungeonOwnership, nil
		}
		if candidates := exactFileTypeCandidates(index.byCoordinate[key], fallbackType); len(candidates) > 0 {
			return candidates, ResolutionDirectoryPath, nil
		}
	}
	return nil, "", fmt.Errorf(
		"%w: dungeon=%d maze=%d coordinate=(%d,%d) expected_type=%s",
		ErrMapUnresolved, request.DungeonID, request.MazeIndex, request.X, request.Y, expectedType,
	)
}

type MapPoolRequest struct {
	DungeonID int64       `json:"dungeon_id"`
	MazeIndex int         `json:"maze_index"`
	FileType  MapFileType `json:"file_type"`
}

func (r *Resolver) PoolCandidates(request MapPoolRequest) ([]Map, error) {
	index, err := r.mazeIndex(request.DungeonID, request.MazeIndex)
	if err != nil {
		return nil, err
	}
	refs := uniqueIndexedMaps(index.byPool[request.FileType])
	if len(refs) == 0 {
		return nil, fmt.Errorf(
			"%w: dungeon=%d maze=%d file_type=%s",
			ErrMapPoolEmpty, request.DungeonID, request.MazeIndex, request.FileType,
		)
	}
	out := make([]Map, 0, len(refs))
	for _, ref := range refs {
		out = append(out, cloneMap(r.table.maps[ref.mapPos]))
	}
	return out, nil
}

func (r *Resolver) ResolvePoolChoice(request MapPoolRequest, mapID int64) (ResolvedMap, error) {
	candidates, err := r.PoolCandidates(request)
	if err != nil {
		return ResolvedMap{}, err
	}
	for _, candidate := range candidates {
		if candidate.ID == mapID {
			return ResolvedMap{Map: candidate, Source: ResolutionTypePool}, nil
		}
	}
	return ResolvedMap{}, fmt.Errorf(
		"%w: dungeon=%d maze=%d file_type=%s map_id=%d",
		ErrMapPoolChoiceInvalid, request.DungeonID, request.MazeIndex, request.FileType, mapID,
	)
}

func (r *Resolver) mazeIndex(dungeonID int64, mazeIndex int) (mazeMapIndex, error) {
	if r == nil || r.table == nil {
		return mazeMapIndex{}, ErrResolverTableRequired
	}
	dungeonIndex, ok := r.byDungeon[dungeonID]
	if !ok {
		return mazeMapIndex{}, fmt.Errorf("%w: dungeon=%d", ErrDungeonNotIndexed, dungeonID)
	}
	if mazeIndex < 0 || mazeIndex >= len(dungeonIndex.mazes) {
		return mazeMapIndex{}, fmt.Errorf("%w: dungeon=%d maze=%d", ErrMazeNotFound, dungeonID, mazeIndex)
	}
	return dungeonIndex.mazes[mazeIndex], nil
}

func exactFileTypeCandidates(candidates []indexedMap, expectedType MapFileType) []indexedMap {
	var exact []indexedMap
	for _, candidate := range candidates {
		if candidate.fileType == expectedType {
			exact = appendUniqueIndexedMap(exact, candidate)
		}
	}
	return exact
}

func (r *Resolver) TryResolve(request ResolveRequest) (ResolvedMap, bool) {
	resolved, err := r.Resolve(request)
	return resolved, err == nil
}

func (r *Resolver) ResolvePath(dungeonID int64, mazeIndex int, mapPath string) (Map, bool) {
	if r == nil || r.table == nil {
		return Map{}, false
	}
	dungeonIndex, ok := r.byDungeon[dungeonID]
	if !ok || mazeIndex < 0 || mazeIndex >= len(dungeonIndex.mazes) {
		return Map{}, false
	}
	ref, ok := dungeonIndex.mazes[mazeIndex].byPath[pathKey(mapPath)]
	if !ok {
		return Map{}, false
	}
	return cloneMap(r.table.maps[ref.mapPos]), true
}

func (r *Resolver) Snapshot(dungeonID int64) (DungeonMapIndexSnapshot, bool) {
	if r == nil {
		return DungeonMapIndexSnapshot{}, false
	}
	index, ok := r.byDungeon[dungeonID]
	if !ok {
		return DungeonMapIndexSnapshot{}, false
	}
	snapshot := DungeonMapIndexSnapshot{DungeonID: dungeonID, Mazes: len(index.mazes)}
	for _, maze := range index.mazes {
		snapshot.Specifications += maze.specifications
		for _, entries := range maze.byOwnership {
			snapshot.OwnershipEntries += len(entries)
		}
		for _, entries := range maze.byCoordinate {
			snapshot.CoordinateEntries += len(entries)
		}
		snapshot.PathEntries += len(maze.byPath)
	}
	return snapshot, true
}

func (r *Resolver) resolveCandidates(request ResolveRequest, candidates []indexedMap, source ResolutionSource) (ResolvedMap, error) {
	candidates = uniqueIndexedMaps(candidates)
	if len(candidates) != 1 {
		ids := make([]int64, 0, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, r.table.maps[candidate.mapPos].ID)
		}
		return ResolvedMap{}, fmt.Errorf(
			"%w: dungeon=%d maze=%d coordinate=(%d,%d) map_ids=%v",
			ErrMapAmbiguous, request.DungeonID, request.MazeIndex, request.X, request.Y, ids,
		)
	}
	candidate := candidates[0]
	return ResolvedMap{
		Map: cloneMap(r.table.maps[candidate.mapPos]), Source: source,
		SpecificationType: candidate.specificationType,
	}, nil
}

func selectSpecificationCandidates(candidates []indexedMap, maze Maze, key coordinateKey) []indexedMap {
	if len(candidates) == 0 {
		return nil
	}
	expected := expectedMapFileType(maze, key)
	var typed []indexedMap
	for _, candidate := range candidates {
		if candidate.fileType == expected {
			typed = appendUniqueIndexedMap(typed, candidate)
		}
	}
	if len(typed) > 0 {
		return typed
	}
	return uniqueIndexedMaps(candidates)
}

func expectedMapFileType(maze Maze, key coordinateKey) MapFileType {
	if maze.Boss != nil && maze.Boss.X == key.x && maze.Boss.Y == key.y {
		return MapFileBoss
	}
	if maze.Start != nil && maze.Start.X == key.x && maze.Start.Y == key.y {
		return MapFileStart
	}
	if mazeQuestConnected(maze) {
		return MapFileQuest
	}
	return MapFileNormal
}

func mazeRoomRoles(maze Maze, key coordinateKey) (start bool, boss bool) {
	start = maze.Start != nil && maze.Start.X == key.x && maze.Start.Y == key.y
	for _, coordinate := range mazeBossCoordinates(maze.Boss) {
		if coordinate.X == key.x && coordinate.Y == key.y {
			boss = true
			break
		}
	}
	return start, boss
}

func startBossCoordinateFallbackFileTypes(fileType MapFileType, start bool, boss bool) []MapFileType {
	if fileType != MapFileBoss || !start || !boss {
		return nil
	}
	// The production training-room PVF marks its only coordinate as both
	// start and boss while the only coordinate-owned map is explicitly named
	// start. Exact boss/specification matches have already won before this
	// narrow compatibility fallback.
	return []MapFileType{MapFileStart}
}

func mazeQuestConnected(maze Maze) bool {
	return len(maze.QuestConnection) >= 2
}

func specificationFileType(value string) MapFileType {
	switch sectionKey(strings.Trim(value, "[]")) {
	case "start":
		return MapFileStart
	case "boss":
		return MapFileBoss
	case "named":
		return MapFileNamed
	case "end":
		return MapFileEnd
	case "hidden":
		return MapFileHidden
	case "quest":
		return MapFileQuest
	case "default":
		return MapFileDefault
	default:
		return MapFileNormal
	}
}

func ParseMapFileCoordinate(mapPath string) (int64, int64, bool) {
	stem := strings.TrimSuffix(path.Base(strings.ReplaceAll(mapPath, "\\", "/")), path.Ext(mapPath))
	match := mapCoordinatePattern.FindStringSubmatch(stem)
	if len(match) != 3 {
		return 0, 0, false
	}
	var x, y int64
	if _, err := fmt.Sscan(match[1], &x); err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscan(match[2], &y); err != nil {
		return 0, 0, false
	}
	return x, y, true
}

func ClassifyMapFileType(mapPath string) MapFileType {
	stem := strings.TrimSuffix(path.Base(strings.ReplaceAll(mapPath, "\\", "/")), path.Ext(mapPath))
	lower := strings.ToLower(stem)
	if strings.HasPrefix(lower, "q_") || strings.HasPrefix(lower, "quest") || prefixedDigit(lower, "q") {
		return MapFileQuest
	}
	if strings.HasPrefix(lower, "bn") && len(lower) > 2 && isASCIIDigit(lower[2]) {
		return MapFileNamed
	}
	if strings.Contains(lower, "boss") || prefixedDigit(lower, "b") || typedSuffix(lower, 'b') {
		return MapFileBoss
	}
	if strings.Contains(lower, "start") || prefixedDigit(lower, "s") || typedSuffix(lower, 's') {
		return MapFileStart
	}
	if prefixedDigit(lower, "e") {
		return MapFileEnd
	}
	if prefixedDigit(lower, "h") {
		return MapFileHidden
	}
	if prefixedDigit(lower, "d") {
		return MapFileDefault
	}
	return MapFileNormal
}

func prefixedDigit(value, prefix string) bool {
	return strings.HasPrefix(value, prefix) && len(value) > len(prefix) && isASCIIDigit(value[len(prefix)])
}

func typedSuffix(value string, suffix byte) bool {
	if len(value) < 2 || value[len(value)-1] != suffix {
		return false
	}
	previous := value[len(value)-2]
	return isASCIIDigit(previous) || previous == ')'
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func appendUniqueIndexedMap(target []indexedMap, candidate indexedMap) []indexedMap {
	for _, existing := range target {
		if existing.mapPos == candidate.mapPos && existing.fileType == candidate.fileType {
			return target
		}
	}
	return append(target, candidate)
}

func uniqueIndexedMaps(values []indexedMap) []indexedMap {
	var out []indexedMap
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value.mapPos]; ok {
			continue
		}
		seen[value.mapPos] = struct{}{}
		out = append(out, value)
	}
	return out
}
