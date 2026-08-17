package dnfbridge

import (
	"context"
	"encoding/binary"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func adventureGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketRequestAdventureInfo)
	legacyHandlers := map[uint16]gameplayHandler{
		opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
			return service.handleCurrentRequestAdventureInfo(session, request.Body)
		},
		uint16(dnfenum.CmdPacketMercenaryInfo):                     adventureLegacyHandler((*Service).handleCurrentAdventureExpeditionInfo),
		uint16(dnfenum.CmdPacketMercenaryCompetition):              adventureLegacyHandler((*Service).handleCurrentAdventureExpeditionStart),
		uint16(dnfenum.CmdPacketMercenaryCompetitionCancle):        adventureLegacyHandler((*Service).handleCurrentAdventureExpeditionCancel),
		uint16(dnfenum.CmdPacketMercenaryCompetitionRewardRequest): adventureLegacyHandler((*Service).handleCurrentAdventureExpeditionReward),
		uint16(dnfenum.CmdPacketMercenaryPointRecalculate):         adventureLegacyHandler((*Service).handleCurrentAdventurePointRecalculate),
		uint16(dnfenum.CmdPacketAdventurerShopPurchase):            adventureLegacyHandler((*Service).handleCurrentAdventureShopPurchase),
		uint16(dnfenum.CmdPacketAdventureGrowthcapsuleExp):         adventureLegacyHandler((*Service).handleCurrentAdventureGrowthCapsule),
		uint16(dnfenum.CmdPacketGetRepresentCharacJob):             adventureLegacyHandler((*Service).handleCurrentAdventureRepresentCharacters),
	}
	return gameplayModuleDefinition{
		Name:           "adventure-info",
		LegacyHandlers: legacyHandlers,
		UpperHandlers: map[uint16]gameplayHandler{
			uint16(dnfenum.CmdPacketMercenaryInfo): defaultClassGameplayHandler(
				"game-adventure-expedition-info-rejected",
				"unexpected_classification",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAdventureExpeditionInfo(session, body)
				},
			),
			uint16(dnfenum.CmdPacketMercenaryCompetition): defaultClassGameplayHandler(
				"game-adventure-expedition-start-rejected",
				"unexpected_classification",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAdventureExpeditionStart(session, body)
				},
			),
			uint16(dnfenum.CmdPacketMercenaryCompetitionCancle): defaultClassGameplayHandler(
				"game-adventure-expedition-cancel-rejected",
				"unexpected_classification",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAdventureExpeditionCancel(session, body)
				},
			),
			uint16(dnfenum.CmdPacketMercenaryCompetitionRewardRequest): defaultClassGameplayHandler(
				"game-adventure-expedition-reward-rejected",
				"unexpected_classification",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAdventureExpeditionReward(session, body)
				},
			),
			uint16(dnfenum.CmdPacketMercenaryPointRecalculate): defaultClassGameplayHandler(
				"game-adventure-expedition-point-recalculate-rejected",
				"unexpected_classification",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAdventurePointRecalculate(session, body)
				},
			),
			uint16(dnfenum.CmdPacketAdventurerShopPurchase): defaultClassGameplayHandler(
				"game-adventure-shop-purchase-rejected",
				"unexpected_classification",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAdventureShopPurchase(session, body)
				},
			),
			uint16(dnfenum.CmdPacketAdventureGrowthcapsuleExp): defaultClassGameplayHandler(
				"game-adventure-growth-capsule-rejected",
				"unexpected_classification",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAdventureGrowthCapsule(session, body)
				},
			),
			uint16(dnfenum.CmdPacketGetRepresentCharacJob): defaultClassGameplayHandler(
				"game-adventure-represent-characters-rejected",
				"unexpected_classification",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAdventureRepresentCharacters(session, body)
				},
			),
		},
	}
}

func adventureLegacyHandler(
	handler func(*Service, *gameSession, []byte) error,
) gameplayHandler {
	return func(service *Service, session *gameSession, request gameplayRequest) error {
		return handler(service, session, request.Body)
	}
}

