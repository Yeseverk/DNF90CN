// 本文件定义背包类命令计划。
// 计划只承载已解析的客户端意图；真正资产变更必须进入可靠 owner、事务和幂等边界后才能回包。
package inventory

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// Command 是 inventory handler 交给后续 inventory/equipment/wallet owner 的最小命令计划。
type Command struct {
	Operation              string
	AccountID              string
	SelectedCharacterID    uint16
	SourceListType         byte
	SourceSlotIndex        int16
	SourceInstanceValue    int32
	ReservedValue          uint32
	MoveCount              int32
	DestinationListType    byte
	DestinationSlotIndex   int16
	DestinationInstance    int32
	DestinationStack       int32
	ActorIndex             int32
	TrailingState0         byte
	TrailingState1         byte
	EntryCount             int
	DeleteEntries          []DeleteEntry
	Count                  int32
	ItemTemplateID         int32
	RepairSlot             int16
	AutoRepair             bool
	QuickRepair            bool
	TargetSlotIndex        int16
	TargetItemTemplateID   int32
	MaterialSlotIndex      int16
	MaterialItemTemplateID int32
	OptionalTicketSlot     int16
	Mode                   string
	TargetItemName         string
	AmplifyAction          byte
	SelectedAmplifyOption  byte
	RandomOptionIndex      byte
	InventoryManagerState  uint16
	BeadListType           byte
	BeadSlotIndex          int16
	TargetListType         byte
	ItemSpace              byte
	DisjointItemSlotIndex  int16
	ContextValue           int32
	ActionIndex            uint32
	DamageFontIndex        uint16
	Category               byte
	Condition              byte
	NeedsOwner             string
	// Upgrade policy fields (populated by dnfbridge from PVF table).
	// SuccessWeight is out of 100000; >= 100000 means guaranteed success.
	UpgradeSuccessWeight int
	// PenaltyType: 0=fail no change, 1=downgrade, 3=destroy.
	UpgradePenaltyType int
	// MaterialItemID/Count are the runtime-PVF upgrade table requirement.
	UpgradeMaterialItemID int
	UpgradeMaterialCount  int
	// DestroyBonusItemID/Count: compensation item on destruction.
	UpgradeDestroyBonusItemID int
	UpgradeDestroyBonusCount  int
	// NoticeLevel: announcement threshold (-1 = disabled).
	UpgradeNoticeLevel int
	// UpgradePolicyError records PVF resolver failures so packet-log command
	// reasons do not silently degrade into materialPVF=(0,0).
	UpgradePolicyError string
}

// NewDeleteCommand 构造删除物品命令计划。
func NewDeleteCommand(req alignedcmd.Request, parsed DeleteRequest) Command {
	cmd := baseCommand(req, "delete_item", "inventory owner + mutation id + backpack/USERINFO/NOTI order")
	cmd.SourceListType = parsed.ListType
	cmd.SourceSlotIndex = parsed.SlotIndex
	cmd.Count = int32(parsed.Count)
	cmd.EntryCount = len(parsed.Entries)
	cmd.DeleteEntries = append([]DeleteEntry(nil), parsed.Entries...)
	if parsed.Extended && len(parsed.Entries) > 0 {
		cmd.SourceSlotIndex = parsed.Entries[0].SlotIndex
		cmd.ItemTemplateID = parsed.Entries[0].ItemID
		cmd.Count = parsed.Entries[0].DeleteCount
	}
	return cmd
}

// NewMoveCommand 构造移动或拆分物品命令计划。
func NewMoveCommand(req alignedcmd.Request, parsed MoveItemspaceRequest) Command {
	cmd := baseCommand(req, "move_itemspace", "inventory/equipment/cargo owner + mutation id + backpack/USERINFO/NOTI order")
	cmd.SourceListType = parsed.SourceListType
	cmd.SourceSlotIndex = parsed.SourceSlotIndex
	cmd.SourceInstanceValue = parsed.SourceInstanceValue
	cmd.MoveCount = parsed.MoveCount
	cmd.DestinationListType = parsed.DestinationListType
	cmd.DestinationSlotIndex = parsed.DestinationSlotIndex
	cmd.DestinationInstance = parsed.DestinationInstanceValue
	cmd.DestinationStack = parsed.DestinationStack
	cmd.ActorIndex = parsed.ActorIndex
	cmd.TrailingState0 = parsed.TrailingState0
	cmd.TrailingState1 = parsed.TrailingState1
	return cmd
}

