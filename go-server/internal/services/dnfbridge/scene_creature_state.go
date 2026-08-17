package dnfbridge

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnppet "longheng.io/server/internal/modules/dnf/pet"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentCreatureStateTableMsgID uint16 = 105

// sendSelectedCreatureInitialStateAfterMode1 refreshes the existing slot-26
// object's durable item state, then installs the repository-backed creature
// table and equipped creature's growth state as soon as mode1 has created it.
func (s *Service) sendSelectedCreatureInitialStateAfterMode1(session *gameSession, source string) error {
	if err := s.sendSelectedEquippedEquipmentEffectRuneRefresh(session, source+"_equipment_effect_rune"); err != nil {
		return err
	}
	if err := s.sendSelectedEquippedCreatureItemRefresh(session, source+"_equipped_item"); err != nil {
		return err
	}
	if err := s.sendSelectedCreatureStateTable(session, source+"_state_table"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	equippedCreature := s.currentEquippedCreatureForCharacter(ctx, strconv.Itoa(int(session.selectedCharacterID)))
	cancel()
	if !equippedCreature.valid() {
		return nil
	}
	return s.sendSelectedEquippedCreatureGrowthState(session, equippedCreature, source+"_equipped_growth")
}

// sendSelectedCreatureSceneReadyProjection is the deferred-tail fallback for a
// lifecycle that did not already publish the creature table after mode1. A
// normal login has already sent op105/op102 and therefore writes nothing here.
//
// Do not send class0/op2 from this bootstrap callback. The callback owns only
// the one-time creature table/growth projection. Runtime slot-26 changes let
// the native class1/op19 handler move the target-26 object, then append only
// the independent creature table/growth state.
func (s *Service) sendSelectedCreatureSceneReadyProjection(session *gameSession, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return fmt.Errorf("selected creature scene projection requires an active character")
	}
	if session.selectedCreatureStateTableSent {
		s.logPacketEvent("game-upper-current-creature-scene-ready-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "op105_and_op102_already_published_after_mode1_no_post_op24_actor_rebuild")
		return nil
	}
	if err := s.sendSelectedEquippedCreatureItemRefresh(session, source+"_equipped_item_fallback"); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	equippedCreature := s.currentEquippedCreatureForCharacter(ctx, strconv.Itoa(int(session.selectedCharacterID)))
	cancel()
	if err := s.sendSelectedCreatureStateTable(session, source+"_state_table_fallback"); err != nil {
		return err
	}
	if !equippedCreature.valid() {
		return nil
	}
	return s.sendSelectedEquippedCreatureGrowthState(session, equippedCreature, source+"_growth_fallback")
}

// sendSelectedCreatureStateAfterMoveAck resolves pet state only after the
// class1/op19 handler has moved the selected actor's target-26 object. The
// absolute op105 table is idempotent for one scene lifecycle; op102 is sent
// only when a creature is currently equipped and therefore has a live target.
func (s *Service) sendSelectedCreatureStateAfterMoveAck(session *gameSession, source string) error {
	if err := s.sendSelectedEquippedCreatureItemRefresh(session, source+"_equipped_item"); err != nil {
		return err
	}
	if err := s.sendSelectedCreatureStateTableWithRefresh(session, source+"_state_table", true); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	equippedCreature := s.currentEquippedCreatureForCharacter(ctx, strconv.Itoa(int(session.selectedCharacterID)))
	cancel()
	if !equippedCreature.valid() {
		return nil
	}
	return s.sendSelectedEquippedCreatureGrowthState(session, equippedCreature, source+"_growth")
}

// sendSelectedCreatureStateTable publishes the current NoPack raw class0/op105
// table only after the scene lifecycle has made the selected actor safe. It is
// deliberately idempotent within one selected-scene lifecycle: op19 itself
// still has only its proved relocation ACK.
func (s *Service) sendSelectedCreatureStateTable(session *gameSession, source string) error {
	return s.sendSelectedCreatureStateTableWithRefresh(session, source, false)
}

func (s *Service) sendSelectedCreatureStateTableWithRefresh(session *gameSession, source string, refresh bool) error {
	if session == nil || session.selectedCharacterID == 0 {
		return fmt.Errorf("selected creature state requires an active character")
	}
	if session.selectedCreatureStateTableSent && !refresh {
		s.logPacketEvent("game-upper-current-creature-state-table-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"msg_id", currentCreatureStateTableMsgID,
			"reason", "already_sent_for_selected_scene")
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Pet == nil {
		return fmt.Errorf("selected creature state repository is unavailable")
	}
	owner, err := dnppet.NewOwner(repositories)
	if err != nil {
		return fmt.Errorf("create selected creature state owner: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := owner.List(ctx, dnppet.ListCommand{SelectedCharacterID: session.selectedCharacterID})
	if err != nil {
		return fmt.Errorf("load selected creature state: %w", err)
	}
	wireEntries, projectedNameCount := s.currentCreatureStateEntriesForWire(result.Entries)
	body, err := dnppet.BuildCreatureListBody(wireEntries)
	if err != nil {
		return fmt.Errorf("build selected creature state: %w", err)
	}
	stateSummary := make([]string, 0, len(wireEntries))
	for _, entry := range wireEntries {
		key := strings.TrimSpace(entry.PetKey)
		if entry.CreatureKey != 0 {
			key = fmt.Sprintf("%d", entry.CreatureKey)
		}
		stateSummary = append(stateSummary, fmt.Sprintf(
			"%s:level=%d:exp=%d:satiety=%d",
			key,
			entry.Level,
			entry.Exp,
			entry.Satiety,
		))
	}
	sort.Strings(stateSummary)
	s.logPacketEvent("game-upper-current-creature-state-table-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", currentCreatureStateTableMsgID,
		"classification", 0,
		"refresh", refresh,
		"entry_count", result.EntryCount,
		"projected_name_count", projectedNameCount,
		"state_summary", strings.Join(stateSummary, ";"),
		"body_len", len(body),
		"body_source", "pet_repository_current_exe_sub_1D57AB0_typed_raw")
	if err := s.sendGameUpperRawClass(session, currentCreatureStateTableMsgID, body, 0); err != nil {
		return err
	}
	session.selectedCreatureStateTableSent = true
	mode := currentPetGrowthClockTown
	session.dungeon.mu.Lock()
	if runtime := session.dungeon.runtime; runtime != nil && runtime.Session != nil {
		mode = currentPetGrowthClockDungeon
	}
	session.dungeon.mu.Unlock()
	if err := s.switchCurrentPetGrowthClock(session, mode, s.gameplayNow(), source+"_after_op105"); err != nil {
		s.logGameEvent(session, "game-pet-growth-clock-start-deferred",
			"char_id", session.selectedCharacterID,
			"mode", mode.String(),
			"source", source,
			"error", err)
	}
	return nil
}

// currentCreatureStateEntriesForWire keeps durable custom names authoritative
// and repairs only empty legacy/imported names at the protocol boundary. PVF
// text is UTF-8 after archive decoding, while the current client consumes the
// op105 DSTR through its native Chinese code page, so the fallback is encoded
// as GB18030 before it is placed on the wire.
func (s *Service) currentCreatureStateEntriesForWire(entries []dnfrepo.PetEntry) ([]dnfrepo.PetEntry, int) {
	out := append([]dnfrepo.PetEntry(nil), entries...)
	projected := 0
	var catalog *dnppet.PVFCatalog
	catalogLoaded := false
	for index := range out {
		entry := &out[index]
		entry.NameRaw = append([]byte(nil), entry.NameRaw...)
		if len(entry.NameRaw) != 0 {
			continue
		}

		name := strings.TrimSpace(entry.Name)
		if name == "" {
			if !catalogLoaded {
				catalog, _ = s.currentPetPVFCatalog()
				catalogLoaded = true
			}
			if catalog != nil {
				if definition, err := catalog.ResolveCreature(entry.ItemID); err == nil {
					name = strings.TrimSpace(definition.Name)
				}
			}
		}
		if name == "" {
			continue
		}
		encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(name))
		if err != nil || len(encoded) == 0 || len(encoded) >= 30 {
			continue
		}
		entry.Name = name
		entry.NameRaw = encoded
		projected++
	}
	return out, projected
}

func (s *Service) sendSelectedEquippedCreatureGrowthState(
	session *gameSession,
	equippedCreature currentEquippedCreatureSnapshot,
	source string,
) error {
	if session == nil || session.selectedCharacterID == 0 || !equippedCreature.valid() {
		return fmt.Errorf("selected equipped creature growth requires an active creature")
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Pet == nil {
		return fmt.Errorf("selected equipped creature growth repository is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	record, found, err := repositories.Pet.Load(ctx, strconv.Itoa(int(session.selectedCharacterID)))
	if err != nil {
		return fmt.Errorf("load selected equipped creature growth: %w", err)
	}
	if !found {
		return fmt.Errorf("selected equipped creature growth record was not found")
	}
	entry, found := currentEquippedCreatureMetadataEntry(equippedCreature, record)
	if !found {
		return fmt.Errorf(
			"selected equipped creature growth entry was not found: item=%d serial=%d",
			equippedCreature.itemID,
			equippedCreature.serialOrHandle,
		)
	}
	body, err := dnppet.BuildCreatureGrowthBody(entry)
	if err != nil {
		return fmt.Errorf("build selected equipped creature growth: %w", err)
	}
	s.logPacketEvent("game-upper-current-equipped-creature-growth-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"creature_key", entry.CreatureKey,
		"item_id", entry.ItemID,
		"level", entry.Level,
		"experience", entry.Exp,
		"satiety", entry.Satiety,
		"msg_id", currentCreatureGrowthMsgID,
		"classification", 0,
		"body_len", len(body),
		"body_source", "current_exe_sub_1D5AF60_scene_op102_after_op105_target26_bind")
	return s.sendGameUpperRawClass(session, currentCreatureGrowthMsgID, body, 0)
}

// sendSelectedActorMode0AppearanceRefresh refreshes an already-bound scene
// actor in the same owner context that created it.
func (s *Service) sendSelectedActorMode0AppearanceRefresh(session *gameSession, operation string, sequence string) error {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, charName, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if charID == 0 || charID != session.selectedCharacterID || !hasCharacter {
		return fmt.Errorf("selected actor mode0 appearance refresh could not load selected actor")
	}
	mode0Body, err := s.buildCurrentSceneObjectListBodyForSessionInContextStrict(
		ctx,
		session,
		charID,
		charName,
		character,
		hasCharacter,
		currentTownActorOwnerContext(session),
	)
	if err != nil {
		return err
	}
	s.logPacketEvent("game-aligned-command-post-action-send",
		"conn_id", session.connID,
		"operation", operation,
		"post_action", "refresh_selected_actor_appearance",
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketSetUDPIPPort),
		"classification", 0,
		"body_len", len(mode0Body),
		"body_source", "character_equipment_repository_full_current_scene_object_mode0",
		"owner_context", currentTownActorOwnerContext(session),
		"sequence", sequence)
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSetUDPIPPort), mode0Body, 0)
}

func (s *Service) sendSelectedActorAppearanceRefresh(session *gameSession, operation string, sequence string) error {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, charName, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if charID == 0 || charID != session.selectedCharacterID || !hasCharacter {
		return fmt.Errorf("selected actor appearance refresh could not load selected actor")
	}
	ownerChannel := currentTownActorOwnerContext(session)
	mode0Body, err := s.buildCurrentSceneObjectListBodyForSessionInContextStrict(
		ctx,
		session,
		charID,
		charName,
		character,
		hasCharacter,
		ownerChannel,
	)
	if err != nil {
		return err
	}
	var legacyRepo dnfrepo.LegacyUserInfoRepository
	if repositories, ok := s.repositoryGroup(); ok {
		legacyRepo = repositories.LegacyUserInfo
	}
	mode1Body := s.buildCurrentSelectedUserInfoMode1BodyInContext(ctx, session, legacyRepo, character, hasCharacter, charID, ownerChannel)
	s.logPacketEvent("game-aligned-command-post-action-send",
		"conn_id", session.connID,
		"operation", operation,
		"post_action", "refresh_selected_actor_appearance",
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketSetUDPIPPort),
		"classification", 0,
		"body_len", len(mode0Body),
		"body_source", "character_equipment_repository_full_current_scene_object_mode0",
		"sequence", sequence)
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSetUDPIPPort), mode0Body, 0); err != nil {
		return err
	}
	s.logPacketEvent("game-aligned-command-post-action-send",
		"conn_id", session.connID,
		"operation", operation,
		"post_action", "rebind_selected_actor_equipment",
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketSetUDPIPPort),
		"classification", 0,
		"body_len", len(mode1Body),
		"body_source", "character_equipment_repository_full_current_scene_object_mode1",
		"object_key", currentSceneActorObjectKey(charID),
		"sequence", sequence)
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSetUDPIPPort), mode1Body, 0)
}

