package dnfbridge

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const currentTownSetUserPositionBodySize = 7

var errCurrentDungeonTownReturnOriginUnavailable = errors.New("current dungeon town-return origin is unavailable")

// currentTownSetUserPositionRequest is the exact plaintext written by the
// current EXE's sub_23E0660. Only the first two u16 values are proven to be
// the actor's town coordinates. The final u8/u16 remain deliberately opaque.
type currentTownSetUserPositionRequest struct {
	PositionX       uint16
	PositionY       uint16
	MovementCode    byte
	OpaqueScaledU16 uint16
}

// currentTownPositionSnapshot is session-owned. The repository remains the
// durable area owner; high-frequency op35 reports are not written to the DB.
// A snapshot can be reused only for the exact character/town/area scene that
// established it.
type currentTownPositionSnapshot struct {
	CharacterID     uint16
	TownID          byte
	AreaID          byte
	PositionX       uint16
	PositionY       uint16
	MovementCode    byte
	OpaqueScaledU16 uint16
	PositionValid   bool
}

func parseCurrentTownSetUserPositionRequest(body []byte) (currentTownSetUserPositionRequest, error) {
	if len(body) != currentTownSetUserPositionBodySize {
		return currentTownSetUserPositionRequest{}, fmt.Errorf(
			"current town op35 body length %d, want %d",
			len(body),
			currentTownSetUserPositionBodySize,
		)
	}
	return currentTownSetUserPositionRequest{
		PositionX:       binary.LittleEndian.Uint16(body[0:2]),
		PositionY:       binary.LittleEndian.Uint16(body[2:4]),
		MovementCode:    body[4],
		OpaqueScaledU16: binary.LittleEndian.Uint16(body[5:7]),
	}, nil
}

func (s *Service) handleTownSetUserPosition(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	request, err := parseCurrentTownSetUserPositionRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-town-set-user-position-blocked",
			"body_len", len(body),
			"reason", "current_exe_op35_writer_boundary_mismatch",
			"error", err)
		return nil
	}
	characterID := session.selectedCharacterID
	if characterID == 0 {
		s.logTownSetUserPositionBlocked(session, request, "selected_character_missing")
		return nil
	}

	if err := s.commitPendingDungeonReturnBeforeTownPosition(session, request); err != nil {
		return err
	}

	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	if runtime != nil {
		runtime.actorPositionX = request.PositionX
		runtime.actorPositionY = request.PositionY
		runtime.actorPositionValid = true
	}
	session.dungeon.mu.Unlock()
	if runtime != nil {
		return nil
	}

	session.townMu.Lock()
	readyCharacterID := session.townSceneReadyCharacterID
	snapshot := session.townPositionSnapshot
	if readyCharacterID == characterID && snapshot.CharacterID == characterID {
		snapshot.PositionX = request.PositionX
		snapshot.PositionY = request.PositionY
		snapshot.MovementCode = request.MovementCode
		snapshot.OpaqueScaledU16 = request.OpaqueScaledU16
		snapshot.PositionValid = true
		session.townPositionSnapshot = snapshot
	}
	session.townMu.Unlock()

	if readyCharacterID != characterID {
		s.logTownSetUserPositionBlocked(session, request, "town_scene_player_state_not_finalized")
		return nil
	}
	if snapshot.CharacterID != characterID {
		s.logTownSetUserPositionBlocked(session, request, "town_scene_location_owner_missing")
		return nil
	}

	s.logGameEvent(session, "game-town-set-user-position-captured",
		"char_id", characterID,
		"town_id", snapshot.TownID,
		"area_id", snapshot.AreaID,
		"position_x", request.PositionX,
		"position_y", request.PositionY,
		"movement_code", request.MovementCode,
		"opaque_scaled_u16", request.OpaqueScaledU16,
		"persisted", false,
		"source", "current_exe_sub_23E0660_exact_plaintext")

	// Broadcast position to other players in the same area.
	if s.onlinePlayers != nil {
		others := s.onlinePlayers.UpdatePosition(characterID, request.PositionX, request.PositionY)
		if len(others) > 0 {
			mover := &onlinePlayerInfo{
				CharacterID: characterID,
				TownID:      snapshot.TownID,
				AreaID:      snapshot.AreaID,
				PositionX:   request.PositionX,
				PositionY:   request.PositionY,
				Direction:   request.MovementCode,
				Session:     session,
			}
			s.broadcastTownPlayerMove(mover, others)
		}
	}
	return nil
}

