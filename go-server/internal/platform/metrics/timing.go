package metrics

import "time"

func (r *Registry) TimeStatsThreshold(name string, labels map[string]string, elapsed time.Duration, threshold time.Duration) {
	if r == nil || name == "" {
		return
	}
	if elapsed < 0 {
		elapsed = 0
	}
	base := JoinName(name)
	r.Inc(JoinName(base, "total"), labels)
	r.SetGauge(JoinName(base, "last_ns"), labels, elapsed.Nanoseconds())
	if threshold > 0 && elapsed >= threshold {
		r.Inc(JoinName(base, "slow_total"), labels)
	}
}

func (s Scope) TimeStatsThreshold(name string, labels map[string]string, elapsed time.Duration, threshold time.Duration) {
	if s.registry == nil {
		return
	}
	s.registry.TimeStatsThreshold(s.Name(name), s.Labels(labels), elapsed, threshold)
}
