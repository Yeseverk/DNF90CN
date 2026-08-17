package itemlock

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

type handler struct{}

func NewHandler() alignedcmd.Handler {
	return handler{}
}

func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainItemLock
}

func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketRequestItemLock:
		parsed, err := DecodeRequest(req.Body)
		cmd := NewCommand(req, "request_item_lock", parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketRequestItemUnlock:
		parsed, err := DecodeRequest(req.Body)
		cmd := NewCommand(req, "request_item_unlock", parsed)
		return applyOwner(ctx, req, cmd, err), nil
	case dnfenum.CmdPacketRequestItemUnlockCancel:
		parsed, err := DecodeRequest(req.Body)
		cmd := NewCommand(req, "request_item_unlock_cancel", parsed)
		return applyOwner(ctx, req, cmd, err), nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("itemlock module has no opcode %d handler", req.Opcode),
		}, nil
	}
}

func applyOwner(ctx context.Context, req alignedcmd.Request, cmd Command, parseErr error) alignedcmd.Result {
	if parseErr != nil {
		return blockedParsedResult(cmd.Operation, cmd.String(), parseErr)
	}
	owner, err := NewOwner(req.Repositories)
	if err != nil {
		return ownerBlockedResult(cmd, err)
	}
	result, err := owner.Apply(ctx, cmd)
	if err != nil {
		return ownerBlockedResult(cmd, err)
	}
	return ownerAppliedResult(req.Opcode, cmd, result)
}

func ownerBlockedResult(cmd Command, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("parsed %s: %s; itemlock owner failed: %v; success ACK is blocked", cmd.Operation, cmd.String(), err),
	}
}

func ownerAppliedResult(opcode uint16, cmd Command, result Result) alignedcmd.Result {
	responses := itemLockUpperResponses(opcode, cmd.Operation, result)
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: len(responses) > 0,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"itemlock owner applied: %s state=%q changed=%t; opened %s response in C# order",
			cmd.String(),
			result.State,
			result.Changed,
			ackEvidenceLabel(cmd.Operation),
		),
		UpperResponses: responses,
	}
}

func itemLockUpperResponses(opcode uint16, operation string, result Result) []alignedcmd.UpperResponse {
	switch operation {
	case "request_item_lock":
		return []alignedcmd.UpperResponse{
			class1Response(opcode, buildLockAck(result.ListType, result.SlotIndex)),
			class0Response(msgItemLockList, buildLockListDelta(result.ListType, result.SlotIndex, itemLockStateActive, 0)),
		}
	case "request_item_unlock":
		return []alignedcmd.UpperResponse{
			class1Response(opcode, buildUnlockAck(result.ListType, result.SlotIndex, 0)),
			class0Response(msgItemUnlockNotice, buildLockAck(result.ListType, result.SlotIndex)),
		}
	case "request_item_unlock_cancel":
		return []alignedcmd.UpperResponse{
			class1Response(opcode, buildLockAck(result.ListType, result.SlotIndex)),
			class0Response(msgItemLockList, buildLockListDelta(result.ListType, result.SlotIndex, itemLockStateActive, 0)),
		}
	default:
		return nil
	}
}

func class1Response(opcode uint16, body []byte) alignedcmd.UpperResponse {
	return alignedcmd.UpperResponse{
		MsgID:          opcode,
		Body:           body,
		Classification: dnfproto.DefaultChannelClassification,
		AllowCodec:     true,
	}
}

func class0Response(msgID uint16, body []byte) alignedcmd.UpperResponse {
	return alignedcmd.UpperResponse{
		MsgID:          msgID,
		Body:           body,
		Classification: 0,
		AllowCodec:     true,
	}
}

func ackEvidenceLabel(operation string) string {
	switch operation {
	case "request_item_lock":
		return "0x010B"
	case "request_item_unlock":
		return "0x010C"
	case "request_item_unlock_cancel":
		return "0x010D"
	default:
		return "ACK"
	}
}

func blockedParsedResult(operation string, summary string, err error) alignedcmd.Result {
	if err != nil {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       operation,
			Reason:          fmt.Sprintf("%s body parse failed: %v; success ACK is blocked", operation, err),
		}
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("parsed %s: %s; itemlock persistence and NOTI order are not closed", operation, summary),
	}
}
