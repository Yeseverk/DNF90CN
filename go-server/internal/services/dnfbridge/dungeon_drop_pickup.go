package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfdungeon "longheng.io/server/internal/modules/dnf/dungeon"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	// Opcode 39 is directional: C2S op39 reports monster death, while current
	// class0/S2C op39 is the ordinary scene-item pickup result.
	currentDungeonPickupResultOpcode   uint16 = 39
	currentDungeonPickupResultSize            = 19
	currentDungeonPickupQuickSlotStart        = int16(3)
	currentDungeonPickupQuickSlotEnd          = int16(8)
)

var (
	errDungeonPickupRuntimeMissing     = errors.New("dnf dungeon pickup runtime is missing")
	errDungeonPickupTransactionMissing = errors.New("dnf dungeon pickup inventory transaction is missing")
	errDungeonPickupInventoryMissing   = errors.New("dnf dungeon pickup inventory record is missing")
	errDungeonPickupInventoryFull      = errors.New("dnf dungeon pickup inventory category is full")
	errDungeonPickupItemInvalid        = errors.New("dnf dungeon pickup item is invalid")
	errDungeonPickupStackLimit         = errors.New("dnf dungeon pickup exceeds real PVF stack limit")
	errDungeonPickupResponseInvalid    = errors.New("dnf dungeon pickup response values are invalid")
	errDungeonPickupDropStateInvalid   = errors.New("dnf dungeon pickup drop state is invalid")
)

func buildCurrentDungeonNormalPickupResultBody(
	dropObjectKey uint32,
	pickerActorObjectKey uint16,
	destinationSlot uint16,
) ([]byte, error) {
	if dropObjectKey == 0 || pickerActorObjectKey == 0 || destinationSlot == 0 {
		return nil, fmt.Errorf(
			"%w: drop=%d picker=%d destination=%d",
			errDungeonPickupResponseInvalid,
			dropObjectKey,
			pickerActorObjectKey,
			destinationSlot,
		)
	}
	var writer packetWriter
	writer.writeUint32(dropObjectKey)
	writer.writeUint16(pickerActorObjectKey)
	writer.writeBytes(make([]byte, 8)) // read by current EXE; semantics unproved, default constructor state
	writer.writeUint16(pickerActorObjectKey)
	writer.writeUint16(destinationSlot)
	writer.writeByte(0) // read by current EXE; not consumed by the visible permission/UI path
	body := writer.bytes()
	if len(body) != currentDungeonPickupResultSize {
		return nil, fmt.Errorf("%w: body_len=%d", errDungeonPickupResponseInvalid, len(body))
	}
	return body, nil
}

func buildCurrentDungeonGoldPickupResultBody(
	dropObjectKey uint32,
	pickerActorObjectKey uint16,
) ([]byte, error) {
	if dropObjectKey == 0 || pickerActorObjectKey == 0 {
		return nil, fmt.Errorf(
			"%w: drop=%d picker=%d",
			errDungeonPickupResponseInvalid,
			dropObjectKey,
			pickerActorObjectKey,
		)
	}
	var writer packetWriter
	writer.writeUint32(dropObjectKey)
	writer.writeUint16(pickerActorObjectKey)
	writer.writeByte(1) // current visible pickup-effect gate, matching 86JP gold branch semantics
	writer.writeBytes(make([]byte, 7))
	writer.writeUint16(pickerActorObjectKey)
	writer.writeUint16(0) // gold has no destination inventory slot
	writer.writeByte(1)   // gold pickup/remove effect flag
	body := writer.bytes()
	if len(body) != currentDungeonPickupResultSize {
		return nil, fmt.Errorf("%w: body_len=%d", errDungeonPickupResponseInvalid, len(body))
	}
	return body, nil
}

