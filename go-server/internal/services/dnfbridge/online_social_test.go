package dnfbridge

import (
	"bytes"
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeCurrentPeerChatRequestMatchesLiveMenuShape(t *testing.T) {
	var writer packetWriter
	writer.writeByte(0x24)
	writer.writeUint16(5)
	writer.writeUint32(0)
	writer.writeRawDstr([]byte("nihao "))
	writer.writeRawDstr(rosterNameBytes("来来来"))

	request, err := decodeCurrentPeerChatRequest(writer.bytes())
	if err != nil {
		t.Fatal(err)
	}
	if request.MessageType != 0x24 || request.TargetID != 5 || request.Unknown != 0 ||
		!bytes.Equal(request.Message, []byte("nihao ")) ||
		!bytes.Equal(request.TargetName, rosterNameBytes("来来来")) {
		t.Fatalf("decoded request = %+v", request)
	}
}

func TestHandleCurrentFriendAddAcknowledgesAndProjectsOnlineFriend(t *testing.T) {
	service, source, _, sourceConn, _ := newCurrentSocialPeerTest(t)
	var request packetWriter
	request.writeUint16(5)
	request.writeRawDstr(rosterNameBytes("来来来"))

	handled, err := service.handleAlignedGameCommand(source, byte(dnfenum.GameCmdCommand), currentFriendAddOpcode, request.bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("friend add was not handled")
	}
	ack, rest := splitGameServerUpperPacket(t, sourceConn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != currentFriendAddOpcode ||
		!bytes.Equal(ack.Body, []byte{1}) {
		t.Fatalf("friend ack = class %d msg %d body=% X", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	notice, rest := splitGameServerUpperPacket(t, rest)
	wantNotice := buildCurrentFriendAddNoticeBody(5, rosterNameBytes("来来来"), true)
	if notice.Header.Classification != 0 || notice.Header.MsgID != currentFriendNotifyOpcode ||
		!bytes.Equal(notice.Body, wantNotice) || len(rest) != 0 {
		t.Fatalf("friend notice = class %d msg %d body=% X trailing=%d", notice.Header.Classification, notice.Header.MsgID, notice.Body, len(rest))
	}
}

func TestHandleCurrentPeerChatAcknowledgesAndForwardsCurrentNotifications(t *testing.T) {
	service, source, _, sourceConn, targetConn := newCurrentSocialPeerTest(t)
	var request packetWriter
	request.writeByte(1)
	request.writeUint16(5)
	request.writeUint32(0)
	request.writeRawDstr([]byte("hello"))
	request.writeRawDstr(rosterNameBytes("来来来"))

	handled, err := service.handleAlignedGameCommand(source, byte(dnfenum.GameCmdCommand), currentPeerChatStateOpcode, request.bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("peer chat was not handled")
	}
	ack, sourceRest := splitGameServerUpperPacket(t, sourceConn.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != currentPeerChatStateOpcode ||
		!bytes.Equal(ack.Body, []byte{1}) {
		t.Fatalf("chat ack = class %d msg %d body=% X", ack.Header.Classification, ack.Header.MsgID, ack.Body)
	}
	sourceState, sourceRest := splitGameServerUpperPacket(t, sourceRest)
	if sourceState.Header.Classification != 0 || sourceState.Header.MsgID != currentPeerChatStateOpcode ||
		!bytes.Equal(sourceState.Body, buildCurrentPeerChatStateBody(5, 1)) || len(sourceRest) != 0 {
		t.Fatalf("source chat state = class %d msg %d body=% X trailing=%d", sourceState.Header.Classification, sourceState.Header.MsgID, sourceState.Body, len(sourceRest))
	}

	targetState, targetRest := splitGameServerUpperPacket(t, targetConn.write.Bytes())
	if targetState.Header.Classification != 0 || targetState.Header.MsgID != currentPeerChatStateOpcode ||
		!bytes.Equal(targetState.Body, buildCurrentPeerChatStateBody(1, 1)) {
		t.Fatalf("target chat state = class %d msg %d body=% X", targetState.Header.Classification, targetState.Header.MsgID, targetState.Body)
	}
	targetNotice, targetRest := splitGameServerUpperPacket(t, targetRest)
	wantNotice := buildCurrentPeerChatNoticeBody(1, 42, 1, rosterNameBytes("pouut"), []byte("hello"))
	if targetNotice.Header.Classification != 0 || targetNotice.Header.MsgID != currentPeerChatNotifyOpcode ||
		!bytes.Equal(targetNotice.Body, wantNotice) || len(targetRest) != 0 {
		t.Fatalf("target chat notice = class %d msg %d body=% X trailing=%d", targetNotice.Header.Classification, targetNotice.Header.MsgID, targetNotice.Body, len(targetRest))
	}
}

func TestHandleCurrentBlacklistMutationEchoesNameDstrInSuccess(t *testing.T) {
	conn := &bufferConn{}
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	session := &gameSession{conn: conn, selectedCharacterID: 1}
	var request packetWriter
	request.writeRawDstr(rosterNameBytes("来来来"))

	handled, err := service.handleAlignedGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketRegisiterToBlacklist), request.bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("blacklist mutation was not handled")
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	want := append([]byte{1}, request.bytes()...)
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketRegisiterToBlacklist) ||
		!bytes.Equal(packet.Body, want) || len(rest) != 0 {
		t.Fatalf("blacklist ack = class %d msg %d body=% X trailing=%d", packet.Header.Classification, packet.Header.MsgID, packet.Body, len(rest))
	}
}

func newCurrentSocialPeerTest(t *testing.T) (*Service, *gameSession, *gameSession, *bufferConn, *bufferConn) {
	t.Helper()
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	for _, character := range []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: "dnf:1", Name: "pouut", Job: "0", Level: 90},
		{CharacterID: "5", AccountID: "dnf:2", Name: "来来来", Job: "1", Level: 90},
	} {
		if err := repositories.Character.Save(ctx, character); err != nil {
			t.Fatal(err)
		}
	}
	sourceConn := &bufferConn{}
	targetConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 42, Name: "ch.42"}
	source := &gameSession{conn: sourceConn, channel: channel, accountID: "dnf:1", selectedCharacterID: 1}
	target := &gameSession{conn: targetConn, channel: channel, accountID: "dnf:2", selectedCharacterID: 5}
	service := &Service{
		options:       options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers: newOnlinePlayerManager(),
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	service.bindGameSessionCharacter(source, 1)
	service.bindGameSessionCharacter(target, 5)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1, TownID: 1, AreaID: 1, Session: source})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 5, TownID: 1, AreaID: 1, Session: target})
	return service, source, target, sourceConn, targetConn
}
