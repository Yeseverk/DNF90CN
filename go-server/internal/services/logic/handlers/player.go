package handlers

import (
	"context"
	"strings"
	"time"

	"longheng.io/server/internal/platform/dispatch"
	"longheng.io/server/internal/reference/player"
	"longheng.io/server/pkg/protocol"
)

type EnterWorld struct {
	Players *player.Module
}

func (h EnterWorld) Handle(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
	if h.Players == nil {
		return dispatch.Response{}, protocol.NewError(protocol.CodeUnavailable, "player module unavailable")
	}
	profile, err := h.Players.LoadOrCreate(ctx, req.AccountID)
	if err != nil {
		return dispatch.Response{}, protocol.WrapError(protocol.CodeInternal, "load player profile", err)
	}
	return dispatch.Response{
		PacketID: protocol.PacketIDReqResp,
		MsgID:    req.MsgID,
		Body: protocol.EncodeEnterWorldResponse(protocol.EnterWorldResponse{
			AccountID:      profile.AccountID,
			RoleID:         profile.RoleID,
			Name:           profile.Name,
			Level:          profile.Level,
			Gold:           profile.Currencies["gold"],
			Diamond:        profile.Currencies["diamond"],
			InventoryCount: len(profile.Inventory),
			QuestCount:     len(profile.Quests),
			UnreadMail:     countUnreadMail(profile.Mailbox),
			ServerTime:     time.Now().Unix(),
		}),
		Note: "EnterWorld",
	}, nil
}

type AddCurrency struct {
	Players *player.Module
}

func (h AddCurrency) Handle(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
	if h.Players == nil {
		return dispatch.Response{}, protocol.NewError(protocol.CodeUnavailable, "player module unavailable")
	}
	body := protocol.DecodeAddCurrencyRequest(req.Body)
	body.Currency = strings.TrimSpace(body.Currency)
	if body.Currency == "" {
		return dispatch.Response{}, protocol.NewError(protocol.CodeBadRequest, "currency is required")
	}
	profile, err := h.Players.AddCurrency(ctx, req.AccountID, body.Currency, body.Delta)
	if err != nil {
		return dispatch.Response{}, protocol.WrapError(protocol.CodeInternal, "add currency", err)
	}
	return dispatch.Response{
		PacketID: protocol.PacketIDReqResp,
		MsgID:    req.MsgID,
		Body: protocol.EncodeAddCurrencyResponse(protocol.AddCurrencyResponse{
			AccountID:  profile.AccountID,
			Currency:   body.Currency,
			Balance:    profile.Currencies[body.Currency],
			ServerTime: time.Now().Unix(),
		}),
		Note: "AddCurrency",
	}, nil
}

type ReadMail struct {
	Players *player.Module
}

func (h ReadMail) Handle(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
	if h.Players == nil {
		return dispatch.Response{}, protocol.NewError(protocol.CodeUnavailable, "player module unavailable")
	}
	body := protocol.DecodeReadMailRequest(req.Body)
	body.MailID = strings.TrimSpace(body.MailID)
	if body.MailID == "" {
		return dispatch.Response{}, protocol.NewError(protocol.CodeBadRequest, "mail_id is required")
	}
	profile, found, changed, err := h.Players.MarkMailRead(ctx, req.AccountID, body.MailID)
	if err != nil {
		return dispatch.Response{}, protocol.WrapError(protocol.CodeInternal, "read mail", err)
	}
	if !found {
		return dispatch.Response{}, protocol.Errorf(protocol.CodeNotFound, "mail_id %s not found", body.MailID)
	}
	return dispatch.Response{
		PacketID: protocol.PacketIDReqResp,
		MsgID:    req.MsgID,
		Body: protocol.EncodeReadMailResponse(protocol.ReadMailResponse{
			AccountID:  profile.AccountID,
			MailID:     body.MailID,
			Changed:    changed,
			UnreadMail: countUnreadMail(profile.Mailbox),
			ServerTime: time.Now().Unix(),
		}),
		Note: "ReadMail",
	}, nil
}

func countUnreadMail(mailbox []player.MailRecord) int {
	total := 0
	for _, mail := range mailbox {
		if mail.State == "unread" {
			total++
		}
	}
	return total
}
