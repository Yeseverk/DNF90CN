package rpc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"longheng.io/server/internal/platform/replay"
	"longheng.io/server/pkg/contracts"
)

// 历史背景：LongHeng 的 internal RPC 直到本改动前完全明文 —— Bus / NATS / Redis
// PubSub 上谁能连进来就能伪造 RPCRequest。生产部署只能靠网络隔离做防线，
// 一旦 sidecar 漏接、test pod 误配置或同集群另一个服务被攻陷，攻击者就能
// 直接调用 logic 内部 handler。
//
// 本文件提供应用层 HMAC-SHA256 鉴权，作为 mTLS / 内网隔离之外的纵深防御：
//   - 共享 secret 由部署期 Secret/KMS 注入，旋转策略由项目侧实现。
//   - 签名覆盖关键字段（RequestID / SourceNodeID / TargetNodeID / Route /
//     SentAt / payload 哈希），任何字段被改都会失效。
//   - 时间戳偏移检查缩小重放窗口；生产多节点应接入 replay.RedisStore 共享 nonce 表。
//   - Sign* / Verify* 是无状态纯函数，便于在 AuthenticatedTransport 之外的
//     场景（例如直接走 HTTP 的 Admin 内部接口）单独使用。

const (
	// AuthSignatureMetadataKey 是签名字段在 RPCRequest.Metadata /
	// RPCResponse.Metadata 上的固定键。
	AuthSignatureMetadataKey = "x-longheng-signature"
	// AuthAlgorithmMetadataKey 用于在 metadata 里声明算法版本，便于未来兼容
	// 多版本（HMAC-SHA256 / HMAC-SHA512 / Ed25519）。
	AuthAlgorithmMetadataKey = "x-longheng-signature-alg"
	// AuthAlgorithmHMACSHA256 是当前唯一支持的算法标识。
	AuthAlgorithmHMACSHA256 = "HMAC-SHA256"
	// DefaultAuthMaxSkew 是默认的双向时间偏移容差。生产建议保持 5min 量级，
	// 太小容易因为 NTP 抖动误拒，太大会扩大重放窗口。
	DefaultAuthMaxSkew = 5 * time.Minute
)

var (
	// ErrAuthSecretRequired 在构造 Authenticator 时 secret 为空返回。
	ErrAuthSecretRequired = errors.New("rpcauth: shared secret is required")
	// ErrAuthSignatureMissing 表示请求 / 响应缺少签名 metadata。
	ErrAuthSignatureMissing = errors.New("rpcauth: signature missing")
	// ErrAuthSignatureInvalid 表示签名不匹配（密钥不对或字段被改）。
	ErrAuthSignatureInvalid = errors.New("rpcauth: signature invalid")
	// ErrAuthAlgUnsup 表示对端声明了不支持的签名算法。
	ErrAuthAlgUnsup = errors.New("rpcauth: algorithm not supported")
	// ErrAuthTimestampSkew 表示请求 / 响应的 SentAt 偏移超出允许窗口。
	ErrAuthTimestampSkew = errors.New("rpcauth: timestamp outside max skew")
	// ErrAuthReqNoMeta 在签名时 RPCRequest 为 nil 等场景下返回。
	ErrAuthReqNoMeta = errors.New("rpcauth: request is nil")
	// ErrAuthReplayDetected 表示同一 SourceNodeID + RequestID 已在 MaxSkew 窗口内验过；可用 errors.Is 判定。
	ErrAuthReplayDetected = errors.New("rpcauth: replay detected")
	// ErrAuthReplayCheckFailed 表示 nonce store 不可用；可用 errors.Is 保留底层 replay/redis 错误。
	ErrAuthReplayCheckFailed = errors.New("rpcauth: replay check failed")
)

// Authenticator 持有共享密钥和默认偏移窗口，是 RPC 鉴权的核心入口。
// 所有方法在常量时间比较签名（使用 hmac.Equal / subtle.ConstantTimeCompare），
// 不会通过失败用时长泄露签名长度。
type Authenticator struct {
	secret  []byte
	maxSkew time.Duration
	now     func() time.Time
	replay  replay.Store
}

// AuthenticatorOptions 用于构造 Authenticator。Secret 必填；MaxSkew=0 使用
// DefaultAuthMaxSkew；ReplayStore 为空时保持旧的纯签名校验语义。
type AuthenticatorOptions struct {
	Secret      []byte
	MaxSkew     time.Duration
	Now         func() time.Time
	ReplayStore replay.Store
}

