// 本文件负责角色穿戴装备 owner 的状态修改入口。
// 它只处理装备 raw entry 的可靠写入，不生成旧客户端成功 ACK。
package equip

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/creaturestate"
	"longheng.io/server/internal/modules/dnf/itemquality"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable        = errors.New("equipment owner unavailable")
	ErrCharacterRequired       = errors.New("selected character id required")
	ErrEquipmentNotFound       = errors.New("equipment record not found")
	ErrSlotNotFound            = errors.New("equipped slot not found")
	ErrRawEntryTooShort        = errors.New("equipped raw entry is too short")
	ErrDurabilityMissing       = errors.New("equipped durability evidence is missing")
	ErrDurabilityInvalid       = errors.New("equipped durability evidence is invalid")
	ErrRepairCostMissing       = errors.New("equipped repair cost evidence is missing")
	ErrRepairCostInvalid       = errors.New("equipped repair cost evidence is invalid")
	ErrRepairNotRepairable     = errors.New("equipped item is not repairable by PVF durability evidence")
	ErrRepairGoldInsufficient  = errors.New("equipped repair gold is insufficient")
	ErrWalletTxnRequired       = errors.New("wallet transaction is required")
	ErrRepairAllRequiresOwners = errors.New("repair all requires equipment and wallet owners")
	ErrInventoryUnavailable    = errors.New("inventory repository unavailable")
	ErrInventoryNotFound       = errors.New("inventory record not found")
	ErrMoveUnsupported         = errors.New("equipment move is unsupported")
	ErrMoveSlotNotFound        = errors.New("equipment move slot not found")
	ErrMoveRawEntryMissing     = errors.New("equipment raw entry evidence is missing")
	ErrMoveRawEntryInvalid     = errors.New("equipment raw entry evidence is invalid")
	ErrMoveStackCountInvalid   = errors.New("equipment move stack count is invalid")
	ErrMoveTransactionRequired = errors.New("equipment move transaction is required")
	ErrMoveSlotOutOfRange      = errors.New("equipment move slot is outside the current EXE range")
	ErrMoveSlotMismatch        = errors.New("equipment entry key and slot do not match")
	ErrMoveValidatorRequired   = errors.New("equipment placement validator is required")
	ErrEquipmentTxnRequired    = errors.New("equipment mutation requires a character item transaction")
	ErrLegacyPVFSlotCollision  = errors.New("legacy PVF equipment slot migration collides with current equipment slot")
	ErrPetRepositoryRequired   = errors.New("pet repository is required for a creature equipment move")
	ErrPetMoveTxnRequired      = errors.New("pet equipment move requires a character pet transaction")
	ErrPetRecordNotFound       = errors.New("pet record not found for creature equipment move")
	ErrPetEntryNotFound        = errors.New("pet entry not found for creature equipment move")
	ErrPetOwnershipMismatch    = errors.New("pet inventory, equipment, and creature record do not match")
)

// Owner 是穿戴装备和耐久的 durable owner 边界。
type Owner struct {
	equipment          dnfrepo.EquipmentRepository
	inventory          dnfrepo.InventoryRepository
	character          dnfrepo.CharacterRepository
	accounts           dnfrepo.AccountRepository
	items              dnfrepo.CharacterItemUnitOfWork
	assets             dnfrepo.CharacterAssetUnitOfWork
	pets               dnfrepo.PetRepository
	petItems           dnfrepo.CharacterPetUnitOfWork
	placementValidator PlacementValidator
	nameTagChecker     func(itemID uint32) bool
	inItemTransaction  bool
}

// RepairCommand 描述修理穿戴装备的最小命令。
type RepairCommand struct {
	SelectedCharacterID uint16
	AccountID           string
	SlotIndex           int16
	QuickRepair         bool
	AutoRepair          bool
}

// RepairResult 描述穿戴装备耐久修理的写库结果；当前只允许零金币单件修理由上层回成功 ACK。
type RepairResult struct {
	CharacterID   string
	SlotIndex     int16
	ItemID        int64
	OldDurability uint16
	NewDurability uint16
	Cost          int64
	UpdatedGold   int64
	Changed       bool
	// FreeRepair marks the 魔王契约 auto-repair path (premium type 586):
	// body[5]=1 plus an active contract makes the repair cost zero.
	FreeRepair bool
	// RepairedCount is set by RepairAll: how many entries were restored.
	RepairedCount int
}

// MoveCommand 描述装备槽和物品栏之间移动的最小命令。
type MoveCommand struct {
	SelectedCharacterID      uint16
	SourceListType           byte
	SourceSlotIndex          int16
	SourceInstanceValue      int32
	MoveCount                int32
	DestinationListType      byte
	DestinationSlotIndex     int16
	DestinationInstanceValue int32
}

// Placement is the typed input required to authorize an item entering an
// equipment slot. The validator owns PVF and character compatibility rules.
type Placement struct {
	CharacterID     string
	ItemID          int64
	SourceListType  byte
	SourceSlotIndex int16
	TargetSlotIndex int16
}

// PlacementValidator validates PVF item type, character restrictions, and the
// exact target worn slot. Owner refuses ordinary equipment writes without it.
type PlacementValidator interface {
	ValidateEquipmentPlacement(context.Context, Placement) error
}

// PlacementValidatorFunc adapts a function to PlacementValidator.
type PlacementValidatorFunc func(context.Context, Placement) error

func (f PlacementValidatorFunc) ValidateEquipmentPlacement(ctx context.Context, placement Placement) error {
	if f == nil {
		return ErrMoveValidatorRequired
	}
	return f(ctx, placement)
}

// MoveResult 描述穿戴/卸下写库结果；它不是旧客户端成功 ACK 许可。
type MoveResult struct {
	CharacterID          string
	SourceListType       byte
	SourceSlotIndex      int16
	DestinationListType  byte
	DestinationSlotIndex int16
	ItemID               int64
	SwappedItemID        int64
	Mode                 string
	Changed              bool
	InventorySlots       map[string]dnfrepo.ItemStack
}

// NewOwner 创建穿戴装备 owner；缺少装备仓储时拒绝处理。
func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Equipment == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{
		equipment: repos.Equipment,
		inventory: repos.Inventory,
		character: repos.Character,
		accounts:  repos.Account,
		items:     repos.CharacterItems,
		assets:    repos.CharacterAssets,
		pets:      repos.Pet,
		petItems:  repos.CharacterPets,
	}, nil
}

// NewOwnerWithPlacementValidator enables ordinary equipment writes only when
// the caller supplies the typed PVF/character placement owner.
func NewOwnerWithPlacementValidator(repos dnfrepo.Group, validator PlacementValidator) (*Owner, error) {
	if validator == nil {
		return nil, ErrMoveValidatorRequired
	}
	owner, err := NewOwner(repos)
	if err != nil {
		return nil, err
	}
	owner.placementValidator = validator
	return owner, nil
}

// SetNameTagChecker registers a PVF-backed predicate that identifies name tag
// cards. When set, unequip of a name tag card is blocked (PR #239).
func (o *Owner) SetNameTagChecker(checker func(itemID uint32) bool) {
	if o != nil {
		o.nameTagChecker = checker
	}
}

