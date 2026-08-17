package dnfbridge

import (
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

var (
	errDungeonEntryModeUnsupported   = errors.New("dnf dungeon entry mode is not supported by the current packet owner")
	errDungeonBossCoordinateRequired = errors.New("dnf dungeon boss coordinate is required")
	errDungeonBossCoordinateRange    = errors.New("dnf dungeon boss coordinate exceeds current EXE range")
	errDungeonMazeIndexRange         = errors.New("dnf dungeon maze index exceeds current EXE range")
	errDungeonRandomizedObjectOwner  = errors.New("dnf dungeon randomized object owner is required")
)

const (
	currentDungeonInfoNotification  uint16 = 28
	currentDungeonStartNotification uint16 = 29
)

type currentDungeonEntryPackets struct {
	DungeonInfo []byte
	StartMap    []byte
}

func buildCurrentDungeonEntryPackets(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
) (currentDungeonEntryPackets, error) {
	if runtime == nil || runtime.Room == nil || runtime.Session == nil {
		return currentDungeonEntryPackets{}, errDungeonWorldMapUnavailable
	}
	if err := validateCurrentDungeonEntryRequest(runtime.Request); err != nil {
		return currentDungeonEntryPackets{}, err
	}
	if runtime.Dungeon.ID <= 0 || runtime.Dungeon.ID > int64(^uint32(0)) {
		return currentDungeonEntryPackets{}, fmt.Errorf("%w: dungeon=%d", errDungeonStartMapMapIDRange, runtime.Dungeon.ID)
	}
	if runtime.MazeIndex < 0 || runtime.MazeIndex > int(^uint8(0)) {
		return currentDungeonEntryPackets{}, fmt.Errorf("%w: maze=%d", errDungeonMazeIndexRange, runtime.MazeIndex)
	}
	if !runtime.BossSet {
		return currentDungeonEntryPackets{}, errDungeonBossCoordinateRequired
	}
	if runtime.BossCoordinate.X < 0 || runtime.BossCoordinate.X > 0xff || runtime.BossCoordinate.Y < 0 || runtime.BossCoordinate.Y > 0xff {
		return currentDungeonEntryPackets{}, fmt.Errorf("%w: boss=%s", errDungeonBossCoordinateRange, runtime.BossCoordinate)
	}
	if runtime.MazeIndex >= len(runtime.Dungeon.Mazes) {
		return currentDungeonEntryPackets{}, fmt.Errorf("%w: maze=%d count=%d", worldmap.ErrMazeNotFound, runtime.MazeIndex, len(runtime.Dungeon.Mazes))
	}
	if count := len(runtime.Dungeon.Mazes[runtime.MazeIndex].RandomizedObjects); count != 0 {
		return currentDungeonEntryPackets{}, fmt.Errorf("%w: count=%d", errDungeonRandomizedObjectOwner, count)
	}
	// Map [special passive object] child rows such as [item], [trap], [quest],
	// and [hellparty] are script metadata owned by the type-9 passive object.
	// The current op29 reader has no actor row for those child kinds: only the
	// passive object itself and [monster] children belong to Actors. ExtraEntries
	// are a different table of materialized item drops and must not be invented
	// from these metadata rows. Keep the retained rows in the room runtime for
	// their later feature owners, but do not block the typed op28/op29 entry.

	// These values are protocol sentinels, not map fallbacks. The current EXE
	// branches on packetSeed == UINT32_MAX to skip the absent hell-party state.
	infoPacket := currentDungeonInfo{
		DungeonID:      uint32(runtime.Dungeon.ID),
		Difficulty:     runtime.Request.Difficulty,
		EntryOption:    runtime.Request.EntryOption,
		MazeIndex:      byte(runtime.MazeIndex),
		BossX:          byte(runtime.BossCoordinate.X),
		BossY:          byte(runtime.BossCoordinate.Y),
		HellPartyRoomX: 0xff,
		HellPartyRoomY: 0xff,
		DungeonValue:   12,
		PacketSeed:     ^uint32(0),
	}
	dungeonInfoBody, err := infoPacket.Build()
	if err != nil {
		return currentDungeonEntryPackets{}, fmt.Errorf("build current dungeon-info body: %w", err)
	}
	partyMemberIndex := byte(0xff)
	if runtime.partyMemberIndexed {
		partyMemberIndex = runtime.partyMemberIndex
	}
	startPacket, err := currentDungeonStartMapFromRuntime(runtime, scene, currentDungeonStartMapState{
		Seed:             runtime.Seed,
		RoomStateValue:   1,
		RoomStateFlag:    currentDungeonStartMapPayloadBuild,
		PartyMemberIndex: partyMemberIndex,
	})
	if err != nil {
		return currentDungeonEntryPackets{}, fmt.Errorf("plan current start-map body: %w", err)
	}
	startMapBody, err := startPacket.Build()
	if err != nil {
		return currentDungeonEntryPackets{}, fmt.Errorf("build current start-map body: %w", err)
	}
	return currentDungeonEntryPackets{DungeonInfo: dungeonInfoBody, StartMap: startMapBody}, nil
}

func validateCurrentDungeonEntryRequest(request dungeoncmd.SelectDungeonRequest) error {
	// The current fixed 21-byte writer has now been observed sending a nonzero
	// u32 at offset 16 in an otherwise ordinary solo request. That value is not
	// consumed by either typed op28 or op29 builder, and its current semantics
	// are not proved. Preserve it in the runtime request, but do not reject the
	// normal entry mode or infer leader/quest authority from it.
	runtimeStateSupported := request.RuntimeState == 0
	// Live production traffic proves the current EXE writes runtime state 1
	// only for the training-room request: dungeon 5000, token UINT16_MAX, and
	// every other owned mode field zero. The field is not consumed by the
	// typed op28/op29 builders, so accept only that exact evidence-backed
	// compatibility unit instead of opening state 1 for ordinary dungeons.
	if request.DungeonID == 5000 && request.RuntimeState == 1 && request.RuntimeToken == 0xffff {
		runtimeStateSupported = true
	}
	if request.EntryOption != 0 || request.SelectionMode != 0 || !runtimeStateSupported ||
		(request.RuntimeToken != 0 && request.RuntimeToken != 0xffff) || request.Reserved != 0 || request.PartyState != 0 ||
		request.SpecialMode != 0 || len(request.OpaqueTail) != 0 {
		return fmt.Errorf("%w: entry_option=%d selection_mode=%d runtime_state=%d runtime_token=%d reserved=%d party_state=%d offset16_unproven=%d special_mode=%d opaque_tail=%d",
			errDungeonEntryModeUnsupported,
			request.EntryOption,
			request.SelectionMode,
			request.RuntimeState,
			request.RuntimeToken,
			request.Reserved,
			request.PartyState,
			request.LeaderObjectKey,
			request.SpecialMode,
			len(request.OpaqueTail),
		)
	}
	return nil
}
