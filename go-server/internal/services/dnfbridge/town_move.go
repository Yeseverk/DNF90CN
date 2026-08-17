package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentTownSetUserAreaBodySize = 16

type currentTownSetUserAreaRequest struct {
	TownID       byte
	AreaID       byte
	PositionX    int16
	PositionY    int16
	Direction    byte
	OpaqueU16A   uint16
	OpaqueU16B   uint16
	OpaqueU32    uint32
	OpaqueTailU8 byte
}

func parseCurrentTownSetUserAreaRequest(body []byte) (currentTownSetUserAreaRequest, error) {
	if len(body) != currentTownSetUserAreaBodySize {
		return currentTownSetUserAreaRequest{}, fmt.Errorf("current town op36 body length %d, want %d", len(body), currentTownSetUserAreaBodySize)
	}
	return currentTownSetUserAreaRequest{
		TownID:       body[0],
		AreaID:       body[1],
		PositionX:    int16(binary.LittleEndian.Uint16(body[2:4])),
		PositionY:    int16(binary.LittleEndian.Uint16(body[4:6])),
		Direction:    body[6],
		OpaqueU16A:   binary.LittleEndian.Uint16(body[7:9]),
		OpaqueU16B:   binary.LittleEndian.Uint16(body[9:11]),
		OpaqueU32:    binary.LittleEndian.Uint32(body[11:15]),
		OpaqueTailU8: body[15],
	}, nil
}

