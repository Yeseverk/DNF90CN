package rental

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const PointMetadataKey = "rental_lucky_stars"

var (
	ErrOwnerUnavailable   = errors.New("rental owner is unavailable")
	ErrAccountRequired    = errors.New("rental account id is required")
	ErrCharacterRequired  = errors.New("rental character id is required")
	ErrStateMissing       = errors.New("rental asset state is missing")
	ErrOwnerMismatch      = errors.New("rental asset owner mismatch")
	ErrPointsInsufficient = errors.New("rental points are insufficient")
	ErrGoldInsufficient   = errors.New("rental point gold is insufficient")
	ErrPointLimit         = errors.New("rental point limit exceeded")
	ErrProjectorRequired  = errors.New("rental mutation projector is required")
	ErrCommandInvalid     = errors.New("rental command is invalid")
)

type WalletResult struct {
	Points uint32
	Gold   int64
}

type Assets struct {
	Character *dnfrepo.CharacterRecord
	Inventory *dnfrepo.InventoryRecord
	Equipment *dnfrepo.EquipmentRecord
}

type Changes struct {
	Inventory bool
	Equipment bool
}

type Projector func(*Assets) (Changes, error)

type ChargeCommand struct {
	AccountID    string
	CharacterID  string
	Count        uint32
	Limit        uint32
	GoldPerPoint int64
	UpdatedAt    time.Time
}

type RentCommand struct {
	AccountID   string
	CharacterID string
	PointCost   uint32
	UpdatedAt   time.Time
	Project     Projector
}

type CleanupCommand struct {
	AccountID   string
	CharacterID string
	UpdatedAt   time.Time
	Project     Projector
}

// Owner owns rental wallet rules, account/character authorization, and the
// atomic account, character, inventory, and equipment commit. PVF validation
// and current-client raw-row projection remain in the bridge.
type Owner struct {
	assets dnfrepo.RentalAssetUnitOfWork
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.RentalAssets == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{assets: repositories.RentalAssets}, nil
}

func (o *Owner) Charge(ctx context.Context, command ChargeCommand) (WalletResult, error) {
	if command.Count == 0 || command.Limit == 0 || command.GoldPerPoint <= 0 {
		return WalletResult{}, ErrCommandInvalid
	}
	if int64(command.Count) > math.MaxInt64/command.GoldPerPoint {
		return WalletResult{}, ErrPointLimit
	}
	cost := int64(command.Count) * command.GoldPerPoint
	var result WalletResult
	err := o.withinOwners(ctx, command.AccountID, command.CharacterID, func(
		accounts dnfrepo.AccountRepository,
		characters dnfrepo.CharacterRepository,
		_ dnfrepo.InventoryRepository,
		_ dnfrepo.EquipmentRepository,
		account dnfrepo.AccountRecord,
		character dnfrepo.CharacterRecord,
	) error {
		points, err := Points(account)
		if err != nil {
			return err
		}
		if uint64(points)+uint64(command.Count) > uint64(command.Limit) {
			return ErrPointLimit
		}
		gold := character.Stats["gold"]
		if gold < cost {
			return ErrGoldInsufficient
		}
		points += command.Count
		gold -= cost
		if character.Stats == nil {
			character.Stats = make(map[string]int64)
		}
		character.Stats["gold"] = gold
		SetPoints(&account, points)
		now := normalizedTime(command.UpdatedAt)
		account.UpdatedAt = now
		character.UpdatedAt = now
		if err := accounts.Save(ctx, account); err != nil {
			return err
		}
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		result = WalletResult{Points: points, Gold: gold}
		return nil
	})
	if err != nil {
		return WalletResult{}, err
	}
	return result, nil
}

func (o *Owner) Rent(ctx context.Context, command RentCommand) (WalletResult, error) {
	if command.PointCost == 0 {
		return WalletResult{}, ErrCommandInvalid
	}
	if command.Project == nil {
		return WalletResult{}, ErrProjectorRequired
	}
	var result WalletResult
	err := o.withinOwners(ctx, command.AccountID, command.CharacterID, func(
		accounts dnfrepo.AccountRepository,
		_ dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		equipmentRepo dnfrepo.EquipmentRepository,
		account dnfrepo.AccountRecord,
		character dnfrepo.CharacterRecord,
	) error {
		points, err := Points(account)
		if err != nil {
			return err
		}
		if points < command.PointCost {
			return ErrPointsInsufficient
		}
		inventory, equipment, err := loadRentalItems(ctx, inventories, equipmentRepo, command.CharacterID)
		if err != nil {
			return err
		}
		changes, err := command.Project(&Assets{
			Character: &character,
			Inventory: &inventory,
			Equipment: &equipment,
		})
		if err != nil {
			return err
		}
		points -= command.PointCost
		SetPoints(&account, points)
		now := normalizedTime(command.UpdatedAt)
		account.UpdatedAt = now
		if err := accounts.Save(ctx, account); err != nil {
			return err
		}
		if changes.Inventory {
			inventory.UpdatedAt = now
			if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
		}
		if changes.Equipment {
			equipment.UpdatedAt = now
			if err := dnfrepo.SaveEquipmentFields(ctx, equipmentRepo, equipment, dnfrepo.EquipmentFieldEntries); err != nil {
				return err
			}
		}
		result = WalletResult{Points: points, Gold: character.Stats["gold"]}
		return nil
	})
	if err != nil {
		return WalletResult{}, err
	}
	return result, nil
}

