package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"

	dnfachievement "longheng.io/server/internal/modules/dnf/achievement"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnftitlebook "longheng.io/server/internal/modules/dnf/titlebook"
)

// Title book uses inventory list type 100 with slot encoding:
// slot = category * 1000 + bookIndex
const (
	currentTitleBookListType       byte   = dnftitlebook.ListType
	currentTitleBookSlotBase       int16  = dnftitlebook.SlotBase
	currentTitleBookMsgID          uint16 = 0x0166
	currentTitleBookPutMsgID       uint16 = uint16(dnfenum.CmdPacketTitleBookPut)
	currentTitleBookGetMsgID       uint16 = uint16(dnfenum.CmdPacketTitleBookGet)
	currentTitleBookMaxPerCategory        = dnftitlebook.MaxPerCategory
)

// currentTitleBookEntry is one title in the title book. The current EXE reads
// the 22-byte fixed projection followed by u32 raw_len and raw_len bytes.
type currentTitleBookEntry struct {
	SlotIndex      uint16
	ItemID         int32
	Value          int32
	Attr           byte
	Durability     uint16
	SealFlag       byte
	EnchantIndex   int32
	EnchantUpgrade byte
	AmplifyType    byte
	AmplifyValue   uint16
}

func currentTitleBookSlotKey(category int32, index int32) string {
	slot := int16(category)*currentTitleBookSlotBase + int16(index)
	return fmt.Sprintf("%d:%d", currentTitleBookListType, slot)
}

func currentTitleBookCategoryFromSlot(slot int16) int32 {
	return int32(slot / currentTitleBookSlotBase)
}

func currentTitleBookIndexFromSlot(slot int16) int32 {
	return int32(slot % currentTitleBookSlotBase)
}

// handleCurrentTitleBookPut processes CMD 0x019C (412): put a title from
// inventory/equipment into the title book.
func (s *Service) handleCurrentTitleBookPut(session *gameSession, body []byte) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if len(body) < 20 {
		s.logGameEvent(session, "game-title-book-put-blocked", "body_len", len(body), "reason", "body_too_short")
		return nil
	}
	itemSpace := int32(binary.LittleEndian.Uint32(body[0:4]))
	slot := int16(int32(binary.LittleEndian.Uint32(body[4:8])))
	itemID := int32(binary.LittleEndian.Uint32(body[8:12]))
	category := int32(binary.LittleEndian.Uint32(body[12:16]))
	bookIndex := int32(binary.LittleEndian.Uint32(body[16:20]))

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repos, ok := s.repositoryGroup()
	if !ok {
		return nil
	}
	owner, err := dnftitlebook.NewOwner(repos)
	if err != nil {
		return nil
	}
	result, err := owner.Put(ctx, dnftitlebook.PutCommand{
		SelectedCharacterID: session.selectedCharacterID,
		ItemSpace:           itemSpace,
		SourceSlot:          slot,
		ItemID:              itemID,
		Category:            category,
		BookIndex:           bookIndex,
	})
	switch {
	case errors.Is(err, dnftitlebook.ErrInventoryNotFound):
		s.logGameEvent(session, "game-title-book-put-blocked",
			"char_id", session.selectedCharacterID,
			"reason", "inventory_not_found")
		return s.sendTitleBookFailure(session, currentTitleBookPutMsgID, 1, itemSpace, category)
	case errors.Is(err, dnftitlebook.ErrSourceNotFound):
		s.logGameEvent(session, "game-title-book-put-blocked",
			"char_id", session.selectedCharacterID,
			"source_list", byte(itemSpace),
			"source_slot", slot,
			"item_id", itemID,
			"reason", "source_not_found")
		return s.sendTitleBookFailure(session, currentTitleBookPutMsgID, 2, itemSpace, category)
	case errors.Is(err, dnftitlebook.ErrCategoryFull):
		s.logGameEvent(session, "game-title-book-put-blocked",
			"char_id", session.selectedCharacterID,
			"category", category,
			"reason", "category_full")
		return s.sendTitleBookFailure(session, currentTitleBookPutMsgID, 3, itemSpace, category)
	case errors.Is(err, dnftitlebook.ErrCategoryInvalid):
		return s.sendTitleBookFailure(session, currentTitleBookPutMsgID, 4, itemSpace, category)
	case err != nil:
		s.logGameEvent(session, "game-title-book-put-failed",
			"char_id", session.selectedCharacterID,
			"reason", err)
		return nil
	}

	s.logGameEvent(session, "game-title-book-put-success",
		"char_id", result.CharacterID,
		"item_id", result.ItemID,
		"category", result.Category,
		"book_index", result.BookIndex,
		"source_list", result.SourceList,
		"source_slot", result.SourceSlot,
		"target_slot", result.TargetSlot)

	if err := s.sendTitleBookPutAck(
		session,
		result.ItemSpace,
		result.SourceSlot,
		result.Category,
		result.BookIndex,
	); err != nil {
		return err
	}
	var emptyEntry currentItemListEntry
	emptyEntry.patchCore(result.SourceSlot, 0xFFFFFFFF, 0)
	updateBody := buildCurrentItemUpdateBody(result.SourceList, []currentItemListEntry{emptyEntry})
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), updateBody, 0); err != nil {
		return err
	}
	return s.sendCurrentTitleBookList(session, ctx, repos, result.CharacterID, result.Category)
}

