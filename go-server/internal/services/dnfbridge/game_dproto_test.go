package dnfbridge

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dproto"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	"longheng.io/server/internal/platform/config"
)

type testDprotoProvider struct {
	session *testDprotoSession
	info    dproto.ConnectionInfo
}

func (provider *testDprotoProvider) Open(_ context.Context, info dproto.ConnectionInfo) (dproto.Session, error) {
	provider.info = info
	return provider.session, nil
}

type testDprotoSession struct {
	decodeResult    dproto.DecodeResult
	decodeInput     []byte
	encodeInputs    [][]byte
	controlOpcode   uint16
	controlInput    []byte
	controlOutbound [][]byte
	closed          bool
}

func (session *testDprotoSession) DecodeClient(_ context.Context, packet []byte) (dproto.DecodeResult, error) {
	session.decodeInput = append([]byte(nil), packet...)
	return cloneDprotoDecodeResult(session.decodeResult), nil
}

func (session *testDprotoSession) EncodeServer(_ context.Context, inner []byte) (dproto.EncodeResult, error) {
	session.encodeInputs = append(session.encodeInputs, append([]byte(nil), inner...))
	wire, err := dnfproto.BuildChannelPacket(dnfproto.DprotoServerEnvelopeOpcode, inner, 91, dnfproto.DefaultChannelClassification)
	if err != nil {
		return dproto.EncodeResult{}, err
	}
	return dproto.EncodeResult{Packet: wire, Protected: true}, nil
}

func (session *testDprotoSession) HandleClientControl(_ context.Context, opcode uint16, packet []byte) ([][]byte, error) {
	session.controlOpcode = opcode
	session.controlInput = append([]byte(nil), packet...)
	return cloneDprotoPackets(session.controlOutbound), nil
}

func (session *testDprotoSession) Close() error {
	session.closed = true
	return nil
}

func cloneDprotoDecodeResult(result dproto.DecodeResult) dproto.DecodeResult {
	return dproto.DecodeResult{
		InnerPackets:    cloneDprotoPackets(result.InnerPackets),
		OutboundPackets: cloneDprotoPackets(result.OutboundPackets),
	}
}

func cloneDprotoPackets(packets [][]byte) [][]byte {
	cloned := make([][]byte, len(packets))
	for index, packet := range packets {
		cloned[index] = append([]byte(nil), packet...)
	}
	return cloned
}

func TestOptionsNormalizeGameDprotoMode(t *testing.T) {
	t.Setenv("DNFBRIDGE_DPROTO_MODE", "native_dproto")
	if got := optionsFromConfig(config.ServiceConfig{}).gameDprotoMode; got != gameDprotoModeNative {
		t.Fatalf("gameDprotoMode=%q", got)
	}
	t.Setenv("DNFBRIDGE_DPROTO_MODE", "unknown")
	if got := optionsFromConfig(config.ServiceConfig{}).gameDprotoMode; got != gameDprotoModeLegacy {
		t.Fatalf("fallback gameDprotoMode=%q", got)
	}
}

func TestNativeDprotoModeRequiresServerRoleProvider(t *testing.T) {
	service := &Service{options: options{gameDprotoMode: gameDprotoModeNative}}
	if err := service.validateDprotoConfiguration(); !errors.Is(err, dproto.ErrProviderUnavailable) {
		t.Fatalf("error=%v", err)
	}
	service.dprotoProvider = &testDprotoProvider{session: &testDprotoSession{}}
	if err := service.validateDprotoConfiguration(); err != nil {
		t.Fatalf("validate with provider: %v", err)
	}
}

