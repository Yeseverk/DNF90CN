package logic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"longheng.io/server/internal/platform/dispatch"
	"longheng.io/server/internal/platform/idempotency"
	"longheng.io/server/internal/platform/playerloop"
	"longheng.io/server/pkg/contracts"
	"longheng.io/server/pkg/protocol"
)

const idemFinalizeTimeout = 5 * time.Second

func (s *Service) handlePlayerEvent(ctx context.Context, event playerloop.Event) error {
	switch payload := event.Payload.(type) {
	case contracts.SessionConnected:
		return s.handleSessConnected(ctx, payload)
	case contracts.SessionDisconnected:
		return s.handleSessionDisc(ctx, payload)
	case contracts.GatewayClientPacket:
		return s.handleGwPacket(ctx, payload)
	default:
		return nil
	}
}

func (s *Service) handleGwPacket(ctx context.Context, packet contracts.GatewayClientPacket) error {
	packet.AccountID = strings.TrimSpace(packet.AccountID)
	packet.SessionID = strings.TrimSpace(packet.SessionID)
	if s.isStaleSession(packet.AccountID, packet.SessionID) {
		return nil
	}
	meta, metaOK := s.dispatcher.Meta(packet.MsgID)
	if metaOK {
		// route policy 先于幂等和 handler 执行，非法路由不应占用防重 key。
		if err := dispatch.EnforceRoutePolicy(meta, dispatch.Request{
			GatewayNodeID: packet.GatewayNodeID,
			SessionID:     packet.SessionID,
			AccountID:     packet.AccountID,
			PacketID:      packet.PacketID,
			MsgID:         packet.MsgID,
			Compressed:    packet.Compressed,
			Body:          append([]byte(nil), packet.Body...),
			Metadata:      protocol.CloneFrameMetadata(packet.Metadata),
			ReceivedAt:    packet.ReceivedAt,
		}); err != nil {
			s.incMetric("logic_packet_errors_total", map[string]string{"stage": "route_policy"})
			return s.publishProtocolError(ctx, packet, err)
		}
	}
	var reservation idempotency.Reservation
	var decision idempotency.Decision
	protected := metaOK && s.shouldProtectPacket(meta)
	if protected {
		var err error
		// Begin 只占位，不执行业务；handler 成功后必须 Commit，失败必须 Abort。
		reservation, decision, err = s.reservePktIdem(ctx, packet)
		if err != nil {
			if errors.Is(err, idempotency.ErrRequestConflict) {
				s.incMetric("logic_idempotency_checks_total", map[string]string{"status": "conflict", "msg_id": fmt.Sprint(packet.MsgID)})
				s.incMetric("logic_packet_errors_total", map[string]string{"stage": "idempotency_conflict"})
				return s.publishProtocolError(ctx, packet, protocol.WrapError(protocol.CodeConflict, "idempotency request conflicts with original payload", err))
			}
			if errors.Is(err, idempotency.ErrInvalidRequest) {
				s.incMetric("logic_idempotency_checks_total", map[string]string{"status": "invalid_request", "msg_id": fmt.Sprint(packet.MsgID)})
				s.incMetric("logic_packet_errors_total", map[string]string{"stage": "idempotency_invalid"})
				return s.publishProtocolError(ctx, packet, protocol.WrapError(protocol.CodeBadRequest, "invalid idempotency request", err))
			}
			s.incMetric("logic_idempotency_checks_total", map[string]string{"status": "backend_error", "msg_id": fmt.Sprint(packet.MsgID)})
			s.incMetric("logic_packet_errors_total", map[string]string{"stage": "idempotency"})
			return s.publishProtocolError(ctx, packet, protocol.WrapError(protocol.CodeUnavailable, "idempotency backend unavailable", err))
		}
		s.incMetric("logic_idempotency_checks_total", map[string]string{"status": string(decision.Status), "msg_id": fmt.Sprint(packet.MsgID)})
		if decision.Status == idempotency.StatusDuplicate || decision.Status == idempotency.StatusReplay {
			// 重复请求优先重放缓存响应，不再进入 handler，避免加钱、领奖、读邮件等动作重复执行。
			cached, ok, err := s.cachedIdemResp(ctx, packet, decision)
			if err != nil {
				s.incMetric("logic_idempotency_response_cache_errors_total", map[string]string{"operation": "get", "msg_id": fmt.Sprint(packet.MsgID)})
				s.incMetric("logic_packet_errors_total", map[string]string{"stage": "idempotency_response_cache"})
				return s.publishProtocolError(ctx, packet, protocol.WrapError(protocol.CodeUnavailable, "load idempotent response", err))
			}
			if ok {
				s.incMetric("logic_idempotency_replays_total", map[string]string{"status": string(decision.Status), "msg_id": fmt.Sprint(packet.MsgID)})
				return s.publishPlayerResp(ctx, cached)
			}
			s.incMetric("logic_idempotency_response_cache_misses_total", map[string]string{"status": string(decision.Status), "msg_id": fmt.Sprint(packet.MsgID)})
			s.incMetric("logic_packet_errors_total", map[string]string{"stage": "idempotency_response_cache_miss"})
			return s.publishProtocolError(ctx, packet, protocol.Errorf(protocol.CodeConflict, "idempotent response is no longer available"))
		}
		if decision.Status == idempotency.StatusInFlight {
			// 同一个幂等请求仍在处理时返回冲突，客户端应稍后重试而不是并发打 handler。
			s.incMetric("logic_idempotency_inflight_responses_total", map[string]string{"msg_id": fmt.Sprint(packet.MsgID)})
			s.incMetric("logic_packet_errors_total", map[string]string{"stage": "idempotency_in_flight"})
			return s.publishProtocolError(ctx, packet, protocol.Errorf(protocol.CodeConflict, "idempotent request is still in flight"))
		}
	}
	s.incMetricLow(MetricLogicPacketsTotal, map[string]string{"msg_id": fmt.Sprint(packet.MsgID)})
	dispatchStarted := time.Now()
	resp, err := s.dispatcher.Dispatch(ctx, dispatch.Request{
		GatewayNodeID: packet.GatewayNodeID,
		SessionID:     packet.SessionID,
		AccountID:     packet.AccountID,
		PacketID:      packet.PacketID,
		MsgID:         packet.MsgID,
		Compressed:    packet.Compressed,
		Body:          append([]byte(nil), packet.Body...),
		Metadata:      protocol.CloneFrameMetadata(packet.Metadata),
		ReceivedAt:    packet.ReceivedAt,
	})
	s.observeLoopHandler(meta, packet.MsgID, time.Since(dispatchStarted), err)
	if err != nil {
		// handler 失败必须释放 pending 占位，否则客户端重试会被误判为仍在处理。
		_ = abortIdemReservation(ctx, reservation)
		return s.publishProtocolError(ctx, packet, err)
	}
	response := responseFromDispatch(packet, resp)
	if protected {
		// handler 成功后先提交幂等结果，不让后置激活或热缓存耗尽一致性提交的超时预算。
		commitCtx, commitCancel := idemFinalizeContext(ctx)
		payload, err := encodeIdemResp(response)
		if err == nil {
			err = reservation.CommitResult(commitCtx, payload)
		}
		commitCancel()
		// marker 与响应先在幂等权威后端原子提交；独立 response cache 只做可失败的热缓存。
		if err != nil {
			s.incMetric("logic_idempotency_checks_total", map[string]string{"status": "commit_error", "msg_id": fmt.Sprint(packet.MsgID)})
			s.incMetric("logic_packet_errors_total", map[string]string{"stage": "idempotency_commit"})
			publishCtx, publishCancel := idemFinalizeContext(ctx)
			defer publishCancel()
			return s.publishProtocolError(publishCtx, packet, idemProtocolErr("commit idempotency request", err))
		}
	}
	if metaOK && meta.PlayerScoped && s.accounts != nil {
		activateCtx, activateCancel := idemFinalizeContext(ctx)
		record, err := s.accounts.Activate(activateCtx, packet.AccountID, packet.SessionID)
		activateCancel()
		if err != nil {
			// handler 和幂等结果均已成功，Activate 只是后置生命周期记录，不能影响原业务重放。
			s.incMetric("logic_packet_errors_total", map[string]string{"stage": "activate_profile"})
			if s.logger != nil {
				s.logger.Error("logic player profile activation failed after handler success", "account_id", packet.AccountID, "session_id", packet.SessionID, "msg_id", packet.MsgID, "error", err)
			}
		} else if s.world != nil {
			s.world.ObservePlayer(record.AccountID, record.State)
		}
	}
	if protected {
		cacheCtx, cacheCancel := idemFinalizeContext(ctx)
		err := s.storeIdemResp(cacheCtx, decision, response)
		cacheCancel()
		if err != nil {
			s.incMetric("logic_idempotency_response_cache_errors_total", map[string]string{"operation": "store", "msg_id": fmt.Sprint(packet.MsgID)})
			if s.logger != nil {
				s.logger.Error("logic idempotent response hot cache store failed", "account_id", packet.AccountID, "msg_id", packet.MsgID, "error", err)
			}
		}
	}
	s.incMetricLow(MetricLogicResponsesTotal, map[string]string{"msg_id": fmt.Sprint(response.MsgID)})
	publishCtx := ctx
	var publishCancel context.CancelFunc
	if protected {
		publishCtx, publishCancel = idemFinalizeContext(ctx)
		defer publishCancel()
	}
	return s.publishPlayerResp(publishCtx, response)
}

