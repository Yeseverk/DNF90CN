// 本文件负责 DNF bridge 的 game 端口连接、混合帧分发和登录前 wire 回包。
// 这里只处理客户端字节协议兼容，不持有玩家持久化状态或玩法规则。
package dnfbridge

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dproto"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

type gameSession struct {
	conn            net.Conn
	connID          string
	accountID       string
	clientPID       uint32
	channel         channelcatalog.Channel
	residentChannel channelcatalog.Channel
	packetSeq       uint64
	sequence        uint32
	upperSeq        uint16
	ctx             context.Context
	// wireMu owns the complete per-session wire boundary: sequence
	// allocation, DPROTO state transitions, and socket writes.  A gameplay
	// timer can send concurrently with the connection reader, so callers
	// must not allocate a sequence before acquiring this lock.
	wireMu sync.Mutex
	dproto dproto.Session

	endpointHandshakeMu                     sync.Mutex
	currentChannelResidentNoticeSent        bool
	gameEndpointSuccessSent                 bool
	connectionTownActorOwnerChannel         byte
	events                                  *gameSessionEvents
	characterGeneration                     uint64
	selectedCharacterID                     uint16
	rosterRequested                         bool
	channelReconnect                        bool
	townActorOwnerChannel                   byte
	representAccountNamePending             bool
	representAccountNameRegistrationSent    bool
	pendingCharacterRosterBootstrap         bool
	emptyRosterSlotProbePending             bool
	selectorAdventureInfoPending            bool
	selectorAdventureInfoSlot               uint16
	enterSelectDungeonSent                  bool
	enterSelectDungeonAckSent               bool
	enterSelectDungeonContextSent           bool
	backToVillageEnterSelectPending         bool
	sceneBootstrapTailDeferred              bool
	sceneBootstrapTailSent                  bool
	sceneBootstrapTailPacketIndex           int
	sceneBootstrapTailObjectMode1Pending    bool
	sceneBootstrapTailPostStage             currentDeferredSelectSceneTailPostStage
	runtimeAfterBlacklistSent               bool
	runtimeFinishLoadingGateSent            bool
	fpsFinishLoadingGateSent                bool
	selectedUserInfoRefreshSent             bool
	selectedUserInfoMode3Sent               bool
	currentSceneObjectListSent              bool
	selectedItemListRefreshSent             bool
	selectedItemListBootstrapCharacterID    uint16
	selectedEquipmentUpdateSent             bool
	selectedCreatureStateTableSent          bool
	selectedRentalWalletStateSent           bool
	expertJobInfoCharacterID                uint16
	selectPreviewObjectStateSent            bool
	selectPreviewActorRemoved               bool
	preDungeonContextPlayerStateSent        bool
	postStartMapPlayerStateSent             bool
	deferredDungeonUserStateObjectKey       uint16
	currentFinishLoadingStateSent           bool
	currentFinishLoadingCompletionSent      bool
	postFinishLoadingPlayerStateSent        bool
	returnTownFinishLoadingAckOnly          bool
	confirmedDungeonReturnStatePending      bool
	initialTownRouteCharacterID             uint16
	initialTownRouteStage                   currentInitialTownRouteStage
	initialTownActorSceneSnapshotSent       bool
	initialTownLocationNotificationsSent    bool
	initialTownQuestSnapshotsSent           bool
	initialTownSkillInfoPrepared            bool
	initialTownSkillInfoSent                bool
	initialTownSkillInfo                    currentSceneSkillInfoProjection
	initialTownLegacySceneReadyAccepted     bool
	initialTownAdventureOverheadRefreshSent bool
	initialTownCombatPowerAffixesSent       bool
	townPostTransition                      townPostTransitionState
	crystalContractMu                       sync.Mutex
	crystalContractTownUIReadyStateSent     bool
	auraSkinMu                              sync.Mutex
	auraSkinTownUIReadyStateSent            bool
	townSceneReadyCharacterID               uint16
	townPositionSnapshot                    currentTownPositionSnapshot
	townPrevVillageSnapshot                 currentTownPositionSnapshot
	townSelectorOriginSnapshot              currentTownPositionSnapshot
	townSelectorOriginBound                 bool
	townTransportEnterSelectPending         bool
	returnSelectTownReentryPending          bool
	questReplay                             questReplayState
	party                                   partySessionState
	partyPeerMu                             sync.Mutex
	partyPeer                               currentPartyPeerEndpointRegistration
	lottery                                 lotteryState
	dungeon                                 dungeonSessionState
	townMu                                  sync.Mutex
	petGrowth                               petGrowthClockState
	spendTime                               currentSpendTimeClockState
}

