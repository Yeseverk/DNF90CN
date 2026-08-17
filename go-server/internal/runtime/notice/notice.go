package notice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/eventlog"
)

var (
	ErrStoreRequired        = errors.New("notice store is required")
	ErrNoticeIDRequired     = errors.New("notice id is required")
	ErrNoticeContentEmpty   = errors.New("notice title or body is required")
	ErrRecipientRequired    = errors.New("notice recipient is required")
	ErrIdempotencyRequired  = errors.New("notice idempotency key is required")
	ErrIdempotencyConflict  = errors.New("notice idempotency conflict")
	ErrNoticeExists         = errors.New("notice already exists")
	ErrNoticeNotFound       = errors.New("notice not found")
	ErrDeliveryNotFound     = errors.New("notice delivery not found")
	ErrNoticeInactive       = errors.New("notice is inactive")
	ErrInvalidNotice        = errors.New("notice is invalid")
	ErrInvalidNoticeKind    = errors.New("notice kind is invalid")
	ErrInvalidNoticeRequest = errors.New("notice request is invalid")
	ErrLivePublisherMissing = errors.New("notice live publisher is missing")
)

const (
	KindDirect       = "direct"
	KindAnnouncement = "announcement"

	StatusPending      = "pending"
	StatusAcknowledged = "acknowledged"
	StatusDeleted      = "deleted"

	defaultListLimit = 100
	maxListLimit     = 100
	rollbackTimeout  = 5 * time.Second
)

type Notice struct {
	ID            string            `json:"id"`
	Kind          string            `json:"kind"`
	Scope         string            `json:"scope,omitempty"`
	ShardID       string            `json:"shard_id,omitempty"`
	Title         string            `json:"title,omitempty"`
	Body          string            `json:"body,omitempty"`
	AttachmentRef string            `json:"attachment_ref,omitempty"`
	StartsAt      time.Time         `json:"starts_at,omitempty"`
	EndsAt        time.Time         `json:"ends_at,omitempty"`
	Disabled      bool              `json:"disabled,omitempty"`
	Meta          map[string]string `json:"meta,omitempty"`
}

type PublishRequest struct {
	Notice         Notice            `json:"notice"`
	Recipients     []string          `json:"recipients,omitempty"`
	IdempotencyKey string            `json:"idempotency_key"`
	Meta           map[string]string `json:"meta,omitempty"`
	AdminCommand   *admincmd.Command `json:"admin_command,omitempty"`
}

type PublishResult struct {
	Accepted       bool              `json:"accepted"`
	Duplicate      bool              `json:"duplicate,omitempty"`
	Notice         Notice            `json:"notice"`
	Deliveries     []Delivery        `json:"deliveries,omitempty"`
	IdempotencyKey string            `json:"idempotency_key"`
	AdminReceiptID string            `json:"admin_receipt_id,omitempty"`
	Meta           map[string]string `json:"meta,omitempty"`
	PublishedAt    time.Time         `json:"published_at"`
	Live           LiveStatus        `json:"live,omitempty"`
}

type LiveStatus struct {
	Attempted bool   `json:"attempted,omitempty"`
	Delivered bool   `json:"delivered,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Delivery struct {
	ID             string            `json:"id"`
	NoticeID       string            `json:"notice_id"`
	AccountID      string            `json:"account_id"`
	ShardID        string            `json:"shard_id,omitempty"`
	Status         string            `json:"status"`
	AttachmentRef  string            `json:"attachment_ref,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Meta           map[string]string `json:"meta,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	AcknowledgedAt time.Time         `json:"acknowledged_at,omitempty"`
}

