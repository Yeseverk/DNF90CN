// 本文件负责宠物命令的协议分发。
// 宠物孵化会改动物品栏、宠物列表和城镇显示状态，当前只解析请求体，不直接回成功包。
package pet

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

type handler struct{}

// alignedHatchResolver adapts the bridge-owned runtime-PVF boundary without
// making the pet domain depend on bridge services or PVF parser internals.
type alignedHatchResolver struct {
	resolve alignedcmd.PetHatchResolver
}

func (resolver alignedHatchResolver) ResolveHatch(eggItemID int64) (PetHatchDefinition, error) {
	if resolver.resolve == nil {
		return PetHatchDefinition{}, ErrPetCatalogUnavailable
	}
	resolved, err := resolver.resolve(eggItemID)
	if err != nil {
		return PetHatchDefinition{}, err
	}
	return PetHatchDefinition{
		EggItemID:      resolved.EggItemID,
		HatchedItemID:  resolved.HatchedItemID,
		EggPVFPath:     resolved.EggPVFPath,
		HatchedPVFPath: resolved.HatchedPVFPath,
		MinimumLevel:   resolved.MinimumLevel,
	}, nil
}

// NewHandler 创建宠物协议处理器。
func NewHandler() alignedcmd.Handler {
	return handler{}
}

// Domain 返回宠物业务域。
func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainPet
}

// Handle 解析宠物请求，并在宠物 owner 与列表刷新闭合前禁止成功 ACK。
func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketRenameCreature:
		parsed, err := DecodeRenameCreatureRequest(req.Body)
		cmd := NewRenameCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
		}
		result, err := owner.Rename(ctx, RenameCommand{
			SelectedCharacterID: req.SelectedCharacterID,
			ListType:            parsed.ListType,
			SlotIndex:           parsed.SlotIndex,
			NameRaw:             parsed.NameRaw,
		})
		if err != nil {
			return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
		}
		return ownerRenameAppliedResultOpen(req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketHatchCreature:
		parsed, err := DecodeHatchCreatureRequest(req.Body)
		operation := "hatch_creature"
		cmd := NewHatchCommand(req, operation, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		var owner *Owner
		if req.PetHatchResolver == nil {
			owner, err = NewOwner(req.Repositories)
		} else {
			owner, err = NewOwner(req.Repositories, alignedHatchResolver{resolve: req.PetHatchResolver})
		}
		if err != nil {
			return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
		}
		result, err := owner.Hatch(ctx, HatchCommand{
			SelectedCharacterID: req.SelectedCharacterID,
			ListType:            parsed.ListType,
			SlotIndex:           parsed.SlotIndex,
		})
		if err != nil {
			return ownerBlockedResult(cmd.Operation, cmd.String(), err), nil
		}
		return ownerAppliedResultOpen(req.Opcode, cmd.Operation, cmd.String(), result), nil
	case dnfenum.CmdPacketHatchCreatureEgg:
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       "hatch_creature_egg_blocked",
			Reason:          "current EXE proves op173 only as a bodyless S2C refresh; old C2S hatch mapping is blocked",
		}, nil
	case dnfenum.CmdPacketRequestHatchedCreature:
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       "request_hatched_creature_blocked",
			Reason:          "current NoPack has no proved C2S op174 pet request; scene class0/op174 is a generic runtime-object refresh, so the historical binding is disabled",
		}, nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("pet 模块未登记 opcode %d", req.Opcode),
		}, nil
	}
}

