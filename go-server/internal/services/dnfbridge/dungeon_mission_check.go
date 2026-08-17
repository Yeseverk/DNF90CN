package dnfbridge

import (
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

// handleDungeonMissionCheckSuccess recognizes the exact current-EXE request
// without inventing the dungeon-mission reward that its class-1 response must
// carry. Current EXE sub_1DBAE90 proves the success payload is
// missionSuccess=1 followed by a real item-template ID. The bridge has no
// active op558 mission instance or authoritative reward source yet, so it must
// expose the missing evidence rather than send a fake item ID or conflate this
// general dungeon side-mission ACK with room gates or return-town.
func (s *Service) handleDungeonMissionCheckSuccess(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	if len(body) != 0 {
		s.logGameEvent(session, "game-dungeon-mission-check-success-blocked",
			"body_len", len(body),
			"reason", "current_exe_op560_body_must_be_empty")
		return nil
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil {
		s.logGameEvent(session, "game-dungeon-mission-check-success-deferred",
			"char_id", session.selectedCharacterID,
			"request_msg_id", uint16(dnfenum.CmdPacketDungeonMissionCheckSuccess),
			"classification", 1,
			"body_len", len(body),
			"runtime_owned", false,
			"response_sent", false,
			"reason", "active_dungeon_mission_reward_source_missing")
		return nil
	}
	snapshot := runtime.Session.Snapshot()

	s.logGameEvent(session, "game-dungeon-mission-check-success-deferred",
		"char_id", session.selectedCharacterID,
		"request_msg_id", uint16(dnfenum.CmdPacketDungeonMissionCheckSuccess),
		"classification", 1,
		"body_len", len(body),
		"dungeon_id", snapshot.Run.DungeonID,
		"maze_index", snapshot.Run.MazeIndex,
		"room", snapshot.Run.Current.String(),
		"map_id", snapshot.Scene.Map.Map.ID,
		"run_status", snapshot.Run.Status,
		"runtime_owned", true,
		"boss_die_check_accepted", runtime.bossDieCheckAccepted,
		"response_sent", false,
		"reason", "active_dungeon_mission_reward_source_missing")
	return nil
}