type AcknowledgeRequest struct {
	DeliveryID     string `json:"delivery_id,omitempty"`
	NoticeID       string `json:"notice_id,omitempty"`
	AccountID      string `json:"account_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type DeleteRequest struct {
	DeliveryIDs []string `json:"delivery_ids,omitempty"`
	NoticeID    string   `json:"notice_id,omitempty"`
	AccountID   string   `json:"account_id,omitempty"`
}

type DeleteResult struct {
	DeletedIDs []string `json:"deleted_ids,omitempty"`
	MissingIDs []string `json:"missing_ids,omitempty"`
}

type Query struct {
	AccountID string    `json:"account_id,omitempty"`
	ShardID   string    `json:"shard_id,omitempty"`
	Now       time.Time `json:"now,omitempty"`
	Limit     int       `json:"limit,omitempty"`
	Cursor    string    `json:"cursor,omitempty"`
}

type ListResult struct {
	Items      []Delivery `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type Snapshot struct {
	Notices         int            `json:"notices"`
	Deliveries      int            `json:"deliveries"`
	ByKind          map[string]int `json:"by_kind,omitempty"`
	ByStatus        map[string]int `json:"by_status,omitempty"`
	PublishKeys     int            `json:"publish_keys"`
	AcknowledgeKeys int            `json:"acknowledge_keys"`
}

type Store interface {
	Publish(context.Context, PublishRequest, time.Time) (PublishResult, error)
	Acknowledge(context.Context, AcknowledgeRequest, time.Time) (Delivery, bool, error)
	Delete(context.Context, DeleteRequest) (DeleteResult, error)
	ListForAccount(context.Context, Query) ([]Delivery, error)
	ActiveAnnouncements(context.Context, Query) ([]Notice, error)
	Snapshot(context.Context) (Snapshot, error)
}

type LivePublisher interface {
	PublishNotice(context.Context, PublishResult) error
}

type Service struct {
	Store          Store
	Publisher      LivePublisher
	EventLog       *eventlog.Log
	EventLogStream string
	EventLogType   string
	Now            func() time.Time
}

func (s Service) Publish(ctx context.Context, request PublishRequest) (PublishResult, error) {
	if err := ctxErr(ctx); err != nil {
		return PublishResult{}, err
	}
	if s.Store == nil {
		return PublishResult{}, ErrStoreRequired
	}
	request = normPublishReq(request)
	if err := validPublishReq(request); err != nil {
		return PublishResult{}, err
	}
	if request.AdminCommand != nil {
		command := *request.AdminCommand
		command.Operation = firstNonEmpty(command.Operation, "notice.publish")
		command.Scope = firstNonEmpty(command.Scope, "notice")
		command.Target = firstNonEmpty(command.Target, request.Notice.ID)
		command.Params = mergeCommandParams(command.Params, request)
		if err := admincmd.Validate(command, admincmd.DangerousPolicy()); err != nil {
			return PublishResult{}, err
		}
		receipt, err := admincmd.NewReceipt(command, "accepted", s.now())
		if err != nil {
			return PublishResult{}, err
		}
		request.AdminCommand = &command
		request.Meta = mergeStringMap(request.Meta, map[string]string{"admin_receipt_id": receipt.ID})
	}
	if result, ok, err := s.lookupPublishEvent(ctx, request); err != nil || ok {
		return result, err
	}
	result, err := s.Store.Publish(ctx, request, s.now())
	if err != nil {
		return result, err
	}
	if err := s.appendPublishEvent(ctx, result); err != nil {
		return result, errors.Join(err, s.rollbackPublish(ctx, result))
	}
	if result.Duplicate || s.Publisher == nil {
		return result, err
	}
	result.Live.Attempted = true
	publishErr := s.Publisher.PublishNotice(ctx, result)
	if publishErr != nil {
		result.Live.Error = publishErr.Error()
	} else {
		result.Live.Delivered = true
	}
	return result, nil
}

type publishRollbackStore interface {
	RollbackPublish(context.Context, PublishResult) error
}

func (s Service) rollbackPublish(ctx context.Context, result PublishResult) error {
	if result.Duplicate || s.Store == nil {
		return nil
	}
	store, ok := s.Store.(publishRollbackStore)
	if !ok {
		return nil
	}
	// 可靠性补偿不能继承已取消的请求 ctx，但要保留 trace/value 方便排障。
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	rollbackCtx, cancel := context.WithTimeout(base, rollbackTimeout)
	defer cancel()
	return store.RollbackPublish(rollbackCtx, result)
}

func (s Service) Acknowledge(ctx context.Context, request AcknowledgeRequest) (Delivery, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Delivery{}, false, err
	}
	if s.Store == nil {
		return Delivery{}, false, ErrStoreRequired
	}
	request = normalizeAckRequest(request)
	if request.IdempotencyKey == "" {
		return Delivery{}, false, ErrIdempotencyRequired
	}
	if request.DeliveryID == "" && (request.NoticeID == "" || request.AccountID == "") {
		return Delivery{}, false, ErrInvalidNoticeRequest
	}
	return s.Store.Acknowledge(ctx, request, s.now())
}

