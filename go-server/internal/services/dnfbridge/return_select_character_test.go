package dnfbridge

import (
	"bytes"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func TestHandleUpperReturnSelectCharacterResetsThenSendsAck(t *testing.T) {
	service := &Service{
		options:      options{gameUpperHeader: gameUpperHeaderServer16, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		gameSessions: make(map[uint16]*gameSession),
	}

	connection := &bufferConn{}
	session := &gameSession{
		conn:                                 connection,
		connID:                               "game-return-select-test",
		channel:                              channelcatalog.Channel{ID: 19},
		selectedCharacterID:                  19,
		enterSelectDungeonSent:               true,
		enterSelectDungeonAckSent:            true,
		enterSelectDungeonContextSent:        true,
		sceneBootstrapTailDeferred:           true,
		sceneBootstrapTailSent:               true,
		runtimeAfterBlacklistSent:            true,
		runtimeFinishLoadingGateSent:         true,
		fpsFinishLoadingGateSent:             true,
		selectedUserInfoRefreshSent:          true,
		selectedUserInfoMode3Sent:            true,
		currentSceneObjectListSent:           true,
		selectedItemListRefreshSent:          true,
		selectedItemListBootstrapCharacterID: 19,
		selectedEquipmentUpdateSent:          true,
		selectPreviewObjectStateSent:         true,
		selectPreviewActorRemoved:            true,
		preDungeonContextPlayerStateSent:     true,
		postStartMapPlayerStateSent:          true,
		currentFinishLoadingStateSent:        true,
		currentFinishLoadingCompletionSent:   true,
		postFinishLoadingPlayerStateSent:     true,
		initialTownRouteCharacterID:          19,
		initialTownRouteStage:                currentInitialTownRoutePlayerStateSent,
		townSceneReadyCharacterID:            19,
		townPositionSnapshot: currentTownPositionSnapshot{
			CharacterID:   19,
			TownID:        38,
			AreaID:        0,
			PositionX:     900,
			PositionY:     250,
			PositionValid: true,
		},
		townSelectorOriginSnapshot: currentTownPositionSnapshot{
			CharacterID:   19,
			TownID:        38,
			AreaID:        0,
			PositionX:     900,
			PositionY:     250,
			PositionValid: true,
		},
		townSelectorOriginBound: true,
	}
	service.gameSessions[19] = session

	if err := service.handleUpperReturnSelectCharacter(session, nil); err != nil {
		t.Fatalf("handle return select: %v", err)
	}
	if session.selectedCharacterID != 0 {
		t.Fatalf("selected character = %d, want 0", session.selectedCharacterID)
	}
	if _, ok := service.gameSessions[19]; ok {
		t.Fatal("old character remains in online-session index")
	}
	if !returnSelectTownReentryPending(session) {
		t.Fatal("successful op7 did not retain the selector re-entry owner marker")
	}
	if returnSelectSceneStateRetained(session) {
		t.Fatalf("return-select retained scene state: %+v", session)
	}

	packets := splitServerUpperPacketsForReturnSelectTest(t, connection.write.Bytes())
	if len(packets) != 1 {
		t.Fatalf("packet count = %d, want 1", len(packets))
	}
	if packets[0][0] != 1 || binary.LittleEndian.Uint16(packets[0][1:3]) != uint16(dnfenum.CmdPacketReturnSelectCharacter) ||
		!bytes.Equal(packets[0][16:], []byte{1}) {
		t.Fatalf("first packet = %x, want class1/op7 success", packets[0])
	}
}

func TestHandleUpperReturnSelectCharacterRejectsMalformedBody(t *testing.T) {
	service := &Service{}
	connection := &bufferConn{}
	session := &gameSession{conn: connection, selectedCharacterID: 19, sceneBootstrapTailSent: true}
	if err := service.handleUpperReturnSelectCharacter(session, []byte{0}); err != nil {
		t.Fatalf("handle return select: %v", err)
	}
	if connection.write.Len() != 0 || session.selectedCharacterID != 19 || !session.sceneBootstrapTailSent {
		t.Fatalf("rejected request mutated state: bytes=%d session=%+v", connection.write.Len(), session)
	}
}

func TestHandleGameUpperRoutesOnlyClass1ReturnSelectCharacter(t *testing.T) {
	for _, test := range []struct {
		name               string
		classification     byte
		wantPackets        int
		wantSelectedCharID uint16
	}{
		{name: "class1 command", classification: 1, wantPackets: 1, wantSelectedCharID: 0},
		{name: "class0 notification", classification: 0, wantPackets: 0, wantSelectedCharID: 19},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection := &bufferConn{}
			service := &Service{
				options:      options{gameUpperHeader: gameUpperHeaderServer16, gameUpperBodyCodec: gameUpperBodyCodecPlain},
				gameSessions: make(map[uint16]*gameSession),
			}
			session := &gameSession{conn: connection, connID: "game-return-select-route-test", selectedCharacterID: 19}
			service.gameSessions[19] = session
			request := make([]byte, 13)
			request[0] = test.classification
			binary.LittleEndian.PutUint16(request[1:3], uint16(dnfenum.CmdPacketReturnSelectCharacter))
			binary.LittleEndian.PutUint32(request[3:7], uint32(len(request)))

			if err := service.handleGameUpper(session, request); err != nil {
				t.Fatalf("handle game upper: %v", err)
			}
			if session.selectedCharacterID != test.wantSelectedCharID {
				t.Fatalf("selected character = %d, want %d", session.selectedCharacterID, test.wantSelectedCharID)
			}
			packets := splitServerUpperPacketsForReturnSelectTest(t, connection.write.Bytes())
			if len(packets) != test.wantPackets {
				t.Fatalf("packet count = %d, want %d", len(packets), test.wantPackets)
			}
		})
	}
}

