package bus

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"

	"longheng.io/server/pkg/contracts"
)

type Redis struct {
	logger   *slog.Logger
	endpoint string
	password string
	database int
	options  RedisOptions
	prefix   string
	pool     *redis.Pool

	mu      sync.Mutex
	closed  bool
	metrics *Metrics

	replyMu      sync.Mutex
	replySub     *redisSubscription
	replyWaiters map[string]chan requestResponse
}

type RedisOptions struct {
	Username      string
	Password      string
	Database      int
	TLSEnabled    bool
	TLSServerName string
}

// SetMetrics 在构造后挂接 Metrics 发射器。
func (b *Redis) SetMetrics(m *Metrics) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.metrics = m
	b.mu.Unlock()
}

func NewRedis(endpoints []string, namespace string, password string, database int, logger *slog.Logger) (*Redis, error) {
	return NewRedisWithOptions(endpoints, namespace, logger, RedisOptions{Password: password, Database: database})
}

func NewRedisWithOptions(endpoints []string, namespace string, logger *slog.Logger, options RedisOptions) (*Redis, error) {
	endpoint := firstEndpoint(endpoints)
	if endpoint == "" {
		return nil, fmt.Errorf("redis bus requires at least one endpoint")
	}
	pool := &redis.Pool{
		MaxIdle:     8,
		MaxActive:   32,
		IdleTimeout: time.Minute,
		Wait:        true,
		Dial: func() (redis.Conn, error) {
			dialOptions := []redis.DialOption{
				redis.DialConnectTimeout(5 * time.Second),
				redis.DialReadTimeout(5 * time.Second),
				redis.DialWriteTimeout(5 * time.Second),
			}
			dialOptions = appendRedisDialOpts(dialOptions, options)
			if options.Database > 0 {
				dialOptions = append(dialOptions, redis.DialDatabase(options.Database))
			}
			return redis.Dial(
				"tcp",
				endpoint,
				dialOptions...,
			)
		},
		TestOnBorrow: func(conn redis.Conn, lastUsed time.Time) error {
			if time.Since(lastUsed) < time.Minute {
				return nil
			}
			_, err := conn.Do("PING")
			return err
		},
	}
	return &Redis{
		logger:   logger,
		endpoint: endpoint,
		password: options.Password,
		database: options.Database,
		options:  options,
		prefix:   strings.Trim(namespace, "."),
		pool:     pool,
	}, nil
}

func appendRedisDialOpts(dialOptions []redis.DialOption, options RedisOptions) []redis.DialOption {
	if options.Username != "" {
		dialOptions = append(dialOptions, redis.DialUsername(options.Username))
	}
	if options.Password != "" {
		dialOptions = append(dialOptions, redis.DialPassword(options.Password))
	}
	if options.TLSEnabled {
		dialOptions = append(dialOptions, redis.DialUseTLS(true))
		if options.TLSServerName != "" {
			dialOptions = append(dialOptions, redis.DialTLSConfig(&tls.Config{
				MinVersion:         tls.VersionTLS12,
				ServerName:         options.TLSServerName,
				InsecureSkipVerify: false,
			}))
		}
	}
	return dialOptions
}

func (b *Redis) Publish(ctx context.Context, topic string, payload any) error {
	return b.publishEnvelope(ctx, Envelope{Topic: topic, Payload: payload, PublishedAt: time.Now().UTC()})
}

