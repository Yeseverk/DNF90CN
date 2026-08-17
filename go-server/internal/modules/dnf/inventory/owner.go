// 本文件负责背包 owner 的真实库存变更边界。
// handler 只提交命令；这里只写 repository，不生成旧客户端成功 ACK。
package inventory

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/creaturestate"
	dnfequip "longheng.io/server/internal/modules/dnf/equip"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrevivecoin "longheng.io/server/internal/modules/dnf/revivecoin"
)

var (
	ErrOwnerUnavailable              = errors.New("inventory owner unavailable")
	ErrCharacterRequired             = errors.New("selected character id required")
	ErrInventoryNotFound             = errors.New("inventory record not found")
	ErrUnsupportedList               = errors.New("inventory list type is not supported")
	ErrSlotNotFound                  = errors.New("inventory slot not found")
	ErrItemMismatch                  = errors.New("inventory item mismatch")
	ErrSellPriceMissing              = errors.New("sell price evidence is missing")
	ErrSellPriceInvalid              = errors.New("sell price is invalid")
	ErrWalletTxnRequired             = errors.New("wallet transaction is required")
	ErrItemLocked                    = errors.New("inventory item is equipment locked")
	ErrMoveRequiresEquipmentOwner    = errors.New("move requires equipment owner")
	ErrMoveAccountCargoUnsupported   = errors.New("account cargo move is unsupported")
	ErrAccountCargoNotCreated        = errors.New("account cargo is not created")
	ErrAccountCargoSlotOutOfRange    = errors.New("account cargo slot is outside current capacity")
	ErrSortRequiresEquipmentOwner    = errors.New("sort requires equipment owner")
	ErrSortAccountCargoUnsupported   = errors.New("account cargo sort is unsupported")
	ErrRepairRequiresEquipmentOwner  = errors.New("repair requires equipment owner")
	ErrRepairAllRequiresOwners       = errors.New("repair all requires equipment and wallet owners")
	ErrRepairDurabilityMissing       = errors.New("repair durability evidence is missing")
	ErrRepairDurabilityInvalid       = errors.New("repair durability evidence is invalid")
	ErrRepairCostMissing             = errors.New("repair cost evidence is missing")
	ErrRepairCostInvalid             = errors.New("repair cost evidence is invalid")
	ErrDeleteRequiresEquipmentOwner  = errors.New("equipment delete requires a typed equipment owner and proven ACK")
	ErrPetInventoryOwnerRequired     = errors.New("pet inventory mutation requires a typed pet owner")
	ErrUseStackableContractRequired  = errors.New("use stackable requires PVF-proven [waste] provenance and stable item identity")
	ErrInventoryTransactionRequired  = errors.New("inventory mutation requires a character item transaction")
	ErrCharacterAssetTxnRequired     = errors.New("inventory mutation requires a character asset transaction")
	ErrAccountSharedOwnerUnavailable = errors.New("account-shared inventory owner unavailable")
	ErrReservedSlotRelocationFull    = errors.New("reserved main inventory slot cannot be relocated into the proven item range")
	ErrAccountRequired               = errors.New("authenticated account id required")
	ErrAccountNotFound               = errors.New("authenticated account record not found")
	ErrEnchantResolverRequired       = errors.New("enchant by bead requires a runtime-PVF resolver")
	ErrUpgradeTicketResolverRequired = errors.New("upgrade ticket requires a runtime-PVF resolver")
	ErrPremiumContractResolveFailed  = errors.New("premium contract runtime-PVF resolution failed")
	ErrRandomRewardResolveFailed     = errors.New("random reward item runtime-PVF resolution failed")
	ErrRandomRewardContractRequired  = errors.New("random reward item requires a complete runtime-PVF outcome table")
	ErrRandomRewardInventoryFull     = errors.New("random reward item result has no compatible inventory slot")
	ErrRepairNotRepairable           = errors.New("inventory item is not repairable by PVF durability evidence")
	ErrRepairGoldInsufficient        = errors.New("repair gold is insufficient")
)

const (
	upgradeErrorInvalidTarget              byte = 4
	upgradeErrorInsufficientGold           byte = 10
	upgradeErrorUnsupportedOptionalTicket  byte = 21
	upgradeErrorInvalidMaterial            byte = 22
	upgradeErrorWrongMode                  byte = 23
	upgradeErrorDurability                 byte = 7
	upgradeErrorMaxLevel                   byte = 95
	upgradeErrorAmplifyNotIdentified       byte = 174
	upgradeErrorLocked                     byte = 213
	upgradeResultCodeSuccess               byte = 0
	upgradeResultCodeFailNoChange          byte = 1
	upgradeResultCodeDowngrade             byte = 2
	upgradeResultCodeDestroy               byte = 3
	upgradeMaximumCurrentLevelBeforeReject byte = 30
	currentUpgradeSuccessBase                   = 100000
)

// Enchant-by-bead client-visible error codes, matching the current EXE S2C
// 0x0110 handler (sub_1D0B960): 0x11 -> text 22223, 0x13 -> text 46371,
// 0x17 -> text 27162; any other value falls back to the generic text 44171.
const (
	enchantErrorInvalidBead   byte = 0x11
	enchantErrorInvalidTarget byte = 0x13
	enchantErrorUnsupported   byte = 0x17
)

// Owner 是背包资产的 durable owner 边界。
// 当前只改 repository 状态；旧客户端 ACK/刷新顺序必须在 EXE 证据闭合后再开放。
type Owner struct {
	repo              dnfrepo.InventoryRepository
	accountRepo       dnfrepo.AccountInventoryRepository
	accounts          dnfrepo.AccountRepository
	characters        dnfrepo.CharacterRepository
	items             dnfrepo.CharacterItemUnitOfWork
	accountItems      dnfrepo.AccountCharacterItemUnitOfWork
	assets            dnfrepo.CharacterAssetUnitOfWork
	accountAssets     dnfrepo.AccountCharacterAssetUnitOfWork
	rentalAssets      dnfrepo.RentalAssetUnitOfWork
	pets              dnfrepo.PetRepository
	petItems          dnfrepo.CharacterPetUnitOfWork
	inItemTransaction bool
	inAccountItemTx   bool
	inPetTransaction  bool
}

// DeleteResult 描述一次删除物品写库结果，不代表已经允许回包。
type DeleteResult struct {
	CharacterID string
	Removed     []DeletedItem
	Changed     bool
}

// SellResult 描述一次出售物品写库结果；金币为 0 时只涉及背包 repo。
type SellResult struct {
	CharacterID string
	Sold        DeletedItem
	GoldDelta   int64
	UpdatedGold int64
	Changed     bool
}

// MoveResult 描述一次物品格子移动的写库结果；它不代表已经可以向旧客户端回成功 ACK。
type MoveResult struct {
	CharacterID          string
	SourceListType       byte
	SourceSlotIndex      int16
	DestinationListType  byte
	DestinationSlotIndex int16
	MoveCount            int64
	Mode                 string
	RefreshListTypes     []byte
	Refresh              map[byte]map[string]dnfrepo.ItemStack
	Changed              bool
}

// SortResult 描述一次整理物品栏的写库结果；Refresh 是上层构造 0x000D 全量刷新所需的列表快照。
type SortResult struct {
	CharacterID string
	ListType    byte
	Category    byte
	StartSlot   int16
	EndSlot     int16
	MovedCount  int
	Mode        string
	Refresh     map[string]dnfrepo.ItemStack
	Changed     bool
}

// RepairResult 描述一次耐久修理的写库结果；当前只允许零金币单件修理回成功 ACK。
// 正价扣费、全修和装备刷新仍需 wallet/equipment owner 闭合后再开放。
type RepairResult struct {
	CharacterID   string
	ListType      byte
	SlotIndex     int16
	OldDurability int64
	NewDurability int64
	Cost          int64
	UpdatedGold   int64
	Changed       bool
	// FreeRepair marks the 魔王契约 auto-repair path (premium type 586).
	FreeRepair bool
}

// UpgradeResult describes one equipment reinforcement/amplification attempt.
// Success=false is still a handled client-visible result and maps to an op50
// failure ACK; Changed only means repository state changed.
type UpgradeResult struct {
	CharacterID                 string
	Mode                        string
	Success                     bool
	ErrorCode                   byte
	TargetSlotIndex             int16
	TargetItemTemplateID        int32
	MaterialSlotIndex           int16
	OptionalTicketSlotIndex     int16
	OldLevel                    byte
	NewLevel                    byte
	ResultCode                  byte
	UpgradeSucceeded            bool
	MaterialRemainingStackCount int64
	GoldCost                    int64
	UpdatedGold                 int64
	MainRefresh                 map[string]dnfrepo.ItemStack
	TargetUpdatedStack          dnfrepo.ItemStack
	MaterialUpdated             bool
	MaterialUpdatedStack        dnfrepo.ItemStack
	Changed                     bool
	DestroyBonusSlot            int16
	DestroyBonusItemID          int32
	DestroyBonusCount           int32
}

// EnchantResult describes one enchant-by-bead attempt. Success=false is still
// a handled client-visible result and maps to a 0x0110 failure ACK carrying
// ErrorCode; Changed only means repository state changed.
type EnchantResult struct {
	CharacterID         string
	Success             bool
	ErrorCode           byte
	TargetListType      byte
	TargetSlotIndex     int16
	TargetItemID        int64
	BeadSlotIndex       int16
	BeadItemID          int64
	CardItemID          int64
	EnchantUpgradeCount byte
	BeadRemainingCount  int64
	TargetUpdatedStack  dnfrepo.ItemStack
	BeadUpdatedStack    dnfrepo.ItemStack
	MainRefresh         map[string]dnfrepo.ItemStack
	Changed             bool
}

// DeletedItem 是已扣减物品的审计摘要。
// UseStackableResult 描述一次消耗品使用写库结果；premium 通知仍由上层 S2C writer 闭合。
// UseStackableResult describes a typed stackable-use result once that owner is implemented.

// UseStackableResult describes a typed stackable-use result once that owner is implemented.
type UseStackableResult struct {
	CharacterID             string
	AccountID               string
	ListType                byte
	SlotIndex               int16
	InstanceValue           int32
	ItemID                  int64
	RemainingCount          int64
	Changed                 bool
	PVFPath                 string
	StackableType           string
	PremiumActivated        bool
	PremiumType             int64
	PremiumRemainingSeconds int64
	ReviveCoinWalletUpdated bool
	ReviveCoinWalletTotal   int64
	RandomRewardItemID      int64
	RandomRewardSlots       []int16
}

// DeletedItem records one applied inventory removal.
type DeletedItem struct {
	ListType       byte
	SlotIndex      int16
	ItemID         int64
	RequestedCount int64
	AppliedCount   int64
	RemainingCount int64
}

// NewOwner 创建背包 owner；缺少背包仓储时拒绝处理，避免 handler 直接写状态。
func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Inventory == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{
		repo:          repos.Inventory,
		accountRepo:   repos.AccountInventory,
		accounts:      repos.Account,
		characters:    repos.Character,
		items:         repos.CharacterItems,
		accountItems:  repos.AccountItems,
		assets:        repos.CharacterAssets,
		accountAssets: repos.AccountAssets,
		rentalAssets:  repos.RentalAssets,
		pets:          repos.Pet,
		petItems:      repos.CharacterPets,
	}, nil
}

func (o *Owner) withinInventoryTransaction(ctx context.Context, characterID string, apply func(*Owner) error) error {
	if o.items == nil {
		return ErrInventoryTransactionRequired
	}
	return o.items.WithinCharacterItems(ctx, characterID, func(inventory dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
		txOwner := *o
		txOwner.repo = inventory
		txOwner.inItemTransaction = true
		return apply(&txOwner)
	})
}

func (o *Owner) withinCharacterAssetTransaction(ctx context.Context, characterID string, apply func(*Owner) error) error {
	if o.assets == nil {
		return ErrCharacterAssetTxnRequired
	}
	return o.assets.WithinCharacterAssets(ctx, characterID, func(characters dnfrepo.CharacterRepository, inventory dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
		txOwner := *o
		txOwner.characters = characters
		txOwner.repo = inventory
		txOwner.inItemTransaction = true
		return apply(&txOwner)
	})
}

func (o *Owner) withinAccountCharacterAssetTransaction(ctx context.Context, accountID string, characterID string, apply func(*Owner) error) error {
	if o.accountAssets == nil {
		return dnfrepo.ErrAccountCharacterAssetTransactionUnavailable
	}
	return o.accountAssets.WithinAccountCharacterAssets(ctx, accountID, characterID, func(accountInventory dnfrepo.AccountInventoryRepository, characters dnfrepo.CharacterRepository, inventory dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
		txOwner := *o
		txOwner.accountRepo = accountInventory
		txOwner.characters = characters
		txOwner.repo = inventory
		txOwner.inItemTransaction = true
		return apply(&txOwner)
	})
}

