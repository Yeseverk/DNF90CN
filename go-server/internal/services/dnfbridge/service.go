// service.go 负责 DNF bridge 生命周期、目录加载和监听器管理。
// 这里不写玩法状态，只装配频道服与 game 端口的 transport 边界。
package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/channelinfo"
	dnfcharstat "longheng.io/server/internal/modules/dnf/charstat"
	dnfcombatpower "longheng.io/server/internal/modules/dnf/combatpower"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dproto"
	dnfexpertjob "longheng.io/server/internal/modules/dnf/expertjob"
	dnfhonor "longheng.io/server/internal/modules/dnf/honor"
	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
	"longheng.io/server/internal/modules/dnf/party"
	dnfpet "longheng.io/server/internal/modules/dnf/pet"
	dnfprofession "longheng.io/server/internal/modules/dnf/profession"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
	dnftown "longheng.io/server/internal/modules/dnf/town"
	"longheng.io/server/internal/modules/dnf/worldmap"
	appkit "longheng.io/server/internal/platform/app"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

// Service 是 DNF 最新客户端 bridge 的生命周期组件。
type Service struct {
	logger  *slog.Logger
	options options

	mu                        sync.Mutex
	catalog                   *channelcatalog.Catalog
	channelScript             []byte
	listeners                 []net.Listener
	conns                     map[net.Conn]struct{}
	gameSessions              map[uint16]*gameSession
	nextGameSessionGeneration uint64
	tradeMu                   sync.Mutex
	itemTrades                map[uint16]*onlineItemTrade
	expertJobStoreMu          sync.Mutex
	expertJobStoreOpMu        sync.Mutex
	expertJobStores           map[uint16]*currentExpertJobStore
	expertJobVisitors         map[*gameSession]uint16
	raidMu                    sync.Mutex
	raids                     map[uint32]*runtimeRaidState
	nextRaidSeq               uint32
	packetLog                 *packetLogger
	partyUDPRelay             *currentPartyUDPRelay
	runtimePartyManager       *party.RuntimePartyManager
	cancel                    context.CancelFunc
	wg                        sync.WaitGroup
	packetID                  uint64
	clientAccounts            clientAccountRegistry

	initialEquipmentMu      sync.Mutex
	initialEquipmentArchive *platformpvf.Archive
	initialEquipmentByJob   map[byte][]initialEquipmentEntry
	initialEquipmentLoadErr error
	pvfItemCatalogMu        sync.Mutex
	pvfItemCatalog          *pvfDungeonDropCatalog
	pvfItemCatalogLoadErr   error
	joustCatalogMu          sync.Mutex
	joustCatalog            *dnfjoust.Catalog
	joustCatalogLoadErr     error
	joustHistoryMu          sync.Mutex
	joustOperationMu        sync.Mutex
	spendTimeCatalogMu      sync.Mutex
	spendTimeCatalogState   *currentSpendTimeRuntimeCatalog
	spendTimeCatalogLoadErr error
	expertJobMu             sync.Mutex
	expertJobCatalog        *dnfexpertjob.Catalog
	expertJobCatalogLoadErr error
	petCatalogMu            sync.Mutex
	petCatalog              *dnfpet.PVFCatalog
	petCatalogLoadErr       error
	premiumCatalogMu        sync.Mutex
	premiumCatalog          *currentPremiumCatalog
	premiumCatalogLoadErr   error
	repairCostMu            sync.Mutex
	repairCostCatalog       *currentRepairCostCatalog
	repairCostLoadErr       error
	npcShopCatalogMu        sync.Mutex
	npcShopCatalog          *currentNPCShopCatalog
	npcShopCatalogLoadErr   error
	adventureGroupMu        sync.Mutex
	adventureGroupLoginMu   sync.Mutex
	adventureGroupRuntimeMu sync.Mutex
	adventureGroupTable     *adventuregroup.Tables
	adventureGroupLoadErr   error
	honorMu                 sync.Mutex
	honorTable              *dnfhonor.Tables
	honorLoadErr            error
	initialSkillsMu         sync.Mutex
	initialSkillsByJob      map[byte][]initialSkillEntry
	skillCatalog            *dnfskill.Table
	initialSPTable          map[int]int
	initialTPTable          map[int]int
	professionMu            sync.Mutex
	professionProfiles      *dnfprofession.Profiles
	professionProfilesErr   error
	questCatalogMu          sync.Mutex
	questCatalog            *dnfquest.Catalog
	questCatalogLoadErr     error
	ceraShopMu              sync.Mutex
	ceraShopCatalog         *pvfCeraShopCatalog
	ceraShopCatalogLoadErr  error
	ceraShopPurchaseMu      sync.Mutex
	deathTowerMu            sync.Mutex
	deathTowerStageMapIDs   []int
	deathTowerBasisLevel    int
	titleBookMappingMu      sync.Mutex
	titleBookMappingCache   map[int32]titleBookMappingEntry
	onlinePlayers           *OnlinePlayerManager
	characterStatsMu        sync.Mutex
	characterStats          *dnfcharstat.Table
	characterStatsLoadErr   error
	equipmentStatsMu        sync.Mutex
	equipmentStatPaths      map[int64]string
	equipmentStats          map[int64]dnfcharstat.Vector
	equipmentStatsLoadErr   error
	combatPowerMu           sync.Mutex
	combatPowerCatalog      *dnfcombatpower.Catalog
	combatPowerCatalogErr   error
	townCatalogMu           sync.RWMutex
	townCatalog             *dnftown.Table
	worldMapMu              sync.RWMutex
	worldMapTable           *worldmap.Table
	worldMapResolver        *worldmap.Resolver
	dungeonMonsterTable     *pvfDungeonMonsterCatalog
	dungeonAICharacterTable *pvfDungeonAICharacterCatalog
	dungeonTutorialScripts  *pvfDungeonTutorialScriptCatalog
	dungeonChoice           func(int) (int, error)
	dungeonSeed             func() (uint32, error)
	gameplayTimers          gameplayTimeQueue
	repositoryProvider      func() (dnfrepo.Group, bool)
	initialTownEntryWait    func(time.Duration)
	dprotoProvider          dproto.Provider
}

