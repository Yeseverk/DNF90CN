package dnfbridge

import (
	"context"
	"encoding/binary"
	"math/bits"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

// sendGameInitial sends the endpoint success only after the current client
// answers CHANNELINFO with its native class1/op1 request.
func (s *Service) sendGameInitial(session *gameSession) (mode string, bodyLen int, sent bool, err error) {
	body := s.buildLoginSuccess(session.channel)
	err = s.sendGameUpperSuccess(session, uint16(dnfenum.UpperMsgGameEndpoint), body)
	return gameInitialModeUpper, len(body), true, err
}

// sendGameConnectionBootstrap separates the launcher's all-port discovery
// probes from the one account-bound game connection. CHANNELINFO writes the
// current EXE's native scene-channel global, so it is valid only when the same
// connection can represent that channel in the u8 town actor owner field.
// Discovery probes and channels outside that range keep the proven proactive
// class1/op1 endpoint response, which does not mutate native scene ownership.
func (s *Service) sendGameConnectionBootstrap(session *gameSession, now time.Time) error {
	if session == nil {
		return errResidentChannelUnavailable
	}
	accountBound := strings.TrimSpace(session.accountID) != ""
	ownerChannel, ownerErr := currentCommittedChannelOwner(session)
	if accountBound && ownerErr == nil {
		if err := s.sendCurrentChannelResidentNotice(session, now); err != nil {
			return err
		}
		session.connectionTownActorOwnerChannel = ownerChannel
		session.townActorOwnerChannel = ownerChannel
		return nil
	}

	session.endpointHandshakeMu.Lock()
	defer session.endpointHandshakeMu.Unlock()
	if session.gameEndpointSuccessSent {
		return nil
	}
	mode, bodyLen, sent, err := s.sendGameInitial(session)
	if err != nil || !sent {
		return err
	}
	session.gameEndpointSuccessSent = true
	reason := "unbound_channel_discovery_probe"
	if accountBound {
		reason = "resident_channel_cannot_fit_current_exe_town_owner"
	}
	s.logGameEvent(session, "game-endpoint-success-sent",
		"mode", mode,
		"body_len", bodyLen,
		"request_body_len", 0,
		"target_channel", session.channel.ID,
		"resident_owner_error", ownerErr,
		"reason", reason)
	return nil
}

// handleCurrentGameEndpointRequest completes the current EXE's one-shot
// CHANNELINFO handshake. The class0/op1 notice makes the client send this
// class1/op1 request and arm its watchdog; the first class1/op1 success clears
// that watchdog. Later endpoint probes must remain write-silent.
func (s *Service) handleCurrentGameEndpointRequest(session *gameSession, classification byte, wireBodyLen int) error {
	if session == nil {
		return errResidentChannelUnavailable
	}
	session.endpointHandshakeMu.Lock()
	defer session.endpointHandshakeMu.Unlock()

	if classification != dnfproto.DefaultChannelClassification ||
		!isCurrentChannelReconnectDisplayProbeBodyLen(wireBodyLen) {
		s.logGameEvent(session, "game-endpoint-client-op1-ignored",
			"classification", classification,
			"body_len", wireBodyLen,
			"target_channel", session.channel.ID,
			"reason", "current_exe_endpoint_request_shape_mismatch")
		return nil
	}
	if !session.currentChannelResidentNoticeSent {
		s.logGameEvent(session, "game-endpoint-client-op1-ignored",
			"classification", classification,
			"body_len", wireBodyLen,
			"target_channel", session.channel.ID,
			"reason", "channelinfo_notice_not_committed")
		return nil
	}
	if session.gameEndpointSuccessSent {
		s.logGameEvent(session, "game-channel-stale-endpoint-probe-ignored",
			"body_len", wireBodyLen,
			"target_channel", session.channel.ID,
			"reason", "single_class1_op1_already_sent_after_client_request")
		return nil
	}

	mode, bodyLen, sent, err := s.sendGameInitial(session)
	if err != nil {
		return err
	}
	if !sent {
		return nil
	}
	session.gameEndpointSuccessSent = true
	s.logGameEvent(session, "game-endpoint-success-sent",
		"mode", mode,
		"body_len", bodyLen,
		"request_body_len", wireBodyLen,
		"target_channel", session.channel.ID,
		"reason", "current_exe_channelinfo_triggered_login_request")
	return nil
}

func encNoticeBody(body []byte) []byte {
	encoded := make([]byte, len(body))
	for i, b := range body {
		encoded[i] = bits.RotateLeft8(b, 2) ^ 0xd6
	}
	return encoded
}

func (s *Service) initialNoticeWireBody(body []byte) ([]byte, bool, string) {
	if s.gameUpperBodyCodecMode() == gameUpperBodyCodecPlain {
		return append([]byte(nil), body...), false, "csharp_initial_notice_plain"
	}
	return encNoticeBody(body), true, "csharp_initial_notice_rotl2_xor_d6"
}

// sendPreInitialBootstrap retains the historical option hook for tests and
// configuration compatibility. The live connection path sends CHANNELINFO
// directly and does not call this legacy write-silent hook.
func (s *Service) sendPreInitialBootstrap(_ *gameSession) error {
	return nil
}

func (s *Service) gamePreBootstrapMode() string {
	if s.options.gamePreBootstrap == "" {
		return gamePreBootstrapNone
	}
	return s.options.gamePreBootstrap
}

// sendPostInitialBootstrap 保留旧探针入口，但当前 C# 对齐链路禁止首包后主动补包。
func (s *Service) sendPostInitialBootstrap(session *gameSession, initialMode string, initialSent bool) error {
	// GET_USERINFO/SELECT_CHARACTER 后续初始化链是被动回包，不能在首包后主动推。
	return nil
}

func (s *Service) gamePostBootstrapMode() string {
	if s.options.gamePostBootstrap == "" {
		return gamePostBootstrapNone
	}
	return s.options.gamePostBootstrap
}

func (s *Service) gameUpperHeaderMode() string {
	if s.options.gameUpperHeader == "" {
		return gameUpperHeaderChannel13
	}
	return s.options.gameUpperHeader
}

func (s *Service) gameInitialMode() string {
	if s.options.gameInitialMode == "" {
		return gameInitialModeNotice
	}
	return s.options.gameInitialMode
}

func (s *Service) sendUpperClientCheckList(session *gameSession) error {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeUint32(0)
	return s.sendGameUpperSecondaryRaw(session, uint16(dnfenum.UpperMsgClientCheck), writer.bytes())
}

func (s *Service) sendUpperGuardClear(session *gameSession) error {
	var writer packetWriter
	writer.writeUint32(1)
	writer.writeZero(8)
	writer.writeByte(0)
	writer.writeZero(12)
	writer.writeUint32(0)
	writer.writeUint32(0)
	writer.writeUint32(0)
	return s.sendGameUpperSecondaryRaw(session, uint16(dnfenum.UpperMsgGuardControl), writer.bytes())
}

func (s *Service) sendUpperEmptyRosterInit(session *gameSession) error {
	return s.sendGameUpperCharacterRosterRaw(session, buildCSharpEmptyRosterBody())
}

func buildCSharpEmptyRosterBody() []byte {
	return buildCSharpRosterBody(nil)
}

var emptyDprotoCallbackBody = []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

type csharpSelectInitPacket struct {
	class       byte
	msgID       uint16
	marker      uint32
	file        string
	body        []byte
	occurrence  int
	kind        string
	bodyEncoded bool
	bodyCodec   string
}

var csharpUpperSelectInitTemplate = []csharpSelectInitPacket{
	{class: dnfproto.DefaultChannelClassification, msgID: 0x0004, kind: "select_ack"},
}

func expandedCSharpUpperSelectInitTemplate() []csharpSelectInitPacket {
	return append([]csharpSelectInitPacket(nil), csharpUpperSelectInitTemplate...)
}

func isLargeLongHengSceneRosterPacket(packet csharpSelectInitPacket, body []byte) bool {
	return packet.kind == csharpLongHengSceneBootstrapKind &&
		packet.msgID == uint16(dnfenum.UpperMsgCharacterRoster) &&
		len(body) > 10000
}

func deferredSelectSceneTailPackets() []csharpSelectInitPacket {
	out := make([]csharpSelectInitPacket, 0, len(longHengSceneBootstrapBeforeHudPackets))
	afterLargeRoster := false
	for _, packet := range longHengSceneBootstrapBeforeHudPackets {
		// The current acceptable-quest notification is rebuilt from PVF/DB state
		// at send time.  It must not depend on the old large-roster fixture marker:
		// the current bootstrap inserts dynamic userinfo rows instead, so that marker
		// is absent and would otherwise suppress op0x15 forever.  This function is
		// only reached after either dungeon op29 or the completed-character town
		// route has committed the selected actor, satisfying dword_52B5BB0.
		if packet.kind == currentAcceptableQuestListKind {
			out = append(out, packet)
			continue
		}
		if afterLargeRoster {
			out = append(out, packet)
			continue
		}
		if isLargeLongHengSceneRosterPacket(packet, packet.body) {
			afterLargeRoster = true
		}
	}
	// The old 24-record HUD phase is intentionally not carried into the current
	// deferred tail; its bodies do not match current op583 or op851.
	return out
}

func (s *Service) sendUpperGetUserInfoBootstrap(session *gameSession) error {
	// GET_USERINFO is the role-select phase and must remain passive. The current
	// represent/adventure-name state opens a modal, so it is deliberately sent
	// only after the selected town scene has completed its initialization.
	// The roster must never be gated on first-time registration.
	//
	// The current client can deliver its three-byte op8 request through the
	// legacy stream decoder. Record the semantic roster transition here, where
	// both upper and legacy op8 converge, so the following upper op4 is not
	// mistaken for a fresh channel-reconnect selection.
	clearedReconnect := clearUnboundChannelReconnectForRoster(session)
	session.rosterRequested = true
	if clearedReconnect {
		s.logGameEvent(session, "game-getuserinfo-cleared-unbound-channel-reconnect",
			"reason", "authoritative_roster_request_supersedes_preselection_probe")
	}
	session.representAccountNamePending = false
	if err := s.prepareGetUserInfoPassiveAccountState(session); err != nil {
		return err
	}
	return s.sendUpperGetUserInfoRosterBootstrap(session)
}

func (s *Service) sendLegacyGetUserInfoBootstrap(session *gameSession) error {
	return s.sendUpperGetUserInfoBootstrap(session)
}

func (s *Service) deferUpperGetUserInfoBootstrapUntilHiddenProbe(session *gameSession) error {
	if err := s.prepareGetUserInfoPassiveAccountState(session); err != nil {
		return err
	}
	session.pendingCharacterRosterBootstrap = true
	s.logGameEvent(session, "game-upper-getuserinfo-roster-deferred",
		"source", "legacy_getuserinfo",
		"reason", "wait_for_charac_view_hidden_info_probe")
	return nil
}

func (s *Service) prepareGetUserInfoPassiveAccountState(session *gameSession) error {
	session.selectorAdventureInfoPending = false
	session.selectorAdventureInfoSlot = 0
	repositories, ok := s.repositoryGroup()
	if ok && repositories.Account != nil {
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		defer cancel()
		account, found, err := repositories.Account.Load(ctx, s.accountIDForSession(session))
		if err != nil {
			s.logGameEvent(session, "game-represent-account-name-state-load-failed",
				"account_id", s.accountIDForSession(session),
				"error", err)
			return err
		}
		name := ""
		if found {
			name = strings.TrimSpace(account.RepresentAccountName)
		}
		if name == "" {
			// The current client can surface this state packet as an account
			// protection/password-style modal over the selector.  GET_USERINFO
			// must stay a passive role-list response; represent/adventure-name
			// registration is handled only when the client explicitly submits one
			// of the represent-name commands.
			session.representAccountNamePending = false
			s.logGameEvent(session, "game-represent-account-name-state-passive-deferred",
				"account_id", s.accountIDForSession(session),
				"account_found", found,
				"source", "getuserinfo_before_roster",
				"reason", "do_not_open_registration_or_security_modal_during_getuserinfo")
		} else {
			if slot, ok := currentSelectorAdventureInfoSlot(account.Metadata); ok {
				session.selectorAdventureInfoPending = true
				session.selectorAdventureInfoSlot = uint16(slot)
			}
			s.logGameEvent(session, "game-represent-account-name-state-passive-deferred",
				"account_id", s.accountIDForSession(session),
				"account_found", found,
				"name_byte_len", len(name),
				"source", "getuserinfo_before_roster",
				"selector_adventure_info_pending", session.selectorAdventureInfoPending,
				"selector_adventure_info_slot", session.selectorAdventureInfoSlot,
				"reason", "registered_state_still_opens_current_client_registration_modal")
		}
	}
	return nil
}

func (s *Service) sendUpperGetUserInfoRosterBootstrap(session *gameSession) error {
	return s.sendUpperGetUserInfoRosterBootstrapEnvelope(session, "ordinary_upper")
}

func (s *Service) sendUpperGetUserInfoRosterBootstrapFixed16(session *gameSession) error {
	return s.sendUpperGetUserInfoRosterBootstrapEnvelope(session, "legacy_hidden_probe_fixed16_zero_prefix")
}

func (s *Service) sendUpperGetUserInfoRosterBootstrapEnvelope(session *gameSession, envelope string) error {
	body := s.buildUpperGetUserInfoRosterBody(session)
	s.logPacketEvent("game-upper-get-userinfo-bootstrap-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", "repository_roster_wstr",
		"envelope", envelope,
		"packet_count", 1,
		"body_len", len(body))
	if len(body) >= 15 {
		s.logGameEvent(session, "game-character-roster-prefix",
			"repository_count", body[1],
			"display_count", body[1],
			"selected_page", binary.LittleEndian.Uint16(body[7:9]),
			"entry_count", binary.LittleEndian.Uint16(body[13:15]),
			"prefix_len", 15)
	}
	switch envelope {
	case "legacy_hidden_probe_fixed16_zero_prefix":
		if err := s.sendGameUpperClass0Fixed16ZeroPrefix(session, uint16(dnfenum.UpperMsgCharacterRoster), body, "legacy_getuserinfo_hidden_probe_roster"); err != nil {
			return err
		}
	case "fixed15_character_list":
		if err := s.sendGameFixed15Route(
			session,
			byte(dnfenum.GameCmdNotice),
			uint16(dnfenum.GameTypeCharacterList),
			body,
			latestCharacterStateRoute,
		); err != nil {
			return err
		}
	default:
		if err := s.sendGameUpperSecondaryRaw(session, uint16(dnfenum.UpperMsgCharacterRoster), body); err != nil {
			return err
		}
	}
	noteEmptyRosterSlotProbe(session, body)
	// Current NoPack keeps the role-selector adventure name/level outside the
	// mode2 roster body. Do not append op1340 here: the client has not yet
	// acknowledged that the selector objects are ready, and the former
	// all-roster fan-out crashed the current executable. The verified op645
	// hidden-info probe flushes one remembered-slot snapshot later.
	return nil
}

func (s *Service) buildUpperGetUserInfoRosterBody(session *gameSession) []byte {
	repos, ok := s.repositoryGroup()
	accountID := s.accountIDForSession(session)
	if !ok || repos.Character == nil {
		s.logGameEvent(session, "game-upper-getuserinfo-roster-repository-missing", "account_id", accountID)
		return buildCSharpEmptyRosterBody()
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()

	characters, err := s.listCharacters(ctx, repos, accountID)
	if err != nil {
		s.logGameEvent(session, "game-upper-getuserinfo-roster-list-failed", "account_id", accountID, "error", err)
		return buildCSharpEmptyRosterBody()
	}
	// GET_USERINFO 是客户端请求触发的被动角色表；这里只从仓库构造 roster，不触发栏位扩展包。
	s.logGameEvent(session, "game-upper-getuserinfo-roster-built", "account_id", accountID, "count", len(characters))
	return buildCSharpRosterBody(characters)
}

func (s *Service) buildInitialLoginNotice(channel channelcatalog.Channel) []byte {
	var writer packetWriter
	// C# BuildInitialLoginNotice 只写旧首包字段。DOVE 抓到的 335 字节前缀
	// 是单次运行证据，不能作为所有频道/会话通用模板。
	writer.writeByte(1)
	writer.writeAsciiDstr(noticeChanName(channel))
	writer.writeInt32(0)
	writer.writeInt32(0)
	writer.writeByte(byte(s.channelAdvertiseID()))
	writer.writeByte(byte(channel.ID))
	writer.writeByte(0)
	writer.writeInt32(int(time.Now().Unix()))
	writer.writeInt32(1)
	writer.writeAsciiDstr(s.options.serverIP)
	writer.writeInt32(s.options.initialUDPPort1)
	writer.writeInt32(s.options.initialUDPPort2)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeInt32(s.options.commandCount)
	writer.writeInt32(s.options.notificationCount)
	return writer.bytes()
}

func noticeChanName(channel channelcatalog.Channel) string {
	return dnfenum.ChannelNamePrefix + strconv.Itoa(channel.ID)
}

func (s *Service) buildLatestUpperInitBody(_ *gameSession, request []byte) []byte {
	requestValue := uint16(0)
	if len(request) >= 2 {
		requestValue = binary.LittleEndian.Uint16(request[:2])
	}
	channelOrArea := requestValue
	if channelOrArea == 0 {
		channelOrArea = uint16(dnfenum.LoginChannelServerIndex)
	}

	var writer packetWriter
	writer.writeUint32(1)
	writer.writeUint32(0)
	writer.writeUint16(channelOrArea)
	writer.writeUint16(0)
	writer.writeUint16(0)
	writer.writeUint16(0)
	writer.writeByte(0)
	writer.writeUint32(0)

	// 最新客户端固定读取 30 行表项；0xFFFF 是跳过该行的哨兵。
	for range 30 {
		writer.writeUint16(0xffff)
		writer.writeUint32(0)
	}

	writer.writeUint32(0)
	writer.writeByte(2)
	writer.writeUint16(channelOrArea)
	writer.writeUint16(0)
	writer.writeByte(0)
	writer.writeUint16(0)
	writer.writeByte(0)
	return writer.bytes()
}

func (s *Service) buildLoginSuccess(channel channelcatalog.Channel) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeByte(1)
	writer.writeByte(channel.Type)
	writer.writeByte(1)
	writer.writeUint32(s.options.gameOuterToken)
	writer.writeAsciiDstr(s.options.serverIP)
	writer.writeInt32(channel.Port)
	writer.writeInt32(0)

	// Current NoPack upper_handle_msg1_game_endpoint reads this 24-byte account
	// state tail after the endpoint. Offsets 2 and 3 are useSecurityCard and
	// alreadyAuthentificatedSecurityCard. Keeping both true suppresses the
	// account-protection news modal without changing channel selection.
	var accountState [24]byte
	accountState[2] = 1
	accountState[3] = 1
	writer.writeBytes(accountState[:])
	return writer.bytes()
}

func buildAuctionServiceNotice(serviceType byte, state byte) []byte {
	return []byte{serviceType, state}
}

func zeroBody(count int) []byte {
	if count <= 0 {
		return nil
	}
	return make([]byte, count)
}
