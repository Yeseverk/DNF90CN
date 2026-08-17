package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

// onlinePlayerInfo is one player's town presence state.
type onlinePlayerInfo struct {
	CharacterID uint16
	AccountID   string
	Name        string
	Job         byte
	GrowType    byte
	Level       byte
	TownID      byte
	AreaID      byte
	PositionX   uint16
	PositionY   uint16
	Direction   byte
	AreaState   byte
	Session     *gameSession
}

// areaKey identifies a unique town area.
type areaKey struct {
	TownID byte
	AreaID byte
}

// OnlinePlayerManager tracks online players grouped by (TownID, AreaID)
// and provides broadcast capabilities for town multiplayer visibility.
type OnlinePlayerManager struct {
	mu     sync.RWMutex
	byArea map[areaKey]map[uint16]*onlinePlayerInfo // areaKey -> characterID -> info
	byChar map[uint16]areaKey                       // characterID -> current area
}

func newOnlinePlayerManager() *OnlinePlayerManager {
	return &OnlinePlayerManager{
		byArea: make(map[areaKey]map[uint16]*onlinePlayerInfo),
		byChar: make(map[uint16]areaKey),
	}
}

// EnterArea registers a player in an area and returns the list of other
// players already in that area (for sending the full area list to the newcomer).
func (m *OnlinePlayerManager) EnterArea(info *onlinePlayerInfo) []onlinePlayerInfo {
	if m == nil || info == nil || info.CharacterID == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key := areaKey{TownID: info.TownID, AreaID: info.AreaID}

	// Remove from previous area if any.
	m.removeFromAreaLocked(info.CharacterID)

	// Add to new area.
	if m.byArea[key] == nil {
		m.byArea[key] = make(map[uint16]*onlinePlayerInfo)
	}
	m.byArea[key][info.CharacterID] = info
	m.byChar[info.CharacterID] = key

	// Collect other players in the area.
	others := make([]onlinePlayerInfo, 0, len(m.byArea[key])-1)
	for id, other := range m.byArea[key] {
		if id != info.CharacterID {
			others = append(others, *other)
		}
	}
	return others
}