// ServiceOptions 描述 dnfbridge 对外注入的项目侧依赖。
// 这里只保存依赖入口，不直接创建 MySQL/Redis，也不把玩法状态塞进 bridge。
type ServiceOptions struct {
	RepositoryProvider func() (dnfrepo.Group, bool)
	DprotoProvider     dproto.Provider
}

// New 使用框架环境创建 DNF bridge 服务。
func New(env *appkit.Env) *Service {
	return NewWithOptions(env, ServiceOptions{})
}

// NewWithOptions 使用框架环境和项目侧依赖创建 DNF bridge 服务。
func NewWithOptions(env *appkit.Env, serviceOptions ServiceOptions) *Service {
	if env == nil {
		env = &appkit.Env{}
	}
	core := env.Core()
	service := &Service{
		logger:                  core.Logger,
		options:                 optionsFromConfig(core.Config),
		conns:                   make(map[net.Conn]struct{}),
		gameSessions:            make(map[uint16]*gameSession),
		itemTrades:              make(map[uint16]*onlineItemTrade),
		expertJobStores:         make(map[uint16]*currentExpertJobStore),
		expertJobVisitors:       make(map[*gameSession]uint16),
		raids:                   make(map[uint32]*runtimeRaidState),
		initialEquipmentArchive: env.PVF,
		repositoryProvider:      serviceOptions.RepositoryProvider,
		dprotoProvider:          serviceOptions.DprotoProvider,
		clientAccounts:          newClientAccountRegistry(),
		runtimePartyManager:     party.NewRuntimePartyManager(),
	}
	service.gameplayTimers = newProcessGameplayTimeQueue(func(name string, recovered any) {
		service.logWarn("dnfbridge gameplay timer callback panic", "timer_name", name, "recovered", recovered)
	})
	service.onlinePlayers = newOnlinePlayerManager()
	return service
}

