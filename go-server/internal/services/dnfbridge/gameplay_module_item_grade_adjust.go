package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func itemGradeAdjustGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketResetItemAttr)
	handler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentResetItemAttr(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name:           "item-grade-adjust",
		LegacyHandlers: map[uint16]gameplayHandler{opcode: handler},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: defaultClassGameplayHandler(
				"game-current-reset-item-attr-blocked",
				"current_exe_op81_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentResetItemAttr(session, body)
				},
			),
		},
	}
}
