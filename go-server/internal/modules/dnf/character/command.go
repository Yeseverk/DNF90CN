// 本文件定义角色链命令计划。
// dnfbridge 已有登录、选角和 USERINFO 专门链路；这里仅为 aligned fallback 记录意图和 owner 缺口。
package character

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// Command 是 character handler 交给角色 owner 或登录链路的最小命令计划。
type Command struct {
	Operation           string
	AccountID           string
	SelectedCharacterID uint16
	SlotOrCharacterID   uint16
	Slot                byte
	Job                 byte
	Name                string
	RawLen              int
	NeedsOwner          string
}

// NewSelectCommand 构造选角命令计划。
func NewSelectCommand(req alignedcmd.Request, parsed SelectCharacterRequest) Command {
	cmd := newRawCommand(req, "select_character", "login/character owner + op4/op37/scene bootstrap order")
	cmd.SlotOrCharacterID = parsed.SlotOrCharacterID
	return cmd
}

// NewCreateCommand 构造创角命令计划。
func NewCreateCommand(req alignedcmd.Request, parsed CreateCharacterRequest) Command {
	cmd := newRawCommand(req, "create_character", "character owner + slot/name/job validation + roster op2 refresh order")
	cmd.Job = parsed.Job
	cmd.Name = parsed.Name
	return cmd
}

// NewDeleteCommand 构造删角命令计划。
func NewDeleteCommand(req alignedcmd.Request, parsed DeleteCharacterRequest) Command {
	cmd := newRawCommand(req, "delete_character", "character owner + delete guard + roster op2 refresh order")
	cmd.Slot = parsed.Slot
	cmd.Name = parsed.Name
	return cmd
}

// NewCheckNameCommand 构造角色名查重命令计划。
func NewCheckNameCommand(req alignedcmd.Request, parsed CheckNameRequest) Command {
	cmd := newRawCommand(req, "check_double_character_name", "character name index + current EXE response body evidence")
	cmd.Name = parsed.Name
	return cmd
}

// NewRawCommand 构造无包体或由专门链路处理的角色命令计划。
func NewRawCommand(req alignedcmd.Request, operation string, needs string) Command {
	return newRawCommand(req, operation, needs)
}

func newRawCommand(req alignedcmd.Request, operation string, needs string) Command {
	return Command{
		Operation:           operation,
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		RawLen:              len(req.Body),
		NeedsOwner:          needs,
	}
}

func (c Command) String() string {
	base := fmt.Sprintf("account=%q char=%d", c.AccountID, c.SelectedCharacterID)
	switch c.Operation {
	case "select_character":
		return fmt.Sprintf("%s slotOrChar=%d needs=%s", base, c.SlotOrCharacterID, c.NeedsOwner)
	case "create_character":
		return fmt.Sprintf("%s job=%d name=%q needs=%s", base, c.Job, c.Name, c.NeedsOwner)
	case "delete_character":
		return fmt.Sprintf("%s slot=%d name=%q needs=%s", base, c.Slot, c.Name, c.NeedsOwner)
	case "check_double_character_name":
		return fmt.Sprintf("%s name=%q needs=%s", base, c.Name, c.NeedsOwner)
	default:
		return fmt.Sprintf("%s rawLen=%d needs=%s", base, c.RawLen, c.NeedsOwner)
	}
}
