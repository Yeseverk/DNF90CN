package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentCeraPackageRequestHeaderSize = 3
	currentCeraPackageChoiceStride      = 5
	currentCeraPackageMaxChoices        = 32
	currentCeraPackageMaxRewardRows     = 128
	currentCeraPackageMaxRewardAmount   = 1_000_000
)

var (
	errCurrentCeraPackageRequestMalformed = errors.New("dnf cera package request is malformed")
	errCurrentCeraPackageOwnerUnavailable = errors.New("dnf cera package selected character is unavailable")
	errCurrentCeraPackageSourceMissing    = errors.New("dnf cera package source item is unavailable")
	errCurrentCeraPackagePVFInvalid       = errors.New("dnf cera package PVF data is invalid")
	errCurrentCeraPackageChoiceInvalid    = errors.New("dnf cera package selection is invalid")
	errCurrentCeraPackageExpired          = errors.New("dnf cera package has expired")
)

// currentCeraPackageOpenRequest is the exact current-EXE op518 body written by
// sub_1EC46D0: source slot, selected-avatar count, then item/option pairs.
// Item quantities never come from the client; they are resolved from PVF.
type currentCeraPackageOpenRequest struct {
	SourceSlot int16
	Choices    []currentCeraPackageChoice
}

type currentCeraPackageChoice struct {
	ItemID uint32
	Option byte
}

type currentCeraPackageDefinition struct {
	Source        dungeonDropItemDefinition
	Rewards       []currentCeraPackageReward
	AvatarItemIDs []uint32
}

type currentCeraPackageReward struct {
	Definition dungeonDropItemDefinition
	Count      uint32
	Avatar     bool
}

type currentCeraPackageCommitResult struct {
	ChangedListTypes []byte
	RewardRows       int
	RewardUnits      uint64
	OverflowMailID   string
	OverflowItems    int
}

func parseCurrentCeraPackageOpenRequest(body []byte) (currentCeraPackageOpenRequest, error) {
	if len(body) < currentCeraPackageRequestHeaderSize {
		return currentCeraPackageOpenRequest{}, errCurrentCeraPackageRequestMalformed
	}
	count := int(body[2])
	if count < 1 || count > currentCeraPackageMaxChoices || len(body) != currentCeraPackageRequestHeaderSize+count*currentCeraPackageChoiceStride {
		return currentCeraPackageOpenRequest{}, errCurrentCeraPackageRequestMalformed
	}
	request := currentCeraPackageOpenRequest{
		SourceSlot: int16(binary.LittleEndian.Uint16(body[0:2])),
		Choices:    make([]currentCeraPackageChoice, 0, count),
	}
	seen := make(map[uint32]struct{}, count)
	for index := 0; index < count; index++ {
		offset := currentCeraPackageRequestHeaderSize + index*currentCeraPackageChoiceStride
		itemID := binary.LittleEndian.Uint32(body[offset : offset+4])
		if itemID == 0 {
			return currentCeraPackageOpenRequest{}, errCurrentCeraPackageRequestMalformed
		}
		if _, duplicate := seen[itemID]; duplicate {
			return currentCeraPackageOpenRequest{}, fmt.Errorf("%w: duplicate item=%d", errCurrentCeraPackageChoiceInvalid, itemID)
		}
		seen[itemID] = struct{}{}
		request.Choices = append(request.Choices, currentCeraPackageChoice{ItemID: itemID, Option: body[offset+4]})
	}
	return request, nil
}

