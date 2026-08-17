package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func ceraPackageGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketOpenCerapackage)
	return gameplayModuleDefinition{
		Name: "cera-package",
		LegacyHandlers: map[uint16]gameplayHandler{
			// Current NoPack writes the same op518 body on both direct and
			// selective transports, so both share one atomic package owner.
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentCeraPackageOpen(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(service, session, request, "game-cera-package-open-blocked", "current_exe_op518_command_class_mismatch") {
					return nil
				}
				return service.handleCurrentCeraPackageOpen(session, request.Body)
			},
		},
	}
}
