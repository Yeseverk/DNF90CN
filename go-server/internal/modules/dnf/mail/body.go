package mail

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	mailboxListNotificationMessageID   uint16 = 0x0061
	mailboxRemoveNotificationMessageID uint16 = 0x0062
	MailboxAlarmNotificationMessageID  uint16 = 0x0063
	// Current NoPack.exe registers class0/op718 beside mailbox op97/op98/op99
	// notifications as the response that fills IDC_TREE_MYCHARACTER after
	// class1/op789 requests a server node.
	mailboxRecipientListMessageID    uint16 = 718
	mailboxRecipientListNameMaxBytes        = 29
	mailboxItemRawSize                      = 0x77
)

func class1Response(opcode uint16, body []byte) alignedcmd.UpperResponse {
	return alignedcmd.UpperResponse{
		MsgID:          opcode,
		Body:           append([]byte(nil), body...),
		Classification: dnfproto.DefaultChannelClassification,
		AllowCodec:     true,
	}
}

func class0Response(messageID uint16, body []byte) alignedcmd.UpperResponse {
	return alignedcmd.UpperResponse{
		MsgID:          messageID,
		Body:           append([]byte(nil), body...),
		Classification: 0,
		AllowCodec:     true,
	}
}

func buildMailboxOpenAck(total int) []byte {
	if total < 0 {
		total = 0
	}
	if total > 0xffff {
		total = 0xffff
	}
	body := []byte{0x01, 0x00, 0x00}
	binary.LittleEndian.PutUint16(body[1:], uint16(total))
	return body
}

func buildSuccessAck() []byte {
	return []byte{0x01}
}

func buildExtractAck(mailIDs []uint32) []byte {
	body := make([]byte, 1+4+len(mailIDs)*12)
	body[0] = 0x01
	binary.LittleEndian.PutUint32(body[1:5], uint32(len(mailIDs)))
	offset := 5
	for _, mailID := range mailIDs {
		binary.LittleEndian.PutUint32(body[offset:offset+4], mailID)
		// Current handler sub_1D225F0 consumes three u32 values per result.
		// Zero second/third fields select its normal successful extraction path.
		offset += 12
	}
	return body
}

func buildChangeStateAck(mailIDs []uint32, status uint16) []byte {
	body := make([]byte, 1+4+len(mailIDs)*6)
	body[0] = 0x01
	binary.LittleEndian.PutUint32(body[1:5], uint32(len(mailIDs)))
	offset := 5
	for _, mailID := range mailIDs {
		binary.LittleEndian.PutUint32(body[offset:offset+4], mailID)
		binary.LittleEndian.PutUint16(body[offset+4:offset+6], status)
		offset += 6
	}
	return body
}

func buildQueryCharacterAck(nameRaw []byte, grow byte, level uint16, job byte) []byte {
	body := make([]byte, 0, 1+4+len(nameRaw)+8)
	body = append(body, 0x01)
	var scratch [4]byte
	binary.LittleEndian.PutUint32(scratch[:], uint32(len(nameRaw)))
	body = append(body, scratch[:]...)
	body = append(body, nameRaw...)
	body = append(body, grow)
	var levelBytes [2]byte
	binary.LittleEndian.PutUint16(levelBytes[:], level)
	body = append(body, levelBytes[:]...)
	// sub_1D0BAD0 reads job plus four trailing state bytes after level. The
	// 1 in the third state field matches the handler's initialized normal state.
	body = append(body, job, 0x00, 0x01, 0x00, 0x00)
	return body
}

func buildRecipientCharacterListBody(serverID byte, characters []dnfrepo.CharacterRecord, selectedCharacterID uint16) ([]byte, error) {
	writer := mailboxBodyWriter{}
	writer.u8(serverID)
	writer.u8(0)
	count := 0
	for _, character := range characters {
		if count == math.MaxUint8 {
			break
		}
		characterID, err := strconv.ParseUint(strings.TrimSpace(character.CharacterID), 10, 32)
		if err != nil || characterID == 0 {
			return nil, fmt.Errorf("%w: invalid mailbox account-role character id %q", ErrMailboxInvalidRequest, character.CharacterID)
		}
		if uint32(characterID) == uint32(selectedCharacterID) {
			continue
		}
		name := strings.TrimSpace(character.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: mailbox account-role character %d has no name", ErrMailboxInvalidRequest, characterID)
		}
		nameRaw, err := encodeMailboxText(name, mailboxRecipientListNameMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("%w: mailbox account-role name %q: %v", ErrMailboxInvalidRequest, name, err)
		}
		writer.u32(uint32(characterID))
		writer.u32(uint32(len(nameRaw)))
		writer.raw(nameRaw)
		writer.u8(0)
		count++
	}
	writer.data[1] = byte(count)
	return writer.bytes(), nil
}

