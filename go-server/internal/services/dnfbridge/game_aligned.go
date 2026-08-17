// 本文件把已对齐的 DNF 旧客户端命令转交给模块化 handler。
// bridge 只负责分流和发包，不在这里补业务状态或伪造成功结果。
package dnfbridge

import (
	"context"
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/avatartitle"
	"longheng.io/server/internal/modules/dnf/cargo"
	"longheng.io/server/internal/modules/dnf/character"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfequip "longheng.io/server/internal/modules/dnf/equip"
	"longheng.io/server/internal/modules/dnf/inventory"
	"longheng.io/server/internal/modules/dnf/itemlock"
	"longheng.io/server/internal/modules/dnf/mail"
	"longheng.io/server/internal/modules/dnf/packageitem"
	"longheng.io/server/internal/modules/dnf/party"
	"longheng.io/server/internal/modules/dnf/pet"
	"longheng.io/server/internal/modules/dnf/quest"
	"longheng.io/server/internal/modules/dnf/raid"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
	"longheng.io/server/internal/modules/dnf/skillcmd"
)

var errAlignedPostCommitProjectionDeferred = errors.New("aligned post-commit projection deferred")

var gameAlignedRegistry = alignedcmd.NewRegistry(
	avatartitle.NewHandler(),
	cargo.NewHandler(),
	character.NewHandler(),
	dungeoncmd.NewHandler(),
	inventory.NewHandler(),
	itemlock.NewHandler(),
	mail.NewHandler(),
	packageitem.NewHandler(),
	party.NewHandler(),
	pet.NewHandler(),
	quest.NewHandler(),
	raid.NewHandler(),
	skillcmd.NewHandler(),
)

