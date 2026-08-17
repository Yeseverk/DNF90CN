// Package inventory routes current-EXE inventory commands to repository-backed
// owners. Mutations return success only after the required transaction and
// authoritative ACK/refresh sequence have been constructed.
package inventory

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfequip "longheng.io/server/internal/modules/dnf/equip"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrevivecoin "longheng.io/server/internal/modules/dnf/revivecoin"
)

type handler struct{}

// NewHandler 创建背包协议处理器。
func NewHandler() alignedcmd.Handler {
	return handler{}
}

// Domain 返回背包业务域。
func (handler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainInventory
}

// Handle 解析已经对齐协议号的背包请求，并在关键写路径闭合前阻止 bridge 伪造成功 ACK。
func (handler) Handle(ctx context.Context, req alignedcmd.Request) (alignedcmd.Result, error) {
	switch dnfenum.CmdPacket(req.Opcode) {
	case dnfenum.CmdPacketDeleteItem:
		parsed, err := DecodeDeleteRequest(req.Body)
		cmd := NewDeleteCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		if err := checkDeleteListType(cmd.SourceListType); err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.Delete(ctx, cmd)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedDeleteResult(req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketMoveItemspace:
		parsed, err := DecodeMoveItemspaceRequest(req.Body)
		cmd := NewMoveCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		petEquipmentMove := isProvenPetEquipmentMove(cmd)
		if petEquipmentMove {
			if err := validateProvenPetEquipmentMove(cmd); err != nil {
				return ownerBlockedResult(cmd, err), nil
			}
			var owner *dnfequip.Owner
			if cmd.DestinationSlotIndex == 26 {
				owner, err = dnfequip.NewOwner(req.Repositories)
			} else if req.EquipmentPlacement == nil {
				return ownerBlockedResult(cmd, fmt.Errorf("pet artifact move requires the current PVF placement validator")), nil
			} else {
				owner, err = dnfequip.NewOwnerWithPlacementValidator(req.Repositories, dnfequip.PlacementValidatorFunc(func(ctx context.Context, placement dnfequip.Placement) error {
					return req.EquipmentPlacement(ctx, alignedcmd.EquipmentPlacementRequest{
						CharacterID:     placement.CharacterID,
						ItemID:          placement.ItemID,
						SourceListType:  placement.SourceListType,
						SourceSlotIndex: placement.SourceSlotIndex,
						TargetSlotIndex: placement.TargetSlotIndex,
					})
				}))
			}
			if err != nil {
				return ownerBlockedResult(cmd, err), nil
			}
			ownerCommand := normalizedEquipmentMoveCommand(cmd)
			// Current NoPack uses actor-worn endpoint 3 or 17 on the wire.
			// Both resolve to the same durable EquipmentRecord. Keep the raw
			// endpoint in cmd for request evidence while the ACK reports the
			// owner's committed list-3 endpoint.
			result, err := owner.Move(ctx, ownerCommand)
			if err != nil {
				return ownerBlockedResult(cmd, err), nil
			}
			return ownerAppliedPetEquipmentMoveResult(req.Opcode, cmd, result), nil
		}
		if (cmd.SourceListType == listTypePet || cmd.DestinationListType == listTypePet) &&
			!(cmd.SourceListType == listTypePet && cmd.DestinationListType == listTypePet) {
			return blockedParsedResult(cmd.Operation, cmd.String(), fmt.Errorf("unsupported current-EXE pet move endpoints")), nil
		}
		if isActorWornEndpoint(cmd.SourceListType) || isActorWornEndpoint(cmd.DestinationListType) {
			var owner *dnfequip.Owner
			if req.EquipmentPlacement == nil {
				return ownerBlockedResult(cmd, fmt.Errorf("normal equipment move requires the current PVF placement validator")), nil
			} else {
				owner, err = dnfequip.NewOwnerWithPlacementValidator(req.Repositories, dnfequip.PlacementValidatorFunc(func(ctx context.Context, placement dnfequip.Placement) error {
					return req.EquipmentPlacement(ctx, alignedcmd.EquipmentPlacementRequest{
						CharacterID:     placement.CharacterID,
						ItemID:          placement.ItemID,
						SourceListType:  placement.SourceListType,
						SourceSlotIndex: placement.SourceSlotIndex,
						TargetSlotIndex: placement.TargetSlotIndex,
					})
				}))
			}
			if err != nil {
				return ownerBlockedResult(cmd, err), nil
			}
			owner.SetNameTagChecker(req.NameTagChecker)
			result, err := owner.Move(ctx, normalizedEquipmentMoveCommand(cmd))
			if err != nil {
				return ownerBlockedResult(cmd, err), nil
			}
			return ownerAppliedEquipmentMoveResult(req.Opcode, cmd, result), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.Move(ctx, cmd)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedMoveResult(ctx, req.Repositories.Settings, req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketSortItem:
		parsed, err := DecodeSortItemRequest(req.Body)
		cmd := NewSortCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.Sort(ctx, cmd)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedSortResult(ctx, req.Repositories.Settings, req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketBuyItem:
		parsed, err := DecodeBuyItemRequest(req.Body)
		cmd := NewBuyCommand(req, parsed)
		return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
	case dnfenum.CmdPacketSellItem:
		parsed, err := DecodeDeleteOrSellRequest(req.Body)
		cmd := NewSellCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.Sell(ctx, cmd)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedSellResult(req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketRepairEquipment:
		parsed, err := DecodeRepairEquipmentRequest(req.Body)
		cmd := NewRepairCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		if cmd.SourceSlotIndex == -1 {
			// 全部修理 (86JP TryRepairAll): 穿戴装备 + 主列表快捷栏 slot 3..8,
			// 合计费用一次扣除, ACK 槽位 0xFFFF, 客户端本地拉满。
			if cmd.SourceListType != listTypeEquipment && cmd.SourceListType != listTypeMain {
				return ownerBlockedResult(cmd, fmt.Errorf("%w: %d", ErrUnsupportedList, cmd.SourceListType)), nil
			}
			owner, err := dnfequip.NewOwner(req.Repositories)
			if err != nil {
				return ownerBlockedResult(cmd, err), nil
			}
			result, err := owner.RepairAll(ctx, dnfequip.RepairCommand{
				SelectedCharacterID: cmd.SelectedCharacterID,
				AccountID:           req.AccountID,
				SlotIndex:           cmd.SourceSlotIndex,
				QuickRepair:         cmd.QuickRepair,
				AutoRepair:          cmd.AutoRepair,
			}, req.RepairCostResolver)
			if err != nil {
				return ownerBlockedResult(cmd, err), nil
			}
			return ownerAppliedRepairAllResult(req.Opcode, cmd, result), nil
		}
		if cmd.SourceListType == listTypeEquipment {
			owner, err := dnfequip.NewOwner(req.Repositories)
			if err != nil {
				return ownerBlockedResult(cmd, err), nil
			}
			result, err := owner.Repair(ctx, dnfequip.RepairCommand{
				SelectedCharacterID: cmd.SelectedCharacterID,
				AccountID:           req.AccountID,
				SlotIndex:           cmd.SourceSlotIndex,
				QuickRepair:         cmd.QuickRepair,
				AutoRepair:          cmd.AutoRepair,
			}, req.RepairCostResolver)
			if err != nil {
				return ownerBlockedResult(cmd, err), nil
			}
			return ownerAppliedEquippedRepairResult(req.Opcode, cmd, result), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.Repair(ctx, cmd, req.RepairCostResolver)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedRepairResult(req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketDisjointItem:
		parsed, err := DecodeDisjointItemRequest(req.Body)
		cmd := NewDisjointCommand(req, parsed)
		return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
	case dnfenum.CmdPacketUseStackable:
		parsed, err := DecodeUseStackableRequest(req.Body)
		cmd := NewUseStackableCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, cmd.String(), err), nil
		}
		if err := checkUseStackableListType(cmd.SourceListType); err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.UseStackable(ctx, cmd, req.PremiumContractResolver, req.RandomRewardItemResolver)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedUseStackableResult(req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketUseStackableAction:
		parsed, err := DecodeUseStackableActionRequest(req.Body)
		cmd := NewUseStackableActionCommand(req, parsed)
		if err != nil {
			return blockedParsedResult(cmd.Operation, parsed.String(), err), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.UnlockDamageFont(ctx, cmd, req.DamageFontResolver, req.DamageFontNow)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedDamageFontUnlockResult(req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketSelectDamageFontSkin:
		parsed, err := DecodeSelectDamageFontRequest(req.Body)
		cmd := NewSelectDamageFontCommand(req, parsed)
		if err != nil {
			return alignedcmd.Result{
				Handled:         true,
				ResponseAllowed: true,
				Operation:       cmd.Operation,
				Reason:          fmt.Sprintf("select damage font parse failed: %v", err),
				UpperResponses: []alignedcmd.UpperResponse{
					class1Response(req.Opcode, buildSelectDamageFontErrorAck(damageFontSelectionErrorUnavailable)),
				},
			}, nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.SelectDamageFont(ctx, cmd, req.DamageFontNow)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedDamageFontSelectionResult(req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketUpgradeItem:
		parsed, err := DecodeUpgradeItemRequest(req.Body)
		cmd := NewUpgradeCommand(req, parsed)
		if err != nil {
			return alignedcmd.Result{
				Handled:         true,
				ResponseAllowed: true,
				Operation:       cmd.Operation,
				Reason:          fmt.Sprintf("upgrade_item parse failed: %v; returned current-EXE op50 error ACK", err),
				UpperResponses: []alignedcmd.UpperResponse{
					class1Response(req.Opcode, buildUpgradeItemErrorAck(upgradeErrorInvalidTarget)),
				},
			}, nil
		}
		if req.UpgradeTicketResolver != nil && !upgradeMaterialSlotIsAccountShared(cmd) {
			ticketOwner, ownerErr := NewOwner(req.Repositories)
			if ownerErr != nil {
				return ownerBlockedResult(cmd, ownerErr), nil
			}
			ticketResult, ticketErr := ticketOwner.UpgradeTicket(ctx, cmd, req.UpgradeTicketResolver)
			if ticketErr != nil {
				return ownerBlockedResult(cmd, ticketErr), nil
			}
			if ticketResult.TicketResolved {
				return ownerAppliedUpgradeTicketResult(ctx, req.Repositories.Settings, req.Opcode, cmd, ticketResult), nil
			}
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		if req.UpgradePolicyResolver == nil {
			cmd.UpgradePolicyError = "upgrade policy resolver missing"
		} else if policy, policyErr := req.UpgradePolicyResolver(cmd.Mode, int(upgradeLevelOfSlot(req.Repositories, ctx, cmd))); policyErr == nil {
			cmd.UpgradeSuccessWeight = policy.SuccessWeight
			cmd.UpgradePenaltyType = policy.PenaltyType
			cmd.UpgradeMaterialItemID = policy.MaterialItemID
			cmd.UpgradeMaterialCount = policy.MaterialCount
			cmd.UpgradeDestroyBonusItemID = policy.DestroyBonusItemID
			cmd.UpgradeDestroyBonusCount = policy.DestroyBonusCount
			cmd.UpgradeNoticeLevel = policy.NoticeLevel
		} else {
			cmd.UpgradePolicyError = policyErr.Error()
		}
		result, err := owner.Upgrade(ctx, cmd)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedUpgradeResult(ctx, req.Repositories.Settings, req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketEnchantByBead:
		parsed, err := DecodeEnchantByBeadRequest(req.Body)
		cmd := NewEnchantCommand(req, parsed)
		if err != nil {
			return alignedcmd.Result{
				Handled:         true,
				ResponseAllowed: true,
				Operation:       cmd.Operation,
				Reason:          fmt.Sprintf("enchant_by_bead parse failed: %v; returned current-EXE 0x0110 error ACK", err),
				UpperResponses: []alignedcmd.UpperResponse{
					class1Response(req.Opcode, buildEnchantByBeadErrorAck(enchantErrorInvalidBead)),
				},
			}, nil
		}
		if req.EnchantBeadResolver == nil {
			return alignedcmd.Result{
				Handled:         true,
				ResponseAllowed: false,
				Operation:       cmd.Operation,
				Reason:          fmt.Sprintf("enchant_by_bead blocked: runtime-PVF resolver unavailable; refusing to trust request metadata: %v", ErrEnchantResolverRequired),
			}, nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.Enchant(ctx, cmd, req.EnchantBeadResolver)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedEnchantResult(ctx, req.Repositories.Settings, req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketPurifyItem:
		parsed, err := DecodePurifyItemRequest(req.Body)
		cmd := NewPurifyItemCommand(req, parsed)
		if err != nil {
			return alignedcmd.Result{
				Handled:         true,
				ResponseAllowed: true,
				Operation:       cmd.Operation,
				Reason:          fmt.Sprintf("purify_item parse failed: %v; returned current-EXE op204 fixed failure ACK", err),
				UpperResponses: []alignedcmd.UpperResponse{
					class1Response(req.Opcode, buildPurifyItemErrorAck()),
				},
			}, nil
		}
		if req.AmplifyItemResolver == nil {
			return alignedcmd.Result{
				Handled:         true,
				ResponseAllowed: true,
				Operation:       cmd.Operation,
				Reason:          fmt.Sprintf("purify_item blocked: runtime-PVF resolver unavailable: %v", ErrAmplifyItemResolverRequired),
				UpperResponses: []alignedcmd.UpperResponse{
					class1Response(req.Opcode, buildPurifyItemErrorAck()),
				},
			}, nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.PurifyAmplifyItem(ctx, cmd, req.AmplifyItemResolver)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedPurifyItemResult(req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketInvestItemAmplifyOption:
		parsed, err := DecodeInvestItemAmplifyOptionRequest(req.Body)
		cmd := NewInvestItemAmplifyOptionCommand(req, parsed)
		if err != nil {
			return alignedcmd.Result{
				Handled:         true,
				ResponseAllowed: true,
				Operation:       cmd.Operation,
				Reason:          fmt.Sprintf("invest_item_amplify_option parse failed: %v; returned current-EXE op205 error ACK", err),
				UpperResponses: []alignedcmd.UpperResponse{
					class1Response(req.Opcode, buildInvestItemAmplifyOptionErrorAck(investAmplifyErrorInvalid)),
				},
			}, nil
		}
		if req.AmplifyItemResolver == nil {
			return alignedcmd.Result{
				Handled:         true,
				ResponseAllowed: true,
				Operation:       cmd.Operation,
				Reason:          fmt.Sprintf("invest_item_amplify_option blocked: runtime-PVF resolver unavailable: %v", ErrAmplifyItemResolverRequired),
				UpperResponses: []alignedcmd.UpperResponse{
					class1Response(req.Opcode, buildInvestItemAmplifyOptionErrorAck(investAmplifyErrorInvalid)),
				},
			}, nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		result, err := owner.InvestAmplifyOption(ctx, cmd, req.AmplifyItemResolver)
		if err != nil {
			return ownerBlockedResult(cmd, err), nil
		}
		return ownerAppliedInvestAmplifyOptionResult(req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketUnsealRandomOption:
		parsed, err := DecodeUnsealRandomOptionRequest(req.Body)
		cmd := NewUnsealRandomOptionCommand(req, parsed)
		if err != nil || req.RandomOptionResolver == nil {
			return randomOptionFailureResult(req.Opcode, cmd, fmt.Errorf("parse=%v resolver_available=%t", err, req.RandomOptionResolver != nil)), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return randomOptionFailureResult(req.Opcode, cmd, err), nil
		}
		result, err := owner.UnsealRandomOption(ctx, cmd, req.RandomOptionResolver)
		if err != nil {
			return randomOptionFailureResult(req.Opcode, cmd, err), nil
		}
		return ownerAppliedRandomOptionResult(req.Opcode, cmd, result), nil
	case dnfenum.CmdPacketChangeRandomOption:
		parsed, err := DecodeChangeRandomOptionRequest(req.Body)
		cmd := NewChangeRandomOptionCommand(req, parsed)
		if err != nil || req.RandomOptionResolver == nil {
			return randomOptionFailureResult(req.Opcode, cmd, fmt.Errorf("parse=%v resolver_available=%t", err, req.RandomOptionResolver != nil)), nil
		}
		owner, err := NewOwner(req.Repositories)
		if err != nil {
			return randomOptionFailureResult(req.Opcode, cmd, err), nil
		}
		result, err := owner.ChangeRandomOption(ctx, cmd, req.RandomOptionResolver)
		if err != nil {
			return randomOptionFailureResult(req.Opcode, cmd, err), nil
		}
		return ownerAppliedRandomOptionResult(req.Opcode, cmd, result), nil
	default:
		return alignedcmd.Result{
			Handled:         false,
			ResponseAllowed: false,
			Operation:       "unregistered",
			Reason:          fmt.Sprintf("inventory 模块未登记 opcode %d", req.Opcode),
		}, nil
	}
}

func randomOptionFailureResult(opcode uint16, cmd Command, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("random-option request rejected: %s error=%v", cmd.String(), err),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildRandomOptionStatusAck(false)),
		},
	}
}

func ownerAppliedRandomOptionResult(opcode uint16, cmd Command, result RandomOptionMutationResult) alignedcmd.Result {
	if !result.Success {
		return randomOptionFailureResult(opcode, cmd, fmt.Errorf("owner rejected target item=%d", result.TargetItemID))
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("random-option owner applied: %s item=%d options=%d gold_cost=%d", cmd.String(), result.TargetItemID, len(result.Options), result.GoldCost),
		UpperResponses: []alignedcmd.UpperResponse{
			class0Response(msgItemListUpdate, buildCommonItemListUpdateBody(listTypeMain, []commonItemListEntry{{slot: result.TargetSlotIndex, stack: result.UpdatedStack}})),
			class1Response(opcode, buildRandomOptionStatusAck(true)),
		},
		PostActions: []alignedcmd.PostAction{alignedcmd.PostActionRefreshSelectedItemContainers},
	}
}

func ownerAppliedPurifyItemResult(opcode uint16, cmd Command, result AmplifyMutationResult) alignedcmd.Result {
	if !result.Success {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: true,
			Operation:       cmd.Operation,
			Reason:          fmt.Sprintf("inventory owner rejected: %s internal_error=%d; returned current-EXE op204 fixed failure ACK", cmd.String(), result.ErrorCode),
			UpperResponses: []alignedcmd.UpperResponse{
				class1Response(opcode, buildPurifyItemErrorAck()),
			},
		}
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"inventory owner applied: %s mode=%s amplify=(%d,%d) materialLeft=%d; returned self-contained current-EXE op204 ACK without unproven item-list replay",
			cmd.String(), result.Mode, result.AmplifyType, result.AmplifyValue, result.MaterialRemainingCount,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildPurifyItemSuccessAck(result)),
		},
	}
}

func ownerAppliedInvestAmplifyOptionResult(opcode uint16, cmd Command, result AmplifyMutationResult) alignedcmd.Result {
	if !result.Success {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: true,
			Operation:       cmd.Operation,
			Reason:          fmt.Sprintf("inventory owner rejected: %s error=%d; returned current-EXE op205 error ACK", cmd.String(), result.ErrorCode),
			UpperResponses: []alignedcmd.UpperResponse{
				class1Response(opcode, buildInvestItemAmplifyOptionErrorAck(result.ErrorCode)),
			},
		}
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"inventory owner applied: %s mode=%s amplify=(%d,%d) level=%d materialLeft=%d; returned self-contained current-EXE op205 ACK without item-list replay",
			cmd.String(), result.Mode, result.AmplifyType, result.AmplifyValue, result.AmplifyLevel, result.MaterialRemainingCount,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildInvestItemAmplifyOptionSuccessAck(result)),
		},
	}
}

// upgradeLevelOfSlot reads the current upgrade level of the target equipment
// from the inventory record so the policy resolver can look up the correct
// PVF table row before the atomic transaction begins.
func upgradeLevelOfSlot(repos dnfrepo.Group, ctx context.Context, cmd Command) byte {
	if repos.Inventory == nil || cmd.SelectedCharacterID == 0 {
		return 0
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	record, found, err := repos.Inventory.Load(ctx, characterID)
	if err != nil || !found || record.Slots == nil {
		return 0
	}
	key := slotKey(listTypeMain, cmd.TargetSlotIndex)
	stack, ok := record.Slots[key]
	if !ok || stack.ItemID != int64(cmd.TargetItemTemplateID) {
		return 0
	}
	return upgradeLevelOf(stack)
}

func upgradeMaterialSlotIsAccountShared(cmd Command) bool {
	if cmd.MaterialSlotIndex < 0 {
		return false
	}
	return dnfrepo.IsAccountSharedInventorySlot(listTypeMain, cmd.MaterialSlotIndex)
}

func ownerAppliedUpgradeResult(ctx context.Context, settings dnfrepo.SettingsRepository, opcode uint16, cmd Command, result UpgradeResult) alignedcmd.Result {
	if !result.Success {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: true,
			Operation:       cmd.Operation,
			Reason: fmt.Sprintf(
				"inventory owner rejected: %s error=%d; returned current-EXE op50 error ACK",
				cmd.String(),
				result.ErrorCode,
			),
			UpperResponses: []alignedcmd.UpperResponse{
				class1Response(opcode, buildUpgradeItemErrorAck(result.ErrorCode)),
			},
		}
	}

	responses := []alignedcmd.UpperResponse{
		class1Response(opcode, buildUpgradeItemSuccessAck(result)),
	}
	if entries := upgradeItemUpdateEntries(result); len(entries) > 0 {
		responses = append(responses, class0Response(msgItemListUpdate, buildCommonItemListUpdateBody(listTypeMain, entries)))
	}
	// NOTI 0x0056 upgrade announcement when level meets notice threshold.
	if cmd.UpgradeNoticeLevel >= 0 {
		noticeLevel := result.NewLevel
		if !result.UpgradeSucceeded {
			noticeLevel = result.OldLevel
		}
		if int(noticeLevel) >= cmd.UpgradeNoticeLevel {
			responses = append(responses, class0Response(msgUpgradeNotice, buildUpgradeNoticeBody(
				result.UpgradeSucceeded, cmd.SelectedCharacterID, result.TargetItemTemplateID, noticeLevel)))
		}
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"inventory owner applied: %s mode=%s level=%d->%d materialLeft=%d goldCost=%d changed=%t resultCode=%d; opened 0x0050 ACK + 0x000D main-list refresh in 86JP order",
			cmd.String(),
			result.Mode,
			result.OldLevel,
			result.NewLevel,
			result.MaterialRemainingStackCount,
			result.GoldCost,
			result.Changed,
			result.ResultCode,
		),
		UpperResponses: responses,
	}
}

func upgradeItemUpdateEntries(result UpgradeResult) []commonItemListEntry {
	updates := make(map[int16]dnfrepo.ItemStack, 3)
	if result.TargetSlotIndex >= 0 {
		updates[result.TargetSlotIndex] = result.TargetUpdatedStack
	}
	if result.MaterialUpdated && result.MaterialSlotIndex >= 0 {
		updates[result.MaterialSlotIndex] = result.MaterialUpdatedStack
	}
	if result.DestroyBonusItemID > 0 && result.DestroyBonusSlot >= 0 {
		if stack, ok := result.MainRefresh[slotKey(listTypeMain, result.DestroyBonusSlot)]; ok {
			updates[result.DestroyBonusSlot] = stack
		} else {
			updates[result.DestroyBonusSlot] = dnfrepo.ItemStack{
				ItemID: int64(result.DestroyBonusItemID),
				Count:  int64(result.DestroyBonusCount),
				Extra:  map[string]string{"source": "upgrade_destroy_bonus"},
			}
		}
	}
	entries := make([]commonItemListEntry, 0, len(updates))
	for slot, stack := range updates {
		entries = append(entries, commonItemListEntry{slot: slot, stack: stack})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].slot < entries[j].slot })
	return entries
}

func ownerAppliedEnchantResult(ctx context.Context, settings dnfrepo.SettingsRepository, opcode uint16, cmd Command, result EnchantResult) alignedcmd.Result {
	if !result.Success {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: true,
			Operation:       cmd.Operation,
			Reason: fmt.Sprintf(
				"inventory owner rejected: %s error=%d; returned current-EXE 0x0110 error ACK",
				cmd.String(),
				result.ErrorCode,
			),
			UpperResponses: []alignedcmd.UpperResponse{
				class1Response(opcode, buildEnchantByBeadErrorAck(result.ErrorCode)),
			},
		}
	}

	state, found, err := dnfrepo.LoadCharacterContainerState(ctx, settings, result.CharacterID)
	if err != nil || !found {
		state = dnfrepo.CharacterContainerState{}
	}
	if result.TargetListType == listTypePet {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: true,
			Operation:       cmd.Operation,
			Reason: fmt.Sprintf(
				"inventory owner applied pet enchant: %s card=%d upgrade=%d beadLeft=%d changed=%t; sent target list-7 and bead list-0 op14 rows before current-EXE 0x0110 ACK",
				cmd.String(),
				result.CardItemID,
				result.EnchantUpgradeCount,
				result.BeadRemainingCount,
				result.Changed,
			),
			UpperResponses: []alignedcmd.UpperResponse{
				class0Response(msgItemListUpdate, buildPetItemListUpdateBody([]commonItemListEntry{{slot: result.TargetSlotIndex, stack: result.TargetUpdatedStack}})),
				class0Response(msgItemListUpdate, buildCommonItemListUpdateBody(listTypeMain, []commonItemListEntry{{slot: result.BeadSlotIndex, stack: result.BeadUpdatedStack}})),
				class1Response(opcode, buildEnchantByBeadSuccessAck(result)),
			},
		}
	}
	// The current EXE's 0x0110 success handler resolves the target slot to an
	// item pointer and publishes that pointer to the result popup. A generic
	// selected-container refresh after the ACK destroys and recreates the
	// item objects while the popup still owns the pointer, so the popup can
	// render an unrelated item that later reuses the same allocation.
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"inventory owner applied: %s card=%d upgrade=%d beadLeft=%d changed=%t; opened one 0x000D main-list refresh then 0x0110 ACK without a pointer-invalidating duplicate refresh",
			cmd.String(),
			result.CardItemID,
			result.EnchantUpgradeCount,
			result.BeadRemainingCount,
			result.Changed,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class0Response(msgItemListRefresh, buildCommonItemListRefreshBodyWithState(listTypeMain, result.MainRefresh, state)),
			class1Response(opcode, buildEnchantByBeadSuccessAck(result)),
		},
	}
}

func ownerAppliedUpgradeTicketResult(ctx context.Context, settings dnfrepo.SettingsRepository, opcode uint16, cmd Command, result UpgradeTicketResult) alignedcmd.Result {
	if !result.Success {
		return alignedcmd.Result{
			Handled:         true,
			ResponseAllowed: true,
			Operation:       cmd.Operation,
			Reason: fmt.Sprintf(
				"inventory owner rejected: %s error=%d; returned 0x0050 error ACK",
				cmd.String(),
				result.ErrorCode,
			),
			UpperResponses: []alignedcmd.UpperResponse{
				class1Response(opcode, buildUpgradeTicketErrorAck(result.ErrorCode)),
			},
		}
	}

	responses := []alignedcmd.UpperResponse{
		class1Response(opcode, buildUpgradeTicketSuccessAck(result)),
	}
	if entries := upgradeTicketUpdateEntries(result); len(entries) > 0 {
		responses = append(responses, class0Response(msgItemListUpdate, buildCommonItemListUpdateBody(listTypeMain, entries)))
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"inventory owner applied: %s mode=%s level=%d->%d roll=%t ticketLeft=%d changed=%t; opened 0x0050 ACK + 0x000D main-list refresh in 86JP order",
			cmd.String(),
			result.Mode,
			result.OldLevel,
			result.NewLevel,
			result.UpgradeSucceeded,
			result.MaterialRemainingStackCount,
			result.Changed,
		),
		UpperResponses: responses,
	}
}

func upgradeTicketUpdateEntries(result UpgradeTicketResult) []commonItemListEntry {
	updates := make(map[int16]dnfrepo.ItemStack, 2)
	if result.TargetSlotIndex >= 0 {
		updates[result.TargetSlotIndex] = result.TargetUpdatedStack
	}
	if result.MaterialUpdated && result.MaterialSlotIndex >= 0 {
		updates[result.MaterialSlotIndex] = result.MaterialUpdatedStack
	}
	entries := make([]commonItemListEntry, 0, len(updates))
	for slot, stack := range updates {
		entries = append(entries, commonItemListEntry{slot: slot, stack: stack})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].slot < entries[j].slot })
	return entries
}

