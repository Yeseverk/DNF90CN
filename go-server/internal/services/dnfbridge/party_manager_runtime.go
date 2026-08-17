package dnfbridge

import (
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/party"
)

// runtimePartyManagerForService is intentionally lazy because many protocol
// tests construct Service directly. Production initialization still creates it
// with Service, so all live party mutations share one central manager.
func (s *Service) runtimePartyManagerForService() *party.RuntimePartyManager {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimePartyManager == nil {
		s.runtimePartyManager = party.NewRuntimePartyManager()
	}
	return s.runtimePartyManager
}

func (s *Service) runtimePartyMemberForIdentity(identity boundGameSessionCharacter) (party.RuntimePartyMember, bool) {
	if identity.session == nil || identity.character == 0 || identity.generation == 0 || !s.boundGameSessionCharacterCurrent(identity) {
		return party.RuntimePartyMember{}, false
	}
	state := runtimePartyStateSnapshot(identity.session)
	member := runtimePartyMember(identity.session, state)
	return party.RuntimePartyMember{
		UserID:            identity.character,
		SessionGeneration: identity.generation,
		State:             member,
	}, true
}

func (s *Service) publishRuntimePartySnapshot(snapshot party.RuntimePartySnapshot) alignedcmd.PartyState {
	if s == nil || snapshot.ID == 0 {
		return alignedcmd.PartyState{}
	}
	for _, member := range snapshot.Members {
		identity, ok := s.onlineGameSessionCharacterSnapshot(member.UserID)
		if !ok || identity.generation != member.SessionGeneration {
			continue
		}
		storeRuntimePartyState(identity.session, snapshot.StateFor(member.UserID))
	}
	return snapshot.StateFor(snapshot.Leader)
}

// publishRuntimePartyMembershipSnapshot keeps a one-member manager lobby
// available for the next invite, but deliberately removes its client-facing
// party projection after a leave/kick/disconnect mutation.  The current EXE
// treats a nonzero cached party id as dungeon leader authority even when its
// roster only has one row.  Replaying such a singleton after a successful
// leave is what left the player unable to enter alone.
func (s *Service) publishRuntimePartyMembershipSnapshot(snapshot party.RuntimePartySnapshot) alignedcmd.PartyState {
	state := s.publishRuntimePartySnapshot(snapshot)
	if len(snapshot.Members) > 1 {
		return state
	}
	for _, member := range snapshot.Members {
		s.clearRuntimePartyProjection(member.UserID, member.SessionGeneration)
	}
	return alignedcmd.PartyState{}
}

func (s *Service) clearRuntimePartyProjection(userID uint16, generation uint64) {
	identity, ok := s.onlineGameSessionCharacterSnapshot(userID)
	if !ok || identity.generation != generation {
		return
	}
	storeRuntimePartyState(identity.session, alignedcmd.PartyState{})
}

