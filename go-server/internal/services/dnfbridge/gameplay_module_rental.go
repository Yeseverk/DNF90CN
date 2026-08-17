package dnfbridge

import (
	"context"
	"errors"
	"math"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func rentalGameplayModule() gameplayModuleDefinition {
	rentOpcode := uint16(dnfenum.CmdPacketRentEquipmentItem)
	chargeOpcode := uint16(dnfenum.CmdPacketChargeRentpoint)
	return gameplayModuleDefinition{
		Name: "equipment-rental",
		LegacyHandlers: map[uint16]gameplayHandler{
			rentOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentRentEquipment(session, request.Body)
			},
			chargeOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentChargeRentalPoint(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			rentOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(service, session, request, "game-upper-rental-equipment-request-blocked", "current_exe_op999_command_class_mismatch") {
					return nil
				}
				return service.handleCurrentRentEquipment(session, request.Body)
			},
			chargeOpcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(service, session, request, "game-upper-rental-point-charge-request-blocked", "current_exe_op1000_command_class_mismatch") {
					return nil
				}
				return service.handleCurrentChargeRentalPoint(session, request.Body)
			},
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			rentOpcode: func(body []byte) []byte {
				return stripLegacyTransportTrailer(body, currentRentalRequestWireSize)
			},
			chargeOpcode: func(body []byte) []byte {
				return stripLegacyTransportTrailer(body, currentRentalRequestWireSize)
			},
		},
	}
}

func (s *Service) handleCurrentRentEquipment(session *gameSession, body []byte) error {
	request, err := decodeCurrentRentEquipmentRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-equipment-request-blocked", "body_len", len(body), "reason", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repositories, characterID, character, err := s.currentRentalSelectedCharacter(ctx, session)
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-equipment-request-blocked", "item_id", request.ItemID, "reason", err)
		return nil
	}
	catalog, source, items, err := s.currentRentalSources(ctx)
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-equipment-request-blocked", "item_id", request.ItemID, "reason", err)
		return nil
	}
	jobValue := numericCharacterStat(character.Job)
	if jobValue < 0 || jobValue > math.MaxUint16 {
		s.logGameEvent(session, "game-upper-rental-equipment-request-blocked", "item_id", request.ItemID, "job", character.Job, "reason", "job_out_of_u16_range")
		return nil
	}
	jobTag, err := currentRentalCharacterJobTag(source, byte(jobValue))
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-equipment-request-blocked", "item_id", request.ItemID, "job", jobValue, "reason", err)
		return nil
	}
	now := time.Now().UTC()
	effectiveLevel, overEquipContract := currentRentalEffectiveLevel(
		ctx,
		repositories.Account,
		s.accountIDForSession(session),
		character.Level,
		now,
	)
	tier, ok := catalog.tierForLevel(effectiveLevel)
	if !ok {
		s.logGameEvent(session, "game-upper-rental-equipment-request-blocked",
			"item_id", request.ItemID,
			"level", character.Level,
			"effective_level", effectiveLevel,
			"over_equip_contract", overEquipContract,
			"reason", "no_pvf_tier")
		return nil
	}
	expectedPacked := uint32(uint16(jobValue)) | uint32(uint16(tier))<<16
	if request.PackedJobTier != expectedPacked {
		s.logGameEvent(session, "game-upper-rental-equipment-request-blocked",
			"item_id", request.ItemID,
			"packed_job_tier", request.PackedJobTier,
			"expected_packed_job_tier", expectedPacked,
			"job", jobValue,
			"tier", tier,
			"level", character.Level,
			"effective_level", effectiveLevel,
			"over_equip_contract", overEquipContract,
			"reason", "current_exe_job_tier_key_mismatch")
		return nil
	}
	pointCost, ok := catalog.itemCost(jobTag, tier, request.ItemID)
	if !ok {
		s.logGameEvent(session, "game-upper-rental-equipment-request-blocked", "item_id", request.ItemID, "job_tag", jobTag, "tier", tier, "reason", "item_not_in_current_pvf_group")
		return nil
	}
	definition, err := validateCurrentRentalItem(source, items, request.ItemID, jobTag)
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-equipment-request-blocked", "item_id", request.ItemID, "job_tag", jobTag, "tier", tier, "reason", err)
		return nil
	}
	owner, err := currentRentalAssetOwner(repositories)
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-equipment-request-blocked", "item_id", request.ItemID, "reason", err)
		return nil
	}
	result, err := rentCurrentEquipment(ctx, owner, s.accountIDForSession(session), characterID, request.ItemID, pointCost, definition, now)
	if err != nil {
		resultCode := uint32(1)
		if errors.Is(err, errCurrentRentalInventoryFull) {
			resultCode = 2
		}
		s.logGameEvent(session, "game-upper-rental-equipment-transaction-blocked",
			"character_id", characterID,
			"item_id", request.ItemID,
			"point_cost", pointCost,
			"result_code", resultCode,
			"reason", err)
		return s.sendCurrentRentEquipmentResult(session, resultCode)
	}
	if err := s.sendCurrentRentEquipmentResult(session, 0); err != nil {
		return err
	}
	if err := s.sendSelectedCurrentRentalPointState(session, "op999_after_atomic_rent", true); err != nil {
		return err
	}
	if err := s.sendSelectedCurrentRentalInventoryItemUpdate(session, repositories.Inventory, characterID, request.ItemID, result); err != nil {
		return err
	}
	s.logGameEvent(session, "game-upper-rental-equipment-applied",
		"character_id", characterID,
		"item_id", request.ItemID,
		"job", jobValue,
		"job_tag", jobTag,
		"tier", tier,
		"point_cost", pointCost,
		"remaining_points", result.Points,
		"slot", result.Slot,
		"equipped", result.Equipped,
		"expire_unix", result.ExpireAt.Unix(),
		"pvf_path", definition.PVFPath,
		"durability", definition.Durability)
	return nil
}

