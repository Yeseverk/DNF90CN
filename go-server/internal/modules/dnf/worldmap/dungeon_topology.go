package worldmap

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

var (
	ErrTopologyResolverRequired = errors.New("dnf dungeon topology resolver is required")
	ErrTopologyEmpty            = errors.New("dnf dungeon topology has no rooms")
)

type RoomDirection string

const (
	RoomDirectionUp    RoomDirection = "up"
	RoomDirectionRight RoomDirection = "right"
	RoomDirectionDown  RoomDirection = "down"
	RoomDirectionLeft  RoomDirection = "left"
)

type RoomCoordinate struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
}

func (c RoomCoordinate) String() string {
	return fmt.Sprintf("%d:%d", c.X, c.Y)
}

type RoomNeighbor struct {
	Direction  RoomDirection  `json:"direction"`
	Coordinate RoomCoordinate `json:"coordinate"`
}

type DungeonRoom struct {
	Coordinate      RoomCoordinate `json:"coordinate"`
	Start           bool           `json:"start,omitempty"`
	Boss            bool           `json:"boss,omitempty"`
	Map             *ResolvedMap   `json:"map,omitempty"`
	ResolutionError string         `json:"resolution_error,omitempty"`
	Neighbors       []RoomNeighbor `json:"neighbors,omitempty"`
}

type TopologyDiagnostic struct {
	Code       string          `json:"code"`
	Coordinate *RoomCoordinate `json:"coordinate,omitempty"`
	Message    string          `json:"message"`
}

type DungeonTopology struct {
	DungeonID            int64                `json:"dungeon_id"`
	MazeIndex            int                  `json:"maze_index"`
	Width                OptionalInt          `json:"width"`
	Height               OptionalInt          `json:"height"`
	Start                *RoomCoordinate      `json:"start,omitempty"`
	Bosses               []RoomCoordinate     `json:"bosses,omitempty"`
	AllowMoveBeforeClear bool                 `json:"allow_move_before_clear,omitempty"`
	Diagnostics          []TopologyDiagnostic `json:"diagnostics,omitempty"`

	rooms            map[coordinateKey]DungeonRoom
	resolutionErrors map[coordinateKey]error
	resolver         *Resolver
}

type topologyDelta struct {
	direction RoomDirection
	dx        int64
	dy        int64
	mask      uint8
	opposite  uint8
}

var topologyDeltas = [...]topologyDelta{
	{direction: RoomDirectionUp, dx: 0, dy: -1, mask: 0x02, opposite: 0x08},
	{direction: RoomDirectionRight, dx: 1, dy: 0, mask: 0x01, opposite: 0x04},
	{direction: RoomDirectionDown, dx: 0, dy: 1, mask: 0x08, opposite: 0x02},
	{direction: RoomDirectionLeft, dx: -1, dy: 0, mask: 0x04, opposite: 0x01},
}