// handleCurrentRequestAdventureInfo owns current-EXE class1/op1403 for the
// independently registered adventure-info gameplay.
func (s *Service) handleCurrentRequestAdventureInfo(session *gameSession, request []byte) error {
	targetID, requestOK := currentAdventureInfoRequestTarget(session, request)
	if !requestOK {
		if s != nil {
			s.logGameEvent(session, "game-adventure-info-request-rejected",
				"selected_character_id", selectedCharacterIDForLog(session),
				"body_len", len(request),
				"reason", "request_shape_or_selected_character")
		}
		return nil
	}
	sourceID := selectedCharacterID(session)
	targetSession := session
	if targetID != sourceID {
		if s.onlinePlayers == nil || !s.onlinePlayers.PeerInSameArea(sourceID, targetID) {
			s.logGameEvent(session, "game-adventure-info-request-rejected",
				"selected_character_id", sourceID,
				"target_character_id", targetID,
				"body_len", len(request),
				"reason", "target_not_in_same_area")
			return nil
		}
		var online bool
		targetSession, online = s.onlineGameSession(targetID)
		if !online {
			s.logGameEvent(session, "game-adventure-info-request-rejected",
				"selected_character_id", sourceID,
				"target_character_id", targetID,
				"body_len", len(request),
				"reason", "target_offline")
			return nil
		}
	}

	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || repositories.Account == nil {
		s.logGameEvent(session, "game-adventure-info-request-rejected",
			"selected_character_id", session.selectedCharacterID,
			"body_len", len(request),
			"reason", "repository_unavailable")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterKey := strconv.Itoa(int(targetID))
	selected, found, err := repositories.Character.Load(ctx, characterKey)
	if err != nil || !found || numericCharacterID(selected) != int(targetID) {
		s.logGameEvent(session, "game-adventure-info-request-rejected",
			"selected_character_id", sourceID,
			"target_character_id", targetID,
			"body_len", len(request),
			"character_found", found,
			"error", err,
			"reason", "selected_character_unavailable")
		return nil
	}

	accountID := strings.TrimSpace(s.accountIDForSession(targetSession))
	if accountID == "" || strings.TrimSpace(selected.AccountID) != accountID {
		s.logGameEvent(session, "game-adventure-info-request-rejected",
			"selected_character_id", sourceID,
			"target_character_id", targetID,
			"body_len", len(request),
			"reason", "selected_character_owner_mismatch")
		return nil
	}
	account, accountFound, err := repositories.Account.Load(ctx, accountID)
	if err != nil || !accountFound {
		s.logGameEvent(session, "game-adventure-info-request-rejected",
			"selected_character_id", sourceID,
			"target_character_id", targetID,
			"body_len", len(request),
			"account_found", accountFound,
			"error", err,
			"reason", "account_unavailable")
		return nil
	}

	state, err := s.currentAccountAdventureGroupInfoState(ctx, selected, true, targetSession)
	if err != nil {
		s.logGameEvent(session, "game-adventure-info-request-rejected",
			"selected_character_id", sourceID,
			"target_character_id", targetID,
			"body_len", len(request),
			"error", err,
			"reason", "account_summary_unavailable")
		return nil
	}

	createdDate, dateSource, err := adventureGroupCreatedDisplayDate(account)
	if err != nil {
		s.logGameEvent(session, "game-adventure-info-request-rejected",
			"selected_character_id", sourceID,
			"target_character_id", targetID,
			"body_len", len(request),
			"error", err,
			"reason", "adventure_group_created_date_invalid")
		return nil
	}
	body := buildCurrentAdventureInfoBodyWithState(
		targetID,
		state.Summary,
		state.Characters,
		account.RepresentAccountName,
		createdDate,
		state.Projection,
	)
	s.logGameEvent(session, "game-adventure-info-response-send",
		"msg_id", uint16(dnfenum.CmdPacketRequestAdventureInfo),
		"classification", 1,
		"selected_character_id", sourceID,
		"target_character_id", targetID,
		"request_body_len", len(request),
		"plain_payload_len", len(body),
		"raw_state_len", currentAdventureInfoRawLength,
		"total_point", state.Summary.TotalPoint,
		"manage_level", state.Summary.ManageLevel,
		"roster_count", len(state.Characters),
		"consecutive_login_days", state.Projection.ConsecutiveLoginDays,
		"recommended_dungeon_clears", state.Projection.ContentCounts[adventuregroup.ContentTypeRecommendedDungeon],
		"created_date", createdDate,
		"date_source", dateSource,
		"name_source", "account_repository",
		"growth_capsule_progress", state.Projection.Runtime.GrowthExperience,
		"independent_tail_u32", 0)
	return s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketRequestAdventureInfo), body)
}

func currentAdventureInfoRequestTarget(session *gameSession, request []byte) (uint16, bool) {
	if session == nil || session.selectedCharacterID == 0 {
		return 0, false
	}
	switch len(request) {
	case currentAdventureInfoRequestWireLength:
		return session.selectedCharacterID, true
	case currentAdventureInfoSceneRequestLength:
		targetID := binary.LittleEndian.Uint16(request)
		return targetID, targetID != 0
	default:
		return 0, false
	}
}

func currentAdventureInfoRequestMatchesSession(session *gameSession, request []byte) bool {
	if session == nil || session.selectedCharacterID == 0 {
		return false
	}
	switch len(request) {
	case currentAdventureInfoRequestWireLength:
		return true
	case currentAdventureInfoSceneRequestLength:
		return binary.LittleEndian.Uint16(request) == currentSceneActorObjectKey(session.selectedCharacterID)
	default:
		return false
	}
}

func selectedCharacterIDForLog(session *gameSession) uint16 {
	if session == nil {
		return 0
	}
	return session.selectedCharacterID
}
