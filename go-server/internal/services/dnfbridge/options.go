// options.go 负责 DNF bridge 启动配置和环境变量兼容。
// 这里只归一化协议开关和监听参数，不持有运行期玩家状态。
package dnfbridge

import (
	"net"
	"os"
	"strconv"
	"strings"

	"longheng.io/server/internal/platform/config"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

const (
	defaultChannelListen           = ":7001"
	defaultChannelInfoFile         = "data/dnf/channel_info.etc"
	defaultServerIP                = "127.0.0.1"
	defaultScriptVersion           = "59"
	channelInfoBodyLatest          = "latest"
	gameInitialModeNotice          = "notice"
	gameInitialModeLogin           = "login_success"
	gameInitialModeUpper           = "upper_endpoint"
	gameInitialModeNone            = "none"
	gamePreBootstrapNone           = "none"
	gamePreBootstrapGuard          = "guard_clear"
	gamePostBootstrapNone          = "none"
	gamePostBootstrapCheck         = "client_check"
	gamePostBootstrapRoster        = "roster_init"
	gameUpperHeaderChannel13       = "channel13"
	gameUpperHeaderServer16        = "server16"
	gameUpperBodyCodecAuto         = "auto"
	gameUpperBodyCodecPlain        = "plaintext"
	gameUpperClientBodyCodecPlain  = "plaintext"
	gameUpperClientBodyCodecProbe  = "probe"
	gameUpperClientBodyCodecNative = "native"
	gameDprotoModeLegacy           = "legacy_patch"
	gameDprotoModeNative           = "native"
	// DOVE 明文首包里 UDP 字段是 2311/2312；这里只作为 InitialLoginNotice 的兼容默认值。
	defaultInitialUDPPort1        = 2311
	defaultInitialUDPPort2        = 2312
	defaultCommandCount           = 1382
	defaultNotificationCount      = 1326
	defaultMaxPacketBytes         = 1048576
	defaultAccountPrefix          = "dnf:"
	defaultPVFPath                = "data/dnf/Script.pvf"
	defaultPartyUDPRelayPortStart = 30000
	defaultPartyUDPRelayPortCount = 64
)

type options struct {
	channelListen            string
	gameListenHost           string
	channelInfoFile          string
	channelServerID          int
	channelAdvertiseID       int
	channelInfoBodyMode      string
	serverIP                 string
	scriptVersion            string
	gameOuterToken           uint32
	gameInitialMode          string
	gamePreBootstrap         string
	gamePostBootstrap        string
	gameUpperHeader          string
	gameUpperBodyCodec       string
	gameUpperClientBodyCodec string
	gameDprotoMode           string
	initialUDPPort1          int
	initialUDPPort2          int
	commandCount             int
	notificationCount        int
	maxPacketBytes           int
	packetLogEnabled         bool
	packetLogPath            string
	accountID                string
	accountPrefix            string
	pvfPath                  string
	pvfMaxBytes              int64
	partyUDPRelayEnabled     bool
	partyUDPRelayPortStart   int
	partyUDPRelayPortCount   int
}

func optionsFromConfig(cfg config.ServiceConfig) options {
	opts := options{
		channelListen:   envString("DNFBRIDGE_CHANNEL_LISTEN", defaultChannelListen),
		gameListenHost:  strings.TrimSpace(os.Getenv("DNFBRIDGE_GAME_LISTEN_HOST")),
		channelInfoFile: firstEnv([]string{"DNFBRIDGE_CHANNEL_INFO_PATH", "DNF_CHANNEL_INFO_FILE"}, defaultChannelInfoFile),
		channelServerID: envIntAny([]string{"DNFBRIDGE_CHANNEL_SERVER_INDEX", "DNF_CHANNEL_SOURCE_SERVER_INDEX"}, 1),
		channelAdvertiseID: envIntAny(
			[]string{"DNFBRIDGE_CHANNEL_ADVERTISE_SERVER_INDEX", "DFO_CHANNEL_SERVER_INDEX"},
			-1,
		),
		channelInfoBodyMode: normalizeChannelInfoBodyMode(envString(
			"DNFBRIDGE_CHANNEL_INFO_BODY_MODE",
			channelInfoBodyLatest,
		)),
		serverIP:          chooseServerIP(cfg),
		scriptVersion:     envString("DNF_SCRIPT_VERSION", defaultScriptVersion),
		gameOuterToken:    envUint32Any([]string{"DNFBRIDGE_GAME_OUTER_TOKEN", "DFO_GAME_OUTER_TOKEN"}, 0),
		gameInitialMode:   normalizeGameInitialMode(envString("DNFBRIDGE_GAME_INITIAL_MODE", gameInitialModeNotice)),
		gamePreBootstrap:  normalizeGamePreBootstrap(envString("DNFBRIDGE_GAME_PRE_BOOTSTRAP", gamePreBootstrapNone)),
		gamePostBootstrap: normalizeGamePostBootstrap(envString("DNFBRIDGE_GAME_POST_BOOTSTRAP", gamePostBootstrapNone)),
		gameUpperHeader:   normalizeGameUpperHeader(envString("DNFBRIDGE_GAME_UPPER_HEADER", gameUpperHeaderChannel13)),
		gameUpperBodyCodec: normalizeGameUpperBodyCodec(envString(
			"DNFBRIDGE_GAME_UPPER_BODY_CODEC",
			gameUpperBodyCodecAuto,
		)),
		gameUpperClientBodyCodec: normalizeGameUpperClientBodyCodec(envString(
			"DNFBRIDGE_GAME_UPPER_CLIENT_BODY_CODEC",
			gameUpperClientBodyCodecPlain,
		)),
		gameDprotoMode:         normalizeGameDprotoMode(envString("DNFBRIDGE_DPROTO_MODE", gameDprotoModeLegacy)),
		initialUDPPort1:        envInt("DNF_INITIAL_UDP_PORT1", defaultInitialUDPPort1),
		initialUDPPort2:        envInt("DNF_INITIAL_UDP_PORT2", defaultInitialUDPPort2),
		commandCount:           envInt("DNF_COMMAND_PACKET_COUNT", defaultCommandCount),
		notificationCount:      envInt("DNF_NOTIFICATION_PACKET_COUNT", defaultNotificationCount),
		maxPacketBytes:         envInt("DNF_MAX_PACKET_BYTES", defaultMaxPacketBytes),
		packetLogEnabled:       envBool("DNFBRIDGE_PACKET_LOG", false),
		packetLogPath:          envString("DNFBRIDGE_PACKET_LOG_PATH", "logs/packet_log.txt"),
		accountID:              envString("DNFBRIDGE_ACCOUNT_ID", ""),
		accountPrefix:          envString("DNFBRIDGE_ACCOUNT_PREFIX", defaultAccountPrefix),
		pvfPath:                choosePVFPath(cfg),
		pvfMaxBytes:            choosePVFMaxBytes(cfg),
		partyUDPRelayEnabled:   envBool("DNFBRIDGE_PARTY_UDP_RELAY_ENABLED", true),
		partyUDPRelayPortStart: envInt("DNFBRIDGE_PARTY_UDP_RELAY_PORT_START", defaultPartyUDPRelayPortStart),
		partyUDPRelayPortCount: envInt("DNFBRIDGE_PARTY_UDP_RELAY_PORT_COUNT", defaultPartyUDPRelayPortCount),
	}
	if opts.maxPacketBytes <= 0 {
		opts.maxPacketBytes = defaultMaxPacketBytes
	}
	if opts.channelServerID <= 0 {
		opts.channelServerID = 1
	}
	if opts.channelAdvertiseID < 0 {
		opts.channelAdvertiseID = opts.channelServerID
	}
	if opts.partyUDPRelayPortStart < 1 || opts.partyUDPRelayPortStart > 65535 {
		opts.partyUDPRelayPortStart = defaultPartyUDPRelayPortStart
	}
	if opts.partyUDPRelayPortCount < 1 ||
		opts.partyUDPRelayPortCount > 1024 ||
		opts.partyUDPRelayPortStart+opts.partyUDPRelayPortCount-1 > 65535 {
		opts.partyUDPRelayPortCount = defaultPartyUDPRelayPortCount
	}
	return opts
}

func choosePVFPath(cfg config.ServiceConfig) string {
	for _, key := range []string{"DNFBRIDGE_PVF_PATH", "DNF_SCRIPT_PVF_PATH", "DNF_PVF_PATH"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(cfg.PVF.Path); value != "" {
		return value
	}
	return defaultPVFPath
}

func choosePVFMaxBytes(cfg config.ServiceConfig) int64 {
	for _, key := range []string{"DNFBRIDGE_PVF_MAX_BYTES", "DNF_SCRIPT_PVF_MAX_BYTES", "DNF_PVF_MAX_BYTES"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	if cfg.PVF.MaxBytes > 0 {
		return cfg.PVF.MaxBytes
	}
	return platformpvf.DefaultMaxBytes
}

func normalizeChannelInfoBodyMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", channelInfoBodyLatest, "csharp", "legacy", "legacy_csharp", "old_csharp":
		// IDA/MCP 与 86JP_protocol.log 复核后，最新客户端 0x12 明文必须带 server_count/server_id/count。
		// 旧 C# 的 2 字节保留位前缀会导致客户端把 0x000B0000 当 server_count 反复解析。
		return channelInfoBodyLatest
	default:
		return channelInfoBodyLatest
	}
}

func normalizeGameInitialMode(value string) string {
	// DOVE/C# 对齐：首包模式不再可配置，game 建连后固定主动发送 class=0 op=1。
	return gameInitialModeNotice
}

func normalizeGamePreBootstrap(value string) string {
	// C# 端 game 连接后只主动发首包；其余初始化包都由客户端 C2S 触发。
	// 旧探针开关保留为兼容配置名，但正常链路统一关闭，避免提前推送请求触发包。
	return gamePreBootstrapNone
}

func normalizeGamePostBootstrap(value string) string {
	// C# 行为证据：GET_USERINFO/SELECT_CHARACTER 后续包必须等客户端请求后再回。
	// 因此 post bootstrap 探针不再通过环境变量开启。
	return gamePostBootstrapNone
}

func normalizeGameUpperHeader(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", gameUpperHeaderChannel13, "13", "channel", "raw13":
		return gameUpperHeaderChannel13
	case gameUpperHeaderServer16, "16", "raw16":
		return gameUpperHeaderServer16
	default:
		return gameUpperHeaderChannel13
	}
}

