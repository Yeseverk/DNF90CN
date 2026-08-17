package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateInstance(t *testing.T) {
	cfg := validTestInstance()
	if err := validateInstance(cfg); err != nil {
		t.Fatalf("validateInstance(valid) error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*instanceConfig)
		want   string
	}{
		{
			name: "profile",
			mutate: func(cfg *instanceConfig) {
				cfg.Protocol.Profile = "unknown"
			},
			want: "protocol.profile",
		},
		{
			name: "absolute asset",
			mutate: func(cfg *instanceConfig) {
				cfg.Game.PVFPath = `C:\outside\Script.pvf`
			},
			want: "relative to runtime",
		},
		{
			name: "unsafe password",
			mutate: func(cfg *instanceConfig) {
				cfg.Database.Password = "bad password"
			},
			want: "database.password",
		},
		{
			name: "non-root portable database",
			mutate: func(cfg *instanceConfig) {
				cfg.Database.User = "dnf"
			},
			want: "must be root",
		},
		{
			name: "non-loopback channel",
			mutate: func(cfg *instanceConfig) {
				cfg.Server.ChannelListen = "0.0.0.0:7001"
			},
			want: "127.0.0.1",
		},
		{
			name: "public advertise address",
			mutate: func(cfg *instanceConfig) {
				cfg.Server.AdvertiseIP = "8.8.8.8"
			},
			want: "private LAN",
		},
		{
			name: "loopback advertise address",
			mutate: func(cfg *instanceConfig) {
				cfg.Server.AdvertiseIP = "127.0.0.1"
			},
			want: "not loopback",
		},
		{
			name: "wrong outer token",
			mutate: func(cfg *instanceConfig) {
				cfg.Protocol.GameOuterToken = "00112233445566778899aabbccddeeff"
			},
			want: "compatibility profile",
		},
		{
			name: "wrong channel advertise server",
			mutate: func(cfg *instanceConfig) {
				cfg.Protocol.ChannelAdvertiseServerIndex = 1
			},
			want: "advertise server 0",
		},
		{
			name: "wrong initial game port",
			mutate: func(cfg *instanceConfig) {
				cfg.Client.InitialGamePort = 10011
			},
			want: "must be 0",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := cfg
			test.mutate(&invalid)
			err := validateInstance(invalid)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateInstance() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestGenerateRuntimeConfigs(t *testing.T) {
	root := t.TempDir()
	paths := newProjectPaths(root)
	if err := os.MkdirAll(filepath.Dir(paths.channelAsset), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.channelAsset, []byte("channel fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := validTestInstance()
	if err := generateRuntimeConfigs(paths, cfg); err != nil {
		t.Fatalf("generateRuntimeConfigs() error = %v", err)
	}

	required := []string{
		filepath.Join(paths.runtimeConfigs, "dnfbridge.toml"),
		filepath.Join(paths.runtimeConfigs, "dnf", "logic.toml"),
		filepath.Join(paths.runtimeConfigs, "servergroup", "plan.json"),
		paths.mysqlConfig,
		filepath.Join(paths.runtimeData, "channel_info.etc"),
	}
	for _, path := range required {
		if !isRegularFile(path) {
			t.Errorf("generated file missing: %s", path)
		}
	}
	if isRegularFile(filepath.Join(paths.runtimeConfig, "environment.ps1")) {
		t.Fatal("native controller must not generate PowerShell")
	}
	if isRegularFile(filepath.Join(paths.runtimeConfig, "docker.env")) {
		t.Fatal("portable controller must not generate Docker state")
	}

	serviceData, err := os.ReadFile(filepath.Join(paths.runtimeConfigs, "dnfbridge.toml"))
	if err != nil {
		t.Fatal(err)
	}
	service := string(serviceData)
	for _, want := range []string{
		`public_addr = "192.168.1.113:7001"`,
		`private_addr = "127.0.0.1:18111"`,
		`shard_id = 9999`,
		`path = "data/dnf/Script.pvf"`,
	} {
		if !strings.Contains(service, want) {
			t.Errorf("service config missing %q", want)
		}
	}

	var plan serverGroupPlan
	planData, err := os.ReadFile(filepath.Join(paths.runtimeConfigs, "servergroup", "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(planData, &plan); err != nil {
		t.Fatalf("decode generated plan: %v", err)
	}
	if got := plan.Routes[0].Meta["dnf.repository.write_database"]; got != cfg.Database.Name {
		t.Fatalf("write database = %q, want %q", got, cfg.Database.Name)
	}
	if got := plan.Routes[0].Meta["dnf.repository.read_database"]; got != cfg.Database.Name {
		t.Fatalf("read database = %q, want %q", got, cfg.Database.Name)
	}
}

func TestChoosePrivateLANIPv4(t *testing.T) {
	candidates := []lanIPv4Candidate{
		{Address: "192.168.56.1", InterfaceName: "VirtualBox Host-Only", InterfaceIndex: 2},
		{Address: "10.12.0.5", InterfaceName: "Ethernet", InterfaceIndex: 8},
		{Address: "127.0.0.1", InterfaceName: "Loopback", InterfaceIndex: 1},
		{Address: "169.254.1.2", InterfaceName: "Disconnected", InterfaceIndex: 9},
	}

	got, err := choosePrivateLANIPv4(candidates, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.12.0.5" {
		t.Fatalf("fallback LAN IPv4 = %q, want physical interface 10.12.0.5", got)
	}

	got, err = choosePrivateLANIPv4(candidates, net.ParseIP("192.168.56.1"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.168.56.1" {
		t.Fatalf("route-selected LAN IPv4 = %q, want 192.168.56.1", got)
	}

	if _, err := choosePrivateLANIPv4([]lanIPv4Candidate{
		{Address: "127.0.0.1", InterfaceName: "Loopback", InterfaceIndex: 1},
		{Address: "8.8.8.8", InterfaceName: "Invalid", InterfaceIndex: 2},
	}, nil); err == nil {
		t.Fatal("choosePrivateLANIPv4 accepted a candidate set without a private LAN IPv4")
	}
}

func TestValidateAssetsAndClient(t *testing.T) {
	root := t.TempDir()
	paths := newProjectPaths(root)
	runtimeAsset := filepath.Join(paths.runtimeData, "fixture.bin")
	if err := os.MkdirAll(filepath.Dir(runtimeAsset), 0o755); err != nil {
		t.Fatal(err)
	}
	assetData := []byte("asset")
	if err := os.WriteFile(runtimeAsset, assetData, 0o644); err != nil {
		t.Fatal(err)
	}
	assetDigest, err := fileSHA256(runtimeAsset)
	if err != nil {
		t.Fatal(err)
	}
	assetManifestData, err := json.Marshal(assetManifest{
		SchemaVersion: 1,
		Files: []assetManifestEntry{{
			Path:     "data/dnf/fixture.bin",
			Required: true,
			Size:     int64(len(assetData)),
			SHA256:   assetDigest,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(paths.assetManifest, assetManifestData, 0o644); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := validateAssets(paths, &output); err != nil {
		t.Fatalf("validateAssets() error = %v", err)
	}

	clientRoot := filepath.Join(root, "client")
	if err := os.MkdirAll(clientRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	clientFiles := []string{"DNF.exe", "ijl15.dll", "ijl15_real.dll", "90CN.dll", "90CNLua.dll"}
	manifest := clientManifest{SchemaVersion: 1, Profile: "90cn-decode-bypass-v1"}
	for index, name := range clientFiles {
		path := filepath.Join(clientRoot, name)
		data := []byte{byte(index + 1), byte(index + 9)}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Files = append(manifest.Files, clientManifestEntry{
			Names:    []string{name},
			Required: true,
			SHA256:   digest,
		})
	}
	clientManifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(paths.clientManifest, clientManifestData, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := validTestInstance()
	cfg.Client.Directory = clientRoot
	gotRoot, executable, err := validateClient(paths, cfg, "", &output)
	if err != nil {
		t.Fatalf("validateClient() error = %v", err)
	}
	if gotRoot != clientRoot {
		t.Fatalf("client root = %q, want %q", gotRoot, clientRoot)
	}
	if filepath.Base(executable) != "DNF.exe" {
		t.Fatalf("client executable = %q", executable)
	}
}

func TestMergeEnvironmentOverridesCaseInsensitively(t *testing.T) {
	got := mergeEnvironment(
		[]string{"Path=old", "KEEP=value"},
		map[string]string{"PATH": "new", "ADDED": "yes"},
	)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "Path=old") {
		t.Fatalf("old environment value survived: %q", joined)
	}
	for _, want := range []string{"PATH=new", "KEEP=value", "ADDED=yes"} {
		if !strings.Contains(joined, want) {
			t.Errorf("environment missing %q: %q", want, joined)
		}
	}
}

func TestRuntimeEnvironmentScrubsInheritedProtocolOverrides(t *testing.T) {
	t.Setenv("DNFBRIDGE_GAME_INITIAL_MODE", "inherited-bad-mode")
	t.Setenv("DFO_GAME_OUTER_TOKEN", "inherited-bad-token")
	t.Setenv("DNF_UNRELATED_STALE_VALUE", "inherited")

	cfg := validTestInstance()
	cfg.Server.AdvertiseIP = "192.168.1.113"
	joined := strings.Join(runtimeEnvironment(cfg), "\n")
	for _, forbidden := range []string{
		"inherited-bad-mode",
		"inherited-bad-token",
		"DNF_UNRELATED_STALE_VALUE",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("runtime environment retained inherited DNF override %q", forbidden)
		}
	}
	for _, required := range []string{
		"DNFBRIDGE_GAME_INITIAL_MODE=notice",
		"DNFBRIDGE_GAME_PRE_BOOTSTRAP=none",
		"DNFBRIDGE_GAME_POST_BOOTSTRAP=none",
		"DNFBRIDGE_DPROTO_MODE=legacy_patch",
		"DNFBRIDGE_CHANNEL_SERVER_INDEX=1",
		"DNFBRIDGE_CHANNEL_ADVERTISE_SERVER_INDEX=0",
		"DNFBRIDGE_SERVER_IP=192.168.1.113",
		"DNFBRIDGE_GAME_LISTEN_HOST=192.168.1.113",
	} {
		if !strings.Contains(joined, required) {
			t.Errorf("runtime environment missing locked value %q", required)
		}
	}
}

func TestClientLaunchArgumentUsesBootstrapListenHost(t *testing.T) {
	cfg := validTestInstance()
	cfg.Server.AdvertiseIP = "192.168.1.113"
	cfg.Server.ChannelListen = "127.0.0.1:7001"

	got, err := clientLaunchArgument(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := "99?127.0.0.1?7001?0?de509f65e9ccaae621cb7278fc2b8e6c?01?1?0?0?0?0?1?9n2b1c8r3w7y?0?0?19847"
	if got != want {
		t.Fatalf("client launch argument = %q, want %q", got, want)
	}
}

func validTestInstance() instanceConfig {
	return instanceConfig{
		SchemaVersion:  1,
		InstallationID: "inst_test",
		Mode:           "local-single-account",
		Server: serverConfig{
			AdvertiseIP:   "192.168.1.113",
			ChannelListen: "127.0.0.1:7001",
			AdminListen:   "127.0.0.1:18111",
			AdminToken:    "adm_test",
			AccountID:     "dnf:1",
		},
		Database: databaseConfig{
			Mode:     "portable",
			Host:     "127.0.0.1",
			Port:     13306,
			Name:     "dnf_local",
			User:     "root",
			Password: "db_test",
		},
		Game: gameConfig{
			ShardID:         "9999",
			PVFPath:         "data/dnf/Script.pvf",
			PVFMaxBytes:     536870912,
			ChannelInfoPath: "data/dnf/channel_info.etc",
		},
		Protocol: protocolConfig{
			Profile:                     "90cn-decode-bypass-v1",
			GameUpperHeader:             "server16",
			GameUpperBodyCodec:          "plaintext",
			GameUpperClientBodyCodec:    "plaintext",
			GameOuterToken:              "de509f65e9ccaae621cb7278fc2b8e6c",
			ChannelServerIndex:          1,
			ChannelAdvertiseServerIndex: 0,
		},
		Build:  buildConfig{GoExecutable: "go"},
		Client: clientConfig{InitialGamePort: 0, HookCreate: true},
	}
}