func (s *Service) createOrJoinManagedRuntimeParty(joiner, anchor *gameSession) (alignedcmd.PartyState, party.RuntimePartyResult, bool) {
	joinerIdentity, joinerOK := s.boundGameSessionCharacterSnapshot(joiner)
	anchorIdentity, anchorOK := s.boundGameSessionCharacterSnapshot(anchor)
	if !joinerOK || !anchorOK || joinerIdentity.character == anchorIdentity.character {
		return alignedcmd.PartyState{}, party.RuntimePartyResult{Reason: "invalid_session"}, false
	}
	manager := s.runtimePartyManagerForService()
	joinerMember, joinerMemberOK := s.runtimePartyMemberForIdentity(joinerIdentity)
	anchorMember, anchorMemberOK := s.runtimePartyMemberForIdentity(anchorIdentity)
	if manager == nil || !joinerMemberOK || !anchorMemberOK {
		return alignedcmd.PartyState{}, party.RuntimePartyResult{Reason: "stale_session"}, false
	}
	// SET_PARTY_INFO (op12) is a local client-window operation. It can create
	// a one-member projection on the invitee before that player accepts an
	// incoming invite, but it must never steal leadership from the inviter.
	// Resolve/import the packet target first: accepting or directly entering a
	// target always joins the target's party, creating that target's lobby when
	// it does not yet own one. A joiner's old central party is then detached by
	// RuntimePartyManager.Join as one atomic transition.
	anchorParty, anchorFound := s.bootstrapManagedRuntimePartyForSession(anchor)
	var result party.RuntimePartyResult
	if anchorFound {
		// Import a pre-central joiner's legacy projection only after the target
		// is known to own a party. This retains a real old-party survivor
		// transition during deployment, while a partyless target still wins over
		// an invitee's transient op12 singleton.
		if _, joinerManaged := manager.SnapshotByUser(joinerIdentity.character, joinerIdentity.generation); !joinerManaged &&
			runtimePartyStateSnapshot(joiner).PartyID > 0 {
			if imported, importedOK := s.bootstrapManagedRuntimePartyForSession(joiner); importedOK && imported.ID == anchorParty.ID {
				return s.publishRuntimePartySnapshot(anchorParty), party.RuntimePartyResult{OK: true, Party: anchorParty, TargetUser: joinerIdentity.character}, true
			}
		}
		result = manager.Join(anchorParty.ID, joinerMember)
	} else {
		// EnsureLeader is idempotent for concurrent invite/entry packets. It
		// never detaches a just-created target lobby as Create would.
		created := manager.EnsureLeader(anchorMember, runtimePartyStateSnapshot(anchor))
		if !created.OK {
			return alignedcmd.PartyState{}, created, false
		}
		result = manager.Join(created.Party.ID, joinerMember)
		if result.PriorLeave == nil && created.PriorLeave != nil {
			result.PriorLeave = created.PriorLeave
		}
	}
	if !result.OK {
		return alignedcmd.PartyState{}, result, false
	}
	if err := s.reconcileManagedPriorPartyLeave(result.PriorLeave, joinerIdentity.character); err != nil {
		result.Reason = "prior_party_projection_failed: " + err.Error()
		return alignedcmd.PartyState{}, result, false
	}
	state := s.publishRuntimePartySnapshot(result.Party)
	if state.PartyID <= 0 {
		return alignedcmd.PartyState{}, party.RuntimePartyResult{Reason: "party_projection_failed"}, false
	}
	return state, result, true
}

// reconcileManagedPriorPartyLeave projects the old half of a cross-party
// join. Membership has already moved under RuntimePartyManager's lock; this
// function only emits the corresponding client cache, relay and roster
// transition for remaining old-party members. Without it, a successful join
// could leave those survivors looking at a stale team until their next map
// change.
func (s *Service) reconcileManagedPriorPartyLeave(prior *party.RuntimePartyResult, movedUser uint16) error {
	if prior == nil || !prior.OK || prior.Previous == nil || prior.Previous.ID == 0 {
		return nil
	}
	previous := *prior.Previous
	if prior.Disbanded || prior.Party.ID == 0 {
		s.closeRuntimePartyUDPRelay(int(previous.ID))
		return nil
	}
	nextState := s.publishRuntimePartyMembershipSnapshot(prior.Party)
	if prior.Retired != nil {
		s.closeRuntimePartyUDPRelay(int(previous.ID))
		for _, member := range previous.Members {
			if member.UserID == movedUser {
				continue
			}
			session, online := s.onlineGameSession(member.UserID)
			if !online {
				continue
			}
			if err := s.sendRuntimePartySnapshot(session, alignedcmd.PartyState{}); err != nil {
				return err
			}
		}
		s.syncRuntimePartyUDPRelay(nextState)
		return s.sendManagedRuntimePartySnapshots(nextState)
	}
	if nextState.PartyID <= 0 {
		s.closeRuntimePartyUDPRelay(int(previous.ID))
	} else {
		s.syncRuntimePartyUDPRelay(nextState)
	}
	return s.broadcastRuntimePartyMemberRemoved(previous.StateFor(previous.Leader), nextState, movedUser, nil)
}

