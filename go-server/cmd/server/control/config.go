package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func generateRuntimeConfigs(paths projectPaths, cfg instanceConfig) error {
	if err := paths.ensureDirectories(); err != nil {
		return err
	}
	channelData, err := os.ReadFile(paths.channelAsset)
	if err != nil {
		return fmt.Errorf("read bundled channel_info.etc: %w", err)
	}
	if err := writeFile(filepath.Join(paths.runtimeData, "channel_info.etc"), channelData, 0o644); err != nil {
		return err
	}

	adminPort, err := listenPort(cfg.Server.AdminListen)
	if err != nil {
		return err
	}
	channelPort, err := listenPort(cfg.Server.ChannelListen)
	if err != nil {
		return err
	}
	serviceConfig := renderServiceConfig(cfg, adminPort, channelPort)
	logicConfig := renderLogicConfig(cfg)
	planConfig, err := renderServerGroupPlan(cfg)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(paths.runtimeConfigs, "dnfbridge.toml"), []byte(serviceConfig), 0o600); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(paths.runtimeConfigs, "dnf", "logic.toml"), []byte(logicConfig), 0o600); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(paths.runtimeConfigs, "servergroup", "plan.json"), planConfig, 0o600); err != nil {
		return err
	}

	if err := writeFile(paths.mysqlConfig, []byte(renderMySQLConfig(paths, cfg)), 0o600); err != nil {
		return err
	}
	return nil
}

func validateRunningDatabaseConfig(
	paths projectPaths,
	cfg instanceConfig,
	state processState,
) error {
	expected := map[string][]byte{
		paths.mysqlConfig: []byte(renderMySQLConfig(paths, cfg)),
		filepath.Join(
			paths.runtimeConfigs,
			"dnf",
			"logic.toml",
		): []byte(renderLogicConfig(cfg)),
	}
	for configPath, expectedData := range expected {
		currentData, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf(
				"portable MySQL is running but its active runtime configuration cannot be verified; run STOP.bat before changing configuration: %w",
				err,
			)
		}
		if !bytes.Equal(currentData, expectedData) {
			return fmt.Errorf(
				"portable MySQL is running and database configuration has changed; run STOP.bat before applying the new configuration",
			)
		}
	}
	currentSHA256, err := databaseRuntimeConfigSHA256(paths)
	if err != nil {
		return err
	}
	if state.RuntimeConfigSHA256 != "" &&
		!strings.EqualFold(currentSHA256, state.RuntimeConfigSHA256) {
		return fmt.Errorf(
			"portable MySQL is running but its runtime configuration files changed after launch; run STOP.bat before applying configuration changes",
		)
	}
	return nil
}

