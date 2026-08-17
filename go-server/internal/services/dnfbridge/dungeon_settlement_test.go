package dnfbridge

import (
	"bytes"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestCurrentDungeonPlayResultShapeAcceptsOnlyWriterDerivedLengths(t *testing.T) {
	accepted := make(map[int]struct{})
	for rows := 0; rows <= currentDungeonPlayResultMaximumDynamicRows; rows++ {
		for _, test := range []struct {
			bodyLen  int
			optional bool
		}{
			{bodyLen: currentDungeonPlayResultBaseSize + rows*currentDungeonPlayResultDynamicRowSize},
			{bodyLen: currentDungeonPlayResultOptionalBaseSize + rows*currentDungeonPlayResultDynamicRowSize, optional: true},
		} {
			accepted[test.bodyLen] = struct{}{}
			gotRows, gotOptional, ok := currentDungeonPlayResultShape(make([]byte, test.bodyLen))
			if !ok || gotRows != rows || gotOptional != test.optional {
				t.Fatalf("body_len=%d shape=(%d,%t,%t) want=(%d,%t,true)",
					test.bodyLen, gotRows, gotOptional, ok, rows, test.optional)
			}
		}
	}
	for bodyLen := 0; bodyLen <= currentDungeonPlayResultOptionalBaseSize+
		currentDungeonPlayResultMaximumDynamicRows*currentDungeonPlayResultDynamicRowSize+4; bodyLen++ {
		_, expected := accepted[bodyLen]
		_, _, got := currentDungeonPlayResultShape(make([]byte, bodyLen))
		if got != expected {
			t.Fatalf("body_len=%d valid=%t want=%t", bodyLen, got, expected)
		}
	}
}

func TestHandleDungeonSetPlayResultCapturesOnceWithoutRewardOrResponse(t *testing.T) {
	service, runtime, session, conn, targetKey := prepareCompletedSettlementRuntime(t)
	conn.write.Reset()
	body := make([]byte, currentDungeonPlayResultBaseSize+currentDungeonPlayResultDynamicRowSize)
	body[0] = 3
	binary.LittleEndian.PutUint16(body[1:3], 99)
	binary.LittleEndian.PutUint32(body[3:7], 0x11223344)

	if err := service.handleDungeonSetPlayResult(session, body); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("captured op46 emitted unproven result/reward packets=%x", conn.write.Bytes())
	}
	if !runtime.settlementPlayResultReceived ||
		runtime.settlementPlayResultDynamicRows != 1 ||
		runtime.settlementPlayResultOptionalField ||
		!bytes.Equal(runtime.settlementPlayResultBody, body) {
		t.Fatalf("captured state=%+v body=%x", runtime, runtime.settlementPlayResultBody)
	}
	if !runtime.settlementEntrySent || runtime.townReturnPending || runtime.townReturnOp24Sent ||
		runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunCompleted ||
		runtime.bossDieCheckTargetObjectKey != uint16(targetKey) {
		t.Fatalf("op46 changed completion owner=%+v", runtime)
	}

	if err := service.handleDungeonSetPlayResult(session, append([]byte(nil), body...)); err != nil {
		t.Fatal(err)
	}
	conflict := append([]byte(nil), body...)
	conflict[len(conflict)-1] = 0x7f
	if err := service.handleDungeonSetPlayResult(session, conflict); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 || !bytes.Equal(runtime.settlementPlayResultBody, body) {
		t.Fatalf("op46 replay/conflict mutated owner body=%x wrote=%x", runtime.settlementPlayResultBody, conn.write.Bytes())
	}
}

func TestHandleGameUpperRoutesCurrentPlayResultAndRejectsWrongClassOrStage(t *testing.T) {
	t.Run("ready class one", func(t *testing.T) {
		service, runtime, session, conn, _ := prepareCompletedSettlementRuntime(t)
		conn.write.Reset()
		body := make([]byte, currentDungeonPlayResultOptionalBaseSize)
		packet, err := dnfproto.BuildChannelPacket(
			uint16(dnfenum.CmdPacketSetPlayResult), body, 0, dnfproto.DefaultChannelClassification,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.handleGameUpper(session, packet); err != nil {
			t.Fatal(err)
		}
		if !runtime.settlementPlayResultReceived || !runtime.settlementPlayResultOptionalField ||
			runtime.settlementPlayResultDynamicRows != 0 || conn.write.Len() != 0 {
			t.Fatalf("routed op46 state=%+v wrote=%x", runtime, conn.write.Bytes())
		}
	})

	for _, test := range []struct {
		name           string
		classification byte
		bodyLen        int
		ready          bool
	}{
		{name: "wrong class", classification: 2, bodyLen: currentDungeonPlayResultBaseSize, ready: true},
		{name: "malformed length", classification: dnfproto.DefaultChannelClassification, bodyLen: currentDungeonPlayResultBaseSize + 1, ready: true},
		{name: "before settlement entry", classification: dnfproto.DefaultChannelClassification, bodyLen: currentDungeonPlayResultBaseSize},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
			conn := &bufferConn{}
			session := &gameSession{
				conn:                conn,
				connID:              "rejected-op46-test",
				selectedCharacterID: 99,
				dungeon:             dungeonSessionState{runtime: runtime},
			}
			if test.ready {
				completeSettlementRuntimeForTest(t, service, runtime, session)
				conn.write.Reset()
			}
			packet, err := dnfproto.BuildChannelPacket(
				uint16(dnfenum.CmdPacketSetPlayResult), make([]byte, test.bodyLen), 0, test.classification,
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.handleGameUpper(session, packet); err != nil {
				t.Fatal(err)
			}
			if runtime.settlementPlayResultReceived || conn.write.Len() != 0 {
				t.Fatalf("rejected op46 captured=%t wrote=%x", runtime.settlementPlayResultReceived, conn.write.Bytes())
			}
		})
	}
}

func prepareCompletedSettlementRuntime(t *testing.T) (*Service, *runtimeDungeonState, *gameSession, *bufferConn, uint32) {
	t.Helper()
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	conn := &bufferConn{}
	channel := channelcatalog.Channel{ServerID: 1, ID: 19, Type: 1, Name: "ch.19", Port: 10019}
	session := &gameSession{
		conn:                conn,
		connID:              "completed-settlement-runtime-test",
		channel:             channel,
		residentChannel:     channel,
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	targetKey := completeSettlementRuntimeForTest(t, service, runtime, session)
	return service, runtime, session, conn, targetKey
}

func completeSettlementRuntimeForTest(
	t *testing.T,
	service *Service,
	runtime *runtimeDungeonState,
	session *gameSession,
) uint32 {
	t.Helper()
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], targetKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], session.selectedCharacterID)
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatal(err)
	}
	if err := service.handleDungeonBossDieCheck(
		session,
		bossDieCheckRequestBody(session.selectedCharacterID, uint16(targetKey)),
	); err != nil {
		t.Fatal(err)
	}
	if !runtime.settlementEntrySent || runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunCompleted {
		t.Fatalf("settlement fixture incomplete state=%+v", runtime)
	}
	return targetKey
}
