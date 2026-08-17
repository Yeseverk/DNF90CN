package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfequipmenteffect "longheng.io/server/internal/modules/dnf/equipmenteffect"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// currentAddEquipmentEffectOpcode is CMD 951: weapon effect rune add.
// C# 86JP uses opcode 0x0342 (834); our NoPack EXE enum maps it to 951.
const (
	currentAddEquipmentEffectOpcode      uint16 = uint16(dnfenum.CmdPacketAddEquipmentEffect)
	currentEquipmentEffectRequestBodyLen        = 21

	// Current mode-1 construction forwards raw+0x45..+0x46 into the
	// ordinary-equipment state consumed by sub_1E636E0.  The preceding bytes
	// are the proved current emblem vector: a count at raw+0x3C plus two u32
	// IDs.  raw+0x38 remains the independent expiration timestamp.
	currentEquipmentEffectRuneWireOffset = currentEquipmentVectorOffset + currentEquipmentEmblemDataBytes
)

type currentAddEquipmentEffectRequest struct {
	RequestedSourceSlot int16
	TargetListType      byte
	TargetSlot          int16
	RawBody             []byte
}

func decodeCurrentAddEquipmentEffectRequest(body []byte) (currentAddEquipmentEffectRequest, error) {
	if len(body) != currentEquipmentEffectRequestBodyLen {
		return currentAddEquipmentEffectRequest{}, fmt.Errorf("equipment-effect body size=%d want=%d", len(body), currentEquipmentEffectRequestBodyLen)
	}
	targetListType := body[12]
	if targetListType != dnfrepo.MainInventoryListType {
		return currentAddEquipmentEffectRequest{}, fmt.Errorf("equipment-effect target list=%d is not current-exe main inventory", targetListType)
	}
	targetSlot, ok := currentEquipmentEffectSlotAt(body, 13)
	if !ok {
		return currentAddEquipmentEffectRequest{}, fmt.Errorf("equipment-effect invalid target slot")
	}
	sourceSlot, ok := currentEquipmentEffectSlotAt(body, 17)
	if !ok {
		return currentAddEquipmentEffectRequest{}, fmt.Errorf("equipment-effect invalid source slot")
	}
	return currentAddEquipmentEffectRequest{
		RequestedSourceSlot: sourceSlot,
		TargetListType:      targetListType,
		TargetSlot:          targetSlot,
		RawBody:             append([]byte(nil), body...),
	}, nil
}

func currentEquipmentEffectSlotAt(body []byte, offset int) (int16, bool) {
	if offset < 0 || offset+4 > len(body) {
		return 0, false
	}
	value := binary.LittleEndian.Uint32(body[offset : offset+4])
	if value > math.MaxInt16 {
		return 0, false
	}
	return int16(value), true
}

// currentApplyEquipmentEffectRuneToEntry mirrors only the durable rune ID
// into the proved current-equipment field. The field must be cleared when the
// durable state is absent so an old raw row cannot resurrect a removed rune.
func currentApplyEquipmentEffectRuneToEntry(entry *currentItemListEntry, extra map[string]string) {
	if entry == nil || currentEquipmentEffectRuneWireOffset+2 > len(entry.data) {
		return
	}
	runeID := sceneInventoryExtraUint16(extra, "equipment_effect_id")
	binary.LittleEndian.PutUint16(
		entry.data[currentEquipmentEffectRuneWireOffset:currentEquipmentEffectRuneWireOffset+2],
		runeID,
	)
}

type currentEquipmentEffectCatalog struct {
	catalog *pvfDungeonDropCatalog
}

func (c currentEquipmentEffectCatalog) ResolveEquipmentEffectItem(itemID uint32) (dnfequipmenteffect.ItemDefinition, error) {
	if c.catalog == nil {
		return dnfequipmenteffect.ItemDefinition{}, errors.New("runtime PVF catalog unavailable")
	}
	definition, err := c.catalog.ResolveItem(itemID)
	if err != nil {
		return dnfequipmenteffect.ItemDefinition{}, err
	}
	return dnfequipmenteffect.ItemDefinition{
		IsEquipment:   definition.Kind == dungeonDropItemEquipment,
		EquipmentType: definition.EquipmentType,
		Grade:         definition.Grade,
		StackableType: definition.StackableType,
		EffectID:      definition.EquipmentEffectID,
	}, nil
}

func (s *Service) handleCurrentAddEquipmentEffect(session *gameSession, body []byte) error {
	request, err := decodeCurrentAddEquipmentEffectRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-add-equipment-effect-parse-failed", "body_len", len(body), "err", err)
		return s.sendGameUpperFailure(session, currentAddEquipmentEffectOpcode, 4)
	}
	if session == nil || session.selectedCharacterID == 0 {
		return s.sendGameUpperFailure(session, currentAddEquipmentEffectOpcode, 4)
	}
	repositories, ok := s.repositoryGroup()
	if !ok {
		return s.sendGameUpperFailure(session, currentAddEquipmentEffectOpcode, 4)
	}
	pvfCatalog, err := s.currentPVFItemCatalog()
	if err != nil {
		s.logGameEvent(session, "game-add-equipment-effect-pvf-unavailable", "err", err)
		return s.sendGameUpperFailure(session, currentAddEquipmentEffectOpcode, 4)
	}
	owner, err := dnfequipmenteffect.NewOwner(repositories, currentEquipmentEffectCatalog{catalog: pvfCatalog})
	if err != nil {
		s.logGameEvent(session, "game-add-equipment-effect-owner-unavailable", "err", err)
		return s.sendGameUpperFailure(session, currentAddEquipmentEffectOpcode, 4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := owner.Apply(ctx, dnfequipmenteffect.Command{
		CharacterID:         strconv.Itoa(int(session.selectedCharacterID)),
		RequestedSourceSlot: request.RequestedSourceSlot,
		TargetListType:      request.TargetListType,
		TargetSlot:          request.TargetSlot,
		UpdatedAt:           time.Now().UTC(),
	})
	if err != nil {
		s.logGameEvent(session, "game-add-equipment-effect-rejected",
			"source_requested_slot", request.RequestedSourceSlot,
			"target_list", request.TargetListType,
			"target_slot", request.TargetSlot,
			"err", err)
		return s.sendGameUpperFailure(session, currentAddEquipmentEffectOpcode, 4)
	}

	// The current EXE's successful handler consumes one leading success byte
	// followed by the exact 21-byte request it just sent. It does not accept a
	// synthetic item-id response here.
	if err := s.sendGameUpperSuccess(session, currentAddEquipmentEffectOpcode, request.RawBody); err != nil {
		return err
	}
	s.logGameEvent(session, "game-add-equipment-effect-success",
		"char_id", result.CharacterID,
		"source_requested_slot", result.RequestedSourceSlot,
		"source_slot", result.SourceSlot,
		"source_recovered", result.SourceRecovered,
		"source_item_id", result.SourceItemID,
		"source_remaining", result.SourceRemainingCount,
		"target_slot", result.TargetSlot,
		"target_item_id", result.TargetItemID,
		"effect_id", result.EffectID)
	return s.sendSelectedIncrementalItemSlotRefreshes(session, "equipment_effect_rune", []alignedcmd.ItemSlotRefresh{
		{ListType: dnfrepo.MainInventoryListType, SlotIndex: result.SourceSlot},
		{ListType: result.TargetListType, SlotIndex: result.TargetSlot},
	})
}
