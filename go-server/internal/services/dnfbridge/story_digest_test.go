package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	dnfenum "longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func requireCurrentStoryDigestLastLevelPacket(t *testing.T, stream []byte, want uint32) []byte {
	t.Helper()
	packet, rest := splitGameServerUpperPacket(t, stream)
	if packet.Header.Classification != 0 || packet.Header.MsgID != currentStoryDigestLastLevelMsgID {
		t.Fatalf("story digest state = class %d msg %d body=%x", packet.Header.Classification, packet.Header.MsgID, packet.Body)
	}
	if len(packet.Body) != 4 || binary.LittleEndian.Uint32(packet.Body) != want {
		t.Fatalf("story digest state body=%x, want raw u32 %d", packet.Body, want)
	}
	return rest
}

func TestOldCharacterStoryDigestFirstSelectAcceptAndRelog(t *testing.T) {
	service, repositories := newStoryDigestTestService(t, dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        0,
		Name:        "legacy",
		Job:         "0",
		Level:       35,
		Stats: map[string]int64{
			currentDungeonTutorialCompletedKey: currentDungeonTutorialCompleteFlag,
		},
	})
	session := newStoryDigestTestSession()
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, 77)
	if err := service.sendUpperCSharpSelectInit(session, request); err != nil {
		t.Fatal(err)
	}
	selectAck, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if selectAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		selectAck.Header.MsgID != uint16(dnfenum.CmdPacketSelectCharacter) {
		t.Fatalf("select ack=%+v body=%x", selectAck.Header, selectAck.Body)
	}
	rest = requireCurrentStoryDigestLastLevelPacket(t, rest, 0)
	if len(rest) != 0 {
		t.Fatalf("old completed character emitted scene packets before client route: %x", rest)
	}

	connection := session.conn.(*bufferConn)
	connection.write.Reset()
	accepted, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketStoryDigestUpdate),
		nil,
		0,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, accepted); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("bodyless op1445 fabricated an S2C ACK: %x", connection.write.Bytes())
	}
	stored, found, err := repositories.Character.Load(context.Background(), "77")
	if err != nil || !found {
		t.Fatalf("load advanced character found=%t err=%v", found, err)
	}
	if stored.Stats[dnfrepo.CharacterStoryDigestLastLevelStatKey] != 35 ||
		stored.Stats[dnfrepo.CharacterStoryDigestMigrationVersionStatKey] != int64(dnfrepo.CurrentCharacterStoryDigestMigrationVersion) {
		t.Fatalf("advanced story state=%#v", stored.Stats)
	}

	relogin := newStoryDigestTestSession()
	if err := service.sendUpperCSharpSelectInit(relogin, request); err != nil {
		t.Fatal(err)
	}
	_, rest = splitGameServerUpperPacket(t, relogin.conn.(*bufferConn).write.Bytes())
	rest = requireCurrentStoryDigestLastLevelPacket(t, rest, 35)
	if len(rest) != 0 {
		t.Fatalf("relogin emitted unexpected early scene packets: %x", rest)
	}
}

func TestNewCharacterStoryDigestStartsMigratedBeforeTutorialScene(t *testing.T) {
	stats := defaultCreatedCharacterStatsFromRequest(createCharacterRequest{})
	if got := stats[dnfrepo.CharacterStoryDigestLastLevelStatKey]; got != newCharacterInitialLevel {
		t.Fatalf("new character story level=%d, want %d", got, newCharacterInitialLevel)
	}
	if got := stats[dnfrepo.CharacterStoryDigestMigrationVersionStatKey]; got != int64(dnfrepo.CurrentCharacterStoryDigestMigrationVersion) {
		t.Fatalf("new character story migration=%d, want %d", got, dnfrepo.CurrentCharacterStoryDigestMigrationVersion)
	}
	service, _ := newStoryDigestTestService(t, dnfrepo.CharacterRecord{
		CharacterID: "78",
		AccountID:   defaultAccountPrefix + "1",
		Slot:        0,
		Name:        "origin-created",
		Job:         "0",
		Level:       newCharacterInitialLevel,
		Stats:       stats,
	})
	session := newStoryDigestTestSession()
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, 78)
	if err := service.sendUpperCSharpSelectInit(session, request); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketSelectCharacter) {
		t.Fatalf("first packet=%+v", ack.Header)
	}
	rest = requireCurrentStoryDigestLastLevelPacket(t, rest, newCharacterInitialLevel)
	enter, _ := splitGameServerUpperPacket(t, rest)
	if enter.Header.Classification != dnfproto.DefaultChannelClassification ||
		enter.Header.MsgID != uint16(dnfenum.CmdPacketEnterSelectDungeon) {
		t.Fatalf("packet after story state=%+v body=%x, want tutorial op15", enter.Header, enter.Body)
	}
}