func ownerAppliedMoveResult(ctx context.Context, settings dnfrepo.SettingsRepository, opcode uint16, cmd Command, result MoveResult) alignedcmd.Result {
	moveValue := clampInt32(result.MoveCount)
	if result.Mode == "noop" {
		moveValue = 0
	}
	responses := []alignedcmd.UpperResponse{
		class1Response(opcode, buildMoveItemspaceSuccessAck(cmd, moveValue)),
	}
	itemSlotRefreshes := make([]alignedcmd.ItemSlotRefresh, 0, 2)
	crossContainerStack := result.Changed &&
		result.Mode == "stack" &&
		isMainPersonalCargoCrossList(cmd.SourceListType, cmd.DestinationListType)
	if crossContainerStack {
		itemSlotRefreshes = append(itemSlotRefreshes,
			alignedcmd.ItemSlotRefresh{ListType: cmd.SourceListType, SlotIndex: cmd.SourceSlotIndex},
			alignedcmd.ItemSlotRefresh{ListType: cmd.DestinationListType, SlotIndex: cmd.DestinationSlotIndex},
		)
	} else {
		for _, listType := range result.RefreshListTypes {
			slots, ok := result.Refresh[listType]
			if !ok {
				continue
			}
			state, found, err := dnfrepo.LoadCharacterContainerState(ctx, settings, result.CharacterID)
			if err != nil || !found {
				state = dnfrepo.CharacterContainerState{}
			}
			responses = append(responses, class0Response(
				msgItemListRefresh,
				buildCommonItemListRefreshBodyWithState(listType, slots, state),
			))
		}
	}
	postActions := make([]alignedcmd.PostAction, 0, 1)
	if result.Changed && (cmd.SourceListType == listTypeAccountCargo || cmd.DestinationListType == listTypeAccountCargo) {
		// List 12 is account-owned.  The bridge rebuilds its current-EXE op13
		// snapshot from account metadata + AccountInventory after the op19 ACK;
		// it must not use the per-character common refresh path.
		postActions = append(postActions, alignedcmd.PostActionRefreshSelectedAccountCargo)
	}
	if result.Changed && (cmd.SourceListType == listTypePet || cmd.DestinationListType == listTypePet) {
		postActions = append(postActions, alignedcmd.PostActionRefreshSelectedItemContainers)
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"inventory owner applied: %s moveMode=%s moveCount=%d changed=%t refreshLists=%v incrementalSlots=%v; opened 0x0013 ACK then authoritative refresh for changed counts",
			cmd.String(),
			result.Mode,
			result.MoveCount,
			result.Changed,
			result.RefreshListTypes,
			itemSlotRefreshes,
		),
		UpperResponses:    responses,
		ItemSlotRefreshes: itemSlotRefreshes,
		PostActions:       postActions,
	}
}

