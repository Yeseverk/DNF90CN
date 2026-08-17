package logic

import (
	"errors"
	"time"

	appkit "longheng.io/server/internal/platform/app"
	"longheng.io/server/internal/platform/dispatch"
	"longheng.io/server/internal/platform/metrics"
	modulemgr "longheng.io/server/internal/platform/module"
	"longheng.io/server/internal/platform/playerloop"
	"longheng.io/server/internal/platform/readmodel"
	"longheng.io/server/internal/platform/runtimeguard"
)

type logicServiceWiring struct {
	game           GameManifest
	gameErr        error
	metrics        *metrics.Registry
	modules        *modulemgr.Manager
	accounts       *AccountManager
	runtimeProfile runtimeguard.Profile
}

func newLogicWiring(env *appkit.Env, options Options) logicServiceWiring {
	if env == nil {
		env = &appkit.Env{}
	}
	core := env.Core()
	gameProvider := options.Game
	if gameProvider == nil {
		gameProvider = DefaultGameManifest
	}
	game, gameErr := gameProvider(env)
	metricsRegistry := core.Metrics
	if metricsRegistry == nil {
		metricsRegistry = metrics.New()
	}
	profile := runtimeguard.ProfileFromEnvironment(core.Config.Service.Environment, core.Config.Topology.Enabled)
	manager := modulemgr.New("logic-modules", core.Logger, core.Health)
	manager.SetRuntimeGuardProfile(profile)
	for _, mod := range game.Modules {
		if mod != nil {
			manager.Add(mod)
		}
	}
	accounts := game.Accounts
	if accounts == nil && game.Players != nil {
		accounts = newAccountManager(game.Players, core.Logger, AccountManagerOptions{})
	}
	return logicServiceWiring{
		game:           game,
		gameErr:        gameErr,
		metrics:        metricsRegistry,
		modules:        manager,
		accounts:       accounts,
		runtimeProfile: profile,
	}
}

func (s *Service) wireLogicRuntime(env *appkit.Env, game GameManifest) {
	if env == nil {
		env = &appkit.Env{}
	}
	core := env.Core()
	if game.RegisterReadModels != nil {
		game.RegisterReadModels(s.readModels)
	} else if s.players != nil {
		s.regReadModelAdmins()
	}
	dispatcher, err := s.newLogicDispatcher()
	if err != nil {
		s.initErr = errors.Join(s.initErr, err)
		dispatcher = dispatch.New()
	}
	s.dispatcher = dispatcher
	if env.Cluster != nil {
		env.Cluster.SetRoutes(routesFromHandlers(dispatcher.Snapshot()))
	}
	s.playerLoops = playerloop.NewWithOptions("lg-loops", 128, s.handlePlayerEvent, core.Logger, playerloop.Options{
		IdleTTL:        10 * time.Minute,
		SweepInterval:  time.Minute,
		HandlerTimeout: logicHandlerTimeout,
	})
	s.regRuntimeMetrics()
}

func newLogicReadModels() *readmodel.AdminRegistry {
	return readmodel.NewAdminRegistry()
}
