package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentLotteryPendingTTL = 30 * time.Second

func decodeCurrentLotteryRequest(body []byte) (bool, int16, error) {
	if len(body) < 4 {
		return false, 0, fmt.Errorf("lottery body length %d is below 4", len(body))
	}
	mode := binary.LittleEndian.Uint16(body[:2])
	slot := int16(binary.LittleEndian.Uint16(body[2:4]))
	if mode > 1 || slot < 0 {
		return false, 0, fmt.Errorf("lottery mode=%d slot=%d is invalid", mode, slot)
	}
	return mode == 1, slot, nil
}

func buildCurrentLotteryPhaseStartBody() []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeUint16(math.MaxUint16)
	writer.writeUint16(0)
	writer.writeUint32(0)
	writer.writeUint32(0)
	return writer.bytes()
}

func buildCurrentLotteryErrorBody() []byte {
	body := buildCurrentLotteryPhaseStartBody()
	body[0] = 0
	return body
}

func (s *Service) handleCurrentLotteryItem(session *gameSession, body []byte) error {
	wantsDouble, slot, err := decodeCurrentLotteryRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-lottery-item-rejected", "body_len", len(body), "reason", err)
		return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketUseLotteryItem), buildCurrentLotteryErrorBody(), dnfproto.DefaultChannelClassification)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketUseLotteryItem), buildCurrentLotteryErrorBody(), dnfproto.DefaultChannelClassification)
	}
	request := currentBoosterOpenRequest{Kind: currentBoosterRequestRandom, SourceSlot: slot}
	definition, err := s.prepareCurrentBooster(ctx, session, catalog, request)
	if err != nil || definition.StackableType != "[random upgradable legacy]" {
		s.logGameEvent(session, "game-lottery-item-rejected",
			"slot", slot,
			"stackable_type", definition.StackableType,
			"reason", err)
		return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketUseLotteryItem), buildCurrentLotteryErrorBody(), dnfproto.DefaultChannelClassification)
	}
	doubleReward := false
	now := time.Now().UTC()
	if wantsDouble {
		_, account, character, premiumErr := s.currentPremiumServiceRecords(ctx, session)
		doubleReward = premiumErr == nil &&
			premium.Active(account, premium.DevilSlotType(premium.DevilSlotDoubleJar), now) &&
			premium.DailyUsage(character, premium.DevilSlotDoubleJar, now) < premium.DailyLimit(premium.DevilSlotDoubleJar)
	}
	session.lottery.mu.Lock()
	session.lottery.pending = true
	session.lottery.pendingSlot = slot
	session.lottery.pendingDouble = doubleReward
	session.lottery.pendingAt = now
	session.lottery.mu.Unlock()
	s.logGameEvent(session, "game-lottery-item-phase-start",
		"slot", slot,
		"source_item", definition.Source.ItemID,
		"requested_double", wantsDouble,
		"double_reward", doubleReward,
		"state_source", "current_exe_op27_u16_mode_i16_slot_and_runtime_pvf")
	return s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketUseLotteryItem),
		buildCurrentLotteryPhaseStartBody(),
		dnfproto.DefaultChannelClassification,
	)
}

