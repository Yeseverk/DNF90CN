package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func dungeonNavigationGameplayModule() gameplayModuleDefinition {
	selectOpcode := uint16(dnfenum.UpperMsgSelectEnter)
	moveMapOpcode := uint16(dnfenum.CmdPacketMoveMap)
	giveUpOpcode := uint16(dnfenum.CmdPacketGiveupGame)
	backVillageOpcode := uint16(dnfenum.CmdPacketBack2Village)
	prevVillageOpcode := uint16(dnfenum.CmdPacketPrevVillage)

	return gameplayModuleDefinition{
		Name: "dungeon-navigation",
		LegacyHandlers: map[uint16]gameplayHandler{
			selectOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				service.logInfo("dnfbridge latest select-dungeon received",
					"channel_id", session.channel.ID,
					"body_len", len(request.Body))
				service.logGameEvent(session, "game-select-dungeon-legacy-plain-dispatch",
					"body_len", len(request.Body),
					"reason", "client_plain_clone_or_legacy_route_must_share_current_exe_op16_owner")
				return service.handleDungeonSelectUpper(session, request.Body)
			},
			moveMapOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleDungeonMoveMap(session, request.Body)
			},
			prevVillageOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentPrevVillage(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			selectOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleDungeonSelectUpper(session, request.Body)
			},
			moveMapOpcode: defaultClassGameplayHandler(
				"game-dungeon-move-blocked",
				"current_exe_op45_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonMoveMap(session, body)
				},
			),
			giveUpOpcode: defaultClassGameplayHandler(
				"game-dungeon-giveup-game-blocked",
				"current_exe_op42_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonGiveupGame(session, body)
				},
			),
			backVillageOpcode: defaultClassGameplayHandler(
				"game-dungeon-back-to-village-blocked",
				"current_exe_op132_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonBackToVillage(session, body)
				},
			),
		},
	}
}