// Name 返回生命周期组件名。
func (s *Service) Name() string {
	return "dnfbridge-service"
}

// Start 加载外部 channel_info.etc，并启动频道服和 game 端口监听。
func (s *Service) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.validateDprotoConfiguration(); err != nil {
		return err
	}
	catalog, channelScript, err := s.loadChannelAssets()
	if err != nil {
		return err
	}
	packetLog, err := s.openPacketLog()
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.catalog = catalog
	s.channelScript = append([]byte(nil), channelScript...)
	s.cancel = cancel
	s.packetLog = packetLog
	if s.initialTownEntryWait == nil {
		// Initial-town visual delay is disabled. Production never blocks the
		// server packet stream at the retained test boundary.
		s.initialTownEntryWait = func(time.Duration) {}
	}
	if s.conns == nil {
		s.conns = make(map[net.Conn]struct{})
	}
	s.mu.Unlock()

	// PVF preload stages run in a fixed order before any listener opens. Keep
	// one row per stage: the order, each loader's own wrapped error text, and
	// the shared cleanup path are observable startup behavior. The loop must
	// not re-wrap loader errors or change what is reset on failure; name only
	// labels the stage for audit, it is never merged into the returned error.
	pvfPreloads := []struct {
		name string
		load func(context.Context) error
	}{
		{"initial equipment index", s.preloadInitialEquipmentIndex},
		{"item catalog", s.preloadPVFItemCatalog},
		{"expert job catalog", s.preloadExpertJobCatalog},
		{"adventure group table", s.preloadAdventureGroupTable},
		{"honor table", s.preloadHonorTable},
		{"town catalog", s.preloadTownCatalog},
		{"skill catalog", s.preloadSkillCatalog},
		{"profession profiles", s.preloadProfessionProfiles},
		{"quest catalog", s.preloadQuestCatalog},
		{"dungeon world map", s.preloadDungeonWorldMap},
		{"character stat table", s.preloadCharacterStatTable},
		{"equipment stat index", s.preloadEquipmentStatIndex},
		{"combat power affix catalog", s.preloadCombatPowerAffixCatalog},
	}
	for _, preload := range pvfPreloads {
		if err := preload.load(runCtx); err != nil {
			cancel()
			closeErr := packetLog.Close()
			s.mu.Lock()
			s.catalog = nil
			s.cancel = nil
			s.packetLog = nil
			s.mu.Unlock()
			return errors.Join(err, closeErr)
		}
	}
	gamePorts := s.gameListenPorts(catalog)
	if len(gamePorts) == 0 {
		cancel()
		closeErr := packetLog.Close()
		s.mu.Lock()
		s.catalog = nil
		s.cancel = nil
		s.packetLog = nil
		s.mu.Unlock()
		return errors.Join(fmt.Errorf("dnfbridge has no game listen ports"), closeErr)
	}
	partyRelay, err := newCurrentPartyUDPRelay(
		s.options.partyUDPRelayEnabled,
		s.options.serverIP,
		s.options.partyUDPRelayPortStart,
		s.options.partyUDPRelayPortCount,
		func(message string, args ...any) { s.logWarn(message, args...) },
	)
	if err != nil {
		cancel()
		closeErr := packetLog.Close()
		s.mu.Lock()
		s.catalog = nil
		s.cancel = nil
		s.packetLog = nil
		s.mu.Unlock()
		return errors.Join(err, closeErr)
	}
	s.mu.Lock()
	s.partyUDPRelay = partyRelay
	s.mu.Unlock()

	if err := s.listen(runCtx, s.options.channelListen, s.handleChannelConn); err != nil {
		cancel()
		_ = partyRelay.Close()
		closeErr := packetLog.Close()
		s.mu.Lock()
		s.catalog = nil
		s.cancel = nil
		s.packetLog = nil
		s.partyUDPRelay = nil
		s.mu.Unlock()
		return errors.Join(err, closeErr)
	}
	s.startGameplayTimeQueue(runCtx)
	s.startCurrentJoustScheduler(runCtx)

	for _, port := range gamePorts {
		addr := gameListenAddress(s.options.gameListenHost, port)
		if err := s.listen(runCtx, addr, s.handleGameConn); err != nil {
			cancel()
			_ = s.Stop(context.Background())
			return err
		}
	}
	s.logInfo("dnfbridge started",
		"channel_listen", s.options.channelListen,
		"channel_info", s.options.channelInfoFile,
		"channel_server_index", s.channelServerID(),
		"advertise_server_index", s.channelAdvertiseID(),
		"channel_info_body_mode", s.channelInfoBodyMode(),
		"game_initial_mode", s.gameInitialMode(),
		"game_pre_bootstrap", s.gamePreBootstrapMode(),
		"game_post_bootstrap", s.gamePostBootstrapMode(),
		"game_upper_header", s.gameUpperHeaderMode(),
		"game_upper_client_body_codec", s.gameUpperClientBodyCodecMode(),
		"game_dproto_mode", s.gameDprotoMode(),
		"game_listen_host", s.options.gameListenHost,
		"game_ports", len(gamePorts),
		"server_ip", s.options.serverIP,
		"party_udp_relay", partyRelay.Enabled(),
		"party_udp_relay_port_start", s.options.partyUDPRelayPortStart,
		"party_udp_relay_port_count", s.options.partyUDPRelayPortCount)
	return nil
}

