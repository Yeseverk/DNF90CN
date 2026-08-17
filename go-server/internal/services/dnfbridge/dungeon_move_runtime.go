package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

var (
	errDungeonMoveRuntimeOwnerMismatch = errors.New("dnf dungeon move runtime owner mismatch")
	errDungeonMoveRequestTailUnknown   = errors.New("dnf dungeon move request has an unproven tail")
	errDungeonElevatorIncomplete       = errors.New("dnf dungeon elevator encounter is not complete")
)

type currentDungeonMovePlan struct {
	Source          worldmap.DungeonRoomScene
	Target          worldmap.DungeonRoomScene
	SourceVisit     *runtimeDungeonRoomVisit
	TargetVisit     *runtimeDungeonRoomVisit
	TargetRoom      *runtimeDungeonRoom
	NextObjectKey   uint32
	Seed            uint32
	Revisit         bool
	Layered         bool
	LayerReturn     bool
	LayerExit       bool
	LayerBaseAck    bool
	LayerIndex      int
	LayerBaseKey    runtimeDungeonRoomKey
	StoryStage      bool
	StoryStageIndex int
	Operation       byte
	PayloadMode     byte
}

type runtimeDungeonRoomKey struct {
	X     int64
	Y     int64
	MapID int64
}

type runtimeDungeonRoomVisit struct {
	Scene   worldmap.DungeonRoomScene
	Room    *runtimeDungeonRoom
	Seed    uint32
	DropRNG uint32
}

type runtimeDungeonLayerChain struct {
	BaseKey         runtimeDungeonRoomKey
	Consumed        bool
	FinalAckPending bool
}

func runtimeDungeonRoomKeyFromScene(scene worldmap.DungeonRoomScene) runtimeDungeonRoomKey {
	return runtimeDungeonRoomKey{
		X:     scene.Coordinate.X,
		Y:     scene.Coordinate.Y,
		MapID: scene.Map.Map.ID,
	}
}

func (runtime *runtimeDungeonState) currentDungeonRoomVisit(
	scene worldmap.DungeonRoomScene,
) (*runtimeDungeonRoomVisit, error) {
	if runtime == nil || runtime.Room == nil || runtime.Rooms == nil {
		return nil, errDungeonWorldMapUnavailable
	}
	key := runtimeDungeonRoomKeyFromScene(scene)
	visit := runtime.Rooms[key]
	if visit == nil || visit.Room == nil {
		return nil, fmt.Errorf("%w: room=%s map=%d visit_missing", errDungeonMoveRuntimeOwnerMismatch, scene.Coordinate, scene.Map.Map.ID)
	}
	snapshot := visit.Room.Snapshot()
	if visit.Room != runtime.Room || visit.Seed != runtime.Seed ||
		snapshot.Coordinate != scene.Coordinate || snapshot.MapID != scene.Map.Map.ID {
		return nil, fmt.Errorf(
			"%w: scene=%s/%d runtime_room=%p cached_room=%p cached_owner=%s/%d runtime_seed=%d cached_seed=%d",
			errDungeonMoveRuntimeOwnerMismatch,
			scene.Coordinate,
			scene.Map.Map.ID,
			runtime.Room,
			visit.Room,
			snapshot.Coordinate,
			snapshot.MapID,
			runtime.Seed,
			visit.Seed,
		)
	}
	return visit, nil
}

func (runtime *runtimeDungeonState) cacheCurrentDungeonRoom() error {
	if runtime == nil || runtime.Session == nil || runtime.Room == nil {
		return errDungeonWorldMapUnavailable
	}
	scene, ok := runtime.Session.Scene()
	if !ok {
		return worldmap.ErrDungeonRunRequired
	}
	snapshot := runtime.Room.Snapshot()
	if snapshot.Coordinate != scene.Coordinate || snapshot.MapID != scene.Map.Map.ID {
		return fmt.Errorf(
			"%w: session_room=%s session_map=%d runtime_room=%s runtime_map=%d",
			errDungeonMoveRuntimeOwnerMismatch,
			scene.Coordinate,
			scene.Map.Map.ID,
			snapshot.Coordinate,
			snapshot.MapID,
		)
	}
	if runtime.Rooms == nil {
		runtime.Rooms = make(map[runtimeDungeonRoomKey]*runtimeDungeonRoomVisit)
	}
	key := runtimeDungeonRoomKeyFromScene(scene)
	if previous := runtime.Rooms[key]; previous != nil && previous.Room != runtime.Room {
		return fmt.Errorf(
			"%w: room=%s map=%d cached_room=%p current_room=%p",
			errDungeonMoveRuntimeOwnerMismatch,
			scene.Coordinate,
			scene.Map.Map.ID,
			previous.Room,
			runtime.Room,
		)
	}
	// Dungeon drop rolls are room-scoped and sequential.  Preserve their LCG
	// state when caching a room after a death; resetting it to the start-map
	// seed would make every monster in the room receive the same roll sequence.
	dropRNG := runtime.Seed
	if previous := runtime.Rooms[key]; previous != nil && previous.Room == runtime.Room && previous.Seed == runtime.Seed {
		dropRNG = previous.DropRNG
	}
	runtime.Rooms[key] = &runtimeDungeonRoomVisit{Scene: scene, Room: runtime.Room, Seed: runtime.Seed, DropRNG: dropRNG}
	return nil
}

