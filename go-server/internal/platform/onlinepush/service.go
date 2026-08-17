package onlinepush

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"longheng.io/server/internal/platform/bus"
	"longheng.io/server/internal/platform/presence"
	"longheng.io/server/pkg/contracts"
	"longheng.io/server/pkg/protocol"
)

type Options struct {
	Bus      bus.Bus
	Presence presence.Runtime
	Store    Store
	Now      func() time.Time
}

// Service 负责把业务消息投递到在线网关或保存为离线消息。
type Service struct {
	bus      bus.Bus
	presence presence.Runtime
	store    Store
	now      func() time.Time
}

// New 创建在线推送服务。
func New(options Options) *Service {
	store := options.Store
	if store == nil {
		store = NewMemoryStore()
	}
	return &Service{
		bus:      options.Bus,
		presence: options.Presence,
		store:    store,
		now:      options.Now,
	}
}

// Send 按幂等键投递在线推送请求。
func (s *Service) Send(ctx context.Context, request Request) (Receipt, error) {
	return s.send(ctx, request, false)
}

// Retry 只对可重试的失败或部分成功 receipt 重新投递。
func (s *Service) Retry(ctx context.Context, request Request) (Receipt, error) {
	return s.send(ctx, request, true)
}

func (s *Service) send(ctx context.Context, request Request, retryPartial bool) (Receipt, error) {
	if err := ctxErr(ctx); err != nil {
		return Receipt{}, err
	}
	if s == nil || s.bus == nil {
		return Receipt{}, ErrBusRequired
	}
	if s.store == nil {
		return Receipt{}, ErrStoreRequired
	}
	request = normalizeRequest(request, s.now)
	if request.IdempotencyKey == "" {
		return Receipt{}, ErrIdempotencyRequired
	}
	if !request.Broadcast && request.SessionID == "" && len(pushAccounts(request)) == 0 {
		return Receipt{}, ErrTargetRequired
	}

	receipt := newReceipt(request, s.now)
	receipt, duplicate, err := s.store.ReserveReceipt(ctx, receipt)
	if err != nil {
		return receipt, err
	}
	if duplicate {
		if !retryPartial || !retryableReceipt(receipt) {
			return receipt, nil
		}
		receipt = resetReceiptForRetry(receipt, request, s.now)
	}
	receipt = s.deliver(ctx, request, receipt)
	receipt.UpdatedAt = nowUTC(s.now)
	if err := s.store.UpdateReceipt(ctx, receipt); err != nil {
		return receipt, err
	}
	if len(receipt.Errors) > 0 {
		return receipt, errors.New(strings.Join(receipt.Errors, "; "))
	}
	return receipt, nil
}

// Offline 查询指定账号的离线消息。
func (s *Service) Offline(ctx context.Context, accountID string, limit int) ([]OfflineMessage, error) {
	if s == nil || s.store == nil {
		return nil, ErrStoreRequired
	}
	return s.store.ListOffline(ctx, accountID, limit)
}

// DeleteOffline 删除指定离线消息。
func (s *Service) DeleteOffline(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return ErrStoreRequired
	}
	return s.store.DeleteOffline(ctx, id)
}

// Snapshot 返回在线推送服务状态摘要。
func (s *Service) Snapshot(ctx context.Context) Snapshot {
	if s == nil || s.store == nil {
		return Snapshot{}
	}
	return s.store.Snapshot(ctx)
}

func (s *Service) deliver(ctx context.Context, request Request, receipt Receipt) Receipt {
	if request.Broadcast {
		if err := s.publish(ctx, "", gatewayPushFor(request, "", "", "")); err != nil {
			receipt.Errors = append(receipt.Errors, err.Error())
		} else {
			receipt.Published++
		}
		return finalizeReceipt(receipt)
	}
	if request.SessionID != "" {
		if err := s.publish(ctx, request.TargetGatewayNodeID, gatewayPushFor(request, request.TargetGatewayNodeID, request.SessionID, request.AccountID)); err != nil {
			receipt.Errors = append(receipt.Errors, err.Error())
		} else {
			receipt.Published++
		}
		return finalizeReceipt(receipt)
	}
	for _, accountID := range pushAccounts(request) {
		receipt = s.deliverAccount(ctx, request, receipt, accountID)
	}
	return finalizeReceipt(receipt)
}

func (s *Service) deliverAccount(ctx context.Context, request Request, receipt Receipt, accountID string) Receipt {
	if s.presence == nil {
		if err := s.publish(ctx, request.TargetGatewayNodeID, gatewayPushFor(request, request.TargetGatewayNodeID, "", accountID)); err != nil {
			receipt.Errors = append(receipt.Errors, err.Error())
		} else {
			receipt.Published++
		}
		return receipt
	}
	items, err := s.presence.Presence(ctx, accountID)
	if err != nil {
		receipt.Errors = append(receipt.Errors, err.Error())
		return receipt
	}
	if len(items) == 0 {
		return s.saveOrDropOffline(ctx, request, receipt, accountID)
	}
	for _, item := range items {
		if err := s.publish(ctx, item.NodeID, gatewayPushFor(request, item.NodeID, item.SessionID, accountID)); err != nil {
			receipt.Errors = append(receipt.Errors, err.Error())
			continue
		}
		receipt.Published++
	}
	return receipt
}

