package rpc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"longheng.io/server/pkg/contracts"
)

// RequireMode 描述对端没有签名时是否允许通过；签名"错误"始终拒绝。
//
//	RequireAll       缺签名或签名错误都拒绝。生产推荐。
//	RequirePresent   缺签名放行（灰度），签名错误仍拒绝。仅用于"双方还没全部接入鉴权"
//	                  的滚动升级窗口。完成滚动后应立刻切到 RequireAll。
type RequireMode string

const (
	// RequireAll 表示缺签名和签名错误都拒绝，是生产推荐模式。
	RequireAll RequireMode = "all"
	// RequirePresent 表示缺签名暂时放行，但签名错误仍拒绝。
	RequirePresent RequireMode = "present"
)

// AuthTransportMetrics 是 AuthenticatedTransport 的指标钩子。所有字段都是
// 64-bit counter，调用方可以原子读取作为 Prometheus / scope 输出。
// 调用方可以 nil，AuthenticatedTransport 自己持有一个匿名实例做内部统计。
type AuthTransportMetrics struct {
	SignedRequests      atomic.Uint64
	SignedResponses     atomic.Uint64
	AcceptedRequests    atomic.Uint64
	AcceptedResponses   atomic.Uint64
	RejectedMissing     atomic.Uint64
	RejectedInvalid     atomic.Uint64
	RejectedSkew        atomic.Uint64
	RejectedAlgorithm   atomic.Uint64
	RejectedReplay      atomic.Uint64
	RejectedReplayCheck atomic.Uint64
	GraceLetThrough     atomic.Uint64
	SignFailures        atomic.Uint64
}

// AuthenticatedTransport 包装任意 Transport，发送侧自动签名，接收侧自动验签。
// 不引入新的拓扑或网络层，对调用方完全透明。
type AuthenticatedTransport struct {
	inner   Transport
	auth    *Authenticator
	require RequireMode
	metrics *AuthTransportMetrics
}

// AuthTransportOptions 是装配参数。Auth 必填；Inner 通常是 BusTransport
// 或 GRPCTransport。Require 缺省 RequireAll。
type AuthTransportOptions struct {
	Inner   Transport
	Auth    *Authenticator
	Require RequireMode
	Metrics *AuthTransportMetrics
}

// NewAuthenticatedTransport 构造一个签名 / 验签包装。inner 或 auth 为 nil 返回错误，
// 拒绝静默退化为"不鉴权"。
func NewAuthenticatedTransport(opts AuthTransportOptions) (*AuthenticatedTransport, error) {
	if opts.Inner == nil {
		return nil, errors.New("rpcauth: inner transport is required")
	}
	if opts.Auth == nil {
		return nil, errors.New("rpcauth: authenticator is required")
	}
	require := opts.Require
	switch require {
	case "":
		require = RequireAll
	case RequireAll, RequirePresent:
	default:
		return nil, fmt.Errorf("rpcauth: unsupported require mode %q", require)
	}
	metrics := opts.Metrics
	if metrics == nil {
		metrics = &AuthTransportMetrics{}
	}
	return &AuthenticatedTransport{
		inner:   opts.Inner,
		auth:    opts.Auth,
		require: require,
		metrics: metrics,
	}, nil
}

// Metrics 暴露内部计数器，便于 wiring 层挂到 metrics.Scope 上。
func (t *AuthenticatedTransport) Metrics() *AuthTransportMetrics {
	if t == nil {
		return nil
	}
	return t.metrics
}

// Require 暴露当前模式，主要用于自检 / 日志。
func (t *AuthenticatedTransport) Require() RequireMode {
	if t == nil {
		return ""
	}
	return t.require
}

