package metrics

import "math"

// Int64FromUint64 将 uint64 饱和转换为 int64，避免监控计数器溢出后回绕成负数。
func Int64FromUint64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value) //nolint:gosec // G115：前面已按 int64 最大值钳制。
}
