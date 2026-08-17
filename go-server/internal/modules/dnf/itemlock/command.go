// 本文件定义物品锁命令计划。
// 计划只承接协议解析后的锁定意图，不直接修改物品锁状态或发送 NOTI。
package itemlock

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// Command 是物品锁 handler 交给后续 owner 的最小命令计划。
// 物品锁会影响出售、移动、分解等关键写路径，必须由背包/装备 owner 持久化后再回包。
type Command struct {
	Operation           string
	AccountID           string
	SelectedCharacterID uint16
	ListType            byte
	SlotIndex           int16
	NeedsOwner          string
}

// NewCommand 构造物品锁命令计划。
// 当前只用于日志和后续 owner 接入点，不写角色背包或装备状态。
func NewCommand(req alignedcmd.Request, operation string, lock Request) Command {
	return Command{
		Operation:           operation,
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		ListType:            lock.ListType,
		SlotIndex:           lock.SlotIndex,
		NeedsOwner:          "inventory/equipment owner + lock-state persistence + NOTI order",
	}
}

func (c Command) String() string {
	return fmt.Sprintf("account=%q char=%d list=%d slot=%d needs=%s", c.AccountID, c.SelectedCharacterID, c.ListType, c.SlotIndex, c.NeedsOwner)
}