func resolveCurrentCeraPackageDefinition(
	catalog *pvfDungeonDropCatalog,
	sourceItemID uint32,
) (currentCeraPackageDefinition, error) {
	if catalog == nil || catalog.source == nil || sourceItemID == 0 {
		return currentCeraPackageDefinition{}, errCurrentCeraPackagePVFInvalid
	}
	sourceDefinition, err := catalog.ResolveItem(sourceItemID)
	if err != nil {
		return currentCeraPackageDefinition{}, fmt.Errorf("%w: resolve source item=%d: %v", errCurrentCeraPackagePVFInvalid, sourceItemID, err)
	}
	if sourceDefinition.Kind != dungeonDropItemStackable {
		return currentCeraPackageDefinition{}, fmt.Errorf("%w: source item=%d is not stackable", errCurrentCeraPackagePVFInvalid, sourceItemID)
	}
	document, err := parseDungeonCardPVFDocument(catalog.source, sourceDefinition.PVFPath)
	if err != nil {
		return currentCeraPackageDefinition{}, fmt.Errorf("%w: read source item=%d: %v", errCurrentCeraPackagePVFInvalid, sourceItemID, err)
	}
	stackableType, found := document.Text("stackable type")
	if !found || !strings.EqualFold(strings.TrimSpace(stackableType), "[usable cera package]") {
		return currentCeraPackageDefinition{}, fmt.Errorf("%w: source item=%d stackable_type=%q", errCurrentCeraPackagePVFInvalid, sourceItemID, stackableType)
	}
	values := document.Ints("package data")
	if len(values) == 0 || len(values)%2 != 0 || len(values)/2 > currentCeraPackageMaxRewardRows {
		return currentCeraPackageDefinition{}, fmt.Errorf("%w: source item=%d package_data_values=%d", errCurrentCeraPackagePVFInvalid, sourceItemID, len(values))
	}

	definition := currentCeraPackageDefinition{
		Source:  sourceDefinition,
		Rewards: make([]currentCeraPackageReward, 0, len(values)/2),
	}
	avatarIDs := make(map[uint32]struct{})
	var total uint64
	for offset := 0; offset < len(values); offset += 2 {
		itemValue, countValue := values[offset], values[offset+1]
		if itemValue <= 0 || itemValue > math.MaxUint32 || countValue <= 0 || countValue > currentCeraPackageMaxRewardAmount {
			return currentCeraPackageDefinition{}, fmt.Errorf("%w: source item=%d reward_pair=%d/%d", errCurrentCeraPackagePVFInvalid, sourceItemID, itemValue, countValue)
		}
		total += uint64(countValue)
		if total > currentCeraPackageMaxRewardAmount {
			return currentCeraPackageDefinition{}, fmt.Errorf("%w: source item=%d reward_total=%d", errCurrentCeraPackagePVFInvalid, sourceItemID, total)
		}
		rewardDefinition, resolveErr := catalog.ResolveItem(uint32(itemValue))
		if resolveErr != nil {
			return currentCeraPackageDefinition{}, fmt.Errorf("%w: source item=%d reward item=%d: %v", errCurrentCeraPackagePVFInvalid, sourceItemID, itemValue, resolveErr)
		}
		avatar, avatarErr := currentCeraPackageRewardIsAvatar(catalog.source, rewardDefinition)
		if avatarErr != nil {
			return currentCeraPackageDefinition{}, fmt.Errorf("%w: source item=%d reward item=%d: %v", errCurrentCeraPackagePVFInvalid, sourceItemID, itemValue, avatarErr)
		}
		reward := currentCeraPackageReward{Definition: rewardDefinition, Count: uint32(countValue), Avatar: avatar}
		definition.Rewards = append(definition.Rewards, reward)
		if avatar {
			if _, exists := avatarIDs[rewardDefinition.ItemID]; !exists {
				avatarIDs[rewardDefinition.ItemID] = struct{}{}
				definition.AvatarItemIDs = append(definition.AvatarItemIDs, rewardDefinition.ItemID)
			}
		}
	}
	if len(definition.AvatarItemIDs) == 0 || len(definition.AvatarItemIDs) > currentCeraPackageMaxChoices {
		return currentCeraPackageDefinition{}, fmt.Errorf("%w: source item=%d avatar_choices=%d", errCurrentCeraPackagePVFInvalid, sourceItemID, len(definition.AvatarItemIDs))
	}
	return definition, nil
}

func currentCeraPackageRewardIsAvatar(source dnfpvf.Source, definition dungeonDropItemDefinition) (bool, error) {
	if definition.Kind != dungeonDropItemEquipment {
		return false, nil
	}
	document, err := parseDungeonCardPVFDocument(source, definition.PVFPath)
	if err != nil {
		return false, err
	}
	equipmentType, found := document.Text("equipment type")
	if !found {
		return false, nil
	}
	rule, supported := currentEquipmentPlacementRuleForPVFType(equipmentType)
	return supported && rule.class == currentEquipmentPlacementClassAvatar, nil
}

