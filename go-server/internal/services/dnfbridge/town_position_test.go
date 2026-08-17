package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestParseCurrentTownSetUserPositionExactCurrentWriterBody(t *testing.T) {
	// Exact pre-TerSafe live sample captured from the current EXE:
	// x=828, y=211, movement=8, scaled opaque value=100.
	body := []byte{0x3c, 0x03, 0xd3, 0x00, 0x08, 0x64, 0x00}
	request, err := parseCurrentTownSetUserPositionRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.PositionX != 828 || request.PositionY != 211 ||
		request.MovementCode != 8 || request.OpaqueScaledU16 != 100 {
		t.Fatalf("request=%+v", request)
	}
	for _, malformed := range [][]byte{
		make([]byte, currentTownSetUserPositionBodySize-1),
		make([]byte, currentTownSetUserPositionBodySize+1),
	} {
		if _, err := parseCurrentTownSetUserPositionRequest(malformed); err == nil {
			t.Fatalf("malformed body length %d unexpectedly accepted", len(malformed))
		}
	}
}

func TestHandleTownSetUserPositionCapturesSessionSnapshotWithoutPersistenceOrAck(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	if err := service.handleTownSetUserArea(session, buildTownMoveRequest(38, 0, 900, 250, 5)); err != nil {
		t.Fatal(err)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	body := buildTownPositionRequest(640, 244, 6, 987)
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketSetUserPosition),
		body,
	); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("passive op35 emitted an ACK=%x", connection.write.Bytes())
	}
	session.townMu.Lock()
	snapshot := session.townPositionSnapshot
	session.townMu.Unlock()
	if !snapshot.PositionValid || snapshot.CharacterID != 29 ||
		snapshot.TownID != 38 || snapshot.AreaID != 0 ||
		snapshot.PositionX != 640 || snapshot.PositionY != 244 ||
		snapshot.MovementCode != 6 || snapshot.OpaqueScaledU16 != 987 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	stored, found, err := repositories.Character.Load(context.Background(), "29")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if stored.Stats["pos_x"] != 900 || stored.Stats["pos_y"] != 250 {
		t.Fatalf("op35 unexpectedly persisted high-frequency position: stats=%+v", stored.Stats)
	}
}