func (s *Service) handleCurrentDungeonPickup(session *gameSession, body []byte) error {
	request, err := dungeoncmd.DecodeGetItemRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-pickup-blocked",
			"body_len", len(body),
			"reason", "current_exe_op43_ordinary_request_malformed",
			"error", err)
		return nil
	}
	if request.FixedZero != 0 {
		s.logGameEvent(session, "game-dungeon-pickup-blocked",
			"drop_object_key", request.DropObjectKey,
			"fixed_byte", request.FixedZero,
			"reason", "current_exe_op43_fixed_zero_mismatch")
		return nil
	}
	if session == nil {
		return nil
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil || runtime.Room == nil || runtime.DropOwner == nil {
		s.logGameEvent(session, "game-dungeon-pickup-blocked",
			"drop_object_key", request.DropObjectKey,
			"reason", "dungeon_drop_runtime_missing",
			"error", errDungeonPickupRuntimeMissing)
		return nil
	}
	scene, ok := runtime.Session.Scene()
	if !ok {
		s.logGameEvent(session, "game-dungeon-pickup-blocked",
			"drop_object_key", request.DropObjectKey,
			"reason", "dungeon_scene_missing",
			"error", errDungeonPickupRuntimeMissing)
		return nil
	}
	pickerActorObjectKey := currentSceneActorObjectKey(session.selectedCharacterID)
	drop, err := runtime.DropOwner.owned(
		request.DropObjectKey,
		runtimeDungeonRoomKeyFromScene(scene),
		pickerActorObjectKey,
	)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-pickup-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"drop_object_key", request.DropObjectKey,
			"picker_actor_object_key", pickerActorObjectKey,
			"reason", "runtime_drop_owner_rejected",
			"error", err)
		return nil
	}
	if drop.Status == runtimeDungeonDropConsumed {
		if len(drop.PickupResponseBody) != currentDungeonPickupResultSize {
			s.logGameEvent(session, "game-dungeon-pickup-blocked",
				"drop_object_key", drop.ObjectKey,
				"reason", "consumed_drop_response_missing",
				"error", fmt.Errorf("%w: response_len=%d", errDungeonPickupDropStateInvalid, len(drop.PickupResponseBody)))
			return nil
		}
		s.logGameEvent(session, "game-dungeon-pickup-replayed",
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"drop_object_key", drop.ObjectKey,
			"item_id", drop.Item.ItemID,
			"gold_after", drop.GoldAfter,
			"destination_slot", drop.DestinationSlot,
			"request_context", request.ObjectContext,
			"request_player_x", request.PlayerX,
			"request_player_y", request.PlayerY,
			"request_drop_x", request.DropX,
			"request_drop_y", request.DropY,
			"response_opcode", currentDungeonPickupResultOpcode)
		if err := s.sendGameUpperRawClass(
			session,
			currentDungeonPickupResultOpcode,
			append([]byte(nil), drop.PickupResponseBody...),
			0,
		); err != nil {
			return err
		}
		if drop.isGold() && drop.GoldAfter > 0 {
			return s.sendGameUpperRawClass(
				session,
				uint16(dnfenum.CmdPacketWalkoutPartyMember),
				buildCurrentGoldStateBody(drop.GoldAfter),
				0,
			)
		}
		if len(drop.PickupItemUpdateBody) > 0 {
			return s.sendGameUpperRawClass(
				session,
				uint16(dnfenum.CmdPacketWalkoutPartyMember),
				append([]byte(nil), drop.PickupItemUpdateBody...),
				0,
			)
		}
		return nil
	}
	if drop.Status != runtimeDungeonDropAvailable {
		s.logGameEvent(session, "game-dungeon-pickup-blocked",
			"drop_object_key", drop.ObjectKey,
			"status", drop.Status,
			"reason", "runtime_drop_state_invalid",
			"error", errDungeonPickupDropStateInvalid)
		return nil
	}
	if drop.isGold() {
		repositories, ok := s.repositoryGroup()
		if !ok || repositories.CharacterAssets == nil {
			s.logGameEvent(session, "game-dungeon-pickup-blocked",
				"drop_object_key", drop.ObjectKey,
				"item_id", drop.Item.ItemID,
				"amount", drop.Amount,
				"reason", "character_gold_transaction_missing",
				"error", errDungeonPickupTransactionMissing)
			return nil
		}
		characterID := strconv.Itoa(int(session.selectedCharacterID))
		goldAfter, err := grantCurrentDungeonPickupGold(
			context.Background(),
			repositories.CharacterAssets,
			characterID,
			drop.Amount,
			time.Now().UTC(),
		)
		if err != nil {
			s.logGameEvent(session, "game-dungeon-pickup-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"drop_object_key", drop.ObjectKey,
				"item_id", drop.Item.ItemID,
				"amount", drop.Amount,
				"reason", "character_gold_transaction_failed_drop_remains_available",
				"error", err)
			return nil
		}
		responseBody, err := buildCurrentDungeonGoldPickupResultBody(drop.ObjectKey, pickerActorObjectKey)
		if err != nil {
			drop.Status = runtimeDungeonDropConsumed
			drop.GoldAfter = goldAfter
			s.logGameEvent(session, "game-dungeon-pickup-response-blocked-after-commit",
				"drop_object_key", drop.ObjectKey,
				"item_id", drop.Item.ItemID,
				"amount", drop.Amount,
				"gold_after", goldAfter,
				"reason", "gold_pickup_response_invariant_failed_drop_consumed_to_prevent_duplicate_grant",
				"error", err)
			return nil
		}
		drop.Status = runtimeDungeonDropConsumed
		drop.GoldAfter = goldAfter
		drop.PickupResponseBody = append([]byte(nil), responseBody...)
		s.logGameEvent(session, "game-dungeon-pickup-accepted",
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"drop_object_key", drop.ObjectKey,
			"scene_slot", drop.SceneSlot,
			"item_id", drop.Item.ItemID,
			"amount", drop.Amount,
			"gold_after", goldAfter,
			"picker_actor_object_key", pickerActorObjectKey,
			"request_context", request.ObjectContext,
			"request_player_x", request.PlayerX,
			"request_player_y", request.PlayerY,
			"request_token0", request.Token0,
			"request_drop_x", request.DropX,
			"request_drop_y", request.DropY,
			"request_token1", request.Token1,
			"request_token2", request.Token2,
			"response_opcode", currentDungeonPickupResultOpcode,
			"response_body_len", len(responseBody),
			"asset_owner", "character_assets_uow_gold")
		if err := s.sendGameUpperRawClass(session, currentDungeonPickupResultOpcode, responseBody, 0); err != nil {
			return err
		}
		return s.sendGameUpperRawClass(
			session,
			uint16(dnfenum.CmdPacketWalkoutPartyMember),
			buildCurrentGoldStateBody(goldAfter),
			0,
		)
	}
	repositories, ok := s.repositoryGroup()
	if drop.DiscardOrigin != nil {
		if !ok || repositories.AccountAssets == nil {
			s.logGameEvent(session, "game-dungeon-pickup-blocked",
				"drop_object_key", drop.ObjectKey,
				"item_id", drop.Item.ItemID,
				"reason", "discard_origin_account_assets_transaction_missing",
				"error", errDungeonPickupTransactionMissing)
			return nil
		}
		characterID := strconv.Itoa(int(session.selectedCharacterID))
		destinationSlot, itemUpdate, err := restoreCurrentDungeonDiscardedItem(
			context.Background(),
			repositories.AccountAssets,
			strings.TrimSpace(s.accountIDForSession(session)),
			characterID,
			*drop.DiscardOrigin,
			drop.Item,
			time.Now().UTC(),
		)
		if err != nil {
			s.logGameEvent(session, "game-dungeon-pickup-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"drop_object_key", drop.ObjectKey,
				"item_id", drop.Item.ItemID,
				"amount", drop.Amount,
				"source_slot", drop.DiscardOrigin.SourceSlot,
				"account_owned", drop.DiscardOrigin.AccountOwned,
				"reason", "discard_origin_restore_transaction_failed_drop_remains_available",
				"error", err)
			return nil
		}
		responseBody, err := buildCurrentDungeonNormalPickupResultBody(
			drop.ObjectKey,
			pickerActorObjectKey,
			destinationSlot,
		)
		if err != nil {
			drop.Status = runtimeDungeonDropConsumed
			drop.DestinationSlot = destinationSlot
			s.logGameEvent(session, "game-dungeon-pickup-response-blocked-after-commit",
				"drop_object_key", drop.ObjectKey,
				"item_id", drop.Item.ItemID,
				"destination_slot", destinationSlot,
				"reason", "discard_origin_pickup_response_invariant_failed_drop_consumed_to_prevent_duplicate_grant",
				"error", err)
			return nil
		}
		drop.Status = runtimeDungeonDropConsumed
		drop.DestinationSlot = destinationSlot
		drop.PickupResponseBody = append([]byte(nil), responseBody...)
		itemUpdateBody := buildCurrentItemUpdateBody(
			drop.DiscardOrigin.ListType,
			[]currentItemListEntry{itemUpdate},
		)
		drop.PickupItemUpdateBody = append([]byte(nil), itemUpdateBody...)
		s.logGameEvent(session, "game-dungeon-pickup-accepted",
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"drop_object_key", drop.ObjectKey,
			"scene_slot", drop.SceneSlot,
			"item_id", drop.Item.ItemID,
			"amount", drop.Amount,
			"destination_slot", destinationSlot,
			"picker_actor_object_key", pickerActorObjectKey,
			"request_context", request.ObjectContext,
			"request_player_x", request.PlayerX,
			"request_player_y", request.PlayerY,
			"request_drop_x", request.DropX,
			"request_drop_y", request.DropY,
			"response_opcode", currentDungeonPickupResultOpcode,
			"response_body_len", len(responseBody),
			"asset_owner", "discard_origin_account_assets_uow",
			"item_update_opcode", uint16(dnfenum.CmdPacketWalkoutPartyMember),
			"item_update_body_len", len(itemUpdateBody))
		if err := s.sendGameUpperRawClass(session, currentDungeonPickupResultOpcode, responseBody, 0); err != nil {
			return err
		}
		return s.sendGameUpperRawClass(
			session,
			uint16(dnfenum.CmdPacketWalkoutPartyMember),
			itemUpdateBody,
			0,
		)
	}
	if !ok || repositories.CharacterItems == nil {
		s.logGameEvent(session, "game-dungeon-pickup-blocked",
			"drop_object_key", drop.ObjectKey,
			"item_id", drop.Item.ItemID,
			"reason", "character_items_transaction_missing",
			"error", errDungeonPickupTransactionMissing)
		return nil
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	destinationSlot, itemUpdate, err := grantCurrentDungeonPickupItem(
		context.Background(),
		repositories.CharacterItems,
		characterID,
		drop.Item,
		drop.Amount,
		time.Now().UTC(),
		drop.QualitySeed,
	)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-pickup-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"drop_object_key", drop.ObjectKey,
			"item_id", drop.Item.ItemID,
			"amount", drop.Amount,
			"reason", "character_items_transaction_failed_drop_remains_available",
			"error", err)
		return nil
	}
	responseBody, err := buildCurrentDungeonNormalPickupResultBody(
		drop.ObjectKey,
		pickerActorObjectKey,
		destinationSlot,
	)
	if err != nil {
		// The allocator only returns proved positive uint16 category slots, so
		// this is an invariant failure after a committed transaction. Consume
		// the scene object to prevent a duplicate grant even if no ACK can be sent.
		drop.Status = runtimeDungeonDropConsumed
		drop.DestinationSlot = destinationSlot
		s.logGameEvent(session, "game-dungeon-pickup-response-blocked-after-commit",
			"drop_object_key", drop.ObjectKey,
			"item_id", drop.Item.ItemID,
			"destination_slot", destinationSlot,
			"reason", "pickup_response_invariant_failed_drop_consumed_to_prevent_duplicate_grant",
			"error", err)
		return nil
	}
	drop.Status = runtimeDungeonDropConsumed
	drop.DestinationSlot = destinationSlot
	drop.PickupResponseBody = append([]byte(nil), responseBody...)
	itemUpdateBody := buildCurrentItemUpdateBody(
		dnfrepo.MainInventoryListType,
		[]currentItemListEntry{itemUpdate},
	)
	drop.PickupItemUpdateBody = append([]byte(nil), itemUpdateBody...)
	s.logGameEvent(session, "game-dungeon-pickup-accepted",
		"dungeon_id", runtime.Dungeon.ID,
		"room", scene.Coordinate.String(),
		"map_id", scene.Map.Map.ID,
		"drop_object_key", drop.ObjectKey,
		"scene_slot", drop.SceneSlot,
		"item_id", drop.Item.ItemID,
		"amount", drop.Amount,
		"destination_slot", destinationSlot,
		"picker_actor_object_key", pickerActorObjectKey,
		"request_context", request.ObjectContext,
		"request_player_x", request.PlayerX,
		"request_player_y", request.PlayerY,
		"request_token0", request.Token0,
		"request_drop_x", request.DropX,
		"request_drop_y", request.DropY,
		"request_token1", request.Token1,
		"request_token2", request.Token2,
		"response_opcode", currentDungeonPickupResultOpcode,
		"response_body_len", len(responseBody),
		"asset_owner", "character_items_uow",
		"item_update_opcode", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"item_update_body_len", len(itemUpdateBody))
	if err := s.sendGameUpperRawClass(session, currentDungeonPickupResultOpcode, responseBody, 0); err != nil {
		return err
	}
	return s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketWalkoutPartyMember),
		itemUpdateBody,
		0,
	)
}

