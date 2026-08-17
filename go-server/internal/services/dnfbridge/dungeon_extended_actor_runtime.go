package dnfbridge

import (
	"errors"
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

var (
	errDungeonExtendedActorCodeRange      = errors.New("dnf dungeon extended actor code exceeds current EXE range")
	errDungeonExtendedActorIndexRange     = errors.New("dnf dungeon extended actor index exceeds current EXE range")
	errDungeonExtendedActorObjectKeyRange = errors.New("dnf dungeon extended actor object key exceeds current EXE range")
	errDungeonSpecialSpawnKind            = errors.New("dnf dungeon special object spawn kind is unsupported")
	errDungeonSpecialSpawnLevel           = errors.New("dnf dungeon special object monster level is invalid")
	errDungeonAICharacterDefinitionMiss   = errors.New("dnf dungeon AI character definition is missing")
	errDungeonAICharacterType             = errors.New("dnf dungeon AI character type is unsupported")
	errDungeonAICharacterFaction          = errors.New("dnf dungeon AI character faction is unsupported")
)

type runtimeDungeonExtendedActorKind string

const (
	runtimeDungeonActorSpecialObject  runtimeDungeonExtendedActorKind = "special_object"
	runtimeDungeonActorSpecialMonster runtimeDungeonExtendedActorKind = "special_monster"
	runtimeDungeonActorAICharacter    runtimeDungeonExtendedActorKind = "ai_character"
)

type runtimeDungeonRetainedSpecialSpawn struct {
	ObjectIndex int
	SpawnIndex  int
	Spawn       worldmap.SpecialObjectSpawn
}

type runtimeDungeonExtendedActor struct {
	Kind                runtimeDungeonExtendedActorKind
	ObjectKey           uint32
	Packet              currentDungeonStartMapActor
	HostileReference    *worldmap.HostileReference
	AICharacter         *worldmap.AICharacter
	AICharacterMetadata *pvfDungeonAICharacterDefinition
	SpecialObjectIndex  int
	SpecialSpawnIndex   int
	State               runtimeDungeonMonsterState
}

type runtimeDungeonExtendedActorPlan struct {
	Actors                []runtimeDungeonExtendedActor
	RetainedSpecialSpawns []runtimeDungeonRetainedSpecialSpawn
	Diagnostics           []string
	NextObjectKey         uint32
	SpecialObjectCount    int
	AICharacterCount      int
}

func planRuntimeDungeonExtendedActors(
	scene worldmap.DungeonRoomScene,
	monsterCatalog *pvfDungeonMonsterCatalog,
	aiCharacterCatalog *pvfDungeonAICharacterCatalog,
	basis worldmap.OptionalInt,
	firstObjectKey uint32,
) (runtimeDungeonExtendedActorPlan, error) {
	plan := runtimeDungeonExtendedActorPlan{
		NextObjectKey:      firstObjectKey,
		SpecialObjectCount: len(scene.SpecialPassiveObjects),
		AICharacterCount:   len(scene.AICharacters),
	}
	for objectIndex, object := range scene.SpecialPassiveObjects {
		if object.ObjectID > 0 {
			actor, err := newRuntimeDungeonExtendedActor(
				runtimeDungeonActorSpecialObject,
				plan.NextObjectKey,
				object.ObjectID,
				currentDungeonStartMapActor{PacketIndex: uint32(objectIndex), Level: 0, Type: 9},
			)
			if err != nil {
				return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("special object index %d: %w", objectIndex, err)
			}
			actor.SpecialObjectIndex = objectIndex
			plan.Actors = append(plan.Actors, actor)
			plan.NextObjectKey++
		} else {
			plan.Diagnostics = append(plan.Diagnostics, fmt.Sprintf("special object index %d has non-positive object id %d", objectIndex, object.ObjectID))
		}

		for spawnIndex, spawn := range object.Spawns {
			kind := normalizeDungeonPVFSymbol(spawn.Kind)
			if kind != "monster" {
				plan.RetainedSpecialSpawns = append(plan.RetainedSpecialSpawns, runtimeDungeonRetainedSpecialSpawn{
					ObjectIndex: objectIndex,
					SpawnIndex:  spawnIndex,
					Spawn:       spawn,
				})
				switch kind {
				case "item", "trap", "hellparty", "quest":
					continue
				default:
					return runtimeDungeonExtendedActorPlan{}, fmt.Errorf(
						"%w: object_index=%d spawn_index=%d kind=%q",
						errDungeonSpecialSpawnKind,
						objectIndex,
						spawnIndex,
						spawn.Kind,
					)
				}
			}
			if monsterCatalog == nil {
				return runtimeDungeonExtendedActorPlan{}, errDungeonMonsterCatalogUnavailable
			}
			if objectIndex > int(^uint16(0)) || objectIndex > int(^uint8(0)) {
				return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("%w: special_object_index=%d", errDungeonExtendedActorIndexRange, objectIndex)
			}
			if spawnIndex > int(^uint32(0)) {
				return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("%w: special_spawn_index=%d", errDungeonExtendedActorIndexRange, spawnIndex)
			}
			if spawn.Code <= 0 || spawn.Code > int64(^uint32(0)) {
				return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("%w: special_object_index=%d spawn_index=%d code=%d", errDungeonExtendedActorCodeRange, objectIndex, spawnIndex, spawn.Code)
			}
			if _, found, err := monsterCatalog.Find(spawn.Code); err != nil {
				return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("resolve special monster code=%d: %w", spawn.Code, err)
			} else if !found {
				return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("%w: special monster code=%d", errDungeonMonsterDefinitionMiss, spawn.Code)
			}
			level, err := currentDungeonSpecialMonsterLevel(spawn, basis)
			if err != nil {
				return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("special object index %d spawn index %d: %w", objectIndex, spawnIndex, err)
			}
			actor, err := newRuntimeDungeonExtendedActor(
				runtimeDungeonActorSpecialMonster,
				plan.NextObjectKey,
				spawn.Code,
				currentDungeonStartMapActor{
					TemplateOrder: uint16(objectIndex),
					PacketIndex:   uint32(spawnIndex),
					Level:         level,
					Type:          0,
					Flag0:         1,
					Flag1:         byte(objectIndex),
				},
			)
			if err != nil {
				return runtimeDungeonExtendedActorPlan{}, err
			}
			actor.SpecialObjectIndex = objectIndex
			actor.SpecialSpawnIndex = spawnIndex
			plan.Actors = append(plan.Actors, actor)
			plan.NextObjectKey++
		}
	}

	for actorIndex, actorValue := range scene.AICharacters {
		if aiCharacterCatalog == nil {
			return runtimeDungeonExtendedActorPlan{}, errDungeonAICharacterUnavailable
		}
		if actorValue.Code <= 0 || actorValue.Code > int64(^uint32(0)) {
			return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("%w: AI_character_index=%d code=%d", errDungeonExtendedActorCodeRange, actorIndex, actorValue.Code)
		}
		hostile, err := currentDungeonAICharacterHostile(actorValue.Faction)
		if err != nil {
			return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("AI character index %d code %d: %w", actorIndex, actorValue.Code, err)
		}
		definition, found, err := aiCharacterCatalog.Find(actorValue.Code)
		if err != nil {
			return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("resolve AI character index=%d code=%d: %w", actorIndex, actorValue.Code, err)
		}
		if !found {
			if !hostile {
				plan.Diagnostics = append(plan.Diagnostics, fmt.Sprintf(
					"skip non-hostile AI character index %d code %d because its definition is missing",
					actorIndex,
					actorValue.Code,
				))
				continue
			}
			return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("%w: index=%d code=%d", errDungeonAICharacterDefinitionMiss, actorIndex, actorValue.Code)
		}
		actorType, err := currentDungeonAICharacterType(actorValue.AIType)
		if err != nil {
			return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("AI character index %d code %d: %w", actorIndex, actorValue.Code, err)
		}
		blocking, err := currentDungeonAICharacterBlocking(actorValue.Faction)
		if err != nil {
			return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("AI character index %d code %d: %w", actorIndex, actorValue.Code, err)
		}
		actor, err := newRuntimeDungeonExtendedActor(
			runtimeDungeonActorAICharacter,
			plan.NextObjectKey,
			actorValue.Code,
			currentDungeonStartMapActor{
				PacketIndex: uint32(actorIndex),
				Level:       definition.Level,
				Type:        actorType,
				Blocking:    blocking,
			},
		)
		if err != nil {
			return runtimeDungeonExtendedActorPlan{}, err
		}
		actor.AICharacter = cloneRuntimeDungeonAICharacter(actorValue)
		definitionCopy := cloneDungeonAICharacterDefinition(definition)
		actor.AICharacterMetadata = &definitionCopy
		if hostile {
			reference := worldmap.HostileReference{Kind: worldmap.HostileAICharacter, Index: actorIndex}
			actor.HostileReference = &reference
		}
		plan.Actors = append(plan.Actors, actor)
		plan.NextObjectKey++
	}
	if len(scene.Monsters)+len(plan.Actors) > 0xff {
		return runtimeDungeonExtendedActorPlan{}, fmt.Errorf("%w: normal=%d extended=%d", errDungeonStartMapActorCount, len(scene.Monsters), len(plan.Actors))
	}
	return plan, nil
}

func newRuntimeDungeonExtendedActor(
	kind runtimeDungeonExtendedActorKind,
	objectKey uint32,
	code int64,
	packet currentDungeonStartMapActor,
) (runtimeDungeonExtendedActor, error) {
	if objectKey == 0 || objectKey > uint32(^uint16(0)) {
		return runtimeDungeonExtendedActor{}, fmt.Errorf("%w: object_key=%d", errDungeonExtendedActorObjectKeyRange, objectKey)
	}
	if code <= 0 || code > int64(^uint32(0)) {
		return runtimeDungeonExtendedActor{}, fmt.Errorf("%w: code=%d", errDungeonExtendedActorCodeRange, code)
	}
	packet.ObjectKey = uint16(objectKey)
	packet.Code = uint32(code)
	return runtimeDungeonExtendedActor{
		Kind:      kind,
		ObjectKey: objectKey,
		Packet:    packet,
		State:     runtimeDungeonMonsterPlanned,
	}, nil
}

func currentDungeonSpecialMonsterLevel(spawn worldmap.SpecialObjectSpawn, basis worldmap.OptionalInt) (byte, error) {
	level := spawn.Level
	if level <= 0 {
		if !basis.Set {
			return 0, fmt.Errorf("%w: code=%d level=%d basis=unset", errDungeonSpecialSpawnLevel, spawn.Code, spawn.Level)
		}
		level = basis.Value
	}
	if level <= 0 || level > 0xff {
		return 0, fmt.Errorf("%w: code=%d level=%d basis=%d basis_set=%t resolved=%d", errDungeonSpecialSpawnLevel, spawn.Code, spawn.Level, basis.Value, basis.Set, level)
	}
	return byte(level), nil
}

func currentDungeonAICharacterType(value string) (byte, error) {
	switch normalizeDungeonPVFSymbol(value) {
	case "normal":
		return 5, nil
	case "champion":
		return 6, nil
	case "super champion", "superchampion":
		return 7, nil
	case "boss":
		return 8, nil
	default:
		return 0, fmt.Errorf("%w: type=%q", errDungeonAICharacterType, value)
	}
}

func currentDungeonAICharacterBlocking(value string) (byte, error) {
	if _, err := currentDungeonAICharacterHostile(value); err != nil {
		return 0, err
	}
	// Current check_grid_clear and the old C# behavior both exclude every
	// AICharacter/APC row (types 5..8) from the room-door blocking set. Faction
	// still determines combat ownership, but never this wire flag.
	return 0, nil
}

func currentDungeonAICharacterHostile(value string) (bool, error) {
	switch normalizeDungeonPVFSymbol(value) {
	case "monster":
		return true, nil
	case "character", "neutral":
		return false, nil
	default:
		return false, fmt.Errorf("%w: faction=%q", errDungeonAICharacterFaction, value)
	}
}

func normalizeDungeonPVFSymbol(value string) string {
	return strings.ToLower(strings.Trim(value, "[] \t\r\n"))
}

func cloneRuntimeDungeonAICharacter(value worldmap.AICharacter) *worldmap.AICharacter {
	value.Params = append([]int64(nil), value.Params...)
	return &value
}
