package mail

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type handler struct{}

// NewHandler creates the mailbox protocol handler.
func NewHandler() alignedcmd.Handler {
	return handler{}
}

// Domain returns the mailbox business domain.
func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainMail
}

// Handle routes the mailbox command family using the current NoPack.exe
// readers/writers as the packet authority.
func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	opcode := dnfenum.CmdPacket(req.Opcode)
	switch opcode {
	case dnfenum.CmdPacketMailboxOpen:
		return handleMailboxOpen(ctx, req)
	case dnfenum.CmdPacketMailboxSend:
		return handleMailboxSend(ctx, req)
	case dnfenum.CmdPacketMailboxExtractItem:
		return handleMailboxExtract(ctx, req)
	case dnfenum.CmdPacketChangeLetterStat:
		return handleMailboxChangeState(ctx, req)
	case dnfenum.CmdPacketMultiMailboxSend:
		return handleMailboxMultiSend(ctx, req)
	case dnfenum.CmdPacketQueryCharacInfoMailbox:
		return handleMailboxQueryCharacter(ctx, req)
	case dnfenum.CmdPacketRequestServerCharacterList:
		return handleMailboxRecipientCharacterList(ctx, req)
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("mail module has no route for opcode %d", req.Opcode),
		}, nil
	}
}

func handleMailboxRecipientCharacterList(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	parsed, err := decodeRecipientCharacterListRequest(req.Body)
	if err != nil {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       "mailbox_recipient_list_rejected",
			Reason:          err.Error(),
		}, nil
	}
	if req.Repositories.Character == nil {
		return alignedcmd.Result{}, ErrMailboxCharacterMissing
	}
	accountID := strings.TrimSpace(req.AccountID)
	if accountID == "" {
		return alignedcmd.Result{}, ErrMailboxCharacterMissing
	}
	characters, err := req.Repositories.Character.ListByAccount(ctx, accountID, dnfrepo.DefaultCharacterSlotLimit)
	if err != nil {
		return alignedcmd.Result{}, err
	}
	body, err := buildRecipientCharacterListBody(parsed.ServerID, characters, req.SelectedCharacterID)
	if err != nil {
		return alignedcmd.Result{}, err
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       "mailbox_recipient_list",
		Reason:          fmt.Sprintf("current op789 server=%d returned %d other account roles through op718", parsed.ServerID, body[1]),
		UpperResponses: []alignedcmd.UpperResponse{
			class0Response(mailboxRecipientListMessageID, body),
		},
	}, nil
}

func handleMailboxOpen(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	if err := decodeMailboxOpenRequest(req.Body); err != nil {
		return class1Result(req.Opcode, "mailbox_open_rejected", err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	characterID, err := characterIDFromRequest(req.SelectedCharacterID)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_open_rejected", err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	result, err := NewOwner(req.Repositories).Open(ctx, characterID)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_open_rejected", err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	snapshot, err := buildMailboxListNotification(result.Mails, result.NotLoaded, result.ObservedAt)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_open_rejected", "mailbox snapshot unavailable: "+err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	out := class1Result(
		req.Opcode,
		"mailbox_open",
		fmt.Sprintf("current op96 ACK not_loaded=%d total=%d unread=%d claimable=%d", result.NotLoaded, result.Total, result.Unread, result.Claimable),
		buildMailboxOpenAck(result.NotLoaded),
	)
	out.UpperResponses = append(out.UpperResponses, class0Response(mailboxListNotificationMessageID, snapshot))
	return out, nil
}

func handleMailboxSend(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	return handleMailboxSendOpcode(ctx, req, dnfenum.CmdPacketMailboxSend)
}

func handleMailboxSendOpcode(ctx context.Context, req alignedcmd.Request, opcode dnfenum.CmdPacket) (alignedcmd.Result, error) {
	parsed, err := decodeSendRequest(opcode, req.Body)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_send_rejected", "invalid current mailbox send body: "+err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	characterID, err := characterIDFromRequest(req.SelectedCharacterID)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_send_rejected", err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	result, err := NewOwner(req.Repositories, req.MailboxItemResolver).Send(ctx, characterID, parsed)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_send_rejected", "mailbox send rejected: "+err.Error()+"; request="+parsed.String(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	out := class1Result(
		req.Opcode,
		"mailbox_send_ack",
		fmt.Sprintf("mailbox send committed mail=%s recipient=%s; request=%s", result.MailID, result.RecipientCharacterID, parsed.String()),
		buildSuccessAck(),
	)
	if recipientID, parseErr := strconv.ParseUint(result.RecipientCharacterID, 10, 16); parseErr == nil && recipientID != 0 {
		out.MailboxAlarmRecipientID = uint16(recipientID)
	}
	return out, nil
}