func (o *Owner) Cleanup(ctx context.Context, command CleanupCommand) error {
	if command.Project == nil {
		return ErrProjectorRequired
	}
	return o.withinOwners(ctx, command.AccountID, command.CharacterID, func(
		_ dnfrepo.AccountRepository,
		_ dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		equipmentRepo dnfrepo.EquipmentRepository,
		_ dnfrepo.AccountRecord,
		character dnfrepo.CharacterRecord,
	) error {
		inventory, equipment, err := loadRentalItems(ctx, inventories, equipmentRepo, command.CharacterID)
		if err != nil {
			return err
		}
		changes, err := command.Project(&Assets{
			Character: &character,
			Inventory: &inventory,
			Equipment: &equipment,
		})
		if err != nil {
			return err
		}
		now := normalizedTime(command.UpdatedAt)
		if changes.Inventory {
			inventory.UpdatedAt = now
			if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
		}
		if changes.Equipment {
			equipment.UpdatedAt = now
			if err := dnfrepo.SaveEquipmentFields(ctx, equipmentRepo, equipment, dnfrepo.EquipmentFieldEntries); err != nil {
				return err
			}
		}
		return nil
	})
}

func (o *Owner) withinOwners(
	ctx context.Context,
	accountID string,
	characterID string,
	apply func(
		dnfrepo.AccountRepository,
		dnfrepo.CharacterRepository,
		dnfrepo.InventoryRepository,
		dnfrepo.EquipmentRepository,
		dnfrepo.AccountRecord,
		dnfrepo.CharacterRecord,
	) error,
) error {
	if o == nil || o.assets == nil {
		return ErrOwnerUnavailable
	}
	accountID = strings.TrimSpace(accountID)
	characterID = strings.TrimSpace(characterID)
	if accountID == "" {
		return ErrAccountRequired
	}
	if characterID == "" {
		return ErrCharacterRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return o.assets.WithinRentalAssets(ctx, accountID, characterID, func(
		accounts dnfrepo.AccountRepository,
		characters dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		equipment dnfrepo.EquipmentRepository,
	) error {
		if accounts == nil || characters == nil || inventories == nil || equipment == nil {
			return ErrOwnerUnavailable
		}
		account, accountFound, err := accounts.Load(ctx, accountID)
		if err != nil {
			return err
		}
		character, characterFound, err := characters.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !accountFound || !characterFound {
			return ErrStateMissing
		}
		if strings.TrimSpace(account.AccountID) != accountID ||
			strings.TrimSpace(character.CharacterID) != characterID ||
			strings.TrimSpace(character.AccountID) != accountID {
			return ErrOwnerMismatch
		}
		account = dnfrepo.CloneAccount(account)
		character = dnfrepo.CloneCharacter(character)
		return apply(accounts, characters, inventories, equipment, account, character)
	})
}

func Points(account dnfrepo.AccountRecord) (uint32, error) {
	raw := ""
	if account.Metadata != nil {
		raw = strings.TrimSpace(account.Metadata[PointMetadataKey])
	}
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", PointMetadataKey, raw, err)
	}
	return uint32(value), nil
}

func SetPoints(account *dnfrepo.AccountRecord, points uint32) {
	if account.Metadata == nil {
		account.Metadata = make(map[string]string)
	}
	account.Metadata[PointMetadataKey] = strconv.FormatUint(uint64(points), 10)
}

func loadRentalItems(
	ctx context.Context,
	inventories dnfrepo.InventoryRepository,
	equipment dnfrepo.EquipmentRepository,
	characterID string,
) (dnfrepo.InventoryRecord, dnfrepo.EquipmentRecord, error) {
	inventory, inventoryFound, err := inventories.Load(ctx, characterID)
	if err != nil {
		return dnfrepo.InventoryRecord{}, dnfrepo.EquipmentRecord{}, err
	}
	if !inventoryFound {
		inventory = dnfrepo.InventoryRecord{CharacterID: characterID}
	}
	inventory = dnfrepo.CloneInventory(inventory)
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	worn, equipmentFound, err := equipment.Load(ctx, characterID)
	if err != nil {
		return dnfrepo.InventoryRecord{}, dnfrepo.EquipmentRecord{}, err
	}
	if !equipmentFound {
		worn = dnfrepo.EquipmentRecord{CharacterID: characterID}
	}
	worn = dnfrepo.CloneEquipment(worn)
	if worn.Entries == nil {
		worn.Entries = make(map[string]dnfrepo.EquipmentEntry)
	}
	return inventory, worn, nil
}

func normalizedTime(value time.Time) time.Time {
	value = value.UTC()
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value
}
