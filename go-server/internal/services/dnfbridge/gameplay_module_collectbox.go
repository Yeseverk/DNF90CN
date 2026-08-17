package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

func collectboxGameplayModule() gameplayModuleDefinition {
	opcodes := []uint16{
		uint16(dnfenum.CmdPacketSelectCollectbox),
		uint16(dnfenum.CmdPacketAddCollectboxItem),
		uint16(dnfenum.CmdPacketRemoveCollectboxItem),
		uint16(dnfenum.CmdPacketExtendCollectboxExpiryDate),
	}
	legacy := make(map[uint16]gameplayHandler, len(opcodes))
	upper := make(map[uint16]gameplayHandler, len(opcodes))
	for _, opcode := range opcodes {
		opcode := opcode
		legacy[opcode] = collectboxDeferredGameplayHandler
		upper[opcode] = defaultClassGameplayHandler(
			"game-collectbox-command-class-blocked",
			"current_exe_collectbox_command_class_mismatch",
			func(service *Service, session *gameSession, body []byte) error {
				return collectboxDeferredGameplayHandler(service, session, gameplayRequest{Opcode: opcode, Body: body})
			},
		)
	}
	return gameplayModuleDefinition{
		Name:           "collectbox",
		LegacyHandlers: legacy,
		UpperHandlers:  upper,
	}
}

func collectboxDeferredGameplayHandler(
	service *Service,
	session *gameSession,
	request gameplayRequest,
) error {
	service.logGameEvent(session, "game-collectbox-deferred",
		"opcode", request.Opcode,
		"body_len", len(request.Body),
		"reason", "collectbox_not_implemented")
	return nil
}
