package dnfbridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// currentNameTagCardEquipmentType is the PVF equipment type for name tag cards.
const currentNameTagCardEquipmentType = "[name tag]"

// currentNameTagEquipmentSlot is the worn equipment slot for name tag cards.
// IDA sub_35FC900 + client op19 dst=(3,30) prove slot 30.
const currentNameTagEquipmentSlot int16 = 30

type currentNameTagCardActivation struct {
	ItemID         uint32
	PreviousItemID int64
	PreviousExpire int64
	ExpireAt       int64
	Action         string
}

func currentNameTagCardDuration(
	source dnfpvf.Source,
	definition dungeonDropItemDefinition,
) (int64, bool) {
	if source == nil || definition.Kind != dungeonDropItemEquipment {
		return 0, false
	}
	document, err := parseDungeonCardPVFDocument(source, definition.PVFPath)
	if err != nil {
		return 0, false
	}
	equipmentType, _ := document.Text("equipment type")
	if !strings.EqualFold(strings.TrimSpace(equipmentType), currentNameTagCardEquipmentType) {
		return 0, false
	}
	if days, ok := document.Int("visual duration"); ok && days > 0 {
		return days * 86400, true
	}
	return 30 * 86400, true
}

func applyCurrentNameTagCardAssets(
	character *dnfrepo.CharacterRecord,
	equipment *dnfrepo.EquipmentRecord,
	itemID uint32,
	durationSeconds int64,
	now time.Time,
) (currentNameTagCardActivation, error) {
	if character == nil || equipment == nil || itemID == 0 || durationSeconds <= 0 {
		return currentNameTagCardActivation{}, errCurrentCeraShopProductUnavailable
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if character.Stats == nil {
		character.Stats = make(map[string]int64)
	}
	previousItemID := character.Stats["name_tag_item_id"]
	previousExpire := character.Stats["name_tag_expire_time"]
	base := now.Unix()
	action := "new"
	if previousItemID == int64(itemID) && previousExpire > 0 {
		if previousExpire > base {
			base = previousExpire
		}
		action = "renew"
	} else if previousItemID != 0 && previousItemID != int64(itemID) {
		action = "replace"
	}
	expireAtUnix := base + durationSeconds
	expireAt := time.Unix(expireAtUnix, 0).UTC()
	character.Stats["name_tag_item_id"] = int64(itemID)
	character.Stats["name_tag_expire_time"] = expireAtUnix

	if equipment.Entries == nil {
		equipment.Entries = make(map[string]dnfrepo.EquipmentEntry)
	}
	slotKey := strconv.Itoa(int(currentNameTagEquipmentSlot))
	stack := dnfrepo.ItemStack{
		ItemID:   int64(itemID),
		Count:    1,
		Bind:     true,
		ExpireAt: expireAt,
		Extra: map[string]string{
			"item_kind":      "equipment",
			"instance_value": strconv.FormatUint(uint64(itemID), 10),
			"expire_time":    strconv.FormatInt(expireAtUnix, 10),
		},
	}
	itemEntry := currentItemListEntryFromStack(0, currentNameTagEquipmentSlot, stack)
	equipment.Entries[slotKey] = dnfrepo.EquipmentEntry{
		SlotIndex: currentNameTagEquipmentSlot,
		ItemID:    int64(itemID),
		Bind:      true,
		ExpireAt:  expireAt,
		RawEntry:  append([]byte(nil), itemEntry.data[:]...),
		Extra: map[string]string{
			"current_exe_equipment_type": strconv.Itoa(int(currentNameTagEquipmentSlot)),
			"current_exe_runtime_move":   "1",
			"equipped_slot":              strconv.Itoa(int(currentNameTagEquipmentSlot)),
			"name_tag_source":            "cera_shop_purchase",
			"instance_value":             strconv.FormatUint(uint64(itemID), 10),
			"expire_time":                strconv.FormatInt(expireAtUnix, 10),
		},
	}
	return currentNameTagCardActivation{
		ItemID:         itemID,
		PreviousItemID: previousItemID,
		PreviousExpire: previousExpire,
		ExpireAt:       expireAtUnix,
		Action:         action,
	}, nil
}

// currentCleanupExpiredNameTagCard resets name_tag_item_id if the card has
// expired. Called during character login initialization (PR #239).
func currentCleanupExpiredNameTagCard(character *dnfrepo.CharacterRecord) bool {
	if character == nil || character.Stats == nil {
		return false
	}
	expireTime := character.Stats["name_tag_expire_time"]
	itemID := character.Stats["name_tag_item_id"]
	if itemID == 0 || expireTime == 0 {
		return false
	}
	if currentNameTagExpireNow() < expireTime {
		return false
	}
	character.Stats["name_tag_item_id"] = 0
	character.Stats["name_tag_expire_time"] = 0
	return true
}

// currentNameTagExpireNow returns the current UTC unix timestamp for name tag
// expiry comparisons.
func currentNameTagExpireNow() int64 {
	return time.Now().UTC().Unix()
}

// currentIsNameTagItem checks whether the given item template ID is a name tag
// card by resolving its PVF equipment type.
func (s *Service) currentIsNameTagItem(itemID uint32) bool {
	if s == nil || itemID == 0 {
		return false
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return false
	}
	definition, err := catalog.ResolveItem(itemID)
	if err != nil || definition.Kind != dungeonDropItemEquipment {
		return false
	}
	doc, err := parseDungeonCardPVFDocument(catalog.source, definition.PVFPath)
	if err != nil {
		return false
	}
	equipType, _ := doc.Text("equipment type")
	return strings.EqualFold(strings.TrimSpace(equipType), currentNameTagCardEquipmentType)
}

// currentSendNameTagRefresh sends the current EXE's typed mode0 actor state
// after a name tag card purchase. The legacy subtype0/subtype1 userinfo pair is
// not compatible with NoPack.exe's class0/op2 reader: sub_2009160 owns mode0,
// and ReadAndApplyActorNameTagState (sub_2008D80) consumes the durable item ID
// and expiry from that mode0 tail to populate worn endpoint 30. The same body
// also carries appearance slot 28 for the town name decoration.
func (s *Service) currentSendNameTagRefresh(ctx context.Context, session *gameSession) error {
	if err := s.currentSendNameTagEquipmentSlotRefresh(ctx, session, "cera_shop_name_tag_before_mode0"); err != nil {
		return err
	}
	return s.currentSendNameTagActorRefresh(ctx, session)
}

// currentReapplyNameTagAfterMode1 repeats the actor endpoint only after the
// full mode1 equipment reader has created and attached worn slot 30. The first
// mode0 in the scene bootstrap necessarily runs before that object exists, so
// sub_2008D80 cannot bind the decoration on that first pass.
func (s *Service) currentReapplyNameTagAfterMode1(ctx context.Context, session *gameSession) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	itemID, _, found, err := s.loadCurrentSelectedActorNameTagState(ctx, session, session.selectedCharacterID)
	if err != nil || !found || itemID == 0 {
		return err
	}
	return s.currentSendNameTagActorRefresh(ctx, session)
}

