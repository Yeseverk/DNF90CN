package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnflimitedcube "longheng.io/server/internal/modules/dnf/limitedcube"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentLimitedCubeConsumeCount int64 = 1

// handleCurrentLimitedCubeOrCrystalContractCubeUse dispatches the shared
// current-client op338 by the source item's runtime-PVF behavior. Crystal
// contract cubes retain their existing dungeon-only handler unchanged.
func (s *Service) handleCurrentLimitedCubeOrCrystalContractCubeUse(session *gameSession, body []byte) error {
	request, policy, recognized, err := s.currentLimitedCubePolicyForRequest(session, body)
	if !recognized {
		if err != nil {
			s.logGameEvent(session, "game-limited-cube-pvf-unavailable-fallback",
				"msg_id", uint16(dnfenum.CmdPacketUseLimitCube),
				"body_len", len(body),
				"error", err)
		}
		return s.handleCurrentCrystalContractCubeUse(session, body)
	}
	if err != nil {
		s.logGameEvent(session, "game-limited-cube-use-blocked",
			"msg_id", uint16(dnfenum.CmdPacketUseLimitCube),
			"reason", "runtime_pvf_invalid",
			"error", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseLimitCube), 1)
	}
	return s.handleCurrentLimitedCubeUse(session, request, policy)
}

// currentLimitedCubeUseRequest matches the PVF [limited cube] client action:
// target bead slot, target bead ID, then the originating ticket slot. The
// same op338 shape is also used by crystal contracts, whose last u16 has a
// different meaning, so recognition must prove the ticket from inventory.
type currentLimitedCubeUseRequest struct {
	TargetSlot   int16
	TargetItemID int64
	TicketSlot   int16
}

