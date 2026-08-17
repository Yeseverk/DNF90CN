package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"

	"longheng.io/server/internal/modules/dnf/crystalcontract"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const (
	currentCrystalContractUpdateBodySize  = 2
	currentCrystalContractConsumeBodySize = 8

	// Current NoPack registers sub_1E7FAD0 for op898 in the notification table
	// near 0x4081CEA through sub_225C940. sub_2261E30 reaches that table only
	// from its classification-0 branch; classification 1 first consumes the
	// success/error envelope and dispatches through the separate response table.
	// The generated enum labels this numeric slot as an older housing command,
	// so keep the current-client compatibility meaning local.
	currentCrystalContractStateMsgID uint16 = 898
)

func (s *Service) currentCrystalContractOwner() (*crystalcontract.Owner, error) {
	repositories, ok := s.repositoryGroup()
	if !ok {
		return nil, crystalcontract.ErrRepositoriesUnavailable
	}
	premiumCatalog, err := s.currentPremiumCatalog()
	if err != nil {
		return nil, err
	}
	catalog, err := crystalcontract.NewCatalog(premiumCatalog.crystalCubeIDs)
	if err != nil {
		return nil, err
	}
	return crystalcontract.NewOwner(repositories, catalog)
}

func (s *Service) handleCurrentCrystalContractUpdate(session *gameSession, body []byte) error {
	if len(body) != currentCrystalContractUpdateBodySize {
		s.logGameEvent(session, "game-crystal-contract-selection-malformed",
			"msg_id", uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo),
			"body_len", len(body),
			"expected_body_len", currentCrystalContractUpdateBodySize)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo), 1)
	}
	sourceFlag := body[0]
	selection := crystalSelectionFromWire(body[1])
	if selection < crystalcontract.SelectionNone || selection >= crystalcontract.SelectionCount {
		s.logGameEvent(session, "game-crystal-contract-selection-blocked",
			"msg_id", uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo),
			"source_flag", sourceFlag,
			"selection_wire", body[1],
			"reason", crystalcontract.ErrSelectionInvalid)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo), 1)
	}
	owner, err := s.currentCrystalContractOwner()
	if err != nil {
		s.logGameEvent(session, "game-crystal-contract-selection-blocked",
			"msg_id", uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo),
			"selection", selection,
			"reason", "owner_unavailable",
			"error", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo), 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	state, err := owner.Select(
		ctx,
		s.accountIDForSession(session),
		strconv.Itoa(int(session.selectedCharacterID)),
		selection,
		s.gameplayNow().UTC(),
	)
	if err != nil {
		s.logGameEvent(session, "game-crystal-contract-selection-blocked",
			"msg_id", uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo),
			"selection", selection,
			"source_flag", sourceFlag,
			"reason", "domain_rejected",
			"error", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo), 1)
	}
	s.logGameEvent(session, "game-crystal-contract-selection-committed",
		"msg_id", uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo),
		"state_msg_id", currentCrystalContractStateMsgID,
		"selection", state.Selection,
		"cube_item_id", state.CubeItemID,
		"active", state.Active,
		"source_flag", sourceFlag,
		"state_source", "runtime_pvf_cube_catalog_account_crystal_warehouse_and_character_stats")
	if err := s.sendGameUpperSuccess(
		session,
		uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo),
		[]byte{sourceFlag, crystalSelectionToWire(state.Selection)},
	); err != nil {
		return err
	}
	return s.sendCurrentCrystalContractState(session, "op535_after_selection_commit")
}

