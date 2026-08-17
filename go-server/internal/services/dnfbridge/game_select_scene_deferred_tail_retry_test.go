package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type deferredTailPacketSignature struct {
	classification byte
	msgID          uint16
	body           []byte
}

func TestDeferredSelectSceneTailResumesEveryFailedWriteWithoutReplayingPrefix(t *testing.T) {
	baselineService, baselineSession := newDeferredTailResumeFixture(t)
	baselineConnection := baselineSession.conn.(*bufferConn)
	if err := baselineService.sendDeferredSelectSceneTail(baselineSession, "test_baseline"); err != nil {
		t.Fatalf("baseline deferred tail: %v", err)
	}
	baseline := deferredTailPacketSignatures(t, baselineConnection.write.Bytes())
	if len(baseline) == 0 {
		t.Fatal("baseline deferred tail emitted no packets")
	}

	for failAt := 1; failAt <= len(baseline); failAt++ {
		t.Run(packetSignatureLabel(baseline[failAt-1], failAt), func(t *testing.T) {
			service, session := newDeferredTailResumeFixture(t)
			wantErr := errors.New("deferred tail write failed")
			failing := &failNthDungeonWriteConn{failAt: failAt, err: wantErr}
			session.conn = failing

			err := service.sendDeferredSelectSceneTail(session, "test_failed")
			if !errors.Is(err, wantErr) {
				t.Fatalf("failed write %d error=%v want=%v", failAt, err, wantErr)
			}
			if session.sceneBootstrapTailSent || !session.sceneBootstrapTailDeferred {
				t.Fatalf(
					"failed write %d prematurely completed tail sent=%t deferred=%t",
					failAt,
					session.sceneBootstrapTailSent,
					session.sceneBootstrapTailDeferred,
				)
			}
			prefix := deferredTailPacketSignatures(t, failing.bufferConn.write.Bytes())

			resume := &bufferConn{}
			session.conn = resume
			if err := service.sendDeferredSelectSceneTail(session, "test_resume"); err != nil {
				t.Fatalf("resume after write %d: %v", failAt, err)
			}
			got := append(prefix, deferredTailPacketSignatures(t, resume.write.Bytes())...)
			if !reflect.DeepEqual(got, baseline) {
				t.Fatalf(
					"write %d replay/omission:\n got=%v\nwant=%v",
					failAt,
					got,
					baseline,
				)
			}
			if !session.sceneBootstrapTailSent || session.sceneBootstrapTailDeferred ||
				session.sceneBootstrapTailPostStage != currentDeferredSelectSceneTailComplete {
				t.Fatalf(
					"write %d resume flags sent=%t deferred=%t post_stage=%d",
					failAt,
					session.sceneBootstrapTailSent,
					session.sceneBootstrapTailDeferred,
					session.sceneBootstrapTailPostStage,
				)
			}
			resume.write.Reset()
			if err := service.sendDeferredSelectSceneTail(session, "test_duplicate"); err != nil {
				t.Fatalf("duplicate after write %d: %v", failAt, err)
			}
			if resume.write.Len() != 0 {
				t.Fatalf("duplicate after write %d replayed %x", failAt, resume.write.Bytes())
			}
		})
	}
}

func newDeferredTailResumeFixture(t *testing.T) (*Service, *gameSession) {
	t.Helper()
	service, session, repositories := newTownMoveTest(t)
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID: "dnf:1",
		Metadata:  map[string]string{currentRentalPointMetadataKey: "0"},
	}); err != nil {
		t.Fatal(err)
	}
	session.initialTownRouteCharacterID = session.selectedCharacterID
	session.initialTownRouteStage = currentInitialTownRoutePlayerStateSent
	session.initialTownLegacySceneReadyAccepted = true
	session.townPostTransition.characterID = session.selectedCharacterID
	session.townPostTransition.ownerChannel = currentTownActorOwnerContext(session)
	session.townPostTransition.stage = currentTownPostTransitionComplete
	session.sceneBootstrapTailDeferred = true
	session.sceneBootstrapTailSent = false
	resetCurrentDeferredSelectSceneTailProgress(session)
	return service, session
}

func deferredTailPacketSignatures(t *testing.T, stream []byte) []deferredTailPacketSignature {
	t.Helper()
	signatures := make([]deferredTailPacketSignature, 0)
	for len(stream) > 0 {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		signatures = append(signatures, deferredTailPacketSignature{
			classification: packet.Header.Classification,
			msgID:          packet.Header.MsgID,
			body:           append([]byte(nil), packet.Body...),
		})
		stream = rest
	}
	return signatures
}

func packetSignatureLabel(signature deferredTailPacketSignature, index int) string {
	var marker byte
	if len(signature.body) != 0 {
		marker = signature.body[0]
	}
	return fmt.Sprintf(
		"write_%02d_class_%d_msg_%d_marker_%d",
		index,
		signature.classification,
		signature.msgID,
		marker,
	)
}