func buildErrorAck(code byte) []byte {
	if code == 0 {
		code = 1
	}
	return []byte{0x00, code}
}

// buildMailboxListNotification follows the current client's
// sub_1D913C0 reader exactly: attachment/gold summaries, the deferred-page
// count, then the detailed text/status rows. The common 0x77 item row is
// deliberately retained as a bounded opaque client item row; its front fields
// are projected separately because the client reads both representations.
func buildMailboxListNotification(mails []dnfrepo.MailRecord, notLoaded int, now time.Time) ([]byte, error) {
	if len(mails) > math.MaxUint16 {
		return nil, fmt.Errorf("%w: mailbox page has %d mails", ErrMailboxInvalidRequest, len(mails))
	}

	summaries := make([]mailboxSummary, 0, len(mails))
	for _, durableMail := range mails {
		mail := mailboxPresentationMail(durableMail)
		mailID, err := mailboxWireID(mail.MailID)
		if err != nil {
			return nil, err
		}
		wrote := false
		for _, attachment := range mail.Attachments {
			if attachment.ItemID <= 0 || attachment.Count <= 0 {
				return nil, fmt.Errorf("%w: invalid attachment mail=%d item=%d count=%d", ErrMailboxInvalidRequest, mailID, attachment.ItemID, attachment.Count)
			}
			if len(summaries) >= math.MaxUint8 {
				return nil, fmt.Errorf("%w: mailbox page has more than %d attachment summaries", ErrMailboxInvalidRequest, math.MaxUint8)
			}
			summaries = append(summaries, mailboxSummary{
				mail:        mail,
				mailID:      mailID,
				attachment:  &attachment,
				includeGold: !wrote,
			})
			wrote = true
		}
		if !wrote && mail.Gold > 0 {
			if len(summaries) >= math.MaxUint8 {
				return nil, fmt.Errorf("%w: mailbox page has more than %d summaries", ErrMailboxInvalidRequest, math.MaxUint8)
			}
			summaries = append(summaries, mailboxSummary{mail: mail, mailID: mailID, includeGold: true})
			wrote = true
		}
		// The client keeps the current message identity from the summary pass.
		// A text-only mail therefore needs one intentionally empty seed record.
		if !wrote {
			if len(summaries) >= math.MaxUint8 {
				return nil, fmt.Errorf("%w: mailbox page has more than %d summaries", ErrMailboxInvalidRequest, math.MaxUint8)
			}
			summaries = append(summaries, mailboxSummary{mail: mail, mailID: mailID, seedOnly: true})
		}
	}

	writer := mailboxBodyWriter{}
	writer.u8(byte(len(summaries)))
	// Zero makes sub_1D913C0 clear both current mail containers before this
	// complete snapshot; subsequent page batches would use one here.
	writer.u8(0)
	for _, summary := range summaries {
		if err := writer.writeSummary(summary, now); err != nil {
			return nil, err
		}
	}
	writer.u16(clampMailboxU16(notLoaded))
	writer.u16(uint16(len(mails)))
	for _, durableMail := range mails {
		mail := mailboxPresentationMail(durableMail)
		if err := writer.writeDetail(mail); err != nil {
			return nil, err
		}
	}
	return writer.bytes(), nil
}

// mailboxPresentationMail projects old full-inventory system mail with the
// current Chinese copy. Claimed mail is always projected as an empty, read
// receipt; this also protects the UI while an older durable row is being
// repaired by Owner.Open.
func mailboxPresentationMail(mail dnfrepo.MailRecord) dnfrepo.MailRecord {
	switch strings.TrimSpace(mail.Metadata["source"]) {
	case "magic_box_reward_inventory_full":
		mail.SenderName = "系统"
		mail.Title = "背包已满：礼盒奖励"
		mail.Body = "背包空间不足，礼盒奖励已通过邮件发送。请清理对应道具分页后领取。"
	case "dungeon_card_reward_inventory_full":
		mail.SenderName = "系统"
		mail.Title = "背包已满：通关奖励"
		mail.Body = "背包空间不足，通关奖励已通过邮件发送。请清理对应道具分页后领取。"
	}
	if mail.Claimed {
		mail.Gold = 0
		mail.Attachments = nil
	}
	return mail
}

