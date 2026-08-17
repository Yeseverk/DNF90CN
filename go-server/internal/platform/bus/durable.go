package bus

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrStreamRequired   = errors.New("bus stream is required")
	ErrConsumerRequired = errors.New("bus consumer is required")
)

type StreamMessage struct {
	Stream    string            `json:"stream"`
	Key       string            `json:"key,omitempty"`
	Payload   []byte            `json:"payload,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp time.Time         `json:"timestamp,omitempty"`
}

type PublishOptions struct {
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type ConsumeOptions struct {
	Consumer string `json:"consumer"`
	From     string `json:"from,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type DurablePublisher interface {
	PublishDurable(context.Context, StreamMessage, PublishOptions) error
}

type DurableConsumer interface {
	ConsumeDurable(context.Context, ConsumeOptions, func(context.Context, StreamMessage) error) error
}

type DurableBus interface {
	DurablePublisher
	DurableConsumer
}

func ValidateStreamMessage(message StreamMessage) error {
	if strings.TrimSpace(message.Stream) == "" {
		return ErrStreamRequired
	}
	return nil
}

func ValidateConsumeOptions(options ConsumeOptions) error {
	if strings.TrimSpace(options.Consumer) == "" {
		return ErrConsumerRequired
	}
	return nil
}
