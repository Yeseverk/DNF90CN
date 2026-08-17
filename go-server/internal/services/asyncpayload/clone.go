// Package asyncpayload 提供服务异步发布边界的 payload 快照工具。
package asyncpayload

import "longheng.io/server/pkg/contracts"

// Clone 在异步队列入队、取批和失败归还时复制已知可变字段，避免后续修改污染待发布事件。
func Clone(payload any) any {
	switch value := payload.(type) {
	case []byte:
		return cloneBytes(value)
	case []string:
		return cloneStrings(value)
	case []any:
		return cloneAnySlice(value)
	case map[string]string:
		return cloneStringMap(value)
	case map[string]any:
		return cloneAnyMap(value)
	case contracts.GatewayClientPacket:
		value.Body = cloneBytes(value.Body)
		value.Metadata = cloneStringMap(value.Metadata)
		return value
	case *contracts.GatewayClientPacket:
		if value == nil {
			return value
		}
		cloned := Clone(*value).(contracts.GatewayClientPacket)
		return &cloned
	case contracts.LogicPlayerResponse:
		value.Body = cloneBytes(value.Body)
		value.Metadata = cloneStringMap(value.Metadata)
		return value
	case *contracts.LogicPlayerResponse:
		if value == nil {
			return value
		}
		cloned := Clone(*value).(contracts.LogicPlayerResponse)
		return &cloned
	case contracts.GatewayPush:
		value.Body = cloneBytes(value.Body)
		value.Metadata = cloneStringMap(value.Metadata)
		return value
	case *contracts.GatewayPush:
		if value == nil {
			return value
		}
		cloned := Clone(*value).(contracts.GatewayPush)
		return &cloned
	case contracts.ClusterServiceEvent:
		value.Meta = cloneStringMap(value.Meta)
		return value
	case *contracts.ClusterServiceEvent:
		if value == nil {
			return value
		}
		cloned := Clone(*value).(contracts.ClusterServiceEvent)
		return &cloned
	case contracts.ChatBroadcast:
		value.Metadata = cloneStringMap(value.Metadata)
		return value
	case *contracts.ChatBroadcast:
		if value == nil {
			return value
		}
		cloned := Clone(*value).(contracts.ChatBroadcast)
		return &cloned
	case contracts.NoticePublished:
		value.Recipients = cloneStrings(value.Recipients)
		value.DeliveryIDs = cloneStrings(value.DeliveryIDs)
		value.Meta = cloneStringMap(value.Meta)
		return value
	case *contracts.NoticePublished:
		if value == nil {
			return value
		}
		cloned := Clone(*value).(contracts.NoticePublished)
		return &cloned
	case contracts.RPCRequest:
		value.Metadata = cloneStringMap(value.Metadata)
		value.Payload = cloneBytes(value.Payload)
		return value
	case *contracts.RPCRequest:
		if value == nil {
			return value
		}
		cloned := Clone(*value).(contracts.RPCRequest)
		return &cloned
	case contracts.RPCResponse:
		value.Metadata = cloneStringMap(value.Metadata)
		value.Payload = cloneBytes(value.Payload)
		return value
	case *contracts.RPCResponse:
		if value == nil {
			return value
		}
		cloned := Clone(*value).(contracts.RPCResponse)
		return &cloned
	default:
		return payload
	}
}

func cloneBytes(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	return append([]byte(nil), in...)
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneAnySlice(in []any) []any {
	if len(in) == 0 {
		return nil
	}
	out := make([]any, len(in))
	for idx, value := range in {
		out[idx] = Clone(value)
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = Clone(value)
	}
	return out
}