// commitPendingDungeonReturnBeforeTownPosition accepts the first town-side
// op35 position report as proof that a previously written typed op24 return
// was consumed by the client. Live Tower death logs show exactly this shape:
// op40 -> op32 -> 10s -> op24, then the client sends op35 with the frozen
// return coordinates. Blocking that op35 leaves the active dungeon runtime
// attached forever and the corpse never reaches a usable town scene.
func (s *Service) commitPendingDungeonReturnBeforeTownPosition(
	session *gameSession,
	request currentTownSetUserPositionRequest,
) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	characterID := session.selectedCharacterID

	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	shouldCommit := runtime != nil && runtime.Session != nil &&
		runtime.townReturnPending && runtime.townReturnOp24Sent
	transition := currentDungeonTownTransition{}
	if shouldCommit {
		transition = cloneCurrentDungeonTownTransition(runtime.townReturnTransition)
	}
	session.dungeon.mu.Unlock()
	if !shouldCommit {
		// The runtime is detached before the staged town actor/HUD chain is
		// written. If an earlier confirmation reached that boundary but a
		// later mode0/mode1/creature/skill write failed, op35 is the next
		// current-client town proof and must resume only the unfinished suffix.
		return s.ensureCurrentConfirmedDungeonReturnPlayerState(
			session,
			"current_exe_op35_position_without_retained_runtime",
		)
	}

	if err := s.commitPendingDungeonReturnForSceneRequest(session, "current_exe_op35_position_after_pending_town_return"); err != nil {
		return err
	}

	session.dungeon.mu.Lock()
	stillHasRuntime := session.dungeon.runtime != nil
	session.dungeon.mu.Unlock()
	if stillHasRuntime || session.selectedCharacterID != characterID {
		s.logGameEvent(session, "game-town-set-user-position-pending-return-committed",
			"char_id", characterID,
			"selected_character", session.selectedCharacterID,
			"runtime_retained", stillHasRuntime,
			"reason", "pending_return_committed_but_scene_owner_changed")
		return nil
	}

	session.townMu.Lock()
	clearCurrentTownSelectorOriginLocked(session)
	session.townSceneReadyCharacterID = characterID
	session.townPositionSnapshot = currentTownPositionSnapshot{
		CharacterID:     characterID,
		TownID:          transition.TownID,
		AreaID:          transition.AreaID,
		PositionX:       request.PositionX,
		PositionY:       request.PositionY,
		MovementCode:    request.MovementCode,
		OpaqueScaledU16: request.OpaqueScaledU16,
		PositionValid:   true,
	}
	session.townMu.Unlock()

	s.logGameEvent(session, "game-town-set-user-position-pending-return-committed",
		"char_id", characterID,
		"town_id", transition.TownID,
		"area_id", transition.AreaID,
		"position_x", request.PositionX,
		"position_y", request.PositionY,
		"movement_code", request.MovementCode,
		"opaque_scaled_u16", request.OpaqueScaledU16,
		"position_source", transition.PositionSource,
		"reason", "op35_is_first_town_side_confirmation_after_typed_op24_return")
	return nil
}

// setCurrentTownPositionSceneLocked establishes the exact town scene that may
// own later op35 reports. The caller must hold session.townMu. A scene change
// invalidates the previous coordinate report even when the same character is
// moving between two areas of the same town.
func setCurrentTownPositionSceneLocked(session *gameSession, characterID uint16, townID, areaID byte) {
	if session == nil {
		return
	}
	clearCurrentTownSelectorOriginLocked(session)
	session.townPositionSnapshot = currentTownPositionSnapshot{
		CharacterID: characterID,
		TownID:      townID,
		AreaID:      areaID,
	}
}