func BuildDungeonTopology(resolver *Resolver, dungeonID int64, mazeIndex int) (*DungeonTopology, error) {
	if resolver == nil || resolver.table == nil {
		return nil, ErrTopologyResolverRequired
	}
	dungeonIndex, ok := resolver.byDungeon[dungeonID]
	if !ok {
		return nil, fmt.Errorf("%w: dungeon=%d", ErrDungeonNotIndexed, dungeonID)
	}
	if mazeIndex < 0 || mazeIndex >= len(dungeonIndex.mazes) {
		return nil, fmt.Errorf("%w: dungeon=%d maze=%d", ErrMazeNotFound, dungeonID, mazeIndex)
	}

	dungeon := resolver.table.dungeons[dungeonIndex.dungeonPos]
	maze := dungeon.Mazes[mazeIndex]
	topology := &DungeonTopology{
		DungeonID: dungeonID, MazeIndex: mazeIndex,
		Width: maze.Width, Height: maze.Height,
		AllowMoveBeforeClear: dungeon.Metadata.Flags["move map even enemy"],
		rooms:                make(map[coordinateKey]DungeonRoom),
		resolutionErrors:     make(map[coordinateKey]error),
		resolver:             resolver,
	}
	cells := make(map[coordinateKey]struct{})
	greedMasks, structuredGreed := addGreedCells(topology, maze, cells)
	addSpecificationCells(cells, maze.MapSpecifications)
	addSpecificationCells(cells, maze.BossSpecifications)
	addSpecificationCells(cells, maze.LayeredSpecifications)

	if maze.Start != nil {
		start := RoomCoordinate{X: maze.Start.X, Y: maze.Start.Y}
		topology.Start = &start
		addExplicitCell(topology, maze, cells, start, "start_out_of_bounds")
	}
	for _, boss := range mazeBossCoordinates(maze.Boss) {
		topology.Bosses = append(topology.Bosses, boss)
		addExplicitCell(topology, maze, cells, boss, "boss_out_of_bounds")
	}

	if !structuredGreed {
		// The resolver already limits these paths to directories owned by this maze's
		// explicit specifications. Coordinates are data, not a map-selection fallback.
		for key := range dungeonIndex.mazes[mazeIndex].byCoordinate {
			coordinate := RoomCoordinate{X: key.x, Y: key.y}
			if withinMazeBounds(maze, coordinate) {
				cells[key] = struct{}{}
			}
		}
	}
	if len(cells) == 0 {
		return nil, fmt.Errorf("%w: dungeon=%d maze=%d", ErrTopologyEmpty, dungeonID, mazeIndex)
	}

	bosses := make(map[coordinateKey]struct{}, len(topology.Bosses))
	for _, boss := range topology.Bosses {
		bosses[coordinateKey{x: boss.X, y: boss.Y}] = struct{}{}
	}
	for key := range cells {
		coordinate := RoomCoordinate{X: key.x, Y: key.y}
		room := DungeonRoom{Coordinate: coordinate}
		room.Start = topology.Start != nil && topology.Start.X == key.x && topology.Start.Y == key.y
		_, room.Boss = bosses[key]
		resolved, err := resolver.Resolve(ResolveRequest{
			DungeonID: dungeonID, MazeIndex: mazeIndex, X: key.x, Y: key.y,
		})
		if err != nil {
			room.ResolutionError = err.Error()
			topology.resolutionErrors[key] = err
			topology.addDiagnostic("room_map_unresolved", &coordinate, err.Error())
		} else {
			room.Map = &resolved
		}
		topology.rooms[key] = room
	}
	for key, room := range topology.rooms {
		for _, delta := range topologyDeltas {
			neighborKey := coordinateKey{x: key.x + delta.dx, y: key.y + delta.dy}
			if _, ok := topology.rooms[neighborKey]; !ok {
				continue
			}
			if structuredGreed {
				currentMask, currentInGrid := greedMasks[key]
				neighborMask, neighborInGrid := greedMasks[neighborKey]
				if !currentInGrid || !neighborInGrid || currentMask&delta.mask == 0 || neighborMask&delta.opposite == 0 {
					continue
				}
			}
			room.Neighbors = append(room.Neighbors, RoomNeighbor{
				Direction:  delta.direction,
				Coordinate: RoomCoordinate{X: neighborKey.x, Y: neighborKey.y},
			})
		}
		topology.rooms[key] = room
	}
	return topology, nil
}

func (t *DungeonTopology) Room(coordinate RoomCoordinate) (DungeonRoom, bool) {
	if t == nil {
		return DungeonRoom{}, false
	}
	room, ok := t.rooms[coordinateKey{x: coordinate.X, y: coordinate.Y}]
	if !ok {
		return DungeonRoom{}, false
	}
	return cloneDungeonRoom(room), true
}

func (t *DungeonTopology) Rooms() []DungeonRoom {
	if t == nil {
		return nil
	}
	rooms := make([]DungeonRoom, 0, len(t.rooms))
	for _, room := range t.rooms {
		rooms = append(rooms, cloneDungeonRoom(room))
	}
	sort.Slice(rooms, func(i, j int) bool {
		if rooms[i].Coordinate.Y != rooms[j].Coordinate.Y {
			return rooms[i].Coordinate.Y < rooms[j].Coordinate.Y
		}
		return rooms[i].Coordinate.X < rooms[j].Coordinate.X
	})
	return rooms
}

