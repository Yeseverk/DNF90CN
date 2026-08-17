package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"longheng.io/server/internal/platform/config"
)

// StopDrainSnapshot 描述停服排空探针看到的剩余工作量。
type StopDrainSnapshot struct {
	Remaining int
	Detail    string
}

// StopDrainProber 由需要优雅排空的组件实现，用于停服策略轮询。
type StopDrainProber interface {
	StopDrainSnapshot(context.Context) (StopDrainSnapshot, error)
}

// StopDrainProbe 是停服排空探针函数，便于测试和轻量组件接入。
type StopDrainProbe func(context.Context) (StopDrainSnapshot, error)

// StopPolicy 定义停服排空的初始宽限、探测间隔和稳定窗口。
type StopPolicy struct {
	InitialGrace  time.Duration
	ProbeInterval time.Duration
	StableFor     time.Duration
}

// StopPolicyOutcome 表示停服排空策略最终落在哪一种结果上。
type StopPolicyOutcome string

const (
	// StopPolicyNoProbe 表示组件没有提供排空探针。
	StopPolicyNoProbe StopPolicyOutcome = "no_probe"
	// StopPolicyDrained 表示排空探针确认剩余工作已经归零。
	StopPolicyDrained StopPolicyOutcome = "drained"
	// StopPolicyStableAbandoned 表示剩余工作长时间稳定不变，允许继续停服。
	StopPolicyStableAbandoned StopPolicyOutcome = "stable_abandoned"
	// StopPolicyContextDone 表示停服上下文先于排空完成而结束。
	StopPolicyContextDone StopPolicyOutcome = "context_done"
	// StopPolicyProbeFailed 表示排空探针返回错误。
	StopPolicyProbeFailed StopPolicyOutcome = "probe_failed"
)

// StopPolicyResult 汇总停服排空策略的结果、探测次数和耗时。
type StopPolicyResult struct {
	Outcome   StopPolicyOutcome
	Remaining int
	Detail    string
	Probes    int
	Err       error
	Elapsed   time.Duration
}

func stopPolicyFromConfig(cfg config.ServiceConfig) StopPolicy {
	policy := StopPolicy{
		InitialGrace:  cfg.Service.StopInitialGrace(),
		ProbeInterval: cfg.Service.StopProbeInterval(),
		StableFor:     cfg.Service.StopStableWindow(),
	}
	if policy.ProbeInterval <= 0 {
		policy.ProbeInterval = time.Second
	}
	if policy.StableFor <= 0 {
		policy.StableFor = 5 * time.Second
	}
	return policy
}

// WaitStopDrain 按停服策略轮询排空探针，直到排空、稳定放弃或上下文结束。
func WaitStopDrain(ctx context.Context, policy StopPolicy, probe StopDrainProbe) StopPolicyResult {
	startedAt := time.Now()
	if ctx == nil {
		ctx = context.Background()
	}
	if probe == nil {
		return StopPolicyResult{Outcome: StopPolicyNoProbe, Elapsed: time.Since(startedAt)}
	}
	if policy.ProbeInterval <= 0 {
		policy.ProbeInterval = time.Second
	}
	if policy.StableFor <= 0 {
		policy.StableFor = 5 * time.Second
	}
	if policy.InitialGrace > 0 {
		if err := sleepContext(ctx, policy.InitialGrace); err != nil {
			return StopPolicyResult{Outcome: StopPolicyContextDone, Err: err, Elapsed: time.Since(startedAt)}
		}
	}

	var lastRemaining int
	var lastDetail string
	var stableSince time.Time
	for {
		now := time.Now()
		snapshot, err := probe(ctx)
		if err != nil {
			return StopPolicyResult{
				Outcome:   StopPolicyProbeFailed,
				Remaining: snapshot.Remaining,
				Detail:    snapshot.Detail,
				Probes:    1,
				Err:       err,
				Elapsed:   time.Since(startedAt),
			}
		}
		result := StopPolicyResult{
			Outcome:   StopPolicyDrained,
			Remaining: snapshot.Remaining,
			Detail:    snapshot.Detail,
			Probes:    1,
			Elapsed:   time.Since(startedAt),
		}
		if snapshot.Remaining <= 0 {
			return result
		}
		lastRemaining = snapshot.Remaining
		lastDetail = snapshot.Detail
		stableSince = now

		for {
			if err := sleepContext(ctx, policy.ProbeInterval); err != nil {
				return StopPolicyResult{
					Outcome:   StopPolicyContextDone,
					Remaining: lastRemaining,
					Detail:    lastDetail,
					Probes:    result.Probes,
					Err:       err,
					Elapsed:   time.Since(startedAt),
				}
			}
			now = time.Now()
			snapshot, err = probe(ctx)
			result.Probes++
			result.Elapsed = time.Since(startedAt)
			result.Remaining = snapshot.Remaining
			result.Detail = snapshot.Detail
			if err != nil {
				result.Outcome = StopPolicyProbeFailed
				result.Err = err
				return result
			}
			if snapshot.Remaining <= 0 {
				result.Outcome = StopPolicyDrained
				return result
			}
			if snapshot.Remaining != lastRemaining || snapshot.Detail != lastDetail {
				lastRemaining = snapshot.Remaining
				lastDetail = snapshot.Detail
				stableSince = now
				continue
			}
			if now.Sub(stableSince) >= policy.StableFor {
				result.Outcome = StopPolicyStableAbandoned
				return result
			}
		}
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (a *Application) waitStopDrain(ctx context.Context) StopPolicyResult {
	probe := a.stopDrainProbe()
	if probe == nil {
		return StopPolicyResult{Outcome: StopPolicyNoProbe}
	}
	return WaitStopDrain(ctx, stopPolicyFromConfig(a.configSnapshot()), probe)
}

func (a *Application) stopDrainProbe() StopDrainProbe {
	if a == nil {
		return nil
	}
	var probers []StopDrainProber
	for _, component := range a.components {
		if prober, ok := component.(StopDrainProber); ok {
			probers = append(probers, prober)
		}
	}
	if len(probers) == 0 {
		return nil
	}
	return func(ctx context.Context) (StopDrainSnapshot, error) {
		var remaining int
		var details []string
		var errs []error
		for _, prober := range probers {
			snapshot, err := prober.StopDrainSnapshot(ctx)
			if snapshot.Remaining > 0 {
				remaining += snapshot.Remaining
			}
			if snapshot.Detail != "" {
				details = append(details, snapshot.Detail)
			}
			if err != nil {
				errs = append(errs, err)
			}
		}
		var err error
		if len(errs) > 0 {
			err = errors.Join(errs...)
		}
		return StopDrainSnapshot{
			Remaining: remaining,
			Detail:    strings.Join(details, "; "),
		}, err
	}
}

func formatDrainDetail(result StopPolicyResult) string {
	if result.Detail == "" {
		return fmt.Sprintf("outcome=%s remaining=%d probes=%d", result.Outcome, result.Remaining, result.Probes)
	}
	return fmt.Sprintf("outcome=%s remaining=%d probes=%d detail=%s", result.Outcome, result.Remaining, result.Probes, result.Detail)
}
