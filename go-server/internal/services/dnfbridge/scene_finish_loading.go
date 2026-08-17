package dnfbridge

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentFinishLoadingCharacterStateBodySize   = 87
	currentFinishLoadingProtectedRequestBodySize = 16
	currentFinishLoadingLegacyRequestBodySize    = 8
	currentIncreaseStatusResultMsgID             = 30
	currentIncreaseStatusResultBodySize          = 5
)

func currentFinishLoadingRequestBodyAccepted(body []byte) bool {
	switch len(body) {
	case currentFinishLoadingLegacyRequestBodySize, currentFinishLoadingProtectedRequestBodySize:
		return true
	default:
		return false
	}
}

func (s *Service) sendCurrentFinishLoadingCharacterState(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	stateNeeded := !session.currentFinishLoadingStateSent
	followupsNeeded := !session.currentFinishLoadingCompletionSent ||
		!session.postFinishLoadingPlayerStateSent
	if !stateNeeded && !followupsNeeded {
		return nil
	}

	if stateNeeded {
		if err := s.sendCurrentFinishLoadingCharacterStateSnapshot(session, source); err != nil {
			return err
		}
		// Commit this gate as soon as the state packet is on the wire. A later
		// op30/post-state failure must not replay the absolute op37 snapshot.
		session.currentFinishLoadingStateSent = true
	}
	if !session.currentFinishLoadingCompletionSent {
		if err := s.sendCurrentIncreaseStatusResult(session, source); err != nil {
			return err
		}
		session.currentFinishLoadingCompletionSent = true
	}
	if err := s.sendCurrentPostFinishLoadingPlayerState(session, source+"_after_finish_loading_state"); err != nil {
		return err
	}
	return nil
}

// sendCurrentFinishLoadingCharacterStateSnapshot writes the class0/op37
// character snapshot without consuming the request-owned lifecycle gates.
// Town op24 recovery uses the same authoritative body after rebuilding the
// selected actor, while ordinary client op37 handling keeps its own gates in
// sendCurrentFinishLoadingCharacterState.
func (s *Service) sendCurrentFinishLoadingCharacterStateSnapshot(session *gameSession, source string) error {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, _, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if charID == 0 || !hasCharacter {
		s.logGameEvent(session, "game-main-finish-loading-state-deferred",
			"source", source,
			"char_id", charID,
			"reason", "selected_character_record_missing")
		return fmt.Errorf(
			"finish-loading character snapshot unavailable: selected=%d loaded=%d found=%t",
			session.selectedCharacterID,
			charID,
			hasCharacter,
		)
	}
	points := dnfrepo.SkillPointState{}
	if repositories, ok := s.repositoryGroup(); ok && repositories.Skill != nil {
		skill, found, err := repositories.Skill.Load(ctx, character.CharacterID)
		if err != nil {
			return fmt.Errorf("load finish-loading skill points for character %s: %w", character.CharacterID, err)
		}
		if found {
			points = skill.Points
			if synced, _, syncErr := s.syncCurrentSceneSkillPointLedger(ctx, repositories, character, skill); syncErr == nil {
				points = synced.Points
			} else {
				// The op37 state packet existed before PVF ledger repair and must not
				// disappear when a synthetic/test owner has no runtime PVF. Production
				// records still use the persisted ledger, while the explicit event keeps
				// the unsynchronised state observable instead of inventing point values.
				s.logGameEvent(session, "game-main-finish-loading-skill-point-sync-skipped",
					"source", source,
					"char_id", charID,
					"error", syncErr)
			}
		} else {
			s.logGameEvent(session, "game-main-finish-loading-skill-point-ledger-missing",
				"source", source,
				"char_id", charID)
		}
	} else {
		s.logGameEvent(session, "game-main-finish-loading-skill-point-repository-missing",
			"source", source,
			"char_id", charID)
	}
	body := buildCurrentFinishLoadingCharacterStateBody(character, points)
	if len(body) != currentFinishLoadingCharacterStateBodySize {
		return fmt.Errorf("current finish-loading state body length %d, want %d", len(body), currentFinishLoadingCharacterStateBodySize)
	}
	s.logGameEvent(session, "game-main-finish-loading-state-send",
		"source", source,
		"char_id", charID,
		"msg_id", uint16(dnfenum.CmdPacketFinishLoading),
		"classification", 0,
		"body_len", len(body),
		"level", body[0],
		"total_exp", statU32(character, "exp", 0),
		"remaining_sp", points.RemainingSP,
		"remaining_tp", points.RemainingTP,
		"dynamic_count", body[0x2e],
		"request_followups", true,
		"body_source", "current_exe_sub_1D78240_real_character_minimum_dynamic_table_empty")
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketFinishLoading), body, 0)
}