// NewAuthenticator 校验配置并返回 Authenticator。secret 为空会返回
// ErrAuthSecretRequired，避免装配代码静默退化为"无鉴权"。
func NewAuthenticator(opts AuthenticatorOptions) (*Authenticator, error) {
	if len(opts.Secret) == 0 {
		return nil, ErrAuthSecretRequired
	}
	skew := opts.MaxSkew
	if skew <= 0 {
		skew = DefaultAuthMaxSkew
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	// 复制 secret，避免调用方后续修改影响 Authenticator。
	secret := make([]byte, len(opts.Secret))
	copy(secret, opts.Secret)
	return &Authenticator{secret: secret, maxSkew: skew, now: now, replay: opts.ReplayStore}, nil
}

// SignRequest 计算 RPCRequest 的 HMAC 签名并写到 Metadata。req 不能为 nil。
// SentAt 为零值时会自动填当前时间；Metadata 为 nil 时会被初始化。
func (a *Authenticator) SignRequest(req *contracts.RPCRequest) error {
	if a == nil {
		return ErrAuthSecretRequired
	}
	if req == nil {
		return ErrAuthReqNoMeta
	}
	if req.SentAt.IsZero() {
		req.SentAt = a.now().UTC()
	}
	if req.Metadata == nil {
		req.Metadata = map[string]string{}
	}
	sig := a.requestSignature(*req)
	req.Metadata[AuthSignatureMetadataKey] = sig
	req.Metadata[AuthAlgorithmMetadataKey] = AuthAlgorithmHMACSHA256
	return nil
}

// VerifyRequest 检查 RPCRequest 是否有合法签名 + 时间戳在允许偏移内。
// 缺签名返回 ErrAuthSignatureMissing；签名不匹配返回 ErrAuthSignatureInvalid；
// 时间戳偏移过大返回 ErrAuthTimestampSkew；算法不支持返回 ErrAuthAlgUnsup；
// nonce 重复返回 ErrAuthReplayDetected。
func (a *Authenticator) VerifyRequest(req contracts.RPCRequest) error {
	return a.VerifyRequestContext(context.Background(), req)
}

// VerifyRequestContext 与 VerifyRequest 语义一致，但会把 ctx 传给 ReplayStore。
// 生产 Redis nonce 表应继承调用链超时，否则鉴权路径可能被 Redis 卡死拖垮 handler。
func (a *Authenticator) VerifyRequestContext(ctx context.Context, req contracts.RPCRequest) error {
	if a == nil {
		return ErrAuthSecretRequired
	}
	sig := strings.TrimSpace(req.Metadata[AuthSignatureMetadataKey])
	if sig == "" {
		return ErrAuthSignatureMissing
	}
	if alg := strings.TrimSpace(req.Metadata[AuthAlgorithmMetadataKey]); alg != "" && alg != AuthAlgorithmHMACSHA256 {
		return fmt.Errorf("%w: %s", ErrAuthAlgUnsup, alg)
	}
	if strings.TrimSpace(req.RequestID) == "" || strings.TrimSpace(req.SourceNodeID) == "" {
		return fmt.Errorf("%w: request_id and source_node_id are required", ErrAuthSignatureMissing)
	}
	expected := a.requestSignature(req)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return ErrAuthSignatureInvalid
	}
	if err := a.checkSkew(req.SentAt); err != nil {
		return err
	}
	if err := a.checkReplay(ctx, req); err != nil {
		return err
	}
	return nil
}

// SignResponse 与 SignRequest 对称，用于网关 / logic 之间的响应消息。
func (a *Authenticator) SignResponse(resp *contracts.RPCResponse) error {
	if a == nil {
		return ErrAuthSecretRequired
	}
	if resp == nil {
		return ErrAuthReqNoMeta
	}
	if resp.SentAt.IsZero() {
		resp.SentAt = a.now().UTC()
	}
	if resp.Metadata == nil {
		resp.Metadata = map[string]string{}
	}
	sig := a.responseSignature(*resp)
	resp.Metadata[AuthSignatureMetadataKey] = sig
	resp.Metadata[AuthAlgorithmMetadataKey] = AuthAlgorithmHMACSHA256
	return nil
}

// VerifyResponse 校验 RPCResponse 的签名。语义与 VerifyRequest 一致。
func (a *Authenticator) VerifyResponse(resp contracts.RPCResponse) error {
	return a.VerifyResponseContext(context.Background(), resp)
}

