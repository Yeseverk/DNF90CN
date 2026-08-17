package cargo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable          = errors.New("cargo owner unavailable")
	ErrAccountRequired           = errors.New("account id required")
	ErrCharacterRequired         = errors.New("selected character id required")
	ErrAccountNotFound           = errors.New("account record not found")
	ErrCharacterNotFound         = errors.New("character record not found")
	ErrGoldInsufficient          = errors.New("gold is insufficient")
	ErrCargoExists               = errors.New("account cargo already exists")
	ErrCargoNotCreated           = errors.New("account cargo is not created")
	ErrCargoMaxLevel             = errors.New("account cargo is already max level")
	ErrCargoCostMissing          = errors.New("account cargo upgrade cost owner is missing")
	ErrCargoMaterialInsufficient = errors.New("account cargo upgrade material is insufficient")
)

const accountCargoCreateGoldCost int64 = 100000
const accountCargoVoidMagicStoneItemID int64 = 3299
const accountCargoVoidMagicStoneCost int64 = 250

var accountCargoTiers = []int64{1, 8, 16, 24, 32, 40, 48, 56, 64}

type AccountCommand struct {
	AccountID           string
	SelectedCharacterID uint16
}

type MoneyCommand struct {
	AccountID           string
	SelectedCharacterID uint16
	Direction           MoneyDirection
	Amount              int64
}

type CostKind string

const (
	CostNone     CostKind = ""
	CostGold     CostKind = "gold"
	CostCera     CostKind = "cera"
	CostMaterial CostKind = "material"
)

type CostResult struct {
	Kind   CostKind
	Amount int64
}

// PlanResult is a read-only validation result; it does not mean cargo state
// or gold has been changed.
type PlanResult struct {
	AccountID     string
	CharacterID   string
	Operation     string
	Direction     MoneyDirection
	Amount        int64
	CharacterGold int64
	CharacterCera int64
	CargoGold     int64
	CargoLevel    int64
	CargoCreated  bool
	Cost          CostResult
	Changed       bool
}

type Owner struct {
	accounts   dnfrepo.AccountRepository
	characters dnfrepo.CharacterRepository
	assets     dnfrepo.RentalAssetUnitOfWork
}

func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Account == nil || repos.Character == nil || repos.RentalAssets == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{accounts: repos.Account, characters: repos.Character, assets: repos.RentalAssets}, nil
}

func (o *Owner) PlanAccount(ctx context.Context, operation string, cmd AccountCommand) (PlanResult, error) {
	account, err := o.loadAccount(ctx, cmd.AccountID)
	if err != nil {
		return PlanResult{}, err
	}
	return PlanResult{
		AccountID:    account.AccountID,
		Operation:    operation,
		CargoGold:    metadataInt(account.Metadata, "account_cargo_gold"),
		CargoLevel:   metadataInt(account.Metadata, "account_cargo_level"),
		CargoCreated: metadataBool(account.Metadata, "account_cargo_created"),
	}, nil
}