func (t *DungeonTopology) UnresolvedRooms() []DungeonRoom {
	rooms := t.Rooms()
	unresolved := rooms[:0]
	for _, room := range rooms {
		if room.Map == nil {
			unresolved = append(unresolved, room)
		}
	}
	return unresolved
}

func (t *DungeonTopology) addDiagnostic(code string, coordinate *RoomCoordinate, message string) {
	var copied *RoomCoordinate
	if coordinate != nil {
		value := *coordinate
		copied = &value
	}
	t.Diagnostics = append(t.Diagnostics, TopologyDiagnostic{Code: code, Coordinate: copied, Message: message})
}

func addGreedCells(
	topology *DungeonTopology,
	maze Maze,
	cells map[coordinateKey]struct{},
) (map[coordinateKey]uint8, bool) {
	if maze.Greed == "" {
		return nil, false
	}
	if !maze.Width.Set || !maze.Height.Set || maze.Width.Value <= 0 || maze.Height.Value <= 0 {
		topology.addDiagnostic("maze_size_invalid", nil, "[greed] requires positive [size] width and height")
		return nil, false
	}
	rows := normalizedGreedRows(maze.Greed)
	if len(rows) == 0 {
		return nil, false
	}
	paired := greedRowsArePaired(rows, maze.Width.Value)
	if int64(len(rows)) < maze.Height.Value {
		topology.addDiagnostic(
			"maze_grid_rows_short", nil,
			fmt.Sprintf("[greed] has %d rows, [size] declares %d; omitted rows are empty", len(rows), maze.Height.Value),
		)
	} else if int64(len(rows)) > maze.Height.Value {
		topology.addDiagnostic(
			"maze_grid_rows_extra", nil,
			fmt.Sprintf("[greed] has %d rows, [size] declares %d", len(rows), maze.Height.Value),
		)
	}
	masks := make(map[coordinateKey]uint8)
	rowCount := int64(len(rows))
	if rowCount > maze.Height.Value {
		rowCount = maze.Height.Value
	}
	for y := int64(0); y < rowCount; y++ {
		row := rows[y]
		unitWidth := int64(1)
		if paired {
			unitWidth = 2
			if len(row)%2 != 0 {
				topology.addDiagnostic(
					"maze_grid_row_odd", &RoomCoordinate{X: int64(len(row) / 2), Y: y},
					fmt.Sprintf("[greed] row %d has an unmatched paired symbol", y),
				)
			}
		}
		logicalWidth := int64(len(row)) / unitWidth
		if logicalWidth < maze.Width.Value {
			topology.addDiagnostic(
				"maze_grid_row_short", &RoomCoordinate{X: logicalWidth, Y: y},
				fmt.Sprintf("[greed] row %d has %d cells, [size] declares %d; omitted cells are empty", y, logicalWidth, maze.Width.Value),
			)
		} else if logicalWidth > maze.Width.Value {
			topology.addDiagnostic(
				"maze_grid_row_extra", &RoomCoordinate{X: maze.Width.Value, Y: y},
				fmt.Sprintf("[greed] row %d has %d cells, [size] declares %d", y, logicalWidth, maze.Width.Value),
			)
			logicalWidth = maze.Width.Value
		}
		for x := int64(0); x < logicalWidth; x++ {
			valueIndex := x * unitWidth
			if paired {
				if valueIndex+1 >= int64(len(row)) {
					break
				}
				if row[valueIndex] != row[valueIndex+1] {
					coordinate := RoomCoordinate{X: x, Y: y}
					topology.addDiagnostic(
						"maze_grid_pair_mismatch", &coordinate,
						fmt.Sprintf("[greed] pair %q%q does not repeat one room code", row[valueIndex], row[valueIndex+1]),
					)
					continue
				}
			}
			mask, ok := greedRoomMask(row[valueIndex])
			if !ok {
				coordinate := RoomCoordinate{X: x, Y: y}
				topology.addDiagnostic(
					"maze_grid_code_unknown", &coordinate,
					fmt.Sprintf("[greed] room code %q is unknown", row[valueIndex]),
				)
				continue
			}
			if mask != 0 {
				key := coordinateKey{x: x, y: y}
				cells[key] = struct{}{}
				masks[key] = mask
			}
		}
	}
	return masks, true
}

