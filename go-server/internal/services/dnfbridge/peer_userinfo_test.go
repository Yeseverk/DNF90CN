package dnfbridge

import (
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestHandleCurrentPeerUserInfoSendsTargetMode3WithoutRosterBootstrap(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	for _, account := range []dnfrepo.AccountRecord{
		{AccountID: "dnf:1", State: "active"},
		{AccountID: "dnf:2", State: "active"},
	} {
		if err := repositories.Account.Save(ctx, account); err != nil {
			t.Fatal(err)
		}
	}
	for _, character := range []dnfrepo.CharacterRecord{
		{CharacterID: "1", AccountID: "dnf:1", Name: "pouut", Job: "0", Level: 90},
		{CharacterID: "5", AccountID: "dnf:2", Name: "来来来", Job: "1", Level: 90, Stats: map[string]int64{
			"pvp_grade": 2, "pvp_rank_point": 3456, "expert_job_type": 3,
		}},
	} {
		if err := repositories.Character.Save(ctx, character); err != nil {
			t.Fatal(err)
		}
	}
	sourceConn := &bufferConn{}
	channel := channelcatalog.Channel{ID: 42}
	source := &gameSession{conn: sourceConn, channel: channel, accountID: "dnf:1", selectedCharacterID: 1, townActorOwnerChannel: 42}
	target := &gameSession{conn: &bufferConn{}, channel: channel, accountID: "dnf:2", selectedCharacterID: 5, townActorOwnerChannel: 42}
	service := &Service{
		options:             options{gameUpperHeader: gameUpperHeaderServer16, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers:       newOnlinePlayerManager(),
		adventureGroupTable: loadAdventureGroupTestTables(t),
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	service.bindGameSessionCharacter(source, 1)
	service.bindGameSessionCharacter(target, 5)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 1, TownID: 1, AreaID: 2, Session: source})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 5, TownID: 1, AreaID: 2, Session: target})

	if err := service.handleGameCommand(
		source,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.GameTypeGetUserInfo),
		[]byte{5, 0, currentPeerUserInfoMode},
	); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitLongHengGameServerUpperPacket(t, sourceConn.write.Bytes())
	if packet.Header.Classification != 0 || packet.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) || len(rest) != 0 {
		t.Fatalf("peer userinfo header=%+v trailing=%d", packet.Header, len(rest))
	}
	if len(packet.Body) < 0x17 || packet.Body[0] != currentPeerUserInfoMode ||
		binary.LittleEndian.Uint16(packet.Body[0x0d:0x0f]) != 5 || packet.Body[4] != 42 {
		t.Fatalf("peer mode3 body head=%x", packet.Body[:min(len(packet.Body), 32)])
	}
	if source.rosterRequested {
		t.Fatal("peer mode3 request was misrouted to the character roster bootstrap")
	}
	// With no equipped create rows the current mode3 tail reaches
	// sub_2005520 at these fixed offsets: u16 duel grade, u32 rank points,
	// empty WSTR, then u8 expert-job type and its UI-state byte.
	if len(packet.Body) <= 165 || binary.LittleEndian.Uint16(packet.Body[154:156]) != 2 ||
		binary.LittleEndian.Uint32(packet.Body[156:160]) != 3456 ||
		binary.LittleEndian.Uint32(packet.Body[160:164]) != 0 || packet.Body[164] != 3 {
		t.Fatalf("peer mode3 profession/rank tail=%x", packet.Body[154:min(len(packet.Body), 166)])
	}
}
