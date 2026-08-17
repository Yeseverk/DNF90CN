package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// The current EXE sends op237 (ENUM_CMDPACKET_TELEPORT) when a teleport
// potion is used. Body layout (86JP TownHandler.Handle_ENUM_CMDPACKET_TELEPORT
// and the current-EXE S2C op237 reader contract):
//
//	i16 potionSlot, i32 itemCode, u8 unused, u8 townID
//
// On success the server teleports the character to the town's PVF gate area
// through the proven op36 teleport-array commit chain, consumes one potion,
// refreshes the bag, and returns the op237 ACK {01, i16 slot, i32 code}. The
// failure ACK {00, 23} shows the client's text 36288 (current-EXE op237 error
// branch).
type currentTeleportPotionRequest struct {
	PotionSlotIndex int16
	ItemCode        int32
	TownID          byte
}

const currentTeleportPotionErrorUnsupported byte = 23

func parseCurrentTeleportPotionRequest(body []byte) (currentTeleportPotionRequest, error) {
	if len(body) < 8 {
		return currentTeleportPotionRequest{}, fmt.Errorf("current teleport potion body length %d, want >= 8", len(body))
	}
	return currentTeleportPotionRequest{
		PotionSlotIndex: int16(binary.LittleEndian.Uint16(body[0:2])),
		ItemCode:        int32(binary.LittleEndian.Uint32(body[2:6])),
		TownID:          body[7],
	}, nil
}

func (s *Service) handleTeleportPotion(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	request, err := parseCurrentTeleportPotionRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-town-teleport-potion-blocked",
			"body_len", len(body),
			"reason", "current_exe_op237_writer_boundary_mismatch",
			"error", err)
		return nil
	}
	if session.selectedCharacterID == 0 {
		return s.sendCurrentTeleportPotionFailure(session, request, "selected_character_missing")
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil || catalog == nil {
		return s.sendCurrentTeleportPotionFailure(session, request, "runtime_pvf_catalog_unavailable")
	}
	if !currentTeleportPotionIsValid(catalog, request.ItemCode) {
		return s.sendCurrentTeleportPotionFailure(session, request, "item_is_not_a_pvf_teleport_potion")
	}
	gateArea, found := s.townGateArea(int64(request.TownID))
	if !found || gateArea.Gate == nil {
		return s.sendCurrentTeleportPotionFailure(session, request, "target_town_gate_area_missing_from_runtime_pvf")
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.CharacterItems == nil || repositories.Character == nil {
		return s.sendCurrentTeleportPotionFailure(session, request, "character_item_repository_unavailable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return err
	}
	if !found || character.Stats == nil {
		return s.sendCurrentTeleportPotionFailure(session, request, "selected_character_location_missing")
	}
	currentTown, ok := character.Stats["town_id"]
	if !ok || currentTown < 0 {
		return s.sendCurrentTeleportPotionFailure(session, request, "selected_character_town_id_missing")
	}
	potionKey, err := currentTeleportPotionSlot(ctx, repositories, characterID, request)
	if err != nil {
		return s.sendCurrentTeleportPotionFailure(session, request, "teleport_potion_missing_from_main_inventory")
	}

	// Reuse the proven op36 teleport-array commit chain: PVF gate area + gate
	// coordinates with the source-town teleport shape, which emits the current
	// op24 transition, persists the new town position, and unlocks PVF
	// need-quests.
	if err := s.handleTownSetUserArea(session, buildCurrentTeleportPotionMoveBody(request.TownID, gateArea.ID, gateArea.Gate.X, gateArea.Gate.Y, currentTown)); err != nil {
		return err
	}
	moved, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return err
	}
	if !found || moved.Stats["town_id"] != int64(request.TownID) || moved.Stats["area_id"] != gateArea.ID {
		return s.sendCurrentTeleportPotionFailure(session, request, "town_transition_not_committed")
	}
	if err := consumeCurrentTeleportPotion(ctx, repositories, characterID, potionKey); err != nil {
		return err
	}
	if err := s.sendSelectedCurrentContainerListsWithRefresh(session, "current_exe_op237_teleport_potion_consumed_after_op24", true); err != nil {
		return err
	}
	s.logGameEvent(session, "game-town-teleport-potion-committed",
		"char_id", session.selectedCharacterID,
		"item_code", request.ItemCode,
		"potion_slot", potionKey,
		"town_id", request.TownID,
		"area_id", gateArea.ID,
		"position_x", gateArea.Gate.X,
		"position_y", gateArea.Gate.Y,
		"map_path", gateArea.MapPath)
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketTeleport), buildCurrentTeleportPotionSuccessAck(request), dnfproto.DefaultChannelClassification)
}

