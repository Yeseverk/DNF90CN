package hotupdate

import (
	"context"

	"longheng.io/server/internal/platform/datatable"
)

type DataTableApplier struct {
	Registry *datatable.Registry
	Views    *datatable.ViewManager
}

func (a DataTableApplier) Apply(ctx context.Context, pkg Package) (ApplyResult, error) {
	if a.Registry == nil {
		return ApplyResult{}, datatable.ErrRegistryRequired
	}
	options := datatable.ReloadOptions{}
	if a.Views != nil {
		options.Validators = append(options.Validators, a.Views)
	}
	loadResult, err := a.Registry.ReloadDir(ctx, pkg.Directory, pkg.Version, options)
	if err != nil {
		return ApplyResult{}, err
	}
	result := ApplyResult{
		Version: pkg.Version,
		Applied: []string{
			"data_tables",
		},
		Details: map[string]any{
			"data_tables": loadResult,
		},
	}
	if a.Views != nil {
		// 视图刷新使用 WithoutCancel，避免外层发布请求取消后留下“表已切、视图未刷”的半成品状态。
		viewResult, err := a.Views.Refresh(context.WithoutCancel(ctx), a.Registry, loadResult)
		if err != nil {
			return ApplyResult{}, err
		}
		result.Applied = append(result.Applied, "data_table_views")
		result.Details["data_table_views"] = viewResult
	}
	return result, nil
}
