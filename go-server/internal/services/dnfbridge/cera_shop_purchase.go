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

	dnfcargo "longheng.io/server/internal/modules/dnf/cargo"
	dnfcerashop "longheng.io/server/internal/modules/dnf/cerashop"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentCeraShopRequestHeaderSize = 4
	currentCeraShopRequestItemStride = 15
	currentCeraShopRequestMaxItems   = 64
	currentCeraShopUpdateMsgID       = 0x0035

	currentCeraShopPaymentModeCera         byte = 0
	currentCeraShopPaymentModeAvatarCoupon byte = 1

	// Current NoPack sub_232A9F0 groups list 7 as pet bodies 0..139,
	// pet equipment 140..188, and pet consumables 189..238 (inclusive).
	currentCeraShopPetConsumableSlotStart int16 = 189
	currentCeraShopPetConsumableSlotEnd   int16 = 238
)

var (
	errCurrentCeraShopRequestMalformed   = errors.New("dnf cera shop purchase request is malformed")
	errCurrentCeraShopPaymentUnsupported = errors.New("dnf cera shop payment mode is unsupported")
	errCurrentCeraShopCatalogUnavailable = errors.New("dnf cera shop catalog is unavailable")
	errCurrentCeraShopProductUnavailable = errors.New("dnf cera shop product is unavailable")
	errCurrentCeraShopOwnerUnavailable   = errors.New("dnf cera shop selected character is unavailable")
	errCurrentCeraShopCeraInsufficient   = errors.New("dnf cera shop cera balance is insufficient")
)

// currentCeraShopPurchaseRequest follows the current client's sendBuyPacket
// body.  The first cart row starts at byte 4, has a 15-byte stride, and owns
// its commodity number at +3.  Byte 4 is the client payment selector while
// byte 5 is the row's avatar-attribute selector; the two deliberately remain
// distinct even though the first row begins at byte 4.
type currentCeraShopPurchaseRequest struct {
	PaymentMode byte
	Items       []currentCeraShopPurchaseRequestItem
}

type currentCeraShopPurchaseRequestItem struct {
	CommodityID    uint32
	AttributeValue byte
}

type currentCeraShopCommitResult struct {
	Products                 []currentCeraShopProduct
	Updates                  []currentCeraShopItemUpdate
	MainInventoryExpansion   uint16
	PersonalCargoSlotCount   uint16
	AccountCargoSelectionKey uint16
	PetInventoryChanged      bool
	CeraAfter                int64
	PremiumActivations       []currentCeraShopPremiumActivation
	NameTagActivation        *currentNameTagCardActivation
}

// currentCeraShopPremiumActivation is one premium contract activated at
// checkout: the premium type (580+slot for devil perks) and its remaining
// seconds, used for the class0/op66 activation notification.
type currentCeraShopPremiumActivation struct {
	PremiumType      int64
	RemainingSeconds int64
}

// currentCeraShopItemUpdate is an actual changed inventory container.  A
// Cera package belongs in list 0, while a direct avatar product belongs in
// list 1.  Keep the container with the changed rows so a list-0 item update
// can never make a client interpret an avatar entry as a normal bag row.
type currentCeraShopItemUpdate struct {
	ListType byte
	Entries  []currentItemListEntry
}

type currentCeraShopResolvedPurchase struct {
	Product                    currentCeraShopProduct
	AttributeValue             byte
	MainInventoryUpgradeStage  uint16
	PersonalCargoUpgradeTarget uint16
	AccountCargoUpgrade        bool
	AvatarCouponItemID         int64
	// PremiumType > 0 means the purchase is consumed at checkout and activates
	// this account-level premium contract instead of entering any container
	// (86JP ConsumedOnPurchase). PremiumPackage activates every 魔王契约 devil
	// slot for the same duration ([charac premium package]).
	PremiumType            int64
	PremiumDurationSeconds int64
	PremiumPackage         bool
	NameTagDurationSeconds int64
}

type currentCeraShopChangedSlot struct {
	ListType byte
	Slot     int16
	Product  currentCeraShopProduct
	// Bonus marks a slot filled by a one-plus-one IPG gift
	// (etc/chn_cerashop_multibonus.etc) rather than by the purchased product
	// itself; Product keeps the parent commodity for provenance while
	// Product.ItemID is the granted bonus item.
	Bonus bool
}

