package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentPremiumServiceBlobSize = 74

func premiumGameplayModule() gameplayModuleDefinition {
	serviceOpcode := uint16(dnfenum.CmdPacketPremiumService)
	recoverOpcode := uint16(dnfenum.CmdPacketRecoverStamina)
	crystalUpdateOpcode := uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo)
	crystalConsumeOpcode := uint16(dnfenum.CmdPacketUseLimitCube)
	serviceHandler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentPremiumService(session, request.Body)
	}
	crystalUpdateHandler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentCrystalContractUpdate(session, request.Body)
	}
	crystalConsumeHandler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentLimitedCubeOrCrystalContractCubeUse(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name: "premium-service",
		LegacyHandlers: map[uint16]gameplayHandler{
			serviceOpcode:        serviceHandler,
			crystalUpdateOpcode:  crystalUpdateHandler,
			crystalConsumeOpcode: crystalConsumeHandler,
		},
		UpperHandlers: map[uint16]gameplayHandler{
			serviceOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(
					service,
					session,
					request,
					"game-premium-service-request-blocked",
					"current_exe_op903_command_class_mismatch",
				) {
					return nil
				}
				return serviceHandler(service, session, request)
			},
			recoverOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(
					service,
					session,
					request,
					"game-premium-free-weakness-blocked",
					"current_exe_op9_command_class_mismatch",
				) {
					return nil
				}
				return service.handleCurrentPremiumFreeWeakness(session, request.Body)
			},
			crystalUpdateOpcode: defaultClassGameplayHandler(
				"game-crystal-contract-selection-request-blocked",
				"current_exe_op535_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return crystalUpdateHandler(service, session, gameplayRequest{Body: body})
				},
			),
			crystalConsumeOpcode: defaultClassGameplayHandler(
				"game-limit-cube-use-request-blocked",
				"current_exe_op338_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return crystalConsumeHandler(service, session, gameplayRequest{Body: body})
				},
			),
		},
	}
}

func (s *Service) handleCurrentPremiumService(session *gameSession, body []byte) error {
	// Current EXE action 1 carries a 74-byte blob. sub_206A240 sends slot 3
	// when the seven-buff service is actually applied; account for that daily
	// use before returning the complete authoritative state.
	if len(body) >= 2+currentPremiumServiceBlobSize && binary.LittleEndian.Uint16(body[:2]) == 1 {
		slot := int64(binary.LittleEndian.Uint16(body[2:4]))
		if slot == premium.DevilSlotSevenBuff {
			if err := s.consumeCurrentPremiumServiceDaily(session, slot, time.Now().UTC()); err != nil {
				s.logGameEvent(session, "game-premium-service-use-blocked",
					"slot", slot,
					"reason", err)
			}
		}
	}
	return s.sendCurrentPremiumServiceState(session, "op903_request")
}

func (s *Service) handleCurrentPremiumFreeWeakness(session *gameSession, body []byte) error {
	// NoPack sub_30C6B10 sends class1/op9 u16(1) only after the slot-5
	// 魔王契约 path clears the weakness UI without charging gold. The paid
	// recovery path sends u16(0), so it must not spend the contract quota.
	if len(body) < 2 || binary.LittleEndian.Uint16(body[:2]) != 1 {
		return nil
	}
	if err := s.consumeCurrentPremiumServiceDaily(
		session,
		premium.DevilSlotFreeWeakness,
		time.Now().UTC(),
	); err != nil {
		s.logGameEvent(session, "game-premium-free-weakness-use-blocked",
			"slot", premium.DevilSlotFreeWeakness,
			"reason", err)
	}
	return s.sendCurrentPremiumServiceState(session, "free_weakness_after_op9")
}

