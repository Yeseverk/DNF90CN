package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"longheng.io/server/internal/platform/dispatch"
	"longheng.io/server/internal/platform/idempotency"
	"longheng.io/server/pkg/contracts"
	"longheng.io/server/pkg/protocol"
)

func (s *Service) cachedIdemResp(ctx context.Context, packet contracts.GatewayClientPacket, decision idempotency.Decision) (contracts.LogicPlayerResponse, bool, error) {
	if s == nil || strings.TrimSpace(decision.Key) == "" {
		return contracts.LogicPlayerResponse{}, false, nil
	}
	var cacheErr error
	if s.responses != nil {
		response, ok, err := s.responses.Get(ctx, decision.Key)
		if ok {
			return replayIdemResp(packet, response), true, nil
		}
		cacheErr = err
	}
	// response cache 只是热缓存；miss 或故障时从幂等权威后端读取同事务提交的结果。
	if s.idempotency == nil {
		return contracts.LogicPlayerResponse{}, false, cacheErr
	}
	payload, ok, resultErr := s.idempotency.LookupResult(ctx, decision)
	if resultErr != nil || !ok {
		return contracts.LogicPlayerResponse{}, false, errors.Join(cacheErr, resultErr)
	}
	var response contracts.LogicPlayerResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return contracts.LogicPlayerResponse{}, false, fmt.Errorf("decode idempotency result: %w", err)
	}
	if s.responses != nil {
		// 权威结果读取成功后尽力回填热缓存；回填失败不能让已提交结果再次变成不可用。
		_ = s.responses.Store(ctx, decision.Key, response)
	}
	return replayIdemResp(packet, response), true, nil
}

func replayIdemResp(packet contracts.GatewayClientPacket, response contracts.LogicPlayerResponse) contracts.LogicPlayerResponse {
	// 重放响应只复用业务结果，目标 gateway/session/sequence 必须按本次包重写，避免旧连接收到新响应。
	response.TargetGatewayNodeID = packet.GatewayNodeID
	response.SessionID = packet.SessionID
	response.AccountID = packet.AccountID
	response.Sequence = packet.Sequence
	response.WireFormat = packet.WireFormat
	response.Metadata = replayIdemMetadata(response.Metadata, packet.Metadata)
	return response
}

func replayIdemMetadata(stored, current map[string]string) map[string]string {
	// handler 产生的响应 metadata 属于可重放结果；只有链路跟踪字段应跟随本次请求。
	metadata := protocol.CloneFrameMetadata(stored)
	current = protocol.CloneFrameMetadata(current)
	for _, key := range []string{
		protocol.FrameMetadataTraceParent,
		protocol.FrameMetadataTraceState,
		protocol.FrameMetadataBaggage,
	} {
		delete(metadata, key)
		if value := current[key]; value != "" {
			if metadata == nil {
				metadata = make(map[string]string, 3)
			}
			metadata[key] = value
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func encodeIdemResp(response contracts.LogicPlayerResponse) ([]byte, error) {
	payload, err := json.Marshal(clonePlayerResp(response))
	if err != nil {
		return nil, fmt.Errorf("encode idempotency result: %w", err)
	}
	return payload, nil
}

func (s *Service) storeIdemResp(ctx context.Context, decision idempotency.Decision, response contracts.LogicPlayerResponse) error {
	if s == nil || s.responses == nil || strings.TrimSpace(decision.Key) == "" {
		return nil
	}
	return s.responses.Store(ctx, decision.Key, response)
}

func (s *Service) shouldProtectPacket(meta dispatch.HandlerMeta) bool {
	return meta.Idempotent || meta.PlayerScoped || meta.Stateful || strings.TrimSpace(meta.ProfileOperation) != ""
}

func (s *Service) reservePktIdem(ctx context.Context, packet contracts.GatewayClientPacket) (idempotency.Reservation, idempotency.Decision, error) {
	if s.idempotency == nil {
		return idempotency.Reservation{}, idempotency.Decision{Status: idempotency.StatusAccepted}, nil
	}
	return s.idempotency.Begin(ctx, packetIdemRequest(packet))
}

func packetIdemRequest(packet contracts.GatewayClientPacket) idempotency.Request {
	scope := "logic:msg:" + strconv.FormatUint(uint64(packet.MsgID), 10)
	accountID := strings.ToLower(strings.TrimSpace(packet.AccountID))
	sessionID := strings.ToLower(strings.TrimSpace(packet.SessionID))
	rawKey := strings.TrimSpace(packet.IdempotencyKey)
	clientRequestID := strings.TrimSpace(packet.ClientRequestID)
	key := ""
	if clientRequestID != "" {
		// 稳定的客户端请求 ID 是跨重连业务身份；session 和 sequence 只是当前传输属性。
		key = idempotency.CanonicalKey("logic-packet-client-request-v1", scope, accountID, clientRequestID)
	} else if rawKey != "" {
		// 旧 gateway 的显式 key 未包含 session；logic 必须再做会话隔离，避免重连后 sequence 重置命中旧结果。
		key = idempotency.CanonicalKey("logic-packet-explicit-v1", scope, accountID, sessionID, rawKey)
	} else if packet.Sequence > 0 {
		key = idempotency.CanonicalKey(
			"logic-packet-fallback-v1",
			scope,
			accountID,
			sessionID,
			strconv.FormatInt(int64(packet.PacketID), 10),
			strconv.FormatUint(uint64(packet.MsgID), 10),
			strconv.FormatUint(packet.Sequence, 10),
		)
	}
	return idempotency.Request{
		Scope:   scope,
		Subject: accountID,
		Session: sessionID,
		Key:     key,
		Fingerprint: idempotency.CanonicalKey(
			"logic-packet-fingerprint-v1",
			strconv.FormatInt(int64(packet.PacketID), 10),
			strconv.FormatUint(uint64(packet.MsgID), 10),
			strconv.FormatBool(packet.Compressed),
			string(packet.Body),
		),
		Sequence: packet.Sequence,
	}
}