// Delete 按 EXE/C# 已确认的删除语义扣减物品。
// 这里只保存背包字段；成功 ACK 与背包刷新顺序由上层在证据闭合后处理。
func (o *Owner) Delete(ctx context.Context, cmd Command) (DeleteResult, error) {
	if o == nil || o.repo == nil {
		return DeleteResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return DeleteResult{}, ErrCharacterRequired
	}
	if err := checkDeleteListType(cmd.SourceListType); err != nil {
		return DeleteResult{}, err
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	mutations := deleteMutations(cmd)
	if len(mutations) == 0 {
		return DeleteResult{}, ErrSlotNotFound
	}
	usesAccountShared := false
	for _, mutation := range mutations {
		if dnfrepo.IsAccountSharedInventorySlot(mutation.listType, mutation.slotIndex) {
			usesAccountShared = true
			break
		}
	}
	if usesAccountShared {
		accountID := strings.TrimSpace(cmd.AccountID)
		if accountID == "" {
			return DeleteResult{}, ErrAccountRequired
		}
		if o.accountRepo == nil || o.accountItems == nil {
			return DeleteResult{}, ErrAccountSharedOwnerUnavailable
		}
		if !o.inAccountItemTx {
			var result DeleteResult
			err := o.accountItems.WithinAccountCharacterItems(ctx, accountID, characterID, func(accounts dnfrepo.AccountInventoryRepository, characters dnfrepo.InventoryRepository) error {
				txOwner := *o
				txOwner.accountRepo = accounts
				txOwner.repo = characters
				txOwner.inAccountItemTx = true
				var err error
				result, err = txOwner.Delete(ctx, cmd)
				return err
			})
			return result, err
		}
	} else if !o.inItemTransaction {
		var result DeleteResult
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.Delete(ctx, cmd)
			return err
		})
		return result, err
	}
	record, ok, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return DeleteResult{}, err
	}
	if !ok {
		return DeleteResult{}, ErrInventoryNotFound
	}

	record = dnfrepo.CloneInventory(record)
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if record.Warehouse == nil {
		record.Warehouse = make(map[string]dnfrepo.ItemStack)
	}

	var account dnfrepo.AccountInventoryRecord
	if usesAccountShared {
		accountID := strings.TrimSpace(cmd.AccountID)
		account, _, err = o.accountRepo.Load(ctx, accountID)
		if err != nil {
			return DeleteResult{}, err
		}
		account = dnfrepo.CloneAccountInventory(account)
		account.AccountID = accountID
		if account.Slots == nil {
			account.Slots = make(map[string]dnfrepo.ItemStack)
		}
	}

	changedSlots := false
	changedWarehouse := false
	changedAccount := false
	result := DeleteResult{CharacterID: characterID, Removed: make([]DeletedItem, 0, len(mutations))}
	for _, mutation := range mutations {
		accountOwned := dnfrepo.IsAccountSharedInventorySlot(mutation.listType, mutation.slotIndex)
		var items map[string]dnfrepo.ItemStack
		var field dnfrepo.InventoryField
		if accountOwned {
			items = account.Slots
		} else {
			items, field = itemMapForList(&record, mutation.listType)
		}
		key := slotKey(mutation.listType, mutation.slotIndex)
		stack, ok := items[key]
		if !ok {
			return DeleteResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, mutation.listType, mutation.slotIndex)
		}
		if isEquipmentLocked(stack) {
			return DeleteResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrItemLocked, mutation.listType, mutation.slotIndex)
		}
		if mutation.itemID != 0 && stack.ItemID != int64(mutation.itemID) {
			return DeleteResult{}, fmt.Errorf("%w: list=%d slot=%d want=%d got=%d", ErrItemMismatch, mutation.listType, mutation.slotIndex, mutation.itemID, stack.ItemID)
		}

		applied := normalizeDeleteCount(stack.Count, mutation.count)
		remaining := stack.Count - applied
		if remaining <= 0 {
			delete(items, key)
			remaining = 0
		} else {
			stack.Count = remaining
			items[key] = stack
		}
		if accountOwned {
			changedAccount = true
		} else if field == dnfrepo.InventoryFieldWarehouse {
			changedWarehouse = true
		} else {
			changedSlots = true
		}
		result.Removed = append(result.Removed, DeletedItem{
			ListType:       mutation.listType,
			SlotIndex:      mutation.slotIndex,
			ItemID:         stack.ItemID,
			RequestedCount: mutation.count,
			AppliedCount:   applied,
			RemainingCount: remaining,
		})
	}

	fields := make([]dnfrepo.InventoryField, 0, 2)
	if changedSlots {
		fields = append(fields, dnfrepo.InventoryFieldSlots)
	}
	if changedWarehouse {
		fields = append(fields, dnfrepo.InventoryFieldWarehouse)
	}
	if len(fields) == 0 && !changedAccount {
		return result, nil
	}
	now := time.Now()
	if len(fields) > 0 {
		record.UpdatedAt = now
		if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, fields...); err != nil {
			return DeleteResult{}, err
		}
	}
	if changedAccount {
		account.UpdatedAt = now
		if err := o.accountRepo.Save(ctx, account); err != nil {
			return DeleteResult{}, err
		}
	}
	result.Changed = true
	return result, nil
}

// Sell 校验出售价格证据并扣减物品。
// 正价格出售需要钱包和背包同事务提交；当前仓储没有跨 repo 事务实现时拒绝，避免半提交。
func (o *Owner) Sell(ctx context.Context, cmd Command) (SellResult, error) {
	if o == nil || o.repo == nil {
		return SellResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return SellResult{}, ErrCharacterRequired
	}
	if err := checkInventoryRemovalListType(cmd.SourceListType); err != nil {
		return SellResult{}, err
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	if !o.inItemTransaction {
		var result SellResult
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.Sell(ctx, cmd)
			return err
		})
		return result, err
	}
	record, ok, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return SellResult{}, err
	}
	if !ok {
		return SellResult{}, ErrInventoryNotFound
	}

	record = dnfrepo.CloneInventory(record)
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if record.Warehouse == nil {
		record.Warehouse = make(map[string]dnfrepo.ItemStack)
	}

	items, field := itemMapForList(&record, cmd.SourceListType)
	key := slotKey(cmd.SourceListType, cmd.SourceSlotIndex)
	stack, ok := items[key]
	if !ok {
		return SellResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
	}
	if isEquipmentLocked(stack) {
		return SellResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrItemLocked, cmd.SourceListType, cmd.SourceSlotIndex)
	}
	sellGold, err := sellGoldOf(stack)
	if err != nil {
		return SellResult{}, err
	}
	if o.characters == nil {
		return SellResult{}, fmt.Errorf("%w: character repository missing", ErrWalletTxnRequired)
	}
	if o.characters == nil {
		return SellResult{}, fmt.Errorf("%w: character repository missing", ErrWalletTxnRequired)
	}
	character, ok, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return SellResult{}, err
	}
	if !ok {
		return SellResult{}, fmt.Errorf("%w: character=%s", ErrWalletTxnRequired, characterID)
	}

	applied := normalizeDeleteCount(stack.Count, int64(cmd.Count))
	goldDelta := sellGold * applied
	if goldDelta != 0 {
		return SellResult{}, fmt.Errorf("%w: goldDelta=%d", ErrWalletTxnRequired, goldDelta)
	}

	remaining := stack.Count - applied
	if remaining <= 0 {
		delete(items, key)
		remaining = 0
	} else {
		stack.Count = remaining
		items[key] = stack
	}

	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, field); err != nil {
		return SellResult{}, err
	}
	return SellResult{
		CharacterID: characterID,
		Sold: DeletedItem{
			ListType:       cmd.SourceListType,
			SlotIndex:      cmd.SourceSlotIndex,
			ItemID:         stack.ItemID,
			RequestedCount: int64(cmd.Count),
			AppliedCount:   applied,
			RemainingCount: remaining,
		},
		GoldDelta:   goldDelta,
		UpdatedGold: character.Stats["gold"],
		Changed:     true,
	}, nil
}

// Move 按当前已验证的安全子集移动背包格子，并只写 inventory repository。
// 装备穿脱、账号仓库和成功 ACK/刷新顺序仍需要 EXE/MCP 证据闭合后交给对应 owner。
func (o *Owner) Move(ctx context.Context, cmd Command) (MoveResult, error) {
	if o == nil || o.repo == nil {
		return MoveResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return MoveResult{}, ErrCharacterRequired
	}
	if cmd.SourceListType == listTypeAccountCargo || cmd.DestinationListType == listTypeAccountCargo {
		characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
		return o.moveAccountCargo(ctx, characterID, cmd)
	}
	if err := checkMoveListType(cmd.SourceListType); err != nil {
		return MoveResult{}, err
	}
	if err := checkMoveListType(cmd.DestinationListType); err != nil {
		return MoveResult{}, err
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	petSegment, petMove := petInventoryMoveSegment(cmd)
	if cmd.SourceListType == listTypePet && cmd.DestinationListType == listTypePet {
		if !petMove {
			return MoveResult{}, fmt.Errorf("%w: pet inventory move crosses container segments src=%d dst=%d", ErrUnsupportedList, cmd.SourceSlotIndex, cmd.DestinationSlotIndex)
		}
		if !o.inPetTransaction {
			return o.movePetInventory(ctx, characterID, cmd, petSegment)
		}
	}
	if dnfrepo.IsAccountSharedInventorySlot(cmd.SourceListType, cmd.SourceSlotIndex) ||
		dnfrepo.IsAccountSharedInventorySlot(cmd.DestinationListType, cmd.DestinationSlotIndex) {
		return o.moveAccountShared(ctx, characterID, cmd)
	}
	if !o.inItemTransaction {
		var result MoveResult
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.Move(ctx, cmd)
			return err
		})
		return result, err
	}
	record, ok, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return MoveResult{}, err
	}
	if !ok {
		return MoveResult{}, ErrInventoryNotFound
	}

	record = dnfrepo.CloneInventory(record)
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if record.Warehouse == nil {
		record.Warehouse = make(map[string]dnfrepo.ItemStack)
	}

	srcItems, srcField := itemMapForList(&record, cmd.SourceListType)
	dstItems, dstField := itemMapForList(&record, cmd.DestinationListType)
	srcKey := slotKey(cmd.SourceListType, cmd.SourceSlotIndex)
	dstKey := slotKey(cmd.DestinationListType, cmd.DestinationSlotIndex)
	result := MoveResult{
		CharacterID:          characterID,
		SourceListType:       cmd.SourceListType,
		SourceSlotIndex:      cmd.SourceSlotIndex,
		DestinationListType:  cmd.DestinationListType,
		DestinationSlotIndex: cmd.DestinationSlotIndex,
		MoveCount:            int64(cmd.MoveCount),
		Mode:                 "noop",
	}
	if srcField == dstField && srcKey == dstKey {
		return result, nil
	}

	source, sourceOK := srcItems[srcKey]
	destination, destinationOK := dstItems[dstKey]
	if !sourceOK && !destinationOK {
		return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
	}
	if sourceOK && source.Count <= 0 {
		return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d count=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex, source.Count)
	}
	if !sourceOK {
		if destination.Count <= 0 {
			return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d count=%d", ErrSlotNotFound, cmd.DestinationListType, cmd.DestinationSlotIndex, destination.Count)
		}
		moveCount := normalizeMoveCount(destination, int64(cmd.MoveCount))
		if moveCount <= 0 {
			return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d count=%d", ErrSlotNotFound, cmd.DestinationListType, cmd.DestinationSlotIndex, destination.Count)
		}
		excludedSlots := []int16{cmd.SourceSlotIndex}
		if cmd.SourceListType == cmd.DestinationListType {
			excludedSlots = append(excludedSlots, cmd.DestinationSlotIndex)
		}
		if !isMainItemQuickSlot(cmd.SourceListType, cmd.SourceSlotIndex) {
			if targetSlot, target, ok := findCompatibleStackSlot(
				srcItems,
				cmd.SourceListType,
				destination,
				moveCount,
				excludedSlots...,
			); ok {
				target.Count += moveCount
				updateStackRawAmount(&target)
				if destination.Count <= moveCount {
					delete(dstItems, dstKey)
				} else {
					destination.Count -= moveCount
					updateStackRawAmount(&destination)
					dstItems[dstKey] = destination
				}
				srcItems[slotKey(cmd.SourceListType, targetSlot)] = target
				result.MoveCount = moveCount
				result.Mode = "auto_stack_reverse"
				result, err = o.withMoveRefresh(ctx, cmd, result, &record, cmd.SourceListType, cmd.DestinationListType)
				if err != nil {
					return MoveResult{}, err
				}
				return saveMoveResult(ctx, o.repo, record, srcField, dstField, result)
			}
		}
		if canSplitStack(destination, moveCount) {
			moved := cloneStack(destination)
			moved.Count = moveCount
			updateStackRawAmount(&moved)
			destination.Count -= moveCount
			updateStackRawAmount(&destination)
			dstItems[dstKey] = destination
			srcItems[srcKey] = moved
			result.MoveCount = moveCount
			result.Mode = "reverse_split"
			if cmd.SourceListType == cmd.DestinationListType {
				result, err = o.withMoveRefresh(ctx, cmd, result, &record, cmd.SourceListType)
				if err != nil {
					return MoveResult{}, err
				}
			}
			return saveMoveResult(ctx, o.repo, record, srcField, dstField, result)
		}
		delete(dstItems, dstKey)
		srcItems[srcKey] = destination
		result.MoveCount = destination.Count
		result.Mode = "reverse_move"
		return saveMoveResult(ctx, o.repo, record, srcField, dstField, result)
	}

	moveCount := normalizeMoveCount(source, int64(cmd.MoveCount))
	if moveCount <= 0 {
		return MoveResult{}, fmt.Errorf("%w: list=%d slot=%d count=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex, source.Count)
	}
	if !destinationOK {
		excludedSlots := []int16{cmd.DestinationSlotIndex}
		if cmd.SourceListType == cmd.DestinationListType {
			excludedSlots = append(excludedSlots, cmd.SourceSlotIndex)
		}
		if !isMainItemQuickSlot(cmd.DestinationListType, cmd.DestinationSlotIndex) {
			if targetSlot, target, ok := findCompatibleStackSlot(
				dstItems,
				cmd.DestinationListType,
				source,
				moveCount,
				excludedSlots...,
			); ok {
				target.Count += moveCount
				updateStackRawAmount(&target)
				if source.Count <= moveCount {
					delete(srcItems, srcKey)
				} else {
					source.Count -= moveCount
					updateStackRawAmount(&source)
					srcItems[srcKey] = source
				}
				dstItems[slotKey(cmd.DestinationListType, targetSlot)] = target
				result.MoveCount = moveCount
				result.Mode = "auto_stack"
				result, err = o.withMoveRefresh(ctx, cmd, result, &record, cmd.SourceListType, cmd.DestinationListType)
				if err != nil {
					return MoveResult{}, err
				}
				return saveMoveResult(ctx, o.repo, record, srcField, dstField, result)
			}
		}
		if canSplitStack(source, moveCount) {
			moved := cloneStack(source)
			moved.Count = moveCount
			updateStackRawAmount(&moved)
			source.Count -= moveCount
			updateStackRawAmount(&source)
			srcItems[srcKey] = source
			dstItems[dstKey] = moved
			result.MoveCount = moveCount
			result.Mode = "split"
			if cmd.SourceListType == cmd.DestinationListType {
				result, err = o.withMoveRefresh(ctx, cmd, result, &record, cmd.SourceListType)
				if err != nil {
					return MoveResult{}, err
				}
			}
			return saveMoveResult(ctx, o.repo, record, srcField, dstField, result)
		}
		delete(srcItems, srcKey)
		dstItems[dstKey] = source
		result.MoveCount = source.Count
		result.Mode = "move"
		return saveMoveResult(ctx, o.repo, record, srcField, dstField, result)
	}

	if canStack(source, destination, moveCount) {
		destination.Count += moveCount
		updateStackRawAmount(&destination)
		if source.Count <= moveCount {
			delete(srcItems, srcKey)
		} else {
			source.Count -= moveCount
			updateStackRawAmount(&source)
			srcItems[srcKey] = source
		}
		dstItems[dstKey] = destination
		result.MoveCount = moveCount
		result.Mode = "stack"
		if isMainPersonalCargoCrossList(cmd.SourceListType, cmd.DestinationListType) {
			// The bridge can refresh the exact source and destination rows for
			// this explicit cross-container merge. Avoid constructing full
			// list snapshots that invalidate the client's active op19 objects.
			return saveMoveResult(ctx, o.repo, record, srcField, dstField, result)
		}
		result, err = o.withMoveRefresh(ctx, cmd, result, &record, cmd.SourceListType, cmd.DestinationListType)
		if err != nil {
			return MoveResult{}, err
		}
		return saveMoveResult(ctx, o.repo, record, srcField, dstField, result)
	}

	srcItems[srcKey] = destination
	dstItems[dstKey] = source
	result.MoveCount = source.Count
	result.Mode = "swap"
	return saveMoveResult(ctx, o.repo, record, srcField, dstField, result)
}