func normalizedGreedRows(value string) [][]rune {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	rows := make([][]rune, 0, len(lines))
	for _, line := range lines {
		row := make([]rune, 0, len(line))
		for _, symbol := range line {
			if unicode.IsSpace(symbol) || symbol == '`' || symbol == ',' {
				continue
			}
			row = append(row, symbol)
		}
		rows = append(rows, row)
	}
	for len(rows) > 0 && len(rows[0]) == 0 {
		rows = rows[1:]
	}
	for len(rows) > 0 && len(rows[len(rows)-1]) == 0 {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func greedRowsArePaired(rows [][]rune, width int64) bool {
	allRepeated := true
	hasSymbols := false
	for _, row := range rows {
		if int64(len(row)) > width {
			return true
		}
		if len(row) == 0 {
			continue
		}
		hasSymbols = true
		if len(row)%2 != 0 {
			allRepeated = false
			continue
		}
		for index := 0; index < len(row); index += 2 {
			if row[index] != row[index+1] {
				allRepeated = false
				break
			}
		}
	}
	return hasSymbols && allRepeated
}

func greedRoomMask(value rune) (uint8, bool) {
	if value >= 'a' && value <= 'p' {
		value -= 'a' - 'A'
	}
	if value >= 'A' && value <= 'P' {
		return uint8(value - 'A'), true
	}
	switch value {
	case '0', '.', 'x', 'X':
		return 0, true
	case '1':
		return 0x0f, true
	default:
		return 0, false
	}
}

func addSpecificationCells(cells map[coordinateKey]struct{}, specifications []MapSpecification) {
	for _, specification := range specifications {
		cells[coordinateKey{x: specification.Coordinate.X, y: specification.Coordinate.Y}] = struct{}{}
	}
}

func addExplicitCell(
	topology *DungeonTopology,
	maze Maze,
	cells map[coordinateKey]struct{},
	coordinate RoomCoordinate,
	diagnosticCode string,
) {
	if !withinMazeBounds(maze, coordinate) {
		topology.addDiagnostic(
			diagnosticCode, &coordinate,
			fmt.Sprintf("coordinate %s is outside declared maze size", coordinate),
		)
	}
	cells[coordinateKey{x: coordinate.X, y: coordinate.Y}] = struct{}{}
}

func withinMazeBounds(maze Maze, coordinate RoomCoordinate) bool {
	if maze.Width.Set && maze.Width.Value > 0 && (coordinate.X < 0 || coordinate.X >= maze.Width.Value) {
		return false
	}
	if maze.Height.Set && maze.Height.Value > 0 && (coordinate.Y < 0 || coordinate.Y >= maze.Height.Value) {
		return false
	}
	return true
}

func mazeBossCoordinates(point *MazePoint) []RoomCoordinate {
	if point == nil {
		return nil
	}
	coordinates := []RoomCoordinate{{X: point.X, Y: point.Y}}
	for index := 0; index+1 < len(point.Params); index += 2 {
		coordinates = append(coordinates, RoomCoordinate{X: point.Params[index], Y: point.Params[index+1]})
	}
	return coordinates
}

func cloneDungeonRoom(room DungeonRoom) DungeonRoom {
	room.Neighbors = append([]RoomNeighbor(nil), room.Neighbors...)
	if room.Map != nil {
		resolved := *room.Map
		resolved.Map = cloneMap(resolved.Map)
		room.Map = &resolved
	}
	return room
}