func (s *Service) planCurrentDungeonMove(
	runtime *runtimeDungeonState,
	request dungeoncmd.MoveMapRequest,
) (currentDungeonMovePlan, error) {
	if runtime == nil || runtime.Session == nil || runtime.Room == nil {
		return currentDungeonMovePlan{}, errDungeonWorldMapUnavailable
	}
	if len(request.OpaqueTail) != 0 {
		return currentDungeonMovePlan{}, fmt.Errorf("%w: bytes=%d", errDungeonMoveRequestTailUnknown, len(request.OpaqueTail))
	}

	source, ok := runtime.Session.Scene()
	if !ok {
		return currentDungeonMovePlan{}, worldmap.ErrDungeonRunRequired
	}
	sourceRoom := runtime.Room.Snapshot()
	if source.Coordinate != sourceRoom.Coordinate || source.Map.Map.ID != sourceRoom.MapID {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: session_room=%s session_map=%d runtime_room=%s runtime_map=%d",
			errDungeonMoveRuntimeOwnerMismatch,
			source.Coordinate,
			source.Map.Map.ID,
			sourceRoom.Coordinate,
			sourceRoom.MapID,
		)
	}
	if currentSuspiciousVillageElevatorMoveBlocked(runtime, source, sourceRoom) {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: dungeon=%d maze=%d room=%s map=%d counter=%d state=%d",
			errDungeonElevatorIncomplete,
			runtime.Dungeon.ID,
			runtime.MazeIndex,
			source.Coordinate,
			source.Map.Map.ID,
			runtime.suspiciousVillageElevator.Counter,
			runtime.suspiciousVillageElevator.State,
		)
	}
	sourceVisit, err := runtime.currentDungeonRoomVisit(source)
	if err != nil {
		return currentDungeonMovePlan{}, err
	}
	if stage, stageIndex, pending := currentDungeonNextStoryStage(runtime); pending {
		if int64(request.NextX) == stage.Coordinate.X && int64(request.NextY) == stage.Coordinate.Y {
			return s.planCurrentDungeonStoryStageMove(runtime, source, sourceVisit, stage, stageIndex)
		}
		if runtime.StoryStageIndex >= 0 {
			return currentDungeonMovePlan{}, fmt.Errorf(
				"%w: source=%s/%d story_stage=%d expected_target=%s requested_target=%d:%d",
				errDungeonMoveRuntimeOwnerMismatch,
				source.Coordinate,
				source.Map.Map.ID,
				stageIndex,
				stage.Coordinate,
				request.NextX,
				request.NextY,
			)
		}
	}
	if request.MoveKind == 1 &&
		int64(request.NextX) == source.Coordinate.X &&
		int64(request.NextY) == source.Coordinate.Y {
		return s.planCurrentDungeonLayerMove(runtime, request, source, sourceVisit)
	}
	layerExit, layerBaseKey, err := s.validateCurrentDungeonTerminalLayerExit(runtime, request, source)
	if err != nil {
		return currentDungeonMovePlan{}, err
	}

	preview, err := runtime.Session.PreviewMoveByteTransition(request.NextX, request.NextY)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("preview server-owned dungeon move: %w", err)
	}
	if preview.Revisit {
		targetVisit := runtime.Rooms[runtimeDungeonRoomKeyFromScene(preview.Scene)]
		if targetVisit == nil || targetVisit.Room == nil {
			return currentDungeonMovePlan{}, fmt.Errorf(
				"%w: target=%s/%d revisit_cache_missing",
				errDungeonMoveRuntimeOwnerMismatch,
				preview.Scene.Coordinate,
				preview.Scene.Map.Map.ID,
			)
		}
		validated, validateErr := runtime.Session.ValidateMoveByteTransition(
			request.NextX,
			request.NextY,
			&targetVisit.Scene,
		)
		if validateErr != nil {
			return currentDungeonMovePlan{}, fmt.Errorf("validate cached dungeon revisit: %w", validateErr)
		}
		targetSnapshot := targetVisit.Room.Snapshot()
		if targetSnapshot.Coordinate != validated.Scene.Coordinate || targetSnapshot.MapID != validated.Scene.Map.Map.ID {
			return currentDungeonMovePlan{}, fmt.Errorf(
				"%w: cached_scene=%s/%d cached_runtime=%s/%d",
				errDungeonMoveRuntimeOwnerMismatch,
				validated.Scene.Coordinate,
				validated.Scene.Map.Map.ID,
				targetSnapshot.Coordinate,
				targetSnapshot.MapID,
			)
		}
		return currentDungeonMovePlan{
			Source:        source,
			Target:        validated.Scene,
			SourceVisit:   sourceVisit,
			TargetVisit:   targetVisit,
			TargetRoom:    targetVisit.Room,
			NextObjectKey: runtime.NextObjectKey,
			Seed:          targetVisit.Seed,
			Revisit:       true,
			LayerExit:     layerExit,
			LayerIndex:    -1,
			LayerBaseKey:  layerBaseKey,
			Operation:     currentDungeonStartMapOperationCurrent,
			PayloadMode:   currentDungeonStartMapPayloadCached,
		}, nil
	}
	targetValidation, err := runtime.Session.ValidateMoveByteTransition(request.NextX, request.NextY, nil)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("validate first dungeon room visit: %w", err)
	}
	target := targetValidation.Scene
	if existing := runtime.Rooms[runtimeDungeonRoomKeyFromScene(target)]; existing != nil {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: target=%s/%d first_visit_cache_already_exists",
			errDungeonMoveRuntimeOwnerMismatch,
			target.Coordinate,
			target.Map.Map.ID,
		)
	}
	monsterCatalog, err := s.dungeonMonsterCatalog()
	if err != nil {
		return currentDungeonMovePlan{}, err
	}
	targetRoom, nextObjectKey, err := newRuntimeDungeonRoom(target, monsterCatalog, runtime.NextObjectKey)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("plan target dungeon monster owner: %w", err)
	}
	var aiCatalog *pvfDungeonAICharacterCatalog
	if len(target.AICharacters) != 0 {
		aiCatalog, err = s.dungeonAICharacterCatalog()
		if err != nil {
			return currentDungeonMovePlan{}, err
		}
	}
	extendedPlan, err := planRuntimeDungeonExtendedActors(
		target,
		monsterCatalog,
		aiCatalog,
		runtime.Dungeon.Metadata.BasisLevel,
		nextObjectKey,
	)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("plan target dungeon extended actor owner: %w", err)
	}
	if err := targetRoom.AttachExtendedActors(extendedPlan); err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("attach target dungeon extended actor owner: %w", err)
	}
	if _, err := s.configurePVFTutorialBasicActionRoom(runtime.Dungeon, target, targetRoom); err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("configure target tutorial basic-action room: %w", err)
	}
	seed, err := s.chooseDungeonSeed()
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("choose target dungeon room seed: %w", err)
	}

	return currentDungeonMovePlan{
		Source:        source,
		Target:        target,
		SourceVisit:   sourceVisit,
		TargetRoom:    targetRoom,
		NextObjectKey: extendedPlan.NextObjectKey,
		Seed:          seed,
		Revisit:       false,
		LayerExit:     layerExit,
		LayerIndex:    -1,
		LayerBaseKey:  layerBaseKey,
		Operation:     currentDungeonStartMapOperationCurrent,
		PayloadMode:   currentDungeonStartMapPayloadBuild,
	}, nil
}