// handleCurrentTitleBookGet processes CMD 0x019D (413): take a title from
// the title book back to inventory.
func (s *Service) handleCurrentTitleBookGet(session *gameSession, body []byte) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if len(body) < 20 {
		s.logGameEvent(session, "game-title-book-get-blocked", "body_len", len(body), "reason", "body_too_short")
		return nil
	}
	itemSpace := int32(binary.LittleEndian.Uint32(body[0:4]))
	slot := int16(int32(binary.LittleEndian.Uint32(body[4:8])))
	itemID := int32(binary.LittleEndian.Uint32(body[8:12]))
	category := int32(binary.LittleEndian.Uint32(body[12:16]))
	bookIndex := int32(binary.LittleEndian.Uint32(body[16:20]))

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repos, ok := s.repositoryGroup()
	if !ok {
		return nil
	}
	owner, err := dnftitlebook.NewOwner(repos)
	if err != nil {
		return nil
	}
	result, err := owner.Get(ctx, dnftitlebook.GetCommand{
		SelectedCharacterID: session.selectedCharacterID,
		ItemSpace:           itemSpace,
		SourceSlot:          slot,
		ItemID:              itemID,
		Category:            category,
		BookIndex:           bookIndex,
	})
	switch {
	case errors.Is(err, dnftitlebook.ErrInventoryNotFound):
		return s.sendTitleBookFailure(session, currentTitleBookGetMsgID, 1, itemSpace, category)
	case errors.Is(err, dnftitlebook.ErrSourceNotFound):
		s.logGameEvent(session, "game-title-book-get-blocked",
			"char_id", session.selectedCharacterID,
			"category", category,
			"book_index", bookIndex,
			"reason", "not_found_in_book")
		return s.sendTitleBookFailure(session, currentTitleBookGetMsgID, 2, itemSpace, category)
	case errors.Is(err, dnftitlebook.ErrTargetFull):
		s.logGameEvent(session, "game-title-book-get-blocked",
			"char_id", session.selectedCharacterID,
			"reason", "inventory_full")
		return s.sendTitleBookFailure(session, currentTitleBookGetMsgID, 3, itemSpace, category)
	case errors.Is(err, dnftitlebook.ErrCategoryInvalid):
		return s.sendTitleBookFailure(session, currentTitleBookGetMsgID, 4, itemSpace, category)
	case err != nil:
		s.logGameEvent(session, "game-title-book-get-failed",
			"char_id", session.selectedCharacterID,
			"reason", err)
		return nil
	}

	s.logGameEvent(session, "game-title-book-get-success",
		"char_id", result.CharacterID,
		"item_id", result.ItemID,
		"category", result.Category,
		"book_index", result.BookIndex,
		"target_slot", result.TargetSlot)

	if err := s.sendTitleBookGetAck(
		session,
		result.ItemSpace,
		result.SourceSlot,
		result.Category,
		result.BookIndex,
	); err != nil {
		return err
	}
	entry := currentItemListEntryFromStack(
		dnfrepo.MainInventoryListType,
		result.TargetSlot,
		result.TargetStack,
	)
	updateBody := buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, []currentItemListEntry{entry})
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), updateBody, 0); err != nil {
		return err
	}
	return s.sendCurrentTitleBookList(session, ctx, repos, result.CharacterID, result.Category)
}