func parseCurrentCeraShopPurchaseRequest(body []byte) (currentCeraShopPurchaseRequest, error) {
	if len(body) < currentCeraShopRequestHeaderSize+currentCeraShopRequestItemStride {
		return currentCeraShopPurchaseRequest{}, errCurrentCeraShopRequestMalformed
	}
	count := int(body[2])
	if count == 0 {
		count = 1
	}
	if count < 1 || count > currentCeraShopRequestMaxItems ||
		len(body) < currentCeraShopRequestHeaderSize+count*currentCeraShopRequestItemStride {
		return currentCeraShopPurchaseRequest{}, errCurrentCeraShopRequestMalformed
	}
	request := currentCeraShopPurchaseRequest{
		PaymentMode: body[4],
		Items:       make([]currentCeraShopPurchaseRequestItem, 0, count),
	}
	for index := 0; index < count; index++ {
		itemOffset := currentCeraShopRequestHeaderSize + index*currentCeraShopRequestItemStride
		commodityID := binary.LittleEndian.Uint32(body[itemOffset+3 : itemOffset+7])
		if commodityID == 0 {
			return currentCeraShopPurchaseRequest{}, errCurrentCeraShopRequestMalformed
		}
		request.Items = append(request.Items, currentCeraShopPurchaseRequestItem{
			CommodityID:    commodityID,
			AttributeValue: body[itemOffset+1],
		})
	}
	return request, nil
}

