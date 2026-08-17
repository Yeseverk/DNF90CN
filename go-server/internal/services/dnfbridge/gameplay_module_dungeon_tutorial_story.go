package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func dungeonTutorialStoryGameplayModule() gameplayModuleDefinition {
	tutorialFlagOpcode := uint16(dnfenum.CmdPacketChangeTutorialFlag)
	missionCheckOpcode := uint16(dnfenum.CmdPacketDungeonMissionCheckSuccess)
	storyPauseOpcode := uint16(dnfenum.CmdPacketDungeonEventStoryPause)

	return gameplayModuleDefinition{
		Name: "dungeon-tutorial-story",
		LegacyHandlers: map[uint16]gameplayHandler{
			// Final-room story state is independent of boss death and settlement.
			storyPauseOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentDungeonStoryPause(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			tutorialFlagOpcode: defaultClassGameplayHandler(
				"game-dungeon-tutorial-flag-blocked",
				"current_exe_op143_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonTutorialFlag(session, body)
				},
			),
			missionCheckOpcode: defaultClassGameplayHandler(
				"game-dungeon-mission-check-success-blocked",
				"current_exe_op560_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonMissionCheckSuccess(session, body)
				},
			),
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			storyPauseOpcode: func(body []byte) []byte {
				return stripLegacyTransportTrailer(body, currentDungeonStoryPauseRequestSize)
			},
		},
	}
}