func currentTeleportPotionIsValid(catalog *pvfDungeonDropCatalog, itemCode int32) bool {
	if catalog == nil || itemCode <= 0 {
		return false
	}
	definition, err := catalog.ResolveItem(uint32(itemCode))
	if err != nil || definition.Kind != dungeonDropItemStackable {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(definition.StackableType), "[teleport potion]")
}

// currentTeleportPotionSlot verifies the claimed slot first, then falls back
// to an id-based scan, matching the 86JP slot-mismatch fallback.
func currentTeleportPotionSlot(ctx context.Context, repositories dnfrepo.Group, characterID string, request currentTeleportPotionRequest) (string, error) {
	var matched string
	err := repositories.CharacterItems.WithinCharacterItems(ctx, characterID, func(inventory dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
		record, found, err := inventory.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("inventory record missing: %s", characterID)
		}
		claimed := fmt.Sprintf("0:%d", request.PotionSlotIndex)
		if stack, ok := record.Slots[claimed]; ok && stack.Count > 0 && stack.ItemID == int64(request.ItemCode) {
			matched = claimed
			return nil
		}
		keys := make([]string, 0, len(record.Slots))
		for key, stack := range record.Slots {
			if stack.ItemID == int64(request.ItemCode) && stack.Count > 0 {
				keys = append(keys, key)
			}
		}
		if len(keys) == 0 {
			return fmt.Errorf("teleport potion %d not found", request.ItemCode)
		}
		for _, key := range keys {
			if matched == "" || key < matched {
				matched = key
			}
		}
		return nil
	})
	return matched, err
}

func consumeCurrentTeleportPotion(ctx context.Context, repositories dnfrepo.Group, characterID string, potionKey string) error {
	return repositories.CharacterItems.WithinCharacterItems(ctx, characterID, func(inventory dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
		record, found, err := inventory.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("inventory record missing: %s", characterID)
		}
		record = dnfrepo.CloneInventory(record)
		stack, ok := record.Slots[potionKey]
		if !ok || stack.Count <= 0 {
			return fmt.Errorf("teleport potion slot %s empty", potionKey)
		}
		stack.Count--
		if stack.Count <= 0 {
			delete(record.Slots, potionKey)
		} else {
			if stack.Extra == nil {
				stack.Extra = make(map[string]string, 4)
			}
			stack.Extra["amount_or_count"] = strconv.FormatInt(stack.Count, 10)
			stack.Extra["amount"] = strconv.FormatInt(stack.Count, 10)
			if len(stack.RawEntry) == currentItemListEntryWireSize {
				stack.RawEntry = append([]byte(nil), stack.RawEntry...)
				value := uint32(stack.Count)
				stack.RawEntry[0x06] = byte(value)
				stack.RawEntry[0x07] = byte(value >> 8)
				stack.RawEntry[0x08] = byte(value >> 16)
				stack.RawEntry[0x09] = byte(value >> 24)
			}
			record.Slots[potionKey] = stack
		}
		record.UpdatedAt = time.Now().UTC()
		return dnfrepo.SaveInventoryFields(ctx, inventory, record, dnfrepo.InventoryFieldSlots)
	})
}

func buildCurrentTeleportPotionMoveBody(townID byte, areaID int64, positionX int64, positionY int64, sourceTownID int64) []byte {
	body := make([]byte, currentTownSetUserAreaBodySize)
	body[0] = townID
	body[1] = byte(areaID)
	binary.LittleEndian.PutUint16(body[2:4], uint16(positionX))
	binary.LittleEndian.PutUint16(body[4:6], uint16(positionY))
	body[6] = 0
	// opaqueA = source town: required by the current-EXE teleport-array shape
	// (LooksLikeTeleportArraySelection), which admits PVF-visible quest-gated
	// and cross-town targets and unlocks need-quests on commit.
	binary.LittleEndian.PutUint16(body[7:9], uint16(sourceTownID))
	body[15] = 5
	return body
}

func buildCurrentTeleportPotionSuccessAck(request currentTeleportPotionRequest) []byte {
	return []byte{
		1,
		byte(request.PotionSlotIndex), byte(request.PotionSlotIndex >> 8),
		byte(request.ItemCode), byte(request.ItemCode >> 8), byte(request.ItemCode >> 16), byte(request.ItemCode >> 24),
	}
}

func (s *Service) sendCurrentTeleportPotionFailure(session *gameSession, request currentTeleportPotionRequest, reason string) error {
	s.logGameEvent(session, "game-town-teleport-potion-blocked",
		"char_id", session.selectedCharacterID,
		"item_code", request.ItemCode,
		"town_id", request.TownID,
		"reason", reason)
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketTeleport), []byte{0, currentTeleportPotionErrorUnsupported}, dnfproto.DefaultChannelClassification)
}