// bindCurrentTownSelectorOrigin freezes the town origin owned by the exact
// op15/op27 selector context. A duplicate op15 keeps the first binding. This
// prevents a later town-position report from silently changing where op132
// returns, while the same frozen snapshot remains available after op16 has
// committed an active dungeon runtime.
func bindCurrentTownSelectorOrigin(session *gameSession) (currentTownPositionSnapshot, bool) {
	if session == nil || session.selectedCharacterID == 0 {
		return currentTownPositionSnapshot{}, false
	}
	characterID := session.selectedCharacterID
	session.townMu.Lock()
	defer session.townMu.Unlock()
	if session.townSelectorOriginBound {
		if session.townSelectorOriginSnapshot.CharacterID == characterID {
			return session.townSelectorOriginSnapshot, true
		}
		clearCurrentTownSelectorOriginLocked(session)
	}
	snapshot := session.townPositionSnapshot
	if session.townSceneReadyCharacterID != characterID || snapshot.CharacterID != characterID || !snapshot.PositionValid {
		return currentTownPositionSnapshot{}, false
	}
	session.townSelectorOriginSnapshot = snapshot
	session.townSelectorOriginBound = true
	return snapshot, true
}

// freezeCurrentDungeonTownReturnOrigin copies the exact op35 origin into an
// ordinary dungeon runtime before that runtime becomes visible. Tutorial
// dungeons deliberately retain their independent persisted birth-town return
// owner because a new character can enter them before any town op35 exists.
func freezeCurrentDungeonTownReturnOrigin(session *gameSession, runtime *runtimeDungeonState) error {
	if session == nil || runtime == nil || runtime.Session == nil {
		return fmt.Errorf("%w: runtime owner missing", errCurrentDungeonTownReturnOriginUnavailable)
	}
	if isPVFTutorialDungeon(runtime) {
		runtime.townReturnOrigin = currentTownPositionSnapshot{}
		return nil
	}
	characterID := session.selectedCharacterID
	if characterID == 0 || !dungeonRuntimeOwnsCharacter(runtime, characterID) {
		return fmt.Errorf("%w: runtime character owner mismatch", errCurrentDungeonTownReturnOriginUnavailable)
	}
	townID, areaID, err := currentSceneTransitionLocation(runtime.Character, true)
	if err != nil {
		return fmt.Errorf("%w: %v", errCurrentDungeonTownReturnOriginUnavailable, err)
	}
	session.townMu.Lock()
	origin := session.townSelectorOriginSnapshot
	bound := session.townSelectorOriginBound
	session.townMu.Unlock()
	if !bound {
		return fmt.Errorf("%w: selector origin not bound", errCurrentDungeonTownReturnOriginUnavailable)
	}
	if err := validateCurrentDungeonTownReturnOrigin(origin, characterID, townID, areaID); err != nil {
		return err
	}
	runtime.townReturnOrigin = origin
	return nil
}

func validateCurrentDungeonTownReturnOrigin(
	origin currentTownPositionSnapshot,
	characterID uint16,
	townID byte,
	areaID byte,
) error {
	if !origin.PositionValid {
		return fmt.Errorf("%w: op35 position was not reported", errCurrentDungeonTownReturnOriginUnavailable)
	}
	if origin.CharacterID != characterID || origin.TownID != townID || origin.AreaID != areaID {
		return fmt.Errorf(
			"%w: origin character/town/area %d/%d/%d does not match runtime %d/%d/%d",
			errCurrentDungeonTownReturnOriginUnavailable,
			origin.CharacterID,
			origin.TownID,
			origin.AreaID,
			characterID,
			townID,
			areaID,
		)
	}
	return nil
}

func clearCurrentTownSelectorOrigin(session *gameSession) {
	if session == nil {
		return
	}
	session.townMu.Lock()
	clearCurrentTownSelectorOriginLocked(session)
	session.townMu.Unlock()
}

