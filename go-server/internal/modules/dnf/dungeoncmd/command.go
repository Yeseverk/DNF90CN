// 本文件定义副本类命令计划。
// 副本命令会影响房间、掉落、经验和进场门闸，当前只记录意图和缺口，不伪造成功响应。
package dungeoncmd

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// Command 是 dungeoncmd handler 交给后续 dungeon/room owner 的最小命令计划。
type Command struct {
	Operation           string
	AccountID           string
	SelectedCharacterID uint16
	DungeonID           uint32
	Difficulty          byte
	EntryOption         uint16
	SelectionMode       byte
	DropObjectKey       uint32
	NextX               byte
	NextY               byte
	TutorialProgress    uint32
	TutorialCommit      byte
	RawLen              int
	NeedsOwner          string
}

// NewRawCommand 构造尚未闭合包体字段的副本命令计划。
func NewRawCommand(req alignedcmd.Request, operation string, needs string) Command {
	return Command{
		Operation:           operation,
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		RawLen:              len(req.Body),
		NeedsOwner:          needs,
	}
}

// NewSelectCommand 构造选择副本命令计划。
func NewSelectCommand(req alignedcmd.Request, parsed SelectDungeonRequest) Command {
	cmd := NewRawCommand(req, "select_dungeon", "dungeon owner + party/entry validation + scene bootstrap response order")
	cmd.DungeonID = parsed.DungeonID
	cmd.Difficulty = parsed.Difficulty
	cmd.EntryOption = parsed.EntryOption
	cmd.SelectionMode = parsed.SelectionMode
	return cmd
}

// NewGetItemCommand 构造副本内拾取掉落命令计划。
func NewGetItemCommand(req alignedcmd.Request, parsed GetItemRequest) Command {
	cmd := NewRawCommand(req, "get_item", "room drop owner + inventory owner + mutation id + drop/backpack notify order")
	cmd.DropObjectKey = parsed.DropObjectKey
	return cmd
}

// NewMoveMapCommand 构造切换房间命令计划。
func NewMoveMapCommand(req alignedcmd.Request, parsed MoveMapRequest) Command {
	cmd := NewRawCommand(req, "move_map", "active DungeonSession + current op45 ACK + proven target-room actor/object packet chain")
	cmd.NextX = parsed.NextX
	cmd.NextY = parsed.NextY
	return cmd
}

// NewTutorialCommand 构造教程标记命令计划。
func NewTutorialCommand(req alignedcmd.Request, parsed ChangeTutorialFlagRequest) Command {
	cmd := NewRawCommand(req, "change_tutorial_flag", "tutorial owner + profile persistence + current EXE response evidence")
	cmd.TutorialProgress = parsed.Progress
	cmd.TutorialCommit = parsed.CommitFlag
	return cmd
}

func (c Command) String() string {
	base := fmt.Sprintf("account=%q char=%d", c.AccountID, c.SelectedCharacterID)
	switch c.Operation {
	case "select_dungeon":
		return fmt.Sprintf("%s dungeon=%d difficulty=%d entryOption=%d selectionMode=%d needs=%s", base, c.DungeonID, c.Difficulty, c.EntryOption, c.SelectionMode, c.NeedsOwner)
	case "get_item":
		return fmt.Sprintf("%s dropObject=%d needs=%s", base, c.DropObjectKey, c.NeedsOwner)
	case "move_map":
		return fmt.Sprintf("%s next=(%d,%d) rawLen=%d needs=%s", base, c.NextX, c.NextY, c.RawLen, c.NeedsOwner)
	case "change_tutorial_flag":
		return fmt.Sprintf("%s progress=%d commit=%d rawLen=%d needs=%s", base, c.TutorialProgress, c.TutorialCommit, c.RawLen, c.NeedsOwner)
	default:
		return fmt.Sprintf("%s rawLen=%d needs=%s", base, c.RawLen, c.NeedsOwner)
	}
}