func (s *Service) sendCurrentIncreaseStatusResult(session *gameSession, source string) error {
	body := buildCurrentIncreaseStatusResultBody()
	if len(body) != currentIncreaseStatusResultBodySize {
		return fmt.Errorf("current increase-status result body length %d, want %d", len(body), currentIncreaseStatusResultBodySize)
	}
	s.logGameEvent(session, "game-main-increase-status-result-send",
		"source", source,
		"msg_id", currentIncreaseStatusResultMsgID,
		"classification", 0,
		"body_len", len(body),
		"duration", 0,
		"selector", 0,
		"actor_effect", "none",
		"body_source", "current_exe_op30_increase_status_timer_ui_not_actor_commit")
	return s.sendGameUpperRawClass(session, currentIncreaseStatusResultMsgID, body, 0)
}

func buildCurrentIncreaseStatusResultBody() []byte {
	var writer packetWriter
	writer.writeUint32(0) // No persisted optional duration/status state for this fresh character.
	writer.writeByte(0)
	return writer.bytes()
}

func (s *Service) sendCurrentPostFinishLoadingPlayerState(session *gameSession, source string) error {
	if session == nil || session.postFinishLoadingPlayerStateSent {
		return nil
	}
	initialTownFinalized := currentInitialTownPlayerStateFinalized(session)
	if session.selectedCharacterID == 0 {
		s.logGameEvent(session, "game-main-post-finish-loading-player-state-deferred",
			"source", source,
			"char_id", 0,
			"reason", "selected_character_record_missing")
		return nil
	}
	if initialTownFinalized {
		// A real class1/op37 request may still occur in town, so acknowledge and
		// publish its own op37/op30/op120 state. It does not own initial op19,
		// which the staged post-op24 generation installed after its own op30.
		// This prevents a later request from rebuilding the same native skill
		// manager.
		s.logGameEvent(session, "game-main-post-finish-loading-player-state-send",
			"source", source,
			"char_id", session.selectedCharacterID,
			"sequence", "op120_current_hpmp_only_initial_op19_already_installed_after_post_op24_op30",
			"state_source", "request_owned_finish_loading_does_not_replay_initial_town_skill_manager")
		placementBody := buildCurrentSceneActorPlacementBody()
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketRequestBlacklist), placementBody, 0); err != nil {
			return err
		}
		session.postFinishLoadingPlayerStateSent = true
		return nil
	}
	s.logGameEvent(session, "game-main-post-finish-loading-player-state-send",
		"source", source,
		"char_id", session.selectedCharacterID,
		"sequence", "op19_final_actor_skills_op120_current_hpmp_then_deferred_tutorial_op3",
		"state_source", "pre_op27_mode0_and_post_op29_mode1_already_initialized_the_local_actor")

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, _, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if charID == 0 || !hasCharacter {
		s.logGameEvent(session, "game-main-post-finish-loading-player-state-deferred",
			"source", source,
			"char_id", charID,
			"reason", "selected_character_record_missing")
		return nil
	}
	if err := s.sendCurrentSceneSkillInfo(session, ctx, character, source+"_final_actor_skills"); err != nil {
		return err
	}
	// mode3 is the full personal-information/equipment snapshot and is not part
	// of dungeon actor initialization. Replaying it after op37 opens that panel
	// over the dungeon and can replace live room actor state, so this boundary
	// deliberately emits only the skill refresh and current HP/MP placement.
	placementBody := buildCurrentSceneActorPlacementBody()
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketRequestBlacklist), placementBody, 0); err != nil {
		return err
	}
	// Only PVF tutorial scenes defer state 1 to this boundary.  Ordinary
	// dungeons sent it after their proven mode0/mode1 actor binding above;
	// replaying it here suppresses the native GetItem writer on the current EXE.
	objectKey := session.deferredDungeonUserStateObjectKey
	if objectKey != 0 {
		userStateBody, err := buildCurrentDungeonUserStateBody(objectKey)
		if err != nil {
			return err
		}
		s.logGameEvent(session, "game-main-post-finish-loading-deferred-user-state-send",
			"source", source,
			"char_id", charID,
			"object_key", objectKey,
			"msg_id", uint16(dnfenum.CmdPacketNotifyUserState),
			"classification", 0,
			"user_state", currentDungeonPlayerUserState,
			"body_len", len(userStateBody),
			"body_source", "current_exe_sub_1D88A10_after_mode0_mode1_and_op120_room_object_manager_refresh")
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketNotifyUserState), userStateBody, 0); err != nil {
			return err
		}
		session.deferredDungeonUserStateObjectKey = 0
	}
	session.postFinishLoadingPlayerStateSent = true
	return nil
}

