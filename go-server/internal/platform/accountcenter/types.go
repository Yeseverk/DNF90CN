package accountcenter

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidIdentity       = errors.New("account center identity is invalid")
	ErrAccountNotFound       = errors.New("account center account not found")
	ErrIdentityConflict      = errors.New("account center identity is already bound to another account")
	ErrAccountBanned         = errors.New("account center account is banned")
	ErrAllowListRequired     = errors.New("account center allowlist is required")
	ErrShardNotFound         = errors.New("account center shard not found")
	ErrShardClosed           = errors.New("account center shard is closed")
	ErrGateUnavailable       = errors.New("account center gate is unavailable")
	ErrLoginTokenUnavailable = errors.New("account center login token is unavailable")
	ErrStoreUnavailable      = errors.New("account center store is unavailable")
)

const (
	IdentityDevice  = "device"
	IdentityEmail   = "email"
	IdentityOIDC    = "oidc"
	IdentityChannel = "channel"
)

const (
	ShardOpen        = "open"
	ShardMaintaining = "maintaining"
	ShardClosed      = "closed"
)

type Identity struct {
	Kind       string            `json:"kind"`
	Issuer     string            `json:"issuer,omitempty"`
	Subject    string            `json:"subject"`
	Email      string            `json:"email,omitempty"`
	Channel    string            `json:"channel,omitempty"`
	DeviceID   string            `json:"device_id,omitempty"`
	VerifiedAt time.Time         `json:"verified_at,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type Account struct {
	ID        string            `json:"id"`
	NumID     int64             `json:"num_id,omitempty"`
	Bindings  []Identity        `json:"bindings,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type LoginRequest struct {
	Identity         Identity          `json:"identity"`
	PreferredShardID string            `json:"preferred_shard_id,omitempty"`
	RequireAllowList bool              `json:"require_allowlist,omitempty"`
	RemoteAddr       string            `json:"remote_addr,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type LoginResult struct {
	Account Account       `json:"account"`
	Shard   Shard         `json:"shard"`
	Gate    GateSelection `json:"gate"`
	Created bool          `json:"created,omitempty"`
}

type BindRequest struct {
	AccountID string   `json:"account_id"`
	Identity  Identity `json:"identity"`
}

type BanRecord struct {
	Subject   string            `json:"subject"`
	Scope     string            `json:"scope,omitempty"`
	Reason    string            `json:"reason,omitempty"`
	Until     time.Time         `json:"until,omitempty"`
	Operator  string            `json:"operator,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

type AllowRecord struct {
	Subject   string            `json:"subject"`
	Scope     string            `json:"scope,omitempty"`
	Reason    string            `json:"reason,omitempty"`
	Operator  string            `json:"operator,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

type Shard struct {
	ID          string            `json:"id"`
	Name        string            `json:"name,omitempty"`
	State       string            `json:"state"`
	Weight      int               `json:"weight,omitempty"`
	GateGroupID string            `json:"gate_group_id,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type GateNode struct {
	NodeID      string            `json:"node_id"`
	Address     string            `json:"address"`
	ShardIDs    []string          `json:"shard_ids,omitempty"`
	GateGroupID string            `json:"gate_group_id,omitempty"`
	Weight      int               `json:"weight,omitempty"`
	Draining    bool              `json:"draining,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type GateSelection struct {
	Address    string `json:"address"`
	LoginToken string `json:"login_token"`
	NodeID     string `json:"node_id,omitempty"`
}

type Export struct {
	Accounts []Account     `json:"accounts"`
	Bans     []BanRecord   `json:"bans"`
	Allows   []AllowRecord `json:"allows"`
	Shards   []Shard       `json:"shards"`
	Gates    []GateNode    `json:"gates"`
}

type AccountAccess struct {
	Bans   []BanRecord   `json:"bans,omitempty"`
	Allows []AllowRecord `json:"allows,omitempty"`
}

type Store interface {
	Load(context.Context) (Export, bool, error)
	Save(context.Context, Export) error
}
