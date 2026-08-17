package dnfbridge

import (
	"errors"
	"fmt"
	"sync"

	dnfmonster "longheng.io/server/internal/modules/dnf/monster"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

var (
	errDungeonMonsterIDInvalid        = errors.New("dnf dungeon monster id is invalid")
	errDungeonMonsterDefinitionMiss   = errors.New("dnf dungeon monster definition is missing")
	errDungeonMonsterObjectKeyRange   = errors.New("dnf dungeon monster object key exceeds current EXE range")
	errDungeonMonsterObjectNotFound   = errors.New("dnf dungeon monster runtime object is not found")
	errDungeonMonsterAlreadyAnnounced = errors.New("dnf dungeon monster was already announced")
	errDungeonMonsterNotAnnounced     = errors.New("dnf dungeon monster was not announced to the client")
	errDungeonMonsterAlreadyDefeated  = errors.New("dnf dungeon monster was already defeated")
	errDungeonActorAlreadyAnnounced   = errors.New("dnf dungeon actor was already announced")
	errDungeonActorOwnerConflict      = errors.New("dnf dungeon actor owner conflicts with the room session")
	errDungeonActorNotHostile         = errors.New("dnf dungeon actor is not a hostile room owner")
	errDungeonNonHostileRetirement    = errors.New("dnf non-hostile AI actor retirement is not eligible")
)

const firstDungeonMonsterObjectKey = uint32(currentSceneBootstrapObjectKey) + 1

type runtimeDungeonMonsterState string

const (
	runtimeDungeonMonsterPlanned   runtimeDungeonMonsterState = "planned"
	runtimeDungeonMonsterAnnounced runtimeDungeonMonsterState = "announced"
	runtimeDungeonMonsterDefeated  runtimeDungeonMonsterState = "defeated"
)

type runtimeDungeonMonster struct {
	ObjectKey         uint32
	Reference         worldmap.HostileReference
	Spawn             worldmap.MonsterSpawn
	Definition        dnfmonster.Monster
	State             runtimeDungeonMonsterState
	RoomClearSkipByte byte
}

type runtimeDungeonRoomSnapshot struct {
	Coordinate            worldmap.RoomCoordinate
	MapID                 int64
	Monsters              []runtimeDungeonMonster
	ExtendedActors        []runtimeDungeonExtendedActor
	RetainedSpecialSpawns []runtimeDungeonRetainedSpecialSpawn
	Diagnostics           []string
	OpaqueHostiles        []worldmap.HostileReference
	SpecialObjectCount    int
	AICharacterCount      int
}

type runtimeDungeonRoom struct {
	mu                    sync.RWMutex
	coordinate            worldmap.RoomCoordinate
	mapID                 int64
	monsters              []runtimeDungeonMonster
	byObjectKey           map[uint32]int
	opaqueHostiles        []worldmap.HostileReference
	extendedActors        []runtimeDungeonExtendedActor
	extendedByObjectKey   map[uint32]int
	retainedSpecialSpawns []runtimeDungeonRetainedSpecialSpawn
	diagnostics           []string
	specialObjectCount    int
	aiCharacterCount      int
}

func newRuntimeDungeonRoom(
	scene worldmap.DungeonRoomScene,
	catalog *pvfDungeonMonsterCatalog,
	firstObjectKey uint32,
) (*runtimeDungeonRoom, uint32, error) {
	if catalog == nil {
		return nil, firstObjectKey, errDungeonMonsterCatalogUnavailable
	}
	room := &runtimeDungeonRoom{
		coordinate:  scene.Coordinate,
		mapID:       scene.Map.Map.ID,
		monsters:    make([]runtimeDungeonMonster, 0, len(scene.Monsters)),
		byObjectKey: make(map[uint32]int, len(scene.Monsters)),
	}
	nextObjectKey := firstObjectKey
	for index, spawn := range scene.Monsters {
		if spawn.MonsterID <= 0 {
			return nil, firstObjectKey, fmt.Errorf(
				"%w: map=%d room=%s index=%d id=%d",
				errDungeonMonsterIDInvalid, room.mapID, room.coordinate, index, spawn.MonsterID,
			)
		}
		definition, ok, findErr := catalog.Find(spawn.MonsterID)
		if findErr != nil {
			return nil, firstObjectKey, fmt.Errorf(
				"resolve monster definition: map=%d room=%s index=%d id=%d: %w",
				room.mapID, room.coordinate, index, spawn.MonsterID, findErr,
			)
		}
		if !ok {
			return nil, firstObjectKey, fmt.Errorf(
				"%w: map=%d room=%s index=%d id=%d",
				errDungeonMonsterDefinitionMiss, room.mapID, room.coordinate, index, spawn.MonsterID,
			)
		}
		if nextObjectKey == 0 || nextObjectKey > uint32(^uint16(0)) {
			return nil, firstObjectKey, fmt.Errorf(
				"%w: map=%d room=%s index=%d object_key=%d",
				errDungeonMonsterObjectKeyRange, room.mapID, room.coordinate, index, nextObjectKey,
			)
		}
		reference := worldmap.HostileReference{Kind: worldmap.HostileMonster, Index: index}
		room.byObjectKey[nextObjectKey] = len(room.monsters)
		room.monsters = append(room.monsters, runtimeDungeonMonster{
			ObjectKey:  nextObjectKey,
			Reference:  reference,
			Spawn:      spawn,
			Definition: definition,
			State:      runtimeDungeonMonsterPlanned,
		})
		nextObjectKey++
	}
	for _, reference := range scene.ExpectedHostiles {
		if reference.Kind != worldmap.HostileMonster {
			room.opaqueHostiles = append(room.opaqueHostiles, reference)
		}
	}
	return room, nextObjectKey, nil
}

func (r *runtimeDungeonRoom) MonsterCount() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.monsters)
}

