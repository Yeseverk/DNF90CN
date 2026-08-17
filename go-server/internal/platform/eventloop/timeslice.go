package eventloop

import (
	"context"
	"time"
)

// SliceStep 是时间片调度每次推进时调用的业务步骤。
type SliceStep func(context.Context) (done bool, err error)

// SliceResult 描述一次时间片执行的处理数量、完成状态和错误。
type SliceResult struct {
	Processed      int
	Complete       bool
	BudgetExceeded bool
	Err            error
}

// RunSlice 在 maxItems 或 deadline 预算内反复执行 step。
func RunSlice(ctx context.Context, deadline time.Time, maxItems int, step SliceStep) SliceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if step == nil {
		return SliceResult{Err: ErrNoSliceStep}
	}
	if maxItems <= 0 && deadline.IsZero() {
		return SliceResult{Err: ErrInvalidSliceRun}
	}
	result := SliceResult{}
	// 协作式时间片只在 step 返回后检查预算；单个 step 不能长时间阻塞，否则 deadline 无法及时生效。
	for maxItems <= 0 || result.Processed < maxItems {
		if err := ctx.Err(); err != nil {
			result.Err = err
			return result
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			result.BudgetExceeded = true
			return result
		}
		done, err := step(ctx)
		result.Processed++
		if err != nil {
			result.Err = err
			return result
		}
		if done {
			result.Complete = true
			return result
		}
	}
	return result
}
