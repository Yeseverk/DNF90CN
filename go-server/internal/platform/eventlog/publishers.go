package eventlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"longheng.io/server/internal/platform/bus"
	"longheng.io/server/pkg/contracts"
)

var (
	// ErrInvalidPublisherConfig 表示发布器配置缺少必要字段。
	ErrInvalidPublisherConfig = errors.New("eventlog publisher config is invalid")

	// ErrPermanentPublishFailure 表示发布失败不可重试，应直接进入死信。
	ErrPermanentPublishFailure = errors.New("eventlog publisher failure is permanent")
)

const (
	// HeaderOriginalTopic 表示 outbox 事件需要恢复投递到的原始业务 topic。
	HeaderOriginalTopic = "original_topic"
)

// TopicFunc 根据事件计算下游 topic。
type TopicFunc func(Event) string

// PayloadFunc 根据事件构造 Bus 发布负载。
type PayloadFunc func(Event) (any, error)

// FanoutPublisher 将同一事件投递给多个 Publisher。
type FanoutPublisher struct {
	publishers []Publisher
}

// NewFanoutPublisher 创建扇出发布器并过滤 nil publisher。
func NewFanoutPublisher(publishers ...Publisher) (*FanoutPublisher, error) {
	filtered := make([]Publisher, 0, len(publishers))
	for _, publisher := range publishers {
		if publisher != nil {
			filtered = append(filtered, publisher)
		}
	}
	if len(filtered) == 0 {
		return nil, ErrPublisherRequired
	}
	return &FanoutPublisher{publishers: filtered}, nil
}