func (b *Redis) publishEnvelope(ctx context.Context, env Envelope) error {
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
	b.mu.Lock()
	if b.closed {
		metricsRef := b.metrics
		b.mu.Unlock()
		metricsRef.recordPublish(ResultClosed)
		return ErrClosed
	}
	pool := b.pool
	metricsRef := b.metrics
	b.mu.Unlock()
	if pool == nil {
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
	wire, err := json.Marshal(natsWireMessage{
		Topic:       env.Topic,
		Payload:     body,
		PublishedAt: env.PublishedAt,
		Reply:       env.Reply,
		RequestID:   env.RequestID,
		Trace:       env.Trace,
	})
	if err != nil {
		metricsRef.recordPublish(ResultError)
		return err
	}

	conn, err := pool.GetContext(ctx)
	if err != nil {
		metricsRef.recordPublish(ResultError)
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Do("PUBLISH", b.subject(env.Topic), wire); err != nil {
		metricsRef.recordPublish(ResultError)
		return err
	}
	metricsRef.recordPublish(ResultOK)
	metricsRef.recordPublishBytes(int64(len(body)))
	return nil
}

func (b *Redis) Request(ctx context.Context, topic string, payload any, timeout time.Duration) ([]byte, error) {
	if b == nil {
		return nil, ErrClosed
	}
	ctx, cancel := requestContext(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	requestID := newRequestID(topic)
	replyTopic := requestReplyTopic(topic, requestID)
	responses := make(chan requestResponse, 1)
	if err := b.registerReplyWaiter(replyTopic, responses); err != nil {
		return nil, err
	}
	defer b.unregReplyWaiter(replyTopic)

	if err := b.ensureReplySub(ctx); err != nil {
		return nil, err
	}
	if err := b.publishEnvelope(ctx, Envelope{
		Topic:       topic,
		Payload:     payload,
		PublishedAt: time.Now().UTC(),
		Reply:       replyTopic,
		RequestID:   requestID,
	}); err != nil {
		return nil, err
	}

	select {
	case result := <-responses:
		if result.err != nil {
			return nil, result.err
		}
		return result.data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *Redis) Subscribe(topic string, handler Handler) (Subscription, error) {
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
	endpoint := b.endpoint
	password := b.password
	database := b.database
	options := b.options
	metricsRef := b.metrics
	b.mu.Unlock()
	if endpoint == "" {
		metricsRef.recordSubscribe(ResultClosed)
		return nil, ErrClosed
	}

	// Redis Pub/Sub 没有历史消息，订阅建立前和重连期间的消息都会丢；这里只适合在线协调和推送。
	subject := b.subject(topic)
	sub := &redisSubscription{
		logger:   b.logger,
		endpoint: endpoint,
		password: password,
		database: database,
		options:  options,
		topic:    topic,
		subject:  subject,
		done:     make(chan struct{}),
		metrics:  metricsRef,
	}
	if err := sub.connect(); err != nil {
		metricsRef.recordSubscribe(ResultError)
		return nil, err
	}
	metricsRef.recordSubscribe(ResultOK)
	metricsRef.trackSubscription(1)
	go sub.receive(handler)
	return sub, nil
}

func (b *Redis) QueueSubscribe(topic string, queue string, handler Handler) (Subscription, error) {
	queue = normalizeQueueName(queue)
	if queue == "" {
		return b.Subscribe(topic, handler)
	}
	if handler == nil {
		return nil, ErrHandlerRequired
	}
	return nil, fmt.Errorf("%w: redis pubsub does not support queue groups", ErrUnsupported)
}

func (b *Redis) Check(ctx context.Context) error {
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
	pool := b.pool
	b.mu.Unlock()
	if pool == nil {
		return ErrClosed
	}
	conn, err := pool.GetContext(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_, err = redis.String(conn.Do("PING"))
	return err
}

func (b *Redis) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	pool := b.pool
	b.pool = nil
	b.mu.Unlock()
	b.replyMu.Lock()
	replySub := b.replySub
	b.replySub = nil
	for topic, waiter := range b.replyWaiters {
		delete(b.replyWaiters, topic)
		select {
		case waiter <- requestResponse{err: ErrClosed}:
		default:
		}
	}
	b.replyMu.Unlock()

	var err error
	if replySub != nil {
		err = errors.Join(err, replySub.Close())
	}
	if pool == nil {
		return err
	}
	return errors.Join(err, pool.Close())
}

func (b *Redis) subject(topic string) string {
	if b.prefix == "" {
		return topic
	}
	return b.prefix + "." + topic
}

type redisSubscription struct {
	logger   *slog.Logger
	endpoint string
	password string
	database int
	options  RedisOptions
	topic    string
	subject  string
	pattern  bool
	done     chan struct{}
	metrics  *Metrics

	mu   sync.Mutex
	psc  *redis.PubSubConn
	once sync.Once
	err  error
}

func (s *redisSubscription) receive(handler Handler) {
	// receive 常驻单 goroutine，连接断开后进入重连退避；关闭信号优先于重连，避免 Stop 卡住。
	for {
		if s.isDone() {
			return
		}

		psc := s.current()
		if psc == nil {
			if !s.reconnect() {
				return
			}
			continue
		}

		reply := psc.Receive()
		switch msg := reply.(type) {
		case redis.Message:
			s.handleMessage(handler, msg.Channel, msg.Data)
		case redis.Subscription:
			if msg.Count == 0 {
				if s.isDone() {
					return
				}
				s.clearCurrent(psc)
				if !s.reconnect() {
					return
				}
			}
		case error:
			if s.isDone() {
				return
			}
			if s.logger != nil {
				s.logger.Warn("redis bus subscription lost", "topic", s.topic, "error", msg)
			}
			s.clearCurrent(psc)
			if !s.reconnect() {
				return
			}
		}
	}
}

func (s *redisSubscription) handleMessage(handler Handler, channel string, data []byte) {
	defer func() {
		if rec := recover(); rec != nil {
			if s.logger != nil {
				s.logger.Error("redis bus handler panic recovered", "topic", s.topic, "panic", rec)
			}
			s.metrics.recordHandler(ResultPanic)
		}
	}()

	var wire natsWireMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		if s.logger != nil {
			s.logger.Error("redis bus unmarshal failed", "topic", s.topic, "error", err)
		}
		s.metrics.recordHandler(ResultError)
		return
	}
	var payload any = json.RawMessage(wire.Payload)
	topic := strings.TrimSpace(wire.Topic)
	if topic == "" {
		topic = s.topic
		if s.pattern {
			topic = s.topicFromPattern(channel)
		}
	}
	if !isRequestReplyTopic(topic) {
		decoded, err := contracts.UnmarshalTopicPayload(topic, wire.Payload)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("redis bus payload decode failed", "topic", topic, "error", err)
			}
			s.metrics.recordHandler(ResultError)
			return
		}
		payload = decoded
	}
	publishedAt := wire.PublishedAt
	if publishedAt.IsZero() {
		publishedAt = time.Now().UTC()
	}
	env := Envelope{
		Topic:       topic,
		Payload:     payload,
		PublishedAt: publishedAt,
		Reply:       wire.Reply,
		RequestID:   wire.RequestID,
		Trace:       wire.Trace,
	}
	deliverWithDeadline(contextWithTrace(context.Background(), env), s.logger, s.topic, defHandlerTimeout, env, handler, s.metrics)
}

func (s *redisSubscription) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		// Close 先关闭 done 再 Unsubscribe，唤醒 receive/reconnect 两条路径；未处理完的 Pub/Sub 消息不会重放。
		if s.done != nil {
			close(s.done)
		}
		s.mu.Lock()
		psc := s.psc
		s.psc = nil
		s.mu.Unlock()
		if psc != nil {
			if s.pattern {
				_ = psc.PUnsubscribe(s.subject)
			} else {
				_ = psc.Unsubscribe(s.subject)
			}
			s.err = psc.Close()
		}
		s.metrics.trackSubscription(-1)
	})
	return s.err
}

