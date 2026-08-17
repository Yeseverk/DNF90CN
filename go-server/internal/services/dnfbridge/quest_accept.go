package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
)

const currentAcceptQuestRequestBodySize = 4

// currentAcceptQuestReplayKey identifies an already-acknowledged active
// quest in one TCP session. A quest cannot be accepted twice while active, so
// no later player action can legitimately need another same-session accept
// ACK for this key.
type currentAcceptQuestReplayKey struct {
	characterID uint16
	questID     uint16
}

func newCurrentAcceptQuestReplayKey(characterID uint16, request dnfquest.QuestIDRequest) currentAcceptQuestReplayKey {
	return currentAcceptQuestReplayKey{characterID: characterID, questID: request.QuestID}
}

func (session *gameSession) currentAcceptQuestReplaySuppressed(key currentAcceptQuestReplayKey) bool {
	if session == nil {
		return false
	}
	session.questReplay.acceptMu.Lock()
	defer session.questReplay.acceptMu.Unlock()
	_, ok := session.questReplay.acceptAcknowledged[key]
	return ok
}

func (session *gameSession) suppressCurrentAcceptQuestReplay(key currentAcceptQuestReplayKey) {
	if session == nil {
		return
	}
	session.questReplay.acceptMu.Lock()
	defer session.questReplay.acceptMu.Unlock()
	if session.questReplay.acceptAcknowledged == nil {
		session.questReplay.acceptAcknowledged = make(map[currentAcceptQuestReplayKey]struct{}, 1)
	}
	session.questReplay.acceptAcknowledged[key] = struct{}{}
}

func (session *gameSession) clearCurrentAcceptQuestReplay(key currentAcceptQuestReplayKey) {
	if session == nil {
		return
	}
	session.questReplay.acceptMu.Lock()
	defer session.questReplay.acceptMu.Unlock()
	delete(session.questReplay.acceptAcknowledged, key)
}

// shouldSuppressKnownQuestReplayBeforeGameUpperLog avoids logging an already
// acknowledged terminal quest packet at retry-loop speed. It accepts only the
// exact current plaintext request shapes; malformed or changed requests keep
// their normal logging and validation path.
func shouldSuppressKnownQuestReplayBeforeGameUpperLog(session *gameSession, msgID uint16, classification byte, body []byte) bool {
	if session == nil || classification != dnfproto.DefaultChannelClassification {
		return false
	}
	switch msgID {
	case uint16(dnfenum.CmdPacketAcceptQuest):
		if len(body) != currentAcceptQuestRequestBodySize ||
			binary.LittleEndian.Uint16(body[:2]) != uint16(dnfenum.CmdPacketAcceptQuest) {
			return false
		}
		request, err := dnfquest.DecodeQuestIDRequest(body)
		return err == nil && session.currentAcceptQuestReplaySuppressed(newCurrentAcceptQuestReplayKey(session.selectedCharacterID, request))
	case uint16(dnfenum.CmdPacketGiveupQuest):
		if len(body) != currentGiveUpQuestRequestBodySize ||
			binary.LittleEndian.Uint16(body[:2]) != uint16(dnfenum.CmdPacketGiveupQuest) {
			return false
		}
		request, err := dnfquest.DecodeQuestIDRequest(body)
		return err == nil && session.currentGiveUpQuestReplaySuppressed(newCurrentGiveUpQuestReplayKey(session.selectedCharacterID, request))
	case uint16(dnfenum.CmdPacketSetQuestTrigger):
		if len(body) != currentSetQuestTriggerRequestBodySize ||
			binary.LittleEndian.Uint16(body[:2]) != uint16(dnfenum.CmdPacketSetQuestTrigger) {
			return false
		}
		request, err := dnfquest.DecodeSetTriggerRequest(body)
		return err == nil && session.currentSetQuestTriggerReplaySuppressed(newCurrentSetQuestTriggerReplayKey(session.selectedCharacterID, request))
	default:
		return false
	}
}

