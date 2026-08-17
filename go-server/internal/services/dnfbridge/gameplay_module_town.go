package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func townGameplayModule() gameplayModuleDefinition {
	positionOpcode := uint16(dnfenum.CmdPacketSetUserPosition)
	areaOpcode := uint16(dnfenum.CmdPacketSetUserArea)
	teleportOpcode := uint16(dnfenum.CmdPacketTeleport)
	soloTeleportOpcode := uint16(dnfenum.CmdPacketSoloTelepoart)
	return gameplayModuleDefinition{
		Name: "town",
		LegacyHandlers: map[uint16]gameplayHandler{
			positionOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleTownSetUserPosition(session, request.Body)
			},
			soloTeleportOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentSoloTeleport(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			positionOpcode: defaultClassGameplayHandler(
				"game-town-set-user-position-blocked",
				"current_exe_op35_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleTownSetUserPosition(session, body)
				},
			),
			areaOpcode: defaultClassGameplayHandler(
				"game-town-set-user-area-blocked",
				"current_exe_op36_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleTownSetUserArea(session, body)
				},
			),
			teleportOpcode: defaultClassGameplayHandler(
				"game-town-teleport-potion-blocked",
				"current_exe_op237_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleTeleportPotion(session, body)
				},
			),
			soloTeleportOpcode: defaultClassGameplayHandler(
				"game-town-solo-teleport-blocked",
				"current_exe_op470_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentSoloTeleport(session, body)
				},
			),
		},
	}
}