// sendCurrentTitleBookList sends NOTI 0x0166 for one category.
func (s *Service) sendCurrentTitleBookList(session *gameSession, ctx context.Context, repos dnfrepo.Group, characterID string, category int32) error {
	inventory, found, err := repos.Inventory.Load(ctx, characterID)
	if err != nil || !found {
		return nil
	}
	entries := currentTitleBookEntriesForCategory(inventory, category)
	body := buildCurrentTitleBookListBody(session.selectedCharacterID, category, entries)
	return s.sendGameUpperRawClass(session, currentTitleBookMsgID, body, 0)
}

// sendAllCurrentTitleBookLists sends NOTI 0x0166 for all categories that have entries.
func (s *Service) sendAllCurrentTitleBookLists(session *gameSession, ctx context.Context, repos dnfrepo.Group, characterID string) error {
	inventory, found, err := repos.Inventory.Load(ctx, characterID)
	if err != nil || !found {
		return nil
	}
	categories := currentTitleBookCategories(inventory)
	for _, category := range categories {
		entries := currentTitleBookEntriesForCategory(inventory, category)
		body := buildCurrentTitleBookListBody(session.selectedCharacterID, category, entries)
		if err := s.sendGameUpperRawClass(session, currentTitleBookMsgID, body, 0); err != nil {
			return err
		}
	}
	return nil
}

func currentTitleBookCategories(inventory dnfrepo.InventoryRecord) []int32 {
	categorySet := make(map[int32]struct{})
	for key, stack := range inventory.Slots {
		if stack.ItemID <= 0 || dnfachievement.IsLegacyInventoryProgress(key, stack) {
			continue
		}
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if !ok || listType != currentTitleBookListType {
			continue
		}
		categorySet[currentTitleBookCategoryFromSlot(slot)] = struct{}{}
	}
	categories := make([]int32, 0, len(categorySet))
	for c := range categorySet {
		categories = append(categories, c)
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i] < categories[j] })
	return categories
}

func currentTitleBookEntriesForCategory(inventory dnfrepo.InventoryRecord, category int32) []currentTitleBookEntry {
	entries := make([]currentTitleBookEntry, 0, 8)
	for key, stack := range inventory.Slots {
		if stack.ItemID <= 0 || dnfachievement.IsLegacyInventoryProgress(key, stack) {
			continue
		}
		listType, slot, ok := parseSceneInventorySlotKey(key)
		if !ok || listType != currentTitleBookListType {
			continue
		}
		if currentTitleBookCategoryFromSlot(slot) != category {
			continue
		}
		entries = append(entries, currentTitleBookEntry{
			SlotIndex:      uint16(currentTitleBookIndexFromSlot(slot)),
			ItemID:         int32(stack.ItemID),
			Value:          int32(sceneInventoryStackAmount(stack)),
			Attr:           sceneInventoryExtraByte(stack.Extra, "packed_flag_byte", "packed_flag", "packed", "ext_data0", "ext0"),
			Durability:     sceneInventoryExtraUint16(stack.Extra, "value_b_word", "durability", "max_durability"),
			SealFlag:       sceneInventoryExtraByteDefault(stack.Extra, 1, "create_flag"),
			EnchantIndex:   int32(sceneInventoryExtraUint32(stack.Extra, "value_a", "instance_value", "item_uid", "serial", "count_or_instance")),
			EnchantUpgrade: sceneInventoryExtraByte(stack.Extra, "unused_byte_after_value_a", "unused"),
			AmplifyType:    sceneInventoryExtraByte(stack.Extra, "value_c"),
			AmplifyValue:   sceneInventoryExtraUint16(stack.Extra, "value_d"),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].SlotIndex < entries[j].SlotIndex })
	return entries
}