func validateCurrentCeraPackageChoices(
	definition currentCeraPackageDefinition,
	choices []currentCeraPackageChoice,
) (map[uint32]byte, error) {
	if len(choices) != len(definition.AvatarItemIDs) {
		return nil, fmt.Errorf("%w: got=%d want=%d", errCurrentCeraPackageChoiceInvalid, len(choices), len(definition.AvatarItemIDs))
	}
	expected := make(map[uint32]struct{}, len(definition.AvatarItemIDs))
	for _, itemID := range definition.AvatarItemIDs {
		expected[itemID] = struct{}{}
	}
	options := make(map[uint32]byte, len(choices))
	for _, choice := range choices {
		if _, ok := expected[choice.ItemID]; !ok {
			return nil, fmt.Errorf("%w: item=%d is not an avatar reward", errCurrentCeraPackageChoiceInvalid, choice.ItemID)
		}
		if _, duplicate := options[choice.ItemID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate item=%d", errCurrentCeraPackageChoiceInvalid, choice.ItemID)
		}
		options[choice.ItemID] = choice.Option
	}
	for itemID := range expected {
		if _, ok := options[itemID]; !ok {
			return nil, fmt.Errorf("%w: avatar item=%d was not selected", errCurrentCeraPackageChoiceInvalid, itemID)
		}
	}
	return options, nil
}

func validateCurrentCeraPackageSourceExpiration(
	stack dnfrepo.ItemStack,
	source dungeonDropItemDefinition,
	now time.Time,
) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if expire := currentItemListStackExpire(stack); expire != 0 {
		if uint64(expire) <= uint64(now.Unix()) {
			return errCurrentCeraPackageExpired
		}
		return nil
	}
	if source.UsablePeriodDays > 0 {
		return errCurrentCeraPackageExpired
	}
	if !source.ExpirationDate.IsZero() && !now.Before(source.ExpirationDate) {
		return errCurrentCeraPackageExpired
	}
	return nil
}

func (s *Service) handleCurrentCeraPackageOpen(session *gameSession, body []byte) error {
	request, err := parseCurrentCeraPackageOpenRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-cera-package-open-rejected", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketOpenCerapackage), 4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		s.logGameEvent(session, "game-cera-package-open-rejected", "source_slot", request.SourceSlot, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketOpenCerapackage), 4)
	}
	definition, err := s.prepareCurrentCeraPackage(ctx, session, catalog, request)
	if err != nil {
		s.logGameEvent(session, "game-cera-package-open-rejected", "source_slot", request.SourceSlot, "choice_count", len(request.Choices), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketOpenCerapackage), 4)
	}
	result, err := s.commitCurrentCeraPackage(ctx, session, catalog, definition, request)
	if err != nil {
		s.logGameEvent(session, "game-cera-package-open-rejected", "source_slot", request.SourceSlot, "source_item", definition.Source.ItemID, "choice_count", len(request.Choices), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketOpenCerapackage), 4)
	}
	var success packetWriter
	success.writeUint16(uint16(request.SourceSlot))
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketOpenCerapackage), success.bytes()); err != nil {
		return err
	}
	for _, listType := range result.ChangedListTypes {
		listBody, _, _, ok := s.buildCurrentItemListBodyForSession(ctx, session, listType)
		if !ok {
			return errCurrentCeraPackageOwnerUnavailable
		}
		if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), listBody); err != nil {
			return err
		}
	}
	if result.OverflowMailID != "" {
		if err := s.sendMailboxAlarmToOnlineRecipient(session.selectedCharacterID); err != nil {
			// The package, its directly placed rewards, and the overflow mail
			// have already committed together. A missed live alarm is harmless:
			// the durable mailbox snapshot will still show the attachment when
			// the player next opens the mailbox.
			s.logWarn("cera package mailbox alarm deferred to next mailbox open",
				"character_id", session.selectedCharacterID,
				"mail_id", result.OverflowMailID,
				"error", err)
		}
	}
	s.logGameEvent(session, "game-cera-package-open-committed",
		"source_slot", request.SourceSlot,
		"source_item", definition.Source.ItemID,
		"choice_count", len(request.Choices),
		"reward_rows", result.RewardRows,
		"reward_units", result.RewardUnits,
		"overflow_mail_id", result.OverflowMailID,
		"overflow_items", result.OverflowItems,
		"changed_lists", fmt.Sprint(result.ChangedListTypes),
		"ack_body", "current_exe_op518_success_u16_slot",
		"inventory_refresh", "current_exe_op13_real_repository_lists")
	return nil
}

