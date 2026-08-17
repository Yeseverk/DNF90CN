package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func deathTowerGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketDeathTowerStageCmd)
	return gameplayModuleDefinition{
		Name: "death-tower",
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentDeathTowerStageCmd(session, request.Body)
			},
		},
	}
}
