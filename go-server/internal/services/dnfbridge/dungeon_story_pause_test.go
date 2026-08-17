package dnfbridge

import (
	"bytes"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestNormalizeLegacyGameBodyStoryPauseStrictTransportBoundary(t *testing.T) {
	semantic := []byte{1, 2}
	cloned := append(append([]byte(nil), semantic...), 0xde, 0xad, 0xbe, 0xef)
	got := normalizeLegacyGameBody(uint16(dnfenum.CmdPacketDungeonEventStoryPause), cloned)
	if !bytes.Equal(got, semantic) {
		t.Fatalf("normalized op191 body=%x want=%x", got, semantic)
	}
	if &got[0] == &cloned[0] {
		t.Fatal("normalized op191 body aliases transport packet")
	}
	for _, body := range [][]byte{semantic, make([]byte, 16), make([]byte, 5)} {
		got = normalizeLegacyGameBody(uint16(dnfenum.CmdPacketDungeonEventStoryPause), body)
		if !bytes.Equal(got, body) || len(got) != len(body) {
			t.Fatalf("op191 boundary len=%d normalized to len=%d body=%x", len(body), len(got), got)
		}
	}
}

func TestHandleCurrentDungeonStoryPauseSendsOwnedCurrentOp170(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{}
	session := &gameSession{
		conn:                connection,
		selectedCharacterID: 19,
		dungeon: dungeonSessionState{runtime: &runtimeDungeonState{
			Character: dnfrepo.CharacterRecord{CharacterID: "19"},
			Session:   &worldmap.DungeonSession{},
		}},
	}
	if err := service.handleCurrentDungeonStoryPause(session, []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing packet bytes=%x", rest)
	}
	if packet.Header.Classification != 0 || packet.Header.MsgID != currentDungeonStoryPauseMsgID {
		t.Fatalf("op170 header class=%d msg=%d", packet.Header.Classification, packet.Header.MsgID)
	}
	if len(packet.Body) != currentDungeonStoryPauseResponseSize ||
		binary.LittleEndian.Uint16(packet.Body[0:2]) != 19 ||
		packet.Body[2] != 1 || packet.Body[3] != 2 {
		t.Fatalf("op170 body=%x", packet.Body)
	}
}

func TestHandleCurrentDungeonStoryPauseSendsTutorialUserStateAfterReadyResumeOnce(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{})
	connection := &bufferConn{}
	session := &gameSession{
		conn:                          connection,
		selectedCharacterID:           99,
		dungeon:                       dungeonSessionState{runtime: runtime},
		postStartMapPlayerStateSent:   true,
		currentFinishLoadingStateSent: true,
	}

	if err := service.handleCurrentDungeonStoryPause(session, []byte{0, 1}); err != nil {
		t.Fatal(err)
	}
	first, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if first.Header.Classification != 0 || first.Header.MsgID != currentDungeonStoryPauseMsgID ||
		!bytes.Equal(first.Body, []byte{99, 0, 0, 1}) || len(rest) != 0 {
		t.Fatalf("tutorial pause op170=%+v body=%x rest=%x", first.Header, first.Body, rest)
	}
	connection.write.Reset()

	if err := service.handleCurrentDungeonStoryPause(session, []byte{1, 1}); err != nil {
		t.Fatal(err)
	}
	resume, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if resume.Header.Classification != 0 || resume.Header.MsgID != currentDungeonStoryPauseMsgID ||
		!bytes.Equal(resume.Body, []byte{99, 0, 1, 1}) {
		t.Fatalf("tutorial resume op170=%+v body=%x", resume.Header, resume.Body)
	}
	userState, rest := splitGameServerUpperPacket(t, rest)
	if userState.Header.Classification != 0 ||
		userState.Header.MsgID != uint16(dnfenum.CmdPacketNotifyUserState) ||
		!bytes.Equal(userState.Body, []byte{1, 99, 0, currentDungeonPlayerUserState}) ||
		len(rest) != 0 || !runtime.tutorialInitialUserStateSent {
		t.Fatalf("tutorial deferred op3=%+v body=%x rest=%x sent=%t",
			userState.Header, userState.Body, rest, runtime.tutorialInitialUserStateSent)
	}

	connection.write.Reset()
	if err := service.handleCurrentDungeonStoryPause(session, []byte{1, 1}); err != nil {
		t.Fatal(err)
	}
	replay, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if replay.Header.MsgID != currentDungeonStoryPauseMsgID || len(rest) != 0 {
		t.Fatalf("tutorial resumed replay sent trailing user state=%x", rest)
	}
}

func TestHandleCurrentDungeonStoryPauseDoesNotSendTutorialUserStateBeforeSceneReady(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{})
	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleCurrentDungeonStoryPause(session, []byte{1, 1}); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if packet.Header.MsgID != currentDungeonStoryPauseMsgID || len(rest) != 0 ||
		runtime.tutorialInitialUserStateSent {
		t.Fatalf("unready tutorial op191 sent user state packet=%+v rest=%x sent=%t",
			packet.Header, rest, runtime.tutorialInitialUserStateSent)
	}
}

func TestHandleCurrentDungeonStoryPauseRejectsOpaqueMalformedAndUnowned(t *testing.T) {
	tests := []struct {
		name    string
		body    []byte
		session *gameSession
	}{
		{
			name: "opaque protected body",
			body: make([]byte, 16),
			session: &gameSession{
				selectedCharacterID: 19,
				dungeon: dungeonSessionState{runtime: &runtimeDungeonState{
					Character: dnfrepo.CharacterRecord{CharacterID: "19"},
					Session:   &worldmap.DungeonSession{},
				}},
			},
		},
		{
			name: "invalid state",
			body: []byte{2, 0},
			session: &gameSession{
				selectedCharacterID: 19,
				dungeon: dungeonSessionState{runtime: &runtimeDungeonState{
					Character: dnfrepo.CharacterRecord{CharacterID: "19"},
					Session:   &worldmap.DungeonSession{},
				}},
			},
		},
		{
			name: "invalid request type",
			body: []byte{1, 3},
			session: &gameSession{
				selectedCharacterID: 19,
				dungeon: dungeonSessionState{runtime: &runtimeDungeonState{
					Character: dnfrepo.CharacterRecord{CharacterID: "19"},
					Session:   &worldmap.DungeonSession{},
				}},
			},
		},
		{
			name: "runtime owner mismatch",
			body: []byte{0, 1},
			session: &gameSession{
				selectedCharacterID: 19,
				dungeon: dungeonSessionState{runtime: &runtimeDungeonState{
					Character: dnfrepo.CharacterRecord{CharacterID: "20"},
					Session:   &worldmap.DungeonSession{},
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connection := &bufferConn{}
			test.session.conn = connection
			if err := (&Service{}).handleCurrentDungeonStoryPause(test.session, test.body); err != nil {
				t.Fatal(err)
			}
			if connection.write.Len() != 0 {
				t.Fatalf("blocked op191 wrote=%x", connection.write.Bytes())
			}
		})
	}
}