// LeaveArea removes a player from their current area and returns the list of
// remaining players (for broadcasting the leave notification).
func (m *OnlinePlayerManager) LeaveArea(characterID uint16) (areaKey, []onlinePlayerInfo) {
	if m == nil || characterID == 0 {
		return areaKey{}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key, ok := m.byChar[characterID]
	if !ok {
		return areaKey{}, nil
	}
	m.removeFromAreaLocked(characterID)

	// Collect remaining players.
	others := make([]onlinePlayerInfo, 0, len(m.byArea[key]))
	for _, other := range m.byArea[key] {
		others = append(others, *other)
	}
	return key, others
}

// LeaveAreaSession removes presence only when the disconnecting transport is
// still the transport that registered the town actor.  The current client
// opens short-lived same-character auxiliary game connections (for example
// the cross-channel/raid directory probe); those connections must not evict
// the resident town connection when they close.
func (m *OnlinePlayerManager) LeaveAreaSession(characterID uint16, session *gameSession) (areaKey, []onlinePlayerInfo, bool) {
	if m == nil || characterID == 0 || session == nil {
		return areaKey{}, nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key, ok := m.byChar[characterID]
	if !ok {
		return areaKey{}, nil, false
	}
	area := m.byArea[key]
	current := area[characterID]
	if current == nil || current.Session != session {
		return areaKey{}, nil, false
	}
	m.removeFromAreaLocked(characterID)
	others := make([]onlinePlayerInfo, 0, len(m.byArea[key]))
	for _, other := range m.byArea[key] {
		others = append(others, *other)
	}
	return key, others, true
}

func (m *OnlinePlayerManager) SessionForCharacter(characterID uint16) *gameSession {
	if m == nil || characterID == 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok := m.byChar[characterID]
	if !ok {
		return nil
	}
	info := m.byArea[key][characterID]
	if info == nil {
		return nil
	}
	return info.Session
}

func (m *OnlinePlayerManager) PlayerForCharacter(characterID uint16) (onlinePlayerInfo, bool) {
	if m == nil || characterID == 0 {
		return onlinePlayerInfo{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok := m.byChar[characterID]
	if !ok {
		return onlinePlayerInfo{}, false
	}
	info := m.byArea[key][characterID]
	if info == nil {
		return onlinePlayerInfo{}, false
	}
	return *info, true
}

// UpdatePosition updates a player's position and returns the list of other
// players in the same area (for broadcasting the move notification).
func (m *OnlinePlayerManager) UpdatePosition(characterID uint16, posX, posY uint16) []onlinePlayerInfo {
	if m == nil || characterID == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key, ok := m.byChar[characterID]
	if !ok {
		return nil
	}
	info := m.byArea[key][characterID]
	if info == nil {
		return nil
	}
	info.PositionX = posX
	info.PositionY = posY

	others := make([]onlinePlayerInfo, 0, len(m.byArea[key])-1)
	for id, other := range m.byArea[key] {
		if id != characterID {
			others = append(others, *other)
		}
	}
	return others
}

// GetAreaPlayers returns all players in a specific area.
func (m *OnlinePlayerManager) GetAreaPlayers(townID, areaID byte) []onlinePlayerInfo {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := areaKey{TownID: townID, AreaID: areaID}
	players := make([]onlinePlayerInfo, 0, len(m.byArea[key]))
	for _, info := range m.byArea[key] {
		players = append(players, *info)
	}
	return players
}

// PeerInSameArea verifies that both characters are registered in the same
// concrete town area. A visible remote actor is only interactive inside this
// scope; an online session in another area is not a valid peer target.
func (m *OnlinePlayerManager) PeerInSameArea(sourceID, targetID uint16) bool {
	if m == nil || sourceID == 0 || targetID == 0 || sourceID == targetID {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	sourceKey, sourceOK := m.byChar[sourceID]
	targetKey, targetOK := m.byChar[targetID]
	if !sourceOK || !targetOK || sourceKey != targetKey {
		return false
	}
	area := m.byArea[sourceKey]
	return area != nil && area[sourceID] != nil && area[targetID] != nil
}

// PeersInSameArea returns a stable snapshot of every other player registered
// beside characterID. Callers use it for appearance-only projections that do
// not change movement state or area ownership.
func (m *OnlinePlayerManager) PeersInSameArea(characterID uint16) []onlinePlayerInfo {
	if m == nil || characterID == 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	key, ok := m.byChar[characterID]
	if !ok {
		return nil
	}
	area := m.byArea[key]
	peers := make([]onlinePlayerInfo, 0, len(area))
	for id, info := range area {
		if id == characterID || info == nil {
			continue
		}
		peers = append(peers, *info)
	}
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].CharacterID < peers[j].CharacterID
	})
	return peers
}

func (m *OnlinePlayerManager) removeFromAreaLocked(characterID uint16) {
	key, ok := m.byChar[characterID]
	if !ok {
		return
	}
	delete(m.byChar, characterID)
	if area := m.byArea[key]; area != nil {
		delete(area, characterID)
		if len(area) == 0 {
			delete(m.byArea, key)
		}
	}
}

// --- Broadcast packet builders ---
//
// IDA-verified wire formats (current 23.4.15.0 NoPack EXE):
//
//	NOTI 0x0016 (sub_1D83990): u16 actorKey, u16 x, u16 y, u8 direction, u16 movementRate
//	NOTI 0x0017 (sub_1D89590): u16 actorKey, u8 town, u8 area, u16 x, u16 y, u8 areaState, u8 direction
//	NOTI 0x0006 (C# ref):      u16 userId

const (
	currentTownAreaUserListMsgID uint16 = 0x0018
	currentTownUserLeaveMsgID    uint16 = 0x0006
)

// buildTownAreaUserListBody builds the current EXE sub_1D901D0 layout:
// u8 town, u8 area, u16 count, repeat(u16 actorKey, u16 x, u16 y, u8 dir, u8 state).
//
// This is more than a co-presence roster. The final row for the selected
// character rebinds the local scene actor used by town camera tracking.
func buildTownAreaUserListBody(townID, areaID byte, players []onlinePlayerInfo) []byte {
	count := len(players)
	if count > 0xFFFF {
		count = 0xFFFF
	}
	body := make([]byte, 4+count*8)
	body[0] = townID
	body[1] = areaID
	binary.LittleEndian.PutUint16(body[2:4], uint16(count))
	for i := 0; i < count; i++ {
		offset := 4 + i*8
		binary.LittleEndian.PutUint16(body[offset:offset+2], currentSceneActorObjectKey(players[i].CharacterID))
		binary.LittleEndian.PutUint16(body[offset+2:offset+4], players[i].PositionX)
		binary.LittleEndian.PutUint16(body[offset+4:offset+6], players[i].PositionY)
		body[offset+6] = players[i].Direction
		body[offset+7] = players[i].AreaState
	}
	return body
}

// buildTownUserLeaveBody builds NOTI 0x0006 body: u16 userId.
func buildTownUserLeaveBody(characterID uint16) []byte {
	body := make([]byte, 2)
	binary.LittleEndian.PutUint16(body, characterID)
	return body
}

// currentTownRemoteActorOwnerContext returns the receiving session's committed
// town-channel owner. Current NoPack sub_2009160 treats an owner equal to the
// resident town channel as a global scene actor and creates it by its distinct
// uint16 object key through sub_20036C0. A character-ID-derived owner instead
// selects the auxiliary context-object branch; that object can be drawn, but it
// is absent from the native global table used by player interaction menus.
func currentTownRemoteActorOwnerContext(target *gameSession, _ uint16) byte {
	return currentTownActorOwnerContext(target)
}

// sendTownRemoteActorState installs one repository-backed peer actor in the
// target client's town scene. 86JP establishes co-presence with both appearance
// and full user-info records before the area-state notification. The current
// 90CN client uses its typed mode0/mode1 equivalents instead of 86JP's legacy
// USERINFO subtype0/subtype1 bodies.
func (s *Service) sendTownRemoteActorState(
	target *gameSession,
	actor onlinePlayerInfo,
	source string,
) error {
	if target == nil || target.conn == nil || actor.Session == nil || actor.CharacterID == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, charName, character, hasCharacter := s.characterForEnter(
		ctx,
		actor.Session,
		actor.CharacterID,
	)
	if charID != actor.CharacterID || !hasCharacter {
		return fmt.Errorf(
			"remote town actor repository record unavailable: requested=%d loaded=%d found=%t",
			actor.CharacterID,
			charID,
			hasCharacter,
		)
	}

	remoteOwner := currentTownRemoteActorOwnerContext(target, charID)
	if remoteOwner == currentSceneObjectContext {
		return fmt.Errorf("remote town actor owner channel is not committed")
	}
	mode0Body, err := s.buildCurrentSceneObjectListBodyForSessionInContextStrict(
		ctx,
		actor.Session,
		charID,
		charName,
		character,
		hasCharacter,
		remoteOwner,
	)
	if err != nil {
		return fmt.Errorf("build remote town actor mode0: %w", err)
	}
	adventureSummary := s.currentAccountAdventureGroupSummaryForPacket(
		ctx,
		actor.Session,
		character,
		hasCharacter,
	)
	mode1Body := s.buildCurrentActorBindingMode1BodyForSelectedWithEquipmentInContext(
		ctx,
		actor.Session,
		character,
		hasCharacter,
		charID,
		uint32(adventureSummary.ManageLevel),
		remoteOwner,
		true,
	)
	s.logPacketEvent("game-town-copresence-actor-state-send",
		"conn_id", target.connID,
		"channel_id", target.channel.ID,
		"source", source,
		"target_char_id", target.selectedCharacterID,
		"target_local_owner", currentTownActorOwnerContext(target),
		"actor_char_id", charID,
		"object_key", currentSceneActorObjectKey(charID),
		"remote_owner", remoteOwner,
		"actor_job", numericCharacterStat(character.Job),
		"actor_grow_type", numericCharacterStatValue(character, "grow_type"),
		"actor_level", character.Level,
		"mode0_body_len", len(mode0Body),
		"mode1_body_len", len(mode1Body),
		"body_source", "current_exe_mode0_create_then_mode1_bind_then_same_owner_op9_party_state_before_area_state")
	if err := s.sendGameUpperRawClass(
		target,
		uint16(dnfenum.CmdPacketSetUDPIPPort),
		mode0Body,
		0,
	); err != nil {
		return fmt.Errorf("send remote town actor mode0: %w", err)
	}
	if err := s.sendGameUpperRawClass(
		target,
		uint16(dnfenum.CmdPacketSetUDPIPPort),
		mode1Body,
		0,
	); err != nil {
		return fmt.Errorf("send remote town actor mode1: %w", err)
	}
	// Current EXE sub_1D64CA0 kind0 does not create an actor. Resolve and send
	// the actor's actual runtime party state only after mode0 globally created
	// and mode1 bound the remote object. An empty state clears stale join-party
	// menu state; an explicitly created solo party remains joinable.
	if _, err := s.sendCurrentPartyActorFrameProjection(
		target,
		actor.Session,
		runtimePartyStateSnapshot(actor.Session),
		"town_copresence_actor_initial_party_state",
	); err != nil {
		return fmt.Errorf("send remote town actor op9 display: %w", err)
	}
	return nil
}

// broadcastTownPlayerEnter sends each remote actor's current typed mode0,
// equipment-bearing mode1, same-owner op9 display registration, then
// incremental 0x0017 state. The town transition owner has already sent one
// complete authoritative op24 packet; replaying a second self-containing op24
// here rebinds the local actor and breaks camera ownership.
func (s *Service) broadcastTownPlayerEnter(newPlayer *onlinePlayerInfo, others []onlinePlayerInfo) {
	if s == nil || s.onlinePlayers == nil || newPlayer == nil || newPlayer.Session == nil {
		return
	}
	peers := append([]onlinePlayerInfo(nil), others...)
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].CharacterID < peers[j].CharacterID
	})
	actorKey := currentSceneActorObjectKey(newPlayer.CharacterID)

	// To each existing player: newcomer's mode0/mode1/op9, then 0x0017.
	enterBody := buildCurrentTownUserAreaNotificationBody(
		actorKey, newPlayer.TownID, newPlayer.AreaID,
		newPlayer.PositionX, newPlayer.PositionY,
		newPlayer.Direction, newPlayer.AreaState,
	)
	for i := range peers {
		if peers[i].Session == nil {
			continue
		}
		if err := s.sendTownRemoteActorState(
			peers[i].Session,
			*newPlayer,
			"town_copresence_actor_state_newcomer_to_existing",
		); err != nil {
			s.logGameEvent(peers[i].Session, "game-town-copresence-actor-state-failed",
				"source", "town_copresence_actor_state_newcomer_to_existing",
				"actor_char_id", newPlayer.CharacterID,
				"error", err)
			continue
		}
		_ = s.sendCurrentSceneFixedClass0Packet(peers[i].Session,
			currentTownUserAreaNotificationMsgID, enterBody,
			"town_copresence_enter_0x0017_to_existing")
	}

	// To newcomer: each existing player's mode0/mode1/op9, then 0x0017.
	for i := range peers {
		if peers[i].Session == nil {
			continue
		}
		if err := s.sendTownRemoteActorState(
			newPlayer.Session,
			peers[i],
			"town_copresence_actor_state_existing_to_newcomer",
		); err != nil {
			s.logGameEvent(newPlayer.Session, "game-town-copresence-actor-state-failed",
				"source", "town_copresence_actor_state_existing_to_newcomer",
				"actor_char_id", peers[i].CharacterID,
				"error", err)
			continue
		}
		otherKey := currentSceneActorObjectKey(peers[i].CharacterID)
		otherBody := buildCurrentTownUserAreaNotificationBody(
			otherKey, peers[i].TownID, peers[i].AreaID,
			peers[i].PositionX, peers[i].PositionY,
			peers[i].Direction, peers[i].AreaState,
		)
		_ = s.sendCurrentSceneFixedClass0Packet(newPlayer.Session,
			currentTownUserAreaNotificationMsgID, otherBody,
			"town_copresence_enter_0x0017_existing_to_newcomer")
	}
}

