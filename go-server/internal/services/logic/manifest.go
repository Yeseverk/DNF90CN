package logic

import (
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/samples/world"
	appkit "longheng.io/server/internal/platform/app"
	"longheng.io/server/internal/platform/dispatch"
	"longheng.io/server/internal/platform/integrity"
	modulemgr "longheng.io/server/internal/platform/module"
	"longheng.io/server/internal/platform/notification"
	"longheng.io/server/internal/platform/readmodel"
	"longheng.io/server/internal/platform/runtimeguard"
	"longheng.io/server/internal/platform/scripthost"
	"longheng.io/server/internal/platform/secrets"
	"longheng.io/server/internal/reference/player"
	"longheng.io/server/internal/runtime/leaderboard"
	"longheng.io/server/internal/runtime/moderation"
	"longheng.io/server/internal/runtime/notice"
	"longheng.io/server/internal/runtime/redeem"
	logichandlers "longheng.io/server/internal/services/logic/handlers"
)

// ErrManifestMemStore 表示默认 manifest 在分布式环境仍使用单节点内存后端。
var ErrManifestMemStore = errors.New("logic default manifest uses single-node runtime backend")

// GameManifestProvider 根据框架环境构建游戏 manifest。
type GameManifestProvider func(*appkit.Env) (GameManifest, error)

// ExtensionPoints 是项目侧接入平台扩展包的 wiring 表面。
// 它刻意放在 GameManifest 中，让项目能挂接具体 provider，
// 同时避免 platform 包反向 import services 或游戏专属 runtime 代码。
type ExtensionPoints struct {
	ScriptHost        scripthost.Host
	ScriptRuntime     scripthost.Runtime
	Secrets           secrets.Provider
	Push              notification.PushProvider
	Mail              notification.MailProvider
	Integrity         integrity.ActionValidator
	ReplayDigest      integrity.ReplayDigestProvider
	RiskScoreProvider integrity.RiskScoreProvider
}

// GameManifest 描述 logic/playerd 要装配的模块、运行时和 handler。
type GameManifest struct {
	Modules            []modulemgr.Module
	Players            *player.Module
	World              *world.Module
	Accounts           *AccountManager
	Leaderboards       leaderboard.Runtime
	Sanctions          moderation.SanctionStore
	Notices            notice.Service
	Redeems            redeem.Service
	AdminRoutes        []AdminRouteRegistrar
	RegisterHandlers   func(*dispatch.Mux) error
	RegisterReadModels func(*readmodel.AdminRegistry)
	Extensions         ExtensionPoints
}

// BlankManifest 返回不装配业务模块的 GameManifest。
// 新项目应以它为起点，通过 Modules / Players / RegisterHandlers 注册自己的
// player、world 和 runtime 类型。
func BlankManifest(_ *appkit.Env) (GameManifest, error) {
	return GameManifest{}, nil
}

// DemoGameManifest 装配内置示例模块用于本地演示。
// 即使本地配置为了 route metadata smoke 保持 topology 开启，示例 runtime 也按单节点处理。
func DemoGameManifest(env *appkit.Env) (GameManifest, error) {
	if env == nil {
		env = &appkit.Env{}
	}
	demoEnv := *env
	demoEnv.Config.Topology.Enabled = false
	return DefaultGameManifest(&demoEnv)
}

// DefaultPlayerDManifest 是 playerd 的参考 manifest。
// 它只装配示例 Player/Profile 模块，不继承仅属于 logic 的 world、leaderboard、notice 或 redeem 示例 runtime。
func DefaultPlayerDManifest(env *appkit.Env) (GameManifest, error) {
	profileStore, summaryStore := newPlayerStores(env.Config.ProfileStore, env)
	playerModule, err := player.NewWithStoresChecked(env.Logger, profileStore, summaryStore)
	if err != nil {
		return GameManifest{}, err
	}
	if env.EventLog != nil {
		playerModule.UseEventLog(env.EventLog, env.Config.EventLog.Strict)
	}
	return GameManifest{
		Modules: []modulemgr.Module{
			playerModule,
		},
		Players: playerModule,
		RegisterHandlers: func(mux *dispatch.Mux) error {
			return logichandlers.RegisterAll(mux, logichandlers.Default(logichandlers.Context{Players: playerModule})...)
		},
	}, nil
}

// DefaultGameManifest 创建内置演示用 logic manifest。
func DefaultGameManifest(env *appkit.Env) (GameManifest, error) {
	profileStore, summaryStore := newPlayerStores(env.Config.ProfileStore, env)
	playerModule, err := player.NewWithStoresChecked(env.Logger, profileStore, summaryStore)
	if err != nil {
		return GameManifest{}, err
	}
	if env.EventLog != nil {
		playerModule.UseEventLog(env.EventLog, env.Config.EventLog.Strict)
	}
	worldModule := world.New(env.Logger)
	leaderboards := leaderboard.New(leaderboard.Options{Name: env.Config.Service.Name + "-leaderboards"})
	sanctions := moderation.NewMemorySanctionStore()
	profile := runtimeguard.ProfileFromEnvironment(env.Config.Service.Environment, env.Config.Topology.Enabled)
	notices := notice.Service{
		Store:    notice.NewMemoryStore(),
		EventLog: env.EventLog,
	}
	if env.Bus != nil {
		publishers := []notice.LivePublisher{notice.NewBusPublisher(env.Bus)}
		if env.OnlinePush != nil {
			publishers = append(publishers, notice.NewOnlinePushPublisher(env.OnlinePush))
		} else {
			publishers = append(publishers, notice.NewGatewayPushPublisher(env.Bus))
		}
		notices.Publisher = notice.NewAsyncLivePublisher(notice.NewFanoutPublisher(publishers...))
	}
	redeems := redeem.Service{
		Store:    redeem.NewMemoryStore(),
		EventLog: env.EventLog,
	}
	manifest := GameManifest{
		Modules: []modulemgr.Module{
			playerModule,
			worldModule,
		},
		Players:      playerModule,
		World:        worldModule,
		Leaderboards: leaderboards,
		Sanctions:    sanctions,
		Notices:      notices,
		Redeems:      redeems,
		AdminRoutes: []AdminRouteRegistrar{
			NoticeAdminRouteRegistrar(notices),
			RedeemAdminRouteRegistrar(redeems),
		},
		RegisterHandlers: func(mux *dispatch.Mux) error {
			return logichandlers.RegisterAll(mux, logichandlers.Default(logichandlers.Context{Players: playerModule})...)
		},
	}
	if profile.Distributed {
		return manifest, fmt.Errorf("%w: notice and redeem default stores are memory-only; provide a project manifest with durable stores",
			ErrManifestMemStore)
	}
	return manifest, nil
}