// Publish 依次调用所有子 publisher，并聚合发布错误。
func (p *FanoutPublisher) Publish(ctx context.Context, event Event) error {
	if p == nil || len(p.publishers) == 0 {
		return ErrPublisherRequired
	}
	var errs []error
	// Fanout 会尝试所有 publisher 并聚合错误；任一失败都会让 EventLog 保持可重试状态。
	for _, publisher := range p.publishers {
		if err := publisher.Publish(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Message 是发送给通用消息下游的规范化事件消息。
type Message struct {
	Topic       string            `json:"topic"`
	Key         string            `json:"key,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// MessageSink 是通用消息发布下游的最小接口。
type MessageSink interface {
	PublishMessage(context.Context, Message) error
}

// MessageSinkFunc 允许用函数快速实现 MessageSink。
type MessageSinkFunc func(context.Context, Message) error

// PublishMessage 调用底层函数并保护 nil 函数场景。
func (fn MessageSinkFunc) PublishMessage(ctx context.Context, message Message) error {
	if fn == nil {
		return ErrPublisherRequired
	}
	return fn(ctx, message)
}

// KeyFunc 根据事件计算消息 key。
type KeyFunc func(Event) string

// MessagePublisherOptions 描述通用消息发布器的 topic、key、头和正文策略。
type MessagePublisherOptions struct {
	Sink    MessageSink
	Topic   TopicFunc
	Key     KeyFunc
	Headers map[string]string
	Body    HTTPBodyFunc
}

// MessagePublisher 把 eventlog 事件转换成 MessageSink 可消费的消息。
type MessagePublisher struct {
	sink    MessageSink
	topic   TopicFunc
	key     KeyFunc
	headers map[string]string
	body    HTTPBodyFunc
}

// NewMessagePublisher 创建通用消息发布器并补齐默认 topic、key 和正文策略。
func NewMessagePublisher(options MessagePublisherOptions) (*MessagePublisher, error) {
	if options.Sink == nil {
		return nil, fmt.Errorf("%w: message sink is required", ErrInvalidPublisherConfig)
	}
	topic := options.Topic
	if topic == nil {
		topic = DefaultTopic
	}
	key := options.Key
	if key == nil {
		key = DefaultMessageKey
	}
	body := options.Body
	if body == nil {
		body = EventJSONBody
	}
	return &MessagePublisher{
		sink:    options.Sink,
		topic:   topic,
		key:     key,
		headers: cloneStringMap(options.Headers),
		body:    body,
	}, nil
}

// Publish 构造消息并投递到 MessageSink。
func (p *MessagePublisher) Publish(ctx context.Context, event Event) error {
	if p == nil || p.sink == nil || p.topic == nil || p.key == nil || p.body == nil {
		return ErrPublisherRequired
	}
	topic := strings.TrimSpace(p.topic(event))
	if topic == "" {
		return fmt.Errorf("%w: topic is required", ErrInvalidPublisherConfig)
	}
	body, contentType, err := p.body(event)
	if err != nil {
		return err
	}
	headers := cloneStringMap(p.headers)
	if headers == nil {
		headers = map[string]string{}
	}
	setMessageHeader(headers, "event_id", event.ID)
	setMessageHeader(headers, "event_stream", event.Stream)
	setMessageHeader(headers, "event_type", event.Type)
	setMessageHeader(headers, "event_aggregate_id", event.AggregateID)
	setMessageHeader(headers, "event_idempotency_key", event.IdempotencyKey)
	return p.sink.PublishMessage(ctx, Message{
		Topic:       topic,
		Key:         strings.TrimSpace(p.key(event)),
		ContentType: contentType,
		Payload:     append([]byte(nil), body...),
		Headers:     headers,
	})
}

// BusPublisherOptions 描述事件发布到平台 Bus 时的 topic 和 payload 策略。
type BusPublisherOptions struct {
	Topic   TopicFunc
	Payload PayloadFunc
}

// BusPublisher 把 eventlog 事件发布到平台 Bus。
type BusPublisher struct {
	bus     bus.Bus
	topic   TopicFunc
	payload PayloadFunc
}

// NewBusPublisher 创建 Bus 发布器并补齐默认 topic 和 payload 策略。
func NewBusPublisher(eventBus bus.Bus, options BusPublisherOptions) (*BusPublisher, error) {
	if eventBus == nil {
		return nil, fmt.Errorf("%w: bus is required", ErrInvalidPublisherConfig)
	}
	topic := options.Topic
	if topic == nil {
		topic = DefaultTopic
	}
	payload := options.Payload
	if payload == nil {
		payload = RawJSONPayload
	}
	return &BusPublisher{bus: eventBus, topic: topic, payload: payload}, nil
}

// Publish 将事件转换后发布到平台 Bus。
func (p *BusPublisher) Publish(ctx context.Context, event Event) error {
	if p == nil || p.bus == nil || p.topic == nil || p.payload == nil {
		return ErrPublisherRequired
	}
	topic := strings.TrimSpace(p.topic(event))
	if topic == "" {
		return fmt.Errorf("%w: topic is required", ErrInvalidPublisherConfig)
	}
	payload, err := p.payload(event)
	if err != nil {
		return err
	}
	// BusPublisher 的可靠性只等于底层 Bus；Redis/NATS Pub/Sub 成功不代表消费者持久确认。
	return p.bus.Publish(ctx, topic, payload)
}

// HTTPBodyFunc 根据事件生成 HTTP 请求体和 content-type。
type HTTPBodyFunc func(Event) ([]byte, string, error)

// HTTPPublisherOptions 描述 webhook 发布器的地址、方法、客户端和正文策略。
type HTTPPublisherOptions struct {
	URL     string
	Method  string
	Client  *http.Client
	Headers map[string]string
	Body    HTTPBodyFunc
}

// HTTPPublisher 把 eventlog 事件投递到 HTTP webhook。
type HTTPPublisher struct {
	url     string
	method  string
	client  *http.Client
	headers map[string]string
	body    HTTPBodyFunc
}

// NewHTTPPublisher 创建 webhook 发布器并补齐默认 HTTP 客户端和正文策略。
func NewHTTPPublisher(options HTTPPublisherOptions) (*HTTPPublisher, error) {
	url := strings.TrimSpace(options.URL)
	if url == "" {
		return nil, fmt.Errorf("%w: url is required", ErrInvalidPublisherConfig)
	}
	method := strings.TrimSpace(options.Method)
	if method == "" {
		method = http.MethodPost
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	body := options.Body
	if body == nil {
		body = EventJSONBody
	}
	return &HTTPPublisher{
		url:     url,
		method:  method,
		client:  client,
		headers: cloneStringMap(options.Headers),
		body:    body,
	}, nil
}

// Publish 发送 webhook 请求，并把 4xx 响应归类为永久失败。
func (p *HTTPPublisher) Publish(ctx context.Context, event Event) error {
	if p == nil || p.client == nil || p.body == nil {
		return ErrPublisherRequired
	}
	body, contentType, err := p.body(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, p.method, p.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range p.headers {
		req.Header.Set(key, value)
	}
	setEventHeader(req.Header, "X-Event-ID", event.ID)
	setEventHeader(req.Header, "X-Event-Stream", event.Stream)
	setEventHeader(req.Header, "X-Event-Type", event.Type)
	setEventHeader(req.Header, "X-Event-Aggregate-ID", event.AggregateID)
	setEventHeader(req.Header, "X-Event-Idempotency-Key", event.IdempotencyKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	// HTTP 4xx 通常是不可重试的契约错误，直接进入死信，避免把坏事件反复打到下游。
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	message := strings.TrimSpace(string(data))
	if message == "" {
		message = resp.Status
	}
	publishErr := fmt.Errorf("eventlog webhook returned %s: %s", resp.Status, message)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return fmt.Errorf("%w: %w", ErrPermanentPublishFailure, publishErr)
	}
	return publishErr
}

// DefaultTopic 使用 stream 和 type 组合默认 topic。
func DefaultTopic(event Event) string {
	stream := strings.TrimSpace(event.Stream)
	eventType := strings.TrimSpace(event.Type)
	switch {
	case stream == "":
		return eventType
	case eventType == "":
		return stream
	default:
		return stream + "." + eventType
	}
}

// OriginalTopic 优先使用事件头中记录的原始 topic，缺失时回退到 stream.type。
func OriginalTopic(event Event) string {
	if event.Headers != nil {
		if topic := strings.TrimSpace(event.Headers[HeaderOriginalTopic]); topic != "" {
			return topic
		}
	}
	return DefaultTopic(event)
}

// DefaultMessageKey 优先使用幂等键，缺失时退回事件 ID。
func DefaultMessageKey(event Event) string {
	if key := strings.TrimSpace(event.IdempotencyKey); key != "" {
		return key
	}
	return strings.TrimSpace(event.ID)
}

// RawJSONPayload 返回事件原始 payload 副本。
func RawJSONPayload(event Event) (any, error) {
	return cloneRawMessage(event.Payload), nil
}

// OriginalTopicPayload 只在事件声明 original_topic 时按契约解码 payload，避免恢复事件在 Memory bus 中被 typed 订阅者跳过。
func OriginalTopicPayload(event Event) (any, error) {
	if event.Headers == nil || strings.TrimSpace(event.Headers[HeaderOriginalTopic]) == "" {
		return RawJSONPayload(event)
	}
	return contracts.UnmarshalTopicPayload(OriginalTopic(event), cloneRawMessage(event.Payload))
}

// EventJSONBody 把完整事件编码成 JSON 请求体。
func EventJSONBody(event Event) ([]byte, string, error) {
	data, err := json.Marshal(cloneEvent(event))
	if err != nil {
		return nil, "", err
	}
	return data, "application/json", nil
}

// PayloadJSONBody 把事件 payload 作为 JSON 请求体。
func PayloadJSONBody(event Event) ([]byte, string, error) {
	return cloneRawMessage(event.Payload), "application/json", nil
}

func setEventHeader(headers http.Header, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	headers.Set(key, value)
}

func setMessageHeader(headers map[string]string, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	headers[key] = value
}
