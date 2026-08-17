package alignedcmd

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

// PendingSpec 描述一个已识别但暂不允许回包的命令。
type PendingSpec struct {
	Operation     string
	MinBodyLen    int
	PendingReason string
}

// PendingHandler 是尚未闭合写包证据的业务域占位 handler。
type PendingHandler struct {
	domain dnfenum.AlignedDomain
	specs  map[dnfenum.CmdPacket]PendingSpec
}

// NewPendingHandler 创建一个只识别请求、不允许成功回包的业务域 handler。
func NewPendingHandler(domain dnfenum.AlignedDomain, specs map[dnfenum.CmdPacket]PendingSpec) PendingHandler {
	copied := make(map[dnfenum.CmdPacket]PendingSpec, len(specs))
	for op, spec := range specs {
		copied[op] = spec
	}
	return PendingHandler{domain: domain, specs: copied}
}

// Domain 返回业务域。
func (h PendingHandler) Domain() dnfenum.AlignedDomain {
	return h.domain
}

// Handle 只做请求识别，禁止 bridge 在证据不足时回成功包。
func (h PendingHandler) Handle(_ context.Context, req Request) (Result, error) {
	spec, ok := h.specs[dnfenum.CmdPacket(req.Opcode)]
	if !ok {
		return Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("%s 模块未登记该 opcode", h.domain),
		}, nil
	}
	if spec.MinBodyLen > 0 && len(req.Body) < spec.MinBodyLen {
		return Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       spec.Operation,
			Reason:          fmt.Sprintf("%s 包体过短：got %d want >= %d", spec.Operation, len(req.Body), spec.MinBodyLen),
		}, nil
	}
	return Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       spec.Operation,
		Reason:          spec.PendingReason,
	}, nil
}
