package worldmap

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var (
	ErrDungeonRunRequired      = errors.New("dnf dungeon run is required")
	ErrHostileReferenceInvalid = errors.New("dnf dungeon hostile reference is invalid")
	ErrRuntimeObjectKeyInvalid = errors.New("dnf dungeon runtime object key is invalid")
	ErrRuntimeObjectKeyBound   = errors.New("dnf dungeon runtime object key is already bound")
	ErrHostileAlreadyBound     = errors.New("dnf dungeon hostile is already bound")
	ErrHostileNotBound         = errors.New("dnf dungeon hostile runtime object is not bound")
	ErrHostileAlreadyDefeated  = errors.New("dnf dungeon hostile is already defeated")
	ErrRevisitSceneRequired    = errors.New("dnf dungeon revisit scene is required")
	ErrRevisitSceneUnexpected  = errors.New("dnf dungeon revisit scene was supplied for a first visit")
	ErrRevisitSceneMismatch    = errors.New("dnf dungeon revisit scene does not match persistent room state")
)

type HostileKind string

const (
	HostileMonster     HostileKind = "monster"
	HostileAICharacter HostileKind = "ai_character"
)

type HostileReference struct {
	Kind  HostileKind `json:"kind"`
	Index int         `json:"index"`
}

type DungeonRoomScene struct {
	Coordinate            RoomCoordinate              `json:"coordinate"`
	Start                 bool                        `json:"start,omitempty"`
	Boss                  bool                        `json:"boss,omitempty"`
	Map                   ResolvedMap                 `json:"map"`
	Monsters              []MonsterSpawn              `json:"monsters,omitempty"`
	NPCs                  []NPCSpawn                  `json:"npcs,omitempty"`
	AICharacters          []AICharacter               `json:"ai_characters,omitempty"`
	PassiveObjects        []PassiveObject             `json:"passive_objects,omitempty"`
	SpecialPassiveObjects []SpecialPassiveObject      `json:"special_passive_objects,omitempty"`
	Portals               []Portal                    `json:"portals,omitempty"`
	Summons               []MapSummon                 `json:"summons,omitempty"`
	ExpectedHostiles      []HostileReference          `json:"expected_hostiles,omitempty"`
	BlockingHostiles      []HostileReference          `json:"blocking_hostiles,omitempty"`
	RuntimeObjects        map[uint32]HostileReference `json:"runtime_objects,omitempty"`
	DefeatedObjects       []uint32                    `json:"defeated_objects,omitempty"`
	Cleared               bool                        `json:"cleared,omitempty"`
}

type DungeonSessionSnapshot struct {
	Run   DungeonRunSnapshot `json:"run"`
	Scene DungeonRoomScene   `json:"scene"`
}

// DungeonRoomTransition describes a validated room target. Revisit is derived
// only from DungeonRun's persistent visited set; callers must not infer it from
// a packet cache or from the target room's PVF contents.
type DungeonRoomTransition struct {
	Scene   DungeonRoomScene `json:"scene"`
	Revisit bool             `json:"revisit,omitempty"`
}

type validatedDungeonRoomTransition struct {
	transition DungeonRoomTransition
	bindings   map[HostileReference]uint32
	defeated   map[uint32]struct{}
}

type DungeonSession struct {
	mu       sync.RWMutex
	run      *DungeonRun
	scene    DungeonRoomScene
	bindings map[HostileReference]uint32
	defeated map[uint32]struct{}
}

