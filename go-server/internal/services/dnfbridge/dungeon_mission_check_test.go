package dnfbridge

import (
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestHandleDungeonMissionCheckSuccessRecognizesCompletedTutorialWithoutInventingReward(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		job:              "12",
		dungeonReference: tutorialScopeKnightFDungeonReference,
		mapDirectory:     tutorialScopeKnightFMapDirectory,
	})
	if err := runtime.Session.CompleteCurrentRoom(); err != nil {
		t.Fatal(err)
	}
	runtime.bossDieCheckAccepted = true
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "op560-deferred-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}

	request, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketDungeonMissionCheckSuccess),
		nil,
		0,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, request); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("op560 without reward source emitted invented response=%x", conn.write.Bytes())
	}
	if session.dungeon.runtime != runtime || !runtime.bossDieCheckAccepted {
		t.Fatalf("deferred op560 changed runtime owner=%p accepted=%t", session.dungeon.runtime, runtime.bossDieCheckAccepted)
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunCompleted {
		t.Fatalf("deferred op560 changed run status=%s", status)
	}
}

func TestHandleDungeonMissionCheckSuccessRejectsMalformedOrWrongClass(t *testing.T) {
	tests := []struct {
		name           string
		body           []byte
		classification byte
	}{
		{name: "non-empty body", body: []byte{0}, classification: dnfproto.DefaultChannelClassification},
		{name: "wrong class", classification: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{})
			conn := &bufferConn{}
			session := &gameSession{conn: conn, selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
			request, err := dnfproto.BuildChannelPacket(
				uint16(dnfenum.CmdPacketDungeonMissionCheckSuccess),
				test.body,
				0,
				test.classification,
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.handleGameUpper(session, request); err != nil {
				t.Fatal(err)
			}
			if conn.write.Len() != 0 || session.dungeon.runtime != runtime {
				t.Fatalf("rejected op560 wrote=%x runtime=%p", conn.write.Bytes(), session.dungeon.runtime)
			}
			if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunActive {
				t.Fatalf("rejected op560 status=%s want=%s", status, worldmap.DungeonRunActive)
			}
		})
	}
}

func TestHandleDungeonMissionCheckSuccessDefersGenericActiveDungeonWithoutMutation(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		omitTutorialFlag: true,
	})
	conn := &bufferConn{}
	session := &gameSession{conn: conn, selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
	before := runtime.Session.Snapshot()

	if err := service.handleDungeonMissionCheckSuccess(session, nil); err != nil {
		t.Fatal(err)
	}
	after := runtime.Session.Snapshot()
	if conn.write.Len() != 0 || session.dungeon.runtime != runtime {
		t.Fatalf("generic op560 wrote=%x runtime=%p", conn.write.Bytes(), session.dungeon.runtime)
	}
	if after.Run.Status != before.Run.Status || after.Scene.Cleared != before.Scene.Cleared ||
		len(after.Scene.DefeatedObjects) != len(before.Scene.DefeatedObjects) {
		t.Fatalf("generic op560 mutated dungeon before=%+v after=%+v", before, after)
	}
}
