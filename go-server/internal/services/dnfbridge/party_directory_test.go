package dnfbridge

import (
	"bytes"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/party"
)

func TestRuntimePartyDirectoryRecordsPrefersLeaderProjection(t *testing.T) {
	service := &Service{}
	state := alignedcmd.PartyState{
		PartyID:         1001,
		UserID:          1001,
		UserState:       1,
		SelectionID:     0x01020304,
		TargetDungeonID: 0x12e9,
		MaxMembers:      4,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001, UserState: 1},
			{UserID: 1002, UserState: 1},
		},
	}
	leader := &gameSession{
		conn:                &bufferConn{},
		channel:             channelcatalog.Channel{ID: 300},
		selectedCharacterID: 1001,
		party:               partySessionState{state: state},
	}
	member := &gameSession{
		conn:                &bufferConn{},
		channel:             channelcatalog.Channel{ID: 25},
		selectedCharacterID: 1002,
		party:               partySessionState{state: state},
	}
	service.bindGameSessionCharacter(member, 1002)
	service.bindGameSessionCharacter(leader, 1001)

	got := service.runtimePartyDirectoryRecords()
	want := []party.DirectoryRecord{{
		PartyID:     1,
		SelectionID: 0x01020304,
		MemberIDs:   []uint16{1001, 1002},
	}}
	if len(got) != 1 ||
		got[0].PartyID != want[0].PartyID ||
		got[0].SelectionID != want[0].SelectionID ||
		!equalUint16s(got[0].MemberIDs, want[0].MemberIDs) {
		t.Fatalf("directory records = %+v, want %+v", got, want)
	}
}

func TestHandleRuntimePartyDirectoryRefreshSendsAbsoluteOp87Snapshot(t *testing.T) {
	for _, test := range []struct {
		name        string
		requestMode byte
	}{
		{name: "full directory", requestMode: runtimePartyDirectoryRequestModeFull},
		{name: "region summary", requestMode: runtimePartyDirectoryRequestModeRegion},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
			conn := &bufferConn{}
			session := &gameSession{
				conn:                      conn,
				channel:                   channelcatalog.Channel{ID: 12},
				selectedCharacterID:       1001,
				townSceneReadyCharacterID: 1001,
				party: partySessionState{state: alignedcmd.PartyState{
					PartyID:         1001,
					UserID:          1001,
					UserState:       1,
					SelectionID:     0x01020304,
					TargetDungeonID: 0x12e9,
					MaxMembers:      4,
					Members: []alignedcmd.PartyMemberState{
						{UserID: 1001, UserState: 1},
					},
				}},
			}
			service.bindGameSessionCharacter(session, 1001)

			handled, err := service.handleRuntimePartyDirectoryRefresh(session, []byte{test.requestMode})
			if err != nil {
				t.Fatalf("refresh directory: %v", err)
			}
			if !handled {
				t.Fatalf("current EXE op98 request mode %d was not handled", test.requestMode)
			}
			frame, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
			wantBody := party.BuildDirectorySnapshot([]party.DirectoryRecord{{
				PartyID:     1,
				SelectionID: 0x01020304,
				MemberIDs:   []uint16{1001},
			}})
			if frame.Header.Classification != 0 ||
				frame.Header.MsgID != currentPartyDirectoryMsgID ||
				!bytes.Equal(frame.Body, wantBody) {
				t.Fatalf("refresh frame = class %d msg %d body % X, want % X",
					frame.Header.Classification,
					frame.Header.MsgID,
					frame.Body,
					wantBody)
			}
			if len(rest) != 0 {
				t.Fatalf("refresh trailing bytes = %d", len(rest))
			}
		})
	}
}

func TestHandleRuntimePartyDirectoryRefreshRejectsInvalidRequestMode(t *testing.T) {
	for _, test := range []struct {
		name string
		body []byte
	}{
		{name: "empty", body: nil},
		{name: "unknown mode", body: []byte{2}},
		{name: "multiple bytes", body: []byte{runtimePartyDirectoryRequestModeFull, runtimePartyDirectoryRequestModeRegion}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
			conn := &bufferConn{}
			session := &gameSession{
				conn:                      conn,
				channel:                   channelcatalog.Channel{ID: 12},
				selectedCharacterID:       1001,
				townSceneReadyCharacterID: 1001,
			}

			handled, err := service.handleRuntimePartyDirectoryRefresh(session, test.body)
			if err != nil || handled || conn.write.Len() != 0 {
				t.Fatalf("invalid refresh body % X handled=%t err=%v bytes=%d",
					test.body,
					handled,
					err,
					conn.write.Len())
			}
		})
	}
}

func TestHandleRuntimePartyDirectoryJoinMergesServerPartyState(t *testing.T) {
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	channel := channelcatalog.Channel{ID: 12}
	leaderConn := &bufferConn{}
	joinerConn := &bufferConn{}
	leader := &gameSession{
		conn:                      leaderConn,
		channel:                   channel,
		selectedCharacterID:       1001,
		townSceneReadyCharacterID: 1001,
		party: partySessionState{state: alignedcmd.PartyState{
			PartyID:         1001,
			UserID:          1001,
			UserState:       1,
			TargetDungeonID: 0x12e9,
			MaxMembers:      4,
			Members: []alignedcmd.PartyMemberState{
				{UserID: 1001, UserState: 1, HPPercent: 100, MPPercent: 100},
			},
		}},
	}
	joiner := &gameSession{
		conn:                      joinerConn,
		channel:                   channel,
		selectedCharacterID:       1002,
		townSceneReadyCharacterID: 1002,
	}
	service.bindGameSessionCharacter(leader, 1001)
	service.bindGameSessionCharacter(joiner, 1002)

	body := make([]byte, 2)
	binary.LittleEndian.PutUint16(body, 1)
	handled, err := service.handleRuntimePartyDirectoryJoin(joiner, body)
	if err != nil {
		t.Fatalf("join directory party: %v", err)
	}
	if !handled {
		t.Fatal("current EXE op90 directory join was not handled")
	}
	for name, session := range map[string]*gameSession{"leader": leader, "joiner": joiner} {
		state := runtimePartyStateSnapshot(session)
		if state.PartyID != 1 ||
			state.UserID != 1001 ||
			len(runtimePartyMembers(state)) != 2 ||
			!containsRuntimePartyMember(runtimePartyMembers(state), 1001) ||
			!containsRuntimePartyMember(runtimePartyMembers(state), 1002) {
			t.Fatalf("%s party state = %+v", name, state)
		}
	}

	joinAck, rest := splitGameServerUpperPacket(t, joinerConn.write.Bytes())
	if joinAck.Header.Classification != 0 ||
		joinAck.Header.MsgID != currentPartyDirectoryJoin ||
		len(joinAck.Body) != 0 {
		t.Fatalf("join ack = class %d msg %d body % X",
			joinAck.Header.Classification,
			joinAck.Header.MsgID,
			joinAck.Body)
	}
	rest = assertRuntimePartySnapshot(t, rest, 2)
	if len(rest) != 0 {
		t.Fatalf("joiner unsolicited party-directory bytes = %d", len(rest))
	}

	leaderRest := assertRuntimePartySnapshot(t, leaderConn.write.Bytes(), 2)
	if len(leaderRest) != 0 {
		t.Fatalf("leader unsolicited party-directory bytes = %d", len(leaderRest))
	}
}

func equalUint16s(left, right []uint16) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