func (s *Service) handleCurrentChargeRentalPoint(session *gameSession, body []byte) error {
	request, err := decodeCurrentChargeRentalPointRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-point-charge-request-blocked", "body_len", len(body), "reason", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repositories, characterID, _, err := s.currentRentalSelectedCharacter(ctx, session)
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-point-charge-request-blocked", "count", request.Count, "reason", err)
		return nil
	}
	catalog, _, _, err := s.currentRentalSources(ctx)
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-point-charge-request-blocked", "count", request.Count, "reason", err)
		return nil
	}
	owner, err := currentRentalAssetOwner(repositories)
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-point-charge-request-blocked", "count", request.Count, "reason", err)
		return nil
	}
	result, err := purchaseCurrentRentalPoints(ctx, owner, s.accountIDForSession(session), characterID, request.Count, catalog.Limit, catalog.GoldPerPoint, time.Now().UTC())
	if err != nil {
		s.logGameEvent(session, "game-upper-rental-point-charge-transaction-blocked",
			"character_id", characterID,
			"count", request.Count,
			"point_limit", catalog.Limit,
			"gold_per_point", catalog.GoldPerPoint,
			"reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketChargeRentpoint), 1)
	}
	var response packetWriter
	response.writeUint32(request.ChargeType)
	response.writeUint32(0)
	response.writeUint32(result.Points)
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketChargeRentpoint), response.bytes()); err != nil {
		return err
	}
	if err := s.sendSelectedCurrentRentalPointState(session, "op1000_after_atomic_charge", true); err != nil {
		return err
	}
	s.logGameEvent(session, "game-upper-rental-point-charge-applied",
		"character_id", characterID,
		"count", request.Count,
		"new_points", result.Points,
		"remaining_gold", result.Gold,
		"gold_per_point", catalog.GoldPerPoint,
		"point_limit", catalog.Limit)
	return nil
}
