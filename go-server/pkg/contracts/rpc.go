package contracts

import "time"

// RPCRequest 是跨节点 RPC 请求的总线契约。
type RPCRequest struct {
	RequestID     string            `json:"request_id"`
	SourceService string            `json:"source_service,omitempty"`
	SourceNodeID  string            `json:"source_node_id"`
	TargetNodeID  string            `json:"target_node_id"`
	Route         string            `json:"route"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Payload       []byte            `json:"payload,omitempty"`
	SentAt        time.Time         `json:"sent_at"`
}

// RPCResponse 是跨节点 RPC 响应的总线契约。
type RPCResponse struct {
	RequestID    string            `json:"request_id"`
	SourceNodeID string            `json:"source_node_id,omitempty"`
	TargetNodeID string            `json:"target_node_id"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Payload      []byte            `json:"payload,omitempty"`
	Error        string            `json:"error,omitempty"`
	SentAt       time.Time         `json:"sent_at"`
}