func (s Service) Delete(ctx context.Context, request DeleteRequest) (DeleteResult, error) {
	if err := ctxErr(ctx); err != nil {
		return DeleteResult{}, err
	}
	if s.Store == nil {
		return DeleteResult{}, ErrStoreRequired
	}
	request = normDeleteReq(request)
	return s.Store.Delete(ctx, request)
}

func (s Service) ListForAccount(ctx context.Context, query Query) ([]Delivery, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s.Store == nil {
		return nil, ErrStoreRequired
	}
	query.AccountID = strings.TrimSpace(query.AccountID)
	query.ShardID = strings.TrimSpace(query.ShardID)
	if query.AccountID == "" {
		return nil, ErrRecipientRequired
	}
	if query.Now.IsZero() {
		query.Now = s.now()
	} else {
		query.Now = query.Now.UTC()
	}
	return s.Store.ListForAccount(ctx, query)
}

func (s Service) ListPageForAccount(ctx context.Context, query Query) (ListResult, error) {
	deliveries, err := s.ListForAccount(ctx, query)
	if err != nil {
		return ListResult{}, err
	}
	return paginateDeliveries(deliveries, query), nil
}

func (s Service) ActiveAnnouncements(ctx context.Context, query Query) ([]Notice, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s.Store == nil {
		return nil, ErrStoreRequired
	}
	query.ShardID = strings.TrimSpace(query.ShardID)
	if query.Now.IsZero() {
		query.Now = s.now()
	} else {
		query.Now = query.Now.UTC()
	}
	return s.Store.ActiveAnnouncements(ctx, query)
}

func (s Service) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return Snapshot{}, err
	}
	if s.Store == nil {
		return Snapshot{}, ErrStoreRequired
	}
	return s.Store.Snapshot(ctx)
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

type MemoryStore struct {
	mu              sync.Mutex
	notices         map[string]Notice
	deliveries      map[string]Delivery
	deliveriesByAcc map[string][]string
	publishByKey    map[string]PublishResult
	ackByKey        map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		notices:         make(map[string]Notice),
		deliveries:      make(map[string]Delivery),
		deliveriesByAcc: make(map[string][]string),
		publishByKey:    make(map[string]PublishResult),
		ackByKey:        make(map[string]string),
	}
}