// validateCurrentDungeonTerminalLayerExit allows the client to leave the last
// resolved layer through a normal explicit adjacent move. Earlier layers still
// require their same-coordinate MoveKind=1 transition, preventing a player
// from skipping a pending story layer.
func (s *Service) validateCurrentDungeonTerminalLayerExit(
	runtime *runtimeDungeonState,
	request dungeoncmd.MoveMapRequest,
	source worldmap.DungeonRoomScene,
) (bool, runtimeDungeonRoomKey, error) {
	if runtime == nil || !runtime.LayeredMapActive {
		return false, runtimeDungeonRoomKey{}, nil
	}
	if request.MoveKind != 0 ||
		(int64(request.NextX) == source.Coordinate.X && int64(request.NextY) == source.Coordinate.Y) {
		return false, runtimeDungeonRoomKey{}, fmt.Errorf(
			"%w: source=%s/%d active_layer_requires_terminal_explicit_target_or_same_coordinate_move_kind_1",
			errDungeonMoveRuntimeOwnerMismatch,
			source.Coordinate,
			source.Map.Map.ID,
		)
	}
	chain := runtime.LayerChains[source.Coordinate]
	if chain == nil || chain.Consumed || runtime.LayeredMapIndex < 0 {
		return false, runtimeDungeonRoomKey{}, fmt.Errorf(
			"%w: source=%s/%d active_layer_chain_unavailable",
			errDungeonMoveRuntimeOwnerMismatch,
			source.Coordinate,
			source.Map.Map.ID,
		)
	}
	if !source.Cleared {
		return false, runtimeDungeonRoomKey{}, fmt.Errorf(
			"%w: source=%s/%d layered_terminal_combat_gate_not_cleared",
			errDungeonMoveRuntimeOwnerMismatch,
			source.Coordinate,
			source.Map.Map.ID,
		)
	}
	_, resolver, err := s.dungeonWorldMap()
	if err != nil {
		return false, runtimeDungeonRoomKey{}, err
	}
	current, err := resolver.ResolveLayered(
		runtime.Dungeon.ID,
		runtime.MazeIndex,
		source.Coordinate,
		runtime.LayeredMapIndex,
	)
	if err != nil || current.Map.ID != source.Map.Map.ID {
		return false, runtimeDungeonRoomKey{}, fmt.Errorf(
			"%w: source=%s/%d active_layer_index=%d resolution_error=%v",
			errDungeonMoveRuntimeOwnerMismatch,
			source.Coordinate,
			source.Map.Map.ID,
			runtime.LayeredMapIndex,
			err,
		)
	}
	_, err = resolver.ResolveLayered(
		runtime.Dungeon.ID,
		runtime.MazeIndex,
		source.Coordinate,
		runtime.LayeredMapIndex+1,
	)
	if err == nil {
		return false, runtimeDungeonRoomKey{}, fmt.Errorf(
			"%w: source=%s/%d active_layer_has_pending_successor",
			errDungeonMoveRuntimeOwnerMismatch,
			source.Coordinate,
			source.Map.Map.ID,
		)
	}
	if !errors.Is(err, worldmap.ErrLayeredIndexInvalid) {
		return false, runtimeDungeonRoomKey{}, fmt.Errorf("resolve terminal layered dungeon map: %w", err)
	}
	return true, chain.BaseKey, nil
}