func ownerRenameAppliedResultOpen(opcode uint16, cmd Command, result RenameResult) alignedcmd.Result {
	if result.ListType != listTypePet ||
		result.SourceListType != currentMainInventoryListType ||
		result.SlotIndex < 0 ||
		result.SlotIndex > 139 {
		return ownerBlockedResult(cmd.Operation, cmd.String(), ErrPetStateInvalid)
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"pet owner applied: %s %s char=%s key=%s serial=%d item=%d changed=%t card_remaining=%d; emitted current EXE class1/op100 semantic-creature ACK, native class0/op101 in-place name notification and main-inventory source-card row refresh",
			cmd.Operation,
			cmd.String(),
			result.CharacterID,
			result.PetKey,
			result.CreatureSerial,
			result.ItemID,
			result.Changed,
			result.RemainingCount,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildRenameCreatureSuccessAck(result.SlotIndex, result.ListType)),
			class0Response(101, buildRenameCreatureNameNotificationBody(cmd.SelectedCharacterID, result.NameRaw)),
		},
		ItemSlotRefreshes: []alignedcmd.ItemSlotRefresh{{
			ListType:  result.SourceListType,
			SlotIndex: result.SlotIndex,
		}},
	}
}

func blockedParsedResult(operation string, summary string, err error) alignedcmd.Result {
	if err != nil {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: false,
			Operation:       operation,
			Reason:          fmt.Sprintf("%s 请求体解析失败：%v；禁止回成功包", operation, err),
		}
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；宠物物品、宠物列表和城镇显示刷新闭合前禁止回包", operation, summary),
	}
}

func ownerBlockedResult(operation string, summary string, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；pet owner 写库未完成：%v；禁止回成功包", operation, summary, err),
	}
}

func ownerAppliedResult(operation string, summary string, result HatchResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；pet owner 已写库 char=%s key=%s item=%d slot=%d changed=%t；列表刷新/城镇显示回包未闭合，禁止回成功包", operation, summary, result.CharacterID, result.PetKey, result.ItemID, result.SourceSlotIndex, result.Changed),
	}
}

func ownerAppliedResultOpen(opcode uint16, operation string, summary string, result HatchResult) alignedcmd.Result {
	responses := petHatchResponses(opcode, result)
	if len(responses) == 0 {
		return ownerAppliedResult(operation, summary, result)
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       operation,
		Reason: fmt.Sprintf(
			"pet owner applied: %s %s char=%s key=%s item=%d slot=%d changed=%t; emitted typed ACK, list-7 refresh and creature-list bodies",
			operation,
			summary,
			result.CharacterID,
			result.PetKey,
			result.ItemID,
			result.SourceSlotIndex,
			result.Changed,
		),
		UpperResponses: responses,
	}
}

func ownerReadResult(operation string, summary string, result ListResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；pet owner 已读取 char=%s count=%d equipped=%q townDisplay=%t；当前 EXE 响应包体未闭合，禁止回成功包", operation, summary, result.CharacterID, result.EntryCount, result.EquippedKey, result.TownDisplay),
	}
}

func ownerReadResultOpen(operation string, summary string, result ListResult) alignedcmd.Result {
	responses := petReadResponses(operation, result)
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: len(responses) > 0,
		Operation:       operation,
		Reason: fmt.Sprintf(
			"pet owner read: %s %s char=%s count=%d typed=%d equipped=%q townDisplay=%t",
			operation,
			summary,
			result.CharacterID,
			result.EntryCount,
			len(result.Entries),
			result.EquippedKey,
			result.TownDisplay,
		),
		UpperResponses: responses,
	}
}

func petReadResponses(operation string, result ListResult) []alignedcmd.UpperResponse {
	if operation != "request_hatched_creature" || len(result.Entries) != result.EntryCount {
		return nil
	}
	creatureListBody, err := buildCreatureListBody(result.Entries)
	if err != nil {
		return nil
	}
	return []alignedcmd.UpperResponse{
		class1Response(uint16(dnfenum.CmdPacketRequestHatchedCreature), buildSuccessAck()),
		class0Response(0x0069, creatureListBody),
	}
}

func petHatchResponses(opcode uint16, result HatchResult) []alignedcmd.UpperResponse {
	if len(result.Entries) != result.EntryCount || result.EntryCount == 0 {
		return nil
	}
	creatureListBody, err := buildCreatureListBody(result.Entries)
	if err != nil {
		return nil
	}
	return []alignedcmd.UpperResponse{
		class1Response(opcode, buildSuccessAck()),
		class0Response(0x000D, buildPetItemListRefreshBody(result.PetInventory)),
		class0Response(0x0069, creatureListBody),
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