func gameListenAddress(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ":" + strconv.Itoa(port)
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
}

// Stop 关闭监听并等待连接处理退出。
func (s *Service) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	cancel := s.cancel
	listeners := append([]net.Listener(nil), s.listeners...)
	packetLog := s.packetLog
	partyRelay := s.partyUDPRelay
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.listeners = nil
	s.cancel = nil
	s.packetLog = nil
	s.partyUDPRelay = nil
	s.channelScript = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	var stopErr error
	for _, listener := range listeners {
		stopErr = errors.Join(stopErr, listener.Close())
	}
	for _, conn := range conns {
		stopErr = errors.Join(stopErr, conn.Close())
	}
	if partyRelay != nil {
		stopErr = errors.Join(stopErr, partyRelay.Close())
	}
	stopErr = errors.Join(stopErr, packetLog.Close())
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return errors.Join(stopErr, ctx.Err())
	case <-done:
		return stopErr
	}
}

func (s *Service) listen(ctx context.Context, addr string, handler func(context.Context, net.Conn)) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	s.mu.Lock()
	s.listeners = append(s.listeners, listener)
	s.mu.Unlock()
	s.wg.Add(1)
	go s.acceptLoop(ctx, listener, handler)
	return nil
}

func (s *Service) acceptLoop(ctx context.Context, listener net.Listener, handler func(context.Context, net.Conn)) {
	defer s.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logWarn("dnfbridge accept failed", "addr", listener.Addr().String(), "error", err)
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.trackConn(conn)
			defer s.untrackConn(conn)
			handler(ctx, conn)
		}()
	}
}

func (s *Service) loadChannelAssets() (*channelcatalog.Catalog, []byte, error) {
	raw, err := osReadFile(s.options.channelInfoFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load dnf channel info: %w", err)
	}
	index, err := channelinfo.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parse dnf channel info: %w", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{ServerID: s.channelServerID()})
	if err != nil {
		return nil, nil, err
	}
	script, err := s.buildChannelScript(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("build 90CN online channel script: %w", err)
	}
	return catalog, script, nil
}

func (s *Service) buildChannelScript(raw []byte) ([]byte, error) {
	return channelinfo.Build90CNOnlineScript(
		raw,
		s.channelServerID(),
		s.channelAdvertiseID(),
		dnfenum.BootstrapChannelID,
	)
}

func (s *Service) channelServerID() int {
	if s != nil {
		if s.options.channelServerID > 0 {
			return s.options.channelServerID
		}
	}
	return 1
}