func (s *redisSubscription) connect() error {
	psc, err := s.open()
	if err != nil {
		return err
	}
	if !s.setCurrent(psc) {
		_ = psc.Close()
		return ErrClosed
	}
	return nil
}

func (s *redisSubscription) reconnect() bool {
	backoff := 250 * time.Millisecond
	// 重连使用指数退避，上限 5 秒；这段时间的 Redis Pub/Sub 消息天然不可恢复。
	for {
		if s.isDone() {
			return false
		}
		timer := time.NewTimer(backoff)
		select {
		case <-s.done:
			timer.Stop()
			return false
		case <-timer.C:
		}

		psc, err := s.open()
		if err == nil {
			if !s.setCurrent(psc) {
				_ = psc.Close()
				return false
			}
			if s.logger != nil {
				s.logger.Info("redis bus subscription reconnected", "topic", s.topic)
			}
			return true
		}
		if s.logger != nil {
			s.logger.Warn("redis bus subscription reconnect failed", "topic", s.topic, "error", err)
		}
		if backoff < 5*time.Second {
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
}

func (s *redisSubscription) open() (*redis.PubSubConn, error) {
	dialOptions := []redis.DialOption{
		redis.DialConnectTimeout(5 * time.Second),
		redis.DialWriteTimeout(5 * time.Second),
	}
	options := s.options
	if options.Password == "" {
		options.Password = s.password
	}
	if options.Database == 0 {
		options.Database = s.database
	}
	dialOptions = appendRedisDialOpts(dialOptions, options)
	if options.Database > 0 {
		dialOptions = append(dialOptions, redis.DialDatabase(options.Database))
	}
	conn, err := redis.Dial(
		"tcp",
		s.endpoint,
		dialOptions...,
	)
	if err != nil {
		return nil, err
	}
	psc := &redis.PubSubConn{Conn: conn}
	if s.pattern {
		err = psc.PSubscribe(s.subject)
	} else {
		err = psc.Subscribe(s.subject)
	}
	if err != nil {
		_ = psc.Close()
		return nil, err
	}
	if err := waitRedisSubReady(psc, s.pattern); err != nil {
		_ = psc.Close()
		return nil, err
	}
	return psc, nil
}

func (s *redisSubscription) topicFromPattern(channel string) string {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return s.topic
	}
	subjectPrefix, subjectSuffix, subjectOK := splitRedisPattern(s.subject)
	topicPrefix, topicSuffix, topicOK := splitRedisPattern(s.topic)
	if !subjectOK || !topicOK {
		return channel
	}
	if !strings.HasPrefix(channel, subjectPrefix) || !strings.HasSuffix(channel, subjectSuffix) {
		return channel
	}
	middleEnd := len(channel) - len(subjectSuffix)
	if middleEnd < len(subjectPrefix) {
		return channel
	}
	middle := channel[len(subjectPrefix):middleEnd]
	return topicPrefix + middle + topicSuffix
}

func splitRedisPattern(pattern string) (prefix string, suffix string, ok bool) {
	pattern = strings.TrimSpace(pattern)
	idx := strings.Index(pattern, "*")
	if idx < 0 {
		return "", "", false
	}
	return pattern[:idx], pattern[idx+1:], true
}

func waitRedisSubReady(psc *redis.PubSubConn, pattern bool) error {
	if psc == nil {
		return ErrClosed
	}
	const ackTimeout = 2 * time.Second
	deadline := time.Now().Add(ackTimeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("redis subscription ack timeout")
		}
		reply := psc.ReceiveWithTimeout(remaining)
		switch msg := reply.(type) {
		case redis.Subscription:
			if msg.Count > 0 {
				if pattern && strings.EqualFold(msg.Kind, "psubscribe") {
					return nil
				}
				if !pattern && strings.EqualFold(msg.Kind, "subscribe") {
					return nil
				}
			}
		case error:
			return msg
		}
	}
}

