package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math/bits"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/channelinfo"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func TestBuildCurrentChannelResidentNoticeMatchesCurrentEXEFields(t *testing.T) {
	service := residentNoticeTestService(t)
	channel, _ := service.currentCatalog().Channel(10)
	now := time.Unix(1_721_020_000, 0)
	body, err := service.buildCurrentChannelResidentNotice(channel, now)
	if err != nil {
		t.Fatalf("build current channel notice: %v", err)
	}
	if len(body) < 4 || int(binary.LittleEndian.Uint32(body[:4])) != len(body)-4 {
		t.Fatalf("protobuf length prefix mismatch: %x", body)
	}
	varints, bytesFields := consumeChannelInfoProto(t, body[4:])
	for field, want := range map[protowire.Number]uint64{
		1:     currentChannelInfoEnum,
		2:     currentChannelInfoSuccess,
		7:     0,
		8:     10,
		9:     currentChannelResidentControllerIndex,
		11:    uint64(now.Unix()),
		13:    defaultInitialUDPPort1,
		14:    defaultInitialUDPPort2,
		30004: currentChannelCommandPacketCount,
		30005: currentChannelNotificationPacketCount,
	} {
		values := varints[field]
		if len(values) != 1 || values[0] != want {
			t.Fatalf("field %d = %v, want [%d]", field, values, want)
		}
	}
	if got := string(bytesFields[4][0]); got != "ch.10" {
		t.Fatalf("channel name field = %q, want ch.10", got)
	}
	if got := string(bytesFields[12][0]); got != service.options.serverIP {
		t.Fatalf("server ip field = %q, want %q", got, service.options.serverIP)
	}
}

func TestBuildCurrentChannelResidentNoticeAcceptsOnlineDirectoryType(t *testing.T) {
	service := residentNoticeTestService(t)
	channel, _ := service.currentCatalog().Channel(10)
	channel.Type = uint8(channelinfo.OnlineTypeFor90CN(channel.ID, int(channel.Type)))

	body, err := service.buildCurrentChannelResidentNotice(channel, time.Unix(1_721_020_000, 0))
	if err != nil {
		t.Fatalf("build online channel type notice: %v", err)
	}
	varints, _ := consumeChannelInfoProto(t, body[4:])
	if values := varints[9]; len(values) != 1 || values[0] != currentChannelResidentControllerIndex {
		t.Fatalf("online type-%d controller field = %v, want [0]", channel.Type, values)
	}
	login := service.buildLoginSuccess(channel)
	if len(login) < 3 || login[2] != channel.Type {
		t.Fatalf("endpoint success raw channel type = %v, want %d", login, channel.Type)
	}
}

