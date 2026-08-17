package dnfbridge

import (
	"bytes"
	"context"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestRepresentAccountNameCommandIDsMatchCurrentClientEnum(t *testing.T) {
	tests := []struct {
		name string
		got  uint16
		want uint16
	}{
		{name: "update", got: currentCmdUpdateRepresentAccountName, want: uint16(dnfenum.CmdPacketUpdateRepresentAccountName)},
		{name: "duplicate-check", got: currentCmdRepresentNameDuplicateCheck, want: uint16(dnfenum.CmdPacketRepresentAccountNameDuplicateCheck)},
		{name: "change", got: currentCmdChangeRepresentAccountName, want: uint16(dnfenum.CmdPacketChangeRepresentAccountName)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("command id=%d want=%d", test.got, test.want)
			}
		})
	}
	if currentCmdUpdateRepresentAccountName != 1437 ||
		currentCmdRepresentNameDuplicateCheck != 1443 ||
		currentCmdChangeRepresentAccountName != 1444 {
		t.Fatalf(
			"current-client command ids update=%d duplicate=%d change=%d",
			currentCmdUpdateRepresentAccountName,
			currentCmdRepresentNameDuplicateCheck,
			currentCmdChangeRepresentAccountName,
		)
	}
}

func TestUpperGetUserInfoDefersRepresentNameRegistrationUntilSceneReady(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "dnf:new",
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "represent-name-new", channel: channelcatalog.Channel{ID: 19}}

	if err := service.sendUpperGetUserInfoBootstrap(session); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("unregistered account GET_USERINFO emitted %d trailing bytes", len(rest))
	}
	if packet.Header.Classification != 0 || packet.Header.MsgID != uint16(dnfenum.UpperMsgCharacterRoster) {
		t.Fatalf("GET_USERINFO header = %+v, want passive character roster", packet.Header)
	}
	if session.representAccountNamePending || session.representAccountNameRegistrationSent {
		t.Fatalf("GET_USERINFO mutated scene registration state: %+v", session)
	}
}

func TestRepresentAccountNameRegistrationPersistsWithoutResumingRoleSelect(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "20",
		AccountID:   "dnf:new",
		Slot:        0,
		Name:        "hero",
		Level:       1,
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          "dnf:new",
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		adventureGroupTable: loadAdventureGroupTestTables(t),
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:    connection,
		connID:  "represent-name-register",
		channel: channelcatalog.Channel{ID: 19},
	}
	request := []byte{6, 0, 0, 0, 'g', 'r', 'o', 'u', 'p', '1'}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), currentCmdRepresentNameDuplicateCheck, request); err != nil {
		t.Fatal(err)
	}
	check, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 || check.Header.MsgID != currentCmdRepresentNameDuplicateCheck ||
		!bytes.Equal(check.Body, append([]byte{1}, request...)) {
		t.Fatalf("duplicate-check response header=%+v body=%x trailing=%d", check.Header, check.Body, len(rest))
	}
	account, found, err := repositories.Account.Load(context.Background(), "dnf:new")
	if err != nil {
		t.Fatal(err)
	}
	if found && account.RepresentAccountName != "" {
		t.Fatalf("duplicate check persisted represent account name: %+v", account)
	}

	connection.write.Reset()
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), currentCmdUpdateRepresentAccountName, request); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.MsgID != currentCmdUpdateRepresentAccountName || !bytes.Equal(ack.Body, append([]byte{1}, request...)) {
		t.Fatalf("registration ACK header=%+v body=%x", ack.Header, ack.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("scene registration unexpectedly resumed role-select packets: %x", rest)
	}
	account, found, err = repositories.Account.Load(context.Background(), "dnf:new")
	if err != nil || !found || account.RepresentAccountName != "group1" {
		t.Fatalf("persisted account found=%v err=%v record=%+v", found, err, account)
	}
	createdDate := account.Metadata[adventureGroupCreatedDateMetadataKey]
	if createdDate != time.Now().UTC().Format(adventureGroupCreatedDateLayout) {
		t.Fatalf("persisted adventure-group created date = %q, want current UTC date", createdDate)
	}
	if session.representAccountNamePending {
		t.Fatal("scene registration unexpectedly set role-select gate")
	}
	connection.write.Reset()
	if err := service.sendRepresentAccountNameRegistrationAfterScene(session, "test_after_scene_init"); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 || !session.representAccountNameRegistrationSent {
		t.Fatalf("durably registered login emitted registration state: bytes=%d session=%+v", connection.write.Len(), session)
	}
}

func TestRepresentAccountNameRegistrationAfterSceneSendsExactlyOnce(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "dnf:new", gameUpperHeader: gameUpperHeaderServer16, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "represent-name-after-scene", channel: channelcatalog.Channel{ID: 19}}

	if err := service.sendRepresentAccountNameRegistrationAfterScene(session, "test_after_scene_init"); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 || packet.Header.Classification != 0 || packet.Header.MsgID != currentRepresentAccountNameStateMsgID {
		t.Fatalf("after-scene registration packet=%+v body=%x trailing=%x", packet.Header, packet.Body, rest)
	}
	firstLength := connection.write.Len()
	if err := service.sendRepresentAccountNameRegistrationAfterScene(session, "test_duplicate"); err != nil {
		t.Fatal(err)
	}
	if got := connection.write.Len(); got != firstLength || !session.representAccountNameRegistrationSent {
		t.Fatalf("duplicate scene registration bytes=%d want=%d session=%+v", got, firstLength, session)
	}
}

func TestRepresentAccountNameDuplicateReturnsCurrentExeCode20(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID:            "dnf:existing",
		RepresentAccountName: "group1",
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{accountID: "dnf:new", gameUpperHeader: gameUpperHeaderServer16, gameUpperBodyCodec: gameUpperBodyCodecPlain},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{conn: connection, connID: "represent-name-duplicate"}
	request := []byte{6, 0, 0, 0, 'g', 'r', 'o', 'u', 'p', '1'}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), currentCmdRepresentNameDuplicateCheck, request); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 || !bytes.Equal(packet.Body, []byte{0, representAccountNameDuplicateCode}) {
		t.Fatalf("duplicate response body=%x trailing=%d", packet.Body, len(rest))
	}
}

func TestParseRepresentAccountNameRejectsTrailingBytes(t *testing.T) {
	request := []byte{6, 0, 0, 0, 'g', 'r', 'o', 'u', 'p', '1', 0}
	if name, encoded, code, ok := parseRepresentAccountName(request); ok || name != "" || encoded != nil || code != representAccountNameInvalidCode {
		t.Fatalf("trailing-byte request parsed as name=%q encoded=%x code=%d ok=%v", name, encoded, code, ok)
	}
}
