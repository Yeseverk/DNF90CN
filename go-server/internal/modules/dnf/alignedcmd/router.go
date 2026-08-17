package alignedcmd

import "longheng.io/server/internal/modules/dnf/dnfenum"

// Action 表示命令进入模块化迁移后的处理决策。
type Action string

const (
	ActionPendingModule Action = "pending_module"
	ActionBlocked       Action = "blocked_by_evidence"
)

// Decision 是 dnfbridge 分流层需要的最小决策结果。
type Decision struct {
	Opcode   uint16
	Domain   dnfenum.AlignedDomain
	Support  dnfenum.AlignedSupport
	Action   Action
	Evidence string
	Note     string
	Reason   string
}

// Classify 根据当前 EXE 证据把命令归入模块化队列或阻断旧映射。
func Classify(opcode uint16) (Decision, bool) {
	if aligned, ok := dnfenum.LookupAlignedCommand(opcode); ok {
		return Decision{
			Opcode:   opcode,
			Domain:   aligned.Domain,
			Support:  aligned.Support,
			Action:   ActionPendingModule,
			Evidence: aligned.Evidence,
			Note:     aligned.Note,
		}, true
	}
	if blocked, ok := dnfenum.LookupBlockedMigration(opcode); ok {
		return Decision{
			Opcode:   opcode,
			Domain:   blocked.Domain,
			Action:   ActionBlocked,
			Evidence: blocked.Evidence,
			Reason:   blocked.Reason,
		}, true
	}
	return Decision{}, false
}
