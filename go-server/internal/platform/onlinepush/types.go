package onlinepush

import (
	"errors"
	"time"
)

var (
	// ErrBusRequired 表示在线推送缺少事件总线。
	ErrBusRequired = errors.New("online push bus is required")
	// ErrStoreRequired 表示在线推送缺少状态存储。
	ErrStoreRequired = errors.New("online push store is required")
	// ErrTargetRequired 表示请求没有任何可投递目标。
	ErrTargetRequired = errors.New("online push target is required")
	// ErrIdempotencyRequired 表示请求缺少幂等键。
	ErrIdempotencyRequired = errors.New("online push idempotency key is required")
	// ErrOfflineNotFound 表示离线消息不存在。
	ErrOfflineNotFound = errors.New("online push offline message not found")
)

const (
	// OfflineStore 表示账号离线时保存离线消息。
	OfflineStore = "store"
	// OfflineDrop 表示账号离线时直接丢弃消息。
	OfflineDrop = "drop"

	// StatusAccepted 表示 receipt 已被接收但尚未完成投递。
	StatusAccepted = "accepted"
	// StatusPublished 表示消息已发布到网关。
	StatusPublished = "published"
	// StatusOffline 表示消息已转为离线存储。
	StatusOffline = "offline"
	// StatusPartial 表示部分目标成功、部分目标失败或离线。
	StatusPartial = "partial"
	// StatusDropped 表示消息按策略被丢弃。
	StatusDropped = "dropped"
	// StatusFailed 表示消息投递失败。
	StatusFailed = "failed"
)

// Request 是在线推送入口请求。
type Request struct {
	ID                  string            `json:"id,omitempty"`
	IdempotencyKey      string            `json:"idempotency_key"`
	AccountID           string            `json:"account_id,omitempty"`
	AccountIDs          []string          `json:"account_ids,omitempty"`
	SessionID           string            `json:"session_id,omitempty"`
	TargetGatewayNodeID string            `json:"target_gateway_node_id,omitempty"`
	Broadcast           bool              `json:"broadcast,omitempty"`
	PacketID            int32             `json:"packet_id"`
	MsgID               uint32            `json:"msg_id"`
	Sequence            uint64            `json:"sequence,omitempty"`
	WireFormat          string            `json:"wire_format,omitempty"`
	Compressed          bool              `json:"compressed,omitempty"`
	Body                []byte            `json:"body,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	Note                string            `json:"note,omitempty"`
	OfflinePolicy       string            `json:"offline_policy,omitempty"`
	CreatedAt           time.Time         `json:"created_at,omitempty"`
}

// Receipt 记录一次在线推送请求的投递结果。
type Receipt struct {
	ID             string    `json:"id"`
	RequestID      string    `json:"request_id,omitempty"`
	IdempotencyKey string    `json:"idempotency_key"`
	Target         string    `json:"target"`
	Status         string    `json:"status"`
	Published      int       `json:"published"`
	Offline        int       `json:"offline"`
	Dropped        int       `json:"dropped"`
	Errors         []string  `json:"errors,omitempty"`
	Duplicate      bool      `json:"duplicate,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// OfflineMessage 表示账号离线时保存的待处理消息。
type OfflineMessage struct {
	ID        string            `json:"id"`
	ReceiptID string            `json:"receipt_id"`
	AccountID string            `json:"account_id"`
	PacketID  int32             `json:"packet_id"`
	MsgID     uint32            `json:"msg_id"`
	Body      []byte            `json:"body,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Note      string            `json:"note,omitempty"`
	Status    string            `json:"status"`
	CreatedAt time.Time         `json:"created_at"`
}

// Snapshot 是在线推送状态摘要。
type Snapshot struct {
	Receipts int            `json:"receipts"`
	Offline  int            `json:"offline"`
	ByStatus map[string]int `json:"by_status,omitempty"`
}
