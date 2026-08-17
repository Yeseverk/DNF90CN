package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
)

const currentSoloTeleportRequestWireSize = 15

// currentSoloTeleportRequest is written by current NoPack.exe
// SendSoloTeleportRequest470. The first two values are stable -1 in the live
// personal-teleport samples, but their meaning is not required for routing.
type currentSoloTeleportRequest struct {
	OpaqueI32A int32
	OpaqueI32B int32
	TownID     byte
	AreaID     byte
	PositionX  int16
	PositionY  int16
	Direction  byte
}

func parseCurrentSoloTeleportRequest(body []byte) (currentSoloTeleportRequest, error) {
	if len(body) != currentSoloTeleportRequestWireSize {
		return currentSoloTeleportRequest{}, fmt.Errorf(
			"current solo teleport op470 body length %d, want %d",
			len(body),
			currentSoloTeleportRequestWireSize,
		)
	}
	return currentSoloTeleportRequest{
		OpaqueI32A: int32(binary.LittleEndian.Uint32(body[0:4])),
		OpaqueI32B: int32(binary.LittleEndian.Uint32(body[4:8])),
		TownID:     body[8],
		AreaID:     body[9],
		PositionX:  int16(binary.LittleEndian.Uint16(body[10:12])),
		PositionY:  int16(binary.LittleEndian.Uint16(body[12:14])),
		Direction:  body[14],
	}, nil
}

func (s *Service) handleCurrentSoloTeleport(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	request, err := parseCurrentSoloTeleportRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-town-solo-teleport-blocked",
			"body_len", len(body),
			"reason", "current_exe_op470_writer_boundary_mismatch",
			"error", err)
		return nil
	}

	// op470 has a client-side failure callback, so a successful request must
	// advance through the established op24 scene-transition owner without an
	// op470 ACK. That owner also validates the target against the runtime PVF,
	// character level/quest state, active-dungeon state, and current town.
	s.logGameEvent(session, "game-town-solo-teleport-request",
		"char_id", session.selectedCharacterID,
		"town_id", request.TownID,
		"area_id", request.AreaID,
		"position_x", request.PositionX,
		"position_y", request.PositionY,
		"direction", request.Direction,
		"opaque_i32_a", request.OpaqueI32A,
		"opaque_i32_b", request.OpaqueI32B)

	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || session.selectedCharacterID == 0 {
		s.logGameEvent(session, "game-town-solo-teleport-blocked",
			"char_id", session.selectedCharacterID,
			"reason", "selected_character_location_unavailable")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return err
	}
	currentTown, townFound := character.Stats["town_id"]
	if !found || character.Stats == nil || !townFound ||
		currentTown <= 0 || currentTown > int64(^uint16(0)) {
		s.logGameEvent(session, "game-town-solo-teleport-blocked",
			"char_id", session.selectedCharacterID,
			"reason", "selected_character_town_id_missing_or_invalid")
		return nil
	}

	// Personal teleport is an explicit transport command, not an ordinary
	// op36 portal click. Preserve the persisted current town as the transport
	// source so the PVF-validated route can admit a cross-town destination
	// without weakening normal op36 movement checks.
	return s.handleTownSetUserArea(
		session,
		buildCurrentSoloTeleportMoveBody(request, uint16(currentTown)),
	)
}

func buildCurrentSoloTeleportMoveBody(
	request currentSoloTeleportRequest,
	sourceTownID uint16,
) []byte {
	body := make([]byte, currentTownSetUserAreaBodySize)
	body[0] = request.TownID
	body[1] = request.AreaID
	binary.LittleEndian.PutUint16(body[2:4], uint16(request.PositionX))
	binary.LittleEndian.PutUint16(body[4:6], uint16(request.PositionY))
	body[6] = request.Direction
	binary.LittleEndian.PutUint16(body[7:9], sourceTownID)
	body[15] = 5
	return body
}