// sendTownRemotePartyActors restores party members that are online in another
// town area after SET_USER_AREA rebuilt the receiving client's actor manager.
// Current sub_1D64CA0's same-owner party rows contain only user ids, so every
// referenced user must first exist through the normal mode0/mode1/op9 actor
// path. The following op23 carries the member's real town/area and position;
// it does not claim co-presence in the receiver's destination area.
func (s *Service) sendTownRemotePartyActors(receiver *gameSession, state alignedcmd.PartyState) error {
	if s == nil || s.onlinePlayers == nil || receiver == nil || receiver.selectedCharacterID == 0 || state.PartyID <= 0 {
		return nil
	}
	for _, member := range runtimePartyMembers(state) {
		if member.UserID == 0 || member.UserID == receiver.selectedCharacterID ||
			s.onlinePlayers.PeerInSameArea(receiver.selectedCharacterID, member.UserID) {
			continue
		}
		actor, ok := s.onlinePlayers.PlayerForCharacter(member.UserID)
		if !ok || actor.Session == nil {
			continue
		}
		if err := s.sendTownRemoteActorState(receiver, actor, "town_remote_party_actor_after_set_user_area"); err != nil {
			return err
		}
		areaBody := buildCurrentTownUserAreaNotificationBody(
			currentSceneActorObjectKey(actor.CharacterID),
			actor.TownID,
			actor.AreaID,
			actor.PositionX,
			actor.PositionY,
			actor.Direction,
			actor.AreaState,
		)
		if err := s.sendCurrentSceneFixedClass0Packet(
			receiver,
			currentTownUserAreaNotificationMsgID,
			areaBody,
			"town_remote_party_actor_real_area_after_state",
		); err != nil {
			return err
		}
	}
	return nil
}