func (s *Service) handleCurrentLotteryOverflowConfirm(session *gameSession, body []byte) error {
	if session == nil || !bytes.Equal(body, []byte{1, 0x1b, 0}) {
		return nil
	}
	now := time.Now().UTC()
	session.lottery.mu.Lock()
	pending := session.lottery.pending && now.Sub(session.lottery.pendingAt) <= currentLotteryPendingTTL
	slot := session.lottery.pendingSlot
	doubleReward := session.lottery.pendingDouble
	session.lottery.pending = false
	session.lottery.pendingDouble = false
	session.lottery.mu.Unlock()
	if !pending {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return err
	}
	request := currentBoosterOpenRequest{
		Kind:                currentBoosterRequestRandom,
		SourceSlot:          slot,
		RewardMultiplier:    1,
		ConsumePremiumDaily: doubleReward,
		PremiumDailySlot:    premium.DevilSlotDoubleJar,
	}
	if doubleReward {
		request.RewardMultiplier = 2
	}
	definition, err := s.prepareCurrentBooster(ctx, session, catalog, request)
	if err != nil || definition.StackableType != "[random upgradable legacy]" {
		return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketUseLotteryItem), buildCurrentLotteryErrorBody(), dnfproto.DefaultChannelClassification)
	}
	result, err := s.commitCurrentBooster(ctx, session, catalog, definition, request)
	if err != nil {
		s.logGameEvent(session, "game-lottery-item-open-rejected", "slot", slot, "double_reward", doubleReward, "reason", err)
		return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketUseLotteryItem), buildCurrentLotteryErrorBody(), dnfproto.DefaultChannelClassification)
	}
	resultBody, err := s.buildCurrentLotteryResultBody(ctx, session, catalog, result)
	if err != nil {
		return err
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketUseLotteryItem), resultBody, dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	if err := s.sendSelectedIncrementalItemSlotRefreshes(
		session,
		"use_lottery_item",
		[]alignedcmd.ItemSlotRefresh{{ListType: dnfrepo.MainInventoryListType, SlotIndex: result.SourceSlot}},
	); err != nil {
		return err
	}
	for _, listType := range result.ChangedLists {
		listBody, _, _, ok := s.buildCurrentItemListBodyForSession(ctx, session, listType)
		if !ok {
			return errCurrentBoosterOwnerUnavailable
		}
		if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), listBody); err != nil {
			return err
		}
	}
	if doubleReward {
		if err := s.sendCurrentPremiumServiceState(session, "lottery_double_reward_after_commit"); err != nil {
			return err
		}
	}
	s.logGameEvent(session, "game-lottery-item-open-committed",
		"slot", slot,
		"source_item", result.SourceItemID,
		"source_remaining", result.SourceRemaining,
		"double_reward", doubleReward,
		"reward_count", len(result.Rewards),
		"rewards", fmt.Sprint(result.Rewards))
	return nil
}

func (s *Service) buildCurrentLotteryResultBody(
	ctx context.Context,
	session *gameSession,
	catalog *pvfDungeonDropCatalog,
	result currentBoosterCommitResult,
) ([]byte, error) {
	if len(result.Rewards) == 0 {
		return buildCurrentLotteryErrorBody(), nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Inventory == nil || session == nil {
		return nil, errCurrentBoosterOwnerUnavailable
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	inventory, found, err := repositories.Inventory.Load(ctx, characterID)
	if err != nil || !found {
		return nil, errCurrentBoosterOwnerUnavailable
	}
	rewards := append([]currentBoosterGrantedReward(nil), result.Rewards...)
	sort.Slice(rewards, func(i, j int) bool { return rewards[i].ItemID < rewards[j].ItemID })
	display := rewards[0]
	displaySlot := int16(-1)
	displayStack := dnfrepo.ItemStack{}
	for key, stack := range inventory.Slots {
		listType, slot, parsed := parseSceneInventorySlotKey(key)
		if parsed && listType == dnfrepo.MainInventoryListType && stack.ItemID == int64(display.ItemID) {
			if displaySlot < 0 || slot < displaySlot {
				displaySlot = slot
				displayStack = stack
			}
		}
	}
	if displaySlot < 0 {
		return buildCurrentLotteryErrorBody(), nil
	}
	definition, err := catalog.ResolveItem(display.ItemID)
	if err != nil {
		return nil, err
	}
	displayValue := display.Count
	if definition.Kind != dungeonDropItemStackable {
		displayValue = 0
	}
	durability := uint16(0)
	if raw := displayStack.Extra["durability"]; raw != "" {
		if value, parseErr := strconv.ParseUint(raw, 10, 16); parseErr == nil {
			durability = uint16(value)
		}
	}
	var writer packetWriter
	writer.writeByte(1)
	writer.writeUint16(uint16(result.SourceSlot))
	writer.writeUint16(uint16(displaySlot))
	writer.writeUint32(display.ItemID)
	writer.writeUint32(displayValue)
	writer.writeUint16(durability)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeUint16(0)
	if definition.Kind != dungeonDropItemStackable {
		writer.writeByte(0xef)
		writer.writeUint32(25)
		writer.writeBytes(make([]byte, 25))
	}
	writer.writeBytes([]byte{0, 0, 0})
	return writer.bytes(), nil
}
