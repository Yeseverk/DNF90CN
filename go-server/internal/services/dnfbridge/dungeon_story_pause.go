package dnfbridge

import (
	"encoding/binary"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const (
	currentDungeonStoryPauseRequestSize  = 2
	currentDungeonStoryPauseResponseSize = 4
	currentDungeonStoryPauseMsgID        = uint16(170)
)

// handleCurrentDungeonStoryPause closes the current-EXE op191 -> class0/op170
// story-state exchange. The current client also uses the state-1/type-1 edge
// after the opening tutorial cinematic as its first proved room-ready
// boundary. Tutorial op3 must not be sent at the earlier op120 boundary: the
// current EXE can still have a nil active-room object manager there.
func (s *Service) handleCurrentDungeonStoryPause(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	if len(body) != currentDungeonStoryPauseRequestSize {
		s.logGameEvent(session, "game-dungeon-story-pause-blocked",
			"body_len", len(body),
			"expected_body_len", currentDungeonStoryPauseRequestSize,
			"reason", "current_exe_op191_plaintext_boundary_mismatch")
		return nil
	}
	stateFlag := body[0]
	requestType := body[1]
	if stateFlag > 1 || requestType > 2 {
		s.logGameEvent(session, "game-dungeon-story-pause-blocked",
			"body_len", len(body),
			"state_flag", stateFlag,
			"request_type", requestType,
			"reason", "current_exe_op191_writer_domain_mismatch")
		return nil
	}
	if session.selectedCharacterID == 0 {
		s.logGameEvent(session, "game-dungeon-story-pause-blocked",
			"body_len", len(body),
			"state_flag", stateFlag,
			"request_type", requestType,
			"reason", "selected_character_missing")
		return nil
	}

	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil ||
		!dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		s.logGameEvent(session, "game-dungeon-story-pause-blocked",
			"char_id", session.selectedCharacterID,
			"body_len", len(body),
			"state_flag", stateFlag,
			"request_type", requestType,
			"reason", "active_dungeon_runtime_owner_mismatch")
		return nil
	}

	actorObjectKey := currentSceneActorObjectKey(session.selectedCharacterID)
	response := make([]byte, currentDungeonStoryPauseResponseSize)
	binary.LittleEndian.PutUint16(response[0:2], actorObjectKey)
	response[2] = stateFlag
	response[3] = requestType
	if err := s.sendGameUpperRawClass(
		session,
		currentDungeonStoryPauseMsgID,
		response,
		0,
	); err != nil {
		return err
	}
	s.logGameEvent(session, "game-dungeon-story-pause-op170-sent",
		"request_msg_id", uint16(dnfenum.CmdPacketDungeonEventStoryPause),
		"response_msg_id", currentDungeonStoryPauseMsgID,
		"classification", 0,
		"char_id", session.selectedCharacterID,
		"actor_object_key", actorObjectKey,
		"state_flag", stateFlag,
		"request_type", requestType,
		"body_len", len(response),
		"body_source", "current_exe_sub_1D3A650_u16_actor_u8_state_u8_type")
	if stateFlag != 1 || requestType != 1 || !isPVFTutorialDungeon(runtime) ||
		runtime.tutorialInitialUserStateSent {
		return nil
	}
	if !session.postStartMapPlayerStateSent || !session.currentFinishLoadingStateSent {
		s.logGameEvent(session, "game-dungeon-tutorial-user-state-deferred",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"state_flag", stateFlag,
			"request_type", requestType,
			"post_start_map_state_sent", session.postStartMapPlayerStateSent,
			"finish_loading_state_sent", session.currentFinishLoadingStateSent,
			"reason", "tutorial_story_resume_arrived_before_scene_ready")
		return nil
	}
	userStateBody, err := buildCurrentDungeonUserStateBody(actorObjectKey)
	if err != nil {
		return err
	}
	if err := s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketNotifyUserState),
		userStateBody,
		0,
	); err != nil {
		return err
	}
	runtime.tutorialInitialUserStateSent = true
	s.logGameEvent(session, "game-dungeon-tutorial-user-state-op3-sent",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"room", runtime.Session.Snapshot().Run.Current,
		"actor_object_key", actorObjectKey,
		"request_msg_id", uint16(dnfenum.CmdPacketDungeonEventStoryPause),
		"request_state_flag", stateFlag,
		"request_type", requestType,
		"response_msg_id", uint16(dnfenum.CmdPacketNotifyUserState),
		"classification", 0,
		"body_len", len(userStateBody),
		"user_state", currentDungeonPlayerUserState,
		"body_source", "current_exe_sub_1D88A10_after_op191_state1_type1_story_resume_room_ready")
	return nil
}
