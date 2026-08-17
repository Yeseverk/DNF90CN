package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfitemgrade "longheng.io/server/internal/modules/dnf/itemgrade"
)

// currentResetItemAttrOpcode is CMD 81 (0x0051): equipment grade adjustment box.
const (
	currentResetItemAttrOpcode      uint16 = uint16(dnfenum.CmdPacketResetItemAttr)
	currentResetItemAttrGradeResult uint16 = 2
)

type currentResetItemAttrRequest struct {
	TargetSlot   int16
	TargetItemID int32
	MaterialSlot int16
}

func decodeCurrentResetItemAttrRequest(body []byte) (currentResetItemAttrRequest, error) {
	if len(body) < 8 {
		return currentResetItemAttrRequest{}, fmt.Errorf("reset_item_attr body too short: %d", len(body))
	}
	return currentResetItemAttrRequest{
		TargetSlot:   int16(binary.LittleEndian.Uint16(body[0:2])),
		TargetItemID: int32(binary.LittleEndian.Uint32(body[2:6])),
		MaterialSlot: int16(binary.LittleEndian.Uint16(body[6:8])),
	}, nil
}

func (s *Service) handleCurrentResetItemAttr(session *gameSession, body []byte) error {
	request, err := decodeCurrentResetItemAttrRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-reset-item-attr-parse-failed", "body_len", len(body), "err", err)
		return s.sendGameUpperFailure(session, currentResetItemAttrOpcode, 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repos, ok := s.repositoryGroup()
	if !ok {
		return s.sendGameUpperFailure(session, currentResetItemAttrOpcode, 2)
	}

	var catalog dnfitemgrade.ItemCatalog
	if pvfCatalog, catalogErr := s.currentPVFItemCatalog(); catalogErr == nil {
		catalog = currentItemGradeCatalog{catalog: pvfCatalog}
	}
	owner, err := dnfitemgrade.NewOwner(repos, catalog, nil)
	if err != nil {
		return s.sendGameUpperFailure(session, currentResetItemAttrOpcode, 2)
	}
	result, err := owner.Adjust(ctx, dnfitemgrade.Command{
		SelectedCharacterID: session.selectedCharacterID,
		TargetSlot:          request.TargetSlot,
		TargetItemID:        request.TargetItemID,
		MaterialSlot:        request.MaterialSlot,
	})
	switch {
	case errors.Is(err, dnfitemgrade.ErrTargetMissing):
		s.logGameEvent(session, "game-reset-item-attr-target-missing",
			"target_slot", request.TargetSlot,
			"target_item_id", request.TargetItemID)
		return s.sendGameUpperFailure(session, currentResetItemAttrOpcode, 2)
	case errors.Is(err, dnfitemgrade.ErrMaterialMissing):
		s.logGameEvent(session, "game-reset-item-attr-material-missing",
			"material_slot", request.MaterialSlot)
		return s.sendGameUpperFailure(session, currentResetItemAttrOpcode, 3)
	case err != nil:
		s.logGameEvent(session, "game-reset-item-attr-save-failed", "err", err)
		return s.sendGameUpperFailure(session, currentResetItemAttrOpcode, 4)
	}

	s.logGameEvent(session, "game-reset-item-attr-success",
		"char_id", result.CharacterID,
		"target_slot", result.TargetSlot,
		"target_item_id", result.TargetItemID,
		"material_slot", result.MaterialSlot,
		"material_item_id", result.MaterialItemID,
		"new_seed", result.NewSeed,
		"is_gold", result.GoldKaleido)

	// Current EXE sub_1D16480 reads u16 slot + u32 amount + u16 resultType.
	// Those fields describe the consumed adjustment material, not the target
	// equipment: update/remove the material object at its real slot, then select
	// the native grade-adjust result. Result type 1 is the unrelated item-seal
	// announcement ("item packaged successfully") in this client build.
	var w packetWriter
	w.writeUint16(uint16(result.MaterialSlot))
	w.writeUint32(uint32(result.MaterialRemaining))
	w.writeUint16(currentResetItemAttrGradeResult)
	if err := s.sendGameUpperSuccess(session, currentResetItemAttrOpcode, w.bytes()); err != nil {
		return err
	}

	// The target remains present while op81 consumes only the material. A
	// single-row op14 therefore updates the existing equipment object with the
	// committed full quality seed; unlike the former full op13 refresh it does
	// not rebuild every inventory object or destroy the grade-adjust UI state.
	targetEntry := currentItemListEntryFromStack(0, result.TargetSlot, result.TargetStack)
	refreshBody := buildCurrentItemUpdateBody(0, []currentItemListEntry{targetEntry})
	return s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketWalkoutPartyMember),
		refreshBody,
		0,
	)
}

type currentItemGradeCatalog struct {
	catalog *pvfDungeonDropCatalog
}

func (c currentItemGradeCatalog) ResolveItem(itemID uint32) (dnfitemgrade.ItemDefinition, error) {
	if c.catalog == nil {
		return dnfitemgrade.ItemDefinition{}, dnfitemgrade.ErrOwnerUnavailable
	}
	definition, err := c.catalog.ResolveItem(itemID)
	if err != nil {
		return dnfitemgrade.ItemDefinition{}, err
	}
	return dnfitemgrade.ItemDefinition{StackableType: definition.StackableType}, nil
}
