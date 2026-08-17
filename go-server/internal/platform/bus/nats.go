package bus

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"longheng.io/server/pkg/contracts"
)

type NATS struct {
	logger       *slog.Logger
	prefix       string
	wireEncoding string

	mu      sync.Mutex
	closed  bool
	conn    *nats.Conn
	metrics *Metrics
}

type NATSOptions struct {
	Name                 string
	Timeout              time.Duration
	PingInterval         time.Duration
	MaxPingsOutstanding  int
	MaxReconnects        int
	ReconnectWait        time.Duration
	RetryOnFailedConnect bool
	WireEncoding         string
	Token                string
	Username             string
	Password             string
	Credentials          string
	TLSEnabled           bool
	TLSServerName        string
	CAFile               string
	CertFile             string
	KeyFile              string
}

func DefaultNATSOptions() NATSOptions {
	return NATSOptions{
		Name:                 "longheng-bus",
		Timeout:              5 * time.Second,
		PingInterval:         time.Second,
		MaxPingsOutstanding:  10,
		MaxReconnects:        -1,
		ReconnectWait:        time.Second,
		RetryOnFailedConnect: true,
		WireEncoding:         NATSWireBinary,
	}
}

func normalizeNATSOptions(options NATSOptions) NATSOptions {
	defaults := DefaultNATSOptions()
	if strings.TrimSpace(options.Name) == "" {
		options.Name = defaults.Name
	}
	if options.Timeout <= 0 {
		options.Timeout = defaults.Timeout
	}
	if options.PingInterval <= 0 {
		options.PingInterval = defaults.PingInterval
	}
	if options.MaxPingsOutstanding <= 0 {
		options.MaxPingsOutstanding = defaults.MaxPingsOutstanding
	}
	if options.MaxReconnects == 0 {
		options.MaxReconnects = defaults.MaxReconnects
	}
	if options.ReconnectWait <= 0 {
		options.ReconnectWait = defaults.ReconnectWait
	}
	if !options.RetryOnFailedConnect {
		options.RetryOnFailedConnect = defaults.RetryOnFailedConnect
	}
	if !isNATSWireEncoding(options.WireEncoding) {
		options.WireEncoding = defaults.WireEncoding
	}
	return options
}

// SetMetrics 在构造后挂接 Metrics 发射器，可与 Publish/Subscribe 并发调用。
func (b *NATS) SetMetrics(m *Metrics) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.metrics = m
	b.mu.Unlock()
}

func NewNATS(endpoints []string, namespace string, logger *slog.Logger) (*NATS, error) {
	return NewNATSWithOptions(endpoints, namespace, logger, DefaultNATSOptions())
}

func NewNATSWithOptions(endpoints []string, namespace string, logger *slog.Logger, options NATSOptions) (*NATS, error) {
	endpoints = normNATSEndpoints(endpoints)
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("nats bus requires at least one endpoint")
	}
	options = normalizeNATSOptions(options)

	connectOptions := []nats.Option{
		nats.Name(options.Name),
		nats.Timeout(options.Timeout),
		nats.PingInterval(options.PingInterval),
		nats.MaxPingsOutstanding(options.MaxPingsOutstanding),
		nats.RetryOnFailedConnect(options.RetryOnFailedConnect),
		nats.MaxReconnects(options.MaxReconnects),
		nats.ReconnectWait(options.ReconnectWait),
	}
	connectOptions = appendNATSSecOpts(connectOptions, options)
	conn, err := nats.Connect(
		strings.Join(endpoints, ","),
		connectOptions...,
	)
	if err != nil {
		return nil, err
	}

	return &NATS{
		logger:       logger,
		prefix:       strings.Trim(namespace, "."),
		wireEncoding: options.WireEncoding,
		conn:         conn,
	}, nil
}

func normNATSEndpoints(endpoints []string) []string {
	normalized := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint != "" {
			normalized = append(normalized, endpoint)
		}
	}
	return normalized
}

func appendNATSSecOpts(connectOptions []nats.Option, options NATSOptions) []nats.Option {
	if options.Token != "" {
		connectOptions = append(connectOptions, nats.Token(options.Token))
	}
	if options.Username != "" || options.Password != "" {
		connectOptions = append(connectOptions, nats.UserInfo(options.Username, options.Password))
	}
	if options.Credentials != "" {
		connectOptions = append(connectOptions, nats.UserCredentials(options.Credentials))
	}
	if options.TLSEnabled {
		connectOptions = append(connectOptions, nats.Secure(&tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: options.TLSServerName,
		}))
	}
	if options.CAFile != "" {
		connectOptions = append(connectOptions, nats.RootCAs(options.CAFile))
	}
	if options.CertFile != "" || options.KeyFile != "" {
		connectOptions = append(connectOptions, nats.ClientCert(options.CertFile, options.KeyFile))
	}
	return connectOptions
}

func (b *NATS) Publish(ctx context.Context, topic string, payload any) error {
	return b.publishEnvelope(ctx, Envelope{Topic: topic, Payload: payload, PublishedAt: time.Now().UTC()})
}

