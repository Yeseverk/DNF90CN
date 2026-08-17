package contracts

import (
	"strings"
)

// TopicScope 表示带作用域 topic 的层级类型。
type TopicScope string

const (
	// TopicScopeService 表示服务作用域 topic。
	TopicScopeService TopicScope = "service"

	// TopicScopeZone 表示区服作用域 topic。
	TopicScopeZone TopicScope = "zone"

	// TopicScopeShard 表示分片作用域 topic。
	TopicScopeShard TopicScope = "shard"

	// TopicServiceStarted 是单节点服务启动事件 topic。
	TopicServiceStarted = "service.started"

	// TopicClusterServiceStarted 是集群服务启动事件 topic。
	TopicClusterServiceStarted = "cluster.service.started"

	// TopicClusterServiceState 是集群服务状态变化事件 topic。
	TopicClusterServiceState = "cluster.service.state"

	// TopicClusterServiceBeat 是集群服务心跳事件 topic。
	TopicClusterServiceBeat = "cluster.service.heartbeat"

	// TopicClusterServiceStopped 是集群服务停止事件 topic。
	TopicClusterServiceStopped = "cluster.service.stopped"

	// TopicGatewayClientPacket 是 gateway 收到客户端包后的事件 topic。
	TopicGatewayClientPacket = "gateway.client.packet"

	// TopicGatewayPush 是推送到 gateway 的消息 topic。
	TopicGatewayPush = "gateway.push"

	// TopicLogicPlayerResponse 是 logic 返回玩家响应的事件 topic。
	TopicLogicPlayerResponse = "logic.player.response"

	// TopicSessionConnected 是会话上线事件 topic。
	TopicSessionConnected = "gateway.session.connected"

	// TopicSessionDisconnected 是会话下线事件 topic。
	TopicSessionDisconnected = "gateway.session.disconnected"

	// TopicPlayerStateChange 是玩家状态变化事件 topic。
	TopicPlayerStateChange = "logic.player.state.changed"

	// TopicChatBroadcast 是聊天广播事件 topic。
	TopicChatBroadcast = "social.chat.broadcast"

	// TopicNoticePublished 是公告发布事件 topic。
	TopicNoticePublished = "notice.published"

	// TopicRoomAllocated 是场景房间分配事件 topic。
	TopicRoomAllocated = "scene.room.allocated"

	topicLogicPrefix   = "logic.node."
	logicPacketSuffix  = ".packet"
	sessionOnSuffix    = ".session.connected"
	sessionOffSuffix   = ".session.disconnected"
	topicGwNodePrefix  = "gateway.node."
	topicGwPushSuffix  = ".push"
	topicGwRespSuffix  = ".response"
	topicRPCNodePrefix = "rpc.node."
	topicRPCReqSuffix  = ".request"
	topicRPCRespPrefix = "rpc.response."
	topicRPCRespSuffix = ".reply"
)

// ScopedTopic 是解析后的作用域 topic 结构。
type ScopedTopic struct {
	Scope   TopicScope `json:"scope"`
	Service string     `json:"service,omitempty"`
	ZoneID  string     `json:"zone_id,omitempty"`
	GroupID string     `json:"group_id,omitempty"`
	ShardID string     `json:"shard_id,omitempty"`
	Name    string     `json:"name,omitempty"`
}

// LogicNodePacketTopic 返回指定 logic 节点接收客户端包的 topic。
func LogicNodePacketTopic(nodeID string) string {
	return logicNodeTopic(nodeID, logicPacketSuffix)
}

// LogicSessConnTopic 返回指定 logic 节点接收会话上线事件的 topic。
func LogicSessConnTopic(nodeID string) string {
	return logicNodeTopic(nodeID, sessionOnSuffix)
}

// LogicSessDiscTopic 返回指定 logic 节点接收会话下线事件的 topic。
func LogicSessDiscTopic(nodeID string) string {
	return logicNodeTopic(nodeID, sessionOffSuffix)
}

// IsLogicNodePacketTopic 判断 topic 是否为 logic 节点客户端包 topic。
func IsLogicNodePacketTopic(topic string) bool {
	return isLogicNodeTopic(topic, logicPacketSuffix)
}

// IsLogicSessConnTopic 判断 topic 是否为 logic 节点会话上线 topic。
func IsLogicSessConnTopic(topic string) bool {
	return isLogicNodeTopic(topic, sessionOnSuffix)
}

// IsLogicSessDiscTopic 判断 topic 是否为 logic 节点会话下线 topic。
func IsLogicSessDiscTopic(topic string) bool {
	return isLogicNodeTopic(topic, sessionOffSuffix)
}

// GatewayNodePushTopic 返回指定 gateway 节点接收推送的 topic。
func GatewayNodePushTopic(nodeID string) string {
	return nodeTopic(topicGwNodePrefix, nodeID, topicGwPushSuffix)
}

// IsGatewayNodePushTopic 判断 topic 是否为 gateway 节点推送 topic。
func IsGatewayNodePushTopic(topic string) bool {
	return isNodeTopic(topic, topicGwNodePrefix, topicGwPushSuffix)
}

// GatewayNodeRespTopic 返回指定 gateway 节点接收 logic 玩家响应的 topic。
func GatewayNodeRespTopic(nodeID string) string {
	return nodeTopic(topicGwNodePrefix, nodeID, topicGwRespSuffix)
}