func TestNativeDproto1517DecryptsAndBusinessResponseUses1467(t *testing.T) {
	inner, err := dnfproto.BuildChannelPacket(63, nil, 9, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	native := &testDprotoSession{decodeResult: dproto.DecodeResult{InnerPackets: [][]byte{inner}}}
	connection := &bufferConn{}
	session := &gameSession{
		conn:    connection,
		connID:  "native-dproto-test",
		ctx:     context.Background(),
		dproto:  native,
		channel: channelcatalog.Channel{ID: 1},
	}
	service := &Service{options: options{gameDprotoMode: gameDprotoModeNative, maxPacketBytes: 4096}}
	outer, err := dnfproto.BuildDprotoClientEnvelope([]byte{0x00, 0xad, 0x00, 0x10, 0x00, 1, 2}, 55)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameDprotoPacket(session, outer); err != nil {
		t.Fatalf("handle op1517: %v", err)
	}
	if !bytes.Equal(native.decodeInput, outer) {
		t.Fatalf("decode input=%x", native.decodeInput)
	}
	if len(native.encodeInputs) != 1 {
		t.Fatalf("encode calls=%d", len(native.encodeInputs))
	}
	responseInner, err := dnfproto.ParseChannelPacket(native.encodeInputs[0])
	if err != nil {
		t.Fatalf("parse response inner: %v", err)
	}
	if responseInner.Header.MsgID != 63 || !bytes.Equal(responseInner.Body, []byte{1}) {
		t.Fatalf("response inner header=%+v body=%x", responseInner.Header, responseInner.Body)
	}
	responseOuter, err := dnfproto.ParseChannelPacket(connection.write.Bytes())
	if err != nil {
		t.Fatalf("parse response outer: %v", err)
	}
	if responseOuter.Header.MsgID != dnfproto.DprotoServerEnvelopeOpcode {
		t.Fatalf("response outer=%+v", responseOuter.Header)
	}
}

func TestNativeDproto1517MayAwaitMoreStateWithoutInnerPacket(t *testing.T) {
	native := &testDprotoSession{}
	connection := &bufferConn{}
	session := &gameSession{
		conn:    connection,
		connID:  "native-dproto-wait",
		ctx:     context.Background(),
		dproto:  native,
		channel: channelcatalog.Channel{ID: 1},
	}
	service := &Service{options: options{gameDprotoMode: gameDprotoModeNative, maxPacketBytes: 4096}}
	outer, err := dnfproto.BuildDprotoClientEnvelope([]byte{0x00, 0x01}, 56)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameDprotoPacket(session, outer); err != nil {
		t.Fatalf("handle state-only op1517: %v", err)
	}
	if !bytes.Equal(native.decodeInput, outer) {
		t.Fatalf("decode input=%x", native.decodeInput)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("unexpected response=%x", connection.write.Bytes())
	}
}

func TestNativeDprotoCallbackIsOwnedByProvider(t *testing.T) {
	controlOut, err := dnfproto.BuildChannelPacket(dnfproto.DprotoServerControlOpcode, []byte{1, 2, 3}, 11, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	native := &testDprotoSession{controlOutbound: [][]byte{controlOut}}
	connection := &bufferConn{}
	session := &gameSession{
		conn:    connection,
		connID:  "native-dproto-control",
		ctx:     context.Background(),
		dproto:  native,
		channel: channelcatalog.Channel{ID: 1},
	}
	service := &Service{options: options{gameDprotoMode: gameDprotoModeNative, maxPacketBytes: 4096}}
	callback, err := dnfproto.BuildChannelPacket(dnfproto.DprotoCallbackOpcode, []byte{1, 0}, 12, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, callback); err != nil {
		t.Fatalf("handle callback: %v", err)
	}
	if native.controlOpcode != dnfproto.DprotoCallbackOpcode || !bytes.Equal(native.controlInput, callback) {
		t.Fatalf("control opcode=%d input=%x", native.controlOpcode, native.controlInput)
	}
	if !bytes.Equal(connection.write.Bytes(), controlOut) {
		t.Fatalf("control output=%x want=%x", connection.write.Bytes(), controlOut)
	}
}

func TestLegacyModeDefersOpaque1517WithoutLegacyDispatcher(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "packet.log")
	logger, err := openPacketLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{options: options{gameDprotoMode: gameDprotoModeLegacy, maxPacketBytes: 4096}, packetLog: logger}
	session := &gameSession{conn: &bufferConn{}, connID: "legacy-dproto", channel: channelcatalog.Channel{ID: 1}}
	outer, err := dnfproto.BuildDprotoClientEnvelope([]byte{0x00, 0xad, 0x00, 0x10, 0x00}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameStreamPacket(session, dnfproto.LatestGameStreamPacket{Kind: dnfproto.LatestGameStreamDproto, Data: outer}); err != nil {
		t.Fatalf("legacy dispatch: %v", err)
	}
	if session.conn.(*bufferConn).write.Len() != 0 {
		t.Fatalf("unexpected legacy response=%x", session.conn.(*bufferConn).write.Bytes())
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "kind=game-dproto-opaque-deferred") {
		t.Fatalf("opaque op1517 was not logged as DPROTO provider gap: %q", text)
	}
	if strings.Contains(text, "kind=game-legacy-meta") {
		t.Fatalf("opaque op1517 leaked into legacy dispatcher: %q", text)
	}
}
