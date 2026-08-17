// 本文件定义礼包和随机盒命令计划。
// 计划只记录开包意图，不直接消耗物品、发奖励或触发弹窗。
package packageitem

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// Command 是 packageitem handler 交给后续 reward/package owner 的最小命令计划。
// 礼包可能产出称号、时装、宠物和道具，必须由奖励 owner 严格按 EXE 包体顺序落地。
type Command struct {
	Operation              string
	AccountID              string
	SelectedCharacterID    uint16
	SlotIndex              int16
	SelectionContext       int16
	SelectedItemTemplateID int32
	SelectionFlag          byte
	AvatarChoiceCount      int
	ListType               byte
	RawListType            byte
	MaterialSlotIndex      int16
	NeedsOwner             string
}

// NewSelectableCommand 构造可选礼包开启计划。
func NewSelectableCommand(req alignedcmd.Request, parsed SelectablePackageRequest) Command {
	return Command{
		Operation:              "use_booster_item",
		AccountID:              strings.TrimSpace(req.AccountID),
		SelectedCharacterID:    req.SelectedCharacterID,
		SlotIndex:              parsed.SlotIndex,
		SelectionContext:       parsed.SelectionContext,
		SelectedItemTemplateID: parsed.SelectedItemTemplateID,
		SelectionFlag:          parsed.SelectionFlag,
		AvatarChoiceCount:      parsed.AvatarChoiceCount,
		NeedsOwner:             "inventory owner + package reward owner + mutation id + popup/NOTI order",
	}
}

// NewMagicBoxCommand 构造随机盒单开计划。
func NewMagicBoxCommand(req alignedcmd.Request, parsed MagicBoxSingleRequest) Command {
	return Command{
		Operation:           "use_randombox_item",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		ListType:            parsed.ListType,
		RawListType:         parsed.RawListType,
		SlotIndex:           parsed.SlotIndex,
		MaterialSlotIndex:   parsed.MaterialSlotIndex,
		NeedsOwner:          "inventory owner + random reward owner + mutation id + popup/NOTI order",
	}
}

// NewMagicBoxExpandCommand 构造随机盒连开（全部开启）计划。
func NewMagicBoxExpandCommand(req alignedcmd.Request, parsed MagicBoxExpandRequest) Command {
	return Command{
		Operation:           "use_randombox_item_expand",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		ListType:            parsed.ListType,
		RawListType:         parsed.RawListType,
		SlotIndex:           parsed.SlotIndex,
		MaterialSlotIndex:   parsed.MaterialSlotIndex,
		NeedsOwner:          "inventory owner + random reward owner + mutation id + popup/NOTI order",
	}
}

func (c Command) String() string {
	base := fmt.Sprintf("account=%q char=%d", c.AccountID, c.SelectedCharacterID)
	if c.Operation == "use_randombox_item" || c.Operation == "use_randombox_item_expand" {
		return fmt.Sprintf("%s list=%d slot=%d materialSlot=%d needs=%s", base, c.ListType, c.SlotIndex, c.MaterialSlotIndex, c.NeedsOwner)
	}
	return fmt.Sprintf("%s slot=%d context=%d selected=%d flag=%d avatarChoices=%d needs=%s", base, c.SlotIndex, c.SelectionContext, c.SelectedItemTemplateID, c.SelectionFlag, c.AvatarChoiceCount, c.NeedsOwner)
}