func TestTownSetUserPositionRequiresExactTownSceneOwnerAndClass(t *testing.T) {
	body := buildTownPositionRequest(640, 244, 6, 987)

	t.Run("town scene not ready", func(t *testing.T) {
		service, session, _ := newTownMoveTest(t)
		session.townMu.Lock()
		session.townSceneReadyCharacterID = 0
		setCurrentTownPositionSceneLocked(session, 29, 38, 1)
		session.townMu.Unlock()
		if err := service.handleTownSetUserPosition(session, body); err != nil {
			t.Fatal(err)
		}
		if session.townPositionSnapshot.PositionValid {
			t.Fatalf("unready scene captured snapshot=%+v", session.townPositionSnapshot)
		}
	})

	t.Run("wrong command class", func(t *testing.T) {
		service, session, _ := newTownMoveTest(t)
		service.markCurrentTownSceneReady(session)
		session.townMu.Lock()
		setCurrentTownPositionSceneLocked(session, 29, 38, 1)
		session.townMu.Unlock()
		packet, err := dnfproto.BuildChannelPacket(
			uint16(dnfenum.CmdPacketSetUserPosition),
			body,
			1,
			2,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.handleGameUpper(session, packet); err != nil {
			t.Fatal(err)
		}
		if session.townPositionSnapshot.PositionValid {
			t.Fatalf("wrong-class op35 captured snapshot=%+v", session.townPositionSnapshot)
		}
	})
}

func TestDungeonTownTransitionUsesMatchingCurrentOp35Snapshot(t *testing.T) {
	service, session, _ := newBackToVillageRuntimeAtOrigin(t, 640, 240)

	transition, err := service.prepareCurrentDungeonTownTransitionForSession(
		context.Background(),
		session,
		99,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{7, 3, 1, 0, 99, 0, 0x80, 0x02, 0xf0, 0x00, 5, 3}
	if transition.PositionX != 640 || transition.PositionY != 240 ||
		transition.PositionSource != "current_exe_op35_runtime_origin_snapshot" ||
		!bytes.Equal(transition.Body, want) {
		t.Fatalf("transition=%+v body=%x want=%x", transition, transition.Body, want)
	}
}

func TestDungeonTownTransitionRejectsStaleSceneSnapshot(t *testing.T) {
	service, session, _ := newBackToVillageRuntime(t)
	session.townMu.Lock()
	setCurrentTownPositionSceneLocked(session, 99, 7, 2)
	session.townPositionSnapshot.PositionX = 640
	session.townPositionSnapshot.PositionY = 240
	session.townPositionSnapshot.PositionValid = true
	session.townMu.Unlock()

	transition, err := service.prepareCurrentDungeonTownTransitionForSession(
		context.Background(),
		session,
		99,
	)
	if err != nil {
		t.Fatal(err)
	}
	if transition.PositionX != 474 || transition.PositionY != 234 ||
		transition.PositionSource != "current_exe_op35_runtime_origin_snapshot" ||
		!bytes.Equal(transition.Body, wantDungeonTownTransitionBody()) {
		t.Fatalf("stale snapshot changed transition=%+v body=%x", transition, transition.Body)
	}
}

func TestDungeonTownTransitionRejectsMismatchedBoundSelectorOrigin(t *testing.T) {
	service, session, _ := newBackToVillageRuntime(t)
	session.dungeon.runtime = nil
	session.townMu.Lock()
	setCurrentTownPositionSceneLocked(session, 99, 7, 2)
	session.townPositionSnapshot.PositionX = 640
	session.townPositionSnapshot.PositionY = 240
	session.townPositionSnapshot.PositionValid = true
	session.townMu.Unlock()
	service.markCurrentTownSceneReady(session)
	if _, ok := bindCurrentTownSelectorOrigin(session); !ok {
		t.Fatal("failed to bind mismatched selector origin")
	}
	if _, err := service.prepareCurrentDungeonTownTransitionForSession(context.Background(), session, 99); err == nil {
		t.Fatal("mismatched bound selector origin unexpectedly fell back to repository position")
	}
}

func TestSelectorPageBackToVillageUsesLatestCurrentOp35Snapshot(t *testing.T) {
	service, session, _ := newBackToVillageRuntime(t)
	session.dungeon.runtime = nil
	session.townMu.Lock()
	setCurrentTownPositionSceneLocked(session, 99, 7, 3)
	session.townMu.Unlock()
	service.markCurrentTownSceneReady(session)
	if err := service.handleTownSetUserPosition(session, buildTownPositionRequest(640, 240, 6, 987)); err != nil {
		t.Fatal(err)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()
	if err := service.sendEnterSelectDungeonState(session, "test_selector_origin", false, true); err != nil {
		t.Fatal(err)
	}
	if !session.townSelectorOriginBound ||
		session.townSelectorOriginSnapshot.PositionX != 640 ||
		session.townSelectorOriginSnapshot.PositionY != 240 {
		t.Fatalf("selector origin=%+v bound=%t", session.townSelectorOriginSnapshot, session.townSelectorOriginBound)
	}
	connection.write.Reset()

	// Even if a later report arrives while the selector is visible, op132 must
	// return to the origin frozen by the successful op15/op27 context.
	if err := service.handleTownSetUserPosition(session, buildTownPositionRequest(700, 260, 7, 100)); err != nil {
		t.Fatal(err)
	}

	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	want := []byte{7, 3, 1, 0, 99, 0, 0x80, 0x02, 0xf0, 0x00, 5, 3}
	if packet.Header.MsgID != currentSceneTransitionMsgID ||
		packet.Header.Classification != 0 || !bytes.Equal(packet.Body, want) || len(rest) != 0 {
		t.Fatalf("selector op132 header=%+v body=%x rest=%x want=%x", packet.Header, packet.Body, rest, want)
	}
	if session.townSelectorOriginBound || session.townSelectorOriginSnapshot.CharacterID != 0 {
		t.Fatalf("successful selector return retained origin=%+v bound=%t", session.townSelectorOriginSnapshot, session.townSelectorOriginBound)
	}
}

func TestActiveDungeonPreservesFrozenSelectorOriginAndCapturesDungeonOp35(t *testing.T) {
	service, session, runtime := newBackToVillageRuntimeAtOrigin(t, 640, 240)
	if err := service.handleTownSetUserPosition(session, buildTownPositionRequest(700, 260, 7, 100)); err != nil {
		t.Fatal(err)
	}
	if !runtime.actorPositionValid || runtime.actorPositionX != 700 || runtime.actorPositionY != 260 {
		t.Fatalf("dungeon actor position valid=%t x=%d y=%d", runtime.actorPositionValid, runtime.actorPositionX, runtime.actorPositionY)
	}
	transition, err := service.prepareCurrentDungeonTownTransitionForSession(context.Background(), session, 99)
	if err != nil {
		t.Fatal(err)
	}
	if transition.PositionX != 640 || transition.PositionY != 240 ||
		transition.PositionSource != "current_exe_op35_runtime_origin_snapshot" {
		t.Fatalf("active return transition=%+v", transition)
	}
	session.townMu.Lock()
	live := session.townPositionSnapshot
	origin := session.townSelectorOriginSnapshot
	originBound := session.townSelectorOriginBound
	session.townMu.Unlock()
	if live.PositionX != 640 || live.PositionY != 240 ||
		!originBound || origin.PositionX != 640 || origin.PositionY != 240 {
		t.Fatalf("dungeon op35 changed snapshots live=%+v origin=%+v bound=%t", live, origin, originBound)
	}
}

func TestTownSetUserPositionCommitsPendingDungeonReturnAfterOp24(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatalf("start pending return: %v", err)
	}
	if session.dungeon.runtime != runtime || !runtime.townReturnPending || !runtime.townReturnOp24Sent {
		t.Fatalf("return did not enter pending state owner=%p runtime=%+v", session.dungeon.runtime, runtime)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.handleTownSetUserPosition(session, buildTownPositionRequest(85, 331, 5, 100)); err != nil {
		t.Fatal(err)
	}
	if trailing := splitTownPostTransitionPlayerState(
		t,
		connection.write.Bytes(),
		session,
		session.selectedCharacterID,
		backToVillageSkillProjectionBody(t, service),
		false,
	); len(trailing) != 0 {
		t.Fatalf("town op35 wrote packets after confirmed-return player state=%x", trailing)
	}
	if session.dungeon.runtime != nil {
		t.Fatalf("town op35 retained pending dungeon runtime=%+v", session.dungeon.runtime)
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunAbandoned {
		t.Fatalf("town op35 committed status=%s want abandoned", status)
	}
	session.townMu.Lock()
	snapshot := session.townPositionSnapshot
	readyCharacterID := session.townSceneReadyCharacterID
	session.townMu.Unlock()
	if readyCharacterID != session.selectedCharacterID ||
		!snapshot.PositionValid ||
		snapshot.CharacterID != session.selectedCharacterID ||
		snapshot.TownID != runtime.townReturnTransition.TownID ||
		snapshot.AreaID != runtime.townReturnTransition.AreaID ||
		snapshot.PositionX != 85 ||
		snapshot.PositionY != 331 ||
		snapshot.MovementCode != 5 ||
		snapshot.OpaqueScaledU16 != 100 {
		t.Fatalf("ready=%d snapshot=%+v transition=%+v", readyCharacterID, snapshot, runtime.townReturnTransition)
	}
}

func TestTownSetUserPositionResumesDetachedConfirmedReturnSuffix(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatalf("start pending return: %v", err)
	}

	wantErr := errors.New("confirmed return op37 write failed")
	failing := &failNthDungeonWriteConn{failAt: 4, err: wantErr}
	session.conn = failing
	if err := service.commitPendingDungeonReturnForSceneRequest(
		session,
		"test_op35_detached_confirmed_return_prefix",
	); !errors.Is(err, wantErr) {
		t.Fatalf("confirmed return prefix failure=%v want=%v", err, wantErr)
	}
	if got := townPostTransitionPacketSignatures(t, failing.write.Bytes()); !reflect.DeepEqual(
		got,
		[]string{"mode0", "mode1", "op105"},
	) {
		t.Fatalf("confirmed return prefix=%v want=[mode0 mode1 op105]", got)
	}
	if session.dungeon.runtime != nil ||
		runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunAbandoned ||
		!session.confirmedDungeonReturnStatePending ||
		session.townPostTransition.stage != currentTownPostTransitionCreatureGrowthSent {
		t.Fatalf(
			"detached return owner=%p run=%s pending=%t stage=%d",
			session.dungeon.runtime,
			runtime.Session.Snapshot().Run.Status,
			session.confirmedDungeonReturnStatePending,
			session.townPostTransition.stage,
		)
	}

	resume := &bufferConn{}
	session.conn = resume
	if err := service.handleTownSetUserPosition(
		session,
		buildTownPositionRequest(85, 331, 5, 100),
	); err != nil {
		t.Fatalf("op35 resume confirmed return suffix: %v", err)
	}
	if got := townPostTransitionPacketSignatures(t, resume.write.Bytes()); !reflect.DeepEqual(
		got,
		[]string{"op37", "op30", "op19", "op120"},
	) {
		t.Fatalf("op35 resumed suffix=%v want=[op37 op30 op19 op120]", got)
	}
	if session.confirmedDungeonReturnStatePending ||
		session.townPostTransition.stage != currentTownPostTransitionComplete {
		t.Fatalf(
			"op35 resume pending=%t stage=%d",
			session.confirmedDungeonReturnStatePending,
			session.townPostTransition.stage,
		)
	}
}

func TestDungeonSelectReturnWithoutFrozenOp35OriginFailsClosed(t *testing.T) {
	service, session, _ := newBackToVillageRuntime(t)
	session.dungeon.mu.Lock()
	session.dungeon.runtime = nil
	session.dungeon.mu.Unlock()
	clearCurrentTownSelectorOrigin(session)

	if _, err := service.prepareCurrentDungeonTownTransitionForSession(
		context.Background(),
		session,
		99,
	); !errors.Is(err, errCurrentDungeonTownReturnOriginUnavailable) {
		t.Fatalf("selector return error=%v, want unavailable frozen origin", err)
	}
}

func buildTownPositionRequest(x, y uint16, movementCode byte, opaqueScaledU16 uint16) []byte {
	body := make([]byte, currentTownSetUserPositionBodySize)
	binary.LittleEndian.PutUint16(body[0:2], x)
	binary.LittleEndian.PutUint16(body[2:4], y)
	body[4] = movementCode
	binary.LittleEndian.PutUint16(body[5:7], opaqueScaledU16)
	return body
}