// buildCurrentTitleBookListBody builds NOTI 0x0166 body.
// Format: u8 info_type + u16 owner_id + i32 category + i32 count + count * 22B entries.
func buildCurrentTitleBookListBody(ownerID uint16, category int32, entries []currentTitleBookEntry) []byte {
	var w packetWriter
	w.writeByte(0) // info_type: 0 = self
	w.writeUint16(ownerID)
	w.writeInt32(int(category))
	w.writeInt32(len(entries))
	for _, e := range entries {
		w.writeUint16(e.SlotIndex)
		w.writeInt32(int(e.ItemID))
		w.writeInt32(int(e.Value))
		w.writeByte(e.Attr)
		w.writeUint16(e.Durability)
		w.writeByte(e.SealFlag)
		w.writeInt32(int(e.EnchantIndex))
		w.writeByte(e.EnchantUpgrade)
		w.writeByte(e.AmplifyType)
		w.writeUint16(e.AmplifyValue)
		w.writeUint32(0)
	}
	return w.bytes()
}

func (s *Service) sendTitleBookPutAck(session *gameSession, itemSpace int32, slot int16, category, bookIndex int32) error {
	var w packetWriter
	w.writeInt32(int(itemSpace))
	w.writeInt32(int(slot))
	w.writeInt32(int(category))
	w.writeInt32(int(bookIndex))
	return s.sendGameUpperSuccess(session, currentTitleBookPutMsgID, w.bytes())
}

func (s *Service) sendTitleBookGetAck(session *gameSession, itemSpace int32, slot int16, category, bookIndex int32) error {
	var w packetWriter
	w.writeInt32(int(itemSpace))
	w.writeInt32(int(slot))
	w.writeInt32(int(category))
	w.writeInt32(int(bookIndex))
	return s.sendGameUpperSuccess(session, currentTitleBookGetMsgID, w.bytes())
}

func (s *Service) sendTitleBookFailure(session *gameSession, msgID uint16, errorCode byte, itemSpace int32, category int32) error {
	var w packetWriter
	w.writeByte(0) // failure
	w.writeByte(errorCode)
	w.writeInt32(int(itemSpace))
	w.writeInt32(int(category))
	return s.sendGameUpperRaw(session, msgID, w.bytes())
}

// isCurrentTitleBookRequest returns true if the opcode is a title book operation
// that should be handled by the dnfbridge title book handler instead of the
// aligned command system.
func isCurrentTitleBookRequest(opcode uint16) bool {
	return opcode == uint16(dnfenum.CmdPacketTitleBookPut) || opcode == uint16(dnfenum.CmdPacketTitleBookGet)
}

// --- Phase 2: Achievement progress (CMD 0x01A1 / 417) ---

const (
	currentAchievementTriggerMsgID uint16 = uint16(dnfenum.CmdPacketAchievementTrigger)
)

// resolveAchievementDefinition joins the titlebook.etc placement mapping with
// the same quest PVF catalog used by normal quest logic.
func (s *Service) resolveAchievementDefinition(ctx context.Context, questID int32) (dnfachievement.Definition, error) {
	mapping := s.currentTitleBookMapping()
	if mapping == nil {
		return dnfachievement.Definition{}, dnfachievement.ErrDefinitionNotFound
	}
	entry, ok := mapping[questID]
	if !ok {
		return dnfachievement.Definition{}, fmt.Errorf("%w: quest=%d", dnfachievement.ErrDefinitionNotFound, questID)
	}
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		return dnfachievement.Definition{}, err
	}
	questDefinition, ok := catalog.Find(int64(questID))
	if !ok {
		return dnfachievement.Definition{}, fmt.Errorf("%w: quest=%d pvf", dnfachievement.ErrDefinitionNotFound, questID)
	}
	targets := currentAchievementTargets(questDefinition)
	return dnfachievement.Definition{
		QuestID: questID,
		Target1: targets[0],
		Target2: targets[1],
		Target3: targets[2],
		Reward: dnfachievement.TitleReward{
			ItemID:    entry.titleItemID,
			Category:  entry.category,
			BookIndex: entry.bookIndex,
		},
	}, nil
}

