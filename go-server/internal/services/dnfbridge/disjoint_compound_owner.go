package dnfbridge

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	dnfdisjoint "longheng.io/server/internal/modules/dnf/disjoint"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func (s *Service) currentDisjointMutationOwner(session *gameSession) (string, string, *dnfdisjoint.Owner, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return "", "", nil, errCurrentDisjointUnavailable
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.AccountAssets == nil {
		return "", "", nil, errCurrentDisjointUnavailable
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	if accountID == "" {
		return "", "", nil, errCurrentDisjointUnavailable
	}
	owner, err := dnfdisjoint.NewOwner(repositories)
	if err != nil {
		return "", "", nil, errCurrentDisjointUnavailable
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	return accountID, characterID, owner, nil
}

func currentDisjointMutationError(err error) error {
	switch {
	case errors.Is(err, dnfdisjoint.ErrOwnerUnavailable),
		errors.Is(err, dnfdisjoint.ErrAccountRequired),
		errors.Is(err, dnfdisjoint.ErrCharacterRequired),
		errors.Is(err, dnfdisjoint.ErrCharacterNotFound),
		errors.Is(err, dnfdisjoint.ErrAccountMismatch),
		errors.Is(err, dnfdisjoint.ErrInventoryNotFound):
		return errors.Join(errCurrentDisjointUnavailable, err)
	default:
		return err
	}
}

// commitCurrentDisjoint consumes one equipment instance and grants its
// resolved PVF rows through the domain-owned account+character transaction.
func (s *Service) commitCurrentDisjoint(
	ctx context.Context,
	session *gameSession,
	catalog *pvfDungeonDropCatalog,
	sourceSlot int16,
	listType byte,
	resolve func(dungeonDropItemDefinition, *dnfpvf.Document) ([]currentDisjointReward, error),
) (currentDisjointResult, error) {
	if session == nil || session.selectedCharacterID == 0 || catalog == nil || catalog.source == nil || resolve == nil {
		return currentDisjointResult{}, errCurrentDisjointUnavailable
	}
	accountID, characterID, owner, err := s.currentDisjointMutationOwner(session)
	if err != nil {
		return currentDisjointResult{}, err
	}

	var result currentDisjointResult
	command := dnfdisjoint.Command{
		AccountID:   accountID,
		CharacterID: characterID,
		Project: func(assets *dnfdisjoint.Assets) (dnfdisjoint.Changes, error) {
			inventory := assets.Inventory
			account := assets.AccountInventory
			key := currentCeraShopInventorySlotKey(listType, sourceSlot)
			source, found := inventory.Slots[key]
			validCount := source.Count == 1
			if listType == currentAvatarInventoryListType {
				// Durable avatar rows may keep zero in the wire count field
				// while the slot still owns one non-stackable instance.
				validCount = source.Count == 0 || source.Count == 1
			}
			if !found || source.ItemID <= 0 || source.ItemID > math.MaxUint32 || !validCount || currentNPCShopItemLocked(source) {
				return dnfdisjoint.Changes{}, errCurrentDisjointSourceInvalid
			}
			definition, err := catalog.ResolveItem(uint32(source.ItemID))
			if err != nil || (!definition.ExpirationDate.IsZero() && !time.Now().UTC().Before(definition.ExpirationDate)) {
				return dnfdisjoint.Changes{}, errCurrentDisjointSourceInvalid
			}
			document, err := parseDungeonCardPVFDocument(catalog.source, definition.PVFPath)
			if err != nil {
				return dnfdisjoint.Changes{}, errors.Join(errCurrentDisjointSourceInvalid, err)
			}
			rewards, err := resolve(definition, document)
			if err != nil {
				return dnfdisjoint.Changes{}, err
			}
			if len(rewards) == 0 || len(rewards) > currentDisjointMaxRewards {
				return dnfdisjoint.Changes{}, errCurrentDisjointRewardInvalid
			}
			delete(inventory.Slots, key)
			rewardSlots, accountDirty, err := currentGrantDisjointRewards(inventory, account, catalog, rewards)
			if err != nil {
				return dnfdisjoint.Changes{}, err
			}
			result.Rewards = rewardSlots
			return dnfdisjoint.Changes{AccountInventory: accountDirty, Inventory: true}, nil
		},
	}
	switch listType {
	case 0:
		err = owner.DisjointEquipment(ctx, command)
	case currentAvatarInventoryListType:
		err = owner.DisjointAvatar(ctx, command)
	default:
		err = errCurrentDisjointRequestInvalid
	}
	if err != nil {
		return currentDisjointResult{}, currentDisjointMutationError(err)
	}
	return result, nil
}
