package party

import (
	"sort"
	"sync"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// RuntimePartyManager owns the process-local lifecycle of online parties.
// It deliberately contains no connection or packet code: dnfbridge validates
// the live session generation at its boundary, then supplies that immutable
// identity here. This keeps one authoritative membership graph instead of a
// divergent party copy on every game session.
type RuntimePartyManager struct {
	mu sync.Mutex

	nextID  uint16
	parties map[uint16]*runtimeParty
	byUser  map[uint16]uint16
	invites map[uint16]runtimePartyInvite
}

type RuntimePartyMember struct {
	UserID            uint16
	SessionGeneration uint64
	State             alignedcmd.PartyMemberState
	Slot              byte
}

type RuntimePartySnapshot struct {
	ID       uint16
	Leader   uint16
	Settings alignedcmd.PartyState
	Members  []RuntimePartyMember
}

type RuntimePartyResult struct {
	OK          bool
	Reason      string
	Party       RuntimePartySnapshot
	Previous    *RuntimePartySnapshot
	Retired     *RuntimePartySnapshot
	PriorLeave  *RuntimePartyResult
	TargetUser  uint16
	Disbanded   bool
	LeaderMoved bool
}

type runtimeParty struct {
	id       uint16
	leader   uint16
	settings alignedcmd.PartyState
	members  [currentRuntimePartyMaxMembers]*RuntimePartyMember
}

type runtimePartyInvite struct {
	invitee           uint16
	inviteeGeneration uint64
	inviter           uint16
	inviterGeneration uint64
	partyID           uint16
	mode              byte
}

const currentRuntimePartyMaxMembers = 4

func NewRuntimePartyManager() *RuntimePartyManager {
	return &RuntimePartyManager{
		nextID:  1,
		parties: make(map[uint16]*runtimeParty),
		byUser:  make(map[uint16]uint16),
		invites: make(map[uint16]runtimePartyInvite),
	}
}

func (m *RuntimePartyManager) SnapshotByUser(userID uint16, generation uint64) (RuntimePartySnapshot, bool) {
	if m == nil || userID == 0 {
		return RuntimePartySnapshot{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	party, member := m.partyMemberLocked(userID)
	if party == nil || member == nil || (generation != 0 && member.SessionGeneration != generation) {
		return RuntimePartySnapshot{}, false
	}
	return snapshotRuntimeParty(party), true
}

func (m *RuntimePartyManager) SnapshotByID(partyID uint16) (RuntimePartySnapshot, bool) {
	if m == nil || partyID == 0 {
		return RuntimePartySnapshot{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	party := m.parties[partyID]
	if party == nil {
		return RuntimePartySnapshot{}, false
	}
	return snapshotRuntimeParty(party), true
}

func (m *RuntimePartyManager) Snapshots() []RuntimePartySnapshot {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]int, 0, len(m.parties))
	for partyID := range m.parties {
		ids = append(ids, int(partyID))
	}
	sort.Ints(ids)
	result := make([]RuntimePartySnapshot, 0, len(ids))
	for _, partyID := range ids {
		result = append(result, snapshotRuntimeParty(m.parties[uint16(partyID)]))
	}
	return result
}

// Create starts a fresh party for leader. If that character already belongs
// to another party, it is detached atomically and the old-party result is
// returned through PriorLeave so the bridge can notify survivors.
func (m *RuntimePartyManager) Create(leader RuntimePartyMember, settings alignedcmd.PartyState) RuntimePartyResult {
	if m == nil || !validRuntimePartyMember(leader) {
		return runtimePartyFailure("invalid_leader")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	partyID := m.nextPartyIDLocked()
	if partyID == 0 {
		return runtimePartyFailure("party_id_exhausted")
	}
	prior := m.leaveLocked(leader.UserID, leader.SessionGeneration, true)
	party := &runtimeParty{id: partyID, leader: leader.UserID, settings: normalizeRuntimePartySettings(settings)}
	party.settings.PartyID = int(party.id)
	party.settings.UserID = leader.UserID
	party.settings.IsLeader = true
	if !party.add(leader) {
		return runtimePartyFailure("add_leader_failed")
	}
	m.parties[party.id] = party
	m.byUser[leader.UserID] = party.id
	result := runtimePartySuccess(party, leader.UserID)
	if prior.OK {
		result.PriorLeave = &prior
	}
	return result
}

// EnsureLeader creates a lobby only when this exact live leader has no party.
// It is used by bridge-side migration/first-contact paths, where two packets
// may observe the same partyless target concurrently. Unlike Create, it never
// detaches an existing party merely because a duplicate bootstrap arrived.
func (m *RuntimePartyManager) EnsureLeader(leader RuntimePartyMember, settings alignedcmd.PartyState) RuntimePartyResult {
	if m == nil || !validRuntimePartyMember(leader) {
		return runtimePartyFailure("invalid_leader")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, member := m.partyMemberLocked(leader.UserID); existing != nil && member != nil {
		if member.SessionGeneration != leader.SessionGeneration {
			return runtimePartyFailure("stale_member_session")
		}
		if existing.leader != leader.UserID {
			return runtimePartyFailure("already_party_member")
		}
		return runtimePartySuccess(existing, leader.UserID)
	}
	party := &runtimeParty{id: m.nextPartyIDLocked(), leader: leader.UserID, settings: normalizeRuntimePartySettings(settings)}
	if party.id == 0 {
		return runtimePartyFailure("party_id_exhausted")
	}
	party.settings.PartyID = int(party.id)
	party.settings.UserID = leader.UserID
	party.settings.IsLeader = true
	if !party.add(leader) {
		return runtimePartyFailure("add_leader_failed")
	}
	m.parties[party.id] = party
	m.byUser[leader.UserID] = party.id
	return runtimePartySuccess(party, leader.UserID)
}

// Join joins member to the current party generation. A previous membership is
// always resolved within the same lock, so no character can exist in two
// parties even while join, leave and disconnect packets race.
func (m *RuntimePartyManager) Join(partyID uint16, member RuntimePartyMember) RuntimePartyResult {
	if m == nil || partyID == 0 || !validRuntimePartyMember(member) {
		return runtimePartyFailure("invalid_join")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	party := m.parties[partyID]
	if party == nil {
		return runtimePartyFailure("party_not_found")
	}
	if existing := party.member(member.UserID); existing != nil {
		if existing.SessionGeneration == member.SessionGeneration {
			return runtimePartyFailure("already_member")
		}
		return runtimePartyFailure("stale_member_session")
	}
	if party.full() {
		return runtimePartyFailure("party_full")
	}
	prior := m.leaveLocked(member.UserID, member.SessionGeneration, true)
	if !party.add(member) {
		return runtimePartyFailure("add_member_failed")
	}
	m.byUser[member.UserID] = party.id
	result := runtimePartySuccess(party, member.UserID)
	if prior.OK {
		result.PriorLeave = &prior
	}
	return result
}

func (m *RuntimePartyManager) Leave(userID uint16, generation uint64) RuntimePartyResult {
	if m == nil || userID == 0 || generation == 0 {
		return runtimePartyFailure("invalid_leave")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leaveLocked(userID, generation, false)
}

func (m *RuntimePartyManager) leaveLocked(userID uint16, generation uint64, allowAbsent bool) RuntimePartyResult {
	party, member := m.partyMemberLocked(userID)
	if party == nil || member == nil {
		if allowAbsent {
			return RuntimePartyResult{}
		}
		return runtimePartyFailure("not_in_party")
	}
	if generation != 0 && member.SessionGeneration != generation {
		return runtimePartyFailure("stale_session")
	}
	previous := snapshotRuntimeParty(party)
	previousCopy := previous
	retired := RuntimePartySnapshot{}
	wasLeader := party.leader == userID
	if wasLeader && party.count() > 1 {
		retired = snapshotRuntimeParty(party)
	}
	party.remove(userID)
	delete(m.byUser, userID)
	m.clearInvitesForLocked(userID, generation)
	if party.count() == 0 {
		delete(m.parties, party.id)
		return RuntimePartyResult{OK: true, Previous: &previousCopy, TargetUser: userID, Disbanded: true}
	}
	if !wasLeader {
		result := runtimePartySuccess(party, userID)
		result.Previous = &previousCopy
		return result
	}

	// The current client cannot safely consume a same-id leader replacement.
	// Retire the generation and rebuild surviving members under a new id with
	// the successor in slot zero, matching the tested 86JP lifecycle.
	members := party.membersBySlot()
	newLeader := members[0]
	delete(m.parties, party.id)
	for _, survivor := range members {
		delete(m.byUser, survivor.UserID)
	}
	rebuilt := m.rebuildLocked(party, newLeader, members)
	m.parties[rebuilt.id] = rebuilt
	for _, survivor := range rebuilt.membersBySlot() {
		m.byUser[survivor.UserID] = rebuilt.id
	}
	retiredCopy := retired
	result := runtimePartySuccess(rebuilt, userID)
	result.Previous = &previousCopy
	result.LeaderMoved = true
	result.Retired = &retiredCopy
	return result
}

func (m *RuntimePartyManager) Kick(byUser uint16, byGeneration uint64, targetUser uint16, targetGeneration uint64) RuntimePartyResult {
	if m == nil || byUser == 0 || targetUser == 0 || byUser == targetUser || byGeneration == 0 || targetGeneration == 0 {
		return runtimePartyFailure("invalid_kick")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	party, leader := m.partyMemberLocked(byUser)
	if party == nil || leader == nil {
		return runtimePartyFailure("not_in_party")
	}
	if leader.SessionGeneration != byGeneration {
		return runtimePartyFailure("stale_session")
	}
	if party.leader != byUser {
		return runtimePartyFailure("not_leader")
	}
	target := party.member(targetUser)
	if target == nil {
		return runtimePartyFailure("target_not_member")
	}
	if target.SessionGeneration != targetGeneration {
		return runtimePartyFailure("stale_target_session")
	}
	previous := snapshotRuntimeParty(party)
	party.remove(targetUser)
	delete(m.byUser, targetUser)
	m.clearInvitesForLocked(targetUser, targetGeneration)
	result := runtimePartySuccess(party, targetUser)
	result.Previous = &previous
	return result
}

// TransferLeader is implemented as a full replacement generation rather than
// an in-place slot swap. The retired snapshot lets the bridge clear the old
// client table before it publishes the new party, avoiding stale slot links.
func (m *RuntimePartyManager) TransferLeader(byUser uint16, byGeneration uint64, newLeader uint16, newLeaderGeneration uint64) RuntimePartyResult {
	if m == nil || byUser == 0 || newLeader == 0 || byGeneration == 0 || newLeaderGeneration == 0 {
		return runtimePartyFailure("invalid_leader_transfer")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	party, leader := m.partyMemberLocked(byUser)
	if party == nil || leader == nil || leader.SessionGeneration != byGeneration {
		return runtimePartyFailure("stale_session")
	}
	if party.leader != byUser {
		return runtimePartyFailure("not_leader")
	}
	next := party.member(newLeader)
	if next == nil || next.SessionGeneration != newLeaderGeneration {
		return runtimePartyFailure("stale_target_session")
	}
	retired := snapshotRuntimeParty(party)
	members := party.membersBySlot()
	delete(m.parties, party.id)
	for _, member := range members {
		delete(m.byUser, member.UserID)
	}
	rebuilt := m.rebuildLocked(party, *next, members)
	m.parties[rebuilt.id] = rebuilt
	for _, member := range rebuilt.membersBySlot() {
		m.byUser[member.UserID] = rebuilt.id
	}
	result := runtimePartySuccess(rebuilt, newLeader)
	result.Previous = &retired
	result.LeaderMoved = true
	result.Retired = &retired
	return result
}

func (m *RuntimePartyManager) RecordInvite(invitee uint16, inviteeGeneration uint64, inviter uint16, inviterGeneration uint64, partyID uint16, mode byte) bool {
	if m == nil || invitee == 0 || inviter == 0 || invitee == inviter || inviteeGeneration == 0 || inviterGeneration == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invites[invitee] = runtimePartyInvite{invitee: invitee, inviteeGeneration: inviteeGeneration, inviter: inviter, inviterGeneration: inviterGeneration, partyID: partyID, mode: mode}
	return true
}

func (m *RuntimePartyManager) ConsumeInvite(invitee uint16, inviteeGeneration uint64, inviter uint16, inviterGeneration uint64, mode byte) (uint16, bool) {
	if m == nil {
		return 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	invite, ok := m.invites[invitee]
	if !ok || invite.inviteeGeneration != inviteeGeneration || invite.inviter != inviter || invite.inviterGeneration != inviterGeneration || invite.mode != mode {
		return 0, false
	}
	delete(m.invites, invitee)
	return invite.partyID, true
}

func (m *RuntimePartyManager) UpdateSettings(userID uint16, generation uint64, settings alignedcmd.PartyState) RuntimePartyResult {
	if m == nil || userID == 0 || generation == 0 {
		return runtimePartyFailure("invalid_settings")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	party, member := m.partyMemberLocked(userID)
	if party == nil || member == nil {
		return runtimePartyFailure("not_in_party")
	}
	if member.SessionGeneration != generation {
		return runtimePartyFailure("stale_session")
	}
	if party.leader != userID {
		return runtimePartyFailure("not_leader")
	}
	settings = normalizeRuntimePartySettings(settings)
	settings.PartyID = int(party.id)
	settings.UserID = party.leader
	settings.IsLeader = true
	party.settings = settings
	return runtimePartySuccess(party, userID)
}

func (m *RuntimePartyManager) OnDisconnected(userID uint16, generation uint64) RuntimePartyResult {
	return m.Leave(userID, generation)
}

// RebindSession moves an existing member to a replacement game-session
// generation without changing its party, slot or leader. Channel reconnects
// temporarily have two sockets for one character; accepting packets from the
// retired generation after this point would let it remove the replacement
// member, so this is deliberately generation-conditional.
func (m *RuntimePartyManager) RebindSession(userID uint16, priorGeneration uint64, replacement RuntimePartyMember) RuntimePartyResult {
	if m == nil || userID == 0 || priorGeneration == 0 || !validRuntimePartyMember(replacement) || replacement.UserID != userID {
		return runtimePartyFailure("invalid_session_rebind")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	party, member := m.partyMemberLocked(userID)
	if party == nil || member == nil {
		m.clearInvitesForLocked(userID, priorGeneration)
		return runtimePartyFailure("not_in_party")
	}
	if member.SessionGeneration != priorGeneration {
		return runtimePartyFailure("stale_session")
	}
	replacement.Slot = member.Slot
	replacement.State = normalizeRuntimePartyMember(replacement.State, userID)
	*member = replacement
	m.clearInvitesForLocked(userID, priorGeneration)
	return runtimePartySuccess(party, userID)
}

// Reposition moves a non-leader member between persistent party slots. The
// wire command is issued by the leader; slot zero remains leader-owned because
// the current EXE derives leadership from its first roster entry.
func (m *RuntimePartyManager) Reposition(byUser uint16, byGeneration uint64, fromSlot uint8, toSlot uint8) RuntimePartyResult {
	if m == nil || byUser == 0 || byGeneration == 0 || fromSlot >= currentRuntimePartyMaxMembers || toSlot >= currentRuntimePartyMaxMembers || fromSlot == toSlot {
		return runtimePartyFailure("invalid_reposition")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	party, leader := m.partyMemberLocked(byUser)
	if party == nil || leader == nil || leader.SessionGeneration != byGeneration {
		return runtimePartyFailure("stale_session")
	}
	if party.leader != byUser {
		return runtimePartyFailure("not_leader")
	}
	if fromSlot == 0 || toSlot == 0 || party.members[fromSlot] == nil {
		return runtimePartyFailure("invalid_source_slot")
	}
	party.members[fromSlot], party.members[toSlot] = party.members[toSlot], party.members[fromSlot]
	if party.members[fromSlot] != nil {
		party.members[fromSlot].Slot = fromSlot
	}
	if party.members[toSlot] != nil {
		party.members[toSlot].Slot = toSlot
	}
	return runtimePartySuccess(party, byUser)
}

func (m *RuntimePartyManager) nextPartyIDLocked() uint16 {
	for attempts := 0; attempts < int(^uint16(0))-1; attempts++ {
		id := m.nextID
		m.nextID++
		if m.nextID == 0 || m.nextID == ^uint16(0) {
			m.nextID = 1
		}
		if id != 0 && id != ^uint16(0) && m.parties[id] == nil {
			return id
		}
	}
	return 0
}

func (m *RuntimePartyManager) rebuildLocked(source *runtimeParty, leader RuntimePartyMember, members []RuntimePartyMember) *runtimeParty {
	rebuilt := &runtimeParty{id: m.nextPartyIDLocked(), leader: leader.UserID, settings: cloneRuntimePartySettings(source.settings)}
	rebuilt.settings.PartyID = int(rebuilt.id)
	rebuilt.settings.UserID = leader.UserID
	rebuilt.settings.IsLeader = true
	_ = rebuilt.add(leader)
	for _, member := range members {
		if member.UserID != leader.UserID {
			_ = rebuilt.add(member)
		}
	}
	return rebuilt
}

func (m *RuntimePartyManager) partyMemberLocked(userID uint16) (*runtimeParty, *RuntimePartyMember) {
	partyID := m.byUser[userID]
	party := m.parties[partyID]
	if party == nil {
		return nil, nil
	}
	return party, party.member(userID)
}

func (m *RuntimePartyManager) clearInvitesForLocked(userID uint16, generation uint64) {
	for invitee, invite := range m.invites {
		if (invitee == userID && (generation == 0 || invite.inviteeGeneration == generation)) ||
			(invite.inviter == userID && (generation == 0 || invite.inviterGeneration == generation)) {
			delete(m.invites, invitee)
		}
	}
}

func (p *runtimeParty) count() int {
	count := 0
	for _, member := range p.members {
		if member != nil {
			count++
		}
	}
	return count
}

func (p *runtimeParty) full() bool {
	limit := p.settings.MaxMembers
	if limit == 0 || limit > currentRuntimePartyMaxMembers {
		limit = currentRuntimePartyMaxMembers
	}
	return p.count() >= int(limit)
}

func (p *runtimeParty) member(userID uint16) *RuntimePartyMember {
	for _, member := range p.members {
		if member != nil && member.UserID == userID {
			return member
		}
	}
	return nil
}

func (p *runtimeParty) add(member RuntimePartyMember) bool {
	if p == nil || !validRuntimePartyMember(member) || p.full() || p.member(member.UserID) != nil {
		return false
	}
	for slot := range p.members {
		if p.members[slot] != nil {
			continue
		}
		member.Slot = byte(slot)
		member.State = normalizeRuntimePartyMember(member.State, member.UserID)
		p.members[slot] = &member
		return true
	}
	return false
}

func (p *runtimeParty) remove(userID uint16) bool {
	for slot, member := range p.members {
		if member != nil && member.UserID == userID {
			p.members[slot] = nil
			return true
		}
	}
	return false
}

func (p *runtimeParty) membersBySlot() []RuntimePartyMember {
	result := make([]RuntimePartyMember, 0, currentRuntimePartyMaxMembers)
	for _, member := range p.members {
		if member != nil {
			result = append(result, cloneRuntimePartyMember(*member))
		}
	}
	return result
}

func snapshotRuntimeParty(p *runtimeParty) RuntimePartySnapshot {
	if p == nil {
		return RuntimePartySnapshot{}
	}
	return RuntimePartySnapshot{ID: p.id, Leader: p.leader, Settings: cloneRuntimePartySettings(p.settings), Members: p.membersBySlot()}
}

func (s RuntimePartySnapshot) StateFor(userID uint16) alignedcmd.PartyState {
	if s.ID == 0 || !s.contains(userID) {
		return alignedcmd.PartyState{}
	}
	state := cloneRuntimePartySettings(s.Settings)
	state.PartyID = int(s.ID)
	state.UserID = s.Leader
	state.IsLeader = s.Leader == userID
	state.Members = make([]alignedcmd.PartyMemberState, 0, len(s.Members))
	for _, member := range s.Members {
		state.Members = append(state.Members, normalizeRuntimePartyMember(member.State, member.UserID))
	}
	return state
}

func (s RuntimePartySnapshot) contains(userID uint16) bool {
	for _, member := range s.Members {
		if member.UserID == userID {
			return true
		}
	}
	return false
}

func runtimePartySuccess(p *runtimeParty, target uint16) RuntimePartyResult {
	return RuntimePartyResult{OK: true, Party: snapshotRuntimeParty(p), TargetUser: target}
}

func runtimePartyFailure(reason string) RuntimePartyResult { return RuntimePartyResult{Reason: reason} }

func validRuntimePartyMember(member RuntimePartyMember) bool {
	return member.UserID != 0 && member.UserID != ^uint16(0) && member.SessionGeneration != 0
}

func normalizeRuntimePartySettings(state alignedcmd.PartyState) alignedcmd.PartyState {
	state = cloneRuntimePartySettings(state)
	if state.MaxMembers == 0 || state.MaxMembers > currentRuntimePartyMaxMembers {
		state.MaxMembers = currentRuntimePartyMaxMembers
	}
	state.Members = nil
	return state
}

func cloneRuntimePartySettings(state alignedcmd.PartyState) alignedcmd.PartyState {
	state.NameBytes = append([]byte(nil), state.NameBytes...)
	state.Members = append([]alignedcmd.PartyMemberState(nil), state.Members...)
	return state
}

func normalizeRuntimePartyMember(member alignedcmd.PartyMemberState, userID uint16) alignedcmd.PartyMemberState {
	member.UserID = userID
	if member.UserState == 0 {
		member.UserState = 1
	}
	if member.HPPercent == 0 {
		member.HPPercent = 100
	}
	if member.MPPercent == 0 {
		member.MPPercent = 100
	}
	return member
}

func cloneRuntimePartyMember(member RuntimePartyMember) RuntimePartyMember {
	member.State = normalizeRuntimePartyMember(member.State, member.UserID)
	return member
}