// Repair 按 86JP TryRepairSingleEquipped 修理穿戴槽：PVF 证据定价（repair
// price/grade/durability + pricetable 全局费率），金币与耐久在同一角色资产
// 事务内提交；body[5]=1 且魔王契约(586)生效时免费；body[7]=1 快速修理按
// [quick repair cost rate] 加价。slot=-1 转 RepairAll。
func (o *Owner) Repair(ctx context.Context, cmd RepairCommand, costResolver alignedcmd.RepairCostResolver) (RepairResult, error) {
	if o == nil || o.assets == nil {
		return RepairResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return RepairResult{}, ErrCharacterRequired
	}
	if cmd.SlotIndex == -1 {
		return o.RepairAll(ctx, cmd, costResolver)
	}
	if costResolver == nil {
		return RepairResult{}, ErrRepairCostMissing
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	var result RepairResult
	err := o.assets.WithinCharacterAssets(ctx, characterID, func(characters dnfrepo.CharacterRepository, _ dnfrepo.InventoryRepository, equipment dnfrepo.EquipmentRepository) error {
		var err error
		result, err = o.repairSingleEquipped(ctx, cmd, characterID, characters, equipment, costResolver)
		return err
	})
	return result, err
}

func (o *Owner) repairSingleEquipped(
	ctx context.Context,
	cmd RepairCommand,
	characterID string,
	characters dnfrepo.CharacterRepository,
	equipment dnfrepo.EquipmentRepository,
	costResolver alignedcmd.RepairCostResolver,
) (RepairResult, error) {
	record, ok, err := equipment.Load(ctx, characterID)
	if err != nil {
		return RepairResult{}, err
	}
	if !ok {
		return RepairResult{}, ErrEquipmentNotFound
	}
	record = dnfrepo.CloneEquipment(record)
	record.CharacterID = characterID
	if record.Entries == nil {
		record.Entries = make(map[string]dnfrepo.EquipmentEntry)
	}

	key := entryKey(cmd.SlotIndex)
	entry, ok := record.Entries[key]
	if !ok {
		return RepairResult{}, fmt.Errorf("%w: slot=%d", ErrSlotNotFound, cmd.SlotIndex)
	}
	if len(entry.RawEntry) < 12 {
		return RepairResult{}, fmt.Errorf("%w: slot=%d len=%d", ErrRawEntryTooShort, cmd.SlotIndex, len(entry.RawEntry))
	}
	current := int64(uint16(entry.RawEntry[10]) | uint16(entry.RawEntry[11])<<8)
	evidence, err := costResolver(entry.ItemID)
	if err != nil {
		return RepairResult{}, err
	}
	if evidence.MaxDurability <= 0 {
		return RepairResult{}, fmt.Errorf("%w: slot=%d item=%d", ErrRepairNotRepairable, cmd.SlotIndex, entry.ItemID)
	}
	maxDurability := evidence.MaxDurability
	character, found, err := characters.Load(ctx, characterID)
	if err != nil {
		return RepairResult{}, err
	}
	if !found {
		return RepairResult{}, fmt.Errorf("%w: character=%s", ErrWalletTxnRequired, characterID)
	}
	character = dnfrepo.CloneCharacter(character)
	if character.Stats == nil {
		return RepairResult{}, fmt.Errorf("%w: character=%s has no stats", ErrWalletTxnRequired, characterID)
	}
	gold := character.Stats["gold"]
	result := RepairResult{
		CharacterID:   characterID,
		SlotIndex:     cmd.SlotIndex,
		ItemID:        entry.ItemID,
		OldDurability: uint16(current),
		NewDurability: uint16(maxDurability),
	}
	if current >= maxDurability {
		result.NewDurability = uint16(current)
		result.UpdatedGold = gold
		return result, nil
	}
	cost := CalcRepairCost(evidence, current, upgradeLevelOfRawEntry(entry.RawEntry), cmd.QuickRepair)
	freeRepair := false
	statsChanged := false
	if cost != 0 && cmd.AutoRepair && o.premiumActive(ctx, cmd.AccountID, premium.DevilSlotType(premium.DevilSlotAutoRepair)) {
		// 魔王契约 auto repair: the client sent body[5]=1 and the account
		// holds an active slot-6 contract, so the repair is free (86JP
		// freeRepair = autoRepair && HasActiveAutoRepairForAccount).
		if premium.TryConsumeDaily(&character, premium.DevilSlotAutoRepair, time.Now().UTC()) {
			cost = 0
			freeRepair = true
			statsChanged = true
		}
	}
	if cost > gold {
		return RepairResult{}, fmt.Errorf("%w: cost=%d gold=%d", ErrRepairGoldInsufficient, cost, gold)
	}

	if cost > 0 {
		character.Stats["gold"] = gold - cost
		statsChanged = true
	}
	if statsChanged {
		character.UpdatedAt = time.Now()
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return RepairResult{}, err
		}
	}
	entry.RawEntry = append([]byte(nil), entry.RawEntry...)
	entry.RawEntry[10] = byte(maxDurability)
	entry.RawEntry[11] = byte(maxDurability >> 8)
	if entry.Extra == nil {
		entry.Extra = make(map[string]string, 1)
	}
	entry.Extra["durability"] = strconv.FormatInt(maxDurability, 10)
	record.Entries[key] = entry
	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveEquipmentFields(ctx, equipment, record, dnfrepo.EquipmentFieldEntries); err != nil {
		return RepairResult{}, err
	}
	result.Cost = cost
	result.UpdatedGold = gold - cost
	result.Changed = true
	result.FreeRepair = freeRepair
	return result, nil
}

// RepairAll 按 86JP TryRepairAll 全部修理：扫描穿戴装备与主列表快捷栏
// (slot 3..8)，只处理 13 类可修理装备且耐久未满的条目，合计费用一次扣除，
// ACK 槽位 0xFFFF，客户端据此本地拉满。
func (o *Owner) RepairAll(ctx context.Context, cmd RepairCommand, costResolver alignedcmd.RepairCostResolver) (RepairResult, error) {
	if o == nil || o.assets == nil {
		return RepairResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return RepairResult{}, ErrCharacterRequired
	}
	if costResolver == nil {
		return RepairResult{}, ErrRepairCostMissing
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	var result RepairResult
	err := o.assets.WithinCharacterAssets(ctx, characterID, func(characters dnfrepo.CharacterRepository, inventory dnfrepo.InventoryRepository, equipment dnfrepo.EquipmentRepository) error {
		var err error
		result, err = o.repairAllInTransaction(ctx, cmd, characterID, characters, inventory, equipment, costResolver)
		return err
	})
	return result, err
}

type repairAllTarget struct {
	equipped bool
	key      string
	slot     int16
	itemID   int64
	maxDura  int64
	cost     int64
}

func (o *Owner) repairAllInTransaction(
	ctx context.Context,
	cmd RepairCommand,
	characterID string,
	characters dnfrepo.CharacterRepository,
	inventory dnfrepo.InventoryRepository,
	equipment dnfrepo.EquipmentRepository,
	costResolver alignedcmd.RepairCostResolver,
) (RepairResult, error) {
	character, found, err := characters.Load(ctx, characterID)
	if err != nil {
		return RepairResult{}, err
	}
	if !found {
		return RepairResult{}, fmt.Errorf("%w: character=%s", ErrWalletTxnRequired, characterID)
	}
	character = dnfrepo.CloneCharacter(character)
	if character.Stats == nil {
		return RepairResult{}, fmt.Errorf("%w: character=%s has no stats", ErrWalletTxnRequired, characterID)
	}
	equipRecord, equipFound, err := equipment.Load(ctx, characterID)
	if err != nil {
		return RepairResult{}, err
	}
	equipRecord = dnfrepo.CloneEquipment(equipRecord)
	equipRecord.CharacterID = characterID
	if equipRecord.Entries == nil {
		equipRecord.Entries = make(map[string]dnfrepo.EquipmentEntry)
	}
	invRecord, invFound, err := inventory.Load(ctx, characterID)
	if err != nil {
		return RepairResult{}, err
	}
	invRecord = dnfrepo.CloneInventory(invRecord)
	invRecord.CharacterID = characterID
	if invRecord.Slots == nil {
		invRecord.Slots = make(map[string]dnfrepo.ItemStack)
	}

	autoRepairActive := cmd.AutoRepair && o.premiumActive(ctx, cmd.AccountID, premium.DevilSlotType(premium.DevilSlotAutoRepair))
	freeRepair := false
	targets := make([]repairAllTarget, 0, 16)
	total := int64(0)
	if equipFound {
		for key, entry := range equipRecord.Entries {
			if len(entry.RawEntry) < 12 {
				continue
			}
			slot, err := strconv.Atoi(key)
			if err != nil {
				continue
			}
			evidence, err := costResolver(entry.ItemID)
			if err != nil {
				return RepairResult{}, err
			}
			if evidence.MaxDurability <= 0 || !RepairAllEligible(evidence.EquipmentType) {
				continue
			}
			current := int64(uint16(entry.RawEntry[10]) | uint16(entry.RawEntry[11])<<8)
			if current >= evidence.MaxDurability {
				continue
			}
			cost := CalcRepairCost(evidence, current, upgradeLevelOfRawEntry(entry.RawEntry), cmd.QuickRepair)
			targets = append(targets, repairAllTarget{equipped: true, key: key, slot: int16(slot), itemID: entry.ItemID, maxDura: evidence.MaxDurability, cost: cost})
			total += cost
		}
	}
	if invFound {
		for slot := int16(3); slot <= 8; slot++ {
			key := "0:" + strconv.Itoa(int(slot))
			stack, ok := invRecord.Slots[key]
			if !ok || stack.ItemID <= 0 {
				continue
			}
			evidence, err := costResolver(stack.ItemID)
			if err != nil {
				return RepairResult{}, err
			}
			if evidence.MaxDurability <= 0 || !RepairAllEligible(evidence.EquipmentType) {
				continue
			}
			current, err := repairBagDurabilityOf(stack)
			if err != nil || current >= evidence.MaxDurability {
				continue
			}
			upgradeLevel := 0
			if len(stack.RawEntry) > 0x0A {
				upgradeLevel = int(stack.RawEntry[0x0A] & 0x1F)
			}
			cost := CalcRepairCost(evidence, current, upgradeLevel, cmd.QuickRepair)
			targets = append(targets, repairAllTarget{key: key, slot: slot, itemID: stack.ItemID, maxDura: evidence.MaxDurability, cost: cost})
			total += cost
		}
	}

	gold := character.Stats["gold"]
	result := RepairResult{
		CharacterID: characterID,
		SlotIndex:   -1,
		FreeRepair:  freeRepair,
	}
	if len(targets) == 0 {
		result.UpdatedGold = gold
		return result, nil
	}
	if autoRepairActive && premium.TryConsumeDaily(&character, premium.DevilSlotAutoRepair, time.Now().UTC()) {
		freeRepair = true
		result.FreeRepair = true
		total = 0
		for index := range targets {
			targets[index].cost = 0
		}
	}
	if total > gold {
		return RepairResult{}, fmt.Errorf("%w: cost=%d gold=%d", ErrRepairGoldInsufficient, total, gold)
	}
	if total > 0 || freeRepair {
		character.Stats["gold"] = gold - total
		character.UpdatedAt = time.Now()
		if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
			return RepairResult{}, err
		}
	}
	for _, target := range targets {
		if target.equipped {
			entry := equipRecord.Entries[target.key]
			entry.RawEntry = append([]byte(nil), entry.RawEntry...)
			entry.RawEntry[10] = byte(target.maxDura)
			entry.RawEntry[11] = byte(target.maxDura >> 8)
			if entry.Extra == nil {
				entry.Extra = make(map[string]string, 1)
			}
			entry.Extra["durability"] = strconv.FormatInt(target.maxDura, 10)
			equipRecord.Entries[target.key] = entry
			continue
		}
		stack := invRecord.Slots[target.key]
		stack = cloneItemStackForRepair(stack)
		if stack.Extra == nil {
			stack.Extra = make(map[string]string, 2)
		}
		stack.Extra["durability"] = strconv.FormatInt(target.maxDura, 10)
		if len(stack.RawEntry) >= 12 {
			stack.RawEntry = append([]byte(nil), stack.RawEntry...)
			stack.RawEntry[10] = byte(target.maxDura)
			stack.RawEntry[11] = byte(target.maxDura >> 8)
		}
		invRecord.Slots[target.key] = stack
	}
	if len(targets) > 0 {
		equipRecord.UpdatedAt = time.Now()
		if equipFound {
			if err := dnfrepo.SaveEquipmentFields(ctx, equipment, equipRecord, dnfrepo.EquipmentFieldEntries); err != nil {
				return RepairResult{}, err
			}
		}
		invRecord.UpdatedAt = time.Now()
		if invFound {
			if err := dnfrepo.SaveInventoryFields(ctx, inventory, invRecord, dnfrepo.InventoryFieldSlots); err != nil {
				return RepairResult{}, err
			}
		}
	}
	result.Cost = total
	result.UpdatedGold = gold - total
	result.Changed = true
	result.RepairedCount = len(targets)
	return result, nil
}

