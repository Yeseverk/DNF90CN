package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func epicProductionGameplayModule() gameplayModuleDefinition {
	startOpcode := uint16(dnfenum.CmdPacketEpicProductionStartFinish)
	changeOpcode := uint16(dnfenum.CmdPacketEpicProductionChangeItem)
	processOpcode := uint16(dnfenum.CmdPacketEpicProductionProcess)
	compoundOpcode := uint16(dnfenum.CmdPacketEpicProductionMaterialCompound)
	abilityOpcode := uint16(dnfenum.CmdPacketEpicProductionAbilityOption)
	handleStart := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentEpicProductionStart(session, request.Body)
	}
	handleChange := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentEpicProductionChange(session, request.Body)
	}
	handleProcess := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentEpicProductionProcess(session, request.Body)
	}
	handleCompound := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentEpicProductionCompound(session, request.Body)
	}
	handleAbility := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentEpicProductionAbility(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name: "epic-production",
		LegacyHandlers: map[uint16]gameplayHandler{
			startOpcode:    handleStart,
			changeOpcode:   handleChange,
			processOpcode:  handleProcess,
			compoundOpcode: handleCompound,
			abilityOpcode:  handleAbility,
		},
		UpperHandlers: map[uint16]gameplayHandler{
			startOpcode: defaultClassGameplayHandler(
				"game-upper-epic-production-start-blocked",
				"current_exe_op1417_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentEpicProductionStart(session, body)
				},
			),
			changeOpcode: defaultClassGameplayHandler(
				"game-upper-epic-production-change-blocked",
				"current_exe_op1418_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentEpicProductionChange(session, body)
				},
			),
			processOpcode: defaultClassGameplayHandler(
				"game-upper-epic-production-process-blocked",
				"current_exe_op1419_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentEpicProductionProcess(session, body)
				},
			),
			compoundOpcode: defaultClassGameplayHandler(
				"game-upper-epic-production-compound-blocked",
				"current_exe_op1420_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentEpicProductionCompound(session, body)
				},
			),
			abilityOpcode: defaultClassGameplayHandler(
				"game-upper-epic-production-ability-blocked",
				"current_exe_op1421_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentEpicProductionAbility(session, body)
				},
			),
		},
	}
}