func handleMailboxExtract(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	parsed, err := decodeExtractRequest(req.Body)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_extract_rejected", "invalid current mailbox extract body: "+err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	characterID, err := characterIDFromRequest(req.SelectedCharacterID)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_extract_rejected", err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	result, err := NewOwner(req.Repositories, req.MailboxItemResolver).Claim(ctx, characterID, parsed)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_extract_rejected", "mailbox extract rejected: "+err.Error()+"; request="+parsed.String(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	out := class1Result(
		req.Opcode,
		"mailbox_extract_ack",
		fmt.Sprintf("claimed mails=%v gold=%d items=%d", result.MailIDs, result.Gold, result.ItemCount),
		buildExtractAck(result.MailIDs),
	)
	// op95 changes the claim state, but the current client keeps its prior
	// attachment widgets until it receives another complete class0/0x61 page.
	// Reload after the committed transfer and send that clear-first snapshot in
	// the same response sequence as op96.
	refreshed, refreshErr := NewOwner(req.Repositories).Open(ctx, characterID)
	if refreshErr == nil {
		snapshot, snapshotErr := buildMailboxListNotification(refreshed.Mails, refreshed.NotLoaded, refreshed.ObservedAt)
		if snapshotErr == nil {
			out.UpperResponses = append(out.UpperResponses, class0Response(mailboxListNotificationMessageID, snapshot))
		}
	}
	// The mailbox stays open while op95 is handled. Full op13 pages are only
	// evidenced for character bootstrap and can corrupt that live UI state;
	// refresh exactly the durable rows that this claim changed through op14.
	out.ItemSlotRefreshes = append(out.ItemSlotRefreshes, result.ItemSlotRefreshes...)
	return out, nil
}

func handleMailboxChangeState(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	parsed, err := decodeChangeStateRequest(req.Body)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_state_rejected", "invalid current mailbox state body: "+err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	characterID, err := characterIDFromRequest(req.SelectedCharacterID)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_state_rejected", err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	if err := NewOwner(req.Repositories).ChangeState(ctx, characterID, parsed); err != nil {
		return class1Result(req.Opcode, "mailbox_state_rejected", err.Error()+"; request="+parsed.String(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	out := class1Result(
		req.Opcode,
		"mailbox_state_ack",
		"mailbox state committed; request="+parsed.String(),
		buildChangeStateAck(parsed.MailIDs, parsed.Status),
	)
	if parsed.Status == 0 {
		out.UpperResponses = append(out.UpperResponses, class0Response(
			mailboxRemoveNotificationMessageID,
			buildMailboxRemoveNotification(parsed.MailIDs),
		))
	}
	return out, nil
}

func handleMailboxMultiSend(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	return handleMailboxSendOpcode(ctx, req, dnfenum.CmdPacketMultiMailboxSend)
}

func handleMailboxQueryCharacter(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	parsed, err := decodeQueryCharacterRequest(req.Body)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_query_rejected", "invalid current mailbox query body: "+err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	if req.Repositories.Character == nil {
		return class1Result(req.Opcode, "mailbox_query_rejected", ErrMailboxCharacterMissing.Error(), buildErrorAck(mailboxErrorCode(ErrMailboxCharacterMissing))), nil
	}
	characterID, found, err := req.Repositories.Character.FindIDByName(ctx, parsed.Name)
	if err != nil {
		return class1Result(req.Opcode, "mailbox_query_rejected", "query character failed: "+err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	if !found {
		return class1Result(req.Opcode, "mailbox_query_rejected", "recipient not found: "+parsed.Name, buildErrorAck(mailboxErrorCode(ErrMailboxRecipientMissing))), nil
	}
	character, found, err := req.Repositories.Character.Load(ctx, characterID)
	if err != nil || !found {
		if err == nil {
			err = ErrMailboxRecipientMissing
		}
		return class1Result(req.Opcode, "mailbox_query_rejected", "load queried character: "+err.Error(), buildErrorAck(mailboxErrorCode(err))), nil
	}
	job, grow, level, err := mailboxCharacterProjection(character)
	if err != nil {
		return class1Result(
			req.Opcode,
			"mailbox_query_rejected",
			"queried character projection is unavailable: "+err.Error(),
			buildErrorAck(mailboxErrorCode(err)),
		), nil
	}
	return class1Result(
		req.Opcode,
		"mailbox_query_ack",
		fmt.Sprintf("current op324 character=%s job=%d grow=%d level=%d mode=%d", characterID, job, grow, level, parsed.Mode),
		buildQueryCharacterAck(parsed.NameRaw, grow, level, job),
	), nil
}

func mailboxCharacterProjection(character dnfrepo.CharacterRecord) (byte, byte, uint16, error) {
	jobValue, err := strconv.ParseUint(strings.TrimSpace(character.Job), 10, 8)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%w: invalid job %q", ErrMailboxInvalidRequest, character.Job)
	}
	growValue, found := character.Stats["grow_type"]
	if !found || growValue < 0 || growValue > 0xff {
		return 0, 0, 0, fmt.Errorf("%w: invalid grow_type", ErrMailboxInvalidRequest)
	}
	if character.Level <= 0 || character.Level > 0xffff {
		return 0, 0, 0, fmt.Errorf("%w: invalid level %d", ErrMailboxInvalidRequest, character.Level)
	}
	return byte(jobValue), byte(growValue), uint16(character.Level), nil
}

func class1Result(opcode uint16, operation string, reason string, body []byte) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       operation,
		Reason:          reason,
		UpperResponses:  []alignedcmd.UpperResponse{class1Response(opcode, body)},
	}
}