func databaseRuntimeConfigSHA256(paths projectPaths) (string, error) {
	configPaths := []struct {
		name string
		path string
	}{
		{name: "mysql.ini", path: paths.mysqlConfig},
		{
			name: "dnf/logic.toml",
			path: filepath.Join(paths.runtimeConfigs, "dnf", "logic.toml"),
		},
	}
	hash := sha256.New()
	for _, config := range configPaths {
		data, err := os.ReadFile(config.path)
		if err != nil {
			return "", fmt.Errorf("read database runtime configuration: %w", err)
		}
		_, _ = hash.Write([]byte(config.name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	return fmt.Sprintf("%X", hash.Sum(nil)), nil
}

func desiredServerRuntimeConfigSHA256(cfg instanceConfig) (string, error) {
	adminPort, err := listenPort(cfg.Server.AdminListen)
	if err != nil {
		return "", err
	}
	channelPort, err := listenPort(cfg.Server.ChannelListen)
	if err != nil {
		return "", err
	}
	plan, err := renderServerGroupPlan(cfg)
	if err != nil {
		return "", err
	}
	identity, err := json.Marshal(struct {
		Server   serverConfig   `json:"server"`
		Database databaseConfig `json:"database"`
		Game     gameConfig     `json:"game"`
		Protocol protocolConfig `json:"protocol"`
	}{
		Server:   cfg.Server,
		Database: cfg.Database,
		Game:     cfg.Game,
		Protocol: cfg.Protocol,
	})
	if err != nil {
		return "", fmt.Errorf("encode server runtime identity: %w", err)
	}
	return digestNamedRuntimeConfig([]namedRuntimeConfig{
		{name: "dnfbridge.toml", data: []byte(renderServiceConfig(cfg, adminPort, channelPort))},
		{name: "dnf/logic.toml", data: []byte(renderLogicConfig(cfg))},
		{name: "servergroup/plan.json", data: plan},
		{name: "runtime-identity.json", data: identity},
	}), nil
}

func currentServerRuntimeConfigSHA256(
	paths projectPaths,
	cfg instanceConfig,
) (string, error) {
	files := []struct {
		name string
		path string
	}{
		{name: "dnfbridge.toml", path: filepath.Join(paths.runtimeConfigs, "dnfbridge.toml")},
		{name: "dnf/logic.toml", path: filepath.Join(paths.runtimeConfigs, "dnf", "logic.toml")},
		{
			name: "servergroup/plan.json",
			path: filepath.Join(paths.runtimeConfigs, "servergroup", "plan.json"),
		},
	}
	configs := make([]namedRuntimeConfig, 0, len(files)+1)
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			return "", fmt.Errorf("read active server runtime configuration: %w", err)
		}
		configs = append(configs, namedRuntimeConfig{name: file.name, data: data})
	}
	identity, err := json.Marshal(struct {
		Server   serverConfig   `json:"server"`
		Database databaseConfig `json:"database"`
		Game     gameConfig     `json:"game"`
		Protocol protocolConfig `json:"protocol"`
	}{
		Server:   cfg.Server,
		Database: cfg.Database,
		Game:     cfg.Game,
		Protocol: cfg.Protocol,
	})
	if err != nil {
		return "", fmt.Errorf("encode active server runtime identity: %w", err)
	}
	configs = append(
		configs,
		namedRuntimeConfig{name: "runtime-identity.json", data: identity},
	)
	return digestNamedRuntimeConfig(configs), nil
}

type namedRuntimeConfig struct {
	name string
	data []byte
}

func digestNamedRuntimeConfig(configs []namedRuntimeConfig) string {
	hash := sha256.New()
	for _, config := range configs {
		_, _ = hash.Write([]byte(config.name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(config.data)
		_, _ = hash.Write([]byte{0})
	}
	return fmt.Sprintf("%X", hash.Sum(nil))
}

func renderServiceConfig(cfg instanceConfig, adminPort, channelPort int) string {
	publicAddress := cfg.Server.AdvertiseIP + ":" + strconv.Itoa(channelPort)
	privateAddress := "127.0.0.1:" + strconv.Itoa(adminPort)
	return fmt.Sprintf(`[service]
name = "dnfbridge"
node_id = "dnfbridge-local-1"
environment = "local"
public_addr = %s
private_addr = %s

[cluster]
id = "local"
gid = 2
virtual_gid = 2
shard_id = %s
zone = "local"
version = "local"
data_version = "local"

[admin]
listen = %s
token = %s

[metrics]
prometheus_enabled = true
require_admin_token = false

[pvf]
enabled = true
path = %s
max_bytes = %d
preload_chunks = false

[server_group]
enabled = true
store_kind = "file"
plan_file = "configs/servergroup/plan.json"

[topology]
enabled = false

[idempotency]
kind = "memory"

[registry]
kind = "memory"

[bus]
kind = "memory"

[cache]
kind = "memory"

[lock]
kind = "memory"

[presence]
kind = "memory"

[worker]
size = 8
queue = 256

[profile_store]
store_kind = "memory"
`,
		tomlString(publicAddress),
		tomlString(privateAddress),
		cfg.Game.ShardID,
		tomlString(cfg.Server.AdminListen),
		tomlString(cfg.Server.AdminToken),
		tomlString(filepath.ToSlash(cfg.Game.PVFPath)),
		cfg.Game.PVFMaxBytes,
	)
}

func renderLogicConfig(cfg instanceConfig) string {
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4,utf8",
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.Name,
	)
	return fmt.Sprintf(`[repository]
enabled = true
mysql_dsn = %s
mysql_max_open_conns = 32
mysql_max_idle_conns = 8
mysql_conn_max_lifetime_seconds = 300
shard_id = %s
table_prefix = "dnf"
server_group_plan_file = "configs/servergroup/plan.json"
auto_create_schema = true
csharp_legacy_schema = true
create_databases = false
`,
		tomlString(dsn),
		tomlString(cfg.Game.ShardID),
	)
}

func renderServerGroupPlan(cfg instanceConfig) ([]byte, error) {
	plan := serverGroupPlan{
		Version: "dnf90-local-v1",
		Shards: []serverGroupShard{{
			ID:           cfg.Game.ShardID,
			GroupID:      "logic-local-1",
			State:        "open",
			OpenAt:       "2026-01-01T00:00:00Z",
			PublicOpenAt: "2026-01-01T00:00:00Z",
		}},
		Groups: []serverGroup{{
			ID:       "logic-local-1",
			Service:  "logic",
			MemberID: "logic-local-1",
			State:    "open",
		}},
		Routes: []serverGroupRoute{{
			Feature: "dnf_repository",
			ShardID: cfg.Game.ShardID,
			GroupID: "logic-local-1",
			State:   "open",
			Meta: map[string]string{
				"dnf.repository.write_database": cfg.Database.Name,
				"dnf.repository.read_database":  cfg.Database.Name,
			},
		}},
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode server-group plan: %w", err)
	}
	return append(data, '\n'), nil
}

func renderMySQLConfig(paths projectPaths, cfg instanceConfig) string {
	return fmt.Sprintf(`[mysqld]
basedir=%s
datadir=%s
port=%d
bind-address=127.0.0.1
mysqlx=0
skip-log-bin
local-infile=OFF
symbolic-links=0
character-set-server=utf8mb4
collation-server=utf8mb4_0900_ai_ci
max-connections=128
innodb-flush-log-at-trx-commit=1
log-error=%s
pid-file=%s

[client]
host=127.0.0.1
port=%d
protocol=TCP
default-character-set=utf8mb4
`,
		mysqlOptionValue(paths.mysqlServer),
		mysqlOptionValue(paths.mysqlData),
		cfg.Database.Port,
		mysqlOptionValue(filepath.Join(paths.runtimeLogs, "mysql-error.log")),
		mysqlOptionValue(paths.mysqlPIDFile),
		cfg.Database.Port,
	)
}

func mysqlOptionValue(value string) string {
	value = filepath.ToSlash(value)
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", "", "\n", "")
	return `"` + replacer.Replace(value) + `"`
}

func runtimeEnvironment(cfg instanceConfig) []string {
	values := map[string]string{
		"LONGHENG_DNF_CONFIG":                      "configs/dnf/logic.toml",
		"DNFBRIDGE_SERVER_IP":                      cfg.Server.AdvertiseIP,
		"DNFBRIDGE_CHANNEL_LISTEN":                 cfg.Server.ChannelListen,
		"DNFBRIDGE_GAME_LISTEN_HOST":               cfg.Server.AdvertiseIP,
		"DNFBRIDGE_PARTY_UDP_RELAY_ENABLED":        "true",
		"DNFBRIDGE_PARTY_UDP_RELAY_PORT_START":     strconv.Itoa(cfg.Server.PartyUDPRelayPortStart),
		"DNFBRIDGE_PARTY_UDP_RELAY_PORT_COUNT":     strconv.Itoa(cfg.Server.PartyUDPRelayPortCount),
		"DNFBRIDGE_CHANNEL_INFO_PATH":              filepath.ToSlash(cfg.Game.ChannelInfoPath),
		"DNFBRIDGE_CHANNEL_INFO_BODY_MODE":         "latest",
		"DNFBRIDGE_PVF_PATH":                       filepath.ToSlash(cfg.Game.PVFPath),
		"DNFBRIDGE_ACCOUNT_ID":                     cfg.Server.AccountID,
		"DNFBRIDGE_PACKET_LOG":                     strconv.FormatBool(cfg.Server.PacketLog),
		"DNFBRIDGE_PACKET_LOG_PATH":                "logs/packet_log.txt",
		"DNFBRIDGE_GAME_INITIAL_MODE":              "notice",
		"DNFBRIDGE_GAME_PRE_BOOTSTRAP":             "none",
		"DNFBRIDGE_GAME_POST_BOOTSTRAP":            "none",
		"DNFBRIDGE_GAME_UPPER_HEADER":              cfg.Protocol.GameUpperHeader,
		"DNFBRIDGE_GAME_UPPER_BODY_CODEC":          cfg.Protocol.GameUpperBodyCodec,
		"DNFBRIDGE_GAME_UPPER_CLIENT_BODY_CODEC":   cfg.Protocol.GameUpperClientBodyCodec,
		"DNFBRIDGE_GAME_OUTER_TOKEN":               cfg.Protocol.GameOuterToken,
		"DNFBRIDGE_DPROTO_MODE":                    "legacy_patch",
		"DNFBRIDGE_CHANNEL_SERVER_INDEX":           strconv.Itoa(cfg.Protocol.ChannelServerIndex),
		"DNFBRIDGE_CHANNEL_ADVERTISE_SERVER_INDEX": strconv.Itoa(cfg.Protocol.ChannelAdvertiseServerIndex),
		"DNF_SCRIPT_VERSION":                       "59",
	}
	return mergeEnvironment(cleanRuntimeBaseEnvironment(os.Environ()), values)
}

func cleanRuntimeBaseEnvironment(base []string) []string {
	result := make([]string, 0, len(base))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		normalized := strings.ToUpper(strings.TrimSpace(key))
		if normalized == "LONGHENG_DNF_CONFIG" ||
			strings.HasPrefix(normalized, "DNFBRIDGE_") ||
			strings.HasPrefix(normalized, "DNF_") ||
			strings.HasPrefix(normalized, "DFO_") {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func mergeEnvironment(base []string, overrides map[string]string) []string {
	merged := make(map[string]string, len(base)+len(overrides))
	names := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		normalized := strings.ToUpper(key)
		merged[normalized] = value
		names[normalized] = key
	}
	for key, value := range overrides {
		normalized := strings.ToUpper(key)
		merged[normalized] = value
		names[normalized] = key
	}
	result := make([]string, 0, len(merged))
	for normalized, value := range merged {
		result = append(result, names[normalized]+"="+value)
	}
	return result
}

func tomlString(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\r", `\r`,
		"\n", `\n`,
		"\t", `\t`,
	)
	return `"` + replacer.Replace(value) + `"`
}