func TestGameEndpointHandshakeUsesConnectedChannelExactlyOnce(t *testing.T) {
	service := residentNoticeTestService(t)
	crack, _ := service.currentCatalog().Channel(19)
	crack.ID = 253
	crack.Name = "ch.253"
	crack.NoticeName = "ch.253"
	crack.Port = 10253
	conn := &bufferConn{}
	session := &gameSession{
		conn:            conn,
		accountID:       "dnf:1",
		channel:         crack,
		residentChannel: crack,
	}
	now := time.Unix(1_721_020_000, 0)

	if err := service.sendGameConnectionBootstrap(session, now); err != nil {
		t.Fatalf("send account-bound game bootstrap: %v", err)
	}
	first, rest := splitGameServerUpperPacketWithHeader(t, conn.write.Bytes(), service.gameUpperHeaderSize())
	if first.Header.Classification != 0 || first.Header.MsgID != 1 || first.Header.Seq != 0 {
		t.Fatalf("unexpected CHANNELINFO header: %+v", first.Header)
	}
	wantNotice, err := service.buildCurrentChannelResidentNotice(crack, now)
	if err != nil {
		t.Fatal(err)
	}
	if plain := decodeCurrentEXEChannelNoticeBody(first.Body); !bytes.Equal(plain, wantNotice) {
		t.Fatalf("CHANNELINFO body = %x, want %x", plain, wantNotice)
	}
	if len(rest) != 0 {
		t.Fatalf("TCP open emitted bytes after CHANNELINFO: %x", rest)
	}
	if !session.currentChannelResidentNoticeSent || session.gameEndpointSuccessSent {
		t.Fatalf("state after CHANNELINFO = notice:%t success:%t", session.currentChannelResidentNoticeSent, session.gameEndpointSuccessSent)
	}
	if session.connectionTownActorOwnerChannel != byte(crack.ID) ||
		session.townActorOwnerChannel != byte(crack.ID) {
		t.Fatalf(
			"town actor contexts after CHANNELINFO = connection:%d current:%d, want %d",
			session.connectionTownActorOwnerChannel,
			session.townActorOwnerChannel,
			crack.ID,
		)
	}

	noticeWireLen := conn.write.Len()
	request, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgGameEndpoint),
		make([]byte, currentChannelReconnectDisplayProbeSize),
		1,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, request); err != nil {
		t.Fatalf("handle endpoint request: %v", err)
	}
	success, trailing := splitGameServerUpperPacketWithHeader(t, conn.write.Bytes()[noticeWireLen:], service.gameUpperHeaderSize())
	if success.Header.Classification != dnfproto.DefaultChannelClassification ||
		success.Header.MsgID != uint16(dnfenum.UpperMsgGameEndpoint) ||
		success.Header.Seq != 1 {
		t.Fatalf("endpoint success header = %+v, want class1/op1 seq1", success.Header)
	}
	if want := upperSuccessBody(service.buildLoginSuccess(crack)); !bytes.Equal(success.Body, want) {
		t.Fatalf("endpoint success body = %x, want %x", success.Body, want)
	}
	if len(trailing) != 0 {
		t.Fatalf("endpoint request emitted trailing bytes: %x", trailing)
	}
	if !session.gameEndpointSuccessSent {
		t.Fatal("endpoint success state was not committed")
	}

	handshakeWireLen := conn.write.Len()
	repeat, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgGameEndpoint),
		make([]byte, reference90CNChannelReconnectDisplayProbeBodySize),
		2,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, repeat); err != nil {
		t.Fatalf("handle repeated endpoint request: %v", err)
	}
	if err := service.sendCurrentChannelResidentNotice(session, now.Add(time.Second)); err != nil {
		t.Fatalf("repeat CHANNELINFO: %v", err)
	}
	if conn.write.Len() != handshakeWireLen {
		t.Fatalf("duplicate handshake added %d bytes", conn.write.Len()-handshakeWireLen)
	}
	if session.channel.ID != 253 || session.residentChannel.ID != 253 {
		t.Fatalf("connected channel changed: channel=%+v resident=%+v", session.channel, session.residentChannel)
	}
}

func TestSpecialChannelEndpointHandshakeKeepsRawTypeOnlyInSuccess(t *testing.T) {
	service := residentNoticeTestService(t)
	special := channelcatalog.Channel{
		ServerID: 1,
		ID:       200,
		Type:     23,
		Group:    "raid",
		Name:     "ch.200",
		Port:     10200,
	}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: special, residentChannel: special}

	if err := service.sendCurrentChannelResidentNotice(session, time.Unix(1_721_020_000, 0)); err != nil {
		t.Fatalf("send special-channel notice: %v", err)
	}

	notice, rest := splitGameServerUpperPacketWithHeader(
		t,
		conn.write.Bytes(),
		service.gameUpperHeaderSize(),
	)
	if notice.Header.Classification != 0 || notice.Header.MsgID != 1 || notice.Header.Seq != 0 {
		t.Fatalf("special CHANNELINFO header = %+v", notice.Header)
	}
	if len(rest) != 0 {
		t.Fatalf("special TCP open emitted trailing bytes: %x", rest)
	}
	plainNotice := decodeCurrentEXEChannelNoticeBody(notice.Body)
	varints, _ := consumeChannelInfoProto(t, plainNotice[4:])
	if values := varints[9]; len(values) != 1 || values[0] != currentChannelResidentControllerIndex {
		t.Fatalf("special CHANNELINFO controller index = %v, want [0]", values)
	}

	noticeWireLen := conn.write.Len()
	request, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgGameEndpoint),
		make([]byte, reference90CNChannelReconnectDisplayProbeBodySize),
		1,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, request); err != nil {
		t.Fatalf("handle special endpoint request: %v", err)
	}
	packet, trailing := splitGameServerUpperPacketWithHeader(t, conn.write.Bytes()[noticeWireLen:], service.gameUpperHeaderSize())
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != 1 ||
		packet.Header.Seq != 1 {
		t.Fatalf("special endpoint success = %+v, want class1/op1 seq1", packet.Header)
	}
	if len(trailing) != 0 {
		t.Fatalf("special endpoint emitted trailing bytes: %x", trailing)
	}
	want := upperSuccessBody(service.buildLoginSuccess(special))
	if !bytes.Equal(packet.Body, want) {
		t.Fatalf("special endpoint body = %x, want exact type-%d body %x", packet.Body, special.Type, want)
	}
	// Success envelope byte + buildLoginSuccess channel-type offset 2.
	if len(packet.Body) <= 3 || packet.Body[3] != special.Type {
		t.Fatalf("special endpoint channel type = %d, want exact catalog type %d", packet.Body[3], special.Type)
	}
}