func grantCurrentDungeonPickupItem(
	ctx context.Context,
	uow dnfrepo.CharacterItemUnitOfWork,
	characterID string,
	definition dungeonDropItemDefinition,
	amount uint32,
	now time.Time,
	qualitySeed ...uint32,
) (uint16, currentItemListEntry, error) {
	if uow == nil || characterID == "" {
		return 0, currentItemListEntry{}, errDungeonPickupTransactionMissing
	}
	if definition.ItemID == 0 || definition.SlotStart <= 0 || definition.SlotEnd < definition.SlotStart || amount == 0 {
		return 0, currentItemListEntry{}, fmt.Errorf(
			"%w: item=%d slots=%d..%d amount=%d",
			errDungeonPickupItemInvalid,
			definition.ItemID,
			definition.SlotStart,
			definition.SlotEnd,
			amount,
		)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var err error
	definition, err = currentPVFItemDefinitionForGrantAt(definition, now)
	if err != nil {
		return 0, currentItemListEntry{}, err
	}
	owner, err := dnfdungeon.NewOwner(dnfrepo.Group{CharacterItems: uow})
	if err != nil {
		return 0, currentItemListEntry{}, errDungeonPickupTransactionMissing
	}
	var itemUpdate currentItemListEntry
	result, err := owner.GrantPickupItem(ctx, dnfdungeon.PickupItemCommand{
		CharacterID: characterID,
		Placement:   currentDungeonPickupPlacement(definition, amount, qualitySeed...),
		UpdatedAt:   now,
		Finalize: func(slot int16, stack dnfrepo.ItemStack) (dnfrepo.ItemStack, error) {
			if stack.ItemID != int64(definition.ItemID) || stack.Count <= 0 {
				return dnfrepo.ItemStack{}, errDungeonPickupItemInvalid
			}
			itemUpdate = currentItemListEntryFromStack(
				dnfrepo.MainInventoryListType,
				slot,
				stack,
			)
			stack.RawEntry = append([]byte(nil), itemUpdate.data[:]...)
			return stack, nil
		},
	})
	if err != nil {
		return 0, currentItemListEntry{}, mapCurrentDungeonPickupOwnerError(err)
	}
	return result.Slot, itemUpdate, nil
}

func addCurrentDungeonPickupToInventory(
	record *dnfrepo.InventoryRecord,
	definition dungeonDropItemDefinition,
	amount uint32,
	qualitySeed ...uint32,
) (uint16, error) {
	result, _, err := dnfdungeon.PlacePickupItem(
		record,
		currentDungeonPickupPlacement(definition, amount, qualitySeed...),
	)
	if err != nil {
		return 0, mapCurrentDungeonPickupOwnerError(err)
	}
	return result, nil
}

func currentDungeonPickupPlacement(
	definition dungeonDropItemDefinition,
	amount uint32,
	qualitySeed ...uint32,
) dnfdungeon.PickupItemPlacement {
	kind := dnfdungeon.PickupItemKind(definition.Kind)
	placement := dnfdungeon.PickupItemPlacement{
		Definition: dnfdungeon.PickupItemDefinition{
			ItemID:     definition.ItemID,
			Kind:       kind,
			StackLimit: definition.StackLimit,
			SlotStart:  definition.SlotStart,
			SlotEnd:    definition.SlotEnd,
			PreferQuickSlot: definition.Kind == dungeonDropItemStackable &&
				dungeonDropStackablePrefersItemQuickSlots(definition.StackableType),
		},
		Amount: amount,
	}
	if !definition.ExpirationDate.IsZero() {
		placement.NormalizeExisting = func(stack dnfrepo.ItemStack) (dnfrepo.ItemStack, error) {
			stack, _ = applyCurrentPVFItemExpiration(stack, definition)
			return stack, nil
		}
	}
	placement.BuildNew = func(slot int16) (dnfrepo.ItemStack, error) {
		return buildCurrentDungeonPickupStack(slot, definition, amount, qualitySeed...)
	}
	return placement
}

func buildCurrentDungeonPickupStack(
	slot int16,
	definition dungeonDropItemDefinition,
	amount uint32,
	qualitySeed ...uint32,
) (dnfrepo.ItemStack, error) {
	entry, err := currentDungeonDeathDropItemState(uint16(slot), definition, amount, qualitySeed...)
	if err != nil {
		return dnfrepo.ItemStack{}, err
	}
	extra := map[string]string{
		"source":    "dungeon_pvf_drop_pickup",
		"item_kind": string(definition.Kind),
		"pvf_path":  definition.PVFPath,
	}
	if definition.StackableType != "" {
		extra["stackable_type"] = definition.StackableType
	}
	if definition.StackLimit > 0 {
		extra["stack_limit"] = strconv.FormatInt(definition.StackLimit, 10)
	}
	if definition.Durability > 0 {
		extra["durability"] = strconv.FormatUint(uint64(definition.Durability), 10)
		extra["max_durability"] = strconv.FormatUint(uint64(definition.Durability), 10)
	}
	if definition.EquipmentType != "" {
		extra["equipment_type"] = definition.EquipmentType
	}
	stack := dnfrepo.ItemStack{
		ItemID: int64(definition.ItemID),
		Count:  int64(amount),
		Extra:  extra,
	}
	if definition.Kind == dungeonDropItemEquipment {
		if err := applyCurrentEquipmentQualitySeed(&stack, binary.LittleEndian.Uint32(entry.data[6:10])); err != nil {
			return dnfrepo.ItemStack{}, err
		}
	}
	if !definition.ExpirationDate.IsZero() {
		stack, _ = applyCurrentPVFItemExpiration(stack, definition)
	}
	entry = currentItemListEntryFromStack(dnfrepo.MainInventoryListType, slot, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)
	return stack, nil
}

func insertCurrentDungeonPickup(
	record *dnfrepo.InventoryRecord,
	definition dungeonDropItemDefinition,
	amount uint32,
	slot int16,
	qualitySeed ...uint32,
) (uint16, error) {
	if record == nil {
		return 0, errDungeonPickupItemInvalid
	}
	stack, err := buildCurrentDungeonPickupStack(slot, definition, amount, qualitySeed...)
	if err != nil {
		return 0, err
	}
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	record.Slots[currentDungeonPickupMainSlotKey(slot)] = stack
	return uint16(slot), nil
}

func mapCurrentDungeonPickupOwnerError(err error) error {
	switch {
	case errors.Is(err, dnfdungeon.ErrOwnerUnavailable):
		return errDungeonPickupTransactionMissing
	case errors.Is(err, dnfdungeon.ErrInventoryNotFound):
		return errDungeonPickupInventoryMissing
	case errors.Is(err, dnfdungeon.ErrPickupInventoryFull):
		return fmt.Errorf("%w: %v", errDungeonPickupInventoryFull, err)
	case errors.Is(err, dnfdungeon.ErrPickupStackLimit):
		return fmt.Errorf("%w: %v", errDungeonPickupStackLimit, err)
	case errors.Is(err, dnfdungeon.ErrPickupItemInvalid):
		return fmt.Errorf("%w: %v", errDungeonPickupItemInvalid, err)
	default:
		return err
	}
}

func currentDungeonPickupMainSlotKey(slot int16) string {
	return "0:" + strconv.FormatInt(int64(slot), 10)
}
