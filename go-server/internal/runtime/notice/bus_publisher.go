package notice

import (
	"context"
	"errors"
	"strings"

	buskit "longheng.io/server/internal/platform/bus"
	"longheng.io/server/pkg/contracts"
	"longheng.io/server/pkg/protocol"
)

type BusPublisher struct {
	Bus   buskit.Bus
	Topic string
}

func NewBusPublisher(bus buskit.Bus) *BusPublisher {
	return &BusPublisher{Bus: bus, Topic: contracts.TopicNoticePublished}
}

func (p *BusPublisher) PublishNotice(ctx context.Context, result PublishResult) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if p == nil || p.Bus == nil {
		return ErrLivePublisherMissing
	}
	topic := strings.TrimSpace(p.Topic)
	if topic == "" {
		topic = contracts.TopicNoticePublished
	}
	return p.Bus.Publish(ctx, topic, noticePublishedEvent(result))
}

func noticePublishedEvent(result PublishResult) contracts.NoticePublished {
	recipients := make([]string, 0, len(result.Deliveries))
	deliveryIDs := make([]string, 0, len(result.Deliveries))
	for _, delivery := range result.Deliveries {
		recipients = append(recipients, delivery.AccountID)
		deliveryIDs = append(deliveryIDs, delivery.ID)
	}
	return contracts.NoticePublished{
		NoticeID:      result.Notice.ID,
		Kind:          result.Notice.Kind,
		Scope:         result.Notice.Scope,
		ShardID:       result.Notice.ShardID,
		Title:         result.Notice.Title,
		Body:          result.Notice.Body,
		AttachmentRef: result.Notice.AttachmentRef,
		Recipients:    recipients,
		DeliveryIDs:   deliveryIDs,
		Meta:          cloneStringMap(result.Meta),
		PublishedAt:   result.PublishedAt,
	}
}

type GatewayPushEncoder func(PublishResult, Delivery) ([]byte, error)

type PushPublisherOptions struct {
	Bus                    buskit.Bus
	Topic                  string
	PacketID               int32
	MsgID                  uint32
	WireFormat             string
	Encode                 GatewayPushEncoder
	BroadcastAnnouncements bool
}

type GatewayPushPublisher struct {
	options PushPublisherOptions
}

func NewGatewayPushPublisher(bus buskit.Bus) *GatewayPushPublisher {
	return NewGatewayPushPublisherWithOptions(PushPublisherOptions{Bus: bus})
}

func NewGatewayPushPublisherWithOptions(options PushPublisherOptions) *GatewayPushPublisher {
	if options.Topic == "" {
		options.Topic = contracts.TopicGatewayPush
	}
	if options.PacketID == 0 {
		options.PacketID = protocol.PacketIDContent
	}
	if options.MsgID == 0 {
		options.MsgID = protocol.MessageNoticePush
	}
	if options.Encode == nil {
		options.Encode = EncodeGatewayNoticePush
	}
	return &GatewayPushPublisher{options: options}
}

func (p *GatewayPushPublisher) PublishNotice(ctx context.Context, result PublishResult) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if p == nil || p.options.Bus == nil {
		return ErrLivePublisherMissing
	}
	if result.Notice.Kind == KindAnnouncement && len(result.Deliveries) == 0 && p.options.BroadcastAnnouncements {
		body, err := p.options.Encode(result, Delivery{})
		if err != nil {
			return err
		}
		return p.publish(ctx, contracts.GatewayPush{
			Broadcast:  true,
			PacketID:   p.options.PacketID,
			MsgID:      p.options.MsgID,
			WireFormat: p.options.WireFormat,
			Body:       body,
			Metadata:   gwNoticeMeta(result, Delivery{}),
			Note:       "notice-announcement-push",
			CreatedAt:  result.PublishedAt,
		})
	}
	var errs []error
	for _, delivery := range result.Deliveries {
		body, err := p.options.Encode(result, delivery)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		err = p.publish(ctx, contracts.GatewayPush{
			AccountID:  delivery.AccountID,
			PacketID:   p.options.PacketID,
			MsgID:      p.options.MsgID,
			WireFormat: p.options.WireFormat,
			Body:       body,
			Metadata:   gwNoticeMeta(result, delivery),
			Note:       "notice-direct-push",
			CreatedAt:  result.PublishedAt,
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *GatewayPushPublisher) publish(ctx context.Context, push contracts.GatewayPush) error {
	topic := strings.TrimSpace(p.options.Topic)
	if topic == "" {
		topic = contracts.TopicGatewayPush
	}
	return p.options.Bus.Publish(ctx, topic, push)
}

func EncodeGatewayNoticePush(result PublishResult, delivery Delivery) ([]byte, error) {
	return protocol.EncodeNoticePush(protocol.NoticePush{
		NoticeID:      result.Notice.ID,
		DeliveryID:    delivery.ID,
		Title:         result.Notice.Title,
		Body:          result.Notice.Body,
		AttachmentRef: result.Notice.AttachmentRef,
		CreatedAt:     result.PublishedAt.Unix(),
	}), nil
}

func gwNoticeMeta(result PublishResult, delivery Delivery) map[string]string {
	meta := map[string]string{
		"notice_id": result.Notice.ID,
		"kind":      result.Notice.Kind,
	}
	if result.Notice.ShardID != "" {
		meta["shard_id"] = result.Notice.ShardID
	}
	if delivery.ID != "" {
		meta["delivery_id"] = delivery.ID
	}
	if delivery.AccountID != "" {
		meta["account_id"] = delivery.AccountID
	}
	return meta
}

type FanoutPublisher struct {
	Publishers []LivePublisher
}

func NewFanoutPublisher(publishers ...LivePublisher) FanoutPublisher {
	return FanoutPublisher{Publishers: publishers}
}

func (p FanoutPublisher) PublishNotice(ctx context.Context, result PublishResult) error {
	var errs []error
	for _, publisher := range p.Publishers {
		if publisher == nil {
			continue
		}
		if err := publisher.PublishNotice(ctx, result); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
