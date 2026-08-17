package dnfbridge

import "encoding/binary"

const (
	// These are current notification IDs, not CmdPacket enum values.  The
	// generated Go enum only contains the C2S SET_USER_POSITION/AREA commands.
	// Current NoPack registers the matching S2C readers at sub_1D83990 and
	// sub_1D89590 respectively.
	currentTownUserPositionNotificationMsgID uint16 = 0x0016
	currentTownUserAreaNotificationMsgID     uint16 = 0x0017

	// sub_1D83990 passes this initial movement-rate field to the created town
	// actor.  The same-client Python implementation sends 100 here; it is a
	// protocol initialization value, not a character movement-stat substitute.
	currentTownInitialMovementRate uint16 = 100
)

// buildCurrentTownUserPositionNotificationBody matches current NoPack
// class0/noti0x16 -> sub_1D83990:
//
//	u16 actor_key, u16 x, u16 y, u8 direction, u16 initial_movement_rate
func buildCurrentTownUserPositionNotificationBody(
	actorKey uint16,
	x uint16,
	y uint16,
	direction byte,
) []byte {
	body := make([]byte, 9)
	binary.LittleEndian.PutUint16(body[0:2], actorKey)
	binary.LittleEndian.PutUint16(body[2:4], x)
	binary.LittleEndian.PutUint16(body[4:6], y)
	body[6] = direction
	binary.LittleEndian.PutUint16(body[7:9], currentTownInitialMovementRate)
	return body
}

// buildCurrentTownUserAreaNotificationBody matches current NoPack
// class0/noti0x17 -> sub_1D89590:
//
//	u16 actor_key, u8 town, u8 area, u16 x, u16 y, u8 direction, u8 state_bits
//
// sub_1D89590 passes the final two bytes to sub_23DBA50 in that order:
// offset+6 is switched as the 2/3/4/6/8/9 direction enum, while offset+7 is
// consumed as a bit field.  Swapping them corrupts the selected town actor's
// movement state and prevents the camera from following it.
func buildCurrentTownUserAreaNotificationBody(
	actorKey uint16,
	townID byte,
	areaID byte,
	x uint16,
	y uint16,
	direction byte,
	stateBits byte,
) []byte {
	body := make([]byte, 10)
	binary.LittleEndian.PutUint16(body[0:2], actorKey)
	body[2] = townID
	body[3] = areaID
	binary.LittleEndian.PutUint16(body[4:6], x)
	binary.LittleEndian.PutUint16(body[6:8], y)
	body[8] = direction
	body[9] = stateBits
	return body
}

// sendCurrentTownLocationNotifications publishes the current EXE's local
// position/area pair.  Op23 is not redundant with the following op24: its
// local-actor branch stores the active town/area/coordinates and rebinds the
// controlled scene actor before AREA_USERS applies the area roster.
func (s *Service) sendCurrentTownLocationNotifications(
	session *gameSession,
	characterID uint16,
	townID byte,
	areaID byte,
	row currentSceneTransitionRow,
	source string,
) error {
	actorKey := currentSceneActorObjectKey(characterID)
	positionBody := buildCurrentTownUserPositionNotificationBody(
		actorKey,
		row.Value1,
		row.Value2,
		row.Value3,
	)
	if err := s.sendCurrentSceneFixedClass0Packet(
		session,
		currentTownUserPositionNotificationMsgID,
		positionBody,
		source+"_user_position_current_exe_sub_1D83990",
	); err != nil {
		return err
	}
	areaBody := buildCurrentTownUserAreaNotificationBody(
		actorKey,
		townID,
		areaID,
		row.Value1,
		row.Value2,
		row.Value3,
		row.Value4,
	)
	if err := s.sendCurrentSceneFixedClass0Packet(
		session,
		currentTownUserAreaNotificationMsgID,
		areaBody,
		source+"_user_area_current_exe_sub_1D89590",
	); err != nil {
		return err
	}
	return nil
}

// sendCurrentInitialTownLocationNotificationsLocked publishes the selected
// actor's validated persisted location before the existing AREA_USERS/op24 snapshot.
// The fixed16 zero-tail envelope is the same current scene envelope used by
// the working Python service and by Go's repository-backed ITEM_LIST packets.
// The caller holds session.townMu.
func (s *Service) sendCurrentInitialTownLocationNotificationsLocked(
	session *gameSession,
	characterID uint16,
	townID byte,
	areaID byte,
	row currentSceneTransitionRow,
) error {
	if session == nil || characterID == 0 ||
		session.initialTownRouteCharacterID != characterID ||
		session.initialTownRouteStage < currentInitialTownRoutePlayerStatePrepared ||
		session.initialTownLocationNotificationsSent {
		return nil
	}
	actorKey := currentSceneActorObjectKey(characterID)
	if err := s.sendCurrentTownLocationNotifications(
		session,
		characterID,
		townID,
		areaID,
		row,
		"initial_town_location",
	); err != nil {
		return err
	}
	session.initialTownLocationNotificationsSent = true
	s.logGameEvent(session, "game-initial-town-location-notifications-sent",
		"char_id", characterID,
		"actor_object_key", actorKey,
		"town_id", townID,
		"area_id", areaID,
		"position_x", row.Value1,
		"position_y", row.Value2,
		"direction", row.Value3,
		"area_state", row.Value4,
		"sequence", "noti16_user_position_then_noti17_user_area_then_current_op24_area_users")
	return nil
}