// partySessionState 持有组队投影与待处理邀请的会话状态。
// mu 守护 state 与 pending 邀请字段；state 的地址会传给 alignedcmd.Route，
// 迁移后仍为同一字段地址，锁序（party 锁内不嵌套 wireMu）保持不变。
// partySessionState is a per-client packet projection only. Membership,
// invitations, slots, leader replacement, and session generations are owned
// by party.RuntimePartyManager; this cache exists solely to shape the current
// EXE's existing party packets.
type partySessionState struct {
	mu    sync.Mutex
	state alignedcmd.PartyState
}

// questReplayState 持有任务接受/触发/完成三条回放的幂等去重状态。
// 每把锁各自守护对应 map 的懒初始化与读写，三把锁互不嵌套，保持原有临界区。
type questReplayState struct {
	acceptMu           sync.Mutex
	acceptAcknowledged map[currentAcceptQuestReplayKey]struct{}
	giveUpMu           sync.Mutex
	giveUpAnswered     map[currentGiveUpQuestReplayKey]struct{}
	triggerMu          sync.Mutex
	triggerNoop        map[currentSetQuestTriggerReplayKey]struct{}
	finishMu           sync.Mutex
	finishAnswered     map[currentFinishQuestReplayKey]struct{}
}

// lotteryState 持有魔盒抽奖待确认流程的会话状态。
// mu 守护待确认标记、栏位、双倍标记与发起时间，消费时清零的时机保持不变。
type lotteryState struct {
	mu            sync.Mutex
	pending       bool
	pendingSlot   int16
	pendingDouble bool
	pendingAt     time.Time
}

// petGrowthClockState 持有宠物饱食度时钟的会话状态。
// mu 守护时钟模式、锚点、代数与定时器名的全部读写（含 timequeue 回调），
// 停止/切换时的结算与归零时机保持不变。
type petGrowthClockState struct {
	mu          sync.Mutex
	mode        currentPetGrowthClockMode
	characterID uint16
	anchor      time.Time
	generation  uint64
	timerName   string
}

// townPostTransitionState 持有城镇入场后过渡流程的会话状态。
// mu 守护过渡代数、角色、属主频道、阶段与来源标记，arm/推进/归零时机保持不变。
type townPostTransitionState struct {
	mu           sync.Mutex
	generation   uint64
	characterID  uint16
	ownerChannel byte
	stage        currentTownPostTransitionStage
	source       string
}

// dungeonSessionState 持有单个地下城运行周期的会话状态。
// mu 守护 runtime、runToken 与 deathTower 的全部读写（含定时器回调与
// handler 路径），原有 dungeonMu 的临界区与锁序惯例保持不变。
type dungeonSessionState struct {
	mu         sync.Mutex
	runtime    *runtimeDungeonState
	runToken   uint64
	deathTower *deathTowerRuntime
}