// sendSelectedActorMode1AppearanceRefresh remains available for protocol paths
// that explicitly own a standalone current-EXE mode1. This helper remains for
// flows which explicitly require a full actor rebind.
func (s *Service) sendSelectedActorMode1AppearanceRefresh(session *gameSession, operation string, sequence string) error {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, _, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if charID == 0 || charID != session.selectedCharacterID || !hasCharacter {
		return fmt.Errorf("selected actor mode1 appearance refresh could not load selected actor")
	}
	var legacyRepo dnfrepo.LegacyUserInfoRepository
	if repositories, ok := s.repositoryGroup(); ok {
		legacyRepo = repositories.LegacyUserInfo
	}
	body := s.buildCurrentSelectedUserInfoMode1BodyInContext(
		ctx,
		session,
		legacyRepo,
		character,
		hasCharacter,
		charID,
		currentTownActorOwnerContext(session),
	)
	s.logPacketEvent("game-aligned-command-post-action-send",
		"conn_id", session.connID,
		"operation", operation,
		"post_action", "refresh_selected_actor_appearance",
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketSetUDPIPPort),
		"classification", 0,
		"body_len", len(body),
		"body_source", "character_equipment_repository_full_current_scene_object_mode1",
		"object_key", currentSceneActorObjectKey(charID),
		"sequence", sequence)
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSetUDPIPPort), body, 0)
}
