package dnfbridge

import (
	"errors"
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

var (
	errDungeonStartMapCoordinateRange = errors.New("dnf start-map coordinate exceeds current EXE range")
	errDungeonStartMapMapIDRange      = errors.New("dnf start-map map id exceeds current EXE range")
	errDungeonStartMapActorCount      = errors.New("dnf start-map actor count exceeds current EXE range")
	errDungeonStartMapObjectKeyRange  = errors.New("dnf start-map actor object key exceeds current EXE range")
	errDungeonStartMapActorCodeRange  = errors.New("dnf start-map actor code exceeds current EXE range")
	errDungeonStartMapMonsterRank     = errors.New("dnf start-map monster rank is unsupported")
	errDungeonStartMapMonsterLevel    = errors.New("dnf start-map monster level is invalid")
	errDungeonStartMapOpaqueHostile   = errors.New("dnf start-map contains an unsupported hostile actor")
	errDungeonStartMapSpecialObject   = errors.New("dnf start-map contains an unsupported special passive object")
	errDungeonStartMapSceneMismatch   = errors.New("dnf start-map scene does not match its runtime owner")
	errDungeonStartMapExtraCount      = errors.New("dnf start-map extra entry count exceeds current EXE range")
	errDungeonStartMapGroupCount      = errors.New("dnf start-map ridable group count exceeds current EXE range")
)

const (
	currentDungeonNPCActorType byte = 4

	currentDungeonStartMapOperationCurrent      byte = 0
	currentDungeonStartMapOperationAdvanceLayer byte = 1
	currentDungeonStartMapOperationRestoreBase  byte = 2

	currentDungeonStartMapPayloadCached  byte = 0
	currentDungeonStartMapPayloadBuild   byte = 1
	currentDungeonStartMapPayloadRefresh byte = 2
)

type currentDungeonStartMapActor struct {
	TemplateOrder uint16
	PacketIndex   uint32
	ObjectKey     uint16
	Code          uint32
	Level         byte
	Type          byte
	Flag0         byte
	Flag1         byte
	ExtraState    uint32
	// Blocking is the current EXE's reverse-polarity room-clear byte: zero
	// makes an ordinary monster participate in the alive scan; one skips it.
	Blocking byte
}

// The current EXE reads this row as 21 bytes. The older C# implementation
// wrote a 19-byte row and is not wire-compatible here.
type currentDungeonStartMapExtraEntry struct {
	PassiveObjectIndex byte
	GlobalSequence     uint32
	ItemID             uint32
	StackCount         uint32
	Endurance          uint16
	AmplifyType        byte
	AmplifyValue       uint16
	ExtendedValue      uint16
	ExtendedFlag       byte
}

type currentDungeonStartMapRidableEntry struct {
	PositionX  uint32
	PositionY  uint32
	ObjectCode uint32
	Faction    uint32
	State      uint32
}

type currentDungeonStartMap struct {
	X                         byte
	Y                         byte
	LayeredRoomFlag           byte
	Seed                      uint32
	HellPartyMode             byte
	UnknownAfterHellPartyMode byte
	RoomStateValue            uint32
	RoomStateFlag             byte
	MapID                     uint32
	Actors                    []currentDungeonStartMapActor
	ExtraEntries              []currentDungeonStartMapExtraEntry
	HellPartyFogFlag          byte
	RidableGroups             [][]currentDungeonStartMapRidableEntry
	PartyMemberIndex          byte
}

// currentDungeonStartMapState contains protocol state that is not owned by a
// map PVF record. Callers must provide it explicitly; this layer does not
// invent a seed, mode, room state, drop, or ridable-object value.
type currentDungeonStartMapState struct {
	LayeredRoomFlag           byte
	Seed                      uint32
	HellPartyMode             byte
	UnknownAfterHellPartyMode byte
	RoomStateValue            uint32
	RoomStateFlag             byte
	ExtraEntries              []currentDungeonStartMapExtraEntry
	HellPartyFogFlag          byte
	RidableGroups             [][]currentDungeonStartMapRidableEntry
	PartyMemberIndex          byte
}

func currentDungeonStartMapFromRuntime(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	state currentDungeonStartMapState,
) (currentDungeonStartMap, error) {
	if scene.Coordinate.X < 0 || scene.Coordinate.X > 0xff || scene.Coordinate.Y < 0 || scene.Coordinate.Y > 0xff {
		return currentDungeonStartMap{}, fmt.Errorf("%w: room=%s", errDungeonStartMapCoordinateRange, scene.Coordinate)
	}
	if scene.Map.Map.ID <= 0 || scene.Map.Map.ID > int64(^uint32(0)) {
		return currentDungeonStartMap{}, fmt.Errorf("%w: map=%d", errDungeonStartMapMapIDRange, scene.Map.Map.ID)
	}
	actors, err := currentDungeonStartMapActors(runtime, scene)
	if err != nil {
		return currentDungeonStartMap{}, err
	}
	return currentDungeonStartMap{
		X:                         byte(scene.Coordinate.X),
		Y:                         byte(scene.Coordinate.Y),
		LayeredRoomFlag:           state.LayeredRoomFlag,
		Seed:                      state.Seed,
		HellPartyMode:             state.HellPartyMode,
		UnknownAfterHellPartyMode: state.UnknownAfterHellPartyMode,
		RoomStateValue:            state.RoomStateValue,
		RoomStateFlag:             state.RoomStateFlag,
		MapID:                     uint32(scene.Map.Map.ID),
		Actors:                    actors,
		ExtraEntries:              append([]currentDungeonStartMapExtraEntry(nil), state.ExtraEntries...),
		HellPartyFogFlag:          state.HellPartyFogFlag,
		RidableGroups:             cloneCurrentDungeonRidableGroups(state.RidableGroups),
		PartyMemberIndex:          state.PartyMemberIndex,
	}, nil
}

func cloneCurrentDungeonRidableGroups(values [][]currentDungeonStartMapRidableEntry) [][]currentDungeonStartMapRidableEntry {
	if len(values) == 0 {
		return nil
	}
	result := make([][]currentDungeonStartMapRidableEntry, len(values))
	for index, group := range values {
		result[index] = append([]currentDungeonStartMapRidableEntry(nil), group...)
	}
	return result
}

func (packet currentDungeonStartMap) Build() ([]byte, error) {
	if len(packet.Actors) > 0xff {
		return nil, fmt.Errorf("%w: count=%d", errDungeonStartMapActorCount, len(packet.Actors))
	}
	if len(packet.ExtraEntries) > 0xff {
		return nil, fmt.Errorf("%w: count=%d", errDungeonStartMapExtraCount, len(packet.ExtraEntries))
	}
	if len(packet.RidableGroups) > 0xff {
		return nil, fmt.Errorf("%w: count=%d", errDungeonStartMapGroupCount, len(packet.RidableGroups))
	}
	for groupIndex, group := range packet.RidableGroups {
		if len(group) > 0xff {
			return nil, fmt.Errorf("%w: group=%d count=%d", errDungeonStartMapGroupCount, groupIndex, len(group))
		}
	}

	var writer packetWriter
	writer.writeByte(packet.X)
	writer.writeByte(packet.Y)
	writer.writeByte(packet.LayeredRoomFlag)
	writer.writeUint32(packet.Seed)
	writer.writeByte(packet.HellPartyMode)
	writer.writeByte(packet.UnknownAfterHellPartyMode)
	writer.writeUint32(packet.RoomStateValue)
	writer.writeByte(packet.RoomStateFlag)
	writer.writeUint32(packet.MapID)
	writer.writeByte(byte(len(packet.Actors)))
	for _, actor := range packet.Actors {
		writer.writeUint16(actor.TemplateOrder)
		writer.writeUint32(actor.PacketIndex)
		writer.writeUint16(actor.ObjectKey)
		writer.writeUint32(actor.Code)
		writer.writeByte(actor.Level)
		writer.writeByte(actor.Type)
		writer.writeByte(actor.Flag0)
		writer.writeByte(actor.Flag1)
		writer.writeUint32(actor.ExtraState)
		writer.writeByte(actor.Blocking)
	}
	writer.writeByte(byte(len(packet.ExtraEntries)))
	for _, entry := range packet.ExtraEntries {
		writer.writeByte(entry.PassiveObjectIndex)
		writer.writeUint32(entry.GlobalSequence)
		writer.writeUint32(entry.ItemID)
		writer.writeUint32(entry.StackCount)
		writer.writeUint16(entry.Endurance)
		writer.writeByte(entry.AmplifyType)
		writer.writeUint16(entry.AmplifyValue)
		writer.writeUint16(entry.ExtendedValue)
		writer.writeByte(entry.ExtendedFlag)
	}
	writer.writeByte(packet.HellPartyFogFlag)
	writer.writeByte(byte(len(packet.RidableGroups)))
	for _, group := range packet.RidableGroups {
		writer.writeByte(byte(len(group)))
		for _, entry := range group {
			writer.writeUint32(entry.PositionX)
			writer.writeUint32(entry.PositionY)
			writer.writeUint32(entry.ObjectCode)
			writer.writeUint32(entry.Faction)
			writer.writeUint32(entry.State)
		}
	}
	writer.writeByte(packet.PartyMemberIndex)
	return writer.bytes(), nil
}

// currentDungeonCachedStartMap is the exact mode0 branch consumed by the
// current client. It stops before MapID/actor rows, then reads only the ridable
// group count and party-member index, for a fixed 16-byte body.
type currentDungeonCachedStartMap struct {
	X                         byte
	Y                         byte
	Operation                 byte
	Seed                      uint32
	HellPartyMode             byte
	UnknownAfterHellPartyMode byte
	RoomStateValue            uint32
	RidableGroupCount         byte
	PartyMemberIndex          byte
}

func (packet currentDungeonCachedStartMap) Build() ([]byte, error) {
	if packet.Operation > currentDungeonStartMapOperationRestoreBase {
		return nil, fmt.Errorf("unsupported cached start-map operation=%d", packet.Operation)
	}
	var writer packetWriter
	writer.writeByte(packet.X)
	writer.writeByte(packet.Y)
	writer.writeByte(packet.Operation)
	writer.writeUint32(packet.Seed)
	writer.writeByte(packet.HellPartyMode)
	writer.writeByte(packet.UnknownAfterHellPartyMode)
	writer.writeUint32(packet.RoomStateValue)
	writer.writeByte(currentDungeonStartMapPayloadCached)
	writer.writeByte(packet.RidableGroupCount)
	writer.writeByte(packet.PartyMemberIndex)
	body := writer.bytes()
	if len(body) != 16 {
		return nil, fmt.Errorf("cached start-map body length=%d want=16", len(body))
	}
	return body, nil
}

func currentDungeonStartMapActors(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
) ([]currentDungeonStartMapActor, error) {
	if runtime == nil || runtime.Room == nil {
		return nil, errDungeonWorldMapUnavailable
	}
	snapshot := runtime.Room.Snapshot()
	if snapshot.Coordinate != scene.Coordinate || snapshot.MapID != scene.Map.Map.ID || len(snapshot.Monsters) != len(scene.Monsters) {
		return nil, fmt.Errorf("%w: runtime_room=%s runtime_map=%d scene_room=%s scene_map=%d runtime_monsters=%d scene_monsters=%d",
			errDungeonStartMapSceneMismatch,
			snapshot.Coordinate,
			snapshot.MapID,
			scene.Coordinate,
			scene.Map.Map.ID,
			len(snapshot.Monsters),
			len(scene.Monsters),
		)
	}
	if snapshot.SpecialObjectCount != len(scene.SpecialPassiveObjects) || snapshot.AICharacterCount != len(scene.AICharacters) {
		return nil, fmt.Errorf("%w: planned_special=%d scene_special=%d planned_AI=%d scene_AI=%d",
			errDungeonStartMapSceneMismatch,
			snapshot.SpecialObjectCount,
			len(scene.SpecialPassiveObjects),
			snapshot.AICharacterCount,
			len(scene.AICharacters),
		)
	}
	if len(snapshot.OpaqueHostiles) != 0 {
		return nil, fmt.Errorf("%w: count=%d", errDungeonStartMapOpaqueHostile, len(snapshot.OpaqueHostiles))
	}
	if len(snapshot.Monsters)+len(snapshot.ExtendedActors) > 0xff {
		return nil, fmt.Errorf("%w: normal=%d extended=%d", errDungeonStartMapActorCount, len(snapshot.Monsters), len(snapshot.ExtendedActors))
	}

	actors := make([]currentDungeonStartMapActor, 0, len(snapshot.Monsters)+len(snapshot.ExtendedActors))
	for index, monster := range snapshot.Monsters {
		if monster.ObjectKey == 0 || monster.ObjectKey > uint32(^uint16(0)) {
			return nil, fmt.Errorf("%w: index=%d object_key=%d", errDungeonStartMapObjectKeyRange, index, monster.ObjectKey)
		}
		if monster.Spawn.MonsterID <= 0 || monster.Spawn.MonsterID > int64(^uint32(0)) {
			return nil, fmt.Errorf("%w: index=%d code=%d", errDungeonStartMapActorCodeRange, index, monster.Spawn.MonsterID)
		}
		actorType, err := currentDungeonMonsterActorType(monster.Spawn)
		if err != nil {
			return nil, fmt.Errorf("monster index %d code %d: %w", index, monster.Spawn.MonsterID, err)
		}
		level, err := currentDungeonMonsterLevel(monster.Spawn, runtime.Dungeon.Metadata.BasisLevel)
		if err != nil {
			return nil, fmt.Errorf("monster index %d code %d: %w", index, monster.Spawn.MonsterID, err)
		}
		blocking := monster.RoomClearSkipByte
		if actorType == currentDungeonNPCActorType {
			blocking = 1
		}
		actors = append(actors, currentDungeonStartMapActor{
			PacketIndex: uint32(index),
			ObjectKey:   uint16(monster.ObjectKey),
			Code:        uint32(monster.Spawn.MonsterID),
			Level:       level,
			Type:        actorType,
			Blocking:    blocking,
		})
	}
	for _, actor := range snapshot.ExtendedActors {
		actors = append(actors, actor.Packet)
	}
	return actors, nil
}

func currentDungeonMonsterType(rank string) (byte, error) {
	switch strings.ToLower(strings.Trim(rank, "[] \t\r\n")) {
	case "normal", "dummy":
		return 0, nil
	case "champion":
		return 1, nil
	case "super champion", "superchampion":
		return 2, nil
	case "boss":
		return 3, nil
	default:
		return 0, fmt.Errorf("%w: rank=%q", errDungeonStartMapMonsterRank, rank)
	}
}

func currentDungeonMonsterActorType(spawn worldmap.MonsterSpawn) (byte, error) {
	if strings.EqualFold(strings.Trim(spawn.SuffixMarker, "[] \t\r\n"), "boss") {
		return 3, nil
	}
	if strings.EqualFold(strings.Trim(spawn.Rank, "[] \t\r\n"), "npc") {
		return currentDungeonNPCActorType, nil
	}
	return currentDungeonMonsterType(spawn.Rank)
}

func currentDungeonIsBossActorType(actorType byte) bool {
	// 86JP mirrors the old df_game_r kill_monster domain rule:
	// ordinary boss monsters are actor type 3, hostile AI/APC bosses are
	// actor type 8. Both mean "boss death can end combat"; do not restrict
	// the rule to PVF [monster] rows only.
	return actorType == 3 || actorType == 8
}

func currentDungeonMonsterLevel(spawn worldmap.MonsterSpawn, basis worldmap.OptionalInt) (byte, error) {
	level := spawn.AutoLevel
	if spawn.Level != 0 {
		if !basis.Set {
			return 0, fmt.Errorf("%w: relative=%d auto=%d basis=unset", errDungeonStartMapMonsterLevel, spawn.Level, spawn.AutoLevel)
		}
		level += basis.Value
	}
	if level <= 0 || level > 0xff {
		return 0, fmt.Errorf("%w: relative=%d auto=%d basis=%d basis_set=%t resolved=%d",
			errDungeonStartMapMonsterLevel,
			spawn.Level,
			spawn.AutoLevel,
			basis.Value,
			basis.Set,
			level,
		)
	}
	return byte(level), nil
}
