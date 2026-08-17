package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/premium"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func currentPremiumInventoryContracts(catalog *currentPremiumCatalog) map[int64]premium.InventoryContract {
	if catalog == nil || len(catalog.contractsByItem) == 0 {
		return nil
	}
	contracts := make(map[int64]premium.InventoryContract, len(catalog.contractsByItem))
	for itemID, info := range catalog.contractsByItem {
		contracts[itemID] = premium.InventoryContract{
			ItemID:          info.ItemID,
			PremiumType:     info.PremiumType,
			DurationSeconds: info.DurationSeconds,
		}
	}
	return contracts
}

// activateCurrentPremiumInventoryContracts is the common acquisition
// boundary for premiumlist contract items. The grant transaction may briefly
// persist a recoverable item row, but before any item-list projection reaches
// the client this transaction atomically removes every list-0 contract stack
// and extends the account entitlement by PVF duration * stack count.
func (s *Service) activateCurrentPremiumInventoryContracts(
	ctx context.Context,
	session *gameSession,
) (premium.InventoryActivationResult, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return premium.InventoryActivationResult{}, fmt.Errorf("premium inventory activation requires a selected character")
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.RentalAssets == nil || repositories.Inventory == nil {
		return premium.InventoryActivationResult{}, fmt.Errorf("premium inventory activation repositories are unavailable")
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	if accountID == "" {
		return premium.InventoryActivationResult{}, fmt.Errorf("premium inventory activation account is unavailable")
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	catalog, err := s.currentPremiumCatalog()
	if err != nil {
		return premium.InventoryActivationResult{}, err
	}
	contracts := currentPremiumInventoryContracts(catalog)
	if len(contracts) == 0 {
		return premium.InventoryActivationResult{}, nil
	}
	now := s.gameplayNow().UTC()
	var result premium.InventoryActivationResult
	err = repositories.RentalAssets.WithinRentalAssets(
		ctx,
		accountID,
		characterID,
		func(accounts dnfrepo.AccountRepository, _ dnfrepo.CharacterRepository, inventoryRepo dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
			account, found, loadErr := accounts.Load(ctx, accountID)
			if loadErr != nil {
				return loadErr
			}
			if !found {
				return fmt.Errorf("premium inventory activation account %q not found", accountID)
			}
			inventory, found, loadErr := inventoryRepo.Load(ctx, characterID)
			if loadErr != nil {
				return loadErr
			}
			if !found {
				return fmt.Errorf("premium inventory activation inventory %q not found", characterID)
			}
			account = dnfrepo.CloneAccount(account)
			inventory = dnfrepo.CloneInventory(inventory)
			result, loadErr = premium.ActivateInventoryContracts(&account, &inventory, contracts, now)
			if loadErr != nil || !result.Changed() {
				return loadErr
			}
			account.UpdatedAt = now
			inventory.UpdatedAt = now
			if saveErr := accounts.Save(ctx, account); saveErr != nil {
				return saveErr
			}
			return dnfrepo.SaveInventoryFields(ctx, inventoryRepo, inventory, dnfrepo.InventoryFieldSlots)
		},
	)
	return result, err
}

func (s *Service) logCurrentPremiumInventoryActivation(
	session *gameSession,
	result premium.InventoryActivationResult,
	source string,
) {
	if !result.Changed() {
		return
	}
	s.logGameEvent(session, "game-premium-contract-inventory-auto-activated",
		"source", source,
		"char_id", session.selectedCharacterID,
		"removed_slots", fmt.Sprint(result.RemovedSlots),
		"removed_rows", len(result.RemovedSlots),
		"activation_count", len(result.Activations),
		"activations", fmt.Sprint(result.Activations),
		"state_source", "runtime_pvf_premiumlist_item_type_term_and_account_expiry")
}

func (s *Service) sendCurrentPremiumInventoryActivations(
	session *gameSession,
	result premium.InventoryActivationResult,
	source string,
) error {
	if !result.Changed() {
		return nil
	}
	nowUnix := s.gameplayNow().UTC().Unix()
	refreshCrystal := false
	for _, activation := range result.Activations {
		remaining := activation.ExpireAt - nowUnix
		if remaining < 0 {
			remaining = 0
		}
		if premium.CanNotifyActivation(activation.PremiumType) {
			if err := s.sendGameUpperRawClassCodec(
				session,
				currentPremiumActivatedMsgID,
				buildCurrentPremiumActivatedBody(activation.PremiumType, remaining),
				0,
				true,
			); err != nil {
				return err
			}
		}
		if activation.PremiumType == premium.TypeCrystal {
			refreshCrystal = true
		}
	}
	s.logCurrentPremiumInventoryActivation(session, result, source)
	if refreshCrystal {
		return s.sendCurrentCrystalContractState(session, source+"_after_type97_auto_activation")
	}
	return nil
}

func currentPremiumContractCandidateItemUpdate(body []byte, catalog *currentPremiumCatalog) bool {
	if catalog == nil || len(body) < 3 || body[0] != dnfrepo.MainInventoryListType {
		return false
	}
	count := int(binary.LittleEndian.Uint16(body[1:3]))
	if count <= 0 || len(body) != 3+count*currentItemListEntryWireSize {
		return false
	}
	for index := 0; index < count; index++ {
		offset := 3 + index*currentItemListEntryWireSize
		itemID := int64(binary.LittleEndian.Uint32(body[offset+2 : offset+6]))
		if _, exists := catalog.contractsByItem[itemID]; exists {
			return true
		}
	}
	return false
}

func currentPremiumContractDeletionItemUpdate(
	body []byte,
	removedSlots []int16,
) ([]byte, bool) {
	if len(body) < 3 || body[0] != dnfrepo.MainInventoryListType {
		return body, false
	}
	count := int(binary.LittleEndian.Uint16(body[1:3]))
	if len(body) != 3+count*currentItemListEntryWireSize {
		return body, false
	}
	entries := make([]currentItemListEntry, 0, count+len(removedSlots))
	bySlot := make(map[int16]int, count+len(removedSlots))
	for index := 0; index < count; index++ {
		offset := 3 + index*currentItemListEntryWireSize
		var entry currentItemListEntry
		copy(entry.data[:], body[offset:offset+currentItemListEntryWireSize])
		slot := int16(binary.LittleEndian.Uint16(entry.data[0:2]))
		bySlot[slot] = len(entries)
		entries = append(entries, entry)
	}
	for _, slot := range removedSlots {
		var deletion currentItemListEntry
		deletion.patchCore(slot, math.MaxUint32, 0)
		if index, exists := bySlot[slot]; exists {
			entries[index] = deletion
			continue
		}
		bySlot[slot] = len(entries)
		entries = append(entries, deletion)
	}
	sortCurrentItemListEntries(entries)
	return buildCurrentItemUpdateBody(dnfrepo.MainInventoryListType, entries), true
}

// prepareCurrentPremiumContractItemUpdate intercepts only a class0/op14 list0
// body that itself contains a PVF contract item. After the durable activation,
// every consumed slot is projected as a deletion row, so the awarded contract
// never appears in the client bag even though a crash before this boundary
// would still leave a recoverable item for the login migration.
func (s *Service) prepareCurrentPremiumContractItemUpdate(
	session *gameSession,
	body []byte,
) ([]byte, error) {
	catalog, err := s.currentPremiumCatalog()
	if err != nil || !currentPremiumContractCandidateItemUpdate(body, catalog) {
		return body, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()
	result, err := s.activateCurrentPremiumInventoryContracts(ctx, session)
	if err != nil {
		s.logGameEvent(session, "game-premium-contract-item-update-auto-activation-deferred",
			"char_id", session.selectedCharacterID,
			"reason", err,
			"fallback", "retain_recoverable_inventory_item_and_original_op14")
		return body, nil
	}
	if !result.Changed() {
		return body, nil
	}
	if err := s.sendCurrentPremiumInventoryActivations(session, result, "op14_contract_grant_before_inventory_projection"); err != nil {
		return nil, err
	}
	updated, ok := currentPremiumContractDeletionItemUpdate(body, result.RemovedSlots)
	if !ok {
		return nil, fmt.Errorf("premium contract item update body became invalid after activation")
	}
	return updated, nil
}

func (s *Service) reconcileCurrentPremiumInventoryBeforeList(
	ctx context.Context,
	session *gameSession,
	source string,
	notify bool,
) error {
	result, err := s.activateCurrentPremiumInventoryContracts(ctx, session)
	if err != nil {
		return err
	}
	if !result.Changed() {
		return nil
	}
	if notify {
		return s.sendCurrentPremiumInventoryActivations(session, result, source)
	}
	s.logCurrentPremiumInventoryActivation(session, result, source)
	return nil
}
