package contracts

import (
	"fmt"
	"reflect"

	"longheng.io/server/pkg/serializer"
)

var topicPayloadMakers = map[string]func() any{
	TopicServiceStarted:        func() any { return &ServiceStarted{} },
	TopicClusterServiceStarted: func() any { return &ClusterServiceEvent{} },
	TopicClusterServiceState:   func() any { return &ClusterServiceEvent{} },
	TopicClusterServiceBeat:    func() any { return &ClusterServiceEvent{} },
	TopicClusterServiceStopped: func() any { return &ClusterServiceEvent{} },
	TopicGatewayClientPacket:   func() any { return &GatewayClientPacket{} },
	TopicGatewayPush:           func() any { return &GatewayPush{} },
	TopicLogicPlayerResponse:   func() any { return &LogicPlayerResponse{} },
	TopicSessionConnected:      func() any { return &SessionConnected{} },
	TopicSessionDisconnected:   func() any { return &SessionDisconnected{} },
	TopicPlayerStateChange:     func() any { return &PlayerStateChanged{} },
	TopicChatBroadcast:         func() any { return &ChatBroadcast{} },
	TopicNoticePublished:       func() any { return &NoticePublished{} },
	TopicRoomAllocated:         func() any { return &RoomAllocated{} },
}

// MarshalTopicPayload 使用框架默认 JSON 编码器序列化 topic 负载。
func MarshalTopicPayload(payload any) ([]byte, error) {
	codec, err := serializer.DefaultRegistry.MustGet("json")
	if err != nil {
		return nil, err
	}
	return codec.Marshal(payload)
}

// UnmarshalTopicPayload 按 topic 反序列化为已知契约结构，未知 topic 返回 map。
func UnmarshalTopicPayload(topic string, data []byte) (any, error) {
	factory := topicPayloadFactory(topic)
	if factory == nil {
		var payload map[string]any
		if err := unmarshalJSON(data, &payload); err != nil {
			return nil, fmt.Errorf("decode topic %q: %w", topic, err)
		}
		return payload, nil
	}

	target := factory()
	if err := unmarshalJSON(data, target); err != nil {
		return nil, fmt.Errorf("decode topic %q: %w", topic, err)
	}

	value := reflect.ValueOf(target)
	if value.Kind() == reflect.Pointer && !value.IsNil() {
		return value.Elem().Interface(), nil
	}
	return target, nil
}

func topicPayloadFactory(topic string) func() any {
	if factory, ok := topicPayloadMakers[topic]; ok {
		return factory
	}
	switch {
	case IsLogicNodePacketTopic(topic):
		return func() any { return &GatewayClientPacket{} }
	case IsLogicSessConnTopic(topic):
		return func() any { return &SessionConnected{} }
	case IsLogicSessDiscTopic(topic):
		return func() any { return &SessionDisconnected{} }
	case IsGatewayNodePushTopic(topic):
		return func() any { return &GatewayPush{} }
	case IsGatewayNodeRespTopic(topic):
		return func() any { return &LogicPlayerResponse{} }
	case IsRPCNodeRequestTopic(topic):
		return func() any { return &RPCRequest{} }
	case IsRPCResponseTopic(topic):
		return func() any { return &RPCResponse{} }
	default:
		return nil
	}
}

func unmarshalJSON(data []byte, target any) error {
	codec, err := serializer.DefaultRegistry.MustGet("json")
	if err != nil {
		return err
	}
	return codec.Unmarshal(data, target)
}
