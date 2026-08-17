package notice

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"longheng.io/server/internal/platform/onlinepush"
	"longheng.io/server/pkg/protocol"
)

type OnlinePushSender interface {
	Send(context.Context, onlinepush.Request) (onlinepush.Receipt, error)
}

type OnlinePushPublisherOptions struct {
	Sender                 OnlinePushSender
	PacketID               int32
	MsgID                  uint32
	WireFormat             string
	Encode                 GatewayPushEncoder
	BroadcastAnnouncements bool
	OfflinePolicy          string
	IdempotencyPrefix      string
}

type OnlinePushPublisher struct {
	options OnlinePushPublisherOptions
}

func NewOnlinePushPublisher(sender OnlinePushSender) *OnlinePushPublisher {
	return NewOnlinePushPublisherWithOptions(OnlinePushPublisherOptions{Sender: sender})
}

func NewOnlinePushPublisherWithOptions(options OnlinePushPublisherOptions) *OnlinePushPublisher {
	if options.PacketID == 0 {
		options.PacketID = protocol.PacketIDContent
	}
	if options.MsgID == 0 {
		options.MsgID = protocol.MessageNoticePush
	}
	if options.Encode == nil {
		options.Encode = EncodeGatewayNoticePush
	}
	if strings.TrimSpace(options.OfflinePolicy) == "" {
		options.OfflinePolicy = onlinepush.OfflineStore
	}
	if strings.TrimSpace(options.IdempotencyPrefix) == "" {
		options.IdempotencyPrefix = "notice-live"
	}
	return &OnlinePushPublisher{options: options}
}

func (p *OnlinePushPublisher) PublishNotice(ctx context.Context, result PublishResult) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if p == nil || p.options.Sender == nil {
		return ErrLivePublisherMissing
	}
	if result.Notice.Kind == KindAnnouncement && len(result.Deliveries) == 0 && p.options.BroadcastAnnouncements {
		body, err := p.options.Encode(result, Delivery{})
		if err != nil {
			return err
		}
		_, err = p.options.Sender.Send(ctx, onlinepush.Request{
			IdempotencyKey: p.idempotencyKey(result, Delivery{}, "announcement"),
			Broadcast:      true,
			PacketID:       p.options.PacketID,
			MsgID:          p.options.MsgID,
			WireFormat:     p.options.WireFormat,
			Body:           body,
			Metadata:       gwNoticeMeta(result, Delivery{}),
			Note:           "notice-announcement-push",
			OfflinePolicy:  onlinepush.OfflineDrop,
			CreatedAt:      result.PublishedAt,
		})
		return err
	}
	var errs []error
	for _, delivery := range result.Deliveries {
		body, err := p.options.Encode(result, delivery)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		_, err = p.options.Sender.Send(ctx, onlinepush.Request{
			IdempotencyKey: p.idempotencyKey(result, delivery, "direct"),
			AccountID:      delivery.AccountID,
			PacketID:       p.options.PacketID,
			MsgID:          p.options.MsgID,
			WireFormat:     p.options.WireFormat,
			Body:           body,
			Metadata:       gwNoticeMeta(result, delivery),
			Note:           "notice-direct-push",
			OfflinePolicy:  p.options.OfflinePolicy,
			CreatedAt:      result.PublishedAt,
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *OnlinePushPublisher) idempotencyKey(result PublishResult, delivery Delivery, suffix string) string {
	parts := []string{
		encodePushKeyPart(p.options.IdempotencyPrefix),
		encodePushKeyPart(result.IdempotencyKey),
		encodePushKeyPart(result.Notice.ID),
		encodePushKeyPart(delivery.ID),
		encodePushKeyPart(suffix),
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, ":")
}

func encodePushKeyPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}