func idemProtocolErr(operation string, err error) error {
	switch {
	case errors.Is(err, idempotency.ErrInvalidRequest):
		return protocol.WrapError(protocol.CodeBadRequest, operation, err)
	case errors.Is(err, idempotency.ErrRequestConflict):
		return protocol.WrapError(protocol.CodeConflict, operation, err)
	default:
		return protocol.WrapError(protocol.CodeUnavailable, operation, err)
	}
}

func abortIdemReservation(ctx context.Context, reservation idempotency.Reservation) error {
	if ctx == nil {
		ctx = context.Background()
	}
	abortCtx, cancel := idemFinalizeContext(ctx)
	defer cancel()
	return reservation.Abort(abortCtx)
}

func idemFinalizeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), idemFinalizeTimeout)
}

func responseFromDispatch(packet contracts.GatewayClientPacket, resp dispatch.Response) contracts.LogicPlayerResponse {
	metadata := protocol.CloneFrameMetadata(resp.Metadata)
	if len(metadata) == 0 {
		metadata = protocol.CloneFrameMetadata(packet.Metadata)
	}
	return contracts.LogicPlayerResponse{
		TargetGatewayNodeID: packet.GatewayNodeID,
		SessionID:           packet.SessionID,
		AccountID:           packet.AccountID,
		PacketID:            resp.PacketID,
		MsgID:               resp.MsgID,
		Sequence:            packet.Sequence,
		WireFormat:          packet.WireFormat,
		Compressed:          resp.Compressed,
		Body:                append([]byte(nil), resp.Body...),
		Metadata:            metadata,
		Note:                resp.Note,
		CreatedAt:           time.Now().UTC(),
	}
}

