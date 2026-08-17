package dnfbridge

import (
	"context"
	"fmt"
	"strconv"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

// currentTownPostTransitionStage is a durable-in-session wire cursor for the
// selected actor generation created by one typed town op24. A failed write may
// resume only at the unfinished suffix; replaying an already accepted mode0 or
// mode1 can leave the current client bound to a different native object.
type currentTownPostTransitionStage byte

const (
	currentTownPostTransitionIdle currentTownPostTransitionStage = iota
	currentTownPostTransitionPending
	currentTownPostTransitionMode0Sent
	currentTownPostTransitionMode1Sent
	currentTownPostTransitionCreatureTableSent
	currentTownPostTransitionCreatureGrowthSent
	currentTownPostTransitionFinishStateSent
	currentTownPostTransitionFinishCompletionSent
	currentTownPostTransitionSkillSent
	currentTownPostTransitionComplete
)

// armCurrentTownPostTransitionPlayerState starts a new selected-actor
// generation after an accepted movement/return typed town op24. First
// selection deliberately does not use this cursor: its type1345 callback owns
// only the already-deferred tail and HUD gauge and cannot safely consume
// another actor/finish-loading generation.
func (s *Service) armCurrentTownPostTransitionPlayerState(session *gameSession, source string) {
	if session == nil || session.selectedCharacterID == 0 {
		return
	}
	session.townPostTransition.mu.Lock()
	defer session.townPostTransition.mu.Unlock()

	session.townPostTransition.generation++
	session.townPostTransition.characterID = session.selectedCharacterID
	session.townPostTransition.ownerChannel = currentTownActorOwnerContext(session)
	session.townPostTransition.stage = currentTownPostTransitionPending
	session.townPostTransition.source = source

	// These gates belong to the actor generation invalidated by op24. The
	// staged chain below owns their one replacement for the new generation.
	session.currentFinishLoadingStateSent = false
	session.currentFinishLoadingCompletionSent = false
	session.postFinishLoadingPlayerStateSent = false
	session.initialTownSkillInfoSent = false

	s.logGameEvent(session, "game-town-post-transition-player-state-armed",
		"source", source,
		"char_id", session.townPostTransition.characterID,
		"owner_context", session.townPostTransition.ownerChannel,
		"generation", session.townPostTransition.generation,
		"stage", session.townPostTransition.stage,
		"sequence", "mode0_mode1_op105_optional_op102_op37_op30_op19_op120")
}

// resetCurrentTownPostTransitionPlayerState drops a stale pending generation
// when character selection/session state is reset before another op24.
func resetCurrentTownPostTransitionPlayerState(session *gameSession) {
	if session == nil {
		return
	}
	session.townPostTransition.mu.Lock()
	session.townPostTransition.characterID = 0
	session.townPostTransition.ownerChannel = 0
	session.townPostTransition.stage = currentTownPostTransitionIdle
	session.townPostTransition.source = ""
	session.townPostTransition.mu.Unlock()
}

// sendCurrentTownPostTransitionPlayerState rebuilds the exact current-client
// town generation invalidated by op24. The transport-only DLL does not rewrite
// scene owners, so both actor packets use the CHANNELINFO/connection owner.
func (s *Service) sendCurrentTownPostTransitionPlayerState(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	session.townPostTransition.mu.Lock()
	defer session.townPostTransition.mu.Unlock()

	stage := session.townPostTransition.stage
	if stage == currentTownPostTransitionIdle || stage == currentTownPostTransitionComplete {
		return nil
	}
	characterID := session.townPostTransition.characterID
	ownerChannel := session.townPostTransition.ownerChannel
	currentOwner := currentTownActorOwnerContext(session)
	connectionOwner := currentConnectionTownActorOwnerContext(session)
	if characterID == 0 || characterID != session.selectedCharacterID {
		return fmt.Errorf(
			"town post-transition character changed before stage %d: armed=%d selected=%d",
			stage,
			characterID,
			session.selectedCharacterID,
		)
	}
	if ownerChannel != currentOwner || ownerChannel != connectionOwner {
		return fmt.Errorf(
			"town post-transition owner changed before stage %d: armed=%d current=%d connection=%d",
			stage,
			ownerChannel,
			currentOwner,
			connectionOwner,
		)
	}
	if source == "" {
		source = session.townPostTransition.source
	}
	generation := session.townPostTransition.generation

	if session.townPostTransition.stage < currentTownPostTransitionMode0Sent {
		if err := s.sendSelectedActorMode0AppearanceRefresh(
			session,
			"town_post_transition",
			source+"_mode0_native_owner",
		); err != nil {
			return err
		}
		session.townPostTransition.stage = currentTownPostTransitionMode0Sent
	}
	if session.townPostTransition.stage < currentTownPostTransitionMode1Sent {
		if err := s.sendSelectedActorMode1AppearanceRefresh(
			session,
			"town_post_transition",
			source+"_mode1_native_owner",
		); err != nil {
			return err
		}
		session.townPostTransition.stage = currentTownPostTransitionMode1Sent
	}
	if session.townPostTransition.stage < currentTownPostTransitionCreatureTableSent {
		// op24 created a new native actor generation, so op105 must be written
		// even when the previous generation already consumed the same table.
		session.selectedCreatureStateTableSent = false
		if err := s.sendSelectedCreatureStateTable(session, source+"_op105_after_actor_rebind"); err != nil {
			return err
		}
		session.townPostTransition.stage = currentTownPostTransitionCreatureTableSent
	}
	if session.townPostTransition.stage < currentTownPostTransitionCreatureGrowthSent {
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		equippedCreature, err := s.currentEquippedCreatureForCharacterWithError(
			ctx,
			strconv.Itoa(int(characterID)),
		)
		cancel()
		if err != nil {
			return err
		}
		if equippedCreature.valid() {
			if err := s.sendSelectedEquippedCreatureGrowthState(
				session,
				equippedCreature,
				source+"_op102_after_op105",
			); err != nil {
				return err
			}
		}
		session.townPostTransition.stage = currentTownPostTransitionCreatureGrowthSent
	}
	if session.townPostTransition.stage < currentTownPostTransitionFinishStateSent {
		if err := s.sendCurrentFinishLoadingCharacterStateSnapshot(
			session,
			source+"_op37_after_actor_and_creature_rebind",
		); err != nil {
			return err
		}
		session.currentFinishLoadingStateSent = true
		session.townPostTransition.stage = currentTownPostTransitionFinishStateSent
	}
	if session.townPostTransition.stage < currentTownPostTransitionFinishCompletionSent {
		if err := s.sendCurrentIncreaseStatusResult(
			session,
			source+"_op30_after_finish_state",
		); err != nil {
			return err
		}
		session.currentFinishLoadingCompletionSent = true
		session.townPostTransition.stage = currentTownPostTransitionFinishCompletionSent
	}
	if session.townPostTransition.stage < currentTownPostTransitionSkillSent {
		prepared := session.initialTownSkillInfoPrepared &&
			session.initialTownSkillInfo.characterID == strconv.Itoa(int(characterID)) &&
			len(session.initialTownSkillInfo.body) != 0
		if prepared {
			if err := s.sendCurrentSceneSkillInfoProjection(
				session,
				session.initialTownSkillInfo,
				source+"_op19_prepared_after_op30",
			); err != nil {
				return err
			}
		} else {
			if err := s.sendSelectedActorCurrentSceneSkillInfo(
				session,
				source+"_op19_reloaded_after_op30",
			); err != nil {
				return err
			}
		}
		session.initialTownSkillInfoPrepared = false
		session.initialTownSkillInfoSent = true
		session.initialTownSkillInfo = currentSceneSkillInfoProjection{}
		session.townPostTransition.stage = currentTownPostTransitionSkillSent
	}
	if session.townPostTransition.stage < currentTownPostTransitionComplete {
		placementBody := buildCurrentSceneActorPlacementBody()
		if err := s.sendGameUpperRawClass(
			session,
			uint16(dnfenum.CmdPacketRequestBlacklist),
			placementBody,
			0,
		); err != nil {
			return err
		}
		session.postFinishLoadingPlayerStateSent = true
		session.townPostTransition.stage = currentTownPostTransitionComplete
	}
	s.logGameEvent(session, "game-town-post-transition-player-state-sent",
		"source", source,
		"char_id", characterID,
		"owner_context", ownerChannel,
		"generation", generation,
		"stage", session.townPostTransition.stage,
		"sequence", "mode0_mode1_op105_optional_op102_op37_op30_op19_op120")
	return nil
}
