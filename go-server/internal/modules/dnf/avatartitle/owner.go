// 本文件提供时装和称号命令的只读 owner 预检。
// 当前只读取角色、背包和穿戴快照，外观、称号簿和 USERINFO 刷新链路闭合前不会写库或开放成功 ACK。
package avatartitle

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable  = errors.New("avatar title owner unavailable")
	ErrCharacterRequired = errors.New("selected character id required")
	ErrCharacterNotFound = errors.New("character record not found")
	ErrInventoryNotFound = errors.New("inventory record not found")
	ErrSlotNotFound      = errors.New("inventory slot not found")
	ErrItemMismatch      = errors.New("inventory item mismatch")
)

// Owner 是时装/称号命令的读模型边界。
type Owner struct {
	characters dnfrepo.CharacterRepository
	inventory  dnfrepo.InventoryRepository
	equipment  dnfrepo.EquipmentRepository
}

// PlanResult 描述一次只读预检结果，不代表允许向旧客户端回成功包。
type PlanResult struct {
	AccountID             string
	CharacterID           string
	Operation             string
	InventoryKnown        bool
	EquipmentKnown        bool
	EquipmentEntryCount   int
	AvatarSourceFound     int
	AvatarSourceTotal     int
	TargetFound           bool
	TargetItemID          int64
	MaterialFound         bool
	MaterialItemID        int64
	EmblemMaterialFound   int
	EmblemMaterialTotal   int
	TitleInventoryFound   bool
	TitleInventoryList    byte
	TitleInventorySlot    int16
	TitleInventoryItemID  int64
	TitleBookCategory     int32
	TitleBookIndex        int32
	RequestedOutputItemID int32
}

// NewOwner 创建时装/称号 owner；缺少角色、背包或穿戴仓储时拒绝预检。
func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Character == nil || repos.Inventory == nil || repos.Equipment == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{characters: repos.Character, inventory: repos.Inventory, equipment: repos.Equipment}, nil
}

// Plan 只读取当前角色相关快照并校验请求槽位，等待 EXE/MCP 回包顺序闭合后再接真实写路径。
func (o *Owner) Plan(ctx context.Context, cmd Command) (PlanResult, error) {
	state, err := o.loadState(ctx, cmd)
	if err != nil {
		return PlanResult{}, err
	}
	result := PlanResult{
		AccountID:           state.character.AccountID,
		CharacterID:         state.characterID,
		Operation:           cmd.Operation,
		InventoryKnown:      true,
		EquipmentKnown:      state.equipmentKnown,
		EquipmentEntryCount: len(state.equipment.Entries),
		TitleBookCategory:   cmd.TitleCategory,
		TitleBookIndex:      cmd.TitleIndex,
	}

	switch cmd.Operation {
	case "compound_avatar":
		result.RequestedOutputItemID = cmd.RequestItemID
		found, total, err := state.checkAvatarSources(cmd.ConsumeSlot, cmd.Slot1, cmd.Slot2)
		if err != nil {
			return PlanResult{}, err
		}
		result.AvatarSourceFound = found
		result.AvatarSourceTotal = total
	case "use_emblem":
		target, err := state.requireInventorySlot(listTypeAvatar, cmd.TargetSlot, int64(cmd.TargetItemID))
		if err != nil {
			return PlanResult{}, err
		}
		result.TargetFound = target.Found
		result.TargetItemID = target.ItemID
		found, total, err := state.checkEmblems(cmd.Emblems)
		if err != nil {
			return PlanResult{}, err
		}
		result.EmblemMaterialFound = found
		result.EmblemMaterialTotal = total
	case "add_avatar_socket":
		target, err := state.requireInventorySlot(listTypeAvatar, cmd.TargetSlot, int64(cmd.TargetItemID))
		if err != nil {
			return PlanResult{}, err
		}
		material, err := state.requireInventorySlot(listTypeMain, cmd.MaterialSlot, 0)
		if err != nil {
			return PlanResult{}, err
		}
		result.TargetFound = target.Found
		result.TargetItemID = target.ItemID
		result.MaterialFound = material.Found
		result.MaterialItemID = material.ItemID
	case "title_book_put":
		title, err := state.requireTitleInventory(cmd.ItemSpaceRaw, cmd.TitleSlot, int64(cmd.TitleItemID))
		if err != nil {
			return PlanResult{}, err
		}
		result.TitleInventoryFound = title.Found
		result.TitleInventoryList = title.ListType
		result.TitleInventorySlot = title.SlotIndex
		result.TitleInventoryItemID = title.ItemID
	case "title_book_get":
		title := state.findTitleInventory(cmd.ItemSpaceRaw, cmd.TitleSlot, int64(cmd.TitleItemID))
		result.TitleInventoryFound = title.Found
		result.TitleInventoryList = title.ListType
		result.TitleInventorySlot = title.SlotIndex
		result.TitleInventoryItemID = title.ItemID
	}
	return result, nil
}

type ownerState struct {
	characterID    string
	character      dnfrepo.CharacterRecord
	inventory      dnfrepo.InventoryRecord
	equipment      dnfrepo.EquipmentRecord
	equipmentKnown bool
	inventorySlots map[string]dnfrepo.ItemStack
	inventoryCargo map[string]dnfrepo.ItemStack
}

type slotCheck struct {
	ListType  byte
	SlotIndex int16
	ItemID    int64
	Found     bool
}