func (s *Service) handleCurrentAcceptQuest(session *gameSession, body []byte) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if len(body) != currentAcceptQuestRequestBodySize || binary.LittleEndian.Uint16(body[:2]) != uint16(dnfenum.CmdPacketAcceptQuest) {
		s.logGameEvent(session, "game-upper-accept-quest-blocked",
			"body_len", len(body),
			"reason", "current_exe_op31_requires_exact_echo_and_quest_id")
		return nil
	}
	request, err := dnfquest.DecodeQuestIDRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-upper-accept-quest-blocked",
			"body_len", len(body),
			"reason", "request_decode_failed",
			"error", err)
		return nil
	}
	replayKey := newCurrentAcceptQuestReplayKey(session.selectedCharacterID, request)
	if session.currentAcceptQuestReplaySuppressed(replayKey) {
		return nil
	}
	if suppressed, readiness, routeStage := s.suppressSceneInitializationAcceptQuestRequest(session); suppressed {
		s.logGameEvent(session, "game-upper-accept-quest-scene-initialization-request-suppressed",
			"quest_id", request.QuestID,
			"route_stage", routeStage,
			"scene_readiness", readiness,
			"reason", "login_or_scene_initialization_trigger_is_not_player_accept_intent",
			"response", "none",
			"quest_mutation", "none",
			"preserved_scope", "player_accept_after_scene_actor_ready")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		s.logGameEvent(session, "game-upper-accept-quest-blocked",
			"quest_id", request.QuestID,
			"reason", "quest_catalog_unavailable",
			"error", err)
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || repositories.Quest == nil {
		s.logGameEvent(session, "game-upper-accept-quest-blocked",
			"quest_id", request.QuestID,
			"reason", "character_or_quest_repository_unavailable")
		return nil
	}
	_, _, character, found := s.selectedCharacterForEnter(ctx, session)
	if !found {
		s.logGameEvent(session, "game-upper-accept-quest-blocked",
			"quest_id", request.QuestID,
			"reason", "selected_character_not_found")
		return nil
	}
	job, validJob := characterJobByte(character)
	if !validJob || character.Level <= 0 {
		s.logGameEvent(session, "game-upper-accept-quest-blocked",
			"quest_id", request.QuestID,
			"job", character.Job,
			"level", character.Level,
			"reason", "selected_character_job_or_level_invalid")
		return nil
	}
	owner, err := dnfquest.NewOwner(repositories)
	if err != nil {
		return fmt.Errorf("create quest owner: %w", err)
	}
	result, err := owner.ApplyAccept(ctx, catalog, dnfquest.CharacterEligibility{
		Level:    character.Level,
		Job:      int(job),
		GrowType: int(numericCharacterStatValue(character, "grow_type")),
	}, dnfquest.NewQuestIDCommand(alignedcmd.Request{
		AccountID:           s.accountIDForSession(session),
		SelectedCharacterID: session.selectedCharacterID,
	}, "accept_quest", request))
	if err != nil {
		s.logGameEvent(session, "game-upper-accept-quest-blocked",
			"quest_id", request.QuestID,
			"job", job,
			"level", character.Level,
			"reason", "pvf_db_accept_preflight_or_persist_failed",
			"error", err)
		if errors.Is(err, dnfquest.ErrQuestNotAcceptable) {
			// The current client can repeat a passive op31 for a quest that was
			// already activated/completed by the PVF quest graph. Returning
			// current failure 23 for every such repeat opens the unsolicited
			// "cannot accept quest" popup after login. Keep this exact state
			// mismatch silent: it performs no mutation and does not disable the
			// ordinary eligible, player-driven accept path below.
			s.logGameEvent(session, "game-upper-accept-quest-passive-repeat-suppressed",
				"quest_id", request.QuestID,
				"job", job,
				"level", character.Level,
				"response", "none",
				"quest_mutation", "none",
				"reason", "known_quest_not_currently_acceptable_passive_client_repeat")
			return nil
		}
		if errors.Is(err, dnfquest.ErrQuestDefinitionMissing) ||
			errors.Is(err, dnfquest.ErrQuestInitialTriggerUnsupported) {
			// Current EXE sub_1D29B40 consumes no op31-specific failure
			// fields. Error 23 selects the current "cannot accept quest"
			// client resource. Repository and event-item transaction gaps do
			// not use this business error because their state is not closed.
			return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketAcceptQuest), 23)
		}
		return nil
	}
	if !result.Idempotent {
		// ApplyAccept has committed a new active generation. Drop only this
		// character/quest's terminal op33 keys before any response write so a
		// retryable quest cannot inherit a stale same-session feedback-loop gate.
		session.clearCurrentSetQuestTriggerReplayForQuest(session.selectedCharacterID, result.QuestID)
	}
	if result.Idempotent {
		// Current NoPack's class1/op31 success reader sub_1D29B40 consumes the
		// same u16 quest + u32 trigger + u8 item-count body for a persisted
		// active quest. ACK it once with the DB state, but do not append op574:
		// a fresh snapshot here is the feedback loop observed in live traffic.
		var payload packetWriter
		payload.writeUint16(result.QuestID)
		payload.writeUint32(result.InitTrigger)
		payload.writeByte(0)
		s.logGameEvent(session, "game-upper-accept-quest-idempotent-ack-send",
			"quest_id", result.QuestID,
			"init_trigger", result.InitTrigger,
			"pvf_path", result.PVFPath,
			"quest_type", result.QuestType,
			"msg_id", uint16(dnfenum.CmdPacketAcceptQuest),
			"classification", dnfproto.DefaultChannelClassification,
			"plain_body_len", 8,
			"response", "current_exe_op31_success_no_op574",
			"quest_mutation", "none",
			"body_source", "current_exe_sub_1D29B40_persisted_active_quest_state")
		if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketAcceptQuest), payload.bytes()); err != nil {
			return err
		}
		session.clearCurrentGiveUpQuestReplay(newCurrentGiveUpQuestReplayKey(session.selectedCharacterID, request))
		session.suppressCurrentAcceptQuestReplay(replayKey)
		return nil
	}
	var payload packetWriter
	payload.writeUint16(result.QuestID)
	payload.writeUint32(result.InitTrigger)
	payload.writeByte(0) // ApplyAccept rejects PVF depend-give-item quests.
	s.logGameEvent(session, "game-upper-accept-quest-success-send",
		"quest_id", result.QuestID,
		"init_trigger", result.InitTrigger,
		"event_item_count", 0,
		"idempotent", result.Idempotent,
		"pvf_path", result.PVFPath,
		"quest_type", result.QuestType,
		"msg_id", uint16(dnfenum.CmdPacketAcceptQuest),
		"classification", dnfproto.DefaultChannelClassification,
		"plain_body_len", 8,
		"body_source", "current_exe_sub_1D29B40_and_pvf_db_accept_owner")
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketAcceptQuest), payload.bytes()); err != nil {
		return err
	}
	if err := s.sendCurrentActiveQuestSnapshotForSession(session, "current_exe_op31_after_persisted_success_ack"); err != nil {
		return err
	}
	session.clearCurrentGiveUpQuestReplay(newCurrentGiveUpQuestReplayKey(session.selectedCharacterID, request))
	session.suppressCurrentAcceptQuestReplay(replayKey)
	return nil
}