func buildMailboxRemoveNotification(mailIDs []uint32) []byte {
	writer := mailboxBodyWriter{}
	writer.u32(uint32(len(mailIDs)))
	for _, mailID := range mailIDs {
		writer.u32(mailID)
	}
	return writer.bytes()
}

// buildMailboxAlarmNotification is the exact one-WORD class0/0x63 body. It
// is kept here for the session directory path, which may notify an online
// recipient independently from the sender's command response.
func buildMailboxAlarmNotification(count int) []byte {
	writer := mailboxBodyWriter{}
	writer.u16(clampMailboxU16(count))
	return writer.bytes()
}

// BuildAlarmNotification produces the current class0/0x63 one-WORD payload
// used when a recipient already has an active game session.
func BuildAlarmNotification(count int) []byte {
	return buildMailboxAlarmNotification(count)
}

type mailboxSummary struct {
	mail        dnfrepo.MailRecord
	mailID      uint32
	attachment  *dnfrepo.MailAttachment
	includeGold bool
	seedOnly    bool
}

type mailboxBodyWriter struct{ data []byte }

func (w *mailboxBodyWriter) u8(value byte) { w.data = append(w.data, value) }

func (w *mailboxBodyWriter) u16(value uint16) {
	var encoded [2]byte
	binary.LittleEndian.PutUint16(encoded[:], value)
	w.data = append(w.data, encoded[:]...)
}

func (w *mailboxBodyWriter) u32(value uint32) {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	w.data = append(w.data, encoded[:]...)
}

func (w *mailboxBodyWriter) raw(value []byte) { w.data = append(w.data, value...) }

func (w *mailboxBodyWriter) dstring(value string, maxBytes int) error {
	raw, err := encodeMailboxText(value, maxBytes)
	if err != nil {
		return err
	}
	w.u32(uint32(len(raw)))
	w.raw(raw)
	return nil
}

func (w *mailboxBodyWriter) writeSummary(summary mailboxSummary, now time.Time) error {
	senderID, err := mailboxSenderWireID(summary.mail.SenderCharacterID)
	if err != nil {
		return err
	}
	w.u32(summary.mailID) // v40/claim object: current op95 claims mail IDs.
	w.u32(senderID)
	if err := w.dstring(summary.mail.SenderName, maxMailboxRecipientBytes); err != nil {
		return fmt.Errorf("sender name: %w", err)
	}
	if summary.includeGold {
		if summary.mail.Gold < 0 || summary.mail.Gold > math.MaxUint32 {
			return fmt.Errorf("%w: mail=%d gold=%d", ErrMailboxInvalidRequest, summary.mailID, summary.mail.Gold)
		}
		w.u32(uint32(summary.mail.Gold))
	} else {
		w.u32(0)
	}

	if summary.attachment == nil {
		w.u32(0)
		w.u8(0)
		w.u32(0)
		w.u16(0)
		w.u8(0)
		w.u32(0)
		w.u8(0)
		w.u8(0)
		w.u16(0)
		w.raw(make([]byte, mailboxItemRawSize))
	} else {
		attachment := *summary.attachment
		if attachment.ItemID > math.MaxUint32 || attachment.Count > math.MaxUint32 {
			return fmt.Errorf("%w: attachment item/count outside wire range", ErrMailboxInvalidRequest)
		}
		raw := mailboxAttachmentRaw(attachment)
		w.u32(uint32(attachment.ItemID))
		w.u8(1)
		w.u32(binary.LittleEndian.Uint32(raw[6:10]))
		w.u16(binary.LittleEndian.Uint16(raw[0x0b:0x0d]))
		w.u8(raw[0x0d])
		w.u32(binary.LittleEndian.Uint32(raw[0x0e:0x12]))
		w.u8(raw[0x12])
		w.u8(raw[0x13])
		w.u16(binary.LittleEndian.Uint16(raw[0x14:0x16]))
		if mailboxAttachmentRequiresClass26(attachment) {
			// The current type-26 branch consumes this DWORD and its detail flag
			// before the fixed item row. We have no durable creature-detail UOW,
			// so the explicit zero flag requests only the proven generic item
			// projection instead of fabricating the three detail dwords.
			w.u32(0)
			w.u8(0)
		}
		w.raw(raw)
	}
	w.u32(mailboxRemainingSeconds(summary.mail, now))
	if summary.seedOnly {
		w.u32(0)
	} else {
		w.u32(summary.mailID)
	}
	w.u8(mailboxMailType(summary.mail))
	return nil
}