func (s *Service) handleCurrentCeraShopPurchase(session *gameSession, body []byte) error {
	request, err := parseCurrentCeraShopPurchaseRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-cera-shop-purchase-rejected", "body_len", len(body), "reason", err)
		return s.sendCurrentCeraShopPurchaseFailure(session)
	}
	if request.PaymentMode != currentCeraShopPaymentModeCera && request.PaymentMode != currentCeraShopPaymentModeAvatarCoupon {
		s.logGameEvent(session, "game-cera-shop-purchase-rejected",
			"body_len", len(body), "item_count", len(request.Items), "payment_mode", request.PaymentMode,
			"reason", errCurrentCeraShopPaymentUnsupported)
		return s.sendCurrentCeraShopPurchaseFailure(session)
	}
	catalog, err := s.currentCeraShopCatalog()
	if err != nil {
		s.logGameEvent(session, "game-cera-shop-purchase-rejected", "body_len", len(body), "reason", err)
		return s.sendCurrentCeraShopPurchaseFailure(session)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := s.commitCurrentCeraShopPurchase(ctx, session, catalog, request)
	if err != nil {
		s.logGameEvent(session, "game-cera-shop-purchase-rejected",
			"body_len", len(body), "item_count", len(request.Items), "reason", err)
		return s.sendCurrentCeraShopPurchaseFailure(session)
	}

	if result.NameTagActivation != nil {
		activation := result.NameTagActivation
		s.logGameEvent(session, "game-name-tag-card-applied",
			"char_id", session.selectedCharacterID,
			"action", activation.Action,
			"item_id", activation.ItemID,
			"prev_item_id", activation.PreviousItemID,
			"expire_time", activation.ExpireAt,
			"prev_expire_time", activation.PreviousExpire)
	}

	for _, product := range result.Products {
		if err := s.sendGameUpperRawClass(
			session,
			uint16(dnfenum.CmdPacketBuyCerashopItem),
			buildCurrentCeraShopPurchaseSuccessBodyWithCount(product.CommodityID, product.Count, catalog.bonusByCommodity[product.CommodityID]...),
			dnfproto.DefaultChannelClassification,
		); err != nil {
			return err
		}
	}
	// The current client commits the Cera-shop result before it applies the
	// typed actor refresh. Sending mode0 before the class1/op64 success ACK can
	// race the shop UI owner and leave endpoint 30 empty until the next login.
	if result.NameTagActivation != nil {
		if err := s.currentSendNameTagRefresh(ctx, session); err != nil {
			s.logGameEvent(session, "game-name-tag-card-refresh-failed", "err", err)
		}
	}
	notifiedPremiumActivations := 0
	devilPremiumActivated := false
	crystalPremiumActivated := false
	// Current EXE class0/op66 (sub_1D61460, sub-op 2) reads a one-byte premium
	// type. Ordinary PVF premium types use that route. Devil service slots are
	// internal account types 580..587; sending them here truncates the type to
	// 68..75 and displays unrelated Korean tarot-card effects.
	for _, activation := range result.PremiumActivations {
		if premium.IsDevilSlotType(activation.PremiumType) {
			devilPremiumActivated = true
		}
		if activation.PremiumType == premium.TypeCrystal {
			crystalPremiumActivated = true
		}
		if !premium.CanNotifyActivation(activation.PremiumType) {
			continue
		}
		if err := s.sendGameUpperRawClass(
			session,
			currentPremiumActivatedMsgID,
			buildCurrentPremiumActivatedBody(activation.PremiumType, activation.RemainingSeconds),
			0,
		); err != nil {
			return err
		}
		notifiedPremiumActivations++
	}
	if crystalPremiumActivated {
		// Current sub_1D61460 records premium type 97, but the crystal panel is
		// rebuilt by the raw class0/op898 handler sub_1E7FAD0. Without this
		// immediate follow-up a purchase made after scene bootstrap remains
		// locally disabled and the client never emits its op535 selection.
		if err := s.sendCurrentCrystalContractState(session, "cera_shop_purchase_after_crystal_contract_activation"); err != nil {
			return err
		}
	}
	for _, update := range result.Updates {
		if len(update.Entries) == 0 {
			continue
		}
		body := buildCurrentItemUpdateBody(update.ListType, update.Entries)
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0); err != nil {
			return err
		}
	}
	if result.MainInventoryExpansion != 0 {
		mainBody, _, _, ok := s.buildCurrentItemListBodyForSession(ctx, session, currentExeInventoryMainListType)
		if !ok {
			return errCurrentCeraShopOwnerUnavailable
		}
		if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), mainBody); err != nil {
			return err
		}
	}
	if result.PersonalCargoSlotCount != 0 {
		cargoBody, _, _, ok := s.buildCurrentItemListBodyForSession(ctx, session, currentExeInventoryPersonalCargoListType)
		if !ok {
			return errCurrentCeraShopOwnerUnavailable
		}
		if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), cargoBody); err != nil {
			return err
		}
	}
	if result.AccountCargoSelectionKey != 0 {
		cargoBody, _, _, ok := s.buildCurrentItemListBodyForSession(ctx, session, 12)
		if !ok {
			return errCurrentCeraShopOwnerUnavailable
		}
		if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), cargoBody); err != nil {
			return err
		}
	}
	if result.PetInventoryChanged {
		petBody, _, _, ok := s.buildCurrentItemListBodyForSession(ctx, session, currentPetInventoryListType)
		if !ok {
			return errCurrentCeraShopOwnerUnavailable
		}
		if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), petBody); err != nil {
			return err
		}
	}
	if devilPremiumActivated {
		if err := s.sendCurrentPremiumServiceState(session, "cera_shop_purchase_after_devil_contract_activation"); err != nil {
			return err
		}
	}
	if err := s.sendGameUpperRawClass(session, currentCeraShopUpdateMsgID, buildCurrentCeraShopBalanceBody(result.CeraAfter), 0); err != nil {
		return err
	}
	s.logGameEvent(session, "game-cera-shop-purchase-committed",
		"item_count", len(result.Products), "container_update_count", len(result.Updates), "main_inventory_expansion", result.MainInventoryExpansion, "personal_cargo_slot_count", result.PersonalCargoSlotCount, "account_cargo_selection_key", result.AccountCargoSelectionKey, "cera_after", result.CeraAfter, "premium_activation_count", len(result.PremiumActivations),
		"premium_notification_count", notifiedPremiumActivations,
		"crystal_state_refreshed", crystalPremiumActivated,
		"ack_body", "current_exe_op64_sub_CD9490", "item_update_body", "current_exe_op14_raw77", "cera_body", "current_exe_op53_u8_u32_u32_u32", "premium_body", "current_exe_class0_op66_u8_types_only_internal_devil_slots_suppressed")
	return nil
}