// ApplyAccount 写入账号金库创建/扩展状态，并在需要金币费用时同步扣角色金币。
// 当前还没有统一 wallet/account-cargo 事务，保存账号失败时会补偿回滚角色金币，成功 ACK 仍由 handler 阻断。
func (o *Owner) ApplyAccount(ctx context.Context, operation string, cmd AccountCommand) (PlanResult, error) {
	if o == nil || o.assets == nil {
		return PlanResult{}, ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(cmd.AccountID)
	if accountID == "" {
		return PlanResult{}, ErrAccountRequired
	}
	if cmd.SelectedCharacterID == 0 {
		return PlanResult{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	var result PlanResult
	err := o.assets.WithinRentalAssets(ctx, accountID, characterID, func(accounts dnfrepo.AccountRepository, characters dnfrepo.CharacterRepository, inventoryRepo dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
		account, found, err := accounts.Load(ctx, accountID)
		if err != nil {
			return err
		}
		if !found {
			return ErrAccountNotFound
		}
		character, found, err := characters.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrCharacterNotFound
		}
		if owner := strings.TrimSpace(character.AccountID); owner != "" && owner != accountID {
			return ErrAccountRequired
		}
		account = ensureAccountMetadata(dnfrepo.CloneAccount(account))
		character = ensureCharacterStats(dnfrepo.CloneCharacter(character), characterID)
		var cost CostResult
		inventoryDirty := false
		switch operation {
		case "create_account_cargo":
			if metadataBool(account.Metadata, "account_cargo_created") || metadataInt(account.Metadata, "account_cargo_level") > 0 {
				return ErrCargoExists
			}
			if character.Stats["gold"] < accountCargoCreateGoldCost {
				return fmt.Errorf("%w: character=%d amount=%d", ErrGoldInsufficient, character.Stats["gold"], accountCargoCreateGoldCost)
			}
			character.Stats["gold"] -= accountCargoCreateGoldCost
			cost = CostResult{Kind: CostGold, Amount: accountCargoCreateGoldCost}
			account.Metadata["account_cargo_created"] = "true"
			account.Metadata["account_cargo_level"] = strconv.FormatInt(accountCargoTiers[0], 10)
			if strings.TrimSpace(account.Metadata["account_cargo_gold"]) == "" {
				account.Metadata["account_cargo_gold"] = "0"
			}
		case "upgrade_account_cargo":
			previous := metadataInt(account.Metadata, "account_cargo_level")
			next, ok := nextCargoTier(previous)
			if previous <= 0 {
				return ErrCargoNotCreated
			}
			if !ok {
				return ErrCargoMaxLevel
			}
			var inventory dnfrepo.InventoryRecord
			if previous == 16 || previous == 24 {
				inventory, found, err = inventoryRepo.Load(ctx, characterID)
				if err != nil {
					return err
				}
				if !found {
					return ErrCargoCostMissing
				}
				inventory = dnfrepo.CloneInventory(inventory)
			}
			cost, inventoryDirty, err = applyCargoUpgradeCost(&character, &account, &inventory, previous)
			if err != nil {
				return err
			}
			if inventoryDirty {
				inventory.CharacterID = characterID
				inventory.UpdatedAt = time.Now().UTC()
				if err := dnfrepo.SaveInventoryFields(ctx, inventoryRepo, inventory, dnfrepo.InventoryFieldSlots); err != nil {
					return err
				}
			}
			account.Metadata["account_cargo_created"] = "true"
			account.Metadata["account_cargo_level"] = strconv.FormatInt(next, 10)
		default:
			return fmt.Errorf("operation %q invalid", operation)
		}
		now := time.Now().UTC()
		character.UpdatedAt = now
		account.UpdatedAt = now
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		if err := accounts.Save(ctx, account); err != nil {
			return err
		}
		result = PlanResult{AccountID: account.AccountID, CharacterID: characterID, Operation: operation, CharacterGold: character.Stats["gold"], CharacterCera: metadataInt(account.Metadata, "account_cera"), CargoGold: metadataInt(account.Metadata, "account_cargo_gold"), CargoLevel: metadataInt(account.Metadata, "account_cargo_level"), CargoCreated: metadataBool(account.Metadata, "account_cargo_created"), Cost: cost, Changed: true}
		return nil
	})
	if err != nil {
		return PlanResult{}, err
	}
	return result, nil
}

func (o *Owner) PlanMoney(ctx context.Context, cmd MoneyCommand) (PlanResult, error) {
	if cmd.Amount <= 0 {
		return PlanResult{}, fmt.Errorf("amount %d invalid", cmd.Amount)
	}
	account, err := o.loadAccount(ctx, cmd.AccountID)
	if err != nil {
		return PlanResult{}, err
	}
	characterID, character, err := o.loadCharacter(ctx, cmd.SelectedCharacterID)
	if err != nil {
		return PlanResult{}, err
	}
	character = dnfrepo.CloneCharacter(character)
	characterGold := character.Stats["gold"]
	cargoGold := metadataInt(account.Metadata, "account_cargo_gold")
	switch cmd.Direction {
	case MoneyDeposit:
		if characterGold < cmd.Amount {
			return PlanResult{}, fmt.Errorf("%w: character=%d amount=%d", ErrGoldInsufficient, characterGold, cmd.Amount)
		}
	case MoneyWithdraw:
		if cargoGold < cmd.Amount {
			return PlanResult{}, fmt.Errorf("%w: cargo=%d amount=%d", ErrGoldInsufficient, cargoGold, cmd.Amount)
		}
	default:
		return PlanResult{}, fmt.Errorf("direction %q invalid", cmd.Direction)
	}
	return PlanResult{
		AccountID:     account.AccountID,
		CharacterID:   characterID,
		Operation:     string(cmd.Direction) + "_money",
		Direction:     cmd.Direction,
		Amount:        cmd.Amount,
		CharacterGold: characterGold,
		CharacterCera: metadataInt(account.Metadata, "account_cera"),
		CargoGold:     cargoGold,
		CargoLevel:    metadataInt(account.Metadata, "account_cargo_level"),
		CargoCreated:  metadataBool(account.Metadata, "account_cargo_created"),
	}, nil
}

// ApplyMoney 同步写入角色金币和账号金库金币。
// 这是账号金库 305-308 链路的最小可靠 owner；客户端刷新包顺序未闭合前，handler 仍然阻断成功 ACK。
func (o *Owner) ApplyMoney(ctx context.Context, cmd MoneyCommand) (PlanResult, error) {
	if cmd.Amount <= 0 {
		return PlanResult{}, fmt.Errorf("amount %d invalid", cmd.Amount)
	}
	if o == nil || o.assets == nil {
		return PlanResult{}, ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(cmd.AccountID)
	if accountID == "" {
		return PlanResult{}, ErrAccountRequired
	}
	if cmd.SelectedCharacterID == 0 {
		return PlanResult{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	var result PlanResult
	err := o.assets.WithinRentalAssets(ctx, accountID, characterID, func(accounts dnfrepo.AccountRepository, characters dnfrepo.CharacterRepository, _ dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
		account, found, err := accounts.Load(ctx, accountID)
		if err != nil {
			return err
		}
		if !found {
			return ErrAccountNotFound
		}
		character, found, err := characters.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found {
			return ErrCharacterNotFound
		}
		if owner := strings.TrimSpace(character.AccountID); owner != "" && owner != accountID {
			return ErrAccountRequired
		}
		account = ensureAccountMetadata(dnfrepo.CloneAccount(account))
		character = ensureCharacterStats(dnfrepo.CloneCharacter(character), characterID)
		characterGold := character.Stats["gold"]
		cargoGold := metadataInt(account.Metadata, "account_cargo_gold")
		if !metadataBool(account.Metadata, "account_cargo_created") && metadataInt(account.Metadata, "account_cargo_level") <= 0 {
			return ErrCargoNotCreated
		}
		switch cmd.Direction {
		case MoneyDeposit:
			if characterGold < cmd.Amount {
				return fmt.Errorf("%w: character=%d amount=%d", ErrGoldInsufficient, characterGold, cmd.Amount)
			}
			characterGold -= cmd.Amount
			cargoGold += cmd.Amount
		case MoneyWithdraw:
			if cargoGold < cmd.Amount {
				return fmt.Errorf("%w: cargo=%d amount=%d", ErrGoldInsufficient, cargoGold, cmd.Amount)
			}
			characterGold += cmd.Amount
			cargoGold -= cmd.Amount
		default:
			return fmt.Errorf("direction %q invalid", cmd.Direction)
		}
		now := time.Now().UTC()
		character.Stats["gold"] = characterGold
		character.UpdatedAt = now
		account.Metadata["account_cargo_gold"] = strconv.FormatInt(cargoGold, 10)
		account.Metadata["account_cargo_created"] = "true"
		account.UpdatedAt = now
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return err
		}
		if err := accounts.Save(ctx, account); err != nil {
			return err
		}
		result = PlanResult{AccountID: account.AccountID, CharacterID: characterID, Operation: string(cmd.Direction) + "_money", Direction: cmd.Direction, Amount: cmd.Amount, CharacterGold: characterGold, CharacterCera: metadataInt(account.Metadata, "account_cera"), CargoGold: cargoGold, CargoLevel: metadataInt(account.Metadata, "account_cargo_level"), CargoCreated: metadataBool(account.Metadata, "account_cargo_created"), Changed: true}
		return nil
	})
	if err != nil {
		return PlanResult{}, err
	}
	return result, nil
}

func (o *Owner) loadAccount(ctx context.Context, accountID string) (dnfrepo.AccountRecord, error) {
	if o == nil || o.accounts == nil {
		return dnfrepo.AccountRecord{}, ErrOwnerUnavailable
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return dnfrepo.AccountRecord{}, ErrAccountRequired
	}
	account, ok, err := o.accounts.Load(ctx, accountID)
	if err != nil {
		return dnfrepo.AccountRecord{}, err
	}
	if !ok {
		return dnfrepo.AccountRecord{}, ErrAccountNotFound
	}
	return dnfrepo.CloneAccount(account), nil
}

func (o *Owner) loadCharacter(ctx context.Context, selectedCharacterID uint16) (string, dnfrepo.CharacterRecord, error) {
	if o == nil || o.characters == nil {
		return "", dnfrepo.CharacterRecord{}, ErrOwnerUnavailable
	}
	if selectedCharacterID == 0 {
		return "", dnfrepo.CharacterRecord{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(selectedCharacterID), 10)
	character, ok, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return "", dnfrepo.CharacterRecord{}, err
	}
	if !ok {
		return "", dnfrepo.CharacterRecord{}, ErrCharacterNotFound
	}
	return characterID, dnfrepo.CloneCharacter(character), nil
}

func ensureAccountMetadata(account dnfrepo.AccountRecord) dnfrepo.AccountRecord {
	if account.Metadata == nil {
		account.Metadata = make(map[string]string, 4)
	}
	return account
}

func ensureCharacterStats(character dnfrepo.CharacterRecord, characterID string) dnfrepo.CharacterRecord {
	character.CharacterID = characterID
	if character.Stats == nil {
		character.Stats = make(map[string]int64, 1)
	}
	return character
}

func nextCargoTier(previous int64) (int64, bool) {
	for index, tier := range accountCargoTiers {
		if tier == previous && index+1 < len(accountCargoTiers) {
			return accountCargoTiers[index+1], true
		}
	}
	return previous, false
}

// NextAccountCargoTier is shared by the normal account-cargo command and the
// PVF-resolved Cera-shop upgrade tool.  Keeping one progression prevents the
// shop from silently skipping a normal client-visible tier.
func NextAccountCargoTier(previous int64) (int64, bool) {
	return nextCargoTier(previous)
}

func applyCargoUpgradeCost(character *dnfrepo.CharacterRecord, account *dnfrepo.AccountRecord, inventory *dnfrepo.InventoryRecord, previousTier int64) (CostResult, bool, error) {
	switch previousTier {
	case 1:
		const cost int64 = 2000000
		if character.Stats["gold"] < cost {
			return CostResult{}, false, fmt.Errorf("%w: character=%d amount=%d", ErrGoldInsufficient, character.Stats["gold"], cost)
		}
		character.Stats["gold"] -= cost
		return CostResult{Kind: CostGold, Amount: cost}, false, nil
	case 8, 32, 40, 48, 56:
		cost := map[int64]int64{8: 2000, 32: 2000, 40: 2500, 48: 3000, 56: 5000}[previousTier]
		balance := metadataInt(account.Metadata, "account_cera")
		if balance < cost {
			return CostResult{}, false, fmt.Errorf("%w: cera=%d amount=%d", ErrGoldInsufficient, balance, cost)
		}
		account.Metadata["account_cera"] = strconv.FormatInt(balance-cost, 10)
		return CostResult{Kind: CostCera, Amount: cost}, false, nil
	case 16, 24:
		if inventory == nil || inventory.Slots == nil {
			return CostResult{}, false, fmt.Errorf("%w: void magic stone inventory missing for tier=%d", ErrCargoCostMissing, previousTier)
		}
		remaining := accountCargoVoidMagicStoneCost
		keys := make([]string, 0)
		for key, stack := range inventory.Slots {
			if strings.HasPrefix(key, "0:") && stack.ItemID == accountCargoVoidMagicStoneItemID && stack.Count > 0 {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			stack := inventory.Slots[key]
			if stack.Count >= remaining {
				remaining = 0
				break
			}
			remaining -= stack.Count
		}
		if remaining > 0 {
			return CostResult{}, false, fmt.Errorf("%w: item=%d need=%d", ErrCargoMaterialInsufficient, accountCargoVoidMagicStoneItemID, accountCargoVoidMagicStoneCost)
		}
		remaining = accountCargoVoidMagicStoneCost
		for _, key := range keys {
			stack := inventory.Slots[key]
			consume := stack.Count
			if consume > remaining {
				consume = remaining
			}
			stack.Count -= consume
			remaining -= consume
			if stack.Count == 0 {
				delete(inventory.Slots, key)
			} else {
				inventory.Slots[key] = stack
			}
			if remaining == 0 {
				break
			}
		}
		return CostResult{Kind: CostMaterial, Amount: accountCargoVoidMagicStoneCost}, true, nil
	default:
		return CostResult{}, false, nil
	}
}

func metadataInt(metadata map[string]string, key string) int64 {
	raw := strings.TrimSpace(metadata[key])
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func metadataBool(metadata map[string]string, key string) bool {
	switch strings.ToLower(strings.TrimSpace(metadata[key])) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