// bootstrapManagedRuntimePartyForSession imports the old per-session
// projection exactly once when an already-open client window predates the
// central manager (including channel reconnect tests). From that point on the
// projection is only a client cache; every membership mutation goes through
// RuntimePartyManager. Production joins normally never take this path because
// op12 now creates the central party before its first roster packet is sent.
func (s *Service) bootstrapManagedRuntimePartyForSession(session *gameSession) (party.RuntimePartySnapshot, bool) {
	identity, bound := s.boundGameSessionCharacterSnapshot(session)
	if !bound {
		return party.RuntimePartySnapshot{}, false
	}
	manager := s.runtimePartyManagerForService()
	if manager == nil {
		return party.RuntimePartySnapshot{}, false
	}
	if snapshot, found := manager.SnapshotByUser(identity.character, identity.generation); found {
		return snapshot, true
	}
	state := runtimePartyStateSnapshot(session)
	if state.PartyID <= 0 {
		return party.RuntimePartySnapshot{}, false
	}
	leaderID := state.UserID
	if leaderID == 0 {
		leaderID = identity.character
	}
	leaderIdentity, leaderOnline := s.onlineGameSessionCharacterSnapshot(leaderID)
	if !leaderOnline {
		leaderIdentity = identity
		leaderID = identity.character
	}
	leaderMember, leaderOK := s.runtimePartyMemberForIdentity(leaderIdentity)
	if !leaderOK {
		return party.RuntimePartySnapshot{}, false
	}
	state.UserID = leaderID
	state.IsLeader = true
	created := manager.EnsureLeader(leaderMember, state)
	if !created.OK {
		return party.RuntimePartySnapshot{}, false
	}
	snapshot := created.Party
	for _, cachedMember := range runtimePartyMembers(state) {
		if cachedMember.UserID == 0 || cachedMember.UserID == leaderID {
			continue
		}
		memberIdentity, online := s.onlineGameSessionCharacterSnapshot(cachedMember.UserID)
		if !online {
			continue
		}
		member, memberOK := s.runtimePartyMemberForIdentity(memberIdentity)
		if !memberOK {
			continue
		}
		member.State = cachedMember
		joined := manager.Join(snapshot.ID, member)
		if joined.OK {
			snapshot = joined.Party
		}
	}
	s.publishRuntimePartySnapshot(snapshot)
	return snapshot, true
}

func (s *Service) leaveManagedRuntimeParty(identity boundGameSessionCharacter) (alignedcmd.PartyState, party.RuntimePartyResult, bool) {
	if !s.boundGameSessionCharacterCurrent(identity) {
		return alignedcmd.PartyState{}, party.RuntimePartyResult{Reason: "stale_session"}, false
	}
	manager := s.runtimePartyManagerForService()
	if manager == nil {
		return alignedcmd.PartyState{}, party.RuntimePartyResult{Reason: "party_manager_unavailable"}, false
	}
	if _, found := manager.SnapshotByUser(identity.character, identity.generation); !found {
		s.bootstrapManagedRuntimePartyForSession(identity.session)
	}
	result := manager.Leave(identity.character, identity.generation)
	if !result.OK {
		return alignedcmd.PartyState{}, result, false
	}
	s.clearRuntimePartyProjection(identity.character, identity.generation)
	if result.Disbanded || result.Party.ID == 0 {
		return alignedcmd.PartyState{}, result, true
	}
	return s.publishRuntimePartyMembershipSnapshot(result.Party), result, true
}

func (s *Service) ensureManagedRuntimePartyForSession(session *gameSession) (alignedcmd.PartyState, error) {
	identity, ok := s.boundGameSessionCharacterSnapshot(session)
	if !ok {
		return alignedcmd.PartyState{}, nil
	}
	manager := s.runtimePartyManagerForService()
	if manager == nil {
		return alignedcmd.PartyState{}, fmt.Errorf("runtime party manager unavailable")
	}
	state := runtimePartyStateSnapshot(session)
	if state.PartyID <= 0 {
		return state, nil
	}
	if _, memberOK := s.runtimePartyMemberForIdentity(identity); !memberOK {
		return alignedcmd.PartyState{}, nil
	}
	if snapshot, exists := manager.SnapshotByUser(identity.character, identity.generation); exists {
		result := manager.UpdateSettings(identity.character, identity.generation, state)
		if result.OK {
			return s.publishRuntimePartySnapshot(result.Party), nil
		}
		s.publishRuntimePartySnapshot(snapshot)
		return snapshot.StateFor(identity.character), nil
	}
	snapshot, created := s.bootstrapManagedRuntimePartyForSession(session)
	if !created {
		return alignedcmd.PartyState{}, fmt.Errorf("create runtime party")
	}
	return s.publishRuntimePartySnapshot(snapshot), nil
}

