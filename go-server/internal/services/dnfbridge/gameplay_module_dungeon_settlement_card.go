package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func dungeonSettlementCardGameplayModule() gameplayModuleDefinition {
	playResultOpcode := uint16(dnfenum.CmdPacketSetPlayResult)
	scoreScrollOpcode := uint16(dnfenum.CmdPacketScoreScrollState)
	cardRightOpcode := uint16(dnfenum.CmdPacketCardSelectRightState)
	selectCardOpcode := uint16(dnfenum.CmdPacketSelectCard)
	cardExitOpcode := uint16(dnfenum.CmdPacketEplpCommand)
	statisticOpcode := uint16(dnfenum.CmdPacketCharacterStatistic)

	return gameplayModuleDefinition{
		Name: "dungeon-settlement-card",
		LegacyHandlers: map[uint16]gameplayHandler{
			scoreScrollOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleDungeonScoreScrollState(session, request.Body)
			},
			// The current card flow is observed entirely on the legacy route:
			// op69 advances the ACK, op70 publishes layout, op71 reveals and
			// commits the reward, and op72 exits only after that commit.
			cardRightOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleDungeonCardSelectRightState(session, request.Body)
			},
			selectCardOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleDungeonSelectCard(session, request.Body)
			},
			cardExitOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleDungeonEplpCommand(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			playResultOpcode: defaultClassGameplayHandler(
				"game-dungeon-set-play-result-blocked",
				"current_exe_op46_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonSetPlayResult(session, body)
				},
			),
			scoreScrollOpcode: defaultClassGameplayHandler(
				"game-dungeon-card-layout-blocked",
				"current_exe_op69_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonScoreScrollState(session, body)
				},
			),
			cardRightOpcode: defaultClassGameplayHandler(
				"game-dungeon-card-layout-blocked",
				"current_exe_op70_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonCardSelectRightState(session, body)
				},
			),
			selectCardOpcode: defaultClassGameplayHandler(
				"game-dungeon-card-select-blocked",
				"current_exe_op71_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonSelectCard(session, body)
				},
			),
			cardExitOpcode: defaultClassGameplayHandler(
				"game-dungeon-card-exit-blocked",
				"current_exe_op72_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonEplpCommand(session, body)
				},
			),
			statisticOpcode: defaultClassGameplayHandler(
				"game-dungeon-character-statistic-blocked",
				"current_exe_op123_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonCharacterStatistic(session, body)
				},
			),
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			selectCardOpcode: func(body []byte) []byte {
				return stripLegacyTransportTrailer(body, 2)
			},
			cardExitOpcode: func(body []byte) []byte {
				return stripLegacyTransportTrailer(body, 2)
			},
		},
	}
}
