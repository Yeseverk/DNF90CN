package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"longheng.io/server/pkg/contracts"
)

const requestReplyPrefix = "_reply."

var requestSeq uint64

type envelopePublisher func(context.Context, Envelope) error
type envelopeSubscriber func(string, Handler) (Subscription, error)

type requestResponse struct {
	data []byte
	err  error
}

func requestWithPubSub(ctx context.Context, topic string, payload any, timeout time.Duration, subscribe envelopeSubscriber, publish envelopePublisher) ([]byte, error) {
	if subscribe == nil || publish == nil {
		return nil, ErrUnsupported
	}
	ctx, cancel := requestContext(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	requestID := newRequestID(topic)
	replyTopic := requestReplyTopic(topic, requestID)
	responses := make(chan requestResponse, 1)
	// request-reply 必须先订阅临时 reply topic 再发布请求，否则快速响应可能在订阅建立前丢失。
	// responses 只保留第一条响应；超时后的迟到响应会被丢弃，调用方不能把它当可靠 RPC。
	sub, err := subscribe(replyTopic, func(_ context.Context, env Envelope) error {
		raw, err := marshalBusPayload(env.Payload)
		result := requestResponse{data: raw, err: err}
		select {
		case responses <- result:
		default:
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, fmt.Errorf("%w: request reply subscription missing", ErrUnsupported)
	}
	defer func() { _ = sub.Close() }()

	if err := publish(ctx, Envelope{
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

func requestContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func newRequestID(topic string) string {
	seq := atomic.AddUint64(&requestSeq, 1)
	return fmt.Sprintf("%d-%d", time.Now().UTC().UnixNano(), seq)
}

func requestReplyTopic(topic, requestID string) string {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		topic = "unknown"
	}
	topic = strings.NewReplacer(" ", "_", "\t", "_", "\n", "_", "\r", "_").Replace(topic)
	return requestReplyPrefix + topic + "." + requestID
}

func isRequestReplyTopic(topic string) bool {
	return strings.HasPrefix(strings.TrimSpace(topic), requestReplyPrefix)
}

func normalizeQueueName(queue string) string {
	return strings.TrimSpace(queue)
}

func marshalBusPayload(payload any) ([]byte, error) {
	switch value := payload.(type) {
	case nil:
		return nil, nil
	case []byte:
		return append([]byte(nil), value...), nil
	case json.RawMessage:
		return append([]byte(nil), value...), nil
	default:
		return contracts.MarshalTopicPayload(value)
	}
}