func isMainPersonalCargoCrossList(sourceListType, destinationListType byte) bool {
	return sourceListType != destinationListType &&
		(sourceListType == listTypeMain || sourceListType == listTypePersonalCargo) &&
		(destinationListType == listTypeMain || destinationListType == listTypePersonalCargo)
}

func (o *Owner) movePetInventory(ctx context.Context, characterID string, cmd Command, segment byte) (MoveResult, error) {
	if o.petItems == nil || o.pets == nil {
		return MoveResult{}, ErrPetInventoryOwnerRequired
	}
	var result MoveResult
	err := o.petItems.WithinCharacterPets(ctx, characterID, func(inventory dnfrepo.InventoryRepository, equipment dnfrepo.EquipmentRepository, pets dnfrepo.PetRepository) error {
		txOwner := *o
		txOwner.repo = inventory
		txOwner.pets = pets
		txOwner.inItemTransaction = true
		txOwner.inPetTransaction = true
		if segment == petInventorySegmentCreature {
			if _, err := creaturestate.ReconcileInventory(ctx, characterID, inventory, equipment, pets); err != nil {
				return err
			}
		}
		var err error
		result, err = txOwner.Move(ctx, cmd)
		if err != nil {
			return err
		}
		if segment == petInventorySegmentCreature && result.Changed {
			_, err = creaturestate.ReconcileInventory(ctx, characterID, inventory, equipment, pets)
		}
		return err
	})
	return result, err
}

const (
	petInventorySegmentCreature byte = iota + 1
	petInventorySegmentArtifact
	petInventorySegmentConsumable
)

func petInventoryMoveSegment(cmd Command) (byte, bool) {
	if cmd.SourceListType != listTypePet || cmd.DestinationListType != listTypePet {
		return 0, false
	}
	source := petInventorySlotSegment(cmd.SourceSlotIndex)
	destination := petInventorySlotSegment(cmd.DestinationSlotIndex)
	return source, source != 0 && source == destination
}

func petInventorySlotSegment(slot int16) byte {
	switch {
	case slot >= 0 && slot <= 139:
		return petInventorySegmentCreature
	case slot >= 140 && slot <= 188:
		return petInventorySegmentArtifact
	case slot >= 189 && slot <= 238:
		return petInventorySegmentConsumable
	default:
		return 0
	}
}

// Sort 按当前客户端已确认的 category 段整理物品栏，并只保存对应 inventory 字段。
// 当前 Go 仓储还没有 sort-lock 表，所以这里只实现无锁安全子集；
// Main/Avatar/PersonalCargo 的专用 op13 刷新均由 handler 在成功 ACK 后发送。
// Pet/list7 同时拥有 PetRecord 状态，必须由 typed pet owner 整体整理，通用 inventory owner fail-close。
func (o *Owner) Sort(ctx context.Context, cmd Command) (SortResult, error) {
	if o == nil || o.repo == nil {
		return SortResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return SortResult{}, ErrCharacterRequired
	}
	if err := checkSortListType(cmd.SourceListType); err != nil {
		return SortResult{}, err
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	if !o.inItemTransaction {
		var result SortResult
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.Sort(ctx, cmd)
			return err
		})
		return result, err
	}
	record, ok, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return SortResult{}, err
	}
	if !ok {
		return SortResult{}, ErrInventoryNotFound
	}

	record = dnfrepo.CloneInventory(record)
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if record.Warehouse == nil {
		record.Warehouse = make(map[string]dnfrepo.ItemStack)
	}

	items, field := itemMapForList(&record, cmd.SourceListType)
	start, end, ok := sortSegment(cmd.SourceListType, cmd.Category)
	result := SortResult{
		CharacterID: characterID,
		ListType:    cmd.SourceListType,
		Category:    cmd.Category,
		StartSlot:   start,
		EndSlot:     end,
		Mode:        "noop",
		Refresh:     cloneItemMap(items),
	}
	if !ok {
		return result, nil
	}
	candidates := sortCandidates(items, cmd.SourceListType, start, end)
	result.MovedCount = len(candidates)
	if len(candidates) == 0 {
		return result, nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.kind != right.kind {
			return left.kind < right.kind
		}
		if left.stack.ItemID != right.stack.ItemID {
			return left.stack.ItemID < right.stack.ItemID
		}
		return left.slot < right.slot
	})

	changed := false
	for index, candidate := range candidates {
		targetSlot := start + int16(index)
		if candidate.slot != targetSlot {
			changed = true
			break
		}
	}
	if !changed {
		return result, nil
	}

	for _, candidate := range candidates {
		delete(items, slotKey(cmd.SourceListType, candidate.slot))
	}
	for index, candidate := range candidates {
		targetSlot := start + int16(index)
		items[slotKey(cmd.SourceListType, targetSlot)] = candidate.stack
	}

	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, field); err != nil {
		return SortResult{}, err
	}
	result.Mode = "sort"
	result.Refresh = cloneItemMap(items)
	result.Changed = true
	return result, nil
}

// Repair 按当前仓储中已有的耐久证据修复背包/仓库装备。
// 这里只允许 repair_gold=0 的安全子集；穿戴装备、全修和金币扣减继续交给后续 equipment/wallet owner。
// Repair repairs one bag/cargo equipment stack per the 86JP TryRepairSingle
// model: runtime-PVF evidence prices the job, gold and durability commit in
// the same character-asset transaction, body[5]=1 with an active 魔王契约
// (586) is free, and body[7]=1 quick repair pays the pricetable rate.
// slot=-1 (repair all) is owned by the equipment owner upstream.
func (o *Owner) Repair(ctx context.Context, cmd Command, costResolver alignedcmd.RepairCostResolver) (RepairResult, error) {
	if o == nil || o.repo == nil || o.assets == nil {
		return RepairResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return RepairResult{}, ErrCharacterRequired
	}
	if cmd.SourceSlotIndex == -1 {
		return RepairResult{}, ErrRepairAllRequiresOwners
	}
	if err := checkRepairListType(cmd.SourceListType); err != nil {
		return RepairResult{}, err
	}
	if costResolver == nil {
		return RepairResult{}, ErrRepairCostMissing
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	var result RepairResult
	err := o.assets.WithinCharacterAssets(ctx, characterID, func(characters dnfrepo.CharacterRepository, inventoryRepo dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
		record, ok, err := inventoryRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInventoryNotFound
		}
		record = dnfrepo.CloneInventory(record)
		record.CharacterID = characterID
		if record.Slots == nil {
			record.Slots = make(map[string]dnfrepo.ItemStack)
		}
		if record.Warehouse == nil {
			record.Warehouse = make(map[string]dnfrepo.ItemStack)
		}

		items, field := itemMapForList(&record, cmd.SourceListType)
		key := slotKey(cmd.SourceListType, cmd.SourceSlotIndex)
		stack, ok := items[key]
		if !ok {
			return fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
		}
		oldDurability, _, err := repairDurabilityOf(stack)
		if err != nil {
			return err
		}
		evidence, err := costResolver(stack.ItemID)
		if err != nil {
			return err
		}
		maxDurability := evidence.MaxDurability
		if maxDurability <= 0 {
			return fmt.Errorf("%w: list=%d slot=%d item=%d", ErrRepairNotRepairable, cmd.SourceListType, cmd.SourceSlotIndex, stack.ItemID)
		}
		character, found, err := characters.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: character=%s", ErrWalletTxnRequired, characterID)
		}
		character = dnfrepo.CloneCharacter(character)
		if character.Stats == nil {
			return fmt.Errorf("%w: character=%s has no stats", ErrWalletTxnRequired, characterID)
		}
		gold := character.Stats["gold"]
		result = RepairResult{
			CharacterID:   characterID,
			ListType:      cmd.SourceListType,
			SlotIndex:     cmd.SourceSlotIndex,
			OldDurability: oldDurability,
			NewDurability: maxDurability,
		}
		if oldDurability >= maxDurability {
			result.NewDurability = oldDurability
			result.UpdatedGold = gold
			return nil
		}
		cost := dnfequip.CalcRepairCost(evidence, oldDurability, int(upgradeLevelOf(stack)), cmd.QuickRepair)
		freeRepair := false
		statsChanged := false
		if cost != 0 && cmd.AutoRepair && o.premiumActive(ctx, cmd.AccountID, premium.DevilSlotType(premium.DevilSlotAutoRepair)) {
			if premium.TryConsumeDaily(&character, premium.DevilSlotAutoRepair, time.Now().UTC()) {
				cost = 0
				freeRepair = true
				statsChanged = true
			}
		}
		if cost > gold {
			return fmt.Errorf("%w: cost=%d gold=%d", ErrRepairGoldInsufficient, cost, gold)
		}
		if cost > 0 {
			character.Stats["gold"] = gold - cost
			statsChanged = true
		}
		if statsChanged {
			character.UpdatedAt = time.Now()
			if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
				return err
			}
		}

		stack = cloneStack(stack)
		if stack.Extra == nil {
			stack.Extra = make(map[string]string, 1)
		}
		stack.Extra["durability"] = strconv.FormatInt(maxDurability, 10)
		if len(stack.RawEntry) >= 12 {
			stack.RawEntry = append([]byte(nil), stack.RawEntry...)
			stack.RawEntry[10] = byte(maxDurability)
			stack.RawEntry[11] = byte(maxDurability >> 8)
		}
		items[key] = stack

		record.UpdatedAt = time.Now()
		if err := dnfrepo.SaveInventoryFields(ctx, inventoryRepo, record, field); err != nil {
			return err
		}
		result.Cost = cost
		result.UpdatedGold = gold - cost
		result.Changed = true
		result.FreeRepair = freeRepair
		return nil
	})
	return result, err
}

// premiumActive reports whether the account currently holds an active premium
// contract of the given type. A missing account repository or record reads as
// inactive, so only the explicit free-repair bonus is gated by it.
func (o *Owner) premiumActive(ctx context.Context, accountID string, premiumType int64) bool {
	if o == nil || o.accounts == nil || strings.TrimSpace(accountID) == "" {
		return false
	}
	account, ok, err := o.accounts.Load(ctx, accountID)
	if err != nil || !ok {
		return false
	}
	return premium.Active(account, premiumType, time.Now().UTC())
}

