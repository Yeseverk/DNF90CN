package cargo

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
	return dnfenum.AlignedDomainCargo
}

func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketCreateAccountCargo:
		cmd := NewAccountCommand(req, "create_account_cargo")
		return handleAccountPlan(ctx, req, cmd)
	case dnfenum.CmdPacketUpgradeAccountCargo:
		cmd := NewAccountCommand(req, "upgrade_account_cargo")
		return handleAccountPlan(ctx, req, cmd)
	case dnfenum.CmdPacketDepositMoney:
		parsed, err := DecodeGoldRequest(req.Body)
		cmd := NewMoneyCommand(req, MoneyDeposit, parsed)
		return handleMoneyPlan(ctx, req, cmd, err)
	case dnfenum.CmdPacketWithdrawMoney:
		parsed, err := DecodeGoldRequest(req.Body)
		cmd := NewMoneyCommand(req, MoneyWithdraw, parsed)
		return handleMoneyPlan(ctx, req, cmd, err)
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("cargo module has no opcode %d handler", req.Opcode),
		}, nil
	}
}

func handleAccountPlan(ctx context.Context, req alignedcmd.Request, cmd Command) (alignedcmd.Result, error) {
	owner, err := NewOwner(req.Repositories)
	if err != nil {
		return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
	}
	result, err := owner.ApplyAccount(ctx, cmd.Operation, AccountCommand{
		AccountID:           cmd.AccountID,
		SelectedCharacterID: cmd.SelectedCharacterID,
	})
	if err != nil {
		return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
	}
	return ownerAppliedResult(req.Opcode, cmd.Operation, cmd.String(), result), nil
}

func handleMoneyPlan(ctx context.Context, req alignedcmd.Request, cmd Command, parseErr error) (alignedcmd.Result, error) {
	if parseErr != nil {
		return blockedParsedResult(cmd.Operation, cmd.String(), parseErr), nil
	}
	owner, err := NewOwner(req.Repositories)
	if err != nil {
		return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
	}
	result, err := owner.ApplyMoney(ctx, MoneyCommand{
		AccountID:           cmd.AccountID,
		SelectedCharacterID: cmd.SelectedCharacterID,
		Direction:           cmd.MoneyDirection,
		Amount:              cmd.Amount,
	})
	if err != nil {
		return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
	}
	return ownerAppliedResult(req.Opcode, cmd.Operation, cmd.String(), result), nil
}

func blockedResult(operation string, summary string) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("parsed %s: %s; success ACK is blocked until cargo asset writes are closed", operation, summary),
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
	return blockedResult(operation, summary)
}

func ownerBlockedResult(operation string, summary string, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("parsed %s: %s; cargo owner preflight failed: %v; success ACK is blocked", operation, summary, err),
	}
}

func ownerAppliedResult(opcode uint16, operation string, summary string, result PlanResult) alignedcmd.Result {
	responses := cargoUpperResponses(opcode, operation, result)
	postActions := make([]alignedcmd.PostAction, 0, 1)
	if operation == "create_account_cargo" || operation == "upgrade_account_cargo" {
		// The list-12 body belongs to the bridge because it is decoded by the
		// current EXE's item-list reader.  Do not send the former empty/replayed
		// cargo body here: it lost account-owned rows and mixed owner state.
		if result.Cost.Kind == CostMaterial {
			// The material is a real list-0 stack. Refresh the character-owned
			// containers after its durable transaction as well as list 12.
			postActions = append(postActions, alignedcmd.PostActionRefreshSelectedItemContainers)
		}
		postActions = append(postActions, alignedcmd.PostActionRefreshSelectedAccountCargo)
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: len(responses) > 0,
		Operation:       operation,
		Reason: fmt.Sprintf(
			"parsed %s: %s; cargo owner applied account=%s char=%s direction=%s amount=%d charGold=%d charCera=%d cargoGold=%d cargoLevel=%d cargoCreated=%t cost=%s/%d changed=%t; C# order response opened",
			operation,
			summary,
			result.AccountID,
			result.CharacterID,
			result.Direction,
			result.Amount,
			result.CharacterGold,
			result.CharacterCera,
			result.CargoGold,
			result.CargoLevel,
			result.CargoCreated,
			result.Cost.Kind,
			result.Cost.Amount,
			result.Changed,
		),
		UpperResponses: responses,
		PostActions:    postActions,
	}
}

func cargoUpperResponses(opcode uint16, operation string, result PlanResult) []alignedcmd.UpperResponse {
	switch operation {
	case "deposit_money", "withdraw_money":
		return []alignedcmd.UpperResponse{
			class1Response(opcode, buildCargoGoldAck(result.CargoGold)),
			class0Response(msgItemListUpdate, buildGoldUpdateBody(result.CharacterGold)),
		}
	case "create_account_cargo", "upgrade_account_cargo":
		responses := []alignedcmd.UpperResponse{class1Response(opcode, buildSuccessAck())}
		switch result.Cost.Kind {
		case CostGold:
			responses = append(responses, class0Response(msgItemListUpdate, buildGoldUpdateBody(result.CharacterGold)))
		case CostCera:
			responses = append(responses, class0Response(msgCeraUpdate, buildCeraUpdateBody(result.CharacterCera)))
		}
		return responses
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