// NewSortCommand 构造整理背包命令计划。
func NewSortCommand(req alignedcmd.Request, parsed SortItemRequest) Command {
	cmd := baseCommand(req, "sort_item", "inventory owner + deterministic sort + backpack refresh order")
	cmd.SourceListType = parsed.ListType
	cmd.Category = parsed.Category
	cmd.Condition = parsed.Condition
	return cmd
}

// NewBuyCommand 构造 NPC 购买命令计划。
func NewBuyCommand(req alignedcmd.Request, parsed BuyItemRequest) Command {
	cmd := baseCommand(req, "buy_item", "inventory owner + wallet owner + shop validation + mutation id + popup/NOTI order")
	cmd.ItemTemplateID = parsed.ItemTemplateID
	cmd.Count = parsed.Count
	return cmd
}

// NewSellCommand 构造出售物品命令计划。
func NewSellCommand(req alignedcmd.Request, parsed DeleteOrSellRequest) Command {
	cmd := baseCommand(req, "sell_item", "inventory owner + wallet owner + mutation id + backpack/USERINFO/NOTI order")
	cmd.SourceListType = parsed.ListType
	cmd.SourceSlotIndex = parsed.SlotIndex
	cmd.Count = int32(parsed.Count)
	return cmd
}

// NewRepairCommand 构造修理装备命令计划。
func NewRepairCommand(req alignedcmd.Request, parsed RepairEquipmentRequest) Command {
	cmd := baseCommand(req, "repair_equipment", "equipment owner + wallet owner + durability persistence + USERINFO/NOTI order")
	cmd.SourceListType = parsed.InvenType
	cmd.SourceSlotIndex = parsed.SlotIndex
	cmd.RepairSlot = parsed.RepairItemSlot
	cmd.AutoRepair = parsed.AutoRepair
	cmd.QuickRepair = parsed.QuickRepair
	return cmd
}

// NewDisjointCommand 构造分解物品命令计划。
func NewDisjointCommand(req alignedcmd.Request, parsed DisjointItemRequest) Command {
	cmd := baseCommand(req, "disjoint_item", "inventory owner + disjoint reward owner + mutation id + ACK/backpack/popup order")
	cmd.TargetSlotIndex = parsed.TargetSlotIndex
	cmd.ItemSpace = parsed.ItemSpace
	cmd.DisjointItemSlotIndex = parsed.DisjointItemSlotIndex
	cmd.ContextValue = parsed.ContextValue
	return cmd
}

// NewUseStackableCommand 构造使用消耗品命令计划。
func NewUseStackableCommand(req alignedcmd.Request, parsed UseStackableRequest) Command {
	cmd := baseCommand(req, "use_stackable", "inventory owner + effect/reward owner + mutation id + USERINFO/NOTI order")
	cmd.SourceListType = parsed.ListType
	cmd.SourceSlotIndex = parsed.SlotIndex
	cmd.SourceInstanceValue = parsed.InstanceValue
	cmd.ItemTemplateID = parsed.ItemCode
	cmd.ReservedValue = parsed.Reserved
	return cmd
}

func NewUseStackableActionCommand(req alignedcmd.Request, parsed UseStackableActionRequest) Command {
	cmd := baseCommand(req, "use_stackable_action", "character + inventory atomic owner + runtime-PVF damage-font definition")
	cmd.SourceListType = parsed.ListType
	cmd.SourceSlotIndex = parsed.SourceSlotIndex
	cmd.ActionIndex = parsed.ActionIndex
	return cmd
}

func NewSelectDamageFontCommand(req alignedcmd.Request, parsed SelectDamageFontRequest) Command {
	cmd := baseCommand(req, "select_damage_font", "character asset owner + owned/unexpired damage-font state")
	cmd.DamageFontIndex = parsed.FontIndex
	return cmd
}