func currentDungeonNextStoryStage(
	runtime *runtimeDungeonState,
) (worldmap.DungeonStoryStage, int, bool) {
	if runtime == nil || len(runtime.StoryStages) == 0 {
		return worldmap.DungeonStoryStage{}, -1, false
	}
	nextIndex := runtime.StoryStageIndex + 1
	if nextIndex < 0 || nextIndex >= len(runtime.StoryStages) {
		return worldmap.DungeonStoryStage{}, nextIndex, false
	}
	stage := runtime.StoryStages[nextIndex]
	if stage.Index != nextIndex || stage.MapID <= 0 {
		return worldmap.DungeonStoryStage{}, nextIndex, false
	}
	return stage, nextIndex, true
}

func (s *Service) planCurrentDungeonStoryStageMove(
	runtime *runtimeDungeonState,
	source worldmap.DungeonRoomScene,
	sourceVisit *runtimeDungeonRoomVisit,
	stage worldmap.DungeonStoryStage,
	stageIndex int,
) (currentDungeonMovePlan, error) {
	if runtime == nil || runtime.Session == nil || runtime.Room == nil || sourceVisit == nil || runtime.LayeredMapActive {
		return currentDungeonMovePlan{}, errDungeonWorldMapUnavailable
	}
	transition, err := runtime.Session.PreviewStoryStage(stageIndex)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("preview dungeon story stage: %w", err)
	}
	target := transition.Scene
	if target.Coordinate != stage.Coordinate || target.Map.Map.ID != stage.MapID {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: story_stage=%d runtime=%s/%d session=%s/%d",
			errDungeonMoveRuntimeOwnerMismatch,
			stageIndex,
			stage.Coordinate,
			stage.MapID,
			target.Coordinate,
			target.Map.Map.ID,
		)
	}
	_, resolver, err := s.dungeonWorldMap()
	if err != nil {
		return currentDungeonMovePlan{}, err
	}
	resolved, descriptor, err := resolver.ResolveStoryStage(runtime.Dungeon.ID, runtime.MazeIndex, stageIndex)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("resolve dungeon story stage: %w", err)
	}
	if descriptor != stage || resolved.Map.ID != stage.MapID {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: story_stage=%d cached=%+v resolved=%+v map_id=%d",
			errDungeonMoveRuntimeOwnerMismatch,
			stageIndex,
			stage,
			descriptor,
			resolved.Map.ID,
		)
	}
	if existing := runtime.Rooms[runtimeDungeonRoomKeyFromScene(target)]; existing != nil {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: target=%s/%d story_stage_visit_cache_already_exists",
			errDungeonMoveRuntimeOwnerMismatch,
			target.Coordinate,
			target.Map.Map.ID,
		)
	}
	monsterCatalog, err := s.dungeonMonsterCatalog()
	if err != nil {
		return currentDungeonMovePlan{}, err
	}
	targetRoom, nextObjectKey, err := newRuntimeDungeonRoom(target, monsterCatalog, runtime.NextObjectKey)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("plan dungeon story-stage monster owner: %w", err)
	}
	var aiCatalog *pvfDungeonAICharacterCatalog
	if len(target.AICharacters) != 0 {
		aiCatalog, err = s.dungeonAICharacterCatalog()
		if err != nil {
			return currentDungeonMovePlan{}, err
		}
	}
	extendedPlan, err := planRuntimeDungeonExtendedActors(
		target,
		monsterCatalog,
		aiCatalog,
		runtime.Dungeon.Metadata.BasisLevel,
		nextObjectKey,
	)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("plan dungeon story-stage extended actor owner: %w", err)
	}
	if err := targetRoom.AttachExtendedActors(extendedPlan); err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("attach dungeon story-stage extended actor owner: %w", err)
	}
	if _, err := s.configurePVFTutorialBasicActionRoom(runtime.Dungeon, target, targetRoom); err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("configure dungeon story-stage basic-action room: %w", err)
	}
	seed, err := s.chooseDungeonSeed()
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("choose dungeon story-stage seed: %w", err)
	}
	payloadMode := currentDungeonStartMapPayloadBuild
	if transition.Revisit {
		payloadMode = currentDungeonStartMapPayloadRefresh
	}
	return currentDungeonMovePlan{
		Source:          source,
		Target:          target,
		SourceVisit:     sourceVisit,
		TargetRoom:      targetRoom,
		NextObjectKey:   extendedPlan.NextObjectKey,
		Seed:            seed,
		Revisit:         transition.Revisit,
		LayerIndex:      -1,
		StoryStage:      true,
		StoryStageIndex: stageIndex,
		Operation:       currentDungeonStartMapOperationAdvanceLayer,
		PayloadMode:     payloadMode,
	}, nil
}