type titleBookMappingEntry struct {
	titleItemID int32
	category    int32
	bookIndex   int32
	rewardCount int32
}

// currentTitleBookMapping returns the cached quest_id → title mapping from titlebook.etc.
func (s *Service) currentTitleBookMapping() map[int32]titleBookMappingEntry {
	s.titleBookMappingMu.Lock()
	defer s.titleBookMappingMu.Unlock()
	if s.titleBookMappingCache != nil {
		return s.titleBookMappingCache
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil
	}
	text, err := archive.ReadText("etc/titlebook.etc")
	if err != nil {
		s.logPacketEvent("game-titlebook-etc-read-failed", "error", err)
		return nil
	}
	mapping := parseTitleBookEtc(text)
	s.titleBookMappingCache = mapping
	s.logPacketEvent("game-titlebook-mapping-loaded", "entries", len(mapping))
	return mapping
}

// parseTitleBookEtc parses the current PVF's repeated
// [title collection info] sections. Each section starts with category name and
// max slots, then uses: book_index, marker, quest_id, reward_count, title_id.
// marker -1 denotes an empty slot and therefore has no following triple.
func parseTitleBookEtc(text string) map[int32]titleBookMappingEntry {
	mapping := make(map[int32]titleBookMappingEntry, 256)
	document, err := dnfpvf.Parse("etc/titlebook.etc", text)
	if err != nil {
		return mapping
	}
	category := int32(0)
	for _, section := range document.Sections {
		if !strings.EqualFold(strings.TrimSpace(section.Name), "title collection info") {
			continue
		}
		if section.Start < 0 || section.End > len(document.Tokens) || section.Start >= section.End {
			category++
			continue
		}
		tokens := document.Tokens[section.Start:section.End]
		cursor := 2 // category name + max slots
		for cursor+1 < len(tokens) {
			if tokens[cursor].Kind != dnfpvf.TokenInt || tokens[cursor+1].Kind != dnfpvf.TokenInt {
				cursor++
				continue
			}
			bookIndex := tokens[cursor].Int
			marker := tokens[cursor+1].Int
			cursor += 2
			if marker == -1 {
				continue
			}
			if cursor+2 >= len(tokens) ||
				tokens[cursor].Kind != dnfpvf.TokenInt ||
				tokens[cursor+1].Kind != dnfpvf.TokenInt ||
				tokens[cursor+2].Kind != dnfpvf.TokenInt {
				break
			}
			questID := tokens[cursor].Int
			rewardCount := tokens[cursor+1].Int
			titleItemID := tokens[cursor+2].Int
			cursor += 3
			if questID <= 0 || questID > int64(^uint32(0)>>1) ||
				bookIndex < 0 || bookIndex >= int64(currentTitleBookMaxPerCategory) ||
				titleItemID <= 0 || titleItemID > int64(^uint32(0)>>1) {
				continue
			}
			mapping[int32(questID)] = titleBookMappingEntry{
				titleItemID: int32(titleItemID),
				category:    category,
				bookIndex:   int32(bookIndex),
				rewardCount: int32(rewardCount),
			}
		}
		category++
	}
	return mapping
}

func currentAchievementTargets(definition dnfquest.Definition) [3]uint16 {
	var targets [3]uint16
	for index, value := range definition.CheckCount {
		if index >= len(targets) {
			break
		}
		targets[index] = currentAchievementTargetValue(value)
	}
	if targets[0] != 0 || targets[1] != 0 || targets[2] != 0 {
		return targets
	}
	if strings.EqualFold(strings.TrimSpace(definition.Type), "[seeking]") ||
		strings.EqualFold(strings.TrimSpace(definition.Type), "seeking") {
		for index := 0; index < len(targets) && index*2+1 < len(definition.IntData); index++ {
			targets[index] = currentAchievementTargetValue(definition.IntData[index*2+1])
		}
		if targets[0] != 0 || targets[1] != 0 || targets[2] != 0 {
			return targets
		}
	}
	targets[0] = 1
	return targets
}

func currentAchievementTargetValue(value int64) uint16 {
	if value <= 0 {
		return 0
	}
	if value > int64(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(value)
}
