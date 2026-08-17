package dnfbridge

import (
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func dungeonCombatDropGameplayModule() gameplayModuleDefinition {
	dropOpcode := uint16(dnfenum.CmdPacketDropItem)
	dieMonsterOpcode := uint16(dnfenum.CmdPacketDieMonster)
	pickupOpcode := uint16(dnfenum.CmdPacketGetItem)
	bossCheckOpcode := uint16(dnfenum.CmdPacketBossDieCheck)

	return gameplayModuleDefinition{
		Name: "dungeon-combat-drop",
		LegacyHandlers: map[uint16]gameplayHandler{
			dropOpcode: legacyDungeonDiscardGameplayHandler,
			dieMonsterOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleDungeonMonsterDeath(session, request.Body)
			},
			pickupOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentDungeonPickup(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			dropOpcode: upperDungeonDiscardGameplayHandler,
			dieMonsterOpcode: defaultClassGameplayHandler(
				"game-dungeon-monster-death-blocked",
				"current_exe_op39_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonMonsterDeath(session, body)
				},
			),
			pickupOpcode: defaultClassGameplayHandler(
				"game-dungeon-pickup-blocked",
				"current_exe_op43_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentDungeonPickup(session, body)
				},
			),
			bossCheckOpcode: defaultClassGameplayHandler(
				"game-dungeon-boss-die-check-blocked",
				"current_exe_op117_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleDungeonBossDieCheck(session, body)
				},
			),
		},
	}
}

func legacyDungeonDiscardGameplayHandler(
	service *Service,
	session *gameSession,
	request gameplayRequest,
) error {
	if handled, err := service.handleCurrentDungeonDiscard(session, request.Body); handled || err != nil {
		return err
	}
	if handled, err := service.handleAlignedGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		request.Opcode,
		request.Body,
	); handled || err != nil {
		return err
	}
	service.logInfo("dnfbridge unhandled latest game command",
		"cmd", byte(dnfenum.GameCmdCommand),
		"type", request.Opcode,
		"runtime_cmd_name", dnfenum.CmdPacketName(request.Opcode),
		"runtime_cmd_known", dnfenum.IsKnownCmdPacket(request.Opcode),
		"body_len", len(request.Body))
	return nil
}

func upperDungeonDiscardGameplayHandler(
	service *Service,
	session *gameSession,
	request gameplayRequest,
) error {
	if request.Classification != dnfproto.DefaultChannelClassification {
		service.logInfo("dnfbridge unhandled latest upper packet",
			"msg_id", request.Opcode,
			"runtime_cmd_name", "",
			"runtime_cmd_known", false,
			"body_len", len(request.Body))
		return nil
	}
	if handled, err := service.handleCurrentDungeonDiscard(session, request.Body); handled || err != nil {
		return err
	}
	if handled, err := service.handleAlignedGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		request.Opcode,
		request.Body,
	); handled || err != nil {
		if err != nil {
			return err
		}
		service.logPacketEvent("game-upper-aligned-fallback-handled",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", request.Opcode,
			"runtime_cmd_name", dnfenum.CmdPacketName(request.Opcode),
			"runtime_cmd_known", dnfenum.IsKnownCmdPacket(request.Opcode),
			"body_len", len(request.Body),
			"reason", "unlisted_upper_command_routed_to_server_aligned_owner")
		return nil
	}
	service.logInfo("dnfbridge unhandled latest upper packet",
		"msg_id", request.Opcode,
		"runtime_cmd_name", dnfenum.CmdPacketName(request.Opcode),
		"runtime_cmd_known", dnfenum.IsKnownCmdPacket(request.Opcode),
		"body_len", len(request.Body))
	return nil
}