func ownerAppliedSellResult(opcode uint16, cmd Command, result SellResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"inventory owner applied: %s sold=%d goldDelta=%d updatedGold=%d changed=%t; opened 0x0016 response in C# order",
			cmd.String(),
			result.Sold.AppliedCount,
			result.GoldDelta,
			result.UpdatedGold,
			result.Changed,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildSellItemAck(result.Sold, result.UpdatedGold)),
		},
	}
}

func ownerAppliedSortResult(ctx context.Context, settings dnfrepo.SettingsRepository, opcode uint16, cmd Command, result SortResult) alignedcmd.Result {
	refreshBody, ok := buildSortRefreshBody(ctx, settings, result)
	if !ok {
		return ownerAppliedResult(cmd, fmt.Sprintf("sortMode=%s moved=%d range=%d-%d changed=%t; list=%d needs dedicated 0x000D body", result.Mode, result.MovedCount, result.StartSlot, result.EndSlot, result.Changed, result.ListType))
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"inventory owner applied: %s sortMode=%s moved=%d range=%d-%d changed=%t; opened 0x0014 ACK + 0x000D list refresh in C# order",
			cmd.String(),
			result.Mode,
			result.MovedCount,
			result.StartSlot,
			result.EndSlot,
			result.Changed,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildSortItemAck(result.ListType)),
			class0Response(msgItemListRefresh, refreshBody),
		},
	}
}

