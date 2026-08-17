package rpc

import (
	"context"

	"longheng.io/server/internal/platform/bus"
	"longheng.io/server/pkg/contracts"
)

// RequestHandler 是 transport 收到 RPC 请求后的回调。
type RequestHandler func(context.Context, contracts.RPCRequest) error

// ResponseHandler 是 transport 收到 RPC 响应后的回调。
type ResponseHandler func(context.Context, contracts.RPCResponse) error

// TransportSubscription 表示可关闭的 transport 订阅。
type TransportSubscription interface {
	Close() error
}

// Transport 抽象 RPC 请求和响应的订阅与发布能力。
type Transport interface {
	SubscribeRequests(context.Context, string, RequestHandler) (TransportSubscription, error)
	SubscribeResponses(context.Context, string, ResponseHandler) (TransportSubscription, error)
	PublishRequest(context.Context, string, contracts.RPCRequest) error
	PublishResponse(context.Context, string, contracts.RPCResponse) error
}

// BusTransport 通过框架 bus 投递 RPC 请求和响应。
type BusTransport struct {
	bus bus.Bus
}

// NewBusTransport 创建基于 bus 的 RPC transport。
func NewBusTransport(eventBus bus.Bus) *BusTransport {
	return &BusTransport{bus: eventBus}
}

// SubscribeRequests 订阅指定节点的 RPC 请求主题。
func (t *BusTransport) SubscribeRequests(_ context.Context, nodeID string, handler RequestHandler) (TransportSubscription, error) {
	if t == nil || t.bus == nil {
		return nil, bus.ErrClosed
	}
	return t.bus.Subscribe(contracts.RPCNodeRequestTopic(nodeID), func(ctx context.Context, env bus.Envelope) error {
		req, ok := rpcReqPayload(env.Payload)
		if !ok {
			return nil
		}
		return handler(ctx, req)
	})
}

// SubscribeResponses 订阅指定节点的 RPC 响应主题。
func (t *BusTransport) SubscribeResponses(_ context.Context, nodeID string, handler ResponseHandler) (TransportSubscription, error) {
	if t == nil || t.bus == nil {
		return nil, bus.ErrClosed
	}
	return t.bus.Subscribe(contracts.RPCResponseTopic(nodeID), func(ctx context.Context, env bus.Envelope) error {
		resp, ok := rpcRespPayload(env.Payload)
		if !ok {
			return nil
		}
		return handler(ctx, resp)
	})
}

// PublishRequest 把 RPC 请求发布到目标节点主题。
func (t *BusTransport) PublishRequest(ctx context.Context, targetNodeID string, request contracts.RPCRequest) error {
	if t == nil || t.bus == nil {
		return bus.ErrClosed
	}
	return t.bus.Publish(ctx, contracts.RPCNodeRequestTopic(targetNodeID), request)
}

// PublishResponse 把 RPC 响应发布到目标节点响应主题。
func (t *BusTransport) PublishResponse(ctx context.Context, targetNodeID string, response contracts.RPCResponse) error {
	if t == nil || t.bus == nil {
		return bus.ErrClosed
	}
	return t.bus.Publish(ctx, contracts.RPCResponseTopic(targetNodeID), response)
}
