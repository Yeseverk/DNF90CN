package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func auraSkinGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketOpenAuraSkinSlot)
	return gameplayModuleDefinition{
		Name: "aura-skin-slot",
		LegacyHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentOpenAuraSkinSlot(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(service, session, request, "game-current-open-aura-skin-slot-blocked", "current_exe_op863_command_class_mismatch") {
					return nil
				}
				return service.handleCurrentOpenAuraSkinSlot(session, request.Body)
			},
		},
	}
}