// planCurrentDungeonLayerMove owns the current-EXE's explicit same-coordinate
// MOVE_MAP form.  The live client sets MoveKind=1 when advancing a PVF
// [layered map specification]; ordinary same-coordinate moves continue through
// DungeonRun's adjacency validator and remain rejected.
func (s *Service) planCurrentDungeonLayerMove(
	runtime *runtimeDungeonState,
	request dungeoncmd.MoveMapRequest,
	source worldmap.DungeonRoomScene,
	sourceVisit *runtimeDungeonRoomVisit,
) (currentDungeonMovePlan, error) {
	if runtime == nil || runtime.Session == nil || runtime.Room == nil || sourceVisit == nil {
		return currentDungeonMovePlan{}, errDungeonWorldMapUnavailable
	}
	chain := runtime.LayerChains[source.Coordinate]
	if chain != nil && chain.Consumed {
		if chain.FinalAckPending && !runtime.LayeredMapActive {
			return s.planCurrentDungeonLayerBaseAck(runtime, source, sourceVisit, chain)
		}
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: target=%s/%d layered_transition_already_consumed",
			errDungeonMoveRuntimeOwnerMismatch,
			source.Coordinate,
			source.Map.Map.ID,
		)
	}
	nextLayerIndex := 0
	if runtime.LayeredMapActive {
		if chain == nil {
			return currentDungeonMovePlan{}, fmt.Errorf(
				"%w: target=%s active_layer_chain_missing",
				errDungeonMoveRuntimeOwnerMismatch,
				source.Coordinate,
			)
		}
		nextLayerIndex = runtime.LayeredMapIndex + 1
	}
	_, resolver, err := s.dungeonWorldMap()
	if err != nil {
		return currentDungeonMovePlan{}, err
	}
	resolved, err := resolver.ResolveLayered(
		runtime.Dungeon.ID,
		runtime.MazeIndex,
		source.Coordinate,
		nextLayerIndex,
	)
	if runtime.LayeredMapActive && errors.Is(err, worldmap.ErrLayeredIndexInvalid) {
		return s.planCurrentDungeonLayerReturn(runtime, source, sourceVisit, chain)
	}
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("resolve explicit layered dungeon map: %w", err)
	}
	baseKey := runtimeDungeonRoomKeyFromScene(source)
	if chain != nil {
		baseKey = chain.BaseKey
	} else {
		base, baseErr := runtime.Session.ValidateLayeredBase(source.Coordinate, sourceVisit.Scene)
		if baseErr != nil {
			return currentDungeonMovePlan{}, fmt.Errorf("validate layered dungeon base before entry: %w", baseErr)
		}
		if base.Coordinate != source.Coordinate || base.Map.Map.ID != source.Map.Map.ID {
			return currentDungeonMovePlan{}, fmt.Errorf(
				"%w: layered_base=%s/%d source=%s/%d",
				errDungeonMoveRuntimeOwnerMismatch,
				base.Coordinate,
				base.Map.Map.ID,
				source.Coordinate,
				source.Map.Map.ID,
			)
		}
	}
	target, err := runtime.Session.PreviewLayered(source.Coordinate, nextLayerIndex)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("preview explicit layered dungeon move: %w", err)
	}
	if target.Coordinate != source.Coordinate || target.Map.Map.ID != resolved.Map.ID {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: layered_resolver=%s/%d layered_session=%s/%d",
			errDungeonMoveRuntimeOwnerMismatch,
			source.Coordinate,
			resolved.Map.ID,
			target.Coordinate,
			target.Map.Map.ID,
		)
	}
	if existing := runtime.Rooms[runtimeDungeonRoomKeyFromScene(target)]; existing != nil {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: target=%s/%d layered_visit_cache_already_exists",
			errDungeonMoveRuntimeOwnerMismatch,
			target.Coordinate,
			target.Map.Map.ID,
		)
	}
	monsterCatalog, err := s.dungeonMonsterCatalog()
	if err != nil {
		return currentDungeonMovePlan{}, err
	}
	targetRoom, nextObjectKey, err := newRuntimeDungeonRoom(target, monsterCatalog, runtime.NextObjectKey)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("plan layered dungeon monster owner: %w", err)
	}
	var aiCatalog *pvfDungeonAICharacterCatalog
	if len(target.AICharacters) != 0 {
		aiCatalog, err = s.dungeonAICharacterCatalog()
		if err != nil {
			return currentDungeonMovePlan{}, err
		}
	}
	extendedPlan, err := planRuntimeDungeonExtendedActors(
		target,
		monsterCatalog,
		aiCatalog,
		runtime.Dungeon.Metadata.BasisLevel,
		nextObjectKey,
	)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("plan layered dungeon extended actor owner: %w", err)
	}
	if err := targetRoom.AttachExtendedActors(extendedPlan); err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("attach layered dungeon extended actor owner: %w", err)
	}
	if _, err := s.configurePVFTutorialBasicActionRoom(runtime.Dungeon, target, targetRoom); err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("configure layered tutorial basic-action room: %w", err)
	}
	seed, err := s.chooseDungeonSeed()
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("choose layered dungeon room seed: %w", err)
	}
	return currentDungeonMovePlan{
		Source:        source,
		Target:        target,
		SourceVisit:   sourceVisit,
		TargetRoom:    targetRoom,
		NextObjectKey: extendedPlan.NextObjectKey,
		Seed:          seed,
		Layered:       true,
		LayerIndex:    nextLayerIndex,
		LayerBaseKey:  baseKey,
		Operation:     currentDungeonStartMapOperationAdvanceLayer,
		PayloadMode: func() byte {
			if nextLayerIndex == 0 {
				return currentDungeonStartMapPayloadBuild
			}
			return currentDungeonStartMapPayloadRefresh
		}(),
	}, nil
}

