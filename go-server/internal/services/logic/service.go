package logic

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/modules/samples/world"
	"longheng.io/server/internal/platform/admincmdqueue"
	appkit "longheng.io/server/internal/platform/app"
	"longheng.io/server/internal/platform/bus"
	"longheng.io/server/internal/platform/db"
	"longheng.io/server/internal/platform/dispatch"
	"longheng.io/server/internal/platform/eventlog"
	"longheng.io/server/internal/platform/eventloop"
	"longheng.io/server/internal/platform/health"
	"longheng.io/server/internal/platform/idempotency"
	"longheng.io/server/internal/platform/metrics"
	modulemgr "longheng.io/server/internal/platform/module"
	"longheng.io/server/internal/platform/playerloop"
	"longheng.io/server/internal/platform/readmodel"
	"longheng.io/server/internal/platform/runtimeguard"
	"longheng.io/server/internal/platform/workerpool"
	"longheng.io/server/internal/reference/player"
	"longheng.io/server/internal/runtime/leaderboard"
	"longheng.io/server/internal/runtime/moderation"
	"longheng.io/server/internal/runtime/notice"
	"longheng.io/server/internal/runtime/redeem"
	"longheng.io/server/pkg/contracts"
)

// Service 是 logic 服务入口，负责调度、玩家队列、响应缓存和运行时模块装配。
type Service struct {
	nodeID             string
	componentName      string
	adminPrefix        string
	metricServiceLabel string
	startedService     string
	environment        string
	logger             *slog.Logger
	bus                bus.Bus
	workers            *workerpool.Pool
	health             *health.Service
	metrics            *metrics.Registry

	modules          *modulemgr.Manager
	players          *player.Module
	readModels       *readmodel.AdminRegistry
	accounts         *AccountManager
	world            *world.Module
	leaderboards     leaderboard.Runtime
	sanctions        moderation.SanctionStore
	notices          notice.Service
	redeems          redeem.Service
	extensions       ExtensionPoints
	runtimeProfile   runtimeguard.Profile
	eventLog         *eventlog.Log
	adminCommands    *admincmdqueue.Executor
	dispatcher       *dispatch.Mux
	metricEventLoop  *eventloop.Loop
	adminRoutes      []AdminRouteRegistrar
	registerHandlers func(*dispatch.Mux) error
	idempotency      *idempotency.Guard
	responses        logicResponseCache
	responsesOut     *respPublisher
	eventOutMu       sync.Mutex
	eventOut         *eventPub
	playerLoops      *playerloop.Manager
	subsMu           sync.Mutex
	subs             []bus.Subscription
	admin            func(http.HandlerFunc) http.HandlerFunc
	adminDangerous   func(string, http.HandlerFunc) http.HandlerFunc
	initErr          error

	sessionMu         sync.Mutex
	activeSessions    map[string]string
	closedSessions    map[string]struct{}
	handlerMetricMu   sync.Mutex
	handlerLatencyMax map[string]int64
}

// Options 是 logic 服务创建时的可选覆盖项。
type Options struct {
	ComponentName      string
	AdminPrefix        string
	MetricServiceLabel string
	StartedService     string
	Game               GameManifestProvider
}

type runtimeStoreCloser interface {
	Close() error
}

// New 使用默认配置创建 logic 服务。
func New(env *appkit.Env) *Service {
	return NewWithOptions(env, Options{})
}

// NewPlayerD 创建 playerd 服务实例。
func NewPlayerD(env *appkit.Env) *Service {
	return NewPlayerDWithManifest(env, DefaultPlayerDManifest)
}

// NewPlayerDWithManifest 使用指定 manifest 创建 playerd 服务实例。
func NewPlayerDWithManifest(env *appkit.Env, game GameManifestProvider) *Service {
	if game == nil {
		game = DefaultPlayerDManifest
	}
	return NewWithOptions(env, Options{
		ComponentName:      "playerd-service",
		AdminPrefix:        "/playerd",
		MetricServiceLabel: "playerd",
		StartedService:     "playerd",
		Game:               game,
	})
}