// Upgrade applies the currently proven 86JP/current-EXE upgrade flow for
// backpack equipment: validate target, consume the runtime-PVF material from
// either character inventory or account-shared crystal/soul slots, persist the
// packed upgrade byte, then let the handler send op50 + op0D.
func (o *Owner) Upgrade(ctx context.Context, cmd Command) (UpgradeResult, error) {
	base := newUpgradeResult(cmd, upgradeErrorInvalidTarget)
	if o == nil || o.repo == nil {
		return UpgradeResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return UpgradeResult{}, ErrCharacterRequired
	}
	if cmd.Mode != "reinforce" && cmd.Mode != "amplify" {
		return newUpgradeResult(cmd, upgradeErrorWrongMode), nil
	}
	if cmd.OptionalTicketSlot >= 0 {
		return newUpgradeResult(cmd, upgradeErrorUnsupportedOptionalTicket), nil
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	if !o.inItemTransaction {
		var result UpgradeResult
		wrap := o.withinCharacterAssetTransaction
		if upgradeCommandRequiresAccountMaterial(cmd) {
			wrap = func(ctx context.Context, characterID string, apply func(*Owner) error) error {
				return o.withinAccountCharacterAssetTransaction(ctx, cmd.AccountID, characterID, apply)
			}
		}
		err := wrap(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.Upgrade(ctx, cmd)
			return err
		})
		if err != nil {
			return UpgradeResult{}, err
		}
		if result.CharacterID == "" {
			result = base
			result.CharacterID = characterID
		}
		return result, nil
	}

	record, ok, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return UpgradeResult{}, err
	}
	if !ok {
		return UpgradeResult{}, ErrInventoryNotFound
	}
	record = dnfrepo.CloneInventory(record)
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}

	result := base
	result.CharacterID = characterID
	items := record.Slots
	targetKey := slotKey(listTypeMain, cmd.TargetSlotIndex)
	target, ok := items[targetKey]
	if !ok || target.Count <= 0 || target.ItemID != int64(cmd.TargetItemTemplateID) || !isUpgradeableEquipmentStack(target) {
		return result, nil
	}
	if isEquipmentLocked(target) {
		result.ErrorCode = upgradeErrorLocked
		return result, nil
	}
	if !upgradeDurabilityOK(target) {
		result.ErrorCode = upgradeErrorDurability
		return result, nil
	}
	oldLevel := upgradeLevelOf(target)
	result.OldLevel = oldLevel
	result.NewLevel = oldLevel
	if oldLevel > upgradeMaximumCurrentLevelBeforeReject {
		result.ErrorCode = upgradeErrorMaxLevel
		return result, nil
	}

	amplifyType, amplifyValue := upgradeAmplifyState(target)
	switch cmd.Mode {
	case "reinforce":
		if amplifyType != 0 {
			result.ErrorCode = upgradeErrorWrongMode
			return result, nil
		}
	case "amplify":
		if amplifyType == 0 {
			result.ErrorCode = upgradeErrorWrongMode
			return result, nil
		}
		if amplifyValue <= 0 || (amplifyType&0x80) != 0 {
			result.ErrorCode = upgradeErrorAmplifyNotIdentified
			return result, nil
		}
	}

	const (
		upgradeMaterialNone = iota
		upgradeMaterialMain
		upgradeMaterialAccount
	)
	var materialKey string
	var material dnfrepo.ItemStack
	var accountMaterial dnfrepo.AccountInventoryRecord
	materialOwner := upgradeMaterialNone
	requiredMaterialItemID, requiredMaterialCount, hasPVFMaterial := upgradeMaterialRequirement(cmd)
	if cmd.MaterialSlotIndex >= 0 && cmd.MaterialSlotIndex == cmd.TargetSlotIndex {
		result.ErrorCode = upgradeErrorInvalidMaterial
		return result, nil
	}
	if hasPVFMaterial {
		if fixedSlot, accountOwned := upgradeAccountSharedMaterialSlot(requiredMaterialItemID); accountOwned {
			if o.accountRepo == nil || strings.TrimSpace(cmd.AccountID) == "" {
				result.ErrorCode = upgradeErrorInvalidMaterial
				return result, nil
			}
			account, found, err := o.accountRepo.Load(ctx, cmd.AccountID)
			if err != nil {
				return UpgradeResult{}, err
			}
			if !found || account.Slots == nil {
				result.ErrorCode = upgradeErrorInvalidMaterial
				return result, nil
			}
			accountMaterial = dnfrepo.CloneAccountInventory(account)
			accountMaterial.AccountID = strings.TrimSpace(cmd.AccountID)
			materialKey = dnfrepo.AccountSharedInventorySlotKey(fixedSlot)
			stack, ok := accountMaterial.Slots[materialKey]
			if !ok || stack.ItemID != requiredMaterialItemID || stack.Count < requiredMaterialCount {
				result.ErrorCode = upgradeErrorInvalidMaterial
				return result, nil
			}
			material = stack
			materialOwner = upgradeMaterialAccount
		} else {
			if cmd.MaterialSlotIndex < 0 || dnfrepo.IsAccountSharedInventorySlot(listTypeMain, cmd.MaterialSlotIndex) {
				result.ErrorCode = upgradeErrorInvalidMaterial
				return result, nil
			}
			materialKey = slotKey(listTypeMain, cmd.MaterialSlotIndex)
			stack, ok := items[materialKey]
			if !ok || stack.ItemID != requiredMaterialItemID || stack.Count < requiredMaterialCount {
				result.ErrorCode = upgradeErrorInvalidMaterial
				return result, nil
			}
			material = stack
			materialOwner = upgradeMaterialMain
		}
	} else if cmd.MaterialSlotIndex >= 0 {
		if dnfrepo.IsAccountSharedInventorySlot(listTypeMain, cmd.MaterialSlotIndex) {
			result.ErrorCode = upgradeErrorInvalidMaterial
			return result, nil
		}
		materialKey = slotKey(listTypeMain, cmd.MaterialSlotIndex)
		stack, ok := items[materialKey]
		if !ok || stack.Count <= 0 {
			result.ErrorCode = upgradeErrorInvalidMaterial
			return result, nil
		}
		requiredMaterialCount = 1
		material = stack
		materialOwner = upgradeMaterialMain
	}

	character, ok, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return UpgradeResult{}, err
	}
	if !ok {
		return UpgradeResult{}, fmt.Errorf("%w: character=%s", ErrWalletTxnRequired, characterID)
	}
	if character.Stats == nil {
		character.Stats = make(map[string]int64, 1)
	}
	result.UpdatedGold = character.Stats["gold"]
	goldCost := upgradeGoldCostOf(target, cmd.Mode, oldLevel)
	if goldCost > result.UpdatedGold {
		result.ErrorCode = upgradeErrorInsufficientGold
		result.GoldCost = goldCost
		return result, nil
	}

	// RNG: determine success or failure based on PVF success weight.
	successWeight := cmd.UpgradeSuccessWeight
	if successWeight <= 0 {
		successWeight = currentUpgradeSuccessBase // backward compat: guaranteed success
	}
	rolled := rand.Intn(currentUpgradeSuccessBase)
	upgradeSuccess := rolled < successWeight

	var newLevel byte
	var resultCode byte
	if upgradeSuccess {
		newLevel = byte(int(oldLevel) + 1)
		resultCode = upgradeResultCodeSuccess
		target = cloneStack(target)
		setUpgradeLevel(&target, newLevel)
		items[targetKey] = target
	} else {
		newLevel = oldLevel
		switch cmd.UpgradePenaltyType {
		case 3: // destroy
			resultCode = upgradeResultCodeDestroy
			delete(items, targetKey)
			// Grant destruction compensation item.
			if cmd.UpgradeDestroyBonusItemID > 0 && cmd.UpgradeDestroyBonusCount > 0 {
				destroySlot := firstEmptyUpgradeSlot(items)
				if destroySlot >= 0 {
					bonusStack := dnfrepo.ItemStack{
						ItemID: int64(cmd.UpgradeDestroyBonusItemID),
						Count:  int64(cmd.UpgradeDestroyBonusCount),
						Extra:  map[string]string{"source": "upgrade_destroy_bonus"},
					}
					updateStackRawAmount(&bonusStack)
					items[slotKey(listTypeMain, destroySlot)] = bonusStack
					result.DestroyBonusSlot = destroySlot
					result.DestroyBonusItemID = int32(cmd.UpgradeDestroyBonusItemID)
					result.DestroyBonusCount = int32(cmd.UpgradeDestroyBonusCount)
				}
			}
		case 1: // downgrade
			resultCode = upgradeResultCodeDowngrade
			if oldLevel > 0 {
				newLevel = oldLevel - 1
			}
			target = cloneStack(target)
			setUpgradeLevel(&target, newLevel)
			items[targetKey] = target
		default: // 0: fail, no change
			resultCode = upgradeResultCodeFailNoChange
		}
	}

	if materialOwner != upgradeMaterialNone {
		material = cloneStack(material)
		material.Count -= requiredMaterialCount
		result.MaterialRemainingStackCount = material.Count
		result.MaterialUpdated = true
		if material.Count <= 0 {
			result.MaterialRemainingStackCount = 0
			result.MaterialUpdatedStack = dnfrepo.ItemStack{}
			switch materialOwner {
			case upgradeMaterialMain:
				delete(items, materialKey)
			case upgradeMaterialAccount:
				delete(accountMaterial.Slots, materialKey)
			}
		} else {
			updateStackRawAmount(&material)
			result.MaterialUpdatedStack = material
			switch materialOwner {
			case upgradeMaterialMain:
				items[materialKey] = material
			case upgradeMaterialAccount:
				accountMaterial.Slots[materialKey] = material
			}
		}
		if materialOwner == upgradeMaterialAccount {
			accountMaterial.UpdatedAt = time.Now()
			if err := o.accountRepo.Save(ctx, accountMaterial); err != nil {
				return UpgradeResult{}, err
			}
		}
	}
	if goldCost > 0 {
		character = dnfrepo.CloneCharacter(character)
		if character.Stats == nil {
			character.Stats = make(map[string]int64, 1)
		}
		character.Stats["gold"] -= goldCost
		character.UpdatedAt = time.Now()
		if err := dnfrepo.SaveCharacterFields(ctx, o.characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return UpgradeResult{}, err
		}
		result.GoldCost = goldCost
		result.UpdatedGold = character.Stats["gold"]
	}

	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, dnfrepo.InventoryFieldSlots); err != nil {
		return UpgradeResult{}, err
	}
	result.Success = true
	result.ErrorCode = 0
	result.NewLevel = newLevel
	result.ResultCode = resultCode
	result.UpgradeSucceeded = upgradeSuccess
	if updatedTarget, ok := items[targetKey]; ok {
		result.TargetUpdatedStack = updatedTarget
	} else {
		result.TargetUpdatedStack = dnfrepo.ItemStack{}
	}
	result.MainRefresh = cloneItemMap(record.Slots)
	result.Changed = true
	return result, nil
}

