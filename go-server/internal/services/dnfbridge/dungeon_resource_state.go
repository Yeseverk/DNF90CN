package dnfbridge

import (
	"context"
	"sort"
	"strconv"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	currentDungeonResourceStateMsgID = 5
	// The accepted select-dungeon request owns state 1 for its selected PVF dungeon.
	currentDungeonResourceSelectedState = 1
)

type currentDungeonResourceStateEntry struct {
	DungeonID uint32
	State     byte
}

func buildCurrentDungeonResourceStateEntriesBody(entries []currentDungeonResourceStateEntry) []byte {
	var writer packetWriter
	if len(entries) > int(^uint16(0)) {
		entries = entries[:int(^uint16(0))]
	}
	writer.writeUint16(uint16(len(entries)))
	for _, entry := range entries {
		writer.writeUint32(entry.DungeonID)
		writer.writeByte(entry.State)
	}
	return writer.bytes()
}

func buildCurrentDungeonResourceStateBody(dungeonID uint32) []byte {
	return buildCurrentDungeonResourceStateEntriesBody([]currentDungeonResourceStateEntry{{
		DungeonID: dungeonID,
		State:     currentDungeonResourceSelectedState,
	}})
}

func (s *Service) sendCurrentDungeonResourceState(
	session *gameSession,
	runtime *runtimeDungeonState,
	source string,
) error {
	if session == nil || runtime == nil || runtime.Dungeon.ID <= 0 {
		return nil
	}
	body := buildCurrentDungeonResourceStateBody(uint32(runtime.Dungeon.ID))
	s.logGameEvent(session, "game-upper-current-dungeon-resource-state-op5-send",
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", currentDungeonResourceStateMsgID,
		"classification", 0,
		"body_len", len(body),
		"row_count", 1,
		"dungeon_id", runtime.Dungeon.ID,
		"state", currentDungeonResourceSelectedState,
		"state_source", "validated_current_select_dungeon_request_and_real_pvf_runtime",
		"body_source", "current_exe_sub_1D37AC0_u16_count_u32_key_u8_state")
	return s.sendGameUpperRawClass(session, currentDungeonResourceStateMsgID, body, 0)
}

func (s *Service) sendCurrentDungeonPermissionSnapshot(
	ctx context.Context,
	session *gameSession,
	source string,
) error {
	entries, err := s.currentDungeonPermissionSnapshotEntries(ctx, session)
	if err != nil {
		s.logGameEvent(session, "game-upper-current-dungeon-permission-op5-snapshot-blocked",
			"source", source,
			"char_id", sessionSelectedCharacterID(session),
			"reason", "repository_or_worldmap_unavailable",
			"error", err)
		return nil
	}
	if len(entries) == 0 {
		s.logGameEvent(session, "game-upper-current-dungeon-permission-op5-snapshot-empty",
			"source", source,
			"char_id", sessionSelectedCharacterID(session),
			"reason", "no_persisted_current_exe_dungeon_clear_state")
		return nil
	}
	body := buildCurrentDungeonResourceStateEntriesBody(entries)
	s.logGameEvent(session, "game-upper-current-dungeon-permission-op5-snapshot-send",
		"source", source,
		"char_id", sessionSelectedCharacterID(session),
		"msg_id", currentDungeonResourceStateMsgID,
		"classification", 0,
		"body_len", len(body),
		"row_count", len(entries),
		"body_source", "current_exe_sub_1D37AC0_u16_count_u32_key_u8_state",
		"state_source", "persisted_dungeon_clear_state")
	return s.sendGameUpperRawClass(session, currentDungeonResourceStateMsgID, body, 0)
}