func TestAccountBoundMaxByteChannelCommitsTownOwner(t *testing.T) {
	service := residentNoticeTestService(t)
	channel := channelcatalog.Channel{
		ServerID:   1,
		ID:         255,
		Type:       40,
		Group:      "special",
		Name:       "ch.255",
		NoticeName: "ch.255",
		Port:       10255,
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:            conn,
		accountID:       "dnf:1",
		channel:         channel,
		residentChannel: channel,
	}

	if err := service.sendGameConnectionBootstrap(session, time.Unix(1_721_020_000, 0)); err != nil {
		t.Fatalf("send max-byte channel bootstrap: %v", err)
	}
	packet, trailing := splitGameServerUpperPacketWithHeader(t, conn.write.Bytes(), service.gameUpperHeaderSize())
	if packet.Header.Classification != 0 || packet.Header.MsgID != 1 || packet.Header.Seq != 0 {
		t.Fatalf("max-byte CHANNELINFO header = %+v", packet.Header)
	}
	if len(trailing) != 0 {
		t.Fatalf("max-byte CHANNELINFO emitted trailing bytes: %x", trailing)
	}
	plain := decodeCurrentEXEChannelNoticeBody(packet.Body)
	varints, _ := consumeChannelInfoProto(t, plain[4:])
	if values := varints[8]; len(values) != 1 || values[0] != 255 {
		t.Fatalf("max-byte CHANNELINFO field8 = %v, want [255]", values)
	}
	if session.connectionTownActorOwnerChannel != 255 ||
		session.townActorOwnerChannel != 255 ||
		!session.currentChannelResidentNoticeSent ||
		session.gameEndpointSuccessSent {
		t.Fatalf(
			"max-byte bootstrap state connection=%d current=%d notice=%t success=%t",
			session.connectionTownActorOwnerChannel,
			session.townActorOwnerChannel,
			session.currentChannelResidentNoticeSent,
			session.gameEndpointSuccessSent,
		)
	}
}