func (s *Service) currentPremiumServiceRecords(
	ctx context.Context,
	session *gameSession,
) (dnfrepo.Group, dnfrepo.AccountRecord, dnfrepo.CharacterRecord, error) {
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Account == nil || repositories.Character == nil {
		return dnfrepo.Group{}, dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, fmt.Errorf("premium repositories are unavailable")
	}
	if session == nil || session.selectedCharacterID == 0 {
		return dnfrepo.Group{}, dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, fmt.Errorf("premium selected character is missing")
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	account, found, err := repositories.Account.Load(ctx, accountID)
	if err != nil {
		return dnfrepo.Group{}, dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, fmt.Errorf("load premium account: %w", err)
	}
	if !found {
		return dnfrepo.Group{}, dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, fmt.Errorf("premium account %q is missing", accountID)
	}
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return dnfrepo.Group{}, dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, fmt.Errorf("load premium character: %w", err)
	}
	if !found || strings.TrimSpace(character.AccountID) != accountID {
		return dnfrepo.Group{}, dnfrepo.AccountRecord{}, dnfrepo.CharacterRecord{}, fmt.Errorf("premium character ownership mismatch")
	}
	return repositories, account, character, nil
}

func (s *Service) consumeCurrentPremiumServiceDaily(session *gameSession, slot int64, now time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Account == nil || repositories.CharacterAssets == nil {
		return fmt.Errorf("premium account/character transaction is unavailable")
	}
	if session == nil || session.selectedCharacterID == 0 {
		return fmt.Errorf("premium selected character is missing")
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	account, found, err := repositories.Account.Load(ctx, accountID)
	if err != nil {
		return fmt.Errorf("load premium account: %w", err)
	}
	if !found {
		return fmt.Errorf("premium account %q is missing", accountID)
	}
	if !premium.Active(account, premium.DevilSlotType(slot), now) {
		return fmt.Errorf("premium slot %d is inactive", slot)
	}
	return repositories.CharacterAssets.WithinCharacterAssets(
		ctx,
		characterID,
		func(
			characters dnfrepo.CharacterRepository,
			_ dnfrepo.InventoryRepository,
			_ dnfrepo.EquipmentRepository,
		) error {
			character, characterFound, loadErr := characters.Load(ctx, characterID)
			if loadErr != nil {
				return fmt.Errorf("load premium character: %w", loadErr)
			}
			if !characterFound || strings.TrimSpace(character.AccountID) != accountID {
				return fmt.Errorf("premium character ownership mismatch")
			}
			if !premium.TryConsumeDaily(&character, slot, now) {
				return fmt.Errorf("premium slot %d daily limit %d reached", slot, premium.DailyLimit(slot))
			}
			return dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats)
		},
	)
}

func (s *Service) sendCurrentPremiumServiceState(session *gameSession, source string) error {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Account == nil || repositories.Character == nil {
		return fmt.Errorf("premium repositories are unavailable")
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	account, found, err := repositories.Account.Load(ctx, accountID)
	if err != nil {
		return fmt.Errorf("load premium account state: %w", err)
	}
	if !found {
		account = dnfrepo.AccountRecord{}
	}
	character := dnfrepo.CharacterRecord{}
	if session != nil && session.selectedCharacterID != 0 {
		characterID := strconv.Itoa(int(session.selectedCharacterID))
		loaded, characterFound, loadErr := repositories.Character.Load(ctx, characterID)
		if loadErr != nil {
			return fmt.Errorf("load premium character state: %w", loadErr)
		}
		if characterFound {
			if strings.TrimSpace(loaded.AccountID) != accountID {
				return fmt.Errorf("premium character ownership mismatch")
			}
			character = loaded
		}
	}
	now := time.Now().UTC()
	body := buildCurrentPremiumServiceDataBody(account, character, now)
	s.logGameEvent(session, "game-premium-service-state-send",
		"source", source,
		"msg_id", uint16(dnfenum.CmdPacketPremiumService),
		"classification", dnfproto.DefaultChannelClassification,
		"body_len", len(body),
		"action", 1,
		"slot_count", premium.DevilSlotCount,
		"state_source", "account_expiry_and_character_service_day_usage")
	return s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketPremiumService),
		body,
		dnfproto.DefaultChannelClassification,
	)
}