func (b *Redis) registerReplyWaiter(replyTopic string, responses chan requestResponse) error {
	if b == nil {
		return ErrClosed
	}
	replyTopic = strings.TrimSpace(replyTopic)
	if replyTopic == "" {
		return fmt.Errorf("%w: request reply topic missing", ErrUnsupported)
	}
	b.replyMu.Lock()
	defer b.replyMu.Unlock()
	if b.replyWaiters == nil {
		b.replyWaiters = make(map[string]chan requestResponse)
	}
	if _, exists := b.replyWaiters[replyTopic]; exists {
		return fmt.Errorf("duplicate redis reply waiter %q", replyTopic)
	}
	b.replyWaiters[replyTopic] = responses
	return nil
}

func (b *Redis) unregReplyWaiter(replyTopic string) {
	if b == nil {
		return
	}
	b.replyMu.Lock()
	delete(b.replyWaiters, strings.TrimSpace(replyTopic))
	b.replyMu.Unlock()
}

func (b *Redis) ensureReplySub(ctx context.Context) error {
	if b == nil {
		return ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.replyMu.Lock()
	if b.replySub != nil && !b.replySub.isDone() {
		b.replyMu.Unlock()
		return nil
	}
	b.replyMu.Unlock()

	b.mu.Lock()
	if b.closed {
		metricsRef := b.metrics
		b.mu.Unlock()
		metricsRef.recordSubscribe(ResultClosed)
		return ErrClosed
	}
	endpoint := b.endpoint
	password := b.password
	database := b.database
	options := b.options
	logger := b.logger
	metricsRef := b.metrics
	subject := b.subject(requestReplyPrefix + "*")
	b.mu.Unlock()
	if endpoint == "" {
		metricsRef.recordSubscribe(ResultClosed)
		return ErrClosed
	}

	sub := &redisSubscription{
		logger:   logger,
		endpoint: endpoint,
		password: password,
		database: database,
		options:  options,
		topic:    requestReplyPrefix + "*",
		subject:  subject,
		pattern:  true,
		done:     make(chan struct{}),
		metrics:  metricsRef,
	}
	if err := sub.connect(); err != nil {
		metricsRef.recordSubscribe(ResultError)
		return err
	}
	metricsRef.recordSubscribe(ResultOK)
	metricsRef.trackSubscription(1)

	b.mu.Lock()
	b.replyMu.Lock()
	if b.closed {
		b.replyMu.Unlock()
		b.mu.Unlock()
		_ = sub.Close()
		metricsRef.recordSubscribe(ResultClosed)
		return ErrClosed
	}
	if b.replySub != nil && !b.replySub.isDone() {
		b.replyMu.Unlock()
		b.mu.Unlock()
		_ = sub.Close()
		return nil
	}
	b.replySub = sub
	b.replyMu.Unlock()
	b.mu.Unlock()

	go sub.receive(func(_ context.Context, env Envelope) error {
		b.dispatchReply(env)
		return nil
	})
	return nil
}

func (b *Redis) dispatchReply(env Envelope) {
	if b == nil {
		return
	}
	topic := strings.TrimSpace(env.Topic)
	if topic == "" {
		return
	}
	raw, err := marshalBusPayload(env.Payload)
	result := requestResponse{data: raw, err: err}
	b.replyMu.Lock()
	waiter := b.replyWaiters[topic]
	b.replyMu.Unlock()
	if waiter == nil {
		return
	}
	select {
	case waiter <- result:
	default:
	}
}

func (s *redisSubscription) current() *redis.PubSubConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.psc
}

func (s *redisSubscription) setCurrent(psc *redis.PubSubConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isDone() {
		return false
	}
	s.psc = psc
	return true
}

func (s *redisSubscription) clearCurrent(psc *redis.PubSubConn) {
	s.mu.Lock()
	if s.psc == psc {
		s.psc = nil
	}
	s.mu.Unlock()
	_ = psc.Close()
}

func (s *redisSubscription) isDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func firstEndpoint(endpoints []string) string {
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint != "" {
			return endpoint
		}
	}
	return ""
}
