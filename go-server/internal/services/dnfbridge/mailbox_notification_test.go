package dnfbridge

import (
	"bytes"
	"context"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOnlineMailboxRecipientGetsCurrentClass0Alarm(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Mailbox.Save(ctx, dnfrepo.MailboxRecord{
		CharacterID: "88",
		Mails: map[string]dnfrepo.MailRecord{
			"1": {MailID: "1", CreatedAt: time.Now().UTC()},
		},
	}); err != nil {
		t.Fatalf("save mailbox: %v", err)
	}
	connection := &bufferConn{}
	recipient := &gameSession{
		conn:                connection,
		connID:              "mailbox-online-recipient",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 88,
	}
	service := &Service{
		options: options{
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		gameSessions:       map[uint16]*gameSession{88: recipient},
	}

	if err := service.sendMailboxAlarmToOnlineRecipient(88); err != nil {
		t.Fatalf("send mailbox alarm: %v", err)
	}
	packet, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || packet.Header.MsgID != 0x0063 || packet.Header.Classification != 0 ||
		!bytes.Equal(packet.Body, []byte{0x00, 0x00, 0x00, 0x01, 0x00}) {
		t.Fatalf("class0/0x63 packet header=%+v body=% X trailing=% X", packet.Header, packet.Body, trailing)
	}
}

func TestMailboxAlarmForSessionClearsEmptyMailboxState(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	connection := &bufferConn{}
	session := &gameSession{
		conn:                connection,
		connID:              "mailbox-empty-state",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 88,
	}
	service := &Service{
		options: options{
			gameUpperHeader:    gameUpperHeaderServer16,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}

	if err := service.sendMailboxAlarmForSession(session, 88, "test_empty_mailbox"); err != nil {
		t.Fatalf("send empty mailbox alarm: %v", err)
	}
	packet, trailing := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(trailing) != 0 || packet.Header.MsgID != 0x0063 || packet.Header.Classification != 0 ||
		!bytes.Equal(packet.Body, []byte{0x00, 0x00, 0x00, 0x00, 0x00}) {
		t.Fatalf("empty class0/0x63 packet header=%+v body=% X trailing=% X", packet.Header, packet.Body, trailing)
	}
}

func TestInitialTownSceneTailSendsMailboxAlarmOnce(t *testing.T) {
	service, session := newDeferredTailResumeFixture(t)
	repositories, available := service.repositoryGroup()
	if !available {
		t.Fatal("mailbox repositories are unavailable")
	}
	characterID := "29"
	if err := repositories.Mailbox.Save(context.Background(), dnfrepo.MailboxRecord{
		CharacterID: characterID,
		Mails: map[string]dnfrepo.MailRecord{
			"1": {MailID: "1", CreatedAt: time.Now().UTC()},
			"2": {MailID: "2", Read: true, CreatedAt: time.Now().UTC()},
		},
	}); err != nil {
		t.Fatalf("save mailbox: %v", err)
	}

	if err := service.sendDeferredSelectSceneTail(session, "initial_town_mailbox_alarm"); err != nil {
		t.Fatalf("send initial town scene tail: %v", err)
	}
	packets := splitAllCurrentUpperPackets(t, session.conn.(*bufferConn).write.Bytes())
	alarmCount := 0
	alarmIndex := -1
	rentalIndex := -1
	for index, packet := range packets {
		if packet.Header.Classification != 0 {
			continue
		}
		if packet.Header.MsgID == currentRentalStateMsgID {
			rentalIndex = index
		}
		if packet.Header.MsgID == 0x0063 {
			alarmCount++
			alarmIndex = index
			if !bytes.Equal(packet.Body, []byte{0x01, 0x00}) {
				t.Fatalf("initial mailbox alarm body=% X, want 01 00", packet.Body)
			}
		}
	}
	if alarmCount != 1 || alarmIndex <= rentalIndex {
		t.Fatalf("initial mailbox alarm count=%d index=%d rental_index=%d", alarmCount, alarmIndex, rentalIndex)
	}

	firstWriteLen := session.conn.(*bufferConn).write.Len()
	if err := service.sendDeferredSelectSceneTail(session, "duplicate_initial_town_mailbox_alarm"); err != nil {
		t.Fatalf("repeat initial town scene tail: %v", err)
	}
	if session.conn.(*bufferConn).write.Len() != firstWriteLen {
		t.Fatalf("duplicate initial town scene tail replayed mailbox alarm")
	}
}
