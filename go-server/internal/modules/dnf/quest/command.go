// 本文件定义任务命令计划。
// 计划只承接 C2S 解析后的任务意图，不直接修改任务状态、发奖励或刷新 USERINFO。
package quest

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// Command 是 quest handler 交给后续 owner 的最小命令计划。
// 完成任务可能发经验、金币、物品、职业状态或后续 NOTI，必须等 quest/reward owner 闭合后再回包。
type Command struct {
	Operation           string
	AccountID           string
	SelectedCharacterID uint16
	QuestID             uint16
	TriggerType         byte
	IsIncrement         bool
	RewardSelectIndex   uint16
	HasRewardSelect     bool
	Multiplier          uint16
	NeedsOwner          string
}

// NewQuestIDCommand 构造只携带 questId 的接受/放弃任务计划。
func NewQuestIDCommand(req alignedcmd.Request, operation string, parsed QuestIDRequest) Command {
	return Command{
		Operation:           operation,
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		QuestID:             parsed.QuestID,
		NeedsOwner:          "quest owner + state persistence + response order",
	}
}

// NewTriggerCommand 构造任务触发进度计划。
func NewTriggerCommand(req alignedcmd.Request, parsed SetTriggerRequest) Command {
	return Command{
		Operation:           "set_quest_trigger",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		QuestID:             parsed.QuestID,
		TriggerType:         parsed.TriggerType,
		IsIncrement:         parsed.IsIncrement,
		NeedsOwner:          "quest owner + progress idempotency + notify order",
	}
}

// NewFinishCommand 构造完成任务计划。
func NewFinishCommand(req alignedcmd.Request, parsed FinishQuestRequest) Command {
	return Command{
		Operation:           "finish_quest",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		QuestID:             parsed.QuestID,
		RewardSelectIndex:   parsed.RewardSelectIndex,
		HasRewardSelect:     parsed.HasRewardSelect,
		Multiplier:          parsed.Multiplier,
		NeedsOwner:          "quest owner + reward owner + mutation id + USERINFO/NOTI order",
	}
}

func (c Command) String() string {
	base := fmt.Sprintf("account=%q char=%d quest=%d", c.AccountID, c.SelectedCharacterID, c.QuestID)
	switch c.Operation {
	case "set_quest_trigger":
		return fmt.Sprintf("%s triggerType=%d increment=%t needs=%s", base, c.TriggerType, c.IsIncrement, c.NeedsOwner)
	case "finish_quest":
		return fmt.Sprintf("%s rewardSelect=%d hasReward=%t multiplier=%d needs=%s", base, c.RewardSelectIndex, c.HasRewardSelect, c.Multiplier, c.NeedsOwner)
	default:
		return fmt.Sprintf("%s needs=%s", base, c.NeedsOwner)
	}
}