func (s *Service) prepareCurrentCeraPackage(
	ctx context.Context,
	session *gameSession,
	catalog *pvfDungeonDropCatalog,
	request currentCeraPackageOpenRequest,
) (currentCeraPackageDefinition, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || request.SourceSlot < 0 || catalog == nil {
		return currentCeraPackageDefinition{}, errCurrentCeraPackageOwnerUnavailable
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Inventory == nil {
		return currentCeraPackageDefinition{}, errCurrentCeraPackageOwnerUnavailable
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	inventory, found, err := repositories.Inventory.Load(ctx, characterID)
	if err != nil {
		return currentCeraPackageDefinition{}, err
	}
	if !found || inventory.CharacterID != characterID {
		return currentCeraPackageDefinition{}, errCurrentCeraPackageOwnerUnavailable
	}
	stack, found := inventory.Slots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, request.SourceSlot)]
	if !found || stack.ItemID <= 0 || stack.ItemID > math.MaxUint32 || stack.Count <= 0 {
		return currentCeraPackageDefinition{}, errCurrentCeraPackageSourceMissing
	}
	definition, err := resolveCurrentCeraPackageDefinition(catalog, uint32(stack.ItemID))
	if err != nil {
		return currentCeraPackageDefinition{}, err
	}
	now := time.Now().UTC()
	if err := validateCurrentCeraPackageSourceExpiration(stack, definition.Source, now); err != nil {
		return currentCeraPackageDefinition{}, err
	}
	if _, err := validateCurrentCeraPackageChoices(definition, request.Choices); err != nil {
		return currentCeraPackageDefinition{}, err
	}
	return definition, nil
}

