package worldmap

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	ErrRunTopologyRequired         = errors.New("dnf dungeon run topology is required")
	ErrRunStartMissing             = errors.New("dnf dungeon run start room is missing")
	ErrRunNotActive                = errors.New("dnf dungeon run is not active")
	ErrRunCurrentRoomNotCleared    = errors.New("dnf dungeon run current room is not cleared")
	ErrRunTargetVisitChanged       = errors.New("dnf dungeon run target visit state changed")
	ErrRoomCoordinateMalformed     = errors.New("dnf dungeon room coordinate is malformed")
	ErrRoomNotFound                = errors.New("dnf dungeon room is not in topology")
	ErrRoomNotAdjacent             = errors.New("dnf dungeon room is not adjacent")
	ErrRoomMapUnresolved           = errors.New("dnf dungeon room map is unresolved")
	ErrLayeredCoordinateMismatch   = errors.New("dnf dungeon layered map coordinate does not match the current room")
	ErrLayeredTransitionChanged    = errors.New("dnf dungeon layered map transition changed")
	ErrLayeredBaseMismatch         = errors.New("dnf dungeon layered base map does not match the cached room")
	ErrStoryStageTransitionChanged = errors.New("dnf dungeon story stage transition changed")
)

type DungeonRunStatus string

const (
	DungeonRunActive    DungeonRunStatus = "active"
	DungeonRunCompleted DungeonRunStatus = "completed"
	DungeonRunAbandoned DungeonRunStatus = "abandoned"
)

type DungeonRunSnapshot struct {
	DungeonID int64            `json:"dungeon_id"`
	MazeIndex int              `json:"maze_index"`
	Status    DungeonRunStatus `json:"status"`
	Current   RoomCoordinate   `json:"current"`
	Visited   []RoomCoordinate `json:"visited,omitempty"`
	Cleared   []RoomCoordinate `json:"cleared,omitempty"`
}

type DungeonRun struct {
	mu       sync.RWMutex
	topology *DungeonTopology
	current  coordinateKey
	visited  map[coordinateKey]struct{}
	cleared  map[coordinateKey]struct{}
	status   DungeonRunStatus
}

func ParseRoomCoordinate(value string) (RoomCoordinate, error) {
	value = strings.TrimSpace(value)
	xText, yText, ok := strings.Cut(value, ":")
	if !ok || xText == "" || yText == "" || strings.Contains(yText, ":") {
		return RoomCoordinate{}, fmt.Errorf("%w: %q", ErrRoomCoordinateMalformed, value)
	}
	x, err := strconv.ParseInt(xText, 10, 64)
	if err != nil {
		return RoomCoordinate{}, fmt.Errorf("%w: %q", ErrRoomCoordinateMalformed, value)
	}
	y, err := strconv.ParseInt(yText, 10, 64)
	if err != nil {
		return RoomCoordinate{}, fmt.Errorf("%w: %q", ErrRoomCoordinateMalformed, value)
	}
	return RoomCoordinate{X: x, Y: y}, nil
}

func NewDungeonRun(topology *DungeonTopology) (*DungeonRun, error) {
	if topology == nil {
		return nil, ErrRunTopologyRequired
	}
	if topology.Start == nil {
		return nil, fmt.Errorf("%w: dungeon=%d maze=%d", ErrRunStartMissing, topology.DungeonID, topology.MazeIndex)
	}
	start := *topology.Start
	room, ok := topology.Room(start)
	if !ok {
		return nil, fmt.Errorf("%w: dungeon=%d maze=%d room=%s", ErrRoomNotFound, topology.DungeonID, topology.MazeIndex, start)
	}
	if room.Map == nil {
		return nil, roomMapError(topology, start)
	}
	key := coordinateKey{x: start.X, y: start.Y}
	return &DungeonRun{
		topology: topology,
		current:  key,
		visited:  map[coordinateKey]struct{}{key: {}},
		cleared:  make(map[coordinateKey]struct{}),
		status:   DungeonRunActive,
	}, nil
}