// SubscribeRequests 订阅请求并在交给业务 handler 前完成验签。
func (t *AuthenticatedTransport) SubscribeRequests(ctx context.Context, nodeID string, handler RequestHandler) (TransportSubscription, error) {
	if t == nil {
		return nil, errors.New("rpcauth: transport is nil")
	}
	return t.inner.SubscribeRequests(ctx, nodeID, func(ctx context.Context, req contracts.RPCRequest) error {
		if err := t.auth.VerifyRequestContext(ctx, req); err != nil {
			t.recordVerifyFailure(err)
			if t.shouldAcceptDespite(err) {
				return handler(ctx, req)
			}
			return err
		}
		t.metrics.AcceptedRequests.Add(1)
		return handler(ctx, req)
	})
}

// SubscribeResponses 订阅响应并在交给等待方前完成验签。
func (t *AuthenticatedTransport) SubscribeResponses(ctx context.Context, nodeID string, handler ResponseHandler) (TransportSubscription, error) {
	if t == nil {
		return nil, errors.New("rpcauth: transport is nil")
	}
	return t.inner.SubscribeResponses(ctx, nodeID, func(ctx context.Context, resp contracts.RPCResponse) error {
		if err := t.auth.VerifyResponseContext(ctx, resp); err != nil {
			t.recordVerifyFailure(err)
			if t.shouldAcceptDespite(err) {
				return handler(ctx, resp)
			}
			return err
		}
		t.metrics.AcceptedResponses.Add(1)
		return handler(ctx, resp)
	})
}

// PublishRequest 对请求签名后交给底层 transport 发布。
func (t *AuthenticatedTransport) PublishRequest(ctx context.Context, targetNodeID string, request contracts.RPCRequest) error {
	if t == nil {
		return errors.New("rpcauth: transport is nil")
	}
	if err := t.auth.SignRequest(&request); err != nil {
		t.metrics.SignFailures.Add(1)
		return err
	}
	t.metrics.SignedRequests.Add(1)
	return t.inner.PublishRequest(ctx, targetNodeID, request)
}

// PublishResponse 对响应签名后交给底层 transport 发布。
func (t *AuthenticatedTransport) PublishResponse(ctx context.Context, targetNodeID string, response contracts.RPCResponse) error {
	if t == nil {
		return errors.New("rpcauth: transport is nil")
	}
	if err := t.auth.SignResponse(&response); err != nil {
		t.metrics.SignFailures.Add(1)
		return err
	}
	t.metrics.SignedResponses.Add(1)
	return t.inner.PublishResponse(ctx, targetNodeID, response)
}

// shouldAcceptDespite 区分"可灰度通过"和"必须拒绝"的失败。
// 只有 ErrAuthSignatureMissing 在 RequirePresent 模式下允许放行；
// invalid / skew / algorithm 一律拒绝，无论什么模式。
func (t *AuthenticatedTransport) shouldAcceptDespite(err error) bool {
	if t.require != RequirePresent {
		return false
	}
	if !errors.Is(err, ErrAuthSignatureMissing) {
		return false
	}
	t.metrics.GraceLetThrough.Add(1)
	return true
}

func (t *AuthenticatedTransport) recordVerifyFailure(err error) {
	switch {
	case errors.Is(err, ErrAuthSignatureMissing):
		t.metrics.RejectedMissing.Add(1)
	case errors.Is(err, ErrAuthSignatureInvalid):
		t.metrics.RejectedInvalid.Add(1)
	case errors.Is(err, ErrAuthTimestampSkew):
		t.metrics.RejectedSkew.Add(1)
	case errors.Is(err, ErrAuthAlgUnsup):
		t.metrics.RejectedAlgorithm.Add(1)
	case errors.Is(err, ErrAuthReplayDetected):
		t.metrics.RejectedReplay.Add(1)
	case errors.Is(err, ErrAuthReplayCheckFailed):
		t.metrics.RejectedReplayCheck.Add(1)
	default:
		// 未知错误不分类，但仍然计入 invalid，避免悄悄漏统计。
		t.metrics.RejectedInvalid.Add(1)
		// 兜底，让调用方在日志里能看到具体类型。
		_ = strings.TrimSpace(err.Error())
	}
}