func (s *MemoryStore) Publish(ctx context.Context, request PublishRequest, now time.Time) (PublishResult, error) {
	if err := ctxErr(ctx); err != nil {
		return PublishResult{}, err
	}
	if s == nil {
		return PublishResult{}, ErrStoreRequired
	}
	request = normPublishReq(request)
	if err := validPublishReq(request); err != nil {
		return PublishResult{}, err
	}
	now = normalizeNow(now)

	s.mu.Lock()
	defer s.mu.Unlock()
	lookupKey := publishLookupKey(request)
	if existing, ok := s.publishByKey[lookupKey]; ok {
		if publishReplayClash(existing, request) {
			return PublishResult{}, ErrIdempotencyConflict
		}
		existing.Duplicate = true
		return clonePublishResult(existing), nil
	}
	if _, exists := s.notices[request.Notice.ID]; exists {
		return PublishResult{}, ErrNoticeExists
	}

	notice := request.Notice
	s.notices[notice.ID] = cloneNotice(notice)
	deliveries := make([]Delivery, 0, len(request.Recipients))
	for _, accountID := range request.Recipients {
		delivery := Delivery{
			ID:             DeliveryID(notice.ID, accountID),
			NoticeID:       notice.ID,
			AccountID:      accountID,
			ShardID:        notice.ShardID,
			Status:         StatusPending,
			AttachmentRef:  notice.AttachmentRef,
			IdempotencyKey: request.IdempotencyKey,
			Meta:           mergeStringMap(notice.Meta, request.Meta),
			CreatedAt:      now,
		}
		if _, exists := s.deliveries[delivery.ID]; !exists {
			s.deliveriesByAcc[accountID] = appendUnique(s.deliveriesByAcc[accountID], delivery.ID)
		}
		s.deliveries[delivery.ID] = cloneDelivery(delivery)
		deliveries = append(deliveries, cloneDelivery(delivery))
	}
	sortDeliveries(deliveries)
	result := PublishResult{
		Accepted:       true,
		Notice:         cloneNotice(notice),
		Deliveries:     deliveries,
		IdempotencyKey: request.IdempotencyKey,
		AdminReceiptID: request.Meta["admin_receipt_id"],
		Meta:           cloneStringMap(request.Meta),
		PublishedAt:    now,
	}
	s.publishByKey[lookupKey] = clonePublishResult(result)
	return clonePublishResult(result), nil
}

func (s *MemoryStore) RollbackPublish(ctx context.Context, result PublishResult) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	if result.Notice.ID == "" || result.IdempotencyKey == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lookupKey := publishLookupKey(PublishRequest{Notice: result.Notice, IdempotencyKey: result.IdempotencyKey})
	existing, ok := s.publishByKey[lookupKey]
	if !ok || existing.Notice.ID != result.Notice.ID {
		return nil
	}
	delete(s.publishByKey, lookupKey)
	rolledBackDeliveries := make(map[string]struct{}, len(result.Deliveries))
	for _, delivery := range result.Deliveries {
		rolledBackDeliveries[delivery.ID] = struct{}{}
		delete(s.deliveries, delivery.ID)
		s.deliveriesByAcc[delivery.AccountID] = removeString(s.deliveriesByAcc[delivery.AccountID], delivery.ID)
		if len(s.deliveriesByAcc[delivery.AccountID]) == 0 {
			delete(s.deliveriesByAcc, delivery.AccountID)
		}
	}
	for key, deliveryID := range s.ackByKey {
		if _, ok := rolledBackDeliveries[deliveryID]; ok {
			delete(s.ackByKey, key)
		}
	}
	delete(s.notices, result.Notice.ID)
	return nil
}

func (s *MemoryStore) Acknowledge(ctx context.Context, request AcknowledgeRequest, now time.Time) (Delivery, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Delivery{}, false, err
	}
	if s == nil {
		return Delivery{}, false, ErrStoreRequired
	}
	request = normalizeAckRequest(request)
	if request.IdempotencyKey == "" {
		return Delivery{}, false, ErrIdempotencyRequired
	}
	now = normalizeNow(now)

	s.mu.Lock()
	defer s.mu.Unlock()
	deliveryID := request.DeliveryID
	if deliveryID == "" {
		deliveryID = DeliveryID(request.NoticeID, request.AccountID)
	}
	lookupKey := acknowledgeLookupKey(deliveryID, request.IdempotencyKey)
	if deliveryID == "" {
		return Delivery{}, false, ErrInvalidNoticeRequest
	}
	if knownDeliveryID, ok := s.ackByKey[lookupKey]; ok {
		deliveryID = knownDeliveryID
		delivery, exists := s.deliveries[deliveryID]
		if !exists {
			return Delivery{}, true, ErrDeliveryNotFound
		}
		return cloneDelivery(delivery), true, nil
	}
	delivery, ok := s.deliveries[deliveryID]
	if !ok {
		return Delivery{}, false, ErrDeliveryNotFound
	}
	if delivery.Status == StatusDeleted {
		return Delivery{}, false, ErrDeliveryNotFound
	}
	if delivery.Status == StatusAcknowledged {
		s.ackByKey[lookupKey] = delivery.ID
		return cloneDelivery(delivery), true, nil
	}
	delivery.Status = StatusAcknowledged
	delivery.AcknowledgedAt = now
	s.deliveries[delivery.ID] = cloneDelivery(delivery)
	s.ackByKey[lookupKey] = delivery.ID
	return cloneDelivery(delivery), false, nil
}