func (r *runtimeDungeonRoom) OpaqueHostileCount() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.opaqueHostiles)
}

func (r *runtimeDungeonRoom) MarkMonsterNonBlocking(monsterIndex int) (runtimeDungeonMonster, error) {
	if r == nil {
		return runtimeDungeonMonster{}, errDungeonMonsterObjectNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if monsterIndex < 0 || monsterIndex >= len(r.monsters) {
		return runtimeDungeonMonster{}, fmt.Errorf("%w: monster_index=%d", errDungeonMonsterObjectNotFound, monsterIndex)
	}
	r.monsters[monsterIndex].RoomClearSkipByte = 1
	return r.monsters[monsterIndex], nil
}

func (r *runtimeDungeonRoom) NonBlockingMonsterReferences() []worldmap.HostileReference {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]worldmap.HostileReference, 0)
	for _, monster := range r.monsters {
		if monster.RoomClearSkipByte != 0 {
			result = append(result, monster.Reference)
		}
	}
	return result
}

func (r *runtimeDungeonRoom) Snapshot() runtimeDungeonRoomSnapshot {
	if r == nil {
		return runtimeDungeonRoomSnapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return runtimeDungeonRoomSnapshot{
		Coordinate:            r.coordinate,
		MapID:                 r.mapID,
		Monsters:              cloneRuntimeDungeonMonsters(r.monsters),
		ExtendedActors:        cloneRuntimeDungeonExtendedActors(r.extendedActors),
		RetainedSpecialSpawns: cloneRuntimeDungeonRetainedSpecialSpawns(r.retainedSpecialSpawns),
		Diagnostics:           append([]string(nil), r.diagnostics...),
		OpaqueHostiles:        append([]worldmap.HostileReference(nil), r.opaqueHostiles...),
		SpecialObjectCount:    r.specialObjectCount,
		AICharacterCount:      r.aiCharacterCount,
	}
}

func (r *runtimeDungeonRoom) AttachExtendedActors(plan runtimeDungeonExtendedActorPlan) error {
	if r == nil {
		return errDungeonWorldMapUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	extendedByObjectKey := make(map[uint32]int, len(plan.Actors))
	handled := make(map[worldmap.HostileReference]struct{})
	for index, actor := range plan.Actors {
		if _, exists := r.byObjectKey[actor.ObjectKey]; exists {
			return fmt.Errorf("%w: object_key=%d normal_and_extended", errDungeonActorOwnerConflict, actor.ObjectKey)
		}
		if _, exists := extendedByObjectKey[actor.ObjectKey]; exists {
			return fmt.Errorf("%w: object_key=%d duplicate_extended", errDungeonActorOwnerConflict, actor.ObjectKey)
		}
		extendedByObjectKey[actor.ObjectKey] = index
		if actor.HostileReference != nil {
			handled[*actor.HostileReference] = struct{}{}
		}
	}
	remaining := make([]worldmap.HostileReference, 0, len(r.opaqueHostiles))
	for _, reference := range r.opaqueHostiles {
		if _, ok := handled[reference]; !ok {
			remaining = append(remaining, reference)
		}
	}
	r.extendedActors = cloneRuntimeDungeonExtendedActors(plan.Actors)
	r.extendedByObjectKey = extendedByObjectKey
	r.retainedSpecialSpawns = cloneRuntimeDungeonRetainedSpecialSpawns(plan.RetainedSpecialSpawns)
	r.diagnostics = append([]string(nil), plan.Diagnostics...)
	r.specialObjectCount = plan.SpecialObjectCount
	r.aiCharacterCount = plan.AICharacterCount
	r.opaqueHostiles = remaining
	return nil
}

func (r *runtimeDungeonRoom) AnnounceAllActors(session *worldmap.DungeonSession) (int, error) {
	if r == nil {
		return 0, errDungeonMonsterObjectNotFound
	}
	if session == nil {
		return 0, worldmap.ErrDungeonRunRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	type binding struct {
		reference worldmap.HostileReference
		objectKey uint32
	}
	bindings := make([]binding, 0, len(r.monsters)+len(r.extendedActors))
	for _, monster := range r.monsters {
		if monster.State != runtimeDungeonMonsterPlanned {
			return 0, fmt.Errorf("%w: object_key=%d state=%s", errDungeonActorAlreadyAnnounced, monster.ObjectKey, monster.State)
		}
		bindings = append(bindings, binding{reference: monster.Reference, objectKey: monster.ObjectKey})
	}
	for _, actor := range r.extendedActors {
		if actor.State != runtimeDungeonMonsterPlanned {
			return 0, fmt.Errorf("%w: object_key=%d state=%s", errDungeonActorAlreadyAnnounced, actor.ObjectKey, actor.State)
		}
		if actor.HostileReference != nil {
			bindings = append(bindings, binding{reference: *actor.HostileReference, objectKey: actor.ObjectKey})
		}
	}

	scene, ok := session.Scene()
	if !ok {
		return 0, worldmap.ErrDungeonRunRequired
	}
	seenReferences := make(map[worldmap.HostileReference]struct{}, len(bindings))
	seenObjectKeys := make(map[uint32]struct{}, len(bindings))
	for _, candidate := range bindings {
		if !runtimeSceneContainsHostile(scene.ExpectedHostiles, candidate.reference) {
			return 0, fmt.Errorf("%w: kind=%s index=%d object_key=%d reference_missing",
				errDungeonActorOwnerConflict,
				candidate.reference.Kind,
				candidate.reference.Index,
				candidate.objectKey,
			)
		}
		if _, exists := seenReferences[candidate.reference]; exists {
			return 0, fmt.Errorf("%w: kind=%s index=%d duplicate_reference",
				errDungeonActorOwnerConflict,
				candidate.reference.Kind,
				candidate.reference.Index,
			)
		}
		if _, exists := seenObjectKeys[candidate.objectKey]; exists {
			return 0, fmt.Errorf("%w: object_key=%d duplicate_key", errDungeonActorOwnerConflict, candidate.objectKey)
		}
		if previous, exists := scene.RuntimeObjects[candidate.objectKey]; exists {
			return 0, fmt.Errorf("%w: object_key=%d existing_kind=%s existing_index=%d",
				errDungeonActorOwnerConflict,
				candidate.objectKey,
				previous.Kind,
				previous.Index,
			)
		}
		for objectKey, previous := range scene.RuntimeObjects {
			if previous == candidate.reference {
				return 0, fmt.Errorf("%w: kind=%s index=%d existing_object_key=%d",
					errDungeonActorOwnerConflict,
					candidate.reference.Kind,
					candidate.reference.Index,
					objectKey,
				)
			}
		}
		seenReferences[candidate.reference] = struct{}{}
		seenObjectKeys[candidate.objectKey] = struct{}{}
	}

	for _, candidate := range bindings {
		if err := session.BindHostileObject(candidate.reference, candidate.objectKey); err != nil {
			return 0, fmt.Errorf("bind announced dungeon actor: %w", err)
		}
	}
	for index := range r.monsters {
		r.monsters[index].State = runtimeDungeonMonsterAnnounced
	}
	for index := range r.extendedActors {
		r.extendedActors[index].State = runtimeDungeonMonsterAnnounced
	}
	return len(r.monsters) + len(r.extendedActors), nil
}

func runtimeSceneContainsHostile(values []worldmap.HostileReference, want worldmap.HostileReference) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (r *runtimeDungeonRoom) AnnounceMonster(objectKey uint32, session *worldmap.DungeonSession) error {
	if r == nil {
		return errDungeonMonsterObjectNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index, ok := r.byObjectKey[objectKey]
	if !ok {
		return fmt.Errorf("%w: object_key=%d", errDungeonMonsterObjectNotFound, objectKey)
	}
	monster := &r.monsters[index]
	if monster.State != runtimeDungeonMonsterPlanned {
		return fmt.Errorf("%w: object_key=%d state=%s", errDungeonMonsterAlreadyAnnounced, objectKey, monster.State)
	}
	if session == nil {
		return worldmap.ErrDungeonRunRequired
	}
	if err := session.BindHostileObject(monster.Reference, objectKey); err != nil {
		return err
	}
	monster.State = runtimeDungeonMonsterAnnounced
	return nil
}

func (r *runtimeDungeonRoom) ContainsAnnouncedActorObjectKey(objectKey uint32) bool {
	if r == nil || objectKey == 0 {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if index, ok := r.byObjectKey[objectKey]; ok {
		return r.monsters[index].State != runtimeDungeonMonsterPlanned
	}
	if index, ok := r.extendedByObjectKey[objectKey]; ok {
		return r.extendedActors[index].State != runtimeDungeonMonsterPlanned
	}
	return false
}

func (r *runtimeDungeonRoom) ContainsAnnouncedNonHostileAICharacterObjectKey(objectKey uint32) bool {
	if r == nil || objectKey == 0 {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	index, ok := r.extendedByObjectKey[objectKey]
	if !ok {
		return false
	}
	actor := r.extendedActors[index]
	return actor.Kind == runtimeDungeonActorAICharacter &&
		actor.HostileReference == nil &&
		actor.State == runtimeDungeonMonsterAnnounced
}

// AnnouncedMonster resolves only an ordinary PVF [monster] owned by this room
// and only while it is in the announced state. Extended actors, planned
// monsters, defeated monsters, and stale keys are intentionally excluded.
func (r *runtimeDungeonRoom) AnnouncedMonster(objectKey uint32) (runtimeDungeonMonster, bool) {
	if r == nil || objectKey == 0 {
		return runtimeDungeonMonster{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	index, ok := r.byObjectKey[objectKey]
	if !ok || r.monsters[index].State != runtimeDungeonMonsterAnnounced {
		return runtimeDungeonMonster{}, false
	}
	return r.monsters[index], true
}

// MonsterByReference resolves the PVF monster definition behind a room
// HostileReference regardless of whether the live actor object is the ordinary
// monster row or a current-EXE extended actor bound to the same reference. 86JP
// performs kill reward/quest checks from the logical monster/enemy code after a
// death event; callers still own the actor state transition before using this
// helper for EXP, drops, or quest trigger inputs.
func (r *runtimeDungeonRoom) MonsterByReference(
	reference worldmap.HostileReference,
	allowedStates ...runtimeDungeonMonsterState,
) (runtimeDungeonMonster, bool) {
	if r == nil || reference.Kind != worldmap.HostileMonster || reference.Index < 0 {
		return runtimeDungeonMonster{}, false
	}
	allowed := make(map[runtimeDungeonMonsterState]struct{}, len(allowedStates))
	for _, state := range allowedStates {
		allowed[state] = struct{}{}
	}
	stateAllowed := func(state runtimeDungeonMonsterState) bool {
		if len(allowed) == 0 {
			return true
		}
		_, ok := allowed[state]
		return ok
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, monster := range r.monsters {
		if monster.Reference == reference && stateAllowed(monster.State) {
			return monster, true
		}
	}
	return runtimeDungeonMonster{}, false
}

type runtimeDungeonDeathTarget struct {
	ObjectKey    uint32
	Reference    worldmap.HostileReference
	ResponseKind currentDungeonDeathResponseKind
}

func (r *runtimeDungeonRoom) CommitActorDeathReport(
	objectKey uint32,
	session *worldmap.DungeonSession,
) (runtimeDungeonDeathTarget, bool, error) {
	if r == nil {
		return runtimeDungeonDeathTarget{}, false, errDungeonMonsterObjectNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	target, state, err := r.deathTargetLocked(objectKey)
	if err != nil {
		return runtimeDungeonDeathTarget{}, false, err
	}
	switch *state {
	case runtimeDungeonMonsterAnnounced:
	case runtimeDungeonMonsterDefeated:
		return runtimeDungeonDeathTarget{}, false, fmt.Errorf("%w: object_key=%d", errDungeonMonsterAlreadyDefeated, objectKey)
	default:
		return runtimeDungeonDeathTarget{}, false, fmt.Errorf("%w: object_key=%d state=%s", errDungeonMonsterNotAnnounced, objectKey, *state)
	}
	if session == nil {
		return runtimeDungeonDeathTarget{}, false, worldmap.ErrDungeonRunRequired
	}
	scene, ok := session.Scene()
	if !ok {
		return runtimeDungeonDeathTarget{}, false, worldmap.ErrDungeonRunRequired
	}
	reference, bound := scene.RuntimeObjects[objectKey]
	if !bound || reference != target.Reference {
		return runtimeDungeonDeathTarget{}, false, fmt.Errorf(
			"%w: object_key=%d expected=%s/%d actual=%s/%d bound=%t",
			errDungeonActorOwnerConflict,
			objectKey,
			target.Reference.Kind,
			target.Reference.Index,
			reference.Kind,
			reference.Index,
			bound,
		)
	}
	cleared, err := session.MarkHostileDefeated(objectKey)
	if err != nil {
		return runtimeDungeonDeathTarget{}, false, err
	}
	*state = runtimeDungeonMonsterDefeated
	return target, cleared, nil
}

// CommitRemainingAnnouncedHostilesAfterBoss mirrors the 86JP dungeon-clear
// domain rule used after a real boss death: the boss death is the authoritative
// end of combat, so remaining already-announced hostile actors in the same
// room must not keep the room/settlement chain stuck.  It intentionally does
// not fabricate a boss death; callers invoke it only after CommitActorDeathReport
// accepted the boss actor's own op39.
func (r *runtimeDungeonRoom) CommitRemainingAnnouncedHostilesAfterBoss(
	bossObjectKey uint32,
	session *worldmap.DungeonSession,
) ([]runtimeDungeonDeathTarget, bool, error) {
	if r == nil {
		return nil, false, errDungeonMonsterObjectNotFound
	}
	if session == nil {
		return nil, false, worldmap.ErrDungeonRunRequired
	}
	snapshot := r.Snapshot()
	var forced []runtimeDungeonDeathTarget
	cleared := false
	for _, monster := range snapshot.Monsters {
		if monster.ObjectKey == bossObjectKey || monster.State != runtimeDungeonMonsterAnnounced {
			continue
		}
		target, roomCleared, err := r.CommitActorDeathReport(monster.ObjectKey, session)
		if err != nil {
			if errors.Is(err, errDungeonMonsterAlreadyDefeated) || errors.Is(err, worldmap.ErrHostileAlreadyDefeated) {
				continue
			}
			return forced, cleared, err
		}
		forced = append(forced, target)
		cleared = cleared || roomCleared
	}
	for _, actor := range snapshot.ExtendedActors {
		if actor.ObjectKey == bossObjectKey || actor.State != runtimeDungeonMonsterAnnounced ||
			actor.HostileReference == nil {
			continue
		}
		target, roomCleared, err := r.CommitActorDeathReport(actor.ObjectKey, session)
		if err != nil {
			if errors.Is(err, errDungeonMonsterAlreadyDefeated) || errors.Is(err, worldmap.ErrHostileAlreadyDefeated) {
				continue
			}
			return forced, cleared, err
		}
		forced = append(forced, target)
		cleared = cleared || roomCleared
	}
	return forced, cleared, nil
}

// RetireNonHostileAIActor marks an announced, non-blocking character AI as
// retired without touching the hostile-object/session clear state. The
// current client still reports this visual helper through DIE_MONSTER with
// owner=0xffff, but it has no HostileReference and must not be committed via
// MarkHostileDefeated. A repeated report for the same already-retired actor
// returns the same target so the caller can safely replay the ACK.
func (r *runtimeDungeonRoom) RetireNonHostileAIActor(
	objectKey uint32,
) (runtimeDungeonDeathTarget, error) {
	if r == nil {
		return runtimeDungeonDeathTarget{}, errDungeonMonsterObjectNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index, ok := r.extendedByObjectKey[objectKey]
	if !ok {
		return runtimeDungeonDeathTarget{}, fmt.Errorf("%w: object_key=%d", errDungeonMonsterObjectNotFound, objectKey)
	}
	actor := &r.extendedActors[index]
	if actor.Kind != runtimeDungeonActorAICharacter || actor.HostileReference != nil || actor.Packet.Blocking != 0 {
		return runtimeDungeonDeathTarget{}, fmt.Errorf(
			"%w: object_key=%d kind=%s blocking=%d hostile_reference=%t",
			errDungeonNonHostileRetirement,
			objectKey,
			actor.Kind,
			actor.Packet.Blocking,
			actor.HostileReference != nil,
		)
	}
	if actor.State != runtimeDungeonMonsterAnnounced && actor.State != runtimeDungeonMonsterDefeated {
		return runtimeDungeonDeathTarget{}, fmt.Errorf(
			"%w: object_key=%d state=%s",
			errDungeonNonHostileRetirement,
			objectKey,
			actor.State,
		)
	}
	actor.State = runtimeDungeonMonsterDefeated
	return runtimeDungeonDeathTarget{
		ObjectKey:    objectKey,
		ResponseKind: currentDungeonDeathResponseAICharacter,
	}, nil
}

// RetireSpecialMonsterActor acknowledges a PVF [special passive object] child
// [monster] actor that the current client reports through DIE_MONSTER, but that
// is intentionally not a hostile room owner. These actors are visual/runtime
// children of a map object (for example enemies spawned out of a breakable
// building), so retiring them must not call MarkHostileDefeated and must not
// affect room-clear, EXP, drops, or quest kill accounting.
func (r *runtimeDungeonRoom) RetireSpecialMonsterActor(
	objectKey uint32,
) (runtimeDungeonDeathTarget, bool, error) {
	if r == nil {
		return runtimeDungeonDeathTarget{}, false, errDungeonMonsterObjectNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index, ok := r.extendedByObjectKey[objectKey]
	if !ok {
		return runtimeDungeonDeathTarget{}, false, nil
	}
	actor := &r.extendedActors[index]
	if actor.Kind != runtimeDungeonActorSpecialMonster {
		return runtimeDungeonDeathTarget{}, false, nil
	}
	if actor.HostileReference != nil {
		return runtimeDungeonDeathTarget{}, true, fmt.Errorf(
			"%w: object_key=%d kind=%s hostile_reference=%t",
			errDungeonNonHostileRetirement,
			objectKey,
			actor.Kind,
			true,
		)
	}
	if actor.State != runtimeDungeonMonsterAnnounced && actor.State != runtimeDungeonMonsterDefeated {
		return runtimeDungeonDeathTarget{}, true, fmt.Errorf(
			"%w: object_key=%d kind=%s state=%s",
			errDungeonNonHostileRetirement,
			objectKey,
			actor.Kind,
			actor.State,
		)
	}
	actor.State = runtimeDungeonMonsterDefeated
	return runtimeDungeonDeathTarget{
		ObjectKey:    objectKey,
		ResponseKind: currentDungeonDeathResponseMonster,
	}, true, nil
}

func (r *runtimeDungeonRoom) deathTargetLocked(
	objectKey uint32,
) (runtimeDungeonDeathTarget, *runtimeDungeonMonsterState, error) {
	if index, ok := r.byObjectKey[objectKey]; ok {
		monster := &r.monsters[index]
		return runtimeDungeonDeathTarget{
			ObjectKey:    objectKey,
			Reference:    monster.Reference,
			ResponseKind: currentDungeonDeathResponseMonster,
		}, &monster.State, nil
	}
	index, ok := r.extendedByObjectKey[objectKey]
	if !ok {
		return runtimeDungeonDeathTarget{}, nil, fmt.Errorf("%w: object_key=%d", errDungeonMonsterObjectNotFound, objectKey)
	}
	actor := &r.extendedActors[index]
	if actor.HostileReference == nil {
		return runtimeDungeonDeathTarget{}, nil, fmt.Errorf("%w: object_key=%d kind=%s", errDungeonActorNotHostile, objectKey, actor.Kind)
	}
	responseKind := currentDungeonDeathResponseMonster
	if actor.Kind == runtimeDungeonActorAICharacter {
		responseKind = currentDungeonDeathResponseAICharacter
	}
	return runtimeDungeonDeathTarget{
		ObjectKey:    objectKey,
		Reference:    *actor.HostileReference,
		ResponseKind: responseKind,
	}, &actor.State, nil
}

func cloneRuntimeDungeonMonsters(values []runtimeDungeonMonster) []runtimeDungeonMonster {
	out := make([]runtimeDungeonMonster, len(values))
	for index, value := range values {
		out[index] = value
		out[index].Definition.Scalars = cloneFloatMap(value.Definition.Scalars)
		out[index].Definition.Sections = append([]string(nil), value.Definition.Sections...)
	}
	return out
}

func cloneRuntimeDungeonExtendedActors(values []runtimeDungeonExtendedActor) []runtimeDungeonExtendedActor {
	out := make([]runtimeDungeonExtendedActor, len(values))
	for index, value := range values {
		out[index] = value
		if value.HostileReference != nil {
			reference := *value.HostileReference
			out[index].HostileReference = &reference
		}
		if value.AICharacter != nil {
			out[index].AICharacter = cloneRuntimeDungeonAICharacter(*value.AICharacter)
		}
		if value.AICharacterMetadata != nil {
			definition := cloneDungeonAICharacterDefinition(*value.AICharacterMetadata)
			out[index].AICharacterMetadata = &definition
		}
	}
	return out
}

func cloneRuntimeDungeonRetainedSpecialSpawns(values []runtimeDungeonRetainedSpecialSpawn) []runtimeDungeonRetainedSpecialSpawn {
	return append([]runtimeDungeonRetainedSpecialSpawn(nil), values...)
}

func cloneFloatMap(values map[string]float64) map[string]float64 {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]float64, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
