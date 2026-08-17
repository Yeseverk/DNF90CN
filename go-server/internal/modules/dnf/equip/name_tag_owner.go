package equip

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrNameTagCharacterNotFound = errors.New("name-tag character not found")
	ErrNameTagAccountMismatch   = errors.New("name-tag character does not belong to account")
)

type CleanupExpiredNameTagCommand struct {
	AccountID   string
	CharacterID string
	SlotIndex   int16
	Now         time.Time
}

type CleanupExpiredNameTagResult struct {
	CharacterID      string
	ItemID           int64
	ExpiredAt        int64
	Changed          bool
	EquipmentRemoved bool
}

// CleanupExpiredNameTag atomically clears expired character projection fields
// and the corresponding worn name-tag entry. Packet refresh and PVF type
// authority remain in the bridge.
func (o *Owner) CleanupExpiredNameTag(
	ctx context.Context,
	command CleanupExpiredNameTagCommand,
) (CleanupExpiredNameTagResult, error) {
	if o == nil || o.assets == nil {
		return CleanupExpiredNameTagResult{}, ErrOwnerUnavailable
	}
	characterID := strings.TrimSpace(command.CharacterID)
	if characterID == "" {
		return CleanupExpiredNameTagResult{}, ErrCharacterRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return CleanupExpiredNameTagResult{}, err
	}
	now := command.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	accountID := strings.TrimSpace(command.AccountID)
	result := CleanupExpiredNameTagResult{CharacterID: characterID}
	err := o.assets.WithinCharacterAssets(ctx, characterID, func(
		characters dnfrepo.CharacterRepository,
		_ dnfrepo.InventoryRepository,
		equipment dnfrepo.EquipmentRepository,
	) error {
		character, found, err := characters.Load(ctx, characterID)
		if err != nil {
			return fmt.Errorf("load name-tag character: %w", err)
		}
		if !found {
			return fmt.Errorf("%w: character=%s", ErrNameTagCharacterNotFound, characterID)
		}
		if accountID != "" && strings.TrimSpace(character.AccountID) != accountID {
			return fmt.Errorf("%w: account=%s character=%s", ErrNameTagAccountMismatch, accountID, characterID)
		}
		if character.Stats == nil {
			return nil
		}
		result.ItemID = character.Stats["name_tag_item_id"]
		result.ExpiredAt = character.Stats["name_tag_expire_time"]
		if result.ItemID == 0 || result.ExpiredAt == 0 || now.Unix() < result.ExpiredAt {
			return nil
		}

		character = dnfrepo.CloneCharacter(character)
		character.Stats["name_tag_item_id"] = 0
		character.Stats["name_tag_expire_time"] = 0
		character.UpdatedAt = now
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return fmt.Errorf("clear expired name-tag character fields: %w", err)
		}

		worn, found, err := equipment.Load(ctx, characterID)
		if err != nil {
			return fmt.Errorf("load expired name-tag equipment: %w", err)
		}
		if found {
			worn = dnfrepo.CloneEquipment(worn)
			slotKey := strconv.Itoa(int(command.SlotIndex))
			if _, exists := worn.Entries[slotKey]; exists {
				delete(worn.Entries, slotKey)
				worn.UpdatedAt = now
				if err := dnfrepo.SaveEquipmentFields(ctx, equipment, worn, dnfrepo.EquipmentFieldEntries); err != nil {
					return fmt.Errorf("remove expired name-tag equipment: %w", err)
				}
				result.EquipmentRemoved = true
			}
		}
		result.Changed = true
		return nil
	})
	return result, err
}
