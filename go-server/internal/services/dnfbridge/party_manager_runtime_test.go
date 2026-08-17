package dnfbridge

import (
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/channelcatalog"
)

func TestManagedPartyCrossJoinRefreshesPriorPartySurvivor(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	partyA := alignedcmd.PartyState{
		PartyID: 77, UserID: 1001, IsLeader: true, MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 1002, UserState: 1, HPPercent: 90, MPPercent: 80},
		},
	}
	partyB := alignedcmd.PartyState{
		PartyID: 88, UserID: 1003, IsLeader: true, MaxMembers: 4,
		Members: []alignedcmd.PartyMemberState{{UserID: 1003, UserState: 1, HPPercent: 100, MPPercent: 100}},
	}
	leaderA := &gameSession{conn: &bufferConn{}, channel: channel, selectedCharacterID: 1001, party: partySessionState{state: partyA}}
	joiner := &gameSession{conn: &bufferConn{}, channel: channel, selectedCharacterID: 1002, party: partySessionState{state: partyA}}
	leaderB := &gameSession{conn: &bufferConn{}, channel: channel, selectedCharacterID: 1003, party: partySessionState{state: partyB}}
	service.bindGameSessionCharacter(leaderA, 1001)
	service.bindGameSessionCharacter(joiner, 1002)
	service.bindGameSessionCharacter(leaderB, 1003)

	state, result, joined := service.createOrJoinManagedRuntimeParty(joiner, leaderB)
	if !joined || !result.OK || result.PriorLeave == nil || !result.PriorLeave.OK {
		t.Fatalf("cross-party join=%t result=%+v", joined, result)
	}
	if state.UserID != 1003 || len(runtimePartyMembers(state)) != 2 {
		t.Fatalf("target party projection=%+v", state)
	}
	if old := runtimePartyStateSnapshot(leaderA); old.PartyID != 0 {
		t.Fatalf("prior party survivor singleton projection was not cleared: %+v", old)
	}
	if next := runtimePartyStateSnapshot(joiner); next.UserID != 1003 || len(runtimePartyMembers(next)) != 2 {
		t.Fatalf("joiner projection=%+v", next)
	}
	if snapshot, found := service.runtimePartyManagerForService().SnapshotByUser(1001, leaderA.characterGeneration); !found || len(snapshot.Members) != 1 {
		t.Fatalf("prior manager party=%+v found=%v", snapshot, found)
	}
}

func TestManagedPartyInviteTargetKeepsLeadershipOverAcceptorTransientSetPartyInfo(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	inviter := &gameSession{conn: &bufferConn{}, channel: channel, selectedCharacterID: 1001}
	acceptor := &gameSession{
		conn:                &bufferConn{},
		channel:             channel,
		selectedCharacterID: 1002,
		party: partySessionState{state: alignedcmd.PartyState{
			PartyID:    1002,
			UserID:     1002,
			IsLeader:   true,
			MaxMembers: 4,
			Members: []alignedcmd.PartyMemberState{{
				UserID: 1002, UserState: 1, HPPercent: 100, MPPercent: 100,
			}},
		}},
	}
	service.bindGameSessionCharacter(inviter, 1001)
	service.bindGameSessionCharacter(acceptor, 1002)

	// op12 can establish this local one-member cache before the accept button.
	// It is deliberately imported here to model that order; accepting the
	// inviter must still rebuild under character 1001.
	if _, err := service.ensureManagedRuntimePartyForSession(acceptor); err != nil {
		t.Fatalf("bootstrap acceptor transient party: %v", err)
	}
	state, result, joined := service.createOrJoinManagedRuntimeParty(acceptor, inviter)
	if !joined || !result.OK {
		t.Fatalf("accept invite joined=%t result=%+v", joined, result)
	}
	if state.UserID != 1001 || !state.IsLeader || len(runtimePartyMembers(state)) != 2 {
		t.Fatalf("invite target did not retain leadership: %+v", state)
	}
	if members := runtimePartyMembers(state); members[0].UserID != 1001 || members[1].UserID != 1002 {
		t.Fatalf("party order=%+v, want inviter then acceptor", members)
	}
	inviterIdentity, ok := service.boundGameSessionCharacterSnapshot(inviter)
	if !ok {
		t.Fatal("inviter identity was not bound")
	}
	snapshot, found := service.runtimePartyManagerForService().SnapshotByUser(inviterIdentity.character, inviterIdentity.generation)
	if !found || snapshot.Leader != 1001 || len(snapshot.Members) != 2 {
		t.Fatalf("authoritative party snapshot=%+v found=%t", snapshot, found)
	}
}

func TestManagedPartyReconnectRebindsMemberGeneration(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	channel := channelcatalog.Channel{ID: 38, Type: 1, Name: "ch.38", Port: 10038}
	leader := &gameSession{conn: &bufferConn{}, channel: channel, selectedCharacterID: 1001}
	member := &gameSession{conn: &bufferConn{}, channel: channel, selectedCharacterID: 1002}
	service.bindGameSessionCharacter(leader, 1001)
	service.bindGameSessionCharacter(member, 1002)
	if _, result, joined := service.createOrJoinManagedRuntimeParty(member, leader); !joined || !result.OK {
		t.Fatalf("initial join=%t result=%+v", joined, result)
	}
	oldGeneration := member.characterGeneration
	replacement := &gameSession{conn: &bufferConn{}, channel: channel, selectedCharacterID: 1002}
	service.bindGameSessionCharacter(replacement, 1002)
	if replacement.characterGeneration == oldGeneration {
		t.Fatalf("replacement generation=%d old=%d", replacement.characterGeneration, oldGeneration)
	}
	if _, stale := service.runtimePartyManagerForService().SnapshotByUser(1002, oldGeneration); stale {
		t.Fatal("retired session generation remained in central party")
	}
	snapshot, current := service.runtimePartyManagerForService().SnapshotByUser(1002, replacement.characterGeneration)
	if !current || len(snapshot.Members) != 2 {
		t.Fatalf("replacement party=%+v current=%v", snapshot, current)
	}
	if projected := runtimePartyStateSnapshot(replacement); projected.PartyID == 0 || len(runtimePartyMembers(projected)) != 2 {
		t.Fatalf("replacement projection=%+v", projected)
	}
}