// Enchant applies one monster card carried by a bead stackable to one main
// inventory equipment entry or one list-7 creature. The durable write surface
// matches the current 86JP reference: entry +0x0E i32 card item id and +0x12
// u8 upgrade count; list-7 creatures additionally update their typed PetEntry
// in the same character-pet transaction. The server never resolves the card's
// stat values because the client renders them from its own card PVF.
func (o *Owner) Enchant(ctx context.Context, cmd Command, resolver alignedcmd.EnchantBeadResolver) (EnchantResult, error) {
	base := EnchantResult{
		ErrorCode:       enchantErrorInvalidBead,
		TargetListType:  cmd.TargetListType,
		TargetSlotIndex: cmd.TargetSlotIndex,
		BeadSlotIndex:   cmd.BeadSlotIndex,
	}
	if o == nil || o.repo == nil {
		return EnchantResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return EnchantResult{}, ErrCharacterRequired
	}
	if resolver == nil {
		return EnchantResult{}, ErrEnchantResolverRequired
	}
	if cmd.BeadListType != listTypeMain || (cmd.TargetListType != listTypeMain && cmd.TargetListType != listTypePet) {
		base.ErrorCode = enchantErrorUnsupported
		return base, nil
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	base.CharacterID = characterID
	if cmd.TargetListType == listTypePet && !o.inPetTransaction {
		if o.petItems == nil || o.pets == nil {
			return EnchantResult{}, ErrPetInventoryOwnerRequired
		}
		var result EnchantResult
		err := o.petItems.WithinCharacterPets(ctx, characterID, func(inventory dnfrepo.InventoryRepository, equipment dnfrepo.EquipmentRepository, pets dnfrepo.PetRepository) error {
			txOwner := *o
			txOwner.repo = inventory
			txOwner.pets = pets
			txOwner.inItemTransaction = true
			txOwner.inPetTransaction = true
			if _, err := creaturestate.ReconcileInventory(ctx, characterID, inventory, equipment, pets); err != nil {
				return err
			}
			var err error
			result, err = txOwner.Enchant(ctx, cmd, resolver)
			return err
		})
		if err != nil {
			return EnchantResult{}, err
		}
		if result.CharacterID == "" {
			result = base
		}
		return result, nil
	}
	if cmd.TargetListType == listTypeMain && !o.inItemTransaction {
		var result EnchantResult
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.Enchant(ctx, cmd, resolver)
			return err
		})
		if err != nil {
			return EnchantResult{}, err
		}
		if result.CharacterID == "" {
			result = base
		}
		return result, nil
	}

	record, ok, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return EnchantResult{}, err
	}
	if !ok {
		return EnchantResult{}, ErrInventoryNotFound
	}
	record = dnfrepo.CloneInventory(record)
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}

	result := base
	items := record.Slots
	beadKey := slotKey(listTypeMain, cmd.BeadSlotIndex)
	bead, ok := items[beadKey]
	if !ok || bead.Count <= 0 {
		return result, nil
	}
	result.BeadItemID = bead.ItemID
	if cmd.TargetListType == cmd.BeadListType && cmd.TargetSlotIndex == cmd.BeadSlotIndex {
		result.ErrorCode = enchantErrorInvalidTarget
		return result, nil
	}
	targetKey := slotKey(cmd.TargetListType, cmd.TargetSlotIndex)
	target, ok := items[targetKey]
	if !ok || target.Count <= 0 {
		result.ErrorCode = enchantErrorInvalidTarget
		return result, nil
	}
	result.TargetItemID = target.ItemID

	resolution, err := resolver(bead.ItemID, target.ItemID)
	if err != nil {
		return EnchantResult{}, err
	}
	if resolution.CardItemID <= 0 {
		return result, nil
	}
	result.CardItemID = resolution.CardItemID
	if !strings.EqualFold(strings.TrimSpace(resolution.TargetKind), "equipment") {
		result.ErrorCode = enchantErrorInvalidTarget
		return result, nil
	}
	if len(resolution.TargetWhitelist) > 0 && !int64SliceContains(resolution.TargetWhitelist, target.ItemID) {
		result.ErrorCode = enchantErrorInvalidTarget
		return result, nil
	}
	if !stringSliceContainsFold(resolution.AllowedEquipmentTypes, resolution.TargetEquipmentType) {
		result.ErrorCode = enchantErrorInvalidTarget
		return result, nil
	}
	var petRecord dnfrepo.PetRecord
	var petEntryKey string
	var petEntry dnfrepo.PetEntry
	if cmd.TargetListType == listTypePet {
		if cmd.TargetSlotIndex < 0 || cmd.TargetSlotIndex > 139 ||
			!strings.EqualFold(strings.Trim(strings.TrimSpace(resolution.TargetEquipmentType), "[]"), "creature") {
			result.ErrorCode = enchantErrorInvalidTarget
			return result, nil
		}
		if o.pets == nil {
			return EnchantResult{}, ErrPetInventoryOwnerRequired
		}
		var found bool
		petRecord, found, err = o.pets.Load(ctx, characterID)
		if err != nil {
			return EnchantResult{}, err
		}
		if !found {
			result.ErrorCode = enchantErrorInvalidTarget
			return result, nil
		}
		petRecord = dnfrepo.ClonePet(petRecord)
		petEntryKey, petEntry, found = enchantPetEntryForTarget(petRecord, target, cmd.TargetSlotIndex)
		if !found {
			result.ErrorCode = enchantErrorInvalidTarget
			return result, nil
		}
	}
	upgradeCount := enchantUpgradeCountOf(bead)
	if len(resolution.UpgradeCounts) > 0 {
		if !int64SliceContains(resolution.UpgradeCounts, int64(upgradeCount)) {
			result.ErrorCode = enchantErrorUnsupported
			return result, nil
		}
	} else if upgradeCount != 0 {
		result.ErrorCode = enchantErrorUnsupported
		return result, nil
	}

	target = cloneStack(target)
	setEnchantFields(&target, resolution.CardItemID, upgradeCount)
	items[targetKey] = target
	result.TargetUpdatedStack = cloneStack(target)
	if cmd.TargetListType == listTypePet {
		setPetEntryEnchantFields(&petEntry, resolution.CardItemID, upgradeCount)
		petRecord.Entries[petEntryKey] = petEntry
	}

	bead = cloneStack(bead)
	bead.Count--
	result.BeadRemainingCount = bead.Count
	if bead.Count <= 0 {
		delete(items, beadKey)
		result.BeadRemainingCount = 0
		result.BeadUpdatedStack = dnfrepo.ItemStack{ItemID: -1}
	} else {
		updateStackRawAmount(&bead)
		items[beadKey] = bead
		result.BeadUpdatedStack = cloneStack(bead)
	}

	now := time.Now()
	record.UpdatedAt = now
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, dnfrepo.InventoryFieldSlots); err != nil {
		return EnchantResult{}, err
	}
	if cmd.TargetListType == listTypePet {
		petRecord.CharacterID = characterID
		petRecord.UpdatedAt = now
		if err := dnfrepo.SavePetFields(ctx, o.pets, petRecord, dnfrepo.PetFieldEntries); err != nil {
			return EnchantResult{}, err
		}
	}
	result.Success = true
	result.ErrorCode = 0
	result.EnchantUpgradeCount = upgradeCount
	result.MainRefresh = cloneItemMap(record.Slots)
	result.Changed = true
	return result, nil
}

func enchantPetEntryForTarget(record dnfrepo.PetRecord, target dnfrepo.ItemStack, slot int16) (string, dnfrepo.PetEntry, bool) {
	serial := firstExtraInt(target.Extra, 0,
		"creature_serial_or_handle", "creature_key", "creature_serial", "pet_serial", "serial", "handle", "instance_value", "item_uid")
	if serial <= 0 || serial > math.MaxUint32 {
		return "", dnfrepo.PetEntry{}, false
	}
	for key, entry := range record.Entries {
		entrySerial := uint64(entry.CreatureKey)
		if entrySerial == 0 {
			parsed, err := strconv.ParseUint(strings.TrimSpace(entry.PetKey), 0, 32)
			if err != nil || parsed == 0 {
				parsed, err = strconv.ParseUint(strings.TrimSpace(key), 0, 32)
			}
			if err == nil {
				entrySerial = parsed
			}
		}
		if entrySerial != uint64(serial) || entry.ItemID != target.ItemID ||
			entry.SourceListType != listTypePet || entry.SourceSlotIndex != slot {
			continue
		}
		return key, entry, true
	}
	return "", dnfrepo.PetEntry{}, false
}

func setPetEntryEnchantFields(entry *dnfrepo.PetEntry, cardItemID int64, upgradeCount byte) {
	if entry == nil {
		return
	}
	if entry.Extra == nil {
		entry.Extra = make(map[string]string, 6)
	}
	entry.Extra["value_a"] = strconv.FormatInt(cardItemID, 10)
	entry.Extra["enchant_card_id"] = strconv.FormatInt(cardItemID, 10)
	entry.Extra["pet_enchant_card_item_id"] = strconv.FormatInt(cardItemID, 10)
	entry.Extra["byte_12"] = strconv.Itoa(int(upgradeCount))
	entry.Extra["enchant_upgrade_count"] = strconv.Itoa(int(upgradeCount))
	entry.Extra["pet_enchant_upgrade_count"] = strconv.Itoa(int(upgradeCount))
	if len(entry.RawEntry) == currentItemListEntrySize {
		entry.RawEntry = append([]byte(nil), entry.RawEntry...)
		binary.LittleEndian.PutUint32(entry.RawEntry[0x0E:0x12], uint32(cardItemID))
		entry.RawEntry[0x12] = upgradeCount
	}
}

// enchantUpgradeCountOf reads the bead's own dynamic upgrade count, which is
// carried in the same common-entry +0x12 byte that receives it on the target.
func enchantUpgradeCountOf(stack dnfrepo.ItemStack) byte {
	if value := firstExtraInt(stack.Extra, 0, "byte_12", "value_12", "enchant_upgrade_count"); value != 0 {
		return byte(value)
	}
	if len(stack.RawEntry) > 0x12 {
		return stack.RawEntry[0x12]
	}
	return 0
}

// setEnchantFields writes the enchant card id (+0x0E i32) and upgrade count
// (+0x12 u8) through the Extra/RawEntry dual-write contract used by the
// current-EXE 0x77 item entry serializer.
func setEnchantFields(stack *dnfrepo.ItemStack, cardItemID int64, upgradeCount byte) {
	if stack == nil {
		return
	}
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 4)
	}
	stack.Extra["value_a"] = strconv.FormatInt(cardItemID, 10)
	stack.Extra["enchant_card_id"] = strconv.FormatInt(cardItemID, 10)
	stack.Extra["byte_12"] = strconv.Itoa(int(upgradeCount))
	stack.Extra["enchant_upgrade_count"] = strconv.Itoa(int(upgradeCount))
	if len(stack.RawEntry) == currentItemListEntrySize {
		stack.RawEntry = append([]byte(nil), stack.RawEntry...)
		value := uint32(cardItemID)
		stack.RawEntry[0x0E] = byte(value)
		stack.RawEntry[0x0F] = byte(value >> 8)
		stack.RawEntry[0x10] = byte(value >> 16)
		stack.RawEntry[0x11] = byte(value >> 24)
		stack.RawEntry[0x12] = upgradeCount
	}
}

func int64SliceContains(values []int64, needle int64) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func stringSliceContainsFold(values []string, needle string) bool {
	trimmed := strings.TrimSpace(needle)
	if trimmed == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), trimmed) {
			return true
		}
	}
	return false
}

// UseStackable consumes one repository-backed item through the current op44
// contract. Most items remain PVF-proven [waste]; [random reward item] uses
// the same ACK and refresh path after its runtime-PVF outcome is committed.
// When premiumResolver proves the item is a premiumlist_new.etc contract item
// ([etc] family, never [waste]), the use activates the account-level premium
// contract instead of the waste path, inside one rental-asset transaction
// (item decrement + account premium upsert).
func (o *Owner) UseStackable(ctx context.Context, cmd Command, premiumResolver alignedcmd.PremiumContractResolver, randomRewardResolvers ...alignedcmd.RandomRewardItemResolver) (UseStackableResult, error) {
	if o == nil || o.repo == nil {
		return UseStackableResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return UseStackableResult{}, ErrCharacterRequired
	}
	if err := checkUseStackableListType(cmd.SourceListType); err != nil {
		return UseStackableResult{}, err
	}
	if cmd.SourceInstanceValue == 0 || cmd.ItemTemplateID <= 0 || cmd.ReservedValue != 0 {
		return UseStackableResult{}, fmt.Errorf(
			"%w: instance=0x%08X item=%d reserved=0x%08X",
			ErrUseStackableContractRequired,
			uint32(cmd.SourceInstanceValue),
			cmd.ItemTemplateID,
			cmd.ReservedValue,
		)
	}

	if premiumResolver != nil {
		resolution, err := premiumResolver(int64(cmd.ItemTemplateID))
		if err != nil {
			return UseStackableResult{}, fmt.Errorf(
				"%w: item=%d: %v",
				ErrPremiumContractResolveFailed,
				cmd.ItemTemplateID,
				err,
			)
		}
		if resolution.PremiumType > 0 {
			return o.usePremiumContract(ctx, cmd, resolution)
		}
	}
	if len(randomRewardResolvers) > 0 && randomRewardResolvers[0] != nil {
		resolution, err := randomRewardResolvers[0](int64(cmd.ItemTemplateID))
		if err != nil {
			return UseStackableResult{}, fmt.Errorf("%w: item=%d: %v", ErrRandomRewardResolveFailed, cmd.ItemTemplateID, err)
		}
		if resolution.SourceItemID != 0 {
			return o.useRandomRewardItem(ctx, cmd, resolution)
		}
	}

	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	if !o.inItemTransaction {
		var result UseStackableResult
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.UseStackable(ctx, cmd, premiumResolver)
			return err
		})
		return result, err
	}

	record, ok, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return UseStackableResult{}, err
	}
	if !ok {
		return UseStackableResult{}, ErrInventoryNotFound
	}
	record = dnfrepo.CloneInventory(record)
	if record.Slots == nil {
		return UseStackableResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
	}
	key := slotKey(cmd.SourceListType, cmd.SourceSlotIndex)
	stack, ok := record.Slots[key]
	if !ok || stack.Count <= 0 {
		return UseStackableResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
	}
	if stack.ItemID != int64(cmd.ItemTemplateID) {
		return UseStackableResult{}, fmt.Errorf(
			"%w: list=%d slot=%d want=%d got=%d",
			ErrItemMismatch,
			cmd.SourceListType,
			cmd.SourceSlotIndex,
			cmd.ItemTemplateID,
			stack.ItemID,
		)
	}
	pvfPath, stackableType, ok := pvfProvenWasteStack(stack)
	if !ok {
		return UseStackableResult{}, fmt.Errorf(
			"%w: list=%d slot=%d item=%d",
			ErrUseStackableContractRequired,
			cmd.SourceListType,
			cmd.SourceSlotIndex,
			stack.ItemID,
		)
	}
	reviveCoinConsumable := dnfrevivecoin.IsConsumable(stack)

	remaining := stack.Count - 1
	if remaining == 0 {
		delete(record.Slots, key)
	} else {
		stack = cloneStack(stack)
		stack.Count = remaining
		record.Slots[key] = stack
	}
	var reviveCoinWalletTotal int64
	if reviveCoinConsumable {
		reviveCoinWalletTotal, err = dnfrevivecoin.Grant(
			&record,
			1,
			"current_exe_op44_coin_general",
		)
		if err != nil {
			return UseStackableResult{}, err
		}
	}
	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, dnfrepo.InventoryFieldSlots); err != nil {
		return UseStackableResult{}, err
	}
	return UseStackableResult{
		CharacterID:             characterID,
		AccountID:               cmd.AccountID,
		ListType:                cmd.SourceListType,
		SlotIndex:               cmd.SourceSlotIndex,
		InstanceValue:           cmd.SourceInstanceValue,
		ItemID:                  stack.ItemID,
		RemainingCount:          remaining,
		Changed:                 true,
		PVFPath:                 pvfPath,
		StackableType:           stackableType,
		ReviveCoinWalletUpdated: reviveCoinConsumable,
		ReviveCoinWalletTotal:   reviveCoinWalletTotal,
	}, nil
}