func (s *MemoryStore) Delete(ctx context.Context, request DeleteRequest) (DeleteResult, error) {
	if err := ctxErr(ctx); err != nil {
		return DeleteResult{}, err
	}
	if s == nil {
		return DeleteResult{}, ErrStoreRequired
	}
	request = normDeleteReq(request)
	if len(request.DeliveryIDs) == 0 {
		return DeleteResult{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	result := DeleteResult{}
	for _, id := range request.DeliveryIDs {
		delivery, ok := s.deliveries[id]
		if !ok || delivery.Status == StatusDeleted || (request.AccountID != "" && delivery.AccountID != request.AccountID) {
			result.MissingIDs = append(result.MissingIDs, id)
			continue
		}
		delivery.Status = StatusDeleted
		s.deliveries[id] = cloneDelivery(delivery)
		result.DeletedIDs = append(result.DeletedIDs, id)
	}
	sort.Strings(result.DeletedIDs)
	sort.Strings(result.MissingIDs)
	return result, nil
}

func (s *MemoryStore) ListForAccount(ctx context.Context, query Query) ([]Delivery, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStoreRequired
	}
	query.AccountID = strings.TrimSpace(query.AccountID)
	query.ShardID = strings.TrimSpace(query.ShardID)
	if query.AccountID == "" {
		return nil, ErrRecipientRequired
	}
	now := normalizeNow(query.Now)
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := append([]string(nil), s.deliveriesByAcc[query.AccountID]...)
	out := make([]Delivery, 0, len(ids))
	for _, id := range ids {
		delivery, ok := s.deliveries[id]
		if !ok {
			continue
		}
		notice, ok := s.notices[delivery.NoticeID]
		if !ok || !noticeVisible(notice, query.ShardID, now) {
			continue
		}
		if delivery.Status == StatusDeleted {
			continue
		}
		out = append(out, cloneDelivery(delivery))
	}
	sortAcctDeliveries(out)
	return out, nil
}

func (s *MemoryStore) ActiveAnnouncements(ctx context.Context, query Query) ([]Notice, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStoreRequired
	}
	query.ShardID = strings.TrimSpace(query.ShardID)
	now := normalizeNow(query.Now)
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Notice, 0)
	for _, notice := range s.notices {
		if notice.Kind != KindAnnouncement || !noticeVisible(notice, query.ShardID, now) {
			continue
		}
		out = append(out, cloneNotice(notice))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemoryStore) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return Snapshot{}, err
	}
	if s == nil {
		return Snapshot{}, ErrStoreRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := Snapshot{
		Notices:         len(s.notices),
		Deliveries:      len(s.deliveries),
		ByKind:          make(map[string]int),
		ByStatus:        make(map[string]int),
		PublishKeys:     len(s.publishByKey),
		AcknowledgeKeys: len(s.ackByKey),
	}
	for _, notice := range s.notices {
		snapshot.ByKind[notice.Kind]++
	}
	for _, delivery := range s.deliveries {
		snapshot.ByStatus[delivery.Status]++
	}
	if len(snapshot.ByKind) == 0 {
		snapshot.ByKind = nil
	}
	if len(snapshot.ByStatus) == 0 {
		snapshot.ByStatus = nil
	}
	return snapshot, nil
}