// NewWithOptions 使用指定 manifest 和选项创建 logic 服务。
func NewWithOptions(env *appkit.Env, options Options) *Service {
	if env == nil {
		env = &appkit.Env{}
	}
	options = normalizeOptions(options)
	// NewWithOptions 是 logic/playerd 的统一装配入口：平台依赖来自 Env，玩法侧能力来自 GameManifest。
	// 默认 logic 使用空玩法，真实项目应显式传入自己的 manifest，避免把示例模块误认为平台核心。
	wiring := newLogicWiring(env, options)
	game := wiring.game
	core := env.Core()
	realtime := env.Realtime()
	persistence := env.Persistence()
	admin := env.AdminRuntime()

	s := &Service{
		nodeID:             core.Config.Service.NodeID,
		componentName:      options.ComponentName,
		adminPrefix:        options.AdminPrefix,
		metricServiceLabel: options.MetricServiceLabel,
		startedService:     options.StartedService,
		environment:        core.Config.Service.Environment,
		logger:             core.Logger,
		bus:                realtime.Bus,
		workers:            realtime.Workers,
		health:             core.Health,
		metrics:            wiring.metrics,
		modules:            wiring.modules,
		players:            game.Players,
		readModels:         newLogicReadModels(),
		accounts:           wiring.accounts,
		world:              game.World,
		leaderboards:       game.Leaderboards,
		sanctions:          game.Sanctions,
		notices:            game.Notices,
		redeems:            game.Redeems,
		extensions:         game.Extensions,
		runtimeProfile:     wiring.runtimeProfile,
		eventLog:           persistence.EventLog,
		adminCommands:      admin.Commands,
		adminRoutes:        append([]AdminRouteRegistrar(nil), game.AdminRoutes...),
		registerHandlers:   game.RegisterHandlers,
		idempotency:        newIdempotencyGuard(core.Config.Idempotency),
		responses:          newLogicRespCache(core.Config.Idempotency),
		responsesOut:       newRespPublisher(realtime.Bus, core.Logger),
		eventOut:           newEventPub(realtime.Bus, core.Logger),
		activeSessions:     make(map[string]string),
		closedSessions:     make(map[string]struct{}),
		admin:              logicAdminWrapper(env, options.MetricServiceLabel),
		adminDangerous:     logicAdminGuard(env, options.MetricServiceLabel),
		initErr:            wiring.gameErr,
	}
	s.wireLogicRuntime(env, game)
	s.registerRoutes(env.AdminMux)
	return s
}

// ExtensionPoints 返回项目侧可挂接的平台扩展点。
func (s *Service) ExtensionPoints() ExtensionPoints {
	if s == nil {
		return ExtensionPoints{}
	}
	return s.extensions
}

func normalizeOptions(options Options) Options {
	if options.ComponentName == "" {
		options.ComponentName = "logic-service"
	}
	if options.AdminPrefix == "" {
		options.AdminPrefix = "/logic"
	}
	options.AdminPrefix = "/" + strings.Trim(strings.TrimSpace(options.AdminPrefix), "/")
	if options.MetricServiceLabel == "" {
		options.MetricServiceLabel = "logic"
	}
	if options.StartedService == "" {
		options.StartedService = "logic"
	}
	return options
}

// Name 返回 logic 服务生命周期组件名。
func (s *Service) Name() string {
	return s.componentName
}

// Preflight 校验 logic 服务启动前所需依赖。
func (s *Service) Preflight(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.initErr != nil {
		return s.initErr
	}
	if err := runtimeguard.Verify(s.runtimeProfile); err != nil {
		return err
	}
	if err := s.checkStoreBackend("notice", s.notices.Store); err != nil {
		return err
	}
	if err := s.checkStoreBackend("redeem", s.redeems.Store); err != nil {
		return err
	}
	return s.modules.Preflight(ctx)
}

func (s *Service) checkStoreBackend(name string, store any) error {
	if store == nil {
		return nil
	}
	describer, ok := store.(runtimeguard.BackendDescriber)
	if !ok {
		return nil
	}
	return runtimeguard.CheckDescriber(name, describer, s.runtimeProfile)
}

func (s *Service) gatewayPacketTopic() string {
	if topic := contracts.LogicNodePacketTopic(s.nodeID); topic != "" {
		return topic
	}
	return contracts.TopicGatewayClientPacket
}