func (s *Service) publishPlayerResp(ctx context.Context, response contracts.LogicPlayerResponse) error {
	topic := contracts.GatewayNodeRespTopic(response.TargetGatewayNodeID)
	if topic == "" {
		topic = contracts.TopicLogicPlayerResponse
	}
	if s.responsesOut != nil {
		return s.responsesOut.Publish(ctx, topic, response)
	}
	if s.bus == nil {
		return errRespMissing
	}
	return s.bus.Publish(ctx, topic, response)
}

func playerLoopErr(err error) error {
	switch {
	case errors.Is(err, playerloop.ErrMissingAccount):
		return protocol.WrapError(protocol.CodeBadRequest, "account_id is required", err)
	case errors.Is(err, playerloop.ErrStopped):
		return protocol.WrapError(protocol.CodeUnavailable, "player loop unavailable", err)
	case errors.Is(err, playerloop.ErrQueueFull):
		return protocol.WrapError(protocol.CodeUnavailable, "player loop queue full", err)
	default:
		return protocol.WrapError(protocol.CodeInternal, "submit player packet", err)
	}
}

func (s *Service) publishProtocolError(ctx context.Context, packet contracts.GatewayClientPacket, err error) error {
	appErr := protocol.ErrorFrom(err)
	s.incMetric("logic_protocol_errors_total", map[string]string{"code": appErr.Code.String(), "msg_id": fmt.Sprint(packet.MsgID)})
	if s.logger != nil {
		s.logger.Error("logic dispatch failed", "account_id", packet.AccountID, "msg_id", packet.MsgID, "code", appErr.Code.String(), "error", err)
	}
	now := time.Now()
	errorResp := protocol.PublicErrorResponse(appErr, now.Unix())
	return s.publishPlayerResp(ctx, contracts.LogicPlayerResponse{
		TargetGatewayNodeID: packet.GatewayNodeID,
		SessionID:           packet.SessionID,
		AccountID:           packet.AccountID,
		PacketID:            protocol.PacketIDReqResp,
		MsgID:               packet.MsgID,
		Sequence:            packet.Sequence,
		WireFormat:          packet.WireFormat,
		Body:                protocol.EncodeErrorResponse(errorResp),
		Metadata:            protocol.CloneFrameMetadata(packet.Metadata),
		Note:                fmt.Sprintf("ProtocolError:%s", appErr.Code.String()),
		CreatedAt:           now.UTC(),
	})
}