func (s *Service) currentSendNameTagEquipmentSlotRefresh(ctx context.Context, session *gameSession, source string) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Equipment == nil {
		return nil
	}
	equipment, found, err := repositories.Equipment.Load(ctx, strconv.Itoa(int(session.selectedCharacterID)))
	if err != nil || !found {
		return err
	}
	equipped, occupied := equipment.Entries[strconv.Itoa(int(currentNameTagEquipmentSlot))]
	if !occupied || equipped.SlotIndex != currentNameTagEquipmentSlot || equipped.ItemID <= 0 {
		return nil
	}
	entry, rowOK := currentItemListEntryFromEquipment(equipped)
	if !rowOK {
		return fmt.Errorf("build name-tag slot %d item update", currentNameTagEquipmentSlot)
	}
	body := buildCurrentItemUpdateBody(currentSocketListEquipment, []currentItemListEntry{entry})
	s.logGameEvent(session, "game-name-tag-card-equipment-slot-refresh-sent",
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"list_type", currentSocketListEquipment,
		"slot", currentNameTagEquipmentSlot,
		"item_id", equipped.ItemID,
		"body_len", len(body),
		"sequence", "class1_purchase_ack_then_class0_op14_slot30_then_mode0_actor_endpoint")
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0)
}

func (s *Service) currentSendNameTagActorRefresh(ctx context.Context, session *gameSession) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	charID := session.selectedCharacterID
	loadedID, charName, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if loadedID != charID || !hasCharacter {
		return fmt.Errorf("load selected name-tag actor: requested=%d loaded=%d found=%t", charID, loadedID, hasCharacter)
	}
	ownerChannel := currentTownActorOwnerContext(session)
	if ownerChannel == currentSceneObjectContext {
		return fmt.Errorf("selected name-tag actor owner channel is not committed")
	}
	body, err := s.buildCurrentSceneObjectListBodyForSessionInContextStrict(
		ctx,
		session,
		charID,
		charName,
		character,
		hasCharacter,
		ownerChannel,
	)
	if err != nil {
		return fmt.Errorf("build selected name-tag mode0 refresh: %w", err)
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSetUDPIPPort), body, 0); err != nil {
		return err
	}

	peerCount := 0
	if s.onlinePlayers != nil {
		for _, peer := range s.onlinePlayers.PeersInSameArea(charID) {
			if peer.Session == nil || peer.Session.conn == nil {
				continue
			}
			peerOwner := currentTownRemoteActorOwnerContext(peer.Session, charID)
			if peerOwner == currentSceneObjectContext {
				s.logGameEvent(peer.Session, "game-name-tag-card-peer-refresh-skipped",
					"actor_char_id", charID,
					"reason", "peer_town_owner_not_committed")
				continue
			}
			peerBody, peerErr := s.buildCurrentSceneObjectListBodyForSessionInContextStrict(
				ctx,
				session,
				charID,
				charName,
				character,
				hasCharacter,
				peerOwner,
			)
			if peerErr == nil {
				peerErr = s.sendGameUpperRawClass(peer.Session, uint16(dnfenum.CmdPacketSetUDPIPPort), peerBody, 0)
			}
			if peerErr != nil {
				s.logWarn("dnfbridge deferred name-tag peer appearance refresh",
					"actor_char_id", charID,
					"peer_char_id", peer.CharacterID,
					"error", peerErr)
				continue
			}
			peerCount++
		}
	}

	s.logGameEvent(session, "game-name-tag-card-refresh-sent",
		"char_id", charID,
		"msg_id", uint16(dnfenum.CmdPacketSetUDPIPPort),
		"mode", 0,
		"owner_channel", ownerChannel,
		"mode0_len", len(body),
		"peer_refresh_count", peerCount,
		"body_source", "current_exe_mode0_name_tag_endpoint30_and_appearance28")
	return nil
}