func (o *Owner) loadState(ctx context.Context, cmd Command) (ownerState, error) {
	if o == nil || o.characters == nil || o.inventory == nil || o.equipment == nil {
		return ownerState{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return ownerState{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	character, ok, err := o.characters.Load(ctx, characterID)
	if err != nil {
		return ownerState{}, err
	}
	if !ok {
		return ownerState{}, ErrCharacterNotFound
	}
	inventory, ok, err := o.inventory.Load(ctx, characterID)
	if err != nil {
		return ownerState{}, err
	}
	if !ok {
		return ownerState{}, ErrInventoryNotFound
	}
	equipment, equipmentKnown, err := o.equipment.Load(ctx, characterID)
	if err != nil {
		return ownerState{}, err
	}

	inventory = dnfrepo.CloneInventory(inventory)
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	if inventory.Warehouse == nil {
		inventory.Warehouse = make(map[string]dnfrepo.ItemStack)
	}
	equipment = dnfrepo.CloneEquipment(equipment)
	if equipment.Entries == nil {
		equipment.Entries = make(map[string]dnfrepo.EquipmentEntry)
	}
	return ownerState{
		characterID:    characterID,
		character:      dnfrepo.CloneCharacter(character),
		inventory:      inventory,
		equipment:      equipment,
		equipmentKnown: equipmentKnown,
		inventorySlots: inventory.Slots,
		inventoryCargo: inventory.Warehouse,
	}, nil
}

func (s ownerState) checkAvatarSources(slots ...int16) (int, int, error) {
	found := 0
	total := 0
	for _, slot := range slots {
		if slot < 0 {
			continue
		}
		total++
		if _, err := s.requireInventorySlot(listTypeAvatar, slot, 0); err != nil {
			return found, total, err
		}
		found++
	}
	if total == 0 {
		return 0, 0, ErrSlotNotFound
	}
	return found, total, nil
}

func (s ownerState) checkEmblems(emblems []EmblemApply) (int, int, error) {
	found := 0
	for _, emblem := range emblems {
		if emblem.EmblemSlot < 0 {
			continue
		}
		check, err := s.requireInventorySlot(listTypeMain, emblem.EmblemSlot, int64(emblem.EmblemItemID))
		if err != nil {
			return found, len(emblems), err
		}
		if check.Found {
			found++
		}
	}
	return found, len(emblems), nil
}

func (s ownerState) requireTitleInventory(spaceRaw int32, slot int16, expectedItemID int64) (slotCheck, error) {
	check := s.findTitleInventory(spaceRaw, slot, expectedItemID)
	if !check.Found {
		return check, fmt.Errorf("%w: title space=%d slot=%d", ErrSlotNotFound, spaceRaw, slot)
	}
	if expectedItemID != 0 && check.ItemID != expectedItemID {
		return check, fmt.Errorf("%w: title space=%d slot=%d want=%d got=%d", ErrItemMismatch, spaceRaw, slot, expectedItemID, check.ItemID)
	}
	return check, nil
}

func (s ownerState) findTitleInventory(spaceRaw int32, slot int16, expectedItemID int64) slotCheck {
	candidates := titleListCandidates(spaceRaw)
	firstFound := slotCheck{}
	for _, listType := range candidates {
		check := s.findInventorySlot(listType, slot)
		if !check.Found {
			continue
		}
		if firstFound == (slotCheck{}) {
			firstFound = check
		}
		if expectedItemID == 0 || check.ItemID == expectedItemID {
			return check
		}
	}
	return firstFound
}

func (s ownerState) requireInventorySlot(listType byte, slot int16, expectedItemID int64) (slotCheck, error) {
	check := s.findInventorySlot(listType, slot)
	if !check.Found {
		return check, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, listType, slot)
	}
	if expectedItemID != 0 && check.ItemID != expectedItemID {
		return check, fmt.Errorf("%w: list=%d slot=%d want=%d got=%d", ErrItemMismatch, listType, slot, expectedItemID, check.ItemID)
	}
	return check, nil
}

func (s ownerState) findInventorySlot(listType byte, slot int16) slotCheck {
	if slot < 0 {
		return slotCheck{ListType: listType, SlotIndex: slot}
	}
	items := s.inventorySlots
	if listType == listTypePersonalCargo {
		items = s.inventoryCargo
	}
	stack, ok := items[inventoryKey(listType, slot)]
	if !ok {
		return slotCheck{ListType: listType, SlotIndex: slot}
	}
	return slotCheck{
		ListType:  listType,
		SlotIndex: slot,
		ItemID:    stack.ItemID,
		Found:     true,
	}
}

func titleListCandidates(spaceRaw int32) []byte {
	candidates := make([]byte, 0, 3)
	add := func(listType byte) {
		for _, existing := range candidates {
			if existing == listType {
				return
			}
		}
		candidates = append(candidates, listType)
	}
	if spaceRaw >= 0 && spaceRaw <= 255 {
		add(byte(spaceRaw))
	}
	add(listTypeMain)
	add(listTypeAvatar)
	return candidates
}

func inventoryKey(listType byte, slotIndex int16) string {
	return fmt.Sprintf("%d:%d", listType, slotIndex)
}

func planError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", strings.TrimSpace(operation), err)
}

const (
	listTypeMain          byte = 0
	listTypePersonalCargo byte = 2
)