func ownerAppliedPetEquipmentMoveResult(opcode uint16, cmd Command, result dnfequip.MoveResult) alignedcmd.Result {
	moveValue := cmd.MoveCount
	if result.Mode == "noop" {
		moveValue = 0
	}
	postActions := []alignedcmd.PostAction(nil)
	if result.Changed {
		postActions = append(postActions, alignedcmd.PostActionRefreshSelectedItemContainers)
		if result.DestinationSlotIndex == 26 {
			postActions = append(postActions,
				alignedcmd.PostActionRefreshSelectedCreatureState,
				alignedcmd.PostActionRefreshSelectedActorAppearance,
			)
		}
	}
	// Current NoPack op19 keeps positional endpoint semantics (A is the
	// inventory/pet side, B is the worn side), but the owner may redirect A to
	// the next real empty slot while resolving an unequip against stale client
	// inventory state. Report the committed slots while retaining the raw
	// actor-worn endpoint (3 or 17): sub_1D2DE80 has distinct native branches
	// for those endpoint values.
	ackCommand := committedEquipmentMoveAckCommand(cmd, result)
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"equipment owner applied current-EXE pet move: %s equipMove=%s durableSrc=(%d,%d) durableDst=(%d,%d) item=%d swap=%d changed=%t; opened exact op19 ACK then authoritative list7 refresh, plus op105/appearance when target26 changed",
			cmd.String(),
			result.Mode,
			result.SourceListType,
			result.SourceSlotIndex,
			result.DestinationListType,
			result.DestinationSlotIndex,
			result.ItemID,
			result.SwappedItemID,
			result.Changed,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildMoveItemspaceSuccessAck(ackCommand, moveValue)),
		},
		PostActions: postActions,
	}
}