func (s *Service) handleTownSetUserArea(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	request, err := parseCurrentTownSetUserAreaRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-town-set-user-area-blocked",
			"body_len", len(body),
			"reason", "current_exe_op36_writer_boundary_mismatch",
			"error", err)
		return nil
	}
	if session.selectedCharacterID == 0 {
		s.logTownSetUserAreaBlocked(session, request, "selected_character_missing")
		return nil
	}
	session.dungeon.mu.Lock()
	pendingDungeonReturn := session.dungeon.runtime != nil &&
		session.dungeon.runtime.Session != nil &&
		session.dungeon.runtime.townReturnPending
	session.dungeon.mu.Unlock()
	if !pendingDungeonReturn {
		session.townMu.Lock()
		townSceneReadyCharacterID := session.townSceneReadyCharacterID
		session.townMu.Unlock()
		if townSceneReadyCharacterID != session.selectedCharacterID {
			s.logTownSetUserAreaBlocked(session, request, "town_scene_player_state_not_finalized")
			return nil
		}
	}
	refreshQuestsAfterDungeonReturn := pendingDungeonReturn
	if err := s.commitPendingDungeonReturnForSceneRequest(session, "current_exe_op36_town_area_request"); err != nil {
		return err
	}
	session.dungeon.mu.Lock()
	hasDungeonRuntime := session.dungeon.runtime != nil
	session.dungeon.mu.Unlock()
	if hasDungeonRuntime {
		s.logTownSetUserAreaBlocked(session, request, "active_dungeon_runtime_rejects_town_area_request")
		return nil
	}

	session.townMu.Lock()
	defer session.townMu.Unlock()
	teleportArrayRequest := request.LooksLikeTeleportArraySelection()
	townMapStationRequest := request.LooksLikeTownMapStationSelection()
	townTransportRequest := request.LooksLikeTownTransportSelection()
	townSeriaRoomPortalRequest := false
	townCrossTownPortalRequest := false
	area, found := s.townArea(int64(request.TownID), int64(request.AreaID))
	if !found {
		s.logTownSetUserAreaBlocked(session, request, "target_town_area_missing_from_runtime_pvf")
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		s.logTownSetUserAreaBlocked(session, request, "character_repository_unavailable")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return err
	}
	if !found || character.Stats == nil {
		s.logTownSetUserAreaBlocked(session, request, "selected_character_location_missing")
		return nil
	}
	currentTown, ok := character.Stats["town_id"]
	if !ok {
		s.logTownSetUserAreaBlocked(session, request, "selected_character_town_id_missing")
		return nil
	}
	townCrossTownPortalRequest = request.LooksLikeTownCrossTownPortalSelection(currentTown)
	townSeriaRoomPortalRequest = request.LooksLikeTownSeriaRoomPortalSelection(currentTown, area.MapPath)
	townTransportRequest = townTransportRequest || townCrossTownPortalRequest || townSeriaRoomPortalRequest
	if currentTown != int64(request.TownID) && !townTransportRequest {
		s.logTownSetUserAreaBlocked(session, request, "cross_town_route_not_proven_by_current_pvf_topology")
		return nil
	}
	if area.MinLevel > int64(character.Level) {
		s.logTownSetUserAreaBlocked(session, request, "target_area_min_level_not_met")
		return nil
	}
	areaState, ok := character.Stats["area_state"]
	if !ok || areaState < 0 || areaState > int64(^byte(0)) {
		s.logTownSetUserAreaBlocked(session, request, "selected_character_area_state_missing_or_invalid")
		return nil
	}
	if len(area.NeedQuests) > 0 {
		needQuestsCompleted, questErr := s.townAreaNeedQuestsCompleted(ctx, repositories, characterID, area.NeedQuests)
		if questErr != nil {
			s.logGameEvent(session, "game-town-set-user-area-need-quest-check-failed",
				"char_id", session.selectedCharacterID,
				"town_id", request.TownID,
				"area_id", request.AreaID,
				"target_need_quests", area.NeedQuests,
				"town_transport_request", townTransportRequest,
				"error", questErr)
		}
		if !needQuestsCompleted {
			s.logTownSetUserAreaBlocked(session, request, "target_area_has_pvf_quest_requirement_without_completed_quest_owner")
			return nil
		}
	}
	prevVillageOrigin := currentTownPositionSnapshot{}
	prevVillageOriginBound := false
	if townMapPathLooksLikeSeria(area.MapPath) {
		if origin, ok := currentTownPrevVillageOriginLocked(session, session.selectedCharacterID, character); ok {
			prevVillageOrigin = origin
			prevVillageOriginBound = true
			session.townPrevVillageSnapshot = origin
			s.logTownPrevVillageOriginBound(session, origin, "current_exe_op36_enter_seria_room")
		} else {
			s.logTownPrevVillageOriginBindFailed(session, session.selectedCharacterID, "current_location_or_position_unavailable_before_enter_seria_room")
		}
	}

	previous := dnfrepo.CloneCharacter(character)
	character = dnfrepo.CloneCharacter(character)
	if prevVillageOriginBound {
		currentTownPrevVillageApplyOriginStats(character.Stats, prevVillageOrigin)
	}
	character.Stats["town_id"] = int64(request.TownID)
	character.Stats["area_id"] = int64(request.AreaID)
	character.Stats["pos_x"] = int64(request.PositionX)
	character.Stats["pos_y"] = int64(request.PositionY)
	character.Stats["direction"] = int64(request.Direction)
	character.UpdatedAt = time.Now().UTC()
	if err := dnfrepo.SaveCharacterFields(ctx, repositories.Character, character, dnfrepo.CharacterFieldStats); err != nil {
		return err
	}

	selfRow := currentSceneTransitionRow{
		ObjectOrResourceKey: currentSceneActorObjectKey(session.selectedCharacterID),
		Value1:              uint16(request.PositionX),
		Value2:              uint16(request.PositionY),
		Value3:              request.Direction,
		Value4:              byte(areaState),
	}
	transitionRows := make([]currentSceneTransitionRow, 0, 1)
	if s.onlinePlayers != nil {
		areaPlayers := s.onlinePlayers.GetAreaPlayers(request.TownID, request.AreaID)
		sort.Slice(areaPlayers, func(i, j int) bool {
			return areaPlayers[i].CharacterID < areaPlayers[j].CharacterID
		})
		for i := range areaPlayers {
			if areaPlayers[i].CharacterID == 0 ||
				areaPlayers[i].CharacterID == session.selectedCharacterID {
				continue
			}
			transitionRows = append(transitionRows, currentSceneTransitionRow{
				ObjectOrResourceKey: currentSceneActorObjectKey(areaPlayers[i].CharacterID),
				Value1:              areaPlayers[i].PositionX,
				Value2:              areaPlayers[i].PositionY,
				Value3:              areaPlayers[i].Direction,
				Value4:              areaPlayers[i].AreaState,
			})
		}
	}
	// Keep the selected actor last. The current EXE's op24 parser binds the
	// final row as the camera/selected actor while earlier rows form the peer
	// roster, matching the full-area roster role used by 86JP after it sends
	// peer appearance and user-info records.
	transitionRows = append(transitionRows, selfRow)
	response, err := buildCurrentSceneTransitionBody(request.TownID, request.AreaID, transitionRows)
	if err != nil {
		return err
	}
	rollbackLocation := func(writeErr error) error {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), createWriteTimeout)
		rollbackErr := dnfrepo.SaveCharacterFields(rollbackCtx, repositories.Character, previous, dnfrepo.CharacterFieldStats)
		rollbackCancel()
		if rollbackErr != nil {
			s.logGameEvent(session, "game-town-set-user-area-rollback-failed",
				"char_id", session.selectedCharacterID,
				"town_id", request.TownID,
				"area_id", request.AreaID,
				"write_error", writeErr,
				"rollback_error", rollbackErr)
		}
		return writeErr
	}
	s.logGameEvent(session, "game-town-set-user-area-transition-send",
		"char_id", session.selectedCharacterID,
		"request_msg_id", uint16(dnfenum.CmdPacketSetUserArea),
		"response_msg_ids", []uint16{
			currentTownUserPositionNotificationMsgID,
			currentTownUserAreaNotificationMsgID,
			currentClearQuestListMsgID,
			currentSceneTransitionMsgID,
		},
		"town_id", request.TownID,
		"area_id", request.AreaID,
		"position_x", request.PositionX,
		"position_y", request.PositionY,
		"direction", request.Direction,
		"area_state", areaState,
		"map_path", area.MapPath,
		"teleport_array_request", teleportArrayRequest,
		"town_map_station_request", townMapStationRequest,
		"town_seria_room_portal_request", townSeriaRoomPortalRequest,
		"town_cross_town_portal_request", townCrossTownPortalRequest,
		"town_transport_request", townTransportRequest,
		"target_need_quests", area.NeedQuests,
		"opaque_u16_a", request.OpaqueU16A,
		"opaque_u16_b", request.OpaqueU16B,
		"opaque_u32", request.OpaqueU32,
		"opaque_tail_u8", request.OpaqueTailU8,
		"row_count", len(transitionRows),
		"peer_row_count", len(transitionRows)-1,
		"op24_body_len", len(response),
		"sequence", "op22_user_position_then_op23_user_area_then_optional_op356_completed_quest_state_then_op24_area_users",
		"body_source", "current_exe_op22_sub_1D83990_op23_sub_1D89590_op356_sub_1D58470_op24_sub_1D901D0")
	if err := s.sendCurrentTownLocationNotifications(
		session,
		session.selectedCharacterID,
		request.TownID,
		request.AreaID,
		selfRow,
		"current_exe_op36_town_area_transition",
	); err != nil {
		return rollbackLocation(err)
	}
	// Initial town bootstrap installs the completed-quest bitmap before op24.
	// Ordinary town transitions need the same order because op24 constructs the
	// destination scene; a later op356 cannot restore an omitted quest gate.
	if _, err := s.sendCurrentPersistedClearQuestListIfCompleted(
		ctx,
		session,
		repositories,
		"current_exe_op36_after_op23_before_op24_seed_completed_quest_scene_state",
	); err != nil {
		return rollbackLocation(err)
	}
	if err := s.sendGameUpperRawClass(session, currentSceneTransitionMsgID, response, 0); err != nil {
		return rollbackLocation(err)
	}
	setCurrentTownPositionSceneLocked(session, session.selectedCharacterID, request.TownID, request.AreaID)
	session.initialTownRouteCharacterID = 0
	session.initialTownRouteStage = currentInitialTownRouteIdle
	session.initialTownLocationNotificationsSent = false
	session.initialTownQuestSnapshotsSent = false
	session.currentFinishLoadingStateSent = false
	session.currentFinishLoadingCompletionSent = false
	session.postFinishLoadingPlayerStateSent = false
	session.initialTownLegacySceneReadyAccepted = false
	session.initialTownAdventureOverheadRefreshSent = false
	session.initialTownCombatPowerAffixesSent = false
	session.enterSelectDungeonSent = false
	session.enterSelectDungeonAckSent = false
	session.enterSelectDungeonContextSent = false
	session.townTransportEnterSelectPending = townTransportRequest
	session.townSceneReadyCharacterID = session.selectedCharacterID

	// Register player in the new area and broadcast to others.
	var newAreaOthers []onlinePlayerInfo
	if s.onlinePlayers != nil {
		characterID := session.selectedCharacterID
		partyStateBeforeTownMove := runtimePartyStateSnapshot(session)
		playerInfo := &onlinePlayerInfo{
			CharacterID: characterID,
			AccountID:   character.AccountID,
			Name:        character.Name,
			Job:         byte(numericCharacterStat(character.Job)),
			GrowType:    byte(numericCharacterStatValue(character, "grow_type")),
			Level:       byte(character.Level),
			TownID:      request.TownID,
			AreaID:      request.AreaID,
			PositionX:   uint16(request.PositionX),
			PositionY:   uint16(request.PositionY),
			Direction:   request.Direction,
			AreaState:   byte(areaState),
			Session:     session,
		}
		// A player store is scoped to one concrete town scene. Close it before
		// removing the owner's old-area presence so every old peer receives the
		// native close notification and stale visitors are detached.
		s.closeCurrentExpertJobStoreSession(session, true)
		// Leave the old area. Ordinary peers receive op6. Party peers retain the
		// same actor identity and receive only its op23 area change, because op6
		// also removes that member from the current client's party manager.
		_, oldOthers := s.onlinePlayers.LeaveArea(characterID)
		if len(oldOthers) > 0 {
			s.broadcastTownPlayerAreaChange(playerInfo, oldOthers, partyStateBeforeTownMove)
		}
		// Enter new area.
		newOthers := s.onlinePlayers.EnterArea(playerInfo)
		newAreaOthers = append(newAreaOthers, newOthers...)
		s.promoteResidentGameSession(session, characterID)
		s.broadcastTownPlayerEnter(playerInfo, newOthers)
		s.replayCurrentExpertJobStores(session, request.TownID, request.AreaID)
	}
	// SET_USER_AREA rebuilds the current EXE's scene-owned party manager. Peer
	// actor projections alone are not sufficient: whichever member crosses the
	// town boundary last can otherwise retain only the selected actor row and
	// lose its own leader/member binding. Replay only the authoritative op9
	// roster after the new-area actors have been installed. The op11 endpoint
	// exchange belongs to party formation and must not be repeated here.
	partyStateAfterTownMove := runtimePartyStateSnapshot(session)
	if partyStateAfterTownMove.PartyID > 0 {
		if err := s.sendTownRemotePartyActors(session, partyStateAfterTownMove); err != nil {
			return err
		}
		if err := s.sendRuntimePartyRosterLocal(session, partyStateAfterTownMove, "town_set_user_area_local_roster_rebind"); err != nil {
			return err
		}
		// Existing same-area party clients have just received the mover's new
		// mode0/mode1 actor. Refresh their selected local roster as the final
		// step so the HUD/minimap keeps the authoritative slot mapping.
		for i := range newAreaOthers {
			peer := newAreaOthers[i].Session
			if peer == nil || peer == session ||
				!containsRuntimePartyMember(runtimePartyMembers(partyStateAfterTownMove), newAreaOthers[i].CharacterID) {
				continue
			}
			peerState := runtimePartyStateSnapshot(peer)
			if peerState.PartyID != partyStateAfterTownMove.PartyID {
				continue
			}
			if err := s.sendRuntimePartyRosterLocal(peer, partyStateAfterTownMove, "town_set_user_area_same_area_peer_roster_rebind"); err != nil {
				return err
			}
		}
	}

	s.logGameEvent(session, "game-town-set-user-area-committed",
		"char_id", session.selectedCharacterID,
		"town_id", request.TownID,
		"area_id", request.AreaID,
		"position_x", request.PositionX,
		"position_y", request.PositionY,
		"map_path", area.MapPath,
		"teleport_array_request", teleportArrayRequest,
		"town_map_station_request", townMapStationRequest,
		"town_seria_room_portal_request", townSeriaRoomPortalRequest,
		"town_cross_town_portal_request", townCrossTownPortalRequest,
		"town_transport_request", townTransportRequest,
		"target_need_quests", area.NeedQuests,
		"finish_loading_state_rearmed", true,
		"quest_refresh_after_dungeon_return", refreshQuestsAfterDungeonReturn)
	// The actor-bound inventory bootstrap owns the normal cold-login op898.
	// Keep the first client-authored op36 as a one-shot fallback for routes that
	// bypassed that bootstrap; by this point the current NoPack class0 handler
	// sub_1E7FAD0 can also see the inventory-panel owner. Keep this optional:
	// town movement must not fail merely because a minimal test runtime has no
	// premium/PVF catalog.
	if err := s.sendCurrentCrystalContractTownUIReadyState(session); err != nil {
		s.logGameEvent(session, "game-crystal-contract-town-area-state-deferred",
			"char_id", session.selectedCharacterID,
			"town_id", request.TownID,
			"area_id", request.AreaID,
			"reason", "runtime_pvf_or_repository_state_unavailable",
			"error", err)
	}
	if refreshQuestsAfterDungeonReturn {
		return s.sendCurrentAcceptableQuestListForSession(session, "current_exe_op36_after_dungeon_return_committed")
	}
	return nil
}

