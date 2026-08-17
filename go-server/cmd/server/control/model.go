package main

import (
	"encoding/json"
	"time"
)

type instanceConfig struct {
	SchemaVersion  int             `json:"schemaVersion"`
	InstallationID string          `json:"installationId"`
	Mode           string          `json:"mode"`
	Server         serverConfig    `json:"server"`
	Database       databaseConfig  `json:"database"`
	LegacyRedis    json.RawMessage `json:"redis,omitempty"`
	Game           gameConfig      `json:"game"`
	Protocol       protocolConfig  `json:"protocol"`
	Build          buildConfig     `json:"build"`
	Client         clientConfig    `json:"client"`
}

type serverConfig struct {
	AdvertiseIP            string `json:"advertiseIp"`
	ChannelListen          string `json:"channelListen"`
	AdminListen            string `json:"adminListen"`
	AdminToken             string `json:"adminToken"`
	AccountID              string `json:"accountId"`
	PacketLog              bool   `json:"packetLog"`
	PartyUDPRelayPortStart int    `json:"partyUdpRelayPortStart"`
	PartyUDPRelayPortCount int    `json:"partyUdpRelayPortCount"`
}

type databaseConfig struct {
	Mode     string `json:"mode"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"`
}

type gameConfig struct {
	ShardID         string `json:"shardId"`
	PVFPath         string `json:"pvfPath"`
	PVFMaxBytes     int64  `json:"pvfMaxBytes"`
	ChannelInfoPath string `json:"channelInfoPath"`
}

type protocolConfig struct {
	Profile                     string `json:"profile"`
	GameUpperHeader             string `json:"gameUpperHeader"`
	GameUpperBodyCodec          string `json:"gameUpperBodyCodec"`
	GameUpperClientBodyCodec    string `json:"gameUpperClientBodyCodec"`
	GameOuterToken              string `json:"gameOuterToken"`
	ChannelServerIndex          int    `json:"channelServerIndex"`
	ChannelAdvertiseServerIndex int    `json:"channelAdvertiseServerIndex"`
}

type buildConfig struct {
	GoExecutable string `json:"goExecutable"`
}

type clientConfig struct {
	Directory       string `json:"directory"`
	InitialGamePort int    `json:"initialGamePort"`
	HookCreate      bool   `json:"hookCreate"`
}

type assetManifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Files         []assetManifestEntry `json:"files"`
}

type assetManifestEntry struct {
	Path     string `json:"path"`
	Required bool   `json:"required"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type clientManifest struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Profile       string                `json:"profile"`
	Files         []clientManifestEntry `json:"files"`
}

type clientManifestEntry struct {
	Names    []string `json:"names"`
	Required bool     `json:"required"`
	SHA256   string   `json:"sha256"`
}

type processState struct {
	PID                 int       `json:"pid"`
	StartedAt           time.Time `json:"startedAt"`
	ProcessCreatedAt    time.Time `json:"processCreatedAt"`
	Executable          string    `json:"executable"`
	ExecutableSHA256    string    `json:"executableSha256"`
	InstallationID      string    `json:"installationId"`
	ServicePort         int       `json:"servicePort,omitempty"`
	RuntimeConfigSHA256 string    `json:"runtimeConfigSha256,omitempty"`
	LauncherPID         int       `json:"launcherPid,omitempty"`
	LauncherCreatedAt   time.Time `json:"launcherCreatedAt,omitempty"`
	DatabaseHost        string    `json:"databaseHost,omitempty"`
	DatabaseName        string    `json:"databaseName,omitempty"`
	DatabaseUser        string    `json:"databaseUser,omitempty"`
	DatabasePassword    string    `json:"databasePassword,omitempty"`
	Stdout              string    `json:"stdout"`
	Stderr              string    `json:"stderr"`
}

type assetState struct {
	GeneratedAt time.Time          `json:"generatedAt"`
	Files       []assetStateRecord `json:"files"`
}

type assetStateRecord struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type portableMySQLManifest struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Product       string                      `json:"product"`
	Version       string                      `json:"version"`
	Platform      string                      `json:"platform"`
	SourceURL     string                      `json:"sourceUrl"`
	Archive       portableMySQLArchive        `json:"archive"`
	Files         []portableMySQLManifestFile `json:"files"`
}

type portableMySQLArchive struct {
	Path   string `json:"path"`
	Root   string `json:"root"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type portableMySQLManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type appLocalRuntimeManifest struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Product       string                      `json:"product"`
	Version       string                      `json:"version"`
	Platform      string                      `json:"platform"`
	SourceURL     string                      `json:"sourceUrl"`
	Files         []portableMySQLManifestFile `json:"files"`
}

type portableMySQLInstallState struct {
	SchemaVersion int       `json:"schemaVersion"`
	Product       string    `json:"product"`
	Version       string    `json:"version"`
	Platform      string    `json:"platform"`
	ArchiveSHA256 string    `json:"archiveSha256"`
	InstalledAt   time.Time `json:"installedAt"`
}

type portableMySQLDataState struct {
	SchemaVersion  int       `json:"schemaVersion"`
	InstallationID string    `json:"installationId"`
	Phase          string    `json:"phase"`
	DataDirectory  string    `json:"dataDirectory"`
	MysqldSHA256   string    `json:"mysqldSha256"`
	AutoCNFSHA256  string    `json:"autoCnfSha256,omitempty"`
	Database       string    `json:"database,omitempty"`
	InitializedAt  time.Time `json:"initializedAt"`
	ReadyAt        time.Time `json:"readyAt,omitempty"`
}

type binaryInstallState struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Entries       []binaryInstallStateEntry `json:"entries"`
}

type binaryInstallStateEntry struct {
	Destination     string `json:"destination"`
	Temporary       string `json:"temporary"`
	Backup          string `json:"backup"`
	HadOriginal     bool   `json:"hadOriginal"`
	OriginalSHA256  string `json:"originalSha256,omitempty"`
	CandidateSHA256 string `json:"candidateSha256"`
}

type serverGroupPlan struct {
	Version string             `json:"version"`
	Shards  []serverGroupShard `json:"shards"`
	Groups  []serverGroup      `json:"groups"`
	Routes  []serverGroupRoute `json:"routes"`
}

type serverGroupShard struct {
	ID           string `json:"id"`
	GroupID      string `json:"group_id"`
	State        string `json:"state"`
	OpenAt       string `json:"open_at"`
	PublicOpenAt string `json:"public_open_at"`
}

type serverGroup struct {
	ID       string `json:"id"`
	Service  string `json:"service"`
	MemberID string `json:"member_id"`
	State    string `json:"state"`
}

type serverGroupRoute struct {
	Feature string            `json:"feature"`
	ShardID string            `json:"shard_id"`
	GroupID string            `json:"group_id"`
	State   string            `json:"state"`
	Meta    map[string]string `json:"meta"`
}

type checkOptions struct {
	skipDatabase bool
	skipPorts    bool
	checkClient  bool
}