func NewDungeonSession(run *DungeonRun) (*DungeonSession, error) {
	if run == nil {
		return nil, ErrDungeonRunRequired
	}
	room, ok := run.CurrentRoom()
	if !ok {
		return nil, ErrRoomNotFound
	}
	session := &DungeonSession{run: run}
	if err := session.loadRoom(room); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *DungeonSession) Scene() (DungeonRoomScene, bool) {
	if s == nil || s.run == nil {
		return DungeonRoomScene{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneDungeonRoomScene(s.scene), true
}

// Rooms exposes the run's frozen map choices without exposing its mutable
// visit/clear state. Party orchestration uses this to give every member the
// leader's exact PVF map selection.
func (s *DungeonSession) Rooms() []DungeonRoom {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	run := s.run
	s.mu.RUnlock()
	if run == nil {
		return nil
	}
	return run.Rooms()
}

func (s *DungeonSession) BindHostileObject(reference HostileReference, objectKey uint32) error {
	if s == nil || s.run == nil {
		return ErrDungeonRunRequired
	}
	if objectKey == 0 {
		return ErrRuntimeObjectKeyInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !containsHostileReference(s.scene.ExpectedHostiles, reference) {
		return fmt.Errorf("%w: kind=%s index=%d", ErrHostileReferenceInvalid, reference.Kind, reference.Index)
	}
	if previous, ok := s.bindings[reference]; ok {
		return fmt.Errorf("%w: kind=%s index=%d object_key=%d", ErrHostileAlreadyBound, reference.Kind, reference.Index, previous)
	}
	if previous, ok := s.scene.RuntimeObjects[objectKey]; ok {
		return fmt.Errorf("%w: object_key=%d kind=%s index=%d", ErrRuntimeObjectKeyBound, objectKey, previous.Kind, previous.Index)
	}
	s.bindings[reference] = objectKey
	s.scene.RuntimeObjects[objectKey] = reference
	return nil
}

// MarkHostileNonBlocking keeps a scripted actor in the room's runtime object
// table while excluding it from the ordinary all-monsters-dead door scan.
func (s *DungeonSession) MarkHostileNonBlocking(reference HostileReference) (bool, error) {
	if s == nil || s.run == nil {
		return false, ErrDungeonRunRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !containsHostileReference(s.scene.ExpectedHostiles, reference) {
		return false, fmt.Errorf("%w: kind=%s index=%d", ErrHostileReferenceInvalid, reference.Kind, reference.Index)
	}
	removed := false
	blocking := s.scene.BlockingHostiles[:0]
	for _, candidate := range s.scene.BlockingHostiles {
		if candidate == reference {
			removed = true
			continue
		}
		blocking = append(blocking, candidate)
	}
	if !removed {
		return false, nil
	}
	s.scene.BlockingHostiles = blocking
	if len(s.scene.BlockingHostiles) == 0 && !s.scene.Cleared {
		if err := s.run.MarkCurrentRoomCleared(); err != nil {
			return false, err
		}
		s.scene.Cleared = true
	}
	return true, nil
}

func (s *DungeonSession) MarkHostileDefeated(objectKey uint32) (bool, error) {
	if s == nil || s.run == nil {
		return false, ErrDungeonRunRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.scene.RuntimeObjects[objectKey]; !ok {
		return false, fmt.Errorf("%w: object_key=%d", ErrHostileNotBound, objectKey)
	}
	if _, ok := s.defeated[objectKey]; ok {
		return false, fmt.Errorf("%w: object_key=%d", ErrHostileAlreadyDefeated, objectKey)
	}
	s.defeated[objectKey] = struct{}{}
	s.scene.DefeatedObjects = append(s.scene.DefeatedObjects, objectKey)
	if s.scene.Cleared {
		return false, nil
	}
	for _, reference := range s.scene.BlockingHostiles {
		boundObjectKey, ok := s.bindings[reference]
		if !ok {
			return false, nil
		}
		if _, defeated := s.defeated[boundObjectKey]; !defeated {
			return false, nil
		}
	}
	if err := s.run.MarkCurrentRoomCleared(); err != nil {
		return false, err
	}
	s.scene.Cleared = true
	return true, nil
}

// PreviewMoveByte resolves and validates the target room without changing the
// active scene or any DungeonRun state. Packet owners must use this before
// writing a room-transition response.
func (s *DungeonSession) PreviewMoveByte(nextX, nextY byte) (DungeonRoomScene, error) {
	transition, err := s.PreviewMoveByteTransition(nextX, nextY)
	return transition.Scene, err
}

// PreviewLayered validates a same-coordinate PVF layer without mutating the
// run, scene, runtime bindings, defeated set, or current-room clear state.
// Source-room clearance is intentionally not required: the client can request
// a cinematic layer while scripted source actors remain alive.
func (s *DungeonSession) PreviewLayered(
	coordinate RoomCoordinate,
	layerIndex int,
) (DungeonRoomScene, error) {
	if s == nil || s.run == nil {
		return DungeonRoomScene{}, ErrDungeonRunRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.previewLayeredLocked(coordinate, layerIndex)
}

// PreviewStoryStage validates the next ordered activity-story map without
// mutating the run. Revisit reports whether the target coordinate was already
// rendered; the returned scene is always freshly built from the stage map.
func (s *DungeonSession) PreviewStoryStage(stageIndex int) (DungeonRoomTransition, error) {
	if s == nil || s.run == nil {
		return DungeonRoomTransition{}, ErrDungeonRunRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	room, revisit, err := s.run.previewStoryStage(stageIndex)
	if err != nil {
		return DungeonRoomTransition{}, err
	}
	scene, err := dungeonRoomScene(room)
	if err != nil {
		return DungeonRoomTransition{}, err
	}
	return DungeonRoomTransition{Scene: scene, Revisit: revisit}, nil
}

// CommitStoryStage installs a fresh ordered story scene. Even when its
// coordinate was visited earlier, cached base/stage actors are never restored;
// payload mode 2 makes the client refresh that coordinate with this new map.
func (s *DungeonSession) CommitStoryStage(stageIndex int) (DungeonRoomTransition, error) {
	if s == nil || s.run == nil {
		return DungeonRoomTransition{}, ErrDungeonRunRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	room, revisit, err := s.run.previewStoryStage(stageIndex)
	if err != nil {
		return DungeonRoomTransition{}, err
	}
	scene, err := dungeonRoomScene(room)
	if err != nil {
		return DungeonRoomTransition{}, err
	}
	if _, err := s.run.commitStoryStage(
		stageIndex,
		scene.Coordinate,
		scene.Map.Map.ID,
		revisit,
		scene.Cleared,
	); err != nil {
		return DungeonRoomTransition{}, err
	}
	s.installSceneLocked(scene)
	return DungeonRoomTransition{Scene: cloneDungeonRoomScene(s.scene), Revisit: revisit}, nil
}

// CommitLayered installs a fresh scene at the current coordinate and resets
// all object bindings and defeats from the previous layer. DungeonRun's clear
// bit is also reset (or recomputed for an intrinsically empty target layer),
// so an already-cleared source cannot make the target immediately passable.
func (s *DungeonSession) CommitLayered(
	coordinate RoomCoordinate,
	layerIndex int,
) (DungeonRoomScene, error) {
	if s == nil || s.run == nil {
		return DungeonRoomScene{}, ErrDungeonRunRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	scene, err := s.previewLayeredLocked(coordinate, layerIndex)
	if err != nil {
		return DungeonRoomScene{}, err
	}
	if _, err := s.run.commitCurrentLayered(
		coordinate,
		layerIndex,
		scene.Map.Map.ID,
		scene.Cleared,
	); err != nil {
		return DungeonRoomScene{}, err
	}
	s.installSceneLocked(scene)
	return cloneDungeonRoomScene(s.scene), nil
}

func (s *DungeonSession) previewLayeredLocked(
	coordinate RoomCoordinate,
	layerIndex int,
) (DungeonRoomScene, error) {
	room, err := s.run.previewCurrentLayered(coordinate, layerIndex)
	if err != nil {
		return DungeonRoomScene{}, err
	}
	scene, err := dungeonRoomScene(room)
	if err != nil {
		return DungeonRoomScene{}, err
	}
	return scene, nil
}

// ValidateLayeredBase validates a caller-owned cached base scene for the
// current coordinate after the explicit PVF layer list is exhausted. The
// cached scene must match the topology-owned base map; a stale or foreign
// cache fails closed. The run, active scene, runtime bindings, and defeated
// set are not mutated.
func (s *DungeonSession) ValidateLayeredBase(
	coordinate RoomCoordinate,
	cachedScene DungeonRoomScene,
) (DungeonRoomScene, error) {
	if s == nil || s.run == nil {
		return DungeonRoomScene{}, ErrDungeonRunRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	scene, _, _, err := s.validateLayeredBaseLocked(coordinate, cachedScene)
	if err != nil {
		return DungeonRoomScene{}, err
	}
	return cloneDungeonRoomScene(scene), nil
}

// CommitLayeredBase restores a caller-owned cached base scene after the
// explicit PVF layer list is exhausted. Like the revisit restore path, the
// cached runtime bindings and defeats are reinstalled so the base room's
// prior combat state survives the layered detour; the coordinate's clear bit
// is replaced with the validated base room state.
func (s *DungeonSession) CommitLayeredBase(
	coordinate RoomCoordinate,
	cachedScene DungeonRoomScene,
) (DungeonRoomScene, error) {
	if s == nil || s.run == nil {
		return DungeonRoomScene{}, ErrDungeonRunRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	scene, bindings, defeated, err := s.validateLayeredBaseLocked(coordinate, cachedScene)
	if err != nil {
		return DungeonRoomScene{}, err
	}
	if _, err := s.run.commitCurrentLayeredBase(coordinate, scene.Map.Map.ID, scene.Cleared); err != nil {
		return DungeonRoomScene{}, err
	}
	s.installRestoredSceneLocked(scene, bindings, defeated)
	return cloneDungeonRoomScene(s.scene), nil
}

func (s *DungeonSession) validateLayeredBaseLocked(
	coordinate RoomCoordinate,
	cachedScene DungeonRoomScene,
) (DungeonRoomScene, map[HostileReference]uint32, map[uint32]struct{}, error) {
	baseRoom, _, err := s.run.previewCurrentLayeredBase(coordinate)
	if err != nil {
		return DungeonRoomScene{}, nil, nil, err
	}
	expected, err := dungeonRoomScene(baseRoom)
	if err != nil {
		return DungeonRoomScene{}, nil, nil, err
	}
	// A layered commit replaced the coordinate's clear bit with the layer's
	// state, so the cached base scene is the only surviving record of the base
	// room's clear state. The revisit validator's strict structural checks
	// still apply; its persistent clear-state input is the cached scene itself.
	return validateRevisitDungeonRoomScene(expected, cachedScene, cachedScene.Cleared)
}

// PreviewMoveByteTransition additionally reports whether the target was
// already visited. Its Scene is the current PVF definition used to validate a
// caller-owned cached scene; it intentionally contains no reconstructed prior
// runtime objects.
func (s *DungeonSession) PreviewMoveByteTransition(nextX, nextY byte) (DungeonRoomTransition, error) {
	if s == nil || s.run == nil {
		return DungeonRoomTransition{}, ErrDungeonRunRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	transition, _, err := s.previewMoveByteTransitionLocked(nextX, nextY)
	return transition, err
}

func (s *DungeonSession) previewMoveByteTransitionLocked(nextX, nextY byte) (DungeonRoomTransition, bool, error) {
	target := RoomCoordinate{X: int64(nextX), Y: int64(nextY)}
	room, revisit, targetCleared, err := s.run.previewMoveToVisit(target)
	if err != nil {
		return DungeonRoomTransition{}, false, err
	}
	scene, err := dungeonRoomScene(room)
	if err != nil {
		return DungeonRoomTransition{}, false, err
	}
	return DungeonRoomTransition{Scene: scene, Revisit: revisit}, targetCleared, nil
}

func (s *DungeonSession) MoveByte(nextX, nextY byte) (DungeonRoomScene, error) {
	transition, err := s.MoveByteTransition(nextX, nextY, nil)
	return transition.Scene, err
}

// ValidateMoveByteTransition runs the complete first-visit/revisit validation
// used by MoveByteTransition without changing the run, active scene, runtime
// bindings, or defeated set. Packet owners call this before writing op29.
func (s *DungeonSession) ValidateMoveByteTransition(
	nextX, nextY byte,
	revisitScene *DungeonRoomScene,
) (DungeonRoomTransition, error) {
	if s == nil || s.run == nil {
		return DungeonRoomTransition{}, ErrDungeonRunRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	validated, err := s.validateMoveByteTransitionLocked(nextX, nextY, revisitScene)
	if err != nil {
		return DungeonRoomTransition{}, err
	}
	validated.transition.Scene = cloneDungeonRoomScene(validated.transition.Scene)
	return validated.transition, nil
}

// MoveByteTransition commits a first visit from PVF data or restores a caller-
// owned cached scene for a revisit. A revisit without its exact prior scene is
// rejected so the persistent DungeonRun.clear state can never be combined
// with freshly respawned actors.
func (s *DungeonSession) MoveByteTransition(
	nextX, nextY byte,
	revisitScene *DungeonRoomScene,
) (DungeonRoomTransition, error) {
	if s == nil || s.run == nil {
		return DungeonRoomTransition{}, ErrDungeonRunRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	validated, err := s.validateMoveByteTransitionLocked(nextX, nextY, revisitScene)
	if err != nil {
		return DungeonRoomTransition{}, err
	}
	scene := validated.transition.Scene
	expectedRevisit := validated.transition.Revisit
	if _, _, err := s.run.moveToVisit(scene.Coordinate, scene.Cleared, &expectedRevisit); err != nil {
		return DungeonRoomTransition{}, err
	}
	if validated.transition.Revisit {
		s.installRestoredSceneLocked(scene, validated.bindings, validated.defeated)
	} else {
		s.installSceneLocked(scene)
	}
	return DungeonRoomTransition{Scene: cloneDungeonRoomScene(s.scene), Revisit: validated.transition.Revisit}, nil
}

func (s *DungeonSession) validateMoveByteTransitionLocked(
	nextX, nextY byte,
	revisitScene *DungeonRoomScene,
) (validatedDungeonRoomTransition, error) {
	preview, targetCleared, err := s.previewMoveByteTransitionLocked(nextX, nextY)
	if err != nil {
		return validatedDungeonRoomTransition{}, err
	}

	validated := validatedDungeonRoomTransition{
		transition: preview,
		bindings:   make(map[HostileReference]uint32, len(preview.Scene.ExpectedHostiles)),
		defeated:   make(map[uint32]struct{}, len(preview.Scene.ExpectedHostiles)),
	}
	if preview.Revisit {
		if revisitScene == nil {
			return validatedDungeonRoomTransition{}, fmt.Errorf(
				"%w: room=%s map=%d",
				ErrRevisitSceneRequired,
				preview.Scene.Coordinate,
				preview.Scene.Map.Map.ID,
			)
		}
		validated.transition.Scene, validated.bindings, validated.defeated, err = validateRevisitDungeonRoomScene(
			preview.Scene,
			*revisitScene,
			targetCleared,
		)
		if err != nil {
			return validatedDungeonRoomTransition{}, err
		}
	} else if revisitScene != nil {
		return validatedDungeonRoomTransition{}, fmt.Errorf(
			"%w: room=%s map=%d",
			ErrRevisitSceneUnexpected,
			preview.Scene.Coordinate,
			preview.Scene.Map.Map.ID,
		)
	}
	return validated, nil
}

// Abandon ends the active run while keeping DungeonRun ownership behind the
// session lock. Callers use this for an explicit mid-dungeon return to town.
func (s *DungeonSession) Abandon() error {
	if s == nil || s.run == nil {
		return ErrDungeonRunRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.run.Abandon()
}

// CompleteCurrentRoom commits an authoritative scripted final-room completion
// without fabricating actor deaths. The bridge must validate the final-room
// protocol event before calling this lifecycle method.
func (s *DungeonSession) CompleteCurrentRoom() error {
	if s == nil || s.run == nil {
		return ErrDungeonRunRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.run.CompleteCurrentRoom(); err != nil {
		return err
	}
	s.scene.Cleared = true
	return nil
}

// Complete closes an ordinary final room after its blocking actors have
// authoritatively cleared it. Unlike CompleteCurrentRoom, this method does not
// bypass the run's current-room-cleared precondition.
func (s *DungeonSession) Complete() error {
	if s == nil || s.run == nil {
		return ErrDungeonRunRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.run.Complete()
}

func (s *DungeonSession) Snapshot() DungeonSessionSnapshot {
	if s == nil || s.run == nil {
		return DungeonSessionSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return DungeonSessionSnapshot{Run: s.run.Snapshot(), Scene: cloneDungeonRoomScene(s.scene)}
}

func (s *DungeonSession) loadRoom(room DungeonRoom) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadRoomLocked(room)
}

func (s *DungeonSession) loadRoomLocked(room DungeonRoom) error {
	scene, err := dungeonRoomScene(room)
	if err != nil {
		return err
	}
	if scene.Cleared {
		if err := s.run.MarkCurrentRoomCleared(); err != nil {
			return err
		}
	}
	s.installSceneLocked(scene)
	return nil
}

func (s *DungeonSession) installSceneLocked(scene DungeonRoomScene) {
	s.scene = scene
	s.bindings = make(map[HostileReference]uint32, len(scene.ExpectedHostiles))
	s.defeated = make(map[uint32]struct{}, len(scene.ExpectedHostiles))
}

func (s *DungeonSession) installRestoredSceneLocked(
	scene DungeonRoomScene,
	bindings map[HostileReference]uint32,
	defeated map[uint32]struct{},
) {
	s.scene = scene
	s.bindings = bindings
	s.defeated = defeated
}

func validateRevisitDungeonRoomScene(
	expected DungeonRoomScene,
	cached DungeonRoomScene,
	runCleared bool,
) (DungeonRoomScene, map[HostileReference]uint32, map[uint32]struct{}, error) {
	if !sameDungeonRoomPVFScene(expected, cached) {
		return DungeonRoomScene{}, nil, nil, fmt.Errorf(
			"%w: room=%s expected_map=%d cached_room=%s cached_map=%d reason=pvf_scene_changed",
			ErrRevisitSceneMismatch,
			expected.Coordinate,
			expected.Map.Map.ID,
			cached.Coordinate,
			cached.Map.Map.ID,
		)
	}

	restored := cloneDungeonRoomScene(cached)
	bindings := make(map[HostileReference]uint32, len(restored.RuntimeObjects))
	for objectKey, reference := range restored.RuntimeObjects {
		if objectKey == 0 || !containsHostileReference(restored.ExpectedHostiles, reference) {
			return DungeonRoomScene{}, nil, nil, fmt.Errorf(
				"%w: room=%s object_key=%d kind=%s index=%d reason=invalid_runtime_binding",
				ErrRevisitSceneMismatch,
				restored.Coordinate,
				objectKey,
				reference.Kind,
				reference.Index,
			)
		}
		if previous, exists := bindings[reference]; exists {
			return DungeonRoomScene{}, nil, nil, fmt.Errorf(
				"%w: room=%s kind=%s index=%d object_key=%d previous_object_key=%d reason=duplicate_hostile_binding",
				ErrRevisitSceneMismatch,
				restored.Coordinate,
				reference.Kind,
				reference.Index,
				objectKey,
				previous,
			)
		}
		bindings[reference] = objectKey
	}
	for _, reference := range restored.ExpectedHostiles {
		if _, bound := bindings[reference]; !bound {
			return DungeonRoomScene{}, nil, nil, fmt.Errorf(
				"%w: room=%s kind=%s index=%d reason=expected_hostile_unbound",
				ErrRevisitSceneMismatch,
				restored.Coordinate,
				reference.Kind,
				reference.Index,
			)
		}
	}

	defeated := make(map[uint32]struct{}, len(restored.DefeatedObjects))
	for _, objectKey := range restored.DefeatedObjects {
		if _, bound := restored.RuntimeObjects[objectKey]; !bound {
			return DungeonRoomScene{}, nil, nil, fmt.Errorf(
				"%w: room=%s object_key=%d reason=defeated_object_unbound",
				ErrRevisitSceneMismatch,
				restored.Coordinate,
				objectKey,
			)
		}
		if _, duplicate := defeated[objectKey]; duplicate {
			return DungeonRoomScene{}, nil, nil, fmt.Errorf(
				"%w: room=%s object_key=%d reason=duplicate_defeated_object",
				ErrRevisitSceneMismatch,
				restored.Coordinate,
				objectKey,
			)
		}
		defeated[objectKey] = struct{}{}
	}

	computedCleared := len(restored.BlockingHostiles) == 0
	if len(restored.BlockingHostiles) != 0 {
		computedCleared = true
		for _, reference := range restored.BlockingHostiles {
			objectKey, bound := bindings[reference]
			if !bound {
				computedCleared = false
				break
			}
			if _, dead := defeated[objectKey]; !dead {
				computedCleared = false
				break
			}
		}
	}
	if restored.Cleared != computedCleared || restored.Cleared != runCleared {
		return DungeonRoomScene{}, nil, nil, fmt.Errorf(
			"%w: room=%s cached_cleared=%t computed_cleared=%t run_cleared=%t reason=clear_state_conflict",
			ErrRevisitSceneMismatch,
			restored.Coordinate,
			restored.Cleared,
			computedCleared,
			runCleared,
		)
	}
	return restored, bindings, defeated, nil
}

func sameDungeonRoomPVFScene(expected, cached DungeonRoomScene) bool {
	expected = cloneDungeonRoomScene(expected)
	cached = cloneDungeonRoomScene(cached)
	expected.RuntimeObjects = nil
	cached.RuntimeObjects = nil
	expected.DefeatedObjects = nil
	cached.DefeatedObjects = nil
	expected.Cleared = false
	cached.Cleared = false
	return reflect.DeepEqual(expected, cached)
}

func dungeonRoomScene(room DungeonRoom) (DungeonRoomScene, error) {
	if room.Map == nil {
		return DungeonRoomScene{}, ErrRoomMapUnresolved
	}
	mapValue := cloneMap(room.Map.Map)
	scene := DungeonRoomScene{
		Coordinate:            room.Coordinate,
		Start:                 room.Start,
		Boss:                  room.Boss,
		Map:                   ResolvedMap{Map: mapValue, Source: room.Map.Source, SpecificationType: room.Map.SpecificationType},
		Monsters:              append([]MonsterSpawn(nil), mapValue.Monsters...),
		NPCs:                  cloneNPCSpawns(mapValue.NPCs),
		AICharacters:          cloneAICharacters(mapValue.AICharacters),
		PassiveObjects:        append([]PassiveObject(nil), mapValue.PassiveObjects...),
		SpecialPassiveObjects: cloneSpecialPassiveObjects(mapValue.SpecialPassiveObjects),
		Portals:               clonePortals(mapValue.Portals),
		Summons:               cloneMapSummons(mapValue.Summons),
		RuntimeObjects:        make(map[uint32]HostileReference),
	}
	for index := range scene.Monsters {
		reference := HostileReference{Kind: HostileMonster, Index: index}
		scene.ExpectedHostiles = append(scene.ExpectedHostiles, reference)
		if monsterSpawnBlocksRoom(mapValue, index) {
			scene.BlockingHostiles = append(scene.BlockingHostiles, reference)
		}
	}
	for index, actor := range scene.AICharacters {
		if strings.EqualFold(strings.Trim(actor.Faction, "[] \t"), "monster") {
			scene.ExpectedHostiles = append(scene.ExpectedHostiles, HostileReference{Kind: HostileAICharacter, Index: index})
		}
	}
	if len(scene.BlockingHostiles) == 0 {
		scene.Cleared = true
	}
	return scene, nil
}

// A map's [monster team] row is positional to [monster]. Team 0 is the
// player/friendly story side and must not keep room doors or tutorial
// settlement blocked. Missing entries retain the historical hostile default.
func monsterSpawnBlocksRoom(mapValue Map, monsterIndex int) bool {
	if monsterIndex < 0 || monsterIndex >= len(mapValue.MonsterTeam) {
		return true
	}
	return mapValue.MonsterTeam[monsterIndex] != 0
}

func containsHostileReference(values []HostileReference, want HostileReference) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cloneDungeonRoomScene(in DungeonRoomScene) DungeonRoomScene {
	in.Map.Map = cloneMap(in.Map.Map)
	in.Monsters = append([]MonsterSpawn(nil), in.Monsters...)
	in.NPCs = cloneNPCSpawns(in.NPCs)
	in.AICharacters = cloneAICharacters(in.AICharacters)
	in.PassiveObjects = append([]PassiveObject(nil), in.PassiveObjects...)
	in.SpecialPassiveObjects = cloneSpecialPassiveObjects(in.SpecialPassiveObjects)
	in.Portals = clonePortals(in.Portals)
	in.Summons = cloneMapSummons(in.Summons)
	in.ExpectedHostiles = append([]HostileReference(nil), in.ExpectedHostiles...)
	in.BlockingHostiles = append([]HostileReference(nil), in.BlockingHostiles...)
	in.DefeatedObjects = append([]uint32(nil), in.DefeatedObjects...)
	runtimeObjects := in.RuntimeObjects
	in.RuntimeObjects = make(map[uint32]HostileReference, len(runtimeObjects))
	for key, value := range runtimeObjects {
		in.RuntimeObjects[key] = value
	}
	return in
}

func cloneNPCSpawns(in []NPCSpawn) []NPCSpawn {
	out := append([]NPCSpawn(nil), in...)
	for index := range out {
		out[index].Params = append([]int64(nil), out[index].Params...)
	}
	return out
}

func cloneAICharacters(in []AICharacter) []AICharacter {
	out := append([]AICharacter(nil), in...)
	for index := range out {
		out[index].Params = append([]int64(nil), out[index].Params...)
	}
	return out
}

func cloneSpecialPassiveObjects(in []SpecialPassiveObject) []SpecialPassiveObject {
	out := append([]SpecialPassiveObject(nil), in...)
	for index := range out {
		out[index].Spawns = append([]SpecialObjectSpawn(nil), out[index].Spawns...)
		out[index].HellParty = append([]HellPartyEntry(nil), out[index].HellParty...)
	}
	return out
}

func clonePortals(in []Portal) []Portal {
	out := append([]Portal(nil), in...)
	for index := range out {
		if out[index].Position != nil {
			position := *out[index].Position
			out[index].Position = &position
		}
	}
	return out
}

func cloneMapSummons(in []MapSummon) []MapSummon {
	out := append([]MapSummon(nil), in...)
	for index := range out {
		out[index].Position = append([]int64(nil), out[index].Position...)
		out[index].Info = cloneExtensions(out[index].Info)
		out[index].SourceSections = cloneSections(out[index].SourceSections)
		out[index].UnknownSections = cloneSections(out[index].UnknownSections)
		out[index].Extensions = cloneExtensions(out[index].Extensions)
	}
	return out
}