func TestHandleGameConnUnboundRealType23ProbeDoesNotPublishChannelInfo(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0,
	})
	if err != nil {
		t.Fatalf("listen special game endpoint: %v", err)
	}
	defer listener.Close()
	listenPort := listener.Addr().(*net.TCPAddr).Port

	raw, err := os.ReadFile("testdata/channel_info.etc")
	if err != nil {
		t.Fatalf("read real channel catalog fixture: %v", err)
	}
	index, err := channelinfo.Parse(raw)
	if err != nil {
		t.Fatalf("parse real channel catalog fixture: %v", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{
		ServerID:     1,
		GamePortBase: listenPort - 221,
	})
	if err != nil {
		t.Fatalf("build real channel catalog fixture: %v", err)
	}
	special, ok := catalog.Channel(221)
	if !ok || special.Type != 23 || special.Port != listenPort {
		t.Fatalf("real special channel = %+v found=%t, want id=221 type=23 port=%d", special, ok, listenPort)
	}
	service := &Service{
		options: options{
			channelServerID:    1,
			channelAdvertiseID: 0,
			serverIP:           "127.0.0.1",
			initialUDPPort1:    defaultInitialUDPPort1,
			initialUDPPort2:    defaultInitialUDPPort2,
			commandCount:       defaultCommandCount,
			notificationCount:  defaultNotificationCount,
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
			maxPacketBytes:     defaultMaxPacketBytes,
		},
		catalog: catalog,
	}
	handlerDone := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.AcceptTCP()
		if acceptErr != nil {
			handlerDone <- acceptErr
			return
		}
		service.handleGameConn(context.Background(), conn)
		handlerDone <- nil
	}()

	client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatalf("dial special game endpoint: %v", err)
	}
	endpointRequest, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgGameEndpoint),
		make([]byte, reference90CNChannelReconnectDisplayProbeBodySize),
		1,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		client.Close()
		t.Fatalf("build special endpoint request: %v", err)
	}
	if _, err := client.Write(endpointRequest); err != nil {
		client.Close()
		t.Fatalf("write special endpoint request: %v", err)
	}
	if err := client.CloseWrite(); err != nil {
		client.Close()
		t.Fatalf("close special client write side: %v", err)
	}
	if err := client.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		client.Close()
		t.Fatalf("set special client read deadline: %v", err)
	}
	wire, readErr := io.ReadAll(client)
	_ = client.Close()
	if readErr != nil {
		t.Fatalf("read special endpoint handshake: %v", readErr)
	}
	select {
	case handleErr := <-handlerDone:
		if handleErr != nil {
			t.Fatalf("handle special game connection: %v", handleErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("special game connection handler did not finish")
	}

	packet, trailing := splitGameServerUpperPacketWithHeader(
		t,
		wire,
		service.gameUpperHeaderSize(),
	)
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != 1 ||
		packet.Header.Seq != 0 {
		t.Fatalf("unbound real type-23 probe bootstrap = %+v, want class1/op1 seq0", packet.Header)
	}
	if len(trailing) != 0 {
		t.Fatalf("unbound real type-23 probe emitted extra wire bytes: %x", trailing)
	}
	want := upperSuccessBody(service.buildLoginSuccess(special))
	if !bytes.Equal(packet.Body, want) {
		t.Fatalf("real type-23 endpoint body = %x, want %x", packet.Body, want)
	}
	if len(packet.Body) <= 3 || packet.Body[3] != special.Type {
		t.Fatalf("real type-23 endpoint channel type = %d, want %d", packet.Body[3], special.Type)
	}
}

func TestAccountBoundHighChannelFallsBackWithoutTruncatingTownOwner(t *testing.T) {
	for _, channelID := range []int{256, 501} {
		t.Run(strconv.Itoa(channelID), func(t *testing.T) {
			service := residentNoticeTestService(t)
			high := channelcatalog.Channel{
				ServerID: 1,
				ID:       channelID,
				Type:     24,
				Group:    "pvp",
				Name:     "ch." + strconv.Itoa(channelID),
				Port:     10000 + channelID,
			}
			conn := &bufferConn{}
			session := &gameSession{
				conn:            conn,
				accountID:       "dnf:1",
				channel:         high,
				residentChannel: high,
			}

			if err := service.sendGameConnectionBootstrap(session, time.Unix(1_721_020_000, 0)); err != nil {
				t.Fatalf("send high-channel bootstrap: %v", err)
			}
			packet, trailing := splitGameServerUpperPacketWithHeader(t, conn.write.Bytes(), service.gameUpperHeaderSize())
			if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
				packet.Header.MsgID != uint16(dnfenum.UpperMsgGameEndpoint) ||
				packet.Header.Seq != 0 {
				t.Fatalf("high-channel fallback = %+v, want class1/op1 seq0", packet.Header)
			}
			if len(trailing) != 0 {
				t.Fatalf("high-channel fallback emitted extra bytes: %x", trailing)
			}
			if session.currentChannelResidentNoticeSent || !session.gameEndpointSuccessSent {
				t.Fatalf(
					"high-channel state = notice:%t success:%t",
					session.currentChannelResidentNoticeSent,
					session.gameEndpointSuccessSent,
				)
			}
			if session.connectionTownActorOwnerChannel != currentSceneObjectContext ||
				session.townActorOwnerChannel != currentSceneObjectContext {
				t.Fatalf(
					"high-channel owner truncated to connection:%d current:%d",
					session.connectionTownActorOwnerChannel,
					session.townActorOwnerChannel,
				)
			}
		})
	}
}

func decodeCurrentEXEChannelNoticeBody(encoded []byte) []byte {
	plain := make([]byte, len(encoded))
	for i, value := range encoded {
		plain[i] = bits.RotateLeft8(value, 6) ^ 0xb5
	}
	return plain
}

func residentNoticeTestService(t *testing.T) *Service {
	t.Helper()
	const fixture = `
[dungeon]
` + "`[crack]` `crack`" + `
[/dungeon]
[dungeon]
` + "`[granfloris]` `granfloris` 1 2" + `
[/dungeon]
[server]
1 19 ` + "`crack`" + ` 1 ` + "`[crack]`" + ` 0 0 10 ` + "`granfloris`" + ` 1 ` + "`[granfloris]`" + ` 0 0
[/server]
`
	index, err := channelinfo.Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse resident channel fixture: %v", err)
	}
	catalog, err := channelcatalog.New(index, channelcatalog.Options{ServerID: 1})
	if err != nil {
		t.Fatalf("build resident channel fixture: %v", err)
	}
	return &Service{
		options: options{
			channelServerID:    1,
			channelAdvertiseID: 0,
			serverIP:           "42.240.165.245",
			initialUDPPort1:    defaultInitialUDPPort1,
			initialUDPPort2:    defaultInitialUDPPort2,
			commandCount:       defaultCommandCount,
			notificationCount:  defaultNotificationCount,
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		catalog: catalog,
	}
}

func consumeChannelInfoProto(t *testing.T, raw []byte) (map[protowire.Number][]uint64, map[protowire.Number][][]byte) {
	t.Helper()
	varints := make(map[protowire.Number][]uint64)
	bytesFields := make(map[protowire.Number][][]byte)
	for len(raw) > 0 {
		field, wireType, n := protowire.ConsumeTag(raw)
		if n < 0 {
			t.Fatalf("consume channel info tag: %v", protowire.ParseError(n))
		}
		raw = raw[n:]
		switch wireType {
		case protowire.VarintType:
			value, consumed := protowire.ConsumeVarint(raw)
			if consumed < 0 {
				t.Fatalf("consume channel info field %d: %v", field, protowire.ParseError(consumed))
			}
			varints[field] = append(varints[field], value)
			raw = raw[consumed:]
		case protowire.BytesType:
			value, consumed := protowire.ConsumeBytes(raw)
			if consumed < 0 {
				t.Fatalf("consume channel info field %d: %v", field, protowire.ParseError(consumed))
			}
			bytesFields[field] = append(bytesFields[field], append([]byte(nil), value...))
			raw = raw[consumed:]
		default:
			t.Fatalf("unexpected channel info field %d wire type %d", field, wireType)
		}
	}
	return varints, bytesFields
}

func TestSendPreInitialBootstrapIsWriteSilent(t *testing.T) {
	service := residentNoticeTestService(t)
	crack, _ := service.currentCatalog().Channel(19)
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: crack}

	if err := service.sendPreInitialBootstrap(session); err != nil {
		t.Fatalf("send pre initial bootstrap: %v", err)
	}
	firstLen := conn.write.Len()
	if firstLen != 0 {
		t.Fatalf("pre bootstrap must be write-silent: len=%d", firstLen)
	}
	if err := service.sendPreInitialBootstrap(session); err != nil {
		t.Fatalf("repeat pre initial bootstrap: %v", err)
	}
	if conn.write.Len() != firstLen {
		t.Fatalf("repeat pre bootstrap added %d bytes", conn.write.Len()-firstLen)
	}
}