func normalizeGameUpperBodyCodec(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", gameUpperBodyCodecAuto, "codec", "encoded", "latest":
		return gameUpperBodyCodecAuto
	case gameUpperBodyCodecPlain, "plain", "none", "off", "decodecopy":
		return gameUpperBodyCodecPlain
	default:
		return gameUpperBodyCodecAuto
	}
}

func normalizeGameUpperClientBodyCodec(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", gameUpperClientBodyCodecPlain, "plain", "none", "off", "disabled", "passthrough":
		return gameUpperClientBodyCodecPlain
	case gameUpperClientBodyCodecProbe, "detect", "log", "log_only", "log-only":
		return gameUpperClientBodyCodecProbe
	case gameUpperClientBodyCodecNative, "decode", "decoded", "server", "server_native":
		return gameUpperClientBodyCodecNative
	default:
		return gameUpperClientBodyCodecPlain
	}
}

func normalizeGameDprotoMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case gameDprotoModeNative, "native_dproto", "protected":
		return gameDprotoModeNative
	case "", gameDprotoModeLegacy, "legacy", "patch", "compat":
		return gameDprotoModeLegacy
	default:
		return gameDprotoModeLegacy
	}
}

func chooseServerIP(cfg config.ServiceConfig) string {
	for _, key := range []string{"DNFBRIDGE_SERVER_IP", "DNFBRIDGE_PUBLIC_HOST", "DFO_PUBLIC_SERVER_IP", "SERVER_IP"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	if host := hostFromAddr(cfg.Service.PublicAddr); host != "" {
		return host
	}
	return defaultServerIP
}

func hostFromAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(addr, "[]")
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func firstEnv(keys []string, fallback string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envIntAny(keys []string, fallback int) int {
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			continue
		}
		return envInt(key, fallback)
	}
	return fallback
}

func envUint32(key string, fallback uint32) uint32 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		parsed, err = parseFlexibleUint32(value)
	}
	if err != nil {
		return fallback
	}
	return uint32(parsed)
}

func envUint32Any(keys []string, fallback uint32) uint32 {
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			continue
		}
		return envUint32(key, fallback)
	}
	return fallback
}

func parseFlexibleUint32(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	trimmed := strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
	if len(trimmed) > 8 {
		// 启动参数里的 32 位 hex token 不是 uint32；调试时传入该值时只取前 4 字节作为 outer 校验候选。
		trimmed = trimmed[:8]
	}
	return strconv.ParseUint(trimmed, 16, 32)
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