func (s *Service) channelAdvertiseID() int {
	if s != nil {
		if s.options.channelAdvertiseID >= 0 {
			return s.options.channelAdvertiseID
		}
	}
	return s.channelServerID()
}

func (s *Service) channelInfoBodyMode() string {
	if s == nil {
		return channelInfoBodyLatest
	}
	return normalizeChannelInfoBodyMode(s.options.channelInfoBodyMode)
}

func (s *Service) currentCatalog() *channelcatalog.Catalog {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.catalog
}

func (s *Service) currentChannelScript() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.channelScript...)
}

func (s *Service) openPacketLog() (*packetLogger, error) {
	if !s.options.packetLogEnabled {
		return nil, nil
	}
	return openPacketLogger(s.options.packetLogPath)
}

func (s *Service) logPacket(direction string, kind string, data []byte, fields ...any) {
	if suppressPacketLogLine(direction, kind, fields...) {
		return
	}
	s.mu.Lock()
	logger := s.packetLog
	s.mu.Unlock()
	logger.Log(direction, kind, data, fields...)
}

func (s *Service) logPacketEvent(kind string, args ...any) {
	if suppressPacketLogEvent(kind, args...) {
		return
	}
	s.mu.Lock()
	logger := s.packetLog
	s.mu.Unlock()
	logger.Event(kind, args...)
}

func suppressPacketLogLine(direction string, kind string, fields ...any) bool {
	if kind == "game-raw" {
		return true
	}
	msgID, hasMsgID := packetLogUintField(fields, "msg_id")
	typ, hasType := packetLogUintField(fields, "type")
	if hasMsgID {
		switch msgID {
		case 1276, 1516, 1518: // heartbeat / antibot / DPROTO callback noise.
			return true
		case 13:
			// op13 full container refreshes already have concise EVENT evidence.
			return direction == "SEND"
		}
	}
	if hasType {
		switch typ {
		case 35: // SET_USER_POSITION, emitted continuously while walking.
			return true
		}
	}
	return false
}

func suppressPacketLogEvent(kind string, fields ...any) bool {
	switch kind {
	case "game-town-set-user-position-captured":
		return true
	case "game-upper-check-user-connection-selected-success":
		return true
	}
	msgID, hasMsgID := packetLogUintField(fields, "msg_id")
	typ, hasType := packetLogUintField(fields, "type")
	if hasMsgID {
		switch msgID {
		case 1276, 1516, 1518:
			return true
		case 13:
			return kind == "game-upper-send-meta"
		}
	}
	if hasType && typ == 35 {
		return kind == "game-legacy-meta"
	}
	return false
}

func packetLogUintField(fields []any, name string) (uint64, bool) {
	for i := 0; i+1 < len(fields); i += 2 {
		if fmt.Sprint(fields[i]) != name {
			continue
		}
		switch v := fields[i+1].(type) {
		case uint8:
			return uint64(v), true
		case uint16:
			return uint64(v), true
		case uint32:
			return uint64(v), true
		case uint64:
			return v, true
		case uint:
			return uint64(v), true
		case int8:
			if v >= 0 {
				return uint64(v), true
			}
		case int16:
			if v >= 0 {
				return uint64(v), true
			}
		case int32:
			if v >= 0 {
				return uint64(v), true
			}
		case int64:
			if v >= 0 {
				return uint64(v), true
			}
		case int:
			if v >= 0 {
				return uint64(v), true
			}
		}
	}
	return 0, false
}

func (s *Service) trackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns == nil {
		s.conns = make(map[net.Conn]struct{})
	}
	s.conns[conn] = struct{}{}
}

func (s *Service) untrackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

func (s *Service) nextPacketConnID(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packetID++
	return fmt.Sprintf("%s-%06d", prefix, s.packetID)
}

func (s *Service) aesKey() string {
	return time.Now().Format("20060102") + "000006"
}

func (s *Service) logInfo(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Info(msg, args...)
	}
}

func (s *Service) logWarn(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Warn(msg, args...)
	}
}