func returnSelectSceneStateRetained(session *gameSession) bool {
	return session.enterSelectDungeonSent || session.enterSelectDungeonAckSent || session.enterSelectDungeonContextSent ||
		session.sceneBootstrapTailDeferred || session.sceneBootstrapTailSent || session.runtimeAfterBlacklistSent ||
		session.runtimeFinishLoadingGateSent || session.fpsFinishLoadingGateSent || session.selectedUserInfoRefreshSent ||
		session.selectedUserInfoMode3Sent || session.currentSceneObjectListSent ||
		session.selectedItemListRefreshSent || session.selectedItemListBootstrapCharacterID != 0 ||
		session.selectedEquipmentUpdateSent || session.selectPreviewObjectStateSent ||
		session.selectPreviewActorRemoved || session.preDungeonContextPlayerStateSent || session.postStartMapPlayerStateSent ||
		session.currentFinishLoadingStateSent || session.currentFinishLoadingCompletionSent ||
		session.postFinishLoadingPlayerStateSent ||
		session.initialTownRouteCharacterID != 0 || session.initialTownRouteStage != currentInitialTownRouteIdle ||
		session.initialTownQuestSnapshotsSent ||
		session.townSceneReadyCharacterID != 0 || session.townSelectorOriginBound ||
		session.townSelectorOriginSnapshot.CharacterID != 0
}

func splitServerUpperPacketsForReturnSelectTest(t *testing.T, stream []byte) [][]byte {
	t.Helper()
	var packets [][]byte
	for len(stream) != 0 {
		if len(stream) < 16 {
			t.Fatalf("short upper header: %d bytes", len(stream))
		}
		packetLen := int(binary.LittleEndian.Uint32(stream[3:7]))
		if packetLen < 16 || packetLen > len(stream) {
			t.Fatalf("invalid upper packet length %d in %d-byte stream", packetLen, len(stream))
		}
		packets = append(packets, append([]byte(nil), stream[:packetLen]...))
		stream = stream[packetLen:]
	}
	return packets
}