// broadcastTownPlayerLeave sends NOTI 0x0006 (u16 userId) to remaining
// players so they remove the departed player's town actor.
func (s *Service) broadcastTownPlayerLeave(characterID uint16, others []onlinePlayerInfo) {
	if s == nil || s.onlinePlayers == nil || len(others) == 0 {
		return
	}
	leaveBody := buildTownUserLeaveBody(characterID)
	for i := range others {
		if others[i].Session != nil {
			_ = s.sendCurrentSceneFixedClass0Packet(others[i].Session,
				currentTownUserLeaveMsgID, leaveBody,
				"town_copresence_leave_0x0006")
		}
	}
}

// broadcastTownPlayerAreaChange removes an ordinary old-area actor, but keeps
// an active party member bound to the stationary client's party manager. The
// current client treats op6 as both a town-actor destroy and a party-member
// removal. For party peers the native op23 area update is sufficient: it moves
// the existing actor out of the rendered area while retaining the roster and
// minimap identity. Recreating that actor after op6 is unsafe because the
// client answers the synthetic cross-area rebuild with LEAVE_PARTY.
func (s *Service) broadcastTownPlayerAreaChange(
	mover *onlinePlayerInfo,
	oldOthers []onlinePlayerInfo,
	state alignedcmd.PartyState,
) {
	if s == nil || mover == nil || len(oldOthers) == 0 {
		return
	}
	partyMembers := make(map[uint16]struct{}, len(runtimePartyMembers(state)))
	if state.PartyID > 0 {
		for _, member := range runtimePartyMembers(state) {
			if member.UserID != 0 && member.UserID != mover.CharacterID {
				partyMembers[member.UserID] = struct{}{}
			}
		}
	}
	leaveBody := buildTownUserLeaveBody(mover.CharacterID)
	areaBody := buildCurrentTownUserAreaNotificationBody(
		currentSceneActorObjectKey(mover.CharacterID),
		mover.TownID,
		mover.AreaID,
		mover.PositionX,
		mover.PositionY,
		mover.Direction,
		mover.AreaState,
	)
	for i := range oldOthers {
		peer := oldOthers[i].Session
		if peer == nil {
			continue
		}
		_, sameAuthoritativeParty := partyMembers[oldOthers[i].CharacterID]
		peerState := runtimePartyStateSnapshot(peer)
		if sameAuthoritativeParty && peerState.PartyID == state.PartyID {
			_ = s.sendCurrentSceneFixedClass0Packet(
				peer,
				currentTownUserAreaNotificationMsgID,
				areaBody,
				"town_party_member_area_change_0x0017",
			)
			continue
		}
		_ = s.sendCurrentSceneFixedClass0Packet(
			peer,
			currentTownUserLeaveMsgID,
			leaveBody,
			"town_copresence_leave_0x0006",
		)
	}
}