func (o *Owner) useRandomRewardItem(ctx context.Context, cmd Command, resolution alignedcmd.RandomRewardItemResolution) (UseStackableResult, error) {
	if resolution.SourceItemID != int64(cmd.ItemTemplateID) || strings.TrimSpace(resolution.SourcePVFPath) == "" ||
		!strings.EqualFold(strings.Trim(resolution.StackableType, "` []"), "random reward item") || len(resolution.Outcomes) == 0 {
		return UseStackableResult{}, fmt.Errorf("%w: item=%d", ErrRandomRewardContractRequired, cmd.ItemTemplateID)
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	if !o.inItemTransaction {
		var result UseStackableResult
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.useRandomRewardItem(ctx, cmd, resolution)
			return err
		})
		return result, err
	}
	record, ok, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return UseStackableResult{}, err
	}
	if !ok {
		return UseStackableResult{}, ErrInventoryNotFound
	}
	record = dnfrepo.CloneInventory(record)
	key := slotKey(cmd.SourceListType, cmd.SourceSlotIndex)
	source, ok := record.Slots[key]
	if !ok || source.Count <= 0 {
		return UseStackableResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
	}
	if source.ItemID != int64(cmd.ItemTemplateID) {
		return UseStackableResult{}, fmt.Errorf("%w: list=%d slot=%d want=%d got=%d", ErrItemMismatch, cmd.SourceListType, cmd.SourceSlotIndex, cmd.ItemTemplateID, source.ItemID)
	}
	outcome, err := chooseRandomRewardItemOutcome(resolution.Outcomes)
	if err != nil {
		return UseStackableResult{}, err
	}
	remaining := source.Count - 1
	if remaining == 0 {
		delete(record.Slots, key)
	} else {
		source = cloneStack(source)
		source.Count = remaining
		updateStackRawAmount(&source)
		record.Slots[key] = source
	}
	result := UseStackableResult{
		CharacterID:    characterID,
		AccountID:      cmd.AccountID,
		ListType:       cmd.SourceListType,
		SlotIndex:      cmd.SourceSlotIndex,
		InstanceValue:  cmd.SourceInstanceValue,
		ItemID:         int64(cmd.ItemTemplateID),
		RemainingCount: remaining,
		Changed:        true,
		PVFPath:        resolution.SourcePVFPath,
		StackableType:  resolution.StackableType,
	}
	if outcome.Reward.ItemID > 0 {
		slots, err := grantRandomRewardItem(record.Slots, outcome.Reward, 1)
		if err != nil {
			return UseStackableResult{}, err
		}
		result.RandomRewardItemID = outcome.Reward.ItemID
		result.RandomRewardSlots = slots
	}
	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, dnfrepo.InventoryFieldSlots); err != nil {
		return UseStackableResult{}, err
	}
	return result, nil
}

func chooseRandomRewardItemOutcome(outcomes []alignedcmd.RandomRewardItemOutcome) (alignedcmd.RandomRewardItemOutcome, error) {
	var total int64
	for _, outcome := range outcomes {
		if outcome.Weight <= 0 {
			continue
		}
		if total > int64(^uint64(0)>>1)-outcome.Weight {
			return alignedcmd.RandomRewardItemOutcome{}, ErrRandomRewardContractRequired
		}
		total += outcome.Weight
	}
	if total <= 0 {
		return alignedcmd.RandomRewardItemOutcome{}, ErrRandomRewardContractRequired
	}
	roll := rand.Int63n(total)
	for _, outcome := range outcomes {
		if outcome.Weight <= 0 {
			continue
		}
		if roll < outcome.Weight {
			return outcome, nil
		}
		roll -= outcome.Weight
	}
	return alignedcmd.RandomRewardItemOutcome{}, ErrRandomRewardContractRequired
}

func grantRandomRewardItem(items map[string]dnfrepo.ItemStack, reward alignedcmd.MagicBoxRewardItem, count int64) ([]int16, error) {
	if count <= 0 {
		return nil, nil
	}
	if reward.ItemID <= 0 || reward.Kind != "stackable" || reward.TargetListType != listTypeMain || reward.SlotStart < 0 || reward.SlotEnd < reward.SlotStart {
		return nil, fmt.Errorf("%w: item=%d kind=%q target_list=%d slots=%d..%d", ErrRandomRewardContractRequired, reward.ItemID, reward.Kind, reward.TargetListType, reward.SlotStart, reward.SlotEnd)
	}
	remaining := count
	changed := make([]int16, 0, 2)
	markChanged := func(slot int16) {
		for _, previous := range changed {
			if previous == slot {
				return
			}
		}
		changed = append(changed, slot)
	}
	for slot := reward.SlotStart; slot <= reward.SlotEnd && remaining > 0; slot++ {
		key := slotKey(listTypeMain, slot)
		stack, found := items[key]
		if !found || stack.ItemID != reward.ItemID || stack.Count <= 0 || reward.StackLimit == 1 || !randomRewardExpirationMatches(stack, reward.ExpireAt) {
			continue
		}
		capacity := remaining
		if reward.StackLimit > 0 {
			capacity = reward.StackLimit - stack.Count
		}
		if capacity <= 0 {
			continue
		}
		add := remaining
		if add > capacity {
			add = capacity
		}
		stack = cloneStack(stack)
		stack.Count += add
		updateStackRawAmount(&stack)
		items[key] = stack
		remaining -= add
		markChanged(slot)
	}
	for slot := reward.SlotStart; slot <= reward.SlotEnd && remaining > 0; slot++ {
		key := slotKey(listTypeMain, slot)
		if _, occupied := items[key]; occupied {
			continue
		}
		insert := remaining
		if reward.StackLimit > 0 && insert > reward.StackLimit {
			insert = reward.StackLimit
		}
		stack := dnfrepo.ItemStack{
			ItemID:   reward.ItemID,
			Count:    insert,
			ExpireAt: reward.ExpireAt,
			Extra: map[string]string{
				"item_kind": "stackable",
				"pvf_path":  reward.PVFPath,
			},
		}
		if !reward.ExpireAt.IsZero() && reward.ExpireAt.Unix() > 0 {
			expire := strconv.FormatInt(reward.ExpireAt.Unix(), 10)
			stack.Extra["expire_time"] = expire
			stack.Extra["expire_unix"] = expire
		}
		if reward.UsablePeriodDays > 0 {
			stack.Extra["usable_period_days"] = strconv.FormatInt(reward.UsablePeriodDays, 10)
			stack.Extra["expiration_source"] = "runtime_pvf_usable_period_grant"
		}
		updateStackRawAmount(&stack)
		items[key] = stack
		remaining -= insert
		markChanged(slot)
	}
	if remaining > 0 {
		return nil, fmt.Errorf("%w: item=%d remaining=%d", ErrRandomRewardInventoryFull, reward.ItemID, remaining)
	}
	return changed, nil
}

func randomRewardExpirationMatches(stack dnfrepo.ItemStack, expiration time.Time) bool {
	want := int64(0)
	if !expiration.IsZero() && expiration.Unix() > 0 {
		want = expiration.Unix()
	}
	got := int64(0)
	for _, key := range []string{"expire_time", "expire_unix"} {
		value, err := strconv.ParseInt(strings.TrimSpace(stack.Extra[key]), 10, 64)
		if err == nil && value > 0 {
			got = value
			break
		}
	}
	if got == 0 && !stack.ExpireAt.IsZero() && stack.ExpireAt.Unix() > 0 {
		got = stack.ExpireAt.Unix()
	}
	return got == want
}

// usePremiumContract activates one premiumlist_new.etc contract item: consume
// exactly one stack item and upsert the account-level premium expiry in the
// same rental-asset transaction, so a failure can never lose the item without
// the contract or grant the contract without the item. The resolver output is
// the only authority for type/duration; request and inventory-Extra metadata
// are never trusted.
func (o *Owner) usePremiumContract(ctx context.Context, cmd Command, resolution alignedcmd.PremiumContractResolution) (UseStackableResult, error) {
	if o.rentalAssets == nil {
		return UseStackableResult{}, ErrOwnerUnavailable
	}
	accountID := strings.TrimSpace(cmd.AccountID)
	if accountID == "" {
		return UseStackableResult{}, ErrAccountRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	var result UseStackableResult
	err := o.rentalAssets.WithinRentalAssets(ctx, accountID, characterID, func(accounts dnfrepo.AccountRepository, _ dnfrepo.CharacterRepository, inventoryRepo dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
		record, ok, err := inventoryRepo.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInventoryNotFound
		}
		record = dnfrepo.CloneInventory(record)
		if record.Slots == nil {
			return fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
		}
		key := slotKey(cmd.SourceListType, cmd.SourceSlotIndex)
		stack, ok := record.Slots[key]
		if !ok || stack.Count <= 0 {
			return fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
		}
		if stack.ItemID != int64(cmd.ItemTemplateID) {
			return fmt.Errorf(
				"%w: list=%d slot=%d want=%d got=%d",
				ErrItemMismatch,
				cmd.SourceListType,
				cmd.SourceSlotIndex,
				cmd.ItemTemplateID,
				stack.ItemID,
			)
		}

		account, found, err := accounts.Load(ctx, accountID)
		if err != nil {
			return err
		}
		if !found {
			return ErrAccountNotFound
		}
		account = dnfrepo.CloneAccount(account)

		remaining := stack.Count - 1
		if remaining == 0 {
			delete(record.Slots, key)
		} else {
			stack = cloneStack(stack)
			stack.Count = remaining
			record.Slots[key] = stack
		}
		now := time.Now()
		record.UpdatedAt = now
		if err := dnfrepo.SaveInventoryFields(ctx, inventoryRepo, record, dnfrepo.InventoryFieldSlots); err != nil {
			return err
		}

		premium.Upsert(&account, resolution.PremiumType, resolution.DurationSeconds, 1, now)
		account.UpdatedAt = now
		if err := accounts.Save(ctx, account); err != nil {
			return err
		}

		result = UseStackableResult{
			CharacterID:             characterID,
			AccountID:               accountID,
			ListType:                cmd.SourceListType,
			SlotIndex:               cmd.SourceSlotIndex,
			InstanceValue:           cmd.SourceInstanceValue,
			ItemID:                  stack.ItemID,
			RemainingCount:          remaining,
			Changed:                 true,
			PremiumActivated:        true,
			PremiumType:             resolution.PremiumType,
			PremiumRemainingSeconds: premium.ExpireAt(account, resolution.PremiumType) - now.Unix(),
		}
		return nil
	})
	return result, err
}

func newUpgradeResult(cmd Command, errorCode byte) UpgradeResult {
	return UpgradeResult{
		Mode:                    cmd.Mode,
		ErrorCode:               errorCode,
		TargetSlotIndex:         cmd.TargetSlotIndex,
		TargetItemTemplateID:    cmd.TargetItemTemplateID,
		MaterialSlotIndex:       cmd.MaterialSlotIndex,
		OptionalTicketSlotIndex: cmd.OptionalTicketSlot,
	}
}

func isUpgradeableEquipmentStack(stack dnfrepo.ItemStack) bool {
	if stack.ItemID <= 0 || stack.Count <= 0 {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(stack.Extra["item_kind"]))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(stack.Extra["kind"]))
	}
	switch kind {
	case "equipment":
		return true
	case "", "unknown":
		return stack.Count == 1
	default:
		return false
	}
}

func upgradeDurabilityOK(stack dnfrepo.ItemStack) bool {
	if stack.Extra == nil {
		return true
	}
	maxRaw := strings.TrimSpace(stack.Extra["max_durability"])
	if maxRaw == "" {
		return true
	}
	maxValue, err := strconv.ParseInt(maxRaw, 0, 64)
	if err != nil || maxValue < 0 {
		return false
	}
	currentRaw := strings.TrimSpace(stack.Extra["durability"])
	if currentRaw == "" {
		return maxValue == 0
	}
	current, err := strconv.ParseInt(currentRaw, 0, 64)
	if err != nil || current < 0 {
		return false
	}
	return current == maxValue
}

func upgradeCommandRequiresAccountMaterial(cmd Command) bool {
	itemID, _, ok := upgradeMaterialRequirement(cmd)
	if !ok {
		return dnfrepo.IsAccountSharedInventorySlot(listTypeMain, cmd.MaterialSlotIndex)
	}
	_, accountOwned := upgradeAccountSharedMaterialSlot(itemID)
	return accountOwned
}

func upgradeMaterialRequirement(cmd Command) (int64, int64, bool) {
	itemID := int64(cmd.UpgradeMaterialItemID)
	count := int64(cmd.UpgradeMaterialCount)
	if itemID <= 0 || count <= 0 {
		return 0, 0, false
	}
	return itemID, count, true
}

func upgradeAccountSharedMaterialSlot(itemID int64) (int16, bool) {
	switch itemID {
	case 3033:
		return 354, true
	case 3034:
		return 355, true
	case 3035:
		return 356, true
	case 3036:
		return 357, true
	case 3037:
		return 358, true
	case 3262:
		return 359, true
	case 10100115:
		return 360, true
	case 10100116:
		return 361, true
	case 10099773:
		return 362, true
	case 10099774:
		return 363, true
	case 10099775:
		return 364, true
	case 10158124:
		return 365, true
	default:
		return 0, false
	}
}

// firstEmptyUpgradeSlot finds the first empty main-inventory slot for destroy
// bonus item placement (searches quickbar 60..69 first, then general 9..59).
func firstEmptyUpgradeSlot(items map[string]dnfrepo.ItemStack) int16 {
	for slot := int16(60); slot <= 69; slot++ {
		if _, ok := items[slotKey(listTypeMain, slot)]; !ok {
			return slot
		}
	}
	for slot := int16(9); slot <= 59; slot++ {
		if _, ok := items[slotKey(listTypeMain, slot)]; !ok {
			return slot
		}
	}
	return -1
}

func upgradeGoldCostOf(stack dnfrepo.ItemStack, mode string, oldLevel byte) int64 {
	mode = strings.ToLower(strings.TrimSpace(mode))
	levelKey := fmt.Sprintf("%s_upgrade_gold_%d", mode, oldLevel)
	return firstExtraInt(
		stack.Extra,
		0,
		levelKey,
		mode+"_upgrade_gold",
		mode+"_gold_cost",
		"upgrade_gold_"+strconv.Itoa(int(oldLevel)),
		"upgrade_gold",
		"upgrade_gold_cost",
		"gold_cost",
	)
}