func (s *Service) sessionOnTopic() string {
	if topic := contracts.LogicSessConnTopic(s.nodeID); topic != "" {
		return topic
	}
	return contracts.TopicSessionConnected
}

func (s *Service) sessionOffTopic() string {
	if topic := contracts.LogicSessDiscTopic(s.nodeID); topic != "" {
		return topic
	}
	return contracts.TopicSessionDisconnected
}

func (s *Service) acceptsLogicTarget(target string) bool {
	target = strings.TrimSpace(target)
	return target == "" || s.nodeID == "" || target == s.nodeID
}

// Start 启动 logic 订阅、玩家队列、运行时模块和后台组件。
func (s *Service) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.Preflight(ctx); err != nil {
		return err
	}
	s.resetSessionTracking()
	if err := s.modules.Start(ctx); err != nil {
		return err
	}
	if err := s.playerLoops.Start(ctx); err != nil {
		_ = s.modules.Stop(ctx)
		return err
	}
	if s.accounts != nil {
		if err := s.accounts.Start(ctx); err != nil {
			_ = s.playerLoops.Stop(ctx)
			_ = s.modules.Stop(ctx)
			return err
		}
	}

	// Start 的订阅按依赖顺序建立，任一订阅失败都会回滚已启动的模块和玩家队列，避免半启动服务接收流量。
	stateSub, err := s.bus.Subscribe(contracts.TopicPlayerStateChange, func(_ context.Context, env bus.Envelope) error {
		if evt, ok := env.Payload.(contracts.PlayerStateChanged); ok {
			if s.world != nil {
				s.world.ObservePlayer(evt.AccountID, evt.State)
			}
		}
		return nil
	})
	if err != nil {
		if s.accounts != nil {
			_ = s.accounts.Stop(ctx)
		}
		_ = s.playerLoops.Stop(ctx)
		_ = s.modules.Stop(ctx)
		return err
	}
	s.addSubscription(stateSub)

	connectSub, err := s.bus.Subscribe(s.sessionOnTopic(), func(ctx context.Context, env bus.Envelope) error {
		evt, ok := env.Payload.(contracts.SessionConnected)
		if !ok {
			return nil
		}
		if !s.acceptsLogicTarget(evt.TargetLogicNodeID) {
			return nil
		}
		evt.AccountID = strings.TrimSpace(evt.AccountID)
		evt.SessionID = strings.TrimSpace(evt.SessionID)
		// 先更新 session 投影再投递玩家队列；如果投递失败必须回滚投影，避免后续包被错误放行。
		s.markSessionConnected(evt.AccountID, evt.SessionID)
		if err := s.playerLoops.Submit(ctx, evt.AccountID, evt); err != nil {
			s.clearCurSession(evt.AccountID, evt.SessionID)
			return playerLoopErr(err)
		}
		return nil
	})
	if err != nil {
		s.closeSubscriptions()
		if s.accounts != nil {
			_ = s.accounts.Stop(ctx)
		}
		_ = s.playerLoops.Stop(ctx)
		_ = s.modules.Stop(ctx)
		return err
	}
	s.addSubscription(connectSub)

	disconnectSub, err := s.bus.Subscribe(s.sessionOffTopic(), func(ctx context.Context, env bus.Envelope) error {
		evt, ok := env.Payload.(contracts.SessionDisconnected)
		if !ok {
			return nil
		}
		if !s.acceptsLogicTarget(evt.TargetLogicNodeID) {
			return nil
		}
		evt.AccountID = strings.TrimSpace(evt.AccountID)
		evt.SessionID = strings.TrimSpace(evt.SessionID)
		// 断开事件允许乱序到达，markSessGone 会拒绝覆盖更晚的新 session。
		accepted, hadCurrent := s.markSessGone(evt.AccountID, evt.SessionID)
		if !accepted {
			return nil
		}
		if err := s.playerLoops.Submit(ctx, evt.AccountID, evt); err != nil {
			s.restoreSessGone(evt.AccountID, evt.SessionID, hadCurrent)
			return playerLoopErr(err)
		}
		return nil
	})
	if err != nil {
		s.closeSubscriptions()
		if s.accounts != nil {
			_ = s.accounts.Stop(ctx)
		}
		_ = s.playerLoops.Stop(ctx)
		_ = s.modules.Stop(ctx)
		return err
	}
	s.addSubscription(disconnectSub)

	packetSub, err := s.bus.Subscribe(s.gatewayPacketTopic(), func(ctx context.Context, env bus.Envelope) error {
		evt, ok := env.Payload.(contracts.GatewayClientPacket)
		if !ok {
			return nil
		}
		if !s.acceptsLogicTarget(evt.TargetLogicNodeID) {
			return nil
		}
		evt.AccountID = strings.TrimSpace(evt.AccountID)
		evt.SessionID = strings.TrimSpace(evt.SessionID)
		// 所有客户端包先进入玩家队列串行化，真正的 route policy、幂等和 handler 执行在 dispatch flow 里完成。
		if err := s.playerLoops.Submit(ctx, evt.AccountID, evt); err != nil {
			s.incMetric("logic_packet_errors_total", map[string]string{"stage": "submit_playerloop"})
			return s.publishProtocolError(ctx, evt, playerLoopErr(err))
		}
		return nil
	})
	if err != nil {
		s.closeSubscriptions()
		if s.accounts != nil {
			_ = s.accounts.Stop(ctx)
		}
		_ = s.playerLoops.Stop(ctx)
		_ = s.modules.Stop(ctx)
		return err
	}
	s.addSubscription(packetSub)

	if err := s.startMetricEventLoop(ctx); err != nil {
		_ = s.Stop(context.Background())
		return err
	}

	if s.health != nil {
		s.health.SetComponent(s.Name(), health.StateReady, "logic modules running")
	}
	if err := s.bus.Publish(ctx, contracts.TopicServiceStarted, contracts.ServiceStarted{
		Service:   s.startedService,
		NodeID:    s.nodeID,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		_ = s.Stop(context.Background())
		return err
	}
	return nil
}