// NewUpgradeCommand 构造强化或增幅命令计划。
func NewUpgradeCommand(req alignedcmd.Request, parsed UpgradeItemRequest) Command {
	cmd := baseCommand(req, "upgrade_item", "equipment owner + wallet/material owner + mutation id + reinforce result/USERINFO order")
	cmd.Mode = parsed.Mode
	cmd.TargetSlotIndex = parsed.TargetSlotIndex
	cmd.TargetItemTemplateID = parsed.TargetItemTemplateID
	cmd.MaterialSlotIndex = parsed.MaterialSlotIndex
	cmd.OptionalTicketSlot = parsed.OptionalTicketSlotIndex
	cmd.TargetItemName = parsed.TargetItemName
	cmd.UpgradeNoticeLevel = -1
	return cmd
}

// NewEnchantCommand 构造宝珠附魔命令计划。
func NewEnchantCommand(req alignedcmd.Request, parsed EnchantByBeadRequest) Command {
	cmd := baseCommand(req, "enchant_by_bead", "inventory owner + equipment owner + mutation id + equipment/USERINFO/NOTI order")
	cmd.BeadListType = parsed.BeadListType
	cmd.BeadSlotIndex = parsed.BeadSlotIndex
	cmd.TargetListType = parsed.TargetListType
	cmd.TargetSlotIndex = parsed.TargetSlotIndex
	return cmd
}

func NewPurifyItemCommand(req alignedcmd.Request, parsed PurifyItemRequest) Command {
	cmd := baseCommand(req, "purify_item", "inventory owner + runtime PVF amplify config + atomic target/material mutation + current-EXE op204 ACK")
	cmd.TargetSlotIndex = parsed.TargetSlotIndex
	cmd.TargetItemTemplateID = parsed.TargetItemTemplateID
	cmd.MaterialSlotIndex = parsed.MaterialSlotIndex
	cmd.MaterialItemTemplateID = parsed.MaterialItemTemplateID
	return cmd
}

func NewInvestItemAmplifyOptionCommand(req alignedcmd.Request, parsed InvestItemAmplifyOptionRequest) Command {
	cmd := baseCommand(req, "invest_item_amplify_option", "inventory owner + runtime PVF amplify config + atomic target/material mutation + current-EXE op205 ACK")
	cmd.AmplifyAction = parsed.Action
	cmd.TargetSlotIndex = parsed.TargetSlotIndex
	cmd.TargetItemTemplateID = parsed.TargetItemTemplateID
	cmd.MaterialSlotIndex = parsed.MaterialSlotIndex
	cmd.MaterialItemTemplateID = parsed.MaterialItemTemplateID
	cmd.SelectedAmplifyOption = parsed.SelectedOption
	cmd.TargetItemName = parsed.TargetItemName
	return cmd
}

func baseCommand(req alignedcmd.Request, operation string, needs string) Command {
	return Command{
		Operation:           operation,
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		NeedsOwner:          needs,
	}
}

// NewUnsealRandomOptionCommand constructs the current op401 mutation plan.
func NewUnsealRandomOptionCommand(req alignedcmd.Request, parsed UnsealRandomOptionRequest) Command {
	cmd := baseCommand(req, "unseal_random_option", "inventory + character wallet atomic owner + runtime PVF")
	cmd.TargetSlotIndex = parsed.TargetSlotIndex
	cmd.InventoryManagerState = parsed.InventoryManagerState
	return cmd
}

// NewChangeRandomOptionCommand constructs the current op437 mutation plan.
func NewChangeRandomOptionCommand(req alignedcmd.Request, parsed ChangeRandomOptionRequest) Command {
	cmd := baseCommand(req, "change_random_option", "inventory + character wallet atomic owner + runtime PVF")
	cmd.TargetSlotIndex = parsed.TargetSlotIndex
	cmd.ReservedValue = uint32(parsed.Reserved)
	cmd.RandomOptionIndex = parsed.OptionIndex
	return cmd
}