func upgradeLevelOf(stack dnfrepo.ItemStack) byte {
	packed := packedUpgradeAttrOf(stack)
	if packed != 0 {
		return packed & 0x1F
	}
	if value := firstExtraInt(stack.Extra, 0, "reinforce", "upgrade_level", "enchant_upgrade", "enchant_upgrade_count"); value != 0 {
		return byte(value) & 0x1F
	}
	if len(stack.RawEntry) > 0x0A {
		return stack.RawEntry[0x0A] & 0x1F
	}
	return 0
}

func packedUpgradeAttrOf(stack dnfrepo.ItemStack) byte {
	if stack.Extra != nil {
		if value := firstExtraInt(stack.Extra, 0, "ext_data0", "ext0", "packed_flag_byte", "packed_flag", "packed"); value != 0 {
			return byte(value)
		}
	}
	if len(stack.RawEntry) > 0x0A {
		return stack.RawEntry[0x0A]
	}
	return 0
}

func setUpgradeLevel(stack *dnfrepo.ItemStack, level byte) {
	if stack == nil {
		return
	}
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 4)
	}
	packed := (packedUpgradeAttrOf(*stack) & 0xE0) | (level & 0x1F)
	stack.Extra["ext_data0"] = strconv.Itoa(int(packed))
	stack.Extra["packed_flag_byte"] = strconv.Itoa(int(packed))
	stack.Extra["reinforce"] = strconv.Itoa(int(level))
	stack.Extra["upgrade_level"] = strconv.Itoa(int(level))
	if len(stack.RawEntry) == currentItemListEntrySize {
		stack.RawEntry = append([]byte(nil), stack.RawEntry...)
		stack.RawEntry[0x0A] = packed
	}
}

func updateStackRawAmount(stack *dnfrepo.ItemStack) {
	if stack == nil {
		return
	}
	value := uint32(clampInt32(stack.Count))
	if len(stack.RawEntry) == currentItemListEntrySize {
		stack.RawEntry = append([]byte(nil), stack.RawEntry...)
		binary.LittleEndian.PutUint32(stack.RawEntry[0x06:0x0A], value)
	}
	if len(stack.Extra) == 0 {
		return
	}
	extra := make(map[string]string, len(stack.Extra))
	for key, raw := range stack.Extra {
		extra[key] = raw
	}
	changed := false
	for _, key := range []string{"amount_or_count", "amount", "count", "stack", "quantity"} {
		if _, exists := extra[key]; !exists {
			continue
		}
		extra[key] = strconv.FormatUint(uint64(value), 10)
		changed = true
	}
	if changed {
		stack.Extra = extra
	}
}

func upgradeAmplifyState(stack dnfrepo.ItemStack) (byte, int64) {
	ampType := byte(firstExtraInt(stack.Extra, 0, "amplify_type", "amplification_type", "amplify_attr", "amplification_attr", "byte_13", "value_13", "value_c"))
	ampValue := firstExtraInt(stack.Extra, 0, "amplify_value", "amplification_value", "amplify_bonus", "amplification_bonus", "marker_16", "marker16", "value_d")
	if ampType == 0 && len(stack.RawEntry) > 0x13 {
		ampType = stack.RawEntry[0x13]
	}
	if ampValue == 0 && len(stack.RawEntry) > 0x15 {
		ampValue = int64(stack.RawEntry[0x14]) | int64(stack.RawEntry[0x15])<<8
	}
	prefix := fixedExtraBytes(stack.Extra, 8, "prefix_data_0e", "prefix0e", "raw_data_0e")
	if ampType == 0 && len(prefix) >= 6 {
		ampType = prefix[5]
	}
	if ampValue == 0 && len(prefix) >= 8 {
		ampValue = int64(prefix[6]) | int64(prefix[7])<<8
	}
	return ampType, ampValue
}

func pvfProvenWasteStack(stack dnfrepo.ItemStack) (string, string, bool) {
	if stack.Extra == nil || !strings.EqualFold(strings.TrimSpace(stack.Extra["item_kind"]), "stackable") {
		return "", "", false
	}
	pvfPath := strings.TrimSpace(stack.Extra["pvf_path"])
	stackableType := strings.TrimSpace(strings.ReplaceAll(stack.Extra["stackable_type"], "`", ""))
	if pvfPath == "" || !strings.HasPrefix(strings.ToLower(stackableType), "[waste]") {
		return "", "", false
	}
	return pvfPath, stackableType, true
}

type deleteMutation struct {
	listType  byte
	slotIndex int16
	itemID    int32
	count     int64
}

func deleteMutations(cmd Command) []deleteMutation {
	if len(cmd.DeleteEntries) == 0 {
		return []deleteMutation{{
			listType:  cmd.SourceListType,
			slotIndex: cmd.SourceSlotIndex,
			itemID:    cmd.ItemTemplateID,
			count:     int64(cmd.Count),
		}}
	}
	out := make([]deleteMutation, 0, len(cmd.DeleteEntries))
	for _, entry := range cmd.DeleteEntries {
		out = append(out, deleteMutation{
			listType:  cmd.SourceListType,
			slotIndex: entry.SlotIndex,
			itemID:    entry.ItemID,
			count:     int64(entry.DeleteCount),
		})
	}
	return out
}

func checkDeleteListType(listType byte) error {
	if listType == listTypeEquipment {
		return fmt.Errorf("%w: %d", ErrDeleteRequiresEquipmentOwner, listType)
	}
	return checkInventoryRemovalListType(listType)
}

func checkInventoryRemovalListType(listType byte) error {
	switch listType {
	case listTypePet:
		return fmt.Errorf("%w: %d", ErrPetInventoryOwnerRequired, listType)
	case listTypeMain, listTypePersonalCargo, listTypeAvatar:
		return nil
	default:
		return fmt.Errorf("%w: %d", ErrUnsupportedList, listType)
	}
}

func checkMoveListType(listType byte) error {
	switch listType {
	case listTypeEquipment:
		return fmt.Errorf("%w: %d", ErrMoveRequiresEquipmentOwner, listType)
	case listTypeMain, listTypePersonalCargo, listTypeAvatar, listTypePet:
		return nil
	default:
		return fmt.Errorf("%w: %d", ErrUnsupportedList, listType)
	}
}

func isMainItemQuickSlot(listType byte, slot int16) bool {
	return listType == listTypeMain && slot >= 3 && slot <= 8
}

func checkSortListType(listType byte) error {
	switch listType {
	case listTypeEquipment:
		return fmt.Errorf("%w: %d", ErrSortRequiresEquipmentOwner, listType)
	case listTypeAccountCargo:
		return fmt.Errorf("%w: %d", ErrSortAccountCargoUnsupported, listType)
	case listTypePet:
		return fmt.Errorf("%w: %d", ErrPetInventoryOwnerRequired, listType)
	case listTypeMain, listTypePersonalCargo, listTypeAvatar:
		return nil
	default:
		return fmt.Errorf("%w: %d", ErrUnsupportedList, listType)
	}
}

func checkRepairListType(listType byte) error {
	switch listType {
	case listTypeEquipment:
		return fmt.Errorf("%w: %d", ErrRepairRequiresEquipmentOwner, listType)
	case listTypeMain, listTypePersonalCargo:
		return nil
	default:
		return fmt.Errorf("%w: %d", ErrUnsupportedList, listType)
	}
}

func checkUseStackableListType(listType byte) error {
	if listType == listTypeMain {
		return nil
	}
	return fmt.Errorf("%w: %d", ErrUnsupportedList, listType)
}

func sortSegment(listType byte, category byte) (int16, int16, bool) {
	switch listType {
	case listTypeMain:
		switch category {
		case 1:
			return 9, 64, true
		case 2:
			return 65, 120, true
		case 3:
			return 121, 176, true
		case 4:
			return 177, 232, true
		case 10:
			return 233, 288, true
		default:
			return 0, 0, false
		}
	case listTypePet:
		switch category {
		case 5:
			return 0, 139, true
		case 6:
			return 140, 188, true
		case 7:
			// Current NoPack sub_232A9F0 accepts list-7 pet-consumable
			// slots 189 through 238 inclusive.
			return 189, 238, true
		default:
			return 0, 0, false
		}
	case listTypeAvatar:
		if category == 8 {
			return 0, 209, true
		}
	case listTypePersonalCargo:
		if category == 11 {
			return 0, 151, true
		}
	}
	return 0, 0, false
}

type sortCandidate struct {
	slot  int16
	kind  string
	stack dnfrepo.ItemStack
}

func sortCandidates(items map[string]dnfrepo.ItemStack, listType byte, start int16, end int16) []sortCandidate {
	out := make([]sortCandidate, 0)
	for key, stack := range items {
		keyListType, slot, ok := parseSlotKey(key)
		if !ok || keyListType != listType || slot < start || slot > end {
			continue
		}
		out = append(out, sortCandidate{
			slot:  slot,
			kind:  sortItemKind(stack),
			stack: stack,
		})
	}
	return out
}

func parseSlotKey(key string) (byte, int16, bool) {
	listRaw, slotRaw, ok := strings.Cut(key, ":")
	if !ok {
		return 0, 0, false
	}
	listValue, err := strconv.ParseInt(strings.TrimSpace(listRaw), 10, 16)
	if err != nil || listValue < 0 || listValue > 255 {
		return 0, 0, false
	}
	slotValue, err := strconv.ParseInt(strings.TrimSpace(slotRaw), 10, 16)
	if err != nil {
		return 0, 0, false
	}
	return byte(listValue), int16(slotValue), true
}

func sortItemKind(stack dnfrepo.ItemStack) string {
	if stack.Extra == nil {
		return "unknown"
	}
	raw := strings.TrimSpace(stack.Extra["item_kind"])
	if raw == "" {
		raw = strings.TrimSpace(stack.Extra["kind"])
	}
	if raw == "" {
		return "unknown"
	}
	return strings.ToLower(raw)
}

func itemMapForList(record *dnfrepo.InventoryRecord, listType byte) (map[string]dnfrepo.ItemStack, dnfrepo.InventoryField) {
	if listType == listTypePersonalCargo {
		return record.Warehouse, dnfrepo.InventoryFieldWarehouse
	}
	return record.Slots, dnfrepo.InventoryFieldSlots
}

func slotKey(listType byte, slotIndex int16) string {
	return fmt.Sprintf("%d:%d", listType, slotIndex)
}

func normalizeDeleteCount(stackCount int64, requested int64) int64 {
	if stackCount <= 0 {
		return 0
	}
	if requested <= 0 || requested >= stackCount {
		return stackCount
	}
	return requested
}

func normalizeMoveCount(stack dnfrepo.ItemStack, requested int64) int64 {
	if stack.Count <= 0 {
		return 0
	}
	if !isStackable(stack) {
		return stack.Count
	}
	if requested <= 0 || requested >= stack.Count {
		return stack.Count
	}
	return requested
}

func canSplitStack(stack dnfrepo.ItemStack, moveCount int64) bool {
	return isStackable(stack) && moveCount > 0 && moveCount < stack.Count
}

func canStack(source dnfrepo.ItemStack, destination dnfrepo.ItemStack, moveCount int64) bool {
	if moveCount <= 0 || source.Count < moveCount || destination.Count <= 0 {
		return false
	}
	if source.ItemID != destination.ItemID || source.Bind != destination.Bind || !source.ExpireAt.Equal(destination.ExpireAt) {
		return false
	}
	sourcePath, sourceLimit, sourceOK := pvfStackContract(source)
	destinationPath, destinationLimit, destinationOK := pvfStackContract(destination)
	if !sourceOK || !destinationOK || sourcePath != destinationPath || sourceLimit != destinationLimit {
		return false
	}
	if sourceLimit == 0 {
		// Runtime PVF uses an omitted [stack limit] as an uncapped stackable,
		// not as evidence that the item cannot stack. The current client and
		// the 86JP reference keep these counts in a signed 32-bit value.
		return moveCount <= math.MaxInt32 && destination.Count <= math.MaxInt32-moveCount
	}
	return destination.Count <= destinationLimit-moveCount
}

func isStackable(stack dnfrepo.ItemStack) bool {
	_, _, ok := pvfStackContract(stack)
	return ok
}

func pvfStackContract(stack dnfrepo.ItemStack) (string, int64, bool) {
	if stack.ItemID <= 0 || stack.Extra == nil {
		return "", 0, false
	}
	if !strings.EqualFold(strings.TrimSpace(stack.Extra["item_kind"]), "stackable") {
		return "", 0, false
	}
	pvfPath := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(stack.Extra["pvf_path"], "\\", "/")))
	if !strings.HasPrefix(pvfPath, "stackable/") || strings.Contains(pvfPath, "../") {
		return "", 0, false
	}
	raw := strings.TrimSpace(stack.Extra["stack_limit"])
	if raw == "" {
		if stack.Count <= 0 || stack.Count > math.MaxInt32 {
			return "", 0, false
		}
		return pvfPath, 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 || stack.Count <= 0 {
		return "", 0, false
	}
	if value == 0 {
		if stack.Count > math.MaxInt32 {
			return "", 0, false
		}
		return pvfPath, 0, true
	}
	if stack.Count > value {
		return "", 0, false
	}
	return pvfPath, value, true
}

