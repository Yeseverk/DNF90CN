package mail

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const (
	maxMailboxRecipientBytes = 30
	maxMailboxMessageBytes   = 512
	maxMailboxAttachments    = 10
	maxMailboxBatch          = 100
)

// SendAttachment is the exact 12-byte attachment row written by current
// NoPack.exe sub_26FC590. ItemID and Count are validation values only; the
// owner must resolve the durable item from ListType and SlotIndex.
type SendAttachment struct {
	ListType  byte
	SlotIndex uint16
	ItemID    uint32
	Count     uint32
}

// SendRequest is the exact current op94/op315 request body. Opcode 315 is the
// multi-attachment variant; it is not a multi-recipient command.
type SendRequest struct {
	RecipientName string
	RecipientRaw  []byte
	Gold          uint32
	Attachments   []SendAttachment
	Body          string
	BodyRaw       []byte
	Special       uint32
	Global        bool
	RawLen        int
}

func (r SendRequest) String() string {
	return fmt.Sprintf(
		"recipient=%q body_len=%d gold=%d attachments=%d special=%d global=%t raw=%d",
		r.RecipientName,
		len(r.BodyRaw),
		r.Gold,
		len(r.Attachments),
		r.Special,
		r.Global,
		r.RawLen,
	)
}

// ExtractRequest is the exact current op95 request: u32 count followed by
// count u32 mail IDs.
type ExtractRequest struct {
	MailIDs []uint32
	RawLen  int
}

func (r ExtractRequest) String() string {
	return fmt.Sprintf("ids=%v raw=%d", r.MailIDs, r.RawLen)
}

// ChangeStateRequest is the exact current op134 request: u32 count, count u32
// mail IDs, then one u16 state.
type ChangeStateRequest struct {
	MailIDs []uint32
	Status  uint16
	RawLen  int
}

func (r ChangeStateRequest) String() string {
	return fmt.Sprintf("ids=%v status=%d raw=%d", r.MailIDs, r.Status, r.RawLen)
}

// QueryCharacterRequest is the exact current op324 request.
type QueryCharacterRequest struct {
	Name    string
	NameRaw []byte
	Mode    byte
	RawLen  int
}

func (r QueryCharacterRequest) String() string {
	return fmt.Sprintf("name=%q mode=%d raw=%d", r.Name, r.Mode, r.RawLen)
}