func (s *Service) commitCurrentCeraShopPurchase(
	ctx context.Context,
	session *gameSession,
	catalog *pvfCeraShopCatalog,
	request currentCeraShopPurchaseRequest,
) (currentCeraShopCommitResult, error) {
	if s == nil {
		return currentCeraShopCommitResult{}, errCurrentCeraShopOwnerUnavailable
	}
	// Settings records are routed by their scope while wallet/inventory rows
	// are routed by character id.  Those keys can live on different database
	// shards, so serialize this checkout owner and compensate the saved header
	// if the character settlement rejects; do not pretend the two shards share
	// one SQL transaction.
	s.ceraShopPurchaseMu.Lock()
	defer s.ceraShopPurchaseMu.Unlock()
	if session == nil || session.selectedCharacterID == 0 || catalog == nil || catalog.items == nil || len(request.Items) == 0 {
		return currentCeraShopCommitResult{}, errCurrentCeraShopOwnerUnavailable
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.CharacterSettlement == nil || repositories.RentalAssets == nil {
		return currentCeraShopCommitResult{}, errCurrentCeraShopOwnerUnavailable
	}
	purchases := make([]currentCeraShopResolvedPurchase, 0, len(request.Items))
	var totalCost uint64
	switch request.PaymentMode {
	case currentCeraShopPaymentModeCera:
	case currentCeraShopPaymentModeAvatarCoupon:
		if len(request.Items) != 1 {
			return currentCeraShopCommitResult{}, fmt.Errorf("%w: avatar coupon checkout must contain exactly one item", errCurrentCeraShopPaymentUnsupported)
		}
	default:
		return currentCeraShopCommitResult{}, errCurrentCeraShopPaymentUnsupported
	}
	// Any purchased item can be a premiumlist contract item (86JP
	// IsContractItem). The premium catalog is loaded lazily; both catalogs
	// come from the same runtime archive in production. The two contract
	// sections are meaningless without it and fail closed, while ordinary
	// sections tolerate a load failure (logged) and keep their normal
	// delivery path.
	var premiumCatalog *currentPremiumCatalog
	premiumCatalogFailed := false
	loadPremiumCatalog := func() error {
		if premiumCatalog != nil {
			return nil
		}
		loaded, err := s.currentPremiumCatalog()
		if err != nil {
			return err
		}
		premiumCatalog = loaded
		return nil
	}
	for _, requestItem := range request.Items {
		product, found := catalog.Product(requestItem.CommodityID)
		if !found {
			return currentCeraShopCommitResult{}, fmt.Errorf("%w: commodity=%d", errCurrentCeraShopProductUnavailable, requestItem.CommodityID)
		}
		product, err := catalog.resolvePurchase(product)
		if err != nil {
			return currentCeraShopCommitResult{}, err
		}
		resolved := currentCeraShopResolvedPurchase{Product: product, AttributeValue: requestItem.AttributeValue}
		if request.PaymentMode == currentCeraShopPaymentModeAvatarCoupon {
			couponItemID, err := catalog.resolveAvatarCouponItemID(product)
			if err != nil {
				return currentCeraShopCommitResult{}, err
			}
			resolved.AvatarCouponItemID = couponItemID
		} else {
			if uint64(product.CeraPrice) > math.MaxUint64-totalCost {
				return currentCeraShopCommitResult{}, errCurrentCeraShopProductUnavailable
			}
			totalCost += uint64(product.CeraPrice)
		}
		switch {
		case product.Section == "selectable character premium":
			if err := loadPremiumCatalog(); err != nil {
				return currentCeraShopCommitResult{}, fmt.Errorf("%w: premium catalog: %v", errCurrentCeraShopProductUnavailable, err)
			}
			slot, ok := premiumCatalog.devilSlots[product.CommodityID]
			if !ok {
				return currentCeraShopCommitResult{}, fmt.Errorf("%w: commodity=%d is not a purchasable devil contract slot", errCurrentCeraShopProductUnavailable, product.CommodityID)
			}
			resolved.PremiumType = premium.DevilSlotType(slot.Slot)
			resolved.PremiumDurationSeconds = slot.Days * 86400
			purchases = append(purchases, resolved)
			continue
		case product.Section == "charac premium package":
			if product.PremiumPackageDays == 0 {
				return currentCeraShopCommitResult{}, fmt.Errorf("%w: commodity=%d has no contract day count", errCurrentCeraShopProductUnavailable, product.CommodityID)
			}
			resolved.PremiumPackage = true
			resolved.PremiumDurationSeconds = int64(product.PremiumPackageDays) * 86400
			purchases = append(purchases, resolved)
			continue
		}
		if err := loadPremiumCatalog(); err != nil {
			if !premiumCatalogFailed {
				premiumCatalogFailed = true
				s.logPacketEvent("game-cera-shop-premium-catalog-unavailable", "error", err)
			}
		} else if contract, ok := premiumCatalog.contractsByItem[int64(product.ItemID)]; ok {
			resolved.PremiumType = contract.PremiumType
			resolved.PremiumDurationSeconds = contract.DurationSeconds
			purchases = append(purchases, resolved)
			continue
		}
		definition, definitionErr := catalog.items.ResolveItem(product.ItemID)
		if definitionErr != nil {
			return currentCeraShopCommitResult{}, fmt.Errorf("%w: commodity=%d item=%d: %v", errCurrentCeraShopProductUnavailable, product.CommodityID, product.ItemID, definitionErr)
		}
		if durationSeconds, isNameTag := currentNameTagCardDuration(catalog.source, definition); isNameTag {
			resolved.NameTagDurationSeconds = durationSeconds
			purchases = append(purchases, resolved)
			continue
		}
		if stage, isMainInventoryUpgrade := currentCeraShopMainInventoryUpgradeStage(definition); isMainInventoryUpgrade {
			if stage == 0 {
				return currentCeraShopCommitResult{}, fmt.Errorf("%w: commodity=%d item=%d has an unsupported main inventory upgrade tier", errCurrentCeraShopProductUnavailable, product.CommodityID, product.ItemID)
			}
			if product.Count != 1 {
				return currentCeraShopCommitResult{}, fmt.Errorf("%w: commodity=%d main inventory upgrades require count=1", errCurrentCeraShopProductUnavailable, product.CommodityID)
			}
			resolved.MainInventoryUpgradeStage = stage
		}
		if target, isPersonalCargoUpgrade := catalog.personalCargoUpgradeTarget(definition); isPersonalCargoUpgrade {
			if target == 0 {
				return currentCeraShopCommitResult{}, fmt.Errorf("%w: commodity=%d item=%d has an unsupported personal cargo upgrade tier", errCurrentCeraShopProductUnavailable, product.CommodityID, product.ItemID)
			}
			if product.Count != 1 {
				return currentCeraShopCommitResult{}, fmt.Errorf("%w: commodity=%d personal cargo upgrades require count=1", errCurrentCeraShopProductUnavailable, product.CommodityID)
			}
			resolved.PersonalCargoUpgradeTarget = target
		}
		if catalog.isAccountCargoUpgradeTool(definition) {
			if product.Count != 1 {
				return currentCeraShopCommitResult{}, fmt.Errorf("%w: commodity=%d account cargo upgrades require count=1", errCurrentCeraShopProductUnavailable, product.CommodityID)
			}
			resolved.AccountCargoUpgrade = true
		}
		purchases = append(purchases, resolved)
	}
	if totalCost > math.MaxInt64 {
		return currentCeraShopCommitResult{}, errCurrentCeraShopProductUnavailable
	}

	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	checkoutOwner, err := dnfcerashop.NewOwner(repositories)
	if err != nil {
		return currentCeraShopCommitResult{}, errCurrentCeraShopOwnerUnavailable
	}
	accountCargoUpgrades := 0
	for _, purchase := range purchases {
		if purchase.AccountCargoUpgrade {
			accountCargoUpgrades++
		}
	}
	if accountCargoUpgrades != 0 {
		if accountCargoUpgrades != 1 || len(purchases) != 1 {
			return currentCeraShopCommitResult{}, fmt.Errorf("%w: account cargo upgrade must be purchased alone", errCurrentCeraShopProductUnavailable)
		}
		return commitCurrentCeraShopAccountCargoUpgrade(ctx, checkoutOwner, accountID, characterID, purchases[0], int64(totalCost), s.gameplayNow())
	}
	var result currentCeraShopCommitResult
	checkoutNow := s.gameplayNow().UTC()
	checkoutResult, err := checkoutOwner.Checkout(ctx, dnfcerashop.CheckoutCommand{
		AccountID:     accountID,
		CharacterID:   characterID,
		SettingsScope: dnfrepo.CharacterContainerStateScope(characterID),
		Cost:          int64(totalCost),
		UpdatedAt:     checkoutNow,
		Project: func(assets *dnfcerashop.CheckoutAssets) (dnfcerashop.CheckoutChanges, error) {
			if assets == nil || assets.Account == nil || assets.Character == nil || assets.Inventory == nil ||
				assets.Equipment == nil || assets.Settings == nil {
				return dnfcerashop.CheckoutChanges{}, errCurrentCeraShopOwnerUnavailable
			}
			containerStateAfter, mainInventoryExpansion, personalCargoSlotCount, hasContainerUpgrade, err :=
				currentCeraShopPrepareContainerUpgrades(
					assets.Settings,
					assets.SettingsFound,
					characterID,
					purchases,
					checkoutNow,
				)
			if err != nil {
				return dnfcerashop.CheckoutChanges{}, err
			}
			changes := dnfcerashop.CheckoutChanges{Settings: hasContainerUpgrade}
			if hasContainerUpgrade {
				*assets.Settings = containerStateAfter
			}
			account := assets.Account
			character := assets.Character
			inventory := assets.Inventory
			equipment := assets.Equipment
			changedSlots := make(map[string]currentCeraShopChangedSlot)
			entriesByList := make(map[byte][]currentItemListEntry)
			premiumActivations := make([]currentCeraShopPremiumActivation, 0, len(purchases))
			petInventoryChanged := false
			inventoryChanged := false
			var nameTagActivation *currentNameTagCardActivation
			premiumNow := checkoutNow
			for _, purchase := range purchases {
				product := purchase.Product
				if purchase.AvatarCouponItemID != 0 {
					entry, err := consumeCurrentCeraShopAvatarCoupon(inventory, purchase.AvatarCouponItemID)
					if err != nil {
						return dnfcerashop.CheckoutChanges{}, err
					}
					entriesByList[dnfrepo.MainInventoryListType] = append(entriesByList[dnfrepo.MainInventoryListType], entry)
					inventoryChanged = true
				}
				if purchase.MainInventoryUpgradeStage != 0 || purchase.PersonalCargoUpgradeTarget != 0 {
					// Container upgrades are consumed at checkout. They have no
					// inventory row: the real effect is the persisted op13 header
					// state prepared above.
					continue
				}
				if purchase.PremiumPackage {
					// [charac premium package]: consumed at checkout; every 魔王契约
					// devil slot renews for the package day count (86JP
					// ConsumedOnPurchase + UpsertPremiumExpire per slot).
					for devilSlot := int64(0); devilSlot < premium.DevilSlotCount; devilSlot++ {
						premiumType := premium.DevilSlotType(devilSlot)
						premium.Upsert(account, premiumType, purchase.PremiumDurationSeconds, 1, premiumNow)
						premiumActivations = append(premiumActivations, currentCeraShopPremiumActivation{
							PremiumType:      premiumType,
							RemainingSeconds: premium.ExpireAt(*account, premiumType) - premiumNow.Unix(),
						})
					}
					continue
				}
				if purchase.PremiumType > 0 {
					// Devil contract slot or premiumlist contract item: consumed at
					// checkout, account-level premium renews; nothing enters a bag.
					premium.Upsert(account, purchase.PremiumType, purchase.PremiumDurationSeconds, 1, premiumNow)
					premiumActivations = append(premiumActivations, currentCeraShopPremiumActivation{
						PremiumType:      purchase.PremiumType,
						RemainingSeconds: premium.ExpireAt(*account, purchase.PremiumType) - premiumNow.Unix(),
					})
					continue
				}
				if purchase.NameTagDurationSeconds > 0 {
					if nameTagActivation == nil {
						activation, err := applyCurrentNameTagCardAssets(
							character,
							equipment,
							product.ItemID,
							purchase.NameTagDurationSeconds,
							premiumNow,
						)
						if err != nil {
							return dnfcerashop.CheckoutChanges{}, err
						}
						nameTagActivation = &activation
						changes.Character = true
						changes.Equipment = true
					}
					continue
				}
				definition, err := catalog.items.ResolveItem(product.ItemID)
				if err != nil {
					return dnfcerashop.CheckoutChanges{}, fmt.Errorf("%w: commodity=%d item=%d: %v", errCurrentCeraShopProductUnavailable, product.CommodityID, product.ItemID, err)
				}
				definition, err = currentCeraShopProductDefinitionForGrantAt(definition, product, premiumNow)
				if err != nil {
					return dnfcerashop.CheckoutChanges{}, fmt.Errorf("%w: commodity=%d item=%d expiration: %v", errCurrentCeraShopProductUnavailable, product.CommodityID, product.ItemID, err)
				}
				if product.Section == "avatar" {
					slot, err := grantCurrentCeraShopAvatar(inventory, catalog.source, definition, product, purchase.AttributeValue)
					if err != nil {
						return dnfcerashop.CheckoutChanges{}, err
					}
					change := currentCeraShopChangedSlot{ListType: 1, Slot: slot, Product: product}
					changedSlots[currentCeraShopInventorySlotKey(change.ListType, change.Slot)] = change
					inventoryChanged = true
					continue
				}
				if isCurrentCeraShopCreatureItem(definition) {
					slot, err := grantCurrentCeraShopPet(inventory, definition)
					if err != nil {
						return dnfcerashop.CheckoutChanges{}, err
					}
					change := currentCeraShopChangedSlot{ListType: currentPetInventoryListType, Slot: slot, Product: product}
					changedSlots[currentCeraShopInventorySlotKey(change.ListType, change.Slot)] = change
					petInventoryChanged = true
					inventoryChanged = true
					continue
				}
				if isCurrentCeraShopPetConsumable(definition) {
					slots, err := grantCurrentCeraShopPetConsumable(inventory, definition, product.Count)
					if err != nil {
						return dnfcerashop.CheckoutChanges{}, err
					}
					for _, slot := range slots {
						change := currentCeraShopChangedSlot{ListType: currentPetInventoryListType, Slot: slot, Product: product}
						changedSlots[currentCeraShopInventorySlotKey(change.ListType, change.Slot)] = change
					}
					petInventoryChanged = true
					inventoryChanged = true
					continue
				}
				slots, err := grantCurrentCeraShopProduct(inventory, definition, product.Count)
				if err != nil {
					return dnfcerashop.CheckoutChanges{}, err
				}
				inventoryChanged = true
				for _, slot := range slots {
					change := currentCeraShopChangedSlot{ListType: dnfrepo.MainInventoryListType, Slot: int16(slot), Product: product}
					changedSlots[currentCeraShopInventorySlotKey(change.ListType, change.Slot)] = change
				}
				// One-plus-one IPG gifts: absolute per-purchase counts from the
				// runtime multibonus document, granted into the same container and
				// transaction as the purchased product.
				for _, bonus := range catalog.bonusByCommodity[product.CommodityID] {
					bonusDefinition, err := catalog.items.ResolveItem(bonus.ItemID)
					if err != nil {
						return dnfcerashop.CheckoutChanges{}, fmt.Errorf("%w: commodity=%d bonus item=%d: %v", errCurrentCeraShopProductUnavailable, product.CommodityID, bonus.ItemID, err)
					}
					bonusDefinition, err = currentPVFItemDefinitionForGrantAt(bonusDefinition, premiumNow)
					if err != nil {
						return dnfcerashop.CheckoutChanges{}, fmt.Errorf("%w: commodity=%d bonus item=%d expiration: %v", errCurrentCeraShopProductUnavailable, product.CommodityID, bonus.ItemID, err)
					}
					bonusSlots, err := grantCurrentCeraShopProduct(inventory, bonusDefinition, bonus.Count)
					if err != nil {
						return dnfcerashop.CheckoutChanges{}, err
					}
					inventoryChanged = true
					bonusProduct := currentCeraShopProduct{CommodityID: product.CommodityID, ItemID: bonus.ItemID, Section: product.Section}
					for _, slot := range bonusSlots {
						change := currentCeraShopChangedSlot{ListType: dnfrepo.MainInventoryListType, Slot: int16(slot), Product: bonusProduct, Bonus: true}
						changedSlots[currentCeraShopInventorySlotKey(change.ListType, change.Slot)] = change
					}
				}
			}

			for _, change := range changedSlots {
				key := currentCeraShopInventorySlotKey(change.ListType, change.Slot)
				stack, found := inventory.Slots[key]
				if !found || stack.ItemID <= 0 || (change.ListType != 1 && stack.Count <= 0) {
					return dnfcerashop.CheckoutChanges{}, errCurrentCeraShopProductUnavailable
				}
				entry := currentItemListEntryFromStack(change.ListType, change.Slot, stack)
				stack.RawEntry = append([]byte(nil), entry.data[:]...)
				if stack.Extra == nil {
					stack.Extra = make(map[string]string, 5)
				}
				stack.Extra["last_grant_source"] = "cera_shop"
				stack.Extra["last_cera_shop_commodity"] = strconv.FormatUint(uint64(change.Product.CommodityID), 10)
				stack.Extra["last_cera_shop_section"] = change.Product.Section
				if change.Bonus {
					stack.Extra["last_cera_shop_bonus"] = "one_plus_one_ipg"
				}
				if change.Product.DurationDays > 0 {
					stack.Extra["cera_shop_duration_days"] = strconv.FormatUint(uint64(change.Product.DurationDays), 10)
					stack.Extra["expiration_source"] = "runtime_pvf_cera_shop_duration_grant"
				}
				inventory.Slots[key] = stack
				entriesByList[change.ListType] = append(entriesByList[change.ListType], entry)
			}
			updates := make([]currentCeraShopItemUpdate, 0, len(entriesByList))
			for _, listType := range []byte{dnfrepo.MainInventoryListType, 1} {
				entries := entriesByList[listType]
				if len(entries) == 0 {
					continue
				}
				sort.Slice(entries, func(left, right int) bool {
					return binary.LittleEndian.Uint16(entries[left].data[0:2]) < binary.LittleEndian.Uint16(entries[right].data[0:2])
				})
				updates = append(updates, currentCeraShopItemUpdate{ListType: listType, Entries: entries})
			}
			products := make([]currentCeraShopProduct, 0, len(purchases))
			for _, purchase := range purchases {
				products = append(products, purchase.Product)
			}
			result = currentCeraShopCommitResult{
				Products:               products,
				Updates:                updates,
				MainInventoryExpansion: mainInventoryExpansion,
				PersonalCargoSlotCount: personalCargoSlotCount,
				PetInventoryChanged:    petInventoryChanged,
				PremiumActivations:     premiumActivations,
				NameTagActivation:      nameTagActivation,
			}
			changes.Inventory = inventoryChanged
			return changes, nil
		},
	})
	if err != nil {
		return currentCeraShopCommitResult{}, err
	}
	result.CeraAfter = checkoutResult.CeraAfter
	return result, nil
}

func commitCurrentCeraShopAccountCargoUpgrade(
	ctx context.Context,
	owner *dnfcerashop.Owner,
	accountID string,
	characterID string,
	purchase currentCeraShopResolvedPurchase,
	cost int64,
	now time.Time,
) (currentCeraShopCommitResult, error) {
	if owner == nil || accountID == "" || characterID == "" || cost < 0 {
		return currentCeraShopCommitResult{}, errCurrentCeraShopOwnerUnavailable
	}
	var result currentCeraShopCommitResult
	checkout, err := owner.Checkout(ctx, dnfcerashop.CheckoutCommand{
		AccountID:     accountID,
		CharacterID:   characterID,
		SettingsScope: dnfrepo.CharacterContainerStateScope(characterID),
		Cost:          cost,
		UpdatedAt:     now,
		Project: func(assets *dnfcerashop.CheckoutAssets) (dnfcerashop.CheckoutChanges, error) {
			if assets == nil || assets.Account == nil || assets.Character == nil ||
				assets.Character.Stats == nil {
				return dnfcerashop.CheckoutChanges{}, errCurrentCeraShopOwnerUnavailable
			}
			account := assets.Account
			if account.Metadata == nil {
				account.Metadata = make(map[string]string, 4)
			}
			previous, err := strconv.ParseInt(strings.TrimSpace(account.Metadata["account_cargo_level"]), 10, 64)
			if err != nil || previous <= 0 {
				return dnfcerashop.CheckoutChanges{}, errCurrentCeraShopProductUnavailable
			}
			next, ok := dnfcargo.NextAccountCargoTier(previous)
			if !ok || next <= previous || next > math.MaxUint16 {
				return dnfcerashop.CheckoutChanges{}, errCurrentCeraShopProductUnavailable
			}
			account.Metadata["account_cargo_created"] = "true"
			account.Metadata["account_cargo_level"] = strconv.FormatInt(next, 10)
			if strings.TrimSpace(account.Metadata["account_cargo_gold"]) == "" {
				account.Metadata["account_cargo_gold"] = "0"
			}
			result = currentCeraShopCommitResult{
				Products:                 []currentCeraShopProduct{purchase.Product},
				AccountCargoSelectionKey: uint16(next),
			}
			return dnfcerashop.CheckoutChanges{}, nil
		},
	})
	if err != nil {
		return currentCeraShopCommitResult{}, err
	}
	result.CeraAfter = checkout.CeraAfter
	return result, nil
}
