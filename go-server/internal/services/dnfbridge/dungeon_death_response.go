package dnfbridge

import (
	"encoding/binary"
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

var (
	errDungeonDeathResponseKind        = errors.New("dnf dungeon death response kind is unsupported")
	errDungeonDeathResponseKeyRange    = errors.New("dnf dungeon death response object key exceeds current EXE range")
	errDungeonDeathDropCountRange      = errors.New("dnf dungeon death response drop count exceeds current EXE range")
	errDungeonDeathDropItemInvalid     = errors.New("dnf dungeon death response drop item is invalid")
	errDungeonDeathRoomCoordinateRange = errors.New("dnf dungeon death response room coordinate exceeds current EXE range")
)

type currentDungeonDeathResponseKind string

const (
	currentDungeonDeathResponseMonster     currentDungeonDeathResponseKind = "monster"
	currentDungeonDeathResponseAICharacter currentDungeonDeathResponseKind = "ai_character"
)

const currentDungeonZeroDropDeathBodySize = 7

const currentDungeonDeathDropWireSize = 4 + currentItemListEntryWireSize + 2 + 2

// currentDungeonDeathDropWire mirrors only the fields proven by the current
// NoPack sub_1D4E680 reader. SceneObjectKey is the same u32 later echoed by the
// ordinary op43 request and class0/op39 removal response. The first trailing
// u16 remains a constructor sentinel with otherwise unproved semantics.
type currentDungeonDeathDropWire struct {
	SceneObjectKey      uint32
	Item                currentItemListEntry
	UnknownTailSentinel uint16
	OwnerActorObjectKey uint16
}

func buildCurrentDungeonDeathNotificationBody(
	objectKey uint32,
	kind currentDungeonDeathResponseKind,
) ([]byte, error) {
	return buildCurrentDungeonDeathNotificationBodyWithDrops(objectKey, kind, nil)
}

func buildCurrentDungeonDeathNotificationBodyWithDrops(
	objectKey uint32,
	kind currentDungeonDeathResponseKind,
	drops []currentDungeonDeathDropWire,
	optionalRoom ...worldmap.RoomCoordinate,
) ([]byte, error) {
	switch kind {
	case currentDungeonDeathResponseMonster, currentDungeonDeathResponseAICharacter:
	default:
		return nil, errDungeonDeathResponseKind
	}
	if objectKey == 0 || objectKey > uint32(^uint16(0)) {
		return nil, fmt.Errorf("%w: object_key=%d", errDungeonDeathResponseKeyRange, objectKey)
	}
	if len(drops) > int(^uint8(0)) {
		return nil, fmt.Errorf("%w: count=%d", errDungeonDeathDropCountRange, len(drops))
	}
	if len(optionalRoom) > 1 {
		return nil, fmt.Errorf("%w: optional_room_count=%d", errDungeonDeathRoomCoordinateRange, len(optionalRoom))
	}
	if len(optionalRoom) == 1 && (optionalRoom[0].X < 0 || optionalRoom[0].X > 0xff || optionalRoom[0].Y < 0 || optionalRoom[0].Y > 0xff) {
		return nil, fmt.Errorf("%w: room=%s", errDungeonDeathRoomCoordinateRange, optionalRoom[0])
	}
	for index, drop := range drops {
		itemID := binary.LittleEndian.Uint32(drop.Item.data[0x02:0x06])
		amount := binary.LittleEndian.Uint32(drop.Item.data[0x06:0x0A])
		if itemID == 0 && amount == 0 {
			return nil, fmt.Errorf("%w: index=%d item_id=0 amount=0", errDungeonDeathDropItemInvalid, index)
		}
	}

	// Current NoPack class-0/op38 reads:
	// u16 target key, u8 drop count,
	// count * (u32 + raw[0x77] + u16 + owner u16),
	// then u8 post flag, u8 mode, u8 progression, u8 optional-room flag.
	// The zero-drop form remains the canonical seven-byte body.
	var writer packetWriter
	writer.writeUint16(uint16(objectKey))
	writer.writeByte(byte(len(drops)))
	for _, drop := range drops {
		writer.writeUint32(drop.SceneObjectKey)
		writer.writeBytes(drop.Item.data[:])
		writer.writeUint16(drop.UnknownTailSentinel)
		writer.writeUint16(drop.OwnerActorObjectKey)
	}
	writer.writeByte(0)    // post flag: no optional u16
	writer.writeByte(0)    // mode 0
	writer.writeByte(0xff) // no progression update
	if len(optionalRoom) == 1 {
		writer.writeByte(1)
		writer.writeByte(byte(optionalRoom[0].X))
		writer.writeByte(byte(optionalRoom[0].Y))
	} else {
		writer.writeByte(0)
	}
	return writer.bytes(), nil
}

func currentDungeonDeathDropItemState(
	sceneSlot uint16,
	definition dungeonDropItemDefinition,
	amount uint32,
	qualitySeed ...uint32,
) (currentItemListEntry, error) {
	if sceneSlot == 0 || amount == 0 {
		return currentItemListEntry{}, fmt.Errorf(
			"%w: scene_slot=%d item_id=%d amount=%d",
			errDungeonDeathDropItemInvalid,
			sceneSlot,
			definition.ItemID,
			amount,
		)
	}
	instanceValue := amount
	if definition.Kind == dungeonDropItemEquipment {
		instanceValue = 0
		if len(qualitySeed) > 0 && validCurrentEquipmentQualitySeed(qualitySeed[0]) {
			instanceValue = qualitySeed[0]
		}
		if instanceValue == 0 {
			var err error
			instanceValue, err = newCurrentEquipmentQualitySeed()
			if err != nil {
				return currentItemListEntry{}, err
			}
		}
	}
	var entry currentItemListEntry
	entry.patchCore(int16(sceneSlot), definition.ItemID, instanceValue)
	if definition.Kind == dungeonDropItemEquipment && definition.Durability != 0 {
		entry.setUint16(0x0B, definition.Durability)
	}
	return entry, nil
}