func ownerAppliedEquipmentMoveResult(opcode uint16, cmd Command, result dnfequip.MoveResult) alignedcmd.Result {
	moveValue := cmd.MoveCount
	if result.Mode == "noop" {
		moveValue = 0
	}
	postActions := []alignedcmd.PostAction(nil)
	if result.Changed {
		postActions = append(postActions,
			alignedcmd.PostActionRefreshSelectedItemContainers,
			alignedcmd.PostActionRefreshSelectedActorAppearance,
		)
	}
	// Current NoPack op19 keeps positional endpoint semantics (A is the
	// inventory/avatar/cargo side, B is the worn side). The equipment owner
	// preserves that order in MoveResult while replacing A with the actual
	// committed empty slot when an unequip destination was stale. Keep the
	// request's raw actor-worn endpoint because current NoPack distinguishes
	// list 3 from list 17 even though both share the durable equipment owner.
	ackCommand := committedEquipmentMoveAckCommand(cmd, result)
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"equipment owner applied endpoint move: %s equipMove=%s requestA=(%d,%d) requestB=(%d,%d) finalA=(%d,%d) finalB=(%d,%d) item=%d swap=%d changed=%t; opened current-EXE op19 ACK then authoritative item containers and selected actor mode0 appearance refresh",
			cmd.String(),
			result.Mode,
			cmd.SourceListType,
			cmd.SourceSlotIndex,
			cmd.DestinationListType,
			cmd.DestinationSlotIndex,
			result.SourceListType,
			result.SourceSlotIndex,
			result.DestinationListType,
			result.DestinationSlotIndex,
			result.ItemID,
			result.SwappedItemID,
			result.Changed,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildMoveItemspaceSuccessAck(ackCommand, moveValue)),
		},
		PostActions: postActions,
	}
}

