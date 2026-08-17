// Package game 装配演示游戏框架的 DNF logic 组件。
// 该子包隔离 internal/services/logic 演示框架依赖，
// 生产二进制（cmd/server/dnf90 经 internal/app/dnf90）只装配仓储底座，
// 不引用本包，演示框架因此留在生产依赖闭包之外。
package game

import (
	"os"
	"strings"

	appkit "longheng.io/server/internal/platform/app"
	logicsvc "longheng.io/server/internal/services/logic"
	logicdnf "longheng.io/server/internal/services/logic/dnf"
)

// Components 按外部配置构建 DNF logic 服务组件列表。
// 仓储组件排在 logic service 前启动，后续 DNF owner 可从 Runtime 读取 repository.Group。
func Components(env *appkit.Env) ([]appkit.Component, error) {
	path := strings.TrimSpace(os.Getenv(logicdnf.EnvConfigPath))
	if path == "" {
		path = logicdnf.DefaultConfigPath
	}
	cfg, err := logicdnf.LoadConfigForEnv(path, env)
	if err != nil {
		return nil, err
	}
	return ComponentsWithConfig(env, cfg)
}

// ComponentsWithConfig 使用已解析配置构建 DNF logic 服务组件列表。
// 该入口用于测试和项目侧自定义配置装配。
func ComponentsWithConfig(env *appkit.Env, cfg logicdnf.Config) ([]appkit.Component, error) {
	runtime, err := logicdnf.NewRuntime(env, cfg)
	if err != nil {
		return nil, err
	}
	components := make([]appkit.Component, 0, 2)
	if runtime.Repository != nil {
		components = append(components, runtime.Repository)
	}
	components = append(components, logicsvc.NewWithOptions(env, logicsvc.Options{
		Game: func(e *appkit.Env) (logicsvc.GameManifest, error) {
			return GameManifest(runtime, e)
		},
	}))
	return components, nil
}

// GameManifest 返回当前 DNF logic 的业务 manifest。
// 本阶段只接入仓储底座，协议 handler 和 owner 后续在这里继续挂接。
func GameManifest(_ *logicdnf.Runtime, env *appkit.Env) (logicsvc.GameManifest, error) {
	return logicsvc.BlankManifest(env)
}
