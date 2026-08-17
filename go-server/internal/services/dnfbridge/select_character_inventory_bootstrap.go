package dnfbridge

import (
	"context"
	"fmt"
	"strconv"

	dnfenum "longheng.io/server/internal/modules/dnf/dnfenum"
)

// currentSelectInventoryBootstrapListTypes is the complete selected-character
// container set installed after the local actor has been bound. The bodies and
// envelope are built with the current NoPack op13 reader contract (16-byte
// fixed upper header and current 0x77 item rows).
var currentSelectInventoryBootstrapListTypes = []byte{0, 1, 2, 7, 12, currentGuildMedalInventoryListType}

type currentSelectInventoryBootstrapPacket struct {
	listType    byte
	body        []byte
	entrySource string
	entryCount  int
}

// sendCurrentSelectInventoryBootstrap owns the one-shot inventory bootstrap
// for a selected character. It must be called after the scene has created and
// bound the selected actor, and before the full equipment-bearing mode1 packet.
// Runtime mutations continue to use the native ACK path, op14, or an explicit
// op13 refresh where the command contract requires one.
func (s *Service) sendCurrentSelectInventoryBootstrap(session *gameSession, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if session.selectedItemListBootstrapCharacterID == session.selectedCharacterID {
		s.logPacketEvent("game-upper-select-inventory-bootstrap-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "already_sent")
		return nil
	}

	s.cleanupSelectedExpiredRentalEquipment(session, source)
	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()

	// Build the complete character snapshot before writing its first packet. A
	// later repository failure must not leave the client with only a subset of
	// its selected-character containers installed.
	plan := make([]currentSelectInventoryBootstrapPacket, 0, len(currentSelectInventoryBootstrapListTypes))
	for _, listType := range currentSelectInventoryBootstrapListTypes {
		body, entrySource, entryCount, ok := s.buildCurrentSelectInventoryBootstrapBody(ctx, session, listType)
		if !ok {
			s.logPacketEvent("game-upper-select-inventory-bootstrap-deferred",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"source", source,
				"char_id", session.selectedCharacterID,
				"list_type", listType,
				"reason", "repository_snapshot_unavailable")
			return fmt.Errorf("selected character %d inventory list %d snapshot unavailable", session.selectedCharacterID, listType)
		}
		plan = append(plan, currentSelectInventoryBootstrapPacket{
			listType:    listType,
			body:        body,
			entrySource: entrySource,
			entryCount:  entryCount,
		})
	}
	// The current client keeps a separate equipment-lock model. It does not
	// derive that model from op13 item rows, so build the complete durable
	// snapshot before any container write and deliver it immediately after all
	// six containers. An explicit empty list is required to clear a stale local
	// locked state before an item-use request such as op338 can be generated.
	lockSnapshot, lockSnapshotOK := s.buildCurrentItemLockSnapshotForSession(ctx, session)
	if !lockSnapshotOK {
		s.logPacketEvent("game-upper-select-inventory-bootstrap-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "item_lock_snapshot_unavailable")
		return fmt.Errorf("selected character %d item-lock snapshot unavailable", session.selectedCharacterID)
	}
	for index, packet := range plan {
		s.logPacketEvent("game-upper-select-inventory-bootstrap-send",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"packet_index", index,
			"msg_id", uint16(dnfenum.CmdPacketLeaveParty),
			"list_type", packet.listType,
			"entry_count", packet.entryCount,
			"body_len", len(packet.body),
			"body_source", packet.entrySource,
			"entry_size", currentItemListEntrySizeForType(packet.listType),
			"wire_contract", "current_exe_sub_1D72380_fixed16_0x77",
			"lifecycle", "current_exe_actor_bound_then_character_containers_before_full_mode1")
		if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), packet.body); err != nil {
			return err
		}
	}
	if err := s.sendCurrentSelectItemLockSnapshot(
		session,
		source+"_after_selected_containers_before_full_mode1",
		lockSnapshot,
	); err != nil {
		return err
	}
	session.selectedItemListRefreshSent = true
	session.selectedItemListBootstrapCharacterID = session.selectedCharacterID
	s.logPacketEvent("game-upper-select-inventory-bootstrap-committed",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"packet_count", len(plan)+1,
		"item_lock_snapshot_body_len", len(lockSnapshot))
	return nil
}

func (s *Service) buildCurrentSelectInventoryBootstrapBody(
	ctx context.Context,
	session *gameSession,
	listType byte,
) ([]byte, string, int, bool) {
	body, source, count, ok := s.buildCurrentItemListBodyForSession(ctx, session, listType)
	if ok {
		return body, source, count, true
	}

	// An absent inventory record is a real empty inventory. 86JP still creates
	// every client container on selection, so emit an empty current-EXE body;
	// repository errors remain fail-closed and are never disguised as empty.
	repos, repositoriesOK := s.repositoryGroup()
	if !repositoriesOK || repos.Inventory == nil {
		return nil, source, count, false
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	_, found, err := repos.Inventory.Load(ctx, characterID)
	if err != nil || found {
		return nil, source, count, false
	}
	state := s.loadCurrentItemListContainerState(ctx, repos.Settings, characterID, listType)
	return buildCurrentItemListBody(listType, nil, state), "inventory_absent_empty", 0, true
}