// broadcastTownPlayerMove sends NOTI 0x0016 (sub_1D83990 position update) to
// all other players in the area.
func (s *Service) broadcastTownPlayerMove(mover *onlinePlayerInfo, others []onlinePlayerInfo) {
	if s == nil || s.onlinePlayers == nil || mover == nil || len(others) == 0 {
		return
	}
	actorKey := currentSceneActorObjectKey(mover.CharacterID)
	moveBody := buildCurrentTownUserPositionNotificationBody(
		actorKey, mover.PositionX, mover.PositionY, mover.Direction,
	)
	for i := range others {
		if others[i].Session != nil {
			_ = s.sendCurrentSceneFixedClass0Packet(others[i].Session,
				currentTownUserPositionNotificationMsgID, moveBody,
				"town_copresence_move_0x0016")
		}
	}
}

// cleanupOnlinePlayer removes a player from the online player manager on
// disconnect and broadcasts the leave notification to remaining players.
func (s *Service) cleanupOnlinePlayer(session *gameSession) {
	if s == nil || s.onlinePlayers == nil || session == nil || session.selectedCharacterID == 0 {
		return
	}
	s.closeCurrentExpertJobStoreSession(session, false)
	_, others, removed := s.onlinePlayers.LeaveAreaSession(session.selectedCharacterID, session)
	if !removed {
		return
	}
	if len(others) > 0 {
		s.broadcastTownPlayerLeave(session.selectedCharacterID, others)
	}
}