func (s *Service) commitCurrentCeraPackage(
	ctx context.Context,
	session *gameSession,
	catalog *pvfDungeonDropCatalog,
	definition currentCeraPackageDefinition,
	request currentCeraPackageOpenRequest,
) (currentCeraPackageCommitResult, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 || catalog == nil || catalog.source == nil || definition.Source.ItemID == 0 {
		return currentCeraPackageCommitResult{}, errCurrentCeraPackageOwnerUnavailable
	}
	options, err := validateCurrentCeraPackageChoices(definition, request.Choices)
	if err != nil {
		return currentCeraPackageCommitResult{}, err
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.MailboxAssets == nil {
		return currentCeraPackageCommitResult{}, errCurrentCeraPackageOwnerUnavailable
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	now := time.Now().UTC()
	var result currentCeraPackageCommitResult
	err = repositories.MailboxAssets.WithinMailboxAssets(ctx, characterID, characterID, func(
		characters dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		mailboxes dnfrepo.MailboxRepository,
	) error {
		character, found, err := characters.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || character.CharacterID != characterID || accountID == "" || strings.TrimSpace(character.AccountID) != accountID {
			return errCurrentCeraPackageOwnerUnavailable
		}
		inventory, found, err := inventories.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || inventory.CharacterID != characterID {
			return errCurrentCeraPackageOwnerUnavailable
		}
		inventory = dnfrepo.CloneInventory(inventory)
		if inventory.Slots == nil {
			return errCurrentCeraPackageSourceMissing
		}
		sourceKey := currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, request.SourceSlot)
		sourceStack, found := inventory.Slots[sourceKey]
		if !found || sourceStack.ItemID != int64(definition.Source.ItemID) || sourceStack.Count <= 0 {
			return errCurrentCeraPackageSourceMissing
		}
		if !definition.Source.ExpirationDate.IsZero() {
			sourceStack, _ = applyCurrentPVFItemExpirationAt(sourceStack, definition.Source, now)
		}
		if err := validateCurrentCeraPackageSourceExpiration(sourceStack, definition.Source, now); err != nil {
			return err
		}

		changed := map[byte]struct{}{dnfrepo.MainInventoryListType: {}}
		rewardSlots := make(map[string]struct{})
		overflow := make([]dnfrepo.MailAttachment, 0)
		if sourceStack.Count == 1 {
			delete(inventory.Slots, sourceKey)
		} else {
			sourceStack.Count--
			entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, request.SourceSlot, sourceStack)
			sourceStack.RawEntry = append([]byte(nil), entry.data[:]...)
			inventory.Slots[sourceKey] = sourceStack
		}

		var rewardUnits uint64
		for _, reward := range definition.Rewards {
			rewardUnits += uint64(reward.Count)
			grantDefinition, expirationErr := currentPVFItemDefinitionForNestedRewardGrantAt(reward.Definition, sourceStack, now)
			if expirationErr != nil {
				return fmt.Errorf("%w: reward item=%d expiration: %v", errCurrentCeraPackagePVFInvalid, reward.Definition.ItemID, expirationErr)
			}
			if reward.Avatar {
				option, selected := options[reward.Definition.ItemID]
				if !selected {
					return fmt.Errorf("%w: avatar item=%d", errCurrentCeraPackageChoiceInvalid, reward.Definition.ItemID)
				}
				for count := uint32(0); count < reward.Count; count++ {
					slot, grantErr := grantCurrentCeraShopAvatar(
						&inventory,
						catalog.source,
						grantDefinition,
						currentCeraShopProduct{ItemID: grantDefinition.ItemID, Count: 1, Section: "avatar"},
						option,
					)
					if grantErr != nil {
						return grantErr
					}
					key := currentCeraShopInventorySlotKey(1, slot)
					rewardSlots[key] = struct{}{}
				}
				changed[1] = struct{}{}
				continue
			}
			if isCurrentCeraShopCreatureItem(grantDefinition) {
				for count := uint32(0); count < reward.Count; count++ {
					slot, grantErr := grantCurrentCeraShopPet(&inventory, grantDefinition)
					if grantErr != nil {
						return grantErr
					}
					rewardSlots[currentCeraShopInventorySlotKey(currentPetInventoryListType, slot)] = struct{}{}
				}
				changed[currentPetInventoryListType] = struct{}{}
				continue
			}
			if isCurrentCeraShopPetConsumable(grantDefinition) {
				slots, grantErr := grantCurrentCeraShopPetConsumable(&inventory, grantDefinition, reward.Count)
				if grantErr != nil {
					return grantErr
				}
				for _, slot := range slots {
					rewardSlots[currentCeraShopInventorySlotKey(currentPetInventoryListType, slot)] = struct{}{}
				}
				changed[currentPetInventoryListType] = struct{}{}
				continue
			}
			// Product placement can merge part of a stack before discovering a
			// full category. Probe a clone first, so the fallback either grants
			// the whole reward directly or sends the whole row to mail, never a
			// duplicated or half-consumed remainder.
			candidate := dnfrepo.CloneInventory(inventory)
			slots, grantErr := grantCurrentCeraShopProduct(&candidate, grantDefinition, reward.Count)
			if grantErr != nil {
				if !errors.Is(grantErr, errDungeonPickupInventoryFull) {
					return grantErr
				}
				attachments, attachmentErr := currentCeraPackageOverflowAttachments(
					grantDefinition,
					reward.Count,
					definition.Source.ItemID,
				)
				if attachmentErr != nil {
					return attachmentErr
				}
				overflow = append(overflow, attachments...)
				continue
			}
			inventory = candidate
			for _, slot := range slots {
				rewardSlots[currentCeraShopInventorySlotKey(dnfrepo.MainInventoryListType, int16(slot))] = struct{}{}
			}
		}

		for key := range rewardSlots {
			stack, found := inventory.Slots[key]
			if !found || stack.ItemID <= 0 {
				return errCurrentCeraPackagePVFInvalid
			}
			if stack.Extra == nil {
				stack.Extra = make(map[string]string, 5)
			}
			stack.Extra["source"] = "cera_package"
			stack.Extra["last_grant_source"] = "cera_package"
			stack.Extra["cera_package_source_item"] = strconv.FormatUint(uint64(definition.Source.ItemID), 10)
			listType, slot, parsed := parseSceneInventorySlotKey(key)
			if !parsed {
				return errCurrentCeraPackagePVFInvalid
			}
			entry := currentItemListEntryFromStack(listType, slot, stack)
			stack.RawEntry = append([]byte(nil), entry.data[:]...)
			inventory.Slots[key] = stack
		}
		inventory.UpdatedAt = now
		if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}
		if len(overflow) > 0 {
			mailID, mailErr := dnfrepo.AppendSystemMail(ctx, mailboxes, dnfrepo.SystemMailDelivery{
				RecipientCharacterID: characterID,
				Title:                "背包已满：礼包奖励",
				Body:                 "背包空间不足，礼包中未能放入背包的奖励已通过邮件发送。请清理对应道具分页后领取。",
				Source:               "cera_package_main_inventory_full",
				Attachments:          overflow,
				CreatedAt:            now,
			})
			if mailErr != nil {
				return mailErr
			}
			result.OverflowMailID = mailID
			result.OverflowItems = len(overflow)
		}

		listTypes := make([]byte, 0, len(changed))
		for listType := range changed {
			listTypes = append(listTypes, listType)
		}
		sort.Slice(listTypes, func(left, right int) bool { return listTypes[left] < listTypes[right] })
		result = currentCeraPackageCommitResult{
			ChangedListTypes: listTypes,
			RewardRows:       len(definition.Rewards),
			RewardUnits:      rewardUnits,
			OverflowMailID:   result.OverflowMailID,
			OverflowItems:    result.OverflowItems,
		}
		return nil
	})
	if err != nil {
		return currentCeraPackageCommitResult{}, err
	}
	return result, nil
}