// suppressSceneInitializationAcceptQuestRequest keeps quest acceptance
// reactive to player input. An op31 emitted while login/scene initialization
// is still constructing the selected actor is a passive client-side effect of
// that initialization, not evidence of a click. This covers cold first-login
// and tutorial/dungeon routes as well as the ordinary initial-town route. The
// request receives no response and cannot mutate quest state. Once the actor is
// fully ready, the normal player-driven op31 path remains available.
func (s *Service) suppressSceneInitializationAcceptQuestRequest(session *gameSession) (bool, string, currentInitialTownRouteStage) {
	if session == nil {
		return false, "session_missing", currentInitialTownRouteIdle
	}
	ready, readiness := s.currentSceneActorReadyForState(session)
	session.townMu.Lock()
	stage := session.initialTownRouteStage
	townSceneReady := session.townSceneReadyCharacterID != 0 &&
		session.townSceneReadyCharacterID == session.selectedCharacterID
	session.townMu.Unlock()
	// The one-shot initial-town route is intentionally reset to idle after a
	// normal area movement or a completed-dungeon return. Its dedicated town
	// readiness marker still proves that the selected actor was fully built.
	// Treating op31 in that state as initialization noise silently discarded
	// genuine task-manual clicks.
	if townSceneReady {
		return false, "town_scene_player_state_finalized", stage
	}
	return session.selectedCharacterID != 0 && !ready, readiness, stage
}
