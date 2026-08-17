package handlers

import (
	"context"
	"fmt"
	"strings"

	"longheng.io/server/internal/platform/dispatch"
	"longheng.io/server/internal/reference/player"
	"longheng.io/server/pkg/protocol"
)

//go:generate go run ../../../../cmd/routegen -schema ../../../../configs/handlers/logic.json -out handler_schema_gen.go -module longheng.io/server

type Context struct {
	Players *player.Module
}

type Handler interface {
	Meta() dispatch.HandlerMeta
	Handle(context.Context, dispatch.Request) (dispatch.Response, error)
}

func RegisterAll(mux *dispatch.Mux, handlers ...Handler) error {
	if mux == nil {
		return fmt.Errorf("dispatch mux is required")
	}
	seen := make(map[uint32]string, len(handlers))
	for _, existing := range mux.Snapshot() {
		seen[existing.MsgID] = handlerName(existing)
	}
	for _, handler := range handlers {
		if handler == nil {
			return fmt.Errorf("handler is nil")
		}
		meta := handler.Meta()
		if meta.MsgID == 0 {
			return fmt.Errorf("handler %s has empty msg_id", meta.Name)
		}
		name := handlerName(meta)
		if existing, ok := seen[meta.MsgID]; ok {
			return fmt.Errorf("duplicate msg_id %d for handlers %s and %s", meta.MsgID, existing, name)
		}
		seen[meta.MsgID] = name
	}
	for _, handler := range handlers {
		meta := handler.Meta()
		current := handler
		if err := mux.HandleMetaChecked(meta, func(ctx context.Context, req dispatch.Request) (dispatch.Response, error) {
			req.AccountID = strings.TrimSpace(req.AccountID)
			if meta.PlayerScoped && req.AccountID == "" {
				return dispatch.Response{}, protocol.NewError(protocol.CodeBadRequest, "account_id is required")
			}
			return current.Handle(ctx, req)
		}); err != nil {
			return err
		}
	}
	return nil
}

func handlerName(meta dispatch.HandlerMeta) string {
	if meta.Name != "" {
		return meta.Name
	}
	return fmt.Sprintf("msg_%d", meta.MsgID)
}