// IsGatewayNodeRespTopic 判断 topic 是否为 gateway 节点玩家响应 topic。
func IsGatewayNodeRespTopic(topic string) bool {
	return isNodeTopic(topic, topicGwNodePrefix, topicGwRespSuffix)
}

// RPCNodeRequestTopic 返回指定节点接收 RPC 请求的 topic。
func RPCNodeRequestTopic(nodeID string) string {
	return nodeTopic(topicRPCNodePrefix, nodeID, topicRPCReqSuffix)
}

// RPCResponseTopic 返回指定节点接收 RPC 响应的 topic。
func RPCResponseTopic(nodeID string) string {
	return nodeTopic(topicRPCRespPrefix, nodeID, topicRPCRespSuffix)
}

// IsRPCNodeRequestTopic 判断 topic 是否为节点 RPC 请求 topic。
func IsRPCNodeRequestTopic(topic string) bool {
	return isNodeTopic(topic, topicRPCNodePrefix, topicRPCReqSuffix)
}

// IsRPCResponseTopic 判断 topic 是否为节点 RPC 响应 topic。
func IsRPCResponseTopic(topic string) bool {
	return isNodeTopic(topic, topicRPCRespPrefix, topicRPCRespSuffix)
}

// ServiceScopedTopic 构造服务作用域 topic。
func ServiceScopedTopic(service string, groupID string, shardID string, name string) string {
	return scopedTopic("service", service, groupID, shardID, name)
}

// ZoneScopedTopic 构造区服作用域 topic。
func ZoneScopedTopic(zoneID string, name string) string {
	return scopedTopic("zone", zoneID, "", "", name)
}

// ShardScopedTopic 构造分片作用域 topic。
func ShardScopedTopic(shardID string, name string) string {
	return scopedTopic("shard", shardID, "", "", name)
}

// ParseScopedTopic 解析服务、区服或分片作用域 topic。
func ParseScopedTopic(topic string) (ScopedTopic, bool) {
	parts := strings.Split(strings.TrimSpace(topic), ".")
	if len(parts) < 2 || hasEmptyPart(parts) {
		return ScopedTopic{}, false
	}
	switch TopicScope(parts[0]) {
	case TopicScopeService:
		return parseScopedTopic(parts)
	case TopicScopeZone:
		return parseOneScopedTopic(TopicScopeZone, parts)
	case TopicScopeShard:
		return parseOneScopedTopic(TopicScopeShard, parts)
	default:
		return ScopedTopic{}, false
	}
}

func logicNodeTopic(nodeID, suffix string) string {
	return nodeTopic(topicLogicPrefix, nodeID, suffix)
}

func nodeTopic(prefix, nodeID, suffix string) string {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ""
	}
	return prefix + nodeID + suffix
}

func isLogicNodeTopic(topic, suffix string) bool {
	return isNodeTopic(topic, topicLogicPrefix, suffix)
}

func isNodeTopic(topic, prefix, suffix string) bool {
	if prefix == "" || suffix == "" {
		return false
	}
	if !strings.HasPrefix(topic, prefix) || !strings.HasSuffix(topic, suffix) {
		return false
	}
	nodeID := strings.TrimSuffix(strings.TrimPrefix(topic, prefix), suffix)
	return strings.TrimSpace(nodeID) != ""
}

func scopedTopic(scope string, first string, groupID string, shardID string, name string) string {
	parts := []string{strings.TrimSpace(scope), strings.TrimSpace(first)}
	if groupID = strings.TrimSpace(groupID); groupID != "" {
		parts = append(parts, "gid", groupID)
	}
	if shardID = strings.TrimSpace(shardID); shardID != "" {
		parts = append(parts, "sid", shardID)
	}
	if name = strings.TrimSpace(name); name != "" {
		parts = append(parts, name)
	}
	for _, part := range parts {
		if part == "" {
			return ""
		}
	}
	return strings.Join(parts, ".")
}

func parseScopedTopic(parts []string) (ScopedTopic, bool) {
	if len(parts) < 2 {
		return ScopedTopic{}, false
	}
	out := ScopedTopic{Scope: TopicScopeService, Service: parts[1]}
	idx := 2
	for idx < len(parts) {
		switch parts[idx] {
		case "gid":
			if idx+1 >= len(parts) || out.GroupID != "" {
				return ScopedTopic{}, false
			}
			out.GroupID = parts[idx+1]
			idx += 2
		case "sid":
			if idx+1 >= len(parts) || out.ShardID != "" {
				return ScopedTopic{}, false
			}
			out.ShardID = parts[idx+1]
			idx += 2
		default:
			out.Name = strings.Join(parts[idx:], ".")
			return out, true
		}
	}
	return out, true
}

func parseOneScopedTopic(scope TopicScope, parts []string) (ScopedTopic, bool) {
	if len(parts) < 2 {
		return ScopedTopic{}, false
	}
	out := ScopedTopic{Scope: scope}
	if scope == TopicScopeZone {
		out.ZoneID = parts[1]
	} else {
		out.ShardID = parts[1]
	}
	if len(parts) > 2 {
		out.Name = strings.Join(parts[2:], ".")
	}
	return out, true
}

func hasEmptyPart(parts []string) bool {
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return true
		}
	}
	return false
}