func (s *Service) currentLimitedCubePolicyForRequest(session *gameSession, body []byte) (currentLimitedCubeUseRequest, dnflimitedcube.Policy, bool, error) {
	if len(body) != currentCrystalContractConsumeBodySize {
		return currentLimitedCubeUseRequest{}, dnflimitedcube.Policy{}, false, nil
	}
	request := currentLimitedCubeUseRequest{
		TargetSlot:   int16(binary.LittleEndian.Uint16(body[0:2])),
		TargetItemID: int64(binary.LittleEndian.Uint32(body[2:6])),
		TicketSlot:   int16(binary.LittleEndian.Uint16(body[6:8])),
	}
	if request.TargetSlot < 0 || request.TicketSlot < 0 || request.TargetSlot == request.TicketSlot || request.TargetItemID <= 0 {
		return request, dnflimitedcube.Policy{}, false, nil
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return request, dnflimitedcube.Policy{}, false, err
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Inventory == nil || session == nil || session.selectedCharacterID == 0 {
		return request, dnflimitedcube.Policy{}, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	inventory, found, err := repositories.Inventory.Load(ctx, characterID)
	if err != nil || !found {
		return request, dnflimitedcube.Policy{}, false, err
	}
	ticket, found := inventory.Slots[currentLimitedCubeMainSlotKey(request.TicketSlot)]
	if !found || ticket.ItemID <= 0 || ticket.Count <= 0 {
		return request, dnflimitedcube.Policy{}, false, nil
	}
	policy, recognized, err := resolveCurrentLimitedCubePolicy(catalog, ticket.ItemID)
	if err != nil || !recognized || !currentLimitedCubePolicyAllowsTarget(policy, request.TargetItemID) {
		return request, policy, false, err
	}
	return request, policy, true, nil
}

func currentLimitedCubeMainSlotKey(slot int16) string {
	return "0:" + strconv.FormatInt(int64(slot), 10)
}

func currentLimitedCubePolicyAllowsTarget(policy dnflimitedcube.Policy, itemID int64) bool {
	for _, condition := range policy.Conditions {
		if condition.ItemID == itemID && condition.Count > 0 {
			return true
		}
	}
	return false
}

// resolveCurrentLimitedCubePolicy recognizes only the exact PVF action family
// used by the pet-bead change tickets. An ordinary or unresolved op338 source
// is deliberately left to the crystal-contract compatibility path.
func resolveCurrentLimitedCubePolicy(catalog *pvfDungeonDropCatalog, ticketItemID int64) (dnflimitedcube.Policy, bool, error) {
	if catalog == nil || catalog.source == nil || ticketItemID <= 0 || ticketItemID > math.MaxUint32 {
		return dnflimitedcube.Policy{}, false, nil
	}
	definition, err := catalog.ResolveItem(uint32(ticketItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return dnflimitedcube.Policy{}, false, nil
		}
		return dnflimitedcube.Policy{}, false, err
	}
	if definition.Kind != dungeonDropItemStackable || !currentLimitedCubeTagEqual(definition.StackableType, "[upgrade limit cube]") {
		return dnflimitedcube.Policy{}, false, nil
	}
	document, err := parseDungeonCardPVFDocument(catalog.source, definition.PVFPath)
	if err != nil {
		return dnflimitedcube.Policy{}, true, fmt.Errorf("parse limited cube item=%d path=%s: %w", ticketItemID, definition.PVFPath, err)
	}
	actionType, actionFound := document.Text("action type")
	usablePlace, placeFound := document.Text("action usable place")
	if !actionFound || !currentLimitedCubeTagEqual(actionType, "[limited cube]") ||
		!placeFound || !currentLimitedCubeTagEqual(usablePlace, "[village]") {
		return dnflimitedcube.Policy{}, false, nil
	}
	conditions, err := currentLimitedCubeRequirements(document, "A condition item")
	if err != nil {
		return dnflimitedcube.Policy{}, true, fmt.Errorf("limited cube item=%d A condition: %w", ticketItemID, err)
	}
	materials, err := currentLimitedCubeRequirements(document, "B condition item")
	if err != nil {
		return dnflimitedcube.Policy{}, true, fmt.Errorf("limited cube item=%d B condition: %w", ticketItemID, err)
	}
	results, err := currentLimitedCubeResults(catalog, document)
	if err != nil {
		return dnflimitedcube.Policy{}, true, fmt.Errorf("limited cube item=%d results: %w", ticketItemID, err)
	}
	return dnflimitedcube.Policy{
		TicketItemID: ticketItemID,
		Conditions:   conditions,
		Materials:    materials,
		Results:      results,
	}, true, nil
}

func currentLimitedCubeRequirements(document *dnfpvf.Document, section string) ([]dnflimitedcube.Requirement, error) {
	values := document.Ints(section)
	if len(values) == 0 || len(values)%2 != 0 {
		return nil, fmt.Errorf("section %q has %d values, want nonempty item/count pairs", section, len(values))
	}
	requirements := make([]dnflimitedcube.Requirement, 0, len(values)/2)
	for offset := 0; offset < len(values); offset += 2 {
		itemID, count := values[offset], values[offset+1]
		if itemID <= 0 || itemID > math.MaxUint32 || count <= 0 {
			return nil, fmt.Errorf("section %q item=%d count=%d", section, itemID, count)
		}
		requirements = append(requirements, dnflimitedcube.Requirement{ItemID: itemID, Count: count})
	}
	return requirements, nil
}

func currentLimitedCubeResults(catalog *pvfDungeonDropCatalog, document *dnfpvf.Document) ([]dnflimitedcube.WeightedResult, error) {
	values := document.Ints("result item")
	if len(values) == 0 || len(values)%3 != 0 {
		return nil, fmt.Errorf("section %q has %d values, want nonempty item/count/weight triples", "result item", len(values))
	}
	results := make([]dnflimitedcube.WeightedResult, 0, len(values)/3)
	for offset := 0; offset < len(values); offset += 3 {
		itemID, count, weight := values[offset], values[offset+1], values[offset+2]
		if itemID <= 0 || itemID > math.MaxUint32 || count <= 0 || weight <= 0 {
			return nil, fmt.Errorf("item=%d count=%d weight=%d", itemID, count, weight)
		}
		definition, err := catalog.ResolveItem(uint32(itemID))
		if err != nil {
			return nil, fmt.Errorf("resolve item=%d: %w", itemID, err)
		}
		stack, err := buildCurrentLimitedCubeResultStack(definition, count)
		if err != nil {
			return nil, fmt.Errorf("build item=%d: %w", itemID, err)
		}
		results = append(results, dnflimitedcube.WeightedResult{Stack: stack, Weight: weight})
	}
	return results, nil
}

func buildCurrentLimitedCubeResultStack(definition dungeonDropItemDefinition, count int64) (dnfrepo.ItemStack, error) {
	if definition.Kind != dungeonDropItemStackable || definition.ItemID == 0 || count <= 0 {
		return dnfrepo.ItemStack{}, fmt.Errorf("result must be a positive stackable item")
	}
	extra := map[string]string{
		"source":         "limited_cube_result",
		"item_kind":      string(definition.Kind),
		"pvf_path":       definition.PVFPath,
		"stackable_type": definition.StackableType,
	}
	if definition.StackLimit > 0 {
		extra["stack_limit"] = strconv.FormatInt(definition.StackLimit, 10)
	}
	stack := dnfrepo.ItemStack{ItemID: int64(definition.ItemID), Count: count, Extra: extra}
	if !definition.ExpirationDate.IsZero() {
		stack, _ = applyCurrentPVFItemExpiration(stack, definition)
	}
	return stack, nil
}

func currentLimitedCubeTagEqual(value, want string) bool {
	normalize := func(raw string) string {
		raw = strings.TrimSpace(strings.ReplaceAll(raw, "`", ""))
		return strings.ToLower(strings.Join(strings.Fields(raw), " "))
	}
	return normalize(value) == normalize(want)
}

func (s *Service) handleCurrentLimitedCubeUse(session *gameSession, request currentLimitedCubeUseRequest, policy dnflimitedcube.Policy) error {
	if currentCrystalContractDungeonOwnedBySession(session) {
		s.logGameEvent(session, "game-limited-cube-use-blocked",
			"msg_id", uint16(dnfenum.CmdPacketUseLimitCube),
			"ticket_slot", request.TicketSlot,
			"target_slot", request.TargetSlot,
			"target_item", request.TargetItemID,
			"reason", "pvf_action_usable_place_village")
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseLimitCube), 1)
	}
	repositories, ok := s.repositoryGroup()
	if !ok {
		s.logGameEvent(session, "game-limited-cube-use-blocked", "msg_id", uint16(dnfenum.CmdPacketUseLimitCube), "reason", "repositories_unavailable")
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseLimitCube), 1)
	}
	owner, err := dnflimitedcube.NewOwner(repositories)
	if err != nil {
		s.logGameEvent(session, "game-limited-cube-use-blocked", "msg_id", uint16(dnfenum.CmdPacketUseLimitCube), "reason", "owner_unavailable", "error", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseLimitCube), 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := owner.Use(ctx, dnflimitedcube.Command{
		AccountID:    s.accountIDForSession(session),
		CharacterID:  strconv.Itoa(int(session.selectedCharacterID)),
		TicketSlot:   request.TicketSlot,
		TicketItemID: policy.TicketItemID,
		TargetSlot:   request.TargetSlot,
		TargetItemID: request.TargetItemID,
	}, policy)
	if err != nil {
		s.logGameEvent(session, "game-limited-cube-use-blocked",
			"msg_id", uint16(dnfenum.CmdPacketUseLimitCube),
			"ticket_slot", request.TicketSlot,
			"target_slot", request.TargetSlot,
			"target_item", request.TargetItemID,
			"reason", "domain_rejected",
			"error", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketUseLimitCube), 1)
	}

	var response packetWriter
	// The limited-cube success handler resolves this item ID for its result
	// prompt. Crystal-contract op338 uses its consumed source ID instead.
	response.writeUint32(uint32(result.ResultItemID))
	response.writeUint16(uint16(currentLimitedCubeConsumeCount))
	response.writeUint32(uint32(result.TicketRemaining))
	response.writeByte(1)
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketUseLimitCube), response.bytes()); err != nil {
		return err
	}
	if err := s.sendCurrentLimitedCubeTargetReplacementClear(session, result.InputSlot); err != nil {
		return err
	}
	refreshes := make([]alignedcmd.ItemSlotRefresh, 0, len(result.ChangedSlots)+len(result.AccountChangedSlots))
	for _, changedSlot := range result.ChangedSlots {
		refreshes = append(refreshes, alignedcmd.ItemSlotRefresh{ListType: dnfrepo.MainInventoryListType, SlotIndex: changedSlot})
	}
	for _, changedSlot := range result.AccountChangedSlots {
		refreshes = append(refreshes, alignedcmd.ItemSlotRefresh{ListType: dnfrepo.MainInventoryListType, SlotIndex: changedSlot})
	}
	if err := s.sendSelectedIncrementalItemSlotRefreshes(session, "limited_cube_use", refreshes); err != nil {
		return err
	}
	s.logGameEvent(session, "game-limited-cube-use-committed",
		"msg_id", uint16(dnfenum.CmdPacketUseLimitCube),
		"ticket_slot", result.TicketSlot,
		"ticket_item", result.TicketItemID,
		"ticket_remaining", result.TicketRemaining,
		"input_slot", result.InputSlot,
		"input_item", result.InputItemID,
		"result_item", result.ResultItemID,
		"character_material_and_item_refresh_slots", fmt.Sprint(result.ChangedSlots),
		"account_material_refresh_slots", fmt.Sprint(result.AccountChangedSlots),
		"state_source", "runtime_pvf_limited_cube_policy_atomic_account_and_character_inventory")
	return nil
}

// sendCurrentLimitedCubeTargetReplacementClear makes the current client
// discard the old item object before the repository-backed result row arrives.
// An op14 row that changes only the template ID keeps the old bead tooltip and
// lock state cached in the already-instantiated object.
func (s *Service) sendCurrentLimitedCubeTargetReplacementClear(session *gameSession, targetSlot int16) error {
	var removed currentItemListEntry
	removed.patchCore(targetSlot, math.MaxUint32, 0)
	body := buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, []currentItemListEntry{removed})
	s.logPacketEvent("game-limited-cube-target-replacement-clear-send",
		"conn_id", session.connID,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"classification", 0,
		"list_type", dnfrepo.MainInventoryListType,
		"slot", targetSlot,
		"sequence", "op338_ack_then_target_delete_then_repository_backed_result_op14")
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0)
}