func (w *mailboxBodyWriter) writeDetail(mail dnfrepo.MailRecord) error {
	mailID, err := mailboxWireID(mail.MailID)
	if err != nil {
		return err
	}
	senderID, err := mailboxSenderWireID(mail.SenderCharacterID)
	if err != nil {
		return err
	}
	w.u32(mailID)
	w.u32(senderID)
	if err := w.dstring(mail.SenderName, maxMailboxRecipientBytes); err != nil {
		return fmt.Errorf("sender name: %w", err)
	}
	if err := w.dstring(mail.Body, maxMailboxMessageBytes); err != nil {
		return fmt.Errorf("body: %w", err)
	}
	w.u32(mailboxUnix(mail.CreatedAt))
	w.u16(mailboxLetterStat(mail))
	w.u8(mailboxMailType(mail))
	return nil
}

func (w mailboxBodyWriter) bytes() []byte { return append([]byte(nil), w.data...) }

func mailboxAttachmentRaw(attachment dnfrepo.MailAttachment) []byte {
	raw := make([]byte, mailboxItemRawSize)
	if len(attachment.RawEntry) == mailboxItemRawSize {
		copy(raw, attachment.RawEntry)
	}
	binary.LittleEndian.PutUint16(raw[0:2], 0)
	binary.LittleEndian.PutUint32(raw[2:6], uint32(attachment.ItemID))
	binary.LittleEndian.PutUint32(raw[6:10], uint32(attachment.Count))
	if attachment.Bind && raw[0x0d] == 0 {
		raw[0x0d] = 1
	}
	if !attachment.ExpireAt.IsZero() && attachment.ExpireAt.Unix() > 0 {
		binary.LittleEndian.PutUint32(raw[0x38:0x3c], mailboxUnix(attachment.ExpireAt))
	}
	return raw
}

func mailboxAttachmentRequiresClass26(attachment dnfrepo.MailAttachment) bool {
	return strings.EqualFold(strings.TrimSpace(attachment.Extra["mailbox_equipment_type"]), "creature")
}

func mailboxLetterStat(mail dnfrepo.MailRecord) uint16 {
	if mailboxMailSaved(mail) {
		return 3
	}
	if mail.Read {
		return 2
	}
	return 1
}

func mailboxMailType(mail dnfrepo.MailRecord) byte {
	value, err := strconv.ParseUint(strings.TrimSpace(mail.Metadata["mail_type"]), 10, 8)
	if err != nil {
		return 0
	}
	return byte(value)
}

func mailboxRemainingSeconds(mail dnfrepo.MailRecord, now time.Time) uint32 {
	if mailboxMailSaved(mail) || now.IsZero() {
		return 0
	}
	expiresAt := mail.ExpireAt
	if expiresAt.IsZero() && !mail.CreatedAt.IsZero() {
		expiresAt = mail.CreatedAt.Add(mailboxNormalLifetime)
	}
	if expiresAt.IsZero() || !expiresAt.After(now) {
		return 0
	}
	remaining := int64(expiresAt.Sub(now) / time.Second)
	if remaining > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(remaining)
}

func mailboxWireID(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("%w: invalid wire mail id %q", ErrMailboxInvalidRequest, value)
	}
	return uint32(parsed), nil
}

func mailboxSenderWireID(value string) (uint32, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid sender id %q", ErrMailboxInvalidRequest, value)
	}
	return uint32(parsed), nil
}

func mailboxUnix(value time.Time) uint32 {
	if value.IsZero() || value.Unix() <= 0 {
		return 0
	}
	if value.Unix() > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value.Unix())
}

func clampMailboxU16(value int) uint16 {
	if value <= 0 {
		return 0
	}
	if value >= math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(value)
}

func encodeMailboxText(value string, maxBytes int) ([]byte, error) {
	encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(value))
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxBytes {
		return nil, fmt.Errorf("length %d exceeds %d", len(encoded), maxBytes)
	}
	return encoded, nil
}
