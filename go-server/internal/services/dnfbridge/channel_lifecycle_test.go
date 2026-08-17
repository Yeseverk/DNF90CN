package dnfbridge

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"longheng.io/server/internal/modules/dnf/channelinfo"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func TestLoadChannelAssetsKeepsRawCatalogAndCachesOnlineScript(t *testing.T) {
	service := &Service{options: options{
		channelInfoFile:    tempChannelInfoFile(t),
		channelServerID:    1,
		channelAdvertiseID: 0,
	}}

	catalog, script, err := service.loadChannelAssets()
	if err != nil {
		t.Fatal(err)
	}
	rawChannel, ok := catalog.Channel(dnfenum.BootstrapChannelID)
	if !ok || rawChannel.ServerID != 1 || rawChannel.Type != 1 {
		t.Fatalf("raw bootstrap channel = %+v, %v; want source server 1 and original type 1", rawChannel, ok)
	}
	online, err := channelinfo.Parse(script)
	if err != nil {
		t.Fatal(err)
	}
	onlineChannel, ok := online.ServerChannel(0, dnfenum.BootstrapChannelID)
	if !ok || onlineChannel.Type != 22 {
		t.Fatalf("online bootstrap channel = %+v, %v; want directory type 22", onlineChannel, ok)
	}

	service.catalog = catalog
	service.channelScript = append([]byte(nil), script...)
	directory, err := service.buildChannelInfoBody([]byte("cain"))
	if err != nil {
		t.Fatal(err)
	}
	plainDirectory, err := decryptCompressedChannelData(directory, service.aesKey())
	if err != nil {
		t.Fatal(err)
	}
	if len(plainDirectory) < 12 ||
		binary.LittleEndian.Uint32(plainDirectory[0:4]) != 1 ||
		binary.LittleEndian.Uint32(plainDirectory[4:8]) != 0 {
		t.Fatalf("online directory prefix = %x, want one server advertised as 0", plainDirectory)
	}

	notice, err := service.buildCurrentChannelResidentNotice(rawChannel, time.Unix(1_721_020_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := consumeChannelInfoProto(t, notice[4:])
	for field, want := range map[uint64]uint64{7: 0, 8: dnfenum.BootstrapChannelID, 9: currentChannelResidentControllerIndex} {
		values := fields[protowire.Number(field)]
		if len(values) != 1 || values[0] != want {
			t.Fatalf("current channel notice field %d = %v, want [%d]", field, values, want)
		}
	}
}

func TestLoadBundledChannelAssetsBuildsOnlineDirectory(t *testing.T) {
	service := &Service{options: options{
		channelInfoFile:    filepath.Join("testdata", "channel_info.etc"),
		channelServerID:    1,
		channelAdvertiseID: 0,
	}}

	catalog, script, err := service.loadChannelAssets()
	if err != nil {
		t.Fatal(err)
	}
	if len(script) == 0 {
		t.Fatal("complete online channel script is empty")
	}
	if strings.Contains(string(script), " `` ``\n") {
		t.Fatal("90cn online script contains a duplicate record terminator")
	}
	if !strings.Contains(string(script), "19 `<chn_channel_info_028>` 11 `[granfloris]`") {
		t.Fatalf("online script did not preserve the complete ETC mapping for channel 19:\n%s", script)
	}
	if strings.Contains(string(script), "19 `<4::chn_channel_info_012>`") {
		t.Fatalf("online script contains the removed legacy mapping for channel 19:\n%s", script)
	}
	if _, ok := catalog.Channel(38); ok {
		t.Fatal("raw bundled catalog unexpectedly contains channel 38")
	}
	ordinary201, ok := catalog.Channel(201)
	if !ok || ordinary201.Type != 11 || ordinary201.Group != "metro" {
		t.Fatalf("raw bundled channel 201 = %+v, %v; want source type 11 metro", ordinary201, ok)
	}
	hiddenRaid, ok := catalog.Channel(241)
	if !ok || hiddenRaid.Type != 32 || hiddenRaid.Group != "luke_raid" {
		t.Fatalf("raw bundled channel 241 = %+v, %v; want source type 32 luke_raid", hiddenRaid, ok)
	}
	online, err := channelinfo.Parse(script)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(online.ChannelsForServer(0)); got != 118 {
		t.Fatalf("90cn online channels = %d, want 118", got)
	}
	ordinary, ok := online.ServerChannel(0, 19)
	if !ok ||
		ordinary.Type != 11 ||
		ordinary.NameKey != "chn_channel_info_028" ||
		ordinary.AreaKey != "granfloris" {
		t.Fatalf("online channel 19 = %+v, %v; want complete ETC mapping", ordinary, ok)
	}
	if channel38, ok := online.ServerChannel(0, 38); ok {
		t.Fatalf("online directory unexpectedly contains channel 38: %+v", channel38)
	}
}

func TestLegacyBootstrapDirectoryClosesConnection(t *testing.T) {
	service := channelLifecycleTestService(t)
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		service.handleChannelConn(ctx, server)
		close(done)
	}()

	writeLegacyLifecyclePacket(t, client, dnfproto.BuildLegacyClientPacket(dnfenum.LegacyMsgDofLoginPreface, make([]byte, dnfproto.DofLoginTokenSize)))
	if packet := readLegacyLifecyclePacket(t, client); packet[1] != legacyMsgID(dnfenum.LegacyMsgDofLoginAck) {
		t.Fatalf("login ACK message = %d", packet[1])
	}
	writeLegacyLifecyclePacket(t, client, dnfproto.BuildLegacyClientPacket(dnfenum.LegacyMsgDofGetScript, nil))
	if packet := readLegacyLifecyclePacket(t, client); packet[1] != legacyMsgID(dnfenum.LegacyMsgDofScript) {
		t.Fatalf("script message = %d", packet[1])
	}
	ask := dnfproto.BuildLegacyClientPacket(dnfenum.LegacyMsgAskChannelInfo, []byte("cain"))
	writeLegacyLifecyclePacket(t, client, ask)
	if packet := readLegacyLifecyclePacket(t, client); packet[1] != legacyMsgID(dnfenum.LegacyMsgChannelInfo) {
		t.Fatalf("directory message = %d", packet[1])
	}

	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	var trailing [1]byte
	if _, err := client.Read(trailing[:]); err != io.EOF {
		t.Fatalf("read after bootstrap directory = %v, want EOF", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bootstrap channel handler did not stop")
	}
	_ = client.Close()
}

func TestLegacyDirectoryRefreshKeepsConnectionOpen(t *testing.T) {
	service := channelLifecycleTestService(t)
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.handleChannelConn(ctx, server)
		close(done)
	}()

	writeLegacyLifecyclePacket(t, client, dnfproto.BuildLegacyClientPacket(dnfenum.LegacyMsgDofLoginPreface, make([]byte, dnfproto.DofLoginTokenSize)))
	_ = readLegacyLifecyclePacket(t, client)
	ask := dnfproto.BuildLegacyClientPacket(dnfenum.LegacyMsgAskChannelInfo, []byte("cain"))
	writeLegacyLifecyclePacket(t, client, ask)
	if packet := readLegacyLifecyclePacket(t, client); packet[1] != legacyMsgID(dnfenum.LegacyMsgChannelInfo) {
		t.Fatalf("refresh response message = %d, want directory without script", packet[1])
	}

	_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	var trailing [1]byte
	if _, err := client.Read(trailing[:]); err == nil {
		t.Fatal("refresh connection returned unexpected trailing data")
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("read after refresh directory = %v, want open connection timeout", err)
	}

	cancel()
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh channel handler did not stop")
	}
}