func (s *Service) saveOrDropOffline(ctx context.Context, request Request, receipt Receipt, accountID string) Receipt {
	if request.OfflinePolicy == OfflineDrop {
		receipt.Dropped++
		return receipt
	}
	message := OfflineMessage{
		ID:        hashID("offline-", receipt.ID, accountID),
		ReceiptID: receipt.ID,
		AccountID: accountID,
		PacketID:  request.PacketID,
		MsgID:     request.MsgID,
		Body:      append([]byte(nil), request.Body...),
		Metadata:  cloneStringMap(request.Metadata),
		Note:      request.Note,
		Status:    StatusOffline,
		CreatedAt: nowUTC(s.now),
	}
	if err := s.store.SaveOffline(ctx, message); err != nil {
		receipt.Errors = append(receipt.Errors, err.Error())
		return receipt
	}
	receipt.Offline++
	return receipt
}

func (s *Service) publish(ctx context.Context, nodeID string, push contracts.GatewayPush) error {
	topic := contracts.TopicGatewayPush
	if nodeID = strings.TrimSpace(nodeID); nodeID != "" {
		topic = contracts.GatewayNodePushTopic(nodeID)
	}
	return s.bus.Publish(ctx, topic, push)
}

func gatewayPushFor(request Request, nodeID string, sessionID string, accountID string) contracts.GatewayPush {
	return contracts.GatewayPush{
		TargetGatewayNodeID: strings.TrimSpace(nodeID),
		SessionID:           strings.TrimSpace(sessionID),
		AccountID:           strings.TrimSpace(accountID),
		Broadcast:           request.Broadcast,
		PacketID:            request.PacketID,
		MsgID:               request.MsgID,
		Sequence:            request.Sequence,
		WireFormat:          request.WireFormat,
		Compressed:          request.Compressed,
		Body:                append([]byte(nil), request.Body...),
		Metadata:            cloneStringMap(request.Metadata),
		Note:                request.Note,
		CreatedAt:           request.CreatedAt,
	}
}

func normalizeRequest(request Request, now func() time.Time) Request {
	request.ID = strings.TrimSpace(request.ID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.AccountIDs = uniqueStrings([]string{request.AccountID}, request.AccountIDs)
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.TargetGatewayNodeID = strings.TrimSpace(request.TargetGatewayNodeID)
	request.WireFormat = protocol.NormalizeWireFormat(strings.TrimSpace(request.WireFormat))
	request.Metadata = cloneStringMap(request.Metadata)
	request.Note = strings.TrimSpace(request.Note)
	request.OfflinePolicy = strings.ToLower(strings.TrimSpace(request.OfflinePolicy))
	if request.OfflinePolicy == "" {
		request.OfflinePolicy = OfflineStore
	}
	if request.CreatedAt.IsZero() {
		request.CreatedAt = nowUTC(now)
	} else {
		request.CreatedAt = request.CreatedAt.UTC()
	}
	request.Body = append([]byte(nil), request.Body...)
	return request
}

func newReceipt(request Request, now func() time.Time) Receipt {
	id := request.ID
	if id == "" {
		id = hashID("push-", request.IdempotencyKey, describeTarget(request))
	}
	at := nowUTC(now)
	return Receipt{
		ID:             id,
		RequestID:      request.ID,
		IdempotencyKey: request.IdempotencyKey,
		Target:         describeTarget(request),
		Status:         StatusAccepted,
		CreatedAt:      at,
		UpdatedAt:      at,
	}
}

func finalizeReceipt(receipt Receipt) Receipt {
	switch {
	case len(receipt.Errors) > 0 && receipt.Published == 0 && receipt.Offline == 0 && receipt.Dropped == 0:
		receipt.Status = StatusFailed
	case len(receipt.Errors) > 0 || (receipt.Published > 0 && (receipt.Offline > 0 || receipt.Dropped > 0)):
		receipt.Status = StatusPartial
	case receipt.Published > 0:
		receipt.Status = StatusPublished
	case receipt.Offline > 0:
		receipt.Status = StatusOffline
	case receipt.Dropped > 0:
		receipt.Status = StatusDropped
	default:
		receipt.Status = StatusFailed
		receipt.Errors = append(receipt.Errors, "no target was delivered")
	}
	return receipt
}

func retryableReceipt(receipt Receipt) bool {
	switch receipt.Status {
	case StatusAccepted, StatusPartial, StatusFailed:
		return true
	default:
		return false
	}
}

func resetReceiptForRetry(receipt Receipt, request Request, now func() time.Time) Receipt {
	receipt.Duplicate = false
	receipt.Target = describeTarget(request)
	receipt.Published = 0
	receipt.Offline = 0
	receipt.Dropped = 0
	receipt.Errors = nil
	receipt.Status = StatusAccepted
	receipt.UpdatedAt = nowUTC(now)
	return receipt
}

func pushAccounts(request Request) []string {
	return uniqueStrings(request.AccountIDs)
}

func describeTarget(request Request) string {
	switch {
	case request.Broadcast:
		return "broadcast"
	case request.SessionID != "":
		return "session:" + request.SessionID
	case len(request.AccountIDs) == 1:
		return "account:" + request.AccountIDs[0]
	case len(request.AccountIDs) > 1:
		return fmt.Sprintf("accounts:%d", len(request.AccountIDs))
	default:
		return ""
	}
}