func (b *NATS) publishEnvelope(ctx context.Context, env Envelope) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if b != nil {
			b.mu.Lock()
			metricsRef := b.metrics
			b.mu.Unlock()
			metricsRef.recordPublish(classifyHandlerError(err))
		}
		return err
	}
	if b == nil {
		return ErrClosed
	}

	// NATS core publish 只提供在线投递，不保存消息、不等待消费者确认；关键状态变更必须走 EventLog/Outbox。
	b.mu.Lock()
	if b.closed {
		metricsRef := b.metrics
		b.mu.Unlock()
		metricsRef.recordPublish(ResultClosed)
		return ErrClosed
	}
	conn := b.conn
	metricsRef := b.metrics
	b.mu.Unlock()
	if conn == nil {
		metricsRef.recordPublish(ResultClosed)
		return ErrClosed
	}

	body, err := contracts.MarshalTopicPayload(env.Payload)
	if err != nil {
		metricsRef.recordPublish(ResultError)
		return err
	}
	if env.PublishedAt.IsZero() {
		env.PublishedAt = time.Now().UTC()
	}
	env = envelopeWithTrace(ctx, env)

	wire, err := encodeNATSWire(natsWireMessage{
		Topic:       env.Topic,
		Payload:     body,
		PublishedAt: env.PublishedAt,
		Reply:       env.Reply,
		RequestID:   env.RequestID,
		Trace:       env.Trace,
	}, b.wireEncoding)
	if err != nil {
		metricsRef.recordPublish(ResultError)
		return err
	}

	if err := conn.Publish(b.subject(env.Topic), wire); err != nil {
		metricsRef.recordPublish(ResultError)
		return err
	}
	metricsRef.recordPublish(ResultOK)
	metricsRef.recordPublishBytes(int64(len(body)))
	return nil
}

func (b *NATS) Request(ctx context.Context, topic string, payload any, timeout time.Duration) ([]byte, error) {
	return requestWithPubSub(ctx, topic, payload, timeout, b.Subscribe, b.publishEnvelope)
}

func (b *NATS) Subscribe(topic string, handler Handler) (Subscription, error) {
	return b.subscribe(topic, "", handler)
}

func (b *NATS) QueueSubscribe(topic string, queue string, handler Handler) (Subscription, error) {
	queue = normalizeQueueName(queue)
	if queue == "" {
		return b.Subscribe(topic, handler)
	}
	return b.subscribe(topic, queue, handler)
}

func (b *NATS) subscribe(topic string, queue string, handler Handler) (Subscription, error) {
	if handler == nil {
		return nil, ErrHandlerRequired
	}
	if b == nil {
		return nil, ErrClosed
	}
	b.mu.Lock()
	if b.closed {
		metricsRef := b.metrics
		b.mu.Unlock()
		metricsRef.recordSubscribe(ResultClosed)
		return nil, ErrClosed
	}
	conn := b.conn
	metricsRef := b.metrics
	b.mu.Unlock()
	if conn == nil {
		metricsRef.recordSubscribe(ResultClosed)
		return nil, ErrClosed
	}

	// 回调里只做解包和 handler deadline 保护；超时只能记录失败，不能中断已经进入业务 handler 的 goroutine。
	callback := func(msg *nats.Msg) {
		defer func() {
			if rec := recover(); rec != nil {
				if b.logger != nil {
					b.logger.Error("nats handler panic recovered", "topic", topic, "panic", rec)
				}
				metricsRef.recordHandler(ResultPanic)
			}
		}()

		wire, err := decodeNATSWire(msg.Data)
		if err != nil {
			if b.logger != nil {
				b.logger.Error("nats unmarshal failed", "topic", topic, "error", err)
			}
			metricsRef.recordHandler(ResultError)
			return
		}

		var payload any = json.RawMessage(wire.Payload)
		if !isRequestReplyTopic(topic) {
			decoded, err := contracts.UnmarshalTopicPayload(topic, wire.Payload)
			if err != nil {
				if b.logger != nil {
					b.logger.Error("nats payload decode failed", "topic", topic, "error", err)
				}
				metricsRef.recordHandler(ResultError)
				return
			}
			payload = decoded
		}

		publishedAt := wire.PublishedAt
		if publishedAt.IsZero() {
			publishedAt = time.Now().UTC()
		}
		reply := wire.Reply
		if reply == "" {
			reply = msg.Reply
		}

		env := Envelope{
			Topic:       topic,
			Payload:     payload,
			PublishedAt: publishedAt,
			Reply:       reply,
			RequestID:   wire.RequestID,
			Trace:       wire.Trace,
		}
		deliverWithDeadline(contextWithTrace(context.Background(), env), b.logger, topic, defHandlerTimeout, env, handler, metricsRef)
	}
	var sub *nats.Subscription
	var err error
	if queue == "" {
		sub, err = conn.Subscribe(b.subject(topic), callback)
	} else {
		sub, err = conn.QueueSubscribe(b.subject(topic), queue, callback)
	}
	if err != nil {
		metricsRef.recordSubscribe(ResultError)
		return nil, err
	}

	metricsRef.recordSubscribe(ResultOK)
	metricsRef.trackSubscription(1)
	return &natsSubscription{sub: sub, metrics: metricsRef}, nil
}

func (b *NATS) Check(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil {
		return ErrClosed
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrClosed
	}
	conn := b.conn
	b.mu.Unlock()

	if conn == nil {
		return ErrClosed
	}
	return conn.FlushWithContext(ctx)
}

func (b *NATS) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	conn := b.conn
	b.conn = nil
	b.mu.Unlock()

	// Drain 给正在发送的消息一个收敛窗口，但 NATS core 仍不是持久队列；关闭期间丢失的 live 消息不做补偿。
	if conn == nil {
		return nil
	}
	if err := conn.Drain(); err != nil {
		conn.Close()
		return err
	}
	conn.Close()
	return nil
}

func (b *NATS) subject(topic string) string {
	if b.prefix == "" {
		return topic
	}
	return b.prefix + "." + topic
}

type natsSubscription struct {
	sub     *nats.Subscription
	metrics *Metrics
	closed  bool
	mu      sync.Mutex
}

func (s *natsSubscription) Close() error {
	if s == nil || s.sub == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	err := s.sub.Unsubscribe()
	s.metrics.trackSubscription(-1)
	return err
}