func (s *Service) handleGameConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	connID := s.nextPacketConnID("game")
	channel, ok := s.channelForConn(conn)
	if !ok {
		s.logPacketEvent("game-connect-rejected",
			"conn_id", connID,
			"remote", conn.RemoteAddr().String(),
			"local", conn.LocalAddr().String(),
			"reason", "local_port_not_in_channel_catalog")
		s.logWarn("dnfbridge rejected game connection for unknown channel port",
			"remote", conn.RemoteAddr().String(),
			"local", conn.LocalAddr().String())
		return
	}
	session := &gameSession{
		conn:            conn,
		connID:          connID,
		channel:         channel,
		residentChannel: channel,
		// The numeric channel belongs to the transport/session.  Until the
		// current EXE commits a different scene context through its native
		// protocol, the selected local actor is owned by context zero.
		townActorOwnerChannel: currentSceneObjectContext,
		ctx:                   ctx,
	}
	if pid, found := localTCPConnectionOwnerPID(conn); found {
		session.clientPID = pid
		if accountID, registered := s.registeredClientAccount(pid); registered {
			session.accountID = accountID
		}
	}
	s.logInfo("dnfbridge game connection accepted",
		"conn_id", session.connID,
		"remote", conn.RemoteAddr().String(),
		"local", conn.LocalAddr().String(),
		"channel_id", channel.ID,
		"channel_type", channel.Type,
		"game_port", channel.Port,
		"client_pid", session.clientPID,
		"session_account_bound", session.accountID != "")
	s.logPacketEvent("game-connect",
		"conn_id", session.connID,
		"remote", conn.RemoteAddr().String(),
		"local", conn.LocalAddr().String(),
		"channel_id", channel.ID,
		"channel_type", channel.Type,
		"game_port", channel.Port)
	if err := s.startGameSessionEvents(ctx, session); err != nil {
		s.logPacketEvent("game-session-events-start-failed", "conn_id", session.connID, "error", err)
		s.logWarn("dnfbridge start game session events failed", "remote", conn.RemoteAddr().String(), "error", err)
		return
	}
	dprotoOpened := false
	defer func() {
		s.shutdownGameSessionEvents(session, dprotoOpened)
	}()
	if err := s.callGameSession(ctx, session, "game-dproto-open", func() error {
		return s.openGameDprotoSession(ctx, session)
	}); err != nil {
		s.logPacketEvent("game-dproto-open-failed", "conn_id", session.connID, "error", err)
		s.logWarn("dnfbridge open native dproto session failed", "remote", conn.RemoteAddr().String(), "error", err)
		return
	}
	dprotoOpened = true
	defer s.logPacketEvent("game-close",
		"conn_id", session.connID,
		"remote", conn.RemoteAddr().String(),
		"local", conn.LocalAddr().String(),
		"channel_id", channel.ID,
		"game_port", channel.Port)
	if err := s.callGameSession(ctx, session, "game-connection-bootstrap", func() error {
		return s.sendGameConnectionBootstrap(session, time.Now())
	}); err != nil {
		s.logPacketEvent("game-connection-bootstrap-failed",
			"conn_id", session.connID,
			"remote", conn.RemoteAddr().String(),
			"local", conn.LocalAddr().String(),
			"channel_id", channel.ID,
			"error", err)
		s.logWarn("dnfbridge send game connection bootstrap failed", "remote", conn.RemoteAddr().String(), "error", err)
		return
	}

	buffer := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for {
		n, err := conn.Read(chunk)
		if err != nil {
			quiet := isQuietGameReadError(err)
			s.logPacketEvent("game-read-close",
				"conn_id", session.connID,
				"remote", conn.RemoteAddr().String(),
				"local", conn.LocalAddr().String(),
				"channel_id", channel.ID,
				"pending_bytes", len(buffer),
				"quiet", quiet,
				"error", err)
			if !quiet && ctx.Err() == nil {
				s.logWarn("dnfbridge read game packet failed", "remote", conn.RemoteAddr().String(), "error", err)
			}
			return
		}
		if n > 0 {
			s.logGamePacket(session, "RECV", "game-raw", chunk[:n])
		}
		buffer = append(buffer, chunk[:n]...)
		packets, remaining, skipped, err := dnfproto.SplitLatestGameStream(buffer, s.options.maxPacketBytes)
		if err != nil {
			s.logPacketEvent("game-split-error",
				"conn_id", session.connID,
				"remote", conn.RemoteAddr().String(),
				"local", conn.LocalAddr().String(),
				"channel_id", channel.ID,
				"buffer_bytes", len(buffer),
				"error", err)
			s.logWarn("dnfbridge split game stream failed", "remote", conn.RemoteAddr().String(), "error", err)
			return
		}
		if skipped > 0 {
			s.logWarn("dnfbridge game frame resynced", "remote", conn.RemoteAddr().String(), "skipped", skipped)
		}
		buffer = remaining
		for _, packet := range packets {
			packet := packet
			if err := s.callGameSession(ctx, session, "game-stream-packet", func() error {
				return s.handleGameStreamPacket(session, packet)
			}); err != nil {
				s.logWarn("dnfbridge handle game packet failed", "remote", conn.RemoteAddr().String(), "error", err)
				return
			}
		}
	}
}

func (s *Service) channelForConn(conn net.Conn) (channelcatalog.Channel, bool) {
	port := 0
	if endpoint, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		port = endpoint.Port
	}
	return s.channelForPort(port)
}

func (s *Service) channelForPort(port int) (channelcatalog.Channel, bool) {
	catalog := s.currentCatalog()
	if catalog == nil {
		return channelcatalog.Channel{}, false
	}
	return catalog.ForPort(port)
}

func (s *Service) gameListenPorts(catalog *channelcatalog.Catalog) []int {
	if catalog == nil {
		return nil
	}
	return catalog.GamePorts()
}

func (s *Service) handleGameStreamPacket(session *gameSession, packet dnfproto.LatestGameStreamPacket) error {
	switch packet.Kind {
	case dnfproto.LatestGameStreamTransport:
		return s.handleGameFrame(session, packet.Data)
	case dnfproto.LatestGameStreamUpper:
		return s.handleGameUpper(session, packet.Data)
	case dnfproto.LatestGameStreamLegacy:
		return s.handleLegacyGamePacket(session, packet.Data)
	case dnfproto.LatestGameStreamDproto:
		return s.handleGameDprotoPacket(session, packet.Data)
	default:
		return dnfproto.ErrPacketLength
	}
}

func isQuietGameReadError(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET)
}