func findCompatibleStackSlot(
	items map[string]dnfrepo.ItemStack,
	listType byte,
	moving dnfrepo.ItemStack,
	moveCount int64,
	excludedSlots ...int16,
) (int16, dnfrepo.ItemStack, bool) {
	if listType != listTypeMain && listType != listTypeAvatar && listType != listTypePersonalCargo {
		return 0, dnfrepo.ItemStack{}, false
	}
	excluded := make(map[int16]struct{}, len(excludedSlots))
	for _, slot := range excludedSlots {
		excluded[slot] = struct{}{}
	}
	var selectedSlot int16
	var selected dnfrepo.ItemStack
	found := false
	for key, candidate := range items {
		keyListType, slot, ok := parseSlotKey(key)
		if !ok || keyListType != listType {
			continue
		}
		if _, skip := excluded[slot]; skip || dnfrepo.IsAccountSharedInventorySlot(listType, slot) {
			continue
		}
		if !canStack(moving, candidate, moveCount) {
			continue
		}
		if !found || slot < selectedSlot {
			selectedSlot = slot
			selected = candidate
			found = true
		}
	}
	return selectedSlot, selected, found
}

func (o *Owner) withMoveRefresh(
	ctx context.Context,
	cmd Command,
	result MoveResult,
	record *dnfrepo.InventoryRecord,
	listTypes ...byte,
) (MoveResult, error) {
	if record == nil {
		return result, nil
	}
	if result.Refresh == nil {
		result.Refresh = make(map[byte]map[string]dnfrepo.ItemStack, len(listTypes))
	}
	for _, listType := range listTypes {
		if listType != listTypeMain && listType != listTypeAvatar && listType != listTypePersonalCargo {
			continue
		}
		if _, exists := result.Refresh[listType]; exists {
			continue
		}
		items, _ := itemMapForList(record, listType)
		snapshot := cloneItemMap(items)
		if listType == listTypeMain {
			if o == nil || o.accountRepo == nil {
				return MoveResult{}, ErrAccountSharedOwnerUnavailable
			}
			accountID := strings.TrimSpace(cmd.AccountID)
			if accountID == "" {
				return MoveResult{}, ErrAccountRequired
			}
			account, _, err := o.accountRepo.Load(ctx, accountID)
			if err != nil {
				return MoveResult{}, err
			}
			merged := dnfrepo.MergeAccountSharedInventory(dnfrepo.InventoryRecord{Slots: snapshot}, account)
			snapshot = merged.Slots
		}
		result.Refresh[listType] = snapshot
		result.RefreshListTypes = append(result.RefreshListTypes, listType)
	}
	return result, nil
}

func isEquipmentLocked(stack dnfrepo.ItemStack) bool {
	if stack.Extra == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(stack.Extra["equipment_lock_state"])) {
	case "1", "2", "active", "locked", "unlocking", "pending_unlock":
		return true
	default:
		return false
	}
}

func cloneStack(stack dnfrepo.ItemStack) dnfrepo.ItemStack {
	stack.RawEntry = append([]byte(nil), stack.RawEntry...)
	if len(stack.Extra) == 0 {
		return stack
	}
	extra := make(map[string]string, len(stack.Extra))
	for key, value := range stack.Extra {
		extra[key] = value
	}
	stack.Extra = extra
	return stack
}

func cloneItemMap(items map[string]dnfrepo.ItemStack) map[string]dnfrepo.ItemStack {
	if len(items) == 0 {
		return map[string]dnfrepo.ItemStack{}
	}
	out := make(map[string]dnfrepo.ItemStack, len(items))
	for key, stack := range items {
		out[key] = cloneStack(stack)
	}
	return out
}

func saveMoveResult(ctx context.Context, repo dnfrepo.InventoryRepository, record dnfrepo.InventoryRecord, srcField dnfrepo.InventoryField, dstField dnfrepo.InventoryField, result MoveResult) (MoveResult, error) {
	fields := []dnfrepo.InventoryField{srcField}
	if dstField != srcField {
		fields = append(fields, dstField)
	}
	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, repo, record, fields...); err != nil {
		return MoveResult{}, err
	}
	result.Changed = true
	return result, nil
}

func sellGoldOf(stack dnfrepo.ItemStack) (int64, error) {
	if stack.Extra == nil {
		return 0, ErrSellPriceMissing
	}
	raw, ok := stack.Extra["sell_gold"]
	if !ok {
		return 0, ErrSellPriceMissing
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%w: %q", ErrSellPriceInvalid, raw)
	}
	return value, nil
}

func repairDurabilityOf(stack dnfrepo.ItemStack) (int64, int64, error) {
	if stack.Extra == nil {
		return 0, 0, ErrRepairDurabilityMissing
	}
	currentRaw := strings.TrimSpace(stack.Extra["durability"])
	maxRaw := strings.TrimSpace(stack.Extra["max_durability"])
	if currentRaw == "" || maxRaw == "" {
		return 0, 0, ErrRepairDurabilityMissing
	}
	current, err := strconv.ParseInt(currentRaw, 10, 64)
	if err != nil || current < 0 {
		return 0, 0, fmt.Errorf("%w: durability=%q", ErrRepairDurabilityInvalid, currentRaw)
	}
	maxValue, err := strconv.ParseInt(maxRaw, 10, 64)
	if err != nil || maxValue <= 0 || current > maxValue {
		return 0, 0, fmt.Errorf("%w: max_durability=%q durability=%q", ErrRepairDurabilityInvalid, maxRaw, currentRaw)
	}
	return current, maxValue, nil
}

// Upgrade-ticket (op50 ticket scene) client-visible error codes, matching the
// 86JP ItemUpgradeResult contract: 4 invalid target, 7 durability, 19
// forbidden/unsupported, 22 invalid material, 23 wrong mode, 95 max level,
// 174 amplify not identified, 213 item locked.
const (
	upgradeTicketErrorInvalidTarget        byte = 4
	upgradeTicketErrorDurability           byte = 7
	upgradeTicketErrorForbidden            byte = 19
	upgradeTicketErrorInvalidMaterial      byte = 22
	upgradeTicketErrorWrongMode            byte = 23
	upgradeTicketErrorMaxLevel             byte = 95
	upgradeTicketErrorAmplifyNotIdentified byte = 174
	upgradeTicketErrorLocked               byte = 213
	upgradeTicketResultCodeSuccess         byte = 0
	upgradeTicketResultCodeFailureRetain   byte = 1
)

// UpgradeTicketResult describes one op50 ticket-scene attempt. Success=true
// means the operation completed and the success-layout op50 ACK must be sent
// (ResultCode carries the roll outcome); TicketResolved=false means the
// material is not an upgrade ticket and the request stays on the pending
// normal-reinforcement path.
type UpgradeTicketResult struct {
	CharacterID                 string
	Success                     bool
	TicketResolved              bool
	ErrorCode                   byte
	Mode                        string
	TargetSlotIndex             int16
	MaterialSlotIndex           int16
	TicketItemID                int64
	OldLevel                    byte
	NewLevel                    byte
	ResultCode                  byte
	UpgradeSucceeded            bool
	MaterialRemainingStackCount int64
	MainRefresh                 map[string]dnfrepo.ItemStack
	TargetUpdatedStack          dnfrepo.ItemStack
	MaterialUpdated             bool
	MaterialUpdatedStack        dnfrepo.ItemStack
	Changed                     bool
}

// UpgradeTicket applies one equipment reinforcement/amplify ticket (op50 with
// the ticket in the material slot) to one main-inventory equipment entry,
// following the 86JP InventoryItemUpgradeStore ticket scene: on a successful
// roll the level jumps directly to the ticket's target level, on failure the
// level is retained without penalty; exactly one ticket is consumed either
// way. The request's optional ticket slot is ignored, mirroring the
// reference. Gold cost, protect tickets, [enchant random] multi-candidate
// tickets, and the 0x0056 notice broadcast are intentionally not owned yet.
func (o *Owner) UpgradeTicket(ctx context.Context, cmd Command, resolver alignedcmd.UpgradeTicketResolver) (UpgradeTicketResult, error) {
	base := UpgradeTicketResult{
		TicketResolved:    true,
		ErrorCode:         upgradeTicketErrorInvalidTarget,
		Mode:              cmd.Mode,
		TargetSlotIndex:   cmd.TargetSlotIndex,
		MaterialSlotIndex: cmd.MaterialSlotIndex,
	}
	if o == nil || o.repo == nil {
		return UpgradeTicketResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return UpgradeTicketResult{}, ErrCharacterRequired
	}
	if resolver == nil {
		return UpgradeTicketResult{}, ErrUpgradeTicketResolverRequired
	}
	if cmd.Mode != "reinforce" && cmd.Mode != "amplify" {
		base.ErrorCode = upgradeTicketErrorWrongMode
		return base, nil
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	base.CharacterID = characterID
	if !o.inItemTransaction {
		var result UpgradeTicketResult
		err := o.withinInventoryTransaction(ctx, characterID, func(txOwner *Owner) error {
			var err error
			result, err = txOwner.UpgradeTicket(ctx, cmd, resolver)
			return err
		})
		if err != nil {
			return UpgradeTicketResult{}, err
		}
		if result.CharacterID == "" {
			result = base
		}
		return result, nil
	}

	record, ok, err := o.repo.Load(ctx, characterID)
	if err != nil {
		return UpgradeTicketResult{}, err
	}
	if !ok {
		return UpgradeTicketResult{}, ErrInventoryNotFound
	}
	record = dnfrepo.CloneInventory(record)
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}

	result := base
	items := record.Slots
	targetKey := slotKey(listTypeMain, cmd.TargetSlotIndex)
	target, ok := items[targetKey]
	if !ok || target.Count <= 0 || target.ItemID != int64(cmd.TargetItemTemplateID) {
		return result, nil
	}
	result.OldLevel = upgradeLevelOf(target)
	result.NewLevel = result.OldLevel
	if isEquipmentLocked(target) {
		result.ErrorCode = upgradeTicketErrorLocked
		return result, nil
	}
	if !upgradeDurabilityOK(target) {
		result.ErrorCode = upgradeTicketErrorDurability
		return result, nil
	}
	if result.OldLevel > 30 {
		result.ErrorCode = upgradeTicketErrorMaxLevel
		return result, nil
	}
	if cmd.MaterialSlotIndex < 0 || cmd.MaterialSlotIndex == cmd.TargetSlotIndex {
		result.ErrorCode = upgradeTicketErrorInvalidMaterial
		return result, nil
	}
	materialKey := slotKey(listTypeMain, cmd.MaterialSlotIndex)
	material, ok := items[materialKey]
	if !ok || material.Count <= 0 {
		result.ErrorCode = upgradeTicketErrorInvalidMaterial
		return result, nil
	}
	result.TicketItemID = material.ItemID

	resolution, err := resolver(material.ItemID, target.ItemID)
	if err != nil {
		return UpgradeTicketResult{}, err
	}
	if resolution.TicketMode == "" {
		result.TicketResolved = false
		return result, nil
	}
	result.TicketResolved = true
	if !strings.EqualFold(strings.TrimSpace(resolution.TargetKind), "equipment") {
		result.ErrorCode = upgradeTicketErrorInvalidTarget
		return result, nil
	}
	if resolution.TicketRandom {
		result.ErrorCode = upgradeTicketErrorForbidden
		return result, nil
	}
	if resolution.TargetUpgradeForbidden {
		result.ErrorCode = upgradeTicketErrorForbidden
		return result, nil
	}
	if !strings.EqualFold(strings.TrimSpace(resolution.TicketMode), cmd.Mode) {
		result.ErrorCode = upgradeTicketErrorWrongMode
		return result, nil
	}
	amplifyType, amplifyValue := upgradeAmplifyState(target)
	switch cmd.Mode {
	case "reinforce":
		if amplifyType != 0 {
			result.ErrorCode = upgradeTicketErrorWrongMode
			return result, nil
		}
	case "amplify":
		if amplifyType == 0 {
			result.ErrorCode = upgradeTicketErrorWrongMode
			return result, nil
		}
		if amplifyValue <= 0 || (amplifyType&0x80) != 0 {
			result.ErrorCode = upgradeTicketErrorAmplifyNotIdentified
			return result, nil
		}
	}

	weight := resolution.SuccessWeight
	if weight < 0 {
		weight = 0
	}
	if weight > 100000 {
		weight = 100000
	}
	roll := int64(rand.Intn(100000))
	if roll < weight {
		newLevel := resolution.TargetLevel
		if newLevel < 0 {
			newLevel = 0
		}
		if newLevel > 31 {
			newLevel = 31
		}
		target = cloneStack(target)
		setUpgradeLevel(&target, byte(newLevel))
		items[targetKey] = target
		result.NewLevel = byte(newLevel)
		result.UpgradeSucceeded = true
		result.ResultCode = upgradeTicketResultCodeSuccess
	} else {
		result.ResultCode = upgradeTicketResultCodeFailureRetain
	}

	material = cloneStack(material)
	material.Count--
	result.MaterialRemainingStackCount = material.Count
	result.MaterialUpdated = true
	if material.Count <= 0 {
		delete(items, materialKey)
		result.MaterialRemainingStackCount = 0
		result.MaterialUpdatedStack = dnfrepo.ItemStack{}
	} else {
		updateStackRawAmount(&material)
		items[materialKey] = material
		result.MaterialUpdatedStack = material
	}

	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, o.repo, record, dnfrepo.InventoryFieldSlots); err != nil {
		return UpgradeTicketResult{}, err
	}
	result.Success = true
	result.ErrorCode = 0
	if updatedTarget, ok := items[targetKey]; ok {
		result.TargetUpdatedStack = updatedTarget
	} else {
		result.TargetUpdatedStack = dnfrepo.ItemStack{}
	}
	result.MainRefresh = cloneItemMap(record.Slots)
	result.Changed = true
	return result, nil
}