func channelLifecycleTestService(t *testing.T) *Service {
	t.Helper()
	return &Service{
		options: options{
			serverIP:           "127.0.0.1",
			channelInfoFile:    tempChannelInfoFile(t),
			channelServerID:    1,
			channelAdvertiseID: 0,
		},
		catalog: testTowerCatalog(t),
	}
}

func writeLegacyLifecyclePacket(t *testing.T, writer io.Writer, packet []byte) {
	t.Helper()
	if _, err := writer.Write(packet); err != nil {
		t.Fatal(err)
	}
}

func readLegacyLifecyclePacket(t *testing.T, reader io.Reader) []byte {
	t.Helper()
	header := make([]byte, dnfproto.LegacyChannelHeaderSize)
	if _, err := io.ReadFull(reader, header); err != nil {
		t.Fatal(err)
	}
	length := int(binary.LittleEndian.Uint32(header[2:6]))
	if length < len(header) || length > 1<<20 {
		t.Fatalf("legacy packet length = %d, header=%x", length, header)
	}
	body := make([]byte, length-len(header))
	if _, err := io.ReadFull(reader, body); err != nil {
		t.Fatal(err)
	}
	return append(header, body...)
}

func TestCachedOnlineScriptIsUsedWithoutReadingRemovedFile(t *testing.T) {
	service := &Service{
		options:       options{channelInfoFile: "missing-after-start.etc"},
		channelScript: []byte("[server]\n0\n19 `cached` 22 `[crack]` ``\n[/server]\n"),
	}
	encrypted, err := service.buildScriptBody()
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptCompressedChannelData(encrypted, service.aesKey())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plain), "19 `cached` 22") {
		t.Fatalf("cached script was not sent: %q", plain)
	}
}
