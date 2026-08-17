package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func equipmentEffectGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketAddEquipmentEffect)
	handler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentAddEquipmentEffect(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name:           "equipment-effect",
		LegacyHandlers: map[uint16]gameplayHandler{opcode: handler},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: defaultClassGameplayHandler(
				"game-current-add-equipment-effect-blocked",
				"current_exe_op951_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAddEquipmentEffect(session, body)
				},
			),
		},
	}
}
