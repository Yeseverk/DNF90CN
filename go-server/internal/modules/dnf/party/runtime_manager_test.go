package party

import (
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

func TestRuntimePartyManagerLeaderLeaveRebuildsFreshPartyGeneration(t *testing.T) {
	manager := NewRuntimePartyManager()
	leader := testRuntimePartyMember(101, 1)
	created := manager.Create(leader, alignedcmd.PartyState{MaxMembers: 4})
	if !created.OK || created.Party.ID == 0 {
		t.Fatalf("create result = %+v", created)
	}
	joined := manager.Join(created.Party.ID, testRuntimePartyMember(202, 1))
	if !joined.OK {
		t.Fatalf("join result = %+v", joined)
	}
	left := manager.Leave(101, 1)
	if !left.OK || !left.LeaderMoved || left.Retired == nil {
		t.Fatalf("leader leave result = %+v", left)
	}
	if left.Retired.ID != created.Party.ID || left.Party.ID == created.Party.ID {
		t.Fatalf("party generations old=%d retired=%d new=%d", created.Party.ID, left.Retired.ID, left.Party.ID)
	}
	if left.Party.Leader != 202 || len(left.Party.Members) != 1 || left.Party.Members[0].Slot != 0 {
		t.Fatalf("rebuilt party = %+v", left.Party)
	}
	if _, found := manager.SnapshotByID(created.Party.ID); found {
		t.Fatal("retired party remained addressable")
	}
	if current, found := manager.SnapshotByUser(202, 1); !found || current.ID != left.Party.ID {
		t.Fatalf("survivor lookup = %+v found=%v", current, found)
	}
}

func TestRuntimePartyManagerMaintainsStableSlotsUntilLeaderRebuild(t *testing.T) {
	manager := NewRuntimePartyManager()
	created := manager.Create(testRuntimePartyMember(101, 1), alignedcmd.PartyState{MaxMembers: 4})
	for _, userID := range []uint16{202, 303, 404} {
		if result := manager.Join(created.Party.ID, testRuntimePartyMember(userID, 1)); !result.OK {
			t.Fatalf("join %d = %+v", userID, result)
		}
	}
	if result := manager.Leave(202, 1); !result.OK || result.LeaderMoved {
		t.Fatalf("nonleader leave = %+v", result)
	}
	snapshot, found := manager.SnapshotByUser(303, 1)
	if !found {
		t.Fatal("remaining party not found")
	}
	if got := runtimePartySlot(snapshot, 303); got != 2 {
		t.Fatalf("member 303 slot = %d, want preserved slot 2", got)
	}
	if got := runtimePartySlot(snapshot, 404); got != 3 {
		t.Fatalf("member 404 slot = %d, want preserved slot 3", got)
	}
	if result := manager.Join(snapshot.ID, testRuntimePartyMember(505, 1)); !result.OK {
		t.Fatalf("join replacement = %+v", result)
	}
	snapshot, _ = manager.SnapshotByUser(505, 1)
	if got := runtimePartySlot(snapshot, 505); got != 1 {
		t.Fatalf("replacement slot = %d, want released slot 1", got)
	}
}

func TestRuntimePartyManagerBindsInvitesAndMutationsToSessionGeneration(t *testing.T) {
	manager := NewRuntimePartyManager()
	created := manager.Create(testRuntimePartyMember(101, 9), alignedcmd.PartyState{MaxMembers: 4})
	if !manager.RecordInvite(202, 7, 101, 9, created.Party.ID, 0) {
		t.Fatal("record invite failed")
	}
	if _, ok := manager.ConsumeInvite(202, 6, 101, 9, 0); ok {
		t.Fatal("retired invitee session consumed pending invite")
	}
	partyID, ok := manager.ConsumeInvite(202, 7, 101, 9, 0)
	if !ok || partyID != created.Party.ID {
		t.Fatalf("consume invite party=%d ok=%v", partyID, ok)
	}
	if result := manager.Join(created.Party.ID, testRuntimePartyMember(202, 7)); !result.OK {
		t.Fatalf("join result = %+v", result)
	}
	if result := manager.Kick(101, 8, 202, 7); result.Reason != "stale_session" {
		t.Fatalf("stale leader kick = %+v", result)
	}
	if result := manager.Kick(101, 9, 202, 6); result.Reason != "stale_target_session" {
		t.Fatalf("stale target kick = %+v", result)
	}
}

func TestRuntimePartyManagerMovesJoinerOutOfPriorPartyAndReportsPriorLeave(t *testing.T) {
	manager := NewRuntimePartyManager()
	first := manager.Create(testRuntimePartyMember(101, 1), alignedcmd.PartyState{MaxMembers: 4})
	if result := manager.Join(first.Party.ID, testRuntimePartyMember(202, 1)); !result.OK {
		t.Fatalf("join first party = %+v", result)
	}
	second := manager.Create(testRuntimePartyMember(303, 1), alignedcmd.PartyState{MaxMembers: 4})
	result := manager.Join(second.Party.ID, testRuntimePartyMember(202, 1))
	if !result.OK || result.PriorLeave == nil || !result.PriorLeave.OK {
		t.Fatalf("cross-party join = %+v", result)
	}
	if old, found := manager.SnapshotByUser(101, 1); !found || len(old.Members) != 1 {
		t.Fatalf("old party = %+v found=%v", old, found)
	}
	if next, found := manager.SnapshotByUser(202, 1); !found || next.ID != second.Party.ID {
		t.Fatalf("new party = %+v found=%v", next, found)
	}
}

func TestRuntimePartyManagerRejectsFifthMemberAndKeepsFourStableSlots(t *testing.T) {
	manager := NewRuntimePartyManager()
	created := manager.Create(testRuntimePartyMember(101, 1), alignedcmd.PartyState{MaxMembers: 4})
	for _, userID := range []uint16{202, 303, 404} {
		if joined := manager.Join(created.Party.ID, testRuntimePartyMember(userID, 1)); !joined.OK {
			t.Fatalf("join %d = %+v", userID, joined)
		}
	}
	if fifth := manager.Join(created.Party.ID, testRuntimePartyMember(505, 1)); fifth.Reason != "party_full" {
		t.Fatalf("fifth join = %+v", fifth)
	}
	snapshot, found := manager.SnapshotByID(created.Party.ID)
	if !found || len(snapshot.Members) != 4 {
		t.Fatalf("party after fifth join = %+v found=%v", snapshot, found)
	}
	for slot, userID := range []uint16{101, 202, 303, 404} {
		if got := runtimePartySlot(snapshot, userID); got != byte(slot) {
			t.Fatalf("member %d slot=%d want=%d", userID, got, slot)
		}
	}
}

func TestRuntimePartyManagerRebindInvalidatesPriorGenerationInvites(t *testing.T) {
	manager := NewRuntimePartyManager()
	created := manager.Create(testRuntimePartyMember(101, 4), alignedcmd.PartyState{MaxMembers: 4})
	if !manager.RecordInvite(202, 7, 101, 4, created.Party.ID, 13) {
		t.Fatal("record invite")
	}
	rebound := manager.RebindSession(101, 4, testRuntimePartyMember(101, 5))
	if !rebound.OK || rebound.Party.ID != created.Party.ID {
		t.Fatalf("rebind = %+v", rebound)
	}
	if _, accepted := manager.ConsumeInvite(202, 7, 101, 4, 13); accepted {
		t.Fatal("invite from retired session was accepted")
	}
	if stale := manager.Leave(101, 4); stale.Reason != "stale_session" {
		t.Fatalf("stale leave = %+v", stale)
	}
	if current, found := manager.SnapshotByUser(101, 5); !found || current.ID != created.Party.ID {
		t.Fatalf("rebound snapshot = %+v found=%v", current, found)
	}
}

func TestRuntimePartyManagerRepositionPreservesLeaderSlot(t *testing.T) {
	manager := NewRuntimePartyManager()
	created := manager.Create(testRuntimePartyMember(101, 1), alignedcmd.PartyState{MaxMembers: 4})
	for _, userID := range []uint16{202, 303} {
		if joined := manager.Join(created.Party.ID, testRuntimePartyMember(userID, 1)); !joined.OK {
			t.Fatalf("join %d = %+v", userID, joined)
		}
	}
	if moved := manager.Reposition(101, 1, 1, 2); !moved.OK {
		t.Fatalf("reposition = %+v", moved)
	}
	snapshot, _ := manager.SnapshotByID(created.Party.ID)
	if runtimePartySlot(snapshot, 101) != 0 || runtimePartySlot(snapshot, 202) != 2 || runtimePartySlot(snapshot, 303) != 1 {
		t.Fatalf("reposition snapshot = %+v", snapshot)
	}
	if invalid := manager.Reposition(101, 1, 0, 1); invalid.Reason != "invalid_source_slot" {
		t.Fatalf("leader reposition = %+v", invalid)
	}
}

func TestRuntimePartyManagerEnsureLeaderIsIdempotent(t *testing.T) {
	manager := NewRuntimePartyManager()
	first := manager.EnsureLeader(testRuntimePartyMember(101, 1), alignedcmd.PartyState{MaxMembers: 4, SelectionID: 7})
	if !first.OK {
		t.Fatalf("first ensure = %+v", first)
	}
	second := manager.EnsureLeader(testRuntimePartyMember(101, 1), alignedcmd.PartyState{MaxMembers: 4, SelectionID: 99})
	if !second.OK || second.Party.ID != first.Party.ID || second.Party.Settings.SelectionID != 7 {
		t.Fatalf("duplicate ensure = %+v first=%+v", second, first)
	}
}

func testRuntimePartyMember(userID uint16, generation uint64) RuntimePartyMember {
	return RuntimePartyMember{UserID: userID, SessionGeneration: generation, State: alignedcmd.PartyMemberState{UserID: userID, UserState: 1, HPPercent: 100, MPPercent: 100}}
}

func runtimePartySlot(snapshot RuntimePartySnapshot, userID uint16) byte {
	for _, member := range snapshot.Members {
		if member.UserID == userID {
			return member.Slot
		}
	}
	return 0xff
}