func validPublishReq(request PublishRequest) error {
	notice := request.Notice
	if notice.ID == "" {
		return ErrNoticeIDRequired
	}
	if notice.Title == "" && notice.Body == "" {
		return ErrNoticeContentEmpty
	}
	if request.IdempotencyKey == "" {
		return ErrIdempotencyRequired
	}
	switch notice.Kind {
	case KindDirect:
		if len(request.Recipients) == 0 {
			return ErrRecipientRequired
		}
	case KindAnnouncement:
	default:
		return fmt.Errorf("%w: %s", ErrInvalidNoticeKind, notice.Kind)
	}
	if !notice.StartsAt.IsZero() && !notice.EndsAt.IsZero() && notice.EndsAt.Before(notice.StartsAt) {
		return fmt.Errorf("%w: ends_at is before starts_at", ErrInvalidNotice)
	}
	return nil
}

func normPublishReq(request PublishRequest) PublishRequest {
	request.Notice = normalizeNotice(request.Notice)
	request.Recipients = normalizeIDs(request.Recipients)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.Meta = normalizeStringMap(request.Meta)
	return request
}

func normalizeNotice(notice Notice) Notice {
	notice.ID = strings.TrimSpace(notice.ID)
	notice.Kind = strings.ToLower(strings.TrimSpace(notice.Kind))
	if notice.Kind == "" {
		notice.Kind = KindDirect
	}
	notice.Scope = strings.TrimSpace(notice.Scope)
	notice.ShardID = strings.TrimSpace(notice.ShardID)
	notice.Title = strings.TrimSpace(notice.Title)
	notice.Body = strings.TrimSpace(notice.Body)
	notice.AttachmentRef = strings.TrimSpace(notice.AttachmentRef)
	notice.StartsAt = normalizeTime(notice.StartsAt)
	notice.EndsAt = normalizeTime(notice.EndsAt)
	notice.Meta = normalizeStringMap(notice.Meta)
	return notice
}

func normalizeAckRequest(request AcknowledgeRequest) AcknowledgeRequest {
	request.DeliveryID = strings.TrimSpace(request.DeliveryID)
	request.NoticeID = strings.TrimSpace(request.NoticeID)
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	return request
}

func normDeleteReq(request DeleteRequest) DeleteRequest {
	request.NoticeID = strings.TrimSpace(request.NoticeID)
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.DeliveryIDs = normalizeIDs(request.DeliveryIDs)
	if len(request.DeliveryIDs) == 0 && request.NoticeID != "" && request.AccountID != "" {
		request.DeliveryIDs = []string{DeliveryID(request.NoticeID, request.AccountID)}
	}
	return request
}

func noticeVisible(notice Notice, shardID string, now time.Time) bool {
	if notice.Disabled {
		return false
	}
	if notice.ShardID != "" && shardID != "" && notice.ShardID != shardID {
		return false
	}
	if !notice.StartsAt.IsZero() && now.Before(notice.StartsAt) {
		return false
	}
	if !notice.EndsAt.IsZero() && now.After(notice.EndsAt) {
		return false
	}
	return true
}

func DeliveryID(noticeID, accountID string) string {
	seed := encodeNoticeKey(strings.TrimSpace(noticeID), strings.TrimSpace(accountID))
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:16])
}

func publishLookupKey(request PublishRequest) string {
	return encodeNoticeKey(strings.TrimSpace(request.Notice.ID), strings.TrimSpace(request.IdempotencyKey))
}

func publishReplayClash(existing PublishResult, request PublishRequest) bool {
	if !reflect.DeepEqual(existing.Notice, request.Notice) {
		return true
	}
	if !reflect.DeepEqual(publishRecipients(existing.Deliveries), request.Recipients) {
		return true
	}
	if !reflect.DeepEqual(existing.Meta, request.Meta) {
		return true
	}
	return false
}

