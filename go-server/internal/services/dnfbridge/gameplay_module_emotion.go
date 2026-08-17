package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func emotionGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketChangeEmotion)
	handler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentChangeEmotion(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name:           "emotion",
		LegacyHandlers: map[uint16]gameplayHandler{opcode: handler},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: defaultClassGameplayHandler(
				"game-change-emotion-blocked",
				"current_exe_emotion_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentChangeEmotion(session, body)
				},
			),
		},
	}
}