func decodeSendRequest(opcode dnfenum.CmdPacket, body []byte) (SendRequest, error) {
	cursor := mailboxCursor{body: body}
	recipientRaw, err := cursor.dstring(maxMailboxRecipientBytes)
	if err != nil {
		return SendRequest{}, fmt.Errorf("recipient: %w", err)
	}
	recipientName, err := decodeMailboxText(recipientRaw)
	if err != nil || strings.TrimSpace(recipientName) == "" {
		return SendRequest{}, fmt.Errorf("recipient: invalid client text")
	}
	gold, err := cursor.u32()
	if err != nil {
		return SendRequest{}, fmt.Errorf("gold: %w", err)
	}

	attachmentCount := 1
	switch opcode {
	case dnfenum.CmdPacketMailboxSend:
	case dnfenum.CmdPacketMultiMailboxSend:
		count, readErr := cursor.u8()
		if readErr != nil {
			return SendRequest{}, fmt.Errorf("attachment count: %w", readErr)
		}
		attachmentCount = int(count)
		if attachmentCount > maxMailboxAttachments {
			return SendRequest{}, fmt.Errorf("attachment count %d exceeds %d", attachmentCount, maxMailboxAttachments)
		}
	default:
		return SendRequest{}, fmt.Errorf("unsupported mailbox send opcode %d", opcode)
	}

	attachments := make([]SendAttachment, 0, attachmentCount)
	for idx := 0; idx < attachmentCount; idx++ {
		listType, readErr := cursor.u8()
		if readErr != nil {
			return SendRequest{}, fmt.Errorf("attachment %d list type: %w", idx, readErr)
		}
		slot, readErr := cursor.u16()
		if readErr != nil {
			return SendRequest{}, fmt.Errorf("attachment %d slot: %w", idx, readErr)
		}
		itemID, readErr := cursor.u32()
		if readErr != nil {
			return SendRequest{}, fmt.Errorf("attachment %d item id: %w", idx, readErr)
		}
		count, readErr := cursor.u32()
		if readErr != nil {
			return SendRequest{}, fmt.Errorf("attachment %d count: %w", idx, readErr)
		}
		// op94 writes one all-zero row when the composer has no attachment.
		if listType == 0 && slot == 0 && itemID == 0 && count == 0 {
			continue
		}
		if itemID == 0 || count == 0 {
			return SendRequest{}, fmt.Errorf("attachment %d has incomplete identity", idx)
		}
		attachments = append(attachments, SendAttachment{
			ListType:  listType,
			SlotIndex: slot,
			ItemID:    itemID,
			Count:     count,
		})
	}

	messageRaw, err := cursor.dstring(maxMailboxMessageBytes)
	if err != nil {
		return SendRequest{}, fmt.Errorf("message: %w", err)
	}
	message, err := decodeMailboxTextAllowEmpty(messageRaw)
	if err != nil {
		return SendRequest{}, fmt.Errorf("message: invalid client text")
	}
	special, err := cursor.u32()
	if err != nil {
		return SendRequest{}, fmt.Errorf("special: %w", err)
	}
	global, err := cursor.u8()
	if err != nil {
		return SendRequest{}, fmt.Errorf("global: %w", err)
	}
	if global > 1 {
		return SendRequest{}, fmt.Errorf("global flag is %d", global)
	}
	if !cursor.done() {
		return SendRequest{}, fmt.Errorf("trailing mailbox send bytes: %d", cursor.remaining())
	}
	return SendRequest{
		RecipientName: recipientName,
		RecipientRaw:  append([]byte(nil), recipientRaw...),
		Gold:          gold,
		Attachments:   attachments,
		Body:          message,
		BodyRaw:       append([]byte(nil), messageRaw...),
		Special:       special,
		Global:        global == 1,
		RawLen:        len(body),
	}, nil
}

func decodeExtractRequest(body []byte) (ExtractRequest, error) {
	ids, cursor, err := decodeMailIDList(body)
	if err != nil {
		return ExtractRequest{}, err
	}
	if !cursor.done() {
		return ExtractRequest{}, fmt.Errorf("trailing mailbox extract bytes: %d", cursor.remaining())
	}
	return ExtractRequest{MailIDs: ids, RawLen: len(body)}, nil
}

func decodeChangeStateRequest(body []byte) (ChangeStateRequest, error) {
	ids, cursor, err := decodeMailIDList(body)
	if err != nil {
		return ChangeStateRequest{}, err
	}
	status, err := cursor.u16()
	if err != nil {
		return ChangeStateRequest{}, fmt.Errorf("mailbox state: %w", err)
	}
	if !cursor.done() {
		return ChangeStateRequest{}, fmt.Errorf("trailing mailbox state bytes: %d", cursor.remaining())
	}
	return ChangeStateRequest{MailIDs: ids, Status: status, RawLen: len(body)}, nil
}

func decodeQueryCharacterRequest(body []byte) (QueryCharacterRequest, error) {
	cursor := mailboxCursor{body: body}
	nameRaw, err := cursor.dstring(maxMailboxRecipientBytes)
	if err != nil {
		return QueryCharacterRequest{}, fmt.Errorf("query name: %w", err)
	}
	name, err := decodeMailboxText(nameRaw)
	if err != nil || strings.TrimSpace(name) == "" {
		return QueryCharacterRequest{}, fmt.Errorf("query name: invalid client text")
	}
	mode, err := cursor.u8()
	if err != nil {
		return QueryCharacterRequest{}, fmt.Errorf("query mode: %w", err)
	}
	if !cursor.done() {
		return QueryCharacterRequest{}, fmt.Errorf("trailing mailbox query bytes: %d", cursor.remaining())
	}
	return QueryCharacterRequest{
		Name:    name,
		NameRaw: append([]byte(nil), nameRaw...),
		Mode:    mode,
		RawLen:  len(body),
	}, nil
}