func TestGameEndpointRequestRequiresNoticeAndKnownShape(t *testing.T) {
	service := residentNoticeTestService(t)
	channel, _ := service.currentCatalog().Channel(19)
	conn := &bufferConn{}
	session := &gameSession{conn: conn, channel: channel, residentChannel: channel}

	beforeNotice, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgGameEndpoint),
		make([]byte, currentChannelReconnectDisplayProbeSize),
		0,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, beforeNotice); err != nil {
		t.Fatalf("handle endpoint request before notice: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("request before CHANNELINFO emitted bytes: %x", conn.write.Bytes())
	}

	if err := service.sendCurrentChannelResidentNotice(session, time.Unix(1_721_020_000, 0)); err != nil {
		t.Fatalf("send CHANNELINFO: %v", err)
	}
	noticeLen := conn.write.Len()

	for index, test := range []struct {
		class   byte
		bodyLen int
	}{
		{class: dnfproto.DefaultChannelClassification, bodyLen: 0},
		{class: 2, bodyLen: currentChannelReconnectDisplayProbeSize},
	} {
		frame, err := dnfproto.BuildChannelPacket(
			uint16(dnfenum.UpperMsgGameEndpoint),
			make([]byte, test.bodyLen),
			uint16(index),
			test.class,
		)
		if err != nil {
			t.Fatalf("build invalid upper op1/%d: %v", test.bodyLen, err)
		}
		if err := service.handleGameUpper(session, frame); err != nil {
			t.Fatalf("handle invalid upper op1/%d: %v", test.bodyLen, err)
		}
		if conn.write.Len() != noticeLen {
			t.Fatalf("invalid upper op1/%d added %d response bytes", test.bodyLen, conn.write.Len()-noticeLen)
		}
	}

	valid, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgGameEndpoint),
		make([]byte, reference90CNChannelReconnectDisplayProbeBodySize),
		3,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, valid); err != nil {
		t.Fatalf("handle valid endpoint request: %v", err)
	}
	handshakeLen := conn.write.Len()
	if handshakeLen <= noticeLen {
		t.Fatal("valid endpoint request did not emit class1/op1")
	}
	if err := service.handleGameCommand(
		session,
		byte(1),
		1,
		make([]byte, reference90CNChannelReconnectDisplayProbeBodySize),
	); err != nil {
		t.Fatalf("handle legacy op1: %v", err)
	}
	if conn.write.Len() != handshakeLen {
		t.Fatalf("legacy op1 added %d response bytes", conn.write.Len()-handshakeLen)
	}
}

