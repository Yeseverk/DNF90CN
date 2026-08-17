package dnfbridge

import (
	"context"
	"strings"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type currentDeferredSelectSceneTailPostStage byte

const (
	currentDeferredSelectSceneTailPackets currentDeferredSelectSceneTailPostStage = iota
	currentDeferredSelectSceneTailFinishGate
	currentDeferredSelectSceneTailCreature
	currentDeferredSelectSceneTailCrystal
	currentDeferredSelectSceneTailRental
	currentDeferredSelectSceneTailMailbox
	currentDeferredSelectSceneTailDamageFont
	currentDeferredSelectSceneTailRepresentName
	currentDeferredSelectSceneTailComplete
)

func resetCurrentDeferredSelectSceneTailProgress(session *gameSession) {
	if session == nil {
		return
	}
	session.sceneBootstrapTailPacketIndex = 0
	session.sceneBootstrapTailObjectMode1Pending = false
	session.sceneBootstrapTailPostStage = currentDeferredSelectSceneTailPackets
}

// sendCurrentSelectSceneReadyBoundary is the common actor-tail boundary for the
// DPROTO callback, CheckUserConnection, and legacy type1345 routes.
func (s *Service) sendCurrentSelectSceneReadyBoundary(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	if err := s.sendDeferredSelectSceneTail(session, source); err != nil {
		return err
	}
	if session.sceneBootstrapTailDeferred || !session.sceneBootstrapTailSent {
		return nil
	}
	session.townMu.Lock()
	legacyHUDReady := session.initialTownLegacySceneReadyAccepted
	session.townMu.Unlock()
	if !legacyHUDReady {
		return nil
	}
	if err := s.sendInitialTownAdventureOverheadRefresh(session, source+"_adventure_overhead_hud"); err != nil {
		return err
	}
	return s.sendInitialTownCombatPowerAffixes(session, source+"_combat_power_hud")
}

// sendInitialTownAdventureOverheadRefresh replays the authoritative account
// model only after the legacy type1345 HUD-ready boundary. The first op1340 /
// op1346 pair is necessarily sent before mode1 during actor construction; on
// the current EXE that early pair populates its internal adventure model but
// can be ignored by the already-created overhead-name widget. This one replay
// reaches the same native data path after the selected actor and HUD exist.
func (s *Service) sendInitialTownAdventureOverheadRefresh(session *gameSession, source string) error {
	if s == nil || session == nil {
		return nil
	}
	session.townMu.Lock()
	defer session.townMu.Unlock()
	if session.initialTownAdventureOverheadRefreshSent ||
		session.initialTownRouteCharacterID == 0 ||
		session.initialTownRouteCharacterID != session.selectedCharacterID ||
		session.initialTownRouteStage < currentInitialTownRoutePlayerStateSent {
		return nil
	}
	objectKey := session.selectedCharacterID
	if err := s.sendCurrentAdventureInfoPushFromAccount(session, objectKey, source+"_op1340"); err != nil {
		return err
	}
	if err := s.sendCurrentAdventureActorRefreshFromAccount(session, objectKey, source+"_op1346"); err != nil {
		return err
	}
	name, _, _, err := s.currentRepresentAccountIdentity(context.Background(), session)
	if err != nil {
		s.logGameEvent(session, "game-adventure-overhead-name-replay-skipped",
			"source", source,
			"reason", "represent_account_identity_unavailable",
			"error", err)
	} else if strings.TrimSpace(name) != "" {
		if err := s.sendRepresentAccountNameState(session, name, false, source+"_op1371"); err != nil {
			return err
		}
	}
	session.initialTownAdventureOverheadRefreshSent = true
	return nil
}

// sendCurrentLegacyTownSceneReadyBoundary consumes the current NoPack's proved
// type1345/u32(2) callback after the first typed op24. This callback does not
// own another actor/finish-loading generation: live traffic proved that
// replaying mode0/mode1/op105/op37/op30 here leaves the client update loop with
// a null scene actor. Complete only the already-owned deferred tail. Skills
// were installed at the accepted pre-op24 boundary.
func (s *Service) sendCurrentLegacyTownSceneReadyBoundary(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	session.townMu.Lock()
	if session.initialTownRouteCharacterID == 0 ||
		session.initialTownRouteCharacterID != session.selectedCharacterID ||
		session.initialTownRouteStage < currentInitialTownRoutePlayerStateSent {
		session.townMu.Unlock()
		return nil
	}
	session.initialTownLegacySceneReadyAccepted = true
	session.townMu.Unlock()

	if err := s.sendCurrentSelectSceneReadyBoundary(session, source); err != nil {
		return err
	}
	return nil
}

func (s *Service) sendInitialTownCombatPowerAffixes(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	session.townMu.Lock()
	defer session.townMu.Unlock()
	if session.initialTownCombatPowerAffixesSent ||
		session.initialTownRouteStage < currentInitialTownRouteTransitionSent {
		return nil
	}
	sent, err := s.sendSelectedCurrentCombatPowerAffixProjection(session, source)
	if err != nil {
		return err
	}
	if sent {
		session.initialTownCombatPowerAffixesSent = true
	}
	return nil
}

func currentInitialTownWaitsForLegacySceneReady(session *gameSession) bool {
	if session == nil {
		return false
	}
	session.townMu.Lock()
	waiting := session.initialTownRouteCharacterID != 0 &&
		session.initialTownRouteCharacterID == session.selectedCharacterID &&
		session.initialTownRouteStage >= currentInitialTownRouteTransitionSent &&
		!session.initialTownLegacySceneReadyAccepted
	session.townMu.Unlock()
	return waiting
}

func (s *Service) sendDeferredSelectSceneTail(session *gameSession, source string) error {
	if !session.sceneBootstrapTailDeferred {
		return nil
	}
	if currentInitialTownWaitsForLegacySceneReady(session) {
		s.logGameEvent(session, "game-upper-select-character-scene-tail-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "wait_for_current_exe_legacy_type1345_u32_2_scene_ready")
		return nil
	}
	transitionReady, transitionReadySource := s.currentSceneTransitionReadyForState(session)
	if !transitionReady {
		if err := s.resumeCurrentInitialTownRouteAfterSelectHeartbeat(session, source); err != nil {
			return err
		}
		if currentInitialTownWaitsForLegacySceneReady(session) {
			s.logGameEvent(session, "game-upper-select-character-scene-tail-deferred",
				"source", source,
				"char_id", session.selectedCharacterID,
				"reason", "initial_town_transition_committed_wait_for_current_exe_legacy_type1345_u32_2")
			return nil
		}
		transitionReady, transitionReadySource = s.currentSceneTransitionReadyForState(session)
	}
	if !transitionReady {
		s.logGameEvent(session, "game-upper-select-character-scene-tail-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"actor_ready_source", transitionReadySource,
			"reason", "wait_for_selected_actor_and_scene_transition_owner")
		return nil
	}
	if transitionReadySource == "initial_town_actor_bound_and_transition_sent" {
		if err := s.sendCurrentInitialTownPlayerState(session, source+"_after_town_transition"); err != nil {
			return err
		}
	}
	actorReady, actorReadySource := s.currentSceneActorReadyForState(session)
	if !actorReady {
		s.logGameEvent(session, "game-upper-select-character-scene-tail-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"actor_ready_source", actorReadySource,
			"reason", "wait_for_full_player_state_and_scene_finalizer")
		return nil
	}
	if session.sceneBootstrapTailSent {
		s.logGameEvent(session, "game-upper-select-character-scene-tail-duplicate-skipped",
			"source", source,
			"char_id", session.selectedCharacterID)
		return nil
	}
	packets := deferredSelectSceneTailPackets()
	s.logGameEvent(session, "game-upper-select-character-scene-tail-send",
		"source", source,
		"char_id", session.selectedCharacterID,
		"actor_ready_source", actorReadySource,
		"packet_count", len(packets),
		"resume_packet_index", session.sceneBootstrapTailPacketIndex,
		"resume_object_mode1_pending", session.sceneBootstrapTailObjectMode1Pending,
		"resume_post_stage", session.sceneBootstrapTailPostStage)
	for session.sceneBootstrapTailPacketIndex < len(packets) {
		packetIndex := session.sceneBootstrapTailPacketIndex
		packet := packets[packetIndex]
		body := packet.body
		if packet.kind == csharpCurrentSceneObjectListKind &&
			!session.sceneBootstrapTailObjectMode1Pending {
			ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
			var err error
			body, err = s.buildCurrentSceneObjectListBodyForSessionInContextStrict(
				ctx,
				session,
				0,
				"",
				dnfrepo.CharacterRecord{},
				false,
				currentTownActorOwnerContext(session),
			)
			cancel()
			if err != nil {
				return err
			}
		}
		if !session.sceneBootstrapTailObjectMode1Pending {
			if err := s.sendCSharpSelectInitPacket(session, packet, body); err != nil {
				return err
			}
			if packet.kind == csharpCurrentSceneObjectListKind {
				// The object packet is already accepted. If the paired mode1
				// write fails, resume at mode1 instead of replaying mode0.
				session.sceneBootstrapTailObjectMode1Pending = true
			} else {
				session.sceneBootstrapTailPacketIndex++
				continue
			}
		}
		if packet.kind == csharpCurrentSceneObjectListKind {
			if err := s.sendSelectedSceneUserInfo23Mode1Only(session, source+"_after_current_object_list"); err != nil {
				return err
			}
			session.sceneBootstrapTailObjectMode1Pending = false
		}
		session.sceneBootstrapTailPacketIndex++
	}
	if session.sceneBootstrapTailPostStage < currentDeferredSelectSceneTailFinishGate {
		session.sceneBootstrapTailPostStage = currentDeferredSelectSceneTailFinishGate
	}
	if session.sceneBootstrapTailPostStage == currentDeferredSelectSceneTailFinishGate {
		if actorReadySource != "initial_town_player_state_finalized" {
			if err := s.sendUpperFinishLoadingGate(session, source); err != nil {
				return err
			}
		}
		session.sceneBootstrapTailPostStage = currentDeferredSelectSceneTailCreature
	}
	// Do not publish skills from the generic tail. type1345 owns one explicit
	// retained op19 write only after this complete tail has committed; generic
	// CheckUserConnection/DPROTO callbacks must not consume that HUD boundary.
	// The actor-bound pre-mode1 stage already installs the complete current
	// item-list family. Replaying list 0/1/2/7/38 here rebuilds the same client
	// containers after op24 and can overwrite scene-local item state. Keep
	// scene-ready projections narrowly owned by their dedicated packets below.
	// The select-character ACK already installs the ordinary premium list.
	// Devil-contract service state is request-owned by class1/op903; replaying
	// it here makes the client announce the cached premium list a second time.
	// Durable aura expansion is already projected by selected-user subtype0's
	// repository-backed aura_flag. Do not emit unsolicited op863 here: its
	// native result handler is request/UI-owned and opens the avatar panel.
	if session.sceneBootstrapTailPostStage == currentDeferredSelectSceneTailCreature {
		if err := s.sendSelectedCreatureSceneReadyProjection(session, source+"_scene_ready_creature"); err != nil {
			return err
		}
		session.sceneBootstrapTailPostStage = currentDeferredSelectSceneTailCrystal
	}
	// Do not send op898 from the scene tail. The actor-bound player-state path
	// owns the cold-login class0 notification after every inventory container
	// has been installed; the first client-authored op36 is only an idempotent
	// fallback for routes that bypass that bootstrap. Sending from this earlier
	// tail would again require the rejected ABI-unsafe deferred call into
	// sub_1E7F810 and can corrupt adjacent UI state.
	if session.sceneBootstrapTailPostStage == currentDeferredSelectSceneTailCrystal {
		session.sceneBootstrapTailPostStage = currentDeferredSelectSceneTailRental
	}
	// The select-character container bootstrap does not carry rental points.
	// Publish the absolute wallet once, after the scene actor is committed.
	if session.sceneBootstrapTailPostStage == currentDeferredSelectSceneTailRental {
		if err := s.sendSelectedCurrentRentalPointState(session, source+"_after_scene_actor_commit", true); err != nil {
			return err
		}
		session.sceneBootstrapTailPostStage = currentDeferredSelectSceneTailMailbox
	}
	// class0/op63 is the current client's mailbox-system bootstrap. It is a
	// one-WORD unread-count projection, not an unsolicited op97 mailbox list,
	// and runs only after the selected actor and UI-safe tail are complete.
	if session.sceneBootstrapTailPostStage == currentDeferredSelectSceneTailMailbox {
		if err := s.sendMailboxAlarmForSession(
			session,
			session.selectedCharacterID,
			source+"_after_scene_actor_commit",
		); err != nil {
			return err
		}
		session.sceneBootstrapTailPostStage = currentDeferredSelectSceneTailDamageFont
	}
	// Damage-font ownership is independent of the inventory bootstrap. The
	// current client installs it through a class-0 NOTI 1239 projection once
	// the selected actor and other scene-owned stores are ready.
	if session.sceneBootstrapTailPostStage == currentDeferredSelectSceneTailDamageFont {
		if err := s.sendSelectedDamageFontState(session, source+"_after_scene_actor_commit"); err != nil {
			return err
		}
		session.sceneBootstrapTailPostStage = currentDeferredSelectSceneTailRepresentName
	}
	// This is the first point at which the selected town scene has received the
	// complete bootstrap tail. First-time adventure-group registration must not
	// interrupt the role selector or any earlier actor/state initialization.
	if session.sceneBootstrapTailPostStage == currentDeferredSelectSceneTailRepresentName {
		if err := s.sendRepresentAccountNameRegistrationAfterScene(session, source+"_after_scene_init_complete"); err != nil {
			return err
		}
		session.sceneBootstrapTailPostStage = currentDeferredSelectSceneTailComplete
	}
	session.sceneBootstrapTailSent = true
	session.sceneBootstrapTailDeferred = false
	s.logGameEvent(session, "game-select-scene-tail-active-finished",
		"source", source,
		"char_id", session.selectedCharacterID,
		"packet_count", len(packets)+1)
	return nil
}

func (s *Service) sendCurrentCargoPadResetBeforeItemLists(session *gameSession, source string) error {
	body := buildCurrentCancelCargoPadTransportBody()
	s.logPacketEvent("game-upper-current-cargo-pad-reset-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketCancelCargoPad),
		"classification", 0,
		"header_marker", 1,
		"body_encoded", true,
		"body_codec", currentCancelCargoPadTransportCodec,
		"expanded_body_len", currentCancelCargoPadHeaderSize+currentCancelCargoPadSlotCount*4,
		"slot_count", currentCancelCargoPadSlotCount,
		"protected_tail_len", currentCancelCargoPadProtectedTailSize,
		"state_source", "current_exe_protected_cargo_pad_reset_before_repository_item_lists")
	return s.sendCurrentProtectedClass0Packet(
		session,
		uint16(dnfenum.CmdPacketCancelCargoPad),
		body,
		currentCancelCargoPadTransportCodec,
		"initial_town_cargo_pad_reset_current_protected_state",
	)
}
