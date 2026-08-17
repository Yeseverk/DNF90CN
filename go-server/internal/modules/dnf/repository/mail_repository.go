package repository

import (
	"context"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

// MailboxRepository persists the selected character's mailbox state.
type MailboxRepository interface {
	db.Store[MailboxRecord]
}

// MailboxRecord is the durable mailbox aggregate for one character.
type MailboxRecord struct {
	CharacterID string                `json:"character_id"`
	Mails       map[string]MailRecord `json:"mails,omitempty"`
	UpdatedAt   time.Time             `json:"updated_at,omitempty"`
}

// MailRecord is one server-owned mail. Attachments are claimed exactly once.
type MailRecord struct {
	MailID               string            `json:"mail_id"`
	SenderCharacterID    string            `json:"sender_character_id,omitempty"`
	SenderName           string            `json:"sender_name,omitempty"`
	RecipientCharacterID string            `json:"recipient_character_id,omitempty"`
	RecipientName        string            `json:"recipient_name,omitempty"`
	Title                string            `json:"title,omitempty"`
	Body                 string            `json:"body,omitempty"`
	Gold                 int64             `json:"gold,omitempty"`
	Attachments          []MailAttachment  `json:"attachments,omitempty"`
	Read                 bool              `json:"read,omitempty"`
	Claimed              bool              `json:"claimed,omitempty"`
	Deleted              bool              `json:"deleted,omitempty"`
	CreatedAt            time.Time         `json:"created_at,omitempty"`
	ExpireAt             time.Time         `json:"expire_at,omitempty"`
	Metadata             map[string]string `json:"metadata,omitempty"`
}

// MailAttachment is an inventory item attached to one mail.
type MailAttachment struct {
	ItemID   int64             `json:"item_id"`
	Count    int64             `json:"count"`
	Bind     bool              `json:"bind,omitempty"`
	ExpireAt time.Time         `json:"expire_at,omitempty"`
	RawEntry []byte            `json:"raw_entry,omitempty"`
	Extra    map[string]string `json:"extra,omitempty"`
}

// MailboxField represents mailbox fields that support partial persistence.
type MailboxField string

const (
	MailboxFieldMails MailboxField = "mails"
)

// SaveMailboxFields persists only selected mailbox fields when supported.
func SaveMailboxFields(ctx context.Context, repo MailboxRepository, record MailboxRecord, fields ...MailboxField) error {
	return db.SaveFields(ctx, repo, record, MailboxFields.Normalize, fields...)
}

// CloneMailbox deep-copies a mailbox aggregate.
func CloneMailbox(record MailboxRecord) MailboxRecord {
	record.Mails = cloneMailMap(record.Mails)
	return record
}

func MailboxKey(record MailboxRecord) string {
	return strings.TrimSpace(record.CharacterID)
}

func cloneMailMap(in map[string]MailRecord) map[string]MailRecord {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]MailRecord, len(in))
	for key, value := range in {
		value.Attachments = cloneMailAttachments(value.Attachments)
		value.Metadata = cloneStringMap(value.Metadata)
		out[key] = value
	}
	return out
}

func cloneMailAttachments(in []MailAttachment) []MailAttachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]MailAttachment, len(in))
	for idx, value := range in {
		value.RawEntry = append([]byte(nil), value.RawEntry...)
		value.Extra = cloneStringMap(value.Extra)
		out[idx] = value
	}
	return out
}