func isProvenPetEquipmentMove(cmd Command) bool {
	if cmd.SourceListType != listTypePet || !isActorWornEndpoint(cmd.DestinationListType) {
		return false
	}
	if cmd.DestinationSlotIndex == 26 {
		return cmd.SourceSlotIndex >= 0 && cmd.SourceSlotIndex <= 139
	}
	return cmd.DestinationSlotIndex >= 27 && cmd.DestinationSlotIndex <= 29 &&
		cmd.SourceSlotIndex >= 140 && cmd.SourceSlotIndex <= 188
}

func isActorWornEndpoint(listType byte) bool {
	return listType == listTypeEquipment || listType == listTypeActorWornAlt
}

func petMoveTouchesWornCreatureSlot(cmd Command, result dnfequip.MoveResult) bool {
	return isWornCreatureSlot(cmd.SourceListType, cmd.SourceSlotIndex) ||
		isWornCreatureSlot(cmd.DestinationListType, cmd.DestinationSlotIndex) ||
		isWornCreatureSlot(result.SourceListType, result.SourceSlotIndex) ||
		isWornCreatureSlot(result.DestinationListType, result.DestinationSlotIndex)
}

func isWornCreatureSlot(listType byte, slotIndex int16) bool {
	return isActorWornEndpoint(listType) && slotIndex == 26
}