// handleAlignedGameCommand 把协议号已对齐、但尚未落业务实现的命令归入模块边界。
// 当前只记录分流证据，不伪造成功 ACK；具体回包必须等对应模块按 EXE/日志补齐。
func (s *Service) handleAlignedGameCommand(session *gameSession, cmd byte, typ uint16, body []byte) (bool, error) {
	if typ == uint16(dnfenum.CmdPacketUseStackable) {
		if handled, err := s.handleCurrentExpertJobRecipeLearning(session, body); handled || err != nil {
			return handled, err
		}
	}
	if handled, err := s.handleOnlineItemTradeCommand(session, typ, body); handled || err != nil {
		return handled, err
	}
	if handled, err := s.handleOnlineRaidCommand(session, typ, body); handled || err != nil {
		return handled, err
	}
	if handled, err := s.handleOnlineSocialCommand(session, typ, body); handled || err != nil {
		return handled, err
	}
	if handled, err := s.handleOnlinePartyCommand(session, typ, body); handled || err != nil {
		return handled, err
	}
	repos, _ := s.repositoryGroup()
	accountID := s.accountIDForSession(session)
	if dnfenum.CmdPacket(typ) == dnfenum.CmdPacketMailboxOpen {
		s.reconcileCurrentJoustSettlementMailBeforeOpen(session)
	}
	var skillCatalog *dnfskill.Table
	var initialSkillLevels map[uint16]int
	var skillPointBaseline *dnfrepo.SkillPointState
	var equipmentPlacement alignedcmd.EquipmentPlacementValidator
	var petHatchResolver alignedcmd.PetHatchResolver
	if alignedSkillOwnerRulesRequired(dnfenum.CmdPacket(typ)) {
		skillCatalog, initialSkillLevels, skillPointBaseline = s.skillOwnerRules(context.Background(), repos, session.selectedCharacterID)
	}
	petHatchResolver, petCatalogErr := s.alignedPetHatchResolverForCommand(dnfenum.CmdPacket(typ))
	if petCatalogErr != nil {
		s.logPacketEvent("game-aligned-pet-hatch-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", petCatalogErr)
		s.logInfo("dnfbridge pet hatch resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", petCatalogErr)
	}
	enchantBeadResolver, enchantCatalogErr := s.alignedEnchantBeadResolverForCommand(dnfenum.CmdPacket(typ))
	if enchantCatalogErr != nil {
		s.logPacketEvent("game-aligned-enchant-bead-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", enchantCatalogErr)
		s.logInfo("dnfbridge enchant bead resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", enchantCatalogErr)
	}
	amplifyItemResolver, amplifyItemCatalogErr := s.alignedAmplifyItemResolverForCommand(dnfenum.CmdPacket(typ))
	if amplifyItemCatalogErr != nil {
		s.logPacketEvent("game-aligned-amplify-item-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", amplifyItemCatalogErr)
		s.logInfo("dnfbridge amplify item resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", amplifyItemCatalogErr)
	}
	randomOptionResolver, randomOptionCatalogErr := s.alignedRandomOptionResolverForCommand(dnfenum.CmdPacket(typ))
	if randomOptionCatalogErr != nil {
		s.logPacketEvent("game-aligned-random-option-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", randomOptionCatalogErr)
		s.logInfo("dnfbridge random option resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", randomOptionCatalogErr)
	}
	upgradeTicketResolver, upgradeTicketCatalogErr := s.alignedUpgradeTicketResolverForCommand(dnfenum.CmdPacket(typ))
	if upgradeTicketCatalogErr != nil {
		s.logPacketEvent("game-aligned-upgrade-ticket-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", upgradeTicketCatalogErr)
		s.logInfo("dnfbridge upgrade ticket resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", upgradeTicketCatalogErr)
	}
	upgradePolicyResolver := s.alignedUpgradePolicyResolver()
	magicBoxResolver, magicBoxRewardResolver, magicBoxCatalogErr := s.alignedMagicBoxResolversForCommand(dnfenum.CmdPacket(typ))
	if magicBoxCatalogErr != nil {
		s.logPacketEvent("game-aligned-magic-box-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", magicBoxCatalogErr)
		s.logInfo("dnfbridge magic box resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", magicBoxCatalogErr)
	}
	premiumContractResolver, premiumCatalogErr := s.alignedPremiumContractResolverForCommand(dnfenum.CmdPacket(typ))
	if premiumCatalogErr != nil {
		s.logPacketEvent("game-aligned-premium-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", premiumCatalogErr)
		s.logInfo("dnfbridge premium contract resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", premiumCatalogErr)
	}
	damageFontResolver, damageFontCatalogErr := s.alignedDamageFontResolverForCommand(dnfenum.CmdPacket(typ))
	if damageFontCatalogErr != nil {
		s.logPacketEvent("game-aligned-damage-font-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", damageFontCatalogErr)
		s.logInfo("dnfbridge damage-font resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", damageFontCatalogErr)
	}
	randomRewardItemResolver, randomRewardCatalogErr := s.alignedRandomRewardItemResolverForCommand(dnfenum.CmdPacket(typ))
	if randomRewardCatalogErr != nil {
		s.logPacketEvent("game-aligned-random-reward-item-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", randomRewardCatalogErr)
		s.logInfo("dnfbridge random-reward-item resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", randomRewardCatalogErr)
	}
	repairCostResolver, repairCostCatalogErr := s.alignedRepairCostResolverForCommand(dnfenum.CmdPacket(typ))
	if repairCostCatalogErr != nil {
		s.logPacketEvent("game-aligned-repair-cost-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", repairCostCatalogErr)
		s.logInfo("dnfbridge repair cost resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", repairCostCatalogErr)
	}
	mailboxItemResolver, mailboxCatalogErr := s.alignedMailboxItemResolverForCommand(dnfenum.CmdPacket(typ))
	if mailboxCatalogErr != nil {
		s.logPacketEvent("game-aligned-mailbox-item-resolver-unavailable",
			"conn_id", session.connID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", mailboxCatalogErr)
		s.logInfo("dnfbridge mailbox item resolver unavailable",
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"error", mailboxCatalogErr)
	}
	if dnfenum.CmdPacket(typ) == dnfenum.CmdPacketMoveItemspace {
		if currentPetMoveTouchesGrowthState(body) {
			if settleErr := s.settleCurrentPetGrowthClock(session, s.gameplayNow(), "current_exe_op19_before_pet_or_artifact_mutation"); settleErr != nil {
				s.logPacketEvent("game-pet-satiety-before-op19-deferred",
					"conn_id", session.connID,
					"selected_character_id", session.selectedCharacterID,
					"type", typ,
					"reason", "growth_clock_settlement_failed",
					"error", settleErr)
			}
		}
		placementContext, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		validator, placementErr := s.newCurrentEquipmentPlacementValidator(placementContext, accountID, session.selectedCharacterID, repos)
		cancel()
		if placementErr != nil {
			s.logPacketEvent("game-aligned-equipment-placement-validator-unavailable",
				"conn_id", session.connID,
				"account_id", accountID,
				"selected_character_id", session.selectedCharacterID,
				"type", typ,
				"error", placementErr)
		} else {
			equipmentPlacement = func(ctx context.Context, placement alignedcmd.EquipmentPlacementRequest) error {
				return validator.ValidateEquipmentPlacement(ctx, dnfequip.Placement{
					CharacterID:     placement.CharacterID,
					ItemID:          placement.ItemID,
					SourceListType:  placement.SourceListType,
					SourceSlotIndex: placement.SourceSlotIndex,
					TargetSlotIndex: placement.TargetSlotIndex,
				})
			}
		}
	}
	session.party.mu.Lock()
	result, ok, err := gameAlignedRegistry.Route(context.Background(), alignedcmd.Request{
		Command:                    cmd,
		CommandKnown:               true,
		Opcode:                     typ,
		Body:                       body,
		AccountID:                  accountID,
		SelectedCharacterID:        session.selectedCharacterID,
		Repositories:               repos,
		EquipmentPlacement:         equipmentPlacement,
		NameTagChecker:             func(itemID uint32) bool { return s.currentIsNameTagItem(itemID) },
		PetHatchResolver:           petHatchResolver,
		EnchantBeadResolver:        enchantBeadResolver,
		AmplifyItemResolver:        amplifyItemResolver,
		RandomOptionResolver:       randomOptionResolver,
		UpgradeTicketResolver:      upgradeTicketResolver,
		UpgradePolicyResolver:      upgradePolicyResolver,
		MagicBoxResolver:           magicBoxResolver,
		MagicBoxRewardItemResolver: magicBoxRewardResolver,
		RandomRewardItemResolver:   randomRewardItemResolver,
		PremiumContractResolver:    premiumContractResolver,
		DamageFontResolver:         damageFontResolver,
		DamageFontNow:              s.gameplayNow(),
		RepairCostResolver:         repairCostResolver,
		MailboxItemResolver:        mailboxItemResolver,
		SkillCatalog:               skillCatalog,
		InitialSkillLevels:         initialSkillLevels,
		SkillPointBaseline:         skillPointBaseline,
		Party:                      &session.party.state,
	})
	session.party.mu.Unlock()
	if err != nil || !ok {
		return ok, err
	}
	decision := result.Decision
	if !result.Handled {
		return false, nil
	}
	switch decision.Action {
	case alignedcmd.ActionPendingModule:
		if result.ResponseAllowed && len(result.UpperResponses) > 0 {
			s.logPacketEvent("game-aligned-command-real-response",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"account_id", accountID,
				"selected_character_id", session.selectedCharacterID,
				"cmd", cmd,
				"type", typ,
				"runtime_cmd_name", runtimeCmdPacketName(cmd, typ),
				"domain", string(decision.Domain),
				"support", string(decision.Support),
				"evidence", decision.Evidence,
				"note", decision.Note,
				"operation", result.Operation,
				"response_count", len(result.UpperResponses),
				"reason", result.Reason,
				"body_len", len(body))
			if err := s.sendAlignedUpperResponses(session, result); err != nil {
				return true, err
			}
			return true, nil
		}
		s.logPacketEvent("game-aligned-command-pending-module",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"cmd", cmd,
			"type", typ,
			"runtime_cmd_name", runtimeCmdPacketName(cmd, typ),
			"domain", string(decision.Domain),
			"support", string(decision.Support),
			"evidence", decision.Evidence,
			"note", decision.Note,
			"operation", result.Operation,
			"response_allowed", result.ResponseAllowed,
			"reason", result.Reason,
			"body_len", len(body))
		s.logInfo("dnfbridge aligned game command pending module implementation",
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"runtime_cmd_name", runtimeCmdPacketName(cmd, typ),
			"domain", string(decision.Domain),
			"support", string(decision.Support),
			"operation", result.Operation,
			"response_allowed", result.ResponseAllowed,
			"reason", result.Reason,
			"body_len", len(body))
		return true, nil
	case alignedcmd.ActionBlocked:
		s.logPacketEvent("game-legacy-migration-blocked-by-evidence",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"cmd", cmd,
			"type", typ,
			"runtime_cmd_name", runtimeCmdPacketName(cmd, typ),
			"domain", string(decision.Domain),
			"evidence", decision.Evidence,
			"operation", result.Operation,
			"reason", decision.Reason,
			"body_len", len(body))
		s.logInfo("dnfbridge legacy migration blocked by current exe evidence",
			"account_id", accountID,
			"selected_character_id", session.selectedCharacterID,
			"type", typ,
			"runtime_cmd_name", runtimeCmdPacketName(cmd, typ),
			"domain", string(decision.Domain),
			"operation", result.Operation,
			"body_len", len(body))
		return true, nil
	default:
		return false, nil
	}
}

func alignedSkillOwnerRulesRequired(opcode dnfenum.CmdPacket) bool {
	switch opcode {
	case dnfenum.CmdPacketChangeSkillslot, dnfenum.CmdPacketBuySkill, dnfenum.CmdPacketSkillInit:
		return true
	default:
		return false
	}
}

func (s *Service) skillOwnerRules(ctx context.Context, repos dnfrepo.Group, characterID uint16) (*dnfskill.Table, map[uint16]int, *dnfrepo.SkillPointState) {
	if s == nil || repos.Character == nil || characterID == 0 {
		return nil, nil, nil
	}
	record, ok, err := repos.Character.Load(ctx, fmt.Sprintf("%d", characterID))
	if err != nil || !ok {
		return nil, nil, nil
	}
	job, ok := characterJobByte(record)
	if !ok {
		return nil, nil, nil
	}
	entries, err := s.initialCharacterSkills(ctx, job)
	if err != nil {
		s.logInfo("dnfbridge skill rules unavailable", "character_id", characterID, "job", job, "error", err)
		return nil, nil, nil
	}
	points, err := s.initialSkillPoints(ctx, record.Level)
	if err != nil {
		s.logInfo("dnfbridge skill point baseline unavailable", "character_id", characterID, "level", record.Level, "error", err)
		return nil, nil, nil
	}
	s.initialSkillsMu.Lock()
	catalog := s.skillCatalog
	s.initialSkillsMu.Unlock()
	if catalog == nil {
		return nil, nil, nil
	}
	levels := make(map[uint16]int, len(entries))
	for _, entry := range entries {
		if entry.SkillID < 0 || entry.SkillID > 0xffff || entry.Level < 0 {
			continue
		}
		levels[uint16(entry.SkillID)] = entry.Level
	}
	baseline := &dnfrepo.SkillPointState{
		TotalSP: points.TotalSP, RemainingSP: points.TotalSP,
		TotalTP: points.TotalTP, RemainingTP: points.TotalTP,
		SyncedLevel: points.SyncedLevel,
	}
	return catalog, levels, baseline
}

// sendAlignedUpperResponses 发送模块返回的真实 upper 响应。
// 模块给出的 Body 已经是完整业务包体，不能再统一套成功字节。
func (s *Service) sendAlignedUpperResponses(session *gameSession, result alignedcmd.Result) error {
	for _, response := range result.UpperResponses {
		if err := s.sendGameUpperRawClassCodec(session, response.MsgID, response.Body, response.Classification, response.AllowCodec); err != nil {
			return err
		}
	}
	if err := s.sendSelectedIncrementalItemSlotRefreshes(session, result.Operation, result.ItemSlotRefreshes); err != nil {
		return err
	}
	refreshCombatPowerAffixes := false
	for _, action := range result.PostActions {
		if alignedPostActionTouchesCombatPower(action) {
			refreshCombatPowerAffixes = true
		}
		if err := s.sendAlignedPostAction(session, result.Operation, action); err != nil {
			if errors.Is(err, errAlignedPostCommitProjectionDeferred) {
				s.logWarn("dnfbridge deferred remaining aligned post-commit projections",
					"conn_id", session.connID,
					"operation", result.Operation,
					"post_action", action,
					"char_id", session.selectedCharacterID,
					"error", err)
				break
			}
			return err
		}
	}
	if refreshCombatPowerAffixes {
		if err := s.sendSelectedCurrentCombatPowerAffixes(
			session,
			"aligned_"+result.Operation+"_after_ack_and_worn_state_commit",
		); err != nil {
			// The business mutation and ACK already succeeded. A private Lua
			// projection failure is retryable on the next panel/town refresh and
			// must not turn the committed item move into a false client failure.
			s.logWarn("dnfbridge combat-power affix post-action deferred",
				"conn_id", session.connID,
				"operation", result.Operation,
				"char_id", session.selectedCharacterID,
				"error", err)
		}
	}
	if result.MailboxAlarmRecipientID != 0 {
		if err := s.sendMailboxAlarmToOnlineRecipient(result.MailboxAlarmRecipientID); err != nil {
			// The durable send and sender ACK have already succeeded. A stale
			// recipient connection must not turn that committed operation into a
			// false failure; their next op96 projects the same mailbox state.
			s.logWarn("dnfbridge mailbox online alarm deferred to next mailbox open",
				"recipient_character_id", result.MailboxAlarmRecipientID,
				"operation", result.Operation,
				"error", err)
		}
	}
	return nil
}

func alignedPostActionTouchesCombatPower(action alignedcmd.PostAction) bool {
	switch action {
	case alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
		alignedcmd.PostActionRefreshSelectedEquipmentSlots,
		alignedcmd.PostActionRefreshSelectedCreatureState:
		return true
	default:
		return false
	}
}

func (s *Service) sendAlignedPostAction(session *gameSession, operation string, action alignedcmd.PostAction) error {
	switch action {
	case alignedcmd.PostActionRefreshSelectedItemContainers:
		s.logPacketEvent("game-aligned-command-post-action-send",
			"conn_id", session.connID,
			"operation", operation,
			"post_action", action,
			"char_id", session.selectedCharacterID,
			"msg_id", uint16(dnfenum.CmdPacketLeaveParty),
			"classification", 0,
			"list_types", currentSelectedItemListTypes,
			"sequence", "durable_owner_then_class1_op19_ack_then_repository_backed_op13_list0_list1_list2")
		if err := s.sendSelectedCurrentContainerListsWithRefresh(
			session,
			"aligned_"+operation+"_after_ack",
			true,
		); err != nil {
			return fmt.Errorf("%w: refresh selected item containers: %v", errAlignedPostCommitProjectionDeferred, err)
		}
		return nil
	case alignedcmd.PostActionRefreshSelectedEquipmentSlots:
		s.logPacketEvent("game-aligned-command-post-action-send",
			"conn_id", session.connID,
			"operation", operation,
			"post_action", action,
			"char_id", session.selectedCharacterID,
			"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
			"classification", 0,
			"list_type", currentSocketListEquipment,
			"sequence", "unsafe_op14_list3_equipment_rows_suppressed",
			"evidence", "86jp_move_itemspace_sends_ack_sortlock_pet_specific_refresh_then_noti2_no_generic_list3")
		return nil
	case alignedcmd.PostActionRefreshSelectedAccountCargo:
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		defer cancel()
		body, source, count, ok := s.buildCurrentItemListBodyForSession(ctx, session, 12)
		if !ok {
			return fmt.Errorf("aligned post action %q could not build account cargo", action)
		}
		s.logPacketEvent("game-aligned-command-post-action-send",
			"conn_id", session.connID,
			"operation", operation,
			"post_action", action,
			"char_id", session.selectedCharacterID,
			"msg_id", uint16(dnfenum.CmdPacketLeaveParty),
			"classification", 0,
			"list_type", 12,
			"entry_count", count,
			"body_len", len(body),
			"body_source", source,
			"sequence", "durable_account_owner_then_class1_ack_then_current_exe_op13_list12")
		return s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), body)
	case alignedcmd.PostActionRefreshSelectedActorAppearance:
		// Full mode1 is reserved for rebuilding a new post-op24 actor generation;
		// using it per click recreates the actor and blocks the next move.
		if !currentTownSelfActorAppearanceReady(session) {
			s.logWarn("dnfbridge deferred selected actor mode0 appearance",
				"conn_id", session.connID,
				"operation", operation,
				"char_id", session.selectedCharacterID,
				"reason", "selected_town_actor_generation_or_native_owner_not_ready")
			return fmt.Errorf(
				"%w: selected town actor generation or native owner is not ready",
				errAlignedPostCommitProjectionDeferred,
			)
		}
		if err := s.sendSelectedActorMode0AppearanceRefresh(
			session,
			operation,
			"durable_owner_then_class1_ack_then_repository_backed_native_owner_mode0",
		); err != nil {
			s.logWarn("dnfbridge deferred selected actor mode0 appearance",
				"conn_id", session.connID,
				"operation", operation,
				"char_id", session.selectedCharacterID,
				"error", err)
			return fmt.Errorf("%w: refresh selected actor mode0 appearance: %v", errAlignedPostCommitProjectionDeferred, err)
		}
		return nil
	case alignedcmd.PostActionRefreshSelectedCreatureState:
		if err := s.sendSelectedCreatureStateAfterMoveAck(session, "aligned_"+operation+"_after_ack"); err != nil {
			// Creature state is an absolute post-ACK projection. Preserve the
			// already-committed endpoint move and let the next scene bootstrap
			// converge it instead of disconnecting after a successful ACK.
			s.logWarn("dnfbridge deferred selected creature state after move",
				"conn_id", session.connID,
				"operation", operation,
				"char_id", session.selectedCharacterID,
				"error", err)
		}
		return nil
	case alignedcmd.PostActionRefreshCrystalContractState:
		return s.sendCurrentCrystalContractState(session, "aligned_"+operation+"_after_type97_activation")
	case alignedcmd.PostActionRefreshSelectedDamageFontState:
		return s.sendSelectedDamageFontState(session, "aligned_"+operation+"_after_ack_and_item_refresh")
	case alignedcmd.PostActionRefreshSelectedPartyFrame:
		state, err := s.ensureManagedRuntimePartyForSession(session)
		if err != nil {
			return err
		}
		if err := s.sendRuntimePartySnapshot(session, state); err != nil {
			return err
		}
		return nil
	case alignedcmd.PostActionRefreshSelectedActorSkills:
		s.logPacketEvent("game-aligned-command-post-action-send",
			"conn_id", session.connID,
			"operation", operation,
			"post_action", action,
			"char_id", session.selectedCharacterID,
			"msg_id", currentSkillInfoMsgID,
			"classification", 0,
			"sequence", "durable_skill_reset_then_class1_op491_ack_then_class0_op19_refresh")
		return s.sendSelectedActorCurrentSceneSkillInfo(session, "aligned_"+operation+"_after_ack")
	default:
		return fmt.Errorf("aligned post action %q is not registered", action)
	}
}

// currentTownSelfActorAppearanceReady is stricter than the general scene-ready
// predicate. The in-place op357 redraw is allowed only for the already-finalized
// selected actor owned by the context committed for this town connection. This
// prevents an inventory action from targeting a remote/stale actor or crossing
// a town transition boundary.
func currentTownSelfActorAppearanceReady(session *gameSession) bool {
	if session == nil || session.selectedCharacterID == 0 {
		return false
	}
	session.townMu.Lock()
	ready := session.townSceneReadyCharacterID == session.selectedCharacterID &&
		session.townActorOwnerChannel == currentConnectionTownActorOwnerContext(session)
	session.townMu.Unlock()
	if !ready {
		return false
	}
	session.townPostTransition.mu.Lock()
	stage := session.townPostTransition.stage
	session.townPostTransition.mu.Unlock()
	return stage == currentTownPostTransitionIdle ||
		stage == currentTownPostTransitionComplete
}