func decodeMailboxOpenRequest(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("mailbox open body must be empty, got %d bytes", len(body))
	}
	return nil
}

type recipientCharacterListRequest struct {
	ServerID byte
}

func decodeRecipientCharacterListRequest(body []byte) (recipientCharacterListRequest, error) {
	if len(body) != 1 {
		return recipientCharacterListRequest{}, fmt.Errorf("mailbox account-role request must contain one server id byte, got %d bytes", len(body))
	}
	return recipientCharacterListRequest{ServerID: body[0]}, nil
}

func decodeMailIDList(body []byte) ([]uint32, mailboxCursor, error) {
	cursor := mailboxCursor{body: body}
	count, err := cursor.u32()
	if err != nil {
		return nil, cursor, fmt.Errorf("mail id count: %w", err)
	}
	if count == 0 || count > maxMailboxBatch {
		return nil, cursor, fmt.Errorf("mail id count %d is outside 1..%d", count, maxMailboxBatch)
	}
	if uint64(cursor.remaining()) < uint64(count)*4 {
		return nil, cursor, fmt.Errorf("mail id list is truncated")
	}
	ids := make([]uint32, 0, count)
	seen := make(map[uint32]struct{}, count)
	for idx := uint32(0); idx < count; idx++ {
		id, readErr := cursor.u32()
		if readErr != nil {
			return nil, cursor, fmt.Errorf("mail id %d: %w", idx, readErr)
		}
		if id == 0 {
			return nil, cursor, fmt.Errorf("mail id %d is zero", idx)
		}
		if _, exists := seen[id]; exists {
			return nil, cursor, fmt.Errorf("mail id %d is duplicated", id)
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, cursor, nil
}

func decodeMailboxText(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("empty text")
	}
	return decodeMailboxTextAllowEmpty(raw)
}

func decodeMailboxTextAllowEmpty(raw []byte) (string, error) {
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(decoded) {
		return "", fmt.Errorf("decoded text is not utf8")
	}
	return string(decoded), nil
}

type mailboxCursor struct {
	body []byte
	pos  int
}

func (c *mailboxCursor) u8() (byte, error) {
	if c.remaining() < 1 {
		return 0, fmt.Errorf("need 1 byte, have %d", c.remaining())
	}
	value := c.body[c.pos]
	c.pos++
	return value, nil
}

func (c *mailboxCursor) u16() (uint16, error) {
	if c.remaining() < 2 {
		return 0, fmt.Errorf("need 2 bytes, have %d", c.remaining())
	}
	value := binary.LittleEndian.Uint16(c.body[c.pos : c.pos+2])
	c.pos += 2
	return value, nil
}

func (c *mailboxCursor) u32() (uint32, error) {
	if c.remaining() < 4 {
		return 0, fmt.Errorf("need 4 bytes, have %d", c.remaining())
	}
	value := binary.LittleEndian.Uint32(c.body[c.pos : c.pos+4])
	c.pos += 4
	return value, nil
}

func (c *mailboxCursor) dstring(max int) ([]byte, error) {
	length, err := c.u32()
	if err != nil {
		return nil, err
	}
	if length > uint32(max) {
		return nil, fmt.Errorf("DSTR length %d exceeds %d", length, max)
	}
	if uint64(c.remaining()) < uint64(length) {
		return nil, fmt.Errorf("DSTR length %d exceeds remaining %d", length, c.remaining())
	}
	start := c.pos
	c.pos += int(length)
	return c.body[start:c.pos], nil
}

func (c mailboxCursor) remaining() int {
	return len(c.body) - c.pos
}

func (c mailboxCursor) done() bool {
	return c.pos == len(c.body)
}