func publishRecipients(deliveries []Delivery) []string {
	if len(deliveries) == 0 {
		return nil
	}
	out := make([]string, 0, len(deliveries))
	for _, delivery := range deliveries {
		out = append(out, delivery.AccountID)
	}
	sort.Strings(out)
	return out
}

func acknowledgeLookupKey(deliveryID, key string) string {
	return encodeNoticeKey(strings.TrimSpace(deliveryID), strings.TrimSpace(key))
}

// encodeNoticeKey 用长度前缀编码多段内部键，避免字段自带分隔符或控制字符时发生碰撞。
func encodeNoticeKey(parts ...string) string {
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteByte(':')
		builder.WriteString(part)
	}
	return builder.String()
}

func mergeCommandParams(params map[string]any, request PublishRequest) map[string]any {
	out := make(map[string]any, len(params)+5)
	for key, value := range params {
		out[key] = value
	}
	out["notice_id"] = request.Notice.ID
	out["kind"] = request.Notice.Kind
	out["shard_id"] = request.Notice.ShardID
	out["recipient_count"] = len(request.Recipients)
	out["idempotency_key"] = request.IdempotencyKey
	return out
}

func normalizeIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func removeString(values []string, value string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	for _, existing := range values {
		if existing != value {
			out = append(out, existing)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeNow(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func normalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func normalizeStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeStringMap(base map[string]string, overlays ...map[string]string) map[string]string {
	out := cloneStringMap(base)
	for _, overlay := range overlays {
		for key, value := range overlay {
			if out == nil {
				out = make(map[string]string)
			}
			out[key] = value
		}
	}
	return out
}

func cloneNotice(notice Notice) Notice {
	notice.Meta = cloneStringMap(notice.Meta)
	return notice
}

func cloneDelivery(delivery Delivery) Delivery {
	delivery.Meta = cloneStringMap(delivery.Meta)
	return delivery
}

func clonePublishResult(result PublishResult) PublishResult {
	result.Notice = cloneNotice(result.Notice)
	result.Deliveries = cloneDeliveries(result.Deliveries)
	result.Meta = cloneStringMap(result.Meta)
	return result
}

func cloneDeliveries(in []Delivery) []Delivery {
	if len(in) == 0 {
		return nil
	}
	out := make([]Delivery, len(in))
	for idx, delivery := range in {
		out[idx] = cloneDelivery(delivery)
	}
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func sortDeliveries(deliveries []Delivery) {
	sort.Slice(deliveries, func(i, j int) bool {
		if deliveries[i].AccountID != deliveries[j].AccountID {
			return deliveries[i].AccountID < deliveries[j].AccountID
		}
		return deliveries[i].ID < deliveries[j].ID
	})
}

func sortAcctDeliveries(deliveries []Delivery) {
	sort.Slice(deliveries, func(i, j int) bool {
		if !deliveries[i].CreatedAt.Equal(deliveries[j].CreatedAt) {
			return deliveries[i].CreatedAt.Before(deliveries[j].CreatedAt)
		}
		return deliveries[i].ID < deliveries[j].ID
	})
}

func paginateDeliveries(deliveries []Delivery, query Query) ListResult {
	sortAcctDeliveries(deliveries)
	limit := query.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	cursor := strings.TrimSpace(query.Cursor)
	start := 0
	if cursor != "" {
		for idx, delivery := range deliveries {
			if delivery.ID == cursor {
				start = idx + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(deliveries) {
		end = len(deliveries)
	}
	items := cloneDeliveries(deliveries[start:end])
	nextCursor := ""
	if end < len(deliveries) && len(items) > 0 {
		nextCursor = items[len(items)-1].ID
	}
	return ListResult{Items: items, NextCursor: nextCursor}
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