func upgradeLevelOfRawEntry(raw []byte) int {
	if len(raw) > 0x0A {
		return int(raw[0x0A] & 0x1F)
	}
	return 0
}

func repairBagDurabilityOf(stack dnfrepo.ItemStack) (int64, error) {
	if stack.Extra == nil {
		return 0, ErrRepairCostMissing
	}
	raw := strings.TrimSpace(stack.Extra["durability"])
	if raw == "" {
		return 0, ErrRepairCostMissing
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%w: %q", ErrRepairCostInvalid, raw)
	}
	return value, nil
}

func cloneItemStackForRepair(stack dnfrepo.ItemStack) dnfrepo.ItemStack {
	if stack.Extra != nil {
		extra := make(map[string]string, len(stack.Extra)+1)
		for key, value := range stack.Extra {
			extra[key] = value
		}
		stack.Extra = extra
	}
	return stack
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

// Move 在装备槽和背包/装扮/宠物物品栏之间迁移 raw entry。
// Ordinary placement requires structural bounds plus a typed PVF/character validator.
func (o *Owner) Move(ctx context.Context, cmd MoveCommand) (MoveResult, error) {
	if o == nil || o.equipment == nil {
		return MoveResult{}, ErrOwnerUnavailable
	}
	if o.inventory == nil {
		return MoveResult{}, ErrInventoryUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return MoveResult{}, ErrCharacterRequired
	}
	kind, err := validateMoveStructure(cmd)
	if err != nil {
		return MoveResult{}, err
	}
	creatureMove := kind == moveKindPet
	petAggregateMove := creatureMove || kind == moveKindArtifact
	if cmd.MoveCount != 1 {
		return MoveResult{}, fmt.Errorf("%w: count=%d", ErrMoveStackCountInvalid, cmd.MoveCount)
	}
	if !creatureMove && o.placementValidator == nil {
		return MoveResult{}, ErrMoveValidatorRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	if !o.inItemTransaction {
		var result MoveResult
		if petAggregateMove {
			if o.pets == nil {
				return MoveResult{}, ErrPetRepositoryRequired
			}
			if o.petItems == nil {
				return MoveResult{}, ErrPetMoveTxnRequired
			}
			err = o.petItems.WithinCharacterPets(ctx, characterID, func(inventory dnfrepo.InventoryRepository, equipment dnfrepo.EquipmentRepository, pets dnfrepo.PetRepository) error {
				txOwner := &Owner{
					equipment:          equipment,
					inventory:          inventory,
					character:          o.character,
					pets:               pets,
					placementValidator: o.placementValidator,
					inItemTransaction:  true,
				}
				var err error
				result, err = txOwner.Move(ctx, cmd)
				return err
			})
		} else {
			if o.items == nil {
				return MoveResult{}, ErrMoveTransactionRequired
			}
			err = o.items.WithinCharacterItems(ctx, characterID, func(inventory dnfrepo.InventoryRepository, equipment dnfrepo.EquipmentRepository) error {
				txOwner := &Owner{
					equipment:          equipment,
					inventory:          inventory,
					character:          o.character,
					placementValidator: o.placementValidator,
					inItemTransaction:  true,
				}
				var err error
				result, err = txOwner.Move(ctx, cmd)
				return err
			})
		}
		return result, err
	}
	if !petAggregateMove {
		if _, err := o.normalizeLegacyPVFInitialEquipmentSlots(ctx, characterID); err != nil {
			return MoveResult{}, err
		}
	} else if creatureMove {
		if _, err := creaturestate.ReconcileInventory(ctx, characterID, o.inventory, o.equipment, o.pets); err != nil {
			return MoveResult{}, err
		}
	}

	// Current NoPack op19 carries two endpoint addresses, not a reliable move
	// direction. Equip and unequip requests keep the inventory/avatar/cargo
	// endpoint first and the worn endpoint second; only the endpoint instance
	// values and authoritative server state change. Resolve the direction from
	// the transaction-scoped DB snapshot so the decision and mutation are
	// serialized by CharacterItemUnitOfWork.
	resolved, reversed, err := o.resolveMoveEndpoints(ctx, characterID, cmd)
	if err != nil {
		return MoveResult{}, err
	}
	var result MoveResult
	if resolved.SourceListType == wireListEquipment && resolved.DestinationListType == wireListEquipment {
		result, err = o.swapEquipSlots(ctx, resolved)
	} else if resolved.SourceListType == wireListEquipment {
		result, err = o.unequip(ctx, resolved)
	} else {
		result, err = o.equip(ctx, resolved)
	}
	if err != nil {
		return MoveResult{}, err
	}
	switch kind {
	case moveKindPet:
		if err := o.syncPetEquipmentMove(ctx, characterID, result); err != nil {
			return MoveResult{}, err
		}
	case moveKindArtifact:
		if err := o.syncPetArtifactEquipmentMove(ctx, characterID, result); err != nil {
			return MoveResult{}, err
		}
	}
	if reversed {
		// Keep the request's A/B list direction, but return the authoritative
		// inventory slot selected by the owner. Cross-list op19 accepts a
		// redirected final slot; this is required when the client's stale bag
		// view nominates an already occupied unequip destination.
		result.SourceListType = cmd.SourceListType
		result.SourceSlotIndex = result.DestinationSlotIndex
		result.DestinationListType = cmd.DestinationListType
		result.DestinationSlotIndex = cmd.DestinationSlotIndex
	}
	return result, nil
}

type legacyPVFInitialEquipmentSlotMove struct {
	sourceKey string
	targetKey string
	target    int16
	entry     dnfrepo.EquipmentEntry
}

// normalizeLegacyPVFInitialEquipmentSlots converts only untouched
// source=pvf_create_equipment_list records from the legacy worn-slot keys to
// the current EXE actor-slot keys used by op19. It runs inside the same
// CharacterItemUnitOfWork as the requested move, so a later placement/save
// failure rolls the migration back with the move. Runtime rows and already
// migrated PVF rows carry current_exe_runtime_move and are never reinterpreted.
func (o *Owner) normalizeLegacyPVFInitialEquipmentSlots(ctx context.Context, characterID string) (bool, error) {
	record, found, err := o.equipment.Load(ctx, characterID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, ErrEquipmentNotFound
	}
	record = ensureEquipmentRecord(dnfrepo.CloneEquipment(record), characterID)

	plans := make(map[string]legacyPVFInitialEquipmentSlotMove)
	targets := make(map[string]string)
	for key, entry := range record.Entries {
		if !isUntouchedLegacyPVFInitialEquipment(entry) {
			continue
		}
		target, ok := currentActorSlotForLegacyPVFInitialSlot(entry.SlotIndex)
		if !ok {
			continue
		}
		sourceKey := entryKey(entry.SlotIndex)
		if key != sourceKey {
			return false, fmt.Errorf("%w: key=%q entry=%d", ErrMoveSlotMismatch, key, entry.SlotIndex)
		}
		targetKey := entryKey(target)
		if previous, exists := targets[targetKey]; exists {
			return false, fmt.Errorf(
				"%w: source=%s other_source=%s target=%s",
				ErrLegacyPVFSlotCollision,
				sourceKey,
				previous,
				targetKey,
			)
		}
		targets[targetKey] = sourceKey
		plans[sourceKey] = legacyPVFInitialEquipmentSlotMove{
			sourceKey: sourceKey,
			targetKey: targetKey,
			target:    target,
			entry:     entry,
		}
	}
	if len(plans) == 0 {
		return false, nil
	}

	// A target occupied by another planned legacy row is safe because every
	// source key is deleted before any target key is inserted. Any other
	// occupant is authoritative current/runtime state and must fail closed.
	for _, plan := range plans {
		if _, occupied := record.Entries[plan.targetKey]; !occupied {
			continue
		}
		if _, movingAway := plans[plan.targetKey]; !movingAway {
			return false, fmt.Errorf(
				"%w: source=%s target=%s",
				ErrLegacyPVFSlotCollision,
				plan.sourceKey,
				plan.targetKey,
			)
		}
	}

	for sourceKey := range plans {
		delete(record.Entries, sourceKey)
	}
	for _, plan := range plans {
		entry := plan.entry
		entry.SlotIndex = plan.target
		entry.Extra = cloneExtra(entry.Extra)
		entry.Extra["equipped_slot"] = strconv.Itoa(int(plan.target))
		entry.Extra["current_exe_equipment_type"] = strconv.Itoa(int(plan.target))
		entry.Extra["current_exe_runtime_move"] = "1"
		record.Entries[plan.targetKey] = entry
	}
	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveEquipmentFields(ctx, o.equipment, record, dnfrepo.EquipmentFieldEntries); err != nil {
		return false, err
	}
	return true, nil
}

func isUntouchedLegacyPVFInitialEquipment(entry dnfrepo.EquipmentEntry) bool {
	if !strings.EqualFold(strings.TrimSpace(entry.Extra["source"]), "pvf_create_equipment_list") {
		return false
	}
	raw := strings.TrimSpace(strings.ToLower(entry.Extra["current_exe_runtime_move"]))
	switch raw {
	case "", "0", "false", "no", "off":
		return true
	default:
		// Unknown non-empty markers also fail closed: they may represent a
		// newer runtime migration state that this owner must not reinterpret.
		return false
	}
}

func currentActorSlotForLegacyPVFInitialSlot(slot int16) (int16, bool) {
	switch {
	case slot == 11:
		return 12, true
	case slot >= 13 && slot <= 22:
		return slot + 1, true
	case slot == 23:
		return 25, true
	default:
		return 0, false
	}
}

func (o *Owner) resolveMoveEndpoints(ctx context.Context, characterID string, cmd MoveCommand) (MoveCommand, bool, error) {
	var inventory dnfrepo.InventoryRecord
	if cmd.SourceListType != wireListEquipment || cmd.DestinationListType != wireListEquipment {
		loaded, ok, err := o.inventory.Load(ctx, characterID)
		if err != nil {
			return MoveCommand{}, false, err
		}
		if !ok {
			return MoveCommand{}, false, ErrInventoryNotFound
		}
		inventory = ensureInventoryRecord(dnfrepo.CloneInventory(loaded), characterID)
	}
	equipment, ok, err := o.equipment.Load(ctx, characterID)
	if err != nil {
		return MoveCommand{}, false, err
	}
	if !ok {
		return MoveCommand{}, false, ErrEquipmentNotFound
	}
	equipment = ensureEquipmentRecord(dnfrepo.CloneEquipment(equipment), characterID)
	aOccupied, err := moveEndpointOccupied(&inventory, &equipment, cmd.SourceListType, cmd.SourceSlotIndex)
	if err != nil {
		return MoveCommand{}, false, err
	}
	bOccupied, err := moveEndpointOccupied(&inventory, &equipment, cmd.DestinationListType, cmd.DestinationSlotIndex)
	if err != nil {
		return MoveCommand{}, false, err
	}
	if !aOccupied && !bOccupied {
		return MoveCommand{}, false, fmt.Errorf(
			"%w: endpoints=(%d,%d),(%d,%d)",
			ErrMoveSlotNotFound,
			cmd.SourceListType,
			cmd.SourceSlotIndex,
			cmd.DestinationListType,
			cmd.DestinationSlotIndex,
		)
	}
	// The current 28-byte op19 body carries the live instance value for both
	// endpoints. A zero A instance and nonzero B instance is the observed
	// unequip form. Prefer that direction even when the server still has an
	// item in A because a missed/early list0 snapshot can leave the client's
	// chosen empty destination stale. The owner will relocate to the next real
	// empty slot in the same proven inventory page instead of corrupting the
	// worn slot with A.
	if cmd.SourceInstanceValue == 0 && cmd.DestinationInstanceValue != 0 && bOccupied {
		return reverseMoveEndpoints(cmd), true, nil
	}
	if cmd.SourceInstanceValue != 0 && cmd.DestinationInstanceValue == 0 && aOccupied {
		return cmd, false, nil
	}
	if aOccupied {
		return cmd, false, nil
	}
	return reverseMoveEndpoints(cmd), true, nil
}

func moveEndpointOccupied(inventory *dnfrepo.InventoryRecord, equipment *dnfrepo.EquipmentRecord, listType byte, slotIndex int16) (bool, error) {
	if listType == wireListEquipment {
		entry, occupied := equipment.Entries[entryKey(slotIndex)]
		if occupied && entry.SlotIndex != slotIndex {
			return false, fmt.Errorf("%w: key=%d entry=%d", ErrMoveSlotMismatch, slotIndex, entry.SlotIndex)
		}
		return occupied, nil
	}
	items, _, ok := inventoryMap(inventory, listType)
	if !ok {
		return false, fmt.Errorf("%w: endpoint list=%d", ErrMoveUnsupported, listType)
	}
	_, occupied := items[inventoryKey(listType, slotIndex)]
	return occupied, nil
}

func reverseMoveEndpoints(cmd MoveCommand) MoveCommand {
	return MoveCommand{
		SelectedCharacterID:      cmd.SelectedCharacterID,
		SourceListType:           cmd.DestinationListType,
		SourceSlotIndex:          cmd.DestinationSlotIndex,
		SourceInstanceValue:      cmd.DestinationInstanceValue,
		MoveCount:                cmd.MoveCount,
		DestinationListType:      cmd.SourceListType,
		DestinationSlotIndex:     cmd.SourceSlotIndex,
		DestinationInstanceValue: cmd.SourceInstanceValue,
	}
}

func (o *Owner) equip(ctx context.Context, cmd MoveCommand) (MoveResult, error) {
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	inventory, equipment, err := o.loadMoveRecords(ctx, characterID)
	if err != nil {
		return MoveResult{}, err
	}
	srcItems, srcField, ok := inventoryMap(&inventory, cmd.SourceListType)
	if !ok {
		return MoveResult{}, fmt.Errorf("%w: source list=%d", ErrMoveUnsupported, cmd.SourceListType)
	}
	if cmd.SourceListType == wireListGuildMedal && !guildMedalInventorySlotContains(cmd.SourceSlotIndex) {
		return MoveResult{}, fmt.Errorf("%w: guild-medal source slot=%d", ErrMoveUnsupported, cmd.SourceSlotIndex)
	}
	srcKey := inventoryKey(cmd.SourceListType, cmd.SourceSlotIndex)
	source, ok := srcItems[srcKey]
	if !ok {
		return MoveResult{}, fmt.Errorf("%w: source list=%d slot=%d", ErrMoveSlotNotFound, cmd.SourceListType, cmd.SourceSlotIndex)
	}
	if !isEquipmentMoveUnit(source, cmd.SourceListType) {
		return MoveResult{}, fmt.Errorf("%w: count=%d", ErrMoveStackCountInvalid, source.Count)
	}
	entry, err := entryFromStack(source, cmd.SourceListType, cmd.DestinationSlotIndex, cmd.SourceInstanceValue)
	if err != nil {
		return MoveResult{}, err
	}
	entry = removeSealOnFirstEquipmentUse(entry, source)
	if err := o.validatePlacement(ctx, characterID, source.ItemID, cmd.SourceListType, cmd.SourceSlotIndex, cmd.DestinationSlotIndex); err != nil {
		return MoveResult{}, err
	}

	result := MoveResult{
		CharacterID:          characterID,
		SourceListType:       cmd.SourceListType,
		SourceSlotIndex:      cmd.SourceSlotIndex,
		DestinationListType:  cmd.DestinationListType,
		DestinationSlotIndex: cmd.DestinationSlotIndex,
		ItemID:               source.ItemID,
		Mode:                 "equip",
	}
	dstKey := entryKey(cmd.DestinationSlotIndex)
	if previous, occupied := equipment.Entries[dstKey]; occupied {
		if previous.SlotIndex != cmd.DestinationSlotIndex {
			return MoveResult{}, fmt.Errorf("%w: key=%d entry=%d", ErrMoveSlotMismatch, cmd.DestinationSlotIndex, previous.SlotIndex)
		}
		srcItems[srcKey] = stackFromEntry(previous)
		result.SwappedItemID = previous.ItemID
		result.Mode = "equip_swap"
	} else {
		delete(srcItems, srcKey)
	}
	equipment.Entries[dstKey] = entry
	return o.saveMove(ctx, inventory, equipment, []dnfrepo.InventoryField{srcField}, result)
}

func removeSealOnFirstEquipmentUse(entry dnfrepo.EquipmentEntry, source dnfrepo.ItemStack) dnfrepo.EquipmentEntry {
	// The current 0x77 item row's byte +0x0D is the visible sealed-state
	// byte. Runtime-PVF [attach type] [sealing] grants persist it as the
	// explicit seal_flag extra. The first successful equip consumes that
	// state permanently; otherwise unequipping copies the old flag back and
	// the client asks to remove the seal every time.
	if firstExtraInt(source.Extra, 0, "seal_flag", "seal") == 0 {
		return entry
	}
	if len(entry.RawEntry) > 0x0D {
		entry.RawEntry[0x0D] = 0
	}
	if entry.Extra == nil {
		entry.Extra = make(map[string]string, 4)
	}
	for _, key := range []string{"seal_flag", "seal", "equipment_update_seal_flag", "update_seal_flag"} {
		delete(entry.Extra, key)
	}
	entry.Extra["seal_removed_by_first_equip"] = "1"
	entry.Extra["trade_locked_by_first_equip"] = "1"
	return entry
}

func (o *Owner) unequip(ctx context.Context, cmd MoveCommand) (MoveResult, error) {
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	inventory, equipment, err := o.loadMoveRecords(ctx, characterID)
	if err != nil {
		return MoveResult{}, err
	}
	dstItems, dstField, ok := inventoryMap(&inventory, cmd.DestinationListType)
	if !ok {
		return MoveResult{}, fmt.Errorf("%w: destination list=%d", ErrMoveUnsupported, cmd.DestinationListType)
	}
	if cmd.DestinationListType == wireListGuildMedal && !guildMedalInventorySlotContains(cmd.DestinationSlotIndex) {
		return MoveResult{}, fmt.Errorf("%w: guild-medal destination slot=%d", ErrMoveUnsupported, cmd.DestinationSlotIndex)
	}
	srcKey := entryKey(cmd.SourceSlotIndex)
	source, ok := equipment.Entries[srcKey]
	if !ok {
		return MoveResult{}, fmt.Errorf("%w: equip slot=%d", ErrMoveSlotNotFound, cmd.SourceSlotIndex)
	}
	if source.SlotIndex != cmd.SourceSlotIndex {
		return MoveResult{}, fmt.Errorf("%w: key=%d entry=%d", ErrMoveSlotMismatch, cmd.SourceSlotIndex, source.SlotIndex)
	}
	// PR #239: name tag cards can only be replaced by purchase, not manually unequipped.
	if o.nameTagChecker != nil && source.ItemID > 0 && o.nameTagChecker(uint32(source.ItemID)) {
		return MoveResult{}, fmt.Errorf("%w: name tag card cannot be unequipped", ErrMoveUnsupported)
	}
	if len(source.RawEntry) == 0 {
		return MoveResult{}, ErrMoveRawEntryMissing
	}

	result := MoveResult{
		CharacterID:          characterID,
		SourceListType:       cmd.SourceListType,
		SourceSlotIndex:      cmd.SourceSlotIndex,
		DestinationListType:  cmd.DestinationListType,
		DestinationSlotIndex: cmd.DestinationSlotIndex,
		ItemID:               source.ItemID,
		Mode:                 "unequip",
	}
	dstKey := inventoryKey(cmd.DestinationListType, cmd.DestinationSlotIndex)
	if _, occupied := dstItems[dstKey]; occupied && cmd.DestinationInstanceValue == 0 {
		actualSlot, found := nextEmptyUnequipInventorySlot(dstItems, cmd.DestinationListType, cmd.DestinationSlotIndex)
		if !found {
			return MoveResult{}, fmt.Errorf("%w: no empty unequip destination after list=%d slot=%d", ErrMoveSlotNotFound, cmd.DestinationListType, cmd.DestinationSlotIndex)
		}
		cmd.DestinationSlotIndex = actualSlot
		result.DestinationSlotIndex = actualSlot
		dstKey = inventoryKey(cmd.DestinationListType, actualSlot)
	}
	if previous, occupied := dstItems[dstKey]; occupied {
		entry, err := entryFromStack(previous, cmd.DestinationListType, cmd.SourceSlotIndex, cmd.DestinationInstanceValue)
		if err != nil {
			return MoveResult{}, err
		}
		if err := o.validatePlacement(ctx, characterID, previous.ItemID, cmd.DestinationListType, cmd.DestinationSlotIndex, cmd.SourceSlotIndex); err != nil {
			return MoveResult{}, err
		}
		equipment.Entries[srcKey] = entry
		result.SwappedItemID = previous.ItemID
		result.Mode = "unequip_swap"
	} else {
		delete(equipment.Entries, srcKey)
	}
	dstItems[dstKey] = stackFromEntry(source)
	return o.saveMove(ctx, inventory, equipment, []dnfrepo.InventoryField{dstField}, result)
}

const (
	mainEquipmentInventorySlotStart int16 = 9
	mainEquipmentInventorySlotEnd   int16 = 64
	avatarInventorySlotStart        int16 = 0
	avatarInventorySlotEnd          int16 = 209
	petBodyInventorySlotStart       int16 = 0
	petBodyInventorySlotEnd         int16 = 139
	petArtifactInventorySlotStart   int16 = 140
	petArtifactInventorySlotEnd     int16 = 188
	guildMedalInventorySlotStart    int16 = 0
	guildMedalInventorySlotEnd      int16 = 48
)

func nextEmptyUnequipInventorySlot(items map[string]dnfrepo.ItemStack, listType byte, requested int16) (int16, bool) {
	start, end := mainEquipmentInventorySlotStart, mainEquipmentInventorySlotEnd
	switch listType {
	case wireListMain:
		// Equipment page only. Slots 0..8 are the main-list quick bar and the
		// later PVF pages own consumables, materials, quests, and professions.
	case wireListAvatar:
		// Current NoPack list-1 sort/page reader owns the 0..209 avatar page.
		start, end = avatarInventorySlotStart, avatarInventorySlotEnd
	case wireListPet:
		// Current NoPack splits list 7 into creature bodies 0..139 and
		// creature artifacts 140..188. Never redirect an unequip across those
		// pages; slots 189..238 are pet consumables and are not worn endpoints.
		switch {
		case requested >= petBodyInventorySlotStart && requested <= petBodyInventorySlotEnd:
			start, end = petBodyInventorySlotStart, petBodyInventorySlotEnd
		case requested >= petArtifactInventorySlotStart && requested <= petArtifactInventorySlotEnd:
			start, end = petArtifactInventorySlotStart, petArtifactInventorySlotEnd
		default:
			return 0, false
		}
	case wireListGuildMedal:
		// Current NoPack sub_E9AC00/sub_E9AC30 split the shared 98-slot
		// list-38 container into two 49-slot pages. Medal equip/unequip owns
		// only the first page; slots 49..97 belong to guardian gems.
		start, end = guildMedalInventorySlotStart, guildMedalInventorySlotEnd
	default:
		return 0, false
	}
	if requested < start || requested > end {
		return 0, false
	}
	for slot := requested; slot <= end; slot++ {
		if _, occupied := items[inventoryKey(listType, slot)]; !occupied {
			return slot, true
		}
	}
	for slot := start; slot < requested; slot++ {
		if _, occupied := items[inventoryKey(listType, slot)]; !occupied {
			return slot, true
		}
	}
	return 0, false
}

func guildMedalInventorySlotContains(slot int16) bool {
	return slot >= guildMedalInventorySlotStart && slot <= guildMedalInventorySlotEnd
}

func (o *Owner) swapEquipSlots(ctx context.Context, cmd MoveCommand) (MoveResult, error) {
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	record, ok, err := o.equipment.Load(ctx, characterID)
	if err != nil {
		return MoveResult{}, err
	}
	if !ok {
		return MoveResult{}, ErrEquipmentNotFound
	}
	record = ensureEquipmentRecord(dnfrepo.CloneEquipment(record), characterID)
	srcKey := entryKey(cmd.SourceSlotIndex)
	dstKey := entryKey(cmd.DestinationSlotIndex)
	source, sourceOK := record.Entries[srcKey]
	destination, destinationOK := record.Entries[dstKey]
	if !sourceOK {
		return MoveResult{}, fmt.Errorf("%w: equip slot=%d", ErrMoveSlotNotFound, cmd.SourceSlotIndex)
	}
	if source.SlotIndex != cmd.SourceSlotIndex {
		return MoveResult{}, fmt.Errorf("%w: key=%d entry=%d", ErrMoveSlotMismatch, cmd.SourceSlotIndex, source.SlotIndex)
	}
	if len(source.RawEntry) == 0 {
		return MoveResult{}, ErrMoveRawEntryMissing
	}
	if destinationOK && destination.SlotIndex != cmd.DestinationSlotIndex {
		return MoveResult{}, fmt.Errorf("%w: key=%d entry=%d", ErrMoveSlotMismatch, cmd.DestinationSlotIndex, destination.SlotIndex)
	}
	if destinationOK && len(destination.RawEntry) == 0 {
		return MoveResult{}, ErrMoveRawEntryMissing
	}
	if cmd.SourceSlotIndex == cmd.DestinationSlotIndex {
		return MoveResult{CharacterID: characterID, SourceListType: cmd.SourceListType, SourceSlotIndex: cmd.SourceSlotIndex, DestinationListType: cmd.DestinationListType, DestinationSlotIndex: cmd.DestinationSlotIndex, ItemID: source.ItemID, Mode: "noop"}, nil
	}
	if err := o.validatePlacement(ctx, characterID, source.ItemID, wireListEquipment, cmd.SourceSlotIndex, cmd.DestinationSlotIndex); err != nil {
		return MoveResult{}, err
	}
	if destinationOK {
		if err := o.validatePlacement(ctx, characterID, destination.ItemID, wireListEquipment, cmd.DestinationSlotIndex, cmd.SourceSlotIndex); err != nil {
			return MoveResult{}, err
		}
	}
	source.SlotIndex = cmd.DestinationSlotIndex
	record.Entries[dstKey] = source
	if destinationOK {
		destination.SlotIndex = cmd.SourceSlotIndex
		record.Entries[srcKey] = destination
	} else {
		delete(record.Entries, srcKey)
	}
	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveEquipmentFields(ctx, o.equipment, record, dnfrepo.EquipmentFieldEntries); err != nil {
		return MoveResult{}, err
	}
	result := MoveResult{
		CharacterID:          characterID,
		SourceListType:       cmd.SourceListType,
		SourceSlotIndex:      cmd.SourceSlotIndex,
		DestinationListType:  cmd.DestinationListType,
		DestinationSlotIndex: cmd.DestinationSlotIndex,
		ItemID:               source.ItemID,
		Mode:                 "equip_slot_swap",
		Changed:              true,
	}
	if destinationOK {
		result.SwappedItemID = destination.ItemID
	}
	return result, nil
}

func (o *Owner) loadMoveRecords(ctx context.Context, characterID string) (dnfrepo.InventoryRecord, dnfrepo.EquipmentRecord, error) {
	inventory, ok, err := o.inventory.Load(ctx, characterID)
	if err != nil {
		return dnfrepo.InventoryRecord{}, dnfrepo.EquipmentRecord{}, err
	}
	if !ok {
		return dnfrepo.InventoryRecord{}, dnfrepo.EquipmentRecord{}, ErrInventoryNotFound
	}
	equipment, ok, err := o.equipment.Load(ctx, characterID)
	if err != nil {
		return dnfrepo.InventoryRecord{}, dnfrepo.EquipmentRecord{}, err
	}
	if !ok {
		return dnfrepo.InventoryRecord{}, dnfrepo.EquipmentRecord{}, ErrEquipmentNotFound
	}
	return ensureInventoryRecord(dnfrepo.CloneInventory(inventory), characterID), ensureEquipmentRecord(dnfrepo.CloneEquipment(equipment), characterID), nil
}

func (o *Owner) saveMove(ctx context.Context, inventory dnfrepo.InventoryRecord, equipment dnfrepo.EquipmentRecord, fields []dnfrepo.InventoryField, result MoveResult) (MoveResult, error) {
	now := time.Now()
	inventory.UpdatedAt = now
	equipment.UpdatedAt = now
	if err := dnfrepo.SaveInventoryFields(ctx, o.inventory, inventory, fields...); err != nil {
		return MoveResult{}, err
	}
	if err := dnfrepo.SaveEquipmentFields(ctx, o.equipment, equipment, dnfrepo.EquipmentFieldEntries); err != nil {
		return MoveResult{}, err
	}
	result.Changed = true
	result.InventorySlots = cloneItemMap(inventory.Slots)
	return result, nil
}

func entryKey(slotIndex int16) string {
	return strconv.FormatInt(int64(slotIndex), 10)
}

const currentEquipmentSlotCount int16 = 33

func validateMoveStructure(cmd MoveCommand) (moveKind, error) {
	if cmd.SourceListType != wireListEquipment && cmd.DestinationListType != wireListEquipment {
		return moveKindOrdinary, ErrMoveUnsupported
	}
	if cmd.SourceSlotIndex < 0 || cmd.DestinationSlotIndex < 0 {
		return moveKindOrdinary, fmt.Errorf("%w: src=%d dst=%d", ErrMoveSlotOutOfRange, cmd.SourceSlotIndex, cmd.DestinationSlotIndex)
	}
	if cmd.SourceListType == wireListEquipment && cmd.SourceSlotIndex >= currentEquipmentSlotCount {
		return moveKindOrdinary, fmt.Errorf("%w: source=%d max=%d", ErrMoveSlotOutOfRange, cmd.SourceSlotIndex, currentEquipmentSlotCount-1)
	}
	if cmd.DestinationListType == wireListEquipment && cmd.DestinationSlotIndex >= currentEquipmentSlotCount {
		return moveKindOrdinary, fmt.Errorf("%w: destination=%d max=%d", ErrMoveSlotOutOfRange, cmd.DestinationSlotIndex, currentEquipmentSlotCount-1)
	}

	petMove := (cmd.SourceListType == wireListPet &&
		cmd.DestinationListType == wireListEquipment &&
		cmd.SourceSlotIndex <= 139 &&
		cmd.DestinationSlotIndex == petCreatureWornSlot) ||
		(cmd.SourceListType == wireListEquipment &&
			cmd.SourceSlotIndex == petCreatureWornSlot &&
			cmd.DestinationListType == wireListPet &&
			cmd.DestinationSlotIndex <= 139)
	artifactMove := (cmd.SourceListType == wireListPet &&
		cmd.DestinationListType == wireListEquipment &&
		cmd.SourceSlotIndex >= petArtifactSourceSlotMin && cmd.SourceSlotIndex <= petArtifactSourceSlotMax &&
		isPetArtifactTarget(cmd.DestinationSlotIndex)) ||
		(cmd.SourceListType == wireListEquipment &&
			isPetArtifactTarget(cmd.SourceSlotIndex) &&
			cmd.DestinationListType == wireListPet &&
			cmd.DestinationSlotIndex >= petArtifactSourceSlotMin && cmd.DestinationSlotIndex <= petArtifactSourceSlotMax)
	if cmd.SourceListType == wireListPet || cmd.DestinationListType == wireListPet {
		if petMove {
			return moveKindPet, nil
		}
		if artifactMove {
			return moveKindArtifact, nil
		}
		return moveKindOrdinary, fmt.Errorf("%w: pet move src=(%d,%d) dst=(%d,%d)", ErrMoveUnsupported, cmd.SourceListType, cmd.SourceSlotIndex, cmd.DestinationListType, cmd.DestinationSlotIndex)
	}
	if !isOrdinaryMoveList(cmd.SourceListType) || !isOrdinaryMoveList(cmd.DestinationListType) {
		return moveKindOrdinary, fmt.Errorf("%w: src=%d dst=%d", ErrMoveUnsupported, cmd.SourceListType, cmd.DestinationListType)
	}
	return moveKindOrdinary, nil
}

type moveKind int

const (
	moveKindOrdinary moveKind = iota
	moveKindPet
	moveKindArtifact
)

// Current NoPack closes the artifact family as runtime targets 27/28/29
// (sub_35FC900 -> equip_artifactred/blue/green) and the shared list-7 source
// range 140..188 (sub_30A4950's unequip search); it is not one slot per color.
const (
	petArtifactSourceSlotMin int16 = 140
	petArtifactSourceSlotMax int16 = 188
)

func isPetArtifactTarget(slot int16) bool {
	return slot >= 27 && slot <= 29
}

func isOrdinaryMoveList(listType byte) bool {
	switch listType {
	case wireListMain, wireListAvatar, wireListPersonalCargo, wireListEquipment, wireListGuildMedal:
		return true
	default:
		return false
	}
}

func (o *Owner) validatePlacement(ctx context.Context, characterID string, itemID int64, sourceListType byte, sourceSlotIndex int16, targetSlotIndex int16) error {
	// Current NoPack op19 closes the creature body path as list-7 slots
	// 0..139 <-> actor worn target 26.
	if sourceListType == wireListPet && sourceSlotIndex >= 0 && sourceSlotIndex <= 139 && targetSlotIndex == petCreatureWornSlot {
		return nil
	}
	if o == nil || o.placementValidator == nil {
		return ErrMoveValidatorRequired
	}
	return o.placementValidator.ValidateEquipmentPlacement(ctx, Placement{
		CharacterID:     characterID,
		ItemID:          itemID,
		SourceListType:  sourceListType,
		SourceSlotIndex: sourceSlotIndex,
		TargetSlotIndex: targetSlotIndex,
	})
}

const (
	wireListMain          byte  = 0
	wireListAvatar        byte  = 1
	wireListPersonalCargo byte  = 2
	wireListEquipment     byte  = 3
	wireListPet           byte  = 7
	wireListGuildMedal    byte  = 38
	petCreatureWornSlot   int16 = 26
)

func ensureInventoryRecord(record dnfrepo.InventoryRecord, characterID string) dnfrepo.InventoryRecord {
	record.CharacterID = characterID
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if record.Warehouse == nil {
		record.Warehouse = make(map[string]dnfrepo.ItemStack)
	}
	return record
}

func ensureEquipmentRecord(record dnfrepo.EquipmentRecord, characterID string) dnfrepo.EquipmentRecord {
	record.CharacterID = characterID
	if record.Entries == nil {
		record.Entries = make(map[string]dnfrepo.EquipmentEntry)
	}
	return record
}

func inventoryMap(record *dnfrepo.InventoryRecord, listType byte) (map[string]dnfrepo.ItemStack, dnfrepo.InventoryField, bool) {
	switch listType {
	case wireListMain, wireListAvatar, wireListPet, wireListGuildMedal:
		return record.Slots, dnfrepo.InventoryFieldSlots, true
	case wireListPersonalCargo:
		return record.Warehouse, dnfrepo.InventoryFieldWarehouse, true
	default:
		return nil, "", false
	}
}

func inventoryKey(listType byte, slotIndex int16) string {
	return fmt.Sprintf("%d:%d", listType, slotIndex)
}

func entryFromStack(stack dnfrepo.ItemStack, sourceListType byte, equipSlot int16, sourceInstanceValue int32) (dnfrepo.EquipmentEntry, error) {
	if sourceListType == wireListPet && equipSlot == petCreatureWornSlot {
		return petCreatureEntryFromStack(stack, equipSlot)
	}
	raw := append([]byte(nil), stack.RawEntry...)
	if len(raw) == 0 {
		if currentTitleBookStackNeedsRawEntry(stack, sourceListType, equipSlot) {
			raw = buildCurrentTitleEquipmentRawEntry(stack.ItemID, equipSlot)
		} else if rebuilt, ok := buildCurrentEquipmentRawEntryFromMoveEvidence(stack, sourceListType, equipSlot, sourceInstanceValue); ok {
			raw = rebuilt
		} else {
			var err error
			raw, err = rawEntryFromExtra(stack.Extra)
			if err != nil {
				return dnfrepo.EquipmentEntry{}, err
			}
		}
	}
	raw = overlayEquipmentTailDataFromExtra(raw, stack.Extra)
	extra := cloneExtra(stack.Extra)
	// A permanent avatar is a real, occupied list-1 item whose current-EXE
	// amount field is zero.  Count==0 therefore cannot mean "empty slot" for
	// this container. Preserve the repository value explicitly while the item
	// is worn, otherwise an unequip would silently turn it into Count==1.
	//
	// This marker is internal persistence state only; the outward 0x77 amount
	// remains reconstructed from amount_or_count / the real ItemStack when a
	// list snapshot is emitted.
	extra["current_exe_inventory_count"] = strconv.FormatInt(stack.Count, 10)
	extra["equipped_slot"] = strconv.Itoa(int(equipSlot))
	// Persist the current-EXE actor slot chosen by the validated op19 move.
	// Initial PVF records use legacy worn-slot numbering and deliberately do
	// not carry this marker; once such an item is moved by the runtime, the
	// explicit slot must win on later op13/mode1 rebuilds.
	extra["current_exe_equipment_type"] = strconv.Itoa(int(equipSlot))
	extra["current_exe_runtime_move"] = "1"
	return dnfrepo.EquipmentEntry{
		SlotIndex: equipSlot,
		ItemID:    stack.ItemID,
		Bind:      stack.Bind,
		ExpireAt:  stack.ExpireAt,
		RawEntry:  raw,
		Extra:     extra,
	}, nil
}

const currentEquipmentRawEntryWireSize = 0x77

// buildCurrentEquipmentRawEntryFromMoveEvidence repairs only the historical
// relational rows that already have enough current-client/PVF evidence to be
// rendered as equipment but lost their opaque 0x77 row during import.  The
// live op19 source instance is required; no identity is invented when the
// client did not prove one.
func buildCurrentEquipmentRawEntryFromMoveEvidence(
	stack dnfrepo.ItemStack,
	sourceListType byte,
	equipSlot int16,
	sourceInstanceValue int32,
) ([]byte, bool) {
	if (sourceListType != wireListMain && sourceListType != wireListAvatar) ||
		stack.ItemID <= 0 || stack.ItemID > int64(^uint32(0)) || stack.Count != 1 ||
		sourceInstanceValue == 0 || stack.Extra == nil ||
		!strings.EqualFold(strings.TrimSpace(stack.Extra["item_kind"]), "equipment") ||
		!strings.HasPrefix(strings.ToLower(strings.TrimSpace(stack.Extra["pvf_path"])), "equipment/") {
		return nil, false
	}
	qualitySeed := uint32(firstExtraInt(stack.Extra, 0, "quality_seed"))
	durability := firstExtraInt(stack.Extra, -1, "durability", "current_durability")
	if !itemquality.Valid(qualitySeed) || durability < 0 || durability > int64(^uint16(0)) {
		return nil, false
	}
	raw := make([]byte, currentEquipmentRawEntryWireSize)
	binary.LittleEndian.PutUint16(raw[0:2], uint16(equipSlot))
	binary.LittleEndian.PutUint32(raw[2:6], uint32(stack.ItemID))
	binary.LittleEndian.PutUint32(raw[6:10], qualitySeed)
	raw[0x0A] = byte(firstExtraInt(stack.Extra, 0, "packed_flag_byte", "packed_flag"))
	binary.LittleEndian.PutUint16(raw[0x0B:0x0D], uint16(durability))
	raw[0x0D] = byte(firstExtraInt(stack.Extra, 0, "seal_flag", "seal"))
	binary.LittleEndian.PutUint32(raw[0x0E:0x12], uint32(sourceInstanceValue))
	return raw, true
}

func currentTitleBookStackNeedsRawEntry(stack dnfrepo.ItemStack, sourceListType byte, equipSlot int16) bool {
	if sourceListType != wireListMain ||
		equipSlot != 13 ||
		stack.ItemID <= 0 ||
		stack.ItemID > int64(^uint32(0)) ||
		!strings.EqualFold(strings.TrimSpace(stack.Extra["source"]), "title_book_get") {
		return false
	}
	category, categoryErr := strconv.ParseInt(strings.TrimSpace(stack.Extra["title_book_category"]), 10, 32)
	index, indexErr := strconv.ParseInt(strings.TrimSpace(stack.Extra["title_book_index"]), 10, 32)
	return categoryErr == nil && category >= 0 && category < 5 &&
		indexErr == nil && index >= 0 && index < 200
}

func buildCurrentTitleEquipmentRawEntry(itemID int64, equipSlot int16) []byte {
	// Current selected-equipment records use the same 43-byte constructor
	// summary as initial equipment. A title has no durability; byte 0 is the
	// validated actor slot, +1 is the PVF item ID, and +5 is the constructor's
	// required create scalar 1. The remaining fields are zero.
	raw := make([]byte, 43)
	raw[0] = byte(equipSlot)
	binary.LittleEndian.PutUint32(raw[1:5], uint32(itemID))
	binary.LittleEndian.PutUint32(raw[5:9], 1)
	return raw
}

func petCreatureEntryFromStack(stack dnfrepo.ItemStack, equipSlot int16) (dnfrepo.EquipmentEntry, error) {
	serial := firstExtraInt(stack.Extra, 0, "creature_serial_or_handle", "creature_serial", "pet_serial", "serial", "handle", "instance_value", "item_uid")
	if serial <= 0 {
		return dnfrepo.EquipmentEntry{}, ErrMoveRawEntryMissing
	}
	enchantCardID, enchantUpgradeCount, err := petEnchantFieldsFromStack(stack)
	if err != nil {
		return dnfrepo.EquipmentEntry{}, err
	}
	raw := buildPetCreatureEquipEntry(equipSlot, stack.ItemID, serial)
	binary.LittleEndian.PutUint32(raw[0x10:0x14], enchantCardID)
	raw[0x14] = enchantUpgradeCount
	extra := cloneExtra(stack.Extra)
	extra["equipped_slot"] = strconv.Itoa(int(equipSlot))
	extra["pet_enchant_card_item_id"] = strconv.FormatUint(uint64(enchantCardID), 10)
	extra["enchant_card_id"] = strconv.FormatUint(uint64(enchantCardID), 10)
	extra["enchant_upgrade_count"] = strconv.FormatUint(uint64(enchantUpgradeCount), 10)
	extra["value_a"] = strconv.FormatUint(uint64(enchantCardID), 10)
	extra["byte_12"] = strconv.FormatUint(uint64(enchantUpgradeCount), 10)
	extra["raw_entry_hex"] = hex.EncodeToString(raw)
	return dnfrepo.EquipmentEntry{
		SlotIndex: equipSlot,
		ItemID:    stack.ItemID,
		Bind:      stack.Bind,
		ExpireAt:  stack.ExpireAt,
		RawEntry:  raw,
		Extra:     extra,
	}, nil
}

func petEnchantFieldsFromStack(stack dnfrepo.ItemStack) (uint32, byte, error) {
	cardID := firstExtraInt(stack.Extra, -1, "pet_enchant_card_item_id", "enchant_card_id", "value_a")
	if cardID < 0 && len(stack.RawEntry) >= 0x13 {
		cardID = int64(binary.LittleEndian.Uint32(stack.RawEntry[0x0E:0x12]))
	}
	if cardID < 0 {
		cardID = 0
	}
	if cardID > int64(^uint32(0)) {
		return 0, 0, ErrMoveRawEntryInvalid
	}
	upgradeCount := firstExtraInt(stack.Extra, -1, "enchant_upgrade_count", "pet_enchant_upgrade_count", "byte_12")
	if upgradeCount < 0 && len(stack.RawEntry) >= 0x13 {
		upgradeCount = int64(stack.RawEntry[0x12])
	}
	if upgradeCount < 0 {
		upgradeCount = 0
	}
	if upgradeCount > int64(^byte(0)) {
		return 0, 0, ErrMoveRawEntryInvalid
	}
	return uint32(cardID), byte(upgradeCount), nil
}

func stackFromEntry(entry dnfrepo.EquipmentEntry) dnfrepo.ItemStack {
	extra := cloneExtra(entry.Extra)
	raw := overlayEquipmentTailDataFromExtra(append([]byte(nil), entry.RawEntry...), entry.Extra)
	if len(raw) > 0 {
		extra["raw_entry_hex"] = hex.EncodeToString(raw)
	}
	extra["equipment_slot"] = strconv.Itoa(int(entry.SlotIndex))
	if entry.SlotIndex == petCreatureWornSlot {
		if serial := petSerialFromEquippedRaw(raw); serial > 0 {
			extra["creature_serial_or_handle"] = strconv.FormatInt(serial, 10)
		}
	}
	return dnfrepo.ItemStack{
		ItemID:   entry.ItemID,
		Count:    equipmentEntryInventoryCount(entry),
		Bind:     entry.Bind,
		ExpireAt: entry.ExpireAt,
		RawEntry: raw,
		Extra:    extra,
	}
}

const (
	equipmentSocketCollectionOffset = 0x27
	equipmentSocketCollectionBytes  = 17
	equipmentLegacyTailDataOffset   = 0x2F
	equipmentTailDataBytes          = 37
)

var equipmentTailDataExtraKeys = []string{"tail_data_2f", "tailData2F", "tail2f", "raw_data_2f"}

func overlayEquipmentTailDataFromExtra(raw []byte, extra map[string]string) []byte {
	// Current NoPack parses raw+0x27..+0x37 as a live fixed-row vector.
	// Earlier code overlaid a legacy C# tail representation on that vector
	// during ordinary equip moves.  The current EXE has not proved that either
	// structure represents a normal-equipment socket, so preserve repository
	// raw bytes exactly until the real mutation is closed.
	_ = extra
	return raw
}

func equipmentSocketPrimaryValue(value []byte, visible bool) uint32 {
	if !visible {
		return 0
	}
	if len(value) >= 4 {
		if raw := binary.LittleEndian.Uint32(value[:4]); raw != 0 {
			return raw
		}
	}
	return ^uint32(0)
}

func equipmentTailDataFromExtra(extra map[string]string) []byte {
	if len(extra) == 0 {
		return nil
	}
	var fallback []byte
	for _, key := range equipmentTailDataExtraKeys {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		raw = strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", "0x", "", "0X", "").Replace(raw)
		decoded, err := hex.DecodeString(raw)
		if err != nil || len(decoded) == 0 {
			continue
		}
		out := make([]byte, equipmentTailDataBytes)
		copy(out, decoded)
		if equipmentTailHasSocketData(out) {
			return out
		}
		if fallback == nil {
			fallback = out
		}
	}
	return fallback
}

func equipmentTailHasSocketData(tail []byte) bool {
	if len(tail) == 0 {
		return false
	}
	if tail[0] != 0 {
		return true
	}
	if len(tail) >= 9 {
		return binary.LittleEndian.Uint32(tail[1:5]) != 0 || binary.LittleEndian.Uint32(tail[5:9]) != 0
	}
	for _, value := range tail {
		if value != 0 {
			return true
		}
	}
	return false
}

// isEquipmentMoveUnit accepts exactly one ordinary equipment item. The
// current client represents a permanent avatar in list 1 with amount/count
// zero, but it is still a single wearable object (its non-empty item id and
// later PVF placement check are authoritative). Do not broaden this exception
// to normal inventory lists, where zero remains invalid for an equip move.
func isEquipmentMoveUnit(stack dnfrepo.ItemStack, listType byte) bool {
	if stack.Count == 1 {
		return true
	}
	return listType == wireListAvatar && stack.Count == 0
}

// equipmentEntryInventoryCount restores the exact container count recorded at
// equip time. Historical worn rows predate that marker and are normal
// equipment, so they retain the proven single-item fallback of one.
func equipmentEntryInventoryCount(entry dnfrepo.EquipmentEntry) int64 {
	if raw := strings.TrimSpace(entry.Extra["current_exe_inventory_count"]); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value >= 0 {
			return value
		}
	}
	return 1
}

func rawEntryFromExtra(extra map[string]string) ([]byte, error) {
	if extra == nil {
		return nil, ErrMoveRawEntryMissing
	}
	raw := strings.TrimSpace(extra["raw_entry_hex"])
	if raw == "" {
		raw = strings.TrimSpace(extra["equipment_raw_entry_hex"])
	}
	if raw == "" {
		return nil, ErrMoveRawEntryMissing
	}
	raw = strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", "0x", "", "0X", "").Replace(raw)
	if len(raw)%2 != 0 {
		return nil, fmt.Errorf("%w: odd hex length", ErrMoveRawEntryInvalid)
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) == 0 {
		return nil, fmt.Errorf("%w: %v", ErrMoveRawEntryInvalid, err)
	}
	return decoded, nil
}

func buildPetCreatureEquipEntry(slot int16, itemID int64, serial int64) []byte {
	var buf bytes.Buffer
	writeByte := func(value byte) {
		_ = buf.WriteByte(value)
	}
	writeUint16 := func(value uint16) {
		var tmp [2]byte
		binary.LittleEndian.PutUint16(tmp[:], value)
		buf.Write(tmp[:])
	}
	writeUint32 := func(value uint32) {
		var tmp [4]byte
		binary.LittleEndian.PutUint32(tmp[:], value)
		buf.Write(tmp[:])
	}

	writeByte(byte(slot))
	writeUint32(uint32(clampInt32(itemID)))
	writeUint32(uint32(clampInt32(serial)))
	writeByte(0)
	writeUint16(0)
	writeUint32(0)
	writeUint32(0)
	writeByte(0)
	writeByte(0)
	writeUint16(0)
	writeUint32(uint32(clampInt32(serial)))
	writeByte(0)
	writeUint32(0)
	writeByte(0)
	writeUint16(0)
	writeByte(0)
	buf.Write(make([]byte, 10))
	return buf.Bytes()
}

func petSerialFromEquippedRaw(raw []byte) int64 {
	// The current pet constructor repeats the creature serial/handle at +24.
	// Do not accept the ordinary equipment instance scalar at +5. The current
	// creature row carries its authoritative serial at +24, while ordinary
	// equipment instance values are not creature identities.
	// identity. buildPetCreatureEquipEntry always writes the authoritative +24
	// field, so a shorter historical/ordinary row must fail closed here.
	if len(raw) >= 28 {
		value := int64(binary.LittleEndian.Uint32(raw[24:28]))
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstExtraInt(extra map[string]string, fallback int64, keys ...string) int64 {
	for _, key := range keys {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		value, err := strconv.ParseInt(raw, 0, 64)
		if err == nil {
			return value
		}
	}
	return fallback
}

func clampInt32(value int64) int32 {
	if value > int64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	if value < -int64(^uint32(0)>>1)-1 {
		return -int32(^uint32(0)>>1) - 1
	}
	return int32(value)
}

func cloneExtra(extra map[string]string) map[string]string {
	if len(extra) == 0 {
		return make(map[string]string)
	}
	out := make(map[string]string, len(extra)+2)
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func cloneItemMap(in map[string]dnfrepo.ItemStack) map[string]dnfrepo.ItemStack {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]dnfrepo.ItemStack, len(in))
	for key, value := range in {
		value.Extra = cloneExtra(value.Extra)
		out[key] = value
	}
	return out
}