// planCurrentDungeonLayerBaseAck owns the final automatic CHANGE MAP request
// emitted after flag 1 has restored the same-coordinate base room. The client
// is already in the correct room, so this is a short ordinary-room revisit ACK
// and must not rebuild actors or restart layer zero.
func (s *Service) planCurrentDungeonLayerBaseAck(
	runtime *runtimeDungeonState,
	source worldmap.DungeonRoomScene,
	sourceVisit *runtimeDungeonRoomVisit,
	chain *runtimeDungeonLayerChain,
) (currentDungeonMovePlan, error) {
	if runtime == nil || runtime.Session == nil || runtime.Room == nil || sourceVisit == nil || chain == nil {
		return currentDungeonMovePlan{}, errDungeonWorldMapUnavailable
	}
	if !chain.Consumed || !chain.FinalAckPending || runtime.LayeredMapActive {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: layered base ACK requested without completed layer return",
			errDungeonMoveRuntimeOwnerMismatch,
		)
	}
	if runtimeDungeonRoomKeyFromScene(source) != chain.BaseKey ||
		sourceVisit.Room != runtime.Room || sourceVisit.Seed != runtime.Seed {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: layered_base_ack source=%s/%d base=%s/%d",
			errDungeonMoveRuntimeOwnerMismatch,
			source.Coordinate,
			source.Map.Map.ID,
			worldmap.RoomCoordinate{X: chain.BaseKey.X, Y: chain.BaseKey.Y},
			chain.BaseKey.MapID,
		)
	}
	return currentDungeonMovePlan{
		Source:        source,
		Target:        source,
		SourceVisit:   sourceVisit,
		TargetVisit:   sourceVisit,
		TargetRoom:    sourceVisit.Room,
		NextObjectKey: runtime.NextObjectKey,
		Seed:          sourceVisit.Seed,
		Revisit:       true,
		LayerBaseAck:  true,
		LayerIndex:    -1,
		LayerBaseKey:  chain.BaseKey,
		Operation:     currentDungeonStartMapOperationRestoreBase,
		PayloadMode:   currentDungeonStartMapPayloadCached,
	}, nil
}

