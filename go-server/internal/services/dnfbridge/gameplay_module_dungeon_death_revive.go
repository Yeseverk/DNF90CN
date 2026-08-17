package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func dungeonDeathReviveGameplayModule() gameplayModuleDefinition {
	dieCharacterOpcode := uint16(dnfenum.CmdPacketDieCharacter)
	useCoinOpcode := uint16(dnfenum.CmdPacketUseCoin)

	return gameplayModuleDefinition{
		Name: "dungeon-death-revive",
		LegacyHandlers: map[uint16]gameplayHandler{
			// Current op40 can arrive through the legacy decoder. It owns the
			// same death state and queued ten-second return as upper op40.
			dieCharacterOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleDungeonDieCharacter(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			dieCharacterOpcode: defaultClassGameplayHandler(
				"game-dungeon-character-death-blocked",
				"current_exe_op40_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonDieCharacter(session, body)
				},
			),
			useCoinOpcode: defaultClassGameplayHandler(
				"game-dungeon-use-coin-blocked",
				"current_exe_op41_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonUseCoin(session, body)
				},
			),
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			dieCharacterOpcode: stripLegacyDieCharacterZeroTail,
		},
	}
}
