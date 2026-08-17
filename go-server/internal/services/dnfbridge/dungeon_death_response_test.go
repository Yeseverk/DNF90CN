package dnfbridge

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestBuildCurrentDungeonDeathNotificationBodyMatchesCurrentEXEOp38ZeroDropReader(t *testing.T) {
	tests := []struct {
		name      string
		objectKey uint32
		kind      currentDungeonDeathResponseKind
		want      []byte
	}{
		{name: "tutorial target 402", objectKey: 402, kind: currentDungeonDeathResponseMonster, want: []byte{0x92, 0x01, 0, 0, 0, 0xff, 0}},
		{name: "hostile AI character 403", objectKey: 403, kind: currentDungeonDeathResponseAICharacter, want: []byte{0x93, 0x01, 0, 0, 0, 0xff, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := buildCurrentDungeonDeathNotificationBody(test.objectKey, test.kind)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body, test.want) {
				t.Fatalf("body=%x want=%x", body, test.want)
			}
		})
	}
	if _, err := buildCurrentDungeonDeathNotificationBody(402, "future"); !errors.Is(err, errDungeonDeathResponseKind) {
		t.Fatalf("unsupported kind error=%v", err)
	}
	for _, objectKey := range []uint32{0, 1 << 16} {
		if _, err := buildCurrentDungeonDeathNotificationBody(objectKey, currentDungeonDeathResponseMonster); !errors.Is(err, errDungeonDeathResponseKeyRange) {
			t.Fatalf("object key %d error=%v", objectKey, err)
		}
	}
}

func TestBuildCurrentDungeonDeathNotificationBodyCarriesOptionalStoryRoom(t *testing.T) {
	body, err := buildCurrentDungeonDeathNotificationBodyWithDrops(
		402,
		currentDungeonDeathResponseMonster,
		nil,
		worldmap.RoomCoordinate{X: 4, Y: 0},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x92, 0x01, 0, 0, 0, 0xff, 1, 4, 0}
	if !bytes.Equal(body, want) {
		t.Fatalf("story-room body=%x want=%x", body, want)
	}
	if _, err := buildCurrentDungeonDeathNotificationBodyWithDrops(
		402,
		currentDungeonDeathResponseMonster,
		nil,
		worldmap.RoomCoordinate{X: 256},
	); !errors.Is(err, errDungeonDeathRoomCoordinateRange) {
		t.Fatalf("out-of-range story room error=%v", err)
	}
}

func TestBuildCurrentDungeonDeathNotificationBodyWithDropMatchesCurrentEXEOp38Reader(t *testing.T) {
	definition := dungeonDropItemDefinition{
		ItemID:     9001,
		Kind:       dungeonDropItemEquipment,
		PVFPath:    "equipment/weapon/test_sword.equ",
		Durability: 57,
	}
	const qualitySeed = uint32(345678901)
	itemState, err := currentDungeonDeathDropItemState(21, definition, 1, qualitySeed)
	if err != nil {
		t.Fatal(err)
	}
	body, err := buildCurrentDungeonDeathNotificationBodyWithDrops(
		402,
		currentDungeonDeathResponseMonster,
		[]currentDungeonDeathDropWire{{
			SceneObjectKey:      0x44332211,
			Item:                itemState,
			UnknownTailSentinel: 0x6655,
			OwnerActorObjectKey: 401,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := currentDungeonZeroDropDeathBodySize + currentDungeonDeathDropWireSize; len(body) != want {
		t.Fatalf("body len=%d want=%d", len(body), want)
	}
	if got := binary.LittleEndian.Uint16(body[0:2]); got != 402 || body[2] != 1 {
		t.Fatalf("header target=%d count=%d", got, body[2])
	}
	if got := binary.LittleEndian.Uint32(body[3:7]); got != 0x44332211 {
		t.Fatalf("prefix=%#x", got)
	}
	itemOffset := 7
	if got := binary.LittleEndian.Uint16(body[itemOffset : itemOffset+2]); got != 21 {
		t.Fatalf("scene slot=%d", got)
	}
	if got := binary.LittleEndian.Uint32(body[itemOffset+2 : itemOffset+6]); got != 9001 {
		t.Fatalf("item id=%d", got)
	}
	if got := binary.LittleEndian.Uint32(body[itemOffset+6 : itemOffset+10]); got != qualitySeed {
		t.Fatalf("quality seed=%d want=%d", got, qualitySeed)
	}
	if got := binary.LittleEndian.Uint16(body[itemOffset+0x0B : itemOffset+0x0D]); got != 57 {
		t.Fatalf("durability=%d", got)
	}
	tailOffset := itemOffset + currentItemListEntryWireSize
	if got := binary.LittleEndian.Uint16(body[tailOffset : tailOffset+2]); got != 0x6655 {
		t.Fatalf("tail value=%#x", got)
	}
	if got := binary.LittleEndian.Uint16(body[tailOffset+2 : tailOffset+4]); got != 401 {
		t.Fatalf("owner actor object key=%d", got)
	}
	if got, want := body[len(body)-4:], []byte{0, 0, 0xff, 0}; !bytes.Equal(got, want) {
		t.Fatalf("tail=%x want=%x", got, want)
	}
}

func TestCurrentDungeonDeathDropBuilderRejectsInvalidRowsAndCount(t *testing.T) {
	if _, err := currentDungeonDeathDropItemState(0, dungeonDropItemDefinition{ItemID: 100, Kind: dungeonDropItemStackable}, 1); !errors.Is(err, errDungeonDeathDropItemInvalid) {
		t.Fatalf("zero scene slot error=%v", err)
	}
	goldState, err := currentDungeonDeathDropItemState(1, dungeonDropItemDefinition{Kind: dungeonDropItemStackable}, 7)
	if err != nil {
		t.Fatalf("gold item row rejected: %v", err)
	}
	if got := binary.LittleEndian.Uint32(goldState.data[0x02:0x06]); got != 0 {
		t.Fatalf("gold item id=%d want 0", got)
	}
	if got := binary.LittleEndian.Uint32(goldState.data[0x06:0x0A]); got != 7 {
		t.Fatalf("gold amount=%d want 7", got)
	}
	if _, err := currentDungeonDeathDropItemState(1, dungeonDropItemDefinition{ItemID: 100, Kind: dungeonDropItemStackable}, 0); !errors.Is(err, errDungeonDeathDropItemInvalid) {
		t.Fatalf("zero amount error=%v", err)
	}
	if _, err := buildCurrentDungeonDeathNotificationBodyWithDrops(
		402,
		currentDungeonDeathResponseMonster,
		make([]currentDungeonDeathDropWire, 256),
	); !errors.Is(err, errDungeonDeathDropCountRange) {
		t.Fatalf("drop count error=%v", err)
	}
	if _, err := buildCurrentDungeonDeathNotificationBodyWithDrops(
		402,
		currentDungeonDeathResponseMonster,
		[]currentDungeonDeathDropWire{{}},
	); !errors.Is(err, errDungeonDeathDropItemInvalid) {
		t.Fatalf("empty item row error=%v", err)
	}
}