// planCurrentDungeonLayerReturn restores the cached topology-owned base room
// after the explicit layer list at the current coordinate is exhausted.
func (s *Service) planCurrentDungeonLayerReturn(
	runtime *runtimeDungeonState,
	source worldmap.DungeonRoomScene,
	sourceVisit *runtimeDungeonRoomVisit,
	chain *runtimeDungeonLayerChain,
) (currentDungeonMovePlan, error) {
	if runtime == nil || runtime.Session == nil || runtime.Room == nil || sourceVisit == nil || chain == nil {
		return currentDungeonMovePlan{}, errDungeonWorldMapUnavailable
	}
	if !runtime.LayeredMapActive || runtime.LayeredMapIndex < 0 || chain.Consumed {
		return currentDungeonMovePlan{}, fmt.Errorf("%w: layered base return requested without an active layer", errDungeonMoveRuntimeOwnerMismatch)
	}
	targetVisit := runtime.Rooms[chain.BaseKey]
	if targetVisit == nil || targetVisit.Room == nil {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: target=%s/%d layered_base_cache_missing",
			errDungeonMoveRuntimeOwnerMismatch,
			worldmap.RoomCoordinate{X: chain.BaseKey.X, Y: chain.BaseKey.Y},
			chain.BaseKey.MapID,
		)
	}
	target, err := runtime.Session.ValidateLayeredBase(source.Coordinate, targetVisit.Scene)
	if err != nil {
		return currentDungeonMovePlan{}, fmt.Errorf("validate cached layered dungeon base: %w", err)
	}
	if runtimeDungeonRoomKeyFromScene(target) != chain.BaseKey {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: cached_base=%s/%d topology_base=%s/%d",
			errDungeonMoveRuntimeOwnerMismatch,
			worldmap.RoomCoordinate{X: chain.BaseKey.X, Y: chain.BaseKey.Y},
			chain.BaseKey.MapID,
			target.Coordinate,
			target.Map.Map.ID,
		)
	}
	targetSnapshot := targetVisit.Room.Snapshot()
	if targetSnapshot.Coordinate != target.Coordinate || targetSnapshot.MapID != target.Map.Map.ID {
		return currentDungeonMovePlan{}, fmt.Errorf(
			"%w: cached_scene=%s/%d cached_runtime=%s/%d",
			errDungeonMoveRuntimeOwnerMismatch,
			target.Coordinate,
			target.Map.Map.ID,
			targetSnapshot.Coordinate,
			targetSnapshot.MapID,
		)
	}
	return currentDungeonMovePlan{
		Source:        source,
		Target:        target,
		SourceVisit:   sourceVisit,
		TargetVisit:   targetVisit,
		TargetRoom:    targetVisit.Room,
		NextObjectKey: runtime.NextObjectKey,
		Seed:          targetVisit.Seed,
		LayerReturn:   true,
		LayerIndex:    -1,
		LayerBaseKey:  chain.BaseKey,
		Operation:     currentDungeonStartMapOperationRestoreBase,
		PayloadMode:   currentDungeonStartMapPayloadCached,
	}, nil
}

// currentDungeonPendingLayer reports whether the active PVF maze has another
// explicit layer at the current coordinate. Missing specifications and a
// consumed layer list are normal terminal states; ambiguous or unresolved PVF
// data fails closed so settlement cannot skip an intended story layer.
func (s *Service) currentDungeonPendingLayer(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
) (bool, int, int64, error) {
	if runtime == nil || runtime.Session == nil {
		return false, -1, 0, errDungeonWorldMapUnavailable
	}
	if chain := runtime.LayerChains[scene.Coordinate]; chain != nil && chain.Consumed {
		return false, -1, 0, nil
	}
	if !currentDungeonCoordinateDeclaresLayer(runtime, scene.Coordinate) {
		return false, -1, 0, nil
	}
	nextLayerIndex := 0
	if runtime.LayeredMapActive {
		nextLayerIndex = runtime.LayeredMapIndex + 1
	}
	_, resolver, err := s.dungeonWorldMap()
	if err != nil {
		return false, nextLayerIndex, 0, err
	}
	resolved, err := resolver.ResolveLayered(
		runtime.Dungeon.ID,
		runtime.MazeIndex,
		scene.Coordinate,
		nextLayerIndex,
	)
	if errors.Is(err, worldmap.ErrLayeredSpecMissing) || errors.Is(err, worldmap.ErrLayeredIndexInvalid) {
		return false, nextLayerIndex, 0, nil
	}
	if err != nil {
		return false, nextLayerIndex, 0, fmt.Errorf("resolve pending layered dungeon map: %w", err)
	}
	if !runtime.LayeredMapActive {
		catalog, catalogErr := s.loadQuestCatalog(context.Background())
		if catalogErr == nil && currentDungeonConnectedClearMapQuestOwnsBaseMap(
			runtime,
			scene.Map.Map.ID,
			resolved.Map.ID,
			catalog,
		) {
			return false, nextLayerIndex, resolved.Map.ID, nil
		}
	}
	return true, nextLayerIndex, resolved.Map.ID, nil
}

