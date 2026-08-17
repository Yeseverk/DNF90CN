package dnfbridge

import (
	"context"
	"strings"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// sendCurrentAdventureActorRefreshFromAccount owns current-EXE class0/op1346.
func (s *Service) sendCurrentAdventureActorRefreshFromAccount(
	session *gameSession,
	currentObjectKey uint16,
	source string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	state, err := s.currentAccountAdventureGroupState(ctx, dnfrepo.CharacterRecord{}, false, session)
	if err != nil {
		s.logGameEvent(session, "game-adventure-actor-refresh-skipped",
			"msg_id", currentAdventureActorRefreshMsgID,
			"source", source,
			"current_object_key", currentObjectKey,
			"error", err,
			"reason", "account_summary_unavailable")
		return nil
	}
	body := buildCurrentAdventureActorRefreshBody(currentObjectKey, state.Summary)
	s.logGameEvent(session, "game-adventure-actor-refresh-send",
		"msg_id", currentAdventureActorRefreshMsgID,
		"classification", 0,
		"source", source,
		"current_object_key", currentObjectKey,
		"plain_payload_len", len(body),
		"raw_state_len", currentAdventureActorRefreshRawLength,
		"total_point", state.Summary.TotalPoint,
		"manage_level", state.Summary.ManageLevel)
	return s.sendGameUpperRawClass(session, currentAdventureActorRefreshMsgID, body, 0)
}

// sendCurrentAdventureInfoPushFromAccount owns the current-EXE class0/op1340
// account-state push used before character selection.
func (s *Service) sendCurrentAdventureInfoPushFromAccount(
	session *gameSession,
	currentObjectKey uint16,
	source string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	state, err := s.currentAccountAdventureGroupInfoState(ctx, dnfrepo.CharacterRecord{}, false, session)
	if err != nil {
		s.logGameEvent(session, "game-adventure-info-push-skipped",
			"msg_id", currentAdventureInfoPushMsgID,
			"source", source,
			"current_object_key", currentObjectKey,
			"error", err,
			"reason", "account_summary_unavailable")
		return nil
	}
	name, createdDate, dateSource, err := s.currentRepresentAccountIdentity(ctx, session)
	if err != nil {
		s.logGameEvent(session, "game-adventure-info-push-skipped",
			"msg_id", currentAdventureInfoPushMsgID,
			"source", source,
			"error", err,
			"reason", "represent_account_name_unavailable")
		return nil
	}
	return s.sendCurrentAdventureInfoPushState(
		session,
		currentObjectKey,
		state.Summary,
		state.Characters,
		state.Projection,
		name,
		createdDate,
		dateSource,
		source,
	)
}

// sendCurrentSelectorAdventureInfoAfterHiddenProbe sends the one safe
// role-list adventure refresh after op645 proves selector objects exist.
func (s *Service) sendCurrentSelectorAdventureInfoAfterHiddenProbe(session *gameSession) error {
	if session == nil || !session.selectorAdventureInfoPending {
		return nil
	}
	slot := session.selectorAdventureInfoSlot
	session.selectorAdventureInfoPending = false

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	state, err := s.currentAccountAdventureGroupInfoState(ctx, dnfrepo.CharacterRecord{}, false, session)
	if err != nil {
		s.logGameEvent(session, "game-selector-adventure-info-push-skipped",
			"msg_id", currentAdventureInfoPushMsgID,
			"slot", slot,
			"error", err,
			"reason", "account_summary_unavailable")
		return nil
	}
	slotExists := false
	for _, character := range state.Characters {
		if character.Slot == int(slot) {
			slotExists = true
			break
		}
	}
	if !slotExists {
		s.logGameEvent(session, "game-selector-adventure-info-push-skipped",
			"msg_id", currentAdventureInfoPushMsgID,
			"slot", slot,
			"reason", "remembered_slot_not_in_current_roster")
		return nil
	}
	name, createdDate, dateSource, err := s.currentRepresentAccountIdentity(ctx, session)
	if err != nil {
		s.logGameEvent(session, "game-selector-adventure-info-push-skipped",
			"msg_id", currentAdventureInfoPushMsgID,
			"slot", slot,
			"error", err,
			"reason", "represent_account_name_unavailable")
		return nil
	}
	if strings.TrimSpace(name) == "" {
		s.logGameEvent(session, "game-selector-adventure-info-push-skipped",
			"msg_id", currentAdventureInfoPushMsgID,
			"slot", slot,
			"reason", "represent_account_name_empty")
		return nil
	}
	return s.sendCurrentAdventureInfoPushState(
		session,
		slot,
		state.Summary,
		state.Characters,
		state.Projection,
		name,
		createdDate,
		dateSource,
		"charac_view_hidden_info_remembered_slot",
	)
}

func (s *Service) sendCurrentAdventureInfoPushState(
	session *gameSession,
	currentObjectKey uint16,
	summary adventuregroup.Summary,
	characters []dnfrepo.CharacterRecord,
	projection adventuregroup.Projection,
	representAccountName string,
	createdDate uint32,
	dateSource string,
	source string,
) error {
	body := buildCurrentAdventureInfoBodyWithState(
		currentObjectKey,
		summary,
		characters,
		representAccountName,
		createdDate,
		projection,
	)
	s.logGameEvent(session, "game-adventure-info-push-send",
		"msg_id", currentAdventureInfoPushMsgID,
		"classification", 0,
		"source", source,
		"current_object_key", currentObjectKey,
		"plain_payload_len", len(body),
		"raw_state_len", currentAdventureInfoRawLength,
		"total_point", summary.TotalPoint,
		"manage_level", summary.ManageLevel,
		"roster_count", len(characters),
		"consecutive_login_days", projection.ConsecutiveLoginDays,
		"recommended_dungeon_clears", projection.ContentCounts[adventuregroup.ContentTypeRecommendedDungeon],
		"represent_account_name_set", strings.TrimSpace(representAccountName) != "",
		"created_date", createdDate,
		"date_source", dateSource)
	return s.sendGameUpperRawClass(session, currentAdventureInfoPushMsgID, body, 0)
}