func committedEquipmentMoveAckCommand(cmd Command, result dnfequip.MoveResult) Command {
	ack := cmd
	ack.SourceListType = result.SourceListType
	ack.SourceSlotIndex = result.SourceSlotIndex
	ack.DestinationListType = result.DestinationListType
	ack.DestinationSlotIndex = result.DestinationSlotIndex
	if isActorWornEndpoint(cmd.SourceListType) && isActorWornEndpoint(result.SourceListType) {
		ack.SourceListType = cmd.SourceListType
	}
	if isActorWornEndpoint(cmd.DestinationListType) && isActorWornEndpoint(result.DestinationListType) {
		ack.DestinationListType = cmd.DestinationListType
	}
	return ack
}

func validateProvenPetEquipmentMove(cmd Command) error {
	if cmd.MoveCount != 1 || cmd.DestinationStack != 0 || cmd.TrailingState0 != 0 || cmd.TrailingState1 != 0 {
		return fmt.Errorf("invalid current-EXE pet op19 constants: count=%d destination_stack=%d tail=(%d,%d)", cmd.MoveCount, cmd.DestinationStack, cmd.TrailingState0, cmd.TrailingState1)
	}
	if cmd.ActorIndex < -1 || cmd.ActorIndex > 2 {
		return fmt.Errorf("invalid current-EXE pet op19 actor index: %d", cmd.ActorIndex)
	}
	return nil
}

func equipmentMoveCommand(cmd Command) dnfequip.MoveCommand {
	return dnfequip.MoveCommand{
		SelectedCharacterID:      cmd.SelectedCharacterID,
		SourceListType:           cmd.SourceListType,
		SourceSlotIndex:          cmd.SourceSlotIndex,
		SourceInstanceValue:      cmd.SourceInstanceValue,
		MoveCount:                cmd.MoveCount,
		DestinationListType:      cmd.DestinationListType,
		DestinationSlotIndex:     cmd.DestinationSlotIndex,
		DestinationInstanceValue: cmd.DestinationInstance,
	}
}

func normalizedEquipmentMoveCommand(cmd Command) dnfequip.MoveCommand {
	ownerCommand := equipmentMoveCommand(cmd)
	if isActorWornEndpoint(ownerCommand.SourceListType) {
		ownerCommand.SourceListType = listTypeEquipment
	}
	if isActorWornEndpoint(ownerCommand.DestinationListType) {
		ownerCommand.DestinationListType = listTypeEquipment
	}
	return ownerCommand
}

func buildSortRefreshBody(ctx context.Context, settings dnfrepo.SettingsRepository, result SortResult) ([]byte, bool) {
	switch result.ListType {
	case listTypeMain, listTypePersonalCargo, listTypeAvatar:
		state, found, err := dnfrepo.LoadCharacterContainerState(ctx, settings, result.CharacterID)
		if err != nil || !found {
			state = dnfrepo.CharacterContainerState{}
		}
		return buildCommonItemListRefreshBodyWithState(result.ListType, result.Refresh, state), true
	case listTypePet:
		return buildPetItemListRefreshBody(result.Refresh), true
	default:
		return nil, false
	}
}

func ownerAppliedRepairResult(opcode uint16, cmd Command, result RepairResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"inventory owner applied: %s list=%d slot=%d durability=%d->%d cost=%d gold=%d changed=%t; opened 0x0017 response in C# order",
			cmd.String(),
			result.ListType,
			result.SlotIndex,
			result.OldDurability,
			result.NewDurability,
			result.Cost,
			result.UpdatedGold,
			result.Changed,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildRepairEquipmentAck(cmd.SourceListType, result.SlotIndex, result.UpdatedGold)),
		},
	}
}

func ownerAppliedRepairAllResult(opcode uint16, cmd Command, result dnfequip.RepairResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"equipment owner applied repair-all: %s repaired=%d cost=%d free_repair=%t gold=%d changed=%t; opened 0x0017 slot=0xFFFF response in 86JP order",
			cmd.String(),
			result.RepairedCount,
			result.Cost,
			result.FreeRepair,
			result.UpdatedGold,
			result.Changed,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildRepairEquipmentAck(cmd.SourceListType, -1, result.UpdatedGold)),
		},
	}
}

func ownerAppliedEquippedRepairResult(opcode uint16, cmd Command, result dnfequip.RepairResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"equipment owner applied: %s equipSlot=%d item=%d durability=%d->%d cost=%d free_repair=%t gold=%d changed=%t; opened 0x0017 response in C# order",
			cmd.String(),
			result.SlotIndex,
			result.ItemID,
			result.OldDurability,
			result.NewDurability,
			result.Cost,
			result.FreeRepair,
			result.UpdatedGold,
			result.Changed,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildRepairEquipmentAck(cmd.SourceListType, result.SlotIndex, result.UpdatedGold)),
		},
	}
}