// VerifyResponseContext 与 VerifyResponse 语义一致，但会把 ctx 传给 ReplayStore。
func (a *Authenticator) VerifyResponseContext(ctx context.Context, resp contracts.RPCResponse) error {
	if a == nil {
		return ErrAuthSecretRequired
	}
	sig := strings.TrimSpace(resp.Metadata[AuthSignatureMetadataKey])
	if sig == "" {
		return ErrAuthSignatureMissing
	}
	if alg := strings.TrimSpace(resp.Metadata[AuthAlgorithmMetadataKey]); alg != "" && alg != AuthAlgorithmHMACSHA256 {
		return fmt.Errorf("%w: %s", ErrAuthAlgUnsup, alg)
	}
	if strings.TrimSpace(resp.RequestID) == "" || strings.TrimSpace(resp.SourceNodeID) == "" || strings.TrimSpace(resp.TargetNodeID) == "" {
		return fmt.Errorf("%w: request_id, source_node_id and target_node_id are required", ErrAuthSignatureMissing)
	}
	expected := a.responseSignature(resp)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return ErrAuthSignatureInvalid
	}
	if err := a.checkSkew(resp.SentAt); err != nil {
		return err
	}
	if err := a.checkResponseReplay(ctx, resp); err != nil {
		return err
	}
	return nil
}

// MaxSkew 暴露当前的偏移窗口，便于装配代码做断言或日志。
func (a *Authenticator) MaxSkew() time.Duration {
	if a == nil {
		return 0
	}
	return a.maxSkew
}

func (a *Authenticator) checkSkew(at time.Time) error {
	if a.maxSkew <= 0 {
		return nil
	}
	if at.IsZero() {
		return ErrAuthTimestampSkew
	}
	now := a.now().UTC()
	diff := now.Sub(at.UTC())
	if diff > a.maxSkew || diff < -a.maxSkew {
		return ErrAuthTimestampSkew
	}
	return nil
}

func (a *Authenticator) checkReplay(ctx context.Context, req contracts.RPCRequest) error {
	if a.replay == nil {
		return nil
	}
	return a.checkReplayKey(ctx, replayKey(req))
}

func (a *Authenticator) checkResponseReplay(ctx context.Context, resp contracts.RPCResponse) error {
	if a.replay == nil {
		return nil
	}
	return a.checkReplayKey(ctx, responseReplayKey(resp))
}

func (a *Authenticator) checkReplayKey(ctx context.Context, key string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	first, err := a.replay.Seen(ctx, key, authReplayTTL(a.maxSkew))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAuthReplayCheckFailed, err)
	}
	if !first {
		return ErrAuthReplayDetected
	}
	return nil
}

func replayKey(req contracts.RPCRequest) string {
	source := strings.TrimSpace(req.SourceNodeID)
	requestID := strings.TrimSpace(req.RequestID)
	if source == "" || requestID == "" {
		return ""
	}
	return "v1:" + encodeReplayKeyPart(source) + ":" + encodeReplayKeyPart(requestID)
}

func responseReplayKey(resp contracts.RPCResponse) string {
	source := strings.TrimSpace(resp.SourceNodeID)
	target := strings.TrimSpace(resp.TargetNodeID)
	requestID := strings.TrimSpace(resp.RequestID)
	if source == "" || target == "" || requestID == "" {
		return ""
	}
	return "v1:resp:" + encodeReplayKeyPart(source) + ":" + encodeReplayKeyPart(target) + ":" + encodeReplayKeyPart(requestID)
}

func encodeReplayKeyPart(value string) string {
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func authReplayTTL(maxSkew time.Duration) time.Duration {
	if maxSkew <= 0 {
		return 2*DefaultAuthMaxSkew + time.Minute
	}
	return 2*maxSkew + time.Minute
}

// requestSignature 拼出 canonical string 然后 HMAC。canonical 故意排除
// Metadata（避免循环依赖签名字段本身），只覆盖 routing / identity / 时间
// / payload 哈希这几个攻击面最大的字段。
func (a *Authenticator) requestSignature(req contracts.RPCRequest) string {
	canonical := strings.Join([]string{
		"rpc.request",
		req.RequestID,
		req.SourceService,
		req.SourceNodeID,
		req.TargetNodeID,
		req.Route,
		req.SentAt.UTC().Format(time.RFC3339Nano),
		hashPayload(req.Payload),
	}, "\n")
	return a.compute(canonical)
}

// responseSignature 与 request 对称，但用 Error 字段替代 Route，强制响应里
// 即使内容相同的失败也对应不同的签名（攻击者无法复制一个 OK 响应去顶替一个失败的）。
func (a *Authenticator) responseSignature(resp contracts.RPCResponse) string {
	canonical := strings.Join([]string{
		"rpc.response",
		resp.RequestID,
		resp.SourceNodeID,
		resp.TargetNodeID,
		resp.Error,
		resp.SentAt.UTC().Format(time.RFC3339Nano),
		hashPayload(resp.Payload),
	}, "\n")
	return a.compute(canonical)
}

func (a *Authenticator) compute(canonical string) string {
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// hashPayload 把 payload 摘要成 sha256，避免 canonical string 巨大、CPU 浪费在
// HMAC 内部分块上。空 payload 显式返回固定标记，便于跨语言重现。
func hashPayload(payload []byte) string {
	if len(payload) == 0 {
		return "empty"
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