// currentDungeonConnectedClearMapQuestOwnsBaseMap recognizes the narrow PVF
// case where a maze declares a story layer but its active clear-map quest
// explicitly targets the base map. Unknown or multi-target definitions remain
// pending so an intended story layer cannot be skipped.
func currentDungeonConnectedClearMapQuestOwnsBaseMap(
	runtime *runtimeDungeonState,
	currentMapID int64,
	nextLayerMapID int64,
	catalog *dnfquest.Catalog,
) bool {
	if runtime == nil || catalog == nil || runtime.LayeredMapActive ||
		runtime.MazeIndex < 0 || runtime.MazeIndex >= len(runtime.Dungeon.Mazes) {
		return false
	}
	connection := runtime.Dungeon.Mazes[runtime.MazeIndex].QuestConnection
	if len(connection) < 2 || connection[0] != 0 || connection[1] <= 0 {
		return false
	}
	if currentMapID <= 0 || currentMapID == nextLayerMapID {
		return false
	}
	definition, ok := catalog.Find(connection[1])
	return ok &&
		normalizeDungeonPVFSymbol(definition.Type) == "clear map" &&
		len(definition.IntData) == 1 &&
		definition.IntData[0] == currentMapID
}

func currentDungeonCoordinateDeclaresLayer(
	runtime *runtimeDungeonState,
	coordinate worldmap.RoomCoordinate,
) bool {
	if runtime == nil || runtime.MazeIndex < 0 || runtime.MazeIndex >= len(runtime.Dungeon.Mazes) {
		return false
	}
	maze := runtime.Dungeon.Mazes[runtime.MazeIndex]
	for _, specification := range maze.MapSpecifications {
		if strings.EqualFold(strings.Trim(specification.Type, " []"), "layered") &&
			specification.Coordinate.X == coordinate.X && specification.Coordinate.Y == coordinate.Y {
			return true
		}
	}
	for _, specification := range maze.LayeredSpecifications {
		if specification.Coordinate.X == coordinate.X && specification.Coordinate.Y == coordinate.Y {
			return true
		}
	}
	return false
}

func buildCurrentDungeonMoveStartMapBody(
	runtime *runtimeDungeonState,
	plan currentDungeonMovePlan,
) ([]byte, error) {
	if runtime == nil || plan.TargetRoom == nil {
		return nil, errDungeonWorldMapUnavailable
	}
	if plan.Target.Coordinate.X < 0 || plan.Target.Coordinate.X > 0xff ||
		plan.Target.Coordinate.Y < 0 || plan.Target.Coordinate.Y > 0xff {
		return nil, fmt.Errorf("%w: room=%s", errDungeonStartMapCoordinateRange, plan.Target.Coordinate)
	}
	if plan.PayloadMode == currentDungeonStartMapPayloadCached {
		if plan.TargetVisit == nil || plan.TargetVisit.Room != plan.TargetRoom {
			return nil, fmt.Errorf("%w: cached target owner missing", errDungeonMoveRuntimeOwnerMismatch)
		}
		body, err := (currentDungeonCachedStartMap{
			X:                byte(plan.Target.Coordinate.X),
			Y:                byte(plan.Target.Coordinate.Y),
			Operation:        plan.Operation,
			Seed:             plan.Seed,
			RoomStateValue:   1,
			PartyMemberIndex: currentDungeonRuntimePartyMemberIndex(runtime),
		}).Build()
		if err != nil {
			return nil, fmt.Errorf("build cached target-room start-map body: %w", err)
		}
		return body, nil
	}
	if plan.PayloadMode != currentDungeonStartMapPayloadBuild &&
		plan.PayloadMode != currentDungeonStartMapPayloadRefresh {
		return nil, fmt.Errorf("unsupported full start-map payload mode=%d", plan.PayloadMode)
	}
	targetRuntime := *runtime
	targetRuntime.Room = plan.TargetRoom
	targetRuntime.NextObjectKey = plan.NextObjectKey
	targetRuntime.Seed = plan.Seed
	packet, err := currentDungeonStartMapFromRuntime(&targetRuntime, plan.Target, currentDungeonStartMapState{
		LayeredRoomFlag:  plan.Operation,
		Seed:             plan.Seed,
		RoomStateValue:   1,
		RoomStateFlag:    plan.PayloadMode,
		PartyMemberIndex: currentDungeonRuntimePartyMemberIndex(runtime),
	})
	if err != nil {
		return nil, fmt.Errorf("plan target-room start-map body: %w", err)
	}
	body, err := packet.Build()
	if err != nil {
		return nil, fmt.Errorf("build target-room start-map body: %w", err)
	}
	return body, nil
}