func (s *Service) currentDungeonPermissionSnapshotEntries(
	ctx context.Context,
	session *gameSession,
) ([]currentDungeonResourceStateEntry, error) {
	if session == nil || session.selectedCharacterID == 0 {
		return nil, nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.DungeonPermission == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	record, found, err := repositories.DungeonPermission.Load(ctx, characterID)
	if err != nil || !found {
		return nil, err
	}
	table, _, err := s.dungeonWorldMap()
	if err != nil {
		return nil, err
	}
	entries := currentDungeonResourceStateEntriesFromPermissionRecord(table, record)
	return entries, nil
}

func currentDungeonResourceStateEntriesFromPermissionRecord(
	table *worldmap.Table,
	record dnfrepo.DungeonPermissionRecord,
) []currentDungeonResourceStateEntry {
	if table == nil || len(record.Entries) == 0 {
		return nil
	}
	out := make([]currentDungeonResourceStateEntry, 0, len(record.Entries))
	for _, entry := range record.Entries {
		if entry.DungeonID == 0 || entry.ClearState == 0 {
			continue
		}
		dungeon, found := table.FindDungeon(int64(entry.DungeonID))
		if !found {
			continue
		}
		state := entry.ClearState
		if maxState := currentDungeonMaxClearState(dungeon); maxState == 0 {
			continue
		} else if state > maxState {
			state = maxState
		}
		out = append(out, currentDungeonResourceStateEntry{
			DungeonID: entry.DungeonID,
			State:     state,
		})
	}
	sort.SliceStable(out, func(left, right int) bool {
		return out[left].DungeonID < out[right].DungeonID
	})
	return out
}

func currentDungeonMaxClearState(dungeon worldmap.Dungeon) byte {
	if len(dungeon.Metadata.DifficultyLevels) != 0 {
		count := 0
		for _, level := range dungeon.Metadata.DifficultyLevels {
			if level != 0 {
				count++
			}
		}
		if count > 1 {
			if count-1 > int(^uint8(0)) {
				return ^uint8(0)
			}
			return byte(count - 1)
		}
		return 0
	}
	if len(dungeon.Metadata.DesignatedDifficulties) != 0 {
		return 4
	}
	if dungeon.Metadata.Difficulty.Set && dungeon.Metadata.Difficulty.Value >= 0 {
		return 4
	}
	return 0
}

func currentDungeonNextClearState(dungeon worldmap.Dungeon, difficulty byte) byte {
	maxState := currentDungeonMaxClearState(dungeon)
	if maxState == 0 {
		return 0
	}
	next := difficulty + 1
	if next == 0 {
		next = 1
	}
	if next > maxState {
		next = maxState
	}
	return next
}

func (s *Service) commitCurrentDungeonPermission(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
) ([]byte, byte, bool, error) {
	if session == nil || runtime == nil || session.selectedCharacterID == 0 || runtime.Dungeon.ID <= 0 {
		return nil, 0, false, nil
	}
	state := currentDungeonNextClearState(runtime.Dungeon, runtime.Request.Difficulty)
	if state == 0 || runtime.Dungeon.ID > int64(^uint32(0)) {
		return nil, 0, false, nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.DungeonPermission == nil {
		return nil, 0, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	entry, updated, err := repositories.DungeonPermission.UpsertMax(
		ctx,
		characterID,
		uint32(runtime.Dungeon.ID),
		state,
	)
	if err != nil {
		return nil, 0, false, err
	}
	if !updated {
		s.logGameEvent(session, "game-dungeon-permission-clear-state-unchanged",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"difficulty", runtime.Request.Difficulty,
			"state", entry.ClearState,
			"state_source", "persisted_clear_state_already_at_or_above_current_clear")
		return nil, entry.ClearState, false, nil
	}
	body := buildCurrentDungeonResourceStateEntriesBody([]currentDungeonResourceStateEntry{{
		DungeonID: entry.DungeonID,
		State:     entry.ClearState,
	}})
	s.logGameEvent(session, "game-dungeon-permission-clear-state-updated",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"difficulty", runtime.Request.Difficulty,
		"state", entry.ClearState,
		"body_len", len(body),
		"body_source", "current_exe_sub_1D37AC0_u16_count_u32_key_u8_state",
		"state_source", "settlement_clear_difficulty_plus_one_capped_by_runtime_pvf")
	return body, entry.ClearState, true, nil
}

func sessionSelectedCharacterID(session *gameSession) uint16 {
	if session == nil {
		return 0
	}
	return session.selectedCharacterID
}