func ownerAppliedUseStackableResult(opcode uint16, cmd Command, result UseStackableResult) alignedcmd.Result {
	responses := []alignedcmd.UpperResponse{
		class1Response(opcode, buildUseStackableSuccessAck(result)),
	}
	postActions := make([]alignedcmd.PostAction, 0, 1)
	itemSlotRefreshes := []alignedcmd.ItemSlotRefresh{{
		ListType:  result.ListType,
		SlotIndex: result.SlotIndex,
	}}
	for _, slot := range result.RandomRewardSlots {
		itemSlotRefreshes = append(itemSlotRefreshes, alignedcmd.ItemSlotRefresh{
			ListType:  listTypeMain,
			SlotIndex: slot,
		})
	}
	reason := fmt.Sprintf(
		"inventory owner applied: %s item=%d remaining=%d pvf_path=%q stackable_type=%q changed=%t; opened exact current-EXE class1/op44 success response + one-row op14 refresh",
		cmd.String(),
		result.ItemID,
		result.RemainingCount,
		result.PVFPath,
		result.StackableType,
		result.Changed,
	)
	if result.PremiumActivated {
		// Current EXE class0/op66 (sub_1D61460) reads u16 sub-op + u8 type +
		// raw[8] expiry; sub-op 2 is the activate/set path (86JP
		// PremiumService.ActivateAndNotify body matches it exactly). The 0x0312
		// service-data blob is old-client-only (current 786 = skill quick slot
		// sort) and is never pushed. Internal devil slot types 580..587 cannot
		// fit this body and are deliberately suppressed.
		if premium.CanNotifyActivation(result.PremiumType) {
			responses = append(responses, class0Response(premiumActivatedNotifyMsgID, premium.BuildActivatedBody(result.PremiumType, result.PremiumRemainingSeconds)))
		}
		reason = fmt.Sprintf(
			"inventory owner applied: %s contract item=%d remaining=%d premium_type=%d premium_remaining=%ds changed=%t; opened op44 ACK + NOTI 0x0042",
			cmd.String(),
			result.ItemID,
			result.RemainingCount,
			result.PremiumType,
			result.PremiumRemainingSeconds,
			result.Changed,
		)
		if result.PremiumType == premium.TypeCrystal {
			postActions = append(postActions, alignedcmd.PostActionRefreshCrystalContractState)
		}
	}
	if result.ReviveCoinWalletUpdated {
		itemSlotRefreshes = append(itemSlotRefreshes, alignedcmd.ItemSlotRefresh{
			ListType:  dnfrepo.MainInventoryListType,
			SlotIndex: dnfrevivecoin.WalletSlot,
		})
		reason = fmt.Sprintf(
			"inventory owner applied: %s revive-coin consumable item=%d remaining=%d wallet_total=%d changed=%t; atomic source decrement + slot1 wallet increment, then exact op44 ACK + two-row op14 refresh",
			cmd.String(),
			result.ItemID,
			result.RemainingCount,
			result.ReviveCoinWalletTotal,
			result.Changed,
		)
	}
	if result.RandomRewardItemID > 0 {
		reason = fmt.Sprintf(
			"inventory owner applied: %s random reward item=%d remaining=%d reward=%d slots=%v; opened exact op44 ACK then refreshed source and reward rows",
			cmd.String(),
			result.ItemID,
			result.RemainingCount,
			result.RandomRewardItemID,
			result.RandomRewardSlots,
		)
	}
	return alignedcmd.Result{
		Handled:           true,
		ResponseAllowed:   true,
		Operation:         cmd.Operation,
		Reason:            reason,
		UpperResponses:    responses,
		ItemSlotRefreshes: itemSlotRefreshes,
		PostActions:       postActions,
	}
}

func ownerAppliedDamageFontUnlockResult(opcode uint16, cmd Command, result DamageFontUnlockResult) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"damage-font owner applied: slot=%d item=%d font=%d expires_at=%d remaining=%d pvf_path=%q; op515 ACK then op14 slot refresh then NOTI 1239",
			result.SourceSlotIndex, result.ItemID, result.FontIndex, result.ExpiresAt, result.RemainingCount, result.PVFPath,
		),
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, buildUseStackableActionSuccessAck(result)),
		},
		ItemSlotRefreshes: []alignedcmd.ItemSlotRefresh{{
			ListType:  listTypeMain,
			SlotIndex: result.SourceSlotIndex,
		}},
		PostActions: []alignedcmd.PostAction{alignedcmd.PostActionRefreshSelectedDamageFontState},
	}
}

func ownerAppliedDamageFontSelectionResult(opcode uint16, cmd Command, result DamageFontSelectionResult) alignedcmd.Result {
	body := buildSelectDamageFontErrorAck(damageFontSelectionErrorUnavailable)
	reason := fmt.Sprintf("damage font %d is not owned or has expired; returned current-EXE error 17", result.FontIndex)
	if result.Success {
		body = buildSelectDamageFontSuccessAck(result.FontIndex)
		reason = fmt.Sprintf("selected damage font %d persisted; returned current-EXE op1288 success ACK", result.FontIndex)
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: true,
		Operation:       cmd.Operation,
		Reason:          reason,
		UpperResponses: []alignedcmd.UpperResponse{
			class1Response(opcode, body),
		},
	}
}

func ownerBlockedResult(cmd Command, err error) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("已解析 %s：%s；inventory owner 未落库：%v；禁止回成功包", cmd.Operation, cmd.String(), err),
	}
}

func ownerAppliedResult(cmd Command, detail string) alignedcmd.Result {
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       cmd.Operation,
		Reason:          fmt.Sprintf("已写入 inventory owner：%s %s；缺少 mutation id 与当前 EXE 的 %s/背包刷新/USERINFO 顺序证据，暂不回成功包", cmd.String(), detail, ackEvidenceLabel(cmd.Operation)),
	}
}

func ownerAppliedDeleteResult(opcode uint16, cmd Command, result DeleteResult) alignedcmd.Result {
	responses := make([]alignedcmd.UpperResponse, 0, 1)
	if len(result.Removed) > 0 {
		responses = append(responses, class1Response(opcode, buildDeleteItemAck(result.Removed)))
	}
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: len(responses) > 0,
		Operation:       cmd.Operation,
		Reason: fmt.Sprintf(
			"inventory owner applied: %s removed=%d changed=%t; opened 0x0012 response in C# order",
			cmd.String(),
			len(result.Removed),
			result.Changed,
		),
		UpperResponses: responses,
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
	case "delete_item":
		return "0x0012"
	case "sell_item":
		return "0x0016"
	case "move_itemspace":
		return "0x0013"
	case "sort_item":
		return "0x0014"
	case "repair_equipment":
		return "0x0017"
	default:
		return "对应 ACK"
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
		Reason:          fmt.Sprintf("已解析 %s：%s；资产写路径需接入 durable owner、事务、幂等和当前 EXE S2C 刷新顺序后才能回包", operation, summary),
	}
}
