package metrics

import "runtime"

func ObserveRuntime(registry *Registry) {
	if registry == nil {
		return
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	registry.SetGauge("go_goroutines", nil, int64(runtime.NumGoroutine()))
	registry.SetGauge("go_heap_alloc_bytes", nil, Int64FromUint64(stats.HeapAlloc))
	registry.SetGauge("go_heap_inuse_bytes", nil, Int64FromUint64(stats.HeapInuse))
	registry.SetGauge("go_heap_objects", nil, Int64FromUint64(stats.HeapObjects))
	registry.SetGauge("go_gc_next_bytes", nil, Int64FromUint64(stats.NextGC))
	registry.SetGauge("go_gc_cycles_total", nil, int64(stats.NumGC))
	registry.SetGauge("go_gc_pause_total_ns", nil, Int64FromUint64(stats.PauseTotalNs))
	registry.SetGauge("go_gc_last_pause_ns", nil, Int64FromUint64(lastPauseNS(stats)))
}

func lastPauseNS(stats runtime.MemStats) uint64 {
	if stats.NumGC == 0 {
		return 0
	}
	return stats.PauseNs[(stats.NumGC+255)%256]
}