func currentInitialTownPlayerStateFinalized(session *gameSession) bool {
	if session == nil {
		return false
	}
	session.townMu.Lock()
	finalized := session.initialTownRouteCharacterID != 0 &&
		session.initialTownRouteCharacterID == session.selectedCharacterID &&
		session.initialTownRouteStage >= currentInitialTownRoutePlayerStateSent
	session.townMu.Unlock()
	return finalized
}

func buildCurrentFinishLoadingCharacterStateBody(
	character dnfrepo.CharacterRecord,
	points dnfrepo.SkillPointState,
) []byte {
	return buildCurrentFinishLoadingCharacterStateBodyWithPresentation(character, points, nil)
}

type currentFinishLoadingExperiencePresentation struct {
	GrowthContractBonus uint32
}

func buildCurrentFinishLoadingCharacterStateBodyWithPresentation(
	character dnfrepo.CharacterRecord,
	points dnfrepo.SkillPointState,
	presentation *currentFinishLoadingExperiencePresentation,
) []byte {
	var writer packetWriter
	writer.writeByte(currentFinishLoadingLevel(character.Level))
	writer.writeUint32(statU32(character, "exp", 0))
	writer.writeUint32(statU32(character, "finish_loading_exp_category_0", 0))
	writer.writeUint16(currentFinishLoadingSkillPoint(points.RemainingSP))
	writer.writeUint16(currentFinishLoadingSkillPoint(points.RemainingSP))
	writer.writeUint16(currentFinishLoadingSkillPoint(points.RemainingTP))
	writer.writeUint16(currentFinishLoadingSkillPoint(points.RemainingTP))
	writer.writeUint32(statU32(character, "finish_loading_currency_slot2_total", 0))
	writer.writeUint32(statU32(character, "finish_loading_exp_category_1", 0))
	writer.writeByte(byte(statU32(character, "finish_loading_result_flag", 0)))
	writer.writeUint32(statU32(character, "finish_loading_exp_category_2", 0))
	growthContractBonus := statU32(character, "finish_loading_exp_category_3", 0)
	if presentation != nil {
		growthContractBonus = presentation.GrowthContractBonus
	}
	writer.writeUint32(growthContractBonus)
	writer.writeUint32(statU32(character, "finish_loading_independent_scalar", 0))
	writer.writeUint32(statU32(character, "finish_loading_exp_category_4", 0))
	writer.writeUint32(statU32(character, "finish_loading_exp_category_5", 0))
	writer.writeByte(0) // No persisted current dynamic EXP-category rows.
	writer.writeUint32(statU32(character, "finish_loading_exp_category_6", 0))
	writer.writeUint32(statU32(character, "finish_loading_exp_category_7", 0))
	writeCurrentHonorExpertState(&writer, character)
	writer.writeUint32(statU32(character, "finish_loading_exp_category_8", 0))
	writer.writeUint32(0) // Consumed but unused by the current reader.
	writer.writeUint32(statU32(character, "finish_loading_exp_category_9", 0))
	writer.writeUint32(statU32(character, "finish_loading_exp_category_10", 0))
	writer.writeUint32(statU32(character, "finish_loading_exp_category_11", 0))
	return writer.bytes()
}

func currentFinishLoadingLevel(level int) byte {
	switch {
	case level < 1:
		return newCharacterInitialLevel
	case level > 0xff:
		return 0xff
	default:
		return byte(level)
	}
}

func currentFinishLoadingSkillPoint(value int) uint16 {
	if value <= 0 {
		return 0
	}
	if value > int(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(value)
}