func TestGameEndpointHandshakeAcceptsCurrentRequestSizes(t *testing.T) {
	for _, bodyLen := range []int{
		currentChannelReconnectDisplayProbeSize,
		reference90CNChannelReconnectDisplayProbeBodySize,
	} {
		t.Run(strconv.Itoa(bodyLen), func(t *testing.T) {
			service := residentNoticeTestService(t)
			channel, _ := service.currentCatalog().Channel(19)
			conn := &bufferConn{}
			session := &gameSession{conn: conn, channel: channel, residentChannel: channel}

			if err := service.sendCurrentChannelResidentNotice(session, time.Unix(1_721_020_000, 0)); err != nil {
				t.Fatal(err)
			}
			request, err := dnfproto.BuildChannelPacket(
				uint16(dnfenum.UpperMsgGameEndpoint),
				make([]byte, bodyLen),
				1,
				dnfproto.DefaultChannelClassification,
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.handleGameUpper(session, request); err != nil {
				t.Fatalf("handle endpoint request: %v", err)
			}

			first, rest := splitGameServerUpperPacketWithHeader(t, conn.write.Bytes(), service.gameUpperHeaderSize())
			if first.Header.Classification != 0 || first.Header.MsgID != 1 || first.Header.Seq != 0 {
				t.Fatalf("CHANNELINFO packet = %+v", first.Header)
			}
			second, rest := splitGameServerUpperPacketWithHeader(t, rest, service.gameUpperHeaderSize())
			if second.Header.Classification != dnfproto.DefaultChannelClassification ||
				second.Header.MsgID != 1 ||
				second.Header.Seq != 1 {
				t.Fatalf("endpoint success packet = %+v", second.Header)
			}
			if len(rest) != 0 {
				t.Fatalf("endpoint handshake emitted an extra packet: %x", rest)
			}

			wireLen := conn.write.Len()
			if err := service.handleGameUpper(session, request); err != nil {
				t.Fatalf("handle repeated endpoint request: %v", err)
			}
			if conn.write.Len() != wireLen {
				t.Fatalf("repeated endpoint request added %d bytes", conn.write.Len()-wireLen)
			}
		})
	}
}

// TestLegacyGameEndpointRequestCompletesChannelInfoHandshake pins live capture
// game-000121: on channel 253 the current EXE answered class0/op1 CHANNELINFO
// with a legacy cmd=1/type=1 594-byte login request, not with the upper
// envelope. Swallowing it left every channel whose ID fits the u8 town-owner
// field stuck on "connecting" until the client's Error2 watchdog.
func TestLegacyGameEndpointRequestCompletesChannelInfoHandshake(t *testing.T) {
	service := residentNoticeTestService(t)
	crack, _ := service.currentCatalog().Channel(19)
	crack.ID = 253
	crack.Name = "ch.253"
	crack.NoticeName = "ch.253"
	crack.Port = 10253
	conn := &bufferConn{}
	session := &gameSession{
		conn:            conn,
		accountID:       "dnf:1",
		channel:         crack,
		residentChannel: crack,
	}
	now := time.Unix(1_721_020_000, 0)

	if err := service.sendGameConnectionBootstrap(session, now); err != nil {
		t.Fatalf("send account-bound game bootstrap: %v", err)
	}
	if !session.currentChannelResidentNoticeSent || session.gameEndpointSuccessSent {
		t.Fatalf(
			"state after CHANNELINFO = notice:%t success:%t",
			session.currentChannelResidentNoticeSent,
			session.gameEndpointSuccessSent,
		)
	}
	noticeWireLen := conn.write.Len()

	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.GameTypeLogin),
		make([]byte, currentLegacyEndpointRequestBodySize),
	); err != nil {
		t.Fatalf("handle legacy endpoint request: %v", err)
	}
	success, trailing := splitGameServerUpperPacketWithHeader(
		t,
		conn.write.Bytes()[noticeWireLen:],
		service.gameUpperHeaderSize(),
	)
	if success.Header.Classification != dnfproto.DefaultChannelClassification ||
		success.Header.MsgID != uint16(dnfenum.UpperMsgGameEndpoint) {
		t.Fatalf("endpoint success header = %+v, want class1/op1", success.Header)
	}
	if want := upperSuccessBody(service.buildLoginSuccess(crack)); !bytes.Equal(success.Body, want) {
		t.Fatalf("endpoint success body = %x, want %x", success.Body, want)
	}
	if len(trailing) != 0 {
		t.Fatalf("legacy endpoint request emitted trailing bytes: %x", trailing)
	}
	if !session.gameEndpointSuccessSent {
		t.Fatal("endpoint success state was not committed")
	}

	handshakeWireLen := conn.write.Len()
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.GameTypeLogin),
		make([]byte, currentLegacyEndpointRequestBodySize),
	); err != nil {
		t.Fatalf("handle repeated legacy endpoint request: %v", err)
	}
	if conn.write.Len() != handshakeWireLen {
		t.Fatalf("repeated legacy endpoint request added %d bytes", conn.write.Len()-handshakeWireLen)
	}
	if session.channel.ID != 253 || session.residentChannel.ID != 253 {
		t.Fatalf("connected channel changed: channel=%+v resident=%+v", session.channel, session.residentChannel)
	}
}

