// 本文件定义时装和称号命令计划。
// 计划只记录客户端意图和 owner 预检所需字段，不直接修改外观、称号簿或物品资产。
package avatartitle

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// Command 是 avatartitle handler 交给 avatar/title owner 的最小命令计划。
type Command struct {
	Operation           string
	AccountID           string
	SelectedCharacterID uint16
	ConsumeSlot         int16
	Slot1               int16
	Slot2               int16
	RequestItemID       int32
	TargetSlot          int16
	TargetItemID        int32
	MaterialSlot        int16
	Emblems             []EmblemApply
	EmblemCount         int
	ItemSpaceRaw        int32
	TitleSlot           int16
	TitleItemID         int32
	TitleCategory       int32
	TitleIndex          int32
	NeedsOwner          string
}

// NewCompoundCommand 构造时装合成命令计划。
func NewCompoundCommand(req alignedcmd.Request, parsed CompoundAvatarRequest) Command {
	return Command{
		Operation:           "compound_avatar",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		ConsumeSlot:         parsed.ConsumeSlot,
		Slot1:               parsed.Slot1,
		Slot2:               parsed.Slot2,
		RequestItemID:       parsed.RequestItemID,
		NeedsOwner:          "inventory owner + avatar owner + mutation id + appearance/USERINFO/NOTI order",
	}
}

// NewEmblemCommand 构造时装徽章镶嵌命令计划。
func NewEmblemCommand(req alignedcmd.Request, parsed AvatarEmblemRequest) Command {
	emblems := append([]EmblemApply(nil), parsed.Emblems...)
	return Command{
		Operation:           "use_emblem",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		TargetSlot:          parsed.TargetSlot,
		TargetItemID:        parsed.TargetItemID,
		Emblems:             emblems,
		EmblemCount:         len(emblems),
		NeedsOwner:          "inventory owner + avatar owner + socket persistence + appearance/USERINFO/NOTI order",
	}
}

// NewSocketCommand 构造时装开孔命令计划。
func NewSocketCommand(req alignedcmd.Request, parsed AvatarSocketRequest) Command {
	return Command{
		Operation:           "add_avatar_socket",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		TargetSlot:          parsed.TargetSlot,
		TargetItemID:        parsed.TargetItemID,
		MaterialSlot:        parsed.MaterialSlot,
		NeedsOwner:          "inventory owner + avatar owner + socket persistence + appearance/USERINFO/NOTI order",
	}
}

// NewTitleBookCommand 构造称号簿放入或取出命令计划。
func NewTitleBookCommand(req alignedcmd.Request, operation string, parsed TitleBookRequest) Command {
	return Command{
		Operation:           operation,
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		ItemSpaceRaw:        parsed.ItemSpaceRaw,
		TitleSlot:           parsed.Slot,
		TitleItemID:         parsed.ItemID,
		TitleCategory:       parsed.Category,
		TitleIndex:          parsed.Index,
		NeedsOwner:          "inventory owner + title owner + mutation id + title book/USERINFO/NOTI order",
	}
}

func (c Command) String() string {
	base := fmt.Sprintf("account=%q char=%d", c.AccountID, c.SelectedCharacterID)
	switch c.Operation {
	case "compound_avatar":
		return fmt.Sprintf("%s consume=%d slot1=%d slot2=%d requestItem=%d needs=%s", base, c.ConsumeSlot, c.Slot1, c.Slot2, c.RequestItemID, c.NeedsOwner)
	case "use_emblem":
		return fmt.Sprintf("%s target=(%d,%d) emblems=%d needs=%s", base, c.TargetSlot, c.TargetItemID, c.EmblemCount, c.NeedsOwner)
	case "add_avatar_socket":
		return fmt.Sprintf("%s target=(%d,%d) material=%d needs=%s", base, c.TargetSlot, c.TargetItemID, c.MaterialSlot, c.NeedsOwner)
	default:
		return fmt.Sprintf("%s space=%d slot=%d item=%d category=%d index=%d needs=%s", base, c.ItemSpaceRaw, c.TitleSlot, c.TitleItemID, c.TitleCategory, c.TitleIndex, c.NeedsOwner)
	}
}