func (r *DungeonRun) CurrentRoom() (DungeonRoom, bool) {
	if r == nil {
		return DungeonRoom{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.topology.Room(RoomCoordinate{X: r.current.x, Y: r.current.y})
}

// Rooms returns the resolved immutable room layout selected for this run.
// Callers receive cloned room values and cannot mutate the run topology.
func (r *DungeonRun) Rooms() []DungeonRoom {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.topology.Rooms()
}

func (r *DungeonRun) MoveByte(nextX, nextY byte) (DungeonRoom, error) {
	return r.MoveTo(RoomCoordinate{X: int64(nextX), Y: int64(nextY)})
}

// PreviewMoveByte validates a room transition without changing the run.
func (r *DungeonRun) PreviewMoveByte(nextX, nextY byte) (DungeonRoom, error) {
	return r.PreviewMoveTo(RoomCoordinate{X: int64(nextX), Y: int64(nextY)})
}

func (r *DungeonRun) MoveRoomID(roomID string) (DungeonRoom, error) {
	coordinate, err := ParseRoomCoordinate(roomID)
	if err != nil {
		return DungeonRoom{}, err
	}
	return r.MoveTo(coordinate)
}

// PreviewMoveTo validates a room transition without changing current, visited,
// cleared, or completion state.
func (r *DungeonRun) PreviewMoveTo(target RoomCoordinate) (DungeonRoom, error) {
	room, _, _, err := r.previewMoveToVisit(target)
	return room, err
}

// previewStoryStage validates one PVF-declared activity-story stage as
// adjacent room travel, then replaces the topology's base map descriptor with
// the exact ordered story candidate. A previously visited coordinate remains
// a fresh story scene; the revisit bit is used only to select client payload
// mode 2.
func (r *DungeonRun) previewStoryStage(stageIndex int) (DungeonRoom, bool, error) {
	if r == nil {
		return DungeonRoom{}, false, ErrRunTopologyRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolveStoryStageLocked(stageIndex)
}

func (r *DungeonRun) commitStoryStage(
	stageIndex int,
	expectedCoordinate RoomCoordinate,
	expectedMapID int64,
	expectedRevisit bool,
	targetCleared bool,
) (DungeonRoom, error) {
	if r == nil {
		return DungeonRoom{}, ErrRunTopologyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	room, revisit, err := r.resolveStoryStageLocked(stageIndex)
	if err != nil {
		return DungeonRoom{}, err
	}
	actualMapID := int64(0)
	if room.Map != nil {
		actualMapID = room.Map.Map.ID
	}
	if room.Coordinate != expectedCoordinate || actualMapID != expectedMapID || revisit != expectedRevisit {
		return DungeonRoom{}, fmt.Errorf(
			"%w: stage=%d expected=%s/%d revisit=%t actual=%s/%d revisit=%t",
			ErrStoryStageTransitionChanged,
			stageIndex,
			expectedCoordinate,
			expectedMapID,
			expectedRevisit,
			room.Coordinate,
			actualMapID,
			revisit,
		)
	}
	targetKey := coordinateKey{x: room.Coordinate.X, y: room.Coordinate.Y}
	r.current = targetKey
	r.visited[targetKey] = struct{}{}
	delete(r.cleared, targetKey)
	if targetCleared {
		r.cleared[targetKey] = struct{}{}
	}
	return room, nil
}

func (r *DungeonRun) resolveStoryStageLocked(stageIndex int) (DungeonRoom, bool, error) {
	if r.topology == nil || r.topology.resolver == nil {
		return DungeonRoom{}, false, ErrResolverTableRequired
	}
	if r.status != DungeonRunActive {
		return DungeonRoom{}, false, fmt.Errorf("%w: status=%s", ErrRunNotActive, r.status)
	}
	resolved, stage, err := r.topology.resolver.ResolveStoryStage(
		r.topology.DungeonID,
		r.topology.MazeIndex,
		stageIndex,
	)
	if err != nil {
		return DungeonRoom{}, false, err
	}
	targetKey := coordinateKey{x: stage.Coordinate.X, y: stage.Coordinate.Y}
	room, ok := r.topology.rooms[targetKey]
	if !ok {
		return DungeonRoom{}, false, fmt.Errorf("%w: room=%s", ErrRoomNotFound, stage.Coordinate)
	}
	if abs64(stage.Coordinate.X-r.current.x)+abs64(stage.Coordinate.Y-r.current.y) != 1 {
		return DungeonRoom{}, false, fmt.Errorf(
			"%w: current=%s target=%s story_stage=%d",
			ErrRoomNotAdjacent,
			RoomCoordinate{X: r.current.x, Y: r.current.y},
			stage.Coordinate,
			stageIndex,
		)
	}
	if !r.topology.AllowMoveBeforeClear {
		if _, ok := r.cleared[r.current]; !ok {
			return DungeonRoom{}, false, fmt.Errorf(
				"%w: room=%s story_stage=%d",
				ErrRunCurrentRoomNotCleared,
				RoomCoordinate{X: r.current.x, Y: r.current.y},
				stageIndex,
			)
		}
	}
	_, revisit := r.visited[targetKey]
	room.Map = &resolved
	return cloneDungeonRoom(room), revisit, nil
}

// previewCurrentLayered resolves a PVF-declared layer for the current room
// without requiring the source room to be clear. Current clients request this
// same-coordinate transition from cinematic [CHANGE MAP] with MOVE_MAP kind 1.
func (r *DungeonRun) previewCurrentLayered(coordinate RoomCoordinate, layerIndex int) (DungeonRoom, error) {
	if r == nil {
		return DungeonRoom{}, ErrRunTopologyRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolveCurrentLayeredLocked(coordinate, layerIndex)
}

// commitCurrentLayered revalidates the exact previewed map and replaces the
// coordinate's prior clear bit with the fresh layer's state. Visited/current
// coordinates do not change because a layered transition is not room travel.
func (r *DungeonRun) commitCurrentLayered(
	coordinate RoomCoordinate,
	layerIndex int,
	expectedMapID int64,
	targetCleared bool,
) (DungeonRoom, error) {
	if r == nil {
		return DungeonRoom{}, ErrRunTopologyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	room, err := r.resolveCurrentLayeredLocked(coordinate, layerIndex)
	if err != nil {
		return DungeonRoom{}, err
	}
	if room.Map == nil || room.Map.Map.ID != expectedMapID {
		actualMapID := int64(0)
		if room.Map != nil {
			actualMapID = room.Map.Map.ID
		}
		return DungeonRoom{}, fmt.Errorf(
			"%w: room=%s layer=%d expected_map=%d actual_map=%d",
			ErrLayeredTransitionChanged,
			coordinate,
			layerIndex,
			expectedMapID,
			actualMapID,
		)
	}
	delete(r.cleared, r.current)
	if targetCleared {
		r.cleared[r.current] = struct{}{}
	}
	return room, nil
}

func (r *DungeonRun) resolveCurrentLayeredLocked(
	coordinate RoomCoordinate,
	layerIndex int,
) (DungeonRoom, error) {
	if r.topology == nil {
		return DungeonRoom{}, ErrRunTopologyRequired
	}
	if r.status != DungeonRunActive {
		return DungeonRoom{}, fmt.Errorf("%w: status=%s", ErrRunNotActive, r.status)
	}
	current := RoomCoordinate{X: r.current.x, Y: r.current.y}
	if coordinate != current {
		return DungeonRoom{}, fmt.Errorf(
			"%w: current=%s requested=%s",
			ErrLayeredCoordinateMismatch,
			current,
			coordinate,
		)
	}
	room, ok := r.topology.rooms[r.current]
	if !ok {
		return DungeonRoom{}, fmt.Errorf("%w: room=%s", ErrRoomNotFound, current)
	}
	if r.topology.resolver == nil {
		return DungeonRoom{}, ErrResolverTableRequired
	}
	resolved, err := r.topology.resolver.ResolveLayered(
		r.topology.DungeonID,
		r.topology.MazeIndex,
		coordinate,
		layerIndex,
	)
	if err != nil {
		return DungeonRoom{}, err
	}
	room.Map = &resolved
	return cloneDungeonRoom(room), nil
}

// previewCurrentLayeredBase resolves the topology-owned base room for the
// current coordinate without mutating the run. A layered transition never
// changes the current coordinate, so the topology entry remains the base room
// that the explicit PVF layer list temporarily replaced. The coordinate's
// current clear bit is reported for diagnostics only: after a layered commit
// it describes the layer, not the base room.
func (r *DungeonRun) previewCurrentLayeredBase(coordinate RoomCoordinate) (DungeonRoom, bool, error) {
	if r == nil {
		return DungeonRoom{}, false, ErrRunTopologyRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	room, err := r.resolveCurrentLayeredBaseLocked(coordinate)
	if err != nil {
		return DungeonRoom{}, false, err
	}
	_, cleared := r.cleared[r.current]
	return room, cleared, nil
}

// commitCurrentLayeredBase revalidates the exact topology-owned base map and
// replaces the coordinate's clear bit with the restored base room's state.
// Visited/current coordinates do not change because a layered base return is
// not room travel.
func (r *DungeonRun) commitCurrentLayeredBase(
	coordinate RoomCoordinate,
	expectedMapID int64,
	targetCleared bool,
) (DungeonRoom, error) {
	if r == nil {
		return DungeonRoom{}, ErrRunTopologyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	room, err := r.resolveCurrentLayeredBaseLocked(coordinate)
	if err != nil {
		return DungeonRoom{}, err
	}
	if room.Map == nil || room.Map.Map.ID != expectedMapID {
		actualMapID := int64(0)
		if room.Map != nil {
			actualMapID = room.Map.Map.ID
		}
		return DungeonRoom{}, fmt.Errorf(
			"%w: room=%s expected_base_map=%d actual_base_map=%d",
			ErrLayeredBaseMismatch,
			coordinate,
			expectedMapID,
			actualMapID,
		)
	}
	delete(r.cleared, r.current)
	if targetCleared {
		r.cleared[r.current] = struct{}{}
	}
	return room, nil
}

func (r *DungeonRun) resolveCurrentLayeredBaseLocked(coordinate RoomCoordinate) (DungeonRoom, error) {
	if r.topology == nil {
		return DungeonRoom{}, ErrRunTopologyRequired
	}
	if r.status != DungeonRunActive && r.status != DungeonRunCompleted {
		return DungeonRoom{}, fmt.Errorf("%w: status=%s", ErrRunNotActive, r.status)
	}
	current := RoomCoordinate{X: r.current.x, Y: r.current.y}
	if coordinate != current {
		return DungeonRoom{}, fmt.Errorf(
			"%w: current=%s requested=%s",
			ErrLayeredCoordinateMismatch,
			current,
			coordinate,
		)
	}
	room, ok := r.topology.rooms[r.current]
	if !ok {
		return DungeonRoom{}, fmt.Errorf("%w: room=%s", ErrRoomNotFound, current)
	}
	if room.Map == nil {
		return DungeonRoom{}, roomMapError(r.topology, current)
	}
	return cloneDungeonRoom(room), nil
}

// previewMoveToVisit validates a transition and reports the persistent run
// state for its target under the same read lock. DungeonSession uses this to
// distinguish a first visit from a revisit without treating rebuilt PVF scene
// data as prior runtime state.
func (r *DungeonRun) previewMoveToVisit(target RoomCoordinate) (DungeonRoom, bool, bool, error) {
	if r == nil {
		return DungeonRoom{}, false, false, ErrRunTopologyRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	room, err := r.validateMoveLocked(target)
	if err != nil {
		return DungeonRoom{}, false, false, err
	}
	targetKey := coordinateKey{x: target.X, y: target.Y}
	_, visited := r.visited[targetKey]
	_, cleared := r.cleared[targetKey]
	return room, visited, cleared, nil
}

func (r *DungeonRun) MoveTo(target RoomCoordinate) (DungeonRoom, error) {
	return r.moveTo(target, false)
}

func (r *DungeonRun) moveTo(target RoomCoordinate, clearTarget bool) (DungeonRoom, error) {
	room, _, err := r.moveToVisit(target, clearTarget, nil)
	return room, err
}

// moveToVisit commits a transition and can require the target's first/revisit
// classification to match a prior preview. The optional expectation keeps a
// cached-room restore fail-closed if another owner mutated DungeonRun directly
// between preview and commit.
func (r *DungeonRun) moveToVisit(target RoomCoordinate, clearTarget bool, expectedRevisit *bool) (DungeonRoom, bool, error) {
	if r == nil {
		return DungeonRoom{}, false, ErrRunTopologyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	room, err := r.validateMoveLocked(target)
	if err != nil {
		return DungeonRoom{}, false, err
	}
	targetKey := coordinateKey{x: target.X, y: target.Y}
	_, revisit := r.visited[targetKey]
	if expectedRevisit != nil && revisit != *expectedRevisit {
		return DungeonRoom{}, revisit, fmt.Errorf(
			"%w: room=%s expected_revisit=%t actual_revisit=%t",
			ErrRunTargetVisitChanged,
			target,
			*expectedRevisit,
			revisit,
		)
	}
	r.current = targetKey
	r.visited[targetKey] = struct{}{}
	if clearTarget {
		r.cleared[targetKey] = struct{}{}
	}
	return room, revisit, nil
}

func (r *DungeonRun) validateMoveLocked(target RoomCoordinate) (DungeonRoom, error) {
	if r.status != DungeonRunActive {
		return DungeonRoom{}, fmt.Errorf("%w: status=%s", ErrRunNotActive, r.status)
	}
	targetKey := coordinateKey{x: target.X, y: target.Y}
	room, ok := r.topology.rooms[targetKey]
	if !ok {
		return DungeonRoom{}, fmt.Errorf("%w: room=%s", ErrRoomNotFound, target)
	}
	if abs64(target.X-r.current.x)+abs64(target.Y-r.current.y) != 1 {
		return DungeonRoom{}, fmt.Errorf(
			"%w: current=%s target=%s",
			ErrRoomNotAdjacent, RoomCoordinate{X: r.current.x, Y: r.current.y}, target,
		)
	}
	if !r.topology.AllowMoveBeforeClear {
		if _, ok := r.cleared[r.current]; !ok {
			return DungeonRoom{}, fmt.Errorf(
				"%w: room=%s",
				ErrRunCurrentRoomNotCleared, RoomCoordinate{X: r.current.x, Y: r.current.y},
			)
		}
	}
	if room.Map == nil {
		return DungeonRoom{}, roomMapError(r.topology, target)
	}
	return cloneDungeonRoom(room), nil
}

func (r *DungeonRun) MarkCurrentRoomCleared() error {
	if r == nil {
		return ErrRunTopologyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status != DungeonRunActive {
		return fmt.Errorf("%w: status=%s", ErrRunNotActive, r.status)
	}
	r.cleared[r.current] = struct{}{}
	return nil
}

func (r *DungeonRun) Complete() error {
	if r == nil {
		return ErrRunTopologyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status != DungeonRunActive {
		return fmt.Errorf("%w: status=%s", ErrRunNotActive, r.status)
	}
	if _, ok := r.cleared[r.current]; !ok {
		return fmt.Errorf(
			"%w: room=%s",
			ErrRunCurrentRoomNotCleared, RoomCoordinate{X: r.current.x, Y: r.current.y},
		)
	}
	r.status = DungeonRunCompleted
	return nil
}

// CompleteCurrentRoom records an authoritative scripted completion event for
// the active room. Unlike Complete, it does not require every blocking actor to
// have reported a death: some PVF tutorial finals end by escape/cinematic while
// ordinary actors are still present. It marks only the room/run lifecycle and
// never invents defeated runtime objects.
func (r *DungeonRun) CompleteCurrentRoom() error {
	if r == nil {
		return ErrRunTopologyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status != DungeonRunActive {
		return fmt.Errorf("%w: status=%s", ErrRunNotActive, r.status)
	}
	r.cleared[r.current] = struct{}{}
	r.status = DungeonRunCompleted
	return nil
}

func (r *DungeonRun) Abandon() error {
	if r == nil {
		return ErrRunTopologyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status != DungeonRunActive {
		return fmt.Errorf("%w: status=%s", ErrRunNotActive, r.status)
	}
	r.status = DungeonRunAbandoned
	return nil
}

func (r *DungeonRun) Snapshot() DungeonRunSnapshot {
	if r == nil {
		return DungeonRunSnapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot := DungeonRunSnapshot{
		DungeonID: r.topology.DungeonID,
		MazeIndex: r.topology.MazeIndex,
		Status:    r.status,
		Current:   RoomCoordinate{X: r.current.x, Y: r.current.y},
		Visited:   sortedCoordinates(r.visited),
		Cleared:   sortedCoordinates(r.cleared),
	}
	return snapshot
}

func roomMapError(topology *DungeonTopology, coordinate RoomCoordinate) error {
	key := coordinateKey{x: coordinate.X, y: coordinate.Y}
	if cause := topology.resolutionErrors[key]; cause != nil {
		return fmt.Errorf("%w: room=%s: %w", ErrRoomMapUnresolved, coordinate, cause)
	}
	return fmt.Errorf("%w: room=%s", ErrRoomMapUnresolved, coordinate)
}

func sortedCoordinates(values map[coordinateKey]struct{}) []RoomCoordinate {
	coordinates := make([]RoomCoordinate, 0, len(values))
	for key := range values {
		coordinates = append(coordinates, RoomCoordinate{X: key.x, Y: key.y})
	}
	sort.Slice(coordinates, func(i, j int) bool {
		if coordinates[i].Y != coordinates[j].Y {
			return coordinates[i].Y < coordinates[j].Y
		}
		return coordinates[i].X < coordinates[j].X
	})
	return coordinates
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