// currentCeraPackageOverflowAttachments makes mail-owned instances for a
// package row that cannot fit its PVF inventory category. The item instances
// retain the same expiration and PVF metadata used by the direct placement
// path, so claiming them later is equivalent to receiving them immediately.
func currentCeraPackageOverflowAttachments(
	definition dungeonDropItemDefinition,
	count uint32,
	sourceItemID uint32,
) ([]dnfrepo.MailAttachment, error) {
	if definition.ItemID == 0 || count == 0 || sourceItemID == 0 {
		return nil, errCurrentCeraPackagePVFInvalid
	}
	attachments := make([]dnfrepo.MailAttachment, 0, 1)
	remaining := count
	for remaining > 0 {
		chunk := remaining
		switch definition.Kind {
		case dungeonDropItemEquipment:
			chunk = 1
		case dungeonDropItemStackable:
			if definition.StackLimit > 0 && int64(chunk) > definition.StackLimit {
				chunk = uint32(definition.StackLimit)
			}
		default:
			return nil, errCurrentCeraShopProductUnavailable
		}
		// The mailbox wire builder replaces the location field. Build with a
		// valid PVF category slot solely to obtain the exact item-instance
		// payload and expiration fields.
		stack, err := buildCurrentDungeonPickupStack(definition.SlotStart, definition, chunk)
		if err != nil {
			return nil, err
		}
		if stack.Extra == nil {
			stack.Extra = make(map[string]string, 4)
		}
		stack.Extra["source"] = "cera_package"
		stack.Extra["last_grant_source"] = "cera_package"
		stack.Extra["cera_package_source_item"] = strconv.FormatUint(uint64(sourceItemID), 10)
		stack.Extra["cera_package_overflow_mail"] = "true"
		attachments = append(attachments, dnfrepo.MailAttachment{
			ItemID:   stack.ItemID,
			Count:    stack.Count,
			Bind:     stack.Bind,
			ExpireAt: stack.ExpireAt,
			RawEntry: append([]byte(nil), stack.RawEntry...),
			Extra:    stack.Extra,
		})
		remaining -= chunk
	}
	return attachments, nil
}