func (s *Service) sendCurrentCrystalContractState(session *gameSession, source string) error {
	owner, err := s.currentCrystalContractOwner()
	if err != nil {
		return fmt.Errorf("build crystal contract state owner: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	state, err := owner.State(
		ctx,
		s.accountIDForSession(session),
		strconv.Itoa(int(session.selectedCharacterID)),
		s.gameplayNow().UTC(),
	)
	if err != nil {
		return fmt.Errorf("load crystal contract state: %w", err)
	}
	body := []byte{0, crystalSelectionToWire(state.Selection)}
	s.logGameEvent(session, "game-crystal-contract-state-send",
		"source", source,
		"msg_id", currentCrystalContractStateMsgID,
		"classification", 0,
		"active", state.Active,
		"selection", state.Selection,
		"cube_item_id", state.CubeItemID,
		"body_len", len(body),
		"state_source", "account_contract_expiry_character_selection_and_account_crystal_warehouse")
	// NoPack registers op898 through sub_225C940 as a class0 raw notification.
	// sub_1E7FAD0 reads the two bytes directly, unlike the class1 op535/op338
	// response handlers that receive a separately decoded success flag.
	return s.sendGameUpperRawClass(
		session,
		currentCrystalContractStateMsgID,
		body,
		0,
	)
}

// sendCurrentCrystalContractStateOnce replays the durable selection once for
// each town-scene UI lifecycle. The actor-bound inventory bootstrap owns the
// cold-login send after every native container exists and before full mode1 or
// op24 can expose the town UI. A later op36 remains a fallback for routes that
// do not consume that bootstrap, without applying the same state twice.
func (s *Service) sendCurrentCrystalContractStateOnce(
	session *gameSession,
	source string,
) error {
	if session == nil {
		return nil
	}
	session.crystalContractMu.Lock()
	if session.crystalContractTownUIReadyStateSent {
		session.crystalContractMu.Unlock()
		s.logGameEvent(session, "game-crystal-contract-town-ui-ready-state-skipped",
			"source", source,
			"reason", "already_sent_for_current_town_scene")
		return nil
	}
	session.crystalContractTownUIReadyStateSent = true
	session.crystalContractMu.Unlock()

	if err := s.sendCurrentCrystalContractState(session, source); err != nil {
		session.crystalContractMu.Lock()
		session.crystalContractTownUIReadyStateSent = false
		session.crystalContractMu.Unlock()
		return err
	}
	return nil
}

func (s *Service) sendCurrentCrystalContractTownUIReadyState(session *gameSession) error {
	return s.sendCurrentCrystalContractStateOnce(
		session,
		"current_exe_op36_after_town_area_ui_ready",
	)
}

func (s *Service) handleCurrentCrystalContractCubeUse(session *gameSession, body []byte) error {
	if len(body) != currentCrystalContractConsumeBodySize {
		s.logGameEvent(session, "game-crystal-contract-cube-use-malformed",
			"msg_id", uint16(dnfenum.CmdPacketUseLimitCube),
			"body_len", len(body),
			"expected_body_len", currentCrystalContractConsumeBodySize)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseLimitCube), 1)
	}
	slotIndex := binary.LittleEndian.Uint16(body[0:2])
	itemID := int64(binary.LittleEndian.Uint32(body[2:6]))
	clientStackCount := binary.LittleEndian.Uint16(body[6:8])
	if !currentCrystalContractDungeonOwnedBySession(session) {
		s.logGameEvent(session, "game-crystal-contract-cube-use-blocked",
			"msg_id", uint16(dnfenum.CmdPacketUseLimitCube),
			"slot_index", slotIndex,
			"item_id", itemID,
			"client_stack_count", clientStackCount,
			"reason", "no_current_owned_dungeon_runtime")
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseLimitCube), 1)
	}
	owner, err := s.currentCrystalContractOwner()
	if err != nil {
		s.logGameEvent(session, "game-crystal-contract-cube-use-blocked",
			"msg_id", uint16(dnfenum.CmdPacketUseLimitCube),
			"slot_index", slotIndex,
			"item_id", itemID,
			"reason", "owner_unavailable",
			"error", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseLimitCube), 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := owner.Consume(
		ctx,
		s.accountIDForSession(session),
		strconv.Itoa(int(session.selectedCharacterID)),
		slotIndex,
		itemID,
		s.gameplayNow().UTC(),
	)
	if err != nil {
		s.logGameEvent(session, "game-crystal-contract-cube-use-blocked",
			"msg_id", uint16(dnfenum.CmdPacketUseLimitCube),
			"slot_index", slotIndex,
			"item_id", itemID,
			"client_stack_count", clientStackCount,
			"reason", "domain_rejected",
			"error", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseLimitCube), 1)
	}
	var response packetWriter
	// NoPack sub_1D10130 consumes success + u32 item ID + u16 consumed count
	// + u32 remaining stack count + u8 refresh flag.
	response.writeUint32(uint32(result.ItemID))
	response.writeUint16(uint16(result.Consumed))
	response.writeUint32(uint32(result.Remaining))
	response.writeByte(1)
	s.logGameEvent(session, "game-crystal-contract-cube-use-committed",
		"msg_id", uint16(dnfenum.CmdPacketUseLimitCube),
		"slot_index", result.SlotIndex,
		"item_id", result.ItemID,
		"client_stack_count", clientStackCount,
		"consumed", result.Consumed,
		"remaining", result.Remaining,
		"selection_after", result.SelectionAfter,
		"state_source", "atomic_account_crystal_warehouse_and_character_owner")
	if err := s.sendGameUpperSuccess(
		session,
		uint16(dnfenum.CmdPacketUseLimitCube),
		response.bytes(),
	); err != nil {
		return err
	}
	if result.SelectionAfter == crystalcontract.SelectionNone {
		return s.sendCurrentCrystalContractState(session, "op338_after_last_selected_cube")
	}
	return nil
}

func currentCrystalContractDungeonOwnedBySession(session *gameSession) bool {
	if session == nil {
		return false
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	return dungeonRuntimeOwnsCharacter(session.dungeon.runtime, session.selectedCharacterID)
}

func crystalSelectionFromWire(value byte) int8 {
	if value == 0xff {
		return crystalcontract.SelectionNone
	}
	return int8(value)
}

func crystalSelectionToWire(selection int8) byte {
	if selection == crystalcontract.SelectionNone {
		return 0xff
	}
	return byte(selection)
}
