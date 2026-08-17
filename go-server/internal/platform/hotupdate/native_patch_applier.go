package hotupdate

import (
	"context"
	"time"

	"longheng.io/server/internal/platform/nativepatch"
)

type NativePatchApplier struct {
	Engine nativepatch.Engine
	Policy nativepatch.Policy
}

func (a NativePatchApplier) Apply(ctx context.Context, pkg Package) (ApplyResult, error) {
	// Native patch applier 只处理已验签、已解包的补丁目录；函数级替换策略由 nativepatch.Policy 再校验一次。
	engine := a.engine()
	nativePkg, err := nativepatch.LoadPackage(ctx, pkg.Directory)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := a.Policy.Validate(nativePkg.Plan); err != nil {
		return ApplyResult{}, err
	}
	result, err := engine.Apply(ctx, nativePkg)
	if err != nil {
		return ApplyResult{}, err
	}
	return patchApplyResult("native_patch", result.Version, result.Target, result.Applied, result.StartedAt, result.EndedAt), nil
}

func (a NativePatchApplier) Restore(ctx context.Context, intent Intent) (ApplyResult, error) {
	// Restore 走 engine 的快照恢复，不重新加载包；失败时必须保留错误给控制面和 release gate。
	engine := a.engine()
	result, err := engine.Restore(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	version := intent.Version
	if version == "" {
		version = result.Version
	}
	return patchApplyResult("native_patch_restore", version, result.Target, result.Applied, result.StartedAt, result.EndedAt), nil
}

func (a NativePatchApplier) engine() nativepatch.Engine {
	if a.Engine != nil {
		return a.Engine
	}
	return nativepatch.NewNativeEngine()
}

func (a NativePatchApplier) PatchLiveDuration() time.Duration {
	// MaxLiveDuration 是原生补丁自动恢复窗口，0 表示不自动恢复，生产不建议关闭。
	return a.Policy.MaxLiveDuration
}

func patchApplyResult(name, version, target string, symbols int, startedAt, endedAt time.Time) ApplyResult {
	return ApplyResult{
		Version: version,
		Applied: []string{
			name,
		},
		Details: map[string]any{
			"target":     target,
			"symbols":    symbols,
			"started_at": startedAt,
			"ended_at":   endedAt,
		},
	}
}