func (s *Service) townAreaNeedQuestsCompleted(
	ctx context.Context,
	repositories dnfrepo.Group,
	characterID string,
	needQuests []int64,
) (bool, error) {
	if len(needQuests) == 0 {
		return true, nil
	}
	if repositories.Quest == nil {
		return false, dnfrepo.ErrRepoMissing
	}
	record, found, err := repositories.Quest.Load(ctx, characterID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	for _, questID := range needQuests {
		if questID <= 0 {
			continue
		}
		if !townQuestStateCompleted(record, questID) {
			return false, nil
		}
	}
	return true, nil
}

func townQuestStateCompleted(record dnfrepo.QuestRecord, questID int64) bool {
	completed := false
	if state, ok := record.Progress[questID]; ok {
		completed = isTownQuestCompletedStatus(state.Status)
	}
	if state, ok := record.States[questID]; ok {
		completed = isTownQuestCompletedStatus(state.Status)
	}
	return completed
}

func isTownQuestCompletedStatus(status string) bool {
	switch normalizeDungeonQuestStatus(status) {
	case "complete", "completed", "cleared", "finished", "done":
		return true
	default:
		return false
	}
}

// LooksLikeTeleportArraySelection identifies the current client's town
// teleport-array selection shape observed from the live 23.4.15.0 EXE. The
// destination itself is still validated against the runtime PVF town catalog;
// this only prevents server-side quest/topology guards from rejecting a
// client-visible teleport destination before op24 can be emitted.
func (request currentTownSetUserAreaRequest) LooksLikeTeleportArraySelection() bool {
	return request.OpaqueTailU8 == 5 && request.OpaqueU32 == 0 && request.OpaqueU16A != 0
}

// LooksLikeTownMapStationSelection identifies the current EXE world-map /
// town transfer-station op36 shape observed live from West Coast:
// town=40, area=0, opaqueA=40, opaqueB=1, opaqueU32=0, tail=0. It is distinct
// from ordinary route-click bodies whose opaque fields are zero.
func (request currentTownSetUserAreaRequest) LooksLikeTownMapStationSelection() bool {
	return request.OpaqueTailU8 == 0 &&
		request.OpaqueU32 == 0 &&
		request.OpaqueU16A != 0 &&
		request.OpaqueU16B != 0
}

// LooksLikeTownSeriaRoomPortalSelection identifies the current EXE town
// in-scene Seria-room portal shape observed live from West Coast:
// source town=40, target town=38/area=1, opaqueA=40, opaqueB=0, opaqueU32=0,
// tail=0. The target area is still resolved from runtime PVF and must be a
// Seria-room map; this only admits the client's explicit portal request across
// town ownership boundaries.
func (request currentTownSetUserAreaRequest) LooksLikeTownSeriaRoomPortalSelection(currentTown int64, targetMapPath string) bool {
	if currentTown <= 0 {
		return false
	}
	return request.LooksLikeTownCrossTownPortalSelection(currentTown) &&
		townMapPathLooksLikeSeria(targetMapPath)
}

// LooksLikeTownCrossTownPortalSelection identifies the current EXE in-scene
// cross-town portal shape observed from West Coast to HendonMyre:
// target town=39/area=0, opaqueA=current town 40, opaqueB=0, opaqueU32=0,
// tail=0. The destination is still resolved from the runtime PVF town catalog;
// this only admits a client-emitted portal body whose source-town ownership is
// explicit in the request.
func (request currentTownSetUserAreaRequest) LooksLikeTownCrossTownPortalSelection(currentTown int64) bool {
	if currentTown <= 0 {
		return false
	}
	return request.OpaqueTailU8 == 0 &&
		request.OpaqueU32 == 0 &&
		request.OpaqueU16A != 0 &&
		int64(request.OpaqueU16A) == currentTown &&
		request.OpaqueU16B == 0 &&
		int64(request.TownID) != currentTown
}

func (request currentTownSetUserAreaRequest) LooksLikeTownTransportSelection() bool {
	return request.LooksLikeTeleportArraySelection() || request.LooksLikeTownMapStationSelection()
}

func (s *Service) logTownSetUserAreaBlocked(session *gameSession, request currentTownSetUserAreaRequest, reason string) {
	s.logGameEvent(session, "game-town-set-user-area-blocked",
		"char_id", session.selectedCharacterID,
		"town_id", request.TownID,
		"area_id", request.AreaID,
		"position_x", request.PositionX,
		"position_y", request.PositionY,
		"direction", request.Direction,
		"opaque_u16_a", request.OpaqueU16A,
		"opaque_u16_b", request.OpaqueU16B,
		"opaque_u32", request.OpaqueU32,
		"opaque_tail_u8", request.OpaqueTailU8,
		"reason", reason)
}