func (s *Service) addSubscription(sub bus.Subscription) {
	if s == nil || sub == nil {
		return
	}
	s.subsMu.Lock()
	s.subs = append(s.subs, sub)
	s.subsMu.Unlock()
}

func (s *Service) closeSubscriptions() {
	if s == nil {
		return
	}
	s.subsMu.Lock()
	subs := append([]bus.Subscription(nil), s.subs...)
	s.subs = nil
	s.subsMu.Unlock()
	for _, sub := range subs {
		if sub != nil {
			_ = sub.Close()
		}
	}
}

// Stop 停止 logic 服务并释放订阅、玩家队列和缓存资源。
func (s *Service) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.closeSubscriptions()
	var stopErr error
	stopErr = errors.Join(stopErr, s.drainMetricEventLoop(ctx))
	if s.playerLoops != nil {
		stopErr = errors.Join(stopErr, s.playerLoops.Stop(ctx))
	}
	if pub := s.eventPublisher(); pub != nil {
		stopErr = errors.Join(stopErr, pub.Close(ctx))
	}
	if s.responsesOut != nil {
		stopErr = errors.Join(stopErr, s.responsesOut.Close(ctx))
	}
	if s.accounts != nil {
		stopErr = errors.Join(stopErr, s.accounts.Stop(ctx))
	}
	if s.idempotency != nil {
		stopErr = errors.Join(stopErr, s.idempotency.Close(ctx))
	}
	if s.responses != nil {
		stopErr = errors.Join(stopErr, s.responses.Close(ctx))
	}
	if closer, ok := s.notices.Store.(runtimeStoreCloser); ok {
		stopErr = errors.Join(stopErr, closer.Close())
	}
	stopErr = errors.Join(stopErr, db.CloseOrFlush(ctx, s.notices.Publisher))
	if closer, ok := s.redeems.Store.(runtimeStoreCloser); ok {
		stopErr = errors.Join(stopErr, closer.Close())
	}
	stopErr = errors.Join(stopErr, db.CloseOrFlush(ctx, s.leaderboards))
	stopErr = errors.Join(stopErr, s.modules.Stop(ctx))
	s.resetSessionTracking()
	return stopErr
}