func clearCurrentTownSelectorOriginLocked(session *gameSession) {
	if session == nil {
		return
	}
	session.townSelectorOriginSnapshot = currentTownPositionSnapshot{}
	session.townSelectorOriginBound = false
}

func currentTownPositionForTransition(
	session *gameSession,
	characterID uint16,
	townID byte,
	areaID byte,
) (currentTownPositionSnapshot, string, bool, error) {
	if session == nil || characterID == 0 {
		return currentTownPositionSnapshot{}, "", false, nil
	}
	session.townMu.Lock()
	selectorSnapshot := session.townSelectorOriginSnapshot
	selectorBound := session.townSelectorOriginBound
	session.townMu.Unlock()
	if selectorBound {
		if selectorSnapshot.CharacterID != characterID ||
			selectorSnapshot.TownID != townID ||
			selectorSnapshot.AreaID != areaID {
			return currentTownPositionSnapshot{}, "", false, fmt.Errorf(
				"selector origin character/town/area %d/%d/%d does not match transition %d/%d/%d",
				selectorSnapshot.CharacterID,
				selectorSnapshot.TownID,
				selectorSnapshot.AreaID,
				characterID,
				townID,
				areaID,
			)
		}
		if !selectorSnapshot.PositionValid {
			return currentTownPositionSnapshot{}, "", false, fmt.Errorf(
				"%w: bound selector origin has no op35 position",
				errCurrentDungeonTownReturnOriginUnavailable,
			)
		}
		return selectorSnapshot, "current_exe_op35_selector_origin_snapshot", true, nil
	}
	return currentTownPositionSnapshot{}, "", false, nil
}

func (s *Service) applyCurrentTownPositionToTransition(
	session *gameSession,
	characterID uint16,
	transition currentDungeonTownTransition,
) (currentDungeonTownTransition, bool, error) {
	snapshot, positionSource, ok, err := currentTownPositionForTransition(
		session,
		characterID,
		transition.TownID,
		transition.AreaID,
	)
	if err != nil {
		return currentDungeonTownTransition{}, false, err
	}
	if !ok {
		return transition, false, nil
	}
	transition, err = applyCurrentTownPositionSnapshotToTransition(
		transition,
		snapshot,
		characterID,
		positionSource,
	)
	if err != nil {
		return currentDungeonTownTransition{}, false, err
	}
	return transition, true, nil
}

func applyCurrentTownPositionSnapshotToTransition(
	transition currentDungeonTownTransition,
	snapshot currentTownPositionSnapshot,
	characterID uint16,
	positionSource string,
) (currentDungeonTownTransition, error) {
	if err := validateCurrentDungeonTownReturnOrigin(
		snapshot,
		characterID,
		transition.TownID,
		transition.AreaID,
	); err != nil {
		return currentDungeonTownTransition{}, err
	}
	row := currentSceneTransitionRow{
		ObjectOrResourceKey: transition.ActorObjectKey,
		Value1:              snapshot.PositionX,
		Value2:              snapshot.PositionY,
		Value3:              transition.Direction,
		Value4:              transition.AreaState,
	}
	body, err := buildCurrentSceneTransitionBody(
		transition.TownID,
		transition.AreaID,
		[]currentSceneTransitionRow{row},
	)
	if err != nil {
		return currentDungeonTownTransition{}, err
	}
	transition.PositionX = int16(snapshot.PositionX)
	transition.PositionY = int16(snapshot.PositionY)
	transition.PositionSource = positionSource
	transition.Body = body
	return transition, nil
}

func (s *Service) logTownSetUserPositionBlocked(
	session *gameSession,
	request currentTownSetUserPositionRequest,
	reason string,
) {
	s.logGameEvent(session, "game-town-set-user-position-blocked",
		"char_id", session.selectedCharacterID,
		"position_x", request.PositionX,
		"position_y", request.PositionY,
		"movement_code", request.MovementCode,
		"opaque_scaled_u16", request.OpaqueScaledU16,
		"reason", reason)
}
