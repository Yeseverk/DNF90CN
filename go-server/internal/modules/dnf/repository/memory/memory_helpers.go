// 本文件提供 DNF 内存仓储的私有通用 helper（与 mysql 实现相互独立）。
package memory

import (
	"context"
	"time"
)

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func timeOrNow(value time.Time, now func() time.Time) time.Time {
	if !value.IsZero() {
		return value.UTC()
	}
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}
