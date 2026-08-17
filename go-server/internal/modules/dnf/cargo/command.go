// 本文件定义账号仓库命令计划。
// 计划只记录协议解析后的业务意图，不直接修改金币、仓库或角色状态。
package cargo

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// MoneyDirection 表示金币在角色钱包和账号仓库之间的流向。
type MoneyDirection string

const (
	MoneyDeposit  MoneyDirection = "deposit"
	MoneyWithdraw MoneyDirection = "withdraw"
)

// Command 是 cargo handler 交给后续 owner 的最小命令计划。
// 金币属于关键资产；在 wallet/account-cargo owner 和幂等事务落地前，该计划不能直接变成成功回包。
type Command struct {
	Operation           string
	AccountID           string
	SelectedCharacterID uint16
	MoneyDirection      MoneyDirection
	Amount              int64
	NeedsOwner          string
}

// NewAccountCommand 构造账号仓库创建或升级命令计划。
// 当前只用于日志和后续 owner 接入点，不写仓库扩展状态。
func NewAccountCommand(req alignedcmd.Request, operation string) Command {
	return Command{
		Operation:           operation,
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		NeedsOwner:          "account-cargo owner + durable idempotency",
	}
}

// NewMoneyCommand 构造金币存取命令计划。
// 它只消费已经解析出的金额，真正扣减/增加必须由可靠 owner 在一个事务语义中完成。
func NewMoneyCommand(req alignedcmd.Request, direction MoneyDirection, money GoldRequest) Command {
	return Command{
		Operation:           string(direction) + "_money",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		MoneyDirection:      direction,
		Amount:              int64(money.Amount),
		NeedsOwner:          "wallet owner + account-cargo owner + mutation id",
	}
}

func (c Command) String() string {
	if c.Amount > 0 {
		return fmt.Sprintf("account=%q char=%d direction=%s amount=%d needs=%s", c.AccountID, c.SelectedCharacterID, c.MoneyDirection, c.Amount, c.NeedsOwner)
	}
	return fmt.Sprintf("account=%q char=%d needs=%s", c.AccountID, c.SelectedCharacterID, c.NeedsOwner)
}
