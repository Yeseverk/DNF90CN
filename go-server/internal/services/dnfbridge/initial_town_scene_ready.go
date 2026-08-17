package dnfbridge

func markReturnSelectTownReentry(session *gameSession, previousCharacterID uint16) {
	if session == nil {
		return
	}
	session.townMu.Lock()
	session.returnSelectTownReentryPending = previousCharacterID != 0
	session.townMu.Unlock()
}

func clearReturnSelectTownReentry(session *gameSession) {
	if session == nil {
		return
	}
	session.townMu.Lock()
	session.returnSelectTownReentryPending = false
	session.townMu.Unlock()
}

func returnSelectTownReentryPending(session *gameSession) bool {
	if session == nil {
		return false
	}
	session.townMu.Lock()
	pending := session.returnSelectTownReentryPending
	session.townMu.Unlock()
	return pending
}

// currentSceneTransitionReadyForState identifies the first boundary at which
// the selected actor and its scene transition both exist.  A town route still
// needs the current player/object finalization chain after this point.
func (s *Service) currentSceneTransitionReadyForState(session *gameSession) (bool, string) {
	if session == nil {
		return false, "session_missing"
	}
	if session.postStartMapPlayerStateSent {
		return true, "dungeon_post_op29_player_state"
	}
	session.townMu.Lock()
	ready := session.initialTownRouteCharacterID != 0 &&
		session.initialTownRouteCharacterID == session.selectedCharacterID &&
		session.initialTownRouteStage >= currentInitialTownRouteTransitionSent
	session.townMu.Unlock()
	if ready {
		return true, "initial_town_actor_bound_and_transition_sent"
	}
	return false, "actor_binding_or_scene_transition_incomplete"
}

// currentSceneActorReadyForState is the stronger finish-loading predicate.
// Dungeon entry reaches it through the post-op29 state chain. Completed town
// login prepares the full mode1/mode3/object-finalizer/display/placement chain
// before op24 and commits the transition only after that chain succeeds.
func (s *Service) currentSceneActorReadyForState(session *gameSession) (bool, string) {
	if session == nil {
		return false, "session_missing"
	}
	if session.postStartMapPlayerStateSent {
		return true, "dungeon_post_op29_player_state"
	}
	session.townMu.Lock()
	ready := session.initialTownRouteCharacterID != 0 &&
		session.initialTownRouteCharacterID == session.selectedCharacterID &&
		session.initialTownRouteStage >= currentInitialTownRoutePlayerStateSent
	session.townMu.Unlock()
	if ready {
		session.townPostTransition.mu.Lock()
		postTransitionStage := session.townPostTransition.stage
		postTransitionCharacterID := session.townPostTransition.characterID
		session.townPostTransition.mu.Unlock()
		if postTransitionCharacterID == session.selectedCharacterID &&
			postTransitionStage >= currentTownPostTransitionPending &&
			postTransitionStage < currentTownPostTransitionComplete {
			return false, "post_op24_actor_hud_generation_incomplete"
		}
		return true, "initial_town_player_state_finalized"
	}
	return false, "player_state_or_scene_finalizer_incomplete"
}