func (c Command) String() string {
	base := fmt.Sprintf("account=%q char=%d", c.AccountID, c.SelectedCharacterID)
	switch c.Operation {
	case "delete_item", "sell_item":
		return fmt.Sprintf("%s list=%d slot=%d count=%d entries=%d item=%d needs=%s", base, c.SourceListType, c.SourceSlotIndex, c.Count, c.EntryCount, c.ItemTemplateID, c.NeedsOwner)
	case "move_itemspace":
		return fmt.Sprintf("%s src=(%d,%d,0x%08X) count=%d dst=(%d,%d,0x%08X) dstStack=%d actor=%d tail=(%d,%d) needs=%s", base, c.SourceListType, c.SourceSlotIndex, uint32(c.SourceInstanceValue), c.MoveCount, c.DestinationListType, c.DestinationSlotIndex, uint32(c.DestinationInstance), c.DestinationStack, c.ActorIndex, c.TrailingState0, c.TrailingState1, c.NeedsOwner)
	case "sort_item":
		return fmt.Sprintf("%s list=%d category=%d condition=%d needs=%s", base, c.SourceListType, c.Category, c.Condition, c.NeedsOwner)
	case "buy_item":
		return fmt.Sprintf("%s item=%d count=%d needs=%s", base, c.ItemTemplateID, c.Count, c.NeedsOwner)
	case "repair_equipment":
		return fmt.Sprintf("%s list=%d slot=%d repairSlot=%d auto=%t quick=%t needs=%s", base, c.SourceListType, c.SourceSlotIndex, c.RepairSlot, c.AutoRepair, c.QuickRepair, c.NeedsOwner)
	case "disjoint_item":
		return fmt.Sprintf("%s targetSlot=%d itemSpace=%d disjointSlot=%d ctx=0x%08X needs=%s", base, c.TargetSlotIndex, c.ItemSpace, c.DisjointItemSlotIndex, uint32(c.ContextValue), c.NeedsOwner)
	case "use_stackable":
		return fmt.Sprintf("%s list=%d slot=%d instance=0x%08X item=0x%08X reserved=0x%08X needs=%s", base, c.SourceListType, c.SourceSlotIndex, uint32(c.SourceInstanceValue), uint32(c.ItemTemplateID), c.ReservedValue, c.NeedsOwner)
	case "upgrade_item":
		if c.UpgradePolicyError != "" {
			return fmt.Sprintf("%s mode=%s target=(%d,%d) materialSlot=%d materialPVF=(%d,%d) policyErr=%q ticket=%d name=%q needs=%s", base, c.Mode, c.TargetSlotIndex, c.TargetItemTemplateID, c.MaterialSlotIndex, c.UpgradeMaterialItemID, c.UpgradeMaterialCount, c.UpgradePolicyError, c.OptionalTicketSlot, c.TargetItemName, c.NeedsOwner)
		}
		return fmt.Sprintf("%s mode=%s target=(%d,%d) materialSlot=%d materialPVF=(%d,%d) ticket=%d name=%q needs=%s", base, c.Mode, c.TargetSlotIndex, c.TargetItemTemplateID, c.MaterialSlotIndex, c.UpgradeMaterialItemID, c.UpgradeMaterialCount, c.OptionalTicketSlot, c.TargetItemName, c.NeedsOwner)
	case "enchant_by_bead":
		return fmt.Sprintf("%s bead=(%d,%d) target=(%d,%d) needs=%s", base, c.BeadListType, c.BeadSlotIndex, c.TargetListType, c.TargetSlotIndex, c.NeedsOwner)
	case "purify_item":
		return fmt.Sprintf("%s target=(%d,%d) material=(%d,%d) needs=%s", base, c.TargetSlotIndex, c.TargetItemTemplateID, c.MaterialSlotIndex, c.MaterialItemTemplateID, c.NeedsOwner)
	case "invest_item_amplify_option":
		return fmt.Sprintf("%s action=%d target=(%d,%d) material=(%d,%d) selected=%d name=%q needs=%s", base, c.AmplifyAction, c.TargetSlotIndex, c.TargetItemTemplateID, c.MaterialSlotIndex, c.MaterialItemTemplateID, c.SelectedAmplifyOption, c.TargetItemName, c.NeedsOwner)
	case "unseal_random_option":
		return fmt.Sprintf("%s targetSlot=%d managerState=0x%04X needs=%s", base, c.TargetSlotIndex, c.InventoryManagerState, c.NeedsOwner)
	case "change_random_option":
		return fmt.Sprintf("%s targetSlot=%d reserved=0x%04X optionIndex=%d needs=%s", base, c.TargetSlotIndex, c.ReservedValue, c.RandomOptionIndex, c.NeedsOwner)
	default:
		return fmt.Sprintf("%s needs=%s", base, c.NeedsOwner)
	}
}