func (s *Service) hasManagedRuntimeParty(session *gameSession) bool {
	identity, bound := s.boundGameSessionCharacterSnapshot(session)
	if !bound {
		return false
	}
	if snapshot, found := s.runtimePartyManagerForService().SnapshotByUser(identity.character, identity.generation); found {
		return snapshot.ID != 0
	}
	if runtimePartyStateSnapshot(session).PartyID <= 0 {
		return false
	}
	_, found := s.bootstrapManagedRuntimePartyForSession(session)
	return found
}

// replaceManagedRuntimeParty expresses a higher-level grouping (currently a
// raid attack-party assignment) through the same central party graph. It
// intentionally does not reuse the caller's synthetic PartyID: the manager
// allocates a fresh generation just like ordinary create/leader transfer, then
// callers project the returned snapshot to the current EXE.
func (s *Service) replaceManagedRuntimeParty(state alignedcmd.PartyState) (alignedcmd.PartyState, bool) {
	members := runtimePartyMembers(state)
	if len(members) == 0 {
		return alignedcmd.PartyState{}, false
	}
	leaderID := state.UserID
	if leaderID == 0 {
		leaderID = members[0].UserID
	}
	leaderIdentity, online := s.onlineGameSessionCharacterSnapshot(leaderID)
	if !online {
		return alignedcmd.PartyState{}, false
	}
	leader, leaderOK := s.runtimePartyMemberForIdentity(leaderIdentity)
	if !leaderOK {
		return alignedcmd.PartyState{}, false
	}
	for _, cached := range members {
		if cached.UserID == leaderID {
			leader.State = cached
			break
		}
	}
	manager := s.runtimePartyManagerForService()
	if manager == nil {
		return alignedcmd.PartyState{}, false
	}
	created := manager.Create(leader, state)
	if !created.OK {
		return alignedcmd.PartyState{}, false
	}
	snapshot := created.Party
	for _, cached := range members {
		if cached.UserID == leaderID {
			continue
		}
		identity, online := s.onlineGameSessionCharacterSnapshot(cached.UserID)
		if !online {
			return alignedcmd.PartyState{}, false
		}
		member, memberOK := s.runtimePartyMemberForIdentity(identity)
		if !memberOK {
			return alignedcmd.PartyState{}, false
		}
		member.State = cached
		joined := manager.Join(snapshot.ID, member)
		if !joined.OK {
			return alignedcmd.PartyState{}, false
		}
		snapshot = joined.Party
	}
	return s.publishRuntimePartySnapshot(snapshot), true
}

func (s *Service) publishAllManagedRuntimeParties() {
	manager := s.runtimePartyManagerForService()
	if manager == nil {
		return
	}
	for _, snapshot := range manager.Snapshots() {
		s.publishRuntimePartySnapshot(snapshot)
	}
}

func (s *Service) sendManagedRuntimePartySnapshots(state alignedcmd.PartyState) error {
	if state.PartyID <= 0 || state.PartyID >= int(^uint16(0)) {
		return nil
	}
	manager := s.runtimePartyManagerForService()
	snapshot, found := manager.SnapshotByID(uint16(state.PartyID))
	if !found {
		return fmt.Errorf("runtime party %d is no longer current", state.PartyID)
	}
	for _, member := range snapshot.Members {
		identity, online := s.onlineGameSessionCharacterSnapshot(member.UserID)
		if !online || identity.generation != member.SessionGeneration {
			continue
		}
		if err := s.sendRuntimePartySnapshot(identity.session, snapshot.StateFor(member.UserID)); err != nil {
			return err
		}
	}
	return nil
}