func TestStoryDigestUpdateRejectsWrongClassAndNonemptyBody(t *testing.T) {
	service, repositories := newStoryDigestTestService(t, dnfrepo.CharacterRecord{
		CharacterID: "79",
		AccountID:   defaultAccountPrefix + "1",
		Level:       70,
		Stats:       map[string]int64{},
	})
	session := newStoryDigestTestSession()
	session.selectedCharacterID = 79
	for _, test := range []struct {
		name  string
		class byte
		body  []byte
	}{
		{name: "wrong class", class: 2},
		{name: "nonempty", class: dnfproto.DefaultChannelClassification, body: []byte{0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			frame, err := dnfproto.BuildChannelPacket(uint16(dnfenum.CmdPacketStoryDigestUpdate), test.body, 0, test.class)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.handleGameUpper(session, frame); err != nil {
				t.Fatal(err)
			}
		})
	}
	stored, found, err := repositories.Character.Load(context.Background(), "79")
	if err != nil || !found {
		t.Fatalf("load found=%t err=%v", found, err)
	}
	if stored.Stats[dnfrepo.CharacterStoryDigestLastLevelStatKey] != 0 ||
		stored.Stats[dnfrepo.CharacterStoryDigestMigrationVersionStatKey] != 0 {
		t.Fatalf("invalid updates mutated story state=%#v", stored.Stats)
	}
	if got := session.conn.(*bufferConn).write.Bytes(); !bytes.Equal(got, nil) {
		t.Fatalf("invalid updates wrote response=%x", got)
	}
}

func TestStoryDigestUpdateAdvancesAcrossLaterLevelSegments(t *testing.T) {
	service, repositories := newStoryDigestTestService(t, dnfrepo.CharacterRecord{
		CharacterID: "80",
		AccountID:   defaultAccountPrefix + "1",
		Level:       20,
		Stats: map[string]int64{
			dnfrepo.CharacterStoryDigestLastLevelStatKey:        10,
			dnfrepo.CharacterStoryDigestMigrationVersionStatKey: int64(dnfrepo.CurrentCharacterStoryDigestMigrationVersion),
		},
	})
	session := newStoryDigestTestSession()
	session.selectedCharacterID = 80
	accepted, err := dnfproto.BuildChannelPacket(uint16(dnfenum.CmdPacketStoryDigestUpdate), nil, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, accepted); err != nil {
		t.Fatal(err)
	}
	record, found, err := repositories.Character.Load(context.Background(), "80")
	if err != nil || !found || record.Stats[dnfrepo.CharacterStoryDigestLastLevelStatKey] != 20 {
		t.Fatalf("first later segment found=%t err=%v record=%#v", found, err, record)
	}
	record.Level = 50
	if err := repositories.Character.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, accepted); err != nil {
		t.Fatal(err)
	}
	record, found, err = repositories.Character.Load(context.Background(), "80")
	if err != nil || !found || record.Stats[dnfrepo.CharacterStoryDigestLastLevelStatKey] != 50 {
		t.Fatalf("second later segment found=%t err=%v record=%#v", found, err, record)
	}
}

func newStoryDigestTestService(t *testing.T, record dnfrepo.CharacterRecord) (*Service, dnfrepo.Group) {
	t.Helper()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountPrefix: defaultAccountPrefix},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	return service, repositories
}

func newStoryDigestTestSession() *gameSession {
	channel := channelcatalog.Channel{ID: 16, Type: 1, Name: "ch.16", Port: 10016}
	return &gameSession{
		conn:            &bufferConn{},
		channel:         channel,
		residentChannel: channel,
	}
}
