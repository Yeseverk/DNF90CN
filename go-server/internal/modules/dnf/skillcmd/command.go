// 本文件定义技能命令计划。
// 计划只记录技能协议解析后的意图，不直接修改技能等级、SP/TP 或技能指令配置。
package skillcmd

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// Command 是 skillcmd handler 交给后续 skill owner 的最小命令计划。
// 技能学习、退还和指令配置会影响战斗表现，必须由 skill owner 持久化并按 EXE 证据刷新后再回包。
type Command struct {
	Operation           string
	AccountID           string
	SelectedCharacterID uint16
	SkillTree           byte
	From                byte
	To                  byte
	ContextIndex        int32
	Mode                byte
	FinalMode           byte
	DeclaredCount       byte
	EntryCount          int
	RefundCount         int
	SkillIDs            []int64
	BuyEntries          []BuySkillEntry
	RecordCount         int
	NeedsOwner          string
}

// NewSlotCommand 构造技能栏互换计划。
func NewSlotCommand(req alignedcmd.Request, parsed ChangeSkillSlotRequest) Command {
	return Command{
		Operation:           "change_skill_slot",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		SkillTree:           parsed.SkillTree,
		From:                parsed.From,
		To:                  parsed.To,
		ContextIndex:        parsed.ContextIndex,
		Mode:                parsed.Mode,
		NeedsOwner:          "skill owner + bar persistence + NOTI 0x13 order",
	}
}

// NewBuyCommand 构造学习或退还技能计划。
func NewBuyCommand(req alignedcmd.Request, parsed BuySkillRequest) Command {
	refunds := 0
	skillIDs := make([]int64, 0, len(parsed.Entries))
	entries := make([]BuySkillEntry, len(parsed.Entries))
	copy(entries, parsed.Entries)
	for _, entry := range parsed.Entries {
		skillIDs = append(skillIDs, int64(entry.SkillID))
		if entry.RefundFlag != 0 {
			refunds++
		}
	}
	return Command{
		Operation:           "buy_skill",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		SkillTree:           parsed.RawSkillTree,
		FinalMode:           parsed.FinalMode,
		DeclaredCount:       parsed.Count,
		EntryCount:          len(parsed.Entries),
		RefundCount:         refunds,
		SkillIDs:            skillIDs,
		BuyEntries:          entries,
		NeedsOwner:          "skill owner + SP/TP validation + skill-state persistence + NOTI 0x13 order",
	}
}

// NewResetCommand constructs the persisted reset requested by the current
// Skill(K) initialize confirmation dialog.
func NewResetCommand(req alignedcmd.Request, parsed SkillInitRequest) Command {
	return Command{
		Operation:           "skill_init",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		SkillTree:           parsed.SkillTree,
		Mode:                parsed.Mode,
		NeedsOwner:          "skill owner + PVF initial levels + full SP/TP refund + layout persistence",
	}
}

// NewTreeCommand 构造切换技能树计划。
func NewTreeCommand(req alignedcmd.Request, parsed ChangeAnotherSkillTreeRequest) Command {
	return Command{
		Operation:           "change_another_skill_tree",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		SkillTree:           parsed.SkillTree,
		NeedsOwner:          "skill owner + active tree persistence + op260 ACK order",
	}
}

// NewSkillCommand 构造技能指令自定义计划。
func NewSkillCommand(req alignedcmd.Request, parsed SkillCommandRequest) Command {
	return Command{
		Operation:           "skill_command_customizing",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		SkillTree:           parsed.SkillTree,
		RecordCount:         len(parsed.Records),
		NeedsOwner:          "skill owner + command persistence + NOTI 0x13 order",
	}
}

// NewSimpleCommand 构造无包体或包体未闭合命令计划。
func NewSimpleCommand(req alignedcmd.Request, operation string, needs string) Command {
	return Command{
		Operation:           operation,
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		NeedsOwner:          needs,
	}
}

func (c Command) String() string {
	base := fmt.Sprintf("account=%q char=%d", c.AccountID, c.SelectedCharacterID)
	switch c.Operation {
	case "change_skill_slot":
		return fmt.Sprintf("%s tree=%d from=%d to=%d context=%d mode=%d needs=%s", base, c.SkillTree, c.From, c.To, c.ContextIndex, c.Mode, c.NeedsOwner)
	case "buy_skill":
		return fmt.Sprintf("%s tree=%d declared=%d entries=%d refunds=%d mode=%d needs=%s", base, c.SkillTree, c.DeclaredCount, c.EntryCount, c.RefundCount, c.FinalMode, c.NeedsOwner)
	case "skill_init":
		return fmt.Sprintf("%s tree=%d mode=%d needs=%s", base, c.SkillTree, c.Mode, c.NeedsOwner)
	case "change_another_skill_tree":
		return fmt.Sprintf("%s tree=%d needs=%s", base, c.SkillTree, c.NeedsOwner)
	case "skill_command_customizing":
		return fmt.Sprintf("%s tree=%d records=%d needs=%s", base, c.SkillTree, c.RecordCount, c.NeedsOwner)
	default:
		return fmt.Sprintf("%s needs=%s", base, c.NeedsOwner)
	}
}
