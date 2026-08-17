package dnfbridge

import (
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

const (
	currentInitialTownProgress                        = uint32(36)
	currentInitialTownEntryDelay                      = time.Duration(0)
	currentChannelReconnectProbeSize                  = 31
	currentChannelReconnectDisplayProbeSize           = 590
	reference90CNChannelReconnectDisplayProbeBodySize = 598
)

type currentInitialTownRouteStage byte

type currentInitialTownRoutePolicy struct {
	includeReportClientSpec bool
	includeSecondFullMode1  bool
	skipPrerequisiteMode1   bool
}

const (
	currentInitialTownRouteIdle currentInitialTownRouteStage = iota
	currentInitialTownRouteArmed
	currentInitialTownRouteActionTableSent
	currentInitialTownRouteObjectSent
	currentInitialTownRouteActorBound
	currentInitialTownRoutePlayerStatePrepared
	currentInitialTownRouteTransitionSent
	currentInitialTownRoutePlayerStateSent
)

func isCurrentChannelReconnectLifecycleBody(msgID uint16, classification byte, bodyLen int) bool {
	if classification != dnfproto.DefaultChannelClassification {
		return false
	}
	switch dnfenum.UpperMsg(msgID) {
	case dnfenum.UpperMsgGameEndpoint:
		return isCurrentChannelReconnectDisplayProbeBodyLen(bodyLen)
	case dnfenum.UpperMsgCharacterRoster:
		return bodyLen == currentChannelReconnectProbeSize
	default:
		return false
	}
}

func isCurrentChannelReconnectDisplayProbeBodyLen(bodyLen int) bool {
	return bodyLen == currentChannelReconnectDisplayProbeSize ||
		bodyLen == reference90CNChannelReconnectDisplayProbeBodySize
}

func markCurrentChannelReconnect(session *gameSession, bodyLen int) bool {
	if session == nil || bodyLen != currentChannelReconnectProbeSize ||
		session.rosterRequested || currentTownSceneReady(session) || currentDungeonSceneActive(session) {
		return false
	}
	armCurrentChannelReconnect(session)
	return true
}

func markCurrentChannelReconnectOnSelection(session *gameSession) bool {
	if session == nil || session.channelReconnect || session.rosterRequested ||
		session.selectedCharacterID != 0 || currentTownSceneReady(session) || currentDungeonSceneActive(session) {
		return false
	}
	armCurrentChannelReconnect(session)
	return true
}

func armCurrentChannelReconnect(session *gameSession) {
	if session == nil {
		return
	}
	session.channelReconnect = true
	session.sceneBootstrapTailDeferred = false
	session.sceneBootstrapTailSent = false
	session.townActorOwnerChannel = currentConnectionTownActorOwnerContext(session)
}

func currentTownSceneReady(session *gameSession) bool {
	if session == nil {
		return false
	}
	session.townMu.Lock()
	ready := session.townSceneReadyCharacterID != 0 ||
		session.initialTownRouteStage >= currentInitialTownRouteTransitionSent
	session.townMu.Unlock()
	return ready
}

func currentDungeonSceneActive(session *gameSession) bool {
	if session == nil {
		return false
	}
	session.dungeon.mu.Lock()
	active := session.dungeon.runtime != nil
	session.dungeon.mu.Unlock()
	return active
}

func (s *Service) resetCurrentInitialTownRoute(session *gameSession) {
	if session == nil {
		return
	}
	session.townMu.Lock()
	resetCurrentTownPostTransitionPlayerState(session)
	session.initialTownRouteCharacterID = 0
	session.initialTownRouteStage = currentInitialTownRouteIdle
	session.initialTownActorSceneSnapshotSent = false
	session.initialTownLocationNotificationsSent = false
	session.initialTownQuestSnapshotsSent = false
	session.initialTownSkillInfoPrepared = false
	session.initialTownSkillInfoSent = false
	session.initialTownSkillInfo = currentSceneSkillInfoProjection{}
	session.initialTownLegacySceneReadyAccepted = false
	session.initialTownAdventureOverheadRefreshSent = false
	session.initialTownCombatPowerAffixesSent = false
	session.crystalContractMu.Lock()
	session.crystalContractTownUIReadyStateSent = false
	session.crystalContractMu.Unlock()
	session.auraSkinMu.Lock()
	session.auraSkinTownUIReadyStateSent = false
	session.auraSkinMu.Unlock()
	session.townActorOwnerChannel = currentConnectionTownActorOwnerContext(session)
	session.townMu.Unlock()
}
