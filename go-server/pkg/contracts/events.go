package contracts

import "time"

// ServiceStarted 描述单节点服务启动事件。
type ServiceStarted struct {
	Service   string    `json:"service"`
	NodeID    string    `json:"node_id"`
	StartedAt time.Time `json:"started_at"`
}

// ClusterServiceEvent 描述服务在集群拓扑中的状态、地址和版本信息。
type ClusterServiceEvent struct {
	ClusterID   string            `json:"cluster_id"`
	GID         int64             `json:"gid,omitempty"`
	VirtualGID  int64             `json:"virtual_gid,omitempty"`
	ShardID     int64             `json:"shard_id,omitempty"`
	Service     string            `json:"service"`
	NodeID      string            `json:"node_id"`
	Environment string            `json:"environment"`
	Zone        string            `json:"zone"`
	PublicAddr  string            `json:"public_addr,omitempty"`
	PrivateAddr string            `json:"private_addr,omitempty"`
	AdminAddr   string            `json:"admin_addr,omitempty"`
	Version     string            `json:"version,omitempty"`
	DataVersion string            `json:"data_version,omitempty"`
	State       string            `json:"state"`
	Maintaining bool              `json:"maintaining"`
	Meta        map[string]string `json:"meta,omitempty"`
	At          time.Time         `json:"at"`
}

// GatewayClientPacket 是 gateway 转发到 logic 的客户端请求包。
type GatewayClientPacket struct {
	GatewayNodeID     string            `json:"gateway_node_id"`
	TargetLogicNodeID string            `json:"target_logic_node_id,omitempty"`
	SessionID         string            `json:"session_id"`
	AccountID         string            `json:"account_id"`
	PacketID          int32             `json:"packet_id"`
	MsgID             uint32            `json:"msg_id"`
	Sequence          uint64            `json:"sequence,omitempty"`
	IdempotencyKey    string            `json:"idempotency_key,omitempty"`
	ClientRequestID   string            `json:"client_request_id,omitempty"`
	WireFormat        string            `json:"wire_format,omitempty"`
	Compressed        bool              `json:"compressed"`
	Body              []byte            `json:"body,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	ReceivedAt        time.Time         `json:"received_at"`
}

// LogicPlayerResponse 是 logic 返回给 gateway 的玩家响应包。
type LogicPlayerResponse struct {
	TargetGatewayNodeID string            `json:"target_gateway_node_id"`
	SessionID           string            `json:"session_id"`
	AccountID           string            `json:"account_id"`
	PacketID            int32             `json:"packet_id"`
	MsgID               uint32            `json:"msg_id"`
	Sequence            uint64            `json:"sequence,omitempty"`
	WireFormat          string            `json:"wire_format,omitempty"`
	Compressed          bool              `json:"compressed"`
	Body                []byte            `json:"body,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	Note                string            `json:"note,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
}

// GatewayPush 是服务侧推送到 gateway 的单人或广播消息。
type GatewayPush struct {
	TargetGatewayNodeID string            `json:"target_gateway_node_id,omitempty"`
	SessionID           string            `json:"session_id,omitempty"`
	AccountID           string            `json:"account_id,omitempty"`
	Broadcast           bool              `json:"broadcast,omitempty"`
	PacketID            int32             `json:"packet_id"`
	MsgID               uint32            `json:"msg_id"`
	Sequence            uint64            `json:"sequence,omitempty"`
	WireFormat          string            `json:"wire_format,omitempty"`
	Compressed          bool              `json:"compressed"`
	Body                []byte            `json:"body,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	Note                string            `json:"note,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
}

// SessionConnected 描述会话绑定到账号后的上线事件。
type SessionConnected struct {
	TargetLogicNodeID string    `json:"target_logic_node_id,omitempty"`
	SessionID         string    `json:"session_id"`
	AccountID         string    `json:"account_id"`
	ConnectedAt       time.Time `json:"connected_at"`
}

// SessionDisconnected 描述会话断开和下线原因。
type SessionDisconnected struct {
	TargetLogicNodeID string    `json:"target_logic_node_id,omitempty"`
	SessionID         string    `json:"session_id"`
	AccountID         string    `json:"account_id"`
	Reason            string    `json:"reason"`
	DisconnectedAt    time.Time `json:"disconnected_at"`
}

// PlayerStateChanged 描述玩家状态变化事件。
type PlayerStateChanged struct {
	AccountID string    `json:"account_id"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChatBroadcast 描述社交聊天广播事件。
type ChatBroadcast struct {
	MessageID string            `json:"message_id,omitempty"`
	Seq       uint64            `json:"seq,omitempty"`
	Channel   string            `json:"channel"`
	Kind      string            `json:"kind,omitempty"`
	AccountID string            `json:"account_id"`
	To        string            `json:"to,omitempty"`
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	SentAt    time.Time         `json:"sent_at"`
}

// NoticePublished 描述公告发布后的事件负载。
type NoticePublished struct {
	NoticeID      string            `json:"notice_id"`
	Kind          string            `json:"kind"`
	Scope         string            `json:"scope,omitempty"`
	ShardID       string            `json:"shard_id,omitempty"`
	Title         string            `json:"title,omitempty"`
	Body          string            `json:"body,omitempty"`
	AttachmentRef string            `json:"attachment_ref,omitempty"`
	Recipients    []string          `json:"recipients,omitempty"`
	DeliveryIDs   []string          `json:"delivery_ids,omitempty"`
	Meta          map[string]string `json:"meta,omitempty"`
	PublishedAt   time.Time         `json:"published_at"`
}

// RoomAllocated 描述场景房间分配结果。
type RoomAllocated struct {
	RoomID      string    `json:"room_id"`
	AccountID   string    `json:"account_id"`
	AllocatedAt time.Time `json:"allocated_at"`
}