// TestLegacyGameEndpointRequestIgnoresUnprovedShape keeps the historical
// write-silent behaviour for every legacy op1 body the current EXE has not
// been observed to send, and before CHANNELINFO is committed.
func TestLegacyGameEndpointRequestIgnoresUnprovedShape(t *testing.T) {
	service := residentNoticeTestService(t)
	crack, _ := service.currentCatalog().Channel(19)
	crack.ID = 253
	crack.Port = 10253

	t.Run("unproved body length", func(t *testing.T) {
		conn := &bufferConn{}
		session := &gameSession{conn: conn, accountID: "dnf:1", channel: crack, residentChannel: crack}
		if err := service.sendGameConnectionBootstrap(session, time.Unix(1_721_020_000, 0)); err != nil {
			t.Fatalf("send account-bound game bootstrap: %v", err)
		}
		wireLen := conn.write.Len()
		if err := service.handleGameCommand(
			session,
			byte(dnfenum.GameCmdCommand),
			uint16(dnfenum.GameTypeLogin),
			make([]byte, currentLegacyEndpointRequestBodySize+1),
		); err != nil {
			t.Fatalf("handle legacy endpoint request: %v", err)
		}
		if conn.write.Len() != wireLen {
			t.Fatalf("unproved legacy op1 added %d bytes", conn.write.Len()-wireLen)
		}
		if session.gameEndpointSuccessSent {
			t.Fatal("unproved legacy op1 committed the endpoint success state")
		}
	})

	t.Run("before channelinfo", func(t *testing.T) {
		conn := &bufferConn{}
		session := &gameSession{conn: conn, accountID: "dnf:1", channel: crack, residentChannel: crack}
		if err := service.handleGameCommand(
			session,
			byte(dnfenum.GameCmdCommand),
			uint16(dnfenum.GameTypeLogin),
			make([]byte, currentLegacyEndpointRequestBodySize),
		); err != nil {
			t.Fatalf("handle legacy endpoint request: %v", err)
		}
		if conn.write.Len() != 0 {
			t.Fatalf("legacy op1 before CHANNELINFO wrote %d bytes", conn.write.Len())
		}
	})
}
